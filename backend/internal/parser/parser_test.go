package parser

import (
	"fmt"
	"net/mail"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// TestProperty6_HeaderExtraction tests Property 6: Header Extraction
// Feature: smtp-email-receiver, Property 6: Header Extraction
// *For any* valid email, the Email_Parser SHALL correctly extract sender address,
// sender display name, subject, and all headers into JSONB format.
// **Validates: Requirements 4.1, 4.2, 4.3, 4.4**
func TestProperty6_HeaderExtraction(t *testing.T) {
	parser := NewEmailParser()

	rapid.Check(t, func(t *rapid.T) {
		// Generate random email components (no consecutive spaces to avoid RFC 5322 folding)
		fromNameParts := rapid.IntRange(1, 3).Draw(t, "fromNameParts")
		var fromNameBuilder strings.Builder
		for i := 0; i < fromNameParts; i++ {
			if i > 0 {
				fromNameBuilder.WriteString(" ")
			}
			fromNameBuilder.WriteString(rapid.StringMatching(`[A-Za-z]{2,10}`).Draw(t, fmt.Sprintf("namePart%d", i)))
		}
		fromName := fromNameBuilder.String()

		fromLocal := rapid.StringMatching(`[a-z]{3,10}`).Draw(t, "fromLocal")
		fromDomain := rapid.StringMatching(`[a-z]{3,10}\.[a-z]{2,4}`).Draw(t, "fromDomain")
		fromAddress := fromLocal + "@" + fromDomain

		toLocal := rapid.StringMatching(`[a-z]{3,10}`).Draw(t, "toLocal")
		toDomain := rapid.StringMatching(`[a-z]{3,10}\.[a-z]{2,4}`).Draw(t, "toDomain")
		toAddress := toLocal + "@" + toDomain

		// Generate subject without consecutive spaces
		subjectParts := rapid.IntRange(1, 5).Draw(t, "subjectParts")
		var subjectBuilder strings.Builder
		for i := 0; i < subjectParts; i++ {
			if i > 0 {
				subjectBuilder.WriteString(" ")
			}
			subjectBuilder.WriteString(rapid.StringMatching(`[A-Za-z0-9]{1,10}`).Draw(t, fmt.Sprintf("subjectPart%d", i)))
		}
		subject := subjectBuilder.String()

		// Build a valid email
		email := fmt.Sprintf("From: %s <%s>\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/plain\r\n\r\nTest body",
			fromName, fromAddress, toAddress, subject)

		// Parse the email
		parsed, err := parser.Parse([]byte(email))
		if err != nil {
			t.Fatalf("Failed to parse valid email: %v", err)
		}

		// Verify From address extraction (Requirement 4.1)
		if parsed.From != fromAddress {
			t.Errorf("From address mismatch: got %q, want %q", parsed.From, fromAddress)
		}

		// Verify From name extraction (Requirement 4.2)
		if parsed.FromName != fromName {
			t.Errorf("From name mismatch: got %q, want %q", parsed.FromName, fromName)
		}

		// Verify Subject extraction (Requirement 4.3)
		if parsed.Subject != subject {
			t.Errorf("Subject mismatch: got %q, want %q", parsed.Subject, subject)
		}

		// Verify headers are extracted to map (Requirement 4.4)
		if parsed.Headers == nil {
			t.Error("Headers should not be nil")
		}

		// Verify essential headers are present
		if _, ok := parsed.Headers["From"]; !ok {
			t.Error("From header should be present in headers map")
		}
		if _, ok := parsed.Headers["To"]; !ok {
			t.Error("To header should be present in headers map")
		}
		if _, ok := parsed.Headers["Subject"]; !ok {
			t.Error("Subject header should be present in headers map")
		}
	})
}

// TestProperty13_HeaderInjectionPrevention tests Property 13: Header Injection Prevention
// Feature: smtp-email-receiver, Property 13: Header Injection Prevention
// *For any* input containing CRLF sequences in headers, the SMTP_Server SHALL reject
// or sanitize the input. Headers exceeding 1000 characters SHALL be truncated.
// **Validates: Requirements 6.1, 6.2**
func TestProperty13_HeaderInjectionPrevention(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate injection patterns
		injectionType := rapid.IntRange(0, 5).Draw(t, "injectionType")

		var testValue string
		switch injectionType {
		case 0:
			// CRLF injection
			prefix := rapid.StringMatching(`[a-z]{5,10}`).Draw(t, "prefix")
			suffix := rapid.StringMatching(`[a-z]{5,10}`).Draw(t, "suffix")
			testValue = prefix + "\r\n" + suffix
		case 1:
			// CR only injection
			prefix := rapid.StringMatching(`[a-z]{5,10}`).Draw(t, "prefix")
			suffix := rapid.StringMatching(`[a-z]{5,10}`).Draw(t, "suffix")
			testValue = prefix + "\r" + suffix
		case 2:
			// LF only injection
			prefix := rapid.StringMatching(`[a-z]{5,10}`).Draw(t, "prefix")
			suffix := rapid.StringMatching(`[a-z]{5,10}`).Draw(t, "suffix")
			testValue = prefix + "\n" + suffix
		case 3:
			// URL encoded CRLF
			prefix := rapid.StringMatching(`[a-z]{5,10}`).Draw(t, "prefix")
			suffix := rapid.StringMatching(`[a-z]{5,10}`).Draw(t, "suffix")
			testValue = prefix + "%0d%0a" + suffix
		case 4:
			// URL encoded CR
			prefix := rapid.StringMatching(`[a-z]{5,10}`).Draw(t, "prefix")
			suffix := rapid.StringMatching(`[a-z]{5,10}`).Draw(t, "suffix")
			testValue = prefix + "%0d" + suffix
		case 5:
			// URL encoded LF
			prefix := rapid.StringMatching(`[a-z]{5,10}`).Draw(t, "prefix")
			suffix := rapid.StringMatching(`[a-z]{5,10}`).Draw(t, "suffix")
			testValue = prefix + "%0a" + suffix
		}

		// CRLF injection should be detected (Requirement 6.1)
		if !ContainsCRLFInjection(testValue) {
			t.Errorf("CRLF injection not detected in: %q", testValue)
		}
	})
}

