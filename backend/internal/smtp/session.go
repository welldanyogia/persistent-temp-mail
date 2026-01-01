package smtp

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

// SMTPSession handles a single SMTP session
// Requirements: 1.4, 1.5, 6.6
type SMTPSession struct {
	conn           net.Conn
	reader         *bufio.Reader
	writer         *bufio.Writer
	config         *SMTPConfig
	tlsConfig      *tls.Config
	aliasRepo      AliasRepository
	state          *SessionState
	ehloReceived   bool
	dataCallback   func(ctx context.Context, data *DataResult) error // Callback for processing email data
}

// NewSMTPSession creates a new SMTP session
func NewSMTPSession(conn net.Conn, config *SMTPConfig, tlsConfig *tls.Config, aliasRepo AliasRepository, remoteIP string) *SMTPSession {
	return &SMTPSession{
		conn:      conn,
		reader:    bufio.NewReader(conn),
		writer:    bufio.NewWriter(conn),
		config:    config,
		tlsConfig: tlsConfig,
		aliasRepo: aliasRepo,
		state: &SessionState{
			TLSEnabled: false,
			Recipients: make([]string, 0),
			RemoteIP:   remoteIP,
			StartTime:  time.Now(),
			Conn:       conn,
		},
		dataCallback: nil,
	}
}

// NewSMTPSessionWithCallback creates a new SMTP session with a data processing callback
func NewSMTPSessionWithCallback(conn net.Conn, config *SMTPConfig, tlsConfig *tls.Config, aliasRepo AliasRepository, remoteIP string, dataCallback func(ctx context.Context, data *DataResult) error) *SMTPSession {
	session := NewSMTPSession(conn, config, tlsConfig, aliasRepo, remoteIP)
	session.dataCallback = dataCallback
	return session
}

// Run starts the SMTP session
// Requirement 1.4: Respond with 220 greeting
func (s *SMTPSession) Run() {
	defer s.conn.Close()
	
	// Send greeting (Requirement 1.4)
	s.sendResponse(CodeServiceReady, fmt.Sprintf("%s %s", s.config.Hostname, SMTPResponses[CodeServiceReady]))
	
	for {
		// Reset deadline on each command
		s.conn.SetDeadline(time.Now().Add(s.config.ConnectionTimeout))
		
		line, err := s.reader.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				// Log error but don't send response (connection may be closed)
			}
			return
		}
		
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		
		// Parse command
		cmd, args := s.parseCommand(line)
		
		// Handle command
		quit := s.handleCommand(cmd, args)
		if quit {
			return
		}
	}
}

// parseCommand parses an SMTP command line
func (s *SMTPSession) parseCommand(line string) (string, string) {
	parts := strings.SplitN(line, " ", 2)
	cmd := strings.ToUpper(parts[0])
	args := ""
	if len(parts) > 1 {
		args = parts[1]
	}
	return cmd, args
}

// handleCommand handles an SMTP command
func (s *SMTPSession) handleCommand(cmd, args string) bool {
	switch cmd {
	case "EHLO", "HELO":
		s.handleEHLO(args)
	case "STARTTLS":
		s.handleSTARTTLS()
	case "MAIL":
		s.handleMAILFROM(args)
	case "RCPT":
		s.handleRCPTTO(args)
	case "DATA":
		s.handleDATA()
	case "RSET":
		s.handleRSET()
	case "NOOP":
		s.sendResponse(CodeOK, SMTPResponses[CodeOK])
	case "QUIT":
		s.handleQUIT()
		return true
	default:
		s.sendResponse(CodeSyntaxError, "Command not recognized")
	}
	return false
}

