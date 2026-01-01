package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/repository"
	"pgregory.net/rapid"
)

// Mock implementations for testing

// mockUserRepository implements repository.UserRepository for testing
type mockUserRepository struct {
	users       map[string]*repository.User
	emailExists map[string]bool
}

func newMockUserRepository() *mockUserRepository {
	return &mockUserRepository{
		users:       make(map[string]*repository.User),
		emailExists: make(map[string]bool),
	}
}

func (m *mockUserRepository) Create(ctx context.Context, user *repository.User) error {
	email := strings.ToLower(user.Email)
	if m.emailExists[email] {
		return repository.ErrEmailAlreadyExists
	}
	user.ID = uuid.New()
	user.CreatedAt = time.Now().UTC()
	user.UpdatedAt = time.Now().UTC()
	m.users[user.ID.String()] = user
	m.emailExists[email] = true
	return nil
}

func (m *mockUserRepository) GetByID(ctx context.Context, id uuid.UUID) (*repository.User, error) {
	if user, ok := m.users[id.String()]; ok {
		return user, nil
	}
	return nil, repository.ErrUserNotFound
}

func (m *mockUserRepository) GetByEmail(ctx context.Context, email string) (*repository.User, error) {
	email = strings.ToLower(email)
	for _, user := range m.users {
		if strings.ToLower(user.Email) == email {
			return user, nil
		}
	}
	return nil, repository.ErrUserNotFound
}

func (m *mockUserRepository) UpdateLastLogin(ctx context.Context, id uuid.UUID) error {
	if user, ok := m.users[id.String()]; ok {
		now := time.Now().UTC()
		user.LastLoginAt = &now
		return nil
	}
	return repository.ErrUserNotFound
}

func (m *mockUserRepository) EmailExists(ctx context.Context, email string) (bool, error) {
	return m.emailExists[strings.ToLower(email)], nil
}

func (m *mockUserRepository) Delete(ctx context.Context, id uuid.UUID) error {
	if _, ok := m.users[id.String()]; ok {
		delete(m.users, id.String())
		return nil
	}
	return repository.ErrUserNotFound
}

func (m *mockUserRepository) GetDeleteInfo(ctx context.Context, id uuid.UUID) (domainCount, aliasCount, emailCount, attachmentCount int, totalSize int64, err error) {
	if _, ok := m.users[id.String()]; !ok {
		return 0, 0, 0, 0, 0, repository.ErrUserNotFound
	}
	// Return mock values for testing
	return 0, 0, 0, 0, 0, nil
}

func (m *mockUserRepository) GetAttachmentStorageKeys(ctx context.Context, id uuid.UUID) ([]string, error) {
	if _, ok := m.users[id.String()]; !ok {
		return nil, repository.ErrUserNotFound
	}
	// Return empty slice for testing
	return []string{}, nil
}

// mockSessionRepository implements repository.SessionRepository for testing
type mockSessionRepository struct {
	sessions       map[string]*repository.Session
	failedAttempts map[string][]time.Time
}

func newMockSessionRepository() *mockSessionRepository {
	return &mockSessionRepository{
		sessions:       make(map[string]*repository.Session),
		failedAttempts: make(map[string][]time.Time),
	}
}

func (m *mockSessionRepository) Create(ctx context.Context, session *repository.Session) error {
	session.ID = uuid.New()
	session.CreatedAt = time.Now().UTC()
	m.sessions[session.TokenHash] = session
	return nil
}

func (m *mockSessionRepository) GetByTokenHash(ctx context.Context, tokenHash string) (*repository.Session, error) {
	if session, ok := m.sessions[tokenHash]; ok {
		return session, nil
	}
	return nil, repository.ErrSessionNotFound
}

func (m *mockSessionRepository) Delete(ctx context.Context, id uuid.UUID) error {
	for hash, session := range m.sessions {
		if session.ID == id {
			delete(m.sessions, hash)
			return nil
		}
	}
	return repository.ErrSessionNotFound
}

