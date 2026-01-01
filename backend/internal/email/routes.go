// Package email provides email inbox management functionality
// Feature: email-inbox-api
// Requirements: All
package email

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// RegisterRoutes registers email management routes with the Chi router
// All routes require authentication via auth middleware
// Requirements: All email management endpoints
func RegisterRoutes(r chi.Router, handler *Handler, authMiddleware func(next http.Handler) http.Handler) {
	RegisterRoutesWithRateLimit(r, handler, authMiddleware, nil)
}

// RegisterRoutesWithRateLimit registers email management routes with optional attachment download rate limiting
// All routes require authentication via auth middleware
// Requirements: All email management endpoints, 6.7 (Download rate limiting)
func RegisterRoutesWithRateLimit(r chi.Router, handler *Handler, authMiddleware func(next http.Handler) http.Handler, attachmentRateLimiter func(next http.Handler) http.Handler) {
	r.Route("/emails", func(r chi.Router) {
		// Apply auth middleware to all email routes
		r.Use(authMiddleware)

		// GET /api/v1/emails - List emails (paginated with filters)
		// Requirements: 1.1-1.9
		r.Get("/", handler.List)

		// GET /api/v1/emails/stats - Get inbox statistics
		// Requirements: 6.1-6.5
		r.Get("/stats", handler.GetStats)

		// POST /api/v1/emails/bulk/delete - Bulk delete emails
		// Requirements: 5.1, 5.3, 5.4, 5.5
		r.Post("/bulk/delete", handler.BulkDelete)

		// POST /api/v1/emails/bulk/mark-read - Bulk mark emails as read
		// Requirements: 5.2, 5.3, 5.4, 5.5
		r.Post("/bulk/mark-read", handler.BulkMarkAsRead)

		// GET /api/v1/emails/:id - Get email details
		// Requirements: 2.1-2.8
		r.Get("/{id}", handler.GetByID)

		// DELETE /api/v1/emails/:id - Delete email
		// Requirements: 4.1-4.5
		r.Delete("/{id}", handler.Delete)

		// Attachment download routes with optional rate limiting
		// Requirements: 3.1-3.7, 6.7 (Download rate limiting)
		if attachmentRateLimiter != nil {
			// GET /api/v1/emails/:id/attachments/:attachmentId - Download attachment (rate limited)
			// Query params: inline=true (display inline), url_only=true (return pre-signed URL)
			r.With(attachmentRateLimiter).Get("/{id}/attachments/{attachmentId}", handler.DownloadAttachment)

			// GET /api/v1/emails/:id/attachments/:attachmentId/url - Get pre-signed download URL (rate limited)
			// Requirements: 3.2 (Generate pre-signed URL with 15-minute expiration)
			r.With(attachmentRateLimiter).Get("/{id}/attachments/{attachmentId}/url", handler.GetAttachmentURL)
		} else {
			// GET /api/v1/emails/:id/attachments/:attachmentId - Download attachment
			// Query params: inline=true (display inline), url_only=true (return pre-signed URL)
			r.Get("/{id}/attachments/{attachmentId}", handler.DownloadAttachment)

			// GET /api/v1/emails/:id/attachments/:attachmentId/url - Get pre-signed download URL
			// Requirements: 3.2 (Generate pre-signed URL with 15-minute expiration)
			r.Get("/{id}/attachments/{attachmentId}/url", handler.GetAttachmentURL)
		}
	})
}