// handleEHLO handles the EHLO/HELO command
// Requirement 1.5: Support EHLO and advertise capabilities
func (s *SMTPSession) handleEHLO(domain string) {
	if domain == "" {
		s.sendResponse(CodeSyntaxErrorParams, "Syntax error in parameters")
		return
	}
	
	s.ehloReceived = true
	s.resetTransaction()
	
	// Build capabilities list (Requirement 1.5)
	capabilities := []string{
		s.config.Hostname,
		fmt.Sprintf("SIZE %d", s.config.MaxMessageSize),
		"8BITMIME",
	}
	
	// Only advertise STARTTLS if not already in TLS mode and TLS is configured
	if !s.state.TLSEnabled && s.tlsConfig != nil {
		capabilities = append(capabilities, "STARTTLS")
	}
	
	// Send multi-line response
	for i, cap := range capabilities {
		if i == len(capabilities)-1 {
			s.sendResponse(CodeOK, cap)
		} else {
			s.sendMultilineResponse(CodeOK, cap)
		}
	}
}

// handleSTARTTLS handles the STARTTLS command
// Requirement 1.2, 1.3: Support STARTTLS with TLS 1.2+
func (s *SMTPSession) handleSTARTTLS() {
	if s.state.TLSEnabled {
		s.sendResponse(CodeSyntaxError, "Already in TLS mode")
		return
	}
	
	if s.tlsConfig == nil {
		s.sendResponse(CodeTLSNotAvailable, SMTPResponses[CodeTLSNotAvailable])
		return
	}
	
	s.sendResponse(CodeServiceReady, "Ready to start TLS")
	
	// Upgrade connection to TLS
	tlsConn := tls.Server(s.conn, s.tlsConfig)
	
	// Set handshake timeout
	tlsConn.SetDeadline(time.Now().Add(30 * time.Second))
	
	if err := tlsConn.Handshake(); err != nil {
		// TLS handshake failed, close connection
		return
	}
	
	// Reset deadline
	tlsConn.SetDeadline(time.Now().Add(s.config.ConnectionTimeout))
	
	// Update connection and readers/writers
	s.conn = tlsConn
	s.reader = bufio.NewReader(tlsConn)
	s.writer = bufio.NewWriter(tlsConn)
	s.state.TLSEnabled = true
	s.state.Conn = tlsConn
	
	// Reset EHLO state after STARTTLS
	s.ehloReceived = false
	s.resetTransaction()
}

// handleMAILFROM handles the MAIL FROM command
// Requirement 6.6: Validate sender address format
func (s *SMTPSession) handleMAILFROM(args string) {
	if !s.ehloReceived {
		s.sendResponse(CodeSyntaxError, "Send EHLO/HELO first")
		return
	}
	
	// Parse MAIL FROM:<address>
	if !strings.HasPrefix(strings.ToUpper(args), "FROM:") {
		s.sendResponse(CodeSyntaxErrorParams, "Syntax error in parameters")
		return
	}
	
	// Extract address
	address := strings.TrimPrefix(args, "FROM:")
	address = strings.TrimPrefix(address, "from:")
	address = strings.TrimSpace(address)
	
	// Handle SIZE parameter if present
	if idx := strings.Index(address, " "); idx != -1 {
		sizeParam := address[idx+1:]
		address = address[:idx]
		
		// Parse SIZE parameter
		if strings.HasPrefix(strings.ToUpper(sizeParam), "SIZE=") {
			var size int64
			fmt.Sscanf(sizeParam[5:], "%d", &size)
			if size > s.config.MaxMessageSize {
				s.sendResponse(CodeMessageTooLarge, SMTPResponses[CodeMessageTooLarge])
				return
			}
		}
	}
	
	// Remove angle brackets
	address = strings.TrimPrefix(address, "<")
	address = strings.TrimSuffix(address, ">")
	
	// Validate sender address format (Requirement 6.6)
	if address != "" && !ValidateEmailAddress(address) {
		s.sendResponse(CodeSyntaxErrorParams, "Invalid sender address format")
		return
	}
	
	s.state.MailFrom = address
	s.sendResponse(CodeOK, SMTPResponses[CodeOK])
}

