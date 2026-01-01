package attachment

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/welldanyogia/persistent-temp-mail/backend/internal/parser"
	"pgregory.net/rapid"
)

// TestProperty10_AttachmentStorage tests Property 10: Attachment Storage
// Feature: smtp-email-receiver, Property 10: Attachment Storage
// *For any* email with attachments, the Attachment_Handler SHALL extract each attachment,
// store it in S3 with unique key, calculate SHA-256 checksum, and record metadata
// (filename, content_type, size_bytes) in database.
// **Validates: Requirements 5.1, 5.2, 5.3, 5.4, 5.5**
func TestProperty10_AttachmentStorage(t *testing.T) {
	handler := NewHandler(nil, "test-bucket")

	rapid.Check(t, func(t *rapid.T) {
		// Generate random email ID
		emailID := rapid.StringMatching(`[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`).Draw(t, "emailID")

		// Generate random filename (safe characters only)
		filename := rapid.StringMatching(`[a-zA-Z0-9_-]{1,20}\.[a-z]{2,4}`).Draw(t, "filename")

		// Generate random attachment data
		dataLen := rapid.IntRange(10, 1000).Draw(t, "dataLen")
		data := make([]byte, dataLen)
		for i := range data {
			data[i] = byte(rapid.IntRange(0, 255).Draw(t, fmt.Sprintf("byte%d", i)))
		}

		// Test storage key generation (Requirement 5.3)
		storageKey := handler.GenerateStorageKey(emailID, filename)

		// Storage key should contain email ID
		if !strings.Contains(storageKey, emailID) {
			t.Errorf("Storage key should contain email ID: got %q", storageKey)
		}

		// Storage key should be unique (contains UUID)
		storageKey2 := handler.GenerateStorageKey(emailID, filename)
		if storageKey == storageKey2 {
			t.Error("Storage keys should be unique")
		}

		// Test checksum calculation (Requirement 5.4)
		checksum := handler.CalculateChecksum(data)

		// Checksum should be valid SHA-256 hex string (64 chars)
		if len(checksum) != 64 {
			t.Errorf("Checksum should be 64 chars, got %d", len(checksum))
		}

		// Verify checksum is correct
		expectedHash := sha256.Sum256(data)
		expectedChecksum := hex.EncodeToString(expectedHash[:])
		if checksum != expectedChecksum {
			t.Errorf("Checksum mismatch: got %q, want %q", checksum, expectedChecksum)
		}

		// Same data should produce same checksum
		checksum2 := handler.CalculateChecksum(data)
		if checksum != checksum2 {
			t.Error("Same data should produce same checksum")
		}
	})
}

// TestProperty10_AttachmentExtraction tests attachment extraction from multipart emails
// Feature: smtp-email-receiver, Property 10: Attachment Storage
// **Validates: Requirements 5.1**
func TestProperty10_AttachmentExtraction(t *testing.T) {
	handler := NewHandler(nil, "test-bucket")

	rapid.Check(t, func(t *rapid.T) {
		// Generate random attachment properties
		filename := rapid.StringMatching(`[a-zA-Z]{1,10}\.[a-z]{2,4}`).Draw(t, "filename")
		contentType := rapid.SampledFrom([]string{
			"application/pdf",
			"image/png",
			"image/jpeg",
			"text/plain",
			"application/octet-stream",
		}).Draw(t, "contentType")

		// Generate random attachment content (alphanumeric only for simplicity)
		content := rapid.StringMatching(`[A-Za-z0-9]{10,50}`).Draw(t, "content")

		// Build multipart email with attachment using proper MIME format
		boundary := "----=_Part_0_123456789"
		email := fmt.Sprintf(
			"From: sender@example.com\r\n"+
				"To: recipient@example.com\r\n"+
				"Subject: Test with attachment\r\n"+
				"MIME-Version: 1.0\r\n"+
				"Content-Type: multipart/mixed; boundary=\"%s\"\r\n"+
				"\r\n"+
				"--%s\r\n"+
				"Content-Type: text/plain; charset=utf-8\r\n"+
				"\r\n"+
				"Email body\r\n"+
				"--%s\r\n"+
				"Content-Type: %s\r\n"+
				"Content-Disposition: attachment; filename=\"%s\"\r\n"+
				"Content-Transfer-Encoding: 7bit\r\n"+
				"\r\n"+
				"%s\r\n"+
				"--%s--\r\n",
			boundary, boundary, boundary, contentType, filename, content, boundary)

		// Extract attachments
		attachments, err := handler.ExtractAttachments([]byte(email))
		if err != nil {
			t.Fatalf("Failed to extract attachments: %v", err)
		}

		// Should have exactly one attachment
		if len(attachments) != 1 {
			t.Errorf("Expected 1 attachment, got %d", len(attachments))
			return
		}

		// Verify attachment properties
		att := attachments[0]
		if att.Filename != filename {
			t.Errorf("Filename mismatch: got %q, want %q", att.Filename, filename)
		}
		if att.ContentType != contentType {
			t.Errorf("ContentType mismatch: got %q, want %q", att.ContentType, contentType)
		}
		if att.SizeBytes != int64(len(att.Data)) {
			t.Errorf("SizeBytes mismatch: got %d, want %d", att.SizeBytes, len(att.Data))
		}
	})
}

