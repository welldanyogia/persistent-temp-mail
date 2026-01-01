// Package email provides email inbox management functionality
// Feature: email-inbox-api
// Requirements: 1.1-1.9, 2.1-2.8, 3.1-3.7, 4.1-4.5, 5.1-5.5, 6.1-6.5, 7.1-7.5
package email

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/events"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/repository"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/sanitizer"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/storage"
)

// Service errors
var (
	ErrEmailNotFound       = errors.New("email not found")
	ErrAttachmentNotFound  = errors.New("attachment not found")
	ErrAccessDenied        = errors.New("access denied")
	ErrAttachmentGone      = errors.New("attachment file missing from storage")
	ErrChecksumMismatch    = errors.New("attachment checksum mismatch")
	ErrBulkLimitExceeded   = errors.New("bulk operation limit exceeded")
)

// Error codes for API responses
const (
	CodeValidationError     = "VALIDATION_ERROR"
	CodeEmailNotFound       = "EMAIL_NOT_FOUND"
	CodeAttachmentNotFound  = "ATTACHMENT_NOT_FOUND"
	CodeForbidden           = "FORBIDDEN"
	CodeAttachmentDeleted   = "ATTACHMENT_DELETED"
	CodeBulkLimitExceeded   = "BULK_LIMIT_EXCEEDED"
	CodeChecksumMismatch    = "CHECKSUM_MISMATCH"
)

// MaxBulkOperationItems is the maximum number of items in a bulk operation
const MaxBulkOperationItems = 100

// ListEmailParams holds parameters for listing emails
type ListEmailParams struct {
	Page           int        `json:"page" validate:"min=1"`
	Limit          int        `json:"limit" validate:"min=1,max=100"`
	AliasID        string     `json:"alias_id,omitempty" validate:"omitempty,uuid"`
	Search         string     `json:"search,omitempty" validate:"omitempty,max=100"`
	FromDate       *time.Time `json:"from_date,omitempty"`
	ToDate         *time.Time `json:"to_date,omitempty"`
	HasAttachments *bool      `json:"has_attachments,omitempty"`
	IsRead         *bool      `json:"is_read,omitempty"`
	Sort           string     `json:"sort,omitempty" validate:"omitempty,oneof=received_at size"`
	Order          string     `json:"order,omitempty" validate:"omitempty,oneof=asc desc"`
}

// EmailListResponse represents the paginated list of emails
type EmailListResponse struct {
	Emails     []EmailWithPreview `json:"emails"`
	Pagination Pagination         `json:"pagination"`
}

// EmailWithPreview represents an email with preview text for list responses
type EmailWithPreview struct {
	ID              string     `json:"id"`
	AliasID         string     `json:"alias_id"`
	AliasEmail      string     `json:"alias_email"`
	FromAddress     string     `json:"from_address"`
	FromName        *string    `json:"from_name,omitempty"`
	Subject         *string    `json:"subject,omitempty"`
	PreviewText     string     `json:"preview_text"`
	ReceivedAt      time.Time  `json:"received_at"`
	HasAttachments  bool       `json:"has_attachments"`
	AttachmentCount int        `json:"attachment_count"`
	SizeBytes       int64      `json:"size_bytes"`
	IsRead          bool       `json:"is_read"`
}

// Pagination represents pagination metadata
type Pagination struct {
	CurrentPage int `json:"current_page"`
	PerPage     int `json:"per_page"`
	TotalPages  int `json:"total_pages"`
	TotalCount  int `json:"total_count"`
}

// EmailDetailResponse represents complete email content
type EmailDetailResponse struct {
	ID             string               `json:"id"`
	AliasID        string               `json:"alias_id"`
	AliasEmail     string               `json:"alias_email"`
	FromAddress    string               `json:"from_address"`
	FromName       *string              `json:"from_name,omitempty"`
	Subject        *string              `json:"subject,omitempty"`
	BodyHTML       *string              `json:"body_html,omitempty"`
	BodyText       *string              `json:"body_text,omitempty"`
	Headers        map[string]string    `json:"headers"`
	ReceivedAt     time.Time            `json:"received_at"`
	SizeBytes      int64                `json:"size_bytes"`
	IsRead         bool                 `json:"is_read"`
	HasAttachments bool                 `json:"has_attachments"`
	Attachments    []AttachmentResponse `json:"attachments"`
}

