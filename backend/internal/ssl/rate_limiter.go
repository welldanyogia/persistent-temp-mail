// Package ssl provides SSL certificate management functionality
// Requirements: 6.1, 6.2, 6.3, 6.4, 6.6 - Rate limit handling for Let's Encrypt
package ssl

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"math"
	"sync"
	"time"
)

// Rate limit errors
var (
	ErrSSLRateLimited       = errors.New("SSL rate limit exceeded")
	ErrSSLRateLimitApproach = errors.New("approaching SSL rate limit")
)

// Let's Encrypt rate limits (as of 2024)
// https://letsencrypt.org/docs/rate-limits/
const (
	// MaxCertsPerDomainPerWeek is the maximum certificates per registered domain per week
	// Requirements: 6.2 - Enforce Let's Encrypt rate limits locally (50 certs/week/domain)
	MaxCertsPerDomainPerWeek = 50

	// MaxFailedValidationsPerHour is the maximum failed validations per account per hostname per hour
	MaxFailedValidationsPerHour = 5

	// MaxAccountsPerIP is the maximum accounts per IP per 3 hours
	MaxAccountsPerIP = 10

	// MaxPendingAuthorizationsPerAccount is the maximum pending authorizations per account
	MaxPendingAuthorizationsPerAccount = 300

	// RateLimitWindow is the time window for certificate rate limiting (1 week)
	RateLimitWindow = 7 * 24 * time.Hour

	// FailedValidationWindow is the time window for failed validation rate limiting (1 hour)
	FailedValidationWindow = time.Hour

	// DefaultMaxBackoff is the maximum backoff duration for exponential backoff
	DefaultMaxBackoff = 24 * time.Hour

	// DefaultBaseBackoff is the base backoff duration for exponential backoff
	DefaultBaseBackoff = time.Minute
)


// RateLimitConfig holds configuration for the SSL rate limiter
type RateLimitConfig struct {
	// MaxCertsPerWeek is the maximum certificates per domain per week
	// Default: 50 (Let's Encrypt limit)
	MaxCertsPerWeek int

	// MaxFailedValidations is the maximum failed validations per domain per hour
	// Default: 5 (Let's Encrypt limit)
	MaxFailedValidations int

	// BaseBackoff is the base duration for exponential backoff
	// Default: 1 minute
	BaseBackoff time.Duration

	// MaxBackoff is the maximum duration for exponential backoff
	// Default: 24 hours
	MaxBackoff time.Duration

	// WarningThreshold is the percentage of rate limit at which to warn
	// Default: 0.8 (80%)
	WarningThreshold float64
}

// DefaultRateLimitConfig returns the default rate limit configuration
func DefaultRateLimitConfig() RateLimitConfig {
	return RateLimitConfig{
		MaxCertsPerWeek:      MaxCertsPerDomainPerWeek,
		MaxFailedValidations: MaxFailedValidationsPerHour,
		BaseBackoff:          DefaultBaseBackoff,
		MaxBackoff:           DefaultMaxBackoff,
		WarningThreshold:     0.8,
	}
}

// CertificateRequest represents a certificate request for rate limiting
type CertificateRequest struct {
	DomainName string
	RequestedAt time.Time
	Success     bool
}

// DomainRateLimitInfo contains rate limit information for a domain
type DomainRateLimitInfo struct {
	DomainName           string        `json:"domain_name"`
	RequestsThisWeek     int           `json:"requests_this_week"`
	FailedValidations    int           `json:"failed_validations_this_hour"`
	RemainingRequests    int           `json:"remaining_requests"`
	NextResetAt          time.Time     `json:"next_reset_at"`
	IsRateLimited        bool          `json:"is_rate_limited"`
	IsApproachingLimit   bool          `json:"is_approaching_limit"`
	CurrentBackoff       time.Duration `json:"current_backoff_seconds"`
	NextAllowedRequestAt time.Time     `json:"next_allowed_request_at"`
}


