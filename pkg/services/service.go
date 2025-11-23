package services

import (
	"context"
)

// Service defines the common interface for all services
type Service interface {
	// Start initializes and starts the service
	Start(ctx context.Context) error

	// Stop gracefully shuts down the service
	Stop(ctx context.Context) error

	// Name returns the service name for logging
	Name() string

	// HealthCheck returns the health status of the service
	HealthCheck() error
}

// Manager orchestrates multiple services with proper startup/shutdown order
type Manager struct {
	services []Service
}

// NewManager creates a new service manager
func NewManager() *Manager {
	return &Manager{
		services: make([]Service, 0),
	}
}

// AddService registers a service with the manager
func (m *Manager) AddService(service Service) {
	m.services = append(m.services, service)
}

// Start starts all services in order
func (m *Manager) Start(ctx context.Context) error {
	for _, service := range m.services {
		if err := service.Start(ctx); err != nil {
			return err
		}
	}
	return nil
}

// Stop stops all services in reverse order
func (m *Manager) Stop(ctx context.Context) error {
	// Stop services in reverse order
	for i := len(m.services) - 1; i >= 0; i-- {
		if err := m.services[i].Stop(ctx); err != nil {
			// Log error but continue stopping other services
			continue
		}
	}
	return nil
}
