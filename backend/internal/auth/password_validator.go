package auth

import (
	"unicode"

	"golang.org/x/crypto/bcrypt"
)

const (
	// MinPasswordLength is the minimum required password length
	MinPasswordLength = 8
	// BcryptCost is the cost factor for bcrypt hashing
	BcryptCost = 12
)

// PasswordValidationError represents a specific password validation failure
type PasswordValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// PasswordValidator handles password validation and hashing
type PasswordValidator struct{}

// NewPasswordValidator creates a new PasswordValidator instance
func NewPasswordValidator() *PasswordValidator {
	return &PasswordValidator{}
}

// ValidatePassword checks if a password meets all complexity requirements
// Returns a list of validation errors (empty if password is valid)
// Requirements: 1.4, 1.5, 6.1-6.5
func (v *PasswordValidator) ValidatePassword(password string) []PasswordValidationError {
	var errors []PasswordValidationError

	// Check minimum length (Requirement 6.1)
	if len(password) < MinPasswordLength {
		errors = append(errors, PasswordValidationError{
			Field:   "password",
			Message: "Password must be at least 8 characters long",
		})
	}

	var hasUpper, hasLower, hasNumber, hasSpecial bool

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

	// Check uppercase (Requirement 6.2)
	if !hasUpper {
		errors = append(errors, PasswordValidationError{
			Field:   "password",
			Message: "Password must contain at least one uppercase letter",
		})
	}

	// Check lowercase (Requirement 6.3)
	if !hasLower {
		errors = append(errors, PasswordValidationError{
			Field:   "password",
			Message: "Password must contain at least one lowercase letter",
		})
	}

	// Check number (Requirement 6.4)
	if !hasNumber {
		errors = append(errors, PasswordValidationError{
			Field:   "password",
			Message: "Password must contain at least one number",
		})
	}

	// Check special character (Requirement 6.5)
	if !hasSpecial {
		errors = append(errors, PasswordValidationError{
			Field:   "password",
			Message: "Password must contain at least one special character",
		})
	}

	return errors
}

// IsValidPassword returns true if the password meets all requirements
func (v *PasswordValidator) IsValidPassword(password string) bool {
	return len(v.ValidatePassword(password)) == 0
}

// HashPassword creates a bcrypt hash of the password with cost factor 12
// Requirement: 1.7
func (v *PasswordValidator) HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), BcryptCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// VerifyPassword compares a password with its bcrypt hash
// Returns nil if they match, error otherwise
func (v *PasswordValidator) VerifyPassword(password, hash string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

// GetBcryptCost extracts the cost factor from a bcrypt hash
func GetBcryptCost(hash string) (int, error) {
	return bcrypt.Cost([]byte(hash))
}
