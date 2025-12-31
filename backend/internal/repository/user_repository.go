package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Common errors
var (
	ErrUserNotFound      = errors.New("user not found")
	ErrEmailAlreadyExists = errors.New("email already exists")
)

// UserRepository defines the interface for user data access
type UserRepository interface {
	Create(ctx context.Context, user *User) error
	GetByID(ctx context.Context, id uuid.UUID) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
	UpdateLastLogin(ctx context.Context, id uuid.UUID) error
	EmailExists(ctx context.Context, email string) (bool, error)
}

// userRepository implements UserRepository using PostgreSQL
type userRepository struct {
	pool *pgxpool.Pool
}

// NewUserRepository creates a new UserRepository instance
func NewUserRepository(pool *pgxpool.Pool) UserRepository {
	return &userRepository{pool: pool}
}

// Create inserts a new user into the database
// Requirements: 1.1 (create user account)
func (r *userRepository) Create(ctx context.Context, user *User) error {
	query := `
		INSERT INTO users (email, password_hash, is_active)
		VALUES ($1, $2, $3)
		RETURNING id, created_at, updated_at
	`

	err := r.pool.QueryRow(ctx, query,
		strings.ToLower(user.Email),
		user.PasswordHash,
		true,
	).Scan(&user.ID, &user.CreatedAt, &user.UpdatedAt)

	if err != nil {
		// Check for unique constraint violation
		if strings.Contains(err.Error(), "idx_users_email") {
			return ErrEmailAlreadyExists
		}
		return err
	}

	user.IsActive = true
	return nil
}

// GetByID retrieves a user by their ID
func (r *userRepository) GetByID(ctx context.Context, id uuid.UUID) (*User, error) {
	query := `
		SELECT id, email, password_hash, created_at, updated_at, last_login_at, is_active
		FROM users
		WHERE id = $1
	`

	user := &User{}
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&user.ID,
		&user.Email,
		&user.PasswordHash,
		&user.CreatedAt,
		&user.UpdatedAt,
		&user.LastLoginAt,
		&user.IsActive,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}

	return user, nil
}

// GetByEmail retrieves a user by their email address (case-insensitive)
// Requirements: 1.2 (check email exists), 2.4 (login validation)
func (r *userRepository) GetByEmail(ctx context.Context, email string) (*User, error) {
	query := `
		SELECT id, email, password_hash, created_at, updated_at, last_login_at, is_active
		FROM users
		WHERE LOWER(email) = LOWER($1)
	`

	user := &User{}
	err := r.pool.QueryRow(ctx, query, email).Scan(
		&user.ID,
		&user.Email,
		&user.PasswordHash,
		&user.CreatedAt,
		&user.UpdatedAt,
		&user.LastLoginAt,
		&user.IsActive,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}

	return user, nil
}

// UpdateLastLogin updates the last_login_at timestamp for a user
// Requirements: 2.4 (update last_login_at on successful login)
func (r *userRepository) UpdateLastLogin(ctx context.Context, id uuid.UUID) error {
	query := `
		UPDATE users
		SET last_login_at = $1
		WHERE id = $2
	`

	now := time.Now().UTC()
	result, err := r.pool.Exec(ctx, query, now, id)
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return ErrUserNotFound
	}

	return nil
}

// EmailExists checks if an email address is already registered (case-insensitive)
// Requirements: 1.2 (check for duplicate email)
func (r *userRepository) EmailExists(ctx context.Context, email string) (bool, error) {
	query := `
		SELECT EXISTS(SELECT 1 FROM users WHERE LOWER(email) = LOWER($1))
	`

	var exists bool
	err := r.pool.QueryRow(ctx, query, email).Scan(&exists)
	if err != nil {
		return false, err
	}

	return exists, nil
}
