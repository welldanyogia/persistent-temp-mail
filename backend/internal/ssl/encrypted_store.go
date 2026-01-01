// Package ssl provides SSL certificate management functionality
// Requirements: 2.1, 2.2, 2.3, 2.5, 2.6 - Encrypted certificate storage
package ssl

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Custom errors for encrypted store operations
var (
	ErrInvalidKeyLength    = errors.New("encryption key must be 32 bytes for AES-256")
	ErrCiphertextTooShort  = errors.New("ciphertext too short")
	ErrCertificateNotExist = errors.New("certificate does not exist")
	ErrInvalidCertificate  = errors.New("invalid certificate data")
	ErrInvalidPrivateKey   = errors.New("invalid private key data")
	ErrBackupFailed        = errors.New("backup operation failed")
)

// CertificateStore defines the interface for certificate storage operations
// Requirements: 2.1, 2.5 - Store certificates in encrypted file storage
type CertificateStore interface {
	// Store encrypts and stores certificate, key, and chain
	Store(domainID string, cert []byte, key []byte, chain []byte) error

	// Load decrypts and returns a tls.Certificate
	Load(domainID string) (*tls.Certificate, error)

	// Delete removes certificate files for a domain
	Delete(domainID string) error

	// Exists checks if certificate files exist for a domain
	Exists(domainID string) bool

	// Backup creates a backup of certificate files
	Backup(domainID string) error
}

// EncryptedStore implements CertificateStore with AES-256-GCM encryption
// Requirements: 2.1, 2.6 - Encrypt private keys at rest using AES-256
type EncryptedStore struct {
	basePath string
	key      []byte // AES-256 key (32 bytes)
}

// NewEncryptedStore creates a new EncryptedStore instance
// Requirements: 2.6 - Encrypt private keys at rest using AES-256
func NewEncryptedStore(basePath string, encryptionKey []byte) (*EncryptedStore, error) {
	if len(encryptionKey) != 32 {
		return nil, ErrInvalidKeyLength
	}

	// Ensure base directory exists with restrictive permissions
	if err := os.MkdirAll(basePath, 0700); err != nil {
		return nil, fmt.Errorf("failed to create base directory: %w", err)
	}

	// Create certificates subdirectory
	certsPath := filepath.Join(basePath, "certificates")
	if err := os.MkdirAll(certsPath, 0700); err != nil {
		return nil, fmt.Errorf("failed to create certificates directory: %w", err)
	}

	// Create backup subdirectory
	backupPath := filepath.Join(basePath, "backup")
	if err := os.MkdirAll(backupPath, 0700); err != nil {
		return nil, fmt.Errorf("failed to create backup directory: %w", err)
	}

	return &EncryptedStore{
		basePath: basePath,
		key:      encryptionKey,
	}, nil
}


// Store encrypts and stores certificate, private key, and chain files
// Requirements: 2.1 - Store certificates in encrypted file storage
// Requirements: 2.2 - Store private keys with restrictive file permissions (600)
// Requirements: 2.6 - Encrypt private keys at rest using AES-256
func (s *EncryptedStore) Store(domainID string, cert []byte, key []byte, chain []byte) error {
	if domainID == "" {
		return errors.New("domain ID cannot be empty")
	}

	domainPath := filepath.Join(s.basePath, "certificates", domainID)
	if err := os.MkdirAll(domainPath, 0700); err != nil {
		return fmt.Errorf("failed to create domain directory: %w", err)
	}

	// Create fullchain by combining cert and chain
	fullchain := append(cert, chain...)

	// Define files to store
	files := map[string][]byte{
		"cert.pem.enc":      cert,
		"privkey.pem.enc":   key,
		"chain.pem.enc":     chain,
		"fullchain.pem.enc": fullchain,
	}

	// Encrypt and store each file
	for filename, data := range files {
		encrypted, err := s.encrypt(data)
		if err != nil {
			// Clean up on failure
			_ = os.RemoveAll(domainPath)
			return fmt.Errorf("failed to encrypt %s: %w", filename, err)
		}

		path := filepath.Join(domainPath, filename)
		// Requirements: 2.2 - Store private keys with restrictive file permissions (600)
		if err := os.WriteFile(path, encrypted, 0600); err != nil {
			// Clean up on failure
			_ = os.RemoveAll(domainPath)
			return fmt.Errorf("failed to write %s: %w", filename, err)
		}
	}

	return nil
}

