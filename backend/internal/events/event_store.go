package events

import (
	"container/list"
	"sync"
	"time"
)

// InMemoryEventStore implements EventStore using an in-memory buffer.
type InMemoryEventStore struct {
	mu         sync.RWMutex
	events     *list.List                    // Doubly linked list for efficient removal
	eventIndex map[string]*list.Element      // eventID -> list element for O(1) lookup
	userEvents map[string][]*list.Element    // userID -> list of event elements
	maxSize    int                           // Maximum number of events to store
}

// NewEventStore creates a new InMemoryEventStore with the given buffer size.
func NewEventStore(maxSize int) *InMemoryEventStore {
	if maxSize <= 0 {
		maxSize = 1000 // Default buffer size
	}

	return &InMemoryEventStore{
		events:     list.New(),
		eventIndex: make(map[string]*list.Element),
		userEvents: make(map[string][]*list.Element),
		maxSize:    maxSize,
	}
}

// Store saves an event for later replay.
// If the buffer is full, the oldest event is removed.
func (es *InMemoryEventStore) Store(event Event) error {
	es.mu.Lock()
	defer es.mu.Unlock()

	// Remove oldest event if buffer is full
	if es.events.Len() >= es.maxSize {
		es.removeOldestLocked()
	}

	// Add new event to the end
	elem := es.events.PushBack(event)
	es.eventIndex[event.ID] = elem

	// Track event by user
	es.userEvents[event.UserID] = append(es.userEvents[event.UserID], elem)

	return nil
}

// GetSince returns events after the given event ID for a specific user.
// If eventID is empty, returns the most recent events up to limit.
func (es *InMemoryEventStore) GetSince(userID string, eventID string, limit int) ([]Event, error) {
	es.mu.RLock()
	defer es.mu.RUnlock()

	if limit <= 0 {
		limit = 100
	}

	result := make([]Event, 0)

	// If no eventID provided, return recent events for user
	if eventID == "" {
		userElems := es.userEvents[userID]
		start := 0
		if len(userElems) > limit {
			start = len(userElems) - limit
		}
		for i := start; i < len(userElems); i++ {
			event := userElems[i].Value.(Event)
			result = append(result, event)
		}
		return result, nil
	}

	// Find the starting point
	startElem, exists := es.eventIndex[eventID]
	if !exists {
		// Event not found, return empty (client may need to refresh)
		return result, nil
	}

	// Collect events after the starting point for this user
	for elem := startElem.Next(); elem != nil && len(result) < limit; elem = elem.Next() {
		event := elem.Value.(Event)
		if event.UserID == userID {
			result = append(result, event)
		}
	}

	return result, nil
}

// Cleanup removes events older than the given duration.
func (es *InMemoryEventStore) Cleanup(olderThan time.Duration) error {
	es.mu.Lock()
	defer es.mu.Unlock()

	cutoff := time.Now().Add(-olderThan)
	
	// Remove events from the front (oldest) until we find one that's newer
	for es.events.Len() > 0 {
		front := es.events.Front()
		event := front.Value.(Event)
		
		if event.Timestamp.After(cutoff) {
			break // All remaining events are newer
		}
		
		es.removeElementLocked(front)
	}

	return nil
}

// removeOldestLocked removes the oldest event. Must be called with lock held.
func (es *InMemoryEventStore) removeOldestLocked() {
	if es.events.Len() == 0 {
		return
	}
	
	front := es.events.Front()
	es.removeElementLocked(front)
}

// removeElementLocked removes an element from all indexes. Must be called with lock held.
func (es *InMemoryEventStore) removeElementLocked(elem *list.Element) {
	event := elem.Value.(Event)
	
	// Remove from main list
	es.events.Remove(elem)
	
	// Remove from event index
	delete(es.eventIndex, event.ID)
	
	// Remove from user events
	userElems := es.userEvents[event.UserID]
	for i, e := range userElems {
		if e == elem {
			es.userEvents[event.UserID] = append(userElems[:i], userElems[i+1:]...)
			break
		}
	}
	
	// Clean up empty user entry
	if len(es.userEvents[event.UserID]) == 0 {
		delete(es.userEvents, event.UserID)
	}
}

// Len returns the number of events in the store.
func (es *InMemoryEventStore) Len() int {
	es.mu.RLock()
	defer es.mu.RUnlock()
	return es.events.Len()
}

// LenForUser returns the number of events for a specific user.
func (es *InMemoryEventStore) LenForUser(userID string) int {
	es.mu.RLock()
	defer es.mu.RUnlock()
	return len(es.userEvents[userID])
}

// Clear removes all events from the store.
func (es *InMemoryEventStore) Clear() {
	es.mu.Lock()
	defer es.mu.Unlock()
	
	es.events = list.New()
	es.eventIndex = make(map[string]*list.Element)
	es.userEvents = make(map[string][]*list.Element)
}
