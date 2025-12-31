//go:build integration

package auth_test

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
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/auth"
	authmw "github.com/welldanyogia/persistent-temp-mail/backend/internal/middleware"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/repository"
)

var (
	testDB      *pgxpool.Pool
	testRouter  *chi.Mux
	authHandler *auth.AuthHandler
)

// TestMain sets up the test database and router
func TestMain(m *testing.M) {
	// Get database connection string from environment
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = "host=localhost port=5432 user=postgres password=postgres dbname=persistent_temp_mail_test sslmode=disable"
	}

	ctx := context.Background()

	// Connect to database
	var err error
	testDB, err = pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Printf("Failed to connect to test database: %v\n", err)
		os.Exit(1)
	}
	defer testDB.Close()

	// Verify connection
	if err := testDB.Ping(ctx); err != nil {
		fmt.Printf("Failed to ping test database: %v\n", err)
		os.Exit(1)
	}

	// Setup test router
	setupTestRouter()

	// Run tests
	code := m.Run()

	os.Exit(code)
}

func setupTestRouter() {
	// Initialize repositories
	userRepo := repository.NewUserRepository(testDB)
	sessionRepo := repository.NewSessionRepository(testDB)

	// Initialize services
	tokenService := auth.NewTokenService(auth.TokenServiceConfig{
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

	// Initialize handlers
	authHandler = auth.NewAuthHandler(authService)

	// Initialize middleware
	authMiddleware := authmw.NewAuthMiddleware(tokenService)

	// Setup router
	testRouter = chi.NewRouter()
	testRouter.Route("/api/v1", func(r chi.Router) {
		auth.RegisterRoutes(r, authHandler, authMiddleware.Authenticate)
	})
}

// cleanupTestData removes test data from the database
func cleanupTestData(t *testing.T) {
	ctx := context.Background()

	// Delete in order to respect foreign key constraints
	_, err := testDB.Exec(ctx, "DELETE FROM failed_login_attempts")
	if err != nil {
		t.Logf("Warning: failed to cleanup failed_login_attempts: %v", err)
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
	ID          string     `json:"id"`
	Email       string     `json:"email"`
	CreatedAt   time.Time  `json:"created_at"`
	LastLogin   *time.Time `json:"last_login,omitempty"`
	DomainCount int        `json:"domain_count,omitempty"`
	AliasCount  int        `json:"alias_count,omitempty"`
	EmailCount  int        `json:"email_count,omitempty"`
}

// TokenResponse represents token data
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

// TestIntegration_FullRegistrationLoginLogoutFlow tests the complete auth flow
// Requirements: All (1.1-1.7, 2.1-2.5, 3.4-3.5, 4.1-4.3, 5.1-5.3)
func TestIntegration_FullRegistrationLoginLogoutFlow(t *testing.T) {
	cleanupTestData(t)
	defer cleanupTestData(t)

	email := fmt.Sprintf("test_%d@example.com", time.Now().UnixNano())
	password := "ValidPass1!"

	// Step 1: Register a new user
	t.Run("Register", func(t *testing.T) {
		reqBody := map[string]string{
			"email":            email,
			"password":         password,
			"confirm_password": password,
		}

		rr := makeRequest(t, "POST", "/api/v1/auth/register", reqBody, "")

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
		}
	})

	// Step 2: Login with the registered user
	var accessToken, refreshToken string
	t.Run("Login", func(t *testing.T) {
		reqBody := map[string]string{
			"email":    email,
			"password": password,
		}

		rr := makeRequest(t, "POST", "/api/v1/auth/login", reqBody, "")

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

		var authResp AuthResponse
		if err := json.Unmarshal(resp.Data, &authResp); err != nil {
			t.Fatalf("Failed to parse auth response: %v", err)
		}

		accessToken = authResp.Tokens.AccessToken
		refreshToken = authResp.Tokens.RefreshToken

		if accessToken == "" {
			t.Error("Access token should not be empty")
		}
		if refreshToken == "" {
			t.Error("Refresh token should not be empty")
		}
		if authResp.User.LastLogin == nil {
			t.Error("Last login should be set after login")
		}
	})

	// Step 3: Get user profile with access token
	t.Run("GetProfile", func(t *testing.T) {
		if accessToken == "" {
			t.Skip("No access token available")
		}

		rr := makeRequest(t, "GET", "/api/v1/auth/me", nil, accessToken)

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

	// Step 4: Logout
	t.Run("Logout", func(t *testing.T) {
		if accessToken == "" || refreshToken == "" {
			t.Skip("No tokens available")
		}

		reqBody := map[string]string{
			"refresh_token": refreshToken,
		}

		rr := makeRequest(t, "POST", "/api/v1/auth/logout", reqBody, accessToken)

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

	// Step 5: Verify refresh token is invalidated after logout
	t.Run("RefreshAfterLogout", func(t *testing.T) {
		if refreshToken == "" {
			t.Skip("No refresh token available")
		}

		reqBody := map[string]string{
			"refresh_token": refreshToken,
		}

		rr := makeRequest(t, "POST", "/api/v1/auth/refresh", reqBody, "")

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d. Body: %s", rr.Code, rr.Body.String())
		}
	})
}

// TestIntegration_TokenRefreshFlow tests the token refresh functionality
// Requirements: 3.4, 3.5
func TestIntegration_TokenRefreshFlow(t *testing.T) {
	cleanupTestData(t)
	defer cleanupTestData(t)

	email := fmt.Sprintf("refresh_test_%d@example.com", time.Now().UnixNano())
	password := "ValidPass1!"

	// Register and login
	regBody := map[string]string{
		"email":            email,
		"password":         password,
		"confirm_password": password,
	}
	makeRequest(t, "POST", "/api/v1/auth/register", regBody, "")

	loginBody := map[string]string{
		"email":    email,
		"password": password,
	}
	loginRR := makeRequest(t, "POST", "/api/v1/auth/login", loginBody, "")

	var loginResp APIResponse
	json.Unmarshal(loginRR.Body.Bytes(), &loginResp)

	var authResp AuthResponse
	json.Unmarshal(loginResp.Data, &authResp)

	oldRefreshToken := authResp.Tokens.RefreshToken

	// Step 1: Refresh token
	var newRefreshToken string
	t.Run("RefreshToken", func(t *testing.T) {
		reqBody := map[string]string{
			"refresh_token": oldRefreshToken,
		}

		rr := makeRequest(t, "POST", "/api/v1/auth/refresh", reqBody, "")

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

		// Extract new tokens
		var tokensResp struct {
			Tokens TokenResponse `json:"tokens"`
		}
		if err := json.Unmarshal(resp.Data, &tokensResp); err != nil {
			t.Fatalf("Failed to parse tokens: %v", err)
		}

		newRefreshToken = tokensResp.Tokens.RefreshToken

		if newRefreshToken == "" {
			t.Error("New refresh token should not be empty")
		}
		if newRefreshToken == oldRefreshToken {
			t.Error("New refresh token should be different from old one")
		}
	})

	// Step 2: Old refresh token should be invalidated
	t.Run("OldTokenInvalidated", func(t *testing.T) {
		reqBody := map[string]string{
			"refresh_token": oldRefreshToken,
		}

		rr := makeRequest(t, "POST", "/api/v1/auth/refresh", reqBody, "")

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d. Body: %s", rr.Code, rr.Body.String())
		}
	})

	// Step 3: New refresh token should work
	t.Run("NewTokenWorks", func(t *testing.T) {
		if newRefreshToken == "" {
			t.Skip("No new refresh token available")
		}

		reqBody := map[string]string{
			"refresh_token": newRefreshToken,
		}

		rr := makeRequest(t, "POST", "/api/v1/auth/refresh", reqBody, "")

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d. Body: %s", rr.Code, rr.Body.String())
		}
	})
}

// TestIntegration_BruteForceProtection tests the brute force protection
// Requirements: 2.3
func TestIntegration_BruteForceProtection(t *testing.T) {
	cleanupTestData(t)
	defer cleanupTestData(t)

	email := fmt.Sprintf("bruteforce_test_%d@example.com", time.Now().UnixNano())
	password := "ValidPass1!"

	// Register user
	regBody := map[string]string{
		"email":            email,
		"password":         password,
		"confirm_password": password,
	}
	makeRequest(t, "POST", "/api/v1/auth/register", regBody, "")

	// Step 1: Make 5 failed login attempts
	t.Run("FailedAttempts", func(t *testing.T) {
		for i := 0; i < 5; i++ {
			reqBody := map[string]string{
				"email":    email,
				"password": "WrongPassword!",
			}

			rr := makeRequest(t, "POST", "/api/v1/auth/login", reqBody, "")

			if rr.Code != http.StatusUnauthorized {
				t.Errorf("Attempt %d: Expected status 401, got %d", i+1, rr.Code)
			}
		}
	})

	// Step 2: 6th attempt should be blocked even with correct password
	t.Run("AccountLocked", func(t *testing.T) {
		reqBody := map[string]string{
			"email":    email,
			"password": password,
		}

		rr := makeRequest(t, "POST", "/api/v1/auth/login", reqBody, "")

		if rr.Code != http.StatusTooManyRequests {
			t.Errorf("Expected status 429, got %d. Body: %s", rr.Code, rr.Body.String())
			return
		}

		var resp APIResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if resp.Error == nil || resp.Error.Code != "TOO_MANY_ATTEMPTS" {
			t.Errorf("Expected error code TOO_MANY_ATTEMPTS, got %+v", resp.Error)
		}
	})
}

// TestIntegration_ProtectedEndpointWithoutAuth tests accessing protected endpoints without auth
// Requirements: 5.2
func TestIntegration_ProtectedEndpointWithoutAuth(t *testing.T) {
	t.Run("NoAuthHeader", func(t *testing.T) {
		rr := makeRequest(t, "GET", "/api/v1/auth/me", nil, "")

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d. Body: %s", rr.Code, rr.Body.String())
			return
		}

		var resp APIResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if resp.Error == nil || resp.Error.Code != "AUTH_TOKEN_MISSING" {
			t.Errorf("Expected error code AUTH_TOKEN_MISSING, got %+v", resp.Error)
		}
	})

	t.Run("InvalidToken", func(t *testing.T) {
		rr := makeRequest(t, "GET", "/api/v1/auth/me", nil, "invalid-token")

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d. Body: %s", rr.Code, rr.Body.String())
			return
		}

		var resp APIResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if resp.Error == nil || resp.Error.Code != "AUTH_TOKEN_INVALID" {
			t.Errorf("Expected error code AUTH_TOKEN_INVALID, got %+v", resp.Error)
		}
	})
}

// TestIntegration_DuplicateEmailRegistration tests registering with existing email
// Requirements: 1.2
func TestIntegration_DuplicateEmailRegistration(t *testing.T) {
	cleanupTestData(t)
	defer cleanupTestData(t)

	email := fmt.Sprintf("duplicate_test_%d@example.com", time.Now().UnixNano())
	password := "ValidPass1!"

	// First registration
	regBody := map[string]string{
		"email":            email,
		"password":         password,
		"confirm_password": password,
	}
	rr := makeRequest(t, "POST", "/api/v1/auth/register", regBody, "")

	if rr.Code != http.StatusCreated {
		t.Fatalf("First registration failed: %s", rr.Body.String())
	}

	// Second registration with same email
	rr = makeRequest(t, "POST", "/api/v1/auth/register", regBody, "")

	if rr.Code != http.StatusConflict {
		t.Errorf("Expected status 409, got %d. Body: %s", rr.Code, rr.Body.String())
		return
	}

	var resp APIResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp.Error == nil || resp.Error.Code != "EMAIL_EXISTS" {
		t.Errorf("Expected error code EMAIL_EXISTS, got %+v", resp.Error)
	}
}

// TestIntegration_InvalidCredentials tests login with wrong credentials
// Requirements: 2.2
func TestIntegration_InvalidCredentials(t *testing.T) {
	cleanupTestData(t)
	defer cleanupTestData(t)

	email := fmt.Sprintf("invalid_creds_%d@example.com", time.Now().UnixNano())
	password := "ValidPass1!"

	// Register user
	regBody := map[string]string{
		"email":            email,
		"password":         password,
		"confirm_password": password,
	}
	makeRequest(t, "POST", "/api/v1/auth/register", regBody, "")

	t.Run("WrongPassword", func(t *testing.T) {
		reqBody := map[string]string{
			"email":    email,
			"password": "WrongPassword!",
		}

		rr := makeRequest(t, "POST", "/api/v1/auth/login", reqBody, "")

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d. Body: %s", rr.Code, rr.Body.String())
			return
		}

		var resp APIResponse
		json.Unmarshal(rr.Body.Bytes(), &resp)

		if resp.Error == nil || resp.Error.Code != "INVALID_CREDENTIALS" {
			t.Errorf("Expected error code INVALID_CREDENTIALS, got %+v", resp.Error)
		}
	})

	t.Run("NonExistentEmail", func(t *testing.T) {
		reqBody := map[string]string{
			"email":    "nonexistent@example.com",
			"password": password,
		}

		rr := makeRequest(t, "POST", "/api/v1/auth/login", reqBody, "")

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d. Body: %s", rr.Code, rr.Body.String())
			return
		}

		var resp APIResponse
		json.Unmarshal(rr.Body.Bytes(), &resp)

		// Same error code for both cases to prevent enumeration
		if resp.Error == nil || resp.Error.Code != "INVALID_CREDENTIALS" {
			t.Errorf("Expected error code INVALID_CREDENTIALS, got %+v", resp.Error)
		}
	})
}
