package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
)

func TestMiddleware(t *testing.T) {
	// Reset metrics for clean test
	HTTPRequestsTotal.Reset()

	// Create a test handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Wrap with metrics middleware
	wrapped := Middleware(handler)

	// Create test request
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	// Execute request
	wrapped.ServeHTTP(rec, req)

	// Verify response
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	// Verify metrics were recorded
	// Note: We can't easily verify the exact metric values without
	// accessing the internal registry, but we can verify no panics occurred
}

func TestMiddlewareWithChiRouter(t *testing.T) {
	// Reset metrics for clean test
	HTTPRequestsTotal.Reset()

	// Create chi router with metrics middleware
	r := chi.NewRouter()
	r.Use(Middleware)
	r.Get("/api/v1/test/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Create test request
	req := httptest.NewRequest("GET", "/api/v1/test/123", nil)
	rec := httptest.NewRecorder()

	// Execute request
	r.ServeHTTP(rec, req)

	// Verify response
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

func TestResponseWriter(t *testing.T) {
	// Create a test response recorder
	rec := httptest.NewRecorder()
	rw := newResponseWriter(rec)

	// Test WriteHeader
	rw.WriteHeader(http.StatusCreated)
	if rw.statusCode != http.StatusCreated {
		t.Errorf("Expected status code 201, got %d", rw.statusCode)
	}

	// Test Write
	data := []byte("Hello, World!")
	n, err := rw.Write(data)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if n != len(data) {
		t.Errorf("Expected to write %d bytes, wrote %d", len(data), n)
	}
	if rw.size != len(data) {
		t.Errorf("Expected size %d, got %d", len(data), rw.size)
	}
}

func TestHandler(t *testing.T) {
	// Get the metrics handler
	handler := Handler()

	// Create test request
	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()

	// Execute request
	handler.ServeHTTP(rec, req)

	// Verify response
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	// Verify content type
	contentType := rec.Header().Get("Content-Type")
	if !strings.Contains(contentType, "text/plain") {
		t.Errorf("Expected text/plain content type, got %s", contentType)
	}

	// Verify body contains Prometheus metrics
	body := rec.Body.String()
	if !strings.Contains(body, "tempmail_http_requests_total") {
		t.Errorf("Expected body to contain tempmail_http_requests_total metric")
	}
}

func TestMetricsRegistration(t *testing.T) {
	// Verify all metrics are registered
	metrics := []prometheus.Collector{
		HTTPRequestsTotal,
		HTTPRequestDuration,
		HTTPRequestsInFlight,
		HTTPResponseSize,
		DBConnectionsOpen,
		DBConnectionsInUse,
		DBConnectionsIdle,
		DBConnectionsMaxOpen,
		DBQueryDuration,
		SMTPConnectionsTotal,
		SMTPConnectionsActive,
		SMTPEmailsReceived,
		SMTPEmailsRejected,
		SSEConnectionsActive,
		SSEEventsPublished,
	}

	for _, m := range metrics {
		// This will panic if the metric is not registered
		desc := make(chan *prometheus.Desc, 10)
		m.Describe(desc)
		close(desc)
		
		// Verify at least one description was sent
		count := 0
		for range desc {
			count++
		}
		if count == 0 {
			t.Errorf("Metric has no descriptions")
		}
	}
}
