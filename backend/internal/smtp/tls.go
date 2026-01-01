package smtp

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"net"
	"os"
	"time"
)

// SSLServiceInterface defines the interface for SSL certificate management
// This allows the SMTP server to use dynamic TLS configuration
// Requirements: 4.1, 4.6, 4.7 - STARTTLS support with dynamic certificates
type SSLServiceInterface interface {
	// GetCertificate retrieves a certificate for a domain
	GetCertificate(ctx context.Context, domainName string) (*tls.Certificate, error)
	// GetTLSConfig returns a TLS configuration for use with servers
	GetTLSConfig() *tls.Config
}

// TLSHandler manages TLS configuration for the SMTP server
// Requirements: 4.1, 4.2, 4.6, 4.7, 4.8 - STARTTLS support with dynamic certificates
type TLSHandler struct {
	sslService   SSLServiceInterface
	fallbackCert *tls.Certificate // Fallback certificate when SSLService is unavailable
}

// NewTLSHandler creates a new TLSHandler with the given SSL service
// Requirements: 4.1, 4.6, 4.7 - STARTTLS support with dynamic certificates
func NewTLSHandler(sslService SSLServiceInterface) *TLSHandler {
	return &TLSHandler{
		sslService: sslService,
	}
}

// NewTLSHandlerWithFallback creates a TLSHandler with a fallback certificate
// The fallback is used when the SSL service cannot provide a certificate
func NewTLSHandlerWithFallback(sslService SSLServiceInterface, fallbackCert *tls.Certificate) *TLSHandler {
	return &TLSHandler{
		sslService:   sslService,
		fallbackCert: fallbackCert,
	}
}

// GetTLSConfig returns the TLS configuration for the SMTP server
// Requirements: 4.3, 4.4, 4.5, 4.7 - TLS configuration with SNI support
func (h *TLSHandler) GetTLSConfig() *tls.Config {
	if h.sslService != nil {
		// Delegate to SSLService for dynamic TLS config
		config := h.sslService.GetTLSConfig()
		// Override GetCertificate to use our handler with logging
		config.GetCertificate = h.getCertificateForSNI
		return config
	}

	// Fallback to static configuration if no SSL service
	return h.getStaticTLSConfig()
}

// getStaticTLSConfig returns a static TLS configuration when no SSL service is available
func (h *TLSHandler) getStaticTLSConfig() *tls.Config {
	config := &tls.Config{
		// Requirements: 4.3 - Use TLS 1.2 as minimum version
		MinVersion: tls.VersionTLS12,

		// Requirements: 4.5 - Use strong cipher suites only (no RC4, DES, 3DES)
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		},

		PreferServerCipherSuites: true,
	}

	// Add fallback certificate if available
	if h.fallbackCert != nil {
		config.Certificates = []tls.Certificate{*h.fallbackCert}
	}

	return config
}

// getCertificateForSNI handles SNI-based certificate selection with logging
// Requirements: 4.6, 4.7, 4.8 - Present correct certificate for recipient domain, SNI support, logging
func (h *TLSHandler) getCertificateForSNI(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	serverName := hello.ServerName

	// Log TLS connection attempt
	// Requirements: 4.8 - Log TLS version and cipher for each connection
	log.Printf("TLS SNI request: server_name=%s client_addr=%s", serverName, getClientAddr(hello))

	if serverName == "" {
		// No SNI provided, use fallback if available
		if h.fallbackCert != nil {
			log.Printf("TLS: No SNI provided, using fallback certificate")
			return h.fallbackCert, nil
		}
		return nil, fmt.Errorf("no server name provided and no fallback certificate available")
	}

	// Try to get certificate from SSL service
	if h.sslService != nil {
		ctx := context.Background()
		cert, err := h.sslService.GetCertificate(ctx, serverName)
		if err == nil {
			log.Printf("TLS: Certificate found for %s", serverName)
			return cert, nil
		}

		// Try parent domain for mail subdomains
		// e.g., if mail.example.com fails, try example.com
		if len(serverName) > 5 && serverName[:5] == "mail." {
			parentDomain := serverName[5:]
			cert, err = h.sslService.GetCertificate(ctx, parentDomain)
			if err == nil {
				log.Printf("TLS: Certificate found for parent domain %s (requested: %s)", parentDomain, serverName)
				return cert, nil
			}
		}

		log.Printf("TLS: No certificate found for %s: %v", serverName, err)
	}

	// Use fallback certificate if available
	if h.fallbackCert != nil {
		log.Printf("TLS: Using fallback certificate for %s", serverName)
		return h.fallbackCert, nil
	}

	return nil, fmt.Errorf("no certificate available for %s", serverName)
}

// AdvertiseSTARTTLS returns whether STARTTLS should be advertised
// Requirements: 4.1 - Advertise STARTTLS capability in EHLO response
func (h *TLSHandler) AdvertiseSTARTTLS() bool {
	// Advertise STARTTLS if we have either an SSL service or a fallback certificate
	return h.sslService != nil || h.fallbackCert != nil
}

