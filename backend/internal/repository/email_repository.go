package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

// Email repository errors
var (
	ErrEmailNotFound = errors.New("email not found")
)

// EmailRepositoryInterface defines the interface for email repository operations
// Requirements: 1.1-1.9, 2.1, 4.1, 5.1-5.2, 6.1-6.5
type EmailRepositoryInterface interface {
	List(ctx context.Context, userID uuid.UUID, params ListEmailParams) ([]EmailWithPreview, int, error)
	GetByID(ctx context.Context, id uuid.UUID) (*Email, error)
	Delete(ctx context.Context, id uuid.UUID) error
	DeleteBatch(ctx context.Context, ids []uuid.UUID) (int, error)
	MarkAsRead(ctx context.Context, id uuid.UUID) error
	MarkAsReadBatch(ctx context.Context, ids []uuid.UUID) (int, error)
	GetStats(ctx context.Context, userID uuid.UUID) (*InboxStats, error)
	IsOwnedByUser(ctx context.Context, emailID, userID uuid.UUID) (bool, error)
	Create(ctx context.Context, email *Email) error
	GetSizeByID(ctx context.Context, id uuid.UUID) (int64, error)
}

// EmailRepo implements EmailRepositoryInterface using PostgreSQL
type EmailRepo struct {
	db *sqlx.DB
}

// NewEmailRepo creates a new EmailRepo instance
func NewEmailRepo(db *sqlx.DB) *EmailRepo {
	return &EmailRepo{db: db}
}

// GeneratePreviewText generates a preview from body text (max 200 chars, truncated at word boundary)
// Requirements: 1.8
func GeneratePreviewText(bodyText string, maxLength int) string {
	if maxLength <= 0 {
		maxLength = 200
	}

	// Trim whitespace
	text := strings.TrimSpace(bodyText)
	if text == "" {
		return ""
	}

	// If text is shorter than max length, return as is
	if len(text) <= maxLength {
		return text
	}

	// Find the last space before maxLength to truncate at word boundary
	truncated := text[:maxLength]
	lastSpace := strings.LastIndexFunc(truncated, unicode.IsSpace)

	// If we found a space and it's not too close to the beginning, truncate there
	if lastSpace > maxLength/2 {
		truncated = truncated[:lastSpace]
	}

	// Add ellipsis
	return strings.TrimSpace(truncated) + "..."
}


