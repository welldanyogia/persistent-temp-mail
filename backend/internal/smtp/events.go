// Package smtp provides SMTP server functionality
// Feature: smtp-email-receiver, Property 16: Real-time Notification
package smtp

import (
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"
)

// EventType represents the type of event
type EventType string

const (
	// EventTypeNewEmail is published when a new email is successfully stored
	// Requirements: 8.1, 8.2, 8.3
	EventTypeNewEmail EventType = "new_email"
)

// Event represents a generic event that can be published
type Event struct {
	ID        string          `json:"id"`
	Type      EventType       `json:"type"`
	UserID    string          `json:"-"` // Internal, not sent to client
	Data      json.RawMessage `json:"data"`
	Timestamp time.Time       `json:"timestamp"`
}

// NewEmailEvent represents a new email notification event
// Requirements: 8.1, 8.2, 8.3
// Property 16: Real-time Notification - includes alias_id, email_id, sender, subject, preview_text
type NewEmailEvent struct {
	ID             string    `json:"id"`              // Email ID
	AliasID        string    `json:"alias_id"`        // Alias ID that received the email
	AliasEmail     string    `json:"alias_email"`     // Full email address of the alias
	FromAddress    string    `json:"from_address"`    // Sender email address
	FromName       *string   `json:"from_name,omitempty"` // Sender display name (optional)
	Subject        *string   `json:"subject,omitempty"`   // Email subject (optional)
	PreviewText    string    `json:"preview_text"`    // Preview of email body (first 200 chars)
	ReceivedAt     time.Time `json:"received_at"`     // When the email was received
	HasAttachments bool      `json:"has_attachments"` // Whether email has attachments
	SizeBytes      int64     `json:"size_bytes"`      // Size of the email in bytes
}

// EventPublisher defines the interface for publishing events
// This allows the SMTP server to publish events without knowing the implementation details
type EventPublisher interface {
	// Publish publishes an event to the event bus
	// Returns error if publishing fails
	Publish(event Event) error
}

// NoOpEventPublisher is a no-op implementation of EventPublisher
// Used when no event bus is configured
type NoOpEventPublisher struct{}

// Publish does nothing and returns nil
func (p *NoOpEventPublisher) Publish(event Event) error {
	return nil
}

// NewNoOpEventPublisher creates a new no-op event publisher
func NewNoOpEventPublisher() *NoOpEventPublisher {
	return &NoOpEventPublisher{}
}

// CreateNewEmailEvent creates a NewEmailEvent from email data
// Requirements: 8.1, 8.2, 8.3
// Property 16: Real-time Notification
func CreateNewEmailEvent(
	emailID string,
	aliasID string,
	aliasEmail string,
	fromAddress string,
	fromName *string,
	subject *string,
	bodyText *string,
	bodyHTML *string,
	receivedAt time.Time,
	hasAttachments bool,
	sizeBytes int64,
) *NewEmailEvent {
	// Generate preview text from body (prefer text over HTML)
	previewText := generatePreviewText(bodyText, bodyHTML)

	return &NewEmailEvent{
		ID:             emailID,
		AliasID:        aliasID,
		AliasEmail:     aliasEmail,
		FromAddress:    fromAddress,
		FromName:       fromName,
		Subject:        subject,
		PreviewText:    previewText,
		ReceivedAt:     receivedAt,
		HasAttachments: hasAttachments,
		SizeBytes:      sizeBytes,
	}
}

// generatePreviewText generates a preview text from email body
// Prefers plain text over HTML, truncates to 200 characters
func generatePreviewText(bodyText *string, bodyHTML *string) string {
	const maxPreviewLength = 200

	var text string
	if bodyText != nil && *bodyText != "" {
		text = *bodyText
	} else if bodyHTML != nil && *bodyHTML != "" {
		// Strip HTML tags for preview (simple approach)
		text = stripHTMLTags(*bodyHTML)
	}

	// Truncate to max length
	if len(text) > maxPreviewLength {
		// Find a good break point (space or punctuation)
		breakPoint := maxPreviewLength
		for i := maxPreviewLength - 1; i >= maxPreviewLength-50 && i >= 0; i-- {
			if text[i] == ' ' || text[i] == '.' || text[i] == ',' || text[i] == '!' || text[i] == '?' {
				breakPoint = i
				break
			}
		}
		text = text[:breakPoint] + "..."
	}

	return text
}

// stripHTMLTags removes HTML tags from a string (simple implementation)
func stripHTMLTags(html string) string {
	var result []byte
	inTag := false

	for i := 0; i < len(html); i++ {
		switch html[i] {
		case '<':
			inTag = true
		case '>':
			inTag = false
			// Add space after closing tags to separate words
			if len(result) > 0 && result[len(result)-1] != ' ' {
				result = append(result, ' ')
			}
		default:
			if !inTag {
				result = append(result, html[i])
			}
		}
	}

	// Clean up multiple spaces
	return cleanSpaces(string(result))
}

// cleanSpaces removes multiple consecutive spaces and trims
func cleanSpaces(s string) string {
	var result []byte
	lastWasSpace := true // Start true to trim leading spaces

	for i := 0; i < len(s); i++ {
		if s[i] == ' ' || s[i] == '\n' || s[i] == '\r' || s[i] == '\t' {
			if !lastWasSpace {
				result = append(result, ' ')
				lastWasSpace = true
			}
		} else {
			result = append(result, s[i])
			lastWasSpace = false
		}
	}

	// Trim trailing space
	if len(result) > 0 && result[len(result)-1] == ' ' {
		result = result[:len(result)-1]
	}

	return string(result)
}

// ToEvent converts NewEmailEvent to a generic Event
// Requirements: 8.1, 8.2, 8.3
func (e *NewEmailEvent) ToEvent(userID string) (*Event, error) {
	data, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}

	return &Event{
		ID:        GenerateEventID(),
		Type:      EventTypeNewEmail,
		UserID:    userID,
		Data:      data,
		Timestamp: time.Now().UTC(),
	}, nil
}

// eventCounter is used to ensure unique event IDs
var eventCounter uint64

// GenerateEventID generates a unique event ID
// Uses timestamp + atomic counter for uniqueness
func GenerateEventID() string {
	counter := atomic.AddUint64(&eventCounter, 1)
	timestamp := time.Now().UnixNano()
	return fmt.Sprintf("%x-%x", timestamp, counter)
}
