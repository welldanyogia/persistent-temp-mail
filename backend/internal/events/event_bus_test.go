package events

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestEventBus_PublishSubscribe(t *testing.T) {
	store := NewEventStore(100)
	bus := NewEventBus(store)

	userID := "test-user-123"
	received := make(chan Event, 1)

	// Subscribe
	unsubscribe := bus.Subscribe(userID, func(event Event) {
		received <- event
	})
	defer unsubscribe()

	// Publish
	event := Event{
		ID:        uuid.New().String(),
		Type:      EventTypeNewEmail,
		UserID:    userID,
		Data:      json.RawMessage(`{"test": "data"}`),
		Timestamp: time.Now(),
	}

	err := bus.Publish(event)
	if err != nil {
		t.Fatalf("failed to publish: %v", err)
	}

	// Wait for event
	select {
	case receivedEvent := <-received:
		if receivedEvent.ID != event.ID {
			t.Errorf("received wrong event ID: expected %s, got %s", event.ID, receivedEvent.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestEventBus_Unsubscribe(t *testing.T) {
	store := NewEventStore(100)
	bus := NewEventBus(store)

	userID := "test-user-123"
	received := make(chan Event, 1)

	// Subscribe and immediately unsubscribe
	unsubscribe := bus.Subscribe(userID, func(event Event) {
		received <- event
	})
	unsubscribe()

	// Publish
	event := Event{
		ID:        uuid.New().String(),
		Type:      EventTypeNewEmail,
		UserID:    userID,
		Data:      json.RawMessage(`{}`),
		Timestamp: time.Now(),
	}

	err := bus.Publish(event)
	if err != nil {
		t.Fatalf("failed to publish: %v", err)
	}

	// Should not receive event
	select {
	case <-received:
		t.Fatal("should not receive event after unsubscribe")
	case <-time.After(100 * time.Millisecond):
		// Expected
	}
}

func TestEventBus_MultipleSubscribers(t *testing.T) {
	store := NewEventStore(100)
	bus := NewEventBus(store)

	userID := "test-user-123"
	var wg sync.WaitGroup
	receivedCount := 0
	var mu sync.Mutex

	// Subscribe multiple handlers
	for i := 0; i < 3; i++ {
		wg.Add(1)
		bus.Subscribe(userID, func(event Event) {
			mu.Lock()
			receivedCount++
			mu.Unlock()
			wg.Done()
		})
	}

	// Publish
	event := Event{
		ID:        uuid.New().String(),
		Type:      EventTypeNewEmail,
		UserID:    userID,
		Data:      json.RawMessage(`{}`),
		Timestamp: time.Now(),
	}

	err := bus.Publish(event)
	if err != nil {
		t.Fatalf("failed to publish: %v", err)
	}

	// Wait for all handlers
	wg.Wait()

	if receivedCount != 3 {
		t.Errorf("expected 3 handlers to receive event, got %d", receivedCount)
	}
}

func TestEventBus_UserIsolation(t *testing.T) {
	store := NewEventStore(100)
	bus := NewEventBus(store)

	user1 := "user-1"
	user2 := "user-2"

	user1Received := make(chan Event, 1)
	user2Received := make(chan Event, 1)

	bus.Subscribe(user1, func(event Event) {
		user1Received <- event
	})
	bus.Subscribe(user2, func(event Event) {
		user2Received <- event
	})

	// Publish event for user1
	event := Event{
		ID:        uuid.New().String(),
		Type:      EventTypeNewEmail,
		UserID:    user1,
		Data:      json.RawMessage(`{}`),
		Timestamp: time.Now(),
	}

	err := bus.Publish(event)
	if err != nil {
		t.Fatalf("failed to publish: %v", err)
	}

	// User1 should receive
	select {
	case <-user1Received:
		// Expected
	case <-time.After(time.Second):
		t.Fatal("user1 should receive event")
	}

	// User2 should not receive
	select {
	case <-user2Received:
		t.Fatal("user2 should not receive event for user1")
	case <-time.After(100 * time.Millisecond):
		// Expected
	}
}

func TestEventBus_PublishWithoutUserID(t *testing.T) {
	store := NewEventStore(100)
	bus := NewEventBus(store)

	event := Event{
		ID:        uuid.New().String(),
		Type:      EventTypeNewEmail,
		UserID:    "", // Empty user ID
		Data:      json.RawMessage(`{}`),
		Timestamp: time.Now(),
	}

	err := bus.Publish(event)
	if err == nil {
		t.Fatal("expected error when publishing without UserID")
	}
}

func TestEventBus_SubscriberCount(t *testing.T) {
	store := NewEventStore(100)
	bus := NewEventBus(store)

	userID := "test-user"

	if bus.SubscriberCount(userID) != 0 {
		t.Error("expected 0 subscribers initially")
	}

	unsub1 := bus.Subscribe(userID, func(event Event) {})
	if bus.SubscriberCount(userID) != 1 {
		t.Error("expected 1 subscriber after first subscribe")
	}

	unsub2 := bus.Subscribe(userID, func(event Event) {})
	if bus.SubscriberCount(userID) != 2 {
		t.Error("expected 2 subscribers after second subscribe")
	}

	unsub1()
	if bus.SubscriberCount(userID) != 1 {
		t.Error("expected 1 subscriber after first unsubscribe")
	}

	unsub2()
	if bus.SubscriberCount(userID) != 0 {
		t.Error("expected 0 subscribers after all unsubscribe")
	}
}

func TestEventBus_GetEventsSince(t *testing.T) {
	store := NewEventStore(100)
	bus := NewEventBus(store)

	userID := "test-user"

	// Publish some events
	eventIDs := make([]string, 5)
	for i := 0; i < 5; i++ {
		event := Event{
			ID:        uuid.New().String(),
			Type:      EventTypeNewEmail,
			UserID:    userID,
			Data:      json.RawMessage(`{}`),
			Timestamp: time.Now(),
		}
		eventIDs[i] = event.ID
		bus.Publish(event)
	}

	// Get events since the second event
	events, err := bus.GetEventsSince(userID, eventIDs[1])
	if err != nil {
		t.Fatalf("failed to get events: %v", err)
	}

	// Should return events 2, 3, 4 (3 events)
	if len(events) != 3 {
		t.Errorf("expected 3 events, got %d", len(events))
	}
}

func TestEventBus_NilStore(t *testing.T) {
	bus := NewEventBus(nil)

	userID := "test-user"
	received := make(chan Event, 1)

	bus.Subscribe(userID, func(event Event) {
		received <- event
	})

	event := Event{
		ID:        uuid.New().String(),
		Type:      EventTypeNewEmail,
		UserID:    userID,
		Data:      json.RawMessage(`{}`),
		Timestamp: time.Now(),
	}

	// Should work without store
	err := bus.Publish(event)
	if err != nil {
		t.Fatalf("failed to publish: %v", err)
	}

	select {
	case <-received:
		// Expected
	case <-time.After(time.Second):
		t.Fatal("should receive event even without store")
	}

	// GetEventsSince should return empty without store
	events, err := bus.GetEventsSince(userID, "some-id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 0 {
		t.Error("expected empty events without store")
	}
}
