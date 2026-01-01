package sse

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/auth"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/events"
	"pgregory.net/rapid"
)

// testTokenService creates a token service for testing.
func testTokenService() *auth.TokenService {
	return auth.NewTokenService(auth.TokenServiceConfig{
		AccessSecret:       "test-access-secret-key-32-bytes!",
		RefreshSecret:      "test-refresh-secret-key-32-bytes",
		AccessTokenExpiry:  15 * time.Minute,
		RefreshTokenExpiry: 7 * 24 * time.Hour,
		Issuer:             "test-issuer",
	})
}

// testHandler creates a handler for testing.
func testHandler() (*Handler, *InMemoryConnectionManager, *events.InMemoryEventBus) {
	config := DefaultConfig()
	eventStore := events.NewEventStore(100)
	eventBus := events.NewEventBus(eventStore)
	connManager := NewConnectionManager(config)
	tokenService := testTokenService()

	handler := NewHandler(config, connManager, eventBus, tokenService)
	return handler, connManager, eventBus
}

// Feature: realtime-notifications, Property 1: Connection Establishment
// *For any* valid authentication (via query parameter or Authorization header),
// the SSE_Server SHALL establish connection, send connected event, and set correct
// headers (Content-Type: text/event-stream, Cache-Control: no-cache).
// **Validates: Requirements 1.2, 1.3, 1.4**
func TestProperty1_ConnectionEstablishment(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate random user data
		userID := rapid.StringMatching(`[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`).Draw(t, "userID")
		email := rapid.StringMatching(`[a-z]{5,10}@[a-z]{5,10}\.[a-z]{2,3}`).Draw(t, "email")
		useQueryParam := rapid.Bool().Draw(t, "useQueryParam")

		handler, connManager, _ := testHandler()
		tokenService := testTokenService()

		// Generate valid token
		token, err := tokenService.GenerateAccessToken(userID, email)
		if err != nil {
			t.Fatalf("failed to generate token: %v", err)
		}

		// Create request with authentication
		var req *http.Request
		if useQueryParam {
			req = httptest.NewRequest("GET", "/api/v1/events/stream?token="+token, nil)
		} else {
			req = httptest.NewRequest("GET", "/api/v1/events/stream", nil)
			req.Header.Set("Authorization", "Bearer "+token)
		}

		// Create response recorder that supports flushing
		w := newMockResponseWriter()

		// Handle in goroutine since it blocks
		done := make(chan struct{})
		go func() {
			defer close(done)
			handler.HandleStream(w, req)
		}()

		// Wait a bit for connection to establish
		time.Sleep(50 * time.Millisecond)

		// Property 1: Connection should be established
		if connManager.CountConnections(userID) != 1 {
			t.Errorf("expected 1 connection for user %s, got %d", userID, connManager.CountConnections(userID))
		}

		// Property 2: Correct headers should be set
		contentType := w.Header().Get("Content-Type")
		if contentType != "text/event-stream" {
			t.Errorf("expected Content-Type: text/event-stream, got %s", contentType)
		}

		cacheControl := w.Header().Get("Cache-Control")
		if cacheControl != "no-cache" {
			t.Errorf("expected Cache-Control: no-cache, got %s", cacheControl)
		}

		// Property 3: Connected event should be sent
		body := w.Body.String()
		if !strings.Contains(body, "event: connected") {
			t.Error("connected event should be sent")
		}

		// Property 4: Connected event should have proper SSE format
		if !strings.Contains(body, "data: ") {
			t.Error("connected event should have data field")
		}
		if !strings.Contains(body, "id: ") {
			t.Error("connected event should have id field")
		}

		// Property 5: Connected event data should be valid JSON with timestamp and message
		lines := strings.Split(body, "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "data: ") {
				dataStr := strings.TrimPrefix(line, "data: ")
				var connectedEvent events.ConnectedEvent
				if err := json.Unmarshal([]byte(dataStr), &connectedEvent); err != nil {
					t.Errorf("connected event data should be valid JSON: %v", err)
				}
				if connectedEvent.Timestamp.IsZero() {
					t.Error("connected event should have timestamp")
				}
				if connectedEvent.Message == "" {
					t.Error("connected event should have message")
				}
				break
			}
		}

		// Cleanup: close connection
		conns := connManager.GetConnections(userID)
		for _, conn := range conns {
			conn.Close()
		}

		// Wait for handler to finish
		select {
		case <-done:
		case <-time.After(100 * time.Millisecond):
		}
	})
}