// AttachmentResponse represents attachment metadata with download URL
type AttachmentResponse struct {
	ID          string    `json:"id"`
	Filename    string    `json:"filename"`
	ContentType string    `json:"content_type"`
	SizeBytes   int64     `json:"size_bytes"`
	DownloadURL string    `json:"download_url"`
	CreatedAt   time.Time `json:"created_at"`
}

// AttachmentDownload represents attachment data for download
type AttachmentDownload struct {
	Filename    string
	ContentType string
	SizeBytes   int64
	Data        io.ReadCloser
	Checksum    string
}

// AttachmentURLResponse represents a pre-signed URL response for large file downloads
// Requirements: 3.2 (Return pre-signed URL instead of streaming for large files)
type AttachmentURLResponse struct {
	DownloadURL string `json:"download_url"`
	ExpiresIn   int    `json:"expires_in"` // Expiration time in seconds
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
	Checksum    string `json:"checksum"`
}

// DeleteEmailResponse represents the response after deleting an email
type DeleteEmailResponse struct {
	Message             string `json:"message"`
	EmailID             string `json:"email_id"`
	AttachmentsDeleted  int    `json:"attachments_deleted"`
	TotalSizeFreedBytes int64  `json:"total_size_freed_bytes"`
}

// BulkOperationRequest represents a bulk operation request
type BulkOperationRequest struct {
	EmailIDs []string `json:"email_ids" validate:"required,min=1,max=100"`
}

// BulkOperationResponse represents the response for bulk operations
type BulkOperationResponse struct {
	SuccessCount int      `json:"success_count"`
	FailedCount  int      `json:"failed_count"`
	FailedIDs    []string `json:"failed_ids,omitempty"`
}

// InboxStatsResponse represents inbox statistics
type InboxStatsResponse struct {
	TotalEmails     int               `json:"total_emails"`
	UnreadEmails    int               `json:"unread_emails"`
	TotalSizeBytes  int64             `json:"total_size_bytes"`
	EmailsToday     int               `json:"emails_today"`
	EmailsThisWeek  int               `json:"emails_this_week"`
	EmailsThisMonth int               `json:"emails_this_month"`
	EmailsPerAlias  []AliasEmailCount `json:"emails_per_alias"`
}

// AliasEmailCount represents email count per alias
type AliasEmailCount struct {
	AliasID    string `json:"alias_id"`
	AliasEmail string `json:"alias_email"`
	Count      int    `json:"count"`
}


// Service handles email business logic
// Feature: email-inbox-api
type Service struct {
	emailRepo      *repository.EmailRepo
	attachmentRepo *repository.AttachmentRepository
	storageService *storage.StorageService
	sanitizer      sanitizer.HTMLSanitizer
	eventBus       events.EventBus
	logger         *slog.Logger
	baseURL        string // Base URL for generating download URLs
}

// ServiceConfig contains configuration for the email Service
type ServiceConfig struct {
	EmailRepo      *repository.EmailRepo
	AttachmentRepo *repository.AttachmentRepository
	StorageService *storage.StorageService
	Sanitizer      sanitizer.HTMLSanitizer
	EventBus       events.EventBus
	Logger         *slog.Logger
	BaseURL        string // Base URL for generating download URLs (e.g., "https://api.webrana.id/v1")
}

// NewService creates a new email Service instance
func NewService(cfg ServiceConfig) *Service {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Sanitizer == nil {
		cfg.Sanitizer = sanitizer.NewHTMLSanitizer()
	}

	return &Service{
		emailRepo:      cfg.EmailRepo,
		attachmentRepo: cfg.AttachmentRepo,
		storageService: cfg.StorageService,
		sanitizer:      cfg.Sanitizer,
		eventBus:       cfg.EventBus,
		logger:         cfg.Logger,
		baseURL:        cfg.BaseURL,
	}
}

