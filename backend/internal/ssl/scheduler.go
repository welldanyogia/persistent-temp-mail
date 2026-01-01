// Package ssl provides SSL certificate management functionality
// Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.8 - Certificate renewal scheduler
package ssl

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// NotificationService defines the interface for sending notifications
// Requirements: 3.4, 3.5 - Send notifications before certificate expiry
type NotificationService interface {
	// SendCertificateExpiryAlert sends an alert about an expiring certificate
	SendCertificateExpiryAlert(ctx context.Context, cert *CertificateInfo, daysUntilExpiry int) error
	
	// SendRenewalFailureAlert sends an alert about a failed renewal
	SendRenewalFailureAlert(ctx context.Context, cert *CertificateInfo, err error) error
	
	// SendRenewalSuccessNotification sends a notification about successful renewal
	SendRenewalSuccessNotification(ctx context.Context, cert *CertificateInfo) error
}

// RenewalSchedulerConfig holds configuration for the renewal scheduler
type RenewalSchedulerConfig struct {
	// CheckInterval is how often to check for expiring certificates (default: 24 hours)
	CheckInterval time.Duration
	
	// RenewalDays is how many days before expiry to start renewal (default: 30)
	RenewalDays int
	
	// AlertDays are the days before expiry to send alerts (default: [14, 7, 3, 1])
	AlertDays []int
	
	// RetryInterval is how long to wait before retrying a failed renewal (default: 24 hours)
	RetryInterval time.Duration
	
	// MaxConcurrentRenewals limits parallel renewal operations (default: 5)
	MaxConcurrentRenewals int
}

// DefaultRenewalSchedulerConfig returns the default configuration
func DefaultRenewalSchedulerConfig() RenewalSchedulerConfig {
	return RenewalSchedulerConfig{
		CheckInterval:         24 * time.Hour,
		RenewalDays:           30,
		AlertDays:             []int{14, 7, 3, 1},
		RetryInterval:         24 * time.Hour,
		MaxConcurrentRenewals: 5,
	}
}

// RenewalScheduler handles automatic certificate renewal
// Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.8
type RenewalScheduler struct {
	sslService SSLService
	repo       SSLCertificateRepository
	notifier   NotificationService
	config     RenewalSchedulerConfig
	
	// Control channels
	stopCh   chan struct{}
	doneCh   chan struct{}
	
	// State tracking
	mu       sync.Mutex
	running  bool
	lastRun  time.Time
}

// NewRenewalScheduler creates a new RenewalScheduler instance
func NewRenewalScheduler(
	sslService SSLService,
	repo SSLCertificateRepository,
	notifier NotificationService,
	config RenewalSchedulerConfig,
) *RenewalScheduler {
	// Apply defaults for zero values
	if config.CheckInterval == 0 {
		config.CheckInterval = 24 * time.Hour
	}
	if config.RenewalDays == 0 {
		config.RenewalDays = 30
	}
	if len(config.AlertDays) == 0 {
		config.AlertDays = []int{14, 7, 3, 1}
	}
	if config.RetryInterval == 0 {
		config.RetryInterval = 24 * time.Hour
	}
	if config.MaxConcurrentRenewals == 0 {
		config.MaxConcurrentRenewals = 5
	}
	
	return &RenewalScheduler{
		sslService: sslService,
		repo:       repo,
		notifier:   notifier,
		config:     config,
		stopCh:     make(chan struct{}),
		doneCh:     make(chan struct{}),
	}
}


// Start begins the renewal scheduler background process
// Requirements: 3.1 - Check certificate expiration daily
func (s *RenewalScheduler) Start(ctx context.Context) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		log.Println("Renewal scheduler is already running")
		return
	}
	s.running = true
	s.stopCh = make(chan struct{})
	s.doneCh = make(chan struct{})
	s.mu.Unlock()
	
	log.Printf("Starting certificate renewal scheduler (check interval: %v, renewal days: %d)",
		s.config.CheckInterval, s.config.RenewalDays)
	
	go s.run(ctx)
}

// run is the main scheduler loop
func (s *RenewalScheduler) run(ctx context.Context) {
	defer close(s.doneCh)
	
	ticker := time.NewTicker(s.config.CheckInterval)
	defer ticker.Stop()
	
	// Run immediately on start
	s.runRenewalCheck(ctx)
	
	for {
		select {
		case <-ctx.Done():
			log.Println("Renewal scheduler stopped: context cancelled")
			return
		case <-s.stopCh:
			log.Println("Renewal scheduler stopped: stop signal received")
			return
		case <-ticker.C:
			s.runRenewalCheck(ctx)
		}
	}
}

// Stop gracefully stops the renewal scheduler
func (s *RenewalScheduler) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	s.mu.Unlock()
	
	close(s.stopCh)
	<-s.doneCh
	log.Println("Renewal scheduler stopped")
}

// IsRunning returns whether the scheduler is currently running
func (s *RenewalScheduler) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// LastRunTime returns the time of the last renewal check
func (s *RenewalScheduler) LastRunTime() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastRun
}