// Feature: realtime-notifications, Property 2: Authentication Enforcement
// *For any* request without valid authentication, the SSE_Server SHALL return
// 401 Unauthorized and not establish connection.
// **Validates: Requirements 8.1**
func TestProperty2_AuthenticationEnforcement(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate random invalid token scenarios
		invalidTokenType := rapid.IntRange(0, 4).Draw(t, "invalidTokenType")

		handler, connManager, _ := testHandler()

		var req *http.Request
		switch invalidTokenType {
		case 0:
			// No authentication at all
			req = httptest.NewRequest("GET", "/api/v1/events/stream", nil)
		case 1:
			// Empty token in query param
			req = httptest.NewRequest("GET", "/api/v1/events/stream?token=", nil)
		case 2:
			// Invalid token in query param
			invalidToken := rapid.StringMatching(`[a-zA-Z0-9]{20,50}`).Draw(t, "invalidToken")
			req = httptest.NewRequest("GET", "/api/v1/events/stream?token="+invalidToken, nil)
		case 3:
			// Empty Authorization header
			req = httptest.NewRequest("GET", "/api/v1/events/stream", nil)
			req.Header.Set("Authorization", "")
		case 4:
			// Invalid Authorization header format
			req = httptest.NewRequest("GET", "/api/v1/events/stream", nil)
			req.Header.Set("Authorization", "InvalidFormat")
		}

		w := httptest.NewRecorder()

		// Handle request
		handler.HandleStream(w, req)

		// Property 1: Should return 401 Unauthorized
		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected status 401, got %d", w.Code)
		}

		// Property 2: No connection should be established
		// Check all possible user IDs (there shouldn't be any)
		if connManager.TotalConnections() != 0 {
			t.Errorf("no connections should be established, got %d", connManager.TotalConnections())
		}

		// Property 3: Response should be JSON with error details
		var response map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Errorf("response should be valid JSON: %v", err)
		}

		// Property 4: Response should indicate failure
		if success, ok := response["success"].(bool); !ok || success {
			t.Error("response should have success: false")
		}

		// Property 5: Response should have error code
		if errorObj, ok := response["error"].(map[string]interface{}); ok {
			if code, ok := errorObj["code"].(string); !ok || code == "" {
				t.Error("response should have error code")
			}
		} else {
			t.Error("response should have error object")
		}
	})
}

// Test authentication with expired token
func TestProperty2_ExpiredToken(t *testing.T) {
	// Create token service with very short expiry
	tokenService := auth.NewTokenService(auth.TokenServiceConfig{
		AccessSecret:       "test-access-secret-key-32-bytes!",
		RefreshSecret:      "test-refresh-secret-key-32-bytes",
		AccessTokenExpiry:  1 * time.Millisecond, // Very short expiry
		RefreshTokenExpiry: 7 * 24 * time.Hour,
		Issuer:             "test-issuer",
	})

	config := DefaultConfig()
	eventStore := events.NewEventStore(100)
	eventBus := events.NewEventBus(eventStore)
	connManager := NewConnectionManager(config)
	handler := NewHandler(config, connManager, eventBus, tokenService)

	// Generate token
	userID := uuid.New().String()
	token, _ := tokenService.GenerateAccessToken(userID, "test@example.com")

	// Wait for token to expire
	time.Sleep(10 * time.Millisecond)

	// Create request with expired token
	req := httptest.NewRequest("GET", "/api/v1/events/stream?token="+token, nil)
	w := httptest.NewRecorder()

	handler.HandleStream(w, req)

	// Should return 401
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401 for expired token, got %d", w.Code)
	}

	// No connection should be established
	if connManager.TotalConnections() != 0 {
		t.Error("no connections should be established for expired token")
	}
}