// SSLRateLimiter implements rate limiting for SSL certificate operations
// Requirements: 6.1, 6.2, 6.3, 6.4, 6.6
type SSLRateLimiter struct {
	config RateLimitConfig

	mu sync.RWMutex

	// Track certificate requests per domain
	// Requirements: 6.1 - Track certificate requests per domain
	requests map[string][]CertificateRequest

	// Track failed validations per domain
	failedValidations map[string][]time.Time

	// Track backoff state per domain for exponential backoff
	// Requirements: 6.4 - Use exponential backoff when rate limited
	backoffState map[string]*backoffInfo

	// ACME account credentials cache
	// Requirements: 6.6 - Cache ACME account credentials
	acmeAccountCache *ACMEAccountCache
}

// backoffInfo tracks exponential backoff state for a domain
type backoffInfo struct {
	ConsecutiveFailures int
	LastFailureAt       time.Time
	NextAllowedAt       time.Time
}

// NewSSLRateLimiter creates a new SSL rate limiter with default configuration
func NewSSLRateLimiter() *SSLRateLimiter {
	return NewSSLRateLimiterWithConfig(DefaultRateLimitConfig())
}

// NewSSLRateLimiterWithConfig creates a new SSL rate limiter with custom configuration
func NewSSLRateLimiterWithConfig(config RateLimitConfig) *SSLRateLimiter {
	rl := &SSLRateLimiter{
		config:            config,
		requests:          make(map[string][]CertificateRequest),
		failedValidations: make(map[string][]time.Time),
		backoffState:      make(map[string]*backoffInfo),
		acmeAccountCache:  NewACMEAccountCache(),
	}

	// Start cleanup goroutine
	go rl.cleanup()

	return rl
}


// AllowRequest checks if a certificate request is allowed for the given domain
// Requirements: 6.2 - Enforce Let's Encrypt rate limits locally (50 certs/week/domain)
// Requirements: 6.3 - Delay non-urgent requests when approaching rate limit
func (rl *SSLRateLimiter) AllowRequest(ctx context.Context, domainName string) error {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	// Check if domain is in backoff period
	// Requirements: 6.4 - Use exponential backoff when rate limited
	if backoff, ok := rl.backoffState[domainName]; ok {
		if now.Before(backoff.NextAllowedAt) {
			return fmt.Errorf("%w: retry after %v", ErrSSLRateLimited, backoff.NextAllowedAt.Sub(now))
		}
	}

	// Count requests in the current week
	weekAgo := now.Add(-RateLimitWindow)
	requestCount := rl.countRequestsInWindow(domainName, weekAgo)

	// Check if rate limited
	if requestCount >= rl.config.MaxCertsPerWeek {
		return fmt.Errorf("%w: %d requests in the last week (limit: %d)",
			ErrSSLRateLimited, requestCount, rl.config.MaxCertsPerWeek)
	}

	// Check if approaching limit and warn
	// Requirements: 6.3 - Delay non-urgent requests when approaching rate limit
	threshold := int(float64(rl.config.MaxCertsPerWeek) * rl.config.WarningThreshold)
	if requestCount >= threshold {
		log.Printf("Warning: Domain %s is approaching rate limit (%d/%d requests this week)",
			domainName, requestCount, rl.config.MaxCertsPerWeek)
	}

	// Check failed validations in the last hour
	hourAgo := now.Add(-FailedValidationWindow)
	failedCount := rl.countFailedValidationsInWindow(domainName, hourAgo)
	if failedCount >= rl.config.MaxFailedValidations {
		return fmt.Errorf("%w: %d failed validations in the last hour (limit: %d)",
			ErrSSLRateLimited, failedCount, rl.config.MaxFailedValidations)
	}

	return nil
}

