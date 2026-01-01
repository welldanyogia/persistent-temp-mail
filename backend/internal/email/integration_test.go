//go:build integration

package email_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jmoiron/sqlx"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/auth"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/email"
	authmw "github.com/welldanyogia/persistent-temp-mail/backend/internal/middleware"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/repository"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/sanitizer"
)

var (
	testDB       *pgxpool.Pool
	testSqlxDB   *sqlx.DB
	testRouter   *chi.Mux
	tokenService *auth.TokenService
)

// TestMain sets up the test database and router
func TestMain(m *testing.M) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = "host=localhost port=5432 user=postgres password=postgres dbname=persistent_temp_mail_test sslmode=disable"
	}

	ctx := context.Background()

	var err error
	testDB, err = pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Printf("Failed to connect to test database: %v\n", err)
		os.Exit(1)
	}
	defer testDB.Close()

	if err := testDB.Ping(ctx); err != nil {
		fmt.Printf("Failed to ping test database: %v\n", err)
		os.Exit(1)
	}

	// Setup sqlx connection
	testSqlxDB, err = sqlx.Connect("pgx", dbURL)
	if err != nil {
		fmt.Printf("Failed to connect to test database with sqlx: %v\n", err)
		os.Exit(1)
	}
	defer testSqlxDB.Close()

	setupTestRouter()

	code := m.Run()
	os.Exit(code)
}


func setupTestRouter() {
	// Initialize repositories
	userRepo := repository.NewUserRepository(testDB)
	sessionRepo := repository.NewSessionRepository(testDB)
	emailRepo := repository.NewEmailRepo(testSqlxDB)
	attachmentRepo := repository.NewAttachmentRepository(testSqlxDB)

	// Initialize services
	tokenService = auth.NewTokenService(auth.TokenServiceConfig{
		AccessSecret:       "test-access-secret-key-32-chars!",
		RefreshSecret:      "test-refresh-secret-key-32-chars",
		AccessTokenExpiry:  15 * time.Minute,
		RefreshTokenExpiry: 7 * 24 * time.Hour,
		Issuer:             "test-issuer",
	})

	passwordValidator := auth.NewPasswordValidator()

	authService := auth.NewAuthService(
		userRepo,
		sessionRepo,
		tokenService,
		passwordValidator,
	)

	htmlSanitizer := sanitizer.NewHTMLSanitizer()

	emailService := email.NewService(email.ServiceConfig{
		EmailRepo:      emailRepo,
		AttachmentRepo: attachmentRepo,
		StorageService: nil, // No storage service for tests
		Sanitizer:      htmlSanitizer,
		Logger:         nil,
		BaseURL:        "http://localhost:8080/api/v1",
	})

	// Initialize handlers
	authHandler := auth.NewAuthHandler(authService)
	emailHandler := email.NewHandler(emailService, nil)

	// Initialize middleware
	authMiddleware := authmw.NewAuthMiddleware(tokenService)

	// Setup router
	testRouter = chi.NewRouter()
	testRouter.Route("/api/v1", func(r chi.Router) {
		auth.RegisterRoutes(r, authHandler, authMiddleware.Authenticate)
		email.RegisterRoutes(r, emailHandler, authMiddleware.Authenticate)
	})
}

// cleanupTestData removes test data from the database
func cleanupTestData(t *testing.T) {
	ctx := context.Background()

	// Delete in order to respect foreign key constraints
	_, err := testDB.Exec(ctx, "DELETE FROM attachments")
	if err != nil {
		t.Logf("Warning: failed to cleanup attachments: %v", err)
	}

	_, err = testDB.Exec(ctx, "DELETE FROM emails")
	if err != nil {
		t.Logf("Warning: failed to cleanup emails: %v", err)
	}

	_, err = testDB.Exec(ctx, "DELETE FROM aliases")
	if err != nil {
		t.Logf("Warning: failed to cleanup aliases: %v", err)
	}

	_, err = testDB.Exec(ctx, "DELETE FROM domains")
	if err != nil {
		t.Logf("Warning: failed to cleanup domains: %v", err)
	}

	_, err = testDB.Exec(ctx, "DELETE FROM sessions")
	if err != nil {
		t.Logf("Warning: failed to cleanup sessions: %v", err)
	}

	_, err = testDB.Exec(ctx, "DELETE FROM users")
	if err != nil {
		t.Logf("Warning: failed to cleanup users: %v", err)
	}
}

