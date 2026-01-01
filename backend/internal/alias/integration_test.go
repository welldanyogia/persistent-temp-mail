//go:build integration

package alias_test

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
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/alias"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/auth"
	authmw "github.com/welldanyogia/persistent-temp-mail/backend/internal/middleware"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/repository"
)

var (
	testDB         *pgxpool.Pool
	testRouter     *chi.Mux
	authHandler    *auth.AuthHandler
	aliasHandler   *alias.Handler
	tokenService   *auth.TokenService
	domainRepo     *repository.DomainRepository
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

	setupTestRouter()

	code := m.Run()
	os.Exit(code)
}

func setupTestRouter() {
	// Initialize repositories
	userRepo := repository.NewUserRepository(testDB)
	sessionRepo := repository.NewSessionRepository(testDB)
	domainRepo = repository.NewDomainRepository(testDB)
	aliasRepo := repository.NewAliasRepository(testDB)

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

	aliasService := alias.NewService(alias.ServiceConfig{
		AliasRepository: aliasRepo,
		DomainRepo:      domainRepo,
		StorageService:  nil, // No storage service for tests
		AliasLimit:      50,
		Logger:          nil,
	})

	// Initialize handlers
	authHandler = auth.NewAuthHandler(authService)
	aliasHandler = alias.NewHandler(aliasService, nil)

	// Initialize middleware
	authMiddleware := authmw.NewAuthMiddleware(tokenService)

	// Setup router
	testRouter = chi.NewRouter()
	testRouter.Route("/api/v1", func(r chi.Router) {
		auth.RegisterRoutes(r, authHandler, authMiddleware.Authenticate)
		alias.RegisterRoutes(r, aliasHandler, authMiddleware.Authenticate)
	})
}