// List retrieves emails for a user with pagination, filtering, search, and sorting
// Requirements: 1.1-1.9 (List emails with filters)
// Property 1: Pagination Correctness
// Property 2: List Filtering Correctness
// Property 3: Sort Order Correctness
// Property 5: Authorization Enforcement (list part)
func (s *Service) List(ctx context.Context, userID uuid.UUID, params ListEmailParams) (*EmailListResponse, error) {
	// Apply defaults (Requirement: 1.7)
	if params.Page < 1 {
		params.Page = 1
	}
	if params.Limit < 1 {
		params.Limit = 20
	}
	if params.Limit > 100 {
		params.Limit = 100
	}

	// Convert params to repository params
	repoParams := repository.ListEmailParams{
		Page:           params.Page,
		Limit:          params.Limit,
		Search:         params.Search,
		FromDate:       params.FromDate,
		ToDate:         params.ToDate,
		HasAttachments: params.HasAttachments,
		IsRead:         params.IsRead,
		Sort:           params.Sort,
		Order:          params.Order,
	}

	// Parse alias filter if provided (Requirement: 1.2)
	if params.AliasID != "" {
		aliasID, err := uuid.Parse(params.AliasID)
		if err != nil {
			return nil, fmt.Errorf("invalid alias_id format: %w", err)
		}
		repoParams.AliasID = &aliasID
	}

	// Get emails from repository (Requirement: 1.9 - only returns emails for user's aliases)
	emails, totalCount, err := s.emailRepo.List(ctx, userID, repoParams)
	if err != nil {
		return nil, fmt.Errorf("failed to list emails: %w", err)
	}

	// Convert to response format
	emailResponses := make([]EmailWithPreview, len(emails))
	for i, e := range emails {
		emailResponses[i] = EmailWithPreview{
			ID:              e.ID.String(),
			AliasID:         e.AliasID.String(),
			AliasEmail:      e.AliasEmail,
			FromAddress:     e.FromAddress,
			FromName:        e.FromName,
			Subject:         e.Subject,
			PreviewText:     e.PreviewText,
			ReceivedAt:      e.ReceivedAt,
			HasAttachments:  e.HasAttachments,
			AttachmentCount: e.AttachmentCount,
			SizeBytes:       e.SizeBytes,
			IsRead:          e.IsRead,
		}
	}

	// Calculate pagination
	totalPages := (totalCount + params.Limit - 1) / params.Limit
	if totalPages < 1 {
		totalPages = 1
	}

	return &EmailListResponse{
		Emails: emailResponses,
		Pagination: Pagination{
			CurrentPage: params.Page,
			PerPage:     params.Limit,
			TotalPages:  totalPages,
			TotalCount:  totalCount,
		},
	}, nil
}


// GetByID retrieves an email by ID with ownership check
// Requirements: 2.1-2.8 (Get email details)
// Property 5: Authorization Enforcement (get part)
// Property 6: Email Details Content
// Property 7: Mark As Read Behavior
// Property 8: HTML Sanitization
func (s *Service) GetByID(ctx context.Context, userID uuid.UUID, emailID string, markAsRead bool) (*EmailDetailResponse, error) {
	// Parse email ID
	id, err := uuid.Parse(emailID)
	if err != nil {
		return nil, ErrEmailNotFound
	}

	// Check ownership (Requirement: 2.3)
	owned, err := s.emailRepo.IsOwnedByUser(ctx, id, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to check email ownership: %w", err)
	}
	if !owned {
		// Check if email exists at all
		_, err := s.emailRepo.GetByID(ctx, id)
		if err != nil {
			if errors.Is(err, repository.ErrEmailNotFound) {
				return nil, ErrEmailNotFound
			}
			return nil, fmt.Errorf("failed to get email: %w", err)
		}
		return nil, ErrAccessDenied
	}

	// Get email from repository (Requirement: 2.1)
	email, err := s.emailRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrEmailNotFound) {
			return nil, ErrEmailNotFound
		}
		return nil, fmt.Errorf("failed to get email: %w", err)
	}

	// Mark as read if requested (Requirement: 2.7)
	if markAsRead && !email.IsRead {
		if err := s.emailRepo.MarkAsRead(ctx, id); err != nil {
			s.logger.Warn("Failed to mark email as read", "email_id", id, "error", err)
			// Continue even if marking as read fails
		} else {
			email.IsRead = true
		}
	}

	// Get attachments (Requirement: 2.6)
	attachments, err := s.attachmentRepo.GetByEmailID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get attachments: %w", err)
	}

	// Get alias email address
	aliasEmail := s.getAliasEmail(ctx, email.AliasID)

	// Sanitize HTML content (Requirement: 2.8, 7.3, 7.4)
	var sanitizedHTML *string
	if email.BodyHTML != nil && *email.BodyHTML != "" {
		sanitized := s.sanitizer.Sanitize(*email.BodyHTML)
		sanitizedHTML = &sanitized
	}

	// Build attachment responses with download URLs
	attachmentResponses := make([]AttachmentResponse, len(attachments))
	for i, att := range attachments {
		attachmentResponses[i] = AttachmentResponse{
			ID:          att.ID.String(),
			Filename:    att.Filename,
			ContentType: att.ContentType,
			SizeBytes:   att.SizeBytes,
			DownloadURL: s.generateDownloadURL(emailID, att.ID.String()),
			CreatedAt:   att.CreatedAt,
		}
	}

	return &EmailDetailResponse{
		ID:             email.ID.String(),
		AliasID:        email.AliasID.String(),
		AliasEmail:     aliasEmail,
		FromAddress:    email.SenderAddress,
		FromName:       email.SenderName,
		Subject:        email.Subject,
		BodyHTML:       sanitizedHTML,
		BodyText:       email.BodyText,
		Headers:        email.Headers,
		ReceivedAt:     email.ReceivedAt,
		SizeBytes:      email.SizeBytes,
		IsRead:         email.IsRead,
		HasAttachments: len(attachments) > 0,
		Attachments:    attachmentResponses,
	}, nil
}