func (m *mockSessionRepository) DeleteByTokenHash(ctx context.Context, tokenHash string) error {
	if _, ok := m.sessions[tokenHash]; ok {
		delete(m.sessions, tokenHash)
		return nil
	}
	return repository.ErrSessionNotFound
}

func (m *mockSessionRepository) CountFailedAttempts(ctx context.Context, email string, since time.Time) (int, error) {
	email = strings.ToLower(email)
	count := 0
	for _, t := range m.failedAttempts[email] {
		if t.After(since) {
			count++
		}
	}
	return count, nil
}

func (m *mockSessionRepository) RecordFailedAttempt(ctx context.Context, email string, ip string) error {
	email = strings.ToLower(email)
	m.failedAttempts[email] = append(m.failedAttempts[email], time.Now().UTC())
	return nil
}

func (m *mockSessionRepository) CleanupExpiredSessions(ctx context.Context) (int64, error) {
	return 0, nil
}

func (m *mockSessionRepository) CleanupOldFailedAttempts(ctx context.Context, before time.Time) (int64, error) {
	return 0, nil
}

func (m *mockSessionRepository) DeleteByUserID(ctx context.Context, userID uuid.UUID) error {
	// Delete all sessions for the user
	for hash, session := range m.sessions {
		if session.UserID == userID {
			delete(m.sessions, hash)
		}
	}
	return nil
}

// Helper function to create a test AuthService
func newTestAuthService() (*AuthService, *mockUserRepository, *mockSessionRepository) {
	userRepo := newMockUserRepository()
	sessionRepo := newMockSessionRepository()
	tokenService := newTestTokenService()
	passwordValidator := NewPasswordValidator()

	authService := NewAuthService(userRepo, sessionRepo, tokenService, passwordValidator)
	return authService, userRepo, sessionRepo
}


// Helper function to check if password is valid
func isValidPassword(password string) bool {
	if len(password) < MinPasswordLength {
		return false
	}
	hasUpper, hasLower, hasNumber, hasSpecial := false, false, false, false
	for _, char := range password {
		switch {
		case unicode.IsUpper(char):
			hasUpper = true
		case unicode.IsLower(char):
			hasLower = true
		case unicode.IsDigit(char):
			hasNumber = true
		case unicode.IsPunct(char) || unicode.IsSymbol(char):
			hasSpecial = true
		}
	}
	return hasUpper && hasLower && hasNumber && hasSpecial
}

