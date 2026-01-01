package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/welldanyogia/persistent-temp-mail/backend/internal/domain"
)

// DomainRepository implements domain.Repository using PostgreSQL
type DomainRepository struct {
	pool *pgxpool.Pool
}

// NewDomainRepository creates a new DomainRepository instance
func NewDomainRepository(pool *pgxpool.Pool) *DomainRepository {
	return &DomainRepository{pool: pool}
}

// Create inserts a new domain into the database
// Requirements: FR-DOM-002 (Add new domain)
func (r *DomainRepository) Create(ctx context.Context, d *domain.Domain) error {
	query := `
		INSERT INTO domains (id, user_id, domain_name, verification_token, is_verified, created_at, updated_at)
		VALUES ($1, $2, LOWER($3), $4, $5, $6, $7)
		RETURNING created_at, updated_at
	`

	now := time.Now().UTC()
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}

	err := r.pool.QueryRow(ctx, query,
		d.ID,
		d.UserID,
		d.DomainName,
		d.VerificationToken,
		d.IsVerified,
		now,
		now,
	).Scan(&d.CreatedAt, &d.UpdatedAt)

	if err != nil {
		// Check for unique constraint violation (domain already exists)
		if strings.Contains(err.Error(), "idx_domains_name") {
			return domain.ErrDomainExists
		}
		return fmt.Errorf("failed to create domain: %w", err)
	}

	return nil
}


// GetByID retrieves a domain by its ID with alias count
// Requirements: FR-DOM-003 (Get domain details)
func (r *DomainRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Domain, error) {
	query := `
		SELECT 
			d.id, d.user_id, d.domain_name, d.verification_token,
			d.is_verified, d.verified_at, d.ssl_enabled, d.ssl_expires_at,
			d.created_at, d.updated_at,
			COALESCE(COUNT(a.id), 0) as alias_count
		FROM domains d
		LEFT JOIN aliases a ON a.domain_id = d.id
		WHERE d.id = $1
		GROUP BY d.id
	`

	d := &domain.Domain{}
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&d.ID,
		&d.UserID,
		&d.DomainName,
		&d.VerificationToken,
		&d.IsVerified,
		&d.VerifiedAt,
		&d.SSLEnabled,
		&d.SSLExpiresAt,
		&d.CreatedAt,
		&d.UpdatedAt,
		&d.AliasCount,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrDomainNotFound
		}
		return nil, fmt.Errorf("failed to get domain by ID: %w", err)
	}

	return d, nil
}

// GetByUserID retrieves all domains for a user with pagination and filtering
// Requirements: FR-DOM-001 (List user domains)
func (r *DomainRepository) GetByUserID(ctx context.Context, userID uuid.UUID, opts domain.ListOptions) ([]domain.Domain, int, error) {
	// Apply defaults
	if opts.Page < 1 {
		opts.Page = 1
	}
	if opts.Limit < 1 {
		opts.Limit = 20
	}
	if opts.Limit > 100 {
		opts.Limit = 100
	}

	// Build base query with alias count
	baseQuery := `
		FROM domains d
		LEFT JOIN aliases a ON a.domain_id = d.id
		WHERE d.user_id = $1
	`
	args := []interface{}{userID}
	argIdx := 2

	// Add status filter
	if opts.Status != "" {
		switch opts.Status {
		case "verified":
			baseQuery += fmt.Sprintf(" AND d.is_verified = $%d", argIdx)
			args = append(args, true)
			argIdx++
		case "pending":
			baseQuery += fmt.Sprintf(" AND d.is_verified = $%d", argIdx)
			args = append(args, false)
			argIdx++
		}
	}

	// Count total records
	countQuery := "SELECT COUNT(DISTINCT d.id) " + baseQuery
	var totalCount int
	err := r.pool.QueryRow(ctx, countQuery, args...).Scan(&totalCount)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count domains: %w", err)
	}

	// Build select query with pagination
	selectQuery := `
		SELECT 
			d.id, d.user_id, d.domain_name, d.verification_token,
			d.is_verified, d.verified_at, d.ssl_enabled, d.ssl_expires_at,
			d.created_at, d.updated_at,
			COALESCE(COUNT(a.id), 0) as alias_count
	` + baseQuery + `
		GROUP BY d.id
		ORDER BY d.created_at DESC
	`

	// Add pagination
	offset := (opts.Page - 1) * opts.Limit
	selectQuery += fmt.Sprintf(" LIMIT $%d OFFSET $%d", argIdx, argIdx+1)
	args = append(args, opts.Limit, offset)

	rows, err := r.pool.Query(ctx, selectQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query domains: %w", err)
	}
	defer rows.Close()

	var domains []domain.Domain
	for rows.Next() {
		var d domain.Domain
		err := rows.Scan(
			&d.ID,
			&d.UserID,
			&d.DomainName,
			&d.VerificationToken,
			&d.IsVerified,
			&d.VerifiedAt,
			&d.SSLEnabled,
			&d.SSLExpiresAt,
			&d.CreatedAt,
			&d.UpdatedAt,
			&d.AliasCount,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan domain: %w", err)
		}
		domains = append(domains, d)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("error iterating domains: %w", err)
	}

	return domains, totalCount, nil
}


