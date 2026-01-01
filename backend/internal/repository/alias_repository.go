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
)

// AliasRepository errors
var (
	ErrAliasNotFound = errors.New("alias not found")
	ErrAliasExists   = errors.New("alias already exists")
)

// AliasRepository implements alias data access using PostgreSQL
type AliasRepository struct {
	pool *pgxpool.Pool
}

// NewAliasRepository creates a new AliasRepository instance
func NewAliasRepository(pool *pgxpool.Pool) *AliasRepository {
	return &AliasRepository{pool: pool}
}

// Create inserts a new alias into the database
// Requirements: 1.1 (Create alias)
func (r *AliasRepository) Create(ctx context.Context, alias *Alias) error {
	query := `
		INSERT INTO aliases (id, user_id, domain_id, local_part, full_address, description, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, LOWER($4), LOWER($5), $6, $7, $8, $9)
		RETURNING created_at, updated_at
	`

	now := time.Now().UTC()
	if alias.ID == uuid.Nil {
		alias.ID = uuid.New()
	}

	err := r.pool.QueryRow(ctx, query,
		alias.ID,
		alias.UserID,
		alias.DomainID,
		alias.LocalPart,
		alias.FullAddress,
		alias.Description,
		alias.IsActive,
		now,
		now,
	).Scan(&alias.CreatedAt, &alias.UpdatedAt)

	if err != nil {
		// Check for unique constraint violation (alias already exists)
		if strings.Contains(err.Error(), "idx_aliases_full_address") {
			return ErrAliasExists
		}
		return fmt.Errorf("failed to create alias: %w", err)
	}

	return nil
}

// GetByID retrieves an alias by its ID with stats
// Requirements: 3.1 (Get alias details)
func (r *AliasRepository) GetByID(ctx context.Context, id uuid.UUID) (*AliasWithStats, error) {
	query := `
		SELECT 
			a.id, a.user_id, a.domain_id, a.local_part, a.full_address,
			a.description, a.is_active, a.created_at, a.updated_at,
			d.domain_name,
			COALESCE(COUNT(e.id), 0) as email_count,
			MAX(e.received_at) as last_email_received_at,
			COALESCE(SUM(e.size_bytes), 0) as total_size_bytes
		FROM aliases a
		JOIN domains d ON d.id = a.domain_id
		LEFT JOIN emails e ON e.alias_id = a.id
		WHERE a.id = $1
		GROUP BY a.id, d.domain_name
	`

	alias := &AliasWithStats{}
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&alias.ID,
		&alias.UserID,
		&alias.DomainID,
		&alias.LocalPart,
		&alias.FullAddress,
		&alias.Description,
		&alias.IsActive,
		&alias.CreatedAt,
		&alias.UpdatedAt,
		&alias.DomainName,
		&alias.EmailCount,
		&alias.LastEmailReceivedAt,
		&alias.TotalSizeBytes,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrAliasNotFound
		}
		return nil, fmt.Errorf("failed to get alias by ID: %w", err)
	}

	return alias, nil
}


// GetByFullAddress retrieves an alias by its full email address (case-insensitive)
// Requirements: 1.7 (Check alias exists)
func (r *AliasRepository) GetByFullAddress(ctx context.Context, fullAddress string) (*AliasWithStats, error) {
	query := `
		SELECT 
			a.id, a.user_id, a.domain_id, a.local_part, a.full_address,
			a.description, a.is_active, a.created_at, a.updated_at,
			d.domain_name,
			COALESCE(COUNT(e.id), 0) as email_count,
			MAX(e.received_at) as last_email_received_at,
			COALESCE(SUM(e.size_bytes), 0) as total_size_bytes
		FROM aliases a
		JOIN domains d ON d.id = a.domain_id
		LEFT JOIN emails e ON e.alias_id = a.id
		WHERE LOWER(a.full_address) = LOWER($1)
		GROUP BY a.id, d.domain_name
	`

	alias := &AliasWithStats{}
	err := r.pool.QueryRow(ctx, query, fullAddress).Scan(
		&alias.ID,
		&alias.UserID,
		&alias.DomainID,
		&alias.LocalPart,
		&alias.FullAddress,
		&alias.Description,
		&alias.IsActive,
		&alias.CreatedAt,
		&alias.UpdatedAt,
		&alias.DomainName,
		&alias.EmailCount,
		&alias.LastEmailReceivedAt,
		&alias.TotalSizeBytes,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrAliasNotFound
		}
		return nil, fmt.Errorf("failed to get alias by full address: %w", err)
	}

	return alias, nil
}