// RecordRequest records a certificate request for rate limiting
// Requirements: 6.1 - Track certificate requests per domain
func (rl *SSLRateLimiter) RecordRequest(domainName string, success bool) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	// Record the request
	request := CertificateRequest{
		DomainName:  domainName,
		RequestedAt: now,
		Success:     success,
	}
	rl.requests[domainName] = append(rl.requests[domainName], request)

	// Handle success/failure for backoff
	if success {
		// Reset backoff on success
		delete(rl.backoffState, domainName)
	} else {
		// Record failed validation
		rl.failedValidations[domainName] = append(rl.failedValidations[domainName], now)

		// Update backoff state
		// Requirements: 6.4 - Use exponential backoff when rate limited
		rl.updateBackoff(domainName)
	}
}


// GetRateLimitInfo returns rate limit information for a domain
func (rl *SSLRateLimiter) GetRateLimitInfo(domainName string) *DomainRateLimitInfo {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	now := time.Now()
	weekAgo := now.Add(-RateLimitWindow)
	hourAgo := now.Add(-FailedValidationWindow)

	requestCount := rl.countRequestsInWindow(domainName, weekAgo)
	failedCount := rl.countFailedValidationsInWindow(domainName, hourAgo)
	remaining := rl.config.MaxCertsPerWeek - requestCount
	if remaining < 0 {
		remaining = 0
	}

	info := &DomainRateLimitInfo{
		DomainName:         domainName,
		RequestsThisWeek:   requestCount,
		FailedValidations:  failedCount,
		RemainingRequests:  remaining,
		NextResetAt:        rl.getNextResetTime(domainName),
		IsRateLimited:      requestCount >= rl.config.MaxCertsPerWeek || failedCount >= rl.config.MaxFailedValidations,
		IsApproachingLimit: requestCount >= int(float64(rl.config.MaxCertsPerWeek)*rl.config.WarningThreshold),
	}

	// Add backoff info if present
	if backoff, ok := rl.backoffState[domainName]; ok {
		info.CurrentBackoff = rl.calculateBackoffDuration(backoff.ConsecutiveFailures)
		info.NextAllowedRequestAt = backoff.NextAllowedAt
		if now.Before(backoff.NextAllowedAt) {
			info.IsRateLimited = true
		}
	}

	return info
}

// ShouldDelayRequest checks if a request should be delayed (non-urgent)
// Requirements: 6.3 - Delay non-urgent requests when approaching rate limit
func (rl *SSLRateLimiter) ShouldDelayRequest(domainName string) (bool, time.Duration) {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	now := time.Now()
	weekAgo := now.Add(-RateLimitWindow)

	requestCount := rl.countRequestsInWindow(domainName, weekAgo)
	threshold := int(float64(rl.config.MaxCertsPerWeek) * rl.config.WarningThreshold)

	if requestCount >= threshold {
		// Calculate delay based on how close we are to the limit
		remaining := rl.config.MaxCertsPerWeek - requestCount
		if remaining <= 0 {
			return true, RateLimitWindow // Max delay
		}

		// Spread remaining requests over the remaining time window
		delay := RateLimitWindow / time.Duration(remaining)
		if delay > time.Hour {
			delay = time.Hour // Cap at 1 hour
		}
		return true, delay
	}

	return false, 0
}


// GetBackoffDuration returns the current backoff duration for a domain
// Requirements: 6.4 - Use exponential backoff when rate limited
func (rl *SSLRateLimiter) GetBackoffDuration(domainName string) time.Duration {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	if backoff, ok := rl.backoffState[domainName]; ok {
		return rl.calculateBackoffDuration(backoff.ConsecutiveFailures)
	}
	return 0
}

// ResetBackoff resets the backoff state for a domain
func (rl *SSLRateLimiter) ResetBackoff(domainName string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.backoffState, domainName)
}

