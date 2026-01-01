package smtp

import (
	"regexp"
	"strings"
)

// Email validation regex based on RFC 5321
// Requirement 6.6: Validate sender address format (basic RFC 5321 check)
var emailRegex = regexp.MustCompile(`^[a-zA-Z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$`)

// ValidateEmailAddress validates an email address format according to RFC 5321
// Requirement 6.6: Basic RFC 5321 check
func ValidateEmailAddress(email string) bool {
	if email == "" {
		return false
	}
	
	// Check length limits (RFC 5321)
	// Total address max: 320 chars
	// Local part max: 64 chars
	// Domain max: 255 chars
	if len(email) > 320 {
		return false
	}
	
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return false
	}
	
	localPart := parts[0]
	domain := parts[1]
	
	if len(localPart) > 64 {
		return false
	}
	
	if len(domain) > 255 {
		return false
	}
	
	if localPart == "" || domain == "" {
		return false
	}
	
	return emailRegex.MatchString(email)
}

// ValidateHeaderValue validates a header value for injection attempts
// Requirement 6.1: Validate email header injection attempts (CRLF injection)
// Requirement 6.2: Limit header length to 1000 characters
func ValidateHeaderValue(value string) (string, bool) {
	// Check for CRLF injection (Requirement 6.1)
	if strings.Contains(value, "\r") || strings.Contains(value, "\n") {
		return "", false
	}
	
	// Truncate if exceeds limit (Requirement 6.2)
	if len(value) > 1000 {
		return value[:1000], true
	}
	
	return value, true
}

// SanitizeHeaderValue removes potentially dangerous characters from header values
func SanitizeHeaderValue(value string) string {
	// Remove CRLF sequences
	value = strings.ReplaceAll(value, "\r\n", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	
	// Truncate if too long
	if len(value) > 1000 {
		value = value[:1000]
	}
	
	return value
}
