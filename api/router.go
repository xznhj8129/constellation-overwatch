package api

import (
	"database/sql"
	"net/http"

	"constellation-overwatch/api/handlers"
	"constellation-overwatch/api/middleware"
	"constellation-overwatch/api/services"
	embeddednats "constellation-overwatch/pkg/services/embedded-nats"

	"github.com/go-chi/chi/v5"
)

func NewRouter(db *sql.DB, nats *embeddednats.EmbeddedNATS) http.Handler {
	r := chi.NewRouter()

	// Services
	orgService := services.NewOrganizationService(db, nats)
	entityService := services.NewEntityService(db, nats)

	// Handlers
	healthHandler := handlers.NewHealthHandler(db, nats)
	orgHandler := handlers.NewOrganizationHandler(orgService)
	entityHandler := handlers.NewEntityHandler(entityService)
	monitorHandler := handlers.NewMonitorHandler()

	// Global Middleware
	r.Use(middleware.CORS)

	// Routes
	r.Route("/v1", func(r chi.Router) {
		// Health
		r.Get("/health", healthHandler.Check)

		// System Monitor
		r.Get("/sys/monitor/sse", monitorHandler.SSE)

		// Organizations
		r.Route("/organizations", func(r chi.Router) {
			r.Use(middleware.BearerAuth)
			r.Get("/", orgHandler.List)
			r.Post("/", orgHandler.Create)
			r.Delete("/", orgHandler.Delete)
		})

		// Entities
		r.Route("/entities", func(r chi.Router) {
			r.Use(middleware.BearerAuth)
			r.Get("/", entityHandler.List)
			r.Post("/", entityHandler.Create)
			r.Put("/", entityHandler.Update)
			r.Delete("/", entityHandler.Delete)
		})
	})

	return r
}