// Feature: user-authentication, Property 1: Valid Registration Creates User and Returns Tokens
// *For any* valid email (matching email format) and valid password (meeting all complexity requirements),
// registering should create a user in the database and return both access and refresh tokens with correct structure.
// **Validates: Requirements 1.1**
func TestProperty1_ValidRegistrationCreatesUserAndReturnsTokens(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		authService, userRepo, _ := newTestAuthService()
		ctx := context.Background()

		// Generate valid email
		localPart := rapid.StringMatching(`[a-z]{5,10}`).Draw(t, "localPart")
		domain := rapid.StringMatching(`[a-z]{5,10}`).Draw(t, "domain")
		tld := rapid.StringMatching(`[a-z]{2,3}`).Draw(t, "tld")
		email := localPart + "@" + domain + "." + tld

		// Generate valid password (meets all requirements)
		// Format: Uppercase + lowercase + number + special + padding (min 8 chars)
		upper := rapid.StringMatching(`[A-Z]{2}`).Draw(t, "upper")
		lower := rapid.StringMatching(`[a-z]{2}`).Draw(t, "lower")
		number := rapid.StringMatching(`[0-9]{2}`).Draw(t, "number")
		special := rapid.SampledFrom([]string{"!", "@", "#", "$", "%"}).Draw(t, "special")
		padding := rapid.StringMatching(`[a-z]{2}`).Draw(t, "padding")
		password := upper + lower + number + special + padding

		req := RegisterRequest{
			Email:           email,
			Password:        password,
			ConfirmPassword: password,
		}

		// Register user
		response, validationErrors, err := authService.Register(ctx, req)

		// Should not have validation errors for valid input
		if len(validationErrors) > 0 {
			t.Fatalf("unexpected validation errors: %v", validationErrors)
		}

		// Should not have error
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should return response
		if response == nil {
			t.Fatal("expected response, got nil")
		}

		// Verify user was created in repository
		exists, _ := userRepo.EmailExists(ctx, email)
		if !exists {
			t.Error("user should exist in repository after registration")
		}

		// Verify response contains valid user data
		if response.User.ID == "" {
			t.Error("user ID should not be empty")
		}
		if response.User.Email != strings.ToLower(email) {
			t.Errorf("email mismatch: expected %s, got %s", strings.ToLower(email), response.User.Email)
		}

		// Verify response contains valid tokens
		if response.Tokens.AccessToken == "" {
			t.Error("access token should not be empty")
		}
		if response.Tokens.RefreshToken == "" {
			t.Error("refresh token should not be empty")
		}
		if response.Tokens.TokenType != "Bearer" {
			t.Errorf("token type should be Bearer, got %s", response.Tokens.TokenType)
		}
		if response.Tokens.ExpiresIn <= 0 {
			t.Error("expires_in should be positive")
		}

		// Verify tokens are valid JWT format (3 parts)
		accessParts := strings.Split(response.Tokens.AccessToken, ".")
		if len(accessParts) != 3 {
			t.Errorf("access token should have 3 parts, got %d", len(accessParts))
		}
		refreshParts := strings.Split(response.Tokens.RefreshToken, ".")
		if len(refreshParts) != 3 {
			t.Errorf("refresh token should have 3 parts, got %d", len(refreshParts))
		}
	})
}