// Test authentication with both query param and header (query param takes precedence)
func TestAuthentication_QueryParamPrecedence(t *testing.T) {
	handler, connManager, _ := testHandler()
	tokenService := testTokenService()

	userID := uuid.New().String()
	validToken, _ := tokenService.GenerateAccessToken(userID, "test@example.com")

	// Create request with valid token in query param and invalid in header
	req := httptest.NewRequest("GET", "/api/v1/events/stream?token="+validToken, nil)
	req.Header.Set("Authorization", "Bearer invalid-token")

	w := newMockResponseWriter()

	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.HandleStream(w, req)
	}()

	time.Sleep(50 * time.Millisecond)

	// Should establish connection (query param takes precedence)
	if connManager.CountConnections(userID) != 1 {
		t.Error("connection should be established with valid query param token")
	}

	// Cleanup
	conns := connManager.GetConnections(userID)
	for _, conn := range conns {
		conn.Close()
	}

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
	}
}

// Test authentication with header when no query param
func TestAuthentication_HeaderFallback(t *testing.T) {
	handler, connManager, _ := testHandler()
	tokenService := testTokenService()

	userID := uuid.New().String()
	validToken, _ := tokenService.GenerateAccessToken(userID, "test@example.com")

	// Create request with valid token only in header
	req := httptest.NewRequest("GET", "/api/v1/events/stream", nil)
	req.Header.Set("Authorization", "Bearer "+validToken)

	w := newMockResponseWriter()

	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.HandleStream(w, req)
	}()

	time.Sleep(50 * time.Millisecond)

	// Should establish connection
	if connManager.CountConnections(userID) != 1 {
		t.Error("connection should be established with valid header token")
	}

	// Cleanup
	conns := connManager.GetConnections(userID)
	for _, conn := range conns {
		conn.Close()
	}

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
	}
}

// Test SSE headers are set correctly
func TestSSEHeaders(t *testing.T) {
	handler, connManager, _ := testHandler()
	tokenService := testTokenService()

	userID := uuid.New().String()
	token, _ := tokenService.GenerateAccessToken(userID, "test@example.com")

	req := httptest.NewRequest("GET", "/api/v1/events/stream?token="+token, nil)
	w := newMockResponseWriter()

	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.HandleStream(w, req)
	}()

	time.Sleep(50 * time.Millisecond)

	// Check all required headers
	expectedHeaders := map[string]string{
		"Content-Type":      "text/event-stream",
		"Cache-Control":     "no-cache",
		"Connection":        "keep-alive",
		"X-Accel-Buffering": "no",
	}

	for header, expected := range expectedHeaders {
		actual := w.Header().Get(header)
		if actual != expected {
			t.Errorf("expected %s: %s, got %s", header, expected, actual)
		}
	}

	// Cleanup
	conns := connManager.GetConnections(userID)
	for _, conn := range conns {
		conn.Close()
	}

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
	}
}

// Test FormatSSEEvent function
func TestFormatSSEEvent(t *testing.T) {
	event := events.Event{
		ID:        "test-event-id",
		Type:      "test_event",
		UserID:    "test-user",
		Data:      json.RawMessage(`{"key":"value"}`),
		Timestamp: time.Now(),
	}

	formatted := FormatSSEEvent(event)

	// Check format
	if !strings.Contains(formatted, "event: test_event\n") {
		t.Error("formatted event should contain event type")
	}
	if !strings.Contains(formatted, "data: {\"key\":\"value\"}\n") {
		t.Error("formatted event should contain data")
	}
	if !strings.Contains(formatted, "id: test-event-id\n") {
		t.Error("formatted event should contain id")
	}
	if !strings.HasSuffix(formatted, "\n\n") {
		t.Error("formatted event should end with double newline")
	}
}


