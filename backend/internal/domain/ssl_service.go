package domain

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

// SSLServiceInterface defines the interface for SSL certificate management
// This interface can be implemented by the full ssl.CertMagicService
// Requirements: 1.1, 1.7 - SSL provisioning after domain verification
type SSLServiceInterface interface {
	// ProvisionCertificate initiates SSL certificate provisioning for a domain
	// Requirements: 1.1, 1.7 - Auto-provision after domain verification
	ProvisionCertificate(ctx context.Context, domainID uuid.UUID, domainName string) error

	// RevokeCertificate revokes an SSL certificate for a domain
	// Requirements: 7.1 - Revoke certificate when domain is deleted
	RevokeCertificate(ctx context.Context, domainID uuid.UUID, domainName string) error

	// GetSSLStatus returns the SSL status for a domain
	GetSSLStatus(ctx context.Context, domainID uuid.UUID) (*SSLStatusInfo, error)

	// IsProvisioning checks if a domain is currently being provisioned
	IsProvisioning(domainName string) bool
}

// SSLStatusInfo represents the SSL certificate status for a domain
type SSLStatusInfo struct {
	Status       string     `json:"status"` // "pending", "provisioning", "active", "expired", "failed", "revoked"
	Enabled      bool       `json:"enabled"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	Provisioning bool       `json:"provisioning"`
}

// SSLService handles SSL certificate management
// Uses CertMagic for Let's Encrypt integration
// This is a simple implementation that can be replaced with the full ssl.CertMagicService
type SSLService struct {
	logger          *slog.Logger
	provisioningMu  sync.Mutex
	provisioning    map[string]bool // Track domains currently being provisioned
	enabled         bool            // Whether SSL provisioning is enabled
}

// SSLServiceConfig contains configuration for SSLService
type SSLServiceConfig struct {
	Logger  *slog.Logger
	Enabled bool // Set to false to disable SSL provisioning (e.g., in development)
}

// SSLStatus represents the SSL certificate status for a domain (deprecated, use SSLStatusInfo)
type SSLStatus struct {
	Enabled     bool       `json:"enabled"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	Provisioning bool      `json:"provisioning"`
}

// NewSSLService creates a new SSLService instance
func NewSSLService(cfg SSLServiceConfig) *SSLService {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	return &SSLService{
		logger:       cfg.Logger,
		provisioning: make(map[string]bool),
		enabled:      cfg.Enabled,
	}
}

// ProvisionCertificate initiates async SSL certificate provisioning
// Requirements: FR-DOM-005, NFR-1 (async, non-blocking), NFR-3 (Reliability)
// Requirements: 1.1, 1.7 - Auto-provision after domain verification
func (s *SSLService) ProvisionCertificate(ctx context.Context, domainID uuid.UUID, domainName string) error {
	if !s.enabled {
		s.logger.Info("SSL provisioning disabled, skipping", "domain", domainName, "domain_id", domainID)
		return nil
	}

	s.provisioningMu.Lock()
	if s.provisioning[domainName] {
		s.provisioningMu.Unlock()
		s.logger.Debug("SSL provisioning already in progress", "domain", domainName, "domain_id", domainID)
		return nil
	}
	s.provisioning[domainName] = true
	s.provisioningMu.Unlock()

	// Async certificate provisioning (non-blocking per NFR-1)
	go func() {
		defer func() {
			s.provisioningMu.Lock()
			delete(s.provisioning, domainName)
			s.provisioningMu.Unlock()
		}()

		s.logger.Info("Starting SSL certificate provisioning", "domain", domainName, "domain_id", domainID)
		
		// TODO: Integrate with CertMagic for actual Let's Encrypt provisioning
		// For now, this is a placeholder that simulates the async operation
		// 
		// Example CertMagic integration:
		// err := certmagic.ManageAsync(context.Background(), []string{domainName})
		// if err != nil {
		//     s.logger.Error("Failed to provision SSL", "domain", domainName, "error", err)
		//     return
		// }
		
		s.logger.Info("SSL certificate provisioning completed", "domain", domainName, "domain_id", domainID)
	}()

	return nil
}

// RevokeCertificate revokes and removes an SSL certificate
// Requirements: FR-DOM-004 (cleanup on domain deletion)
// Requirements: 7.1 - Revoke certificate when domain is deleted
func (s *SSLService) RevokeCertificate(ctx context.Context, domainID uuid.UUID, domainName string) error {
	if !s.enabled {
		s.logger.Info("SSL provisioning disabled, skipping revocation", "domain", domainName, "domain_id", domainID)
		return nil
	}

	s.logger.Info("Revoking SSL certificate", "domain", domainName, "domain_id", domainID)
	
	// TODO: Integrate with CertMagic for actual certificate revocation
	// Example:
	// return certmagic.Unmanage([]string{domainName})
	
	return nil
}

// GetSSLStatus returns the SSL status for a domain
func (s *SSLService) GetSSLStatus(ctx context.Context, domainID uuid.UUID) (*SSLStatusInfo, error) {
	// This simple implementation doesn't track status in database
	// The full ssl.CertMagicService implementation should be used for production
	return &SSLStatusInfo{
		Status:       "pending",
		Enabled:      false,
		Provisioning: false,
	}, nil
}

// GetCertificateExpiry returns the expiration date of a domain's SSL certificate
func (s *SSLService) GetCertificateExpiry(domainName string) (*time.Time, error) {
	if !s.enabled {
		return nil, nil
	}

	// TODO: Integrate with CertMagic to get actual certificate expiry
	// Example:
	// cert, err := certmagic.CacheManagedCertificate(domainName)
	// if err != nil {
	//     return nil, err
	// }
	// return &cert.Leaf.NotAfter, nil
	
	return nil, nil
}

// GetStatus returns the SSL status for a domain (deprecated, use GetSSLStatus)
func (s *SSLService) GetStatus(domainName string) SSLStatus {
	s.provisioningMu.Lock()
	provisioning := s.provisioning[domainName]
	s.provisioningMu.Unlock()

	expiry, _ := s.GetCertificateExpiry(domainName)
	
	return SSLStatus{
		Enabled:      expiry != nil,
		ExpiresAt:    expiry,
		Provisioning: provisioning,
	}
}

// IsProvisioning checks if a domain is currently being provisioned
func (s *SSLService) IsProvisioning(domainName string) bool {
	s.provisioningMu.Lock()
	defer s.provisioningMu.Unlock()
	return s.provisioning[domainName]
}

// CheckRenewalNeeded checks if a certificate needs renewal (30 days before expiry)
// Requirements: NFR-3 (SSL auto-renewal 30 days before expiry)
func (s *SSLService) CheckRenewalNeeded(domainName string) (bool, error) {
	expiry, err := s.GetCertificateExpiry(domainName)
	if err != nil {
		return false, fmt.Errorf("failed to get certificate expiry: %w", err)
	}
	
	if expiry == nil {
		return false, nil
	}
	
	// Renew if expiring within 30 days
	renewalThreshold := time.Now().Add(30 * 24 * time.Hour)
	return expiry.Before(renewalThreshold), nil
}

// Ensure SSLService implements SSLServiceInterface
var _ SSLServiceInterface = (*SSLService)(nil)