// TestChecksumCalculation tests SHA-256 checksum calculation
func TestChecksumCalculation(t *testing.T) {
	handler := NewHandler(nil, "test-bucket")

	tests := []struct {
		name     string
		data     []byte
		expected string
	}{
		{
			name:     "empty data",
			data:     []byte{},
			expected: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			name:     "hello world",
			data:     []byte("hello world"),
			expected: "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9",
		},
		{
			name:     "binary data",
			data:     []byte{0x00, 0x01, 0x02, 0x03},
			expected: "054edec1d0211f624fed0cbca9d4f9400b0e491c43742af2c5b0abebf0c990d8",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checksum := handler.CalculateChecksum(tt.data)
			if checksum != tt.expected {
				t.Errorf("CalculateChecksum() = %q, want %q", checksum, tt.expected)
			}
		})
	}
}

// TestStorageKeyGeneration tests unique storage key generation
func TestStorageKeyGeneration(t *testing.T) {
	handler := NewHandler(nil, "test-bucket")

	emailID := "test-email-123"
	filename := "document.pdf"

	// Generate multiple keys
	keys := make(map[string]bool)
	for i := 0; i < 100; i++ {
		key := handler.GenerateStorageKey(emailID, filename)

		// Key should be unique
		if keys[key] {
			t.Errorf("Duplicate storage key generated: %s", key)
		}
		keys[key] = true

		// Key should contain email ID
		if !strings.Contains(key, emailID) {
			t.Errorf("Storage key should contain email ID: %s", key)
		}

		// Key should have proper format
		if !strings.HasPrefix(key, "attachments/") {
			t.Errorf("Storage key should start with 'attachments/': %s", key)
		}
	}
}

// TestAttachmentExtractionFromMultipart tests extraction from various multipart formats
func TestAttachmentExtractionFromMultipart(t *testing.T) {
	handler := NewHandler(nil, "test-bucket")

	tests := []struct {
		name           string
		email          string
		expectedCount  int
		expectedNames  []string
	}{
		{
			name: "single attachment",
			email: "From: sender@example.com\r\n" +
				"To: recipient@example.com\r\n" +
				"Subject: Test\r\n" +
				"Content-Type: multipart/mixed; boundary=\"boundary123\"\r\n" +
				"\r\n" +
				"--boundary123\r\n" +
				"Content-Type: text/plain\r\n" +
				"\r\n" +
				"Body\r\n" +
				"--boundary123\r\n" +
				"Content-Type: application/pdf\r\n" +
				"Content-Disposition: attachment; filename=\"test.pdf\"\r\n" +
				"\r\n" +
				"PDF content\r\n" +
				"--boundary123--\r\n",
			expectedCount: 1,
			expectedNames: []string{"test.pdf"},
		},
		{
			name: "multiple attachments",
			email: "From: sender@example.com\r\n" +
				"To: recipient@example.com\r\n" +
				"Subject: Test\r\n" +
				"Content-Type: multipart/mixed; boundary=\"boundary456\"\r\n" +
				"\r\n" +
				"--boundary456\r\n" +
				"Content-Type: text/plain\r\n" +
				"\r\n" +
				"Body\r\n" +
				"--boundary456\r\n" +
				"Content-Type: application/pdf\r\n" +
				"Content-Disposition: attachment; filename=\"doc1.pdf\"\r\n" +
				"\r\n" +
				"PDF1\r\n" +
				"--boundary456\r\n" +
				"Content-Type: image/png\r\n" +
				"Content-Disposition: attachment; filename=\"image.png\"\r\n" +
				"\r\n" +
				"PNG data\r\n" +
				"--boundary456--\r\n",
			expectedCount: 2,
			expectedNames: []string{"doc1.pdf", "image.png"},
		},
		{
			name: "no attachments",
			email: "From: sender@example.com\r\n" +
				"To: recipient@example.com\r\n" +
				"Subject: Test\r\n" +
				"Content-Type: text/plain\r\n" +
				"\r\n" +
				"Just plain text",
			expectedCount: 0,
			expectedNames: nil,
		},
		{
			name: "inline attachment with filename",
			email: "From: sender@example.com\r\n" +
				"To: recipient@example.com\r\n" +
				"Subject: Test\r\n" +
				"Content-Type: multipart/mixed; boundary=\"boundary789\"\r\n" +
				"\r\n" +
				"--boundary789\r\n" +
				"Content-Type: text/plain\r\n" +
				"\r\n" +
				"Body\r\n" +
				"--boundary789\r\n" +
				"Content-Type: image/png\r\n" +
				"Content-Disposition: inline; filename=\"inline.png\"\r\n" +
				"\r\n" +
				"PNG data\r\n" +
				"--boundary789--\r\n",
			expectedCount: 1,
			expectedNames: []string{"inline.png"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attachments, err := handler.ExtractAttachments([]byte(tt.email))
			if err != nil {
				t.Fatalf("ExtractAttachments failed: %v", err)
			}

			if len(attachments) != tt.expectedCount {
				t.Errorf("Expected %d attachments, got %d", tt.expectedCount, len(attachments))
			}

			for i, name := range tt.expectedNames {
				if i < len(attachments) && attachments[i].Filename != name {
					t.Errorf("Attachment %d filename = %q, want %q", i, attachments[i].Filename, name)
				}
			}
		})
	}
}