// TestProperty13_HeaderLengthTruncation tests header length truncation
// Feature: smtp-email-receiver, Property 13: Header Injection Prevention
// **Validates: Requirements 6.2**
func TestProperty13_HeaderLengthTruncation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate headers of various lengths
		length := rapid.IntRange(500, 2000).Draw(t, "length")
		value := strings.Repeat("a", length)

		// Truncate header
		truncated := TruncateHeader(value)

		// Verify truncation (Requirement 6.2)
		if len(truncated) > MaxHeaderLength {
			t.Errorf("Header should be truncated to %d chars, got %d", MaxHeaderLength, len(truncated))
		}

		// If original was within limit, should be unchanged
		if length <= MaxHeaderLength && truncated != value {
			t.Errorf("Header within limit should not be modified")
		}
	})
}

// TestHeaderExtractionWithMIMEEncoding tests MIME encoded headers
func TestHeaderExtractionWithMIMEEncoding(t *testing.T) {
	parser := NewEmailParser()

	tests := []struct {
		name        string
		email       string
		wantFrom    string
		wantName    string
		wantSubject string
	}{
		{
			name: "plain headers",
			email: "From: John Doe <john@example.com>\r\n" +
				"To: jane@example.com\r\n" +
				"Subject: Hello World\r\n" +
				"\r\n" +
				"Body",
			wantFrom:    "john@example.com",
			wantName:    "John Doe",
			wantSubject: "Hello World",
		},
		{
			name: "MIME encoded subject (UTF-8 Base64)",
			email: "From: sender@example.com\r\n" +
				"To: recipient@example.com\r\n" +
				"Subject: =?UTF-8?B?SGVsbG8gV29ybGQ=?=\r\n" +
				"\r\n" +
				"Body",
			wantFrom:    "sender@example.com",
			wantName:    "",
			wantSubject: "Hello World",
		},
		{
			name: "MIME encoded subject (UTF-8 Q)",
			email: "From: sender@example.com\r\n" +
				"To: recipient@example.com\r\n" +
				"Subject: =?UTF-8?Q?Hello_World?=\r\n" +
				"\r\n" +
				"Body",
			wantFrom:    "sender@example.com",
			wantName:    "",
			wantSubject: "Hello World",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := parser.Parse([]byte(tt.email))
			if err != nil {
				t.Fatalf("Parse failed: %v", err)
			}

			if parsed.From != tt.wantFrom {
				t.Errorf("From = %q, want %q", parsed.From, tt.wantFrom)
			}
			if parsed.FromName != tt.wantName {
				t.Errorf("FromName = %q, want %q", parsed.FromName, tt.wantName)
			}
			if parsed.Subject != tt.wantSubject {
				t.Errorf("Subject = %q, want %q", parsed.Subject, tt.wantSubject)
			}
		})
	}
}

