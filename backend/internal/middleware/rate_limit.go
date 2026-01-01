package middleware

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// RateLimiter implements a simple in-memory rate limiter
type RateLimiter struct {
	mu       sync.RWMutex
	requests map[string][]time.Time
	limit    int           // Max requests
	window   time.Duration // Time window
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	rl := &RateLimiter{
		requests: make(map[string][]time.Time),
		limit:    limit,
		window:   window,
	}
	
	// Start cleanup goroutine
	go rl.cleanup()
	
	return rl
}

// Allow checks if a request is allowed for the given key
func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	windowStart := now.Add(-rl.window)

	// Get existing requests for this key
	requests := rl.requests[key]

	// Filter out old requests
	var validRequests []time.Time
	for _, t := range requests {
		if t.After(windowStart) {
			validRequests = append(validRequests, t)
		}
	}

	// Check if limit exceeded
	if len(validRequests) >= rl.limit {
		rl.requests[key] = validRequests
		return false
	}

	// Add new request
	validRequests = append(validRequests, now)
	rl.requests[key] = validRequests

	return true
}

// Remaining returns the number of remaining requests for a key
func (rl *RateLimiter) Remaining(key string) int {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	now := time.Now()
	windowStart := now.Add(-rl.window)

	requests := rl.requests[key]
	count := 0
	for _, t := range requests {
		if t.After(windowStart) {
			count++
		}
	}

	remaining := rl.limit - count
	if remaining < 0 {
		return 0
	}
	return remaining
}

// Reset returns the time when the rate limit resets
func (rl *RateLimiter) Reset(key string) time.Time {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	requests := rl.requests[key]
	if len(requests) == 0 {
		return time.Now()
	}

	// Find the oldest request in the window
	oldest := requests[0]
	for _, t := range requests {
		if t.Before(oldest) {
			oldest = t
		}
	}

	return oldest.Add(rl.window)
}

// cleanup periodically removes old entries
func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(rl.window)
	defer ticker.Stop()

	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()
		windowStart := now.Add(-rl.window)

		for key, requests := range rl.requests {
			var validRequests []time.Time
			for _, t := range requests {
				if t.After(windowStart) {
					validRequests = append(validRequests, t)
				}
			}
			if len(validRequests) == 0 {
				delete(rl.requests, key)
			} else {
				rl.requests[key] = validRequests
			}
		}
		rl.mu.Unlock()
	}
}


// DomainVerifyRateLimiter is a specialized rate limiter for domain verification
// Requirements: NFR-2 (Security) - 10 verify requests per domain per hour
type DomainVerifyRateLimiter struct {
	limiter *RateLimiter
}

// NewDomainVerifyRateLimiter creates a rate limiter for domain verification
// Limit: 10 requests per domain per hour
func NewDomainVerifyRateLimiter() *DomainVerifyRateLimiter {
	return &DomainVerifyRateLimiter{
		limiter: NewRateLimiter(10, time.Hour),
	}
}

// RateLimitVerify creates middleware that rate limits domain verification requests
func (rl *DomainVerifyRateLimiter) RateLimitVerify(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract domain ID from URL path
		// Expected path: /api/v1/domains/{id}/verify
		domainID := extractDomainID(r.URL.Path)
		if domainID == "" {
			next.ServeHTTP(w, r)
			return
		}

		// Check rate limit
		if !rl.limiter.Allow(domainID) {
			writeRateLimitError(w, rl.limiter.Reset(domainID))
			return
		}

		// Set rate limit headers
		w.Header().Set("X-RateLimit-Limit", "10")
		w.Header().Set("X-RateLimit-Remaining", formatInt(rl.limiter.Remaining(domainID)))
		w.Header().Set("X-RateLimit-Reset", formatInt64(rl.limiter.Reset(domainID).Unix()))

		next.ServeHTTP(w, r)
	})
}