// List retrieves emails for a user with pagination, filtering, search, and sorting
// Requirements: 1.1-1.9 (List emails with filters)
func (r *EmailRepo) List(ctx context.Context, userID uuid.UUID, params ListEmailParams) ([]EmailWithPreview, int, error) {
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

	// Build base query - join with aliases to filter by user ownership
	baseQuery := `
		FROM emails e
		JOIN aliases a ON e.alias_id = a.id
		WHERE a.user_id = $1
	`
	args := []interface{}{userID}
	argIdx := 2

	// Add alias filter
	if params.AliasID != nil {
		baseQuery += fmt.Sprintf(" AND e.alias_id = $%d", argIdx)
		args = append(args, *params.AliasID)
		argIdx++
	}

	// Add search filter (search in subject, sender_address, body_text)
	if params.Search != "" {
		baseQuery += fmt.Sprintf(` AND (
			LOWER(e.subject) LIKE LOWER($%d) OR
			LOWER(e.sender_address) LIKE LOWER($%d) OR
			LOWER(e.body_text) LIKE LOWER($%d)
		)`, argIdx, argIdx, argIdx)
		args = append(args, "%"+params.Search+"%")
		argIdx++
	}

	// Add date range filter
	if params.FromDate != nil {
		baseQuery += fmt.Sprintf(" AND e.received_at >= $%d", argIdx)
		args = append(args, *params.FromDate)
		argIdx++
	}
	if params.ToDate != nil {
		baseQuery += fmt.Sprintf(" AND e.received_at <= $%d", argIdx)
		args = append(args, *params.ToDate)
		argIdx++
	}

	// Add has_attachments filter
	if params.HasAttachments != nil {
		if *params.HasAttachments {
			baseQuery += " AND EXISTS (SELECT 1 FROM attachments att WHERE att.email_id = e.id)"
		} else {
			baseQuery += " AND NOT EXISTS (SELECT 1 FROM attachments att WHERE att.email_id = e.id)"
		}
	}

	// Add is_read filter
	if params.IsRead != nil {
		baseQuery += fmt.Sprintf(" AND e.is_read = $%d", argIdx)
		args = append(args, *params.IsRead)
		argIdx++
	}

	// Count total records
	countQuery := "SELECT COUNT(*) " + baseQuery
	var totalCount int
	err := r.db.GetContext(ctx, &totalCount, countQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count emails: %w", err)
	}

	// Build select query
	selectQuery := `
		SELECT 
			e.id,
			e.alias_id,
			a.full_address as alias_email,
			e.sender_address as from_address,
			e.sender_name as from_name,
			e.subject,
			COALESCE(e.body_text, '') as body_text,
			e.received_at,
			e.size_bytes,
			e.is_read,
			(SELECT COUNT(*) FROM attachments att WHERE att.email_id = e.id) as attachment_count
	` + baseQuery

	// Add sorting
	sortField := "e.received_at"
	if params.Sort == "size" {
		sortField = "e.size_bytes"
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

	rows, err := r.db.QueryContext(ctx, selectQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query emails: %w", err)
	}
	defer rows.Close()

	var emails []EmailWithPreview
	for rows.Next() {
		var email EmailWithPreview
		var bodyText string
		err := rows.Scan(
			&email.ID,
			&email.AliasID,
			&email.AliasEmail,
			&email.FromAddress,
			&email.FromName,
			&email.Subject,
			&bodyText,
			&email.ReceivedAt,
			&email.SizeBytes,
			&email.IsRead,
			&email.AttachmentCount,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan email: %w", err)
		}

		// Generate preview text
		email.PreviewText = GeneratePreviewText(bodyText, 200)
		email.HasAttachments = email.AttachmentCount > 0

		emails = append(emails, email)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("error iterating emails: %w", err)
	}

	return emails, totalCount, nil
}


// GetByID retrieves an email by its ID
// Requirements: 2.1 (Get email details)
func (r *EmailRepo) GetByID(ctx context.Context, id uuid.UUID) (*Email, error) {
	query := `
		SELECT id, alias_id, sender_address, sender_name, subject, body_html, body_text, 
		       headers, size_bytes, is_read, raw_email, received_at, created_at
		FROM emails
		WHERE id = $1
	`

	var email Email
	var headersJSON []byte

	row := r.db.QueryRowContext(ctx, query, id)
	err := row.Scan(
		&email.ID,
		&email.AliasID,
		&email.SenderAddress,
		&email.SenderName,
		&email.Subject,
		&email.BodyHTML,
		&email.BodyText,
		&headersJSON,
		&email.SizeBytes,
		&email.IsRead,
		&email.RawEmail,
		&email.ReceivedAt,
		&email.CreatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrEmailNotFound
		}
		return nil, fmt.Errorf("failed to get email: %w", err)
	}

	// Parse headers JSON
	if len(headersJSON) > 0 {
		if err := json.Unmarshal(headersJSON, &email.Headers); err != nil {
			email.Headers = make(map[string]string)
		}
	} else {
		email.Headers = make(map[string]string)
	}

	return &email, nil
}

// Delete deletes an email by its ID
// Requirements: 4.1 (Delete email)
func (r *EmailRepo) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM emails WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete email: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return ErrEmailNotFound
	}

	return nil
}