// Helper function to make HTTP requests
func makeRequest(t *testing.T, method, path string, body interface{}, authToken string) *httptest.ResponseRecorder {
	var reqBody []byte
	var err error

	if body != nil {
		reqBody, err = json.Marshal(body)
		if err != nil {
			t.Fatalf("Failed to marshal request body: %v", err)
		}
	}

	req := httptest.NewRequest(method, path, bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}

	rr := httptest.NewRecorder()
	testRouter.ServeHTTP(rr, req)
	return rr
}


// APIResponse represents the standard API response
type APIResponse struct {
	Success   bool            `json:"success"`
	Data      json.RawMessage `json:"data,omitempty"`
	Error     *APIError       `json:"error,omitempty"`
	Timestamp time.Time       `json:"timestamp"`
}

// APIError represents error details
type APIError struct {
	Code    string              `json:"code"`
	Message string              `json:"message"`
	Details map[string][]string `json:"details,omitempty"`
}

// AuthResponse represents authentication response data
type AuthResponse struct {
	User   UserResponse  `json:"user"`
	Tokens TokenResponse `json:"tokens"`
}

// UserResponse represents user data
type UserResponse struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

// TokenResponse represents token data
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

// registerAndLogin creates a test user and returns access token
func registerAndLogin(t *testing.T, email, password string) string {
	regBody := map[string]string{
		"email":            email,
		"password":         password,
		"confirm_password": password,
	}
	rr := makeRequest(t, "POST", "/api/v1/auth/register", regBody, "")
	if rr.Code != http.StatusCreated {
		t.Fatalf("Failed to register user: %s", rr.Body.String())
	}

	loginBody := map[string]string{
		"email":    email,
		"password": password,
	}
	rr = makeRequest(t, "POST", "/api/v1/auth/login", loginBody, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to login: %s", rr.Body.String())
	}

	var resp APIResponse
	json.Unmarshal(rr.Body.Bytes(), &resp)

	var authResp AuthResponse
	json.Unmarshal(resp.Data, &authResp)

	return authResp.Tokens.AccessToken
}

// getUserIDFromToken extracts user ID from access token
func getUserIDFromToken(t *testing.T, accessToken string) uuid.UUID {
	claims, err := tokenService.ValidateAccessToken(accessToken)
	if err != nil {
		t.Fatalf("Failed to validate token: %v", err)
	}
	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		t.Fatalf("Failed to parse user ID: %v", err)
	}
	return userID
}

