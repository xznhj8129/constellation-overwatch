package workers

import (
	"database/sql"
	"encoding/json"
	"sync"
	"time"

	"constellation-overwatch/pkg/services/logger"
	"constellation-overwatch/pkg/shared"

	"github.com/nats-io/nats.go"
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

// InitializeKVStoreFromDB ensures all entities in the database have a corresponding KV entry
// This is called on boot to populate the KV store with initial entity states
func (r *EntityRegistry) InitializeKVStoreFromDB(kv nats.KeyValue) error {
	logger.Infow("🔧 Initializing KV store from database entities", "component", "EntityRegistry")

	// First, count how many entities exist in the registry
	entityCount := r.Count()
	logger.Infow("📊 Entity registry status", "component", "EntityRegistry", "registered_entities", entityCount)

	// Query all entities from the database - keep it simple, just essential info
	// Publishers (telemetry, detection workers) will fill in the rest
	query := `
		SELECT e.entity_id, e.org_id, o.name as org_name, e.entity_type, COALESCE(e.name, '') as name
		FROM entities e
		LEFT JOIN organizations o ON e.org_id = o.org_id`

	logger.Infow("🔍 Querying database for entities", "component", "EntityRegistry")
	rows, err := r.db.Query(query)
	if err != nil {
		logger.Errorw("❌ Failed to query entities from database", "component", "EntityRegistry", "error", err)
		return err
	}
	defer rows.Close()

	initialized := 0
	skipped := 0

	for rows.Next() {
		var entityID, orgID, orgName, entityType, name string

		if err := rows.Scan(&entityID, &orgID, &orgName, &entityType, &name); err != nil {
			logger.Errorw("Error scanning entity row", "component", "EntityRegistry", "error", err)
			continue
		}

		// Check if KV entry already exists
		kvKey := shared.EntityKey(entityID)
		_, err := kv.Get(kvKey)
		if err == nil {
			// Entry already exists, skip
			skipped++
			continue
		}

		// Create minimal initial EntityState - just enough for the UI to display
		// Telemetry and detection workers will populate the rest
		state := shared.EntityState{
			EntityID:   entityID,
			OrgID:      orgID,
			OrgName:    orgName,
			Name:       name,
			EntityType: entityType,
			Status:     "unknown", // Will be updated by telemetry
			Priority:   "normal",  // Default
			IsLive:     false,     // Will be set true when telemetry arrives
			Components: make(map[string]interface{}),
			Aliases:    make(map[string]string),
			Tags:       []string{},
			Metadata:   make(map[string]interface{}),
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}

		// Marshal and store in KV
		data, err := json.Marshal(state)
		if err != nil {
			logger.Errorw("Failed to marshal entity state", "component", "EntityRegistry", "entity_id", entityID, "error", err)
			continue
		}

		if _, err := kv.Create(kvKey, data); err != nil {
			logger.Errorw("Failed to create KV entry", "component", "EntityRegistry", "entity_id", entityID, "error", err)
			continue
		}

		initialized++
		logger.Infow("✅ Initialized KV entry for entity", "component", "EntityRegistry", "entity_id", entityID, "org_id", orgID, "kv_key", kvKey)
	}

	logger.Infow("🎉 KV store initialization complete", "component", "EntityRegistry", "initialized", initialized, "skipped", skipped, "total_processed", initialized+skipped)

	if initialized == 0 && skipped == 0 {
		logger.Warnw("⚠️  No entities were processed - database might be empty", "component", "EntityRegistry")
	}

	return rows.Err()
}
