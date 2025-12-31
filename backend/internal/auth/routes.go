package auth

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Middleware is an interface for HTTP middleware
type Middleware func(http.Handler) http.Handler

// RegisterRoutes registers all authentication routes with the Chi router
// Public routes: /register, /login, /refresh
// Protected routes: /logout, /me
func RegisterRoutes(r chi.Router, handler *AuthHandler, authMiddleware Middleware) {
	r.Route("/auth", func(r chi.Router) {
		// Public routes (no authentication required)
		r.Post("/register", handler.Register)
		r.Post("/login", handler.Login)
		r.Post("/refresh", handler.Refresh)

		// Protected routes (authentication required)
		r.Group(func(r chi.Router) {
			r.Use(authMiddleware)
			r.Post("/logout", handler.Logout)
			r.Get("/me", handler.GetMe)
		})
	})
}