// createTestDomain creates a verified domain for testing
func createTestDomain(t *testing.T, userID uuid.UUID, domainName string) uuid.UUID {
	ctx := context.Background()

	domainID := uuid.New()
	now := time.Now().UTC()

	_, err := testDB.Exec(ctx, `
		INSERT INTO domains (id, user_id, domain_name, verification_token, is_verified, verified_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, domainID, userID, domainName, "test-token", true, now, now, now)

	if err != nil {
		t.Fatalf("Failed to create domain: %v", err)
	}

	return domainID
}

// createTestAlias creates an alias for testing
func createTestAlias(t *testing.T, userID, domainID uuid.UUID, localPart, domainName string) uuid.UUID {
	ctx := context.Background()

	aliasID := uuid.New()
	fullAddress := fmt.Sprintf("%s@%s", localPart, domainName)
	now := time.Now().UTC()

	_, err := testDB.Exec(ctx, `
		INSERT INTO aliases (id, user_id, domain_id, local_part, full_address, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, aliasID, userID, domainID, localPart, fullAddress, true, now, now)

	if err != nil {
		t.Fatalf("Failed to create alias: %v", err)
	}

	return aliasID
}

// createTestEmail creates an email for testing
func createTestEmail(t *testing.T, aliasID uuid.UUID, subject, bodyText, bodyHTML string) uuid.UUID {
	ctx := context.Background()

	emailID := uuid.New()
	now := time.Now().UTC()

	_, err := testDB.Exec(ctx, `
		INSERT INTO emails (id, alias_id, sender_address, sender_name, subject, body_html, body_text, 
		                    headers, size_bytes, is_read, received_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`, emailID, aliasID, "sender@example.com", "Test Sender", subject, bodyHTML, bodyText,
		`{"From": "sender@example.com", "To": "test@example.com"}`, 1024, false, now, now)

	if err != nil {
		t.Fatalf("Failed to create email: %v", err)
	}

	return emailID
}


// TestIntegration_FullEmailFlow tests the complete email list → get → delete flow
// Requirements: All (1.1-1.9, 2.1-2.8, 4.1-4.5)
func TestIntegration_FullEmailFlow(t *testing.T) {
	cleanupTestData(t)
	defer cleanupTestData(t)

	userEmail := fmt.Sprintf("email_test_%d@example.com", time.Now().UnixNano())
	password := "ValidPass1!"
	domainName := fmt.Sprintf("test%d.example.com", time.Now().UnixNano())

	// Setup: Register user, create domain and alias
	accessToken := registerAndLogin(t, userEmail, password)
	userID := getUserIDFromToken(t, accessToken)
	domainID := createTestDomain(t, userID, domainName)
	aliasID := createTestAlias(t, userID, domainID, "inbox", domainName)

	// Create test emails
	emailID1 := createTestEmail(t, aliasID, "Test Email 1", "This is the body of test email 1", "<p>This is the body of test email 1</p>")
	emailID2 := createTestEmail(t, aliasID, "Test Email 2", "This is the body of test email 2", "<p>This is the body of test email 2</p>")

	// Step 1: List emails
	t.Run("ListEmails", func(t *testing.T) {
		rr := makeRequest(t, "GET", "/api/v1/emails", nil, accessToken)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d. Body: %s", rr.Code, rr.Body.String())
			return
		}

		var resp APIResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if !resp.Success {
			t.Errorf("Expected success=true, got false. Error: %+v", resp.Error)
			return
		}

		var data struct {
			Emails     []json.RawMessage `json:"emails"`
			Pagination struct {
				CurrentPage int `json:"current_page"`
				PerPage     int `json:"per_page"`
				TotalCount  int `json:"total_count"`
			} `json:"pagination"`
		}
		if err := json.Unmarshal(resp.Data, &data); err != nil {
			t.Fatalf("Failed to parse list data: %v", err)
		}

		if len(data.Emails) != 2 {
			t.Errorf("Expected 2 emails, got %d", len(data.Emails))
		}
		if data.Pagination.TotalCount != 2 {
			t.Errorf("Expected total count 2, got %d", data.Pagination.TotalCount)
		}
	})

	// Step 2: Get email details
	t.Run("GetEmailDetails", func(t *testing.T) {
		rr := makeRequest(t, "GET", "/api/v1/emails/"+emailID1.String(), nil, accessToken)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d. Body: %s", rr.Code, rr.Body.String())
			return
		}

		var resp APIResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if !resp.Success {
			t.Errorf("Expected success=true, got false. Error: %+v", resp.Error)
			return
		}

		var data struct {
			Email struct {
				ID          string  `json:"id"`
				Subject     *string `json:"subject"`
				BodyHTML    *string `json:"body_html"`
				BodyText    *string `json:"body_text"`
				IsRead      bool    `json:"is_read"`
				FromAddress string  `json:"from_address"`
			} `json:"email"`
		}
		if err := json.Unmarshal(resp.Data, &data); err != nil {
			t.Fatalf("Failed to parse email data: %v", err)
		}

		if data.Email.ID != emailID1.String() {
			t.Errorf("Expected email ID %s, got %s", emailID1.String(), data.Email.ID)
		}
		if data.Email.Subject == nil || *data.Email.Subject != "Test Email 1" {
			t.Errorf("Expected subject 'Test Email 1', got %v", data.Email.Subject)
		}
		if !data.Email.IsRead {
			t.Error("Email should be marked as read after viewing")
		}
	})

	// Step 3: Delete email
	t.Run("DeleteEmail", func(t *testing.T) {
		rr := makeRequest(t, "DELETE", "/api/v1/emails/"+emailID2.String(), nil, accessToken)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d. Body: %s", rr.Code, rr.Body.String())
			return
		}

		var resp APIResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if !resp.Success {
			t.Errorf("Expected success=true, got false. Error: %+v", resp.Error)
		}
	})

	// Step 4: Verify email is deleted
	t.Run("VerifyDeleted", func(t *testing.T) {
		rr := makeRequest(t, "GET", "/api/v1/emails/"+emailID2.String(), nil, accessToken)

		if rr.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d. Body: %s", rr.Code, rr.Body.String())
		}
	})

	// Step 5: Verify list now has 1 email
	t.Run("VerifyListAfterDelete", func(t *testing.T) {
		rr := makeRequest(t, "GET", "/api/v1/emails", nil, accessToken)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d. Body: %s", rr.Code, rr.Body.String())
			return
		}

		var resp APIResponse
		json.Unmarshal(rr.Body.Bytes(), &resp)

		var data struct {
			Pagination struct {
				TotalCount int `json:"total_count"`
			} `json:"pagination"`
		}
		json.Unmarshal(resp.Data, &data)

		if data.Pagination.TotalCount != 1 {
			t.Errorf("Expected 1 email after delete, got %d", data.Pagination.TotalCount)
		}
	})
}


