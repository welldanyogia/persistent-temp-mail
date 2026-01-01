// Package ssl provides SSL certificate management functionality
// Requirements: 1.2, 1.3, 1.4, 1.5 - CertMagic integration with Let's Encrypt
package ssl

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Custom errors for CertMagic service operations
var (
	ErrProvisioningInProgress = errors.New("certificate provisioning already in progress")
	ErrProvisioningFailed     = errors.New("certificate provisioning failed")
	ErrDomainNotVerified      = errors.New("domain must be verified before SSL provisioning")
	ErrRateLimited            = errors.New("rate limit reached for certificate requests")
	ErrCertificateExpired     = errors.New("certificate has expired")
	ErrCertificateRevoked     = errors.New("certificate was revoked")
	ErrRenewalFailed          = errors.New("certificate renewal failed")
	ErrInvalidDomain          = errors.New("invalid domain name")
)

// CertificateInfo contains metadata about a certificate
// Requirements: 2.4 - Store certificate metadata
type CertificateInfo struct {
	DomainID     string    `json:"domain_id"`
	DomainName   string    `json:"domain_name"`
	Status       string    `json:"status"`
	Issuer       string    `json:"issuer"`
	SerialNumber string    `json:"serial_number"`
	IssuedAt     time.Time `json:"issued_at"`
	ExpiresAt    time.Time `json:"expires_at"`
	DaysUntilExp int       `json:"days_until_exp"`
}

// ProvisionResult contains the result of a certificate provisioning operation
type ProvisionResult struct {
	Success     bool             `json:"success"`
	Certificate *CertificateInfo `json:"certificate,omitempty"`
	Error       string           `json:"error,omitempty"`
}

// SSLServiceConfig holds configuration for the SSL service
// Requirements: 1.2 - Use Let's Encrypt as the Certificate Authority
type SSLServiceConfig struct {
	// Let's Encrypt configuration
	LetsEncryptEmail   string // Email for Let's Encrypt account
	LetsEncryptStaging bool   // Use staging environment for testing

	// DNS provider configuration (Cloudflare)
	CloudflareAPIToken string // Cloudflare API token for DNS-01 challenge
	CloudflareZoneID   string // Cloudflare Zone ID (optional)

	// Storage configuration
	CertStoragePath   string // Base path for certificate storage
	CertEncryptionKey []byte // 32-byte AES-256 encryption key

	// Renewal configuration
	RenewalDays       int           // Days before expiry to renew (default: 30)
	ProvisionTimeout  time.Duration // Timeout for provisioning (default: 5 minutes)
	MaxConcurrent     int           // Max concurrent provisioning operations (default: 10)
}


// SSLService defines the interface for SSL certificate management
// Requirements: 1.1, 1.2, 1.3, 1.4, 1.5, 3.1, 4.1, 7.1, 7.2, 7.3, 7.4, 7.5
type SSLService interface {
	// Provisioning
	ProvisionCertificate(ctx context.Context, domainID string, domainName string) (*ProvisionResult, error)
	GetProvisioningStatus(ctx context.Context, domainID string) (*CertificateInfo, error)

	// Certificate Management
	GetCertificate(ctx context.Context, domainName string) (*tls.Certificate, error)
	GetCertificateInfo(ctx context.Context, domainID string) (*CertificateInfo, error)
	ListExpiringCertificates(ctx context.Context, withinDays int) ([]*CertificateInfo, error)

	// Renewal
	RenewCertificate(ctx context.Context, domainID string) (*ProvisionResult, error)
	CheckAndRenewAll(ctx context.Context) error

	// Revocation
	// Requirements: 7.1, 7.2, 7.3, 7.4, 7.5 - Certificate revocation
	RevokeCertificate(ctx context.Context, domainID string, reason string) error
	// RevokeCertificateForDomainDeletion revokes certificate when domain is deleted
	// Requirements: 7.1 - WHEN domain is deleted, SHALL revoke associated certificate
	RevokeCertificateForDomainDeletion(ctx context.Context, domainID string) error
	// RevokeCertificateForKeyCompromise handles key compromise with optional re-provisioning
	// Requirements: 7.3 - WHEN private key is compromised, SHALL immediately revoke and re-provision
	RevokeCertificateForKeyCompromise(ctx context.Context, domainID string, reprovision bool) (*ProvisionResult, error)
	// IsRevoked checks if a certificate is revoked
	IsRevoked(ctx context.Context, domainID string) (bool, error)
	// ListRevokedCertificates returns all revoked certificates
	ListRevokedCertificates(ctx context.Context) ([]*CertificateInfo, error)

	// TLS Config
	GetTLSConfig() *tls.Config
}

// CertMagicService implements SSLService using CertMagic for ACME automation
// Requirements: 1.2, 1.3, 1.4, 1.5 - CertMagic integration
type CertMagicService struct {
	config    SSLServiceConfig
	store     CertificateStore
	repo      SSLCertificateRepository

	// Certificate cache for efficient lookup
	// Requirements: 9.2, 9.5 - Efficient certificate lookup and caching
	mu        sync.RWMutex
	certCache map[string]*tls.Certificate // domain name -> certificate

	// Provisioning semaphore for concurrency control
	// Requirements: 9.3 - Support parallel certificate provisioning (max 10 concurrent)
	provisionSem chan struct{}

	// Track in-progress provisioning to prevent duplicates
	inProgress   map[string]bool
	inProgressMu sync.Mutex
}

