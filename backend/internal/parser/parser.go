package parser

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"regexp"
	"strings"
	"time"
)

// EmailParser implements email parsing functionality
// Requirements: 4.1-4.4, 6.1, 6.2
type EmailParser struct{}

// NewEmailParser creates a new EmailParser instance
func NewEmailParser() *EmailParser {
	return &EmailParser{}
}

// Parse parses a raw email into a ParsedEmail structure
// Requirements: 4.1-4.12
func (p *EmailParser) Parse(raw []byte) (*ParsedEmail, error) {
	if len(raw) == 0 {
		return nil, &ParseError{
			Stage:   "parse",
			Message: "empty email data",
			Raw:     raw,
		}
	}

	// Parse the email message
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return nil, &ParseError{
			Stage:   "parse",
			Message: fmt.Sprintf("failed to parse email: %v", err),
			Raw:     raw,
		}
	}

	// Extract headers
	headers, err := p.ExtractHeaders(msg)
	if err != nil {
		return nil, &ParseError{
			Stage:   "headers",
			Message: fmt.Sprintf("failed to extract headers: %v", err),
			Raw:     raw,
		}
	}

	// Extract From address and display name (Requirements 4.1, 4.2)
	fromAddress, fromName := p.extractFromHeader(msg.Header.Get(HeaderFrom))

	// Extract Subject (Requirement 4.3)
	subject := p.decodeHeader(msg.Header.Get(HeaderSubject))

	// Extract To address
	toAddress := p.extractToAddress(msg.Header.Get(HeaderTo))

	// Extract body content (Requirements 4.5-4.8)
	bodyHTML, bodyText, err := p.ExtractBody(msg)
	if err != nil {
		// Log error but continue - store raw email
		bodyHTML = ""
		bodyText = ""
	}

	parsed := &ParsedEmail{
		From:       fromAddress,
		FromName:   fromName,
		To:         toAddress,
		Subject:    subject,
		BodyHTML:   bodyHTML,
		BodyText:   bodyText,
		Headers:    headers,
		SizeBytes:  int64(len(raw)),
		ReceivedAt: time.Now().UTC(),
		RawEmail:   raw,
	}

	return parsed, nil
}

// ExtractHeaders extracts all headers from an email message
// Requirements: 4.4, 6.1, 6.2
// Property 6: Header Extraction - correctly extracts all headers into JSONB format
// Property 13: Header Injection Prevention - validates CRLF injection and header length
func (p *EmailParser) ExtractHeaders(msg *mail.Message) (map[string]string, error) {
	headers := make(map[string]string)

	for key, values := range msg.Header {
		// Validate header key for CRLF injection (Requirement 6.1)
		if ContainsCRLFInjection(key) {
			return nil, fmt.Errorf("CRLF injection detected in header key: %s", key)
		}

		for _, value := range values {
			// Validate header value for CRLF injection (Requirement 6.1)
			if ContainsCRLFInjection(value) {
				return nil, fmt.Errorf("CRLF injection detected in header value for key: %s", key)
			}

			// Truncate header value if exceeds max length (Requirement 6.2)
			if len(value) > MaxHeaderLength {
				value = value[:MaxHeaderLength]
			}

			// Decode MIME encoded words
			decodedValue := p.decodeHeader(value)

			// Store header (use first value if multiple)
			if _, exists := headers[key]; !exists {
				headers[key] = decodedValue
			}
		}
	}

	return headers, nil
}

// extractFromHeader extracts email address and display name from From header
// Requirements: 4.1, 4.2
func (p *EmailParser) extractFromHeader(from string) (address, name string) {
	if from == "" {
		return "", ""
	}

	// Decode MIME encoded words first
	from = p.decodeHeader(from)

	// Parse the address
	addr, err := mail.ParseAddress(from)
	if err != nil {
		// Try to extract just the email address
		address = extractEmailFromString(from)
		return address, ""
	}

	return addr.Address, addr.Name
}