// TestIntegration_BulkOperations tests bulk delete and bulk mark as read
// Requirements: 5.1-5.5
func TestIntegration_BulkOperations(t *testing.T) {
	cleanupTestData(t)
	defer cleanupTestData(t)

	userEmail := fmt.Sprintf("bulk_test_%d@example.com", time.Now().UnixNano())
	password := "ValidPass1!"
	domainName := fmt.Sprintf("bulk%d.example.com", time.Now().UnixNano())

	// Setup
	accessToken := registerAndLogin(t, userEmail, password)
	userID := getUserIDFromToken(t, accessToken)
	domainID := createTestDomain(t, userID, domainName)
	aliasID := createTestAlias(t, userID, domainID, "bulk", domainName)

	// Create 5 test emails
	var emailIDs []string
	for i := 0; i < 5; i++ {
		emailID := createTestEmail(t, aliasID, fmt.Sprintf("Bulk Email %d", i), "Body text", "<p>Body</p>")
		emailIDs = append(emailIDs, emailID.String())
	}

	// Test bulk mark as read
	t.Run("BulkMarkAsRead", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"email_ids": emailIDs[:3], // Mark first 3 as read
		}

		rr := makeRequest(t, "POST", "/api/v1/emails/bulk/mark-read", reqBody, accessToken)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d. Body: %s", rr.Code, rr.Body.String())
			return
		}

		var resp APIResponse
		json.Unmarshal(rr.Body.Bytes(), &resp)

		var data struct {
			SuccessCount int      `json:"success_count"`
			FailedCount  int      `json:"failed_count"`
			FailedIDs    []string `json:"failed_ids"`
		}
		json.Unmarshal(resp.Data, &data)

		if data.SuccessCount != 3 {
			t.Errorf("Expected 3 successful, got %d", data.SuccessCount)
		}
		if data.FailedCount != 0 {
			t.Errorf("Expected 0 failed, got %d", data.FailedCount)
		}
	})

	// Test bulk delete
	t.Run("BulkDelete", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"email_ids": emailIDs[3:], // Delete last 2
		}

		rr := makeRequest(t, "POST", "/api/v1/emails/bulk/delete", reqBody, accessToken)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d. Body: %s", rr.Code, rr.Body.String())
			return
		}

		var resp APIResponse
		json.Unmarshal(rr.Body.Bytes(), &resp)

		var data struct {
			SuccessCount int      `json:"success_count"`
			FailedCount  int      `json:"failed_count"`
			FailedIDs    []string `json:"failed_ids"`
		}
		json.Unmarshal(resp.Data, &data)

		if data.SuccessCount != 2 {
			t.Errorf("Expected 2 successful, got %d", data.SuccessCount)
		}
	})

	// Verify remaining emails
	t.Run("VerifyRemainingEmails", func(t *testing.T) {
		rr := makeRequest(t, "GET", "/api/v1/emails", nil, accessToken)

		var resp APIResponse
		json.Unmarshal(rr.Body.Bytes(), &resp)

		var data struct {
			Pagination struct {
				TotalCount int `json:"total_count"`
			} `json:"pagination"`
		}
		json.Unmarshal(resp.Data, &data)

		if data.Pagination.TotalCount != 3 {
			t.Errorf("Expected 3 remaining emails, got %d", data.Pagination.TotalCount)
		}
	})

	// Test bulk limit exceeded
	t.Run("BulkLimitExceeded", func(t *testing.T) {
		// Create 101 fake IDs
		var tooManyIDs []string
		for i := 0; i < 101; i++ {
			tooManyIDs = append(tooManyIDs, uuid.New().String())
		}

		reqBody := map[string]interface{}{
			"email_ids": tooManyIDs,
		}

		rr := makeRequest(t, "POST", "/api/v1/emails/bulk/delete", reqBody, accessToken)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d. Body: %s", rr.Code, rr.Body.String())
		}
	})
}