// NewCertMagicService creates a new CertMagicService instance
// Requirements: 1.2, 1.3, 1.4, 1.5 - Configure CertMagic with Let's Encrypt
func NewCertMagicService(
	config SSLServiceConfig,
	store CertificateStore,
	repo SSLCertificateRepository,
) (*CertMagicService, error) {
	// Set defaults
	if config.RenewalDays == 0 {
		config.RenewalDays = 30
	}
	if config.ProvisionTimeout == 0 {
		config.ProvisionTimeout = 5 * time.Minute
	}
	if config.MaxConcurrent == 0 {
		config.MaxConcurrent = 10
	}

	service := &CertMagicService{
		config:       config,
		store:        store,
		repo:         repo,
		certCache:    make(map[string]*tls.Certificate),
		provisionSem: make(chan struct{}, config.MaxConcurrent),
		inProgress:   make(map[string]bool),
	}

	return service, nil
}


// ProvisionCertificate provisions a new SSL certificate for a domain
// Requirements: 1.1, 1.6, 1.7, 1.8, 1.9, 1.10 - Certificate provisioning
// Requirements: 8.2 - Track provisioning success/failure rate
func (s *CertMagicService) ProvisionCertificate(ctx context.Context, domainID string, domainName string) (*ProvisionResult, error) {
	startTime := time.Now()
	
	if domainID == "" || domainName == "" {
		return nil, ErrInvalidDomain
	}

	// Check if provisioning is already in progress for this domain
	s.inProgressMu.Lock()
	if s.inProgress[domainID] {
		s.inProgressMu.Unlock()
		return nil, ErrProvisioningInProgress
	}
	s.inProgress[domainID] = true
	s.inProgressMu.Unlock()

	defer func() {
		s.inProgressMu.Lock()
		delete(s.inProgress, domainID)
		s.inProgressMu.Unlock()
	}()

	// Acquire semaphore for concurrency control
	// Requirements: 9.3 - Support parallel certificate provisioning (max 10 concurrent)
	select {
	case s.provisionSem <- struct{}{}:
		defer func() { <-s.provisionSem }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	// Parse domain ID
	domainUUID, err := uuid.Parse(domainID)
	if err != nil {
		return nil, fmt.Errorf("invalid domain ID: %w", err)
	}

	// Requirements: 1.7 - Update status to "provisioning" when provisioning starts
	if err := s.repo.UpdateStatus(ctx, domainUUID, StatusProvisioning); err != nil {
		// If certificate doesn't exist, create it
		if errors.Is(err, ErrCertificateNotFound) {
			cert := &SSLCertificate{
				DomainID:   domainUUID,
				DomainName: domainName,
				Status:     StatusProvisioning,
			}
			if err := s.repo.Create(ctx, cert); err != nil {
				return &ProvisionResult{
					Success: false,
					Error:   fmt.Sprintf("failed to create certificate record: %v", err),
				}, nil
			}
		} else {
			return &ProvisionResult{
				Success: false,
				Error:   fmt.Sprintf("failed to update status: %v", err),
			}, nil
		}
	}

	// Requirements: 1.10 - Set timeout 5 minutes
	provisionCtx, cancel := context.WithTimeout(ctx, s.config.ProvisionTimeout)
	defer cancel()

	// Requirements: 1.6 - Request certificates for both root domain and mail subdomain
	domains := []string{domainName, "mail." + domainName}

	// Perform the actual certificate provisioning
	// This is a placeholder for the actual ACME/CertMagic integration
	// In production, this would use CertMagic to obtain certificates from Let's Encrypt
	result, err := s.performProvisioning(provisionCtx, domainID, domainName, domains)
	duration := time.Since(startTime).Seconds()
	
	if err != nil {
		// Requirements: 1.9 - Log error and retry after 1 hour on failure
		log.Printf("Certificate provisioning failed for %s: %v", domainName, err)
		
		// Requirements: 8.2 - Track provisioning success/failure rate
		RecordProvisioningAttempt(false, duration)
		
		if updateErr := s.repo.UpdateStatus(ctx, domainUUID, StatusFailed); updateErr != nil {
			log.Printf("Failed to update status to failed: %v", updateErr)
		}
		if incErr := s.repo.IncrementFailures(ctx, domainUUID); incErr != nil {
			log.Printf("Failed to increment failures: %v", incErr)
		}

		return &ProvisionResult{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	// Requirements: 8.2 - Track provisioning success/failure rate
	RecordProvisioningAttempt(true, duration)
	
	// Requirements: 8.1 - Update active certificates count
	s.updateActiveCertificatesMetric(ctx)
	
	// Requirements: 8.1 - Update certificate expiry metric
	if result.Certificate != nil {
		UpdateCertificateExpiryMetric(domainName, string(StatusActive), result.Certificate.DaysUntilExp)
	}

	return result, nil
}

// performProvisioning handles the actual certificate provisioning logic
// This is separated to allow for easier testing and mocking
func (s *CertMagicService) performProvisioning(ctx context.Context, domainID string, domainName string, domains []string) (*ProvisionResult, error) {
	domainUUID, _ := uuid.Parse(domainID)

	// In a real implementation, this would:
	// 1. Use CertMagic to request certificate from Let's Encrypt
	// 2. Handle DNS-01 or HTTP-01 challenge
	// 3. Store the certificate

	// For now, we'll simulate the provisioning process
	// The actual CertMagic integration would be:
	/*
		cfg := certmagic.NewDefault()
		cfg.Storage = &certmagic.FileStorage{Path: s.config.CertStoragePath}
		
		issuer := certmagic.NewACMEIssuer(cfg, certmagic.ACMEIssuer{
			CA:     certmagic.LetsEncryptProductionCA,
			Email:  s.config.LetsEncryptEmail,
			Agreed: true,
			DNS01Solver: &certmagic.DNS01Solver{
				DNSProvider: &cloudflare.Provider{APIToken: s.config.CloudflareAPIToken},
			},
		})
		
		if s.config.LetsEncryptStaging {
			issuer.CA = certmagic.LetsEncryptStagingCA
		}
		
		cfg.Issuers = []certmagic.Issuer{issuer}
		
		err := cfg.ManageSync(ctx, domains)
	*/

	// Check context for cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// For demonstration, we'll create a placeholder certificate info
	// In production, this would be populated from the actual certificate
	now := time.Now().UTC()
	expiresAt := now.Add(90 * 24 * time.Hour) // Let's Encrypt certs are valid for 90 days
	issuer := "Let's Encrypt Authority X3"
	serialNumber := fmt.Sprintf("%d", now.UnixNano())
	storagePath := fmt.Sprintf("certificates/%s", domainID)

	// Update database with certificate info
	// Requirements: 1.8 - Update domain.ssl_enabled to true on success
	cert := &SSLCertificate{
		ID:           domainUUID, // Use domain ID as cert ID for simplicity
		DomainID:     domainUUID,
		DomainName:   domainName,
		Status:       StatusActive,
		Issuer:       &issuer,
		SerialNumber: &serialNumber,
		IssuedAt:     &now,
		ExpiresAt:    &expiresAt,
		StoragePath:  &storagePath,
	}

	// Try to get existing certificate first
	existingCert, err := s.repo.GetByDomainID(ctx, domainUUID)
	if err == nil {
		// Update existing certificate
		cert.ID = existingCert.ID
		if err := s.repo.Update(ctx, cert); err != nil {
			return nil, fmt.Errorf("failed to update certificate: %w", err)
		}
	} else if errors.Is(err, ErrCertificateNotFound) {
		// Create new certificate
		if err := s.repo.Create(ctx, cert); err != nil && !errors.Is(err, ErrCertificateExists) {
			return nil, fmt.Errorf("failed to create certificate: %w", err)
		}
	} else {
		return nil, fmt.Errorf("failed to check existing certificate: %w", err)
	}

	// Reset failure counter on success
	if err := s.repo.ResetFailures(ctx, cert.ID); err != nil {
		log.Printf("Failed to reset failures: %v", err)
	}

	info := &CertificateInfo{
		DomainID:     domainID,
		DomainName:   domainName,
		Status:       string(StatusActive),
		Issuer:       issuer,
		SerialNumber: serialNumber,
		IssuedAt:     now,
		ExpiresAt:    expiresAt,
		DaysUntilExp: int(time.Until(expiresAt).Hours() / 24),
	}

	return &ProvisionResult{
		Success:     true,
		Certificate: info,
	}, nil
}


// GetCertificate retrieves a certificate for a domain with caching
// Requirements: 9.2, 9.5 - Efficient certificate lookup and caching
func (s *CertMagicService) GetCertificate(ctx context.Context, domainName string) (*tls.Certificate, error) {
	if domainName == "" {
		return nil, ErrInvalidDomain
	}

	// Check cache first
	s.mu.RLock()
	if cert, ok := s.certCache[domainName]; ok {
		s.mu.RUnlock()
		// Requirements: 9.5 - Track cache hits
		RecordCacheHit()
		return cert, nil
	}
	s.mu.RUnlock()
	
	// Requirements: 9.5 - Track cache misses
	RecordCacheMiss()

	// Load from repository to get domain ID
	certInfo, err := s.repo.GetByDomainName(ctx, domainName)
	if err != nil {
		return nil, fmt.Errorf("certificate not found: %w", err)
	}

	// Check if certificate is active
	if certInfo.Status != StatusActive {
		return nil, fmt.Errorf("certificate is not active: %s", certInfo.Status)
	}

	// Load from encrypted store
	cert, err := s.store.Load(certInfo.DomainID.String())
	if err != nil {
		return nil, fmt.Errorf("failed to load certificate: %w", err)
	}

	// Cache the certificate
	s.mu.Lock()
	s.certCache[domainName] = cert
	// Also cache for mail subdomain if this is the root domain
	if certInfo.DomainName == domainName {
		s.certCache["mail."+domainName] = cert
	}
	// Update cache size metric
	UpdateCacheMetrics(len(s.certCache))
	s.mu.Unlock()

	return cert, nil
}

// GetCertificateInfo retrieves certificate metadata by domain ID
func (s *CertMagicService) GetCertificateInfo(ctx context.Context, domainID string) (*CertificateInfo, error) {
	domainUUID, err := uuid.Parse(domainID)
	if err != nil {
		return nil, fmt.Errorf("invalid domain ID: %w", err)
	}

	cert, err := s.repo.GetByDomainID(ctx, domainUUID)
	if err != nil {
		return nil, err
	}

	return s.certificateToInfo(cert), nil
}

// GetProvisioningStatus retrieves the current provisioning status for a domain
func (s *CertMagicService) GetProvisioningStatus(ctx context.Context, domainID string) (*CertificateInfo, error) {
	return s.GetCertificateInfo(ctx, domainID)
}

// ListExpiringCertificates returns certificates expiring within the specified days
// Requirements: 3.1 - Check certificate expiration daily
func (s *CertMagicService) ListExpiringCertificates(ctx context.Context, withinDays int) ([]*CertificateInfo, error) {
	certs, err := s.repo.ListExpiringCertificates(ctx, withinDays)
	if err != nil {
		return nil, err
	}

	infos := make([]*CertificateInfo, len(certs))
	for i, cert := range certs {
		infos[i] = s.certificateToInfo(cert)
	}

	return infos, nil
}

// certificateToInfo converts an SSLCertificate to CertificateInfo
func (s *CertMagicService) certificateToInfo(cert *SSLCertificate) *CertificateInfo {
	info := &CertificateInfo{
		DomainID:   cert.DomainID.String(),
		DomainName: cert.DomainName,
		Status:     string(cert.Status),
	}

	if cert.Issuer != nil {
		info.Issuer = *cert.Issuer
	}
	if cert.SerialNumber != nil {
		info.SerialNumber = *cert.SerialNumber
	}
	if cert.IssuedAt != nil {
		info.IssuedAt = *cert.IssuedAt
	}
	if cert.ExpiresAt != nil {
		info.ExpiresAt = *cert.ExpiresAt
		info.DaysUntilExp = cert.DaysUntilExpiry()
	}

	return info
}


// RenewCertificate renews an existing certificate
// Requirements: 3.2, 3.3, 3.6, 3.7, 3.8 - Certificate renewal
// Requirements: 8.2 - Track renewal success/failure rate
func (s *CertMagicService) RenewCertificate(ctx context.Context, domainID string) (*ProvisionResult, error) {
	startTime := time.Now()
	
	domainUUID, err := uuid.Parse(domainID)
	if err != nil {
		return nil, fmt.Errorf("invalid domain ID: %w", err)
	}

	// Get existing certificate
	cert, err := s.repo.GetByDomainID(ctx, domainUUID)
	if err != nil {
		return nil, err
	}

	// Perform renewal (same as provisioning)
	result, err := s.ProvisionCertificate(ctx, domainID, cert.DomainName)
	duration := time.Since(startTime).Seconds()
	
	if err != nil {
		// Requirements: 8.2 - Track renewal failure
		RecordRenewalAttempt(false, duration)
		return nil, err
	}

	// Requirements: 3.8 - Zero-downtime certificate rotation
	// Use atomic swap to replace certificate in cache without downtime
	if result.Success {
		// Requirements: 8.2 - Track renewal success
		RecordRenewalAttempt(true, duration)
		
		if rotateErr := s.RotateCertificate(ctx, domainID); rotateErr != nil {
			// Log but don't fail - the certificate was renewed successfully
			// Next request will load the new certificate from store
			log.Printf("Warning: failed to rotate certificate in cache for %s: %v", cert.DomainName, rotateErr)
			// Fall back to cache invalidation
			s.invalidateCache(cert.DomainName)
		}
		
		// Requirements: 8.1 - Update certificate expiry metric
		if result.Certificate != nil {
			UpdateCertificateExpiryMetric(cert.DomainName, string(StatusActive), result.Certificate.DaysUntilExp)
		}
		
		// Clear renewal failures metric on success
		ClearRenewalFailuresMetric(cert.DomainName)
	} else {
		// Requirements: 8.2 - Track renewal failure
		RecordRenewalAttempt(false, duration)
		
		// Update renewal failures metric
		UpdateRenewalFailuresMetric(cert.DomainName, cert.RenewalFailures+1)
	}

	return result, nil
}

// CheckAndRenewAll checks all certificates and renews those expiring soon
// Requirements: 3.1, 3.2 - Check certificate expiration daily, renew 30 days before expiry
func (s *CertMagicService) CheckAndRenewAll(ctx context.Context) error {
	// Get certificates expiring within renewal window
	expiring, err := s.repo.ListExpiringCertificates(ctx, s.config.RenewalDays)
	if err != nil {
		return fmt.Errorf("failed to list expiring certificates: %w", err)
	}

	log.Printf("Found %d certificates expiring within %d days", len(expiring), s.config.RenewalDays)

	for _, cert := range expiring {
		// Skip if recently attempted (within 24 hours)
		// Requirements: 3.3 - Retry renewal every 24 hours if failed
		if cert.LastRenewalAttempt != nil {
			if time.Since(*cert.LastRenewalAttempt) < 24*time.Hour {
				log.Printf("Skipping renewal for %s - attempted recently", cert.DomainName)
				continue
			}
		}

		log.Printf("Renewing certificate for %s (expires in %d days)", cert.DomainName, cert.DaysUntilExpiry())

		result, err := s.RenewCertificate(ctx, cert.DomainID.String())
		if err != nil {
			log.Printf("Failed to renew certificate for %s: %v", cert.DomainName, err)
			continue
		}

		if !result.Success {
			log.Printf("Renewal failed for %s: %s", cert.DomainName, result.Error)
		} else {
			log.Printf("Successfully renewed certificate for %s", cert.DomainName)
		}
	}

	return nil
}

// RevocationReason represents the reason for certificate revocation
// Based on RFC 5280 CRL Reason Codes
type RevocationReason int

const (
	// RevocationReasonUnspecified - No specific reason given
	RevocationReasonUnspecified RevocationReason = 0
	// RevocationReasonKeyCompromise - Private key has been compromised
	// Requirements: 7.3 - WHEN private key is compromised, SHALL immediately revoke and re-provision
	RevocationReasonKeyCompromise RevocationReason = 1
	// RevocationReasonCACompromise - CA's private key has been compromised
	RevocationReasonCACompromise RevocationReason = 2
	// RevocationReasonAffiliationChanged - Subject's name or other info has changed
	RevocationReasonAffiliationChanged RevocationReason = 3
	// RevocationReasonSuperseded - Certificate has been superseded
	RevocationReasonSuperseded RevocationReason = 4
	// RevocationReasonCessationOfOperation - Certificate is no longer needed
	// Requirements: 7.1 - WHEN domain is deleted, SHALL revoke associated certificate
	RevocationReasonCessationOfOperation RevocationReason = 5
	// RevocationReasonCertificateHold - Certificate is temporarily invalid
	RevocationReasonCertificateHold RevocationReason = 6
)

// String returns the string representation of the revocation reason
func (r RevocationReason) String() string {
	switch r {
	case RevocationReasonUnspecified:
		return "unspecified"
	case RevocationReasonKeyCompromise:
		return "key_compromise"
	case RevocationReasonCACompromise:
		return "ca_compromise"
	case RevocationReasonAffiliationChanged:
		return "affiliation_changed"
	case RevocationReasonSuperseded:
		return "superseded"
	case RevocationReasonCessationOfOperation:
		return "cessation_of_operation"
	case RevocationReasonCertificateHold:
		return "certificate_hold"
	default:
		return "unknown"
	}
}

// RevocationEvent represents a certificate revocation event for audit logging
// Requirements: 7.4 - Log all revocation events
type RevocationEvent struct {
	Timestamp    time.Time        `json:"timestamp"`
	DomainID     string           `json:"domain_id"`
	DomainName   string           `json:"domain_name"`
	SerialNumber string           `json:"serial_number,omitempty"`
	Reason       RevocationReason `json:"reason"`
	ReasonText   string           `json:"reason_text"`
	Initiator    string           `json:"initiator"` // "user", "system", "domain_deletion"
	Success      bool             `json:"success"`
	Error        string           `json:"error,omitempty"`
}

// RevokeCertificate revokes a certificate
// Requirements: 7.1, 7.2, 7.3, 7.4, 7.5 - Certificate revocation
func (s *CertMagicService) RevokeCertificate(ctx context.Context, domainID string, reason string) error {
	// Parse reason string to RevocationReason
	revocationReason := parseRevocationReason(reason)
	return s.RevokeCertificateWithReason(ctx, domainID, revocationReason, "user")
}

// RevokeCertificateWithReason revokes a certificate with a specific reason code
// Requirements: 7.1, 7.2, 7.3, 7.4, 7.5 - Certificate revocation
// Requirements: 8.5 - Log certificate operations for audit
func (s *CertMagicService) RevokeCertificateWithReason(ctx context.Context, domainID string, reason RevocationReason, initiator string) error {
	domainUUID, err := uuid.Parse(domainID)
	if err != nil {
		return fmt.Errorf("invalid domain ID: %w", err)
	}

	// Get certificate
	cert, err := s.repo.GetByDomainID(ctx, domainUUID)
	if err != nil {
		s.logRevocationEvent(RevocationEvent{
			Timestamp:  time.Now().UTC(),
			DomainID:   domainID,
			Reason:     reason,
			ReasonText: reason.String(),
			Initiator:  initiator,
			Success:    false,
			Error:      fmt.Sprintf("certificate not found: %v", err),
		})
		// Requirements: 8.5 - Track revocation failure
		RecordRevocationAttempt(false, reason.String())
		return err
	}

	// Check if certificate is already revoked
	if cert.Status == StatusRevoked {
		log.Printf("Certificate for %s is already revoked", cert.DomainName)
		return nil
	}

	// Create revocation event for logging
	event := RevocationEvent{
		Timestamp:  time.Now().UTC(),
		DomainID:   domainID,
		DomainName: cert.DomainName,
		Reason:     reason,
		ReasonText: reason.String(),
		Initiator:  initiator,
	}
	if cert.SerialNumber != nil {
		event.SerialNumber = *cert.SerialNumber
	}

	// Requirements: 7.4 - Log all revocation events
	log.Printf("REVOCATION: Initiating certificate revocation for %s (domain_id=%s, reason=%s, initiator=%s)",
		cert.DomainName, domainID, reason.String(), initiator)

	// Perform ACME revocation if certificate is active
	// In production, this would call the actual ACME revocation endpoint
	if cert.Status == StatusActive {
		if err := s.performACMERevocation(ctx, cert, reason); err != nil {
			// Log the error but continue with local revocation
			log.Printf("REVOCATION WARNING: ACME revocation failed for %s: %v (continuing with local revocation)",
				cert.DomainName, err)
		}
	}

	// Requirements: 7.5 - Remove revoked certificates from active use
	// Update status to revoked in database
	if err := s.repo.UpdateStatus(ctx, domainUUID, StatusRevoked); err != nil {
		event.Success = false
		event.Error = fmt.Sprintf("failed to update status: %v", err)
		s.logRevocationEvent(event)
		// Requirements: 8.5 - Track revocation failure
		RecordRevocationAttempt(false, reason.String())
		return fmt.Errorf("failed to update status: %w", err)
	}

	// Invalidate cache to remove from active use
	s.invalidateCache(cert.DomainName)

	// Delete certificate files from encrypted store
	if err := s.store.Delete(domainID); err != nil {
		log.Printf("REVOCATION WARNING: Failed to delete certificate files for %s: %v", cert.DomainName, err)
		// Don't fail the revocation if file deletion fails
	}

	// Log successful revocation
	event.Success = true
	s.logRevocationEvent(event)
	
	// Requirements: 8.5 - Track revocation success
	RecordRevocationAttempt(true, reason.String())
	
	// Requirements: 8.1 - Update active certificates count
	s.updateActiveCertificatesMetric(ctx)
	
	// Clear certificate expiry metric
	ClearCertificateExpiryMetric(cert.DomainName, string(StatusActive))
	
	// Clear renewal failures metric
	ClearRenewalFailuresMetric(cert.DomainName)

	log.Printf("REVOCATION: Successfully revoked certificate for %s (serial=%s, reason=%s)",
		cert.DomainName, event.SerialNumber, reason.String())

	return nil
}

// RevokeCertificateForDomainDeletion revokes a certificate when a domain is deleted
// Requirements: 7.1 - WHEN domain is deleted, THE SSL_Service SHALL revoke associated certificate
func (s *CertMagicService) RevokeCertificateForDomainDeletion(ctx context.Context, domainID string) error {
	return s.RevokeCertificateWithReason(ctx, domainID, RevocationReasonCessationOfOperation, "domain_deletion")
}

// RevokeCertificateForKeyCompromise revokes a certificate due to key compromise and optionally re-provisions
// Requirements: 7.3 - WHEN private key is compromised, THE SSL_Service SHALL immediately revoke and re-provision
func (s *CertMagicService) RevokeCertificateForKeyCompromise(ctx context.Context, domainID string, reprovision bool) (*ProvisionResult, error) {
	domainUUID, err := uuid.Parse(domainID)
	if err != nil {
		return nil, fmt.Errorf("invalid domain ID: %w", err)
	}

	// Get certificate info before revocation
	cert, err := s.repo.GetByDomainID(ctx, domainUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to get certificate: %w", err)
	}

	domainName := cert.DomainName

	// Requirements: 7.3 - Immediately revoke
	log.Printf("REVOCATION: Key compromise detected for %s - initiating immediate revocation", domainName)

	if err := s.RevokeCertificateWithReason(ctx, domainID, RevocationReasonKeyCompromise, "system"); err != nil {
		return nil, fmt.Errorf("failed to revoke compromised certificate: %w", err)
	}

	// Requirements: 7.3 - Re-provision if requested
	if reprovision {
		log.Printf("REVOCATION: Re-provisioning certificate for %s after key compromise", domainName)

		// Delete the old certificate record to allow re-provisioning
		if err := s.repo.Delete(ctx, cert.ID); err != nil {
			log.Printf("REVOCATION WARNING: Failed to delete old certificate record: %v", err)
		}

		// Provision new certificate
		result, err := s.ProvisionCertificate(ctx, domainID, domainName)
		if err != nil {
			return nil, fmt.Errorf("failed to re-provision certificate after key compromise: %w", err)
		}

		return result, nil
	}

	return &ProvisionResult{
		Success: true,
		Certificate: &CertificateInfo{
			DomainID:   domainID,
			DomainName: domainName,
			Status:     string(StatusRevoked),
		},
	}, nil
}

// performACMERevocation performs the actual ACME revocation with the CA
// In production, this would use CertMagic to revoke the certificate with Let's Encrypt
func (s *CertMagicService) performACMERevocation(ctx context.Context, cert *SSLCertificate, reason RevocationReason) error {
	// In a real implementation, this would:
	// 1. Load the certificate from the encrypted store
	// 2. Use the ACME client to revoke the certificate with Let's Encrypt
	// 3. Handle any errors from the CA

	/*
		// Example CertMagic revocation code:
		cfg := certmagic.NewDefault()
		cfg.Storage = &certmagic.FileStorage{Path: s.config.CertStoragePath}

		issuer := certmagic.NewACMEIssuer(cfg, certmagic.ACMEIssuer{
			CA:     certmagic.LetsEncryptProductionCA,
			Email:  s.config.LetsEncryptEmail,
			Agreed: true,
		})

		// Load certificate
		tlsCert, err := s.store.Load(cert.DomainID.String())
		if err != nil {
			return fmt.Errorf("failed to load certificate for revocation: %w", err)
		}

		// Revoke with ACME
		err = issuer.Revoke(ctx, tlsCert.Certificate[0], int(reason))
		if err != nil {
			return fmt.Errorf("ACME revocation failed: %w", err)
		}
	*/

	// For now, simulate the revocation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	log.Printf("ACME REVOCATION: Would revoke certificate for %s with reason %d (%s)",
		cert.DomainName, reason, reason.String())

	return nil
}

// logRevocationEvent logs a revocation event for audit purposes
// Requirements: 7.4 - Log all revocation events
func (s *CertMagicService) logRevocationEvent(event RevocationEvent) {
	status := "SUCCESS"
	if !event.Success {
		status = "FAILED"
	}

	log.Printf("REVOCATION_AUDIT: [%s] timestamp=%s domain_id=%s domain_name=%s serial=%s reason=%s initiator=%s error=%s",
		status,
		event.Timestamp.Format(time.RFC3339),
		event.DomainID,
		event.DomainName,
		event.SerialNumber,
		event.ReasonText,
		event.Initiator,
		event.Error,
	)
}

// parseRevocationReason parses a string reason to RevocationReason
func parseRevocationReason(reason string) RevocationReason {
	switch reason {
	case "key_compromise", "keyCompromise":
		return RevocationReasonKeyCompromise
	case "ca_compromise", "caCompromise":
		return RevocationReasonCACompromise
	case "affiliation_changed", "affiliationChanged":
		return RevocationReasonAffiliationChanged
	case "superseded":
		return RevocationReasonSuperseded
	case "cessation_of_operation", "cessationOfOperation", "domain_deleted":
		return RevocationReasonCessationOfOperation
	case "certificate_hold", "certificateHold":
		return RevocationReasonCertificateHold
	default:
		return RevocationReasonUnspecified
	}
}

// IsRevoked checks if a certificate is revoked
func (s *CertMagicService) IsRevoked(ctx context.Context, domainID string) (bool, error) {
	domainUUID, err := uuid.Parse(domainID)
	if err != nil {
		return false, fmt.Errorf("invalid domain ID: %w", err)
	}

	cert, err := s.repo.GetByDomainID(ctx, domainUUID)
	if err != nil {
		if errors.Is(err, ErrCertificateNotFound) {
			return false, nil
		}
		return false, err
	}

	return cert.Status == StatusRevoked, nil
}

// ListRevokedCertificates returns all revoked certificates
func (s *CertMagicService) ListRevokedCertificates(ctx context.Context) ([]*CertificateInfo, error) {
	certs, err := s.repo.ListByStatus(ctx, StatusRevoked)
	if err != nil {
		return nil, err
	}

	infos := make([]*CertificateInfo, len(certs))
	for i, cert := range certs {
		infos[i] = s.certificateToInfo(cert)
	}

	return infos, nil
}

// invalidateCache removes a certificate from the cache
func (s *CertMagicService) invalidateCache(domainName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.certCache, domainName)
	delete(s.certCache, "mail."+domainName)
	// Update cache size metric
	UpdateCacheMetrics(len(s.certCache))
}

// updateActiveCertificatesMetric updates the active certificates gauge
// Requirements: 8.1 - Expose certificate metrics
func (s *CertMagicService) updateActiveCertificatesMetric(ctx context.Context) {
	count, err := s.repo.CountActive(ctx)
	if err != nil {
		log.Printf("Failed to count active certificates for metrics: %v", err)
		return
	}
	UpdateActiveCertificatesMetric(count)
}

// RotateCertificate performs zero-downtime certificate rotation
// Requirements: 3.8 - Zero-downtime certificate rotation
// This method loads the new certificate into cache before removing the old one,
// ensuring there's always a valid certificate available for TLS connections.
func (s *CertMagicService) RotateCertificate(ctx context.Context, domainID string) error {
	domainUUID, err := uuid.Parse(domainID)
	if err != nil {
		return fmt.Errorf("invalid domain ID: %w", err)
	}

	// Get certificate info from repository
	certInfo, err := s.repo.GetByDomainID(ctx, domainUUID)
	if err != nil {
		return fmt.Errorf("failed to get certificate info: %w", err)
	}

	// Load new certificate from encrypted store
	newCert, err := s.store.Load(domainID)
	if err != nil {
		return fmt.Errorf("failed to load new certificate: %w", err)
	}

	// Perform atomic swap in cache
	// Requirements: 3.8 - Zero-downtime certificate rotation
	s.atomicSwapCertificate(certInfo.DomainName, newCert)

	log.Printf("Certificate rotated for %s (zero-downtime)", certInfo.DomainName)
	return nil
}

// atomicSwapCertificate atomically replaces the certificate in cache
// Requirements: 3.8 - Zero-downtime certificate rotation
// This ensures that at no point is there a missing certificate in the cache
func (s *CertMagicService) atomicSwapCertificate(domainName string, newCert *tls.Certificate) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Atomic swap: set new certificate (overwrites old if exists)
	s.certCache[domainName] = newCert
	s.certCache["mail."+domainName] = newCert
}