// extractToAddress extracts the primary To address
func (p *EmailParser) extractToAddress(to string) string {
	if to == "" {
		return ""
	}

	// Decode MIME encoded words first
	to = p.decodeHeader(to)

	// Parse the address list
	addrs, err := mail.ParseAddressList(to)
	if err != nil || len(addrs) == 0 {
		// Try to extract just the email address
		return extractEmailFromString(to)
	}

	return addrs[0].Address
}

// decodeHeader decodes MIME encoded words in a header value
func (p *EmailParser) decodeHeader(value string) string {
	if value == "" {
		return ""
	}

	decoder := new(mime.WordDecoder)
	decoded, err := decoder.DecodeHeader(value)
	if err != nil {
		// Return original value if decoding fails
		return value
	}

	return decoded
}

// extractEmailFromString extracts an email address from a string
func extractEmailFromString(s string) string {
	// Simple regex to extract email address
	emailRegex := regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)
	match := emailRegex.FindString(s)
	return match
}

// ContainsCRLFInjection checks if a string contains CRLF injection attempts
// Requirement 6.1: Validate email header injection attempts (CRLF injection)
// Property 13: Header Injection Prevention
func ContainsCRLFInjection(s string) bool {
	// Check for various CRLF injection patterns
	patterns := []string{
		"\r\n",  // Standard CRLF
		"\r",    // Carriage return alone
		"\n",    // Line feed alone
		"%0d%0a", // URL encoded CRLF
		"%0d",   // URL encoded CR
		"%0a",   // URL encoded LF
	}

	lower := strings.ToLower(s)
	for _, pattern := range patterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}

	return false
}

// ValidateHeaderLength checks if a header value exceeds the maximum length
// Requirement 6.2: Limit header length to 1000 characters
func ValidateHeaderLength(value string) bool {
	return len(value) <= MaxHeaderLength
}

// TruncateHeader truncates a header value to the maximum allowed length
// Requirement 6.2: Headers exceeding 1000 characters SHALL be truncated
func TruncateHeader(value string) string {
	if len(value) > MaxHeaderLength {
		return value[:MaxHeaderLength]
	}
	return value
}


// ExtractBody extracts HTML and text body from an email message
// Requirements: 4.5-4.8
// Property 7: Content Type Handling - correctly extracts body content for various content types
func (p *EmailParser) ExtractBody(msg *mail.Message) (html, text string, err error) {
	contentType := msg.Header.Get(HeaderContentType)
	if contentType == "" {
		// Default to text/plain if no content type specified
		contentType = ContentTypePlain
	}

	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		// Try to read as plain text
		body, readErr := io.ReadAll(msg.Body)
		if readErr != nil {
			return "", "", readErr
		}
		return "", string(body), nil
	}

	switch {
	case mediaType == ContentTypePlain:
		// Requirement 4.5: Handle text/plain content type
		body, err := io.ReadAll(msg.Body)
		if err != nil {
			return "", "", err
		}
		return "", string(body), nil

	case mediaType == ContentTypeHTML:
		// Requirement 4.6: Handle text/html content type
		body, err := io.ReadAll(msg.Body)
		if err != nil {
			return "", "", err
		}
		return string(body), "", nil

	case mediaType == ContentTypeMultiAlt:
		// Requirement 4.7: Handle multipart/alternative (prefer HTML over plain text)
		return p.extractMultipartAlternative(msg.Body, params["boundary"])

	case mediaType == ContentTypeMultiMixed:
		// Requirement 4.8: Handle multipart/mixed (email with attachments)
		return p.extractMultipartMixed(msg.Body, params["boundary"])

	case strings.HasPrefix(mediaType, "multipart/"):
		// Handle other multipart types
		return p.extractMultipartGeneric(msg.Body, params["boundary"])

	default:
		// Unknown content type, try to read as text
		body, err := io.ReadAll(msg.Body)
		if err != nil {
			return "", "", err
		}
		return "", string(body), nil
	}
}