// TestIntegration_Statistics tests inbox statistics calculation
// Requirements: 6.1-6.5
func TestIntegration_Statistics(t *testing.T) {
	cleanupTestData(t)
	defer cleanupTestData(t)

	userEmail := fmt.Sprintf("stats_test_%d@example.com", time.Now().UnixNano())
	password := "ValidPass1!"
	domainName := fmt.Sprintf("stats%d.example.com", time.Now().UnixNano())

	// Setup
	accessToken := registerAndLogin(t, userEmail, password)
	userID := getUserIDFromToken(t, accessToken)
	domainID := createTestDomain(t, userID, domainName)
	aliasID1 := createTestAlias(t, userID, domainID, "alias1", domainName)
	aliasID2 := createTestAlias(t, userID, domainID, "alias2", domainName)

	// Create emails on alias1 (3 emails, 1 read)
	emailID1 := createTestEmail(t, aliasID1, "Email 1", "Body", "<p>Body</p>")
	createTestEmail(t, aliasID1, "Email 2", "Body", "<p>Body</p>")
	createTestEmail(t, aliasID1, "Email 3", "Body", "<p>Body</p>")

	// Create emails on alias2 (2 emails, all unread)
	createTestEmail(t, aliasID2, "Email 4", "Body", "<p>Body</p>")
	createTestEmail(t, aliasID2, "Email 5", "Body", "<p>Body</p>")

	// Mark one email as read
	ctx := context.Background()
	_, err := testDB.Exec(ctx, "UPDATE emails SET is_read = true WHERE id = $1", emailID1)
	if err != nil {
		t.Fatalf("Failed to mark email as read: %v", err)
	}

	// Get statistics
	t.Run("GetStats", func(t *testing.T) {
		rr := makeRequest(t, "GET", "/api/v1/emails/stats", nil, accessToken)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d. Body: %s", rr.Code, rr.Body.String())
			return
		}

		var resp APIResponse
		json.Unmarshal(rr.Body.Bytes(), &resp)

		var data struct {
			TotalEmails    int   `json:"total_emails"`
			UnreadEmails   int   `json:"unread_emails"`
			TotalSizeBytes int64 `json:"total_size_bytes"`
			EmailsPerAlias []struct {
				AliasID    string `json:"alias_id"`
				AliasEmail string `json:"alias_email"`
				Count      int    `json:"count"`
			} `json:"emails_per_alias"`
		}
		json.Unmarshal(resp.Data, &data)

		if data.TotalEmails != 5 {
			t.Errorf("Expected 5 total emails, got %d", data.TotalEmails)
		}
		if data.UnreadEmails != 4 {
			t.Errorf("Expected 4 unread emails, got %d", data.UnreadEmails)
		}
		if len(data.EmailsPerAlias) != 2 {
			t.Errorf("Expected 2 aliases in stats, got %d", len(data.EmailsPerAlias))
		}
	})
}


