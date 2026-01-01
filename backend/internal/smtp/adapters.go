// Package smtp provides SMTP server functionality
// Feature: smtp-email-receiver
// Task 11.1: Wire all components together - Repository adapters
package smtp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/events"
)

// PgxAliasRepository implements AliasLookupRepository using pgxpool
type PgxAliasRepository struct {
	pool *pgxpool.Pool
}

// NewPgxAliasRepository creates a new PgxAliasRepository
func NewPgxAliasRepository(pool *pgxpool.Pool) *PgxAliasRepository {
	return &PgxAliasRepository{pool: pool}
}

// GetByFullAddress retrieves alias info by full email address (case-insensitive)
// Requirements: 2.1-2.5 - Recipient validation
func (r *PgxAliasRepository) GetByFullAddress(ctx context.Context, fullAddress string) (*AliasInfo, error) {
	query := `
		SELECT id, is_active
		FROM aliases
		WHERE LOWER(full_address) = LOWER($1)
	`

	var alias AliasInfo
	err := r.pool.QueryRow(ctx, query, fullAddress).Scan(&alias.ID, &alias.IsActive)
	if err != nil {
		return nil, fmt.Errorf("alias not found: %w", err)
	}

	return &alias, nil
}

// GetUserIDByAliasID retrieves the user ID for an alias
func (r *PgxAliasRepository) GetUserIDByAliasID(ctx context.Context, aliasID string) (string, error) {
	query := `SELECT user_id FROM aliases WHERE id = $1`

	var userID string
	err := r.pool.QueryRow(ctx, query, aliasID).Scan(&userID)
	if err != nil {
		return "", fmt.Errorf("failed to get user ID: %w", err)
	}

	return userID, nil
}

// PgxEmailRepository implements EmailRepository using pgxpool
type PgxEmailRepository struct {
	pool *pgxpool.Pool
}

// NewPgxEmailRepository creates a new PgxEmailRepository
func NewPgxEmailRepository(pool *pgxpool.Pool) *PgxEmailRepository {
	return &PgxEmailRepository{pool: pool}
}