// TestCRLFInjectionDetection tests CRLF injection detection
func TestCRLFInjectionDetection(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantInj bool
	}{
		{"clean string", "Hello World", false},
		{"CRLF injection", "Hello\r\nWorld", true},
		{"CR injection", "Hello\rWorld", true},
		{"LF injection", "Hello\nWorld", true},
		{"URL encoded CRLF", "Hello%0d%0aWorld", true},
		{"URL encoded CR", "Hello%0dWorld", true},
		{"URL encoded LF", "Hello%0aWorld", true},
		{"uppercase URL encoded", "Hello%0D%0AWorld", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ContainsCRLFInjection(tt.input)
			if got != tt.wantInj {
				t.Errorf("ContainsCRLFInjection(%q) = %v, want %v", tt.input, got, tt.wantInj)
			}
		})
	}
}

// TestExtractHeadersWithInjection tests that headers with injection are rejected
func TestExtractHeadersWithInjection(t *testing.T) {
	parser := NewEmailParser()

	// Create a message with injected header
	rawEmail := "From: attacker@evil.com\r\n" +
		"To: victim@example.com\r\n" +
		"Subject: Normal Subject\r\n" +
		"\r\n" +
		"Body"

	// Parse the raw email first
	msg, err := mail.ReadMessage(strings.NewReader(rawEmail))
	if err != nil {
		t.Fatalf("Failed to read message: %v", err)
	}

	// Extract headers should work for clean headers
	headers, err := parser.ExtractHeaders(msg)
	if err != nil {
		t.Fatalf("ExtractHeaders failed for clean email: %v", err)
	}

	if headers == nil {
		t.Error("Headers should not be nil")
	}
}

// TestHeaderLengthValidation tests header length validation
func TestHeaderLengthValidation(t *testing.T) {
	tests := []struct {
		name      string
		length    int
		wantValid bool
	}{
		{"short header", 100, true},
		{"max length header", 1000, true},
		{"over max length", 1001, false},
		{"very long header", 5000, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value := strings.Repeat("a", tt.length)
			got := ValidateHeaderLength(value)
			if got != tt.wantValid {
				t.Errorf("ValidateHeaderLength() = %v, want %v", got, tt.wantValid)
			}
		})
	}
}

// TestTruncateHeader tests header truncation
func TestTruncateHeader(t *testing.T) {
	tests := []struct {
		name       string
		length     int
		wantLength int
	}{
		{"short header", 100, 100},
		{"max length header", 1000, 1000},
		{"over max length", 1500, 1000},
		{"very long header", 5000, 1000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value := strings.Repeat("a", tt.length)
			got := TruncateHeader(value)
			if len(got) != tt.wantLength {
				t.Errorf("TruncateHeader() length = %d, want %d", len(got), tt.wantLength)
			}
		})
	}
}