// RunNow triggers an immediate renewal check
func (s *RenewalScheduler) RunNow(ctx context.Context) {
	s.runRenewalCheck(ctx)
}


// runRenewalCheck performs the certificate renewal check
// Requirements: 3.1 - Check certificate expiration daily
// Requirements: 3.2 - Initiate renewal 30 days before expiration
// Requirements: 3.3 - Retry renewal every 24 hours if failed
// Requirements: 3.4 - Send notification 14 days before expiration if renewal fails
// Requirements: 3.5 - Send critical alert 7 days before expiration if still not renewed
// Requirements: 8.1, 8.3 - Update certificate metrics
func (s *RenewalScheduler) runRenewalCheck(ctx context.Context) {
	s.mu.Lock()
	s.lastRun = time.Now().UTC()
	s.mu.Unlock()
	
	log.Println("Starting certificate renewal check...")
	
	// Get certificates expiring within renewal window
	// Requirements: 3.2 - Initiate renewal 30 days before expiration
	expiring, err := s.sslService.ListExpiringCertificates(ctx, s.config.RenewalDays)
	if err != nil {
		log.Printf("Failed to list expiring certificates: %v", err)
		return
	}
	
	log.Printf("Found %d certificates expiring within %d days", len(expiring), s.config.RenewalDays)
	
	// Requirements: 8.3 - Update expiring certificates metrics
	s.updateExpiringMetrics(ctx)
	
	// Use semaphore for concurrency control
	sem := make(chan struct{}, s.config.MaxConcurrentRenewals)
	var wg sync.WaitGroup
	
	for _, cert := range expiring {
		// Check context cancellation
		select {
		case <-ctx.Done():
			log.Println("Renewal check cancelled")
			return
		default:
		}
		
		// Requirements: 3.3 - Retry renewal every 24 hours if failed
		if s.shouldSkipRenewal(cert) {
			log.Printf("Skipping renewal for %s - attempted recently", cert.DomainName)
			continue
		}
		
		wg.Add(1)
		go func(c *CertificateInfo) {
			defer wg.Done()
			
			// Acquire semaphore
			sem <- struct{}{}
			defer func() { <-sem }()
			
			s.processCertificateRenewal(ctx, c)
		}(cert)
	}
	
	wg.Wait()
	log.Println("Certificate renewal check completed")
}

// updateExpiringMetrics updates the expiring certificates metrics
// Requirements: 8.3 - Alert on certificates expiring within 14 days
func (s *RenewalScheduler) updateExpiringMetrics(ctx context.Context) {
	// Update expiring certificates for various time windows
	timeWindows := []int{7, 14, 30}
	for _, days := range timeWindows {
		expiring, err := s.sslService.ListExpiringCertificates(ctx, days)
		if err != nil {
			log.Printf("Failed to list certificates expiring within %d days for metrics: %v", days, err)
			continue
		}
		UpdateExpiringCertificatesMetric(fmt.Sprintf("%d", days), len(expiring))
	}
}

// shouldSkipRenewal checks if we should skip renewal for a certificate
// Requirements: 3.3 - Retry renewal every 24 hours if failed
func (s *RenewalScheduler) shouldSkipRenewal(cert *CertificateInfo) bool {
	// Get full certificate info from repository to check last renewal attempt
	fullCert, err := s.repo.GetByDomainName(context.Background(), cert.DomainName)
	if err != nil {
		return false // If we can't get info, try to renew
	}
	
	if fullCert.LastRenewalAttempt != nil {
		timeSinceLastAttempt := time.Since(*fullCert.LastRenewalAttempt)
		if timeSinceLastAttempt < s.config.RetryInterval {
			return true
		}
	}
	
	return false
}


// processCertificateRenewal handles the renewal of a single certificate
// Requirements: 3.4, 3.5 - Send notifications if renewal fails
func (s *RenewalScheduler) processCertificateRenewal(ctx context.Context, cert *CertificateInfo) {
	log.Printf("Processing renewal for %s (expires in %d days)", cert.DomainName, cert.DaysUntilExp)
	
	// Attempt renewal
	result, err := s.sslService.RenewCertificate(ctx, cert.DomainID)
	if err != nil {
		log.Printf("Failed to renew certificate for %s: %v", cert.DomainName, err)
		s.handleRenewalFailure(ctx, cert, err)
		return
	}
	
	if !result.Success {
		log.Printf("Renewal failed for %s: %s", cert.DomainName, result.Error)
		s.handleRenewalFailure(ctx, cert, ErrRenewalFailed)
		return
	}
	
	log.Printf("Successfully renewed certificate for %s", cert.DomainName)
	
	// Send success notification if notifier is available
	if s.notifier != nil {
		if err := s.notifier.SendRenewalSuccessNotification(ctx, result.Certificate); err != nil {
			log.Printf("Failed to send renewal success notification for %s: %v", cert.DomainName, err)
		}
	}
}

