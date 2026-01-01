package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/repository"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/storage"
)

// Auth service errors
var (
	ErrInvalidEmail         = errors.New("invalid email format")
	ErrEmailExists          = errors.New("email already exists")
	ErrPasswordMismatch     = errors.New("password and confirm_password do not match")
	ErrInvalidCredentials   = errors.New("invalid email or password")
	ErrTooManyAttempts      = errors.New("too many failed login attempts")
	ErrInvalidRefreshToken  = errors.New("invalid or expired refresh token")
	ErrSessionNotFound      = errors.New("session not found")
	ErrUserNotFound         = errors.New("user not found")
)

// Error codes for API responses
const (
	CodeValidationError     = "VALIDATION_ERROR"
	CodeEmailExists         = "EMAIL_EXISTS"
	CodeInvalidCredentials  = "INVALID_CREDENTIALS"
	CodeTooManyAttempts     = "TOO_MANY_ATTEMPTS"
	CodeInvalidRefreshToken = "INVALID_REFRESH_TOKEN"
	CodeAuthTokenMissing    = "AUTH_TOKEN_MISSING"
	CodeAuthTokenInvalid    = "AUTH_TOKEN_INVALID"
)

// Brute force protection constants
const (
	MaxFailedAttempts     = 5
	FailedAttemptWindow   = 15 * time.Minute
)

// RegisterRequest represents the registration request payload
type RegisterRequest struct {
	Email           string `json:"email"`
	Password        string `json:"password"`
	ConfirmPassword string `json:"confirm_password"`
}

// LoginRequest represents the login request payload
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// RefreshRequest represents the token refresh request payload
type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// LogoutRequest represents the logout request payload
type LogoutRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// AuthResponse represents the authentication response
type AuthResponse struct {
	User   UserResponse  `json:"user"`
	Tokens TokenResponse `json:"tokens"`
}

// TokenResponse represents the token response
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

// UserResponse represents the user data in responses
type UserResponse struct {
	ID          string     `json:"id"`
	Email       string     `json:"email"`
	CreatedAt   time.Time  `json:"created_at"`
	LastLogin   *time.Time `json:"last_login,omitempty"`
	DomainCount int        `json:"domain_count,omitempty"`
	AliasCount  int        `json:"alias_count,omitempty"`
	EmailCount  int        `json:"email_count,omitempty"`
}

// ValidationError represents a validation error with field details
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// AuthService handles authentication business logic
type AuthService struct {
	userRepo          repository.UserRepository
	sessionRepo       repository.SessionRepository
	tokenService      *TokenService
	passwordValidator *PasswordValidator
	storageService    *storage.StorageService
	logger            *slog.Logger
}

// NewAuthService creates a new AuthService instance
func NewAuthService(
	userRepo repository.UserRepository,
	sessionRepo repository.SessionRepository,
	tokenService *TokenService,
	passwordValidator *PasswordValidator,
) *AuthService {
	return &AuthService{
		userRepo:          userRepo,
		sessionRepo:       sessionRepo,
		tokenService:      tokenService,
		passwordValidator: passwordValidator,
		logger:            slog.Default(),
	}
}

// NewAuthServiceWithStorage creates a new AuthService instance with storage service
// This is used when storage cleanup is needed for user deletion
func NewAuthServiceWithStorage(
	userRepo repository.UserRepository,
	sessionRepo repository.SessionRepository,
	tokenService *TokenService,
	passwordValidator *PasswordValidator,
	storageService *storage.StorageService,
	logger *slog.Logger,
) *AuthService {
	if logger == nil {
		logger = slog.Default()
	}
	return &AuthService{
		userRepo:          userRepo,
		sessionRepo:       sessionRepo,
		tokenService:      tokenService,
		passwordValidator: passwordValidator,
		storageService:    storageService,
		logger:            logger,
	}
}