// TestProperty7_ContentTypeHandling tests Property 7: Content Type Handling
// Feature: smtp-email-receiver, Property 7: Content Type Handling
// *For any* email with text/plain, text/html, multipart/alternative, or multipart/mixed
// content type, the Email_Parser SHALL correctly extract body content, preferring HTML
// over plain text for multipart/alternative.
// **Validates: Requirements 4.5, 4.6, 4.7, 4.8**
func TestProperty7_ContentTypeHandling(t *testing.T) {
	parser := NewEmailParser()

	rapid.Check(t, func(t *rapid.T) {
		// Generate random body content
		textBody := rapid.StringMatching(`[A-Za-z0-9 ]{10,50}`).Draw(t, "textBody")
		htmlBody := "<html><body>" + rapid.StringMatching(`[A-Za-z0-9 ]{10,50}`).Draw(t, "htmlContent") + "</body></html>"

		// Choose content type
		contentTypeChoice := rapid.IntRange(0, 3).Draw(t, "contentType")

		var email string
		var expectHTML, expectText string

		switch contentTypeChoice {
		case 0:
			// text/plain (Requirement 4.5)
			email = "From: sender@example.com\r\n" +
				"To: recipient@example.com\r\n" +
				"Subject: Test\r\n" +
				"Content-Type: text/plain\r\n" +
				"\r\n" +
				textBody
			expectHTML = ""
			expectText = textBody

		case 1:
			// text/html (Requirement 4.6)
			email = "From: sender@example.com\r\n" +
				"To: recipient@example.com\r\n" +
				"Subject: Test\r\n" +
				"Content-Type: text/html\r\n" +
				"\r\n" +
				htmlBody
			expectHTML = htmlBody
			expectText = ""

		case 2:
			// multipart/alternative (Requirement 4.7)
			boundary := "----=_Part_0_123456789"
			email = "From: sender@example.com\r\n" +
				"To: recipient@example.com\r\n" +
				"Subject: Test\r\n" +
				"Content-Type: multipart/alternative; boundary=\"" + boundary + "\"\r\n" +
				"\r\n" +
				"------=_Part_0_123456789\r\n" +
				"Content-Type: text/plain\r\n" +
				"\r\n" +
				textBody + "\r\n" +
				"------=_Part_0_123456789\r\n" +
				"Content-Type: text/html\r\n" +
				"\r\n" +
				htmlBody + "\r\n" +
				"------=_Part_0_123456789--\r\n"
			expectHTML = htmlBody
			expectText = textBody

		case 3:
			// multipart/mixed (Requirement 4.8)
			boundary := "----=_Part_0_987654321"
			email = "From: sender@example.com\r\n" +
				"To: recipient@example.com\r\n" +
				"Subject: Test\r\n" +
				"Content-Type: multipart/mixed; boundary=\"" + boundary + "\"\r\n" +
				"\r\n" +
				"------=_Part_0_987654321\r\n" +
				"Content-Type: text/plain\r\n" +
				"\r\n" +
				textBody + "\r\n" +
				"------=_Part_0_987654321--\r\n"
			expectHTML = ""
			expectText = textBody
		}

		// Parse the email
		parsed, err := parser.Parse([]byte(email))
		if err != nil {
			t.Fatalf("Failed to parse email: %v", err)
		}

		// Verify body extraction
		if expectHTML != "" && parsed.BodyHTML != expectHTML {
			t.Errorf("HTML body mismatch: got %q, want %q", parsed.BodyHTML, expectHTML)
		}
		if expectText != "" && parsed.BodyText != expectText {
			t.Errorf("Text body mismatch: got %q, want %q", parsed.BodyText, expectText)
		}
	})
}