// updateBackoff updates the backoff state for a domain after a failure
// Requirements: 6.4 - Use exponential backoff when rate limited
func (rl *SSLRateLimiter) updateBackoff(domainName string) {
	now := time.Now()

	backoff, ok := rl.backoffState[domainName]
	if !ok {
		backoff = &backoffInfo{}
		rl.backoffState[domainName] = backoff
	}

	backoff.ConsecutiveFailures++
	backoff.LastFailureAt = now

	// Calculate next allowed time using exponential backoff
	backoffDuration := rl.calculateBackoffDuration(backoff.ConsecutiveFailures)
	backoff.NextAllowedAt = now.Add(backoffDuration)

	log.Printf("SSL rate limiter: Domain %s backoff updated (failures: %d, next allowed: %v)",
		domainName, backoff.ConsecutiveFailures, backoff.NextAllowedAt)
}

// calculateBackoffDuration calculates the backoff duration using exponential backoff
// Formula: min(maxBackoff, baseBackoff * 2^(failures-1))
// Requirements: 6.4 - Use exponential backoff when rate limited
func (rl *SSLRateLimiter) calculateBackoffDuration(failures int) time.Duration {
	if failures <= 0 {
		return 0
	}

	// Exponential backoff: base * 2^(failures-1)
	multiplier := math.Pow(2, float64(failures-1))
	backoff := time.Duration(float64(rl.config.BaseBackoff) * multiplier)

	// Cap at max backoff
	if backoff > rl.config.MaxBackoff {
		backoff = rl.config.MaxBackoff
	}

	return backoff
}


// countRequestsInWindow counts requests for a domain since the given time
func (rl *SSLRateLimiter) countRequestsInWindow(domainName string, since time.Time) int {
	requests := rl.requests[domainName]
	count := 0
	for _, req := range requests {
		if req.RequestedAt.After(since) {
			count++
		}
	}
	return count
}

// countFailedValidationsInWindow counts failed validations for a domain since the given time
func (rl *SSLRateLimiter) countFailedValidationsInWindow(domainName string, since time.Time) int {
	failures := rl.failedValidations[domainName]
	count := 0
	for _, t := range failures {
		if t.After(since) {
			count++
		}
	}
	return count
}

// getNextResetTime returns the time when the oldest request will expire from the window
func (rl *SSLRateLimiter) getNextResetTime(domainName string) time.Time {
	requests := rl.requests[domainName]
	if len(requests) == 0 {
		return time.Now()
	}

	// Find the oldest request in the window
	weekAgo := time.Now().Add(-RateLimitWindow)
	var oldest time.Time
	for _, req := range requests {
		if req.RequestedAt.After(weekAgo) {
			if oldest.IsZero() || req.RequestedAt.Before(oldest) {
				oldest = req.RequestedAt
			}
		}
	}

	if oldest.IsZero() {
		return time.Now()
	}

	return oldest.Add(RateLimitWindow)
}

// cleanup periodically removes old entries from the rate limiter
func (rl *SSLRateLimiter) cleanup() {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()
		weekAgo := now.Add(-RateLimitWindow)
		hourAgo := now.Add(-FailedValidationWindow)

		// Clean up old requests
		for domain, requests := range rl.requests {
			var validRequests []CertificateRequest
			for _, req := range requests {
				if req.RequestedAt.After(weekAgo) {
					validRequests = append(validRequests, req)
				}
			}
			if len(validRequests) == 0 {
				delete(rl.requests, domain)
			} else {
				rl.requests[domain] = validRequests
			}
		}

		// Clean up old failed validations
		for domain, failures := range rl.failedValidations {
			var validFailures []time.Time
			for _, t := range failures {
				if t.After(hourAgo) {
					validFailures = append(validFailures, t)
				}
			}
			if len(validFailures) == 0 {
				delete(rl.failedValidations, domain)
			} else {
				rl.failedValidations[domain] = validFailures
			}
		}

		// Clean up old backoff states (if last failure was more than max backoff ago)
		for domain, backoff := range rl.backoffState {
			if now.After(backoff.NextAllowedAt.Add(rl.config.MaxBackoff)) {
				delete(rl.backoffState, domain)
			}
		}

		rl.mu.Unlock()
	}
}


