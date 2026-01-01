// Package sse provides Server-Sent Events (SSE) functionality for real-time notifications.
package sse

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/auth"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/events"
)

// Handler implements the SSE handler for real-time notifications.
type Handler struct {
	config       Config
	connManager  *InMemoryConnectionManager
	eventBus     *events.InMemoryEventBus
	tokenService *auth.TokenService
}

// NewHandler creates a new SSE handler.
func NewHandler(config Config, connManager *InMemoryConnectionManager, eventBus *events.InMemoryEventBus, tokenService *auth.TokenService) *Handler {
	return &Handler{
		config:       config,
		connManager:  connManager,
		eventBus:     eventBus,
		tokenService: tokenService,
	}
}

// HandleStream handles an SSE stream request.
// It supports authentication via query parameter (token) or Authorization header.
func (h *Handler) HandleStream(w http.ResponseWriter, r *http.Request) {
	// Authenticate the request
	userID, err := h.authenticate(r)
	if err != nil {
		h.writeUnauthorized(w)
		return
	}

	// Check if streaming is supported
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	// Create connection
	connID := uuid.New().String()
	conn := &Connection{
		ID:        connID,
		UserID:    userID,
		Writer:    w,
		Flusher:   flusher,
		Done:      make(chan struct{}),
		CreatedAt: time.Now(),
		LastPing:  time.Now(),
	}

	// Add connection to manager
	if err := h.connManager.AddConnection(userID, conn); err != nil {
		http.Error(w, "Failed to establish connection", http.StatusInternalServerError)
		return
	}

	// Send connected event
	h.sendConnectedEvent(conn)

	// Handle Last-Event-ID for replay
	lastEventID := r.Header.Get("Last-Event-ID")
	if lastEventID != "" {
		h.replayEvents(conn, userID, lastEventID)
	}

	// Subscribe to events for this user
	unsubscribe := h.eventBus.Subscribe(userID, func(event events.Event) {
		h.sendEvent(conn, event)
	})
	defer unsubscribe()

	// Start heartbeat goroutine
	heartbeatDone := make(chan struct{})
	go h.heartbeatLoop(conn, heartbeatDone)

	// Wait for connection close or timeout
	ctx := r.Context()
	timeout := time.NewTimer(h.config.ConnectionTimeout)
	defer timeout.Stop()

	select {
	case <-ctx.Done():
		// Client disconnected
	case <-conn.Done:
		// Connection closed by server (e.g., limit exceeded)
	case <-timeout.C:
		// Connection timeout
	}

	// Cleanup
	close(heartbeatDone)
	h.connManager.RemoveConnection(userID, connID)
}

// authenticate extracts and validates the JWT token from the request.
// It supports both query parameter (token) and Authorization header.
func (h *Handler) authenticate(r *http.Request) (string, error) {
	var tokenString string

	// Try query parameter first
	tokenString = r.URL.Query().Get("token")

	// If not in query, try Authorization header
	if tokenString == "" {
		authHeader := r.Header.Get("Authorization")
		if authHeader != "" {
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
				tokenString = parts[1]
			}
		}
	}

	if tokenString == "" {
		return "", ErrInvalidToken
	}

	// Validate the token
	claims, err := h.tokenService.ValidateAccessToken(tokenString)
	if err != nil {
		return "", ErrInvalidToken
	}

	return claims.UserID(), nil
}

// writeUnauthorized writes a 401 Unauthorized response.
func (h *Handler) writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": false,
		"error": map[string]string{
			"code":    "AUTH_TOKEN_INVALID",
			"message": "Invalid or missing authentication token",
		},
		"timestamp": time.Now().UTC(),
	})
}

// sendConnectedEvent sends the connected event to a connection.
func (h *Handler) sendConnectedEvent(conn *Connection) {
	connectedData := events.ConnectedEvent{
		Timestamp: time.Now(),
		Message:   "Connected to real-time notifications",
	}

	data, err := json.Marshal(connectedData)
	if err != nil {
		return
	}

	event := events.Event{
		ID:        uuid.New().String(),
		Type:      events.EventTypeConnected,
		UserID:    conn.UserID,
		Data:      data,
		Timestamp: time.Now(),
	}

	h.sendEvent(conn, event)
}

// sendEvent sends an event to a connection.
func (h *Handler) sendEvent(conn *Connection, event events.Event) error {
	if conn.IsClosed() {
		return ErrConnectionClosed
	}

	// Format as SSE
	sseData := FormatSSEEvent(event)

	// Write to connection
	_, err := fmt.Fprint(conn.Writer, sseData)
	if err != nil {
		return err
	}

	// Flush the data
	conn.Flusher.Flush()
	return nil
}

// heartbeatLoop sends heartbeat events at regular intervals.
func (h *Handler) heartbeatLoop(conn *Connection, done <-chan struct{}) {
	ticker := time.NewTicker(h.config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-conn.Done:
			return
		case <-ticker.C:
			h.sendHeartbeat(conn)
		}
	}
}

// sendHeartbeat sends a heartbeat event to a connection.
func (h *Handler) sendHeartbeat(conn *Connection) {
	heartbeatData := events.HeartbeatEvent{
		Timestamp: time.Now(),
	}

	data, err := json.Marshal(heartbeatData)
	if err != nil {
		return
	}

	event := events.Event{
		ID:        uuid.New().String(),
		Type:      events.EventTypeHeartbeat,
		UserID:    conn.UserID,
		Data:      data,
		Timestamp: time.Now(),
	}

	if err := h.sendEvent(conn, event); err != nil {
		// Connection may be dead, mark for cleanup
		return
	}

	// Update last ping time
	h.connManager.UpdateLastPing(conn.UserID, conn.ID)
}

// replayEvents replays missed events to a reconnecting client.
func (h *Handler) replayEvents(conn *Connection, userID, lastEventID string) {
	missedEvents, err := h.eventBus.GetEventsSince(userID, lastEventID)
	if err != nil {
		return
	}

	for _, event := range missedEvents {
		if err := h.sendEvent(conn, event); err != nil {
			return
		}
	}
}

// FormatSSEEvent formats an event as an SSE message.
// Format: event: <type>\ndata: <json>\nid: <id>\n\n
func FormatSSEEvent(event events.Event) string {
	return fmt.Sprintf("event: %s\ndata: %s\nid: %s\n\n",
		event.Type,
		string(event.Data),
		event.ID,
	)
}