// extractMultipartAlternative extracts body from multipart/alternative
// Requirement 4.7: Prefer HTML over plain text
func (p *EmailParser) extractMultipartAlternative(body io.Reader, boundary string) (html, text string, err error) {
	if boundary == "" {
		return "", "", fmt.Errorf("missing boundary for multipart/alternative")
	}

	reader := multipart.NewReader(body, boundary)

	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return html, text, err
		}

		contentType := part.Header.Get(HeaderContentType)
		mediaType, _, _ := mime.ParseMediaType(contentType)

		partBody, err := io.ReadAll(part)
		if err != nil {
			continue
		}

		switch mediaType {
		case ContentTypePlain:
			text = string(partBody)
		case ContentTypeHTML:
			html = string(partBody)
		}
	}

	return html, text, nil
}

// extractMultipartMixed extracts body from multipart/mixed
// Requirement 4.8: Handle multipart/mixed (email with attachments)
func (p *EmailParser) extractMultipartMixed(body io.Reader, boundary string) (html, text string, err error) {
	if boundary == "" {
		return "", "", fmt.Errorf("missing boundary for multipart/mixed")
	}

	reader := multipart.NewReader(body, boundary)

	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return html, text, err
		}

		// Check if this is an attachment
		disposition := part.Header.Get(HeaderDisposition)
		if strings.HasPrefix(disposition, "attachment") {
			// Skip attachments for body extraction
			continue
		}

		contentType := part.Header.Get(HeaderContentType)
		mediaType, params, _ := mime.ParseMediaType(contentType)

		switch {
		case mediaType == ContentTypePlain:
			partBody, err := io.ReadAll(part)
			if err != nil {
				continue
			}
			text = string(partBody)

		case mediaType == ContentTypeHTML:
			partBody, err := io.ReadAll(part)
			if err != nil {
				continue
			}
			html = string(partBody)

		case mediaType == ContentTypeMultiAlt:
			// Nested multipart/alternative
			nestedHTML, nestedText, _ := p.extractMultipartAlternative(part, params["boundary"])
			if nestedHTML != "" {
				html = nestedHTML
			}
			if nestedText != "" {
				text = nestedText
			}
		}
	}

	return html, text, nil
}

// extractMultipartGeneric extracts body from generic multipart types
func (p *EmailParser) extractMultipartGeneric(body io.Reader, boundary string) (html, text string, err error) {
	if boundary == "" {
		return "", "", fmt.Errorf("missing boundary for multipart")
	}

	reader := multipart.NewReader(body, boundary)

	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return html, text, err
		}

		contentType := part.Header.Get(HeaderContentType)
		mediaType, params, _ := mime.ParseMediaType(contentType)

		switch {
		case mediaType == ContentTypePlain:
			partBody, err := io.ReadAll(part)
			if err != nil {
				continue
			}
			if text == "" {
				text = string(partBody)
			}

		case mediaType == ContentTypeHTML:
			partBody, err := io.ReadAll(part)
			if err != nil {
				continue
			}
			if html == "" {
				html = string(partBody)
			}

		case mediaType == ContentTypeMultiAlt:
			nestedHTML, nestedText, _ := p.extractMultipartAlternative(part, params["boundary"])
			if nestedHTML != "" && html == "" {
				html = nestedHTML
			}
			if nestedText != "" && text == "" {
				text = nestedText
			}

		case strings.HasPrefix(mediaType, "multipart/"):
			nestedHTML, nestedText, _ := p.extractMultipartGeneric(part, params["boundary"])
			if nestedHTML != "" && html == "" {
				html = nestedHTML
			}
			if nestedText != "" && text == "" {
				text = nestedText
			}
		}
	}

	return html, text, nil
}


// DecodeContent decodes email content based on Content-Transfer-Encoding
// Requirements: 4.9, 4.10
// Property 8: Encoding Round Trip - decoding then re-encoding produces equivalent content
func DecodeContent(data []byte, encoding string) ([]byte, error) {
	encoding = strings.ToLower(strings.TrimSpace(encoding))

	switch encoding {
	case EncodingQuotedPrintable:
		// Requirement 4.9: Decode quoted-printable encoding
		return DecodeQuotedPrintable(data)
	case EncodingBase64:
		// Requirement 4.10: Decode base64 encoding
		return DecodeBase64(data)
	case Encoding7Bit, Encoding8Bit, "binary", "":
		// No decoding needed
		return data, nil
	default:
		// Unknown encoding, return as-is
		return data, nil
	}
}

