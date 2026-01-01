package alias

import (
	"regexp"
	"strings"

	"github.com/go-playground/validator/v10"
)

const (
	// MinLocalPartLength is the minimum length of local part per RFC 5321
	MinLocalPartLength = 1
	// MaxLocalPartLength is the maximum length of local part per RFC 5321
	MaxLocalPartLength = 64
)

// localPartRegex validates local part format
// Pattern: lowercase alphanumeric, dots, underscores, percent, plus, hyphens
// Requirements: 1.8, 6.1
var localPartRegex = regexp.MustCompile(`^[a-z0-9._%+-]+$`)

// Validator instance for request validation
var validate *validator.Validate

func init() {
	validate = validator.New()
}

// GetValidator returns the validator instance
func GetValidator() *validator.Validate {
	return validate
}

// ValidateLocalPart validates the local part of an email address
// Returns a list of validation errors (empty if valid)
// Requirements: 1.2, 1.8, 6.1-6.5
func ValidateLocalPart(localPart string) []string {
	var errors []string

	// Check length (Requirements: 1.3, 6.2)
	if len(localPart) < MinLocalPartLength {
		errors = append(errors, "local_part must be at least 1 character")
	}
	if len(localPart) > MaxLocalPartLength {
		errors = append(errors, "local_part must not exceed 64 characters")
	}

	// Check pattern (Requirements: 1.8, 6.1)
	if !localPartRegex.MatchString(localPart) {
		errors = append(errors, "local_part contains invalid characters (only lowercase alphanumeric, dots, underscores, percent, plus, and hyphens allowed)")
	}

	// Check leading/trailing dots (Requirement: 6.3)
	if strings.HasPrefix(localPart, ".") {
		errors = append(errors, "local_part must not start with a dot")
	}
	if strings.HasSuffix(localPart, ".") {
		errors = append(errors, "local_part must not end with a dot")
	}

	// Check consecutive dots (Requirement: 6.4)
	if strings.Contains(localPart, "..") {
		errors = append(errors, "local_part must not contain consecutive dots")
	}

	return errors
}

// GenerateFullAddress generates the full email address from local part and domain
// Returns lowercase full address for case-insensitive matching
// Requirements: 1.9, 6.5
func GenerateFullAddress(localPart, domainName string) string {
	return strings.ToLower(localPart) + "@" + strings.ToLower(domainName)
}
