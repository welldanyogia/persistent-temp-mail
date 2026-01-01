// Package sse provides Server-Sent Events (SSE) functionality for real-time notifications.
// Feature: realtime-notifications
// Requirements: 1.1, 1.2 - SSE Connection endpoint with authentication
package sse

import (
	"github.com/go-chi/chi/v5"
)

// RegisterRoutes registers SSE routes with the Chi router.
// The SSE endpoint supports authentication via both query parameter (token) and Authorization header.
// Requirements: 1.1 - THE SSE_Server SHALL provide endpoint GET /api/v1/events/stream
// Requirements: 1.2 - THE SSE_Server SHALL accept authentication via query parameter (token) atau Authorization header
func RegisterRoutes(r chi.Router, handler *Handler) {
	r.Route("/events", func(r chi.Router) {
		// GET /api/v1/events/stream - SSE stream endpoint
		// Authentication is handled internally by the handler to support both:
		// - Query parameter: ?token=<jwt_token>
		// - Authorization header: Bearer <jwt_token>
		// Requirements: 1.1, 1.2
		r.Get("/stream", handler.HandleStream)
	})
}
