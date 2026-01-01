package smtp

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// SMTPServer implements the SMTP server interface
// Requirements: 1.1, 1.6, 1.7, 1.8, 4.1, 4.6, 4.7
type SMTPServer struct {
	config          *SMTPConfig
	listener        net.Listener
	tlsConfig       *tls.Config
	tlsHandler      *TLSHandler      // Dynamic TLS handler with SSL service
	sslService      SSLServiceInterface // SSL service for dynamic certificates
	aliasRepo       AliasRepository
	
	// Email processor callback
	dataCallback    func(ctx context.Context, data *DataResult) error
	
	// Connection management
	activeConns     int64
	ipConnections   map[string]int
	ipConnMu        sync.RWMutex
	
	// Rate limiting (connections per minute per IP)
	ipRateLimit     map[string]*rateLimitEntry
	ipRateMu        sync.RWMutex
	
	// Server state
	running         atomic.Bool
	wg              sync.WaitGroup
	shutdownCh      chan struct{}
}

// rateLimitEntry tracks rate limiting per IP
type rateLimitEntry struct {
	count     int
	resetTime time.Time
}

// AliasRepository interface for recipient validation
type AliasRepository interface {
	GetByFullAddress(ctx context.Context, fullAddress string) (*AliasInfo, error)
}

// AliasInfo contains alias information for SMTP validation
type AliasInfo struct {
	ID       string
	IsActive bool
}

// NewSMTPServer creates a new SMTP server instance
func NewSMTPServer(config *SMTPConfig, tlsConfig *tls.Config, aliasRepo AliasRepository) *SMTPServer {
	return &SMTPServer{
		config:        config,
		tlsConfig:     tlsConfig,
		aliasRepo:     aliasRepo,
		dataCallback:  nil,
		ipConnections: make(map[string]int),
		ipRateLimit:   make(map[string]*rateLimitEntry),
		shutdownCh:    make(chan struct{}),
	}
}

// NewSMTPServerWithSSLService creates a new SMTP server with dynamic SSL certificate support
// Requirements: 4.1, 4.6, 4.7 - STARTTLS with dynamic certificates via SSLService
func NewSMTPServerWithSSLService(config *SMTPConfig, sslService SSLServiceInterface, aliasRepo AliasRepository) *SMTPServer {
	server := &SMTPServer{
		config:        config,
		sslService:    sslService,
		aliasRepo:     aliasRepo,
		dataCallback:  nil,
		ipConnections: make(map[string]int),
		ipRateLimit:   make(map[string]*rateLimitEntry),
		shutdownCh:    make(chan struct{}),
	}

	// Create TLS handler with SSL service
	if sslService != nil {
		server.tlsHandler = NewTLSHandler(sslService)
		// Also set tlsConfig for backward compatibility
		server.tlsConfig = server.tlsHandler.GetTLSConfig()
	}

	return server
}

// NewSMTPServerWithSSLServiceAndFallback creates a new SMTP server with SSL service and fallback certificate
// The fallback certificate is used when the SSL service cannot provide a certificate for a domain
// Requirements: 4.1, 4.6, 4.7 - STARTTLS with dynamic certificates and fallback
func NewSMTPServerWithSSLServiceAndFallback(config *SMTPConfig, sslService SSLServiceInterface, fallbackCert *tls.Certificate, aliasRepo AliasRepository) *SMTPServer {
	server := &SMTPServer{
		config:        config,
		sslService:    sslService,
		aliasRepo:     aliasRepo,
		dataCallback:  nil,
		ipConnections: make(map[string]int),
		ipRateLimit:   make(map[string]*rateLimitEntry),
		shutdownCh:    make(chan struct{}),
	}

	// Create TLS handler with SSL service and fallback
	server.tlsHandler = NewTLSHandlerWithFallback(sslService, fallbackCert)
	// Also set tlsConfig for backward compatibility
	server.tlsConfig = server.tlsHandler.GetTLSConfig()

	return server
}

// SetSSLService sets the SSL service for dynamic certificate management
// This can be called after server creation to enable dynamic TLS
// Requirements: 4.1, 4.6, 4.7 - STARTTLS with dynamic certificates
func (s *SMTPServer) SetSSLService(sslService SSLServiceInterface) {
	s.sslService = sslService
	if sslService != nil {
		s.tlsHandler = NewTLSHandler(sslService)
		s.tlsConfig = s.tlsHandler.GetTLSConfig()
	}
}

// SetSSLServiceWithFallback sets the SSL service with a fallback certificate
// Requirements: 4.1, 4.6, 4.7 - STARTTLS with dynamic certificates and fallback
func (s *SMTPServer) SetSSLServiceWithFallback(sslService SSLServiceInterface, fallbackCert *tls.Certificate) {
	s.sslService = sslService
	s.tlsHandler = NewTLSHandlerWithFallback(sslService, fallbackCert)
	s.tlsConfig = s.tlsHandler.GetTLSConfig()
}

// GetTLSConfig returns the current TLS configuration
// If a TLS handler is configured, it returns the dynamic config
// Otherwise, it returns the static config
func (s *SMTPServer) GetTLSConfig() *tls.Config {
	if s.tlsHandler != nil {
		return s.tlsHandler.GetTLSConfig()
	}
	return s.tlsConfig
}