// Register creates a new user account and returns tokens
// Requirements: 1.1, 1.2, 1.3, 1.4, 1.5, 1.6, 1.7
func (s *AuthService) Register(ctx context.Context, req RegisterRequest) (*AuthResponse, []ValidationError, error) {
	var validationErrors []ValidationError

	// Validate email format (Requirement 1.3)
	email := strings.TrimSpace(strings.ToLower(req.Email))
	if !isValidEmail(email) {
		validationErrors = append(validationErrors, ValidationError{
			Field:   "email",
			Message: "Invalid email format",
		})
	}

	// Validate password complexity (Requirements 1.4, 1.5, 6.1-6.5)
	passwordErrors := s.passwordValidator.ValidatePassword(req.Password)
	for _, err := range passwordErrors {
		validationErrors = append(validationErrors, ValidationError{
			Field:   err.Field,
			Message: err.Message,
		})
	}

	// Validate password confirmation (Requirement 1.6)
	if req.Password != req.ConfirmPassword {
		validationErrors = append(validationErrors, ValidationError{
			Field:   "confirm_password",
			Message: "Password and confirm_password do not match",
		})
	}

	// Return validation errors if any
	if len(validationErrors) > 0 {
		return nil, validationErrors, nil
	}

	// Check if email already exists (Requirement 1.2)
	exists, err := s.userRepo.EmailExists(ctx, email)
	if err != nil {
		return nil, nil, err
	}
	if exists {
		return nil, nil, ErrEmailExists
	}

	// Hash password with bcrypt (Requirement 1.7)
	passwordHash, err := s.passwordValidator.HashPassword(req.Password)
	if err != nil {
		return nil, nil, err
	}

	// Create user (Requirement 1.1)
	user := &repository.User{
		Email:        email,
		PasswordHash: passwordHash,
		IsActive:     true,
	}

	if err := s.userRepo.Create(ctx, user); err != nil {
		if errors.Is(err, repository.ErrEmailAlreadyExists) {
			return nil, nil, ErrEmailExists
		}
		return nil, nil, err
	}

	// Generate tokens (Requirement 1.1)
	tokenPair, err := s.tokenService.GenerateTokenPair(user.ID.String(), user.Email)
	if err != nil {
		return nil, nil, err
	}

	return &AuthResponse{
		User: UserResponse{
			ID:        user.ID.String(),
			Email:     user.Email,
			CreatedAt: user.CreatedAt,
			LastLogin: user.LastLoginAt,
		},
		Tokens: TokenResponse{
			AccessToken:  tokenPair.AccessToken,
			RefreshToken: tokenPair.RefreshToken,
			ExpiresIn:    tokenPair.ExpiresIn,
			TokenType:    "Bearer",
		},
	}, nil, nil
}

// Login authenticates a user and returns tokens
// Requirements: 2.1, 2.2, 2.3, 2.4, 2.5
func (s *AuthService) Login(ctx context.Context, req LoginRequest, ipAddress, userAgent string) (*AuthResponse, error) {
	email := strings.TrimSpace(strings.ToLower(req.Email))

	// Check brute force protection (Requirement 2.3)
	since := time.Now().UTC().Add(-FailedAttemptWindow)
	failedAttempts, err := s.sessionRepo.CountFailedAttempts(ctx, email, since)
	if err != nil {
		return nil, err
	}
	if failedAttempts >= MaxFailedAttempts {
		return nil, ErrTooManyAttempts
	}

	// Get user by email
	user, err := s.userRepo.GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			// Record failed attempt and return generic error (prevent enumeration)
			_ = s.sessionRepo.RecordFailedAttempt(ctx, email, ipAddress)
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}

	// Verify password (Requirement 2.2)
	if err := s.passwordValidator.VerifyPassword(req.Password, user.PasswordHash); err != nil {
		// Record failed attempt
		_ = s.sessionRepo.RecordFailedAttempt(ctx, email, ipAddress)
		return nil, ErrInvalidCredentials
	}

	// Update last login (Requirement 2.4)
	if err := s.userRepo.UpdateLastLogin(ctx, user.ID); err != nil {
		return nil, err
	}

	// Generate tokens
	tokenPair, err := s.tokenService.GenerateTokenPair(user.ID.String(), user.Email)
	if err != nil {
		return nil, err
	}

	// Create session with IP and user agent (Requirement 2.5)
	tokenHash := s.tokenService.HashRefreshToken(tokenPair.RefreshToken)
	session := &repository.Session{
		UserID:    user.ID,
		TokenHash: tokenHash,
		ExpiresAt: time.Now().UTC().Add(s.tokenService.GetRefreshTokenExpiry()),
		IPAddress: &ipAddress,
		UserAgent: &userAgent,
	}

	if err := s.sessionRepo.Create(ctx, session); err != nil {
		return nil, err
	}

	// Refresh user to get updated last_login_at
	user, _ = s.userRepo.GetByID(ctx, user.ID)

	return &AuthResponse{
		User: UserResponse{
			ID:        user.ID.String(),
			Email:     user.Email,
			CreatedAt: user.CreatedAt,
			LastLogin: user.LastLoginAt,
		},
		Tokens: TokenResponse{
			AccessToken:  tokenPair.AccessToken,
			RefreshToken: tokenPair.RefreshToken,
			ExpiresIn:    tokenPair.ExpiresIn,
			TokenType:    "Bearer",
		},
	}, nil
}


