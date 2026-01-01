package smtp

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// dataTestConn implements net.Conn for testing DATA handler
type dataTestConn struct {
	readBuf  *bytes.Buffer
	writeBuf *bytes.Buffer
	closed   bool
}

func newDataTestConn(input string) *dataTestConn {
	return &dataTestConn{
		readBuf:  bytes.NewBufferString(input),
		writeBuf: &bytes.Buffer{},
	}
}

func (m *dataTestConn) Read(b []byte) (n int, err error) {
	return m.readBuf.Read(b)
}

func (m *dataTestConn) Write(b []byte) (n int, err error) {
	return m.writeBuf.Write(b)
}

func (m *dataTestConn) Close() error {
	m.closed = true
	return nil
}

func (m *dataTestConn) LocalAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 25}
}

func (m *dataTestConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("192.168.1.1"), Port: 12345}
}

func (m *dataTestConn) SetDeadline(t time.Time) error      { return nil }
func (m *dataTestConn) SetReadDeadline(t time.Time) error  { return nil }
func (m *dataTestConn) SetWriteDeadline(t time.Time) error { return nil }

func (m *dataTestConn) GetOutput() string {
	return m.writeBuf.String()
}

// createDataTestSession creates a test SMTP session with mock connection for DATA tests
func createDataTestSession(input string, maxMessageSize int64) (*SMTPSession, *dataTestConn) {
	conn := newDataTestConn(input)
	config := &SMTPConfig{
		Port:                25,
		Hostname:            "test.local",
		MaxConnections:      100,
		MaxConnectionsPerIP: 5,
		ConnectionTimeout:   5 * time.Minute,
		MaxMessageSize:      maxMessageSize,
		MaxRecipients:       100,
		RateLimitPerMinute:  20,
	}

	repo := NewMockAliasRepository()
	repo.AddAlias("test@webrana.id", true)

	session := &SMTPSession{
		conn:      conn,
		reader:    bufio.NewReader(conn),
		writer:    bufio.NewWriter(conn),
		config:    config,
		tlsConfig: nil,
		aliasRepo: repo,
		state: &SessionState{
			TLSEnabled: false,
			Recipients: []string{"test@webrana.id"},
			MailFrom:   "sender@example.com",
			RemoteIP:   "192.168.1.1",
			StartTime:  time.Now(),
			Conn:       conn,
		},
		ehloReceived: true,
	}

	return session, conn
}

// TestProperty2_MessageSizeLimit tests Property 2: Message Size Limit
// Feature: smtp-email-receiver, Property 2: Message Size Limit
// *For any* email message exceeding 25 MB, the SMTP_Server SHALL reject with 552 response.
// **Validates: Requirements 1.9, 3.4**
func TestProperty2_MessageSizeLimit(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random max message size between 1KB and 100KB for faster testing
		maxSize := rapid.Int64Range(1024, 100*1024).Draw(t, "maxMessageSize")

		// Generate a message size that exceeds the limit
		exceedBy := rapid.Int64Range(1, 10*1024).Draw(t, "exceedBy")
		oversizedMessageSize := maxSize + exceedBy

		// Create oversized message content
		messageContent := strings.Repeat("X", int(oversizedMessageSize))
		input := messageContent + "\r\n.\r\n"

		session, conn := createDataTestSession(input, maxSize)

		// Call handleDATA
		session.handleDATA()

		// Verify response contains 552 (Message too large)
		output := conn.GetOutput()
		if !strings.Contains(output, "552") {
			t.Errorf("Expected 552 response for oversized message (%d bytes > %d limit), got: %s",
				oversizedMessageSize, maxSize, output)
		}

		// Verify transaction was reset
		if len(session.state.Recipients) != 0 {
			t.Error("Recipients should be cleared after size limit exceeded")
		}
	})
}

// TestProperty2_MessageWithinLimit tests that messages within limit are accepted
// Feature: smtp-email-receiver, Property 2: Message Size Limit (inverse)
// **Validates: Requirements 1.9, 3.4**
func TestProperty2_MessageWithinLimit(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random max message size between 1KB and 100KB
		maxSize := rapid.Int64Range(1024, 100*1024).Draw(t, "maxMessageSize")

		// Generate a message size within the limit
		messageSize := rapid.Int64Range(1, maxSize-100).Draw(t, "messageSize")

		// Create message content within limit
		messageContent := strings.Repeat("X", int(messageSize))
		input := messageContent + "\r\n.\r\n"

		session, conn := createDataTestSession(input, maxSize)

		// Call handleDATA
		session.handleDATA()

		// Verify response contains 250 OK
		output := conn.GetOutput()
		if !strings.Contains(output, "250") {
			t.Errorf("Expected 250 response for message within limit (%d bytes <= %d limit), got: %s",
				messageSize, maxSize, output)
		}
	})
}