// cleanupTestData removes test data from the database
func cleanupTestData(t *testing.T) {
	ctx := context.Background()

	// Delete in order to respect foreign key constraints
	_, err := testDB.Exec(ctx, "DELETE FROM aliases")
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

// AliasData represents alias response data
type AliasData struct {
	ID           string     `json:"id"`
	EmailAddress string     `json:"email_address"`
	LocalPart    string     `json:"local_part"`
	DomainID     string     `json:"domain_id"`
	DomainName   string     `json:"domain_name"`
	Description  *string    `json:"description,omitempty"`
	IsActive     bool       `json:"is_active"`
	CreatedAt    time.Time  `json:"created_at"`
	EmailCount   int        `json:"email_count"`
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

// createVerifiedDomain creates a verified domain for testing
func createVerifiedDomain(t *testing.T, userID uuid.UUID, domainName string) uuid.UUID {
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


// TestIntegration_FullAliasCRUDFlow tests the complete alias CRUD flow
// Requirements: All (1.1-1.9, 2.1-2.6, 3.1-3.4, 4.1-4.5, 5.1-5.5)
func TestIntegration_FullAliasCRUDFlow(t *testing.T) {
	cleanupTestData(t)
	defer cleanupTestData(t)

	email := fmt.Sprintf("alias_test_%d@example.com", time.Now().UnixNano())
	password := "ValidPass1!"
	domainName := fmt.Sprintf("test%d.example.com", time.Now().UnixNano())

	// Setup: Register user and create verified domain
	accessToken := registerAndLogin(t, email, password)
	userID := getUserIDFromToken(t, accessToken)
	domainID := createVerifiedDomain(t, userID, domainName)

	var aliasID string

	// Step 1: Create alias
	t.Run("CreateAlias", func(t *testing.T) {
		reqBody := map[string]string{
			"local_part": "test-alias",
			"domain_id":  domainID.String(),
		}

		rr := makeRequest(t, "POST", "/api/v1/aliases", reqBody, accessToken)

		if rr.Code != http.StatusCreated {
			t.Errorf("Expected status 201, got %d. Body: %s", rr.Code, rr.Body.String())
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
			Alias AliasData `json:"alias"`
		}
		if err := json.Unmarshal(resp.Data, &data); err != nil {
			t.Fatalf("Failed to parse alias data: %v", err)
		}

		aliasID = data.Alias.ID
		if aliasID == "" {
			t.Error("Alias ID should not be empty")
		}
		if data.Alias.EmailAddress != "test-alias@"+domainName {
			t.Errorf("Expected email address test-alias@%s, got %s", domainName, data.Alias.EmailAddress)
		}
		if !data.Alias.IsActive {
			t.Error("New alias should be active by default")
		}
	})

	// Step 2: List aliases
	t.Run("ListAliases", func(t *testing.T) {
		rr := makeRequest(t, "GET", "/api/v1/aliases", nil, accessToken)

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
			Aliases    []AliasData `json:"aliases"`
			Pagination struct {
				CurrentPage int `json:"current_page"`
				PerPage     int `json:"per_page"`
				TotalCount  int `json:"total_count"`
			} `json:"pagination"`
		}
		if err := json.Unmarshal(resp.Data, &data); err != nil {
			t.Fatalf("Failed to parse list data: %v", err)
		}

		if len(data.Aliases) != 1 {
			t.Errorf("Expected 1 alias, got %d", len(data.Aliases))
		}
		if data.Pagination.TotalCount != 1 {
			t.Errorf("Expected total count 1, got %d", data.Pagination.TotalCount)
		}
	})

	// Step 3: Get alias by ID
	t.Run("GetAliasDetails", func(t *testing.T) {
		if aliasID == "" {
			t.Skip("No alias ID available")
		}

		rr := makeRequest(t, "GET", "/api/v1/aliases/"+aliasID, nil, accessToken)

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

	// Step 4: Update alias
	t.Run("UpdateAlias", func(t *testing.T) {
		if aliasID == "" {
			t.Skip("No alias ID available")
		}

		description := "Test description"
		reqBody := map[string]interface{}{
			"is_active":   false,
			"description": description,
		}

		rr := makeRequest(t, "PUT", "/api/v1/aliases/"+aliasID, reqBody, accessToken)

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
			Alias AliasData `json:"alias"`
		}
		if err := json.Unmarshal(resp.Data, &data); err != nil {
			t.Fatalf("Failed to parse alias data: %v", err)
		}

		if data.Alias.IsActive {
			t.Error("Alias should be inactive after update")
		}
		if data.Alias.Description == nil || *data.Alias.Description != description {
			t.Errorf("Expected description '%s', got %v", description, data.Alias.Description)
		}
	})

	// Step 5: Delete alias
	t.Run("DeleteAlias", func(t *testing.T) {
		if aliasID == "" {
			t.Skip("No alias ID available")
		}

		rr := makeRequest(t, "DELETE", "/api/v1/aliases/"+aliasID, nil, accessToken)

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

	// Step 6: Verify alias is deleted
	t.Run("VerifyDeleted", func(t *testing.T) {
		if aliasID == "" {
			t.Skip("No alias ID available")
		}

		rr := makeRequest(t, "GET", "/api/v1/aliases/"+aliasID, nil, accessToken)

		if rr.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d. Body: %s", rr.Code, rr.Body.String())
		}
	})
}


// TestIntegration_DomainVerificationCheck tests that unverified domains cannot have aliases
// Requirements: 1.6
func TestIntegration_DomainVerificationCheck(t *testing.T) {
	cleanupTestData(t)
	defer cleanupTestData(t)

	email := fmt.Sprintf("domain_verify_test_%d@example.com", time.Now().UnixNano())
	password := "ValidPass1!"
	domainName := fmt.Sprintf("unverified%d.example.com", time.Now().UnixNano())

	accessToken := registerAndLogin(t, email, password)
	userID := getUserIDFromToken(t, accessToken)

	// Create unverified domain
	ctx := context.Background()
	domainID := uuid.New()
	now := time.Now().UTC()

	_, err := testDB.Exec(ctx, `
		INSERT INTO domains (id, user_id, domain_name, verification_token, is_verified, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, domainID, userID, domainName, "test-token", false, now, now)

	if err != nil {
		t.Fatalf("Failed to create unverified domain: %v", err)
	}

	// Try to create alias on unverified domain
	reqBody := map[string]string{
		"local_part": "test-alias",
		"domain_id":  domainID.String(),
	}

	rr := makeRequest(t, "POST", "/api/v1/aliases", reqBody, accessToken)

	if rr.Code != http.StatusForbidden {
		t.Errorf("Expected status 403, got %d. Body: %s", rr.Code, rr.Body.String())
		return
	}

	var resp APIResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp.Error == nil || resp.Error.Code != "DOMAIN_NOT_VERIFIED" {
		t.Errorf("Expected error code DOMAIN_NOT_VERIFIED, got %+v", resp.Error)
	}
}

// TestIntegration_AliasLimitEnforcement tests the alias limit per user
// Requirements: 7.1
func TestIntegration_AliasLimitEnforcement(t *testing.T) {
	cleanupTestData(t)
	defer cleanupTestData(t)

	email := fmt.Sprintf("limit_test_%d@example.com", time.Now().UnixNano())
	password := "ValidPass1!"
	domainName := fmt.Sprintf("limit%d.example.com", time.Now().UnixNano())

	accessToken := registerAndLogin(t, email, password)
	userID := getUserIDFromToken(t, accessToken)
	domainID := createVerifiedDomain(t, userID, domainName)

	// Create 50 aliases directly in DB to reach limit
	ctx := context.Background()
	now := time.Now().UTC()

	for i := 0; i < 50; i++ {
		aliasID := uuid.New()
		localPart := fmt.Sprintf("alias%d", i)
		fullAddress := fmt.Sprintf("%s@%s", localPart, domainName)

		_, err := testDB.Exec(ctx, `
			INSERT INTO aliases (id, user_id, domain_id, local_part, full_address, is_active, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		`, aliasID, userID, domainID, localPart, fullAddress, true, now, now)

		if err != nil {
			t.Fatalf("Failed to create alias %d: %v", i, err)
		}
	}

	// Try to create one more alias
	reqBody := map[string]string{
		"local_part": "one-more-alias",
		"domain_id":  domainID.String(),
	}

	rr := makeRequest(t, "POST", "/api/v1/aliases", reqBody, accessToken)

	if rr.Code != http.StatusPaymentRequired {
		t.Errorf("Expected status 402, got %d. Body: %s", rr.Code, rr.Body.String())
		return
	}

	var resp APIResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp.Error == nil || resp.Error.Code != "ALIAS_LIMIT_REACHED" {
		t.Errorf("Expected error code ALIAS_LIMIT_REACHED, got %+v", resp.Error)
	}
}

// TestIntegration_AliasUniqueness tests global uniqueness of full_address
// Requirements: 1.7, 7.3
func TestIntegration_AliasUniqueness(t *testing.T) {
	cleanupTestData(t)
	defer cleanupTestData(t)

	// Create two users
	email1 := fmt.Sprintf("user1_%d@example.com", time.Now().UnixNano())
	email2 := fmt.Sprintf("user2_%d@example.com", time.Now().UnixNano())
	password := "ValidPass1!"
	domainName := fmt.Sprintf("unique%d.example.com", time.Now().UnixNano())

	accessToken1 := registerAndLogin(t, email1, password)
	userID1 := getUserIDFromToken(t, accessToken1)
	domainID1 := createVerifiedDomain(t, userID1, domainName)

	accessToken2 := registerAndLogin(t, email2, password)
	userID2 := getUserIDFromToken(t, accessToken2)
	// Create same domain for user2 (different domain ID but same name)
	domainID2 := createVerifiedDomain(t, userID2, domainName+"2")

	// User 1 creates alias
	reqBody := map[string]string{
		"local_part": "shared-alias",
		"domain_id":  domainID1.String(),
	}

	rr := makeRequest(t, "POST", "/api/v1/aliases", reqBody, accessToken1)
	if rr.Code != http.StatusCreated {
		t.Fatalf("User 1 failed to create alias: %s", rr.Body.String())
	}

	// User 1 tries to create same alias again
	rr = makeRequest(t, "POST", "/api/v1/aliases", reqBody, accessToken1)

	if rr.Code != http.StatusConflict {
		t.Errorf("Expected status 409, got %d. Body: %s", rr.Code, rr.Body.String())
		return
	}

	var resp APIResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp.Error == nil || resp.Error.Code != "ALIAS_EXISTS" {
		t.Errorf("Expected error code ALIAS_EXISTS, got %+v", resp.Error)
	}

	// User 2 can create alias with same local_part but different domain
	reqBody2 := map[string]string{
		"local_part": "shared-alias",
		"domain_id":  domainID2.String(),
	}

	rr = makeRequest(t, "POST", "/api/v1/aliases", reqBody2, accessToken2)
	if rr.Code != http.StatusCreated {
		t.Errorf("User 2 should be able to create alias with different domain: %s", rr.Body.String())
	}
}


// TestIntegration_DomainOwnershipCheck tests that users can only access their own aliases
// Requirements: 1.5, 3.3, 4.4, 5.4
func TestIntegration_DomainOwnershipCheck(t *testing.T) {
	cleanupTestData(t)
	defer cleanupTestData(t)

	// Create two users
	email1 := fmt.Sprintf("owner1_%d@example.com", time.Now().UnixNano())
	email2 := fmt.Sprintf("owner2_%d@example.com", time.Now().UnixNano())
	password := "ValidPass1!"
	domainName := fmt.Sprintf("owner%d.example.com", time.Now().UnixNano())

	accessToken1 := registerAndLogin(t, email1, password)
	userID1 := getUserIDFromToken(t, accessToken1)
	domainID1 := createVerifiedDomain(t, userID1, domainName)

	accessToken2 := registerAndLogin(t, email2, password)

	// User 1 creates alias
	reqBody := map[string]string{
		"local_part": "private-alias",
		"domain_id":  domainID1.String(),
	}

	rr := makeRequest(t, "POST", "/api/v1/aliases", reqBody, accessToken1)
	if rr.Code != http.StatusCreated {
		t.Fatalf("User 1 failed to create alias: %s", rr.Body.String())
	}

	var resp APIResponse
	json.Unmarshal(rr.Body.Bytes(), &resp)
	var data struct {
		Alias AliasData `json:"alias"`
	}
	json.Unmarshal(resp.Data, &data)
	aliasID := data.Alias.ID

	// User 2 tries to get User 1's alias
	t.Run("GetOtherUserAlias", func(t *testing.T) {
		rr := makeRequest(t, "GET", "/api/v1/aliases/"+aliasID, nil, accessToken2)

		if rr.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d. Body: %s", rr.Code, rr.Body.String())
		}
	})

	// User 2 tries to update User 1's alias
	t.Run("UpdateOtherUserAlias", func(t *testing.T) {
		updateBody := map[string]interface{}{
			"is_active": false,
		}

		rr := makeRequest(t, "PUT", "/api/v1/aliases/"+aliasID, updateBody, accessToken2)

		if rr.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d. Body: %s", rr.Code, rr.Body.String())
		}
	})

	// User 2 tries to delete User 1's alias
	t.Run("DeleteOtherUserAlias", func(t *testing.T) {
		rr := makeRequest(t, "DELETE", "/api/v1/aliases/"+aliasID, nil, accessToken2)

		if rr.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d. Body: %s", rr.Code, rr.Body.String())
		}
	})

	// User 2 tries to create alias on User 1's domain
	t.Run("CreateAliasOnOtherUserDomain", func(t *testing.T) {
		createBody := map[string]string{
			"local_part": "hacker-alias",
			"domain_id":  domainID1.String(),
		}

		rr := makeRequest(t, "POST", "/api/v1/aliases", createBody, accessToken2)

		if rr.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d. Body: %s", rr.Code, rr.Body.String())
		}
	})
}

// TestIntegration_LocalPartValidation tests local part validation rules
// Requirements: 1.2, 1.8, 6.1-6.4
func TestIntegration_LocalPartValidation(t *testing.T) {
	cleanupTestData(t)
	defer cleanupTestData(t)

	email := fmt.Sprintf("validation_test_%d@example.com", time.Now().UnixNano())
	password := "ValidPass1!"
	domainName := fmt.Sprintf("validation%d.example.com", time.Now().UnixNano())

	accessToken := registerAndLogin(t, email, password)
	userID := getUserIDFromToken(t, accessToken)
	domainID := createVerifiedDomain(t, userID, domainName)

	testCases := []struct {
		name       string
		localPart  string
		shouldFail bool
	}{
		{"ValidSimple", "test", false},
		{"ValidWithDot", "test.alias", false},
		{"ValidWithHyphen", "test-alias", false},
		{"ValidWithUnderscore", "test_alias", false},
		{"ValidWithPlus", "test+alias", false},
		{"ValidWithNumbers", "test123", false},
		{"InvalidStartsWithDot", ".test", true},
		{"InvalidEndsWithDot", "test.", true},
		{"InvalidConsecutiveDots", "test..alias", true},
		{"InvalidSpecialChars", "test@alias", true},
		{"InvalidSpaces", "test alias", true},
		{"InvalidEmpty", "", true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			reqBody := map[string]string{
				"local_part": tc.localPart,
				"domain_id":  domainID.String(),
			}

			rr := makeRequest(t, "POST", "/api/v1/aliases", reqBody, accessToken)

			if tc.shouldFail {
				if rr.Code != http.StatusBadRequest {
					t.Errorf("Expected status 400 for invalid local_part '%s', got %d. Body: %s",
						tc.localPart, rr.Code, rr.Body.String())
				}
			} else {
				if rr.Code != http.StatusCreated && rr.Code != http.StatusConflict {
					t.Errorf("Expected status 201 or 409 for valid local_part '%s', got %d. Body: %s",
						tc.localPart, rr.Code, rr.Body.String())
				}
			}
		})
	}
}

// TestIntegration_PaginationAndFiltering tests list pagination and filtering
// Requirements: 2.1-2.6
func TestIntegration_PaginationAndFiltering(t *testing.T) {
	cleanupTestData(t)
	defer cleanupTestData(t)

	email := fmt.Sprintf("pagination_test_%d@example.com", time.Now().UnixNano())
	password := "ValidPass1!"
	domainName1 := fmt.Sprintf("domain1_%d.example.com", time.Now().UnixNano())
	domainName2 := fmt.Sprintf("domain2_%d.example.com", time.Now().UnixNano())

	accessToken := registerAndLogin(t, email, password)
	userID := getUserIDFromToken(t, accessToken)
	domainID1 := createVerifiedDomain(t, userID, domainName1)
	domainID2 := createVerifiedDomain(t, userID, domainName2)

	// Create 5 aliases on domain1 and 3 on domain2
	ctx := context.Background()
	now := time.Now().UTC()

	for i := 0; i < 5; i++ {
		aliasID := uuid.New()
		localPart := fmt.Sprintf("alias%d", i)
		fullAddress := fmt.Sprintf("%s@%s", localPart, domainName1)

		_, err := testDB.Exec(ctx, `
			INSERT INTO aliases (id, user_id, domain_id, local_part, full_address, is_active, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		`, aliasID, userID, domainID1, localPart, fullAddress, true, now.Add(time.Duration(i)*time.Second), now)

		if err != nil {
			t.Fatalf("Failed to create alias: %v", err)
		}
	}

	for i := 0; i < 3; i++ {
		aliasID := uuid.New()
		localPart := fmt.Sprintf("other%d", i)
		fullAddress := fmt.Sprintf("%s@%s", localPart, domainName2)

		_, err := testDB.Exec(ctx, `
			INSERT INTO aliases (id, user_id, domain_id, local_part, full_address, is_active, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		`, aliasID, userID, domainID2, localPart, fullAddress, true, now, now)

		if err != nil {
			t.Fatalf("Failed to create alias: %v", err)
		}
	}

	// Test pagination
	t.Run("Pagination", func(t *testing.T) {
		rr := makeRequest(t, "GET", "/api/v1/aliases?page=1&limit=3", nil, accessToken)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d. Body: %s", rr.Code, rr.Body.String())
			return
		}

		var resp APIResponse
		json.Unmarshal(rr.Body.Bytes(), &resp)

		var data struct {
			Aliases    []AliasData `json:"aliases"`
			Pagination struct {
				CurrentPage int `json:"current_page"`
				PerPage     int `json:"per_page"`
				TotalCount  int `json:"total_count"`
				TotalPages  int `json:"total_pages"`
			} `json:"pagination"`
		}
		json.Unmarshal(resp.Data, &data)

		if len(data.Aliases) != 3 {
			t.Errorf("Expected 3 aliases, got %d", len(data.Aliases))
		}
		if data.Pagination.TotalCount != 8 {
			t.Errorf("Expected total count 8, got %d", data.Pagination.TotalCount)
		}
		if data.Pagination.TotalPages != 3 {
			t.Errorf("Expected 3 total pages, got %d", data.Pagination.TotalPages)
		}
	})

	// Test domain filter
	t.Run("DomainFilter", func(t *testing.T) {
		rr := makeRequest(t, "GET", "/api/v1/aliases?domain_id="+domainID1.String(), nil, accessToken)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d. Body: %s", rr.Code, rr.Body.String())
			return
		}

		var resp APIResponse
		json.Unmarshal(rr.Body.Bytes(), &resp)

		var data struct {
			Aliases    []AliasData `json:"aliases"`
			Pagination struct {
				TotalCount int `json:"total_count"`
			} `json:"pagination"`
		}
		json.Unmarshal(resp.Data, &data)

		if data.Pagination.TotalCount != 5 {
			t.Errorf("Expected 5 aliases for domain1, got %d", data.Pagination.TotalCount)
		}
	})

	// Test search filter
	t.Run("SearchFilter", func(t *testing.T) {
		rr := makeRequest(t, "GET", "/api/v1/aliases?search=other", nil, accessToken)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d. Body: %s", rr.Code, rr.Body.String())
			return
		}

		var resp APIResponse
		json.Unmarshal(rr.Body.Bytes(), &resp)

		var data struct {
			Aliases    []AliasData `json:"aliases"`
			Pagination struct {
				TotalCount int `json:"total_count"`
			} `json:"pagination"`
		}
		json.Unmarshal(resp.Data, &data)

		if data.Pagination.TotalCount != 3 {
			t.Errorf("Expected 3 aliases matching 'other', got %d", data.Pagination.TotalCount)
		}
	})
}
