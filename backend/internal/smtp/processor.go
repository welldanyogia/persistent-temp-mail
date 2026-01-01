// Package smtp provides SMTP server functionality
// Feature: smtp-email-receiver
// Task 11.1: Wire all components together
package smtp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/attachment"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/parser"
)

// EmailProcessor handles the full email processing pipeline
// Connects SMTP server → parser → attachment handler → repositories
// Requirements: All SMTP email receiver requirements
type EmailProcessor struct {
	parser            *parser.EmailParser
	attachmentHandler *attachment.Handler
	emailRepo         EmailRepository
	attachmentRepo    AttachmentRepository
	aliasRepo         AliasLookupRepository
	eventPublisher    EventPublisher
	logger            *log.Logger
}

// EmailRepository interface for storing emails
type EmailRepository interface {
	Create(ctx context.Context, email *Email) error
}

// AttachmentRepository interface for storing attachment metadata
type AttachmentRepository interface {
	CreateBatch(ctx context.Context, attachments []*Attachment) error
}

// AliasLookupRepository interface for looking up alias information
type AliasLookupRepository interface {
	GetByFullAddress(ctx context.Context, fullAddress string) (*AliasInfo, error)
	GetUserIDByAliasID(ctx context.Context, aliasID string) (string, error)
}

// Email represents an email to be stored
type Email struct {
	ID            uuid.UUID         `db:"id"`
	AliasID       uuid.UUID         `db:"alias_id"`
	SenderAddress string            `db:"sender_address"`
	SenderName    *string           `db:"sender_name"`
	Subject       *string           `db:"subject"`
	BodyHTML      *string           `db:"body_html"`
	BodyText      *string           `db:"body_text"`
	Headers       map[string]string `db:"headers"`
	SizeBytes     int64             `db:"size_bytes"`
	IsRead        bool              `db:"is_read"`
	RawEmail      []byte            `db:"raw_email"`
	ReceivedAt    time.Time         `db:"received_at"`
	CreatedAt     time.Time         `db:"created_at"`
}

// Attachment represents attachment metadata to be stored
type Attachment struct {
	ID          uuid.UUID `db:"id"`
	EmailID     uuid.UUID `db:"email_id"`
	Filename    string    `db:"filename"`
	ContentType string    `db:"content_type"`
	SizeBytes   int64     `db:"size_bytes"`
	StorageKey  string    `db:"storage_key"`
	StorageURL  string    `db:"storage_url"`
	Checksum    string    `db:"checksum"`
	CreatedAt   time.Time `db:"created_at"`
}

// ProcessorConfig holds configuration for the email processor
type ProcessorConfig struct {
	Parser            *parser.EmailParser
	AttachmentHandler *attachment.Handler
	EmailRepo         EmailRepository
	AttachmentRepo    AttachmentRepository
	AliasRepo         AliasLookupRepository
	EventPublisher    EventPublisher
	Logger            *log.Logger
}

// NewEmailProcessor creates a new email processor
func NewEmailProcessor(cfg ProcessorConfig) *EmailProcessor {
	logger := cfg.Logger
	if logger == nil {
		logger = log.Default()
	}

	eventPublisher := cfg.EventPublisher
	if eventPublisher == nil {
		eventPublisher = NewNoOpEventPublisher()
	}

	return &EmailProcessor{
		parser:            cfg.Parser,
		attachmentHandler: cfg.AttachmentHandler,
		emailRepo:         cfg.EmailRepo,
		attachmentRepo:    cfg.AttachmentRepo,
		aliasRepo:         cfg.AliasRepo,
		eventPublisher:    eventPublisher,
		logger:            logger,
	}
}

// ProcessResult contains the result of processing an email
type ProcessResult struct {
	EmailID        string
	QueueID        string
	Recipients     []string
	AttachmentCount int
	Errors         []string
}

// ProcessEmail processes a received email through the full pipeline
// Requirements: All - connects SMTP → parser → attachment handler → repositories
func (p *EmailProcessor) ProcessEmail(ctx context.Context, data *DataResult) (*ProcessResult, error) {
	result := &ProcessResult{
		QueueID:    data.QueueID,
		Recipients: data.Recipients,
		Errors:     []string{},
	}

	// Parse the email
	// Requirements: 4.1-4.12
	parsedEmail := p.parser.SafeParse(data.Data)
	if parsedEmail == nil {
		return nil, fmt.Errorf("failed to parse email")
	}

	// Process for each recipient
	for _, recipient := range data.Recipients {
		emailID, attachmentCount, err := p.processForRecipient(ctx, parsedEmail, data, recipient)
		if err != nil {
			p.logger.Printf("Error processing email for recipient %s: %v", recipient, err)
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", recipient, err))
			continue
		}
		result.EmailID = emailID
		result.AttachmentCount = attachmentCount
	}

	return result, nil
}

