package domain

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"
)

const (
	// DefaultDNSTimeout is the default timeout for DNS lookups
	DefaultDNSTimeout = 5 * time.Second
	// TXTRecordPrefix is the subdomain prefix for verification TXT records
	TXTRecordPrefix = "_tempmail-verification"
)

// DNSService handles DNS lookups for domain verification
type DNSService struct {
	resolver   *net.Resolver
	mailServer string // Expected MX record target (e.g., "mail.webrana.id")
	txtPrefix  string // TXT record subdomain prefix
	timeout    time.Duration
	logger     *slog.Logger
}

// DNSServiceConfig contains configuration for DNSService
type DNSServiceConfig struct {
	MailServer string        // Expected MX record target
	TXTPrefix  string        // TXT record subdomain prefix (default: _tempmail-verification)
	Timeout    time.Duration // DNS lookup timeout (default: 5s)
	Logger     *slog.Logger
}

// MXRecord represents an MX record lookup result
type MXRecord struct {
	Priority int    `json:"priority"`
	Hostname string `json:"hostname"`
	IsValid  bool   `json:"is_valid"`
}

// TXTRecord represents a TXT record lookup result
type TXTRecord struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	IsValid bool   `json:"is_valid"`
}

// DNSCheckResult contains the results of a DNS verification check
type DNSCheckResult struct {
	MXRecords       []MXRecord `json:"mx_records"`
	TXTRecords      []TXTRecord `json:"txt_records"`
	MXValid         bool       `json:"mx_valid"`
	TXTValid        bool       `json:"txt_valid"`
	IsReadyToVerify bool       `json:"is_ready_to_verify"`
	Issues          []string   `json:"issues,omitempty"`
}

// NewDNSService creates a new DNSService instance
func NewDNSService(cfg DNSServiceConfig) *DNSService {
	if cfg.TXTPrefix == "" {
		cfg.TXTPrefix = TXTRecordPrefix
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = DefaultDNSTimeout
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	return &DNSService{
		resolver:   net.DefaultResolver,
		mailServer: cfg.MailServer,
		txtPrefix:  cfg.TXTPrefix,
		timeout:    cfg.Timeout,
		logger:     cfg.Logger,
	}
}


// CheckDNS performs a complete DNS check for domain verification
// Requirements: FR-DOM-005, FR-DOM-006, NFR-1
func (s *DNSService) CheckDNS(ctx context.Context, domainName, verificationToken string) (*DNSCheckResult, error) {
	result := &DNSCheckResult{
		MXRecords:  make([]MXRecord, 0),
		TXTRecords: make([]TXTRecord, 0),
		Issues:     make([]string, 0),
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	// 1. Lookup MX records
	s.checkMXRecords(ctx, domainName, result)

	// 2. Lookup TXT records for verification
	s.checkTXTRecords(ctx, domainName, verificationToken, result)

	// Determine if ready to verify
	result.IsReadyToVerify = result.MXValid && result.TXTValid

	return result, nil
}

// checkMXRecords looks up and validates MX records
func (s *DNSService) checkMXRecords(ctx context.Context, domainName string, result *DNSCheckResult) {
	mxRecords, err := s.resolver.LookupMX(ctx, domainName)
	if err != nil {
		s.logger.Debug("MX lookup failed", "domain", domainName, "error", err)
		result.Issues = append(result.Issues, fmt.Sprintf("Failed to lookup MX records: %v", err))
		return
	}

	if len(mxRecords) == 0 {
		result.Issues = append(result.Issues, "No MX records found")
		return
	}

	for _, mx := range mxRecords {
		hostname := strings.TrimSuffix(mx.Host, ".")
		isValid := s.isValidMXHost(hostname)
		
		record := MXRecord{
			Priority: int(mx.Pref),
			Hostname: hostname,
			IsValid:  isValid,
		}
		result.MXRecords = append(result.MXRecords, record)
		
		if isValid {
			result.MXValid = true
		}
	}

	if !result.MXValid {
		result.Issues = append(result.Issues, fmt.Sprintf("MX record should point to %s", s.mailServer))
	}
}

// isValidMXHost checks if the MX hostname points to our mail server
func (s *DNSService) isValidMXHost(hostname string) bool {
	hostname = strings.ToLower(hostname)
	mailServer := strings.ToLower(s.mailServer)
	
	// Exact match or subdomain match
	return hostname == mailServer || strings.HasSuffix(hostname, "."+mailServer)
}

// checkTXTRecords looks up and validates TXT records for verification
func (s *DNSService) checkTXTRecords(ctx context.Context, domainName, verificationToken string, result *DNSCheckResult) {
	txtName := fmt.Sprintf("%s.%s", s.txtPrefix, domainName)
	
	txtRecords, err := s.resolver.LookupTXT(ctx, txtName)
	if err != nil {
		s.logger.Debug("TXT lookup failed", "name", txtName, "error", err)
		result.Issues = append(result.Issues, fmt.Sprintf("Failed to lookup TXT record at %s: %v", txtName, err))
		return
	}

	if len(txtRecords) == 0 {
		result.Issues = append(result.Issues, fmt.Sprintf("No TXT record found at %s", txtName))
		return
	}

	for _, txt := range txtRecords {
		isValid := txt == verificationToken
		
		record := TXTRecord{
			Name:    txtName,
			Value:   txt,
			IsValid: isValid,
		}
		result.TXTRecords = append(result.TXTRecords, record)
		
		if isValid {
			result.TXTValid = true
		}
	}

	if !result.TXTValid {
		result.Issues = append(result.Issues, "TXT record value does not match verification token")
	}
}

// LookupMX performs a standalone MX record lookup
func (s *DNSService) LookupMX(ctx context.Context, domainName string) ([]MXRecord, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	mxRecords, err := s.resolver.LookupMX(ctx, domainName)
	if err != nil {
		return nil, fmt.Errorf("MX lookup failed: %w", err)
	}

	records := make([]MXRecord, 0, len(mxRecords))
	for _, mx := range mxRecords {
		hostname := strings.TrimSuffix(mx.Host, ".")
		records = append(records, MXRecord{
			Priority: int(mx.Pref),
			Hostname: hostname,
			IsValid:  s.isValidMXHost(hostname),
		})
	}

	return records, nil
}

// LookupTXT performs a standalone TXT record lookup for verification
func (s *DNSService) LookupTXT(ctx context.Context, domainName string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	txtName := fmt.Sprintf("%s.%s", s.txtPrefix, domainName)
	return s.resolver.LookupTXT(ctx, txtName)
}

// GetDNSInstructions returns the DNS configuration instructions for a domain
func (s *DNSService) GetDNSInstructions(domainName, verificationToken string) DNSInstructions {
	return DNSInstructions{
		MXRecord: MXInstruction{
			Type:     "MX",
			Priority: 10,
			Value:    s.mailServer,
		},
		TXTRecord: TXTInstruction{
			Type:  "TXT",
			Name:  fmt.Sprintf("%s.%s", s.txtPrefix, domainName),
			Value: verificationToken,
		},
	}
}

// DNSInstructions contains the DNS configuration instructions
type DNSInstructions struct {
	MXRecord  MXInstruction  `json:"mx_record"`
	TXTRecord TXTInstruction `json:"txt_record"`
}

// MXInstruction contains MX record setup instructions
type MXInstruction struct {
	Type     string `json:"type"`
	Priority int    `json:"priority"`
	Value    string `json:"value"`
}

// TXTInstruction contains TXT record setup instructions
type TXTInstruction struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	Value string `json:"value"`
}