// SetDataCallback sets the callback function for processing email data
// This callback is called when DATA command completes successfully
// Requirements: All - Wire SMTP → parser → attachment handler → repositories
func (s *SMTPServer) SetDataCallback(callback func(ctx context.Context, data *DataResult) error) {
	s.dataCallback = callback
}

// Start starts the SMTP server
// Requirements: 1.1 (Listen on port 25)
func (s *SMTPServer) Start() error {
	addr := fmt.Sprintf(":%d", s.config.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to start SMTP server on %s: %w", addr, err)
	}
	
	s.listener = listener
	s.running.Store(true)
	
	log.Printf("SMTP server started on port %d", s.config.Port)
	
	go s.acceptLoop()
	
	return nil
}

// Stop gracefully stops the SMTP server
func (s *SMTPServer) Stop() error {
	if !s.running.Load() {
		return nil
	}
	
	s.running.Store(false)
	close(s.shutdownCh)
	
	if s.listener != nil {
		s.listener.Close()
	}
	
	// Wait for all connections to finish with timeout
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	
	select {
	case <-done:
		log.Println("SMTP server stopped gracefully")
	case <-time.After(30 * time.Second):
		log.Println("SMTP server shutdown timed out")
	}
	
	return nil
}

// acceptLoop accepts incoming connections
func (s *SMTPServer) acceptLoop() {
	for s.running.Load() {
		conn, err := s.listener.Accept()
		if err != nil {
			if s.running.Load() {
				log.Printf("Error accepting connection: %v", err)
			}
			continue
		}
		
		go s.handleConnection(conn)
	}
}

// handleConnection handles a single SMTP connection
// Requirements: 1.6, 1.7, 1.8, 6.3, 6.5, 4.1, 4.6, 4.7
func (s *SMTPServer) handleConnection(conn net.Conn) {
	s.wg.Add(1)
	defer s.wg.Done()
	
	remoteAddr := conn.RemoteAddr().String()
	remoteIP, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		remoteIP = remoteAddr
	}
	
	// Log connection attempt (Requirement 6.5)
	log.Printf("SMTP connection attempt from %s", remoteIP)
	
	// Check rate limit (Requirement 6.3: 20 connections per minute per IP)
	if !s.checkRateLimit(remoteIP) {
		s.sendResponse(conn, CodeServiceUnavailable, "Too many connections from your IP")
		conn.Close()
		return
	}
	
	// Check global connection limit (Requirement 1.6: max 100 connections)
	if !s.acquireConnection() {
		s.sendResponse(conn, CodeServiceUnavailable, "Too many connections")
		conn.Close()
		return
	}
	defer s.releaseConnection()
	
	// Check per-IP connection limit (Requirement 1.7: max 5 per IP)
	if !s.acquireIPConnection(remoteIP) {
		s.sendResponse(conn, CodeServiceUnavailable, "Too many connections from your IP")
		conn.Close()
		return
	}
	defer s.releaseIPConnection(remoteIP)
	
	// Set connection timeout (Requirement 1.8: 5 minutes)
	conn.SetDeadline(time.Now().Add(s.config.ConnectionTimeout))
	
	// Get TLS config - use TLS handler if available for dynamic certificates
	// Requirements: 4.1, 4.6, 4.7 - STARTTLS with dynamic certificates
	tlsConfig := s.GetTLSConfig()
	
	// Create and run session
	session := NewSMTPSessionWithCallback(conn, s.config, tlsConfig, s.aliasRepo, remoteIP, s.dataCallback)
	session.Run()
}

// acquireConnection attempts to acquire a global connection slot
// Returns false if max connections reached (Requirement 1.6)
func (s *SMTPServer) acquireConnection() bool {
	for {
		current := atomic.LoadInt64(&s.activeConns)
		if current >= int64(s.config.MaxConnections) {
			return false
		}
		if atomic.CompareAndSwapInt64(&s.activeConns, current, current+1) {
			return true
		}
	}
}

// releaseConnection releases a global connection slot
func (s *SMTPServer) releaseConnection() {
	atomic.AddInt64(&s.activeConns, -1)
}

// acquireIPConnection attempts to acquire a per-IP connection slot
// Returns false if max per-IP connections reached (Requirement 1.7)
func (s *SMTPServer) acquireIPConnection(ip string) bool {
	s.ipConnMu.Lock()
	defer s.ipConnMu.Unlock()
	
	count := s.ipConnections[ip]
	if count >= s.config.MaxConnectionsPerIP {
		return false
	}
	
	s.ipConnections[ip] = count + 1
	return true
}

// releaseIPConnection releases a per-IP connection slot
func (s *SMTPServer) releaseIPConnection(ip string) {
	s.ipConnMu.Lock()
	defer s.ipConnMu.Unlock()
	
	count := s.ipConnections[ip]
	if count <= 1 {
		delete(s.ipConnections, ip)
	} else {
		s.ipConnections[ip] = count - 1
	}
}

