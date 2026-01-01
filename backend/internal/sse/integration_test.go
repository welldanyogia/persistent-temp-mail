// Package sse provides Server-Sent Events (SSE) functionality for real-time notifications.
// Feature: realtime-notifications
// Task 11.2: Integration tests for SSE system
//go:build integration

package sse

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/auth"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/events"
)

// testSetup creates all SSE components for integration testing.
type testSetup struct {
	config       Config
	eventStore   *events.InMemoryEventStore
	eventBus     *events.InMemoryEventBus
	connManager  *InMemoryConnectionManager
	tokenService *auth.TokenService
	handler      *Handler
	eventRouter  *EventRouter
	router       *chi.Mux
	stopCleanup  func()
}

// newTestSetup creates a new test setup with all components wired together.
func newTestSetup(config Config) *testSetup {
	eventStore := events.NewEventStore(config.EventBufferSize)
	eventBus := events.NewEventBus(eventStore)
	connManager := NewConnectionManager(config)
	tokenService := auth.NewTokenService(auth.TokenServiceConfig{
		AccessSecret:       "test-access-secret-key-32-bytes!",
		RefreshSecret:      "test-refresh-secret-key-32-bytes",
		AccessTokenExpiry:  15 * time.Minute,
		RefreshTokenExpiry: 7 * 24 * time.Hour,
		Issuer:             "test-issuer",
	})

	handler := NewHandler(config, connManager, eventBus, tokenService)
	eventRouter := NewEventRouter(connManager, eventBus)

	// Start cleanup routine
	stopCleanup := connManager.StartCleanupRoutine(config.HeartbeatInterval * 3)

	// Setup router
	router := chi.NewRouter()
	router.Route("/api/v1", func(r chi.Router) {
		RegisterRoutes(r, handler)
	})

	return &testSetup{
		config:       config,
		eventStore:   eventStore,
		eventBus:     eventBus,
		connManager:  connManager,
		tokenService: tokenService,
		handler:      handler,
		eventRouter:  eventRouter,
		router:       router,
		stopCleanup:  stopCleanup,
	}
}

// cleanup stops the cleanup routine and cleans up resources.
func (ts *testSetup) cleanup() {
	ts.stopCleanup()
}

// generateToken generates a valid access token for testing.
func (ts *testSetup) generateToken(userID, email string) string {
	token, _ := ts.tokenService.GenerateAccessToken(userID, email)
	return token
}

// Integration Test: Full connection → receive event → disconnect flow
// Requirements: All - Test complete SSE lifecycle
func TestIntegration_FullConnectionLifecycle(t *testing.T) {
	config := Config{
		HeartbeatInterval:     100 * time.Millisecond,
		ConnectionTimeout:     1 * time.Hour,
		MaxConnectionsPerUser: 10,
		EventBufferSize:       100,
	}

	ts := newTestSetup(config)
	defer ts.cleanup()

	userID := uuid.New().String()
	token := ts.generateToken(userID, "test@example.com")

	// Create request
	req := httptest.NewRequest("GET", "/api/v1/events/stream?token="+token, nil)
	w := newMockResponseWriter()

	// Start connection in goroutine
	done := make(chan struct{})
	go func() {
		defer close(done)
		ts.router.ServeHTTP(w, req)
	}()

	// Wait for connection to establish
	time.Sleep(50 * time.Millisecond)

	// Verify connection is established
	if ts.connManager.CountConnections(userID) != 1 {
		t.Errorf("expected 1 connection, got %d", ts.connManager.CountConnections(userID))
	}

	// Verify connected event was sent
	body := w.Body.String()
	if !strings.Contains(body, "event: connected") {
		t.Error("connected event should be sent")
	}

	// Publish an event
	emailEvent := events.NewEmailEvent{
		ID:          uuid.New().String(),
		AliasID:     uuid.New().String(),
		AliasEmail:  "alias@example.com",
		FromAddress: "sender@example.com",
		PreviewText: "Test email content",
		ReceivedAt:  time.Now(),
	}
	err := ts.eventRouter.RouteNewEmailEvent(userID, emailEvent)
	if err != nil {
		t.Fatalf("failed to route event: %v", err)
	}

	// Wait for event to be delivered
	time.Sleep(50 * time.Millisecond)

	// Verify event was received
	body = w.Body.String()
	if !strings.Contains(body, "event: new_email") {
		t.Error("new_email event should be received")
	}
	if !strings.Contains(body, emailEvent.ID) {
		t.Error("event should contain email ID")
	}

	// Disconnect
	conns := ts.connManager.GetConnections(userID)
	for _, conn := range conns {
		conn.Close()
	}

	// Wait for handler to finish
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
	}

	// Verify connection is removed
	if ts.connManager.CountConnections(userID) != 0 {
		t.Errorf("expected 0 connections after disconnect, got %d", ts.connManager.CountConnections(userID))
	}
}