// ACMEAccountCache caches ACME account credentials to avoid re-registration
// Requirements: 6.6 - Cache ACME account credentials
type ACMEAccountCache struct {
	mu       sync.RWMutex
	accounts map[string]*ACMEAccount
}

// ACMEAccount represents cached ACME account credentials
type ACMEAccount struct {
	Email        string    `json:"email"`
	AccountURL   string    `json:"account_url"`
	PrivateKey   []byte    `json:"private_key"` // PEM-encoded private key
	CreatedAt    time.Time `json:"created_at"`
	LastUsedAt   time.Time `json:"last_used_at"`
}

// NewACMEAccountCache creates a new ACME account cache
func NewACMEAccountCache() *ACMEAccountCache {
	return &ACMEAccountCache{
		accounts: make(map[string]*ACMEAccount),
	}
}

// Get retrieves an ACME account from the cache
// Requirements: 6.6 - Cache ACME account credentials
func (c *ACMEAccountCache) Get(email string) (*ACMEAccount, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	account, ok := c.accounts[email]
	if ok {
		// Update last used time (need write lock for this)
		c.mu.RUnlock()
		c.mu.Lock()
		account.LastUsedAt = time.Now()
		c.mu.Unlock()
		c.mu.RLock()
	}
	return account, ok
}

// Set stores an ACME account in the cache
// Requirements: 6.6 - Cache ACME account credentials
func (c *ACMEAccountCache) Set(account *ACMEAccount) {
	c.mu.Lock()
	defer c.mu.Unlock()

	account.LastUsedAt = time.Now()
	c.accounts[account.Email] = account
}

// Delete removes an ACME account from the cache
func (c *ACMEAccountCache) Delete(email string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.accounts, email)
}

// Clear removes all accounts from the cache
func (c *ACMEAccountCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.accounts = make(map[string]*ACMEAccount)
}

// Size returns the number of cached accounts
func (c *ACMEAccountCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.accounts)
}

// GetACMEAccountCache returns the ACME account cache from the rate limiter
func (rl *SSLRateLimiter) GetACMEAccountCache() *ACMEAccountCache {
	return rl.acmeAccountCache
}


// RateLimitedSSLService wraps an SSLService with rate limiting
// This provides a convenient way to add rate limiting to certificate operations
type RateLimitedSSLService struct {
	service     SSLService
	rateLimiter *SSLRateLimiter
}

// NewRateLimitedSSLService creates a new rate-limited SSL service wrapper
func NewRateLimitedSSLService(service SSLService, rateLimiter *SSLRateLimiter) *RateLimitedSSLService {
	return &RateLimitedSSLService{
		service:     service,
		rateLimiter: rateLimiter,
	}
}

