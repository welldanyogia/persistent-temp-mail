package alias

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// RegisterRoutes registers alias management routes with the Chi router
// All routes require authentication via auth middleware
// Requirements: All alias management endpoints
func RegisterRoutes(r chi.Router, handler *Handler, authMiddleware func(next http.Handler) http.Handler) {
	r.Route("/aliases", func(r chi.Router) {
		// Apply auth middleware to all alias routes
		r.Use(authMiddleware)

		// POST /api/v1/aliases - Create new alias
		// Requirements: 1.1-1.9, 6.1-6.5, 7.1, 7.3
		r.Post("/", handler.Create)

		// GET /api/v1/aliases - List user aliases (paginated)
		// Requirements: 2.1-2.6
		r.Get("/", handler.List)

		// GET /api/v1/aliases/:id - Get alias details
		// Requirements: 3.1-3.4
		r.Get("/{id}", handler.GetByID)

		// PUT /api/v1/aliases/:id - Update alias
		// Requirements: 4.1-4.5
		r.Put("/{id}", handler.Update)

		// PATCH /api/v1/aliases/:id - Update alias (partial update)
		// Requirements: 4.1-4.5
		r.Patch("/{id}", handler.Update)

		// DELETE /api/v1/aliases/:id - Delete alias
		// Requirements: 5.1-5.5
		r.Delete("/{id}", handler.Delete)
	})
}