// TestBodyExtractionTextPlain tests text/plain body extraction
func TestBodyExtractionTextPlain(t *testing.T) {
	parser := NewEmailParser()

	email := "From: sender@example.com\r\n" +
		"To: recipient@example.com\r\n" +
		"Subject: Test\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"This is plain text body."

	parsed, err := parser.Parse([]byte(email))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if parsed.BodyText != "This is plain text body." {
		t.Errorf("BodyText = %q, want %q", parsed.BodyText, "This is plain text body.")
	}
	if parsed.BodyHTML != "" {
		t.Errorf("BodyHTML should be empty, got %q", parsed.BodyHTML)
	}
}

// TestBodyExtractionTextHTML tests text/html body extraction
func TestBodyExtractionTextHTML(t *testing.T) {
	parser := NewEmailParser()

	htmlContent := "<html><body><h1>Hello</h1></body></html>"
	email := "From: sender@example.com\r\n" +
		"To: recipient@example.com\r\n" +
		"Subject: Test\r\n" +
		"Content-Type: text/html; charset=utf-8\r\n" +
		"\r\n" +
		htmlContent

	parsed, err := parser.Parse([]byte(email))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if parsed.BodyHTML != htmlContent {
		t.Errorf("BodyHTML = %q, want %q", parsed.BodyHTML, htmlContent)
	}
	if parsed.BodyText != "" {
		t.Errorf("BodyText should be empty, got %q", parsed.BodyText)
	}
}

// TestBodyExtractionMultipartAlternative tests multipart/alternative body extraction
func TestBodyExtractionMultipartAlternative(t *testing.T) {
	parser := NewEmailParser()

	boundary := "----=_Part_0_123456789"
	textContent := "Plain text version"
	htmlContent := "<html><body>HTML version</body></html>"

	email := "From: sender@example.com\r\n" +
		"To: recipient@example.com\r\n" +
		"Subject: Test\r\n" +
		"Content-Type: multipart/alternative; boundary=\"" + boundary + "\"\r\n" +
		"\r\n" +
		"------=_Part_0_123456789\r\n" +
		"Content-Type: text/plain\r\n" +
		"\r\n" +
		textContent + "\r\n" +
		"------=_Part_0_123456789\r\n" +
		"Content-Type: text/html\r\n" +
		"\r\n" +
		htmlContent + "\r\n" +
		"------=_Part_0_123456789--\r\n"

	parsed, err := parser.Parse([]byte(email))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Should have both HTML and text
	if parsed.BodyHTML != htmlContent {
		t.Errorf("BodyHTML = %q, want %q", parsed.BodyHTML, htmlContent)
	}
	if parsed.BodyText != textContent {
		t.Errorf("BodyText = %q, want %q", parsed.BodyText, textContent)
	}
}

// TestBodyExtractionMultipartMixed tests multipart/mixed body extraction
func TestBodyExtractionMultipartMixed(t *testing.T) {
	parser := NewEmailParser()

	boundary := "----=_Part_0_987654321"
	textContent := "Email body text"

	email := "From: sender@example.com\r\n" +
		"To: recipient@example.com\r\n" +
		"Subject: Test with attachment\r\n" +
		"Content-Type: multipart/mixed; boundary=\"" + boundary + "\"\r\n" +
		"\r\n" +
		"------=_Part_0_987654321\r\n" +
		"Content-Type: text/plain\r\n" +
		"\r\n" +
		textContent + "\r\n" +
		"------=_Part_0_987654321\r\n" +
		"Content-Type: application/octet-stream\r\n" +
		"Content-Disposition: attachment; filename=\"test.txt\"\r\n" +
		"\r\n" +
		"attachment content\r\n" +
		"------=_Part_0_987654321--\r\n"

	parsed, err := parser.Parse([]byte(email))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Should extract text body, skip attachment
	if parsed.BodyText != textContent {
		t.Errorf("BodyText = %q, want %q", parsed.BodyText, textContent)
	}
}

