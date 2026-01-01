package smtp

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// TestableAliasRepository is a mock repository for testing recipient validation
type TestableAliasRepository struct {
	aliases map[string]*AliasInfo
}

func NewTestableAliasRepository() *TestableAliasRepository {
	return &TestableAliasRepository{
		aliases: make(map[string]*AliasInfo),
	}
}

func (r *TestableAliasRepository) GetByFullAddress(ctx context.Context, fullAddress string) (*AliasInfo, error) {
	// Case-insensitive lookup (Requirement 2.5)
	lowerAddress := strings.ToLower(fullAddress)
	if alias, ok := r.aliases[lowerAddress]; ok {
		return alias, nil
	}
	return nil, errors.New("alias not found")
}

func (r *TestableAliasRepository) AddAlias(address string, isActive bool) {
	r.aliases[strings.ToLower(address)] = &AliasInfo{
		ID:       address,
		IsActive: isActive,
	}
}

func (r *TestableAliasRepository) Clear() {
	r.aliases = make(map[string]*AliasInfo)
}

// mockConn implements net.Conn for testing
type mockConn struct {
	readBuf  *bytes.Buffer
	writeBuf *bytes.Buffer
}

func newMockConn() *mockConn {
	return &mockConn{
		readBuf:  bytes.NewBuffer(nil),
		writeBuf: bytes.NewBuffer(nil),
	}
}

func (c *mockConn) Read(b []byte) (n int, err error)   { return c.readBuf.Read(b) }
func (c *mockConn) Write(b []byte) (n int, err error)  { return c.writeBuf.Write(b) }
func (c *mockConn) Close() error                       { return nil }
func (c *mockConn) LocalAddr() net.Addr                { return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 25} }
func (c *mockConn) RemoteAddr() net.Addr               { return &net.TCPAddr{IP: net.ParseIP("192.168.1.1"), Port: 12345} }
func (c *mockConn) SetDeadline(t time.Time) error      { return nil }
func (c *mockConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *mockConn) SetWriteDeadline(t time.Time) error { return nil }

// createTestSession creates a test SMTP session with mock connection
func createTestSession(repo *TestableAliasRepository) (*SMTPSession, *mockConn) {
	conn := newMockConn()
	config := &SMTPConfig{
		Port:                25,
		Hostname:            "test.local",
		MaxConnections:      100,
		MaxConnectionsPerIP: 5,
		ConnectionTimeout:   5 * time.Minute,
		MaxMessageSize:      25 * 1024 * 1024,
		MaxRecipients:       100,
		RateLimitPerMinute:  20,
	}

	session := &SMTPSession{
		conn:      conn,
		reader:    bufio.NewReader(conn.readBuf),
		writer:    bufio.NewWriter(conn.writeBuf),
		config:    config,
		tlsConfig: nil,
		aliasRepo: repo,
		state: &SessionState{
			TLSEnabled: false,
			Recipients: make([]string, 0),
			RemoteIP:   "192.168.1.1",
			StartTime:  time.Now(),
			Conn:       conn,
		},
		ehloReceived: true,
	}

	return session, conn
}

// getLastResponse extracts the last SMTP response from the write buffer
func getLastResponse(conn *mockConn) (int, string) {
	response := conn.writeBuf.String()
	conn.writeBuf.Reset()
	
	lines := strings.Split(strings.TrimSpace(response), "\r\n")
	if len(lines) == 0 {
		return 0, ""
	}
	
	lastLine := lines[len(lines)-1]
	if len(lastLine) < 4 {
		return 0, lastLine
	}
	
	var code int
	_, _ = strings.NewReader(lastLine[:3]).Read(make([]byte, 3))
	code = int(lastLine[0]-'0')*100 + int(lastLine[1]-'0')*10 + int(lastLine[2]-'0')
	message := strings.TrimSpace(lastLine[4:])
	
	return code, message
}


