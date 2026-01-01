package sse

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"pgregory.net/rapid"
)

// mockResponseWriter implements http.ResponseWriter and http.Flusher for testing.
type mockResponseWriter struct {
	*httptest.ResponseRecorder
	flushed bool
}

func newMockResponseWriter() *mockResponseWriter {
	return &mockResponseWriter{
		ResponseRecorder: httptest.NewRecorder(),
	}
}

func (m *mockResponseWriter) Flush() {
	m.flushed = true
}

// createTestConnection creates a test connection with a mock response writer.
func createTestConnection(userID string) (*Connection, *mockResponseWriter) {
	w := newMockResponseWriter()
	conn, _ := NewConnection(uuid.New().String(), userID, w)
	return conn, w
}

// Feature: realtime-notifications, Property 3: Connection Limit Management
// *For any* user with 10 active connections, establishing a new connection SHALL close
// the oldest connection and send connection_limit event to the closed connection.
// **Validates: Requirements 1.5, 1.6, 8.2**
func TestProperty3_ConnectionLimitManagement(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate random parameters
		maxConnections := rapid.IntRange(2, 10).Draw(t, "maxConnections")
		userID := rapid.StringMatching(`[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`).Draw(t, "userID")

		config := Config{
			HeartbeatInterval:     30 * time.Second,
			ConnectionTimeout:     1 * time.Hour,
			MaxConnectionsPerUser: maxConnections,
			EventBufferSize:       100,
		}

		cm := NewConnectionManager(config)

		// Track connections and their creation order
		connections := make([]*Connection, 0, maxConnections+1)
		writers := make([]*mockResponseWriter, 0, maxConnections+1)

		// Add connections up to the limit
		for i := 0; i < maxConnections; i++ {
			conn, w := createTestConnection(userID)
			// Stagger creation times to ensure ordering
			conn.CreatedAt = time.Now().Add(time.Duration(i) * time.Millisecond)
			connections = append(connections, conn)
			writers = append(writers, w)

			err := cm.AddConnection(userID, conn)
			if err != nil {
				t.Fatalf("failed to add connection %d: %v", i, err)
			}
		}

		// Property: Should have exactly maxConnections
		if cm.CountConnections(userID) != maxConnections {
			t.Errorf("expected %d connections, got %d", maxConnections, cm.CountConnections(userID))
		}

		// Add one more connection (exceeds limit)
		newConn, newWriter := createTestConnection(userID)
		newConn.CreatedAt = time.Now().Add(time.Duration(maxConnections) * time.Millisecond)
		connections = append(connections, newConn)
		writers = append(writers, newWriter)

		err := cm.AddConnection(userID, newConn)
		if err != nil {
			t.Fatalf("failed to add connection exceeding limit: %v", err)
		}

		// Property: Should still have exactly maxConnections (oldest was removed)
		if cm.CountConnections(userID) != maxConnections {
			t.Errorf("expected %d connections after limit exceeded, got %d",
				maxConnections, cm.CountConnections(userID))
		}

		// Property: The oldest connection (first one) should be closed
		if !connections[0].IsClosed() {
			t.Error("oldest connection should be closed when limit exceeded")
		}

		// Property: The new connection should be active
		if newConn.IsClosed() {
			t.Error("new connection should be active")
		}

		// Property: Connection limit event should have been sent to oldest connection
		body := writers[0].Body.String()
		if !bytes.Contains([]byte(body), []byte("connection_limit")) {
			t.Error("connection_limit event should be sent to closed connection")
		}
	})
}