// GetByDomainName retrieves a domain by its name (case-insensitive)
// Requirements: FR-DOM-002 (check for duplicate domain)
func (r *DomainRepository) GetByDomainName(ctx context.Context, name string) (*domain.Domain, error) {
	query := `
		SELECT 
			d.id, d.user_id, d.domain_name, d.verification_token,
			d.is_verified, d.verified_at, d.ssl_enabled, d.ssl_expires_at,
			d.created_at, d.updated_at,
			COALESCE(COUNT(a.id), 0) as alias_count
		FROM domains d
		LEFT JOIN aliases a ON a.domain_id = d.id
		WHERE LOWER(d.domain_name) = LOWER($1)
		GROUP BY d.id
	`

	d := &domain.Domain{}
	err := r.pool.QueryRow(ctx, query, name).Scan(
		&d.ID,
		&d.UserID,
		&d.DomainName,
		&d.VerificationToken,
		&d.IsVerified,
		&d.VerifiedAt,
		&d.SSLEnabled,
		&d.SSLExpiresAt,
		&d.CreatedAt,
		&d.UpdatedAt,
		&d.AliasCount,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrDomainNotFound
		}
		return nil, fmt.Errorf("failed to get domain by name: %w", err)
	}

	return d, nil
}

// Update updates an existing domain
// Requirements: FR-DOM-005 (update verification status)
func (r *DomainRepository) Update(ctx context.Context, d *domain.Domain) error {
	query := `
		UPDATE domains
		SET 
			domain_name = LOWER($1),
			verification_token = $2,
			is_verified = $3,
			verified_at = $4,
			ssl_enabled = $5,
			ssl_expires_at = $6,
			updated_at = $7
		WHERE id = $8
	`

	now := time.Now().UTC()
	result, err := r.pool.Exec(ctx, query,
		d.DomainName,
		d.VerificationToken,
		d.IsVerified,
		d.VerifiedAt,
		d.SSLEnabled,
		d.SSLExpiresAt,
		now,
		d.ID,
	)

	if err != nil {
		// Check for unique constraint violation
		if strings.Contains(err.Error(), "idx_domains_name") {
			return domain.ErrDomainExists
		}
		return fmt.Errorf("failed to update domain: %w", err)
	}

	if result.RowsAffected() == 0 {
		return domain.ErrDomainNotFound
	}

	d.UpdatedAt = now
	return nil
}

// Delete deletes a domain and returns info about deleted resources
// Requirements: FR-DOM-004 (Delete domain with cascade)
func (r *DomainRepository) Delete(ctx context.Context, id uuid.UUID) (*domain.DeleteResult, error) {
	// Start a transaction to ensure consistency
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Count resources before delete (cascade will handle actual deletion)
	// Note: aliases table may not exist yet, so we handle that gracefully
	var aliasCount, emailCount, attachmentCount int

	// Try to count aliases (table may not exist yet)
	err = tx.QueryRow(ctx, `
		SELECT COALESCE(COUNT(*), 0) FROM aliases WHERE domain_id = $1
	`, id).Scan(&aliasCount)
	if err != nil {
		// If aliases table doesn't exist, set count to 0
		if strings.Contains(err.Error(), "does not exist") {
			aliasCount = 0
		} else {
			return nil, fmt.Errorf("failed to count aliases: %w", err)
		}
	}

	// Try to count emails (table may not exist yet)
	if aliasCount > 0 {
		err = tx.QueryRow(ctx, `
			SELECT COALESCE(COUNT(*), 0) 
			FROM emails e 
			JOIN aliases a ON e.alias_id = a.id 
			WHERE a.domain_id = $1
		`, id).Scan(&emailCount)
		if err != nil {
			if strings.Contains(err.Error(), "does not exist") {
				emailCount = 0
			} else {
				return nil, fmt.Errorf("failed to count emails: %w", err)
			}
		}

		// Try to count attachments (table may not exist yet)
		err = tx.QueryRow(ctx, `
			SELECT COALESCE(COUNT(*), 0) 
			FROM attachments att 
			JOIN emails e ON att.email_id = e.id 
			JOIN aliases a ON e.alias_id = a.id 
			WHERE a.domain_id = $1
		`, id).Scan(&attachmentCount)
		if err != nil {
			if strings.Contains(err.Error(), "does not exist") {
				attachmentCount = 0
			} else {
				return nil, fmt.Errorf("failed to count attachments: %w", err)
			}
		}
	}

	// Delete domain (cascade handles aliases, emails, attachments)
	result, err := tx.Exec(ctx, "DELETE FROM domains WHERE id = $1", id)
	if err != nil {
		return nil, fmt.Errorf("failed to delete domain: %w", err)
	}

	if result.RowsAffected() == 0 {
		return nil, domain.ErrDomainNotFound
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return &domain.DeleteResult{
		DomainID:           id,
		AliasesDeleted:     aliasCount,
		EmailsDeleted:      emailCount,
		AttachmentsDeleted: attachmentCount,
	}, nil
}

// CountByUserID counts the number of domains owned by a user
// Requirements: FR-DOM-002 (enforce domain limit per user)
func (r *DomainRepository) CountByUserID(ctx context.Context, userID uuid.UUID) (int, error) {
	query := `SELECT COUNT(*) FROM domains WHERE user_id = $1`

	var count int
	err := r.pool.QueryRow(ctx, query, userID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count user domains: %w", err)
	}

	return count, nil
}

// Ensure DomainRepository implements domain.Repository interface
var _ domain.Repository = (*DomainRepository)(nil)
