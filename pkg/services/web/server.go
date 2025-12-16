package web

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/Constellation-Overwatch/constellation-overwatch/api/middleware"
	"github.com/Constellation-Overwatch/constellation-overwatch/api/services"
	"github.com/Constellation-Overwatch/constellation-overwatch/db"
	embeddednats "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/embedded-nats"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/logger"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/shared"

	"github.com/nats-io/nats.go"
)

type Server struct {
	db           *db.Service
	nc           *nats.Conn
	natsEmbedded *embeddednats.EmbeddedNATS
	orgSvc       *services.OrganizationService
	entitySvc    *services.EntityService
	sseHandler   *SSEHandler
	apiHandler   http.Handler
	mux          *http.ServeMux
	server       *http.Server
	bindAddr     string
}

// NewServer creates a new web server instance
func NewServer(dbService *db.Service, nc *nats.Conn, natsEmbedded *embeddednats.EmbeddedNATS, apiHandler http.Handler) (*Server, error) {
	s := &Server{
		db:           dbService,
		nc:           nc,
		natsEmbedded: natsEmbedded,
		apiHandler:   apiHandler,
		orgSvc:       services.NewOrganizationService(dbService.GetDB(), natsEmbedded),
		entitySvc:    services.NewEntityService(dbService.GetDB(), natsEmbedded),
		sseHandler:   NewSSEHandler(natsEmbedded.Connection(), natsEmbedded.JetStream()),
	}

	// Initialize session auth
	sessionAuth := middleware.NewSessionAuth()

	// Initialize the router
	s.mux = NewRouter(s.orgSvc, s.entitySvc, s.natsEmbedded, s.sseHandler, s.apiHandler, sessionAuth)

	return s, nil
}

// NewWebService creates a new web service with environment-based configuration
func NewWebService(dbService *db.Service, nc *nats.Conn, natsEmbedded *embeddednats.EmbeddedNATS, apiHandler http.Handler) (*Server, error) {
	server, err := NewServer(dbService, nc, natsEmbedded, apiHandler)
	if err != nil {
		return nil, err
	}

	// Configure bind address from environment
	host := getEnv("HOST", "0.0.0.0")
	port := getEnv("PORT", "8080")
	server.bindAddr = fmt.Sprintf("%s:%s", host, port)

	return server, nil
}

// Start starts the web server
func (s *Server) Start(ctx context.Context) error {
	logger.Infof("Starting web server on %s", s.bindAddr)

	// Bind to the port first to ensure it's available before returning
	listener, err := net.Listen("tcp", s.bindAddr)
	if err != nil {
		return fmt.Errorf("failed to bind to %s: %w", s.bindAddr, err)
	}

	s.server = &http.Server{
		Addr:    s.bindAddr,
		Handler: s.mux,
	}

	go func() {
		if err := s.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			logger.Errorw("Web server failed", "error", err)
		}
	}()

	return nil
}

// Stop stops the web server
func (s *Server) Stop(ctx context.Context) error {
	if s.server != nil {
		logger.Info("Stopping web server...")
		return s.server.Shutdown(ctx)
	}
	return nil
}

// Name returns the service name (implements Service interface)
func (s *Server) Name() string {
	return "web-server"
}

// HealthCheck returns the health status of the web service (implements Service interface)
func (s *Server) HealthCheck() error {
	// Simple check that server is configured
	if s.server == nil {
		return fmt.Errorf("web server not initialized")
	}
	return nil
}

// HandleHealthCheck handles the health check endpoint
// Note: This is now handled by the API router, but kept here if needed for internal checks
func (s *Server) HandleHealthCheck(w http.ResponseWriter, r *http.Request) {
	health := shared.HealthStatus{
		Status:    "healthy",
		Service:   "constellation-overwatch",
		Timestamp: time.Now(),
		Details:   make(map[string]string),
	}

	// Check database
	if err := s.db.GetDB().Ping(); err != nil {
		health.Status = "unhealthy"
		health.Details["database"] = "unhealthy: " + err.Error()
	} else {
		health.Details["database"] = "healthy"
	}

	// Check NATS
	if err := s.natsEmbedded.HealthCheck(); err != nil {
		health.Status = "unhealthy"
		health.Details["nats"] = "unhealthy: " + err.Error()
	} else {
		health.Details["nats"] = "healthy"
	}

	statusCode := http.StatusOK
	if health.Status == "unhealthy" {
		statusCode = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	// Simple JSON response
	fmt.Fprintf(w, `{"status":"%s"}`, health.Status)
}

// getEnv gets environment variable with fallback
func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
