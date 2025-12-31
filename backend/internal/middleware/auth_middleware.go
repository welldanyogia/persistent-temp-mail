package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/welldanyogia/persistent-temp-mail/backend/internal/auth"
	appctx "github.com/welldanyogia/persistent-temp-mail/backend/internal/context"
)

// ErrorResponse represents the standard error response format
type ErrorResponse struct {
	Success   bool        `json:"success"`
	Error     ErrorDetail `json:"error"`
	Timestamp time.Time   `json:"timestamp"`
}

// ErrorDetail contains error information
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// AuthMiddleware handles JWT authentication for protected routes
type AuthMiddleware struct {
	tokenService *auth.TokenService
}

// NewAuthMiddleware creates a new AuthMiddleware instance
func NewAuthMiddleware(tokenService *auth.TokenService) *AuthMiddleware {
	return &AuthMiddleware{
		tokenService: tokenService,
	}
}

// Authenticate is a middleware that validates JWT tokens from the Authorization header
func (m *AuthMiddleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Get Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			m.writeError(w, http.StatusUnauthorized, "AUTH_TOKEN_MISSING", "Authorization header is required")
			return
		}

		// Check Bearer prefix
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			m.writeError(w, http.StatusUnauthorized, "AUTH_TOKEN_INVALID", "Invalid authorization header format")
			return
		}

		tokenString := parts[1]
		if tokenString == "" {
			m.writeError(w, http.StatusUnauthorized, "AUTH_TOKEN_INVALID", "Token is empty")
			return
		}

		// Validate the access token
		claims, err := m.tokenService.ValidateAccessToken(tokenString)
		if err != nil {
			m.writeError(w, http.StatusUnauthorized, "AUTH_TOKEN_INVALID", "Invalid or expired token")
			return
		}

		// Inject user_id and email into request context
		ctx := context.WithValue(r.Context(), appctx.UserIDKey, claims.UserID())
		ctx = context.WithValue(ctx, appctx.EmailKey, claims.Email)

		// Call the next handler with the updated context
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// writeError writes a JSON error response
func (m *AuthMiddleware) writeError(w http.ResponseWriter, statusCode int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	response := ErrorResponse{
		Success: false,
		Error: ErrorDetail{
			Code:    code,
			Message: message,
		},
		Timestamp: time.Now().UTC(),
	}

	json.NewEncoder(w).Encode(response)
}

// ExtractUserID extracts the user ID from the request context
func ExtractUserID(ctx context.Context) (string, bool) {
	return appctx.ExtractUserID(ctx)
}

// ExtractEmail extracts the email from the request context
func ExtractEmail(ctx context.Context) (string, bool) {
	return appctx.ExtractEmail(ctx)
}
