// Package email provides email inbox management functionality
// Feature: email-inbox-api
// Requirements: All
package email

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	appctx "github.com/welldanyogia/persistent-temp-mail/backend/internal/context"
)

// APIResponse represents the standard API response format
type APIResponse struct {
	Success   bool        `json:"success"`
	Data      interface{} `json:"data,omitempty"`
	Error     *APIError   `json:"error,omitempty"`
	Timestamp time.Time   `json:"timestamp"`
}

// APIError represents the error detail in API response
type APIError struct {
	Code    string              `json:"code"`
	Message string              `json:"message"`
	Details map[string][]string `json:"details,omitempty"`
}

// Handler handles HTTP requests for email management endpoints
type Handler struct {
	emailService *Service
	logger       *slog.Logger
}

// NewHandler creates a new Handler instance
func NewHandler(emailService *Service, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{
		emailService: emailService,
		logger:       logger,
	}
}


// List handles GET /api/v1/emails
// Requirements: 1.1-1.9 (List emails with filters)
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	userIDStr, ok := appctx.ExtractUserID(r.Context())
	if !ok {
		h.writeError(w, http.StatusUnauthorized, "AUTH_TOKEN_INVALID", "Invalid or expired token", nil)
		return
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		h.writeError(w, http.StatusUnauthorized, "AUTH_TOKEN_INVALID", "Invalid user ID", nil)
		return
	}

	// Parse query parameters
	params := ListEmailParams{
		Page:  1,
		Limit: 20,
	}

	if pageStr := r.URL.Query().Get("page"); pageStr != "" {
		if page, err := strconv.Atoi(pageStr); err == nil && page > 0 {
			params.Page = page
		}
	}

	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil && limit > 0 {
			params.Limit = limit
			if params.Limit > 100 {
				params.Limit = 100
			}
		}
	}

	if aliasID := r.URL.Query().Get("alias_id"); aliasID != "" {
		params.AliasID = aliasID
	}

	if search := r.URL.Query().Get("search"); search != "" {
		if len(search) <= 100 {
			params.Search = search
		}
	}

	// Parse date filters
	if fromDateStr := r.URL.Query().Get("from_date"); fromDateStr != "" {
		if fromDate, err := time.Parse(time.RFC3339, fromDateStr); err == nil {
			params.FromDate = &fromDate
		}
	}

	if toDateStr := r.URL.Query().Get("to_date"); toDateStr != "" {
		if toDate, err := time.Parse(time.RFC3339, toDateStr); err == nil {
			params.ToDate = &toDate
		}
	}

	// Parse has_attachments filter
	if hasAttachmentsStr := r.URL.Query().Get("has_attachments"); hasAttachmentsStr != "" {
		hasAttachments := hasAttachmentsStr == "true"
		params.HasAttachments = &hasAttachments
	}

	// Parse is_read filter
	if isReadStr := r.URL.Query().Get("is_read"); isReadStr != "" {
		isRead := isReadStr == "true"
		params.IsRead = &isRead
	}

	// Parse sort and order
	if sort := r.URL.Query().Get("sort"); sort != "" {
		if sort == "received_at" || sort == "size" {
			params.Sort = sort
		}
	}

	if order := r.URL.Query().Get("order"); order != "" {
		if order == "asc" || order == "desc" {
			params.Order = order
		}
	}

	response, err := h.emailService.List(r.Context(), userID, params)
	if err != nil {
		h.logger.Error("Failed to list emails", "error", err, "user_id", userID)
		h.writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to list emails", nil)
		return
	}

	h.writeSuccess(w, http.StatusOK, response)
}


