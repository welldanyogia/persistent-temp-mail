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

// Session repository errors
var (
	ErrSessionNotFound = errors.New("session not found")
)

// SessionRepository defines the interface for session data access
type SessionRepository interface {
	Create(ctx context.Context, session *Session) error
	GetByTokenHash(ctx context.Context, tokenHash string) (*Session, error)
	Delete(ctx context.Context, id uuid.UUID) error
	DeleteByTokenHash(ctx context.Context, tokenHash string) error
	CountFailedAttempts(ctx context.Context, email string, since time.Time) (int, error)
	RecordFailedAttempt(ctx context.Context, email string, ip string) error
	CleanupExpiredSessions(ctx context.Context) (int64, error)
	CleanupOldFailedAttempts(ctx context.Context, before time.Time) (int64, error)
}

// sessionRepository implements SessionRepository using PostgreSQL
type sessionRepository struct {
	pool *pgxpool.Pool
}

// NewSessionRepository creates a new SessionRepository instance
func NewSessionRepository(pool *pgxpool.Pool) SessionRepository {
	return &sessionRepository{pool: pool}
}

// Create inserts a new session into the database
// Requirements: 2.5 (create session with IP and user agent), 3.6 (store token hash)
func (r *sessionRepository) Create(ctx context.Context, session *Session) error {
	query := `
		INSERT INTO sessions (user_id, token_hash, expires_at, ip_address, user_agent)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at
	`

	err := r.pool.QueryRow(ctx, query,
		session.UserID,
		session.TokenHash,
		session.ExpiresAt,
		session.IPAddress,
		session.UserAgent,
	).Scan(&session.ID, &session.CreatedAt)

	if err != nil {
		return err
	}

	return nil
}

// GetByTokenHash retrieves a session by its token hash
// Requirements: 3.6 (lookup by token hash for validation)
func (r *sessionRepository) GetByTokenHash(ctx context.Context, tokenHash string) (*Session, error) {
	query := `
		SELECT id, user_id, token_hash, expires_at, created_at, ip_address, user_agent
		FROM sessions
		WHERE token_hash = $1
	`

	session := &Session{}
	err := r.pool.QueryRow(ctx, query, tokenHash).Scan(
		&session.ID,
		&session.UserID,
		&session.TokenHash,
		&session.ExpiresAt,
		&session.CreatedAt,
		&session.IPAddress,
		&session.UserAgent,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}

	return session, nil
}

// Delete removes a session by its ID
// Requirements: 4.2 (delete session on logout)
func (r *sessionRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM sessions WHERE id = $1`

	result, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return ErrSessionNotFound
	}

	return nil
}

// DeleteByTokenHash removes a session by its token hash
// Requirements: 4.2 (delete session on logout)
func (r *sessionRepository) DeleteByTokenHash(ctx context.Context, tokenHash string) error {
	query := `DELETE FROM sessions WHERE token_hash = $1`

	result, err := r.pool.Exec(ctx, query, tokenHash)
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return ErrSessionNotFound
	}

	return nil
}

// CountFailedAttempts counts failed login attempts for an email since a given time
// Requirements: 2.3 (brute force protection - 5 attempts in 15 minutes)
func (r *sessionRepository) CountFailedAttempts(ctx context.Context, email string, since time.Time) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM failed_login_attempts
		WHERE LOWER(email) = LOWER($1) AND attempted_at >= $2
	`

	var count int
	err := r.pool.QueryRow(ctx, query, strings.ToLower(email), since).Scan(&count)
	if err != nil {
		return 0, err
	}

	return count, nil
}

// RecordFailedAttempt records a failed login attempt
// Requirements: 2.3 (brute force protection tracking)
func (r *sessionRepository) RecordFailedAttempt(ctx context.Context, email string, ip string) error {
	query := `
		INSERT INTO failed_login_attempts (email, ip_address)
		VALUES ($1, $2)
	`

	_, err := r.pool.Exec(ctx, query, strings.ToLower(email), ip)
	return err
}

// CleanupExpiredSessions removes all expired sessions from the database
func (r *sessionRepository) CleanupExpiredSessions(ctx context.Context) (int64, error) {
	query := `DELETE FROM sessions WHERE expires_at < $1`

	result, err := r.pool.Exec(ctx, query, time.Now().UTC())
	if err != nil {
		return 0, err
	}

	return result.RowsAffected(), nil
}

// CleanupOldFailedAttempts removes failed login attempts older than the specified time
func (r *sessionRepository) CleanupOldFailedAttempts(ctx context.Context, before time.Time) (int64, error) {
	query := `DELETE FROM failed_login_attempts WHERE attempted_at < $1`

	result, err := r.pool.Exec(ctx, query, before)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected(), nil
}