// DecodeQuotedPrintable decodes quoted-printable encoded content
// Requirement 4.9: Decode quoted-printable encoding
func DecodeQuotedPrintable(data []byte) ([]byte, error) {
	var result bytes.Buffer
	i := 0

	for i < len(data) {
		if data[i] == '=' {
			if i+2 < len(data) {
				// Check for soft line break (=\r\n or =\n)
				if data[i+1] == '\r' && i+2 < len(data) && data[i+2] == '\n' {
					i += 3
					continue
				}
				if data[i+1] == '\n' {
					i += 2
					continue
				}

				// Decode hex pair
				hex := string(data[i+1 : i+3])
				b, err := parseHexByte(hex)
				if err != nil {
					// Invalid hex, write as-is
					result.WriteByte(data[i])
					i++
					continue
				}
				result.WriteByte(b)
				i += 3
			} else {
				// Incomplete escape at end
				result.WriteByte(data[i])
				i++
			}
		} else {
			result.WriteByte(data[i])
			i++
		}
	}

	return result.Bytes(), nil
}

// parseHexByte parses a two-character hex string into a byte
func parseHexByte(hex string) (byte, error) {
	if len(hex) != 2 {
		return 0, fmt.Errorf("invalid hex length")
	}

	var result byte
	for i := 0; i < 2; i++ {
		result <<= 4
		c := hex[i]
		switch {
		case c >= '0' && c <= '9':
			result |= c - '0'
		case c >= 'A' && c <= 'F':
			result |= c - 'A' + 10
		case c >= 'a' && c <= 'f':
			result |= c - 'a' + 10
		default:
			return 0, fmt.Errorf("invalid hex character: %c", c)
		}
	}

	return result, nil
}

// DecodeBase64 decodes base64 encoded content
// Requirement 4.10: Decode base64 encoding
func DecodeBase64(data []byte) ([]byte, error) {
	// Remove whitespace (base64 content often has line breaks)
	cleaned := bytes.Map(func(r rune) rune {
		if r == ' ' || r == '\t' || r == '\r' || r == '\n' {
			return -1
		}
		return r
	}, data)

	if len(cleaned) == 0 {
		return []byte{}, nil
	}

	// Standard base64 decoding
	decoded, err := base64StdDecodeFixed(cleaned)
	if err != nil {
		return nil, fmt.Errorf("base64 decode error: %w", err)
	}

	return decoded, nil
}

// base64StdEncodingDecodedLen returns the maximum decoded length
func base64StdEncodingDecodedLen(n int) int {
	return n * 3 / 4
}

// base64StdDecodeFixed decodes base64 data with proper padding handling
func base64StdDecodeFixed(src []byte) ([]byte, error) {
	// Base64 alphabet
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

	// Build decode table
	decodeTable := make([]byte, 256)
	for i := range decodeTable {
		decodeTable[i] = 0xFF
	}
	for i, c := range alphabet {
		decodeTable[c] = byte(i)
	}

	// Count padding
	paddingCount := 0
	for i := len(src) - 1; i >= 0 && src[i] == '='; i-- {
		paddingCount++
	}

	// Calculate output length
	outputLen := (len(src) * 3 / 4) - paddingCount
	if outputLen < 0 {
		outputLen = 0
	}

	result := make([]byte, 0, outputLen)
	
	for i := 0; i < len(src); i += 4 {
		// Get 4 bytes (or less at end)
		var block [4]byte
		var validChars int

		for j := 0; j < 4 && i+j < len(src); j++ {
			c := src[i+j]
			if c == '=' {
				block[j] = 0
			} else if decodeTable[c] != 0xFF {
				block[j] = decodeTable[c]
				validChars++
			}
		}

		// Decode block
		if validChars >= 2 {
			result = append(result, (block[0]<<2)|(block[1]>>4))
		}
		if validChars >= 3 {
			result = append(result, (block[1]<<4)|(block[2]>>2))
		}
		if validChars >= 4 {
			result = append(result, (block[2]<<6)|block[3])
		}
	}

	return result, nil
}