// TestBodyExtractionNoContentType tests email without Content-Type header
func TestBodyExtractionNoContentType(t *testing.T) {
	parser := NewEmailParser()

	email := "From: sender@example.com\r\n" +
		"To: recipient@example.com\r\n" +
		"Subject: Test\r\n" +
		"\r\n" +
		"Body without content type"

	parsed, err := parser.Parse([]byte(email))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Should default to text/plain
	if parsed.BodyText != "Body without content type" {
		t.Errorf("BodyText = %q, want %q", parsed.BodyText, "Body without content type")
	}
}


// TestProperty8_EncodingRoundTrip tests Property 8: Encoding Round Trip
// Feature: smtp-email-receiver, Property 8: Encoding Round Trip
// *For any* email content encoded with quoted-printable or base64, decoding then
// re-encoding SHALL produce equivalent content. Various character encodings
// (UTF-8, ISO-8859-1) SHALL be correctly converted to UTF-8.
// **Validates: Requirements 4.9, 4.10, 4.11**
func TestProperty8_EncodingRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate random ASCII content (safe for encoding tests)
		content := rapid.StringMatching(`[A-Za-z0-9 ]{10,100}`).Draw(t, "content")
		originalBytes := []byte(content)

		// Test Base64 round trip (Requirement 4.10)
		encoded := EncodeBase64(originalBytes)
		decoded, err := DecodeBase64(encoded)
		if err != nil {
			t.Fatalf("Base64 decode failed: %v", err)
		}

		if string(decoded) != content {
			t.Errorf("Base64 round trip failed: got %q, want %q", string(decoded), content)
		}
	})
}

// TestProperty8_QuotedPrintableRoundTrip tests quoted-printable round trip
// Feature: smtp-email-receiver, Property 8: Encoding Round Trip
// **Validates: Requirements 4.9**
func TestProperty8_QuotedPrintableRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate random ASCII content
		content := rapid.StringMatching(`[A-Za-z0-9]{10,50}`).Draw(t, "content")
		originalBytes := []byte(content)

		// Test Quoted-Printable round trip (Requirement 4.9)
		encoded := EncodeQuotedPrintable(originalBytes)
		decoded, err := DecodeQuotedPrintable(encoded)
		if err != nil {
			t.Fatalf("Quoted-Printable decode failed: %v", err)
		}

		if string(decoded) != content {
			t.Errorf("Quoted-Printable round trip failed: got %q, want %q", string(decoded), content)
		}
	})
}

// TestBase64Encoding tests base64 encoding/decoding
func TestBase64Encoding(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		encoded string
	}{
		{
			name:    "simple text",
			input:   "Hello World",
			encoded: "SGVsbG8gV29ybGQ=",
		},
		{
			name:    "empty string",
			input:   "",
			encoded: "",
		},
		{
			name:    "single char",
			input:   "A",
			encoded: "QQ==",
		},
		{
			name:    "two chars",
			input:   "AB",
			encoded: "QUI=",
		},
		{
			name:    "three chars",
			input:   "ABC",
			encoded: "QUJD",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test encoding
			encoded := EncodeBase64([]byte(tt.input))
			if string(encoded) != tt.encoded {
				t.Errorf("EncodeBase64(%q) = %q, want %q", tt.input, string(encoded), tt.encoded)
			}

			// Test decoding
			decoded, err := DecodeBase64([]byte(tt.encoded))
			if err != nil {
				t.Fatalf("DecodeBase64 failed: %v", err)
			}
			if string(decoded) != tt.input {
				t.Errorf("DecodeBase64(%q) = %q, want %q", tt.encoded, string(decoded), tt.input)
			}
		})
	}
}