// HandleSTARTTLS upgrades a connection to TLS
// Requirements: 4.2, 4.8 - Upgrade connection to TLS and log TLS info
func (h *TLSHandler) HandleSTARTTLS(conn net.Conn, serverName string) (net.Conn, error) {
	tlsConfig := h.GetTLSConfig()

	// Wrap connection with TLS
	tlsConn := tls.Server(conn, tlsConfig)

	// Perform handshake with timeout
	if err := tlsConn.Handshake(); err != nil {
		return nil, fmt.Errorf("TLS handshake failed: %w", err)
	}

	// Requirements: 4.8 - Log TLS version and cipher used for each connection
	state := tlsConn.ConnectionState()
	log.Printf("TLS connection established: version=%s cipher=%s server_name=%s",
		tlsVersionString(state.Version),
		tlsCipherSuiteString(state.CipherSuite),
		state.ServerName)

	return tlsConn, nil
}

// getClientAddr safely extracts client address from ClientHelloInfo
func getClientAddr(hello *tls.ClientHelloInfo) string {
	if hello.Conn != nil {
		return hello.Conn.RemoteAddr().String()
	}
	return "unknown"
}

// tlsVersionString returns a human-readable TLS version string
func tlsVersionString(version uint16) string {
	switch version {
	case tls.VersionTLS10:
		return "TLS 1.0"
	case tls.VersionTLS11:
		return "TLS 1.1"
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS13:
		return "TLS 1.3"
	default:
		return fmt.Sprintf("0x%04x", version)
	}
}

// tlsCipherSuiteString returns a human-readable cipher suite name
func tlsCipherSuiteString(cipherSuite uint16) string {
	// TLS 1.3 cipher suites
	switch cipherSuite {
	case tls.TLS_AES_128_GCM_SHA256:
		return "TLS_AES_128_GCM_SHA256"
	case tls.TLS_AES_256_GCM_SHA384:
		return "TLS_AES_256_GCM_SHA384"
	case tls.TLS_CHACHA20_POLY1305_SHA256:
		return "TLS_CHACHA20_POLY1305_SHA256"
	}

	// TLS 1.2 cipher suites
	switch cipherSuite {
	case tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384:
		return "TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384"
	case tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384:
		return "TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384"
	case tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305:
		return "TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305"
	case tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305:
		return "TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305"
	case tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256:
		return "TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256"
	case tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256:
		return "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256"
	default:
		return fmt.Sprintf("0x%04x", cipherSuite)
	}
}

// LoadTLSConfig loads TLS configuration from certificate and key files
// Requires TLS 1.2 or higher as per Requirements 1.3
func LoadTLSConfig(certFile, keyFile string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load TLS certificate: %w", err)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12, // Requirement 1.3: TLS 1.2 or higher
		CipherSuites: []uint16{
			// Secure cipher suites as per security guidelines
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		},
	}, nil
}

// GenerateSelfSignedCert generates a self-signed certificate for development/testing
// Returns paths to the generated certificate and key files
func GenerateSelfSignedCert(hostname string, outputDir string) (certPath, keyPath string, err error) {
	// Generate private key
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate private key: %w", err)
	}

	// Create certificate template
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return "", "", fmt.Errorf("failed to generate serial number: %w", err)
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Persistent Temp Mail"},
			CommonName:   hostname,
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour), // Valid for 1 year
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{hostname, "localhost"},
	}

	// Create certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return "", "", fmt.Errorf("failed to create certificate: %w", err)
	}

	// Ensure output directory exists
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", "", fmt.Errorf("failed to create output directory: %w", err)
	}

	// Write certificate to file
	certPath = fmt.Sprintf("%s/smtp.crt", outputDir)
	certFile, err := os.Create(certPath)
	if err != nil {
		return "", "", fmt.Errorf("failed to create certificate file: %w", err)
	}
	defer certFile.Close()

	if err := pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		return "", "", fmt.Errorf("failed to write certificate: %w", err)
	}

	// Write private key to file
	keyPath = fmt.Sprintf("%s/smtp.key", outputDir)
	keyFile, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return "", "", fmt.Errorf("failed to create key file: %w", err)
	}
	defer keyFile.Close()

	keyDER, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal private key: %w", err)
	}

	if err := pem.Encode(keyFile, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}); err != nil {
		return "", "", fmt.Errorf("failed to write private key: %w", err)
	}

	return certPath, keyPath, nil
}

// ValidateTLSConfig validates that the TLS configuration is properly set up
func ValidateTLSConfig(config *tls.Config) error {
	if config == nil {
		return fmt.Errorf("TLS config is nil")
	}

	if len(config.Certificates) == 0 {
		return fmt.Errorf("no certificates configured")
	}

	if config.MinVersion < tls.VersionTLS12 {
		return fmt.Errorf("minimum TLS version must be 1.2 or higher")
	}

	return nil
}
