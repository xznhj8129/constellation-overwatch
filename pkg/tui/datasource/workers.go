package datasource

import (
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/workers"
)

// WorkerHealthChecker is an interface for workers that support health checks
type WorkerHealthChecker interface {
	workers.Worker
	HealthCheck() error
}

// WorkersMonitor monitors worker health status
type WorkersMonitor struct {
	manager *workers.Manager
}

// NewWorkersMonitor creates a new workers monitor
func NewWorkersMonitor(manager *workers.Manager) *WorkersMonitor {
	return &WorkersMonitor{
		manager: manager,
	}
}

// GetStatuses returns the current status of all workers
func (w *WorkersMonitor) GetStatuses() []WorkerStatus {
	workerList := w.manager.GetWorkers()
	statuses := make([]WorkerStatus, 0, len(workerList))

	for _, worker := range workerList {
		healthy := true

		// Check if worker implements HealthCheck
		if hc, ok := worker.(WorkerHealthChecker); ok {
			healthy = hc.HealthCheck() == nil
		}

		statuses = append(statuses, WorkerStatus{
			Name:    worker.Name(),
			Healthy: healthy,
		})
	}

	return statuses
}
