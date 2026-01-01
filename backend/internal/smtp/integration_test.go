//go:build integration

// Package smtp provides SMTP server functionality
// Feature: smtp-email-receiver
// Task 11.2: Write integration tests
package smtp

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestIntegration_FullSMTPTransactionFlow tests a complete SMTP transaction
// Requirements: All - Full SMTP transaction flow
func TestIntegration_FullSMTPTransactionFlow(t *testing.T) {
	// Create mock alias repository
	aliasRepo := &mockAliasRepo{
		aliases: map[string]*AliasInfo{
			"test@webrana.id": {ID: "alias-123", IsActive: true},
		},
	}

	// Create SMTP server
	config := &SMTPConfig{
		Port:                0, // Use random port
		Hostname:            "mail.test.local",
		MaxConnections:      10,
		MaxConnectionsPerIP: 5,
		ConnectionTimeout:   30 * time.Second,
		MaxMessageSize:      1024 * 1024, // 1 MB for testing
		MaxRecipients:       10,
		RateLimitPerMinute:  100,
	}

	server := NewSMTPServer(config, nil, aliasRepo)

	// Start server on random port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	server.listener = listener
	server.running.Store(true)
	go server.acceptLoop()
	defer server.Stop()

	// Get the actual port
	addr := listener.Addr().String()

	// Connect to server
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	reader := bufio.NewReader(conn)

	// Read greeting
	greeting, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("Failed to read greeting: %v", err)
	}
	if !strings.HasPrefix(greeting, "220") {
		t.Errorf("Expected 220 greeting, got: %s", greeting)
	}

	// Send EHLO
	fmt.Fprintf(conn, "EHLO test.client\r\n")
	response := readMultilineResponse(reader)
	if !strings.HasPrefix(response, "250") {
		t.Errorf("Expected 250 response to EHLO, got: %s", response)
	}

	// Send MAIL FROM
	fmt.Fprintf(conn, "MAIL FROM:<sender@example.com>\r\n")
	response, _ = reader.ReadString('\n')
	if !strings.HasPrefix(response, "250") {
		t.Errorf("Expected 250 response to MAIL FROM, got: %s", response)
	}

	// Send RCPT TO
	fmt.Fprintf(conn, "RCPT TO:<test@webrana.id>\r\n")
	response, _ = reader.ReadString('\n')
	if !strings.HasPrefix(response, "250") {
		t.Errorf("Expected 250 response to RCPT TO, got: %s", response)
	}

	// Send DATA
	fmt.Fprintf(conn, "DATA\r\n")
	response, _ = reader.ReadString('\n')
	if !strings.HasPrefix(response, "354") {
		t.Errorf("Expected 354 response to DATA, got: %s", response)
	}

	// Send email content
	emailContent := "From: sender@example.com\r\n" +
		"To: test@webrana.id\r\n" +
		"Subject: Test Email\r\n" +
		"\r\n" +
		"This is a test email body.\r\n" +
		".\r\n"
	fmt.Fprint(conn, emailContent)

	response, _ = reader.ReadString('\n')
	if !strings.HasPrefix(response, "250") {
		t.Errorf("Expected 250 response after DATA, got: %s", response)
	}

	// Send QUIT
	fmt.Fprintf(conn, "QUIT\r\n")
	response, _ = reader.ReadString('\n')
	if !strings.HasPrefix(response, "221") {
		t.Errorf("Expected 221 response to QUIT, got: %s", response)
	}
}