// List retrieves aliases for a user with pagination, filtering, search, and sorting
// Requirements: 2.1-2.6 (List aliases)
func (r *AliasRepository) List(ctx context.Context, userID uuid.UUID, params ListAliasParams) ([]AliasWithStats, int, error) {
	// Apply defaults
	if params.Page < 1 {
		params.Page = 1
	}
	if params.Limit < 1 {
		params.Limit = 20
	}
	if params.Limit > 100 {
		params.Limit = 100
	}

	// Build base query
	baseQuery := `
		FROM aliases a
		JOIN domains d ON d.id = a.domain_id
		LEFT JOIN emails e ON e.alias_id = a.id
		WHERE a.user_id = $1
	`
	args := []interface{}{userID}
	argIdx := 2

	// Add domain filter
	if params.DomainID != nil {
		baseQuery += fmt.Sprintf(" AND a.domain_id = $%d", argIdx)
		args = append(args, *params.DomainID)
		argIdx++
	}

	// Add search filter (case-insensitive search on local_part)
	if params.Search != "" {
		baseQuery += fmt.Sprintf(" AND LOWER(a.local_part) LIKE LOWER($%d)", argIdx)
		args = append(args, "%"+params.Search+"%")
		argIdx++
	}

	// Count total records
	countQuery := "SELECT COUNT(DISTINCT a.id) " + baseQuery
	var totalCount int
	err := r.pool.QueryRow(ctx, countQuery, args...).Scan(&totalCount)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count aliases: %w", err)
	}

	// Build select query with grouping
	selectQuery := `
		SELECT 
			a.id, a.user_id, a.domain_id, a.local_part, a.full_address,
			a.description, a.is_active, a.created_at, a.updated_at,
			d.domain_name,
			COALESCE(COUNT(e.id), 0) as email_count,
			MAX(e.received_at) as last_email_received_at,
			COALESCE(SUM(e.size_bytes), 0) as total_size_bytes
	` + baseQuery + `
		GROUP BY a.id, d.domain_name
	`

	// Add sorting
	sortField := "a.created_at"
	if params.Sort == "email_count" {
		sortField = "email_count"
	}
	sortOrder := "DESC"
	if params.Order == "asc" {
		sortOrder = "ASC"
	}
	selectQuery += fmt.Sprintf(" ORDER BY %s %s", sortField, sortOrder)

	// Add pagination
	offset := (params.Page - 1) * params.Limit
	selectQuery += fmt.Sprintf(" LIMIT $%d OFFSET $%d", argIdx, argIdx+1)
	args = append(args, params.Limit, offset)

	rows, err := r.pool.Query(ctx, selectQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query aliases: %w", err)
	}
	defer rows.Close()

	var aliases []AliasWithStats
	for rows.Next() {
		var alias AliasWithStats
		err := rows.Scan(
			&alias.ID,
			&alias.UserID,
			&alias.DomainID,
			&alias.LocalPart,
			&alias.FullAddress,
			&alias.Description,
			&alias.IsActive,
			&alias.CreatedAt,
			&alias.UpdatedAt,
			&alias.DomainName,
			&alias.EmailCount,
			&alias.LastEmailReceivedAt,
			&alias.TotalSizeBytes,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan alias: %w", err)
		}
		aliases = append(aliases, alias)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("error iterating aliases: %w", err)
	}

	return aliases, totalCount, nil
}


// Update updates an existing alias
// Requirements: 4.1-4.5 (Update alias)
func (r *AliasRepository) Update(ctx context.Context, alias *Alias) error {
	query := `
		UPDATE aliases
		SET 
			is_active = $1,
			description = $2,
			updated_at = $3
		WHERE id = $4
	`

	now := time.Now().UTC()
	result, err := r.pool.Exec(ctx, query,
		alias.IsActive,
		alias.Description,
		now,
		alias.ID,
	)

	if err != nil {
		return fmt.Errorf("failed to update alias: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrAliasNotFound
	}

	alias.UpdatedAt = now
	return nil
}

// Delete deletes an alias by ID
// Requirements: 5.1 (Delete alias)
func (r *AliasRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM aliases WHERE id = $1`

	result, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete alias: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrAliasNotFound
	}

	return nil
}

// CountByUserID counts the number of aliases owned by a user
// Requirements: 7.1, 7.2 (Alias limit enforcement)
func (r *AliasRepository) CountByUserID(ctx context.Context, userID uuid.UUID) (int, error) {
	query := `SELECT COUNT(*) FROM aliases WHERE user_id = $1`

	var count int
	err := r.pool.QueryRow(ctx, query, userID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count user aliases: %w", err)
	}

	return count, nil
}

// ExistsByFullAddress checks if an alias with the given full address exists
// Requirements: 1.7, 7.3 (Global uniqueness)
func (r *AliasRepository) ExistsByFullAddress(ctx context.Context, fullAddress string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM aliases WHERE LOWER(full_address) = LOWER($1))`

	var exists bool
	err := r.pool.QueryRow(ctx, query, fullAddress).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check alias existence: %w", err)
	}

	return exists, nil
}