// Load decrypts and returns a tls.Certificate from stored files
// Requirements: 2.7 - Load certificates into memory on server startup
func (s *EncryptedStore) Load(domainID string) (*tls.Certificate, error) {
	if domainID == "" {
		return nil, errors.New("domain ID cannot be empty")
	}

	domainPath := filepath.Join(s.basePath, "certificates", domainID)

	// Read encrypted fullchain
	fullchainEnc, err := os.ReadFile(filepath.Join(domainPath, "fullchain.pem.enc"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrCertificateNotExist
		}
		return nil, fmt.Errorf("failed to read certificate: %w", err)
	}

	// Read encrypted private key
	keyEnc, err := os.ReadFile(filepath.Join(domainPath, "privkey.pem.enc"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrCertificateNotExist
		}
		return nil, fmt.Errorf("failed to read private key: %w", err)
	}

	// Decrypt fullchain
	fullchainPEM, err := s.decrypt(fullchainEnc)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt certificate: %w", err)
	}

	// Decrypt private key
	keyPEM, err := s.decrypt(keyEnc)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt private key: %w", err)
	}

	// Parse certificate
	cert, err := tls.X509KeyPair(fullchainPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	// Parse the leaf certificate for additional validation
	if len(cert.Certificate) > 0 {
		leaf, err := x509.ParseCertificate(cert.Certificate[0])
		if err != nil {
			return nil, fmt.Errorf("failed to parse leaf certificate: %w", err)
		}
		cert.Leaf = leaf
	}

	return &cert, nil
}

// Delete removes all certificate files for a domain
func (s *EncryptedStore) Delete(domainID string) error {
	if domainID == "" {
		return errors.New("domain ID cannot be empty")
	}

	domainPath := filepath.Join(s.basePath, "certificates", domainID)
	
	// Check if directory exists
	if _, err := os.Stat(domainPath); os.IsNotExist(err) {
		return nil // Already deleted, not an error
	}

	return os.RemoveAll(domainPath)
}

// Exists checks if certificate files exist for a domain
func (s *EncryptedStore) Exists(domainID string) bool {
	if domainID == "" {
		return false
	}

	domainPath := filepath.Join(s.basePath, "certificates", domainID)
	certPath := filepath.Join(domainPath, "cert.pem.enc")
	
	_, err := os.Stat(certPath)
	return err == nil
}


// Backup creates a backup of certificate files to the backup directory
// Requirements: 2.5 - Support certificate backup to secure location
func (s *EncryptedStore) Backup(domainID string) error {
	if domainID == "" {
		return errors.New("domain ID cannot be empty")
	}

	sourcePath := filepath.Join(s.basePath, "certificates", domainID)
	
	// Check if source exists
	if _, err := os.Stat(sourcePath); os.IsNotExist(err) {
		return ErrCertificateNotExist
	}

	// Create backup directory with timestamp
	timestamp := time.Now().UTC().Format("2006-01-02")
	backupPath := filepath.Join(s.basePath, "backup", timestamp, domainID)
	
	if err := os.MkdirAll(backupPath, 0700); err != nil {
		return fmt.Errorf("failed to create backup directory: %w", err)
	}

	// List of files to backup
	files := []string{
		"cert.pem.enc",
		"privkey.pem.enc",
		"chain.pem.enc",
		"fullchain.pem.enc",
	}

	for _, filename := range files {
		srcFile := filepath.Join(sourcePath, filename)
		dstFile := filepath.Join(backupPath, filename)

		// Read source file
		data, err := os.ReadFile(srcFile)
		if err != nil {
			if os.IsNotExist(err) {
				continue // Skip missing files
			}
			return fmt.Errorf("failed to read %s for backup: %w", filename, err)
		}

		// Write to backup location with same permissions
		if err := os.WriteFile(dstFile, data, 0600); err != nil {
			return fmt.Errorf("failed to write backup %s: %w", filename, err)
		}
	}

	return nil
}

// encrypt encrypts plaintext using AES-256-GCM
// Requirements: 2.6 - Encrypt private keys at rest using AES-256
func (s *EncryptedStore) encrypt(plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Generate random nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt and prepend nonce to ciphertext
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// decrypt decrypts ciphertext using AES-256-GCM
func (s *EncryptedStore) decrypt(ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, ErrCiphertextTooShort
	}

	// Extract nonce and ciphertext
	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	
	// Decrypt
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt: %w", err)
	}

	return plaintext, nil
}