// RefreshToken validates a refresh token and returns new tokens
// Requirements: 3.4, 3.5
func (s *AuthService) RefreshToken(ctx context.Context, refreshToken string) (*TokenResponse, error) {
	// Validate refresh token (Requirement 3.5)
	claims, err := s.tokenService.ValidateRefreshToken(refreshToken)
	if err != nil {
		return nil, ErrInvalidRefreshToken
	}

	// Check if session exists in database
	tokenHash := s.tokenService.HashRefreshToken(refreshToken)
	session, err := s.sessionRepo.GetByTokenHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, repository.ErrSessionNotFound) {
			return nil, ErrInvalidRefreshToken
		}
		return nil, err
	}

	// Check if session is expired
	if time.Now().UTC().After(session.ExpiresAt) {
		// Delete expired session
		_ = s.sessionRepo.Delete(ctx, session.ID)
		return nil, ErrInvalidRefreshToken
	}

	// Get user to include email in new access token
	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return nil, ErrInvalidRefreshToken
	}

	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, ErrInvalidRefreshToken
	}

	// Generate new token pair (Requirement 3.4)
	newTokenPair, err := s.tokenService.GenerateTokenPair(user.ID.String(), user.Email)
	if err != nil {
		return nil, err
	}

	// Invalidate old session and create new one
	if err := s.sessionRepo.Delete(ctx, session.ID); err != nil {
		return nil, err
	}

	// Create new session with new refresh token hash
	newTokenHash := s.tokenService.HashRefreshToken(newTokenPair.RefreshToken)
	newSession := &repository.Session{
		UserID:    user.ID,
		TokenHash: newTokenHash,
		ExpiresAt: time.Now().UTC().Add(s.tokenService.GetRefreshTokenExpiry()),
		IPAddress: session.IPAddress,
		UserAgent: session.UserAgent,
	}

	if err := s.sessionRepo.Create(ctx, newSession); err != nil {
		return nil, err
	}

	return &TokenResponse{
		AccessToken:  newTokenPair.AccessToken,
		RefreshToken: newTokenPair.RefreshToken,
		ExpiresIn:    newTokenPair.ExpiresIn,
		TokenType:    "Bearer",
	}, nil
}

// Logout invalidates a user session
// Requirements: 4.1, 4.2, 4.3
func (s *AuthService) Logout(ctx context.Context, refreshToken string) error {
	// Validate refresh token format (Requirement 4.3)
	_, err := s.tokenService.ValidateRefreshToken(refreshToken)
	if err != nil {
		return ErrInvalidRefreshToken
	}

	// Delete session by token hash (Requirement 4.2)
	tokenHash := s.tokenService.HashRefreshToken(refreshToken)
	if err := s.sessionRepo.DeleteByTokenHash(ctx, tokenHash); err != nil {
		if errors.Is(err, repository.ErrSessionNotFound) {
			return ErrSessionNotFound
		}
		return err
	}

	return nil
}

