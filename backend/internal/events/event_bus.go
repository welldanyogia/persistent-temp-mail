// Package events provides event types and the event bus for the real-time notification system.
package events

import (
	"fmt"
	"sync"

	"github.com/google/uuid"
)

// InMemoryEventBus implements EventBus using in-memory channels.
type InMemoryEventBus struct {
	mu          sync.RWMutex
	subscribers map[string]map[string]EventHandler // userID -> subscriptionID -> handler
	store       EventStore
}

// NewEventBus creates a new InMemoryEventBus with the given event store.
func NewEventBus(store EventStore) *InMemoryEventBus {
	return &InMemoryEventBus{
		subscribers: make(map[string]map[string]EventHandler),
		store:       store,
	}
}

// Publish sends an event to all subscribers for the event's user.
// It also stores the event for replay if a store is configured.
func (eb *InMemoryEventBus) Publish(event Event) error {
	if event.UserID == "" {
		return fmt.Errorf("event must have a UserID")
	}

	// Store event for replay if store is configured
	if eb.store != nil {
		if err := eb.store.Store(event); err != nil {
			// Log error but don't fail the publish
			// In production, you might want to handle this differently
		}
	}

	eb.mu.RLock()
	handlers, exists := eb.subscribers[event.UserID]
	if !exists || len(handlers) == 0 {
		eb.mu.RUnlock()
		return nil // No subscribers, not an error
	}

	// Copy handlers to avoid holding lock during delivery
	handlersCopy := make([]EventHandler, 0, len(handlers))
	for _, handler := range handlers {
		handlersCopy = append(handlersCopy, handler)
	}
	eb.mu.RUnlock()

	// Deliver event to all handlers
	for _, handler := range handlersCopy {
		handler(event)
	}

	return nil
}

// Subscribe registers a handler for events for a specific user.
// Returns an unsubscribe function that removes the subscription.
func (eb *InMemoryEventBus) Subscribe(userID string, handler EventHandler) (unsubscribe func()) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if eb.subscribers[userID] == nil {
		eb.subscribers[userID] = make(map[string]EventHandler)
	}

	subscriptionID := uuid.New().String()
	eb.subscribers[userID][subscriptionID] = handler

	return func() {
		eb.mu.Lock()
		defer eb.mu.Unlock()

		if handlers, exists := eb.subscribers[userID]; exists {
			delete(handlers, subscriptionID)
			if len(handlers) == 0 {
				delete(eb.subscribers, userID)
			}
		}
	}
}

// GetEventsSince returns events after the given event ID for replay.
// Returns empty slice if no store is configured or no events found.
func (eb *InMemoryEventBus) GetEventsSince(userID string, lastEventID string) ([]Event, error) {
	if eb.store == nil {
		return []Event{}, nil
	}

	return eb.store.GetSince(userID, lastEventID, 100) // Default limit of 100 events
}

// SubscriberCount returns the number of subscribers for a user.
// Useful for testing and monitoring.
func (eb *InMemoryEventBus) SubscriberCount(userID string) int {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	if handlers, exists := eb.subscribers[userID]; exists {
		return len(handlers)
	}
	return 0
}

// TotalSubscribers returns the total number of subscribers across all users.
// Useful for monitoring.
func (eb *InMemoryEventBus) TotalSubscribers() int {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	total := 0
	for _, handlers := range eb.subscribers {
		total += len(handlers)
	}
	return total
}
