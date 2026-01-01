package domain

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/events"
)

const (
	// DefaultDomainLimit is the default max domains per user (free tier)
	DefaultDomainLimit = 5
)

// Service handles domain business logic
type Service struct {
	repo        Repository
	dnsService  *DNSService
	sslService  SSLServiceInterface
	eventBus    events.EventBus
	domainLimit int
	logger      *slog.Logger
}

// ServiceConfig contains configuration for the domain Service
type ServiceConfig struct {
	Repository  Repository
	DNSService  *DNSService
	SSLService  SSLServiceInterface
	EventBus    events.EventBus
	DomainLimit int // Max domains per user (default: 5)
	Logger      *slog.Logger
}

// NewService creates a new domain Service instance
func NewService(cfg ServiceConfig) *Service {
	if cfg.DomainLimit <= 0 {
		cfg.DomainLimit = DefaultDomainLimit
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	return &Service{
		repo:        cfg.Repository,
		dnsService:  cfg.DNSService,
		sslService:  cfg.SSLService,
		eventBus:    cfg.EventBus,
		domainLimit: cfg.DomainLimit,
		logger:      cfg.Logger,
	}
}

// ListDomains retrieves all domains for a user with pagination and filtering
// Requirements: FR-DOM-001
func (s *Service) ListDomains(ctx context.Context, userID uuid.UUID, opts ListOptions) ([]Domain, int, error) {
	domains, total, err := s.repo.GetByUserID(ctx, userID, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list domains: %w", err)
	}
	return domains, total, nil
}


// CreateDomain creates a new domain with validation and token generation
// Requirements: FR-DOM-002
func (s *Service) CreateDomain(ctx context.Context, userID uuid.UUID, domainName string) (*Domain, *DNSInstructions, error) {
	// Sanitize and validate domain name
	domainName = SanitizeDomainName(domainName)
	if err := ValidateDomainName(domainName); err != nil {
		return nil, nil, err
	}

	// Check domain limit
	count, err := s.repo.CountByUserID(ctx, userID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to count user domains: %w", err)
	}
	if count >= s.domainLimit {
		return nil, nil, ErrDomainLimitReached
	}

	// Check if domain already exists
	existing, err := s.repo.GetByDomainName(ctx, domainName)
	if err != nil && err != ErrDomainNotFound {
		return nil, nil, fmt.Errorf("failed to check domain existence: %w", err)
	}
	if existing != nil {
		return nil, nil, ErrDomainExists
	}

	// Generate verification token
	token, err := GenerateVerificationToken()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate verification token: %w", err)
	}

	// Create domain
	domain := &Domain{
		ID:                uuid.New(),
		UserID:            userID,
		DomainName:        domainName,
		VerificationToken: token,
		IsVerified:        false,
		SSLEnabled:        false,
	}

	if err := s.repo.Create(ctx, domain); err != nil {
		return nil, nil, fmt.Errorf("failed to create domain: %w", err)
	}

	// Get DNS instructions
	instructions := s.dnsService.GetDNSInstructions(domainName, token)

	s.logger.Info("Domain created", "domain_id", domain.ID, "domain_name", domainName, "user_id", userID)

	return domain, &instructions, nil
}

// GetDomain retrieves a domain by ID with ownership check
// Requirements: FR-DOM-003
func (s *Service) GetDomain(ctx context.Context, userID, domainID uuid.UUID) (*Domain, error) {
	domain, err := s.repo.GetByID(ctx, domainID)
	if err != nil {
		return nil, err
	}

	// Check ownership
	if domain.UserID != userID {
		return nil, ErrAccessDenied
	}

	return domain, nil
}

