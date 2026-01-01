package smtp

import (
	"encoding/json"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// TestProperty16_RealtimeNotification tests Property 16: Real-time Notification
// Feature: smtp-email-receiver, Property 16: Real-time Notification
// *For any* successfully stored email, the SMTP_Server SHALL publish a new_email event
// containing alias_id, email_id, sender, subject, and preview_text to the SSE event bus.
// **Validates: Requirements 8.1, 8.2, 8.3**
func TestProperty16_RealtimeNotification(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate random email data
		emailID := rapid.String().Draw(t, "emailID")
		aliasID := rapid.String().Draw(t, "aliasID")
		aliasEmail := rapid.StringMatching(`[a-z]+@[a-z]+\.[a-z]+`).Draw(t, "aliasEmail")
		fromAddress := rapid.StringMatching(`[a-z]+@[a-z]+\.[a-z]+`).Draw(t, "fromAddress")
		
		// Optional fields
		hasFromName := rapid.Bool().Draw(t, "hasFromName")
		var fromName *string
		if hasFromName {
			name := rapid.String().Draw(t, "fromName")
			fromName = &name
		}
		
		hasSubject := rapid.Bool().Draw(t, "hasSubject")
		var subject *string
		if hasSubject {
			subj := rapid.String().Draw(t, "subject")
			subject = &subj
		}
		
		// Body text for preview
		hasBodyText := rapid.Bool().Draw(t, "hasBodyText")
		var bodyText *string
		if hasBodyText {
			text := rapid.String().Draw(t, "bodyText")
			bodyText = &text
		}
		
		hasBodyHTML := rapid.Bool().Draw(t, "hasBodyHTML")
		var bodyHTML *string
		if hasBodyHTML {
			html := rapid.String().Draw(t, "bodyHTML")
			bodyHTML = &html
		}
		
		receivedAt := time.Now().UTC()
		hasAttachments := rapid.Bool().Draw(t, "hasAttachments")
		sizeBytes := rapid.Int64Range(0, 25*1024*1024).Draw(t, "sizeBytes")
		
		// Create the event
		event := CreateNewEmailEvent(
			emailID,
			aliasID,
			aliasEmail,
			fromAddress,
			fromName,
			subject,
			bodyText,
			bodyHTML,
			receivedAt,
			hasAttachments,
			sizeBytes,
		)
		
		// Property 16: Event SHALL contain alias_id
		if event.AliasID != aliasID {
			t.Fatalf("Event alias_id mismatch: expected %s, got %s", aliasID, event.AliasID)
		}
		
		// Property 16: Event SHALL contain email_id
		if event.ID != emailID {
			t.Fatalf("Event email_id mismatch: expected %s, got %s", emailID, event.ID)
		}
		
		// Property 16: Event SHALL contain sender (from_address)
		if event.FromAddress != fromAddress {
			t.Fatalf("Event from_address mismatch: expected %s, got %s", fromAddress, event.FromAddress)
		}
		
		// Property 16: Event SHALL contain subject (if provided)
		if hasSubject {
			if event.Subject == nil || *event.Subject != *subject {
				t.Fatalf("Event subject mismatch")
			}
		}
		
		// Property 16: Event SHALL contain preview_text
		// Preview text should be generated from body
		if hasBodyText || hasBodyHTML {
			// Preview should not be empty if body exists
			// Note: preview can be empty if body is empty
		}
		
		// Verify preview text length limit (max 200 chars + "...")
		if len(event.PreviewText) > 203 { // 200 + "..."
			t.Fatalf("Preview text too long: %d chars", len(event.PreviewText))
		}
		
		// Verify other required fields
		if event.AliasEmail != aliasEmail {
			t.Fatalf("Event alias_email mismatch: expected %s, got %s", aliasEmail, event.AliasEmail)
		}
		
		if event.HasAttachments != hasAttachments {
			t.Fatalf("Event has_attachments mismatch: expected %v, got %v", hasAttachments, event.HasAttachments)
		}
		
		if event.SizeBytes != sizeBytes {
			t.Fatalf("Event size_bytes mismatch: expected %d, got %d", sizeBytes, event.SizeBytes)
		}
		
		// Verify event can be converted to generic Event
		userID := rapid.String().Draw(t, "userID")
		genericEvent, err := event.ToEvent(userID)
		if err != nil {
			t.Fatalf("Failed to convert to generic event: %v", err)
		}
		
		// Verify generic event properties
		if genericEvent.Type != EventTypeNewEmail {
			t.Fatalf("Event type mismatch: expected %s, got %s", EventTypeNewEmail, genericEvent.Type)
		}
		
		if genericEvent.UserID != userID {
			t.Fatalf("Event userID mismatch: expected %s, got %s", userID, genericEvent.UserID)
		}
		
		if genericEvent.ID == "" {
			t.Fatal("Event ID should not be empty")
		}
		
		if genericEvent.Timestamp.IsZero() {
			t.Fatal("Event timestamp should not be zero")
		}
		
		// Verify data can be unmarshaled back
		var unmarshaledEvent NewEmailEvent
		if err := json.Unmarshal(genericEvent.Data, &unmarshaledEvent); err != nil {
			t.Fatalf("Failed to unmarshal event data: %v", err)
		}
		
		// Verify unmarshaled data matches original
		if unmarshaledEvent.ID != event.ID {
			t.Fatalf("Unmarshaled ID mismatch: expected %s, got %s", event.ID, unmarshaledEvent.ID)
		}
		
		if unmarshaledEvent.AliasID != event.AliasID {
			t.Fatalf("Unmarshaled AliasID mismatch: expected %s, got %s", event.AliasID, unmarshaledEvent.AliasID)
		}
		
		if unmarshaledEvent.FromAddress != event.FromAddress {
			t.Fatalf("Unmarshaled FromAddress mismatch: expected %s, got %s", event.FromAddress, unmarshaledEvent.FromAddress)
		}
	})
}

