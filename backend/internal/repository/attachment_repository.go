package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

// AttachmentRepository handles attachment database operations
// Requirements: 5.2, 5.5 - Store attachments and record metadata in database
type AttachmentRepository struct {
	db *sqlx.DB
}

// NewAttachmentRepository creates a new attachment repository
func NewAttachmentRepository(db *sqlx.DB) *AttachmentRepository {
	return &AttachmentRepository{db: db}
}

// Create creates a new attachment record in the database
// Requirements: 5.5 - Record filename, content_type, size_bytes in database
// Requirements: 1.10 - Track attachment status
func (r *AttachmentRepository) Create(ctx context.Context, attachment *Attachment) error {
	query := `
		INSERT INTO attachments (id, email_id, filename, content_type, size_bytes, storage_key, storage_url, checksum, status, error_details, retry_count, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`

	// Default status to 'active' if not set
	status := attachment.Status
	if status == "" {
		status = "active"
	}

	_, err := r.db.ExecContext(ctx, query,
		attachment.ID,
		attachment.EmailID,
		attachment.Filename,
		attachment.ContentType,
		attachment.SizeBytes,
		attachment.StorageKey,
		attachment.StorageURL,
		attachment.Checksum,
		status,
		attachment.ErrorDetails,
		attachment.RetryCount,
		attachment.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to create attachment: %w", err)
	}

	return nil
}

