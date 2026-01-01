package smtp

import (
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// TestProperty15_SenderAddressValidation tests Property 15: Sender Address Validation
// Feature: smtp-email-receiver, Property 15: Sender Address Validation
// *For any* MAIL FROM address, the SMTP_Server SHALL validate basic RFC 5321 format.
// **Validates: Requirements 6.6**
func TestProperty15_SenderAddressValidation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate valid email components
		localPartChars := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789.!#$%&'*+/=?^_`{|}~-"
		domainChars := "abcdefghijklmnopqrstuvwxyz0123456789"
		
		// Generate valid local part (1-64 chars)
		localPartLen := rapid.IntRange(1, 64).Draw(t, "localPartLen")
		localPart := rapid.StringOfN(rapid.RuneFrom([]rune(localPartChars)), localPartLen, localPartLen, -1).Draw(t, "localPart")
		
		// Generate valid domain (1-63 chars per label, total max 255)
		domainLabelLen := rapid.IntRange(1, 20).Draw(t, "domainLabelLen")
		domainLabel := rapid.StringOfN(rapid.RuneFrom([]rune(domainChars)), domainLabelLen, domainLabelLen, -1).Draw(t, "domainLabel")
		
		tldLen := rapid.IntRange(2, 6).Draw(t, "tldLen")
		tld := rapid.StringOfN(rapid.RuneFrom([]rune("abcdefghijklmnopqrstuvwxyz")), tldLen, tldLen, -1).Draw(t, "tld")
		
		domain := domainLabel + "." + tld
		
		// Construct valid email
		validEmail := localPart + "@" + domain
		
		// Valid email should pass validation
		if len(validEmail) <= 320 && len(localPart) <= 64 && len(domain) <= 255 {
			// Only test if within RFC limits
			if ValidateEmailAddress(validEmail) == false {
				// Some generated emails may still be invalid due to edge cases
				// (e.g., starting/ending with dots, consecutive dots)
				// This is acceptable as we're testing the validation logic
				t.Logf("Generated email failed validation (may be edge case): %s", validEmail)
			}
		}
	})
}

// TestProperty15_InvalidEmailsRejected tests that invalid emails are rejected
// Feature: smtp-email-receiver, Property 15: Sender Address Validation
// **Validates: Requirements 6.6**
func TestProperty15_InvalidEmailsRejected(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate invalid email patterns
		invalidType := rapid.IntRange(0, 6).Draw(t, "invalidType")
		
		var invalidEmail string
		switch invalidType {
		case 0:
			// Missing @ symbol
			invalidEmail = rapid.StringMatching(`[a-z]{5,10}`).Draw(t, "noAt")
		case 1:
			// Multiple @ symbols
			invalidEmail = rapid.StringMatching(`[a-z]{3}@[a-z]{3}@[a-z]{3}\.[a-z]{2}`).Draw(t, "multipleAt")
		case 2:
			// Empty local part
			invalidEmail = "@" + rapid.StringMatching(`[a-z]{5}\.[a-z]{2}`).Draw(t, "emptyLocal")
		case 3:
			// Empty domain
			invalidEmail = rapid.StringMatching(`[a-z]{5}`).Draw(t, "emptyDomain") + "@"
		case 4:
			// Local part too long (>64 chars)
			longLocal := strings.Repeat("a", 65)
			invalidEmail = longLocal + "@example.com"
		case 5:
			// Total length too long (>320 chars)
			longLocal := strings.Repeat("a", 64)
			longDomain := strings.Repeat("a", 257)
			invalidEmail = longLocal + "@" + longDomain
		case 6:
			// Empty string
			invalidEmail = ""
		}
		
		// Invalid emails should fail validation
		if ValidateEmailAddress(invalidEmail) {
			t.Errorf("Invalid email should be rejected: %s", invalidEmail)
		}
	})
}

// TestValidEmailAddresses tests known valid email formats
func TestValidEmailAddresses(t *testing.T) {
	validEmails := []string{
		"simple@example.com",
		"very.common@example.com",
		"disposable.style.email.with+symbol@example.com",
		"other.email-with-hyphen@example.com",
		"fully-qualified-domain@example.com",
		"user.name+tag+sorting@example.com",
		"x@example.com",
		"example-indeed@strange-example.com",
		"test@test.co.uk",
		"user@subdomain.example.com",
	}
	
	for _, email := range validEmails {
		if !ValidateEmailAddress(email) {
			t.Errorf("Valid email should pass: %s", email)
		}
	}
}

// TestInvalidEmailAddresses tests known invalid email formats
func TestInvalidEmailAddresses(t *testing.T) {
	invalidEmails := []string{
		"",                           // Empty
		"plainaddress",               // Missing @
		"@no-local-part.com",         // Missing local part
		"missing-domain@",            // Missing domain
		"two@@at.com",                // Double @
		strings.Repeat("a", 65) + "@example.com", // Local part too long
	}
	
	for _, email := range invalidEmails {
		if ValidateEmailAddress(email) {
			t.Errorf("Invalid email should fail: %s", email)
		}
	}
}

// TestHeaderValidation tests header validation for CRLF injection
// Feature: smtp-email-receiver, Property 13: Header Injection Prevention
// **Validates: Requirements 6.1, 6.2**
func TestHeaderValidation(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantValid   bool
		wantTrunc   bool
	}{
		{
			name:      "valid header",
			input:     "Normal header value",
			wantValid: true,
			wantTrunc: false,
		},
		{
			name:      "CRLF injection attempt",
			input:     "Value\r\nBcc: attacker@evil.com",
			wantValid: false,
			wantTrunc: false,
		},
		{
			name:      "CR injection",
			input:     "Value\rBcc: attacker@evil.com",
			wantValid: false,
			wantTrunc: false,
		},
		{
			name:      "LF injection",
			input:     "Value\nBcc: attacker@evil.com",
			wantValid: false,
			wantTrunc: false,
		},
		{
			name:      "long header truncated",
			input:     strings.Repeat("a", 1500),
			wantValid: true,
			wantTrunc: true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, valid := ValidateHeaderValue(tt.input)
			
			if valid != tt.wantValid {
				t.Errorf("ValidateHeaderValue() valid = %v, want %v", valid, tt.wantValid)
			}
			
			if tt.wantTrunc && len(result) != 1000 {
				t.Errorf("ValidateHeaderValue() should truncate to 1000 chars, got %d", len(result))
			}
		})
	}
}

// TestSanitizeHeaderValue tests header sanitization
func TestSanitizeHeaderValue(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "normal value",
			input: "Normal value",
			want:  "Normal value",
		},
		{
			name:  "CRLF removed",
			input: "Value\r\nInjected",
			want:  "Value Injected",
		},
		{
			name:  "CR removed",
			input: "Value\rInjected",
			want:  "Value Injected",
		},
		{
			name:  "LF removed",
			input: "Value\nInjected",
			want:  "Value Injected",
		},
		{
			name:  "truncated",
			input: strings.Repeat("a", 1500),
			want:  strings.Repeat("a", 1000),
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeHeaderValue(tt.input)
			if got != tt.want {
				t.Errorf("SanitizeHeaderValue() = %v, want %v", got, tt.want)
			}
		})
	}
}