// checkRateLimit checks if the IP has exceeded rate limit
// Returns false if rate limit exceeded (Requirement 6.3: 20 per minute)
func (s *SMTPServer) checkRateLimit(ip string) bool {
	s.ipRateMu.Lock()
	defer s.ipRateMu.Unlock()
	
	now := time.Now()
	entry, exists := s.ipRateLimit[ip]
	
	if !exists || now.After(entry.resetTime) {
		// Create new entry or reset expired entry
		s.ipRateLimit[ip] = &rateLimitEntry{
			count:     1,
			resetTime: now.Add(time.Minute),
		}
		return true
	}
	
	if entry.count >= s.config.RateLimitPerMinute {
		return false
	}
	
	entry.count++
	return true
}

// sendResponse sends an SMTP response to the connection
func (s *SMTPServer) sendResponse(conn net.Conn, code int, message string) {
	response := fmt.Sprintf("%d %s\r\n", code, message)
	conn.Write([]byte(response))
}

// GetActiveConnections returns the current number of active connections
func (s *SMTPServer) GetActiveConnections() int64 {
	return atomic.LoadInt64(&s.activeConns)
}

// GetIPConnections returns the number of connections for a specific IP
func (s *SMTPServer) GetIPConnections(ip string) int {
	s.ipConnMu.RLock()
	defer s.ipConnMu.RUnlock()
	return s.ipConnections[ip]
}

// IsRunning returns whether the server is running
func (s *SMTPServer) IsRunning() bool {
	return s.running.Load()
}

// DefaultSMTPConfig returns default SMTP configuration
func DefaultSMTPConfig() *SMTPConfig {
	return &SMTPConfig{
		Port:                25,
		Hostname:            "mail.webrana.id",
		MaxConnections:      100,  // Requirement 1.6
		MaxConnectionsPerIP: 5,    // Requirement 1.7
		ConnectionTimeout:   5 * time.Minute, // Requirement 1.8
		MaxMessageSize:      25 * 1024 * 1024, // 25 MB (Requirement 1.9)
		MaxRecipients:       100,  // Requirement 2.6
		RateLimitPerMinute:  20,   // Requirement 6.3
	}
}


// HealthStatus represents the SMTP server health status
// Requirements: 10.5 - SMTP health check
type HealthStatus struct {
	Status          string `json:"status"`
	Running         bool   `json:"running"`
	ActiveConns     int64  `json:"active_connections"`
	MaxConns        int    `json:"max_connections"`
	TLSEnabled      bool   `json:"tls_enabled"`
	Hostname        string `json:"hostname"`
	Port            int    `json:"port"`
}

// HealthCheck returns the current health status of the SMTP server
// Requirements: 10.5 - SMTP_Server SHALL respond to EHLO command for health check
func (s *SMTPServer) HealthCheck() HealthStatus {
	return HealthStatus{
		Status:      s.getHealthStatus(),
		Running:     s.running.Load(),
		ActiveConns: atomic.LoadInt64(&s.activeConns),
		MaxConns:    s.config.MaxConnections,
		TLSEnabled:  s.tlsConfig != nil || s.tlsHandler != nil,
		Hostname:    s.config.Hostname,
		Port:        s.config.Port,
	}
}

// getHealthStatus returns "healthy" if server is running, "unhealthy" otherwise
func (s *SMTPServer) getHealthStatus() string {
	if s.running.Load() {
		return "healthy"
	}
	return "unhealthy"
}

// PerformEHLOCheck performs an EHLO-based health check by connecting to the server
// Requirements: 10.5 - SMTP_Server SHALL respond to EHLO command for health check
func (s *SMTPServer) PerformEHLOCheck(ctx context.Context) error {
	if !s.running.Load() {
		return fmt.Errorf("SMTP server is not running")
	}

	// Connect to the SMTP server
	addr := fmt.Sprintf("localhost:%d", s.config.Port)
	
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to connect to SMTP server: %w", err)
	}
	defer conn.Close()

	// Set deadline from context
	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	}

	// Read greeting
	buf := make([]byte, 512)
	n, err := conn.Read(buf)
	if err != nil {
		return fmt.Errorf("failed to read SMTP greeting: %w", err)
	}
	
	greeting := string(buf[:n])
	if len(greeting) < 3 || greeting[:3] != "220" {
		return fmt.Errorf("unexpected SMTP greeting: %s", greeting)
	}

	// Send EHLO command
	ehloCmd := fmt.Sprintf("EHLO healthcheck\r\n")
	_, err = conn.Write([]byte(ehloCmd))
	if err != nil {
		return fmt.Errorf("failed to send EHLO command: %w", err)
	}

	// Read EHLO response
	n, err = conn.Read(buf)
	if err != nil {
		return fmt.Errorf("failed to read EHLO response: %w", err)
	}

	response := string(buf[:n])
	if len(response) < 3 || response[:3] != "250" {
		return fmt.Errorf("unexpected EHLO response: %s", response)
	}

	// Send QUIT command
	_, err = conn.Write([]byte("QUIT\r\n"))
	if err != nil {
		// Non-fatal, we already verified the server is healthy
		log.Printf("Warning: failed to send QUIT command: %v", err)
	}

	return nil
}
