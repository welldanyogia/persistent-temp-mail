package domain

import (
	"regexp"
	"strings"
)

const (
	// MaxDomainLength is the maximum length of a domain name per RFC 1035
	MaxDomainLength = 253
	// MaxLabelLength is the maximum length of a domain label per RFC 1035
	MaxLabelLength = 63
)

// Reserved domains that cannot be registered
var reservedDomains = []string{
	"localhost",
	"local",
	"internal",
	"webrana.id",
	"example.com",
	"example.org",
	"example.net",
	"test",
	"invalid",
}

// domainRegex validates domain format per RFC 1035
// Pattern: one or more labels separated by dots, ending with a TLD of 2+ chars
var domainRegex = regexp.MustCompile(`^([a-z0-9]+(-[a-z0-9]+)*\.)+[a-z]{2,}$`)

// ValidateDomainName validates a domain name according to RFC 1035
// Requirements: FR-DOM-002, NFR-2 (Security)
func ValidateDomainName(name string) error {
	// Sanitize input
	name = SanitizeDomainName(name)
	
	// Check empty
	if name == "" {
		return ErrInvalidDomainName
	}
	
	// Check max length (RFC 1035: max 253 chars)
	if len(name) > MaxDomainLength {
		return ErrInvalidDomainName
	}
	
	// Check format with regex
	if !domainRegex.MatchString(name) {
		return ErrInvalidDomainName
	}
	
	// Check individual label lengths (max 63 chars each)
	labels := strings.Split(name, ".")
	for _, label := range labels {
		if len(label) > MaxLabelLength {
			return ErrInvalidDomainName
		}
	}
	
	// Check for reserved domains
	if isReservedDomain(name) {
		return ErrReservedDomain
	}
	
	return nil
}

// SanitizeDomainName normalizes a domain name
// - Converts to lowercase
// - Trims whitespace
// - Removes trailing dots
func SanitizeDomainName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ToLower(name)
	name = strings.TrimSuffix(name, ".")
	return name
}

// isReservedDomain checks if a domain is in the reserved list
func isReservedDomain(name string) bool {
	for _, reserved := range reservedDomains {
		// Exact match
		if name == reserved {
			return true
		}
		// Subdomain of reserved domain
		if strings.HasSuffix(name, "."+reserved) {
			return true
		}
	}
	return false
}