// TestIntegration_STARTTLSUpgrade tests STARTTLS upgrade
// Requirements: 1.2, 1.3 - Support STARTTLS with TLS 1.2+
func TestIntegration_STARTTLSUpgrade(t *testing.T) {
	// Generate self-signed certificate for testing
	certPath, keyPath, err := GenerateSelfSignedCert("mail.test.local", t.TempDir())
	if err != nil {
		t.Fatalf("Failed to generate certificate: %v", err)
	}

	tlsConfig, err := LoadTLSConfig(certPath, keyPath)
	if err != nil {
		t.Fatalf("Failed to load TLS config: %v", err)
	}

	// Create mock alias repository
	aliasRepo := &mockAliasRepo{
		aliases: map[string]*AliasInfo{
			"test@webrana.id": {ID: "alias-123", IsActive: true},
		},
	}

	// Create SMTP server with TLS
	config := &SMTPConfig{
		Port:                0,
		Hostname:            "mail.test.local",
		MaxConnections:      10,
		MaxConnectionsPerIP: 5,
		ConnectionTimeout:   30 * time.Second,
		MaxMessageSize:      1024 * 1024,
		MaxRecipients:       10,
		RateLimitPerMinute:  100,
	}

	server := NewSMTPServer(config, tlsConfig, aliasRepo)

	// Start server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	server.listener = listener
	server.running.Store(true)
	go server.acceptLoop()
	defer server.Stop()

	addr := listener.Addr().String()

	// Connect to server
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	reader := bufio.NewReader(conn)

	// Read greeting
	reader.ReadString('\n')

	// Send EHLO
	fmt.Fprintf(conn, "EHLO test.client\r\n")
	response := readMultilineResponse(reader)
	if !strings.Contains(response, "STARTTLS") {
		t.Errorf("Expected STARTTLS capability, got: %s", response)
	}

	// Send STARTTLS
	fmt.Fprintf(conn, "STARTTLS\r\n")
	response, _ = reader.ReadString('\n')
	if !strings.HasPrefix(response, "220") {
		t.Errorf("Expected 220 response to STARTTLS, got: %s", response)
	}

	// Upgrade to TLS
	tlsConn := tls.Client(conn, &tls.Config{
		InsecureSkipVerify: true, // For testing only
		MinVersion:         tls.VersionTLS12,
	})
	if err := tlsConn.Handshake(); err != nil {
		t.Fatalf("TLS handshake failed: %v", err)
	}

	// Verify TLS version
	state := tlsConn.ConnectionState()
	if state.Version < tls.VersionTLS12 {
		t.Errorf("Expected TLS 1.2 or higher, got: %x", state.Version)
	}

	// Continue with EHLO after STARTTLS
	reader = bufio.NewReader(tlsConn)
	fmt.Fprintf(tlsConn, "EHLO test.client\r\n")
	response = readMultilineResponse(reader)
	if !strings.HasPrefix(response, "250") {
		t.Errorf("Expected 250 response to EHLO after STARTTLS, got: %s", response)
	}

	// STARTTLS should not be advertised after upgrade
	if strings.Contains(response, "STARTTLS") {
		t.Error("STARTTLS should not be advertised after TLS upgrade")
	}
}

// TestIntegration_RateLimiting tests rate limiting functionality
// Requirements: 6.3 - Rate limit connections per IP (20 per minute)
func TestIntegration_RateLimiting(t *testing.T) {
	aliasRepo := &mockAliasRepo{
		aliases: map[string]*AliasInfo{},
	}

	config := &SMTPConfig{
		Port:                0,
		Hostname:            "mail.test.local",
		MaxConnections:      100,
		MaxConnectionsPerIP: 100,
		ConnectionTimeout:   30 * time.Second,
		MaxMessageSize:      1024 * 1024,
		MaxRecipients:       10,
		RateLimitPerMinute:  5, // Low limit for testing
	}

	server := NewSMTPServer(config, nil, aliasRepo)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	server.listener = listener
	server.running.Store(true)
	go server.acceptLoop()
	defer server.Stop()

	addr := listener.Addr().String()

	// Make connections up to the rate limit
	var successCount, rejectCount int
	for i := 0; i < 10; i++ {
		conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err != nil {
			continue
		}

		reader := bufio.NewReader(conn)
		response, _ := reader.ReadString('\n')
		conn.Close()

		if strings.HasPrefix(response, "220") {
			successCount++
		} else if strings.HasPrefix(response, "421") {
			rejectCount++
		}
	}

	// Should have some successful and some rejected connections
	if successCount == 0 {
		t.Error("Expected some successful connections")
	}
	if rejectCount == 0 {
		t.Error("Expected some rejected connections due to rate limiting")
	}
}

