// Package logger provides structured JSON logging with correlation ID support.
// Requirements: 14.3 - Application logs SHALL use structured JSON format
// Requirements: 14.4 - Application logs SHALL include correlation IDs for request tracing
// Requirements: 14.9 - Logs SHALL not contain sensitive data (passwords, tokens)
package logger

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
)

// ContextKey is a custom type for context keys to avoid collisions
type ContextKey string

const (
	// CorrelationIDKey is the context key for correlation/request ID
	CorrelationIDKey ContextKey = "correlation_id"
	// RequestIDKey is an alias for correlation ID (used by chi middleware)
	RequestIDKey ContextKey = "request_id"
)

// Config holds logger configuration
type Config struct {
	// Level is the minimum log level (debug, info, warn, error)
	Level string
	// Format is the log format (json, text)
	Format string
	// Output is the log output destination (stdout, stderr, or file path)
	Output string
	// AddSource adds source file and line number to log entries
	AddSource bool
}

// DefaultConfig returns the default logger configuration
func DefaultConfig() Config {
	return Config{
		Level:     getEnv("LOG_LEVEL", "info"),
		Format:    getEnv("LOG_FORMAT", "json"),
		Output:    getEnv("LOG_OUTPUT", "stdout"),
		AddSource: getBoolEnv("LOG_ADD_SOURCE", false),
	}
}

// New creates a new structured logger based on configuration
// Requirements: 14.3 - Application logs SHALL use structured JSON format
func New(cfg Config) *slog.Logger {
	// Parse log level
	var level slog.Level
	switch strings.ToLower(cfg.Level) {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	// Configure output
	var output io.Writer
	switch strings.ToLower(cfg.Output) {
	case "stdout":
		output = os.Stdout
	case "stderr":
		output = os.Stderr
	default:
		// Try to open file, fallback to stdout
		file, err := os.OpenFile(cfg.Output, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			output = os.Stdout
		} else {
			output = file
		}
	}

	// Configure handler options
	opts := &slog.HandlerOptions{
		Level:     level,
		AddSource: cfg.AddSource,
		// Replace sensitive attributes to prevent logging secrets
		// Requirements: 14.9 - Logs SHALL not contain sensitive data
		ReplaceAttr: sanitizeAttributes,
	}

	// Create handler based on format
	var handler slog.Handler
	switch strings.ToLower(cfg.Format) {
	case "text":
		handler = slog.NewTextHandler(output, opts)
	default:
		// Default to JSON format
		// Requirements: 14.3 - Application logs SHALL use structured JSON format
		handler = slog.NewJSONHandler(output, opts)
	}

	return slog.New(handler)
}

// sanitizeAttributes removes or masks sensitive data from log attributes
// Requirements: 14.9 - Logs SHALL not contain sensitive data (passwords, tokens)
func sanitizeAttributes(groups []string, a slog.Attr) slog.Attr {
	// List of sensitive attribute keys to redact
	sensitiveKeys := map[string]bool{
		"password":       true,
		"token":          true,
		"access_token":   true,
		"refresh_token":  true,
		"secret":         true,
		"api_key":        true,
		"apikey":         true,
		"authorization":  true,
		"auth":           true,
		"credential":     true,
		"credentials":    true,
		"private_key":    true,
		"encryption_key": true,
	}

	key := strings.ToLower(a.Key)
	if sensitiveKeys[key] {
		return slog.String(a.Key, "[REDACTED]")
	}

	// Check for partial matches (e.g., "user_password", "jwt_token")
	for sensitive := range sensitiveKeys {
		if strings.Contains(key, sensitive) {
			return slog.String(a.Key, "[REDACTED]")
		}
	}

	return a
}

// WithCorrelationID returns a new logger with the correlation ID from context
// Requirements: 14.4 - Application logs SHALL include correlation IDs for request tracing
func WithCorrelationID(ctx context.Context, logger *slog.Logger) *slog.Logger {
	correlationID := GetCorrelationID(ctx)
	if correlationID == "" {
		return logger
	}
	return logger.With(slog.String("correlation_id", correlationID))
}

// GetCorrelationID extracts the correlation ID from context
func GetCorrelationID(ctx context.Context) string {
	// Try correlation_id first
	if id, ok := ctx.Value(CorrelationIDKey).(string); ok && id != "" {
		return id
	}
	// Fall back to request_id (chi middleware uses this)
	if id, ok := ctx.Value(RequestIDKey).(string); ok && id != "" {
		return id
	}
	return ""
}

// SetCorrelationID adds a correlation ID to the context
func SetCorrelationID(ctx context.Context, correlationID string) context.Context {
	return context.WithValue(ctx, CorrelationIDKey, correlationID)
}

// Helper functions for environment variables
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getBoolEnv(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		switch strings.ToLower(value) {
		case "true", "1", "yes":
			return true
		case "false", "0", "no":
			return false
		}
	}
	return defaultValue
}
