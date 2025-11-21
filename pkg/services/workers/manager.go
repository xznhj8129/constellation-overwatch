package workers

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sync"

	embeddednats "constellation-overwatch/pkg/services/embedded-nats"
	"github.com/nats-io/nats.go"
)

type Manager struct {
	workers  []Worker
	nc       *nats.Conn
	js       nats.JetStreamContext
	db       *sql.DB
	kv       nats.KeyValue
	registry *EntityRegistry
	wg       sync.WaitGroup
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewManager creates a worker manager with database and KV store access
func NewManager(natsClient *embeddednats.EmbeddedNATS, db *sql.DB) (*Manager, error) {
	nc := natsClient.Connection()
	if nc == nil {
		return nil, fmt.Errorf("NATS connection not initialized")
	}

	js := natsClient.JetStream()
	if js == nil {
		return nil, fmt.Errorf("JetStream not initialized")
	}

	// Get or create KV store for global state
	kv := natsClient.KeyValue()
	if kv == nil {
		return nil, fmt.Errorf("KV store not initialized")
	}

	// Create entity registry and load existing entities from DB
	registry, err := NewEntityRegistry(db)
	if err != nil {
		return nil, fmt.Errorf("failed to create entity registry: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Manager{
		nc:       nc,
		js:       js,
		db:       db,
		kv:       kv,
		registry: registry,
		ctx:      ctx,
		cancel:   cancel,
		workers: []Worker{
			NewTelemetryWorker(nc, js, db, kv, registry),
			NewEntityWorker(nc, js),
			NewEventWorker(nc, js, db, registry),
			NewCommandWorker(nc, js),
		},
	}, nil
}

func (m *Manager) Start() error {
	log.Println("Starting NATS workers...")

	for _, worker := range m.workers {
		m.wg.Add(1)
		go func(w Worker) {
			defer m.wg.Done()
			
			log.Printf("Starting worker: %s", w.Name())
			if err := w.Start(m.ctx); err != nil && err != context.Canceled {
				log.Printf("Worker %s error: %v", w.Name(), err)
			}
			log.Printf("Worker %s stopped", w.Name())
		}(worker)
	}

	log.Printf("Started %d workers", len(m.workers))
	return nil
}

func (m *Manager) Stop() error {
	log.Println("Stopping NATS workers...")
	
	m.cancel()
	
	for _, worker := range m.workers {
		if err := worker.Stop(); err != nil {
			log.Printf("Error stopping worker %s: %v", worker.Name(), err)
		}
	}
	
	m.wg.Wait()
	
	if m.nc != nil {
		m.nc.Close()
	}
	
	log.Println("All workers stopped")
	return nil
}