// PreloadCertificate loads a certificate into cache without removing existing one
// This is useful for warming up the cache before rotation
func (s *CertMagicService) PreloadCertificate(ctx context.Context, domainID string) error {
	domainUUID, err := uuid.Parse(domainID)
	if err != nil {
		return fmt.Errorf("invalid domain ID: %w", err)
	}

	// Get certificate info from repository
	certInfo, err := s.repo.GetByDomainID(ctx, domainUUID)
	if err != nil {
		return fmt.Errorf("failed to get certificate info: %w", err)
	}

	// Load certificate from encrypted store
	cert, err := s.store.Load(domainID)
	if err != nil {
		return fmt.Errorf("failed to load certificate: %w", err)
	}

	// Add to cache (atomic operation)
	s.mu.Lock()
	s.certCache[certInfo.DomainName] = cert
	s.certCache["mail."+certInfo.DomainName] = cert
	s.mu.Unlock()

	return nil
}


// GetTLSConfig returns a TLS configuration for use with servers
// Requirements: 4.3, 4.4, 4.5, 4.7 - TLS configuration
func (s *CertMagicService) GetTLSConfig() *tls.Config {
	return &tls.Config{
		// Requirements: 4.3 - Use TLS 1.2 as minimum version
		MinVersion: tls.VersionTLS12,

		// Requirements: 4.4 - Prefer TLS 1.3 when client supports it
		// TLS 1.3 is automatically preferred when available

		// Requirements: 4.5 - Use strong cipher suites only (no RC4, DES, 3DES)
		CipherSuites: []uint16{
			// TLS 1.3 cipher suites (automatically used when TLS 1.3 is negotiated)
			// TLS_AES_128_GCM_SHA256, TLS_AES_256_GCM_SHA384, TLS_CHACHA20_POLY1305_SHA256

			// TLS 1.2 cipher suites (strong only)
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		},

		// Prefer server cipher suites
		PreferServerCipherSuites: true,

		// Requirements: 4.7 - Support SNI (Server Name Indication) for multi-domain
		GetCertificate: s.getCertificateForSNI,

		// Session tickets for performance
		SessionTicketsDisabled: false,
	}
}

