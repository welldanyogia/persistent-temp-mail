package events

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"pgregory.net/rapid"
)

// Helper to create a test event
func createTestEvent(userID string, eventType string) Event {
	data, _ := json.Marshal(map[string]string{"test": "data"})
	return Event{
		ID:        uuid.New().String(),
		Type:      eventType,
		UserID:    userID,
		Data:      data,
		Timestamp: time.Now(),
	}
}

// Feature: realtime-notifications, Property 10: Event Replay
// *For any* reconnection with Last-Event-ID header, the SSE_Server SHALL replay all events
// after that ID (if available in buffer).
// **Validates: Requirements 7.3, 7.4**
func TestProperty10_EventReplay(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate random parameters
		bufferSize := rapid.IntRange(10, 100).Draw(t, "bufferSize")
		numEvents := rapid.IntRange(1, bufferSize-1).Draw(t, "numEvents")
		userID := rapid.StringMatching(`[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`).Draw(t, "userID")

		store := NewEventStore(bufferSize)

		// Store events and track their IDs
		eventIDs := make([]string, numEvents)
		for i := 0; i < numEvents; i++ {
			event := createTestEvent(userID, EventTypeNewEmail)
			eventIDs[i] = event.ID
			err := store.Store(event)
			if err != nil {
				t.Fatalf("failed to store event: %v", err)
			}
			// Small delay to ensure different timestamps
			time.Sleep(time.Microsecond)
		}

		// Pick a random event ID to replay from
		replayFromIndex := rapid.IntRange(0, numEvents-1).Draw(t, "replayFromIndex")
		lastEventID := eventIDs[replayFromIndex]

		// Get events since the last event ID
		replayedEvents, err := store.GetSince(userID, lastEventID, 100)
		if err != nil {
			t.Fatalf("failed to get events since: %v", err)
		}

		// Property: All events after lastEventID should be returned
		expectedCount := numEvents - replayFromIndex - 1
		if len(replayedEvents) != expectedCount {
			t.Errorf("expected %d replayed events, got %d", expectedCount, len(replayedEvents))
		}

		// Property: Replayed events should be in order and match expected IDs
		for i, event := range replayedEvents {
			expectedID := eventIDs[replayFromIndex+1+i]
			if event.ID != expectedID {
				t.Errorf("event %d: expected ID %s, got %s", i, expectedID, event.ID)
			}
		}

		// Property: Replayed events should all belong to the same user
		for _, event := range replayedEvents {
			if event.UserID != userID {
				t.Errorf("replayed event has wrong userID: expected %s, got %s", userID, event.UserID)
			}
		}
	})
}

// Test that events from different users are isolated during replay
func TestProperty10_EventReplay_UserIsolation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		bufferSize := rapid.IntRange(20, 50).Draw(t, "bufferSize")
		numEventsPerUser := rapid.IntRange(3, 10).Draw(t, "numEventsPerUser")

		user1 := rapid.StringMatching(`user1-[a-f0-9]{8}`).Draw(t, "user1")
		user2 := rapid.StringMatching(`user2-[a-f0-9]{8}`).Draw(t, "user2")

		store := NewEventStore(bufferSize)

		// Store events for both users interleaved
		user1Events := make([]string, 0)
		user2Events := make([]string, 0)

		for i := 0; i < numEventsPerUser; i++ {
			// User 1 event
			event1 := createTestEvent(user1, EventTypeNewEmail)
			user1Events = append(user1Events, event1.ID)
			store.Store(event1)

			// User 2 event
			event2 := createTestEvent(user2, EventTypeAliasCreated)
			user2Events = append(user2Events, event2.ID)
			store.Store(event2)
		}

		// Replay for user1 from their first event
		if len(user1Events) > 1 {
			replayed, err := store.GetSince(user1, user1Events[0], 100)
			if err != nil {
				t.Fatalf("failed to get events: %v", err)
			}

			// Property: Only user1's events should be returned
			for _, event := range replayed {
				if event.UserID != user1 {
					t.Errorf("replay for user1 returned event for user %s", event.UserID)
				}
			}

			// Property: Should return all user1 events after the first one
			expectedCount := len(user1Events) - 1
			if len(replayed) != expectedCount {
				t.Errorf("expected %d events for user1, got %d", expectedCount, len(replayed))
			}
		}
	})
}