// GetByID handles GET /api/v1/emails/:id
// Requirements: 2.1-2.8 (Get email details)
func (h *Handler) GetByID(w http.ResponseWriter, r *http.Request) {
	userIDStr, ok := appctx.ExtractUserID(r.Context())
	if !ok {
		h.writeError(w, http.StatusUnauthorized, "AUTH_TOKEN_INVALID", "Invalid or expired token", nil)
		return
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		h.writeError(w, http.StatusUnauthorized, "AUTH_TOKEN_INVALID", "Invalid user ID", nil)
		return
	}

	emailID := chi.URLParam(r, "id")
	if emailID == "" {
		h.writeError(w, http.StatusBadRequest, CodeValidationError, "Email ID is required", nil)
		return
	}

	// Parse mark_as_read query parameter (default: true)
	markAsRead := true
	if markAsReadStr := r.URL.Query().Get("mark_as_read"); markAsReadStr != "" {
		markAsRead = markAsReadStr != "false"
	}

	email, err := h.emailService.GetByID(r.Context(), userID, emailID, markAsRead)
	if err != nil {
		h.handleEmailError(w, err)
		return
	}

	h.writeSuccess(w, http.StatusOK, map[string]interface{}{
		"email": email,
	})
}

// Delete handles DELETE /api/v1/emails/:id
// Requirements: 4.1-4.5 (Delete email)
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	userIDStr, ok := appctx.ExtractUserID(r.Context())
	if !ok {
		h.writeError(w, http.StatusUnauthorized, "AUTH_TOKEN_INVALID", "Invalid or expired token", nil)
		return
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		h.writeError(w, http.StatusUnauthorized, "AUTH_TOKEN_INVALID", "Invalid user ID", nil)
		return
	}

	emailID := chi.URLParam(r, "id")
	if emailID == "" {
		h.writeError(w, http.StatusBadRequest, CodeValidationError, "Email ID is required", nil)
		return
	}

	response, err := h.emailService.Delete(r.Context(), userID, emailID)
	if err != nil {
		h.handleEmailError(w, err)
		return
	}

	h.writeSuccess(w, http.StatusOK, map[string]interface{}{
		"message": response.Message,
		"deleted_resources": map[string]interface{}{
			"email_id":               response.EmailID,
			"attachments_deleted":    response.AttachmentsDeleted,
			"total_size_freed_bytes": response.TotalSizeFreedBytes,
		},
	})
}