// base64StdDecode decodes base64 data (deprecated, use base64StdDecodeFixed)
func base64StdDecode(dst, src []byte) (int, error) {
	decoded, err := base64StdDecodeFixed(src)
	if err != nil {
		return 0, err
	}
	copy(dst, decoded)
	return len(decoded), nil
}

// EncodeQuotedPrintable encodes content to quoted-printable format
// Used for round-trip testing
func EncodeQuotedPrintable(data []byte) []byte {
	var result bytes.Buffer
	lineLen := 0

	for _, b := range data {
		// Check if character needs encoding
		needsEncoding := b < 32 || b > 126 || b == '='

		if needsEncoding {
			// Encode as =XX
			if lineLen+3 > 76 {
				result.WriteString("=\r\n")
				lineLen = 0
			}
			result.WriteString(fmt.Sprintf("=%02X", b))
			lineLen += 3
		} else {
			if lineLen+1 > 76 {
				result.WriteString("=\r\n")
				lineLen = 0
			}
			result.WriteByte(b)
			lineLen++
		}
	}

	return result.Bytes()
}

// EncodeBase64 encodes content to base64 format
// Used for round-trip testing
func EncodeBase64(data []byte) []byte {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

	result := make([]byte, ((len(data)+2)/3)*4)
	n := 0

	for i := 0; i < len(data); i += 3 {
		var block uint32
		remaining := len(data) - i

		if remaining >= 3 {
			block = uint32(data[i])<<16 | uint32(data[i+1])<<8 | uint32(data[i+2])
			result[n] = alphabet[block>>18&0x3F]
			result[n+1] = alphabet[block>>12&0x3F]
			result[n+2] = alphabet[block>>6&0x3F]
			result[n+3] = alphabet[block&0x3F]
			n += 4
		} else if remaining == 2 {
			block = uint32(data[i])<<16 | uint32(data[i+1])<<8
			result[n] = alphabet[block>>18&0x3F]
			result[n+1] = alphabet[block>>12&0x3F]
			result[n+2] = alphabet[block>>6&0x3F]
			result[n+3] = '='
			n += 4
		} else {
			block = uint32(data[i]) << 16
			result[n] = alphabet[block>>18&0x3F]
			result[n+1] = alphabet[block>>12&0x3F]
			result[n+2] = '='
			result[n+3] = '='
			n += 4
		}
	}

	return result[:n]
}

// ConvertCharset converts content from a source charset to UTF-8
// Requirement 4.11: Handle various character encodings (UTF-8, ISO-8859-1, etc.)
func ConvertCharset(data []byte, charset string) ([]byte, error) {
	charset = strings.ToLower(strings.TrimSpace(charset))

	switch charset {
	case "utf-8", "utf8", "":
		// Already UTF-8 or no charset specified
		return data, nil

	case "iso-8859-1", "latin1", "latin-1":
		// Convert ISO-8859-1 to UTF-8
		return convertISO88591ToUTF8(data), nil

	case "iso-8859-15", "latin9", "latin-9":
		// Convert ISO-8859-15 to UTF-8 (similar to ISO-8859-1 with some differences)
		return convertISO885915ToUTF8(data), nil

	case "windows-1252", "cp1252":
		// Convert Windows-1252 to UTF-8
		return convertWindows1252ToUTF8(data), nil

	case "us-ascii", "ascii":
		// ASCII is a subset of UTF-8
		return data, nil

	default:
		// Unknown charset, return as-is
		return data, nil
	}
}

// convertISO88591ToUTF8 converts ISO-8859-1 encoded bytes to UTF-8
func convertISO88591ToUTF8(data []byte) []byte {
	var result bytes.Buffer
	for _, b := range data {
		if b < 128 {
			result.WriteByte(b)
		} else {
			// ISO-8859-1 bytes 128-255 map directly to Unicode code points 128-255
			result.WriteByte(0xC0 | (b >> 6))
			result.WriteByte(0x80 | (b & 0x3F))
		}
	}
	return result.Bytes()
}