// Feature: user-authentication, Property 2: Invalid Email Format Rejected
// *For any* string that does not match valid email format (missing @, invalid domain, etc.),
// registration should return a 400 error with validation details.
// **Validates: Requirements 1.3**
func TestProperty2_InvalidEmailFormatRejected(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		authService, _, _ := newTestAuthService()
		ctx := context.Background()

		// Generate invalid email formats
		invalidEmailType := rapid.IntRange(0, 4).Draw(t, "invalidType")
		var email string
		switch invalidEmailType {
		case 0:
			// Missing @
			email = rapid.StringMatching(`[a-z]{5,10}[a-z]{5,10}\.[a-z]{2,3}`).Draw(t, "noAt")
		case 1:
			// Missing domain
			email = rapid.StringMatching(`[a-z]{5,10}@`).Draw(t, "noDomain")
		case 2:
			// Empty string
			email = ""
		case 3:
			// Just @
			email = "@"
		case 4:
			// Missing local part
			email = "@" + rapid.StringMatching(`[a-z]{5,10}\.[a-z]{2,3}`).Draw(t, "noLocal")
		}

		// Generate valid password
		password := "ValidPass1!"

		req := RegisterRequest{
			Email:           email,
			Password:        password,
			ConfirmPassword: password,
		}

		// Register user
		response, validationErrors, err := authService.Register(ctx, req)

		// Should have validation errors for invalid email
		hasEmailError := false
		for _, ve := range validationErrors {
			if ve.Field == "email" {
				hasEmailError = true
				break
			}
		}

		if !hasEmailError {
			t.Errorf("expected email validation error for invalid email: %q", email)
		}

		// Should not return response for invalid input
		if response != nil && len(validationErrors) > 0 {
			t.Error("should not return response when validation errors exist")
		}

		// Should not return error (validation errors are returned separately)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

// Feature: user-authentication, Property 4: Password Mismatch Rejected
// *For any* registration request where password and confirm_password are different strings,
// the system should return a 400 error.
// **Validates: Requirements 1.6**
func TestProperty4_PasswordMismatchRejected(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		authService, _, _ := newTestAuthService()
		ctx := context.Background()

		// Generate valid email
		email := rapid.StringMatching(`[a-z]{5,10}@[a-z]{5,10}\.[a-z]{2,3}`).Draw(t, "email")

		// Generate two different valid passwords
		password1 := "ValidPass1!"
		password2 := "DifferentPass2@"

		// Ensure passwords are different
		if password1 == password2 {
			password2 = password2 + "X"
		}

		req := RegisterRequest{
			Email:           email,
			Password:        password1,
			ConfirmPassword: password2,
		}

		// Register user
		response, validationErrors, err := authService.Register(ctx, req)

		// Should have validation error for password mismatch
		hasPasswordMismatchError := false
		for _, ve := range validationErrors {
			if ve.Field == "confirm_password" && strings.Contains(ve.Message, "match") {
				hasPasswordMismatchError = true
				break
			}
		}

		if !hasPasswordMismatchError {
			t.Error("expected password mismatch validation error")
		}

		// Should not return response for invalid input
		if response != nil && len(validationErrors) > 0 {
			t.Error("should not return response when validation errors exist")
		}

		// Should not return error (validation errors are returned separately)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

// Additional test: Duplicate email registration should return ErrEmailExists
func TestRegister_DuplicateEmail(t *testing.T) {
	authService, _, _ := newTestAuthService()
	ctx := context.Background()

	email := "test@example.com"
	password := "ValidPass1!"

	req := RegisterRequest{
		Email:           email,
		Password:        password,
		ConfirmPassword: password,
	}

	// First registration should succeed
	response1, validationErrors1, err1 := authService.Register(ctx, req)
	if err1 != nil || len(validationErrors1) > 0 {
		t.Fatalf("first registration failed: err=%v, validationErrors=%v", err1, validationErrors1)
	}
	if response1 == nil {
		t.Fatal("first registration should return response")
	}

	// Second registration with same email should fail
	_, validationErrors2, err2 := authService.Register(ctx, req)
	if len(validationErrors2) > 0 {
		t.Fatalf("unexpected validation errors: %v", validationErrors2)
	}
	if !errors.Is(err2, ErrEmailExists) {
		t.Errorf("expected ErrEmailExists, got %v", err2)
	}
}


// Feature: user-authentication, Property 6: Valid Login Returns Tokens and Updates State
// *For any* registered user with valid credentials, login should return access and refresh tokens,
// update last_login_at timestamp, and create a session record with IP and user agent.
// **Validates: Requirements 2.1, 2.4, 2.5**
func TestProperty6_ValidLoginReturnsTokensAndUpdatesState(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		authService, userRepo, sessionRepo := newTestAuthService()
		ctx := context.Background()

		// Generate valid email and password
		localPart := rapid.StringMatching(`[a-z]{5,10}`).Draw(t, "localPart")
		domain := rapid.StringMatching(`[a-z]{5,10}`).Draw(t, "domain")
		tld := rapid.StringMatching(`[a-z]{2,3}`).Draw(t, "tld")
		email := localPart + "@" + domain + "." + tld
		password := "ValidPass1!xx"

		// First register the user
		regReq := RegisterRequest{
			Email:           email,
			Password:        password,
			ConfirmPassword: password,
		}
		regResponse, _, err := authService.Register(ctx, regReq)
		if err != nil {
			t.Fatalf("registration failed: %v", err)
		}

		// Generate random IP and user agent
		ipAddress := rapid.StringMatching(`\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}`).Draw(t, "ip")
		userAgent := rapid.StringMatching(`Mozilla/[0-9]\.[0-9]`).Draw(t, "userAgent")

		// Login with valid credentials
		loginReq := LoginRequest{
			Email:    email,
			Password: password,
		}
		loginResponse, err := authService.Login(ctx, loginReq, ipAddress, userAgent)

		// Should not have error
		if err != nil {
			t.Fatalf("login failed: %v", err)
		}

		// Should return response
		if loginResponse == nil {
			t.Fatal("expected response, got nil")
		}

		// Verify response contains valid tokens
		if loginResponse.Tokens.AccessToken == "" {
			t.Error("access token should not be empty")
		}
		if loginResponse.Tokens.RefreshToken == "" {
			t.Error("refresh token should not be empty")
		}
		if loginResponse.Tokens.TokenType != "Bearer" {
			t.Errorf("token type should be Bearer, got %s", loginResponse.Tokens.TokenType)
		}

		// Verify last_login_at was updated (Requirement 2.4)
		if loginResponse.User.LastLogin == nil {
			t.Error("last_login should be set after login")
		}

		// Verify user ID matches
		if loginResponse.User.ID != regResponse.User.ID {
			t.Errorf("user ID mismatch: expected %s, got %s", regResponse.User.ID, loginResponse.User.ID)
		}

		// Verify session was created (Requirement 2.5)
		tokenHash := authService.tokenService.HashRefreshToken(loginResponse.Tokens.RefreshToken)
		session, err := sessionRepo.GetByTokenHash(ctx, tokenHash)
		if err != nil {
			t.Errorf("session should exist after login: %v", err)
		}
		if session != nil {
			if session.IPAddress == nil || *session.IPAddress != ipAddress {
				t.Error("session should have correct IP address")
			}
			if session.UserAgent == nil || *session.UserAgent != userAgent {
				t.Error("session should have correct user agent")
			}
		}

		// Verify user in repository has updated last_login
		userID, _ := uuid.Parse(loginResponse.User.ID)
		user, _ := userRepo.GetByID(ctx, userID)
		if user != nil && user.LastLoginAt == nil {
			t.Error("user last_login_at should be updated in repository")
		}
	})
}

