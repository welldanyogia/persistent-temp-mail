// Package sse provides Server-Sent Events (SSE) functionality for real-time notifications.
package sse

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/events"
)

// InMemoryConnectionManager implements ConnectionManager using in-memory storage.
type InMemoryConnectionManager struct {
	mu          sync.RWMutex
	connections map[string]map[string]*Connection // userID -> connID -> Connection
	config      Config
}

// NewConnectionManager creates a new InMemoryConnectionManager with the given config.
func NewConnectionManager(config Config) *InMemoryConnectionManager {
	return &InMemoryConnectionManager{
		connections: make(map[string]map[string]*Connection),
		config:      config,
	}
}

// AddConnection adds a new connection for a user.
// If the user has reached the connection limit, the oldest connection is closed
// and a connection_limit event is sent to it before removal.
// Returns error only for critical failures, not for limit enforcement.
func (cm *InMemoryConnectionManager) AddConnection(userID string, conn *Connection) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.connections[userID] == nil {
		cm.connections[userID] = make(map[string]*Connection)
	}

	userConns := cm.connections[userID]

	// Check if we need to close the oldest connection
	if len(userConns) >= cm.config.MaxConnectionsPerUser {
		// Find and close the oldest connection
		oldestConn := cm.findOldestConnectionLocked(userID)
		if oldestConn != nil {
			// Send connection_limit event to the oldest connection before closing
			cm.sendConnectionLimitEventLocked(oldestConn)
			oldestConn.Close()
			delete(userConns, oldestConn.ID)
		}
	}

	// Add the new connection
	userConns[conn.ID] = conn
	return nil
}

// RemoveConnection removes a connection for a user.
func (cm *InMemoryConnectionManager) RemoveConnection(userID string, connID string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if userConns, exists := cm.connections[userID]; exists {
		if conn, connExists := userConns[connID]; connExists {
			conn.Close()
			delete(userConns, connID)
		}
		// Clean up empty user map
		if len(userConns) == 0 {
			delete(cm.connections, userID)
		}
	}
}

// GetConnections returns all active connections for a user.
func (cm *InMemoryConnectionManager) GetConnections(userID string) []*Connection {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	userConns, exists := cm.connections[userID]
	if !exists {
		return []*Connection{}
	}

	result := make([]*Connection, 0, len(userConns))
	for _, conn := range userConns {
		if !conn.IsClosed() {
			result = append(result, conn)
		}
	}
	return result
}

// CountConnections returns the number of active connections for a user.
func (cm *InMemoryConnectionManager) CountConnections(userID string) int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	userConns, exists := cm.connections[userID]
	if !exists {
		return 0
	}

	count := 0
	for _, conn := range userConns {
		if !conn.IsClosed() {
			count++
		}
	}
	return count
}

// Broadcast sends an event to all connections for a user.
func (cm *InMemoryConnectionManager) Broadcast(userID string, event events.Event) error {
	cm.mu.RLock()
	conns := cm.GetConnectionsLocked(userID)
	cm.mu.RUnlock()

	if len(conns) == 0 {
		return nil // No connections, not an error
	}

	for _, conn := range conns {
		if err := cm.sendEventToConnection(conn, event); err != nil {
			// Log error but continue broadcasting to other connections
			// Connection will be cleaned up by CleanupDeadConnections
			continue
		}
	}

	return nil
}

// GetConnectionsLocked returns connections without acquiring lock (caller must hold lock).
func (cm *InMemoryConnectionManager) GetConnectionsLocked(userID string) []*Connection {
	userConns, exists := cm.connections[userID]
	if !exists {
		return []*Connection{}
	}

	result := make([]*Connection, 0, len(userConns))
	for _, conn := range userConns {
		if !conn.IsClosed() {
			result = append(result, conn)
		}
	}
	return result
}

// CleanupDeadConnections removes connections that are closed or unresponsive.
func (cm *InMemoryConnectionManager) CleanupDeadConnections() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	for userID, userConns := range cm.connections {
		for connID, conn := range userConns {
			if conn.IsClosed() || cm.isConnectionDead(conn) {
				conn.Close()
				delete(userConns, connID)
			}
		}
		// Clean up empty user map
		if len(userConns) == 0 {
			delete(cm.connections, userID)
		}
	}
}


// isConnectionDead checks if a connection is unresponsive.
// A connection is considered dead if it hasn't received a ping response
// within 3 heartbeat intervals.
func (cm *InMemoryConnectionManager) isConnectionDead(conn *Connection) bool {
	deadThreshold := cm.config.HeartbeatInterval * 3
	return time.Since(conn.LastPing) > deadThreshold
}

// findOldestConnectionLocked finds the oldest connection for a user.
// Caller must hold the lock.
func (cm *InMemoryConnectionManager) findOldestConnectionLocked(userID string) *Connection {
	userConns, exists := cm.connections[userID]
	if !exists || len(userConns) == 0 {
		return nil
	}

	// Sort connections by creation time
	conns := make([]*Connection, 0, len(userConns))
	for _, conn := range userConns {
		conns = append(conns, conn)
	}

	sort.Slice(conns, func(i, j int) bool {
		return conns[i].CreatedAt.Before(conns[j].CreatedAt)
	})

	return conns[0]
}

