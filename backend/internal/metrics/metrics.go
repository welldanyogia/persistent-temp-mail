// Package metrics provides Prometheus metrics for the backend API
// Requirements: 9.5 - Backend SHALL expose /metrics endpoint in Prometheus format
package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// HTTPRequestsTotal counts total HTTP requests by method, path, and status
	HTTPRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "tempmail",
			Subsystem: "http",
			Name:      "requests_total",
			Help:      "Total number of HTTP requests by method, path, and status code",
		},
		[]string{"method", "path", "status"},
	)

	// HTTPRequestDuration measures HTTP request duration in seconds
	HTTPRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "tempmail",
			Subsystem: "http",
			Name:      "request_duration_seconds",
			Help:      "HTTP request duration in seconds",
			Buckets:   []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
		},
		[]string{"method", "path"},
	)

	// HTTPRequestsInFlight tracks current in-flight requests
	HTTPRequestsInFlight = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "tempmail",
			Subsystem: "http",
			Name:      "requests_in_flight",
			Help:      "Current number of HTTP requests being processed",
		},
	)

	// HTTPResponseSize measures HTTP response size in bytes
	HTTPResponseSize = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "tempmail",
			Subsystem: "http",
			Name:      "response_size_bytes",
			Help:      "HTTP response size in bytes",
			Buckets:   []float64{100, 1000, 10000, 100000, 1000000, 10000000},
		},
		[]string{"method", "path"},
	)
)

var (
	// DBConnectionsOpen tracks open database connections
	DBConnectionsOpen = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "tempmail",
			Subsystem: "db",
			Name:      "connections_open",
			Help:      "Number of open database connections",
		},
	)

	// DBConnectionsInUse tracks database connections currently in use
	DBConnectionsInUse = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "tempmail",
			Subsystem: "db",
			Name:      "connections_in_use",
			Help:      "Number of database connections currently in use",
		},
	)

	// DBConnectionsIdle tracks idle database connections
	DBConnectionsIdle = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "tempmail",
			Subsystem: "db",
			Name:      "connections_idle",
			Help:      "Number of idle database connections",
		},
	)

	// DBConnectionsMaxOpen tracks maximum open database connections
	DBConnectionsMaxOpen = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "tempmail",
			Subsystem: "db",
			Name:      "connections_max_open",
			Help:      "Maximum number of open database connections",
		},
	)

	// DBQueryDuration measures database query duration
	DBQueryDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "tempmail",
			Subsystem: "db",
			Name:      "query_duration_seconds",
			Help:      "Database query duration in seconds",
			Buckets:   []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5},
		},
		[]string{"operation"},
	)
)

var (
	// SMTPConnectionsTotal counts total SMTP connections
	SMTPConnectionsTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "tempmail",
			Subsystem: "smtp",
			Name:      "connections_total",
			Help:      "Total number of SMTP connections",
		},
	)

	// SMTPConnectionsActive tracks active SMTP connections
	SMTPConnectionsActive = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "tempmail",
			Subsystem: "smtp",
			Name:      "connections_active",
			Help:      "Number of active SMTP connections",
		},
	)

	// SMTPEmailsReceived counts total emails received
	SMTPEmailsReceived = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "tempmail",
			Subsystem: "smtp",
			Name:      "emails_received_total",
			Help:      "Total number of emails received via SMTP",
		},
	)

	// SMTPEmailsRejected counts rejected emails
	SMTPEmailsRejected = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "tempmail",
			Subsystem: "smtp",
			Name:      "emails_rejected_total",
			Help:      "Total number of rejected emails by reason",
		},
		[]string{"reason"},
	)
)

var (
	// SSEConnectionsActive tracks active SSE connections
	SSEConnectionsActive = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "tempmail",
			Subsystem: "sse",
			Name:      "connections_active",
			Help:      "Number of active SSE connections",
		},
	)

	// SSEEventsPublished counts total SSE events published
	SSEEventsPublished = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "tempmail",
			Subsystem: "sse",
			Name:      "events_published_total",
			Help:      "Total number of SSE events published by type",
		},
		[]string{"event_type"},
	)
)

// responseWriter wraps http.ResponseWriter to capture status code and size
type responseWriter struct {
	http.ResponseWriter
	statusCode int
	size       int
}

// newResponseWriter creates a new responseWriter
func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
	}
}

// WriteHeader captures the status code
func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Write captures the response size
func (rw *responseWriter) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.size += n
	return n, err
}

// Middleware returns a chi middleware that records HTTP metrics
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Track in-flight requests
		HTTPRequestsInFlight.Inc()
		defer HTTPRequestsInFlight.Dec()

		// Wrap response writer to capture status and size
		rw := newResponseWriter(w)

		// Process request
		next.ServeHTTP(rw, r)

		// Calculate duration
		duration := time.Since(start).Seconds()

		// Get route pattern for consistent labeling
		path := getRoutePattern(r)

		// Record metrics
		HTTPRequestsTotal.WithLabelValues(r.Method, path, strconv.Itoa(rw.statusCode)).Inc()
		HTTPRequestDuration.WithLabelValues(r.Method, path).Observe(duration)
		HTTPResponseSize.WithLabelValues(r.Method, path).Observe(float64(rw.size))
	})
}

// getRoutePattern returns the route pattern from chi context
// Falls back to URL path if pattern not available
func getRoutePattern(r *http.Request) string {
	rctx := chi.RouteContext(r.Context())
	if rctx != nil && rctx.RoutePattern() != "" {
		return rctx.RoutePattern()
	}
	return r.URL.Path
}

// Handler returns the Prometheus metrics HTTP handler
func Handler() http.Handler {
	return promhttp.Handler()
}