// TestProperty3_RecipientValidation tests Property 3: Recipient Validation
// Feature: smtp-email-receiver, Property 3: Recipient Validation
// *For any* RCPT TO command, the SMTP_Server SHALL respond with 250 if alias exists and is active,
// 550 "User not found" if alias does not exist, or 550 "User disabled" if alias is inactive.
// Lookup SHALL be case-insensitive.
// **Validates: Requirements 2.1, 2.2, 2.3, 2.4, 2.5**
func TestProperty3_RecipientValidation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		repo := NewTestableAliasRepository()
		session, conn := createTestSession(repo)
		
		// Set MAIL FROM first (required before RCPT TO)
		session.state.MailFrom = "sender@example.com"
		
		// Generate random email address components
		localPartChars := "abcdefghijklmnopqrstuvwxyz0123456789"
		localPartLen := rapid.IntRange(3, 20).Draw(t, "localPartLen")
		localPart := rapid.StringOfN(rapid.RuneFrom([]rune(localPartChars)), localPartLen, localPartLen, -1).Draw(t, "localPart")
		
		domainLen := rapid.IntRange(3, 15).Draw(t, "domainLen")
		domain := rapid.StringOfN(rapid.RuneFrom([]rune(localPartChars)), domainLen, domainLen, -1).Draw(t, "domain")
		
		email := localPart + "@" + domain + ".com"
		
		// Test scenario: alias exists and is active
		aliasExists := rapid.Bool().Draw(t, "aliasExists")
		aliasActive := rapid.Bool().Draw(t, "aliasActive")
		
		if aliasExists {
			repo.AddAlias(email, aliasActive)
		}
		
		// Test with different case variations (Requirement 2.5: case-insensitive)
		caseVariation := rapid.IntRange(0, 2).Draw(t, "caseVariation")
		var testEmail string
		switch caseVariation {
		case 0:
			testEmail = strings.ToLower(email)
		case 1:
			testEmail = strings.ToUpper(email)
		case 2:
			// Mixed case
			testEmail = email
			if len(testEmail) > 0 {
				runes := []rune(testEmail)
				for i := range runes {
					if i%2 == 0 {
						runes[i] = []rune(strings.ToUpper(string(runes[i])))[0]
					}
				}
				testEmail = string(runes)
			}
		}
		
		// Execute RCPT TO command
		session.handleRCPTTO("TO:<" + testEmail + ">")
		
		code, message := getLastResponse(conn)
		
		// Verify response based on alias state
		if !aliasExists {
			// Requirement 2.3: 550 User not found
			if code != CodeUserNotFound || message != "User not found" {
				t.Errorf("Non-existent alias should return 550 'User not found', got %d '%s'", code, message)
			}
		} else if !aliasActive {
			// Requirement 2.4: 550 User disabled
			if code != CodeUserNotFound || message != "User disabled" {
				t.Errorf("Inactive alias should return 550 'User disabled', got %d '%s'", code, message)
			}
		} else {
			// Requirement 2.2: 250 OK
			if code != CodeOK {
				t.Errorf("Active alias should return 250 OK, got %d '%s'", code, message)
			}
			// Verify recipient was added
			if len(session.state.Recipients) != 1 {
				t.Errorf("Expected 1 recipient, got %d", len(session.state.Recipients))
			}
		}
	})
}

