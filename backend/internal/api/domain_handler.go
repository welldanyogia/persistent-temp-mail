package api

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
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/domain"
)

// Error codes for domain operations
const (
	CodeValidationError     = "VALIDATION_ERROR"
	CodeDomainExists        = "DOMAIN_EXISTS"
	CodeDomainNotFound      = "DOMAIN_NOT_FOUND"
	CodeAccessDenied        = "RESOURCE_ACCESS_DENIED"
	CodeDomainLimitReached  = "DOMAIN_LIMIT_REACHED"
	CodeVerificationFailed  = "VERIFICATION_FAILED"
	CodeInternalError       = "INTERNAL_ERROR"
	CodeAuthTokenInvalid    = "AUTH_TOKEN_INVALID"
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

// DomainHandler handles HTTP requests for domain management endpoints
type DomainHandler struct {
	domainService *domain.Service
	logger        *slog.Logger
}

// NewDomainHandler creates a new DomainHandler instance
func NewDomainHandler(domainService *domain.Service, logger *slog.Logger) *DomainHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &DomainHandler{
		domainService: domainService,
		logger:        logger,
	}
}


// ListDomains handles GET /api/v1/domains
// Requirements: FR-DOM-001
func (h *DomainHandler) ListDomains(w http.ResponseWriter, r *http.Request) {
	userIDStr, ok := appctx.ExtractUserID(r.Context())
	if !ok {
		h.writeError(w, http.StatusUnauthorized, CodeAuthTokenInvalid, "Invalid or expired token", nil)
		return
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		h.writeError(w, http.StatusUnauthorized, CodeAuthTokenInvalid, "Invalid user ID", nil)
		return
	}

	// Parse query parameters
	opts := domain.DefaultListOptions()
	
	if pageStr := r.URL.Query().Get("page"); pageStr != "" {
		if page, err := strconv.Atoi(pageStr); err == nil && page > 0 {
			opts.Page = page
		}
	}
	
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil && limit > 0 {
			opts.Limit = limit
			if opts.Limit > 100 {
				opts.Limit = 100
			}
		}
	}
	
	if status := r.URL.Query().Get("status"); status != "" {
		if status == "pending" || status == "verified" {
			opts.Status = status
		}
	}

	domains, totalCount, err := h.domainService.ListDomains(r.Context(), userID, opts)
	if err != nil {
		h.logger.Error("Failed to list domains", "error", err, "user_id", userID)
		h.writeError(w, http.StatusInternalServerError, CodeInternalError, "Failed to list domains", nil)
		return
	}

	// Convert to response DTOs
	domainResponses := make([]DomainResponse, 0, len(domains))
	for _, d := range domains {
		domainResponses = append(domainResponses, ToDomainResponse(&d, nil))
	}

	response := ListDomainsResponse{
		Domains: domainResponses,
		Pagination: PaginationInfo{
			CurrentPage: opts.Page,
			PerPage:     opts.Limit,
			TotalPages:  CalculateTotalPages(totalCount, opts.Limit),
			TotalCount:  totalCount,
		},
	}

	h.writeSuccess(w, http.StatusOK, response)
}

// CreateDomain handles POST /api/v1/domains
// Requirements: FR-DOM-002
func (h *DomainHandler) CreateDomain(w http.ResponseWriter, r *http.Request) {
	userIDStr, ok := appctx.ExtractUserID(r.Context())
	if !ok {
		h.writeError(w, http.StatusUnauthorized, CodeAuthTokenInvalid, "Invalid or expired token", nil)
		return
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		h.writeError(w, http.StatusUnauthorized, CodeAuthTokenInvalid, "Invalid user ID", nil)
		return
	}

	var req CreateDomainRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, CodeValidationError, "Invalid request body", nil)
		return
	}

	if req.DomainName == "" {
		h.writeError(w, http.StatusBadRequest, CodeValidationError, "domain_name is required", nil)
		return
	}

	d, instructions, err := h.domainService.CreateDomain(r.Context(), userID, req.DomainName)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response := map[string]interface{}{
		"domain": ToDomainResponse(d, instructions),
	}

	h.writeSuccess(w, http.StatusCreated, response)
}

// GetDomain handles GET /api/v1/domains/:id
// Requirements: FR-DOM-003
func (h *DomainHandler) GetDomain(w http.ResponseWriter, r *http.Request) {
	userIDStr, ok := appctx.ExtractUserID(r.Context())
	if !ok {
		h.writeError(w, http.StatusUnauthorized, CodeAuthTokenInvalid, "Invalid or expired token", nil)
		return
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		h.writeError(w, http.StatusUnauthorized, CodeAuthTokenInvalid, "Invalid user ID", nil)
		return
	}

	domainID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.writeError(w, http.StatusBadRequest, CodeValidationError, "Invalid domain ID", nil)
		return
	}

	d, instructions, err := h.domainService.GetDNSInstructions(r.Context(), userID, domainID)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response := map[string]interface{}{
		"domain": ToDomainResponse(d, instructions),
	}

	h.writeSuccess(w, http.StatusOK, response)
}

