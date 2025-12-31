package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// TokenType represents the type of JWT token
type TokenType string

const (
	AccessTokenType  TokenType = "access"
	RefreshTokenType TokenType = "refresh"
)

// Claims represents the JWT claims structure
type Claims struct {
	Email  string    `json:"email,omitempty"`
	Type   TokenType `json:"type"`
	jwt.RegisteredClaims
}

// UserID returns the user ID from the Subject claim
func (c *Claims) UserID() string {
	return c.Subject
}

// TokenService handles JWT token generation and validation
type TokenService struct {
	accessSecret       string
	refreshSecret      string
	accessTokenExpiry  time.Duration
	refreshTokenExpiry time.Duration
	issuer             string
}

// TokenServiceConfig holds configuration for TokenService
type TokenServiceConfig struct {
	AccessSecret       string
	RefreshSecret      string
	AccessTokenExpiry  time.Duration
	RefreshTokenExpiry time.Duration
	Issuer             string
}

// NewTokenService creates a new TokenService instance
func NewTokenService(cfg TokenServiceConfig) *TokenService {
	return &TokenService{
		accessSecret:       cfg.AccessSecret,
		refreshSecret:      cfg.RefreshSecret,
		accessTokenExpiry:  cfg.AccessTokenExpiry,
		refreshTokenExpiry: cfg.RefreshTokenExpiry,
		issuer:             cfg.Issuer,
	}
}

// TokenPair represents a pair of access and refresh tokens
type TokenPair struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int64 // Access token expiry in seconds
}

// GenerateAccessToken generates a new access token for the given user
func (s *TokenService) GenerateAccessToken(userID, email string) (string, error) {
	now := time.Now()
	expiresAt := now.Add(s.accessTokenExpiry)

	claims := Claims{
		Email: email,
		Type:  AccessTokenType,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.issuer,
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.accessSecret))
}

// GenerateRefreshToken generates a new refresh token for the given user
func (s *TokenService) GenerateRefreshToken(userID string) (string, error) {
	now := time.Now()
	expiresAt := now.Add(s.refreshTokenExpiry)

	claims := Claims{
		Type: RefreshTokenType,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.issuer,
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			ID:        uuid.New().String(), // Unique ID for refresh token
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.refreshSecret))
}


// GenerateTokenPair generates both access and refresh tokens
func (s *TokenService) GenerateTokenPair(userID, email string) (*TokenPair, error) {
	accessToken, err := s.GenerateAccessToken(userID, email)
	if err != nil {
		return nil, err
	}

	refreshToken, err := s.GenerateRefreshToken(userID)
	if err != nil {
		return nil, err
	}

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    int64(s.accessTokenExpiry.Seconds()),
	}, nil
}

// ValidateAccessToken validates an access token and returns the claims
func (s *TokenService) ValidateAccessToken(tokenString string) (*Claims, error) {
	return s.validateToken(tokenString, s.accessSecret, AccessTokenType)
}

// ValidateRefreshToken validates a refresh token and returns the claims
func (s *TokenService) ValidateRefreshToken(tokenString string) (*Claims, error) {
	return s.validateToken(tokenString, s.refreshSecret, RefreshTokenType)
}

// validateToken validates a JWT token with the given secret and expected type
func (s *TokenService) validateToken(tokenString, secret string, expectedType TokenType) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		// Verify signing method is HS256
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(secret), nil
	})

	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}

	// Verify token type
	if claims.Type != expectedType {
		return nil, errors.New("invalid token type")
	}

	return claims, nil
}

// HashRefreshToken creates a SHA-256 hash of the refresh token for storage
func (s *TokenService) HashRefreshToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

// GetAccessTokenExpiry returns the access token expiry duration
func (s *TokenService) GetAccessTokenExpiry() time.Duration {
	return s.accessTokenExpiry
}

// GetRefreshTokenExpiry returns the refresh token expiry duration
func (s *TokenService) GetRefreshTokenExpiry() time.Duration {
	return s.refreshTokenExpiry
}