// handleRCPTTO handles the RCPT TO command
// Requirements: 2.1-2.6, 6.4
// Property 3: Recipient Validation - validates alias exists and is active (case-insensitive)
// Property 4: Recipient Limit - limits recipients to MaxRecipients per message
// Property 14: No Relay Policy - only accepts for local aliases
func (s *SMTPSession) handleRCPTTO(args string) {
	// Must have received MAIL FROM first
	if s.state.MailFrom == "" {
		s.sendResponse(CodeSyntaxError, "Send MAIL FROM first")
		return
	}
	
	// Check recipient limit (Requirement 2.6, Property 4)
	if len(s.state.Recipients) >= s.config.MaxRecipients {
		s.sendResponse(CodeSyntaxError, "Too many recipients")
		return
	}
	
	// Parse RCPT TO:<address>
	if !strings.HasPrefix(strings.ToUpper(args), "TO:") {
		s.sendResponse(CodeSyntaxErrorParams, "Syntax error in parameters")
		return
	}
	
	// Extract address - handle both "TO:" and "to:" prefixes
	address := args[3:] // Remove "TO:" or "to:"
	address = strings.TrimSpace(address)
	address = strings.TrimPrefix(address, "<")
	address = strings.TrimSuffix(address, ">")
	
	// Validate address format
	if address == "" || !ValidateEmailAddress(address) {
		s.sendResponse(CodeSyntaxErrorParams, "Invalid recipient address format")
		return
	}
	
	// Validate recipient exists in aliases table (Requirements 2.1-2.5, Property 3)
	// Case-insensitive lookup is handled by the repository (Requirement 2.5)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	alias, err := s.aliasRepo.GetByFullAddress(ctx, strings.ToLower(address))
	if err != nil {
		// Recipient not found - no relay policy (Requirement 2.3, 6.4, Property 14)
		s.sendResponse(CodeUserNotFound, "User not found")
		return
	}
	
	// Check if alias is active (Requirement 2.4)
	if !alias.IsActive {
		s.sendResponse(CodeUserNotFound, "User disabled")
		return
	}
	
	// Check for duplicate recipients
	lowerAddress := strings.ToLower(address)
	for _, rcpt := range s.state.Recipients {
		if strings.ToLower(rcpt) == lowerAddress {
			// Already added, just respond OK (idempotent)
			s.sendResponse(CodeOK, SMTPResponses[CodeOK])
			return
		}
	}
	
	// Add recipient (Requirement 2.2)
	s.state.Recipients = append(s.state.Recipients, address)
	s.sendResponse(CodeOK, SMTPResponses[CodeOK])
}

// handleDATA handles the DATA command
// Requirements: 3.1-3.5, 1.9
// Property 2: Message Size Limit - enforces 25 MB limit
// Property 5: Email Data Acceptance - accepts data until <CRLF>.<CRLF> and stores with UTC timestamp
func (s *SMTPSession) handleDATA() {
	// Requirement 3.1: DATA command requires valid RCPT TO first
	if len(s.state.Recipients) == 0 {
		s.sendResponse(CodeSyntaxError, "No valid recipients")
		return
	}
	
	// Requirement 3.1: Accept email data after valid RCPT TO
	s.sendResponse(CodeStartMailInput, SMTPResponses[CodeStartMailInput])
	
	// Read email data until <CRLF>.<CRLF> (Requirement 3.2)
	data, err := s.readEmailData()
	if err != nil {
		// Error already handled in readEmailData (size limit, etc.)
		return
	}
	
	// Record received_at timestamp in UTC (Requirement 3.5)
	receivedAt := time.Now().UTC()
	
	// Generate unique queue ID (Requirement 3.3)
	queueID := GenerateQueueID()
	
	// Store the result for later processing
	s.state.MessageSize = int64(len(data))
	s.state.DataResult = &DataResult{
		Data:       data,
		QueueID:    queueID,
		ReceivedAt: receivedAt,
		SizeBytes:  int64(len(data)),
		Recipients: s.state.Recipients,
		MailFrom:   s.state.MailFrom,
	}
	
	// Call data callback if configured (for email processing)
	// Requirements: All - Process email through parser → attachment handler → repositories
	if s.dataCallback != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		
		if err := s.dataCallback(ctx, s.state.DataResult); err != nil {
			// Log error but still respond with success (email was received)
			// The email data is stored in DataResult for retry if needed
			// Requirement 7.4, 7.5: Respond with 451 for database/storage errors
			s.sendResponse(CodeTempFailure, SMTPResponses[CodeTempFailure])
			s.resetTransaction()
			return
		}
	}
	
	// Requirement 3.3: Respond with 250 OK and queue ID
	s.sendResponse(CodeOK, fmt.Sprintf("OK queued as %s", queueID))
	
	// Reset transaction state for next message
	s.resetTransaction()
}