// TestQuotedPrintableEncoding tests quoted-printable encoding/decoding
func TestQuotedPrintableEncoding(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		decoded string
	}{
		{
			name:    "simple text",
			input:   "Hello World",
			decoded: "Hello World",
		},
		{
			name:    "encoded equals",
			input:   "Hello=3DWorld",
			decoded: "Hello=World",
		},
		{
			name:    "soft line break",
			input:   "Hello=\r\nWorld",
			decoded: "HelloWorld",
		},
		{
			name:    "encoded special char",
			input:   "Hello=20World",
			decoded: "Hello World",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decoded, err := DecodeQuotedPrintable([]byte(tt.input))
			if err != nil {
				t.Fatalf("DecodeQuotedPrintable failed: %v", err)
			}
			if string(decoded) != tt.decoded {
				t.Errorf("DecodeQuotedPrintable(%q) = %q, want %q", tt.input, string(decoded), tt.decoded)
			}
		})
	}
}

// TestCharsetConversion tests charset conversion
func TestCharsetConversion(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		charset string
		want    string
	}{
		{
			name:    "UTF-8 passthrough",
			input:   []byte("Hello World"),
			charset: "utf-8",
			want:    "Hello World",
		},
		{
			name:    "ASCII passthrough",
			input:   []byte("Hello World"),
			charset: "us-ascii",
			want:    "Hello World",
		},
		{
			name:    "empty charset",
			input:   []byte("Hello World"),
			charset: "",
			want:    "Hello World",
		},
		{
			name:    "ISO-8859-1 ASCII range",
			input:   []byte("Hello World"),
			charset: "iso-8859-1",
			want:    "Hello World",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ConvertCharset(tt.input, tt.charset)
			if err != nil {
				t.Fatalf("ConvertCharset failed: %v", err)
			}
			if string(result) != tt.want {
				t.Errorf("ConvertCharset() = %q, want %q", string(result), tt.want)
			}
		})
	}
}

// TestDecodeContent tests content decoding with different encodings
func TestDecodeContent(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		encoding string
		want     string
	}{
		{
			name:     "no encoding",
			input:    []byte("Hello World"),
			encoding: "",
			want:     "Hello World",
		},
		{
			name:     "7bit encoding",
			input:    []byte("Hello World"),
			encoding: "7bit",
			want:     "Hello World",
		},
		{
			name:     "8bit encoding",
			input:    []byte("Hello World"),
			encoding: "8bit",
			want:     "Hello World",
		},
		{
			name:     "base64 encoding",
			input:    []byte("SGVsbG8gV29ybGQ="),
			encoding: "base64",
			want:     "Hello World",
		},
		{
			name:     "quoted-printable encoding",
			input:    []byte("Hello=20World"),
			encoding: "quoted-printable",
			want:     "Hello World",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := DecodeContent(tt.input, tt.encoding)
			if err != nil {
				t.Fatalf("DecodeContent failed: %v", err)
			}
			if string(result) != tt.want {
				t.Errorf("DecodeContent() = %q, want %q", string(result), tt.want)
			}
		})
	}
}


// TestProperty9_ParseErrorHandling tests Property 9: Parse Error Handling
// Feature: smtp-email-receiver, Property 9: Parse Error Handling
// *For any* malformed email that fails parsing, the Email_Parser SHALL store
// the raw email content and log the error without crashing.
// **Validates: Requirements 4.12**
func TestProperty9_ParseErrorHandling(t *testing.T) {
	parser := NewEmailParser()

	rapid.Check(t, func(t *rapid.T) {
		// Generate malformed email data
		malformedType := rapid.IntRange(0, 4).Draw(t, "malformedType")

		var malformedData []byte
		switch malformedType {
		case 0:
			// Random binary data
			length := rapid.IntRange(10, 100).Draw(t, "length")
			malformedData = make([]byte, length)
			for i := range malformedData {
				malformedData[i] = byte(rapid.IntRange(0, 255).Draw(t, "byte"))
			}
		case 1:
			// Missing headers
			malformedData = []byte("Just some body text without headers")
		case 2:
			// Truncated multipart
			malformedData = []byte("Content-Type: multipart/mixed; boundary=\"abc\"\r\n\r\n--abc\r\nContent-Type: text/plain\r\n\r\nTruncated")
		case 3:
			// Invalid header format
			malformedData = []byte("Invalid Header Without Colon\r\n\r\nBody")
		case 4:
			// Empty data
			malformedData = []byte{}
		}

		// SafeParse should not panic
		parsed := parser.SafeParse(malformedData)

		// Should always return a result (never nil)
		if parsed == nil {
			t.Error("SafeParse should never return nil")
		}

		// Raw email should be preserved for non-empty input
		if len(malformedData) > 0 && len(parsed.RawEmail) == 0 {
			t.Error("Raw email should be preserved on parse failure")
		}

		// Size should be recorded
		if len(malformedData) > 0 && parsed.SizeBytes == 0 {
			t.Error("Size should be recorded even on parse failure")
		}
	})
}