// DownloadAttachment handles GET /api/v1/emails/:id/attachments/:attachmentId
// Requirements: 3.1-3.7 (Download attachment)
// Requirements: 6.6 (Log all download attempts with user_id, ip_address, and attachment_id)
// Query parameters:
//   - inline: "true" to display inline (Content-Disposition: inline)
//   - url_only: "true" to return pre-signed URL instead of streaming (for large files)
func (h *Handler) DownloadAttachment(w http.ResponseWriter, r *http.Request) {
	userIDStr, ok := appctx.ExtractUserID(r.Context())
	if !ok {
		h.writeError(w, http.StatusUnauthorized, "AUTH_TOKEN_INVALID", "Invalid or expired token", nil)
		return
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		h.writeError(w, http.StatusUnauthorized, "AUTH_TOKEN_INVALID", "Invalid user ID", nil)
		return
	}

	emailID := chi.URLParam(r, "id")
	if emailID == "" {
		h.writeError(w, http.StatusBadRequest, CodeValidationError, "Email ID is required", nil)
		return
	}

	attachmentID := chi.URLParam(r, "attachmentId")
	if attachmentID == "" {
		h.writeError(w, http.StatusBadRequest, CodeValidationError, "Attachment ID is required", nil)
		return
	}

	// Parse query parameters
	inline := r.URL.Query().Get("inline") == "true"
	urlOnly := r.URL.Query().Get("url_only") == "true"

	// If url_only is requested, return pre-signed URL (Requirement: 3.2)
	if urlOnly {
		urlResponse, err := h.emailService.GetAttachmentURL(r.Context(), userID, emailID, attachmentID)
		if err != nil {
			h.logDownloadAttempt(r, userIDStr, emailID, attachmentID, false, h.getErrorCode(err))
			h.handleEmailError(w, err)
			return
		}

		// Log successful download attempt (Requirement: 6.6)
		h.logDownloadAttempt(r, userIDStr, emailID, attachmentID, true, "")
		h.writeSuccess(w, http.StatusOK, urlResponse)
		return
	}

	// Check if file is large and should return URL instead of streaming
	metadata, err := h.emailService.GetAttachmentMetadata(r.Context(), userID, emailID, attachmentID)
	if err != nil {
		h.logDownloadAttempt(r, userIDStr, emailID, attachmentID, false, h.getErrorCode(err))
		h.handleEmailError(w, err)
		return
	}

	// For large files, return pre-signed URL instead of streaming (Requirement: 3.2)
	if h.emailService.IsLargeFile(metadata.SizeBytes) {
		urlResponse, err := h.emailService.GetAttachmentURL(r.Context(), userID, emailID, attachmentID)
		if err != nil {
			h.logDownloadAttempt(r, userIDStr, emailID, attachmentID, false, h.getErrorCode(err))
			h.handleEmailError(w, err)
			return
		}

		// Log successful download attempt (Requirement: 6.6)
		h.logDownloadAttempt(r, userIDStr, emailID, attachmentID, true, "")
		h.writeSuccess(w, http.StatusOK, urlResponse)
		return
	}

	// For smaller files, stream the content directly
	attachment, err := h.emailService.GetAttachment(r.Context(), userID, emailID, attachmentID)
	if err != nil {
		h.logDownloadAttempt(r, userIDStr, emailID, attachmentID, false, h.getErrorCode(err))
		h.handleEmailError(w, err)
		return
	}
	defer attachment.Data.Close()

	// Log successful download attempt (Requirement: 6.6)
	h.logDownloadAttempt(r, userIDStr, emailID, attachmentID, true, "")

	// Set response headers (Requirement: 3.4, 3.5)
	w.Header().Set("Content-Type", attachment.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(attachment.SizeBytes, 10))
	w.Header().Set("X-File-Size", strconv.FormatInt(attachment.SizeBytes, 10))
	w.Header().Set("X-File-Hash", "sha256:"+attachment.Checksum)

	// Set Content-Disposition based on inline parameter
	disposition := "attachment"
	if inline {
		disposition = "inline"
	}
	w.Header().Set("Content-Disposition", disposition+"; filename=\""+attachment.Filename+"\"")

	// Stream the file content
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, attachment.Data); err != nil {
		h.logger.Error("Failed to stream attachment", "error", err, "attachment_id", attachmentID)
		// Can't write error response at this point since headers are already sent
	}
}

// GetAttachmentURL handles GET /api/v1/emails/:id/attachments/:attachmentId/url
// Returns a pre-signed URL for downloading the attachment
// Requirements: 3.2 (Generate pre-signed URL with 15-minute expiration)
// Requirements: 6.6 (Log all download attempts with user_id, ip_address, and attachment_id)
func (h *Handler) GetAttachmentURL(w http.ResponseWriter, r *http.Request) {
	userIDStr, ok := appctx.ExtractUserID(r.Context())
	if !ok {
		h.writeError(w, http.StatusUnauthorized, "AUTH_TOKEN_INVALID", "Invalid or expired token", nil)
		return
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		h.writeError(w, http.StatusUnauthorized, "AUTH_TOKEN_INVALID", "Invalid user ID", nil)
		return
	}

	emailID := chi.URLParam(r, "id")
	if emailID == "" {
		h.writeError(w, http.StatusBadRequest, CodeValidationError, "Email ID is required", nil)
		return
	}

	attachmentID := chi.URLParam(r, "attachmentId")
	if attachmentID == "" {
		h.writeError(w, http.StatusBadRequest, CodeValidationError, "Attachment ID is required", nil)
		return
	}

	urlResponse, err := h.emailService.GetAttachmentURL(r.Context(), userID, emailID, attachmentID)
	if err != nil {
		h.logDownloadAttempt(r, userIDStr, emailID, attachmentID, false, h.getErrorCode(err))
		h.handleEmailError(w, err)
		return
	}

	// Log successful download attempt (Requirement: 6.6)
	h.logDownloadAttempt(r, userIDStr, emailID, attachmentID, true, "")
	h.writeSuccess(w, http.StatusOK, urlResponse)
}