// ProvisionCertificate provisions a certificate with rate limiting
// Requirements: 6.1, 6.2, 6.3, 6.4 - Rate limited certificate provisioning
func (s *RateLimitedSSLService) ProvisionCertificate(ctx context.Context, domainID string, domainName string) (*ProvisionResult, error) {
	// Check rate limit before provisioning
	if err := s.rateLimiter.AllowRequest(ctx, domainName); err != nil {
		return &ProvisionResult{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	// Check if we should delay the request
	// Requirements: 6.3 - Delay non-urgent requests when approaching rate limit
	if shouldDelay, delay := s.rateLimiter.ShouldDelayRequest(domainName); shouldDelay {
		log.Printf("SSL rate limiter: Delaying request for %s by %v (approaching rate limit)", domainName, delay)
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	// Perform the provisioning
	result, err := s.service.ProvisionCertificate(ctx, domainID, domainName)

	// Record the request for rate limiting
	success := err == nil && result != nil && result.Success
	s.rateLimiter.RecordRequest(domainName, success)

	return result, err
}

// RenewCertificate renews a certificate with rate limiting
// Requirements: 6.1, 6.2, 6.4 - Rate limited certificate renewal
func (s *RateLimitedSSLService) RenewCertificate(ctx context.Context, domainID string) (*ProvisionResult, error) {
	// Get certificate info to get domain name
	info, err := s.service.GetCertificateInfo(ctx, domainID)
	if err != nil {
		return nil, err
	}

	// Check rate limit before renewal
	if err := s.rateLimiter.AllowRequest(ctx, info.DomainName); err != nil {
		return &ProvisionResult{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	// Perform the renewal
	result, err := s.service.RenewCertificate(ctx, domainID)

	// Record the request for rate limiting
	success := err == nil && result != nil && result.Success
	s.rateLimiter.RecordRequest(info.DomainName, success)

	return result, err
}

// GetRateLimitInfo returns rate limit information for a domain
func (s *RateLimitedSSLService) GetRateLimitInfo(domainName string) *DomainRateLimitInfo {
	return s.rateLimiter.GetRateLimitInfo(domainName)
}

// Delegate other methods to the underlying service

func (s *RateLimitedSSLService) GetProvisioningStatus(ctx context.Context, domainID string) (*CertificateInfo, error) {
	return s.service.GetProvisioningStatus(ctx, domainID)
}

func (s *RateLimitedSSLService) GetCertificate(ctx context.Context, domainName string) (*tls.Certificate, error) {
	return s.service.GetCertificate(ctx, domainName)
}

func (s *RateLimitedSSLService) GetCertificateInfo(ctx context.Context, domainID string) (*CertificateInfo, error) {
	return s.service.GetCertificateInfo(ctx, domainID)
}

func (s *RateLimitedSSLService) ListExpiringCertificates(ctx context.Context, withinDays int) ([]*CertificateInfo, error) {
	return s.service.ListExpiringCertificates(ctx, withinDays)
}

func (s *RateLimitedSSLService) CheckAndRenewAll(ctx context.Context) error {
	return s.service.CheckAndRenewAll(ctx)
}

func (s *RateLimitedSSLService) RevokeCertificate(ctx context.Context, domainID string, reason string) error {
	return s.service.RevokeCertificate(ctx, domainID, reason)
}

// RevokeCertificateForDomainDeletion delegates to the underlying service
// Requirements: 7.1 - WHEN domain is deleted, SHALL revoke associated certificate
func (s *RateLimitedSSLService) RevokeCertificateForDomainDeletion(ctx context.Context, domainID string) error {
	return s.service.RevokeCertificateForDomainDeletion(ctx, domainID)
}

// RevokeCertificateForKeyCompromise delegates to the underlying service
// Requirements: 7.3 - WHEN private key is compromised, SHALL immediately revoke and re-provision
func (s *RateLimitedSSLService) RevokeCertificateForKeyCompromise(ctx context.Context, domainID string, reprovision bool) (*ProvisionResult, error) {
	return s.service.RevokeCertificateForKeyCompromise(ctx, domainID, reprovision)
}

// IsRevoked delegates to the underlying service
func (s *RateLimitedSSLService) IsRevoked(ctx context.Context, domainID string) (bool, error) {
	return s.service.IsRevoked(ctx, domainID)
}

// ListRevokedCertificates delegates to the underlying service
func (s *RateLimitedSSLService) ListRevokedCertificates(ctx context.Context) ([]*CertificateInfo, error) {
	return s.service.ListRevokedCertificates(ctx)
}

func (s *RateLimitedSSLService) GetTLSConfig() *tls.Config {
	return s.service.GetTLSConfig()
}

// Ensure RateLimitedSSLService implements SSLService interface
var _ SSLService = (*RateLimitedSSLService)(nil)
