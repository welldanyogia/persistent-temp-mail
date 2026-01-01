package attachment

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"mime"
	"mime/multipart"
	"net/mail"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/parser"
)

// Handler handles attachment extraction, validation, and storage
// Requirements: 5.1-5.10
type Handler struct {
	s3Client *s3.Client
	bucket   string
}

// NewHandler creates a new attachment handler
func NewHandler(s3Client *s3.Client, bucket string) *Handler {
	return &Handler{
		s3Client: s3Client,
		bucket:   bucket,
	}
}

// ExtractAttachments extracts attachments from a raw email message
// Requirements: 5.1 - Extract attachments from multipart emails
// Property 10: Attachment Storage - extracts each attachment from multipart emails
func (h *Handler) ExtractAttachments(raw []byte) ([]*parser.Attachment, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("failed to parse email: %w", err)
	}

	contentType := msg.Header.Get("Content-Type")
	if contentType == "" {
		return nil, nil // No content type, no attachments
	}

	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, nil // Invalid content type, no attachments
	}

	// Only multipart emails can have attachments
	if !strings.HasPrefix(mediaType, "multipart/") {
		return nil, nil
	}

	boundary := params["boundary"]
	if boundary == "" {
		return nil, nil
	}

	return h.extractFromMultipart(msg.Body, boundary)
}

// extractFromMultipart extracts attachments from a multipart message
func (h *Handler) extractFromMultipart(body io.Reader, boundary string) ([]*parser.Attachment, error) {
	var attachments []*parser.Attachment

	reader := multipart.NewReader(body, boundary)

	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return attachments, fmt.Errorf("error reading multipart: %w", err)
		}

		// Check if this part is an attachment
		disposition := part.Header.Get("Content-Disposition")
		contentType := part.Header.Get("Content-Type")

		// Check for nested multipart
		if strings.HasPrefix(contentType, "multipart/") {
			mediaType, params, _ := mime.ParseMediaType(contentType)
			if strings.HasPrefix(mediaType, "multipart/") && params["boundary"] != "" {
				nestedAttachments, _ := h.extractFromMultipart(part, params["boundary"])
				attachments = append(attachments, nestedAttachments...)
				continue
			}
		}

		// Determine if this is an attachment
		isAttachment := false
		filename := ""

		if strings.HasPrefix(disposition, "attachment") {
			isAttachment = true
			_, params, _ := mime.ParseMediaType(disposition)
			filename = params["filename"]
		} else if strings.HasPrefix(disposition, "inline") {
			// Inline attachments with filename are also attachments
			_, params, _ := mime.ParseMediaType(disposition)
			if params["filename"] != "" {
				isAttachment = true
				filename = params["filename"]
			}
		}

		// Also check Content-Type for name parameter
		if filename == "" && contentType != "" {
			_, params, _ := mime.ParseMediaType(contentType)
			if params["name"] != "" {
				filename = params["name"]
				isAttachment = true
			}
		}

		if !isAttachment {
			continue
		}

		// Read attachment data
		data, err := io.ReadAll(part)
		if err != nil {
			continue
		}

		// Decode content if needed
		encoding := part.Header.Get("Content-Transfer-Encoding")
		decodedData, err := parser.DecodeContent(data, encoding)
		if err != nil {
			decodedData = data // Use original if decoding fails
		}

		// Determine content type
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		mediaType, _, _ := mime.ParseMediaType(contentType)
		if mediaType == "" {
			mediaType = "application/octet-stream"
		}

		// Decode filename if MIME encoded
		if filename != "" {
			decoder := new(mime.WordDecoder)
			if decoded, err := decoder.DecodeHeader(filename); err == nil {
				filename = decoded
			}
		}

		attachment := &parser.Attachment{
			Filename:    filename,
			ContentType: mediaType,
			Data:        decodedData,
			SizeBytes:   int64(len(decodedData)),
		}

		attachments = append(attachments, attachment)
	}

	return attachments, nil
}

