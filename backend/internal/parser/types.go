package parser

import (
	"time"
)

// ParsedEmail represents a fully parsed email message
type ParsedEmail struct {
	From        string            `json:"from"`
	FromName    string            `json:"from_name"`
	To          string            `json:"to"`
	Subject     string            `json:"subject"`
	BodyHTML    string            `json:"body_html"`
	BodyText    string            `json:"body_text"`
	Headers     map[string]string `json:"headers"`
	Attachments []*Attachment     `json:"attachments"`
	SizeBytes   int64             `json:"size_bytes"`
	ReceivedAt  time.Time         `json:"received_at"`
	RawEmail    []byte            `json:"-"` // Store raw email for error recovery
}

// Attachment represents an email attachment before processing
type Attachment struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Data        []byte `json:"-"`
	SizeBytes   int64  `json:"size_bytes"`
}

// ParseError represents an error during email parsing
type ParseError struct {
	Stage   string `json:"stage"`   // Which parsing stage failed
	Message string `json:"message"` // Error description
	Raw     []byte `json:"-"`       // Raw email data for recovery
}

// Error implements the error interface
func (e *ParseError) Error() string {
	return e.Message
}

// ContentType constants
const (
	ContentTypePlain       = "text/plain"
	ContentTypeHTML        = "text/html"
	ContentTypeMultiAlt    = "multipart/alternative"
	ContentTypeMultiMixed  = "multipart/mixed"
	ContentTypeOctetStream = "application/octet-stream"
)

// Encoding constants
const (
	EncodingQuotedPrintable = "quoted-printable"
	EncodingBase64          = "base64"
	Encoding7Bit            = "7bit"
	Encoding8Bit            = "8bit"
)

// Header constants
const (
	HeaderFrom        = "From"
	HeaderTo          = "To"
	HeaderSubject     = "Subject"
	HeaderContentType = "Content-Type"
	HeaderEncoding    = "Content-Transfer-Encoding"
	HeaderDisposition = "Content-Disposition"
)

// Limits
const (
	MaxHeaderLength = 1000 // Maximum header length per Requirements 6.2
)