// DeleteDomain deletes a domain with cascade handling and SSL revocation
// Requirements: FR-DOM-004
func (s *Service) DeleteDomain(ctx context.Context, userID, domainID uuid.UUID) (*DeleteResult, error) {
	// Get domain to verify ownership and get domain name for SSL revocation
	domain, err := s.repo.GetByID(ctx, domainID)
	if err != nil {
		return nil, err
	}

	// Check ownership
	if domain.UserID != userID {
		return nil, ErrAccessDenied
	}

	// Revoke SSL certificate if enabled
	// Requirements: 7.1 - Revoke certificate when domain is deleted
	if domain.SSLEnabled && s.sslService != nil {
		if err := s.sslService.RevokeCertificate(ctx, domain.ID, domain.DomainName); err != nil {
			s.logger.Warn("Failed to revoke SSL certificate", "domain", domain.DomainName, "domain_id", domain.ID, "error", err)
			// Continue with deletion even if SSL revocation fails
		}
	}

	// Delete domain (cascade handles aliases, emails, attachments)
	result, err := s.repo.Delete(ctx, domainID)
	if err != nil {
		return nil, fmt.Errorf("failed to delete domain: %w", err)
	}

	s.logger.Info("Domain deleted", 
		"domain_id", domainID, 
		"domain_name", domain.DomainName,
		"aliases_deleted", result.AliasesDeleted,
		"emails_deleted", result.EmailsDeleted,
	)

	// Publish domain_deleted event
	// Requirements: 6.3, 6.4 - Real-time notification for domain deletion
	if s.eventBus != nil {
		s.publishDomainDeletedEvent(domain.UserID.String(), domain.ID.String(), domain.DomainName, time.Now().UTC(), result.AliasesDeleted, result.EmailsDeleted)
	}

	return result, nil
}

// VerifyDomain verifies domain DNS and triggers SSL provisioning
// Requirements: FR-DOM-005
// Requirements: 1.1, 1.7 - Auto-provision SSL after domain verification
func (s *Service) VerifyDomain(ctx context.Context, userID, domainID uuid.UUID) (*Domain, *DNSCheckResult, error) {
	// Get domain to verify ownership
	domain, err := s.repo.GetByID(ctx, domainID)
	if err != nil {
		return nil, nil, err
	}

	// Check ownership
	if domain.UserID != userID {
		return nil, nil, ErrAccessDenied
	}

	// Already verified
	if domain.IsVerified {
		dnsResult := &DNSCheckResult{
			MXValid:         true,
			TXTValid:        true,
			IsReadyToVerify: true,
		}
		return domain, dnsResult, nil
	}

	// Check DNS records
	dnsResult, err := s.dnsService.CheckDNS(ctx, domain.DomainName, domain.VerificationToken)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to check DNS: %w", err)
	}

	// If not ready to verify, return current state with DNS result
	if !dnsResult.IsReadyToVerify {
		return domain, dnsResult, ErrVerificationFailed
	}

	// Update domain as verified
	now := time.Now().UTC()
	domain.IsVerified = true
	domain.VerifiedAt = &now

	if err := s.repo.Update(ctx, domain); err != nil {
		return nil, nil, fmt.Errorf("failed to update domain: %w", err)
	}

	// Trigger SSL certificate provisioning (async)
	// Requirements: 1.1 - Auto-provision SSL after domain verification
	// Requirements: 1.7 - Update ssl_status to "provisioning" when provisioning starts
	sslStatus := "pending"
	if s.sslService != nil {
		// Pass domain ID for proper tracking in ssl_certificates table
		if err := s.sslService.ProvisionCertificate(ctx, domain.ID, domain.DomainName); err != nil {
			s.logger.Warn("Failed to start SSL provisioning", "domain", domain.DomainName, "domain_id", domain.ID, "error", err)
			// Don't fail verification if SSL provisioning fails to start
		} else {
			// SSL provisioning started successfully
			// The ssl_certificates table will be updated to "provisioning" status
			// by the SSL service
			sslStatus = "provisioning"
			s.logger.Info("SSL provisioning triggered after domain verification", 
				"domain_id", domain.ID, 
				"domain_name", domain.DomainName,
				"ssl_status", sslStatus,
			)
		}
	}

	s.logger.Info("Domain verified", "domain_id", domainID, "domain_name", domain.DomainName)

	// Publish domain_verified event
	// Requirements: 6.1, 6.2 - Real-time notification for domain verification
	if s.eventBus != nil {
		if domain.SSLEnabled {
			sslStatus = "active"
		}
		s.publishDomainVerifiedEvent(domain.UserID.String(), domain.ID.String(), domain.DomainName, *domain.VerifiedAt, sslStatus)
	}

	return domain, dnsResult, nil
}

