// Package ssl provides SSL certificate management functionality
// Requirements: 3.7, 8.6 - SSL API endpoints for certificate management
package ssl

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	appctx "github.com/welldanyogia/persistent-temp-mail/backend/internal/context"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/domain"
)

// Error codes for SSL operations
const (
	CodeSSLProvisioningInProgress = "SSL_PROVISIONING_IN_PROGRESS"
	CodeSSLProvisioningFailed     = "SSL_PROVISIONING_FAILED"
	CodeSSLDomainNotVerified      = "SSL_DOMAIN_NOT_VERIFIED"
	CodeSSLRateLimited            = "SSL_RATE_LIMITED"
	CodeSSLValidationFailed       = "SSL_VALIDATION_FAILED"
	CodeSSLCertificateExpired     = "SSL_CERTIFICATE_EXPIRED"
	CodeSSLCertificateRevoked     = "SSL_CERTIFICATE_REVOKED"
	CodeSSLRenewalFailed          = "SSL_RENEWAL_FAILED"
	CodeSSLNotFound               = "SSL_NOT_FOUND"
	CodeValidationError           = "VALIDATION_ERROR"
	CodeAccessDenied              = "RESOURCE_ACCESS_DENIED"
	CodeInternalError             = "INTERNAL_ERROR"
	CodeAuthTokenInvalid          = "AUTH_TOKEN_INVALID"
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
	Code    string `json:"code"`
	Message string `json:"message"`
}