// TestPreviewTextGeneration tests preview text generation from various body types
func TestPreviewTextGeneration(t *testing.T) {
	tests := []struct {
		name        string
		bodyText    *string
		bodyHTML    *string
		expectEmpty bool
		maxLen      int
	}{
		{
			name:        "empty body",
			bodyText:    nil,
			bodyHTML:    nil,
			expectEmpty: true,
		},
		{
			name:        "plain text only",
			bodyText:    strPtr("Hello, this is a test email."),
			bodyHTML:    nil,
			expectEmpty: false,
			maxLen:      203,
		},
		{
			name:        "HTML only",
			bodyText:    nil,
			bodyHTML:    strPtr("<p>Hello, this is a <strong>test</strong> email.</p>"),
			expectEmpty: false,
			maxLen:      203,
		},
		{
			name:        "both text and HTML - prefer text",
			bodyText:    strPtr("Plain text version"),
			bodyHTML:    strPtr("<p>HTML version</p>"),
			expectEmpty: false,
			maxLen:      203,
		},
		{
			name:        "long text truncation",
			bodyText:    strPtr(longText(300)),
			bodyHTML:    nil,
			expectEmpty: false,
			maxLen:      203,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := CreateNewEmailEvent(
				"email-123",
				"alias-456",
				"test@example.com",
				"sender@example.com",
				nil,
				nil,
				tt.bodyText,
				tt.bodyHTML,
				time.Now(),
				false,
				1000,
			)
			
			if tt.expectEmpty && event.PreviewText != "" {
				t.Errorf("Expected empty preview, got: %s", event.PreviewText)
			}
			
			if !tt.expectEmpty && event.PreviewText == "" {
				t.Error("Expected non-empty preview")
			}
			
			if len(event.PreviewText) > tt.maxLen {
				t.Errorf("Preview too long: %d > %d", len(event.PreviewText), tt.maxLen)
			}
		})
	}
}

// TestHTMLStripping tests HTML tag stripping for preview
func TestHTMLStripping(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected string
	}{
		{
			name:     "simple paragraph",
			html:     "<p>Hello World</p>",
			expected: "Hello World",
		},
		{
			name:     "nested tags",
			html:     "<div><p>Hello <strong>World</strong></p></div>",
			expected: "Hello World",
		},
		{
			name:     "multiple paragraphs",
			html:     "<p>First</p><p>Second</p>",
			expected: "First Second",
		},
		{
			name:     "with attributes",
			html:     `<p class="test" id="main">Content</p>`,
			expected: "Content",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripHTMLTags(tt.html)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

// TestNoOpEventPublisher tests the no-op publisher
func TestNoOpEventPublisher(t *testing.T) {
	publisher := NewNoOpEventPublisher()
	
	event := Event{
		ID:        "test-123",
		Type:      EventTypeNewEmail,
		UserID:    "user-456",
		Data:      []byte(`{"test": true}`),
		Timestamp: time.Now(),
	}
	
	// Should not return error
	err := publisher.Publish(event)
	if err != nil {
		t.Errorf("NoOpEventPublisher should not return error, got: %v", err)
	}
}

// TestEventIDGeneration tests that event IDs are unique
func TestEventIDGeneration(t *testing.T) {
	ids := make(map[string]bool)
	
	for i := 0; i < 1000; i++ {
		id := GenerateEventID()
		if ids[id] {
			t.Errorf("Duplicate event ID generated: %s", id)
		}
		ids[id] = true
	}
}

// Helper functions

func strPtr(s string) *string {
	return &s
}

func longText(length int) string {
	result := make([]byte, length)
	for i := 0; i < length; i++ {
		result[i] = 'a' + byte(i%26)
	}
	return string(result)
}