// Integration Test: Connection limit enforcement
// Requirements: 1.5, 1.6, 8.2 - Connection limit management
func TestIntegration_ConnectionLimitEnforcement(t *testing.T) {
	maxConnections := 3
	config := Config{
		HeartbeatInterval:     1 * time.Hour, // Long interval to avoid interference
		ConnectionTimeout:     1 * time.Hour,
		MaxConnectionsPerUser: maxConnections,
		EventBufferSize:       100,
	}

	ts := newTestSetup(config)
	defer ts.cleanup()

	userID := uuid.New().String()
	token := ts.generateToken(userID, "test@example.com")

	// Track connections and their writers
	type connInfo struct {
		done   chan struct{}
		writer *mockResponseWriter
	}
	connections := make([]connInfo, 0)

	// Create connections up to the limit
	for i := 0; i < maxConnections; i++ {
		req := httptest.NewRequest("GET", "/api/v1/events/stream?token="+token, nil)
		w := newMockResponseWriter()
		done := make(chan struct{})

		go func() {
			defer close(done)
			ts.router.ServeHTTP(w, req)
		}()

		connections = append(connections, connInfo{done: done, writer: w})
		time.Sleep(20 * time.Millisecond) // Stagger connections
	}

	// Wait for all connections to establish
	time.Sleep(50 * time.Millisecond)

	// Verify we have maxConnections
	if ts.connManager.CountConnections(userID) != maxConnections {
		t.Errorf("expected %d connections, got %d", maxConnections, ts.connManager.CountConnections(userID))
	}

	// Add one more connection (should close oldest)
	req := httptest.NewRequest("GET", "/api/v1/events/stream?token="+token, nil)
	w := newMockResponseWriter()
	done := make(chan struct{})

	go func() {
		defer close(done)
		ts.router.ServeHTTP(w, req)
	}()

	connections = append(connections, connInfo{done: done, writer: w})

	// Wait for connection limit enforcement
	time.Sleep(100 * time.Millisecond)

	// Should still have maxConnections (oldest was closed)
	if ts.connManager.CountConnections(userID) != maxConnections {
		t.Errorf("expected %d connections after limit enforcement, got %d",
			maxConnections, ts.connManager.CountConnections(userID))
	}

	// Oldest connection should have received connection_limit event
	oldestBody := connections[0].writer.Body.String()
	if !strings.Contains(oldestBody, "connection_limit") {
		t.Error("oldest connection should receive connection_limit event")
	}

	// Cleanup all connections
	conns := ts.connManager.GetConnections(userID)
	for _, conn := range conns {
		conn.Close()
	}

	// Wait for all handlers to finish
	for _, ci := range connections {
		select {
		case <-ci.done:
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// Integration Test: Event routing isolation
// Requirements: 3.3 - Route events to correct user connections only
func TestIntegration_EventRoutingIsolation(t *testing.T) {
	config := Config{
		HeartbeatInterval:     1 * time.Hour,
		ConnectionTimeout:     1 * time.Hour,
		MaxConnectionsPerUser: 10,
		EventBufferSize:       100,
	}

	ts := newTestSetup(config)
	defer ts.cleanup()

	// Create two users
	user1ID := uuid.New().String()
	user2ID := uuid.New().String()
	token1 := ts.generateToken(user1ID, "user1@example.com")
	token2 := ts.generateToken(user2ID, "user2@example.com")

	// Connect both users
	req1 := httptest.NewRequest("GET", "/api/v1/events/stream?token="+token1, nil)
	w1 := newMockResponseWriter()
	done1 := make(chan struct{})

	req2 := httptest.NewRequest("GET", "/api/v1/events/stream?token="+token2, nil)
	w2 := newMockResponseWriter()
	done2 := make(chan struct{})

	go func() {
		defer close(done1)
		ts.router.ServeHTTP(w1, req1)
	}()

	go func() {
		defer close(done2)
		ts.router.ServeHTTP(w2, req2)
	}()

	// Wait for connections
	time.Sleep(50 * time.Millisecond)

	// Verify both users are connected
	if ts.connManager.CountConnections(user1ID) != 1 {
		t.Error("user1 should have 1 connection")
	}
	if ts.connManager.CountConnections(user2ID) != 1 {
		t.Error("user2 should have 1 connection")
	}

	// Send event to user1 only
	emailEvent := events.NewEmailEvent{
		ID:          uuid.New().String(),
		AliasID:     uuid.New().String(),
		AliasEmail:  "user1-alias@example.com",
		FromAddress: "sender@example.com",
		PreviewText: "Email for user1",
		ReceivedAt:  time.Now(),
	}
	ts.eventRouter.RouteNewEmailEvent(user1ID, emailEvent)

	// Wait for event delivery
	time.Sleep(50 * time.Millisecond)

	// User1 should receive the event
	body1 := w1.Body.String()
	if !strings.Contains(body1, "event: new_email") {
		t.Error("user1 should receive new_email event")
	}
	if !strings.Contains(body1, emailEvent.ID) {
		t.Error("user1 should receive correct email ID")
	}

	// User2 should NOT receive the event (only connected event)
	body2 := w2.Body.String()
	if strings.Contains(body2, "event: new_email") {
		t.Error("user2 should NOT receive new_email event for user1")
	}

	// Cleanup
	for _, conn := range ts.connManager.GetConnections(user1ID) {
		conn.Close()
	}
	for _, conn := range ts.connManager.GetConnections(user2ID) {
		conn.Close()
	}

	select {
	case <-done1:
	case <-time.After(100 * time.Millisecond):
	}
	select {
	case <-done2:
	case <-time.After(100 * time.Millisecond):
	}
}


// Integration Test: Reconnection with Last-Event-ID
// Requirements: 7.3, 7.4 - Support Last-Event-ID for reconnection and replay
func TestIntegration_ReconnectionWithLastEventID(t *testing.T) {
	config := Config{
		HeartbeatInterval:     1 * time.Hour,
		ConnectionTimeout:     1 * time.Hour,
		MaxConnectionsPerUser: 10,
		EventBufferSize:       100,
	}

	ts := newTestSetup(config)
	defer ts.cleanup()

	userID := uuid.New().String()
	token := ts.generateToken(userID, "test@example.com")

	// First connection - receive some events
	req1 := httptest.NewRequest("GET", "/api/v1/events/stream?token="+token, nil)
	w1 := newMockResponseWriter()
	done1 := make(chan struct{})

	go func() {
		defer close(done1)
		ts.router.ServeHTTP(w1, req1)
	}()

	time.Sleep(50 * time.Millisecond)

	// Publish events
	event1 := events.NewEmailEvent{
		ID:          "email-1",
		AliasID:     uuid.New().String(),
		AliasEmail:  "alias@example.com",
		FromAddress: "sender1@example.com",
		PreviewText: "First email",
		ReceivedAt:  time.Now(),
	}
	ts.eventRouter.RouteNewEmailEvent(userID, event1)

	time.Sleep(20 * time.Millisecond)

	// Get the event ID from the store
	storedEvents, _ := ts.eventBus.GetEventsSince(userID, "")
	if len(storedEvents) == 0 {
		t.Fatal("no events stored")
	}
	lastEventID := storedEvents[len(storedEvents)-1].ID

	// Publish more events
	event2 := events.NewEmailEvent{
		ID:          "email-2",
		AliasID:     uuid.New().String(),
		AliasEmail:  "alias@example.com",
		FromAddress: "sender2@example.com",
		PreviewText: "Second email",
		ReceivedAt:  time.Now(),
	}
	ts.eventRouter.RouteNewEmailEvent(userID, event2)

	event3 := events.NewEmailEvent{
		ID:          "email-3",
		AliasID:     uuid.New().String(),
		AliasEmail:  "alias@example.com",
		FromAddress: "sender3@example.com",
		PreviewText: "Third email",
		ReceivedAt:  time.Now(),
	}
	ts.eventRouter.RouteNewEmailEvent(userID, event3)

	time.Sleep(50 * time.Millisecond)

	// Disconnect first connection
	for _, conn := range ts.connManager.GetConnections(userID) {
		conn.Close()
	}

	select {
	case <-done1:
	case <-time.After(100 * time.Millisecond):
	}

	// Reconnect with Last-Event-ID
	req2 := httptest.NewRequest("GET", "/api/v1/events/stream?token="+token, nil)
	req2.Header.Set("Last-Event-ID", lastEventID)
	w2 := newMockResponseWriter()
	done2 := make(chan struct{})

	go func() {
		defer close(done2)
		ts.router.ServeHTTP(w2, req2)
	}()

	// Wait for connection and replay
	time.Sleep(100 * time.Millisecond)

	body2 := w2.Body.String()

	// Should have connected event
	if !strings.Contains(body2, "event: connected") {
		t.Error("should receive connected event on reconnection")
	}

	// Should have replayed events after lastEventID (event2 and event3)
	if !strings.Contains(body2, "email-2") {
		t.Error("should replay event2 after Last-Event-ID")
	}
	if !strings.Contains(body2, "email-3") {
		t.Error("should replay event3 after Last-Event-ID")
	}

	// Cleanup
	for _, conn := range ts.connManager.GetConnections(userID) {
		conn.Close()
	}

	select {
	case <-done2:
	case <-time.After(100 * time.Millisecond):
	}
}

// Integration Test: Heartbeat delivery
// Requirements: 2.1, 2.2, 2.3 - Heartbeat every 30 seconds with timestamp
func TestIntegration_HeartbeatDelivery(t *testing.T) {
	config := Config{
		HeartbeatInterval:     50 * time.Millisecond, // Short interval for testing
		ConnectionTimeout:     1 * time.Hour,
		MaxConnectionsPerUser: 10,
		EventBufferSize:       100,
	}

	ts := newTestSetup(config)
	defer ts.cleanup()

	userID := uuid.New().String()
	token := ts.generateToken(userID, "test@example.com")

	req := httptest.NewRequest("GET", "/api/v1/events/stream?token="+token, nil)
	w := newMockResponseWriter()
	done := make(chan struct{})

	go func() {
		defer close(done)
		ts.router.ServeHTTP(w, req)
	}()

	// Wait for at least 3 heartbeats
	time.Sleep(200 * time.Millisecond)

	body := w.Body.String()

	// Count heartbeat events
	heartbeatCount := strings.Count(body, "event: heartbeat")
	if heartbeatCount < 3 {
		t.Errorf("expected at least 3 heartbeat events, got %d", heartbeatCount)
	}

	// Verify heartbeat format
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "event: heartbeat") {
			// Find data line
			for j := i + 1; j < len(lines) && j < i+4; j++ {
				if strings.HasPrefix(lines[j], "data: ") {
					dataStr := strings.TrimPrefix(lines[j], "data: ")
					var heartbeat events.HeartbeatEvent
					if err := json.Unmarshal([]byte(dataStr), &heartbeat); err != nil {
						t.Errorf("heartbeat data should be valid JSON: %v", err)
					}
					if heartbeat.Timestamp.IsZero() {
						t.Error("heartbeat should have timestamp")
					}
					break
				}
			}
		}
	}

	// Cleanup
	for _, conn := range ts.connManager.GetConnections(userID) {
		conn.Close()
	}

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
	}
}

