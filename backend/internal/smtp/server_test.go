package smtp

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// MockAliasRepository implements AliasRepository for testing
type MockAliasRepository struct {
	aliases map[string]*AliasInfo
}

func NewMockAliasRepository() *MockAliasRepository {
	return &MockAliasRepository{
		aliases: make(map[string]*AliasInfo),
	}
}

func (m *MockAliasRepository) GetByFullAddress(ctx context.Context, fullAddress string) (*AliasInfo, error) {
	if alias, ok := m.aliases[fullAddress]; ok {
		return alias, nil
	}
	return nil, context.DeadlineExceeded
}

func (m *MockAliasRepository) AddAlias(address string, isActive bool) {
	m.aliases[address] = &AliasInfo{
		ID:       address,
		IsActive: isActive,
	}
}

// TestProperty1_ConnectionLimitsEnforcement tests Property 1: Connection Limits Enforcement
// Feature: smtp-email-receiver, Property 1: Connection Limits Enforcement
// *For any* number of concurrent connections exceeding 100, or connections per IP exceeding 5,
// the SMTP_Server SHALL reject new connections with 421 response.
// **Validates: Requirements 1.6, 1.7, 6.3**
func TestProperty1_ConnectionLimitsEnforcement(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate random configuration within reasonable bounds
		maxConns := rapid.IntRange(5, 20).Draw(t, "maxConnections")
		maxPerIP := rapid.IntRange(2, 5).Draw(t, "maxConnectionsPerIP")
		
		config := &SMTPConfig{
			Port:                0, // Use random port
			Hostname:            "test.local",
			MaxConnections:      maxConns,
			MaxConnectionsPerIP: maxPerIP,
			ConnectionTimeout:   5 * time.Minute,
			MaxMessageSize:      25 * 1024 * 1024,
			MaxRecipients:       100,
			RateLimitPerMinute:  1000, // High limit to not interfere with test
		}
		
		repo := NewMockAliasRepository()
		server := NewSMTPServer(config, nil, repo)
		
		// Test global connection limit
		t.Log("Testing global connection limit")
		
		// Acquire connections up to limit
		for i := 0; i < maxConns; i++ {
			if !server.acquireConnection() {
				t.Fatalf("Should be able to acquire connection %d (limit: %d)", i+1, maxConns)
			}
		}
		
		// Verify we're at the limit
		if server.GetActiveConnections() != int64(maxConns) {
			t.Fatalf("Expected %d active connections, got %d", maxConns, server.GetActiveConnections())
		}
		
		// Next connection should be rejected
		if server.acquireConnection() {
			t.Fatal("Should NOT be able to acquire connection beyond limit")
		}
		
		// Release all connections
		for i := 0; i < maxConns; i++ {
			server.releaseConnection()
		}
		
		// Verify connections released
		if server.GetActiveConnections() != 0 {
			t.Fatalf("Expected 0 active connections after release, got %d", server.GetActiveConnections())
		}
		
		// Test per-IP connection limit
		t.Log("Testing per-IP connection limit")
		testIP := "192.168.1.100"
		
		// Acquire per-IP connections up to limit
		for i := 0; i < maxPerIP; i++ {
			if !server.acquireIPConnection(testIP) {
				t.Fatalf("Should be able to acquire IP connection %d (limit: %d)", i+1, maxPerIP)
			}
		}
		
		// Verify we're at the per-IP limit
		if server.GetIPConnections(testIP) != maxPerIP {
			t.Fatalf("Expected %d IP connections, got %d", maxPerIP, server.GetIPConnections(testIP))
		}
		
		// Next per-IP connection should be rejected
		if server.acquireIPConnection(testIP) {
			t.Fatal("Should NOT be able to acquire IP connection beyond limit")
		}
		
		// Different IP should still work
		otherIP := "192.168.1.200"
		if !server.acquireIPConnection(otherIP) {
			t.Fatal("Should be able to acquire connection from different IP")
		}
		server.releaseIPConnection(otherIP)
		
		// Release all per-IP connections
		for i := 0; i < maxPerIP; i++ {
			server.releaseIPConnection(testIP)
		}
		
		// Verify per-IP connections released
		if server.GetIPConnections(testIP) != 0 {
			t.Fatalf("Expected 0 IP connections after release, got %d", server.GetIPConnections(testIP))
		}
	})
}