// Feature: realtime-notifications, Property 11: Graceful Disconnection
// *For any* client disconnection, the SSE_Server SHALL clean up connection resources
// without affecting other connections.
// **Validates: Requirements 8.3**
func TestProperty11_GracefulDisconnection(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate random parameters
		numConnections := rapid.IntRange(2, 10).Draw(t, "numConnections")
		userID := rapid.StringMatching(`[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`).Draw(t, "userID")
		disconnectIndex := rapid.IntRange(0, numConnections-1).Draw(t, "disconnectIndex")

		config := Config{
			HeartbeatInterval:     30 * time.Second,
			ConnectionTimeout:     1 * time.Hour,
			MaxConnectionsPerUser: 20, // High limit to avoid interference
			EventBufferSize:       100,
		}

		cm := NewConnectionManager(config)

		// Add multiple connections
		connections := make([]*Connection, numConnections)
		for i := 0; i < numConnections; i++ {
			conn, _ := createTestConnection(userID)
			connections[i] = conn
			cm.AddConnection(userID, conn)
		}

		// Property: All connections should be active initially
		if cm.CountConnections(userID) != numConnections {
			t.Errorf("expected %d connections initially, got %d",
				numConnections, cm.CountConnections(userID))
		}

		// Disconnect one connection
		connToRemove := connections[disconnectIndex]
		cm.RemoveConnection(userID, connToRemove.ID)

		// Property: Connection count should decrease by 1
		expectedCount := numConnections - 1
		if cm.CountConnections(userID) != expectedCount {
			t.Errorf("expected %d connections after removal, got %d",
				expectedCount, cm.CountConnections(userID))
		}

		// Property: Removed connection should be closed
		if !connToRemove.IsClosed() {
			t.Error("removed connection should be closed")
		}

		// Property: Other connections should still be active
		for i, conn := range connections {
			if i != disconnectIndex && conn.IsClosed() {
				t.Errorf("connection %d should still be active", i)
			}
		}

		// Property: Removed connection should not be in GetConnections
		activeConns := cm.GetConnections(userID)
		for _, conn := range activeConns {
			if conn.ID == connToRemove.ID {
				t.Error("removed connection should not be in active connections")
			}
		}
	})
}


// Test concurrent connection management
func TestProperty3_ConcurrentConnectionManagement(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		maxConnections := rapid.IntRange(3, 8).Draw(t, "maxConnections")
		numGoroutines := rapid.IntRange(2, 5).Draw(t, "numGoroutines")
		userID := rapid.StringMatching(`[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`).Draw(t, "userID")

		config := Config{
			HeartbeatInterval:     30 * time.Second,
			ConnectionTimeout:     1 * time.Hour,
			MaxConnectionsPerUser: maxConnections,
			EventBufferSize:       100,
		}

		cm := NewConnectionManager(config)

		var wg sync.WaitGroup
		connectionsPerGoroutine := maxConnections / numGoroutines
		if connectionsPerGoroutine < 1 {
			connectionsPerGoroutine = 1
		}

		// Concurrently add connections
		for g := 0; g < numGoroutines; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < connectionsPerGoroutine; i++ {
					conn, _ := createTestConnection(userID)
					cm.AddConnection(userID, conn)
				}
			}()
		}

		wg.Wait()

		// Property: Connection count should never exceed maxConnections
		count := cm.CountConnections(userID)
		if count > maxConnections {
			t.Errorf("connection count %d exceeds max %d", count, maxConnections)
		}
	})
}

// Test user isolation in connection management
func TestProperty11_UserIsolation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		numUsers := rapid.IntRange(2, 5).Draw(t, "numUsers")
		connectionsPerUser := rapid.IntRange(1, 5).Draw(t, "connectionsPerUser")

		config := Config{
			HeartbeatInterval:     30 * time.Second,
			ConnectionTimeout:     1 * time.Hour,
			MaxConnectionsPerUser: 10,
			EventBufferSize:       100,
		}

		cm := NewConnectionManager(config)

		// Create connections for multiple users
		userConnections := make(map[string][]*Connection)
		for u := 0; u < numUsers; u++ {
			userID := uuid.New().String()
			userConnections[userID] = make([]*Connection, 0)

			for c := 0; c < connectionsPerUser; c++ {
				conn, _ := createTestConnection(userID)
				userConnections[userID] = append(userConnections[userID], conn)
				cm.AddConnection(userID, conn)
			}
		}

		// Pick a random user to disconnect
		var targetUserID string
		for userID := range userConnections {
			targetUserID = userID
			break
		}

		// Remove all connections for target user
		for _, conn := range userConnections[targetUserID] {
			cm.RemoveConnection(targetUserID, conn.ID)
		}

		// Property: Target user should have 0 connections
		if cm.CountConnections(targetUserID) != 0 {
			t.Errorf("target user should have 0 connections, got %d",
				cm.CountConnections(targetUserID))
		}

		// Property: Other users should still have their connections
		for userID, conns := range userConnections {
			if userID != targetUserID {
				count := cm.CountConnections(userID)
				if count != len(conns) {
					t.Errorf("user %s should have %d connections, got %d",
						userID, len(conns), count)
				}
			}
		}
	})
}

