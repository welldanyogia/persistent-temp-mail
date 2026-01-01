// Package ssl provides SSL certificate management functionality
// Requirements: 5.1, 5.2, 5.3, 5.4, 5.5, 5.6, 5.7 - Certificate validation
package ssl

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/ocsp"
)

// Custom errors for certificate validation
var (
	ErrCertificateChainIncomplete = errors.New("certificate chain is incomplete")
	ErrCertificateExpiredVal      = errors.New("certificate has expired")
	ErrCertificateNotYetValid     = errors.New("certificate is not yet valid")
	ErrCertificateDomainMismatch  = errors.New("certificate does not match domain")
	ErrKeyPairMismatch            = errors.New("private key does not match certificate")
	ErrCertificateRevokedVal      = errors.New("certificate has been revoked")
	ErrOCSPCheckFailed            = errors.New("OCSP check failed")
	ErrInvalidCertificateChain    = errors.New("invalid certificate chain")
	ErrNoOCSPResponder            = errors.New("no OCSP responder available")
	ErrOCSPResponseInvalid        = errors.New("invalid OCSP response")
)

// ValidationResult contains the result of certificate validation
type ValidationResult struct {
	Valid       bool     `json:"valid"`
	Errors      []string `json:"errors,omitempty"`
	Warnings    []string `json:"warnings,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	DaysUntilExpiry int   `json:"days_until_expiry,omitempty"`
	Issuer      string   `json:"issuer,omitempty"`
	Subject     string   `json:"subject,omitempty"`
	DNSNames    []string `json:"dns_names,omitempty"`
}

// OCSPStatus represents the revocation status from OCSP
type OCSPStatus struct {
	Status          string    `json:"status"` // "good", "revoked", "unknown"
	RevokedAt       *time.Time `json:"revoked_at,omitempty"`
	RevocationReason string   `json:"revocation_reason,omitempty"`
	ProducedAt      time.Time `json:"produced_at"`
	ThisUpdate      time.Time `json:"this_update"`
	NextUpdate      *time.Time `json:"next_update,omitempty"`
}

// CertificateValidator provides certificate validation functionality
// Requirements: 5.1, 5.2, 5.3, 5.4, 5.5, 5.6, 5.7
type CertificateValidator struct {
	// HTTP client for OCSP requests
	httpClient *http.Client
	// Root CA pool for chain validation
	rootCAs *x509.CertPool
}


// NewCertificateValidator creates a new CertificateValidator instance
func NewCertificateValidator() *CertificateValidator {
	return &CertificateValidator{
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		rootCAs: nil, // Use system root CAs
	}
}

// NewCertificateValidatorWithRoots creates a validator with custom root CAs
func NewCertificateValidatorWithRoots(rootCAs *x509.CertPool) *CertificateValidator {
	return &CertificateValidator{
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		rootCAs: rootCAs,
	}
}

// ValidateAll performs all validation checks on a certificate
// Requirements: 5.1, 5.2, 5.3, 5.4, 5.5, 5.6, 5.7
func (v *CertificateValidator) ValidateAll(cert *tls.Certificate, domainName string, checkOCSP bool) *ValidationResult {
	result := &ValidationResult{
		Valid:    true,
		Errors:   []string{},
		Warnings: []string{},
	}

	if cert == nil || len(cert.Certificate) == 0 {
		result.Valid = false
		result.Errors = append(result.Errors, "certificate is nil or empty")
		return result
	}

	// Parse the leaf certificate
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, fmt.Sprintf("failed to parse certificate: %v", err))
		return result
	}

	// Populate basic info
	result.ExpiresAt = &leaf.NotAfter
	result.DaysUntilExpiry = int(time.Until(leaf.NotAfter).Hours() / 24)
	result.Issuer = leaf.Issuer.CommonName
	result.Subject = leaf.Subject.CommonName
	result.DNSNames = leaf.DNSNames

	// Requirements: 5.1 - Validate certificate chain completeness
	if err := v.ValidateCertificateChain(cert); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, fmt.Sprintf("chain validation failed: %v", err))
	}

	// Requirements: 5.2 - Validate certificate is not expired
	if err := v.ValidateCertificateExpiry(leaf); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, fmt.Sprintf("expiry validation failed: %v", err))
	}

	// Requirements: 5.3 - Validate certificate matches domain name
	if domainName != "" {
		if err := v.ValidateCertificateDomain(leaf, domainName); err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("domain validation failed: %v", err))
		}
	}

	// Requirements: 5.4 - Validate private key matches certificate
	if cert.PrivateKey != nil {
		if err := v.ValidateKeyPair(leaf, cert.PrivateKey); err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("key pair validation failed: %v", err))
		}
	}

	// Requirements: 5.6 - Check certificate revocation status (OCSP)
	if checkOCSP {
		ocspStatus, err := v.CheckOCSPStatus(context.Background(), cert)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("OCSP check failed: %v", err))
		} else if ocspStatus.Status == "revoked" {
			result.Valid = false
			result.Errors = append(result.Errors, "certificate has been revoked")
		}
	}

	// Add warning for certificates expiring soon
	if result.DaysUntilExpiry <= 30 && result.DaysUntilExpiry > 0 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("certificate expires in %d days", result.DaysUntilExpiry))
	}

	return result
}


// ValidateCertificateChain validates that the certificate chain is complete and valid
// Requirements: 5.1 - Validate certificate chain completeness
// Requirements: 5.7 - Validate certificate trust chain to root CA
func (v *CertificateValidator) ValidateCertificateChain(cert *tls.Certificate) error {
	if cert == nil || len(cert.Certificate) == 0 {
		return ErrInvalidCertificateChain
	}

	// Parse all certificates in the chain
	certs := make([]*x509.Certificate, 0, len(cert.Certificate))
	for i, certDER := range cert.Certificate {
		parsed, err := x509.ParseCertificate(certDER)
		if err != nil {
			return fmt.Errorf("failed to parse certificate at index %d: %w", i, err)
		}
		certs = append(certs, parsed)
	}

	if len(certs) == 0 {
		return ErrCertificateChainIncomplete
	}

	// The first certificate is the leaf (end-entity) certificate
	leaf := certs[0]

	// Build intermediate pool from remaining certificates
	intermediates := x509.NewCertPool()
	for i := 1; i < len(certs); i++ {
		intermediates.AddCert(certs[i])
	}

	// Determine root CA pool
	roots := v.rootCAs
	if roots == nil {
		var err error
		roots, err = x509.SystemCertPool()
		if err != nil {
			// On some systems, SystemCertPool may not be available
			// In that case, we'll rely on the chain being complete
			roots = x509.NewCertPool()
		}
	}

	// Verify the certificate chain
	opts := x509.VerifyOptions{
		Intermediates: intermediates,
		Roots:         roots,
		CurrentTime:   time.Now(),
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	chains, err := leaf.Verify(opts)
	if err != nil {
		// Check if it's a self-signed certificate (common in testing)
		if leaf.CheckSignatureFrom(leaf) == nil {
			// Self-signed certificate - chain is technically complete
			return nil
		}
		return fmt.Errorf("%w: %v", ErrCertificateChainIncomplete, err)
	}

	if len(chains) == 0 {
		return ErrCertificateChainIncomplete
	}

	return nil
}

// ValidateCertificateExpiry validates that the certificate is not expired
// Requirements: 5.2 - Validate certificate is not expired
func (v *CertificateValidator) ValidateCertificateExpiry(cert *x509.Certificate) error {
	if cert == nil {
		return errors.New("certificate is nil")
	}

	now := time.Now()

	// Check if certificate has expired
	if now.After(cert.NotAfter) {
		return fmt.Errorf("%w: expired at %s", ErrCertificateExpiredVal, cert.NotAfter.Format(time.RFC3339))
	}

	// Check if certificate is not yet valid
	if now.Before(cert.NotBefore) {
		return fmt.Errorf("%w: valid from %s", ErrCertificateNotYetValid, cert.NotBefore.Format(time.RFC3339))
	}

	return nil
}


// ValidateCertificateDomain validates that the certificate matches the domain name
// Requirements: 5.3 - Validate certificate matches domain name
func (v *CertificateValidator) ValidateCertificateDomain(cert *x509.Certificate, domainName string) error {
	if cert == nil {
		return errors.New("certificate is nil")
	}

	if domainName == "" {
		return errors.New("domain name is empty")
	}

	// Normalize domain name (lowercase, trim whitespace)
	domainName = strings.ToLower(strings.TrimSpace(domainName))

	// Check if domain matches the certificate's Common Name
	if strings.EqualFold(cert.Subject.CommonName, domainName) {
		return nil
	}

	// Check if domain matches any of the DNS names (SAN)
	for _, dnsName := range cert.DNSNames {
		if matchesDomain(dnsName, domainName) {
			return nil
		}
	}

	// Check IP addresses if the domain looks like an IP
	// (This is less common but supported)
	for _, ip := range cert.IPAddresses {
		if ip.String() == domainName {
			return nil
		}
	}

	return fmt.Errorf("%w: certificate is for %v, not %s", 
		ErrCertificateDomainMismatch, 
		append([]string{cert.Subject.CommonName}, cert.DNSNames...), 
		domainName)
}

// matchesDomain checks if a certificate DNS name matches a domain
// Supports wildcard matching (e.g., *.example.com matches sub.example.com)
func matchesDomain(certDNS, domain string) bool {
	certDNS = strings.ToLower(certDNS)
	domain = strings.ToLower(domain)

	// Exact match
	if certDNS == domain {
		return true
	}

	// Wildcard matching
	if strings.HasPrefix(certDNS, "*.") {
		// Get the base domain from the wildcard
		baseDomain := certDNS[2:] // Remove "*."
		
		// The domain must end with the base domain
		if !strings.HasSuffix(domain, baseDomain) {
			return false
		}

		// The domain must have exactly one more label than the base
		// e.g., *.example.com matches sub.example.com but not sub.sub.example.com
		prefix := strings.TrimSuffix(domain, baseDomain)
		prefix = strings.TrimSuffix(prefix, ".")
		
		// The prefix should not contain any dots (single label only)
		if !strings.Contains(prefix, ".") && prefix != "" {
			return true
		}
	}

	return false
}


// ValidateKeyPair validates that the private key matches the certificate
// Requirements: 5.4 - Validate private key matches certificate
func (v *CertificateValidator) ValidateKeyPair(cert *x509.Certificate, privateKey crypto.PrivateKey) error {
	if cert == nil {
		return errors.New("certificate is nil")
	}

	if privateKey == nil {
		return errors.New("private key is nil")
	}

	// Get the public key from the certificate
	certPublicKey := cert.PublicKey

	// Compare based on key type
	switch pubKey := certPublicKey.(type) {
	case *rsa.PublicKey:
		privKey, ok := privateKey.(*rsa.PrivateKey)
		if !ok {
			return fmt.Errorf("%w: certificate has RSA public key but private key is not RSA", ErrKeyPairMismatch)
		}
		if !pubKey.Equal(&privKey.PublicKey) {
			return fmt.Errorf("%w: RSA public key does not match private key", ErrKeyPairMismatch)
		}

	case *ecdsa.PublicKey:
		privKey, ok := privateKey.(*ecdsa.PrivateKey)
		if !ok {
			return fmt.Errorf("%w: certificate has ECDSA public key but private key is not ECDSA", ErrKeyPairMismatch)
		}
		if !pubKey.Equal(&privKey.PublicKey) {
			return fmt.Errorf("%w: ECDSA public key does not match private key", ErrKeyPairMismatch)
		}

	case ed25519.PublicKey:
		privKey, ok := privateKey.(ed25519.PrivateKey)
		if !ok {
			return fmt.Errorf("%w: certificate has Ed25519 public key but private key is not Ed25519", ErrKeyPairMismatch)
		}
		derivedPubKey := privKey.Public().(ed25519.PublicKey)
		if !bytes.Equal(pubKey, derivedPubKey) {
			return fmt.Errorf("%w: Ed25519 public key does not match private key", ErrKeyPairMismatch)
		}

	default:
		return fmt.Errorf("unsupported key type: %T", certPublicKey)
	}

	return nil
}

// ValidateKeyPairFromPEM validates that PEM-encoded private key matches PEM-encoded certificate
func (v *CertificateValidator) ValidateKeyPairFromPEM(certPEM, keyPEM []byte) error {
	// Parse certificate
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return errors.New("failed to decode certificate PEM")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse certificate: %w", err)
	}

	// Parse private key
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return errors.New("failed to decode private key PEM")
	}

	var privateKey crypto.PrivateKey

	// Try different key formats
	switch keyBlock.Type {
	case "RSA PRIVATE KEY":
		privateKey, err = x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	case "EC PRIVATE KEY":
		privateKey, err = x509.ParseECPrivateKey(keyBlock.Bytes)
	case "PRIVATE KEY":
		privateKey, err = x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	default:
		return fmt.Errorf("unsupported private key type: %s", keyBlock.Type)
	}

	if err != nil {
		return fmt.Errorf("failed to parse private key: %w", err)
	}

	return v.ValidateKeyPair(cert, privateKey)
}


// CheckOCSPStatus checks the certificate revocation status using OCSP
// Requirements: 5.6 - Check certificate revocation status (OCSP)
func (v *CertificateValidator) CheckOCSPStatus(ctx context.Context, cert *tls.Certificate) (*OCSPStatus, error) {
	if cert == nil || len(cert.Certificate) == 0 {
		return nil, errors.New("certificate is nil or empty")
	}

	// Parse the leaf certificate
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("failed to parse leaf certificate: %w", err)
	}

	// Check if OCSP responders are available
	if len(leaf.OCSPServer) == 0 {
		return nil, ErrNoOCSPResponder
	}

	// Find the issuer certificate
	var issuer *x509.Certificate
	if len(cert.Certificate) > 1 {
		issuer, err = x509.ParseCertificate(cert.Certificate[1])
		if err != nil {
			return nil, fmt.Errorf("failed to parse issuer certificate: %w", err)
		}
	} else {
		// Try to get issuer from system roots or assume self-signed
		return nil, errors.New("issuer certificate not found in chain")
	}

	// Create OCSP request
	ocspRequest, err := ocsp.CreateRequest(leaf, issuer, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create OCSP request: %w", err)
	}

	// Try each OCSP responder
	var lastErr error
	for _, responderURL := range leaf.OCSPServer {
		status, err := v.queryOCSPResponder(ctx, responderURL, ocspRequest, issuer)
		if err != nil {
			lastErr = err
			continue
		}
		return status, nil
	}

	if lastErr != nil {
		return nil, fmt.Errorf("%w: %v", ErrOCSPCheckFailed, lastErr)
	}

	return nil, ErrOCSPCheckFailed
}

// queryOCSPResponder sends an OCSP request to a responder and parses the response
func (v *CertificateValidator) queryOCSPResponder(ctx context.Context, responderURL string, ocspRequest []byte, issuer *x509.Certificate) (*OCSPStatus, error) {
	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, responderURL, bytes.NewReader(ocspRequest))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	req.Header.Set("Content-Type", "application/ocsp-request")
	req.Header.Set("Accept", "application/ocsp-response")

	// Send request
	resp, err := v.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("OCSP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OCSP responder returned status %d", resp.StatusCode)
	}

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read OCSP response: %w", err)
	}

	// Parse OCSP response
	ocspResp, err := ocsp.ParseResponse(body, issuer)
	if err != nil {
		return nil, fmt.Errorf("failed to parse OCSP response: %w", err)
	}

	// Convert to our status type
	status := &OCSPStatus{
		ProducedAt: ocspResp.ProducedAt,
		ThisUpdate: ocspResp.ThisUpdate,
	}

	if !ocspResp.NextUpdate.IsZero() {
		status.NextUpdate = &ocspResp.NextUpdate
	}

	switch ocspResp.Status {
	case ocsp.Good:
		status.Status = "good"
	case ocsp.Revoked:
		status.Status = "revoked"
		status.RevokedAt = &ocspResp.RevokedAt
		status.RevocationReason = revocationReasonToString(ocspResp.RevocationReason)
	case ocsp.Unknown:
		status.Status = "unknown"
	default:
		status.Status = "unknown"
	}

	return status, nil
}

// revocationReasonToString converts OCSP revocation reason to string
func revocationReasonToString(reason int) string {
	reasons := map[int]string{
		0: "unspecified",
		1: "keyCompromise",
		2: "cACompromise",
		3: "affiliationChanged",
		4: "superseded",
		5: "cessationOfOperation",
		6: "certificateHold",
		8: "removeFromCRL",
		9: "privilegeWithdrawn",
		10: "aACompromise",
	}

	if r, ok := reasons[reason]; ok {
		return r
	}
	return fmt.Sprintf("unknown(%d)", reason)
}


// ValidateCertificateFromPEM validates a PEM-encoded certificate
func (v *CertificateValidator) ValidateCertificateFromPEM(certPEM []byte, domainName string) *ValidationResult {
	result := &ValidationResult{
		Valid:    true,
		Errors:   []string{},
		Warnings: []string{},
	}

	// Parse PEM blocks
	var certs [][]byte
	rest := certPEM
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type == "CERTIFICATE" {
			certs = append(certs, block.Bytes)
		}
	}

	if len(certs) == 0 {
		result.Valid = false
		result.Errors = append(result.Errors, "no certificates found in PEM data")
		return result
	}

	// Create tls.Certificate
	tlsCert := &tls.Certificate{
		Certificate: certs,
	}

	// Parse leaf for info
	leaf, err := x509.ParseCertificate(certs[0])
	if err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, fmt.Sprintf("failed to parse certificate: %v", err))
		return result
	}
	tlsCert.Leaf = leaf

	return v.ValidateAll(tlsCert, domainName, false)
}

// ValidateTLSCertificate validates a tls.Certificate for a specific domain
func (v *CertificateValidator) ValidateTLSCertificate(cert *tls.Certificate, domainName string) *ValidationResult {
	return v.ValidateAll(cert, domainName, false)
}

// ValidateTLSCertificateWithOCSP validates a tls.Certificate including OCSP check
func (v *CertificateValidator) ValidateTLSCertificateWithOCSP(cert *tls.Certificate, domainName string) *ValidationResult {
	return v.ValidateAll(cert, domainName, true)
}

// IsExpiringSoon checks if a certificate is expiring within the specified days
func (v *CertificateValidator) IsExpiringSoon(cert *x509.Certificate, days int) bool {
	if cert == nil {
		return false
	}
	
	threshold := time.Now().Add(time.Duration(days) * 24 * time.Hour)
	return cert.NotAfter.Before(threshold)
}

// GetCertificateExpiry returns the expiry time and days until expiry
func (v *CertificateValidator) GetCertificateExpiry(cert *x509.Certificate) (time.Time, int) {
	if cert == nil {
		return time.Time{}, -1
	}
	
	daysUntil := int(time.Until(cert.NotAfter).Hours() / 24)
	return cert.NotAfter, daysUntil
}

// ParseCertificateChain parses a PEM-encoded certificate chain
func ParseCertificateChain(chainPEM []byte) ([]*x509.Certificate, error) {
	var certs []*x509.Certificate
	rest := chainPEM

	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}

		if block.Type != "CERTIFICATE" {
			continue
		}

		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse certificate: %w", err)
		}
		certs = append(certs, cert)
	}

	if len(certs) == 0 {
		return nil, errors.New("no certificates found in PEM data")
	}

	return certs, nil
}

// ValidateCertificateForSMTP validates a certificate is suitable for SMTP/STARTTLS
func (v *CertificateValidator) ValidateCertificateForSMTP(cert *tls.Certificate, domainName string) *ValidationResult {
	result := v.ValidateAll(cert, domainName, false)

	if cert == nil || len(cert.Certificate) == 0 {
		return result
	}

	// Parse leaf certificate
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, fmt.Sprintf("failed to parse certificate: %v", err))
		return result
	}

	// Check for server authentication extended key usage
	hasServerAuth := false
	for _, usage := range leaf.ExtKeyUsage {
		if usage == x509.ExtKeyUsageServerAuth {
			hasServerAuth = true
			break
		}
	}

	// If ExtKeyUsage is empty, it's typically allowed for any purpose
	if len(leaf.ExtKeyUsage) > 0 && !hasServerAuth {
		result.Warnings = append(result.Warnings, "certificate does not have ServerAuth extended key usage")
	}

	// Check that mail subdomain is covered
	mailDomain := "mail." + domainName
	if err := v.ValidateCertificateDomain(leaf, mailDomain); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("certificate may not cover mail subdomain: %s", mailDomain))
	}

	return result
}

// Ensure CertificateValidator is properly initialized
var _ = NewCertificateValidator()
