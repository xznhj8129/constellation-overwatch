package handlers

import (
	"net/http"
	"time"

	"github.com/Constellation-Overwatch/constellation-overwatch/api/responses"
	embeddednats "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/embedded-nats"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/shared"
	"database/sql"
)

type HealthHandler struct {
	db   *sql.DB
	nats *embeddednats.EmbeddedNATS
}

func NewHealthHandler(db *sql.DB, nats *embeddednats.EmbeddedNATS) *HealthHandler {
	return &HealthHandler{
		db:   db,
		nats: nats,
	}
}

func (h *HealthHandler) Check(w http.ResponseWriter, r *http.Request) {
	health := shared.HealthStatus{
		Status:    "healthy",
		Service:   "constellation-overwatch",
		Timestamp: time.Now(),
		Details:   make(map[string]string),
	}

	// Check database
	if err := h.db.Ping(); err != nil {
		health.Status = "unhealthy"
		health.Details["database"] = "unhealthy: " + err.Error()
	} else {
		health.Details["database"] = "healthy"
	}

	// Check NATS
	if err := h.nats.HealthCheck(); err != nil {
		health.Status = "unhealthy"
		health.Details["nats"] = "unhealthy: " + err.Error()
	} else {
		health.Details["nats"] = "healthy"
	}

	statusCode := http.StatusOK
	if health.Status == "unhealthy" {
		statusCode = http.StatusServiceUnavailable
	}

	responses.SendSuccess(w, statusCode, health)
}