// TestProperty11_AttachmentSizeLimits tests Property 11: Attachment Size Limits
// Feature: smtp-email-receiver, Property 11: Attachment Size Limits
// *For any* attachment exceeding 10 MB individually or 25 MB total per email,
// the Attachment_Handler SHALL reject the attachment.
// **Validates: Requirements 5.6, 5.7**
func TestProperty11_AttachmentSizeLimits(t *testing.T) {
	handler := NewHandler(nil, "test-bucket")

	rapid.Check(t, func(t *rapid.T) {
		// Generate attachment size (test both valid and invalid sizes)
		sizeCategory := rapid.IntRange(0, 2).Draw(t, "sizeCategory")

		var size int64
		var shouldReject bool

		switch sizeCategory {
		case 0:
			// Valid size (under 10 MB)
			size = int64(rapid.IntRange(1, MaxAttachmentSize-1).Draw(t, "validSize"))
			shouldReject = false
		case 1:
			// Invalid size (over 10 MB)
			size = int64(MaxAttachmentSize + rapid.IntRange(1, 1000000).Draw(t, "overSize"))
			shouldReject = true
		case 2:
			// Exactly at limit
			size = MaxAttachmentSize
			shouldReject = false
		}

		// Create test attachment
		att := &parser.Attachment{
			Filename:    "test.txt",
			ContentType: "text/plain",
			SizeBytes:   size,
			Data:        make([]byte, 0), // Don't allocate actual data for size test
		}

		// Validate attachment
		err := handler.ValidateAttachment(att)

		if shouldReject && err == nil {
			t.Errorf("Attachment of size %d should be rejected (exceeds %d)", size, MaxAttachmentSize)
		}
		if !shouldReject && err != nil {
			t.Errorf("Attachment of size %d should be accepted: %v", size, err)
		}
	})
}

// TestProperty11_TotalSizeLimits tests total attachment size limits
// Feature: smtp-email-receiver, Property 11: Attachment Size Limits
// **Validates: Requirements 5.7**
func TestProperty11_TotalSizeLimits(t *testing.T) {
	handler := NewHandler(nil, "test-bucket")

	rapid.Check(t, func(t *rapid.T) {
		// Generate number of attachments
		numAttachments := rapid.IntRange(1, 10).Draw(t, "numAttachments")

		// Generate sizes for each attachment
		var attachments []*parser.Attachment
		var totalSize int64

		for i := 0; i < numAttachments; i++ {
			// Each attachment is under individual limit
			size := int64(rapid.IntRange(1, MaxAttachmentSize/2).Draw(t, fmt.Sprintf("size%d", i)))
			totalSize += size

			attachments = append(attachments, &parser.Attachment{
				Filename:    fmt.Sprintf("file%d.txt", i),
				ContentType: "text/plain",
				SizeBytes:   size,
				Data:        make([]byte, 0),
			})
		}

		// Validate total size
		err := handler.ValidateTotalSize(attachments)

		shouldReject := totalSize > MaxTotalAttachmentSize

		if shouldReject && err == nil {
			t.Errorf("Total size %d should be rejected (exceeds %d)", totalSize, MaxTotalAttachmentSize)
		}
		if !shouldReject && err != nil {
			t.Errorf("Total size %d should be accepted: %v", totalSize, err)
		}
	})
}