// Feature: user-authentication, Property 7: Invalid Credentials Return 401
// *For any* login attempt with non-existent email or wrong password, the system should return
// a 401 error with INVALID_CREDENTIALS code (same error for both cases to prevent enumeration).
// **Validates: Requirements 2.2**
func TestProperty7_InvalidCredentialsReturn401(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		authService, _, _ := newTestAuthService()
		ctx := context.Background()

		// Generate valid email and password for registration
		email := rapid.StringMatching(`[a-z]{5,10}@[a-z]{5,10}\.[a-z]{2,3}`).Draw(t, "email")
		password := "ValidPass1!xx"

		// Register the user
		regReq := RegisterRequest{
			Email:           email,
			Password:        password,
			ConfirmPassword: password,
		}
		_, _, err := authService.Register(ctx, regReq)
		if err != nil {
			t.Fatalf("registration failed: %v", err)
		}

		// Test case type: 0 = wrong password, 1 = non-existent email
		testCase := rapid.IntRange(0, 1).Draw(t, "testCase")

		var loginReq LoginRequest
		switch testCase {
		case 0:
			// Wrong password
			loginReq = LoginRequest{
				Email:    email,
				Password: "WrongPassword1!",
			}
		case 1:
			// Non-existent email
			loginReq = LoginRequest{
				Email:    "nonexistent@example.com",
				Password: password,
			}
		}

		// Attempt login
		response, err := authService.Login(ctx, loginReq, "127.0.0.1", "TestAgent")

		// Should return ErrInvalidCredentials
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Errorf("expected ErrInvalidCredentials, got %v", err)
		}

		// Should not return response
		if response != nil {
			t.Error("should not return response for invalid credentials")
		}
	})
}

