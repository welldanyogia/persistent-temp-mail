package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/welldanyogia/persistent-temp-mail/backend/internal/auth"
	"pgregory.net/rapid"
)

// Test configuration for property tests
func newTestTokenService() *auth.TokenService {
	return auth.NewTokenService(auth.TokenServiceConfig{
		AccessSecret:       "test-access-secret-key-32-chars!",
		RefreshSecret:      "test-refresh-secret-key-32-char!",
		AccessTokenExpiry:  15 * time.Minute,
		RefreshTokenExpiry: 7 * 24 * time.Hour,
		Issuer:             "test-issuer",
	})
}

// Helper to create a test handler that records if it was called
func testHandler() (http.Handler, *bool) {
	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		// Verify user_id is in context
		userID, ok := ExtractUserID(r.Context())
		if !ok || userID == "" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(userID))
	})
	return handler, &called
}

// Feature: user-authentication, Property 15: Missing Auth Header Returns 401
// *For any* request to protected endpoint without Authorization header,
// the system should return 401 with AUTH_TOKEN_MISSING code.
// **Validates: Requirements 5.2**
func TestProperty15_MissingAuthHeaderReturns401(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate random request paths
		path := "/" + rapid.StringMatching(`[a-z]{3,10}`).Draw(t, "path")
		method := rapid.SampledFrom([]string{"GET", "POST", "PUT", "DELETE"}).Draw(t, "method")

		tokenService := newTestTokenService()
		middleware := NewAuthMiddleware(tokenService)
		handler, called := testHandler()

		// Create request without Authorization header
		req := httptest.NewRequest(method, path, nil)
		rec := httptest.NewRecorder()

		// Execute middleware
		middleware.Authenticate(handler).ServeHTTP(rec, req)

		// Property: Should return 401 Unauthorized
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected status 401, got %d", rec.Code)
		}

		// Property: Handler should not be called
		if *called {
			t.Error("handler should not be called when auth header is missing")
		}

		// Property: Response should contain AUTH_TOKEN_MISSING code
		var response ErrorResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		if response.Error.Code != "AUTH_TOKEN_MISSING" {
			t.Errorf("expected error code AUTH_TOKEN_MISSING, got %s", response.Error.Code)
		}

		if response.Success != false {
			t.Error("success should be false")
		}
	})
}

// Feature: user-authentication, Property 16: Invalid Token Returns 401
// *For any* request to protected endpoint with malformed, expired, or invalid JWT token,
// the system should return 401 with AUTH_TOKEN_INVALID code.
// **Validates: Requirements 5.3**
func TestProperty16_InvalidTokenReturns401(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		tokenService := newTestTokenService()
		middleware := NewAuthMiddleware(tokenService)
		handler, called := testHandler()

		// Generate various invalid token scenarios
		invalidTokenType := rapid.IntRange(0, 5).Draw(t, "invalidTokenType")

		var authHeader string
		switch invalidTokenType {
		case 0:
			// Malformed token (random string)
			authHeader = "Bearer " + rapid.StringMatching(`[a-zA-Z0-9]{20,50}`).Draw(t, "randomToken")
		case 1:
			// Missing Bearer prefix
			authHeader = rapid.StringMatching(`[a-zA-Z0-9]{20,50}`).Draw(t, "tokenWithoutBearer")
		case 2:
			// Empty token after Bearer
			authHeader = "Bearer "
		case 3:
			// Invalid JWT format (not three parts)
			authHeader = "Bearer " + rapid.StringMatching(`[a-zA-Z0-9]{10}\.[a-zA-Z0-9]{10}`).Draw(t, "twoPartToken")
		case 4:
			// Wrong prefix (not Bearer)
			authHeader = "Basic " + rapid.StringMatching(`[a-zA-Z0-9]{20,50}`).Draw(t, "basicToken")
		case 5:
			// Token signed with wrong secret
			wrongService := auth.NewTokenService(auth.TokenServiceConfig{
				AccessSecret:       "wrong-secret-key-that-is-32char!",
				RefreshSecret:      "wrong-refresh-secret-32-chars!!",
				AccessTokenExpiry:  15 * time.Minute,
				RefreshTokenExpiry: 7 * 24 * time.Hour,
				Issuer:             "wrong-issuer",
			})
			userID := rapid.StringMatching(`[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`).Draw(t, "userID")
			email := rapid.StringMatching(`[a-z]{5,10}@[a-z]{5,10}\.[a-z]{2,3}`).Draw(t, "email")
			token, _ := wrongService.GenerateAccessToken(userID, email)
			authHeader = "Bearer " + token
		}

		// Create request with invalid token
		req := httptest.NewRequest("GET", "/protected", nil)
		req.Header.Set("Authorization", authHeader)
		rec := httptest.NewRecorder()

		// Execute middleware
		middleware.Authenticate(handler).ServeHTTP(rec, req)

		// Property: Should return 401 Unauthorized
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected status 401, got %d for token type %d", rec.Code, invalidTokenType)
		}

		// Property: Handler should not be called
		if *called {
			t.Errorf("handler should not be called for invalid token type %d", invalidTokenType)
		}

		// Property: Response should contain AUTH_TOKEN_INVALID or AUTH_TOKEN_MISSING code
		var response ErrorResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		validCodes := map[string]bool{
			"AUTH_TOKEN_INVALID": true,
			"AUTH_TOKEN_MISSING": true,
		}
		if !validCodes[response.Error.Code] {
			t.Errorf("expected error code AUTH_TOKEN_INVALID or AUTH_TOKEN_MISSING, got %s", response.Error.Code)
		}

		if response.Success != false {
			t.Error("success should be false")
		}
	})
}

// Additional test: Valid token should pass through and inject user_id into context
func TestValidTokenPassesThrough(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate random user data
		userID := rapid.StringMatching(`[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`).Draw(t, "userID")
		email := rapid.StringMatching(`[a-z]{5,10}@[a-z]{5,10}\.[a-z]{2,3}`).Draw(t, "email")

		tokenService := newTestTokenService()
		middleware := NewAuthMiddleware(tokenService)

		// Generate valid access token
		accessToken, err := tokenService.GenerateAccessToken(userID, email)
		if err != nil {
			t.Fatalf("failed to generate access token: %v", err)
		}

		// Create handler that verifies context values
		var extractedUserID, extractedEmail string
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			extractedUserID, _ = ExtractUserID(r.Context())
			extractedEmail, _ = ExtractEmail(r.Context())
			w.WriteHeader(http.StatusOK)
		})

		// Create request with valid token
		req := httptest.NewRequest("GET", "/protected", nil)
		req.Header.Set("Authorization", "Bearer "+accessToken)
		rec := httptest.NewRecorder()

		// Execute middleware
		middleware.Authenticate(handler).ServeHTTP(rec, req)

		// Property: Should return 200 OK
		if rec.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rec.Code)
		}

		// Property: User ID should be injected into context
		if extractedUserID != userID {
			t.Errorf("expected userID %s in context, got %s", userID, extractedUserID)
		}

		// Property: Email should be injected into context
		if extractedEmail != email {
			t.Errorf("expected email %s in context, got %s", email, extractedEmail)
		}
	})
}