// DeleteBatch deletes multiple emails by their IDs
// Requirements: 5.1 (Bulk delete)
func (r *EmailRepo) DeleteBatch(ctx context.Context, ids []uuid.UUID) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}

	// Build query with placeholders
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	query := fmt.Sprintf("DELETE FROM emails WHERE id IN (%s)", strings.Join(placeholders, ", "))

	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("failed to delete emails: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return int(rowsAffected), nil
}

// MarkAsRead marks an email as read
// Requirements: 2.7 (Mark as read)
func (r *EmailRepo) MarkAsRead(ctx context.Context, id uuid.UUID) error {
	query := `UPDATE emails SET is_read = true WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to mark email as read: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return ErrEmailNotFound
	}

	return nil
}

// MarkAsReadBatch marks multiple emails as read
// Requirements: 5.2 (Bulk mark as read)
func (r *EmailRepo) MarkAsReadBatch(ctx context.Context, ids []uuid.UUID) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}

	// Build query with placeholders
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	query := fmt.Sprintf("UPDATE emails SET is_read = true WHERE id IN (%s)", strings.Join(placeholders, ", "))

	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("failed to mark emails as read: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return int(rowsAffected), nil
}


// GetStats retrieves inbox statistics for a user
// Requirements: 6.1-6.5 (Email statistics)
func (r *EmailRepo) GetStats(ctx context.Context, userID uuid.UUID) (*InboxStats, error) {
	stats := &InboxStats{}

	// Get total emails, unread count, and total size
	summaryQuery := `
		SELECT 
			COUNT(*) as total_emails,
			COALESCE(SUM(CASE WHEN e.is_read = false THEN 1 ELSE 0 END), 0) as unread_emails,
			COALESCE(SUM(e.size_bytes), 0) as total_size_bytes
		FROM emails e
		JOIN aliases a ON e.alias_id = a.id
		WHERE a.user_id = $1
	`
	err := r.db.QueryRowContext(ctx, summaryQuery, userID).Scan(
		&stats.TotalEmails,
		&stats.UnreadEmails,
		&stats.TotalSizeBytes,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get email summary stats: %w", err)
	}

	// Get time-based counts
	timeQuery := `
		SELECT 
			COALESCE(SUM(CASE WHEN e.received_at >= (NOW() AT TIME ZONE 'utc')::date THEN 1 ELSE 0 END), 0) as emails_today,
			COALESCE(SUM(CASE WHEN e.received_at >= (NOW() AT TIME ZONE 'utc') - INTERVAL '7 days' THEN 1 ELSE 0 END), 0) as emails_this_week,
			COALESCE(SUM(CASE WHEN e.received_at >= (NOW() AT TIME ZONE 'utc') - INTERVAL '30 days' THEN 1 ELSE 0 END), 0) as emails_this_month
		FROM emails e
		JOIN aliases a ON e.alias_id = a.id
		WHERE a.user_id = $1
	`
	err = r.db.QueryRowContext(ctx, timeQuery, userID).Scan(
		&stats.EmailsToday,
		&stats.EmailsThisWeek,
		&stats.EmailsThisMonth,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get time-based stats: %w", err)
	}

	// Get emails per alias
	aliasQuery := `
		SELECT 
			a.id as alias_id,
			a.full_address as alias_email,
			COUNT(e.id) as count
		FROM aliases a
		LEFT JOIN emails e ON e.alias_id = a.id
		WHERE a.user_id = $1
		GROUP BY a.id, a.full_address
		ORDER BY count DESC
	`
	rows, err := r.db.QueryContext(ctx, aliasQuery, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get emails per alias: %w", err)
	}
	defer rows.Close()

	stats.EmailsPerAlias = []AliasEmailCount{}
	for rows.Next() {
		var aliasCount AliasEmailCount
		if err := rows.Scan(&aliasCount.AliasID, &aliasCount.AliasEmail, &aliasCount.Count); err != nil {
			return nil, fmt.Errorf("failed to scan alias count: %w", err)
		}
		stats.EmailsPerAlias = append(stats.EmailsPerAlias, aliasCount)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating alias counts: %w", err)
	}

	return stats, nil
}

// IsOwnedByUser checks if an email belongs to a user (via alias ownership)
// Requirements: 1.9, 2.3, 3.3, 4.3 (Authorization check)
func (r *EmailRepo) IsOwnedByUser(ctx context.Context, emailID, userID uuid.UUID) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1 FROM emails e
			JOIN aliases a ON e.alias_id = a.id
			WHERE e.id = $1 AND a.user_id = $2
		)
	`

	var exists bool
	err := r.db.GetContext(ctx, &exists, query, emailID, userID)
	if err != nil {
		return false, fmt.Errorf("failed to check email ownership: %w", err)
	}

	return exists, nil
}