// GetDNSStatus checks DNS status without triggering verification
// Requirements: FR-DOM-006
func (s *Service) GetDNSStatus(ctx context.Context, userID, domainID uuid.UUID) (*Domain, *DNSCheckResult, error) {
	// Get domain to verify ownership
	domain, err := s.repo.GetByID(ctx, domainID)
	if err != nil {
		return nil, nil, err
	}

	// Check ownership
	if domain.UserID != userID {
		return nil, nil, ErrAccessDenied
	}

	// Check DNS records (read-only, doesn't update domain status)
	dnsResult, err := s.dnsService.CheckDNS(ctx, domain.DomainName, domain.VerificationToken)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to check DNS: %w", err)
	}

	return domain, dnsResult, nil
}

// GetDNSInstructions returns DNS setup instructions for a domain
func (s *Service) GetDNSInstructions(ctx context.Context, userID, domainID uuid.UUID) (*Domain, *DNSInstructions, error) {
	domain, err := s.GetDomain(ctx, userID, domainID)
	if err != nil {
		return nil, nil, err
	}

	instructions := s.dnsService.GetDNSInstructions(domain.DomainName, domain.VerificationToken)
	return domain, &instructions, nil
}

// UpdateSSLStatus updates the SSL status for a domain (called by SSL service callbacks)
func (s *Service) UpdateSSLStatus(ctx context.Context, domainID uuid.UUID, enabled bool, expiresAt *time.Time) error {
	domain, err := s.repo.GetByID(ctx, domainID)
	if err != nil {
		return err
	}

	domain.SSLEnabled = enabled
	domain.SSLExpiresAt = expiresAt

	return s.repo.Update(ctx, domain)
}

// GetDomainLimit returns the domain limit for users
func (s *Service) GetDomainLimit() int {
	return s.domainLimit
}


// publishDomainVerifiedEvent publishes a domain_verified event to the event bus
// Requirements: 6.1, 6.2 - Real-time notification for domain verification
func (s *Service) publishDomainVerifiedEvent(userID, domainID, domainName string, verifiedAt time.Time, sslStatus string) {
	eventData := events.DomainVerifiedEvent{
		ID:         domainID,
		DomainName: domainName,
		VerifiedAt: verifiedAt,
		SSLStatus:  sslStatus,
	}

	data, err := json.Marshal(eventData)
	if err != nil {
		s.logger.Warn("Failed to marshal domain_verified event", "error", err)
		return
	}

	event := events.Event{
		ID:        uuid.New().String(),
		Type:      events.EventTypeDomainVerified,
		UserID:    userID,
		Data:      data,
		Timestamp: time.Now().UTC(),
	}

	if err := s.eventBus.Publish(event); err != nil {
		s.logger.Warn("Failed to publish domain_verified event", "domain_id", domainID, "error", err)
	}
}

// publishDomainDeletedEvent publishes a domain_deleted event to the event bus
// Requirements: 6.3, 6.4 - Real-time notification for domain deletion
func (s *Service) publishDomainDeletedEvent(userID, domainID, domainName string, deletedAt time.Time, aliasesDeleted, emailsDeleted int) {
	eventData := events.DomainDeletedEvent{
		ID:             domainID,
		DomainName:     domainName,
		DeletedAt:      deletedAt,
		AliasesDeleted: aliasesDeleted,
		EmailsDeleted:  emailsDeleted,
	}

	data, err := json.Marshal(eventData)
	if err != nil {
		s.logger.Warn("Failed to marshal domain_deleted event", "error", err)
		return
	}

	event := events.Event{
		ID:        uuid.New().String(),
		Type:      events.EventTypeDomainDeleted,
		UserID:    userID,
		Data:      data,
		Timestamp: time.Now().UTC(),
	}

	if err := s.eventBus.Publish(event); err != nil {
		s.logger.Warn("Failed to publish domain_deleted event", "domain_id", domainID, "error", err)
	}
}