// TestProperty4_RecipientLimit tests Property 4: Recipient Limit
// Feature: smtp-email-receiver, Property 4: Recipient Limit
// *For any* SMTP transaction with more than 100 recipients, the SMTP_Server SHALL reject
// additional RCPT TO commands.
// **Validates: Requirements 2.6**
func TestProperty4_RecipientLimit(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		repo := NewTestableAliasRepository()
		
		// Generate random max recipients limit (smaller for faster tests)
		maxRecipients := rapid.IntRange(5, 20).Draw(t, "maxRecipients")
		
		conn := newMockConn()
		config := &SMTPConfig{
			Port:                25,
			Hostname:            "test.local",
			MaxConnections:      100,
			MaxConnectionsPerIP: 5,
			ConnectionTimeout:   5 * time.Minute,
			MaxMessageSize:      25 * 1024 * 1024,
			MaxRecipients:       maxRecipients,
			RateLimitPerMinute:  20,
		}

		session := &SMTPSession{
			conn:      conn,
			reader:    bufio.NewReader(conn.readBuf),
			writer:    bufio.NewWriter(conn.writeBuf),
			config:    config,
			tlsConfig: nil,
			aliasRepo: repo,
			state: &SessionState{
				TLSEnabled: false,
				Recipients: make([]string, 0),
				RemoteIP:   "192.168.1.1",
				StartTime:  time.Now(),
				Conn:       conn,
			},
			ehloReceived: true,
		}
		
		// Set MAIL FROM first
		session.state.MailFrom = "sender@example.com"
		
		// Add aliases up to and beyond the limit
		for i := 0; i <= maxRecipients+5; i++ {
			email := strings.ToLower(rapid.StringMatching(`[a-z]{5}`).Draw(t, "localPart")) + 
				string(rune('a'+i%26)) + "@test.com"
			repo.AddAlias(email, true)
		}
		
		// Add recipients up to the limit
		for i := 0; i < maxRecipients; i++ {
			email := strings.ToLower(rapid.StringMatching(`[a-z]{5}`).Draw(t, "localPart")) + 
				string(rune('a'+i%26)) + "@test.com"
			repo.AddAlias(email, true)
			
			conn.writeBuf.Reset()
			session.handleRCPTTO("TO:<" + email + ">")
			
			code, _ := getLastResponse(conn)
			if code != CodeOK {
				t.Errorf("Recipient %d should be accepted (limit: %d), got code %d", i+1, maxRecipients, code)
			}
		}
		
		// Verify we're at the limit
		if len(session.state.Recipients) != maxRecipients {
			t.Errorf("Expected %d recipients, got %d", maxRecipients, len(session.state.Recipients))
		}
		
		// Try to add one more - should be rejected
		extraEmail := "extra@test.com"
		repo.AddAlias(extraEmail, true)
		
		conn.writeBuf.Reset()
		session.handleRCPTTO("TO:<" + extraEmail + ">")
		
		code, message := getLastResponse(conn)
		if code != CodeSyntaxError || message != "Too many recipients" {
			t.Errorf("Recipient beyond limit should be rejected with 500 'Too many recipients', got %d '%s'", code, message)
		}
		
		// Verify recipient count didn't increase
		if len(session.state.Recipients) != maxRecipients {
			t.Errorf("Recipient count should remain at %d, got %d", maxRecipients, len(session.state.Recipients))
		}
	})
}

// TestProperty14_NoRelayPolicy tests Property 14: No Relay Policy
// Feature: smtp-email-receiver, Property 14: No Relay Policy
// *For any* RCPT TO address not belonging to a registered alias, the SMTP_Server SHALL reject
// with 550 response (no relay).
// **Validates: Requirements 6.4**
func TestProperty14_NoRelayPolicy(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		repo := NewTestableAliasRepository()
		session, conn := createTestSession(repo)
		
		// Set MAIL FROM first
		session.state.MailFrom = "sender@example.com"
		
		// Add some local aliases
		localDomains := []string{"webrana.id", "mail.webrana.id", "test.webrana.id"}
		for _, domain := range localDomains {
			localPart := rapid.StringMatching(`[a-z]{5,10}`).Draw(t, "localPart")
			repo.AddAlias(localPart+"@"+domain, true)
		}
		
		// Generate a random external email that is NOT in our aliases
		externalDomains := []string{"gmail.com", "yahoo.com", "hotmail.com", "external.org", "other.net"}
		externalDomain := externalDomains[rapid.IntRange(0, len(externalDomains)-1).Draw(t, "domainIdx")]
		externalLocal := rapid.StringMatching(`[a-z]{5,10}`).Draw(t, "externalLocal")
		externalEmail := externalLocal + "@" + externalDomain
		
		// Try to relay to external address
		session.handleRCPTTO("TO:<" + externalEmail + ">")
		
		code, message := getLastResponse(conn)
		
		// Should be rejected with 550 (no relay)
		if code != CodeUserNotFound || message != "User not found" {
			t.Errorf("External address should be rejected with 550 'User not found' (no relay), got %d '%s'", code, message)
		}
		
		// Verify no recipient was added
		if len(session.state.Recipients) != 0 {
			t.Errorf("No recipient should be added for relay attempt, got %d", len(session.state.Recipients))
		}
	})
}


