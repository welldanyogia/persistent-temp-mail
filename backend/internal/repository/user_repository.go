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
	Delete(ctx context.Context, id uuid.UUID) error
	GetDeleteInfo(ctx context.Context, id uuid.UUID) (domainCount, aliasCount, emailCount, attachmentCount int, totalSize int64, err error)
	GetAttachmentStorageKeys(ctx context.Context, id uuid.UUID) ([]string, error)
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


// Delete deletes a user by their ID
// Requirements: 4.3 (Delete user account)
// Note: CASCADE delete handles domains, aliases, emails, and attachments in DB
func (r *userRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM users WHERE id = $1`

	result, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return ErrUserNotFound
	}

	return nil
}

// GetDeleteInfo retrieves information needed for cascade delete
// Returns counts of domains, aliases, emails, attachments, and total size
// Requirements: 4.3 (Delete user account with cascade info)
func (r *userRepository) GetDeleteInfo(ctx context.Context, id uuid.UUID) (domainCount, aliasCount, emailCount, attachmentCount int, totalSize int64, err error) {
	// Count domains
	domainQuery := `SELECT COALESCE(COUNT(*), 0) FROM domains WHERE user_id = $1`
	err = r.pool.QueryRow(ctx, domainQuery, id).Scan(&domainCount)
	if err != nil {
		return 0, 0, 0, 0, 0, err
	}

	// Count aliases
	aliasQuery := `SELECT COALESCE(COUNT(*), 0) FROM aliases WHERE user_id = $1`
	err = r.pool.QueryRow(ctx, aliasQuery, id).Scan(&aliasCount)
	if err != nil {
		return 0, 0, 0, 0, 0, err
	}

	// Count emails and get total email size
	emailQuery := `
		SELECT COALESCE(COUNT(*), 0), COALESCE(SUM(size_bytes), 0)
		FROM emails e
		JOIN aliases a ON e.alias_id = a.id
		WHERE a.user_id = $1
	`
	var emailSize int64
	err = r.pool.QueryRow(ctx, emailQuery, id).Scan(&emailCount, &emailSize)
	if err != nil {
		return 0, 0, 0, 0, 0, err
	}

	// Count attachments and get total attachment size
	attachmentQuery := `
		SELECT COALESCE(COUNT(*), 0), COALESCE(SUM(att.size_bytes), 0)
		FROM attachments att
		JOIN emails e ON att.email_id = e.id
		JOIN aliases a ON e.alias_id = a.id
		WHERE a.user_id = $1
	`
	var attachmentSize int64
	err = r.pool.QueryRow(ctx, attachmentQuery, id).Scan(&attachmentCount, &attachmentSize)
	if err != nil {
		return 0, 0, 0, 0, 0, err
	}

	totalSize = emailSize + attachmentSize
	return domainCount, aliasCount, emailCount, attachmentCount, totalSize, nil
}

// GetAttachmentStorageKeys retrieves all storage keys for attachments owned by a user
// Requirements: 4.3 (Delete attachments from storage when user is deleted)
func (r *userRepository) GetAttachmentStorageKeys(ctx context.Context, id uuid.UUID) ([]string, error) {
	query := `
		SELECT att.storage_key
		FROM attachments att
		JOIN emails e ON att.email_id = e.id
		JOIN aliases a ON e.alias_id = a.id
		WHERE a.user_id = $1
	`

	rows, err := r.pool.Query(ctx, query, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return keys, nil
}
