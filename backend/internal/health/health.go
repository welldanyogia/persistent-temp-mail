// Package health provides health check endpoints for the backend service.
// Requirements: 10.1, 10.2, 10.3, 10.10, 10.11 - Health check implementation
package health

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// ServiceStatus represents the status of a single service
type ServiceStatus struct {
	Status  string `json:"status"`
	Latency string `json:"latency,omitempty"`
	Error   string `json:"error,omitempty"`
}

// HealthResponse represents the structured health check response
// Requirements: 10.1 - Structured JSON response with service status
type HealthResponse struct {
	Status    string                   `json:"status"`
	Timestamp string                   `json:"timestamp"`
	Services  map[string]ServiceStatus `json:"services"`
	Version   string                   `json:"version,omitempty"`
}

// ReadinessResponse represents the readiness probe response
// Requirements: 10.10, 10.11 - Readiness and liveness endpoints
type ReadinessResponse struct {
	Ready     bool   `json:"ready"`
	Timestamp string `json:"timestamp"`
}

// LivenessResponse represents the liveness probe response
type LivenessResponse struct {
	Alive     bool   `json:"alive"`
	Timestamp string `json:"timestamp"`
}

// Handler handles health check requests
type Handler struct {
	dbPool      *pgxpool.Pool
	redisClient *redis.Client
	version     string
	timeout     time.Duration
	ready       bool
	mu          sync.RWMutex
}

// Config holds health handler configuration
type Config struct {
	DBPool      *pgxpool.Pool
	RedisClient *redis.Client
	Version     string
	Timeout     time.Duration // Default: 5 seconds (Requirement 10.10)
}

// NewHandler creates a new health check handler
func NewHandler(cfg Config) *Handler {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second // Requirement 10.10: Health check within 5 seconds
	}

	return &Handler{
		dbPool:      cfg.DBPool,
		redisClient: cfg.RedisClient,
		version:     cfg.Version,
		timeout:     timeout,
		ready:       true,
	}
}

// SetReady sets the readiness state of the service
// Requirements: 10.11 - Support graceful shutdown
func (h *Handler) SetReady(ready bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ready = ready
}

// IsReady returns the current readiness state
func (h *Handler) IsReady() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.ready
}


// Health handles the main health check endpoint
// Requirements: 10.1, 10.2, 10.3 - Health endpoint with database and Redis checks
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()

	services := make(map[string]ServiceStatus)
	overallStatus := "healthy"

	// Check database connectivity (Requirement 10.2)
	dbStatus := h.checkDatabase(ctx)
	services["database"] = dbStatus
	if dbStatus.Status != "up" {
		overallStatus = "degraded"
	}

	// Check Redis connectivity (Requirement 10.3)
	if h.redisClient != nil {
		redisStatus := h.checkRedis(ctx)
		services["redis"] = redisStatus
		if redisStatus.Status != "up" {
			overallStatus = "degraded"
		}
	}

	response := HealthResponse{
		Status:    overallStatus,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Services:  services,
		Version:   h.version,
	}

	w.Header().Set("Content-Type", "application/json")
	if overallStatus == "healthy" {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	json.NewEncoder(w).Encode(response)
}