// Test brute force protection
func TestLogin_BruteForceProtection(t *testing.T) {
	authService, _, sessionRepo := newTestAuthService()
	ctx := context.Background()

	email := "test@example.com"
	password := "ValidPass1!xx"

	// Register user
	regReq := RegisterRequest{
		Email:           email,
		Password:        password,
		ConfirmPassword: password,
	}
	_, _, err := authService.Register(ctx, regReq)
	if err != nil {
		t.Fatalf("registration failed: %v", err)
	}

	// Simulate 5 failed login attempts
	for i := 0; i < MaxFailedAttempts; i++ {
		_, err := authService.Login(ctx, LoginRequest{
			Email:    email,
			Password: "WrongPassword!",
		}, "127.0.0.1", "TestAgent")
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Errorf("attempt %d: expected ErrInvalidCredentials, got %v", i+1, err)
		}
	}

	// Verify failed attempts were recorded
	since := time.Now().UTC().Add(-FailedAttemptWindow)
	count, _ := sessionRepo.CountFailedAttempts(ctx, email, since)
	if count < MaxFailedAttempts {
		t.Errorf("expected at least %d failed attempts, got %d", MaxFailedAttempts, count)
	}

	// Next login attempt should be blocked even with correct password
	_, err = authService.Login(ctx, LoginRequest{
		Email:    email,
		Password: password,
	}, "127.0.0.1", "TestAgent")
	if !errors.Is(err, ErrTooManyAttempts) {
		t.Errorf("expected ErrTooManyAttempts, got %v", err)
	}
}


// Feature: user-authentication, Property 10: Token Refresh Round Trip
// *For any* valid refresh token, calling refresh endpoint should return new valid access and refresh tokens,
// and the old refresh token should be invalidated.
// **Validates: Requirements 3.4**
func TestProperty10_TokenRefreshRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		authService, _, sessionRepo := newTestAuthService()
		ctx := context.Background()

		// Generate valid email and password
		email := rapid.StringMatching(`[a-z]{5,10}@[a-z]{5,10}\.[a-z]{2,3}`).Draw(t, "email")
		password := "ValidPass1!xx"

		// Register user
		regReq := RegisterRequest{
			Email:           email,
			Password:        password,
			ConfirmPassword: password,
		}
		_, _, err := authService.Register(ctx, regReq)
		if err != nil {
			t.Fatalf("registration failed: %v", err)
		}

		// Login to get tokens
		loginReq := LoginRequest{
			Email:    email,
			Password: password,
		}
		loginResponse, err := authService.Login(ctx, loginReq, "127.0.0.1", "TestAgent")
		if err != nil {
			t.Fatalf("login failed: %v", err)
		}

		oldRefreshToken := loginResponse.Tokens.RefreshToken
		oldTokenHash := authService.tokenService.HashRefreshToken(oldRefreshToken)

		// Refresh token
		newTokens, err := authService.RefreshToken(ctx, oldRefreshToken)
		if err != nil {
			t.Fatalf("refresh failed: %v", err)
		}

		// Verify new tokens are returned
		if newTokens.AccessToken == "" {
			t.Error("new access token should not be empty")
		}
		if newTokens.RefreshToken == "" {
			t.Error("new refresh token should not be empty")
		}
		if newTokens.TokenType != "Bearer" {
			t.Errorf("token type should be Bearer, got %s", newTokens.TokenType)
		}

		// Verify new tokens are different from old ones
		if newTokens.RefreshToken == oldRefreshToken {
			t.Error("new refresh token should be different from old one")
		}

		// Verify old refresh token is invalidated (session deleted)
		_, err = sessionRepo.GetByTokenHash(ctx, oldTokenHash)
		if !errors.Is(err, repository.ErrSessionNotFound) {
			t.Error("old session should be deleted after refresh")
		}

		// Verify new session exists
		newTokenHash := authService.tokenService.HashRefreshToken(newTokens.RefreshToken)
		newSession, err := sessionRepo.GetByTokenHash(ctx, newTokenHash)
		if err != nil {
			t.Errorf("new session should exist: %v", err)
		}
		if newSession == nil {
			t.Error("new session should not be nil")
		}

		// Verify new access token is valid
		claims, err := authService.tokenService.ValidateAccessToken(newTokens.AccessToken)
		if err != nil {
			t.Errorf("new access token should be valid: %v", err)
		}
		if claims == nil {
			t.Error("claims should not be nil")
		}
	})
}

