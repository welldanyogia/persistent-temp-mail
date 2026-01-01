package smtp

import (
	"crypto/tls"
	"net"
	"time"
)

// SMTPConfig holds SMTP server configuration
type SMTPConfig struct {
	Port                int
	Hostname            string
	MaxConnections      int
	MaxConnectionsPerIP int
	ConnectionTimeout   time.Duration
	MaxMessageSize      int64
	MaxRecipients       int
	RateLimitPerMinute  int
	TLSConfig           *tls.Config
}

// SessionState represents the current state of an SMTP session
type SessionState struct {
	TLSEnabled  bool
	MailFrom    string
	Recipients  []string
	MessageSize int64
	RemoteIP    string
	StartTime   time.Time
	Conn        net.Conn
	DataResult  *DataResult // Result from DATA command processing
}

// DataResult contains the result of processing DATA command
// Requirements: 3.1-3.5, 1.9
type DataResult struct {
	Data       []byte    // Raw email data
	QueueID    string    // Unique queue identifier
	ReceivedAt time.Time // Timestamp in UTC
	SizeBytes  int64     // Size of the email data
	Recipients []string  // List of recipients
	MailFrom   string    // Sender address
}

// SMTPError represents an SMTP error with code and message
type SMTPError struct {
	Code          int       `json:"code"`
	Message       string    `json:"message"`
	CorrelationID string    `json:"correlation_id"`
	RemoteIP      string    `json:"remote_ip"`
	Timestamp     time.Time `json:"timestamp"`
	Details       string    `json:"details,omitempty"`
}

// Error implements the error interface
func (e *SMTPError) Error() string {
	return e.Message
}

// SMTP Response Codes
const (
	CodeServiceReady       = 220
	CodeServiceClosing     = 221
	CodeOK                 = 250
	CodeStartMailInput     = 354
	CodeServiceUnavailable = 421
	CodeTempFailure        = 451
	CodeTLSNotAvailable    = 454
	CodeSyntaxError        = 500
	CodeSyntaxErrorParams  = 501
	CodeUserNotFound       = 550
	CodeMessageTooLarge    = 552
)

// SMTP Response Messages
var SMTPResponses = map[int]string{
	CodeServiceReady:       "ESMTP",
	CodeServiceClosing:     "Bye",
	CodeOK:                 "OK",
	CodeStartMailInput:     "Start mail input; end with <CRLF>.<CRLF>",
	CodeServiceUnavailable: "Service not available",
	CodeTempFailure:        "Temporary failure",
	CodeTLSNotAvailable:    "TLS not available",
	CodeSyntaxError:        "Syntax error",
	CodeSyntaxErrorParams:  "Syntax error in parameters",
	CodeUserNotFound:       "User not found",
	CodeMessageTooLarge:    "Message too large",
}