// DeleteDomain handles DELETE /api/v1/domains/:id
// Requirements: FR-DOM-004
func (h *DomainHandler) DeleteDomain(w http.ResponseWriter, r *http.Request) {
	userIDStr, ok := appctx.ExtractUserID(r.Context())
	if !ok {
		h.writeError(w, http.StatusUnauthorized, CodeAuthTokenInvalid, "Invalid or expired token", nil)
		return
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		h.writeError(w, http.StatusUnauthorized, CodeAuthTokenInvalid, "Invalid user ID", nil)
		return
	}

	domainID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.writeError(w, http.StatusBadRequest, CodeValidationError, "Invalid domain ID", nil)
		return
	}

	result, err := h.domainService.DeleteDomain(r.Context(), userID, domainID)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	h.writeSuccess(w, http.StatusOK, ToDeleteDomainResponse(result))
}

// VerifyDomain handles POST /api/v1/domains/:id/verify
// Requirements: FR-DOM-005
func (h *DomainHandler) VerifyDomain(w http.ResponseWriter, r *http.Request) {
	userIDStr, ok := appctx.ExtractUserID(r.Context())
	if !ok {
		h.writeError(w, http.StatusUnauthorized, CodeAuthTokenInvalid, "Invalid or expired token", nil)
		return
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		h.writeError(w, http.StatusUnauthorized, CodeAuthTokenInvalid, "Invalid user ID", nil)
		return
	}

	domainID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.writeError(w, http.StatusBadRequest, CodeValidationError, "Invalid domain ID", nil)
		return
	}

	d, dnsResult, err := h.domainService.VerifyDomain(r.Context(), userID, domainID)
	if err != nil {
		if errors.Is(err, domain.ErrVerificationFailed) {
			// Return verification details even on failure
			response := VerifyDomainResponse{
				Domain:              ToDomainResponse(d, nil),
				VerificationDetails: ToVerificationDetails(dnsResult, false),
			}
			h.writeErrorWithData(w, http.StatusBadRequest, CodeVerificationFailed, "DNS verification failed", response)
			return
		}
		h.handleDomainError(w, err)
		return
	}

	response := VerifyDomainResponse{
		Domain:              ToDomainResponse(d, nil),
		VerificationDetails: ToVerificationDetails(dnsResult, d.IsVerified),
	}

	h.writeSuccess(w, http.StatusOK, response)
}

// GetDNSStatus handles GET /api/v1/domains/:id/dns-status
// Requirements: FR-DOM-006
func (h *DomainHandler) GetDNSStatus(w http.ResponseWriter, r *http.Request) {
	userIDStr, ok := appctx.ExtractUserID(r.Context())
	if !ok {
		h.writeError(w, http.StatusUnauthorized, CodeAuthTokenInvalid, "Invalid or expired token", nil)
		return
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		h.writeError(w, http.StatusUnauthorized, CodeAuthTokenInvalid, "Invalid user ID", nil)
		return
	}

	domainID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.writeError(w, http.StatusBadRequest, CodeValidationError, "Invalid domain ID", nil)
		return
	}

	d, dnsResult, err := h.domainService.GetDNSStatus(r.Context(), userID, domainID)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response := DNSStatusResponse{
		Domain:    ToDomainResponse(d, nil),
		DNSStatus: ToDNSStatus(dnsResult),
	}

	h.writeSuccess(w, http.StatusOK, response)
}


// handleDomainError maps domain errors to HTTP responses
func (h *DomainHandler) handleDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrDomainNotFound):
		h.writeError(w, http.StatusNotFound, CodeDomainNotFound, "Domain not found", nil)
	case errors.Is(err, domain.ErrDomainExists):
		h.writeError(w, http.StatusConflict, CodeDomainExists, "Domain already registered", nil)
	case errors.Is(err, domain.ErrDomainLimitReached):
		h.writeError(w, http.StatusPaymentRequired, CodeDomainLimitReached, "Domain limit reached", nil)
	case errors.Is(err, domain.ErrAccessDenied):
		h.writeError(w, http.StatusForbidden, CodeAccessDenied, "Access denied", nil)
	case errors.Is(err, domain.ErrInvalidDomainName):
		h.writeError(w, http.StatusBadRequest, CodeValidationError, "Invalid domain name format", nil)
	case errors.Is(err, domain.ErrReservedDomain):
		h.writeError(w, http.StatusBadRequest, CodeValidationError, "Domain is reserved and cannot be registered", nil)
	case errors.Is(err, domain.ErrVerificationFailed):
		h.writeError(w, http.StatusBadRequest, CodeVerificationFailed, "DNS verification failed", nil)
	default:
		h.logger.Error("Unexpected domain error", "error", err)
		h.writeError(w, http.StatusInternalServerError, CodeInternalError, "An unexpected error occurred", nil)
	}
}

// writeSuccess writes a successful JSON response
func (h *DomainHandler) writeSuccess(w http.ResponseWriter, statusCode int, data interface{}) {
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
func (h *DomainHandler) writeError(w http.ResponseWriter, statusCode int, code, message string, details map[string][]string) {
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

// writeErrorWithData writes an error JSON response with additional data
func (h *DomainHandler) writeErrorWithData(w http.ResponseWriter, statusCode int, code, message string, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	response := struct {
		Success   bool        `json:"success"`
		Data      interface{} `json:"data,omitempty"`
		Error     *APIError   `json:"error,omitempty"`
		Timestamp time.Time   `json:"timestamp"`
	}{
		Success: false,
		Data:    data,
		Error: &APIError{
			Code:    code,
			Message: message,
		},
		Timestamp: time.Now().UTC(),
	}

	json.NewEncoder(w).Encode(response)
}
