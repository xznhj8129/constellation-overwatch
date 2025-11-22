package workers

import (
	"database/sql"
	"sync"

	"constellation-overwatch/pkg/services/logger"
)

// EntityRegistry maintains an in-memory set of registered entity IDs
// This prevents excessive database reads for telemetry validation
type EntityRegistry struct {
	entities map[string]bool
	mu       sync.RWMutex
	db       *sql.DB
}

// NewEntityRegistry creates a new entity registry and loads existing entities from DB
func NewEntityRegistry(db *sql.DB) (*EntityRegistry, error) {
	registry := &EntityRegistry{
		entities: make(map[string]bool),
		db:       db,
	}

	// Load existing entities from database
	if err := registry.LoadFromDB(); err != nil {
		return nil, err
	}

	return registry, nil
}

// LoadFromDB loads all entity IDs from the database into memory
func (r *EntityRegistry) LoadFromDB() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	rows, err := r.db.Query(`SELECT entity_id FROM entities`)
	if err != nil {
		return err
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var entityID string
		if err := rows.Scan(&entityID); err != nil {
			logger.Errorw("Error scanning entity_id", "component", "EntityRegistry", "error", err)
			continue
		}
		r.entities[entityID] = true
		count++
	}

	logger.Infow("Loaded entities from database", "component", "EntityRegistry", "count", count)
	return rows.Err()
}

// Register adds an entity_id to the registry
func (r *EntityRegistry) Register(entityID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entities[entityID] = true
	logger.Infow("Registered entity", "component", "EntityRegistry", "entity_id", entityID, "total", len(r.entities))
}

// Unregister removes an entity_id from the registry
func (r *EntityRegistry) Unregister(entityID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entities, entityID)
	logger.Infow("Unregistered entity", "component", "EntityRegistry", "entity_id", entityID, "total", len(r.entities))
}

// IsRegistered checks if an entity_id is in the registry
func (r *EntityRegistry) IsRegistered(entityID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.entities[entityID]
}

// Count returns the number of registered entities
func (r *EntityRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.entities)
}

// GetAll returns all registered entity IDs
func (r *EntityRegistry) GetAll() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ids := make([]string, 0, len(r.entities))
	for id := range r.entities {
		ids = append(ids, id)
	}
	return ids
}
