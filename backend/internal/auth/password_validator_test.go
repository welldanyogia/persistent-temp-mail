package auth

import (
	"testing"
	"unicode"

	"golang.org/x/crypto/bcrypt"
	"pgregory.net/rapid"
)

// Feature: user-authentication, Property 3: Password Complexity Validation
// **Validates: Requirements 1.4, 1.5, 6.1, 6.2, 6.3, 6.4, 6.5**
func TestProperty3_PasswordComplexityValidation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		validator := NewPasswordValidator()
		password := rapid.StringN(0, 20, 20).Draw(t, "password")

		hasUpper, hasLower, hasNumber, hasSpecial := false, false, false, false
		for _, char := range password {
			switch {
			case unicode.IsUpper(char):
				hasUpper = true
			case unicode.IsLower(char):
				hasLower = true
			case unicode.IsDigit(char):
				hasNumber = true
			case unicode.IsPunct(char) || unicode.IsSymbol(char):
				hasSpecial = true
			}
		}

		errors := validator.ValidatePassword(password)
		expectedErrorCount := 0
		if len(password) < MinPasswordLength {
			expectedErrorCount++
		}
		if !hasUpper {
			expectedErrorCount++
		}
		if !hasLower {
			expectedErrorCount++
		}
		if !hasNumber {
			expectedErrorCount++
		}
		if !hasSpecial {
			expectedErrorCount++
		}

		if len(errors) != expectedErrorCount {
			t.Errorf("expected %d errors, got %d", expectedErrorCount, len(errors))
		}
	})
}