// Unit test for basic connection operations
func TestConnectionManager_BasicOperations(t *testing.T) {
	config := DefaultConfig()
	cm := NewConnectionManager(config)

	userID := "test-user"

	// Test empty state
	if cm.CountConnections(userID) != 0 {
		t.Error("expected 0 connections initially")
	}

	conns := cm.GetConnections(userID)
	if len(conns) != 0 {
		t.Error("expected empty connections list initially")
	}

	// Add a connection
	conn, _ := createTestConnection(userID)
	err := cm.AddConnection(userID, conn)
	if err != nil {
		t.Fatalf("failed to add connection: %v", err)
	}

	if cm.CountConnections(userID) != 1 {
		t.Error("expected 1 connection after add")
	}

	// Get connection
	retrieved := cm.GetConnection(userID, conn.ID)
	if retrieved == nil {
		t.Error("should be able to retrieve connection")
	}
	if retrieved.ID != conn.ID {
		t.Error("retrieved connection ID mismatch")
	}

	// Remove connection
	cm.RemoveConnection(userID, conn.ID)
	if cm.CountConnections(userID) != 0 {
		t.Error("expected 0 connections after remove")
	}

	// Connection should be closed
	if !conn.IsClosed() {
		t.Error("connection should be closed after remove")
	}
}

// Unit test for TotalConnections
func TestConnectionManager_TotalConnections(t *testing.T) {
	config := DefaultConfig()
	cm := NewConnectionManager(config)

	user1 := "user-1"
	user2 := "user-2"

	// Add connections for multiple users
	conn1, _ := createTestConnection(user1)
	conn2, _ := createTestConnection(user1)
	conn3, _ := createTestConnection(user2)

	cm.AddConnection(user1, conn1)
	cm.AddConnection(user1, conn2)
	cm.AddConnection(user2, conn3)

	if cm.TotalConnections() != 3 {
		t.Errorf("expected 3 total connections, got %d", cm.TotalConnections())
	}

	// Remove one
	cm.RemoveConnection(user1, conn1.ID)
	if cm.TotalConnections() != 2 {
		t.Errorf("expected 2 total connections after removal, got %d", cm.TotalConnections())
	}
}

// Unit test for UpdateLastPing
func TestConnectionManager_UpdateLastPing(t *testing.T) {
	config := DefaultConfig()
	cm := NewConnectionManager(config)

	userID := "test-user"
	conn, _ := createTestConnection(userID)
	originalPing := conn.LastPing

	cm.AddConnection(userID, conn)

	// Wait a bit
	time.Sleep(10 * time.Millisecond)

	// Update ping
	cm.UpdateLastPing(userID, conn.ID)

	// Get connection and check LastPing was updated
	retrieved := cm.GetConnection(userID, conn.ID)
	if !retrieved.LastPing.After(originalPing) {
		t.Error("LastPing should be updated")
	}
}