// TestIntegration_AuthorizationEnforcement tests that users can only access their own emails
// Requirements: 1.9, 2.3, 4.3, 7.1, 7.2
func TestIntegration_AuthorizationEnforcement(t *testing.T) {
	cleanupTestData(t)
	defer cleanupTestData(t)

	// Create two users
	userEmail1 := fmt.Sprintf("user1_%d@example.com", time.Now().UnixNano())
	userEmail2 := fmt.Sprintf("user2_%d@example.com", time.Now().UnixNano())
	password := "ValidPass1!"
	domainName := fmt.Sprintf("auth%d.example.com", time.Now().UnixNano())

	// Setup user 1
	accessToken1 := registerAndLogin(t, userEmail1, password)
	userID1 := getUserIDFromToken(t, accessToken1)
	domainID1 := createTestDomain(t, userID1, domainName)
	aliasID1 := createTestAlias(t, userID1, domainID1, "user1", domainName)
	emailID1 := createTestEmail(t, aliasID1, "User 1 Email", "Private content", "<p>Private</p>")

	// Setup user 2
	accessToken2 := registerAndLogin(t, userEmail2, password)

	// User 2 tries to access User 1's email
	t.Run("GetOtherUserEmail", func(t *testing.T) {
		rr := makeRequest(t, "GET", "/api/v1/emails/"+emailID1.String(), nil, accessToken2)

		if rr.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d. Body: %s", rr.Code, rr.Body.String())
		}
	})

	// User 2 tries to delete User 1's email
	t.Run("DeleteOtherUserEmail", func(t *testing.T) {
		rr := makeRequest(t, "DELETE", "/api/v1/emails/"+emailID1.String(), nil, accessToken2)

		if rr.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d. Body: %s", rr.Code, rr.Body.String())
		}
	})

	// User 2 tries bulk delete on User 1's email
	t.Run("BulkDeleteOtherUserEmail", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"email_ids": []string{emailID1.String()},
		}

		rr := makeRequest(t, "POST", "/api/v1/emails/bulk/delete", reqBody, accessToken2)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d. Body: %s", rr.Code, rr.Body.String())
			return
		}

		// Should succeed but with 0 successful deletions (skipped unauthorized)
		var resp APIResponse
		json.Unmarshal(rr.Body.Bytes(), &resp)

		var data struct {
			SuccessCount int      `json:"success_count"`
			FailedCount  int      `json:"failed_count"`
			FailedIDs    []string `json:"failed_ids"`
		}
		json.Unmarshal(resp.Data, &data)

		if data.SuccessCount != 0 {
			t.Errorf("Expected 0 successful (unauthorized), got %d", data.SuccessCount)
		}
		if data.FailedCount != 1 {
			t.Errorf("Expected 1 failed (unauthorized), got %d", data.FailedCount)
		}
	})

	// User 2's list should be empty
	t.Run("ListOnlyOwnEmails", func(t *testing.T) {
		rr := makeRequest(t, "GET", "/api/v1/emails", nil, accessToken2)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d. Body: %s", rr.Code, rr.Body.String())
			return
		}

		var resp APIResponse
		json.Unmarshal(rr.Body.Bytes(), &resp)

		var data struct {
			Pagination struct {
				TotalCount int `json:"total_count"`
			} `json:"pagination"`
		}
		json.Unmarshal(resp.Data, &data)

		if data.Pagination.TotalCount != 0 {
			t.Errorf("User 2 should see 0 emails, got %d", data.Pagination.TotalCount)
		}
	})

	// Verify User 1's email still exists
	t.Run("VerifyEmailStillExists", func(t *testing.T) {
		rr := makeRequest(t, "GET", "/api/v1/emails/"+emailID1.String(), nil, accessToken1)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d. Body: %s", rr.Code, rr.Body.String())
		}
	})
}