// TestIndividualAttachmentSizeLimit tests individual attachment size validation
func TestIndividualAttachmentSizeLimit(t *testing.T) {
	handler := NewHandler(nil, "test-bucket")

	tests := []struct {
		name       string
		size       int64
		wantReject bool
	}{
		{
			name:       "small attachment",
			size:       1024, // 1 KB
			wantReject: false,
		},
		{
			name:       "medium attachment",
			size:       5 * 1024 * 1024, // 5 MB
			wantReject: false,
		},
		{
			name:       "at limit",
			size:       MaxAttachmentSize, // 10 MB
			wantReject: false,
		},
		{
			name:       "over limit by 1 byte",
			size:       MaxAttachmentSize + 1,
			wantReject: true,
		},
		{
			name:       "way over limit",
			size:       20 * 1024 * 1024, // 20 MB
			wantReject: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			att := &parser.Attachment{
				Filename:    "test.txt",
				ContentType: "text/plain",
				SizeBytes:   tt.size,
			}

			err := handler.ValidateAttachment(att)

			if tt.wantReject && err == nil {
				t.Error("Expected attachment to be rejected")
			}
			if !tt.wantReject && err != nil {
				t.Errorf("Expected attachment to be accepted: %v", err)
			}
		})
	}
}

// TestTotalAttachmentSizeLimit tests total attachment size validation
func TestTotalAttachmentSizeLimit(t *testing.T) {
	handler := NewHandler(nil, "test-bucket")

	tests := []struct {
		name       string
		sizes      []int64
		wantReject bool
	}{
		{
			name:       "single small attachment",
			sizes:      []int64{1024},
			wantReject: false,
		},
		{
			name:       "multiple small attachments",
			sizes:      []int64{1024, 2048, 3072},
			wantReject: false,
		},
		{
			name:       "at total limit",
			sizes:      []int64{MaxTotalAttachmentSize},
			wantReject: false,
		},
		{
			name:       "over total limit",
			sizes:      []int64{MaxTotalAttachmentSize + 1},
			wantReject: true,
		},
		{
			name:       "multiple attachments over total limit",
			sizes:      []int64{10 * 1024 * 1024, 10 * 1024 * 1024, 10 * 1024 * 1024}, // 30 MB total
			wantReject: true,
		},
		{
			name:       "empty list",
			sizes:      []int64{},
			wantReject: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var attachments []*parser.Attachment
			for i, size := range tt.sizes {
				attachments = append(attachments, &parser.Attachment{
					Filename:    fmt.Sprintf("file%d.txt", i),
					ContentType: "text/plain",
					SizeBytes:   size,
				})
			}

			err := handler.ValidateTotalSize(attachments)

			if tt.wantReject && err == nil {
				t.Error("Expected total size to be rejected")
			}
			if !tt.wantReject && err != nil {
				t.Errorf("Expected total size to be accepted: %v", err)
			}
		})
	}
}


// TestProperty12_AttachmentSecurity tests Property 12: Attachment Security
// Feature: smtp-email-receiver, Property 12: Attachment Security
// *For any* attachment filename, the Attachment_Handler SHALL sanitize path traversal
// characters. *For any* attachment with dangerous extension (.exe, .bat, .cmd, .vbs,
// .js, .jar, .msi), the handler SHALL block and log the event.
// **Validates: Requirements 5.8, 5.9, 5.10**
func TestProperty12_AttachmentSecurity(t *testing.T) {
	handler := NewHandler(nil, "test-bucket")

	rapid.Check(t, func(t *rapid.T) {
		// Test path traversal sanitization (Requirement 5.8)
		traversalType := rapid.IntRange(0, 4).Draw(t, "traversalType")

		var filename string
		switch traversalType {
		case 0:
			// Parent directory traversal
			filename = rapid.StringMatching(`[a-z]{3,10}`).Draw(t, "name") + "/../../../etc/passwd"
		case 1:
			// Windows path traversal
			filename = rapid.StringMatching(`[a-z]{3,10}`).Draw(t, "name") + "\\..\\..\\windows\\system32"
		case 2:
			// Null byte injection
			filename = rapid.StringMatching(`[a-z]{3,10}`).Draw(t, "name") + "\x00.txt"
		case 3:
			// Mixed traversal
			filename = "../" + rapid.StringMatching(`[a-z]{3,10}`).Draw(t, "name") + "/../file.txt"
		case 4:
			// Clean filename (should pass through)
			filename = rapid.StringMatching(`[a-zA-Z0-9_-]{1,20}\.[a-z]{2,4}`).Draw(t, "cleanName")
		}

		sanitized := handler.SanitizeFilename(filename)

		// Sanitized filename should not contain path traversal characters
		for _, char := range PathTraversalChars {
			if strings.Contains(sanitized, char) {
				t.Errorf("Sanitized filename %q still contains path traversal char %q", sanitized, char)
			}
		}
	})
}