// Unit test for removing non-existent connection
func TestConnectionManager_RemoveNonExistent(t *testing.T) {
	config := DefaultConfig()
	cm := NewConnectionManager(config)

	// Should not panic
	cm.RemoveConnection("non-existent-user", "non-existent-conn")

	// Add a connection and try to remove wrong ID
	userID := "test-user"
	conn, _ := createTestConnection(userID)
	cm.AddConnection(userID, conn)

	cm.RemoveConnection(userID, "wrong-id")

	// Original connection should still exist
	if cm.CountConnections(userID) != 1 {
		t.Error("original connection should still exist")
	}
}

// Test Connection struct methods
func TestConnection_Methods(t *testing.T) {
	w := newMockResponseWriter()
	conn, err := NewConnection("test-id", "test-user", w)
	if err != nil {
		t.Fatalf("failed to create connection: %v", err)
	}

	// Test IsClosed
	if conn.IsClosed() {
		t.Error("new connection should not be closed")
	}

	// Test Close
	conn.Close()
	if !conn.IsClosed() {
		t.Error("connection should be closed after Close()")
	}

	// Test double close (should not panic)
	conn.Close()
}

// Test NewConnection with non-flusher writer
func TestNewConnection_NonFlusher(t *testing.T) {
	// Create a writer that doesn't implement Flusher
	w := &nonFlusherWriter{}
	_, err := NewConnection("test-id", "test-user", w)
	if err != ErrStreamingNotSupported {
		t.Errorf("expected ErrStreamingNotSupported, got %v", err)
	}
}

type nonFlusherWriter struct{}

func (w *nonFlusherWriter) Header() http.Header {
	return http.Header{}
}

func (w *nonFlusherWriter) Write(b []byte) (int, error) {
	return len(b), nil
}

func (w *nonFlusherWriter) WriteHeader(statusCode int) {}


// Test dead connection cleanup
func TestConnectionManager_CleanupDeadConnections(t *testing.T) {
	config := Config{
		HeartbeatInterval:     10 * time.Millisecond, // Short interval for testing
		ConnectionTimeout:     1 * time.Hour,
		MaxConnectionsPerUser: 10,
		EventBufferSize:       100,
	}

	cm := NewConnectionManager(config)
	userID := "test-user"

	// Add a connection
	conn, _ := createTestConnection(userID)
	cm.AddConnection(userID, conn)

	// Connection should be alive initially
	if cm.CountConnections(userID) != 1 {
		t.Error("expected 1 connection initially")
	}

	// Simulate dead connection by setting LastPing to old time
	conn.LastPing = time.Now().Add(-1 * time.Hour)

	// Run cleanup
	cm.CleanupDeadConnections()

	// Connection should be removed
	if cm.CountConnections(userID) != 0 {
		t.Error("dead connection should be cleaned up")
	}

	// Connection should be closed
	if !conn.IsClosed() {
		t.Error("dead connection should be closed")
	}
}

// Test cleanup of already closed connections
func TestConnectionManager_CleanupClosedConnections(t *testing.T) {
	config := DefaultConfig()
	cm := NewConnectionManager(config)
	userID := "test-user"

	// Add connections
	conn1, _ := createTestConnection(userID)
	conn2, _ := createTestConnection(userID)
	cm.AddConnection(userID, conn1)
	cm.AddConnection(userID, conn2)

	// Close one connection manually
	conn1.Close()

	// Run cleanup
	cm.CleanupDeadConnections()

	// Only active connection should remain
	if cm.CountConnections(userID) != 1 {
		t.Errorf("expected 1 connection after cleanup, got %d", cm.CountConnections(userID))
	}

	// The remaining connection should be conn2
	conns := cm.GetConnections(userID)
	if len(conns) != 1 || conns[0].ID != conn2.ID {
		t.Error("wrong connection remained after cleanup")
	}
}

