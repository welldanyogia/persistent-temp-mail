package alias

import (
	"strings"
	"testing"
	"unicode"

	"pgregory.net/rapid"
)

// Feature: email-alias-management, Property 1: Local Part Validation
// **Validates: Requirements 1.2, 1.8, 6.1-6.5**
//
// *For any* string input as local_part, the Alias_Service SHALL accept it if and only if
// it matches the pattern ^[a-z0-9._%+-]+$, has length between 1-64 characters,
// does not start or end with a dot, and does not contain consecutive dots.
func TestProperty1_LocalPartValidation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate random string for testing
		localPart := rapid.StringN(0, 100, 100).Draw(t, "localPart")

		errors := ValidateLocalPart(localPart)

		// Calculate expected validation results
		expectedErrors := calculateExpectedErrors(localPart)

		// Verify that validation correctly identifies all issues
		if len(errors) == 0 && len(expectedErrors) > 0 {
			t.Errorf("expected validation errors for %q but got none. Expected: %v", localPart, expectedErrors)
		}
		if len(errors) > 0 && len(expectedErrors) == 0 {
			t.Errorf("expected no validation errors for %q but got: %v", localPart, errors)
		}
	})
}

// calculateExpectedErrors determines what validation errors should be returned
func calculateExpectedErrors(localPart string) []string {
	var expected []string

	// Check length
	if len(localPart) < MinLocalPartLength {
		expected = append(expected, "length_too_short")
	}
	if len(localPart) > MaxLocalPartLength {
		expected = append(expected, "length_too_long")
	}

	// Check pattern - only lowercase alphanumeric, dots, underscores, percent, plus, hyphens
	hasInvalidChars := false
	for _, char := range localPart {
		if !isValidLocalPartChar(char) {
			hasInvalidChars = true
			break
		}
	}
	if hasInvalidChars {
		expected = append(expected, "invalid_chars")
	}

	// Check leading dot
	if strings.HasPrefix(localPart, ".") {
		expected = append(expected, "leading_dot")
	}

	// Check trailing dot
	if strings.HasSuffix(localPart, ".") {
		expected = append(expected, "trailing_dot")
	}

	// Check consecutive dots
	if strings.Contains(localPart, "..") {
		expected = append(expected, "consecutive_dots")
	}

	return expected
}

// isValidLocalPartChar checks if a character is valid for local part
func isValidLocalPartChar(char rune) bool {
	// Lowercase letters
	if char >= 'a' && char <= 'z' {
		return true
	}
	// Digits
	if char >= '0' && char <= '9' {
		return true
	}
	// Special allowed characters: . _ % + -
	switch char {
	case '.', '_', '%', '+', '-':
		return true
	}
	return false
}

// TestProperty1_ValidLocalPartsAccepted tests that valid local parts are accepted
func TestProperty1_ValidLocalPartsAccepted(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate valid local parts
		validChars := "abcdefghijklmnopqrstuvwxyz0123456789_%+-"
		length := rapid.IntRange(1, 64).Draw(t, "length")

		var builder strings.Builder
		for i := 0; i < length; i++ {
			charIdx := rapid.IntRange(0, len(validChars)-1).Draw(t, "charIdx")
			builder.WriteByte(validChars[charIdx])
		}
		localPart := builder.String()

		// Remove leading/trailing dots and consecutive dots
		localPart = strings.TrimPrefix(localPart, ".")
		localPart = strings.TrimSuffix(localPart, ".")
		for strings.Contains(localPart, "..") {
			localPart = strings.ReplaceAll(localPart, "..", ".")
		}

		// Skip if empty after cleanup
		if len(localPart) == 0 {
			return
		}

		errors := ValidateLocalPart(localPart)
		if len(errors) > 0 {
			t.Errorf("expected valid local part %q to pass validation, but got errors: %v", localPart, errors)
		}
	})
}

// TestProperty1_InvalidCharsRejected tests that invalid characters are rejected
func TestProperty1_InvalidCharsRejected(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate string with at least one invalid character
		validPart := rapid.StringMatching(`^[a-z0-9]+$`).Draw(t, "validPart")
		if len(validPart) == 0 {
			validPart = "test"
		}

		// Add an invalid character (uppercase or special)
		invalidChars := "ABCDEFGHIJKLMNOPQRSTUVWXYZ!@#$^&*()={}[]|\\:\";<>,?/"
		invalidCharIdx := rapid.IntRange(0, len(invalidChars)-1).Draw(t, "invalidCharIdx")
		invalidChar := string(invalidChars[invalidCharIdx])

		localPart := validPart + invalidChar

		errors := ValidateLocalPart(localPart)
		hasInvalidCharError := false
		for _, err := range errors {
			if strings.Contains(err, "invalid characters") {
				hasInvalidCharError = true
				break
			}
		}

		if !hasInvalidCharError {
			t.Errorf("expected invalid character error for %q but got: %v", localPart, errors)
		}
	})
}