// TestProperty12_DangerousExtensions tests dangerous extension blocking
// Feature: smtp-email-receiver, Property 12: Attachment Security
// **Validates: Requirements 5.9**
func TestProperty12_DangerousExtensions(t *testing.T) {
	handler := NewHandler(nil, "test-bucket")

	rapid.Check(t, func(t *rapid.T) {
		// Generate filename with dangerous extension
		baseName := rapid.StringMatching(`[a-zA-Z0-9]{1,10}`).Draw(t, "baseName")

		// Choose between dangerous and safe extension
		isDangerous := rapid.Bool().Draw(t, "isDangerous")

		var ext string
		if isDangerous {
			// Pick a dangerous extension
			dangerousExts := []string{".exe", ".bat", ".cmd", ".vbs", ".js", ".jar", ".msi", ".scr", ".pif", ".com"}
			ext = rapid.SampledFrom(dangerousExts).Draw(t, "dangerousExt")
		} else {
			// Pick a safe extension
			safeExts := []string{".txt", ".pdf", ".doc", ".png", ".jpg", ".zip"}
			ext = rapid.SampledFrom(safeExts).Draw(t, "safeExt")
		}

		filename := baseName + ext

		// Check if extension is detected as dangerous
		detected := handler.IsDangerousExtension(filename)

		if isDangerous && !detected {
			t.Errorf("Dangerous extension %q should be detected", ext)
		}
		if !isDangerous && detected {
			t.Errorf("Safe extension %q should not be detected as dangerous", ext)
		}
	})
}