// TestIntegration_PaginationAndFiltering tests list pagination and filtering
// Requirements: 1.1-1.9
func TestIntegration_PaginationAndFiltering(t *testing.T) {
	cleanupTestData(t)
	defer cleanupTestData(t)

	userEmail := fmt.Sprintf("filter_test_%d@example.com", time.Now().UnixNano())
	password := "ValidPass1!"
	domainName := fmt.Sprintf("filter%d.example.com", time.Now().UnixNano())

	// Setup
	accessToken := registerAndLogin(t, userEmail, password)
	userID := getUserIDFromToken(t, accessToken)
	domainID := createTestDomain(t, userID, domainName)
	aliasID := createTestAlias(t, userID, domainID, "filter", domainName)

	// Create 10 test emails with varying properties
	ctx := context.Background()
	now := time.Now().UTC()

	for i := 0; i < 10; i++ {
		emailID := uuid.New()
		subject := fmt.Sprintf("Test Email %d", i)
		isRead := i < 3 // First 3 are read

		_, err := testDB.Exec(ctx, `
			INSERT INTO emails (id, alias_id, sender_address, sender_name, subject, body_html, body_text, 
			                    headers, size_bytes, is_read, received_at, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		`, emailID, aliasID, "sender@example.com", "Sender", subject, "<p>Body</p>", "Body text",
			`{}`, int64(1000+i*100), isRead, now.Add(time.Duration(i)*time.Minute), now)

		if err != nil {
			t.Fatalf("Failed to create email %d: %v", i, err)
		}
	}

	// Test pagination
	t.Run("Pagination", func(t *testing.T) {
		rr := makeRequest(t, "GET", "/api/v1/emails?page=1&limit=3", nil, accessToken)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d. Body: %s", rr.Code, rr.Body.String())
			return
		}

		var resp APIResponse
		json.Unmarshal(rr.Body.Bytes(), &resp)

		var data struct {
			Emails     []json.RawMessage `json:"emails"`
			Pagination struct {
				CurrentPage int `json:"current_page"`
				PerPage     int `json:"per_page"`
				TotalCount  int `json:"total_count"`
				TotalPages  int `json:"total_pages"`
			} `json:"pagination"`
		}
		json.Unmarshal(resp.Data, &data)

		if len(data.Emails) != 3 {
			t.Errorf("Expected 3 emails, got %d", len(data.Emails))
		}
		if data.Pagination.TotalCount != 10 {
			t.Errorf("Expected total count 10, got %d", data.Pagination.TotalCount)
		}
		if data.Pagination.TotalPages != 4 {
			t.Errorf("Expected 4 total pages, got %d", data.Pagination.TotalPages)
		}
	})

	// Test search filter
	t.Run("SearchFilter", func(t *testing.T) {
		rr := makeRequest(t, "GET", "/api/v1/emails?search=Email+5", nil, accessToken)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d. Body: %s", rr.Code, rr.Body.String())
			return
		}

		var resp APIResponse
		json.Unmarshal(rr.Body.Bytes(), &resp)

		var data struct {
			Pagination struct {
				TotalCount int `json:"total_count"`
			} `json:"pagination"`
		}
		json.Unmarshal(resp.Data, &data)

		if data.Pagination.TotalCount != 1 {
			t.Errorf("Expected 1 email matching search, got %d", data.Pagination.TotalCount)
		}
	})

	// Test is_read filter
	t.Run("IsReadFilter", func(t *testing.T) {
		rr := makeRequest(t, "GET", "/api/v1/emails?is_read=false", nil, accessToken)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d. Body: %s", rr.Code, rr.Body.String())
			return
		}

		var resp APIResponse
		json.Unmarshal(rr.Body.Bytes(), &resp)

		var data struct {
			Pagination struct {
				TotalCount int `json:"total_count"`
			} `json:"pagination"`
		}
		json.Unmarshal(resp.Data, &data)

		if data.Pagination.TotalCount != 7 {
			t.Errorf("Expected 7 unread emails, got %d", data.Pagination.TotalCount)
		}
	})

	// Test sort by size
	t.Run("SortBySize", func(t *testing.T) {
		rr := makeRequest(t, "GET", "/api/v1/emails?sort=size&order=desc&limit=1", nil, accessToken)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d. Body: %s", rr.Code, rr.Body.String())
			return
		}

		var resp APIResponse
		json.Unmarshal(rr.Body.Bytes(), &resp)

		var data struct {
			Emails []struct {
				SizeBytes int64 `json:"size_bytes"`
			} `json:"emails"`
		}
		json.Unmarshal(resp.Data, &data)

		if len(data.Emails) != 1 {
			t.Errorf("Expected 1 email, got %d", len(data.Emails))
			return
		}

		// Largest email should be 1900 bytes (1000 + 9*100)
		if data.Emails[0].SizeBytes != 1900 {
			t.Errorf("Expected largest email to be 1900 bytes, got %d", data.Emails[0].SizeBytes)
		}
	})
}