// getAliasEmail retrieves the alias email address for an email
func (s *Service) getAliasEmail(ctx context.Context, aliasID uuid.UUID) string {
	// This is a simplified implementation - in production, you might want to cache this
	// or include it in the email query
	return aliasID.String() // Placeholder - actual implementation would query alias table
}

// generateDownloadURL generates a download URL for an attachment
func (s *Service) generateDownloadURL(emailID, attachmentID string) string {
	return fmt.Sprintf("%s/emails/%s/attachments/%s", s.baseURL, emailID, attachmentID)
}


// GetAttachment retrieves an attachment for download with ownership and checksum verification
// Requirements: 3.1-3.7 (Download attachment)
// Property 5: Authorization Enforcement (attachment part)
// Property 9: Attachment Download
// Property 10: Attachment Integrity
func (s *Service) GetAttachment(ctx context.Context, userID uuid.UUID, emailID, attachmentID string) (*AttachmentDownload, error) {
	// Parse IDs
	eID, err := uuid.Parse(emailID)
	if err != nil {
		return nil, ErrEmailNotFound
	}
	aID, err := uuid.Parse(attachmentID)
	if err != nil {
		return nil, ErrAttachmentNotFound
	}

	// Check email ownership (Requirement: 3.3)
	owned, err := s.emailRepo.IsOwnedByUser(ctx, eID, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to check email ownership: %w", err)
	}
	if !owned {
		// Check if email exists at all
		_, err := s.emailRepo.GetByID(ctx, eID)
		if err != nil {
			if errors.Is(err, repository.ErrEmailNotFound) {
				return nil, ErrEmailNotFound
			}
			return nil, fmt.Errorf("failed to get email: %w", err)
		}
		return nil, ErrAccessDenied
	}

	// Get attachment metadata (Requirement: 3.2)
	attachment, err := s.attachmentRepo.GetByID(ctx, aID)
	if err != nil {
		return nil, fmt.Errorf("failed to get attachment: %w", err)
	}
	if attachment == nil {
		return nil, ErrAttachmentNotFound
	}

	// Verify attachment belongs to the email
	if attachment.EmailID != eID {
		return nil, ErrAttachmentNotFound
	}

	// Get file from storage (Requirement: 3.1)
	output, err := s.storageService.GetClient().GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.storageService.GetBucket()),
		Key:    aws.String(attachment.StorageKey),
	})
	if err != nil {
		// Check if file is missing (Requirement: 3.7)
		s.logger.Error("Failed to get attachment from storage", "attachment_id", aID, "storage_key", attachment.StorageKey, "error", err)
		return nil, ErrAttachmentGone
	}

	// Verify checksum before serving file (Requirements: 3.3, 3.4)
	// Read the entire file content to calculate checksum
	fileContent, err := io.ReadAll(output.Body)
	output.Body.Close()
	if err != nil {
		s.logger.Error("Failed to read attachment content for checksum verification",
			"attachment_id", aID,
			"storage_key", attachment.StorageKey,
			"error", err,
		)
		return nil, ErrAttachmentGone
	}

	// Calculate SHA-256 checksum and verify against stored value
	if !VerifyChecksum(fileContent, attachment.Checksum) {
		// Log critical error on checksum mismatch (Requirement: 3.4)
		s.logger.Error("CRITICAL: Attachment checksum mismatch detected",
			"attachment_id", aID,
			"email_id", eID,
			"storage_key", attachment.StorageKey,
			"expected_checksum", attachment.Checksum,
			"user_id", userID,
		)
		return nil, ErrChecksumMismatch
	}

	// Return attachment with verified content wrapped in a ReadCloser
	return &AttachmentDownload{
		Filename:    attachment.Filename,
		ContentType: attachment.ContentType,
		SizeBytes:   attachment.SizeBytes,
		Data:        io.NopCloser(bytes.NewReader(fileContent)),
		Checksum:    attachment.Checksum,
	}, nil
}