// TestFilenameSanitization tests filename sanitization
func TestFilenameSanitization(t *testing.T) {
	handler := NewHandler(nil, "test-bucket")

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "clean filename",
			input:    "document.pdf",
			expected: "document.pdf",
		},
		{
			name:     "parent directory traversal",
			input:    "../../../etc/passwd",
			expected: "etcpasswd",
		},
		{
			name:     "windows path traversal",
			input:    "..\\..\\windows\\system32",
			expected: "windowssystem32",
		},
		{
			name:     "null byte injection",
			input:    "file.txt\x00.exe",
			expected: "file.txt.exe",
		},
		{
			name:     "mixed traversal",
			input:    "../file/../test.txt",
			expected: "filetest.txt",
		},
		{
			name:     "forward slash in name",
			input:    "path/to/file.txt",
			expected: "pathtofile.txt",
		},
		{
			name:     "backslash in name",
			input:    "path\\to\\file.txt",
			expected: "pathtofile.txt",
		},
		{
			name:     "empty filename",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := handler.SanitizeFilename(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizeFilename(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestDangerousExtensionDetection tests dangerous extension detection
func TestDangerousExtensionDetection(t *testing.T) {
	handler := NewHandler(nil, "test-bucket")

	tests := []struct {
		name       string
		filename   string
		isDangerous bool
	}{
		// Dangerous extensions
		{"exe file", "virus.exe", true},
		{"bat file", "script.bat", true},
		{"cmd file", "command.cmd", true},
		{"vbs file", "script.vbs", true},
		{"js file", "script.js", true},
		{"jar file", "app.jar", true},
		{"msi file", "installer.msi", true},
		{"scr file", "screensaver.scr", true},
		{"pif file", "program.pif", true},
		{"com file", "command.com", true},
		// Case insensitive
		{"uppercase EXE", "virus.EXE", true},
		{"mixed case Exe", "virus.Exe", true},
		// Safe extensions
		{"txt file", "document.txt", false},
		{"pdf file", "document.pdf", false},
		{"doc file", "document.doc", false},
		{"png file", "image.png", false},
		{"jpg file", "photo.jpg", false},
		{"zip file", "archive.zip", false},
		{"no extension", "filename", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := handler.IsDangerousExtension(tt.filename)
			if result != tt.isDangerous {
				t.Errorf("IsDangerousExtension(%q) = %v, want %v", tt.filename, result, tt.isDangerous)
			}
		})
	}
}

// TestAttachmentValidationWithDangerousExtension tests that dangerous extensions are rejected
func TestAttachmentValidationWithDangerousExtension(t *testing.T) {
	handler := NewHandler(nil, "test-bucket")

	dangerousFiles := []string{
		"virus.exe",
		"script.bat",
		"command.cmd",
		"macro.vbs",
		"code.js",
		"app.jar",
		"installer.msi",
	}

	for _, filename := range dangerousFiles {
		t.Run(filename, func(t *testing.T) {
			att := &parser.Attachment{
				Filename:    filename,
				ContentType: "application/octet-stream",
				SizeBytes:   1024,
			}

			err := handler.ValidateAttachment(att)
			if err == nil {
				t.Errorf("Attachment with dangerous extension %q should be rejected", filename)
			}

			// Verify error type
			if validErr, ok := err.(*AttachmentValidationError); ok {
				if !strings.Contains(validErr.Reason, "dangerous") {
					t.Errorf("Error reason should mention 'dangerous': %s", validErr.Reason)
				}
			}
		})
	}
}


// TestContentTypeValidation tests content-type to extension matching
// Requirements: 2.2 - Validate content-type matches file extension
func TestContentTypeValidation(t *testing.T) {
	handler := NewHandler(nil, "test-bucket")

	tests := []struct {
		name        string
		filename    string
		contentType string
		wantErr     bool
	}{
		// Valid matches
		{
			name:        "PDF with correct content-type",
			filename:    "document.pdf",
			contentType: "application/pdf",
			wantErr:     false,
		},
		{
			name:        "JPEG with correct content-type",
			filename:    "image.jpg",
			contentType: "image/jpeg",
			wantErr:     false,
		},
		{
			name:        "PNG with correct content-type",
			filename:    "image.png",
			contentType: "image/png",
			wantErr:     false,
		},
		{
			name:        "Text file with correct content-type",
			filename:    "readme.txt",
			contentType: "text/plain",
			wantErr:     false,
		},
		{
			name:        "ZIP with correct content-type",
			filename:    "archive.zip",
			contentType: "application/zip",
			wantErr:     false,
		},
		{
			name:        "Content-type with charset parameter",
			filename:    "document.txt",
			contentType: "text/plain; charset=utf-8",
			wantErr:     false,
		},
		// Invalid matches
		{
			name:        "PDF with wrong content-type",
			filename:    "document.pdf",
			contentType: "image/jpeg",
			wantErr:     true,
		},
		{
			name:        "JPEG with wrong content-type",
			filename:    "image.jpg",
			contentType: "application/pdf",
			wantErr:     true,
		},
		{
			name:        "PNG with wrong content-type",
			filename:    "image.png",
			contentType: "text/plain",
			wantErr:     true,
		},
		// Special cases
		{
			name:        "application/octet-stream accepts any extension",
			filename:    "document.pdf",
			contentType: "application/octet-stream",
			wantErr:     false,
		},
		{
			name:        "Unknown extension accepts any content-type",
			filename:    "file.xyz",
			contentType: "application/pdf",
			wantErr:     false,
		},
		{
			name:        "Empty filename skips validation",
			filename:    "",
			contentType: "application/pdf",
			wantErr:     false,
		},
		{
			name:        "Empty content-type skips validation",
			filename:    "document.pdf",
			contentType: "",
			wantErr:     false,
		},
		{
			name:        "No extension skips validation",
			filename:    "filename",
			contentType: "application/pdf",
			wantErr:     false,
		},
		// Alternative content types
		{
			name:        "ZIP with alternative content-type",
			filename:    "archive.zip",
			contentType: "application/x-zip-compressed",
			wantErr:     false,
		},
		{
			name:        "CSV with text/plain",
			filename:    "data.csv",
			contentType: "text/plain",
			wantErr:     false,
		},
		{
			name:        "CSV with text/csv",
			filename:    "data.csv",
			contentType: "text/csv",
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := handler.ValidateContentType(tt.filename, tt.contentType)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateContentType(%q, %q) error = %v, wantErr %v", tt.filename, tt.contentType, err, tt.wantErr)
			}
		})
	}
}