// TestProperty1_RateLimitEnforcement tests rate limiting (part of Property 1)
// Feature: smtp-email-receiver, Property 1: Connection Limits Enforcement
// **Validates: Requirements 6.3**
func TestProperty1_RateLimitEnforcement(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate random rate limit
		rateLimit := rapid.IntRange(5, 20).Draw(t, "rateLimitPerMinute")
		
		config := &SMTPConfig{
			Port:                0,
			Hostname:            "test.local",
			MaxConnections:      100,
			MaxConnectionsPerIP: 5,
			ConnectionTimeout:   5 * time.Minute,
			MaxMessageSize:      25 * 1024 * 1024,
			MaxRecipients:       100,
			RateLimitPerMinute:  rateLimit,
		}
		
		repo := NewMockAliasRepository()
		server := NewSMTPServer(config, nil, repo)
		
		testIP := "10.0.0.1"
		
		// Make requests up to rate limit
		for i := 0; i < rateLimit; i++ {
			if !server.checkRateLimit(testIP) {
				t.Fatalf("Request %d should be allowed (limit: %d)", i+1, rateLimit)
			}
		}
		
		// Next request should be rate limited
		if server.checkRateLimit(testIP) {
			t.Fatal("Request beyond rate limit should be rejected")
		}
		
		// Different IP should still work
		otherIP := "10.0.0.2"
		if !server.checkRateLimit(otherIP) {
			t.Fatal("Request from different IP should be allowed")
		}
	})
}

// TestConcurrentConnectionAcquisition tests thread-safety of connection management
func TestConcurrentConnectionAcquisition(t *testing.T) {
	config := &SMTPConfig{
		Port:                0,
		Hostname:            "test.local",
		MaxConnections:      50,
		MaxConnectionsPerIP: 5,
		ConnectionTimeout:   5 * time.Minute,
		MaxMessageSize:      25 * 1024 * 1024,
		MaxRecipients:       100,
		RateLimitPerMinute:  1000,
	}
	
	repo := NewMockAliasRepository()
	server := NewSMTPServer(config, nil, repo)
	
	var wg sync.WaitGroup
	var acquired int64
	numGoroutines := 100
	
	// Try to acquire more connections than allowed concurrently
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if server.acquireConnection() {
				atomic.AddInt64(&acquired, 1)
			}
		}()
	}
	
	wg.Wait()
	
	// Should have acquired exactly MaxConnections
	if acquired != int64(config.MaxConnections) {
		t.Errorf("Expected %d acquired connections, got %d", config.MaxConnections, acquired)
	}
	
	// Active connections should match
	if server.GetActiveConnections() != int64(config.MaxConnections) {
		t.Errorf("Expected %d active connections, got %d", config.MaxConnections, server.GetActiveConnections())
	}
}

// TestConcurrentIPConnectionAcquisition tests thread-safety of per-IP connection management
func TestConcurrentIPConnectionAcquisition(t *testing.T) {
	config := &SMTPConfig{
		Port:                0,
		Hostname:            "test.local",
		MaxConnections:      100,
		MaxConnectionsPerIP: 5,
		ConnectionTimeout:   5 * time.Minute,
		MaxMessageSize:      25 * 1024 * 1024,
		MaxRecipients:       100,
		RateLimitPerMinute:  1000,
	}
	
	repo := NewMockAliasRepository()
	server := NewSMTPServer(config, nil, repo)
	
	testIP := "192.168.1.1"
	var wg sync.WaitGroup
	var acquired int64
	numGoroutines := 20
	
	// Try to acquire more per-IP connections than allowed concurrently
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if server.acquireIPConnection(testIP) {
				atomic.AddInt64(&acquired, 1)
			}
		}()
	}
	
	wg.Wait()
	
	// Should have acquired exactly MaxConnectionsPerIP
	if acquired != int64(config.MaxConnectionsPerIP) {
		t.Errorf("Expected %d acquired IP connections, got %d", config.MaxConnectionsPerIP, acquired)
	}
	
	// IP connections should match
	if server.GetIPConnections(testIP) != config.MaxConnectionsPerIP {
		t.Errorf("Expected %d IP connections, got %d", config.MaxConnectionsPerIP, server.GetIPConnections(testIP))
	}
}

// TestServerStartStop tests server lifecycle
func TestServerStartStop(t *testing.T) {
	config := &SMTPConfig{
		Port:                0, // Use random available port
		Hostname:            "test.local",
		MaxConnections:      100,
		MaxConnectionsPerIP: 5,
		ConnectionTimeout:   5 * time.Minute,
		MaxMessageSize:      25 * 1024 * 1024,
		MaxRecipients:       100,
		RateLimitPerMinute:  20,
	}
	
	repo := NewMockAliasRepository()
	server := NewSMTPServer(config, nil, repo)
	
	// Find available port
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("Failed to find available port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	
	config.Port = port
	
	// Start server
	err = server.Start()
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	
	if !server.IsRunning() {
		t.Fatal("Server should be running after Start()")
	}
	
	// Stop server
	err = server.Stop()
	if err != nil {
		t.Fatalf("Failed to stop server: %v", err)
	}
	
	if server.IsRunning() {
		t.Fatal("Server should not be running after Stop()")
	}
}
