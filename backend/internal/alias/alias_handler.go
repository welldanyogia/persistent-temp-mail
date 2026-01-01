package alias

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
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

// Handler handles HTTP requests for alias management endpoints
type Handler struct {
	aliasService *Service
	logger       *slog.Logger
}

// NewHandler creates a new Handler instance
func NewHandler(aliasService *Service, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{
		aliasService: aliasService,
		logger:       logger,
	}
}

// Create handles POST /api/v1/aliases
// Requirements: 1.1-1.9, 6.1-6.5, 7.1, 7.3
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
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

	var req CreateAliasRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, CodeValidationError, "Invalid request body", nil)
		return
	}

	// Validate required fields
	if req.LocalPart == "" {
		h.writeError(w, http.StatusBadRequest, CodeValidationError, "local_part is required", nil)
		return
	}
	if req.DomainID == "" {
		h.writeError(w, http.StatusBadRequest, CodeValidationError, "domain_id is required", nil)
		return
	}

	alias, validationErrors, err := h.aliasService.Create(r.Context(), userID, req)
	if err != nil {
		h.handleAliasError(w, err, validationErrors)
		return
	}

	h.writeSuccess(w, http.StatusCreated, map[string]interface{}{
		"alias": alias,
	})
}

// List handles GET /api/v1/aliases
// Requirements: 2.1-2.6
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
	params := ListAliasParams{
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

	if domainID := r.URL.Query().Get("domain_id"); domainID != "" {
		params.DomainID = domainID
	}

	if search := r.URL.Query().Get("search"); search != "" {
		if len(search) <= 64 {
			params.Search = search
		}
	}

	if sort := r.URL.Query().Get("sort"); sort != "" {
		if sort == "created_at" || sort == "email_count" {
			params.Sort = sort
		}
	}

	if order := r.URL.Query().Get("order"); order != "" {
		if order == "asc" || order == "desc" {
			params.Order = order
		}
	}

	response, err := h.aliasService.List(r.Context(), userID, params)
	if err != nil {
		h.logger.Error("Failed to list aliases", "error", err, "user_id", userID)
		h.writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to list aliases", nil)
		return
	}

	h.writeSuccess(w, http.StatusOK, response)
}


// GetByID handles GET /api/v1/aliases/:id
// Requirements: 3.1-3.4
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

	aliasID := chi.URLParam(r, "id")
	if aliasID == "" {
		h.writeError(w, http.StatusBadRequest, CodeValidationError, "Alias ID is required", nil)
		return
	}

	alias, err := h.aliasService.GetByID(r.Context(), userID, aliasID)
	if err != nil {
		h.handleAliasError(w, err, nil)
		return
	}

	h.writeSuccess(w, http.StatusOK, map[string]interface{}{
		"alias": alias,
	})
}

// Update handles PUT /api/v1/aliases/:id (also supports PATCH)
// Requirements: 4.1-4.5
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
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

	aliasID := chi.URLParam(r, "id")
	if aliasID == "" {
		h.writeError(w, http.StatusBadRequest, CodeValidationError, "Alias ID is required", nil)
		return
	}

	var req UpdateAliasRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, CodeValidationError, "Invalid request body", nil)
		return
	}

	// Validate description length if provided
	if req.Description != nil && len(*req.Description) > 500 {
		h.writeError(w, http.StatusBadRequest, CodeValidationError, "Description must be at most 500 characters", nil)
		return
	}

	alias, err := h.aliasService.Update(r.Context(), userID, aliasID, req)
	if err != nil {
		h.handleAliasError(w, err, nil)
		return
	}

	h.writeSuccess(w, http.StatusOK, map[string]interface{}{
		"alias": alias,
	})
}


// Delete handles DELETE /api/v1/aliases/:id
// Requirements: 5.1-5.5
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

	aliasID := chi.URLParam(r, "id")
	if aliasID == "" {
		h.writeError(w, http.StatusBadRequest, CodeValidationError, "Alias ID is required", nil)
		return
	}

	response, err := h.aliasService.Delete(r.Context(), userID, aliasID)
	if err != nil {
		h.handleAliasError(w, err, nil)
		return
	}

	h.writeSuccess(w, http.StatusOK, map[string]interface{}{
		"message":           response.Message,
		"deleted_resources": map[string]interface{}{
			"alias_id":               response.AliasID,
			"email_address":          response.EmailAddress,
			"emails_deleted":         response.EmailsDeleted,
			"attachments_deleted":    response.AttachmentsDeleted,
			"total_size_freed_bytes": response.TotalSizeFreedBytes,
		},
	})
}

// handleAliasError maps alias service errors to HTTP responses
func (h *Handler) handleAliasError(w http.ResponseWriter, err error, validationErrors []string) {
	switch {
	case errors.Is(err, ErrValidationFailed):
		details := make(map[string][]string)
		if len(validationErrors) > 0 {
			details["local_part"] = validationErrors
		}
		h.writeError(w, http.StatusBadRequest, CodeValidationError, "Request validation failed", details)
	case errors.Is(err, ErrAliasNotFound):
		h.writeError(w, http.StatusNotFound, CodeAliasNotFound, "Alias not found", nil)
	case errors.Is(err, ErrDomainNotFound):
		h.writeError(w, http.StatusNotFound, CodeDomainNotFound, "Domain not found", nil)
	case errors.Is(err, ErrAccessDenied):
		h.writeError(w, http.StatusForbidden, CodeForbidden, "Access denied", nil)
	case errors.Is(err, ErrDomainNotVerified):
		h.writeError(w, http.StatusForbidden, CodeDomainNotVerified, "Domain is not verified", nil)
	case errors.Is(err, ErrAliasExists):
		h.writeError(w, http.StatusConflict, CodeAliasExists, "Alias already exists", nil)
	case errors.Is(err, ErrAliasLimitReached):
		h.writeError(w, http.StatusPaymentRequired, CodeAliasLimitReached, "Alias limit reached", nil)
	default:
		h.logger.Error("Unexpected alias error", "error", err)
		h.writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "An unexpected error occurred", nil)
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