// handleRenewalFailure handles a failed renewal attempt
// Requirements: 3.4 - Send notification 14 days before expiration if renewal fails
// Requirements: 3.5 - Send critical alert 7 days before expiration if still not renewed
func (s *RenewalScheduler) handleRenewalFailure(ctx context.Context, cert *CertificateInfo, renewalErr error) {
	// Send failure notification if notifier is available
	if s.notifier != nil {
		if err := s.notifier.SendRenewalFailureAlert(ctx, cert, renewalErr); err != nil {
			log.Printf("Failed to send renewal failure alert for %s: %v", cert.DomainName, err)
		}
	}
	
	// Check if we need to send expiry alerts
	// Requirements: 3.4, 3.5 - Send notifications at specific days before expiry
	for _, alertDay := range s.config.AlertDays {
		if cert.DaysUntilExp <= alertDay {
			s.sendExpiryAlert(ctx, cert, alertDay)
			break // Only send the most urgent alert
		}
	}
}

// sendExpiryAlert sends an expiry alert notification
// Requirements: 3.4 - Send notification 14 days before expiration if renewal fails
// Requirements: 3.5 - Send critical alert 7 days before expiration if still not renewed
func (s *RenewalScheduler) sendExpiryAlert(ctx context.Context, cert *CertificateInfo, daysUntilExpiry int) {
	if s.notifier == nil {
		log.Printf("Warning: No notifier configured, cannot send expiry alert for %s", cert.DomainName)
		return
	}
	
	severity := "warning"
	if daysUntilExpiry <= 7 {
		severity = "critical"
	}
	
	log.Printf("Sending %s expiry alert for %s (expires in %d days)", severity, cert.DomainName, cert.DaysUntilExp)
	
	if err := s.notifier.SendCertificateExpiryAlert(ctx, cert, daysUntilExpiry); err != nil {
		log.Printf("Failed to send expiry alert for %s: %v", cert.DomainName, err)
	}
}


// GetStats returns statistics about the scheduler's operation
func (s *RenewalScheduler) GetStats(ctx context.Context) (*SchedulerStats, error) {
	// Get counts of certificates in various states
	activeCount, err := s.repo.CountActive(ctx)
	if err != nil {
		return nil, err
	}
	
	// Get expiring certificates
	expiring30, err := s.sslService.ListExpiringCertificates(ctx, 30)
	if err != nil {
		return nil, err
	}
	
	expiring14, err := s.sslService.ListExpiringCertificates(ctx, 14)
	if err != nil {
		return nil, err
	}
	
	expiring7, err := s.sslService.ListExpiringCertificates(ctx, 7)
	if err != nil {
		return nil, err
	}
	
	s.mu.Lock()
	lastRun := s.lastRun
	running := s.running
	s.mu.Unlock()
	
	return &SchedulerStats{
		Running:            running,
		LastRunTime:        lastRun,
		ActiveCertificates: activeCount,
		ExpiringIn30Days:   len(expiring30),
		ExpiringIn14Days:   len(expiring14),
		ExpiringIn7Days:    len(expiring7),
		CheckInterval:      s.config.CheckInterval,
		RenewalDays:        s.config.RenewalDays,
	}, nil
}

// SchedulerStats contains statistics about the renewal scheduler
type SchedulerStats struct {
	Running            bool          `json:"running"`
	LastRunTime        time.Time     `json:"last_run_time"`
	ActiveCertificates int           `json:"active_certificates"`
	ExpiringIn30Days   int           `json:"expiring_in_30_days"`
	ExpiringIn14Days   int           `json:"expiring_in_14_days"`
	ExpiringIn7Days    int           `json:"expiring_in_7_days"`
	CheckInterval      time.Duration `json:"check_interval"`
	RenewalDays        int           `json:"renewal_days"`
}

// NoOpNotificationService is a no-op implementation of NotificationService
// Used when no notification service is configured
type NoOpNotificationService struct{}

// SendCertificateExpiryAlert logs the alert but takes no action
func (n *NoOpNotificationService) SendCertificateExpiryAlert(ctx context.Context, cert *CertificateInfo, daysUntilExpiry int) error {
	log.Printf("[NoOp] Certificate expiry alert: %s expires in %d days", cert.DomainName, daysUntilExpiry)
	return nil
}

// SendRenewalFailureAlert logs the alert but takes no action
func (n *NoOpNotificationService) SendRenewalFailureAlert(ctx context.Context, cert *CertificateInfo, err error) error {
	log.Printf("[NoOp] Renewal failure alert: %s - %v", cert.DomainName, err)
	return nil
}

// SendRenewalSuccessNotification logs the notification but takes no action
func (n *NoOpNotificationService) SendRenewalSuccessNotification(ctx context.Context, cert *CertificateInfo) error {
	log.Printf("[NoOp] Renewal success: %s renewed until %s", cert.DomainName, cert.ExpiresAt.Format(time.RFC3339))
	return nil
}

// Ensure NoOpNotificationService implements NotificationService
var _ NotificationService = (*NoOpNotificationService)(nil)
