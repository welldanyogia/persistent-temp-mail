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

// Feature: user-authentication, Property 5: Password Stored as Bcrypt Hash
// **Validates: Requirements 1.7**
func TestProperty5_PasswordStoredAsBcryptHash(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		validator := NewPasswordValidator()
		password := rapid.StringN(1, 20, 20).Draw(t, "password")

		hash, err := validator.HashPassword(password)
		if err != nil {
			t.Fatalf("failed to hash password: %v", err)
		}

		if hash == password {
			t.Error("hash should not equal the original password")
		}

		prefix := hash[:4]
		if prefix != "$2a$" && prefix != "$2b$" && prefix != "$2y$" {
			t.Errorf("hash should start with bcrypt prefix, got: %s", prefix)
		}

		cost, err := GetBcryptCost(hash)
		if err != nil {
			t.Fatalf("failed to get bcrypt cost: %v", err)
		}
		if cost != BcryptCost {
			t.Errorf("expected cost factor %d, got %d", BcryptCost, cost)
		}

		err = validator.VerifyPassword(password, hash)
		if err != nil {
			t.Errorf("VerifyPassword failed for correct password: %v", err)
		}

		wrongPassword := password + "wrong"
		err = validator.VerifyPassword(wrongPassword, hash)
		if err == nil {
			t.Error("VerifyPassword should fail for wrong password")
		}

		_, err = bcrypt.Cost([]byte(hash))
		if err != nil {
			t.Errorf("hash should be valid bcrypt format: %v", err)
		}
	})
}

func TestValidatePassword_EdgeCases(t *testing.T) {
	validator := NewPasswordValidator()

	tests := []struct {
		name           string
		password       string
		expectedErrors int
	}{
		{"empty password", "", 5},
		{"valid password", "Password1!", 0},
		{"missing uppercase", "password1!", 1},
		{"missing lowercase", "PASSWORD1!", 1},
		{"missing number", "Password!", 1},
		{"missing special", "Password1", 1},
		{"too short", "Pass1!", 1},
		{"exactly 8 chars valid", "Pass12!a", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors := validator.ValidatePassword(tt.password)
			if len(errors) != tt.expectedErrors {
				t.Errorf("expected %d errors, got %d: %v", tt.expectedErrors, len(errors), errors)
			}
		})
	}
}