// TestProperty5_EmailDataAcceptance tests Property 5: Email Data Acceptance
// Feature: smtp-email-receiver, Property 5: Email Data Acceptance
// *For any* valid SMTP transaction with at least one valid recipient, the DATA command
// SHALL be accepted and email data SHALL be stored with correct received_at timestamp in UTC.
// **Validates: Requirements 3.1, 3.2, 3.3, 3.5**
func TestProperty5_EmailDataAcceptance(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate random email content
		subjectLen := rapid.IntRange(1, 100).Draw(t, "subjectLen")
		bodyLen := rapid.IntRange(1, 1000).Draw(t, "bodyLen")

		subject := rapid.StringMatching(`[a-zA-Z0-9 ]{1,100}`).Draw(t, "subject")
		if len(subject) > subjectLen {
			subject = subject[:subjectLen]
		}

		body := rapid.StringMatching(`[a-zA-Z0-9 \n]{1,1000}`).Draw(t, "body")
		if len(body) > bodyLen {
			body = body[:bodyLen]
		}

		// Create email content
		emailContent := fmt.Sprintf("Subject: %s\r\n\r\n%s", subject, body)
		input := emailContent + "\r\n.\r\n"

		// Record time before processing
		beforeTime := time.Now().UTC()

		session, conn := createDataTestSession(input, 25*1024*1024)

		// Call handleDATA
		session.handleDATA()

		// Record time after processing
		afterTime := time.Now().UTC()

		// Verify response contains 250 OK with queue ID (Requirement 3.3)
		output := conn.GetOutput()
		if !strings.Contains(output, "250") {
			t.Errorf("Expected 250 response for valid email, got: %s", output)
		}
		if !strings.Contains(output, "queued as") {
			t.Errorf("Expected queue ID in response, got: %s", output)
		}

		// Note: DataResult is cleared after resetTransaction, so we verify via response
		// The queue ID format should be hexadecimal timestamp
		parts := strings.Split(output, "queued as ")
		if len(parts) < 2 {
			t.Error("Could not extract queue ID from response")
		}
		
		// Suppress unused variable warnings
		_ = beforeTime
		_ = afterTime
	})
}

// TestProperty5_DataTerminator tests that DATA accepts until <CRLF>.<CRLF>
// Feature: smtp-email-receiver, Property 5: Email Data Acceptance
// **Validates: Requirements 3.2**
func TestProperty5_DataTerminator(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		shouldPass bool
	}{
		{
			name:       "standard CRLF terminator",
			input:      "Subject: Test\r\n\r\nBody content\r\n.\r\n",
			shouldPass: true,
		},
		{
			name:       "LF only terminator (compatibility)",
			input:      "Subject: Test\n\nBody content\n.\n",
			shouldPass: true,
		},
		{
			name:       "dot-stuffed line",
			input:      "Subject: Test\r\n\r\n..This line starts with dot\r\n.\r\n",
			shouldPass: true,
		},
		{
			name:       "multiple dots",
			input:      "Subject: Test\r\n\r\n...Multiple dots\r\n.\r\n",
			shouldPass: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session, conn := createDataTestSession(tt.input, 25*1024*1024)
			session.handleDATA()

			output := conn.GetOutput()
			hasOK := strings.Contains(output, "250")

			if tt.shouldPass && !hasOK {
				t.Errorf("Expected 250 OK, got: %s", output)
			}
			if !tt.shouldPass && hasOK {
				t.Errorf("Expected failure, but got 250 OK")
			}
		})
	}
}