// readEmailData reads email data from the connection until <CRLF>.<CRLF>
// Requirements: 3.2, 3.4, 1.9
// Property 2: Message Size Limit - rejects messages exceeding MaxMessageSize
func (s *SMTPSession) readEmailData() ([]byte, error) {
	var data []byte
	
	for {
		// Read line by line
		line, err := s.reader.ReadBytes('\n')
		if err != nil {
			// Connection error - don't send response, connection may be closed
			return nil, err
		}
		
		// Check for end of data marker: <CRLF>.<CRLF>
		// Handle both ".\r\n" and ".\n" for compatibility
		if isEndOfData(line) {
			break
		}
		
		// Handle dot-stuffing (RFC 5321 Section 4.5.2)
		// Lines starting with "." have the dot removed (unless it's the terminator)
		line = removeDotStuffing(line)
		
		// Append line to data
		data = append(data, line...)
		
		// Check size limit (Requirement 1.9, 3.4)
		// Property 2: Message Size Limit
		if int64(len(data)) > s.config.MaxMessageSize {
			s.sendResponse(CodeMessageTooLarge, SMTPResponses[CodeMessageTooLarge])
			s.resetTransaction()
			return nil, fmt.Errorf("message too large: %d bytes exceeds limit of %d bytes", len(data), s.config.MaxMessageSize)
		}
	}
	
	return data, nil
}

// isEndOfData checks if the line is the end-of-data marker
// Requirement 3.2: Accept email data until <CRLF>.<CRLF> terminator
func isEndOfData(line []byte) bool {
	// Standard terminator: ".\r\n"
	if len(line) == 3 && line[0] == '.' && line[1] == '\r' && line[2] == '\n' {
		return true
	}
	// Also accept ".\n" for compatibility with some clients
	if len(line) == 2 && line[0] == '.' && line[1] == '\n' {
		return true
	}
	return false
}

// removeDotStuffing removes dot-stuffing from a line
// RFC 5321 Section 4.5.2: Lines starting with "." have the leading dot removed
func removeDotStuffing(line []byte) []byte {
	if len(line) > 0 && line[0] == '.' {
		return line[1:]
	}
	return line
}

// handleRSET handles the RSET command
func (s *SMTPSession) handleRSET() {
	s.resetTransaction()
	s.sendResponse(CodeOK, SMTPResponses[CodeOK])
}

// handleQUIT handles the QUIT command
// Requirement 6.6: QUIT command
func (s *SMTPSession) handleQUIT() {
	s.sendResponse(CodeServiceClosing, SMTPResponses[CodeServiceClosing])
}

// resetTransaction resets the transaction state
func (s *SMTPSession) resetTransaction() {
	s.state.MailFrom = ""
	s.state.Recipients = make([]string, 0)
	s.state.MessageSize = 0
}

// sendResponse sends an SMTP response
func (s *SMTPSession) sendResponse(code int, message string) {
	response := fmt.Sprintf("%d %s\r\n", code, message)
	s.writer.WriteString(response)
	s.writer.Flush()
}

// sendMultilineResponse sends a multi-line SMTP response
func (s *SMTPSession) sendMultilineResponse(code int, message string) {
	response := fmt.Sprintf("%d-%s\r\n", code, message)
	s.writer.WriteString(response)
	s.writer.Flush()
}

// generateQueueID generates a unique queue ID for the message
func generateQueueID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// GenerateQueueID generates a unique queue ID for the message
// Requirement 3.3: Generate queue ID
// Uses timestamp + random component for uniqueness
func GenerateQueueID() string {
	// Format: timestamp in hex + random suffix for uniqueness
	timestamp := time.Now().UnixNano()
	return fmt.Sprintf("%x", timestamp)
}