// Integration Test: Multiple event types
// Requirements: 3.1-6.4 - All event types should be routed correctly
func TestIntegration_MultipleEventTypes(t *testing.T) {
	config := Config{
		HeartbeatInterval:     1 * time.Hour,
		ConnectionTimeout:     1 * time.Hour,
		MaxConnectionsPerUser: 10,
		EventBufferSize:       100,
	}

	ts := newTestSetup(config)
	defer ts.cleanup()

	userID := uuid.New().String()
	token := ts.generateToken(userID, "test@example.com")

	req := httptest.NewRequest("GET", "/api/v1/events/stream?token="+token, nil)
	w := newMockResponseWriter()
	done := make(chan struct{})

	go func() {
		defer close(done)
		ts.router.ServeHTTP(w, req)
	}()

	time.Sleep(50 * time.Millisecond)

	// Send various event types
	ts.eventRouter.RouteNewEmailEvent(userID, events.NewEmailEvent{
		ID:          "email-1",
		AliasID:     "alias-1",
		AliasEmail:  "test@example.com",
		FromAddress: "sender@example.com",
		PreviewText: "Test email",
		ReceivedAt:  time.Now(),
	})

	ts.eventRouter.RouteEmailDeletedEvent(userID, events.EmailDeletedEvent{
		ID:        "email-1",
		AliasID:   "alias-1",
		DeletedAt: time.Now(),
	})

	ts.eventRouter.RouteAliasCreatedEvent(userID, events.AliasCreatedEvent{
		ID:           "alias-2",
		EmailAddress: "new@example.com",
		DomainID:     "domain-1",
		CreatedAt:    time.Now(),
	})

	ts.eventRouter.RouteAliasDeletedEvent(userID, events.AliasDeletedEvent{
		ID:            "alias-1",
		EmailAddress:  "test@example.com",
		DeletedAt:     time.Now(),
		EmailsDeleted: 5,
	})

	ts.eventRouter.RouteDomainVerifiedEvent(userID, events.DomainVerifiedEvent{
		ID:         "domain-1",
		DomainName: "example.com",
		VerifiedAt: time.Now(),
		SSLStatus:  "active",
	})

	ts.eventRouter.RouteDomainDeletedEvent(userID, events.DomainDeletedEvent{
		ID:             "domain-1",
		DomainName:     "example.com",
		DeletedAt:      time.Now(),
		AliasesDeleted: 3,
		EmailsDeleted:  10,
	})

	time.Sleep(100 * time.Millisecond)

	body := w.Body.String()

	// Verify all event types were received
	expectedEvents := []string{
		"event: connected",
		"event: new_email",
		"event: email_deleted",
		"event: alias_created",
		"event: alias_deleted",
		"event: domain_verified",
		"event: domain_deleted",
	}

	for _, expected := range expectedEvents {
		if !strings.Contains(body, expected) {
			t.Errorf("expected to receive %s event", expected)
		}
	}

	// Cleanup
	for _, conn := range ts.connManager.GetConnections(userID) {
		conn.Close()
	}

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
	}
}