// TestRCPTTO_RequiresMAILFROM tests that RCPT TO requires MAIL FROM first
func TestRCPTTO_RequiresMAILFROM(t *testing.T) {
	repo := NewTestableAliasRepository()
	session, conn := createTestSession(repo)
	
	// Don't set MAIL FROM
	session.state.MailFrom = ""
	
	repo.AddAlias("test@webrana.id", true)
	
	session.handleRCPTTO("TO:<test@webrana.id>")
	
	code, message := getLastResponse(conn)
	
	if code != CodeSyntaxError || message != "Send MAIL FROM first" {
		t.Errorf("RCPT TO without MAIL FROM should return 500 'Send MAIL FROM first', got %d '%s'", code, message)
	}
}

// TestRCPTTO_InvalidSyntax tests invalid RCPT TO syntax
func TestRCPTTO_InvalidSyntax(t *testing.T) {
	repo := NewTestableAliasRepository()
	session, conn := createTestSession(repo)
	session.state.MailFrom = "sender@example.com"
	
	tests := []struct {
		name string
		args string
	}{
		{"missing TO:", "test@webrana.id"},
		{"wrong prefix", "FROM:<test@webrana.id>"},
		{"empty", ""},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn.writeBuf.Reset()
			session.handleRCPTTO(tt.args)
			
			code, _ := getLastResponse(conn)
			if code != CodeSyntaxErrorParams {
				t.Errorf("Invalid syntax should return 501, got %d", code)
			}
		})
	}
}

// TestRCPTTO_DuplicateRecipient tests that duplicate recipients are handled gracefully
func TestRCPTTO_DuplicateRecipient(t *testing.T) {
	repo := NewTestableAliasRepository()
	session, conn := createTestSession(repo)
	session.state.MailFrom = "sender@example.com"
	
	email := "test@webrana.id"
	repo.AddAlias(email, true)
	
	// Add recipient first time
	session.handleRCPTTO("TO:<" + email + ">")
	code1, _ := getLastResponse(conn)
	
	if code1 != CodeOK {
		t.Errorf("First RCPT TO should succeed, got %d", code1)
	}
	
	// Add same recipient again
	conn.writeBuf.Reset()
	session.handleRCPTTO("TO:<" + email + ">")
	code2, _ := getLastResponse(conn)
	
	// Should still return OK (idempotent)
	if code2 != CodeOK {
		t.Errorf("Duplicate RCPT TO should return OK (idempotent), got %d", code2)
	}
	
	// But should only have one recipient
	if len(session.state.Recipients) != 1 {
		t.Errorf("Should have only 1 recipient after duplicate, got %d", len(session.state.Recipients))
	}
}

// TestRCPTTO_CaseInsensitiveLookup tests case-insensitive recipient lookup
func TestRCPTTO_CaseInsensitiveLookup(t *testing.T) {
	repo := NewTestableAliasRepository()
	session, conn := createTestSession(repo)
	session.state.MailFrom = "sender@example.com"
	
	// Add alias in lowercase
	repo.AddAlias("test@webrana.id", true)
	
	// Test various case combinations
	testCases := []string{
		"test@webrana.id",
		"TEST@WEBRANA.ID",
		"Test@Webrana.Id",
		"tEsT@wEbRaNa.Id",
	}
	
	for _, tc := range testCases {
		t.Run(tc, func(t *testing.T) {
			// Reset session state
			session.state.Recipients = make([]string, 0)
			conn.writeBuf.Reset()
			
			session.handleRCPTTO("TO:<" + tc + ">")
			
			code, _ := getLastResponse(conn)
			if code != CodeOK {
				t.Errorf("Case-insensitive lookup for '%s' should succeed, got %d", tc, code)
			}
		})
	}
}