// TestIntegration_ConnectionLimits tests connection limit enforcement
// Requirements: 1.6, 1.7 - Connection limits
func TestIntegration_ConnectionLimits(t *testing.T) {
	aliasRepo := &mockAliasRepo{
		aliases: map[string]*AliasInfo{},
	}

	config := &SMTPConfig{
		Port:                0,
		Hostname:            "mail.test.local",
		MaxConnections:      3, // Low limit for testing
		MaxConnectionsPerIP: 2, // Low per-IP limit
		ConnectionTimeout:   30 * time.Second,
		MaxMessageSize:      1024 * 1024,
		MaxRecipients:       10,
		RateLimitPerMinute:  100,
	}

	server := NewSMTPServer(config, nil, aliasRepo)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	server.listener = listener
	server.running.Store(true)
	go server.acceptLoop()
	defer server.Stop()

	addr := listener.Addr().String()

	// Open connections up to per-IP limit
	var conns []net.Conn
	var mu sync.Mutex

	for i := 0; i < 5; i++ {
		conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err != nil {
			continue
		}

		reader := bufio.NewReader(conn)
		response, _ := reader.ReadString('\n')

		if strings.HasPrefix(response, "220") {
			mu.Lock()
			conns = append(conns, conn)
			mu.Unlock()
		} else {
			conn.Close()
		}
	}

	// Should have at most MaxConnectionsPerIP connections
	if len(conns) > config.MaxConnectionsPerIP {
		t.Errorf("Expected at most %d connections, got %d", config.MaxConnectionsPerIP, len(conns))
	}

	// Clean up
	for _, conn := range conns {
		conn.Close()
	}
}

// TestIntegration_ErrorScenarios tests various error scenarios
// Requirements: 7.1-7.6 - Error handling
func TestIntegration_ErrorScenarios(t *testing.T) {
	aliasRepo := &mockAliasRepo{
		aliases: map[string]*AliasInfo{
			"active@webrana.id":   {ID: "alias-1", IsActive: true},
			"inactive@webrana.id": {ID: "alias-2", IsActive: false},
		},
	}

	config := &SMTPConfig{
		Port:                0,
		Hostname:            "mail.test.local",
		MaxConnections:      10,
		MaxConnectionsPerIP: 5,
		ConnectionTimeout:   30 * time.Second,
		MaxMessageSize:      100, // Very small for testing
		MaxRecipients:       2,
		RateLimitPerMinute:  100,
	}

	server := NewSMTPServer(config, nil, aliasRepo)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	server.listener = listener
	server.running.Store(true)
	go server.acceptLoop()
	defer server.Stop()

	addr := listener.Addr().String()

	tests := []struct {
		name           string
		commands       []string
		expectedCode   string
		expectedInResp string
	}{
		{
			name: "recipient not found",
			commands: []string{
				"EHLO test.client",
				"MAIL FROM:<sender@example.com>",
				"RCPT TO:<nonexistent@webrana.id>",
			},
			expectedCode:   "550",
			expectedInResp: "not found",
		},
		{
			name: "inactive recipient",
			commands: []string{
				"EHLO test.client",
				"MAIL FROM:<sender@example.com>",
				"RCPT TO:<inactive@webrana.id>",
			},
			expectedCode:   "550",
			expectedInResp: "disabled",
		},
		{
			name: "DATA without RCPT TO",
			commands: []string{
				"EHLO test.client",
				"MAIL FROM:<sender@example.com>",
				"DATA",
			},
			expectedCode: "500",
		},
		{
			name: "RCPT TO without MAIL FROM",
			commands: []string{
				"EHLO test.client",
				"RCPT TO:<active@webrana.id>",
			},
			expectedCode: "500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn, err := net.Dial("tcp", addr)
			if err != nil {
				t.Fatalf("Failed to connect: %v", err)
			}
			defer conn.Close()

			reader := bufio.NewReader(conn)

			// Read greeting
			reader.ReadString('\n')

			var lastResponse string
			for _, cmd := range tt.commands {
				fmt.Fprintf(conn, "%s\r\n", cmd)
				if strings.HasPrefix(cmd, "EHLO") {
					lastResponse = readMultilineResponse(reader)
				} else {
					lastResponse, _ = reader.ReadString('\n')
				}
			}

			if !strings.HasPrefix(lastResponse, tt.expectedCode) {
				t.Errorf("Expected %s response, got: %s", tt.expectedCode, lastResponse)
			}

			if tt.expectedInResp != "" && !strings.Contains(strings.ToLower(lastResponse), strings.ToLower(tt.expectedInResp)) {
				t.Errorf("Expected response to contain '%s', got: %s", tt.expectedInResp, lastResponse)
			}
		})
	}
}