// TestProperty1_LengthValidation tests length constraints
func TestProperty1_LengthValidation(t *testing.T) {
	// Test empty string
	errors := ValidateLocalPart("")
	if len(errors) == 0 {
		t.Error("expected error for empty local part")
	}

	// Test string longer than 64 characters
	longPart := strings.Repeat("a", 65)
	errors = ValidateLocalPart(longPart)
	hasLengthError := false
	for _, err := range errors {
		if strings.Contains(err, "64 characters") {
			hasLengthError = true
			break
		}
	}
	if !hasLengthError {
		t.Errorf("expected length error for 65-char local part, got: %v", errors)
	}

	// Test valid length (64 characters)
	validPart := strings.Repeat("a", 64)
	errors = ValidateLocalPart(validPart)
	if len(errors) > 0 {
		t.Errorf("expected no errors for 64-char local part, got: %v", errors)
	}
}

// TestProperty1_DotRules tests dot placement rules
func TestProperty1_DotRules(t *testing.T) {
	testCases := []struct {
		name        string
		localPart   string
		expectError bool
		errorMsg    string
	}{
		{"leading dot", ".test", true, "start with a dot"},
		{"trailing dot", "test.", true, "end with a dot"},
		{"consecutive dots", "te..st", true, "consecutive dots"},
		{"valid single dot", "te.st", false, ""},
		{"valid multiple dots", "te.s.t", false, ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			errors := ValidateLocalPart(tc.localPart)
			hasExpectedError := false
			for _, err := range errors {
				if strings.Contains(err, tc.errorMsg) {
					hasExpectedError = true
					break
				}
			}

			if tc.expectError && !hasExpectedError {
				t.Errorf("expected error containing %q for %q, got: %v", tc.errorMsg, tc.localPart, errors)
			}
			if !tc.expectError && len(errors) > 0 {
				t.Errorf("expected no errors for %q, got: %v", tc.localPart, errors)
			}
		})
	}
}

// Feature: email-alias-management, Property 2: Lowercase Full Address Generation
// **Validates: Requirements 1.9, 6.5**
//
// *For any* valid local_part and domain_name combination, the generated full_address
// SHALL be the lowercase concatenation of local_part + "@" + domain_name.
func TestProperty2_LowercaseFullAddressGeneration(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate random local part and domain
		localPart := rapid.StringN(1, 64, 64).Draw(t, "localPart")
		domainName := rapid.StringN(1, 253, 253).Draw(t, "domainName")

		fullAddress := GenerateFullAddress(localPart, domainName)

		// Property 1: Result should be lowercase
		if fullAddress != strings.ToLower(fullAddress) {
			t.Errorf("full address %q is not lowercase", fullAddress)
		}

		// Property 2: Result should contain @ separator
		if !strings.Contains(fullAddress, "@") {
			t.Errorf("full address %q does not contain @", fullAddress)
		}

		// Property 3: Result should be localPart@domainName (both lowercase)
		expected := strings.ToLower(localPart) + "@" + strings.ToLower(domainName)
		if fullAddress != expected {
			t.Errorf("expected %q, got %q", expected, fullAddress)
		}
	})
}

// TestProperty2_MixedCaseNormalization tests that mixed case inputs are normalized
func TestProperty2_MixedCaseNormalization(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate strings with mixed case
		chars := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
		localPartLen := rapid.IntRange(1, 20).Draw(t, "localPartLen")
		domainLen := rapid.IntRange(1, 20).Draw(t, "domainLen")

		var localBuilder, domainBuilder strings.Builder
		for i := 0; i < localPartLen; i++ {
			idx := rapid.IntRange(0, len(chars)-1).Draw(t, "localCharIdx")
			localBuilder.WriteByte(chars[idx])
		}
		for i := 0; i < domainLen; i++ {
			idx := rapid.IntRange(0, len(chars)-1).Draw(t, "domainCharIdx")
			domainBuilder.WriteByte(chars[idx])
		}

		localPart := localBuilder.String()
		domainName := domainBuilder.String()

		fullAddress := GenerateFullAddress(localPart, domainName)

		// Verify no uppercase characters in result
		for _, char := range fullAddress {
			if unicode.IsUpper(char) {
				t.Errorf("full address %q contains uppercase character %c", fullAddress, char)
			}
		}
	})
}
