package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

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
	Code    string                 `json:"code"`
	Message string                 `json:"message"`
	Details map[string][]string    `json:"details,omitempty"`
}

// AuthHandler handles HTTP requests for authentication endpoints
type AuthHandler struct {
	authService *AuthService
}

// NewAuthHandler creates a new AuthHandler instance
func NewAuthHandler(authService *AuthService) *AuthHandler {
	return &AuthHandler{
		authService: authService,
	}
}

// Register handles user registration
// POST /api/v1/auth/register
// Requirements: 1.1-1.7
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, CodeValidationError, "Invalid request body", nil)
		return
	}

	response, validationErrors, err := h.authService.Register(r.Context(), req)
	if err != nil {
		if errors.Is(err, ErrEmailExists) {
			h.writeError(w, http.StatusConflict, CodeEmailExists, "An account with this email already exists", nil)
			return
		}
		h.writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "An unexpected error occurred", nil)
		return
	}

	if len(validationErrors) > 0 {
		details := make(map[string][]string)
		for _, ve := range validationErrors {
			details[ve.Field] = append(details[ve.Field], ve.Message)
		}
		h.writeError(w, http.StatusBadRequest, CodeValidationError, "Request validation failed", details)
		return
	}

	h.writeSuccess(w, http.StatusCreated, response)
}


// Login handles user authentication
// POST /api/v1/auth/login
// Requirements: 2.1-2.5
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, CodeValidationError, "Invalid request body", nil)
		return
	}

	// Get IP address and user agent for session tracking
	ipAddress := getClientIP(r)
	userAgent := r.UserAgent()

	response, err := h.authService.Login(r.Context(), req, ipAddress, userAgent)
	if err != nil {
		if errors.Is(err, ErrInvalidCredentials) {
			h.writeError(w, http.StatusUnauthorized, CodeInvalidCredentials, "Invalid email or password", nil)
			return
		}
		if errors.Is(err, ErrTooManyAttempts) {
			details := map[string][]string{
				"retry_after": {"900"},
			}
			h.writeError(w, http.StatusTooManyRequests, CodeTooManyAttempts, "Too many failed login attempts. Please try again later.", details)
			return
		}
		h.writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "An unexpected error occurred", nil)
		return
	}

	h.writeSuccess(w, http.StatusOK, response)
}

// Logout handles user logout
// POST /api/v1/auth/logout
// Requirements: 4.1-4.3
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	var req LogoutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, CodeValidationError, "Invalid request body", nil)
		return
	}

	if req.RefreshToken == "" {
		h.writeError(w, http.StatusBadRequest, CodeValidationError, "refresh_token is required", nil)
		return
	}

	err := h.authService.Logout(r.Context(), req.RefreshToken)
	if err != nil {
		if errors.Is(err, ErrInvalidRefreshToken) || errors.Is(err, ErrSessionNotFound) {
			h.writeError(w, http.StatusUnauthorized, CodeInvalidRefreshToken, "Invalid refresh token", nil)
			return
		}
		h.writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "An unexpected error occurred", nil)
		return
	}

	h.writeSuccess(w, http.StatusOK, map[string]string{
		"message": "Successfully logged out",
	})
}

// Refresh handles token refresh
// POST /api/v1/auth/refresh
// Requirements: 3.4-3.5
func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req RefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, CodeValidationError, "Invalid request body", nil)
		return
	}

	if req.RefreshToken == "" {
		h.writeError(w, http.StatusBadRequest, CodeValidationError, "refresh_token is required", nil)
		return
	}

	tokens, err := h.authService.RefreshToken(r.Context(), req.RefreshToken)
	if err != nil {
		if errors.Is(err, ErrInvalidRefreshToken) {
			h.writeError(w, http.StatusUnauthorized, CodeInvalidRefreshToken, "Invalid or expired refresh token", nil)
			return
		}
		h.writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "An unexpected error occurred", nil)
		return
	}

	h.writeSuccess(w, http.StatusOK, map[string]interface{}{
		"tokens": tokens,
	})
}


// GetMe handles getting current user profile
// GET /api/v1/auth/me
// Requirements: 5.1-5.3
func (h *AuthHandler) GetMe(w http.ResponseWriter, r *http.Request) {
	// Extract user ID from context (set by auth middleware)
	userID, ok := appctx.ExtractUserID(r.Context())
	if !ok {
		h.writeError(w, http.StatusUnauthorized, CodeAuthTokenInvalid, "Invalid or expired token", nil)
		return
	}

	profile, err := h.authService.GetUserProfile(r.Context(), userID)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			h.writeError(w, http.StatusNotFound, "USER_NOT_FOUND", "User not found", nil)
			return
		}
		h.writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "An unexpected error occurred", nil)
		return
	}

	h.writeSuccess(w, http.StatusOK, map[string]interface{}{
		"user": profile,
	})
}

// DeleteMe handles deleting the current user's account
// DELETE /api/v1/auth/me
// Requirements: 4.3 (Delete user account with cascade delete)
// This deletes:
// - All domains owned by the user
// - All aliases under those domains
// - All emails received by those aliases
// - All attachments from those emails (both DB records and S3 storage)
// - All sessions for the user
func (h *AuthHandler) DeleteMe(w http.ResponseWriter, r *http.Request) {
	// Extract user ID from context (set by auth middleware)
	userID, ok := appctx.ExtractUserID(r.Context())
	if !ok {
		h.writeError(w, http.StatusUnauthorized, CodeAuthTokenInvalid, "Invalid or expired token", nil)
		return
	}

	response, err := h.authService.DeleteUser(r.Context(), userID)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			h.writeError(w, http.StatusNotFound, "USER_NOT_FOUND", "User not found", nil)
			return
		}
		h.writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "An unexpected error occurred", nil)
		return
	}

	h.writeSuccess(w, http.StatusOK, map[string]interface{}{
		"message": response.Message,
		"deleted_resources": map[string]interface{}{
			"user_id":                response.UserID,
			"domains_deleted":        response.DomainsDeleted,
			"aliases_deleted":        response.AliasesDeleted,
			"emails_deleted":         response.EmailsDeleted,
			"attachments_deleted":    response.AttachmentsDeleted,
			"total_size_freed_bytes": response.TotalSizeFreedBytes,
		},
	})
}

// writeSuccess writes a successful JSON response
func (h *AuthHandler) writeSuccess(w http.ResponseWriter, statusCode int, data interface{}) {
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
func (h *AuthHandler) writeError(w http.ResponseWriter, statusCode int, code, message string, details map[string][]string) {
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

// getClientIP extracts the client IP address from the request
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header first (for proxied requests)
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		// X-Forwarded-For can contain multiple IPs, take the first one
		ips := splitAndTrim(xff, ",")
		if len(ips) > 0 {
			return ips[0]
		}
	}

	// Check X-Real-IP header
	xri := r.Header.Get("X-Real-IP")
	if xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	return r.RemoteAddr
}

// splitAndTrim splits a string by separator and trims whitespace from each part
func splitAndTrim(s, sep string) []string {
	var result []string
	for _, part := range splitString(s, sep) {
		trimmed := trimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// splitString splits a string by separator
func splitString(s, sep string) []string {
	var result []string
	start := 0
	for i := 0; i < len(s); i++ {
		if i+len(sep) <= len(s) && s[i:i+len(sep)] == sep {
			result = append(result, s[start:i])
			start = i + len(sep)
			i += len(sep) - 1
		}
	}
	result = append(result, s[start:])
	return result
}

// trimSpace removes leading and trailing whitespace
func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}