// TestIntegration_RecipientLimit tests recipient limit enforcement
// Requirements: 2.6 - Limit recipients per message to 100
func TestIntegration_RecipientLimit(t *testing.T) {
	// Create aliases for testing
	aliases := make(map[string]*AliasInfo)
	for i := 0; i < 10; i++ {
		addr := fmt.Sprintf("test%d@webrana.id", i)
		aliases[addr] = &AliasInfo{ID: fmt.Sprintf("alias-%d", i), IsActive: true}
	}

	aliasRepo := &mockAliasRepo{aliases: aliases}

	config := &SMTPConfig{
		Port:                0,
		Hostname:            "mail.test.local",
		MaxConnections:      10,
		MaxConnectionsPerIP: 5,
		ConnectionTimeout:   30 * time.Second,
		MaxMessageSize:      1024 * 1024,
		MaxRecipients:       3, // Low limit for testing
		RateLimitPerMinute:  100,
	}

	server := NewSMTPServer(config, nil, aliasRepo)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	server.listener = listener
	server.running.Store(true)
	go server.acceptLoop()
	defer server.Stop()

	addr := listener.Addr().String()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	reader := bufio.NewReader(conn)

	// Read greeting
	reader.ReadString('\n')

	// EHLO
	fmt.Fprintf(conn, "EHLO test.client\r\n")
	readMultilineResponse(reader)

	// MAIL FROM
	fmt.Fprintf(conn, "MAIL FROM:<sender@example.com>\r\n")
	reader.ReadString('\n')

	// Add recipients up to and beyond limit
	var successCount, rejectCount int
	for i := 0; i < 5; i++ {
		fmt.Fprintf(conn, "RCPT TO:<test%d@webrana.id>\r\n", i)
		response, _ := reader.ReadString('\n')
		if strings.HasPrefix(response, "250") {
			successCount++
		} else if strings.HasPrefix(response, "500") || strings.HasPrefix(response, "452") {
			rejectCount++
		}
	}

	if successCount != config.MaxRecipients {
		t.Errorf("Expected %d successful recipients, got %d", config.MaxRecipients, successCount)
	}
	if rejectCount == 0 {
		t.Error("Expected some recipients to be rejected due to limit")
	}
}

// readMultilineResponse reads a multi-line SMTP response
func readMultilineResponse(reader *bufio.Reader) string {
	var response strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		response.WriteString(line)
		// Check if this is the last line (no hyphen after code)
		if len(line) >= 4 && line[3] == ' ' {
			break
		}
	}
	return response.String()
}

// mockAliasRepo is a mock implementation of AliasRepository for testing
type mockAliasRepo struct {
	aliases map[string]*AliasInfo
	mu      sync.RWMutex
}

func (m *mockAliasRepo) GetByFullAddress(ctx context.Context, fullAddress string) (*AliasInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	alias, ok := m.aliases[strings.ToLower(fullAddress)]
	if !ok {
		return nil, fmt.Errorf("alias not found")
	}
	return alias, nil
}