// CreateBatch creates multiple attachment records in a single transaction
// Requirements: 5.5 - Record metadata in database
// Requirements: 1.10 - Track attachment status
func (r *AttachmentRepository) CreateBatch(ctx context.Context, attachments []*Attachment) error {
	if len(attachments) == 0 {
		return nil
	}

	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	query := `
		INSERT INTO attachments (id, email_id, filename, content_type, size_bytes, storage_key, storage_url, checksum, status, error_details, retry_count, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`

	for _, attachment := range attachments {
		// Default status to 'active' if not set
		status := attachment.Status
		if status == "" {
			status = "active"
		}

		_, err := tx.ExecContext(ctx, query,
			attachment.ID,
			attachment.EmailID,
			attachment.Filename,
			attachment.ContentType,
			attachment.SizeBytes,
			attachment.StorageKey,
			attachment.StorageURL,
			attachment.Checksum,
			status,
			attachment.ErrorDetails,
			attachment.RetryCount,
			attachment.CreatedAt,
		)
		if err != nil {
			return fmt.Errorf("failed to create attachment %s: %w", attachment.Filename, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// GetByID retrieves an attachment by its ID
func (r *AttachmentRepository) GetByID(ctx context.Context, id uuid.UUID) (*Attachment, error) {
	query := `
		SELECT id, email_id, filename, content_type, size_bytes, storage_key, storage_url, checksum, status, error_details, retry_count, created_at
		FROM attachments
		WHERE id = $1
	`

	var attachment Attachment
	err := r.db.GetContext(ctx, &attachment, query, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get attachment: %w", err)
	}

	return &attachment, nil
}

// GetByEmailID retrieves all attachments for an email
func (r *AttachmentRepository) GetByEmailID(ctx context.Context, emailID uuid.UUID) ([]*Attachment, error) {
	query := `
		SELECT id, email_id, filename, content_type, size_bytes, storage_key, storage_url, checksum, status, error_details, retry_count, created_at
		FROM attachments
		WHERE email_id = $1
		ORDER BY created_at ASC
	`

	var attachments []*Attachment
	err := r.db.SelectContext(ctx, &attachments, query, emailID)
	if err != nil {
		return nil, fmt.Errorf("failed to get attachments: %w", err)
	}

	return attachments, nil
}

// GetByStorageKey retrieves an attachment by its storage key
func (r *AttachmentRepository) GetByStorageKey(ctx context.Context, storageKey string) (*Attachment, error) {
	query := `
		SELECT id, email_id, filename, content_type, size_bytes, storage_key, storage_url, checksum, status, error_details, retry_count, created_at
		FROM attachments
		WHERE storage_key = $1
	`

	var attachment Attachment
	err := r.db.GetContext(ctx, &attachment, query, storageKey)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get attachment: %w", err)
	}

	return &attachment, nil
}

// Delete deletes an attachment by its ID
func (r *AttachmentRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM attachments WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete attachment: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("attachment not found")
	}

	return nil
}

// DeleteByEmailID deletes all attachments for an email
func (r *AttachmentRepository) DeleteByEmailID(ctx context.Context, emailID uuid.UUID) (int64, error) {
	query := `DELETE FROM attachments WHERE email_id = $1`

	result, err := r.db.ExecContext(ctx, query, emailID)
	if err != nil {
		return 0, fmt.Errorf("failed to delete attachments: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return rowsAffected, nil
}

// GetStorageKeysByEmailID retrieves storage keys for all attachments of an email
func (r *AttachmentRepository) GetStorageKeysByEmailID(ctx context.Context, emailID uuid.UUID) ([]string, error) {
	query := `SELECT storage_key FROM attachments WHERE email_id = $1`

	var keys []string
	err := r.db.SelectContext(ctx, &keys, query, emailID)
	if err != nil {
		return nil, fmt.Errorf("failed to get storage keys: %w", err)
	}

	return keys, nil
}

// GetTotalSizeByEmailID returns the total size of all attachments for an email
func (r *AttachmentRepository) GetTotalSizeByEmailID(ctx context.Context, emailID uuid.UUID) (int64, error) {
	query := `SELECT COALESCE(SUM(size_bytes), 0) FROM attachments WHERE email_id = $1`

	var totalSize int64
	err := r.db.GetContext(ctx, &totalSize, query, emailID)
	if err != nil {
		return 0, fmt.Errorf("failed to get total size: %w", err)
	}

	return totalSize, nil
}

// CountByEmailID returns the count of attachments for an email
func (r *AttachmentRepository) CountByEmailID(ctx context.Context, emailID uuid.UUID) (int, error) {
	query := `SELECT COUNT(*) FROM attachments WHERE email_id = $1`

	var count int
	err := r.db.GetContext(ctx, &count, query, emailID)
	if err != nil {
		return 0, fmt.Errorf("failed to count attachments: %w", err)
	}

	return count, nil
}

// UpdateStatus updates the status of an attachment
// Requirements: 1.10 - Mark attachment as failed on permanent failure
func (r *AttachmentRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status string, errorDetails *string, retryCount int) error {
	query := `
		UPDATE attachments 
		SET status = $2, error_details = $3, retry_count = $4
		WHERE id = $1
	`

	result, err := r.db.ExecContext(ctx, query, id, status, errorDetails, retryCount)
	if err != nil {
		return fmt.Errorf("failed to update attachment status: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("attachment not found")
	}

	return nil
}

// MarkAsFailed marks an attachment as failed with error details
// Requirements: 1.10 - Mark attachment as failed on permanent failure, log error details
func (r *AttachmentRepository) MarkAsFailed(ctx context.Context, id uuid.UUID, errorDetails string, retryCount int) error {
	return r.UpdateStatus(ctx, id, "failed", &errorDetails, retryCount)
}

// GetFailedAttachments retrieves all failed attachments
// Requirements: 1.10 - Track failed attachments for monitoring
func (r *AttachmentRepository) GetFailedAttachments(ctx context.Context, limit int) ([]*Attachment, error) {
	query := `
		SELECT id, email_id, filename, content_type, size_bytes, storage_key, storage_url, checksum, status, error_details, retry_count, created_at
		FROM attachments
		WHERE status = 'failed'
		ORDER BY created_at DESC
		LIMIT $1
	`

	var attachments []*Attachment
	err := r.db.SelectContext(ctx, &attachments, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get failed attachments: %w", err)
	}

	return attachments, nil
}

// GetFailedAttachmentsByEmailID retrieves failed attachments for a specific email
// Requirements: 1.10 - Track failed attachments
func (r *AttachmentRepository) GetFailedAttachmentsByEmailID(ctx context.Context, emailID uuid.UUID) ([]*Attachment, error) {
	query := `
		SELECT id, email_id, filename, content_type, size_bytes, storage_key, storage_url, checksum, status, error_details, retry_count, created_at
		FROM attachments
		WHERE email_id = $1 AND status = 'failed'
		ORDER BY created_at ASC
	`

	var attachments []*Attachment
	err := r.db.SelectContext(ctx, &attachments, query, emailID)
	if err != nil {
		return nil, fmt.Errorf("failed to get failed attachments: %w", err)
	}

	return attachments, nil
}

// CountFailedAttachments returns the count of failed attachments
// Requirements: 1.10 - Track failed attachments for monitoring
func (r *AttachmentRepository) CountFailedAttachments(ctx context.Context) (int, error) {
	query := `SELECT COUNT(*) FROM attachments WHERE status = 'failed'`

	var count int
	err := r.db.GetContext(ctx, &count, query)
	if err != nil {
		return 0, fmt.Errorf("failed to count failed attachments: %w", err)
	}

	return count, nil
}

// GetActiveAttachmentsByEmailID retrieves only active (successfully uploaded) attachments for an email
func (r *AttachmentRepository) GetActiveAttachmentsByEmailID(ctx context.Context, emailID uuid.UUID) ([]*Attachment, error) {
	query := `
		SELECT id, email_id, filename, content_type, size_bytes, storage_key, storage_url, checksum, status, error_details, retry_count, created_at
		FROM attachments
		WHERE email_id = $1 AND status = 'active'
		ORDER BY created_at ASC
	`

	var attachments []*Attachment
	err := r.db.SelectContext(ctx, &attachments, query, emailID)
	if err != nil {
		return nil, fmt.Errorf("failed to get active attachments: %w", err)
	}

	return attachments, nil
}

// CreateFailedAttachment creates a record for a failed attachment upload
// Requirements: 1.10 - Mark attachment as failed on permanent failure
func (r *AttachmentRepository) CreateFailedAttachment(ctx context.Context, attachment *Attachment) error {
	query := `
		INSERT INTO attachments (id, email_id, filename, content_type, size_bytes, storage_key, storage_url, checksum, status, error_details, retry_count, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'failed', $9, $10, $11)
	`

	_, err := r.db.ExecContext(ctx, query,
		attachment.ID,
		attachment.EmailID,
		attachment.Filename,
		attachment.ContentType,
		attachment.SizeBytes,
		attachment.StorageKey,
		attachment.StorageURL,
		attachment.Checksum,
		attachment.ErrorDetails,
		attachment.RetryCount,
		attachment.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to create failed attachment record: %w", err)
	}

	return nil
}




// ExistsInDatabase checks if a storage key exists in the attachments table
// Requirements: 4.7 - Support orphan cleanup job
func (r *AttachmentRepository) ExistsInDatabase(ctx context.Context, storageKey string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM attachments WHERE storage_key = $1)`

	var exists bool
	err := r.db.GetContext(ctx, &exists, query, storageKey)
	if err != nil {
		return false, fmt.Errorf("failed to check storage key existence: %w", err)
	}

	return exists, nil
}

// BatchExistsInDatabase checks multiple storage keys and returns a map of which ones exist
// Requirements: 4.7 - Support orphan cleanup job with batch operations
func (r *AttachmentRepository) BatchExistsInDatabase(ctx context.Context, storageKeys []string) (map[string]bool, error) {
	if len(storageKeys) == 0 {
		return make(map[string]bool), nil
	}

	// Build query with ANY for efficient batch lookup
	query := `SELECT storage_key FROM attachments WHERE storage_key = ANY($1)`

	var existingKeys []string
	err := r.db.SelectContext(ctx, &existingKeys, query, storageKeys)
	if err != nil {
		return nil, fmt.Errorf("failed to check storage keys existence: %w", err)
	}

	// Build result map
	result := make(map[string]bool, len(storageKeys))
	for _, key := range storageKeys {
		result[key] = false
	}
	for _, key := range existingKeys {
		result[key] = true
	}

	return result, nil
}

// GetAllStorageKeys returns all storage keys in the database
// Requirements: 4.7 - Support orphan cleanup job
func (r *AttachmentRepository) GetAllStorageKeys(ctx context.Context) ([]string, error) {
	query := `SELECT storage_key FROM attachments WHERE storage_key IS NOT NULL AND storage_key != ''`

	var keys []string
	err := r.db.SelectContext(ctx, &keys, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get all storage keys: %w", err)
	}

	return keys, nil
}