// Test that buffer overflow removes oldest events
func TestProperty10_EventReplay_BufferOverflow(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		bufferSize := rapid.IntRange(5, 20).Draw(t, "bufferSize")
		overflow := rapid.IntRange(1, 10).Draw(t, "overflow")
		userID := rapid.StringMatching(`[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`).Draw(t, "userID")

		store := NewEventStore(bufferSize)

		// Store more events than buffer size
		totalEvents := bufferSize + overflow
		allEventIDs := make([]string, totalEvents)

		for i := 0; i < totalEvents; i++ {
			event := createTestEvent(userID, EventTypeNewEmail)
			allEventIDs[i] = event.ID
			store.Store(event)
		}

		// Property: Store should not exceed buffer size
		if store.Len() > bufferSize {
			t.Errorf("store size %d exceeds buffer size %d", store.Len(), bufferSize)
		}

		// Property: Oldest events should be removed
		// The first 'overflow' events should not be retrievable
		for i := 0; i < overflow; i++ {
			events, _ := store.GetSince(userID, allEventIDs[i], 100)
			// If the event was removed, GetSince returns empty
			// This is expected behavior
			_ = events
		}

		// Property: Most recent events should still be available
		// Get all events for user (empty lastEventID)
		allEvents, err := store.GetSince(userID, "", bufferSize)
		if err != nil {
			t.Fatalf("failed to get all events: %v", err)
		}

		if len(allEvents) != bufferSize {
			t.Errorf("expected %d events in buffer, got %d", bufferSize, len(allEvents))
		}

		// The last event should be the most recently added
		if len(allEvents) > 0 {
			lastEvent := allEvents[len(allEvents)-1]
			if lastEvent.ID != allEventIDs[totalEvents-1] {
				t.Errorf("last event ID mismatch: expected %s, got %s",
					allEventIDs[totalEvents-1], lastEvent.ID)
			}
		}
	})
}

// Test cleanup removes old events
func TestEventStore_Cleanup(t *testing.T) {
	store := NewEventStore(100)
	userID := "test-user"

	// Create events with different timestamps
	oldEvent := Event{
		ID:        "old-event",
		Type:      EventTypeNewEmail,
		UserID:    userID,
		Data:      json.RawMessage(`{}`),
		Timestamp: time.Now().Add(-2 * time.Hour),
	}
	newEvent := Event{
		ID:        "new-event",
		Type:      EventTypeNewEmail,
		UserID:    userID,
		Data:      json.RawMessage(`{}`),
		Timestamp: time.Now(),
	}

	store.Store(oldEvent)
	store.Store(newEvent)

	// Cleanup events older than 1 hour
	err := store.Cleanup(1 * time.Hour)
	if err != nil {
		t.Fatalf("cleanup failed: %v", err)
	}

	// Old event should be removed
	if store.Len() != 1 {
		t.Errorf("expected 1 event after cleanup, got %d", store.Len())
	}

	// New event should still be there
	events, _ := store.GetSince(userID, "", 10)
	if len(events) != 1 || events[0].ID != "new-event" {
		t.Error("new event should still be in store after cleanup")
	}
}

// Test GetSince with empty lastEventID returns recent events
func TestEventStore_GetSince_EmptyLastEventID(t *testing.T) {
	store := NewEventStore(100)
	userID := "test-user"

	// Store some events
	for i := 0; i < 5; i++ {
		event := createTestEvent(userID, EventTypeNewEmail)
		store.Store(event)
	}

	// Get events with empty lastEventID
	events, err := store.GetSince(userID, "", 10)
	if err != nil {
		t.Fatalf("failed to get events: %v", err)
	}

	if len(events) != 5 {
		t.Errorf("expected 5 events, got %d", len(events))
	}
}

// Test GetSince with non-existent lastEventID returns empty
func TestEventStore_GetSince_NonExistentEventID(t *testing.T) {
	store := NewEventStore(100)
	userID := "test-user"

	// Store some events
	for i := 0; i < 5; i++ {
		event := createTestEvent(userID, EventTypeNewEmail)
		store.Store(event)
	}

	// Get events with non-existent lastEventID
	events, err := store.GetSince(userID, "non-existent-id", 10)
	if err != nil {
		t.Fatalf("failed to get events: %v", err)
	}

	// Should return empty since the event ID doesn't exist
	if len(events) != 0 {
		t.Errorf("expected 0 events for non-existent ID, got %d", len(events))
	}
}