// Create creates a new email record in the database
func (r *EmailRepo) Create(ctx context.Context, email *Email) error {
	headersJSON, err := json.Marshal(email.Headers)
	if err != nil {
		headersJSON = []byte("{}")
	}

	query := `
		INSERT INTO emails (id, alias_id, sender_address, sender_name, subject, body_html, body_text, 
		                    headers, size_bytes, is_read, raw_email, received_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`

	_, err = r.db.ExecContext(ctx, query,
		email.ID,
		email.AliasID,
		email.SenderAddress,
		email.SenderName,
		email.Subject,
		email.BodyHTML,
		email.BodyText,
		headersJSON,
		email.SizeBytes,
		email.IsRead,
		email.RawEmail,
		email.ReceivedAt,
		email.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to create email: %w", err)
	}

	return nil
}

// GetSizeByID retrieves the size of an email by its ID
// Requirements: 4.5 (Return size freed)
func (r *EmailRepo) GetSizeByID(ctx context.Context, id uuid.UUID) (int64, error) {
	query := `SELECT size_bytes FROM emails WHERE id = $1`

	var size int64
	err := r.db.GetContext(ctx, &size, query, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, ErrEmailNotFound
		}
		return 0, fmt.Errorf("failed to get email size: %w", err)
	}

	return size, nil
}

// GetEmailIDsOwnedByUser filters email IDs to only those owned by the user
// Requirements: 5.3 (Skip unauthorized items in bulk operations)
func (r *EmailRepo) GetEmailIDsOwnedByUser(ctx context.Context, emailIDs []uuid.UUID, userID uuid.UUID) ([]uuid.UUID, error) {
	if len(emailIDs) == 0 {
		return []uuid.UUID{}, nil
	}

	// Build query with placeholders
	placeholders := make([]string, len(emailIDs))
	args := make([]interface{}, len(emailIDs)+1)
	args[0] = userID
	for i, id := range emailIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+2)
		args[i+1] = id
	}

	query := fmt.Sprintf(`
		SELECT e.id FROM emails e
		JOIN aliases a ON e.alias_id = a.id
		WHERE a.user_id = $1 AND e.id IN (%s)
	`, strings.Join(placeholders, ", "))

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to filter owned emails: %w", err)
	}
	defer rows.Close()

	var ownedIDs []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan email ID: %w", err)
		}
		ownedIDs = append(ownedIDs, id)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating email IDs: %w", err)
	}

	return ownedIDs, nil
}

// GetTotalSizeByIDs returns the total size of emails by their IDs
// Requirements: 4.5 (Return size freed in bulk delete)
func (r *EmailRepo) GetTotalSizeByIDs(ctx context.Context, ids []uuid.UUID) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}

	// Build query with placeholders
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	query := fmt.Sprintf("SELECT COALESCE(SUM(size_bytes), 0) FROM emails WHERE id IN (%s)", strings.Join(placeholders, ", "))

	var totalSize int64
	err := r.db.GetContext(ctx, &totalSize, query, args...)
	if err != nil {
		return 0, fmt.Errorf("failed to get total size: %w", err)
	}

	return totalSize, nil
}
