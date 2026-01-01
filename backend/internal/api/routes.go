package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/ssl"
)

// RegisterDomainRoutes registers domain management routes
// All routes require authentication via auth middleware
// Requirements: FR-DOM-001 to FR-DOM-006
func RegisterDomainRoutes(r chi.Router, handler *DomainHandler, authMiddleware func(next http.Handler) http.Handler) {
	r.Route("/domains", func(r chi.Router) {
		// Apply auth middleware to all domain routes
		r.Use(authMiddleware)

		// GET /api/v1/domains - List user domains
		r.Get("/", handler.ListDomains)

		// POST /api/v1/domains - Add new domain
		r.Post("/", handler.CreateDomain)

		// GET /api/v1/domains/:id - Get domain details
		r.Get("/{id}", handler.GetDomain)

		// DELETE /api/v1/domains/:id - Delete domain
		r.Delete("/{id}", handler.DeleteDomain)

		// POST /api/v1/domains/:id/verify - Verify domain DNS
		r.Post("/{id}/verify", handler.VerifyDomain)

		// GET /api/v1/domains/:id/dns-status - Check DNS status
		r.Get("/{id}/dns-status", handler.GetDNSStatus)
	})
}

// RegisterSSLRoutes registers SSL certificate management routes
// Requirements: 3.7 - Support manual renewal trigger via API
// Requirements: 8.6 - Provide API endpoint for SSL health check
func RegisterSSLRoutes(r chi.Router, handler *ssl.SSLHandler, authMiddleware func(next http.Handler) http.Handler) {
	// SSL routes under /domains/:id/ssl (require auth)
	r.Route("/domains/{id}/ssl", func(r chi.Router) {
		r.Use(authMiddleware)

		// GET /api/v1/domains/:id/ssl - Get SSL status
		r.Get("/", handler.GetSSLStatus)

		// POST /api/v1/domains/:id/ssl/provision - Provision SSL certificate
		r.Post("/provision", handler.ProvisionSSL)

		// POST /api/v1/domains/:id/ssl/renew - Renew SSL certificate
		r.Post("/renew", handler.RenewSSL)
	})

	// SSL health check endpoint (no auth required for monitoring)
	r.Route("/ssl", func(r chi.Router) {
		// GET /api/v1/ssl/health - SSL health check
		r.Get("/health", handler.HealthCheck)
	})
}