// Feature: user-authentication, Property 11: Invalid Refresh Token Rejected
// *For any* invalid, expired, or already-used refresh token, the refresh endpoint should return a 401 error.
// **Validates: Requirements 3.5**
func TestProperty11_InvalidRefreshTokenRejected(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		authService, _, _ := newTestAuthService()
		ctx := context.Background()

		// Test case type
		testCase := rapid.IntRange(0, 2).Draw(t, "testCase")

		var refreshToken string
		switch testCase {
		case 0:
			// Completely invalid token
			refreshToken = rapid.StringMatching(`[a-zA-Z0-9]{20,50}`).Draw(t, "invalidToken")
		case 1:
			// Malformed JWT (wrong format)
			refreshToken = "invalid.jwt.token"
		case 2:
			// Valid format but not in database (already used/deleted)
			// First create a valid token, then use it, then try to use it again
			email := rapid.StringMatching(`[a-z]{5,10}@[a-z]{5,10}\.[a-z]{2,3}`).Draw(t, "email")
			password := "ValidPass1!xx"

			// Register and login
			regReq := RegisterRequest{
				Email:           email,
				Password:        password,
				ConfirmPassword: password,
			}
			_, _, err := authService.Register(ctx, regReq)
			if err != nil {
				t.Fatalf("registration failed: %v", err)
			}

			loginResponse, err := authService.Login(ctx, LoginRequest{
				Email:    email,
				Password: password,
			}, "127.0.0.1", "TestAgent")
			if err != nil {
				t.Fatalf("login failed: %v", err)
			}

			// Use the refresh token once
			_, err = authService.RefreshToken(ctx, loginResponse.Tokens.RefreshToken)
			if err != nil {
				t.Fatalf("first refresh failed: %v", err)
			}

			// Try to use the same token again (should fail)
			refreshToken = loginResponse.Tokens.RefreshToken
		}

		// Attempt to refresh with invalid token
		_, err := authService.RefreshToken(ctx, refreshToken)

		// Should return ErrInvalidRefreshToken
		if !errors.Is(err, ErrInvalidRefreshToken) {
			t.Errorf("expected ErrInvalidRefreshToken, got %v", err)
		}
	})
}


// Feature: user-authentication, Property 13: Logout Invalidates Session
// *For any* valid logout request, the session should be deleted from database and subsequent
// requests with the same refresh token should fail.
// **Validates: Requirements 4.1, 4.2**
func TestProperty13_LogoutInvalidatesSession(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		authService, _, sessionRepo := newTestAuthService()
		ctx := context.Background()

		// Generate valid email and password
		email := rapid.StringMatching(`[a-z]{5,10}@[a-z]{5,10}\.[a-z]{2,3}`).Draw(t, "email")
		password := "ValidPass1!xx"

		// Register user
		regReq := RegisterRequest{
			Email:           email,
			Password:        password,
			ConfirmPassword: password,
		}
		_, _, err := authService.Register(ctx, regReq)
		if err != nil {
			t.Fatalf("registration failed: %v", err)
		}

		// Login to get tokens
		loginReq := LoginRequest{
			Email:    email,
			Password: password,
		}
		loginResponse, err := authService.Login(ctx, loginReq, "127.0.0.1", "TestAgent")
		if err != nil {
			t.Fatalf("login failed: %v", err)
		}

		refreshToken := loginResponse.Tokens.RefreshToken
		tokenHash := authService.tokenService.HashRefreshToken(refreshToken)

		// Verify session exists before logout
		session, err := sessionRepo.GetByTokenHash(ctx, tokenHash)
		if err != nil {
			t.Fatalf("session should exist before logout: %v", err)
		}
		if session == nil {
			t.Fatal("session should not be nil before logout")
		}

		// Logout
		err = authService.Logout(ctx, refreshToken)
		if err != nil {
			t.Fatalf("logout failed: %v", err)
		}

		// Verify session is deleted (Requirement 4.2)
		_, err = sessionRepo.GetByTokenHash(ctx, tokenHash)
		if !errors.Is(err, repository.ErrSessionNotFound) {
			t.Error("session should be deleted after logout")
		}

		// Verify subsequent refresh with same token fails
		_, err = authService.RefreshToken(ctx, refreshToken)
		if !errors.Is(err, ErrInvalidRefreshToken) {
			t.Errorf("refresh should fail after logout, got: %v", err)
		}

		// Verify subsequent logout with same token fails
		err = authService.Logout(ctx, refreshToken)
		if !errors.Is(err, ErrSessionNotFound) {
			t.Errorf("second logout should fail, got: %v", err)
		}
	})
}