// extractDomainID extracts the domain ID from the URL path
func extractDomainID(path string) string {
	// Path format: /api/v1/domains/{id}/verify
	// We need to extract {id}
	parts := splitPath(path)
	for i, part := range parts {
		if part == "domains" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

// splitPath splits a URL path into parts
func splitPath(path string) []string {
	var parts []string
	var current string
	for _, c := range path {
		if c == '/' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(c)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}

// writeRateLimitError writes a 429 Too Many Requests response
func writeRateLimitError(w http.ResponseWriter, resetTime time.Time) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Retry-After", formatInt64(resetTime.Unix()-time.Now().Unix()))
	w.WriteHeader(http.StatusTooManyRequests)

	response := map[string]interface{}{
		"success": false,
		"error": map[string]interface{}{
			"code":    "TOO_MANY_REQUESTS",
			"message": "Rate limit exceeded. Please try again later.",
			"details": map[string]interface{}{
				"retry_after": resetTime.Unix() - time.Now().Unix(),
			},
		},
		"timestamp": time.Now().UTC(),
	}

	json.NewEncoder(w).Encode(response)
}

// formatInt converts int to string
func formatInt(n int) string {
	return formatInt64(int64(n))
}

// formatInt64 converts int64 to string
func formatInt64(n int64) string {
	if n == 0 {
		return "0"
	}
	
	negative := n < 0
	if negative {
		n = -n
	}
	
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	
	if negative {
		return "-" + string(digits)
	}
	return string(digits)
}

// AttachmentDownloadRateLimiter is a specialized rate limiter for attachment downloads
// Requirements: 6.7 (Security) - 100 downloads per user per hour
type AttachmentDownloadRateLimiter struct {
	limiter *RateLimiter
}

// NewAttachmentDownloadRateLimiter creates a rate limiter for attachment downloads
// Limit: 100 downloads per user per hour
func NewAttachmentDownloadRateLimiter() *AttachmentDownloadRateLimiter {
	return &AttachmentDownloadRateLimiter{
		limiter: NewRateLimiter(100, time.Hour),
	}
}

// RateLimitDownload creates middleware that rate limits attachment download requests
// Requirements: 6.7 - Rate limit downloads to 100 per user per hour
func (rl *AttachmentDownloadRateLimiter) RateLimitDownload(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract user ID from context (set by auth middleware)
		userID := extractUserIDFromContext(r)
		if userID == "" {
			// If no user ID, let the auth middleware handle it
			next.ServeHTTP(w, r)
			return
		}

		// Set rate limit headers before checking limit
		// This ensures headers are always present
		remaining := rl.limiter.Remaining(userID)
		resetTime := rl.limiter.Reset(userID)
		
		w.Header().Set("X-RateLimit-Limit", "100")
		w.Header().Set("X-RateLimit-Remaining", formatInt(remaining))
		w.Header().Set("X-RateLimit-Reset", formatInt64(resetTime.Unix()))

		// Check rate limit
		if !rl.limiter.Allow(userID) {
			writeDownloadRateLimitError(w, resetTime)
			return
		}

		// Update remaining after allowing the request
		w.Header().Set("X-RateLimit-Remaining", formatInt(rl.limiter.Remaining(userID)))

		next.ServeHTTP(w, r)
	})
}

// Remaining returns the number of remaining downloads for a user
func (rl *AttachmentDownloadRateLimiter) Remaining(userID string) int {
	return rl.limiter.Remaining(userID)
}

// Reset returns the time when the rate limit resets for a user
func (rl *AttachmentDownloadRateLimiter) Reset(userID string) time.Time {
	return rl.limiter.Reset(userID)
}

// extractUserIDFromContext extracts user ID from request context
// The user ID is set by the auth middleware
func extractUserIDFromContext(r *http.Request) string {
	// Try to get user_id from context (set by auth middleware)
	if userID, ok := r.Context().Value("user_id").(string); ok {
		return userID
	}
	return ""
}

// writeDownloadRateLimitError writes a 429 Too Many Requests response for download rate limiting
func writeDownloadRateLimitError(w http.ResponseWriter, resetTime time.Time) {
	w.Header().Set("Content-Type", "application/json")
	retryAfter := resetTime.Unix() - time.Now().Unix()
	if retryAfter < 0 {
		retryAfter = 0
	}
	w.Header().Set("Retry-After", formatInt64(retryAfter))
	w.WriteHeader(http.StatusTooManyRequests)

	response := map[string]interface{}{
		"success": false,
		"error": map[string]interface{}{
			"code":    "DOWNLOAD_RATE_LIMITED",
			"message": "Download rate limit exceeded. Maximum 100 downloads per hour.",
			"details": map[string]interface{}{
				"retry_after":   retryAfter,
				"limit":         100,
				"window_hours":  1,
			},
		},
		"timestamp": time.Now().UTC(),
	}

	json.NewEncoder(w).Encode(response)
}