// Feature: realtime-notifications, Property 4: Heartbeat Delivery
// *For any* active connection, the SSE_Server SHALL send heartbeat event every 30 seconds
// with timestamp. Dead connections SHALL be detected and cleaned up.
// **Validates: Requirements 2.1, 2.2, 2.3**
func TestProperty4_HeartbeatDelivery(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate random user data
		userID := rapid.StringMatching(`[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`).Draw(t, "userID")
		email := rapid.StringMatching(`[a-z]{5,10}@[a-z]{5,10}\.[a-z]{2,3}`).Draw(t, "email")

		// Use short heartbeat interval for testing
		config := Config{
			HeartbeatInterval:     50 * time.Millisecond, // Short interval for testing
			ConnectionTimeout:     1 * time.Hour,
			MaxConnectionsPerUser: 10,
			EventBufferSize:       100,
		}

		eventStore := events.NewEventStore(100)
		eventBus := events.NewEventBus(eventStore)
		connManager := NewConnectionManager(config)
		tokenService := testTokenService()
		handler := NewHandler(config, connManager, eventBus, tokenService)

		// Generate valid token
		token, err := tokenService.GenerateAccessToken(userID, email)
		if err != nil {
			t.Fatalf("failed to generate token: %v", err)
		}

		// Create request
		req := httptest.NewRequest("GET", "/api/v1/events/stream?token="+token, nil)
		w := newMockResponseWriter()

		// Handle in goroutine
		done := make(chan struct{})
		go func() {
			defer close(done)
			handler.HandleStream(w, req)
		}()

		// Wait for at least 2 heartbeats
		time.Sleep(150 * time.Millisecond)

		// Get response body
		body := w.Body.String()

		// Property 1: Heartbeat events should be sent
		heartbeatCount := strings.Count(body, "event: heartbeat")
		if heartbeatCount < 2 {
			t.Errorf("expected at least 2 heartbeat events, got %d", heartbeatCount)
		}

		// Property 2: Each heartbeat should have timestamp
		lines := strings.Split(body, "\n")
		for i, line := range lines {
			if strings.HasPrefix(line, "event: heartbeat") {
				// Find the data line (should be next non-empty line)
				for j := i + 1; j < len(lines); j++ {
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

		// Property 3: Each heartbeat should have unique ID
		ids := make(map[string]bool)
		for i, line := range lines {
			if strings.HasPrefix(line, "event: heartbeat") {
				// Find the id line
				for j := i + 1; j < len(lines) && j < i+4; j++ {
					if strings.HasPrefix(lines[j], "id: ") {
						id := strings.TrimPrefix(lines[j], "id: ")
						if ids[id] {
							t.Error("heartbeat IDs should be unique")
						}
						ids[id] = true
						break
					}
				}
			}
		}

		// Cleanup
		conns := connManager.GetConnections(userID)
		for _, conn := range conns {
			conn.Close()
		}

		select {
		case <-done:
		case <-time.After(100 * time.Millisecond):
		}
	})
}

// Test dead connection detection and cleanup
func TestProperty4_DeadConnectionCleanup(t *testing.T) {
	config := Config{
		HeartbeatInterval:     10 * time.Millisecond, // Very short for testing
		ConnectionTimeout:     1 * time.Hour,
		MaxConnectionsPerUser: 10,
		EventBufferSize:       100,
	}

	connManager := NewConnectionManager(config)
	userID := uuid.New().String()

	// Create a connection
	conn, _ := createTestConnection(userID)
	connManager.AddConnection(userID, conn)

	// Simulate dead connection by setting old LastPing
	conn.LastPing = time.Now().Add(-1 * time.Hour)

	// Start cleanup routine
	stop := connManager.StartCleanupRoutine(20 * time.Millisecond)
	defer stop()

	// Wait for cleanup
	time.Sleep(100 * time.Millisecond)

	// Property: Dead connection should be cleaned up
	if connManager.CountConnections(userID) != 0 {
		t.Error("dead connection should be cleaned up")
	}

	// Property: Connection should be closed
	if !conn.IsClosed() {
		t.Error("dead connection should be closed")
	}
}

// Test heartbeat updates LastPing
func TestHeartbeat_UpdatesLastPing(t *testing.T) {
	config := Config{
		HeartbeatInterval:     30 * time.Millisecond,
		ConnectionTimeout:     1 * time.Hour,
		MaxConnectionsPerUser: 10,
		EventBufferSize:       100,
	}

	eventStore := events.NewEventStore(100)
	eventBus := events.NewEventBus(eventStore)
	connManager := NewConnectionManager(config)
	tokenService := testTokenService()
	handler := NewHandler(config, connManager, eventBus, tokenService)

	userID := uuid.New().String()
	token, _ := tokenService.GenerateAccessToken(userID, "test@example.com")

	req := httptest.NewRequest("GET", "/api/v1/events/stream?token="+token, nil)
	w := newMockResponseWriter()

	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.HandleStream(w, req)
	}()

	// Wait for connection to establish
	time.Sleep(20 * time.Millisecond)

	// Get initial LastPing
	conns := connManager.GetConnections(userID)
	if len(conns) == 0 {
		t.Fatal("no connections found")
	}
	initialPing := conns[0].LastPing

	// Wait for heartbeat
	time.Sleep(50 * time.Millisecond)

	// Get updated LastPing
	conns = connManager.GetConnections(userID)
	if len(conns) == 0 {
		t.Fatal("connection was closed unexpectedly")
	}
	updatedPing := conns[0].LastPing

	// Property: LastPing should be updated after heartbeat
	if !updatedPing.After(initialPing) {
		t.Error("LastPing should be updated after heartbeat")
	}

	// Cleanup
	for _, conn := range conns {
		conn.Close()
	}

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
	}
}