// Feature: user-authentication, Property 14: Profile Returns Complete User Data
// *For any* authenticated request to /me endpoint, the response should contain all required fields:
// id, email, created_at, last_login, domain_count, alias_count, email_count.
// **Validates: Requirements 5.1**
func TestProperty14_ProfileReturnsCompleteUserData(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		authService, _, _ := newTestAuthService()
		ctx := context.Background()

		// Generate valid email and password
		email := rapid.StringMatching(`[a-z]{5,10}@[a-z]{5,10}\.[a-z]{2,3}`).Draw(t, "email")
		password := "ValidPass1!xx"

		// Register user
		regReq := RegisterRequest{
			Email:           email,
			Password:        password,
			ConfirmPassword: password,
		}
		regResponse, _, err := authService.Register(ctx, regReq)
		if err != nil {
			t.Fatalf("registration failed: %v", err)
		}

		userID := regResponse.User.ID

		// Get user profile
		profile, err := authService.GetUserProfile(ctx, userID)
		if err != nil {
			t.Fatalf("get profile failed: %v", err)
		}

		// Verify all required fields are present
		if profile.ID == "" {
			t.Error("profile ID should not be empty")
		}
		if profile.ID != userID {
			t.Errorf("profile ID mismatch: expected %s, got %s", userID, profile.ID)
		}

		if profile.Email == "" {
			t.Error("profile email should not be empty")
		}
		if profile.Email != strings.ToLower(email) {
			t.Errorf("profile email mismatch: expected %s, got %s", strings.ToLower(email), profile.Email)
		}

		if profile.CreatedAt.IsZero() {
			t.Error("profile created_at should not be zero")
		}

		// Domain, alias, and email counts should be present (even if 0)
		// These are initialized to 0 since those repositories aren't implemented yet
		if profile.DomainCount < 0 {
			t.Error("domain_count should be non-negative")
		}
		if profile.AliasCount < 0 {
			t.Error("alias_count should be non-negative")
		}
		if profile.EmailCount < 0 {
			t.Error("email_count should be non-negative")
		}

		// Test with login to verify last_login is populated
		loginReq := LoginRequest{
			Email:    email,
			Password: password,
		}
		_, err = authService.Login(ctx, loginReq, "127.0.0.1", "TestAgent")
		if err != nil {
			t.Fatalf("login failed: %v", err)
		}

		// Get profile again after login
		profileAfterLogin, err := authService.GetUserProfile(ctx, userID)
		if err != nil {
			t.Fatalf("get profile after login failed: %v", err)
		}

		// last_login should be set after login
		if profileAfterLogin.LastLogin == nil {
			t.Error("last_login should be set after login")
		}
	})
}

// Test GetUserProfile with invalid user ID
func TestGetUserProfile_InvalidUserID(t *testing.T) {
	authService, _, _ := newTestAuthService()
	ctx := context.Background()

	// Test with invalid UUID format
	_, err := authService.GetUserProfile(ctx, "invalid-uuid")
	if !errors.Is(err, ErrUserNotFound) {
		t.Errorf("expected ErrUserNotFound for invalid UUID, got %v", err)
	}

	// Test with valid UUID but non-existent user
	_, err = authService.GetUserProfile(ctx, uuid.New().String())
	if !errors.Is(err, ErrUserNotFound) {
		t.Errorf("expected ErrUserNotFound for non-existent user, got %v", err)
	}
}
