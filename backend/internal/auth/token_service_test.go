package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"pgregory.net/rapid"
)

// Test configuration for property tests
func newTestTokenService() *TokenService {
	return NewTokenService(TokenServiceConfig{
		AccessSecret:       "test-access-secret-key-32-chars!",
		RefreshSecret:      "test-refresh-secret-key-32-char!",
		AccessTokenExpiry:  15 * time.Minute,
		RefreshTokenExpiry: 7 * 24 * time.Hour,
		Issuer:             "test-issuer",
	})
}

// Feature: user-authentication, Property 8: Token Expiration Correctness
// *For any* generated token pair, the access token should have exp claim set to 15 minutes from iat,
// and refresh token should have exp claim set to 7 days from iat.
// **Validates: Requirements 3.1, 3.2**
func TestProperty8_TokenExpirationCorrectness(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate random user data
		userID := rapid.StringMatching(`[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`).Draw(t, "userID")
		email := rapid.StringMatching(`[a-z]{5,10}@[a-z]{5,10}\.[a-z]{2,3}`).Draw(t, "email")

		svc := newTestTokenService()
		beforeGeneration := time.Now()

		// Generate token pair
		tokenPair, err := svc.GenerateTokenPair(userID, email)
		if err != nil {
			t.Fatalf("failed to generate token pair: %v", err)
		}

		afterGeneration := time.Now()

		// Validate access token expiration
		accessClaims, err := svc.ValidateAccessToken(tokenPair.AccessToken)
		if err != nil {
			t.Fatalf("failed to validate access token: %v", err)
		}

		// Access token should expire in 15 minutes (with 1 second tolerance)
		expectedAccessExpiry := beforeGeneration.Add(15 * time.Minute)
		actualAccessExpiry := accessClaims.ExpiresAt.Time

		if actualAccessExpiry.Before(expectedAccessExpiry.Add(-1*time.Second)) ||
			actualAccessExpiry.After(afterGeneration.Add(15*time.Minute).Add(1*time.Second)) {
			t.Errorf("access token expiry incorrect: expected ~%v, got %v", expectedAccessExpiry, actualAccessExpiry)
		}

		// Validate refresh token expiration
		refreshClaims, err := svc.ValidateRefreshToken(tokenPair.RefreshToken)
		if err != nil {
			t.Fatalf("failed to validate refresh token: %v", err)
		}

		// Refresh token should expire in 7 days (with 1 second tolerance)
		expectedRefreshExpiry := beforeGeneration.Add(7 * 24 * time.Hour)
		actualRefreshExpiry := refreshClaims.ExpiresAt.Time

		if actualRefreshExpiry.Before(expectedRefreshExpiry.Add(-1*time.Second)) ||
			actualRefreshExpiry.After(afterGeneration.Add(7*24*time.Hour).Add(1*time.Second)) {
			t.Errorf("refresh token expiry incorrect: expected ~%v, got %v", expectedRefreshExpiry, actualRefreshExpiry)
		}

		// Verify iat is set correctly
		if accessClaims.IssuedAt == nil {
			t.Error("access token missing iat claim")
		}
		if refreshClaims.IssuedAt == nil {
			t.Error("refresh token missing iat claim")
		}
	})
}

