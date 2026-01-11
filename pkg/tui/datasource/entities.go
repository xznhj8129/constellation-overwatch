package datasource

import (
	"encoding/json"

	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/workers"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/shared"

	"github.com/nats-io/nats.go"
)

// EntityMonitor monitors entity state from the KV store
type EntityMonitor struct {
	registry *workers.EntityRegistry
	kv       nats.KeyValue
}

// NewEntityMonitor creates a new entity monitor
func NewEntityMonitor(registry *workers.EntityRegistry, kv nats.KeyValue) *EntityMonitor {
	return &EntityMonitor{
		registry: registry,
		kv:       kv,
	}
}

// GetEntities returns a summary of all registered entities
func (e *EntityMonitor) GetEntities() []EntitySummary {
	ids := e.registry.GetAll()
	summaries := make([]EntitySummary, 0, len(ids))

	for _, id := range ids {
		entry, err := e.kv.Get(shared.EntityKey(id))
		if err != nil {
			// Entity in registry but not in KV - create placeholder
			summaries = append(summaries, EntitySummary{
				ID:         id,
				Name:       "",
				EntityType: "unknown",
				Status:     "unknown",
				IsLive:     false,
				OrgID:      "",
			})
			continue
		}

		var state shared.EntityState
		if err := json.Unmarshal(entry.Value(), &state); err != nil {
			continue
		}

		summaries = append(summaries, EntitySummary{
			ID:         state.EntityID,
			Name:       state.Name,
			EntityType: state.EntityType,
			Status:     state.Status,
			IsLive:     state.IsLive,
			OrgID:      state.OrgID,
		})
	}

	return summaries
}