// BulkDelete handles POST /api/v1/emails/bulk/delete
// Requirements: 5.1, 5.3, 5.4, 5.5 (Bulk delete)
func (h *Handler) BulkDelete(w http.ResponseWriter, r *http.Request) {
	userIDStr, ok := appctx.ExtractUserID(r.Context())
	if !ok {
		h.writeError(w, http.StatusUnauthorized, "AUTH_TOKEN_INVALID", "Invalid or expired token", nil)
		return
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		h.writeError(w, http.StatusUnauthorized, "AUTH_TOKEN_INVALID", "Invalid user ID", nil)
		return
	}

	var req BulkOperationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, CodeValidationError, "Invalid request body", nil)
		return
	}

	// Validate request
	if len(req.EmailIDs) == 0 {
		h.writeError(w, http.StatusBadRequest, CodeValidationError, "email_ids is required", nil)
		return
	}

	if len(req.EmailIDs) > MaxBulkOperationItems {
		h.writeError(w, http.StatusBadRequest, CodeBulkLimitExceeded, "Bulk operation limit exceeded (max 100 items)", nil)
		return
	}

	response, err := h.emailService.BulkDelete(r.Context(), userID, req.EmailIDs)
	if err != nil {
		if errors.Is(err, ErrBulkLimitExceeded) {
			h.writeError(w, http.StatusBadRequest, CodeBulkLimitExceeded, "Bulk operation limit exceeded (max 100 items)", nil)
			return
		}
		h.logger.Error("Failed to bulk delete emails", "error", err, "user_id", userID)
		h.writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to delete emails", nil)
		return
	}

	h.writeSuccess(w, http.StatusOK, response)
}

// BulkMarkAsRead handles POST /api/v1/emails/bulk/mark-read
// Requirements: 5.2, 5.3, 5.4, 5.5 (Bulk mark as read)
func (h *Handler) BulkMarkAsRead(w http.ResponseWriter, r *http.Request) {
	userIDStr, ok := appctx.ExtractUserID(r.Context())
	if !ok {
		h.writeError(w, http.StatusUnauthorized, "AUTH_TOKEN_INVALID", "Invalid or expired token", nil)
		return
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		h.writeError(w, http.StatusUnauthorized, "AUTH_TOKEN_INVALID", "Invalid user ID", nil)
		return
	}

	var req BulkOperationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, CodeValidationError, "Invalid request body", nil)
		return
	}

	// Validate request
	if len(req.EmailIDs) == 0 {
		h.writeError(w, http.StatusBadRequest, CodeValidationError, "email_ids is required", nil)
		return
	}

	if len(req.EmailIDs) > MaxBulkOperationItems {
		h.writeError(w, http.StatusBadRequest, CodeBulkLimitExceeded, "Bulk operation limit exceeded (max 100 items)", nil)
		return
	}

	response, err := h.emailService.BulkMarkAsRead(r.Context(), userID, req.EmailIDs)
	if err != nil {
		if errors.Is(err, ErrBulkLimitExceeded) {
			h.writeError(w, http.StatusBadRequest, CodeBulkLimitExceeded, "Bulk operation limit exceeded (max 100 items)", nil)
			return
		}
		h.logger.Error("Failed to bulk mark emails as read", "error", err, "user_id", userID)
		h.writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to mark emails as read", nil)
		return
	}

	h.writeSuccess(w, http.StatusOK, response)
}


// GetStats handles GET /api/v1/emails/stats
// Requirements: 6.1-6.5 (Email statistics)
func (h *Handler) GetStats(w http.ResponseWriter, r *http.Request) {
	userIDStr, ok := appctx.ExtractUserID(r.Context())
	if !ok {
		h.writeError(w, http.StatusUnauthorized, "AUTH_TOKEN_INVALID", "Invalid or expired token", nil)
		return
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		h.writeError(w, http.StatusUnauthorized, "AUTH_TOKEN_INVALID", "Invalid user ID", nil)
		return
	}

	stats, err := h.emailService.GetStats(r.Context(), userID)
	if err != nil {
		h.logger.Error("Failed to get inbox stats", "error", err, "user_id", userID)
		h.writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to get inbox statistics", nil)
		return
	}

	h.writeSuccess(w, http.StatusOK, stats)
}