// GetCertificateInfo extracts certificate information from stored files
// This is useful for getting metadata without loading the full certificate
func (s *EncryptedStore) GetCertificateInfo(domainID string) (*CertificateFileInfo, error) {
	cert, err := s.Load(domainID)
	if err != nil {
		return nil, err
	}

	if cert.Leaf == nil {
		return nil, ErrInvalidCertificate
	}

	return &CertificateFileInfo{
		DomainID:     domainID,
		Subject:      cert.Leaf.Subject.CommonName,
		Issuer:       cert.Leaf.Issuer.CommonName,
		SerialNumber: cert.Leaf.SerialNumber.String(),
		NotBefore:    cert.Leaf.NotBefore,
		NotAfter:     cert.Leaf.NotAfter,
		DNSNames:     cert.Leaf.DNSNames,
	}, nil
}

// CertificateFileInfo contains metadata about a stored certificate
type CertificateFileInfo struct {
	DomainID     string
	Subject      string
	Issuer       string
	SerialNumber string
	NotBefore    time.Time
	NotAfter     time.Time
	DNSNames     []string
}

// ValidateCertificateData validates that the provided data is a valid PEM certificate
func ValidateCertificateData(certPEM []byte) error {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return ErrInvalidCertificate
	}

	if block.Type != "CERTIFICATE" {
		return fmt.Errorf("expected CERTIFICATE, got %s", block.Type)
	}

	_, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse certificate: %w", err)
	}

	return nil
}

// ValidatePrivateKeyData validates that the provided data is a valid PEM private key
func ValidatePrivateKeyData(keyPEM []byte) error {
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return ErrInvalidPrivateKey
	}

	// Accept various private key types
	validTypes := []string{
		"RSA PRIVATE KEY",
		"EC PRIVATE KEY",
		"PRIVATE KEY", // PKCS#8
	}

	isValid := false
	for _, t := range validTypes {
		if block.Type == t {
			isValid = true
			break
		}
	}

	if !isValid {
		return fmt.Errorf("invalid private key type: %s", block.Type)
	}

	return nil
}

// ListBackups returns a list of backup timestamps for a domain
func (s *EncryptedStore) ListBackups(domainID string) ([]string, error) {
	backupBase := filepath.Join(s.basePath, "backup")
	
	entries, err := os.ReadDir(backupBase)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("failed to read backup directory: %w", err)
	}

	var backups []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Check if this date directory contains a backup for the domain
		domainBackupPath := filepath.Join(backupBase, entry.Name(), domainID)
		if _, err := os.Stat(domainBackupPath); err == nil {
			backups = append(backups, entry.Name())
		}
	}

	return backups, nil
}

// RestoreFromBackup restores certificate files from a backup
func (s *EncryptedStore) RestoreFromBackup(domainID string, timestamp string) error {
	if domainID == "" || timestamp == "" {
		return errors.New("domain ID and timestamp cannot be empty")
	}

	backupPath := filepath.Join(s.basePath, "backup", timestamp, domainID)
	
	// Check if backup exists
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return fmt.Errorf("backup not found for domain %s at %s", domainID, timestamp)
	}

	destPath := filepath.Join(s.basePath, "certificates", domainID)
	
	// Create destination directory
	if err := os.MkdirAll(destPath, 0700); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// List of files to restore
	files := []string{
		"cert.pem.enc",
		"privkey.pem.enc",
		"chain.pem.enc",
		"fullchain.pem.enc",
	}

	for _, filename := range files {
		srcFile := filepath.Join(backupPath, filename)
		dstFile := filepath.Join(destPath, filename)

		// Read backup file
		data, err := os.ReadFile(srcFile)
		if err != nil {
			if os.IsNotExist(err) {
				continue // Skip missing files
			}
			return fmt.Errorf("failed to read backup %s: %w", filename, err)
		}

		// Write to destination with restrictive permissions
		if err := os.WriteFile(dstFile, data, 0600); err != nil {
			return fmt.Errorf("failed to restore %s: %w", filename, err)
		}
	}

	return nil
}

// Ensure EncryptedStore implements CertificateStore interface
var _ CertificateStore = (*EncryptedStore)(nil)