// GetDetailStats retrieves detailed statistics for an alias
// Requirements: 3.4 (Alias stats)
func (r *AliasRepository) GetDetailStats(ctx context.Context, aliasID uuid.UUID) (*AliasStats, error) {
	// Get email counts for different time periods
	countQuery := `
		SELECT 
			COALESCE(SUM(CASE WHEN received_at >= (NOW() AT TIME ZONE 'utc')::date THEN 1 ELSE 0 END), 0) as emails_today,
			COALESCE(SUM(CASE WHEN received_at >= (NOW() AT TIME ZONE 'utc') - INTERVAL '7 days' THEN 1 ELSE 0 END), 0) as emails_this_week,
			COALESCE(SUM(CASE WHEN received_at >= (NOW() AT TIME ZONE 'utc') - INTERVAL '30 days' THEN 1 ELSE 0 END), 0) as emails_this_month
		FROM emails
		WHERE alias_id = $1
	`

	stats := &AliasStats{}
	err := r.pool.QueryRow(ctx, countQuery, aliasID).Scan(
		&stats.EmailsToday,
		&stats.EmailsThisWeek,
		&stats.EmailsThisMonth,
	)
	if err != nil {
		// If emails table doesn't exist yet, return zero stats
		if strings.Contains(err.Error(), "does not exist") {
			return &AliasStats{
				EmailsToday:     0,
				EmailsThisWeek:  0,
				EmailsThisMonth: 0,
				TopSenders:      []TopSender{},
			}, nil
		}
		return nil, fmt.Errorf("failed to get alias stats: %w", err)
	}

	// Get top senders (max 5)
	topSendersQuery := `
		SELECT sender_address, COUNT(*) as count
		FROM emails
		WHERE alias_id = $1
		GROUP BY sender_address
		ORDER BY count DESC
		LIMIT 5
	`

	rows, err := r.pool.Query(ctx, topSendersQuery, aliasID)
	if err != nil {
		// If emails table doesn't exist yet, return empty top senders
		if strings.Contains(err.Error(), "does not exist") {
			stats.TopSenders = []TopSender{}
			return stats, nil
		}
		return nil, fmt.Errorf("failed to get top senders: %w", err)
	}
	defer rows.Close()

	stats.TopSenders = []TopSender{}
	for rows.Next() {
		var sender TopSender
		if err := rows.Scan(&sender.Email, &sender.Count); err != nil {
			return nil, fmt.Errorf("failed to scan top sender: %w", err)
		}
		stats.TopSenders = append(stats.TopSenders, sender)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating top senders: %w", err)
	}

	return stats, nil
}

// GetDeleteInfo retrieves information needed for cascade delete
// Requirements: 5.2, 5.3 (Delete cascade info)
func (r *AliasRepository) GetDeleteInfo(ctx context.Context, aliasID uuid.UUID) (emailCount int, attachmentCount int, totalSize int64, err error) {
	// Count emails
	emailQuery := `SELECT COALESCE(COUNT(*), 0) FROM emails WHERE alias_id = $1`
	err = r.pool.QueryRow(ctx, emailQuery, aliasID).Scan(&emailCount)
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") {
			emailCount = 0
		} else {
			return 0, 0, 0, fmt.Errorf("failed to count emails: %w", err)
		}
	}

	// Count attachments and total size
	attachmentQuery := `
		SELECT 
			COALESCE(COUNT(att.id), 0),
			COALESCE(SUM(att.size_bytes), 0)
		FROM attachments att
		JOIN emails e ON att.email_id = e.id
		WHERE e.alias_id = $1
	`
	err = r.pool.QueryRow(ctx, attachmentQuery, aliasID).Scan(&attachmentCount, &totalSize)
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") {
			attachmentCount = 0
			totalSize = 0
		} else {
			return 0, 0, 0, fmt.Errorf("failed to count attachments: %w", err)
		}
	}

	return emailCount, attachmentCount, totalSize, nil
}

// GetAttachmentStorageKeys retrieves all storage keys for attachments of an alias
// Requirements: 5.2 (Delete attachments from storage)
func (r *AliasRepository) GetAttachmentStorageKeys(ctx context.Context, aliasID uuid.UUID) ([]string, error) {
	query := `
		SELECT att.storage_key
		FROM attachments att
		JOIN emails e ON att.email_id = e.id
		WHERE e.alias_id = $1
	`

	rows, err := r.pool.Query(ctx, query, aliasID)
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") {
			return []string{}, nil
		}
		return nil, fmt.Errorf("failed to get attachment storage keys: %w", err)
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("failed to scan storage key: %w", err)
		}
		keys = append(keys, key)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating storage keys: %w", err)
	}

	return keys, nil
}