// processForRecipient processes an email for a single recipient
func (p *EmailProcessor) processForRecipient(ctx context.Context, parsedEmail *parser.ParsedEmail, data *DataResult, recipient string) (string, int, error) {
	// Look up alias information
	alias, err := p.aliasRepo.GetByFullAddress(ctx, strings.ToLower(recipient))
	if err != nil {
		return "", 0, fmt.Errorf("failed to lookup alias: %w", err)
	}

	aliasID, err := uuid.Parse(alias.ID)
	if err != nil {
		return "", 0, fmt.Errorf("invalid alias ID: %w", err)
	}

	// Generate email ID
	emailID := uuid.New()

	// Process attachments if handler is available
	var processedAttachments []*attachment.ProcessedAttachment
	if p.attachmentHandler != nil {
		processed, validationErrors := p.attachmentHandler.ProcessAndValidate(ctx, emailID.String(), data.Data)
		processedAttachments = processed
		for _, validErr := range validationErrors {
			p.logger.Printf("Attachment validation error: %s - %s", validErr.Filename, validErr.Reason)
		}
	}

	// Create email record
	// Requirements: 3.5 - Record received_at timestamp in UTC
	email := &Email{
		ID:            emailID,
		AliasID:       aliasID,
		SenderAddress: parsedEmail.From,
		SenderName:    stringPtr(parsedEmail.FromName),
		Subject:       stringPtr(parsedEmail.Subject),
		BodyHTML:      stringPtr(parsedEmail.BodyHTML),
		BodyText:      stringPtr(parsedEmail.BodyText),
		Headers:       parsedEmail.Headers,
		SizeBytes:     data.SizeBytes,
		IsRead:        false,
		RawEmail:      data.Data,
		ReceivedAt:    data.ReceivedAt,
		CreatedAt:     time.Now().UTC(),
	}

	// Store email in database
	if err := p.emailRepo.Create(ctx, email); err != nil {
		return "", 0, fmt.Errorf("failed to store email: %w", err)
	}

	// Store attachment metadata in database
	// Requirements: 5.5 - Record filename, content_type, size_bytes in database
	if len(processedAttachments) > 0 && p.attachmentRepo != nil {
		attachments := make([]*Attachment, len(processedAttachments))
		for i, att := range processedAttachments {
			attID, _ := uuid.Parse(att.ID)
			attachments[i] = &Attachment{
				ID:          attID,
				EmailID:     emailID,
				Filename:    att.Filename,
				ContentType: att.ContentType,
				SizeBytes:   att.SizeBytes,
				StorageKey:  att.StorageKey,
				StorageURL:  att.StorageURL,
				Checksum:    att.Checksum,
				CreatedAt:   att.CreatedAt,
			}
		}

		if err := p.attachmentRepo.CreateBatch(ctx, attachments); err != nil {
			p.logger.Printf("Failed to store attachment metadata: %v", err)
			// Don't fail the whole email processing for attachment metadata errors
		}
	}

	// Publish new email event
	// Requirements: 8.1, 8.2, 8.3 - Real-time notification
	if err := p.publishNewEmailEvent(ctx, email, alias, len(processedAttachments) > 0); err != nil {
		p.logger.Printf("Failed to publish new email event: %v", err)
		// Don't fail the whole email processing for event publishing errors
	}

	return emailID.String(), len(processedAttachments), nil
}

// publishNewEmailEvent publishes a new email event to the event bus
// Requirements: 8.1, 8.2, 8.3
func (p *EmailProcessor) publishNewEmailEvent(ctx context.Context, email *Email, alias *AliasInfo, hasAttachments bool) error {
	// Get user ID for the alias
	userID, err := p.aliasRepo.GetUserIDByAliasID(ctx, alias.ID)
	if err != nil {
		return fmt.Errorf("failed to get user ID: %w", err)
	}

	// Create new email event
	newEmailEvent := CreateNewEmailEvent(
		email.ID.String(),
		alias.ID,
		"", // alias email - would need to be passed or looked up
		email.SenderAddress,
		email.SenderName,
		email.Subject,
		email.BodyText,
		email.BodyHTML,
		email.ReceivedAt,
		hasAttachments,
		email.SizeBytes,
	)

	// Convert to generic event
	event, err := newEmailEvent.ToEvent(userID)
	if err != nil {
		return fmt.Errorf("failed to create event: %w", err)
	}

	// Publish event
	return p.eventPublisher.Publish(*event)
}

// stringPtr returns a pointer to a string, or nil if empty
func stringPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// EmailRepositoryAdapter adapts the repository.EmailRepository to the EmailRepository interface
type EmailRepositoryAdapter struct {
	repo interface {
		Create(ctx context.Context, email interface{}) error
	}
}

// AttachmentRepositoryAdapter adapts the repository.AttachmentRepository to the AttachmentRepository interface
type AttachmentRepositoryAdapter struct {
	repo interface {
		CreateBatch(ctx context.Context, attachments interface{}) error
	}
}

// MarshalJSON implements json.Marshaler for Email
func (e *Email) MarshalJSON() ([]byte, error) {
	type Alias Email
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(e),
	})
}