// GetAttachmentURL retrieves a pre-signed URL for downloading an attachment
// This is used for large files to avoid streaming through the server
// Requirements: 3.2 (Generate pre-signed URL with 15-minute expiration)
func (s *Service) GetAttachmentURL(ctx context.Context, userID uuid.UUID, emailID, attachmentID string) (*AttachmentURLResponse, error) {
	// Parse IDs
	eID, err := uuid.Parse(emailID)
	if err != nil {
		return nil, ErrEmailNotFound
	}
	aID, err := uuid.Parse(attachmentID)
	if err != nil {
		return nil, ErrAttachmentNotFound
	}

	// Check email ownership (Requirement: 3.3)
	owned, err := s.emailRepo.IsOwnedByUser(ctx, eID, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to check email ownership: %w", err)
	}
	if !owned {
		// Check if email exists at all
		_, err := s.emailRepo.GetByID(ctx, eID)
		if err != nil {
			if errors.Is(err, repository.ErrEmailNotFound) {
				return nil, ErrEmailNotFound
			}
			return nil, fmt.Errorf("failed to get email: %w", err)
		}
		return nil, ErrAccessDenied
	}

	// Get attachment metadata (Requirement: 3.2)
	attachment, err := s.attachmentRepo.GetByID(ctx, aID)
	if err != nil {
		return nil, fmt.Errorf("failed to get attachment: %w", err)
	}
	if attachment == nil {
		return nil, ErrAttachmentNotFound
	}

	// Verify attachment belongs to the email
	if attachment.EmailID != eID {
		return nil, ErrAttachmentNotFound
	}

	// Generate pre-signed URL (Requirement: 3.2)
	url, expiry, err := s.storageService.GetPresignedURL(ctx, attachment.StorageKey)
	if err != nil {
		s.logger.Error("Failed to generate pre-signed URL", "attachment_id", aID, "storage_key", attachment.StorageKey, "error", err)
		return nil, ErrAttachmentGone
	}

	return &AttachmentURLResponse{
		DownloadURL: url,
		ExpiresIn:   int(expiry.Seconds()),
		Filename:    attachment.Filename,
		ContentType: attachment.ContentType,
		SizeBytes:   attachment.SizeBytes,
		Checksum:    attachment.Checksum,
	}, nil
}

// IsLargeFile checks if an attachment is considered a large file
// Requirements: 3.2 (Return pre-signed URL instead of streaming for large files)
func (s *Service) IsLargeFile(sizeBytes int64) bool {
	if s.storageService == nil {
		return false
	}
	return s.storageService.IsLargeFile(sizeBytes)
}

// GetAttachmentMetadata retrieves attachment metadata without downloading the file
// This is useful for checking file size before deciding on download method
func (s *Service) GetAttachmentMetadata(ctx context.Context, userID uuid.UUID, emailID, attachmentID string) (*AttachmentURLResponse, error) {
	// Parse IDs
	eID, err := uuid.Parse(emailID)
	if err != nil {
		return nil, ErrEmailNotFound
	}
	aID, err := uuid.Parse(attachmentID)
	if err != nil {
		return nil, ErrAttachmentNotFound
	}

	// Check email ownership
	owned, err := s.emailRepo.IsOwnedByUser(ctx, eID, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to check email ownership: %w", err)
	}
	if !owned {
		_, err := s.emailRepo.GetByID(ctx, eID)
		if err != nil {
			if errors.Is(err, repository.ErrEmailNotFound) {
				return nil, ErrEmailNotFound
			}
			return nil, fmt.Errorf("failed to get email: %w", err)
		}
		return nil, ErrAccessDenied
	}

	// Get attachment metadata
	attachment, err := s.attachmentRepo.GetByID(ctx, aID)
	if err != nil {
		return nil, fmt.Errorf("failed to get attachment: %w", err)
	}
	if attachment == nil {
		return nil, ErrAttachmentNotFound
	}

	// Verify attachment belongs to the email
	if attachment.EmailID != eID {
		return nil, ErrAttachmentNotFound
	}

	return &AttachmentURLResponse{
		Filename:    attachment.Filename,
		ContentType: attachment.ContentType,
		SizeBytes:   attachment.SizeBytes,
		Checksum:    attachment.Checksum,
	}, nil
}