// Feature: realtime-notifications, Property 9: SSE Format Compliance
// *For any* event sent, the format SHALL comply with SSE specification:
// event type line, data line (JSON), and unique id line.
// **Validates: Requirements 7.1, 7.2**
func TestProperty9_SSEFormatCompliance(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate random event data
		eventID := rapid.StringMatching(`[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`).Draw(t, "eventID")
		eventType := rapid.SampledFrom([]string{
			events.EventTypeConnected,
			events.EventTypeHeartbeat,
			events.EventTypeNewEmail,
			events.EventTypeEmailDeleted,
			events.EventTypeAliasCreated,
			events.EventTypeAliasDeleted,
			events.EventTypeDomainVerified,
			events.EventTypeDomainDeleted,
			events.EventTypeConnectionLimit,
		}).Draw(t, "eventType")
		userID := rapid.StringMatching(`[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`).Draw(t, "userID")

		// Generate random JSON data
		dataKey := rapid.StringMatching(`[a-z]{3,10}`).Draw(t, "dataKey")
		dataValue := rapid.StringMatching(`[a-zA-Z0-9]{5,20}`).Draw(t, "dataValue")
		jsonData := json.RawMessage(fmt.Sprintf(`{"%s":"%s"}`, dataKey, dataValue))

		event := events.Event{
			ID:        eventID,
			Type:      eventType,
			UserID:    userID,
			Data:      jsonData,
			Timestamp: time.Now(),
		}

		formatted := FormatSSEEvent(event)

		// Property 1: Should contain event type line
		expectedEventLine := fmt.Sprintf("event: %s\n", eventType)
		if !strings.Contains(formatted, expectedEventLine) {
			t.Errorf("formatted event should contain event type line: %s", expectedEventLine)
		}

		// Property 2: Should contain data line with JSON
		expectedDataLine := fmt.Sprintf("data: %s\n", string(jsonData))
		if !strings.Contains(formatted, expectedDataLine) {
			t.Errorf("formatted event should contain data line: %s", expectedDataLine)
		}

		// Property 3: Should contain id line
		expectedIDLine := fmt.Sprintf("id: %s\n", eventID)
		if !strings.Contains(formatted, expectedIDLine) {
			t.Errorf("formatted event should contain id line: %s", expectedIDLine)
		}

		// Property 4: Should end with double newline (event separator)
		if !strings.HasSuffix(formatted, "\n\n") {
			t.Error("formatted event should end with double newline")
		}

		// Property 5: Lines should be in correct order (event, data, id)
		eventIdx := strings.Index(formatted, "event:")
		dataIdx := strings.Index(formatted, "data:")
		idIdx := strings.Index(formatted, "id:")

		if eventIdx == -1 || dataIdx == -1 || idIdx == -1 {
			t.Error("formatted event should contain all required fields")
		}

		if eventIdx > dataIdx || dataIdx > idIdx {
			t.Error("SSE fields should be in order: event, data, id")
		}

		// Property 6: Data should be valid JSON
		lines := strings.Split(formatted, "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "data: ") {
				dataStr := strings.TrimPrefix(line, "data: ")
				var parsed map[string]interface{}
				if err := json.Unmarshal([]byte(dataStr), &parsed); err != nil {
					t.Errorf("data should be valid JSON: %v", err)
				}
				break
			}
		}
	})
}