// Feature: user-authentication, Property 9: JWT Structure Correctness
// *For any* generated JWT token, it should be signed with HS256 algorithm and contain all required claims:
// sub (user_id), email, iat, exp, and type.
// **Validates: Requirements 3.3, 7.1, 7.2, 7.3, 7.4, 7.5**
func TestProperty9_JWTStructureCorrectness(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate random user data
		userID := rapid.StringMatching(`[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`).Draw(t, "userID")
		email := rapid.StringMatching(`[a-z]{5,10}@[a-z]{5,10}\.[a-z]{2,3}`).Draw(t, "email")

		svc := newTestTokenService()

		// Generate access token
		accessToken, err := svc.GenerateAccessToken(userID, email)
		if err != nil {
			t.Fatalf("failed to generate access token: %v", err)
		}

		// Parse token without validation to inspect structure
		parser := jwt.NewParser()
		token, _, err := parser.ParseUnverified(accessToken, &Claims{})
		if err != nil {
			t.Fatalf("failed to parse access token: %v", err)
		}

		// Verify signing method is HS256
		if token.Method.Alg() != "HS256" {
			t.Errorf("expected HS256 signing method, got %s", token.Method.Alg())
		}

		// Verify claims structure
		claims, ok := token.Claims.(*Claims)
		if !ok {
			t.Fatal("failed to cast claims")
		}

		// Verify sub claim (user_id) - using Subject from RegisteredClaims
		if claims.Subject != userID {
			t.Errorf("sub claim mismatch: expected %s, got %s", userID, claims.Subject)
		}
		if claims.UserID() != userID {
			t.Errorf("UserID() mismatch: expected %s, got %s", userID, claims.UserID())
		}

		// Verify email claim
		if claims.Email != email {
			t.Errorf("email claim mismatch: expected %s, got %s", email, claims.Email)
		}

		// Verify iat claim exists
		if claims.IssuedAt == nil {
			t.Error("iat claim is missing")
		}

		// Verify exp claim exists
		if claims.ExpiresAt == nil {
			t.Error("exp claim is missing")
		}

		// Verify type claim for access token
		if claims.Type != AccessTokenType {
			t.Errorf("type claim mismatch: expected %s, got %s", AccessTokenType, claims.Type)
		}

		// Generate refresh token and verify its structure
		refreshToken, err := svc.GenerateRefreshToken(userID)
		if err != nil {
			t.Fatalf("failed to generate refresh token: %v", err)
		}

		refreshTokenParsed, _, err := parser.ParseUnverified(refreshToken, &Claims{})
		if err != nil {
			t.Fatalf("failed to parse refresh token: %v", err)
		}

		// Verify signing method is HS256
		if refreshTokenParsed.Method.Alg() != "HS256" {
			t.Errorf("expected HS256 signing method for refresh token, got %s", refreshTokenParsed.Method.Alg())
		}

		refreshClaims, ok := refreshTokenParsed.Claims.(*Claims)
		if !ok {
			t.Fatal("failed to cast refresh claims")
		}

		// Verify type claim for refresh token
		if refreshClaims.Type != RefreshTokenType {
			t.Errorf("refresh token type claim mismatch: expected %s, got %s", RefreshTokenType, refreshClaims.Type)
		}

		// Verify sub claim in refresh token
		if refreshClaims.Subject != userID {
			t.Errorf("refresh token sub claim mismatch: expected %s, got %s", userID, refreshClaims.Subject)
		}

		// Verify JWT format (three parts separated by dots)
		accessParts := strings.Split(accessToken, ".")
		if len(accessParts) != 3 {
			t.Errorf("access token should have 3 parts, got %d", len(accessParts))
		}

		refreshParts := strings.Split(refreshToken, ".")
		if len(refreshParts) != 3 {
			t.Errorf("refresh token should have 3 parts, got %d", len(refreshParts))
		}
	})
}

// Feature: user-authentication, Property 12: Refresh Token Stored as Hash
// *For any* session created during login, the stored token_hash should be SHA-256 hash of the refresh token,
// not the raw token value.
// **Validates: Requirements 3.6**
func TestProperty12_RefreshTokenStoredAsHash(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate random user ID
		userID := rapid.StringMatching(`[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`).Draw(t, "userID")

		svc := newTestTokenService()

		// Generate refresh token
		refreshToken, err := svc.GenerateRefreshToken(userID)
		if err != nil {
			t.Fatalf("failed to generate refresh token: %v", err)
		}

		// Hash the refresh token
		tokenHash := svc.HashRefreshToken(refreshToken)

		// Property 1: Hash should not equal the original token
		if tokenHash == refreshToken {
			t.Error("hash should not equal the original token")
		}

		// Property 2: Hash should be a valid hex string of SHA-256 length (64 chars)
		if len(tokenHash) != 64 {
			t.Errorf("hash length should be 64 (SHA-256 hex), got %d", len(tokenHash))
		}

		// Property 3: Hash should be deterministic (same input = same output)
		tokenHash2 := svc.HashRefreshToken(refreshToken)
		if tokenHash != tokenHash2 {
			t.Error("hash should be deterministic")
		}

		// Property 4: Different tokens should produce different hashes
		refreshToken2, err := svc.GenerateRefreshToken(userID)
		if err != nil {
			t.Fatalf("failed to generate second refresh token: %v", err)
		}
		tokenHash3 := svc.HashRefreshToken(refreshToken2)
		if tokenHash == tokenHash3 && refreshToken != refreshToken2 {
			t.Error("different tokens should produce different hashes")
		}

		// Property 5: Hash should only contain valid hex characters
		for _, c := range tokenHash {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
				t.Errorf("hash contains invalid character: %c", c)
			}
		}
	})
}