// GenerateStorageKey generates a unique storage key for an attachment
// Requirements: 5.3 - Generate unique storage key for each attachment
// Property 10: Attachment Storage - generates unique storage key
func (h *Handler) GenerateStorageKey(emailID, filename string) string {
	// Format: attachments/{emailID}/{uuid}_{sanitized_filename}
	sanitizedFilename := h.SanitizeFilename(filename)
	uniqueID := uuid.New().String()

	if sanitizedFilename == "" {
		sanitizedFilename = "attachment"
	}

	return fmt.Sprintf("attachments/%s/%s_%s", emailID, uniqueID, sanitizedFilename)
}

// CalculateChecksum calculates SHA-256 checksum for data
// Requirements: 5.4 - Calculate SHA-256 checksum for integrity verification
// Property 10: Attachment Storage - calculates SHA-256 checksum
func (h *Handler) CalculateChecksum(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// SanitizeFilename removes path traversal characters from filename
// Requirements: 5.8 - Sanitize filename (remove path traversal characters)
// Property 12: Attachment Security - sanitizes path traversal characters
func (h *Handler) SanitizeFilename(filename string) string {
	if filename == "" {
		return ""
	}

	// Remove path traversal characters
	for _, char := range PathTraversalChars {
		filename = strings.ReplaceAll(filename, char, "")
	}

	// Get only the base filename (remove any remaining path)
	filename = filepath.Base(filename)

	// Remove any null bytes
	filename = strings.ReplaceAll(filename, "\x00", "")

	// Limit filename length
	if len(filename) > 255 {
		ext := filepath.Ext(filename)
		name := filename[:len(filename)-len(ext)]
		if len(name) > 255-len(ext) {
			name = name[:255-len(ext)]
		}
		filename = name + ext
	}

	return filename
}

// IsDangerousExtension checks if a file extension is dangerous
// Requirements: 5.9 - Block dangerous file extensions
// Property 12: Attachment Security - blocks dangerous extensions
func (h *Handler) IsDangerousExtension(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return DangerousExtensions[ext]
}

// ValidateContentType validates that the declared content-type matches the file extension
// Requirements: 2.2 - Validate content-type matches file extension
func (h *Handler) ValidateContentType(filename, contentType string) error {
	if filename == "" || contentType == "" {
		return nil // Skip validation if either is empty
	}

	ext := strings.ToLower(filepath.Ext(filename))
	if ext == "" {
		return nil // No extension to validate against
	}

	// Normalize content type (remove parameters like charset)
	normalizedType := contentType
	if idx := strings.Index(contentType, ";"); idx != -1 {
		normalizedType = strings.TrimSpace(contentType[:idx])
	}
	normalizedType = strings.ToLower(normalizedType)

	// application/octet-stream is a generic type that accepts any extension
	if normalizedType == "application/octet-stream" {
		return nil
	}

	// Check if extension has expected content types
	expectedTypes, hasMapping := ExtensionContentTypeMap[ext]
	if !hasMapping {
		// Unknown extension, allow any content type
		return nil
	}

	// Check if declared content type matches any expected type
	for _, expected := range expectedTypes {
		if normalizedType == expected {
			return nil
		}
	}

	return &ContentTypeMismatchError{
		Filename:      filename,
		DeclaredType:  contentType,
		ExpectedTypes: expectedTypes,
	}
}

// DetectExecutableMagicBytes checks if the data contains executable magic bytes
// Requirements: 2.3 - Scan file magic bytes to detect disguised executables
func (h *Handler) DetectExecutableMagicBytes(data []byte) *MagicSignature {
	if len(data) == 0 {
		return nil
	}

	for _, sig := range ExecutableMagicSignatures {
		// Check if we have enough data to compare
		if len(data) < sig.Offset+len(sig.Signature) {
			continue
		}

		// Compare bytes at the specified offset
		match := true
		for i, b := range sig.Signature {
			if data[sig.Offset+i] != b {
				match = false
				break
			}
		}

		if match {
			return &sig
		}
	}

	return nil
}

// IsDisguisedExecutable checks if a file is a disguised executable
// Requirements: 2.3 - Block disguised executables
func (h *Handler) IsDisguisedExecutable(filename string, data []byte) (*DisguisedExecutableError, bool) {
	// Skip check for files with known executable extensions (already blocked by IsDangerousExtension)
	ext := strings.ToLower(filepath.Ext(filename))
	knownExecutableExts := map[string]bool{
		".exe": true, ".dll": true, ".sys": true, ".drv": true,
		".so": true, ".dylib": true,
		".class": true, ".jar": true,
		".com": true, ".bat": true, ".cmd": true,
		".vbs": true, ".vbe": true, ".js": true, ".jse": true,
		".wsf": true, ".wsh": true,
		".ps1": true, ".psm1": true, ".psd1": true,
		".msi": true, ".msp": true, ".mst": true,
		".scr": true, ".pif": true,
	}

	// If it's a known executable extension, it's not "disguised"
	if knownExecutableExts[ext] {
		return nil, false
	}

	// Check for executable magic bytes
	detected := h.DetectExecutableMagicBytes(data)
	if detected != nil {
		return &DisguisedExecutableError{
			Filename:     filename,
			DetectedType: detected.Name,
			Description:  detected.Description,
		}, true
	}

	return nil, false
}

// ValidateAttachment validates an attachment against size and security rules
// Requirements: 5.6, 5.8, 5.9, 2.2, 2.3
func (h *Handler) ValidateAttachment(attachment *parser.Attachment) error {
	// Check individual size limit (Requirement 5.6)
	if attachment.SizeBytes > MaxAttachmentSize {
		return &AttachmentValidationError{
			Filename: attachment.Filename,
			Reason:   fmt.Sprintf("attachment exceeds maximum size of %d bytes", MaxAttachmentSize),
		}
	}

	// Check for dangerous extension (Requirement 5.9)
	if h.IsDangerousExtension(attachment.Filename) {
		return &AttachmentValidationError{
			Filename: attachment.Filename,
			Reason:   "dangerous file extension blocked",
		}
	}

	// Validate content-type matches extension (Requirement 2.2)
	if err := h.ValidateContentType(attachment.Filename, attachment.ContentType); err != nil {
		if mismatchErr, ok := err.(*ContentTypeMismatchError); ok {
			return &AttachmentValidationError{
				Filename: attachment.Filename,
				Reason:   fmt.Sprintf("content-type mismatch: declared %s, expected one of %v", mismatchErr.DeclaredType, mismatchErr.ExpectedTypes),
			}
		}
		return &AttachmentValidationError{
			Filename: attachment.Filename,
			Reason:   err.Error(),
		}
	}

	// Check for disguised executables via magic bytes (Requirement 2.3)
	if len(attachment.Data) > 0 {
		if disguisedErr, isDisguised := h.IsDisguisedExecutable(attachment.Filename, attachment.Data); isDisguised {
			return &AttachmentValidationError{
				Filename: attachment.Filename,
				Reason:   fmt.Sprintf("disguised executable detected: %s (%s)", disguisedErr.DetectedType, disguisedErr.Description),
			}
		}
	}

	return nil
}

// ValidateTotalSize validates total attachment size
// Requirements: 5.7 - Limit total attachments to 25 MB per email
// Property 11: Attachment Size Limits - validates total size
func (h *Handler) ValidateTotalSize(attachments []*parser.Attachment) error {
	var totalSize int64
	for _, att := range attachments {
		totalSize += att.SizeBytes
	}

	if totalSize > MaxTotalAttachmentSize {
		return &AttachmentValidationError{
			Filename: "",
			Reason:   fmt.Sprintf("total attachment size exceeds maximum of %d bytes", MaxTotalAttachmentSize),
		}
	}

	return nil
}


// Process processes attachments: validates, stores to S3, and returns processed attachments
// Requirements: 5.1-5.10, 1.9, 1.10
// Property 10: Attachment Storage - stores in S3 with unique key, calculates checksum, records metadata
func (h *Handler) Process(ctx context.Context, emailID string, attachments []*parser.Attachment) ([]*ProcessedAttachment, []AttachmentValidationError) {
	var processed []*ProcessedAttachment
	var errors []AttachmentValidationError

	// Validate total size first (Requirement 5.7)
	if err := h.ValidateTotalSize(attachments); err != nil {
		if validErr, ok := err.(*AttachmentValidationError); ok {
			errors = append(errors, *validErr)
		}
		return nil, errors
	}

	for _, att := range attachments {
		// Validate individual attachment
		if err := h.ValidateAttachment(att); err != nil {
			if validErr, ok := err.(*AttachmentValidationError); ok {
				// Log blocked attachment (Requirement 5.10)
				h.logBlockedAttachment(att.Filename, validErr.Reason)
				errors = append(errors, *validErr)
			}
			continue
		}

		// Sanitize filename (Requirement 5.8)
		sanitizedFilename := h.SanitizeFilename(att.Filename)
		if sanitizedFilename == "" {
			sanitizedFilename = "attachment"
		}

		// Generate storage key (Requirement 5.3)
		storageKey := h.GenerateStorageKey(emailID, sanitizedFilename)

		// Calculate checksum (Requirement 5.4)
		checksum := h.CalculateChecksum(att.Data)

		// Store to S3 with retry (Requirements 5.2, 1.9)
		storageURL, uploadErr := h.storeToS3WithRetry(ctx, storageKey, att.Data, att.ContentType, att.Filename)
		if uploadErr != nil {
			// Log failed attachment (Requirement 1.10)
			h.logFailedAttachment(att.Filename, uploadErr)
			errors = append(errors, AttachmentValidationError{
				Filename: att.Filename,
				Reason:   fmt.Sprintf("failed to store attachment after %d attempts: %v", uploadErr.Attempts, uploadErr.LastError),
			})
			continue
		}

		processedAtt := &ProcessedAttachment{
			ID:          uuid.New().String(),
			EmailID:     emailID,
			Filename:    sanitizedFilename,
			ContentType: att.ContentType,
			SizeBytes:   att.SizeBytes,
			StorageKey:  storageKey,
			StorageURL:  storageURL,
			Checksum:    checksum,
			Status:      AttachmentStatusActive,
			CreatedAt:   time.Now().UTC(),
		}

		processed = append(processed, processedAtt)
	}

	return processed, errors
}

// storeToS3 stores attachment data to S3/MinIO
// Requirements: 5.2 - Store attachments in S3-compatible storage
func (h *Handler) storeToS3(ctx context.Context, key string, data []byte, contentType string) (string, error) {
	if h.s3Client == nil {
		return "", fmt.Errorf("S3 client not initialized")
	}

	_, err := h.s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(h.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return "", fmt.Errorf("failed to upload to S3: %w", err)
	}

	// Generate storage URL
	storageURL := fmt.Sprintf("s3://%s/%s", h.bucket, key)

	return storageURL, nil
}

// storeToS3WithRetry stores attachment data to S3/MinIO with exponential backoff retry
// Requirements: 1.9 - Retry up to 3 times with exponential backoff
func (h *Handler) storeToS3WithRetry(ctx context.Context, key string, data []byte, contentType string, filename string) (string, *UploadError) {
	if h.s3Client == nil {
		return "", &UploadError{
			Filename:    filename,
			Attempts:    0,
			LastError:   errors.New("S3 client not initialized"),
			IsPermanent: true,
		}
	}

	var lastErr error
	delay := time.Duration(InitialRetryDelay) * time.Millisecond

	for attempt := 1; attempt <= MaxUploadRetries; attempt++ {
		// Attempt upload
		_, err := h.s3Client.PutObject(ctx, &s3.PutObjectInput{
			Bucket:      aws.String(h.bucket),
			Key:         aws.String(key),
			Body:        bytes.NewReader(data),
			ContentType: aws.String(contentType),
		})

		if err == nil {
			// Success
			storageURL := fmt.Sprintf("s3://%s/%s", h.bucket, key)
			if attempt > 1 {
				log.Printf("Upload succeeded for %s on attempt %d", filename, attempt)
			}
			return storageURL, nil
		}

		lastErr = err
		log.Printf("Upload attempt %d/%d failed for %s: %v", attempt, MaxUploadRetries, filename, err)

		// Check if context is cancelled
		if ctx.Err() != nil {
			return "", &UploadError{
				Filename:    filename,
				Attempts:    attempt,
				LastError:   ctx.Err(),
				IsPermanent: true,
			}
		}

		// Don't sleep after the last attempt
		if attempt < MaxUploadRetries {
			// Add jitter to prevent thundering herd
			jitter := time.Duration(rand.Int63n(int64(delay / 4)))
			sleepDuration := delay + jitter

			// Cap the delay at MaxRetryDelay
			if sleepDuration > time.Duration(MaxRetryDelay)*time.Millisecond {
				sleepDuration = time.Duration(MaxRetryDelay) * time.Millisecond
			}

			select {
			case <-ctx.Done():
				return "", &UploadError{
					Filename:    filename,
					Attempts:    attempt,
					LastError:   ctx.Err(),
					IsPermanent: true,
				}
			case <-time.After(sleepDuration):
				// Continue to next attempt
			}

			// Exponential backoff
			delay *= RetryBackoffMultiplier
		}
	}

	// All retries exhausted - permanent failure
	return "", &UploadError{
		Filename:    filename,
		Attempts:    MaxUploadRetries,
		LastError:   lastErr,
		IsPermanent: true,
	}
}

// logBlockedAttachment logs when an attachment is blocked
// Requirements: 5.10 - Log blocked attachments
func (h *Handler) logBlockedAttachment(filename, reason string) {
	log.Printf("Attachment blocked: filename=%s, reason=%s", filename, reason)
}

// logFailedAttachment logs when an attachment upload permanently fails
// Requirements: 1.10 - Log error details for failed attachments
func (h *Handler) logFailedAttachment(filename string, uploadErr *UploadError) {
	log.Printf("CRITICAL: Attachment upload permanently failed: filename=%s, attempts=%d, error=%v, is_permanent=%v",
		filename, uploadErr.Attempts, uploadErr.LastError, uploadErr.IsPermanent)
}

// ProcessResult represents the result of processing attachments with detailed tracking
// Requirements: 1.9, 1.10 - Track successful and failed attachments
type ProcessResult struct {
	Successful []*ProcessedAttachment
	Failed     []*FailedAttachment
	Errors     []AttachmentValidationError
}

// ProcessWithTracking processes attachments and returns detailed tracking of successes and failures
// Requirements: 1.9, 1.10 - Retry uploads and track failed attachments
func (h *Handler) ProcessWithTracking(ctx context.Context, emailID string, attachments []*parser.Attachment) *ProcessResult {
	result := &ProcessResult{
		Successful: make([]*ProcessedAttachment, 0),
		Failed:     make([]*FailedAttachment, 0),
		Errors:     make([]AttachmentValidationError, 0),
	}

	// Validate total size first (Requirement 5.7)
	if err := h.ValidateTotalSize(attachments); err != nil {
		if validErr, ok := err.(*AttachmentValidationError); ok {
			result.Errors = append(result.Errors, *validErr)
		}
		return result
	}

	for _, att := range attachments {
		// Validate individual attachment
		if err := h.ValidateAttachment(att); err != nil {
			if validErr, ok := err.(*AttachmentValidationError); ok {
				// Log blocked attachment (Requirement 5.10)
				h.logBlockedAttachment(att.Filename, validErr.Reason)
				result.Errors = append(result.Errors, *validErr)
			}
			continue
		}

		// Sanitize filename (Requirement 5.8)
		sanitizedFilename := h.SanitizeFilename(att.Filename)
		if sanitizedFilename == "" {
			sanitizedFilename = "attachment"
		}

		// Generate storage key (Requirement 5.3)
		storageKey := h.GenerateStorageKey(emailID, sanitizedFilename)

		// Calculate checksum (Requirement 5.4)
		checksum := h.CalculateChecksum(att.Data)

		// Store to S3 with retry (Requirements 5.2, 1.9)
		storageURL, uploadErr := h.storeToS3WithRetry(ctx, storageKey, att.Data, att.ContentType, att.Filename)
		if uploadErr != nil {
			// Log failed attachment (Requirement 1.10)
			h.logFailedAttachment(att.Filename, uploadErr)

			// Track failed attachment with status
			failedAtt := &FailedAttachment{
				Filename:    att.Filename,
				ContentType: att.ContentType,
				SizeBytes:   att.SizeBytes,
				Reason:      fmt.Sprintf("upload failed after %d attempts", uploadErr.Attempts),
				Attempts:    uploadErr.Attempts,
				LastError:   uploadErr.LastError.Error(),
			}
			result.Failed = append(result.Failed, failedAtt)

			result.Errors = append(result.Errors, AttachmentValidationError{
				Filename: att.Filename,
				Reason:   fmt.Sprintf("failed to store attachment after %d attempts: %v", uploadErr.Attempts, uploadErr.LastError),
			})
			continue
		}

		processedAtt := &ProcessedAttachment{
			ID:          uuid.New().String(),
			EmailID:     emailID,
			Filename:    sanitizedFilename,
			ContentType: att.ContentType,
			SizeBytes:   att.SizeBytes,
			StorageKey:  storageKey,
			StorageURL:  storageURL,
			Checksum:    checksum,
			Status:      AttachmentStatusActive,
			CreatedAt:   time.Now().UTC(),
		}

		result.Successful = append(result.Successful, processedAtt)
	}

	return result
}

// DeleteAttachment deletes an attachment from S3
func (h *Handler) DeleteAttachment(ctx context.Context, storageKey string) error {
	if h.s3Client == nil {
		return fmt.Errorf("S3 client not initialized")
	}

	_, err := h.s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(h.bucket),
		Key:    aws.String(storageKey),
	})
	if err != nil {
		return fmt.Errorf("failed to delete from S3: %w", err)
	}

	return nil
}

// GetAttachmentURL generates a presigned URL for downloading an attachment
func (h *Handler) GetAttachmentURL(ctx context.Context, storageKey string, expiry time.Duration) (string, error) {
	if h.s3Client == nil {
		return "", fmt.Errorf("S3 client not initialized")
	}

	presignClient := s3.NewPresignClient(h.s3Client)

	request, err := presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(h.bucket),
		Key:    aws.String(storageKey),
	}, s3.WithPresignExpires(expiry))
	if err != nil {
		return "", fmt.Errorf("failed to generate presigned URL: %w", err)
	}

	return request.URL, nil
}

// ProcessAndValidate processes attachments with full validation
// This is the main entry point for attachment processing
func (h *Handler) ProcessAndValidate(ctx context.Context, emailID string, raw []byte) ([]*ProcessedAttachment, []AttachmentValidationError) {
	// Extract attachments from raw email
	attachments, err := h.ExtractAttachments(raw)
	if err != nil {
		return nil, []AttachmentValidationError{{
			Filename: "",
			Reason:   fmt.Sprintf("failed to extract attachments: %v", err),
		}}
	}

	if len(attachments) == 0 {
		return nil, nil
	}

	// Process and store attachments
	return h.Process(ctx, emailID, attachments)
}