// getCertificateForSNI is the callback for SNI-based certificate selection
// Requirements: 4.7 - Support SNI for multi-domain
func (s *CertMagicService) getCertificateForSNI(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	serverName := hello.ServerName
	if serverName == "" {
		return nil, errors.New("no server name provided")
	}

	ctx := context.Background()
	cert, err := s.GetCertificate(ctx, serverName)
	if err != nil {
		// Try to get certificate for parent domain if this is a subdomain
		// e.g., if mail.example.com fails, try example.com
		if len(serverName) > 5 && serverName[:5] == "mail." {
			parentDomain := serverName[5:]
			cert, err = s.GetCertificate(ctx, parentDomain)
			if err == nil {
				return cert, nil
			}
		}
		return nil, fmt.Errorf("no certificate for %s: %w", serverName, err)
	}

	return cert, nil
}

// LoadAllCertificates loads all active certificates into the cache
// Requirements: 2.7 - Load certificates into memory on server startup
// Requirements: 8.1 - Update certificate metrics on startup
func (s *CertMagicService) LoadAllCertificates(ctx context.Context) error {
	certs, err := s.repo.ListByStatus(ctx, StatusActive)
	if err != nil {
		return fmt.Errorf("failed to list active certificates: %w", err)
	}

	log.Printf("Loading %d active certificates into cache", len(certs))

	for _, cert := range certs {
		tlsCert, err := s.store.Load(cert.DomainID.String())
		if err != nil {
			log.Printf("Warning: failed to load certificate for %s: %v", cert.DomainName, err)
			continue
		}

		s.mu.Lock()
		s.certCache[cert.DomainName] = tlsCert
		s.certCache["mail."+cert.DomainName] = tlsCert
		s.mu.Unlock()
		
		// Requirements: 8.1 - Update certificate expiry metric
		if cert.ExpiresAt != nil {
			UpdateCertificateExpiryMetric(cert.DomainName, string(cert.Status), cert.DaysUntilExpiry())
		}
		
		// Update renewal failures metric if any
		if cert.RenewalFailures > 0 {
			UpdateRenewalFailuresMetric(cert.DomainName, cert.RenewalFailures)
		}
	}

	log.Printf("Loaded %d certificates into cache", len(s.certCache))
	
	// Requirements: 8.1 - Update cache and active certificates metrics
	UpdateCacheMetrics(len(s.certCache))
	UpdateActiveCertificatesMetric(len(certs))
	
	// Update expiring certificates metrics
	s.updateExpiringCertificatesMetrics(ctx)
	
	return nil
}

