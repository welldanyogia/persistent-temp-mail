// Package middleware provides HTTP middleware for the backend API.
// This file implements structured JSON logging middleware with correlation ID support.
// Requirements: 14.3 - Application logs SHALL use structured JSON format
// Requirements: 14.4 - Application logs SHALL include correlation IDs for request tracing
package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/logger"
)

// LoggingMiddleware provides structured JSON logging for HTTP requests
type LoggingMiddleware struct {
	logger *slog.Logger
}

// NewLoggingMiddleware creates a new LoggingMiddleware instance
func NewLoggingMiddleware(log *slog.Logger) *LoggingMiddleware {
	if log == nil {
		log = slog.Default()
	}
	return &LoggingMiddleware{
		logger: log,
	}
}

// Handler returns an HTTP middleware that logs requests in structured JSON format
// Requirements: 14.3 - Application logs SHALL use structured JSON format
// Requirements: 14.4 - Application logs SHALL include correlation IDs for request tracing
func (m *LoggingMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Get request ID from chi middleware (set by middleware.RequestID)
		requestID := middleware.GetReqID(r.Context())

		// Add correlation ID to context for downstream use
		ctx := logger.SetCorrelationID(r.Context(), requestID)
		r = r.WithContext(ctx)

		// Create a response wrapper to capture status code
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

		// Process request
		next.ServeHTTP(ww, r)

		// Calculate duration
		duration := time.Since(start)

		// Build log attributes
		// Requirements: 14.4 - Include correlation IDs for request tracing
		attrs := []slog.Attr{
			slog.String("correlation_id", requestID),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.String("query", r.URL.RawQuery),
			slog.Int("status", ww.Status()),
			slog.Int("bytes", ww.BytesWritten()),
			slog.Duration("duration", duration),
			slog.String("remote_addr", r.RemoteAddr),
			slog.String("user_agent", r.UserAgent()),
			slog.String("protocol", r.Proto),
		}

		// Add referer if present
		if referer := r.Referer(); referer != "" {
			attrs = append(attrs, slog.String("referer", referer))
		}

		// Add X-Forwarded-For if present (for proxied requests)
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			attrs = append(attrs, slog.String("x_forwarded_for", xff))
		}

		// Add X-Real-IP if present
		if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
			attrs = append(attrs, slog.String("real_ip", realIP))
		}

		// Log at appropriate level based on status code
		logAttrs := make([]any, len(attrs))
		for i, attr := range attrs {
			logAttrs[i] = attr
		}

		switch {
		case ww.Status() >= 500:
			m.logger.Error("HTTP request completed with server error", logAttrs...)
		case ww.Status() >= 400:
			m.logger.Warn("HTTP request completed with client error", logAttrs...)
		default:
			m.logger.Info("HTTP request completed", logAttrs...)
		}
	})
}

// StructuredLogger returns a chi-compatible logger that uses slog
// This replaces chi's default logger with structured JSON logging
func StructuredLogger(log *slog.Logger) func(next http.Handler) http.Handler {
	return NewLoggingMiddleware(log).Handler
}
