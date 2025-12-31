package repository

import (
	"time"

	"github.com/google/uuid"
)

// User represents a user account in the database
type User struct {
	ID           uuid.UUID  `db:"id"`
	Email        string     `db:"email"`
	PasswordHash string     `db:"password_hash"`
	CreatedAt    time.Time  `db:"created_at"`
	UpdatedAt    time.Time  `db:"updated_at"`
	LastLoginAt  *time.Time `db:"last_login_at"`
	IsActive     bool       `db:"is_active"`
}

// Session represents an authentication session in the database
type Session struct {
	ID        uuid.UUID  `db:"id"`
	UserID    uuid.UUID  `db:"user_id"`
	TokenHash string     `db:"token_hash"`
	ExpiresAt time.Time  `db:"expires_at"`
	CreatedAt time.Time  `db:"created_at"`
	IPAddress *string    `db:"ip_address"`
	UserAgent *string    `db:"user_agent"`
}

// FailedLoginAttempt represents a failed login attempt for brute force protection
type FailedLoginAttempt struct {
	ID          uuid.UUID `db:"id"`
	Email       string    `db:"email"`
	IPAddress   string    `db:"ip_address"`
	AttemptedAt time.Time `db:"attempted_at"`
}
