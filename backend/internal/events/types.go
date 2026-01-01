// Package events provides event types and interfaces for the real-time notification system.
package events

import (
	"encoding/json"
	"time"
)

// Event represents a notification event to be sent to clients.
type Event struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	UserID    string          `json:"-"` // internal, not sent to client
	Data      json.RawMessage `json:"data"`
	Timestamp time.Time       `json:"timestamp"`
}

// EventHandler is a function that handles incoming events.
type EventHandler func(event Event)

// EventBus defines the interface for publishing and subscribing to events.
type EventBus interface {
	// Publish sends an event to all subscribers for the event's user.
	Publish(event Event) error
	// Subscribe registers a handler for events for a specific user.
	// Returns an unsubscribe function.
	Subscribe(userID string, handler EventHandler) (unsubscribe func())
	// GetEventsSince returns events after the given event ID for replay.
	GetEventsSince(userID string, lastEventID string) ([]Event, error)
}

// EventStore defines the interface for storing and retrieving events.
type EventStore interface {
	// Store saves an event for later replay.
	Store(event Event) error
	// GetSince returns events after the given event ID.
	GetSince(userID string, eventID string, limit int) ([]Event, error)
	// Cleanup removes events older than the given duration.
	Cleanup(olderThan time.Duration) error
}
