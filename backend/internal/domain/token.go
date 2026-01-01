package domain

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

const (
	// TokenPrefix is the prefix for verification tokens
	TokenPrefix = "vrf_"
	// TokenByteLength is the number of random bytes (32 bytes = 64 hex chars)
	TokenByteLength = 32
	// TokenTotalLength is the total length of the token (prefix + hex encoded bytes)
	TokenTotalLength = 68 // 4 (prefix) + 64 (hex)
)

// GenerateVerificationToken generates a cryptographically secure verification token
// Format: vrf_ + 64 hex characters (32 random bytes)
// Total length: 68 characters
// Requirements: FR-DOM-002, NFR-2 (Security)
func GenerateVerificationToken() (string, error) {
	bytes := make([]byte, TokenByteLength)
	
	_, err := rand.Read(bytes)
	if err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	
	token := TokenPrefix + hex.EncodeToString(bytes)
	return token, nil
}

// ValidateVerificationToken checks if a token has the correct format
func ValidateVerificationToken(token string) bool {
	if len(token) != TokenTotalLength {
		return false
	}
	
	if token[:4] != TokenPrefix {
		return false
	}
	
	// Check if the rest is valid hex
	_, err := hex.DecodeString(token[4:])
	return err == nil
}