// sendConnectionLimitEventLocked sends a connection_limit event to a connection.
// Caller must hold the lock.
func (cm *InMemoryConnectionManager) sendConnectionLimitEventLocked(conn *Connection) {
	limitEvent := events.ConnectionLimitEvent{
		Message:        "Maximum connections exceeded, closing oldest connection",
		MaxConnections: cm.config.MaxConnectionsPerUser,
	}

	data, err := json.Marshal(limitEvent)
	if err != nil {
		return
	}

	event := events.Event{
		ID:        uuid.New().String(),
		Type:      events.EventTypeConnectionLimit,
		UserID:    conn.UserID,
		Data:      data,
		Timestamp: time.Now(),
	}

	// Best effort - ignore errors since connection is being closed anyway
	_ = cm.sendEventToConnection(conn, event)
}

// sendEventToConnection sends an event to a specific connection.
func (cm *InMemoryConnectionManager) sendEventToConnection(conn *Connection, event events.Event) error {
	if conn.IsClosed() {
		return ErrConnectionClosed
	}

	// Format as SSE
	sseData := formatSSEEvent(event)

	// Write to connection
	_, err := fmt.Fprint(conn.Writer, sseData)
	if err != nil {
		return err
	}

	// Flush the data
	conn.Flusher.Flush()
	return nil
}

// formatSSEEvent formats an event as an SSE message.
func formatSSEEvent(event events.Event) string {
	return fmt.Sprintf("event: %s\ndata: %s\nid: %s\n\n",
		event.Type,
		string(event.Data),
		event.ID,
	)
}

// TotalConnections returns the total number of connections across all users.
// Useful for monitoring.
func (cm *InMemoryConnectionManager) TotalConnections() int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	total := 0
	for _, userConns := range cm.connections {
		for _, conn := range userConns {
			if !conn.IsClosed() {
				total++
			}
		}
	}
	return total
}

// UpdateLastPing updates the last ping time for a connection.
func (cm *InMemoryConnectionManager) UpdateLastPing(userID, connID string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if userConns, exists := cm.connections[userID]; exists {
		if conn, connExists := userConns[connID]; connExists {
			conn.LastPing = time.Now()
		}
	}
}

// GetConnection returns a specific connection by user ID and connection ID.
func (cm *InMemoryConnectionManager) GetConnection(userID, connID string) *Connection {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if userConns, exists := cm.connections[userID]; exists {
		if conn, connExists := userConns[connID]; connExists {
			return conn
		}
	}
	return nil
}


// StartCleanupRoutine starts a background goroutine that periodically cleans up dead connections.
// Returns a stop function to terminate the cleanup routine.
func (cm *InMemoryConnectionManager) StartCleanupRoutine(interval time.Duration) (stop func()) {
	ticker := time.NewTicker(interval)
	done := make(chan struct{})

	go func() {
		for {
			select {
			case <-ticker.C:
				cm.CleanupDeadConnections()
			case <-done:
				ticker.Stop()
				return
			}
		}
	}()

	return func() {
		close(done)
	}
}

// CleanupTimedOutConnections removes connections that have exceeded the connection timeout.
func (cm *InMemoryConnectionManager) CleanupTimedOutConnections() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	for userID, userConns := range cm.connections {
		for connID, conn := range userConns {
			if time.Since(conn.CreatedAt) > cm.config.ConnectionTimeout {
				conn.Close()
				delete(userConns, connID)
			}
		}
		// Clean up empty user map
		if len(userConns) == 0 {
			delete(cm.connections, userID)
		}
	}
}

// MarkConnectionAlive updates the LastPing time for a connection to indicate it's still responsive.
// This should be called when a heartbeat response is received or when any activity occurs.
func (cm *InMemoryConnectionManager) MarkConnectionAlive(userID, connID string) bool {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if userConns, exists := cm.connections[userID]; exists {
		if conn, connExists := userConns[connID]; connExists {
			conn.LastPing = time.Now()
			return true
		}
	}
	return false
}

// GetDeadConnections returns a list of connections that are considered dead.
// Useful for monitoring and debugging.
func (cm *InMemoryConnectionManager) GetDeadConnections() []*Connection {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	var deadConns []*Connection
	for _, userConns := range cm.connections {
		for _, conn := range userConns {
			if conn.IsClosed() || cm.isConnectionDead(conn) {
				deadConns = append(deadConns, conn)
			}
		}
	}
	return deadConns
}

// GetTimedOutConnections returns a list of connections that have exceeded the timeout.
// Useful for monitoring and debugging.
func (cm *InMemoryConnectionManager) GetTimedOutConnections() []*Connection {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	var timedOutConns []*Connection
	for _, userConns := range cm.connections {
		for _, conn := range userConns {
			if time.Since(conn.CreatedAt) > cm.config.ConnectionTimeout {
				timedOutConns = append(timedOutConns, conn)
			}
		}
	}
	return timedOutConns
}