// Integration Test: Concurrent connections from same user
// Requirements: 1.5 - Multiple connections per user
func TestIntegration_ConcurrentConnectionsSameUser(t *testing.T) {
	config := Config{
		HeartbeatInterval:     1 * time.Hour,
		ConnectionTimeout:     1 * time.Hour,
		MaxConnectionsPerUser: 5,
		EventBufferSize:       100,
	}

	ts := newTestSetup(config)
	defer ts.cleanup()

	userID := uuid.New().String()
	token := ts.generateToken(userID, "test@example.com")

	numConnections := 3
	writers := make([]*mockResponseWriter, numConnections)
	dones := make([]chan struct{}, numConnections)

	// Create multiple connections concurrently
	var wg sync.WaitGroup
	for i := 0; i < numConnections; i++ {
		wg.Add(1)
		idx := i
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/api/v1/events/stream?token="+token, nil)
			w := newMockResponseWriter()
			writers[idx] = w
			done := make(chan struct{})
			dones[idx] = done

			go func() {
				defer close(done)
				ts.router.ServeHTTP(w, req)
			}()
		}()
	}

	wg.Wait()
	time.Sleep(100 * time.Millisecond)

	// All connections should be established
	if ts.connManager.CountConnections(userID) != numConnections {
		t.Errorf("expected %d connections, got %d", numConnections, ts.connManager.CountConnections(userID))
	}

	// Send an event - all connections should receive it
	emailEvent := events.NewEmailEvent{
		ID:          "email-broadcast",
		AliasID:     uuid.New().String(),
		AliasEmail:  "alias@example.com",
		FromAddress: "sender@example.com",
		PreviewText: "Broadcast email",
		ReceivedAt:  time.Now(),
	}
	ts.eventRouter.RouteNewEmailEvent(userID, emailEvent)

	time.Sleep(50 * time.Millisecond)

	// All connections should have received the event
	for i, w := range writers {
		if w == nil {
			continue
		}
		body := w.Body.String()
		if !strings.Contains(body, "email-broadcast") {
			t.Errorf("connection %d should receive broadcast event", i)
		}
	}

	// Cleanup
	for _, conn := range ts.connManager.GetConnections(userID) {
		conn.Close()
	}

	for _, done := range dones {
		if done != nil {
			select {
			case <-done:
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
}

// Integration Test: Dead connection cleanup
// Requirements: 2.3 - Detect dead connections and clean up resources
func TestIntegration_DeadConnectionCleanup(t *testing.T) {
	config := Config{
		HeartbeatInterval:     20 * time.Millisecond, // Short interval for testing
		ConnectionTimeout:     1 * time.Hour,
		MaxConnectionsPerUser: 10,
		EventBufferSize:       100,
	}

	ts := newTestSetup(config)
	defer ts.cleanup()

	userID := uuid.New().String()

	// Manually add a connection with old LastPing
	conn, _ := createTestConnection(userID)
	conn.LastPing = time.Now().Add(-1 * time.Hour) // Very old ping
	ts.connManager.AddConnection(userID, conn)

	// Verify connection exists
	if ts.connManager.CountConnections(userID) != 1 {
		t.Error("connection should exist initially")
	}

	// Wait for cleanup routine to run (3 * heartbeat interval)
	time.Sleep(100 * time.Millisecond)

	// Connection should be cleaned up
	if ts.connManager.CountConnections(userID) != 0 {
		t.Error("dead connection should be cleaned up")
	}

	// Connection should be closed
	if !conn.IsClosed() {
		t.Error("dead connection should be closed")
	}
}

// Integration Test: Authentication failure
// Requirements: 8.1 - Return 401 Unauthorized for invalid authentication
func TestIntegration_AuthenticationFailure(t *testing.T) {
	config := Config{
		HeartbeatInterval:     1 * time.Hour,
		ConnectionTimeout:     1 * time.Hour,
		MaxConnectionsPerUser: 10,
		EventBufferSize:       100,
	}

	ts := newTestSetup(config)
	defer ts.cleanup()

	testCases := []struct {
		name  string
		setup func(*http.Request)
	}{
		{
			name:  "no_token",
			setup: func(r *http.Request) {},
		},
		{
			name: "invalid_token",
			setup: func(r *http.Request) {
				r.URL.RawQuery = "token=invalid-token"
			},
		},
		{
			name: "empty_token",
			setup: func(r *http.Request) {
				r.URL.RawQuery = "token="
			},
		},
		{
			name: "invalid_header",
			setup: func(r *http.Request) {
				r.Header.Set("Authorization", "InvalidFormat")
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/v1/events/stream", nil)
			tc.setup(req)
			w := httptest.NewRecorder()

			ts.router.ServeHTTP(w, req)

			if w.Code != http.StatusUnauthorized {
				t.Errorf("expected status 401, got %d", w.Code)
			}

			// No connections should be established
			if ts.connManager.TotalConnections() != 0 {
				t.Error("no connections should be established for invalid auth")
			}
		})
	}
}

// Integration Test: Event store persistence
// Requirements: 7.3, 7.4 - Events should be stored for replay
func TestIntegration_EventStorePersistence(t *testing.T) {
	config := Config{
		HeartbeatInterval:     1 * time.Hour,
		ConnectionTimeout:     1 * time.Hour,
		MaxConnectionsPerUser: 10,
		EventBufferSize:       100,
	}

	ts := newTestSetup(config)
	defer ts.cleanup()

	userID := uuid.New().String()

	// Publish events without any active connections
	for i := 0; i < 5; i++ {
		emailEvent := events.NewEmailEvent{
			ID:          uuid.New().String(),
			AliasID:     uuid.New().String(),
			AliasEmail:  "alias@example.com",
			FromAddress: "sender@example.com",
			PreviewText: "Test email",
			ReceivedAt:  time.Now(),
		}
		ts.eventRouter.RouteNewEmailEvent(userID, emailEvent)
	}

	// Events should be stored
	storedEvents, err := ts.eventBus.GetEventsSince(userID, "")
	if err != nil {
		t.Fatalf("failed to get events: %v", err)
	}

	if len(storedEvents) != 5 {
		t.Errorf("expected 5 stored events, got %d", len(storedEvents))
	}

	// Events should be retrievable by ID
	if len(storedEvents) > 2 {
		eventsAfterSecond, _ := ts.eventBus.GetEventsSince(userID, storedEvents[1].ID)
		if len(eventsAfterSecond) != 3 {
			t.Errorf("expected 3 events after second, got %d", len(eventsAfterSecond))
		}
	}
}

// Integration Test: Context cancellation
// Requirements: 8.3 - Handle client disconnection gracefully
func TestIntegration_ContextCancellation(t *testing.T) {
	config := Config{
		HeartbeatInterval:     1 * time.Hour,
		ConnectionTimeout:     1 * time.Hour,
		MaxConnectionsPerUser: 10,
		EventBufferSize:       100,
	}

	ts := newTestSetup(config)
	defer ts.cleanup()

	userID := uuid.New().String()
	token := ts.generateToken(userID, "test@example.com")

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest("GET", "/api/v1/events/stream?token="+token, nil)
	req = req.WithContext(ctx)
	w := newMockResponseWriter()

	done := make(chan struct{})
	go func() {
		defer close(done)
		ts.router.ServeHTTP(w, req)
	}()

	// Wait for connection
	time.Sleep(50 * time.Millisecond)

	// Verify connection exists
	if ts.connManager.CountConnections(userID) != 1 {
		t.Error("connection should exist")
	}

	// Cancel context (simulate client disconnect)
	cancel()

	// Wait for handler to finish
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Error("handler should finish after context cancellation")
	}

	// Connection should be cleaned up
	time.Sleep(50 * time.Millisecond)
	if ts.connManager.CountConnections(userID) != 0 {
		t.Error("connection should be cleaned up after context cancellation")
	}
}