// VerifyChecksum verifies the SHA-256 checksum of data against expected checksum
// Requirements: 3.6 (Verify checksum)
func VerifyChecksum(data []byte, expectedChecksum string) bool {
	hash := sha256.Sum256(data)
	actualChecksum := hex.EncodeToString(hash[:])
	return actualChecksum == expectedChecksum
}


// Delete deletes an email and its attachments
// Requirements: 4.1-4.5 (Delete email)
// Property 5: Authorization Enforcement (delete part)
// Property 11: Email Deletion
func (s *Service) Delete(ctx context.Context, userID uuid.UUID, emailID string) (*DeleteEmailResponse, error) {
	// Parse email ID
	id, err := uuid.Parse(emailID)
	if err != nil {
		return nil, ErrEmailNotFound
	}

	// Check ownership (Requirement: 4.3)
	owned, err := s.emailRepo.IsOwnedByUser(ctx, id, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to check email ownership: %w", err)
	}
	if !owned {
		// Check if email exists at all (Requirement: 4.4)
		_, err := s.emailRepo.GetByID(ctx, id)
		if err != nil {
			if errors.Is(err, repository.ErrEmailNotFound) {
				return nil, ErrEmailNotFound
			}
			return nil, fmt.Errorf("failed to get email: %w", err)
		}
		return nil, ErrAccessDenied
	}

	// Get email to retrieve alias_id before deletion (for event publishing)
	email, err := s.emailRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrEmailNotFound) {
			return nil, ErrEmailNotFound
		}
		return nil, fmt.Errorf("failed to get email: %w", err)
	}
	aliasID := email.AliasID

	// Get email size before deletion (Requirement: 4.5)
	emailSize, err := s.emailRepo.GetSizeByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrEmailNotFound) {
			return nil, ErrEmailNotFound
		}
		return nil, fmt.Errorf("failed to get email size: %w", err)
	}

	// Get attachment storage keys before deletion (Requirement: 4.2)
	storageKeys, err := s.attachmentRepo.GetStorageKeysByEmailID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get attachment storage keys: %w", err)
	}

	// Get attachment total size
	attachmentSize, err := s.attachmentRepo.GetTotalSizeByEmailID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get attachment size: %w", err)
	}

	// Delete attachments from storage (Requirement: 4.2)
	attachmentsDeleted := 0
	if len(storageKeys) > 0 && s.storageService != nil {
		deleted, _, err := s.storageService.DeleteByKeys(ctx, storageKeys)
		if err != nil {
			s.logger.Warn("Failed to delete attachments from storage", "email_id", id, "error", err)
			// Continue with deletion even if storage cleanup fails
		}
		attachmentsDeleted = deleted
	}

	// Delete email (cascade handles attachments in DB) (Requirement: 4.1)
	if err := s.emailRepo.Delete(ctx, id); err != nil {
		if errors.Is(err, repository.ErrEmailNotFound) {
			return nil, ErrEmailNotFound
		}
		return nil, fmt.Errorf("failed to delete email: %w", err)
	}

	totalSizeFreed := emailSize + attachmentSize

	s.logger.Info("Email deleted",
		"email_id", id,
		"user_id", userID,
		"attachments_deleted", attachmentsDeleted,
		"size_freed", totalSizeFreed,
	)

	// Publish email_deleted event
	// Requirements: 4.1, 4.2 - Real-time notification for email deletion
	if s.eventBus != nil {
		s.publishEmailDeletedEvent(userID.String(), emailID, aliasID.String(), time.Now().UTC())
	}

	return &DeleteEmailResponse{
		Message:             "Email deleted successfully",
		EmailID:             emailID,
		AttachmentsDeleted:  attachmentsDeleted,
		TotalSizeFreedBytes: totalSizeFreed,
	}, nil
}