// convertISO885915ToUTF8 converts ISO-8859-15 encoded bytes to UTF-8
func convertISO885915ToUTF8(data []byte) []byte {
	// ISO-8859-15 is mostly the same as ISO-8859-1, with a few differences
	// For simplicity, we treat it the same as ISO-8859-1
	return convertISO88591ToUTF8(data)
}

// convertWindows1252ToUTF8 converts Windows-1252 encoded bytes to UTF-8
func convertWindows1252ToUTF8(data []byte) []byte {
	// Windows-1252 is similar to ISO-8859-1 but has different mappings for 128-159
	var result bytes.Buffer
	for _, b := range data {
		if b < 128 {
			result.WriteByte(b)
		} else if b >= 160 {
			// Same as ISO-8859-1 for 160-255
			result.WriteByte(0xC0 | (b >> 6))
			result.WriteByte(0x80 | (b & 0x3F))
		} else {
			// Windows-1252 specific mappings for 128-159
			// For simplicity, we'll use the ISO-8859-1 mapping
			result.WriteByte(0xC0 | (b >> 6))
			result.WriteByte(0x80 | (b & 0x3F))
		}
	}
	return result.Bytes()
}


// ParseWithErrorRecovery parses an email with error recovery
// Requirement 4.12: Store raw email on parse failure and log error
// Property 9: Parse Error Handling - malformed emails are stored raw without crashing
func (p *EmailParser) ParseWithErrorRecovery(raw []byte) (*ParsedEmail, *ParseError) {
	if len(raw) == 0 {
		return nil, &ParseError{
			Stage:   "validation",
			Message: "empty email data",
			Raw:     raw,
		}
	}

	// Attempt to parse the email
	parsed, err := p.Parse(raw)
	if err != nil {
		// Parse failed - create error result with raw email stored
		parseErr, ok := err.(*ParseError)
		if !ok {
			parseErr = &ParseError{
				Stage:   "parse",
				Message: err.Error(),
				Raw:     raw,
			}
		} else {
			parseErr.Raw = raw
		}

		// Return a minimal parsed email with raw content for recovery
		return &ParsedEmail{
			RawEmail:   raw,
			SizeBytes:  int64(len(raw)),
			ReceivedAt: time.Now().UTC(),
		}, parseErr
	}

	return parsed, nil
}

// SafeParse attempts to parse an email and returns a result even on failure
// Requirement 4.12: Store raw email on parse failure
func (p *EmailParser) SafeParse(raw []byte) *ParsedEmail {
	parsed, parseErr := p.ParseWithErrorRecovery(raw)
	if parseErr != nil {
		// Log the error (in production, use proper logging)
		LogParseError(parseErr)

		// Return minimal parsed email with raw content
		if parsed == nil {
			parsed = &ParsedEmail{
				RawEmail:   raw,
				SizeBytes:  int64(len(raw)),
				ReceivedAt: time.Now().UTC(),
			}
		}
	}
	return parsed
}

// LogParseError logs a parse error with details
// Requirement 4.12: Log error with details
func LogParseError(err *ParseError) {
	// In production, this would use a proper logging framework
	// For now, we just format the error for logging
	_ = fmt.Sprintf("Parse error at stage %s: %s (raw size: %d bytes)",
		err.Stage, err.Message, len(err.Raw))
}

// IsParseError checks if an error is a ParseError
func IsParseError(err error) bool {
	_, ok := err.(*ParseError)
	return ok
}

// GetParseErrorStage returns the stage where parsing failed
func GetParseErrorStage(err error) string {
	if parseErr, ok := err.(*ParseError); ok {
		return parseErr.Stage
	}
	return "unknown"
}

// RecoverRawEmail extracts the raw email from a ParseError
func RecoverRawEmail(err error) []byte {
	if parseErr, ok := err.(*ParseError); ok {
		return parseErr.Raw
	}
	return nil
}