// GetUserProfile returns the user profile with usage statistics
// Requirement: 5.1
func (s *AuthService) GetUserProfile(ctx context.Context, userID string) (*UserResponse, error) {
	id, err := uuid.Parse(userID)
	if err != nil {
		return nil, ErrUserNotFound
	}

	user, err := s.userRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}

	// TODO: Get domain, alias, and email counts from respective repositories
	// For now, return zeros as those repositories are not yet implemented
	return &UserResponse{
		ID:          user.ID.String(),
		Email:       user.Email,
		CreatedAt:   user.CreatedAt,
		LastLogin:   user.LastLoginAt,
		DomainCount: 0,
		AliasCount:  0,
		EmailCount:  0,
	}, nil
}

// ValidatePassword validates a password against complexity requirements
// Returns validation errors if any
func (s *AuthService) ValidatePassword(password string) []PasswordValidationError {
	return s.passwordValidator.ValidatePassword(password)
}

// isValidEmail checks if the email format is valid
func isValidEmail(email string) bool {
	if email == "" {
		return false
	}
	_, err := mail.ParseAddress(email)
	return err == nil
}


// DeleteUserResponse represents the response after deleting a user account
type DeleteUserResponse struct {
	Message             string `json:"message"`
	UserID              string `json:"user_id"`
	DomainsDeleted      int    `json:"domains_deleted"`
	AliasesDeleted      int    `json:"aliases_deleted"`
	EmailsDeleted       int    `json:"emails_deleted"`
	AttachmentsDeleted  int    `json:"attachments_deleted"`
	TotalSizeFreedBytes int64  `json:"total_size_freed_bytes"`
}

// DeleteUser deletes a user account and all associated data
// Requirements: 4.3 (Delete user account with cascade delete)
// This includes:
// - All domains owned by the user
// - All aliases under those domains
// - All emails received by those aliases
// - All attachments from those emails (both DB records and S3 storage)
// - All sessions for the user
func (s *AuthService) DeleteUser(ctx context.Context, userID string) (*DeleteUserResponse, error) {
	id, err := uuid.Parse(userID)
	if err != nil {
		return nil, ErrUserNotFound
	}

	// Verify user exists
	_, err = s.userRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}

	// Get delete info before deletion
	domainCount, aliasCount, emailCount, attachmentCount, totalSize, err := s.userRepo.GetDeleteInfo(ctx, id)
	if err != nil {
		return nil, err
	}

	// Get attachment storage keys before deletion
	storageKeys, err := s.userRepo.GetAttachmentStorageKeys(ctx, id)
	if err != nil {
		s.logger.Warn("Failed to get attachment storage keys for user deletion", "user_id", id, "error", err)
		// Continue with deletion even if we can't get storage keys
	}

	// Delete attachments from S3 storage
	attachmentsDeletedFromStorage := 0
	if len(storageKeys) > 0 && s.storageService != nil {
		deleted, _, err := s.storageService.DeleteByKeys(ctx, storageKeys)
		if err != nil {
			s.logger.Warn("Failed to delete attachments from storage during user deletion", "user_id", id, "error", err)
			// Continue with deletion even if storage cleanup fails
		}
		attachmentsDeletedFromStorage = deleted
	}

	// Delete all sessions for the user
	if err := s.sessionRepo.DeleteByUserID(ctx, id); err != nil {
		s.logger.Warn("Failed to delete sessions during user deletion", "user_id", id, "error", err)
		// Continue with deletion even if session cleanup fails
	}

	// Delete user (CASCADE handles domains, aliases, emails, attachments in DB)
	if err := s.userRepo.Delete(ctx, id); err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}

	s.logger.Info("User account deleted",
		"user_id", id,
		"domains_deleted", domainCount,
		"aliases_deleted", aliasCount,
		"emails_deleted", emailCount,
		"attachments_deleted", attachmentCount,
		"attachments_deleted_from_storage", attachmentsDeletedFromStorage,
		"total_size_freed", totalSize,
	)

	return &DeleteUserResponse{
		Message:             "User account deleted successfully",
		UserID:              userID,
		DomainsDeleted:      domainCount,
		AliasesDeleted:      aliasCount,
		EmailsDeleted:       emailCount,
		AttachmentsDeleted:  attachmentCount,
		TotalSizeFreedBytes: totalSize,
	}, nil
}