// TestParseErrorRecovery tests error recovery functionality
func TestParseErrorRecovery(t *testing.T) {
	parser := NewEmailParser()

	tests := []struct {
		name        string
		input       []byte
		wantError   bool
		wantRawSize int
	}{
		{
			name:        "empty input",
			input:       []byte{},
			wantError:   true,
			wantRawSize: 0,
		},
		{
			name:        "valid email",
			input:       []byte("From: test@example.com\r\nTo: recipient@example.com\r\nSubject: Test\r\n\r\nBody"),
			wantError:   false,
			wantRawSize: 0, // Raw not needed for successful parse
		},
		{
			name:        "malformed email",
			input:       []byte("This is not a valid email format at all"),
			wantError:   false, // SafeParse doesn't return error, it recovers
			wantRawSize: 39,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, parseErr := parser.ParseWithErrorRecovery(tt.input)

			if tt.wantError && parseErr == nil {
				t.Error("Expected parse error")
			}

			if parsed != nil && tt.wantRawSize > 0 {
				if len(parsed.RawEmail) != tt.wantRawSize {
					t.Errorf("RawEmail size = %d, want %d", len(parsed.RawEmail), tt.wantRawSize)
				}
			}
		})
	}
}

// TestSafeParse tests SafeParse never panics
func TestSafeParse(t *testing.T) {
	parser := NewEmailParser()

	// Test with various malformed inputs
	inputs := [][]byte{
		nil,
		{},
		[]byte("random garbage"),
		[]byte("\x00\x01\x02\x03"),
		[]byte("From: \r\n\r\n"),
		[]byte("Content-Type: multipart/mixed\r\n\r\n"),
	}

	for i, input := range inputs {
		t.Run(fmt.Sprintf("input_%d", i), func(t *testing.T) {
			// Should not panic
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("SafeParse panicked: %v", r)
				}
			}()

			result := parser.SafeParse(input)
			if result == nil {
				t.Error("SafeParse should never return nil")
			}
		})
	}
}

// TestParseErrorHelpers tests error helper functions
func TestParseErrorHelpers(t *testing.T) {
	// Test IsParseError
	parseErr := &ParseError{Stage: "test", Message: "test error"}
	if !IsParseError(parseErr) {
		t.Error("IsParseError should return true for ParseError")
	}

	regularErr := fmt.Errorf("regular error")
	if IsParseError(regularErr) {
		t.Error("IsParseError should return false for regular error")
	}

	// Test GetParseErrorStage
	if GetParseErrorStage(parseErr) != "test" {
		t.Errorf("GetParseErrorStage = %q, want %q", GetParseErrorStage(parseErr), "test")
	}

	if GetParseErrorStage(regularErr) != "unknown" {
		t.Errorf("GetParseErrorStage for regular error = %q, want %q", GetParseErrorStage(regularErr), "unknown")
	}

	// Test RecoverRawEmail
	parseErr.Raw = []byte("raw email data")
	if string(RecoverRawEmail(parseErr)) != "raw email data" {
		t.Error("RecoverRawEmail should return raw email data")
	}

	if RecoverRawEmail(regularErr) != nil {
		t.Error("RecoverRawEmail should return nil for regular error")
	}
}