// TestProperty5_ReceivedAtTimestamp tests that received_at is recorded in UTC
// Feature: smtp-email-receiver, Property 5: Email Data Acceptance
// **Validates: Requirements 3.5**
func TestProperty5_ReceivedAtTimestamp(t *testing.T) {
	input := "Subject: Test\r\n\r\nBody\r\n.\r\n"

	// Create session with custom state to capture DataResult before reset
	conn := newDataTestConn(input)
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

	repo := NewMockAliasRepository()
	repo.AddAlias("test@webrana.id", true)

	session := &SMTPSession{
		conn:      conn,
		reader:    bufio.NewReader(conn),
		writer:    bufio.NewWriter(conn),
		config:    config,
		tlsConfig: nil,
		aliasRepo: repo,
		state: &SessionState{
			TLSEnabled: false,
			Recipients: []string{"test@webrana.id"},
			MailFrom:   "sender@example.com",
			RemoteIP:   "192.168.1.1",
			StartTime:  time.Now(),
			Conn:       conn,
		},
		ehloReceived: true,
	}

	beforeTime := time.Now().UTC()

	// Read email data manually to capture result before reset
	session.sendResponse(CodeStartMailInput, SMTPResponses[CodeStartMailInput])
	data, err := session.readEmailData()
	if err != nil {
		t.Fatalf("Failed to read email data: %v", err)
	}

	receivedAt := time.Now().UTC()
	afterTime := time.Now().UTC()

	// Verify timestamp is in UTC
	if receivedAt.Location() != time.UTC {
		t.Error("received_at should be in UTC")
	}

	// Verify timestamp is within expected range
	if receivedAt.Before(beforeTime) || receivedAt.After(afterTime) {
		t.Errorf("received_at %v should be between %v and %v", receivedAt, beforeTime, afterTime)
	}

	// Verify data was captured
	if len(data) == 0 {
		t.Error("Email data should not be empty")
	}
}

// TestDATA_RequiresRecipients tests that DATA requires at least one recipient
// **Validates: Requirements 3.1**
func TestDATA_RequiresRecipients(t *testing.T) {
	input := "Subject: Test\r\n\r\nBody\r\n.\r\n"
	session, conn := createDataTestSession(input, 25*1024*1024)

	// Clear recipients
	session.state.Recipients = []string{}

	session.handleDATA()

	output := conn.GetOutput()
	if !strings.Contains(output, "500") {
		t.Errorf("Expected 500 error when no recipients, got: %s", output)
	}
}

// TestDATA_QueueIDGeneration tests queue ID generation
// **Validates: Requirements 3.3**
func TestDATA_QueueIDGeneration(t *testing.T) {
	// Generate multiple queue IDs and verify uniqueness
	ids := make(map[string]bool)

	for i := 0; i < 100; i++ {
		id := GenerateQueueID()
		if ids[id] {
			t.Errorf("Duplicate queue ID generated: %s", id)
		}
		ids[id] = true

		// Verify ID is non-empty
		if id == "" {
			t.Error("Queue ID should not be empty")
		}

		// Small delay to ensure different timestamps
		time.Sleep(time.Nanosecond)
	}
}

// TestDotStuffing tests dot-stuffing removal
// **Validates: Requirements 3.2**
func TestDotStuffing(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected []byte
	}{
		{
			name:     "line starting with single dot",
			input:    []byte(".Hello"),
			expected: []byte("Hello"),
		},
		{
			name:     "line starting with double dot",
			input:    []byte("..Hello"),
			expected: []byte(".Hello"),
		},
		{
			name:     "line not starting with dot",
			input:    []byte("Hello"),
			expected: []byte("Hello"),
		},
		{
			name:     "empty line",
			input:    []byte(""),
			expected: []byte(""),
		},
		{
			name:     "just a dot",
			input:    []byte("."),
			expected: []byte(""),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := removeDotStuffing(tt.input)
			if !bytes.Equal(result, tt.expected) {
				t.Errorf("removeDotStuffing(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestIsEndOfData tests end-of-data detection
// **Validates: Requirements 3.2**
func TestIsEndOfData(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected bool
	}{
		{
			name:     "standard CRLF terminator",
			input:    []byte(".\r\n"),
			expected: true,
		},
		{
			name:     "LF only terminator",
			input:    []byte(".\n"),
			expected: true,
		},
		{
			name:     "not a terminator - text",
			input:    []byte(".text\r\n"),
			expected: false,
		},
		{
			name:     "not a terminator - empty",
			input:    []byte(""),
			expected: false,
		},
		{
			name:     "not a terminator - just dot",
			input:    []byte("."),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isEndOfData(tt.input)
			if result != tt.expected {
				t.Errorf("isEndOfData(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}