// Test SSE format with various event types
func TestSSEFormat_AllEventTypes(t *testing.T) {
	eventTypes := []struct {
		eventType string
		data      interface{}
	}{
		{events.EventTypeConnected, events.ConnectedEvent{Timestamp: time.Now(), Message: "Connected"}},
		{events.EventTypeHeartbeat, events.HeartbeatEvent{Timestamp: time.Now()}},
		{events.EventTypeNewEmail, events.NewEmailEvent{ID: "1", AliasID: "2", AliasEmail: "test@example.com", FromAddress: "sender@example.com", PreviewText: "Hello", ReceivedAt: time.Now(), HasAttachments: false, SizeBytes: 100}},
		{events.EventTypeEmailDeleted, events.EmailDeletedEvent{ID: "1", AliasID: "2", DeletedAt: time.Now()}},
		{events.EventTypeAliasCreated, events.AliasCreatedEvent{ID: "1", EmailAddress: "test@example.com", DomainID: "2", CreatedAt: time.Now()}},
		{events.EventTypeAliasDeleted, events.AliasDeletedEvent{ID: "1", EmailAddress: "test@example.com", DeletedAt: time.Now(), EmailsDeleted: 5}},
		{events.EventTypeDomainVerified, events.DomainVerifiedEvent{ID: "1", DomainName: "example.com", VerifiedAt: time.Now(), SSLStatus: "active"}},
		{events.EventTypeDomainDeleted, events.DomainDeletedEvent{ID: "1", DomainName: "example.com", DeletedAt: time.Now(), AliasesDeleted: 3, EmailsDeleted: 10}},
		{events.EventTypeConnectionLimit, events.ConnectionLimitEvent{Message: "Limit exceeded", MaxConnections: 10}},
	}

	for _, tc := range eventTypes {
		t.Run(tc.eventType, func(t *testing.T) {
			data, err := json.Marshal(tc.data)
			if err != nil {
				t.Fatalf("failed to marshal data: %v", err)
			}

			event := events.Event{
				ID:        uuid.New().String(),
				Type:      tc.eventType,
				UserID:    "test-user",
				Data:      data,
				Timestamp: time.Now(),
			}

			formatted := FormatSSEEvent(event)

			// Verify format
			if !strings.Contains(formatted, "event: "+tc.eventType) {
				t.Errorf("missing event type: %s", tc.eventType)
			}
			if !strings.Contains(formatted, "data: ") {
				t.Error("missing data field")
			}
			if !strings.Contains(formatted, "id: ") {
				t.Error("missing id field")
			}
			if !strings.HasSuffix(formatted, "\n\n") {
				t.Error("missing double newline")
			}
		})
	}
}