// Test timed out connection cleanup
func TestConnectionManager_CleanupTimedOutConnections(t *testing.T) {
	config := Config{
		HeartbeatInterval:     30 * time.Second,
		ConnectionTimeout:     10 * time.Millisecond, // Short timeout for testing
		MaxConnectionsPerUser: 10,
		EventBufferSize:       100,
	}

	cm := NewConnectionManager(config)
	userID := "test-user"

	// Add a connection with old creation time
	conn, _ := createTestConnection(userID)
	conn.CreatedAt = time.Now().Add(-1 * time.Hour)
	cm.AddConnection(userID, conn)

	// Run timeout cleanup
	cm.CleanupTimedOutConnections()

	// Connection should be removed
	if cm.CountConnections(userID) != 0 {
		t.Error("timed out connection should be cleaned up")
	}
}

// Test MarkConnectionAlive
func TestConnectionManager_MarkConnectionAlive(t *testing.T) {
	config := DefaultConfig()
	cm := NewConnectionManager(config)
	userID := "test-user"

	// Add a connection
	conn, _ := createTestConnection(userID)
	conn.LastPing = time.Now().Add(-1 * time.Hour) // Old ping time
	cm.AddConnection(userID, conn)

	// Mark as alive
	success := cm.MarkConnectionAlive(userID, conn.ID)
	if !success {
		t.Error("MarkConnectionAlive should return true for existing connection")
	}

	// LastPing should be updated
	retrieved := cm.GetConnection(userID, conn.ID)
	if time.Since(retrieved.LastPing) > time.Second {
		t.Error("LastPing should be updated to recent time")
	}

	// Test with non-existent connection
	success = cm.MarkConnectionAlive(userID, "non-existent")
	if success {
		t.Error("MarkConnectionAlive should return false for non-existent connection")
	}
}

// Test GetDeadConnections
func TestConnectionManager_GetDeadConnections(t *testing.T) {
	config := Config{
		HeartbeatInterval:     10 * time.Millisecond,
		ConnectionTimeout:     1 * time.Hour,
		MaxConnectionsPerUser: 10,
		EventBufferSize:       100,
	}

	cm := NewConnectionManager(config)
	userID := "test-user"

	// Add connections
	aliveConn, _ := createTestConnection(userID)
	deadConn, _ := createTestConnection(userID)
	deadConn.LastPing = time.Now().Add(-1 * time.Hour) // Old ping time

	cm.AddConnection(userID, aliveConn)
	cm.AddConnection(userID, deadConn)

	// Get dead connections
	deadConns := cm.GetDeadConnections()
	if len(deadConns) != 1 {
		t.Errorf("expected 1 dead connection, got %d", len(deadConns))
	}

	if len(deadConns) > 0 && deadConns[0].ID != deadConn.ID {
		t.Error("wrong connection identified as dead")
	}
}

// Test cleanup routine
func TestConnectionManager_StartCleanupRoutine(t *testing.T) {
	config := Config{
		HeartbeatInterval:     5 * time.Millisecond, // Very short for testing
		ConnectionTimeout:     1 * time.Hour,
		MaxConnectionsPerUser: 10,
		EventBufferSize:       100,
	}

	cm := NewConnectionManager(config)
	userID := "test-user"

	// Add a dead connection
	conn, _ := createTestConnection(userID)
	conn.LastPing = time.Now().Add(-1 * time.Hour)
	cm.AddConnection(userID, conn)

	// Start cleanup routine
	stop := cm.StartCleanupRoutine(10 * time.Millisecond)
	defer stop()

	// Wait for cleanup to run
	time.Sleep(50 * time.Millisecond)

	// Connection should be cleaned up
	if cm.CountConnections(userID) != 0 {
		t.Error("cleanup routine should have removed dead connection")
	}
}

// Test empty user map cleanup
func TestConnectionManager_EmptyUserMapCleanup(t *testing.T) {
	config := DefaultConfig()
	cm := NewConnectionManager(config)
	userID := "test-user"

	// Add and remove a connection
	conn, _ := createTestConnection(userID)
	cm.AddConnection(userID, conn)
	cm.RemoveConnection(userID, conn.ID)

	// User map should be cleaned up
	cm.mu.RLock()
	_, exists := cm.connections[userID]
	cm.mu.RUnlock()

	if exists {
		t.Error("empty user map should be cleaned up")
	}
}