// handleEmailError maps email service errors to HTTP responses
func (h *Handler) handleEmailError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrEmailNotFound):
		h.writeError(w, http.StatusNotFound, CodeEmailNotFound, "Email not found", nil)
	case errors.Is(err, ErrAttachmentNotFound):
		h.writeError(w, http.StatusNotFound, CodeAttachmentNotFound, "Attachment not found", nil)
	case errors.Is(err, ErrAccessDenied):
		h.writeError(w, http.StatusForbidden, CodeForbidden, "Access denied", nil)
	case errors.Is(err, ErrAttachmentGone):
		h.writeError(w, http.StatusGone, CodeAttachmentDeleted, "Attachment file missing from storage", nil)
	case errors.Is(err, ErrChecksumMismatch):
		h.writeError(w, http.StatusInternalServerError, CodeChecksumMismatch, "Attachment integrity check failed", nil)
	case errors.Is(err, ErrBulkLimitExceeded):
		h.writeError(w, http.StatusBadRequest, CodeBulkLimitExceeded, "Bulk operation limit exceeded (max 100 items)", nil)
	default:
		h.logger.Error("Unexpected email error", "error", err)
		h.writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "An unexpected error occurred", nil)
	}
}

// getErrorCode extracts the error code from an error for logging purposes
func (h *Handler) getErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrEmailNotFound):
		return CodeEmailNotFound
	case errors.Is(err, ErrAttachmentNotFound):
		return CodeAttachmentNotFound
	case errors.Is(err, ErrAccessDenied):
		return CodeForbidden
	case errors.Is(err, ErrAttachmentGone):
		return CodeAttachmentDeleted
	case errors.Is(err, ErrChecksumMismatch):
		return CodeChecksumMismatch
	case errors.Is(err, ErrBulkLimitExceeded):
		return CodeBulkLimitExceeded
	default:
		return "INTERNAL_ERROR"
	}
}

// writeSuccess writes a successful JSON response
func (h *Handler) writeSuccess(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	response := APIResponse{
		Success:   true,
		Data:      data,
		Timestamp: time.Now().UTC(),
	}

	json.NewEncoder(w).Encode(response)
}

// writeError writes an error JSON response
func (h *Handler) writeError(w http.ResponseWriter, statusCode int, code, message string, details map[string][]string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	response := APIResponse{
		Success: false,
		Error: &APIError{
			Code:    code,
			Message: message,
			Details: details,
		},
		Timestamp: time.Now().UTC(),
	}

	json.NewEncoder(w).Encode(response)
}

// logDownloadAttempt logs attachment download attempts for security auditing
// Requirements: 6.6 (Log all download attempts with user_id, ip_address, and attachment_id)
func (h *Handler) logDownloadAttempt(r *http.Request, userID, emailID, attachmentID string, success bool, errorCode string) {
	ipAddress := getClientIP(r)
	
	logAttrs := []any{
		"user_id", userID,
		"ip_address", ipAddress,
		"attachment_id", attachmentID,
		"email_id", emailID,
		"success", success,
		"method", r.Method,
		"path", r.URL.Path,
		"user_agent", r.UserAgent(),
		"timestamp", time.Now().UTC().Format(time.RFC3339),
	}
	
	if errorCode != "" {
		logAttrs = append(logAttrs, "error_code", errorCode)
	}
	
	if success {
		h.logger.Info("Attachment download attempt", logAttrs...)
	} else {
		h.logger.Warn("Attachment download attempt failed", logAttrs...)
	}
}

// getClientIP extracts the client IP address from the request
// Handles X-Forwarded-For and X-Real-IP headers for proxied requests
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header first (for proxied requests)
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		// X-Forwarded-For can contain multiple IPs, take the first one
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			ip := strings.TrimSpace(ips[0])
			if ip != "" {
				return ip
			}
		}
	}

	// Check X-Real-IP header
	xri := r.Header.Get("X-Real-IP")
	if xri != "" {
		return strings.TrimSpace(xri)
	}

	// Fall back to RemoteAddr
	return r.RemoteAddr
}
