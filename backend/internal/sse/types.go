// Package sse provides Server-Sent Events (SSE) functionality for real-time notifications.
package sse

import (
	"net/http"
	"time"

	"github.com/welldanyogia/persistent-temp-mail/backend/internal/events"
)

// Config holds SSE server configuration.
type Config struct {
	HeartbeatInterval     time.Duration // Default: 30 seconds
	ConnectionTimeout     time.Duration // Default: 1 hour
	MaxConnectionsPerUser int           // Default: 10
	EventBufferSize       int           // Default: 100 events per user
}

// DefaultConfig returns the default SSE configuration.
func DefaultConfig() Config {
	return Config{
		HeartbeatInterval:     30 * time.Second,
		ConnectionTimeout:     1 * time.Hour,
		MaxConnectionsPerUser: 10,
		EventBufferSize:       100,
	}
}

// Connection represents an active SSE connection.
type Connection struct {
	ID        string
	UserID    string
	Writer    http.ResponseWriter
	Flusher   http.Flusher
	Done      chan struct{}
	CreatedAt time.Time
	LastPing  time.Time
}

// NewConnection creates a new SSE connection.
func NewConnection(id, userID string, w http.ResponseWriter) (*Connection, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, ErrStreamingNotSupported
	}

	return &Connection{
		ID:        id,
		UserID:    userID,
		Writer:    w,
		Flusher:   flusher,
		Done:      make(chan struct{}),
		CreatedAt: time.Now(),
		LastPing:  time.Now(),
	}, nil
}

// Close closes the connection.
func (c *Connection) Close() {
	select {
	case <-c.Done:
		// Already closed
	default:
		close(c.Done)
	}
}

// IsClosed returns true if the connection is closed.
func (c *Connection) IsClosed() bool {
	select {
	case <-c.Done:
		return true
	default:
		return false
	}
}

// ConnectionManager defines the interface for managing SSE connections.
type ConnectionManager interface {
	// AddConnection adds a new connection for a user.
	// Returns error if connection limit is exceeded.
	AddConnection(userID string, conn *Connection) error
	// RemoveConnection removes a connection.
	RemoveConnection(userID string, connID string)
	// GetConnections returns all connections for a user.
	GetConnections(userID string) []*Connection
	// CountConnections returns the number of connections for a user.
	CountConnections(userID string) int
	// Broadcast sends an event to all connections for a user.
	Broadcast(userID string, event events.Event) error
	// CleanupDeadConnections removes dead connections.
	CleanupDeadConnections()
}

// SSEHandler defines the interface for handling SSE requests.
type SSEHandler interface {
	// HandleStream handles an SSE stream request.
	HandleStream(w http.ResponseWriter, r *http.Request)
}