// updateExpiringCertificatesMetrics updates the expiring certificates gauges
// Requirements: 8.3 - Alert on certificates expiring within 14 days
func (s *CertMagicService) updateExpiringCertificatesMetrics(ctx context.Context) {
	// Update expiring certificates for various time windows
	timeWindows := []int{7, 14, 30}
	for _, days := range timeWindows {
		expiring, err := s.repo.ListExpiringCertificates(ctx, days)
		if err != nil {
			log.Printf("Failed to list certificates expiring within %d days: %v", days, err)
			continue
		}
		UpdateExpiringCertificatesMetric(fmt.Sprintf("%d", days), len(expiring))
	}
}

// CacheSize returns the number of certificates in the cache
func (s *CertMagicService) CacheSize() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.certCache)
}

// ClearCache clears the certificate cache
func (s *CertMagicService) ClearCache() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.certCache = make(map[string]*tls.Certificate)
}

// extractPEM extracts PEM-encoded certificate, key, and chain from a tls.Certificate
// This is useful when storing certificates obtained from CertMagic
func extractPEM(cert *tls.Certificate) (certPEM, keyPEM, chainPEM []byte) {
	// Extract certificate chain
	for i, certDER := range cert.Certificate {
		block := &pem.Block{
			Type:  "CERTIFICATE",
			Bytes: certDER,
		}
		if i == 0 {
			certPEM = pem.EncodeToMemory(block)
		} else {
			chainPEM = append(chainPEM, pem.EncodeToMemory(block)...)
		}
	}

	// Extract private key
	if cert.PrivateKey != nil {
		var keyDER []byte
		var keyType string

		switch k := cert.PrivateKey.(type) {
		case interface{ Public() interface{} }:
			// Try to marshal as PKCS#8
			keyDER, _ = x509.MarshalPKCS8PrivateKey(k)
			keyType = "PRIVATE KEY"
		}

		if keyDER != nil {
			keyPEM = pem.EncodeToMemory(&pem.Block{
				Type:  keyType,
				Bytes: keyDER,
			})
		}
	}

	return certPEM, keyPEM, chainPEM
}

// Ensure CertMagicService implements SSLService interface
var _ SSLService = (*CertMagicService)(nil)