// Readiness handles the readiness probe endpoint
// Requirements: 10.10, 10.11 - Readiness endpoint for Kubernetes/Docker
func (h *Handler) Readiness(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()

	ready := h.IsReady()

	// Also check if critical services are available
	if ready {
		// Check database
		dbStatus := h.checkDatabase(ctx)
		if dbStatus.Status != "up" {
			ready = false
		}
	}

	response := ReadinessResponse{
		Ready:     ready,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	if ready {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	json.NewEncoder(w).Encode(response)
}

// Liveness handles the liveness probe endpoint
// Requirements: 10.10, 10.11 - Liveness endpoint for Kubernetes/Docker
func (h *Handler) Liveness(w http.ResponseWriter, r *http.Request) {
	response := LivenessResponse{
		Alive:     true,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// checkDatabase checks PostgreSQL connectivity
// Requirements: 10.2 - Check database connectivity in health endpoint
func (h *Handler) checkDatabase(ctx context.Context) ServiceStatus {
	if h.dbPool == nil {
		return ServiceStatus{
			Status: "down",
			Error:  "database pool not configured",
		}
	}

	start := time.Now()
	err := h.dbPool.Ping(ctx)
	latency := time.Since(start)

	if err != nil {
		return ServiceStatus{
			Status:  "down",
			Latency: latency.String(),
			Error:   err.Error(),
		}
	}

	return ServiceStatus{
		Status:  "up",
		Latency: latency.String(),
	}
}

// checkRedis checks Redis connectivity
// Requirements: 10.3 - Check Redis connectivity in health endpoint
func (h *Handler) checkRedis(ctx context.Context) ServiceStatus {
	if h.redisClient == nil {
		return ServiceStatus{
			Status: "down",
			Error:  "redis client not configured",
		}
	}

	start := time.Now()
	_, err := h.redisClient.Ping(ctx).Result()
	latency := time.Since(start)

	if err != nil {
		return ServiceStatus{
			Status:  "down",
			Latency: latency.String(),
			Error:   err.Error(),
		}
	}

	return ServiceStatus{
		Status:  "up",
		Latency: latency.String(),
	}
}


// SMTPHealthChecker interface for SMTP health checks
// Requirements: 10.5 - SMTP health check
type SMTPHealthChecker interface {
	IsRunning() bool
	GetActiveConnections() int64
}

// SMTPHealthResponse represents the SMTP health check response
type SMTPHealthResponse struct {
	Status    string                 `json:"status"`
	Timestamp string                 `json:"timestamp"`
	SMTP      map[string]interface{} `json:"smtp"`
	EHLOCheck string                 `json:"ehlo_check,omitempty"`
	Error     string                 `json:"error,omitempty"`
}

// SMTPEHLOChecker interface for EHLO-based health checks
type SMTPEHLOChecker interface {
	PerformEHLOCheck(ctx context.Context) error
}

// SMTPHandler handles SMTP health check requests
type SMTPHandler struct {
	smtpServer  SMTPHealthChecker
	ehloChecker SMTPEHLOChecker
	hostname    string
	port        int
	timeout     time.Duration
}

// SMTPHandlerConfig holds configuration for SMTP health handler
type SMTPHandlerConfig struct {
	SMTPServer  SMTPHealthChecker
	EHLOChecker SMTPEHLOChecker
	Hostname    string
	Port        int
	Timeout     time.Duration
}

// NewSMTPHandler creates a new SMTP health handler
func NewSMTPHandler(cfg SMTPHandlerConfig) *SMTPHandler {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	return &SMTPHandler{
		smtpServer:  cfg.SMTPServer,
		ehloChecker: cfg.EHLOChecker,
		hostname:    cfg.Hostname,
		port:        cfg.Port,
		timeout:     timeout,
	}
}

// SMTPHealth handles the SMTP health check endpoint
// Requirements: 10.5 - SMTP_Server SHALL respond to EHLO command for health check
func (h *SMTPHandler) SMTPHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()

	response := SMTPHealthResponse{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		SMTP:      make(map[string]interface{}),
	}

	if h.smtpServer == nil {
		response.Status = "unavailable"
		response.Error = "SMTP server not configured"
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Get basic health status
	running := h.smtpServer.IsRunning()
	activeConns := h.smtpServer.GetActiveConnections()

	response.SMTP["running"] = running
	response.SMTP["active_connections"] = activeConns
	response.SMTP["hostname"] = h.hostname
	response.SMTP["port"] = h.port

	if running {
		response.SMTP["status"] = "healthy"
	} else {
		response.SMTP["status"] = "unhealthy"
	}

	// Perform EHLO check if server is running and EHLO checker is available
	if running && h.ehloChecker != nil {
		err := h.ehloChecker.PerformEHLOCheck(ctx)
		if err != nil {
			response.Status = "degraded"
			response.EHLOCheck = "failed"
			response.Error = err.Error()
		} else {
			response.Status = "healthy"
			response.EHLOCheck = "passed"
		}
	} else if running {
		response.Status = "healthy"
		response.EHLOCheck = "skipped"
	} else {
		response.Status = "unhealthy"
		response.Error = "SMTP server is not running"
	}

	w.Header().Set("Content-Type", "application/json")
	if response.Status == "healthy" {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	json.NewEncoder(w).Encode(response)
}