// Test SSE format with special characters in data
func TestSSEFormat_SpecialCharacters(t *testing.T) {
	testCases := []struct {
		name string
		data string
	}{
		{"unicode", `{"message":"Hello 世界 🌍"}`},
		{"newlines_escaped", `{"message":"Line1\\nLine2"}`},
		{"quotes", `{"message":"He said \"hello\""}`},
		{"backslash", `{"path":"C:\\Users\\test"}`},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			event := events.Event{
				ID:        uuid.New().String(),
				Type:      events.EventTypeConnected,
				UserID:    "test-user",
				Data:      json.RawMessage(tc.data),
				Timestamp: time.Now(),
			}

			formatted := FormatSSEEvent(event)

			// Data should be preserved exactly
			if !strings.Contains(formatted, "data: "+tc.data) {
				t.Errorf("data should be preserved: expected %s", tc.data)
			}
		})
	}
}

// Test SSE format with empty data
func TestSSEFormat_EmptyData(t *testing.T) {
	event := events.Event{
		ID:        uuid.New().String(),
		Type:      events.EventTypeHeartbeat,
		UserID:    "test-user",
		Data:      json.RawMessage(`{}`),
		Timestamp: time.Now(),
	}

	formatted := FormatSSEEvent(event)

	if !strings.Contains(formatted, "data: {}") {
		t.Error("empty JSON object should be preserved")
	}
}


// Test Last-Event-ID handling for event replay
func TestLastEventID_Replay(t *testing.T) {
	config := Config{
		HeartbeatInterval:     1 * time.Hour, // Long interval to avoid interference
		ConnectionTimeout:     1 * time.Hour,
		MaxConnectionsPerUser: 10,
		EventBufferSize:       100,
	}

	eventStore := events.NewEventStore(100)
	eventBus := events.NewEventBus(eventStore)
	connManager := NewConnectionManager(config)
	tokenService := testTokenService()
	handler := NewHandler(config, connManager, eventBus, tokenService)

	userID := uuid.New().String()
	token, _ := tokenService.GenerateAccessToken(userID, "test@example.com")

	// Store some events in the event bus
	event1Data, _ := json.Marshal(events.NewEmailEvent{
		ID:          "email-1",
		AliasID:     "alias-1",
		AliasEmail:  "test@example.com",
		FromAddress: "sender@example.com",
		PreviewText: "First email",
		ReceivedAt:  time.Now(),
	})
	event1 := events.Event{
		ID:        "evt-1",
		Type:      events.EventTypeNewEmail,
		UserID:    userID,
		Data:      event1Data,
		Timestamp: time.Now(),
	}
	eventBus.Publish(event1)

	event2Data, _ := json.Marshal(events.NewEmailEvent{
		ID:          "email-2",
		AliasID:     "alias-1",
		AliasEmail:  "test@example.com",
		FromAddress: "sender2@example.com",
		PreviewText: "Second email",
		ReceivedAt:  time.Now(),
	})
	event2 := events.Event{
		ID:        "evt-2",
		Type:      events.EventTypeNewEmail,
		UserID:    userID,
		Data:      event2Data,
		Timestamp: time.Now(),
	}
	eventBus.Publish(event2)

	// Create request with Last-Event-ID header
	req := httptest.NewRequest("GET", "/api/v1/events/stream?token="+token, nil)
	req.Header.Set("Last-Event-ID", "evt-1")
	w := newMockResponseWriter()

	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.HandleStream(w, req)
	}()

	// Wait for connection and replay
	time.Sleep(100 * time.Millisecond)

	body := w.Body.String()

	// Should contain connected event
	if !strings.Contains(body, "event: connected") {
		t.Error("should contain connected event")
	}

	// Should contain replayed event (evt-2, which is after evt-1)
	if !strings.Contains(body, "id: evt-2") {
		t.Error("should replay event after Last-Event-ID")
	}

	// Should NOT contain evt-1 (it's the last event ID, not after it)
	// Note: The event store returns events AFTER the given ID
	lines := strings.Split(body, "\n")
	evt1Count := 0
	for _, line := range lines {
		if strings.Contains(line, "id: evt-1") {
			evt1Count++
		}
	}
	// evt-1 should not be replayed (it's the last received event)
	if evt1Count > 0 {
		t.Error("should not replay the Last-Event-ID itself")
	}

	// Cleanup
	conns := connManager.GetConnections(userID)
	for _, conn := range conns {
		conn.Close()
	}

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
	}
}