// TestContentTypeValidationInAttachmentValidation tests that content-type validation is integrated
func TestContentTypeValidationInAttachmentValidation(t *testing.T) {
	handler := NewHandler(nil, "test-bucket")

	tests := []struct {
		name        string
		filename    string
		contentType string
		wantErr     bool
		errContains string
	}{
		{
			name:        "Valid PDF attachment",
			filename:    "document.pdf",
			contentType: "application/pdf",
			wantErr:     false,
		},
		{
			name:        "PDF with wrong content-type",
			filename:    "document.pdf",
			contentType: "image/jpeg",
			wantErr:     true,
			errContains: "content-type mismatch",
		},
		{
			name:        "Dangerous extension takes precedence",
			filename:    "virus.exe",
			contentType: "application/x-msdownload",
			wantErr:     true,
			errContains: "dangerous",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			att := &parser.Attachment{
				Filename:    tt.filename,
				ContentType: tt.contentType,
				SizeBytes:   1024,
			}

			err := handler.ValidateAttachment(att)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateAttachment() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && tt.errContains != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("ValidateAttachment() error = %v, should contain %q", err, tt.errContains)
				}
			}
		})
	}
}


// TestMagicByteDetection tests magic byte detection for executables
// Requirements: 2.3 - Scan file magic bytes to detect disguised executables
func TestMagicByteDetection(t *testing.T) {
	handler := NewHandler(nil, "test-bucket")

	tests := []struct {
		name         string
		data         []byte
		wantDetected bool
		wantType     string
	}{
		// Windows PE executable (MZ header)
		{
			name:         "Windows PE executable",
			data:         []byte{0x4D, 0x5A, 0x90, 0x00, 0x03, 0x00, 0x00, 0x00},
			wantDetected: true,
			wantType:     "Windows PE",
		},
		// Linux ELF executable
		{
			name:         "Linux ELF executable",
			data:         []byte{0x7F, 0x45, 0x4C, 0x46, 0x02, 0x01, 0x01, 0x00},
			wantDetected: true,
			wantType:     "Linux ELF",
		},
		// Mach-O 64-bit executable
		{
			name:         "Mach-O 64-bit executable",
			data:         []byte{0xFE, 0xED, 0xFA, 0xCF, 0x00, 0x00, 0x00, 0x00},
			wantDetected: true,
			wantType:     "Mach-O 64-bit",
		},
		// Mach-O 32-bit executable
		{
			name:         "Mach-O 32-bit executable",
			data:         []byte{0xFE, 0xED, 0xFA, 0xCE, 0x00, 0x00, 0x00, 0x00},
			wantDetected: true,
			wantType:     "Mach-O 32-bit",
		},
		// Java class file
		{
			name:         "Java class file",
			data:         []byte{0xCA, 0xFE, 0xBA, 0xBE, 0x00, 0x00, 0x00, 0x34},
			wantDetected: true,
			wantType:     "Java Class",
		},
		// PDF file (not executable)
		{
			name:         "PDF file",
			data:         []byte{0x25, 0x50, 0x44, 0x46, 0x2D, 0x31, 0x2E, 0x34}, // %PDF-1.4
			wantDetected: false,
		},
		// PNG image (not executable)
		{
			name:         "PNG image",
			data:         []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A},
			wantDetected: false,
		},
		// JPEG image (not executable)
		{
			name:         "JPEG image",
			data:         []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46},
			wantDetected: false,
		},
		// ZIP archive (not executable)
		{
			name:         "ZIP archive",
			data:         []byte{0x50, 0x4B, 0x03, 0x04, 0x14, 0x00, 0x00, 0x00},
			wantDetected: false,
		},
		// Plain text (not executable)
		{
			name:         "Plain text",
			data:         []byte("Hello, World!"),
			wantDetected: false,
		},
		// Empty data
		{
			name:         "Empty data",
			data:         []byte{},
			wantDetected: false,
		},
		// Data too short for signature
		{
			name:         "Data too short",
			data:         []byte{0x4D}, // Only first byte of MZ
			wantDetected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detected := handler.DetectExecutableMagicBytes(tt.data)
			if tt.wantDetected {
				if detected == nil {
					t.Errorf("Expected to detect executable, but got nil")
				} else if detected.Name != tt.wantType {
					t.Errorf("Expected type %q, got %q", tt.wantType, detected.Name)
				}
			} else {
				if detected != nil {
					t.Errorf("Expected no detection, but got %q", detected.Name)
				}
			}
		})
	}
}