// SSLStatusResponse represents the SSL status response
type SSLStatusResponse struct {
	DomainID     string     `json:"domain_id"`
	DomainName   string     `json:"domain_name"`
	Status       string     `json:"status"`
	Issuer       string     `json:"issuer,omitempty"`
	IssuedAt     *time.Time `json:"issued_at,omitempty"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	DaysUntilExp int        `json:"days_until_exp,omitempty"`
}

// ProvisionSSLResponse represents the SSL provisioning response
type ProvisionSSLResponse struct {
	Message     string             `json:"message"`
	Status      string             `json:"status"`
	Certificate *SSLStatusResponse `json:"certificate,omitempty"`
}

// RenewSSLResponse represents the SSL renewal response
type RenewSSLResponse struct {
	Message     string             `json:"message"`
	Certificate *SSLStatusResponse `json:"certificate,omitempty"`
}

// HealthCheckResponse represents the SSL health check response
type HealthCheckResponse struct {
	Status           string               `json:"status"`
	ActiveCerts      int                  `json:"active_certificates"`
	ExpiringSoon     int                  `json:"expiring_soon"`
	ExpiringWithin7d []*SSLStatusResponse `json:"expiring_within_7d,omitempty"`
}

// DomainRepository defines the interface for domain data access
type DomainRepository interface {
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Domain, error)
}

// SSLHandler handles HTTP requests for SSL certificate management endpoints
// Requirements: 3.7, 8.6 - SSL API endpoints
type SSLHandler struct {
	sslService SSLService
	domainRepo DomainRepository
	logger     *slog.Logger
}

// NewSSLHandler creates a new SSLHandler instance
func NewSSLHandler(sslService SSLService, domainRepo DomainRepository, logger *slog.Logger) *SSLHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &SSLHandler{
		sslService: sslService,
		domainRepo: domainRepo,
		logger:     logger,
	}
}


// GetSSLStatus handles GET /api/v1/domains/:id/ssl
// Requirements: 3.7 - Support manual renewal trigger via API
func (h *SSLHandler) GetSSLStatus(w http.ResponseWriter, r *http.Request) {
	userIDStr, ok := appctx.ExtractUserID(r.Context())
	if !ok {
		h.writeError(w, http.StatusUnauthorized, CodeAuthTokenInvalid, "Invalid or expired token")
		return
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		h.writeError(w, http.StatusUnauthorized, CodeAuthTokenInvalid, "Invalid user ID")
		return
	}

	domainID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.writeError(w, http.StatusBadRequest, CodeValidationError, "Invalid domain ID")
		return
	}

	// Verify domain ownership
	d, err := h.domainRepo.GetByID(r.Context(), domainID)
	if err != nil {
		if errors.Is(err, domain.ErrDomainNotFound) {
			h.writeError(w, http.StatusNotFound, CodeSSLNotFound, "Domain not found")
			return
		}
		h.logger.Error("Failed to get domain", "error", err, "domain_id", domainID)
		h.writeError(w, http.StatusInternalServerError, CodeInternalError, "Failed to get domain")
		return
	}

	if d.UserID != userID {
		h.writeError(w, http.StatusForbidden, CodeAccessDenied, "You don't have access to this domain")
		return
	}

	// Get SSL certificate info
	info, err := h.sslService.GetCertificateInfo(r.Context(), domainID.String())
	if err != nil {
		if errors.Is(err, ErrCertificateNotFound) {
			h.writeError(w, http.StatusNotFound, CodeSSLNotFound, "No SSL certificate found for this domain")
			return
		}
		h.logger.Error("Failed to get SSL info", "error", err, "domain_id", domainID)
		h.writeError(w, http.StatusInternalServerError, CodeInternalError, "Failed to get SSL status")
		return
	}

	response := h.certificateInfoToResponse(info)
	h.writeSuccess(w, http.StatusOK, response)
}


// ProvisionSSL handles POST /api/v1/domains/:id/ssl/provision
// Requirements: 3.7 - Support manual renewal trigger via API
func (h *SSLHandler) ProvisionSSL(w http.ResponseWriter, r *http.Request) {
	userIDStr, ok := appctx.ExtractUserID(r.Context())
	if !ok {
		h.writeError(w, http.StatusUnauthorized, CodeAuthTokenInvalid, "Invalid or expired token")
		return
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		h.writeError(w, http.StatusUnauthorized, CodeAuthTokenInvalid, "Invalid user ID")
		return
	}

	domainID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.writeError(w, http.StatusBadRequest, CodeValidationError, "Invalid domain ID")
		return
	}

	// Verify domain ownership and verification status
	d, err := h.domainRepo.GetByID(r.Context(), domainID)
	if err != nil {
		if errors.Is(err, domain.ErrDomainNotFound) {
			h.writeError(w, http.StatusNotFound, CodeSSLNotFound, "Domain not found")
			return
		}
		h.logger.Error("Failed to get domain", "error", err, "domain_id", domainID)
		h.writeError(w, http.StatusInternalServerError, CodeInternalError, "Failed to get domain")
		return
	}

	if d.UserID != userID {
		h.writeError(w, http.StatusForbidden, CodeAccessDenied, "You don't have access to this domain")
		return
	}

	// Check if domain is verified
	if !d.IsVerified {
		h.writeError(w, http.StatusBadRequest, CodeSSLDomainNotVerified, "Domain must be verified before SSL provisioning")
		return
	}

	// Start provisioning asynchronously
	go func() {
		ctx := context.Background()
		result, err := h.sslService.ProvisionCertificate(ctx, domainID.String(), d.DomainName)
		if err != nil {
			h.logger.Error("SSL provisioning failed", "error", err, "domain_id", domainID, "domain_name", d.DomainName)
			return
		}
		if !result.Success {
			h.logger.Error("SSL provisioning failed", "error", result.Error, "domain_id", domainID, "domain_name", d.DomainName)
			return
		}
		h.logger.Info("SSL provisioning completed", "domain_id", domainID, "domain_name", d.DomainName)
	}()

	response := ProvisionSSLResponse{
		Message: "SSL provisioning started",
		Status:  "provisioning",
	}

	h.writeSuccess(w, http.StatusAccepted, response)
}


// RenewSSL handles POST /api/v1/domains/:id/ssl/renew
// Requirements: 3.7 - Support manual renewal trigger via API
func (h *SSLHandler) RenewSSL(w http.ResponseWriter, r *http.Request) {
	userIDStr, ok := appctx.ExtractUserID(r.Context())
	if !ok {
		h.writeError(w, http.StatusUnauthorized, CodeAuthTokenInvalid, "Invalid or expired token")
		return
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		h.writeError(w, http.StatusUnauthorized, CodeAuthTokenInvalid, "Invalid user ID")
		return
	}

	domainID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.writeError(w, http.StatusBadRequest, CodeValidationError, "Invalid domain ID")
		return
	}

	// Verify domain ownership
	d, err := h.domainRepo.GetByID(r.Context(), domainID)
	if err != nil {
		if errors.Is(err, domain.ErrDomainNotFound) {
			h.writeError(w, http.StatusNotFound, CodeSSLNotFound, "Domain not found")
			return
		}
		h.logger.Error("Failed to get domain", "error", err, "domain_id", domainID)
		h.writeError(w, http.StatusInternalServerError, CodeInternalError, "Failed to get domain")
		return
	}

	if d.UserID != userID {
		h.writeError(w, http.StatusForbidden, CodeAccessDenied, "You don't have access to this domain")
		return
	}

	// Perform renewal
	result, err := h.sslService.RenewCertificate(r.Context(), domainID.String())
	if err != nil {
		h.handleSSLError(w, err)
		return
	}

	if !result.Success {
		h.writeError(w, http.StatusBadRequest, CodeSSLRenewalFailed, result.Error)
		return
	}

	response := RenewSSLResponse{
		Message:     "SSL certificate renewed successfully",
		Certificate: h.certificateInfoToResponse(result.Certificate),
	}

	h.writeSuccess(w, http.StatusOK, response)
}


// HealthCheck handles GET /api/v1/ssl/health
// Requirements: 8.6 - Provide API endpoint for SSL health check
func (h *SSLHandler) HealthCheck(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get expiring certificates (within 7 days)
	expiring, err := h.sslService.ListExpiringCertificates(ctx, 7)
	if err != nil {
		h.logger.Error("Failed to list expiring certificates", "error", err)
		// Return healthy status even if we can't get expiring certs
		response := HealthCheckResponse{
			Status:       "healthy",
			ActiveCerts:  0,
			ExpiringSoon: 0,
		}
		h.writeSuccess(w, http.StatusOK, response)
		return
	}

	// Convert to response format
	expiringResponses := make([]*SSLStatusResponse, 0, len(expiring))
	for _, cert := range expiring {
		expiringResponses = append(expiringResponses, h.certificateInfoToResponse(cert))
	}

	response := HealthCheckResponse{
		Status:           "healthy",
		ExpiringSoon:     len(expiring),
		ExpiringWithin7d: expiringResponses,
	}

	h.writeSuccess(w, http.StatusOK, response)
}

// certificateInfoToResponse converts CertificateInfo to SSLStatusResponse
func (h *SSLHandler) certificateInfoToResponse(info *CertificateInfo) *SSLStatusResponse {
	if info == nil {
		return nil
	}

	response := &SSLStatusResponse{
		DomainID:     info.DomainID,
		DomainName:   info.DomainName,
		Status:       info.Status,
		Issuer:       info.Issuer,
		DaysUntilExp: info.DaysUntilExp,
	}

	if !info.IssuedAt.IsZero() {
		response.IssuedAt = &info.IssuedAt
	}
	if !info.ExpiresAt.IsZero() {
		response.ExpiresAt = &info.ExpiresAt
	}

	return response
}


// handleSSLError maps SSL errors to HTTP responses
func (h *SSLHandler) handleSSLError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrProvisioningInProgress):
		h.writeError(w, http.StatusAccepted, CodeSSLProvisioningInProgress, "Certificate is being provisioned")
	case errors.Is(err, ErrProvisioningFailed):
		h.writeError(w, http.StatusInternalServerError, CodeSSLProvisioningFailed, "Certificate provisioning failed")
	case errors.Is(err, ErrDomainNotVerified):
		h.writeError(w, http.StatusBadRequest, CodeSSLDomainNotVerified, "Domain must be verified first")
	case errors.Is(err, ErrRateLimited):
		h.writeError(w, http.StatusTooManyRequests, CodeSSLRateLimited, "Let's Encrypt rate limit reached")
	case errors.Is(err, ErrCertificateExpired):
		h.writeError(w, http.StatusGone, CodeSSLCertificateExpired, "Certificate has expired")
	case errors.Is(err, ErrCertificateRevoked):
		h.writeError(w, http.StatusGone, CodeSSLCertificateRevoked, "Certificate was revoked")
	case errors.Is(err, ErrRenewalFailed):
		h.writeError(w, http.StatusInternalServerError, CodeSSLRenewalFailed, "Certificate renewal failed")
	case errors.Is(err, ErrCertificateNotFound):
		h.writeError(w, http.StatusNotFound, CodeSSLNotFound, "No SSL certificate found")
	case errors.Is(err, ErrInvalidDomain):
		h.writeError(w, http.StatusBadRequest, CodeValidationError, "Invalid domain")
	default:
		h.logger.Error("Unexpected SSL error", "error", err)
		h.writeError(w, http.StatusInternalServerError, CodeInternalError, "An unexpected error occurred")
	}
}

// writeSuccess writes a successful JSON response
func (h *SSLHandler) writeSuccess(w http.ResponseWriter, statusCode int, data interface{}) {
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
func (h *SSLHandler) writeError(w http.ResponseWriter, statusCode int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	response := APIResponse{
		Success: false,
		Error: &APIError{
			Code:    code,
			Message: message,
		},
		Timestamp: time.Now().UTC(),
	}

	json.NewEncoder(w).Encode(response)
}