// BulkDelete deletes multiple emails
// Requirements: 5.1, 5.3, 5.4, 5.5 (Bulk delete)
// Property 12: Bulk Operations
func (s *Service) BulkDelete(ctx context.Context, userID uuid.UUID, emailIDs []string) (*BulkOperationResponse, error) {
	// Check bulk limit (Requirement: 5.5)
	if len(emailIDs) > MaxBulkOperationItems {
		return nil, ErrBulkLimitExceeded
	}

	if len(emailIDs) == 0 {
		return &BulkOperationResponse{
			SuccessCount: 0,
			FailedCount:  0,
			FailedIDs:    []string{},
		}, nil
	}

	// Parse all email IDs
	parsedIDs := make([]uuid.UUID, 0, len(emailIDs))
	failedIDs := make([]string, 0)

	for _, idStr := range emailIDs {
		id, err := uuid.Parse(idStr)
		if err != nil {
			failedIDs = append(failedIDs, idStr)
			continue
		}
		parsedIDs = append(parsedIDs, id)
	}

	// Filter to only owned emails (Requirement: 5.3)
	ownedIDs, err := s.emailRepo.GetEmailIDsOwnedByUser(ctx, parsedIDs, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to filter owned emails: %w", err)
	}

	// Track which IDs were not owned
	ownedIDSet := make(map[uuid.UUID]bool)
	for _, id := range ownedIDs {
		ownedIDSet[id] = true
	}
	for _, id := range parsedIDs {
		if !ownedIDSet[id] {
			failedIDs = append(failedIDs, id.String())
		}
	}

	if len(ownedIDs) == 0 {
		return &BulkOperationResponse{
			SuccessCount: 0,
			FailedCount:  len(emailIDs),
			FailedIDs:    failedIDs,
		}, nil
	}

	// Get total size before deletion
	totalSize, err := s.emailRepo.GetTotalSizeByIDs(ctx, ownedIDs)
	if err != nil {
		s.logger.Warn("Failed to get total size for bulk delete", "error", err)
	}

	// Get all attachment storage keys
	var allStorageKeys []string
	for _, id := range ownedIDs {
		keys, err := s.attachmentRepo.GetStorageKeysByEmailID(ctx, id)
		if err != nil {
			s.logger.Warn("Failed to get storage keys for email", "email_id", id, "error", err)
			continue
		}
		allStorageKeys = append(allStorageKeys, keys...)
	}

	// Delete attachments from storage
	if len(allStorageKeys) > 0 && s.storageService != nil {
		_, _, err := s.storageService.DeleteByKeys(ctx, allStorageKeys)
		if err != nil {
			s.logger.Warn("Failed to delete attachments from storage", "error", err)
		}
	}

	// Delete emails (Requirement: 5.1)
	deletedCount, err := s.emailRepo.DeleteBatch(ctx, ownedIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to delete emails: %w", err)
	}

	s.logger.Info("Bulk delete completed",
		"user_id", userID,
		"requested", len(emailIDs),
		"deleted", deletedCount,
		"size_freed", totalSize,
	)

	return &BulkOperationResponse{
		SuccessCount: deletedCount,
		FailedCount:  len(emailIDs) - deletedCount,
		FailedIDs:    failedIDs,
	}, nil
}

