package api

import (
	"database/sql"
	"net/http"

	"github.com/Constellation-Overwatch/constellation-overwatch/api/handlers"
	"github.com/Constellation-Overwatch/constellation-overwatch/api/middleware"
	"github.com/Constellation-Overwatch/constellation-overwatch/api/services"
	embeddednats "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/embedded-nats"

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
	videoHandler := handlers.NewVideoHandler(nats)
	webrtcHandler := handlers.NewWebRTCHandler(nats)
	go func() {
		if err := webrtcHandler.Start(); err != nil {
			// Log error (using fmt for now as we don't have a logger passed in)
			// In a real app, we should pass a logger
			println("WebRTC Handler Start Error:", err.Error())
		}
	}()

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

		// Video streams (no auth - streaming endpoints)
		r.Route("/video", func(r chi.Router) {
			r.Get("/list", videoHandler.List)
			r.Get("/stream/*", videoHandler.Stream)
		})

		// WebRTC Signaling
		r.Post("/webrtc/signal", webrtcHandler.Signal)
	})

	return r
}