// Test Last-Event-ID with no events to replay
func TestLastEventID_NoEventsToReplay(t *testing.T) {
	config := Config{
		HeartbeatInterval:     1 * time.Hour,
		ConnectionTimeout:     1 * time.Hour,
		MaxConnectionsPerUser: 10,
		EventBufferSize:       100,
	}

	eventStore := events.NewEventStore(100)
	eventBus := events.NewEventBus(eventStore)
	connManager := NewConnectionManager(config)
	tokenService := testTokenService()
	handler := NewHandler(config, connManager, eventBus, tokenService)

	userID := uuid.New().String()
	token, _ := tokenService.GenerateAccessToken(userID, "test@example.com")

	// Create request with Last-Event-ID but no events stored
	req := httptest.NewRequest("GET", "/api/v1/events/stream?token="+token, nil)
	req.Header.Set("Last-Event-ID", "non-existent-event")
	w := newMockResponseWriter()

	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.HandleStream(w, req)
	}()

	time.Sleep(50 * time.Millisecond)

	body := w.Body.String()

	// Should still connect successfully
	if !strings.Contains(body, "event: connected") {
		t.Error("should connect even with non-existent Last-Event-ID")
	}

	// Connection should be established
	if connManager.CountConnections(userID) != 1 {
		t.Error("connection should be established")
	}

	// Cleanup
	conns := connManager.GetConnections(userID)
	for _, conn := range conns {
		conn.Close()
	}

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
	}
}

// Test Last-Event-ID without header (no replay)
func TestLastEventID_NoHeader(t *testing.T) {
	config := Config{
		HeartbeatInterval:     1 * time.Hour,
		ConnectionTimeout:     1 * time.Hour,
		MaxConnectionsPerUser: 10,
		EventBufferSize:       100,
	}

	eventStore := events.NewEventStore(100)
	eventBus := events.NewEventBus(eventStore)
	connManager := NewConnectionManager(config)
	tokenService := testTokenService()
	handler := NewHandler(config, connManager, eventBus, tokenService)

	userID := uuid.New().String()
	token, _ := tokenService.GenerateAccessToken(userID, "test@example.com")

	// Store an event
	eventData, _ := json.Marshal(events.NewEmailEvent{
		ID:          "email-1",
		AliasID:     "alias-1",
		AliasEmail:  "test@example.com",
		FromAddress: "sender@example.com",
		PreviewText: "Test email",
		ReceivedAt:  time.Now(),
	})
	event := events.Event{
		ID:        "evt-1",
		Type:      events.EventTypeNewEmail,
		UserID:    userID,
		Data:      eventData,
		Timestamp: time.Now(),
	}
	eventBus.Publish(event)

	// Create request WITHOUT Last-Event-ID header
	req := httptest.NewRequest("GET", "/api/v1/events/stream?token="+token, nil)
	w := newMockResponseWriter()

	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.HandleStream(w, req)
	}()

	time.Sleep(50 * time.Millisecond)

	body := w.Body.String()

	// Should contain connected event
	if !strings.Contains(body, "event: connected") {
		t.Error("should contain connected event")
	}

	// Should NOT replay old events without Last-Event-ID
	if strings.Contains(body, "id: evt-1") {
		t.Error("should not replay events without Last-Event-ID header")
	}

	// Cleanup
	conns := connManager.GetConnections(userID)
	for _, conn := range conns {
		conn.Close()
	}

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
	}
}