// BulkMarkAsRead marks multiple emails as read
// Requirements: 5.2, 5.3, 5.4, 5.5 (Bulk mark as read)
// Property 12: Bulk Operations
func (s *Service) BulkMarkAsRead(ctx context.Context, userID uuid.UUID, emailIDs []string) (*BulkOperationResponse, error) {
	// Check bulk limit (Requirement: 5.5)
	if len(emailIDs) > MaxBulkOperationItems {
		return nil, ErrBulkLimitExceeded
	}

	if len(emailIDs) == 0 {
		return &BulkOperationResponse{
			SuccessCount: 0,
			FailedCount:  0,
			FailedIDs:    []string{},
		}, nil
	}

	// Parse all email IDs
	parsedIDs := make([]uuid.UUID, 0, len(emailIDs))
	failedIDs := make([]string, 0)

	for _, idStr := range emailIDs {
		id, err := uuid.Parse(idStr)
		if err != nil {
			failedIDs = append(failedIDs, idStr)
			continue
		}
		parsedIDs = append(parsedIDs, id)
	}

	// Filter to only owned emails (Requirement: 5.3)
	ownedIDs, err := s.emailRepo.GetEmailIDsOwnedByUser(ctx, parsedIDs, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to filter owned emails: %w", err)
	}

	// Track which IDs were not owned
	ownedIDSet := make(map[uuid.UUID]bool)
	for _, id := range ownedIDs {
		ownedIDSet[id] = true
	}
	for _, id := range parsedIDs {
		if !ownedIDSet[id] {
			failedIDs = append(failedIDs, id.String())
		}
	}

	if len(ownedIDs) == 0 {
		return &BulkOperationResponse{
			SuccessCount: 0,
			FailedCount:  len(emailIDs),
			FailedIDs:    failedIDs,
		}, nil
	}

	// Mark emails as read (Requirement: 5.2)
	updatedCount, err := s.emailRepo.MarkAsReadBatch(ctx, ownedIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to mark emails as read: %w", err)
	}

	s.logger.Info("Bulk mark as read completed",
		"user_id", userID,
		"requested", len(emailIDs),
		"updated", updatedCount,
	)

	return &BulkOperationResponse{
		SuccessCount: updatedCount,
		FailedCount:  len(emailIDs) - updatedCount,
		FailedIDs:    failedIDs,
	}, nil
}


// GetStats retrieves inbox statistics for a user
// Requirements: 6.1-6.5 (Email statistics)
// Property 13: Statistics Accuracy
func (s *Service) GetStats(ctx context.Context, userID uuid.UUID) (*InboxStatsResponse, error) {
	stats, err := s.emailRepo.GetStats(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get inbox stats: %w", err)
	}

	// Convert to response format
	emailsPerAlias := make([]AliasEmailCount, len(stats.EmailsPerAlias))
	for i, a := range stats.EmailsPerAlias {
		emailsPerAlias[i] = AliasEmailCount{
			AliasID:    a.AliasID.String(),
			AliasEmail: a.AliasEmail,
			Count:      a.Count,
		}
	}

	return &InboxStatsResponse{
		TotalEmails:     stats.TotalEmails,
		UnreadEmails:    stats.UnreadEmails,
		TotalSizeBytes:  stats.TotalSizeBytes,
		EmailsToday:     stats.EmailsToday,
		EmailsThisWeek:  stats.EmailsThisWeek,
		EmailsThisMonth: stats.EmailsThisMonth,
		EmailsPerAlias:  emailsPerAlias,
	}, nil
}

// GetEmailIDsOwnedByUser is a helper method exposed for testing
// It filters email IDs to only those owned by the user
func (s *Service) GetEmailIDsOwnedByUser(ctx context.Context, emailIDs []uuid.UUID, userID uuid.UUID) ([]uuid.UUID, error) {
	return s.emailRepo.GetEmailIDsOwnedByUser(ctx, emailIDs, userID)
}

// IsOwnedByUser checks if an email belongs to a user
func (s *Service) IsOwnedByUser(ctx context.Context, emailID, userID uuid.UUID) (bool, error) {
	return s.emailRepo.IsOwnedByUser(ctx, emailID, userID)
}


// publishEmailDeletedEvent publishes an email_deleted event to the event bus
// Requirements: 4.1, 4.2 - Real-time notification for email deletion
func (s *Service) publishEmailDeletedEvent(userID, emailID, aliasID string, deletedAt time.Time) {
	eventData := events.EmailDeletedEvent{
		ID:        emailID,
		AliasID:   aliasID,
		DeletedAt: deletedAt,
	}

	data, err := json.Marshal(eventData)
	if err != nil {
		s.logger.Warn("Failed to marshal email_deleted event", "error", err)
		return
	}

	event := events.Event{
		ID:        uuid.New().String(),
		Type:      events.EventTypeEmailDeleted,
		UserID:    userID,
		Data:      data,
		Timestamp: time.Now().UTC(),
	}

	if err := s.eventBus.Publish(event); err != nil {
		s.logger.Warn("Failed to publish email_deleted event", "email_id", emailID, "error", err)
	}
}