// TestDisguisedExecutableDetection tests detection of disguised executables
// Requirements: 2.3 - Block disguised executables
func TestDisguisedExecutableDetection(t *testing.T) {
	handler := NewHandler(nil, "test-bucket")

	tests := []struct {
		name         string
		filename     string
		data         []byte
		wantDisguised bool
	}{
		// Disguised executables (non-executable extension with executable content)
		{
			name:         "EXE disguised as PDF",
			filename:     "document.pdf",
			data:         []byte{0x4D, 0x5A, 0x90, 0x00, 0x03, 0x00, 0x00, 0x00},
			wantDisguised: true,
		},
		{
			name:         "ELF disguised as PNG",
			filename:     "image.png",
			data:         []byte{0x7F, 0x45, 0x4C, 0x46, 0x02, 0x01, 0x01, 0x00},
			wantDisguised: true,
		},
		{
			name:         "Java class disguised as TXT",
			filename:     "readme.txt",
			data:         []byte{0xCA, 0xFE, 0xBA, 0xBE, 0x00, 0x00, 0x00, 0x34},
			wantDisguised: true,
		},
		// Not disguised (known executable extension)
		{
			name:         "EXE with EXE extension",
			filename:     "program.exe",
			data:         []byte{0x4D, 0x5A, 0x90, 0x00, 0x03, 0x00, 0x00, 0x00},
			wantDisguised: false, // Not disguised, it's a known executable
		},
		{
			name:         "JAR with JAR extension",
			filename:     "app.jar",
			data:         []byte{0xCA, 0xFE, 0xBA, 0xBE, 0x00, 0x00, 0x00, 0x34},
			wantDisguised: false, // Not disguised, it's a known executable
		},
		// Legitimate files
		{
			name:         "Real PDF file",
			filename:     "document.pdf",
			data:         []byte{0x25, 0x50, 0x44, 0x46, 0x2D, 0x31, 0x2E, 0x34},
			wantDisguised: false,
		},
		{
			name:         "Real PNG file",
			filename:     "image.png",
			data:         []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A},
			wantDisguised: false,
		},
		{
			name:         "Real text file",
			filename:     "readme.txt",
			data:         []byte("This is a text file"),
			wantDisguised: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err, isDisguised := handler.IsDisguisedExecutable(tt.filename, tt.data)
			if isDisguised != tt.wantDisguised {
				t.Errorf("IsDisguisedExecutable(%q) = %v, want %v", tt.filename, isDisguised, tt.wantDisguised)
			}
			if tt.wantDisguised && err == nil {
				t.Error("Expected error for disguised executable")
			}
		})
	}
}

// TestDisguisedExecutableInAttachmentValidation tests that disguised executables are blocked
func TestDisguisedExecutableInAttachmentValidation(t *testing.T) {
	handler := NewHandler(nil, "test-bucket")

	tests := []struct {
		name        string
		filename    string
		contentType string
		data        []byte
		wantErr     bool
		errContains string
	}{
		{
			name:        "EXE disguised as PDF",
			filename:    "document.pdf",
			contentType: "application/pdf",
			data:        []byte{0x4D, 0x5A, 0x90, 0x00, 0x03, 0x00, 0x00, 0x00},
			wantErr:     true,
			errContains: "disguised executable",
		},
		{
			name:        "ELF disguised as image",
			filename:    "photo.jpg",
			contentType: "image/jpeg",
			data:        []byte{0x7F, 0x45, 0x4C, 0x46, 0x02, 0x01, 0x01, 0x00},
			wantErr:     true,
			errContains: "disguised executable",
		},
		{
			name:        "Legitimate PDF",
			filename:    "document.pdf",
			contentType: "application/pdf",
			data:        []byte{0x25, 0x50, 0x44, 0x46, 0x2D, 0x31, 0x2E, 0x34},
			wantErr:     false,
		},
		{
			name:        "Legitimate image",
			filename:    "photo.png",
			contentType: "image/png",
			data:        []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A},
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			att := &parser.Attachment{
				Filename:    tt.filename,
				ContentType: tt.contentType,
				SizeBytes:   int64(len(tt.data)),
				Data:        tt.data,
			}

			err := handler.ValidateAttachment(att)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateAttachment() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && tt.errContains != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("ValidateAttachment() error = %v, should contain %q", err, tt.errContains)
				}
			}
		})
	}
}