// Create creates a new email record in the database
// Requirements: 3.3, 3.5 - Store email with queue ID and received_at timestamp
func (r *PgxEmailRepository) Create(ctx context.Context, email *Email) error {
	headersJSON, err := json.Marshal(email.Headers)
	if err != nil {
		headersJSON = []byte("{}")
	}

	query := `
		INSERT INTO emails (id, alias_id, sender_address, sender_name, subject, body_html, body_text, headers, size_bytes, is_read, raw_email, received_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`

	_, err = r.pool.Exec(ctx, query,
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

// PgxAttachmentRepository implements AttachmentRepository using pgxpool
type PgxAttachmentRepository struct {
	pool *pgxpool.Pool
}

// NewPgxAttachmentRepository creates a new PgxAttachmentRepository
func NewPgxAttachmentRepository(pool *pgxpool.Pool) *PgxAttachmentRepository {
	return &PgxAttachmentRepository{pool: pool}
}

// CreateBatch creates multiple attachment records in a single transaction
// Requirements: 5.5 - Record filename, content_type, size_bytes in database
func (r *PgxAttachmentRepository) CreateBatch(ctx context.Context, attachments []*Attachment) error {
	if len(attachments) == 0 {
		return nil
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	query := `
		INSERT INTO attachments (id, email_id, filename, content_type, size_bytes, storage_key, storage_url, checksum, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`

	for _, att := range attachments {
		_, err := tx.Exec(ctx, query,
			att.ID,
			att.EmailID,
			att.Filename,
			att.ContentType,
			att.SizeBytes,
			att.StorageKey,
			att.StorageURL,
			att.Checksum,
			att.CreatedAt,
		)
		if err != nil {
			return fmt.Errorf("failed to create attachment %s: %w", att.Filename, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// SMTPServerWithProcessor extends SMTPServer with email processing capabilities
type SMTPServerWithProcessor struct {
	*SMTPServer
	processor *EmailProcessor
}

// NewSMTPServerWithProcessor creates a new SMTP server with email processing
func NewSMTPServerWithProcessor(server *SMTPServer, processor *EmailProcessor) *SMTPServerWithProcessor {
	return &SMTPServerWithProcessor{
		SMTPServer: server,
		processor:  processor,
	}
}

// ProcessDataResult processes the data result from an SMTP session
func (s *SMTPServerWithProcessor) ProcessDataResult(ctx context.Context, data *DataResult) (*ProcessResult, error) {
	if s.processor == nil {
		return nil, fmt.Errorf("email processor not configured")
	}
	return s.processor.ProcessEmail(ctx, data)
}

// SMTPSessionWithProcessor extends SMTPSession with processing callback
type SMTPSessionWithProcessor struct {
	*SMTPSession
	onDataComplete func(ctx context.Context, data *DataResult) error
}

// SetDataCompleteCallback sets the callback for when DATA command completes
func (s *SMTPSession) SetDataCompleteCallback(callback func(ctx context.Context, data *DataResult) error) {
	// Store callback for use after DATA command
	// This would be called in handleDATA after successful data reception
}

// ConvertToRepositoryEmail converts smtp.Email to repository format
func ConvertToRepositoryEmail(email *Email) map[string]interface{} {
	return map[string]interface{}{
		"id":             email.ID,
		"alias_id":       email.AliasID,
		"sender_address": email.SenderAddress,
		"sender_name":    email.SenderName,
		"subject":        email.Subject,
		"body_html":      email.BodyHTML,
		"body_text":      email.BodyText,
		"headers":        email.Headers,
		"size_bytes":     email.SizeBytes,
		"is_read":        email.IsRead,
		"raw_email":      email.RawEmail,
		"received_at":    email.ReceivedAt,
		"created_at":     email.CreatedAt,
	}
}

// ConvertToRepositoryAttachment converts smtp.Attachment to repository format
func ConvertToRepositoryAttachment(att *Attachment) map[string]interface{} {
	return map[string]interface{}{
		"id":           att.ID,
		"email_id":     att.EmailID,
		"filename":     att.Filename,
		"content_type": att.ContentType,
		"size_bytes":   att.SizeBytes,
		"storage_key":  att.StorageKey,
		"storage_url":  att.StorageURL,
		"checksum":     att.Checksum,
		"created_at":   att.CreatedAt,
	}
}

// CreateEmailFromParsed creates an Email from parsed email data
func CreateEmailFromParsed(aliasID uuid.UUID, parsed interface{}, data *DataResult) *Email {
	now := time.Now().UTC()
	return &Email{
		ID:         uuid.New(),
		AliasID:    aliasID,
		SizeBytes:  data.SizeBytes,
		IsRead:     false,
		RawEmail:   data.Data,
		ReceivedAt: data.ReceivedAt,
		CreatedAt:  now,
	}
}


// EventBusAdapter adapts events.EventBus to the smtp.EventPublisher interface
// This allows the SMTP processor to publish events to the real-time notification system
// Requirements: 3.1 - Publish new_email event after email stored
type EventBusAdapter struct {
	eventBus events.EventBus
}

// NewEventBusAdapter creates a new EventBusAdapter
func NewEventBusAdapter(eventBus events.EventBus) *EventBusAdapter {
	return &EventBusAdapter{eventBus: eventBus}
}

// Publish publishes an SMTP event to the event bus
// Converts the SMTP Event type to the events.Event type
func (a *EventBusAdapter) Publish(event Event) error {
	if a.eventBus == nil {
		return nil
	}

	// Convert SMTP Event to events.Event
	eventsEvent := events.Event{
		ID:        event.ID,
		Type:      string(event.Type),
		UserID:    event.UserID,
		Data:      event.Data,
		Timestamp: event.Timestamp,
	}

	return a.eventBus.Publish(eventsEvent)
}