// TestIntegration_MarkAsReadBehavior tests the mark as read functionality
// Requirements: 2.7
func TestIntegration_MarkAsReadBehavior(t *testing.T) {
	cleanupTestData(t)
	defer cleanupTestData(t)

	userEmail := fmt.Sprintf("read_test_%d@example.com", time.Now().UnixNano())
	password := "ValidPass1!"
	domainName := fmt.Sprintf("read%d.example.com", time.Now().UnixNano())

	// Setup
	accessToken := registerAndLogin(t, userEmail, password)
	userID := getUserIDFromToken(t, accessToken)
	domainID := createTestDomain(t, userID, domainName)
	aliasID := createTestAlias(t, userID, domainID, "read", domainName)
	emailID := createTestEmail(t, aliasID, "Unread Email", "Body", "<p>Body</p>")

	// Verify email is initially unread
	t.Run("InitiallyUnread", func(t *testing.T) {
		ctx := context.Background()
		var isRead bool
		err := testDB.QueryRow(ctx, "SELECT is_read FROM emails WHERE id = $1", emailID).Scan(&isRead)
		if err != nil {
			t.Fatalf("Failed to query email: %v", err)
		}
		if isRead {
			t.Error("Email should be initially unread")
		}
	})

	// Get email with mark_as_read=false
	t.Run("GetWithoutMarkingRead", func(t *testing.T) {
		rr := makeRequest(t, "GET", "/api/v1/emails/"+emailID.String()+"?mark_as_read=false", nil, accessToken)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d. Body: %s", rr.Code, rr.Body.String())
			return
		}

		// Verify still unread
		ctx := context.Background()
		var isRead bool
		err := testDB.QueryRow(ctx, "SELECT is_read FROM emails WHERE id = $1", emailID).Scan(&isRead)
		if err != nil {
			t.Fatalf("Failed to query email: %v", err)
		}
		if isRead {
			t.Error("Email should still be unread after mark_as_read=false")
		}
	})

	// Get email with default mark_as_read=true
	t.Run("GetWithMarkingRead", func(t *testing.T) {
		rr := makeRequest(t, "GET", "/api/v1/emails/"+emailID.String(), nil, accessToken)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d. Body: %s", rr.Code, rr.Body.String())
			return
		}

		// Verify now read
		ctx := context.Background()
		var isRead bool
		err := testDB.QueryRow(ctx, "SELECT is_read FROM emails WHERE id = $1", emailID).Scan(&isRead)
		if err != nil {
			t.Fatalf("Failed to query email: %v", err)
		}
		if !isRead {
			t.Error("Email should be marked as read after default get")
		}
	})
}

// TestIntegration_HTMLSanitization tests that HTML content is sanitized
// Requirements: 2.8, 7.3, 7.4
func TestIntegration_HTMLSanitization(t *testing.T) {
	cleanupTestData(t)
	defer cleanupTestData(t)

	userEmail := fmt.Sprintf("sanitize_test_%d@example.com", time.Now().UnixNano())
	password := "ValidPass1!"
	domainName := fmt.Sprintf("sanitize%d.example.com", time.Now().UnixNano())

	// Setup
	accessToken := registerAndLogin(t, userEmail, password)
	userID := getUserIDFromToken(t, accessToken)
	domainID := createTestDomain(t, userID, domainName)
	aliasID := createTestAlias(t, userID, domainID, "sanitize", domainName)

	// Create email with malicious HTML
	maliciousHTML := `<p>Hello</p><script>alert('xss')</script><img src="http://tracker.com/pixel.gif" onclick="evil()"><a href="javascript:alert('xss')">Click</a>`
	emailID := createTestEmail(t, aliasID, "Malicious Email", "Plain text", maliciousHTML)

	// Get email and verify sanitization
	t.Run("VerifySanitization", func(t *testing.T) {
		rr := makeRequest(t, "GET", "/api/v1/emails/"+emailID.String(), nil, accessToken)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d. Body: %s", rr.Code, rr.Body.String())
			return
		}

		var resp APIResponse
		json.Unmarshal(rr.Body.Bytes(), &resp)

		var data struct {
			Email struct {
				BodyHTML *string `json:"body_html"`
			} `json:"email"`
		}
		json.Unmarshal(resp.Data, &data)

		if data.Email.BodyHTML == nil {
			t.Fatal("Expected body_html to be present")
		}

		html := *data.Email.BodyHTML

		// Verify script tags are removed
		if containsString(html, "<script") {
			t.Error("Script tags should be removed")
		}

		// Verify onclick handlers are removed
		if containsString(html, "onclick") {
			t.Error("Event handlers should be removed")
		}

		// Verify external images are blocked
		if containsString(html, "http://tracker.com") {
			t.Error("External image URLs should be blocked")
		}
	})
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStringHelper(s, substr))
}

func containsStringHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
