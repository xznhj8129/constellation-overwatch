package workers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/logger"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/shared"
	"github.com/nats-io/nats.go"
)

type EventWorker struct {
	*BaseWorker
	registry *EntityRegistry
	db       *sql.DB
}

func NewEventWorker(nc *nats.Conn, js nats.JetStreamContext, db *sql.DB, registry *EntityRegistry) *EventWorker {
	return &EventWorker{
		BaseWorker: NewBaseWorker(
			"EventWorker",
			nc,
			js,
			shared.StreamEvents,
			shared.ConsumerEventProcessor,
			shared.SubjectEventsAll,
		),
		registry: registry,
		db:       db,
	}
}

func (w *EventWorker) Start(ctx context.Context) error {
	logger.Infow("Starting with entity lifecycle management", "worker", w.Name())
	return w.processMessages(ctx, w.handleEvent)
}

// extractEntityID extracts the entity_id from an event using a consistent fallback chain:
// 1. event.entity_id (preferred)
// 2. event.source.entity_id (nested fallback)
// 3. event.device_id (device fallback)
// 4. event.source.device_id (last resort)
func extractEntityID(event map[string]interface{}) string {
	// Try root level entity_id first
	if entityID, ok := event["entity_id"].(string); ok && entityID != "" {
		return entityID
	}

	// Get source object for fallbacks
	source, hasSource := event["source"].(map[string]interface{})

	// Try source.entity_id
	if hasSource {
		if srcEntityID, ok := source["entity_id"].(string); ok && srcEntityID != "" {
			return srcEntityID
		}
	}

	// Try root level device_id
	if deviceID, ok := event["device_id"].(string); ok && deviceID != "" {
		return deviceID
	}

	// Try source.device_id as last resort
	if hasSource {
		if srcDeviceID, ok := source["device_id"].(string); ok && srcDeviceID != "" {
			return srcDeviceID
		}
	}

	return ""
}

func (w *EventWorker) handleEvent(msg *nats.Msg) error {
	var event map[string]interface{}
	if err := json.Unmarshal(msg.Data, &event); err != nil {
		logger.Errorw("Failed to unmarshal event", "worker", w.Name(), "error", err)
		return fmt.Errorf("failed to unmarshal event: %w", err)
	}

	eventType, _ := event["event_type"].(string)
	entityID := extractEntityID(event)

	// Log all events for debugging
	prettyJSON, _ := json.MarshalIndent(event, "", "  ")
	logger.Debugw("Processing event", "worker", w.Name(), "event_type", eventType, "entity_id", entityID, "event_data", string(prettyJSON))

	// Handle lifecycle events
	switch eventType {
	case "bootsequence":
		w.handleBootSequence(event)
	case "shutdown", "delete":
		w.handleShutdown(entityID)
	}

	return nil
}

func (w *EventWorker) handleBootSequence(event map[string]interface{}) {
	// Use shared extraction function for consistent entity_id resolution
	entityID := extractEntityID(event)

	if entityID == "" {
		logger.Warnw("Bootsequence event missing both entity_id and device_id, cannot register", "worker", w.Name())
		return
	}

	// Validate entity_id format
	if err := validateEntityID(entityID); err != nil {
		logger.Warnw("Invalid entity_id in bootsequence", "worker", w.Name(), "entity_id", entityID, "error", err)
		return
	}

	// Register in memory
	w.registry.Register(entityID)

	// TODO: Optionally persist to database if needed
	// For now, we assume entities are created via API and this just tracks online status

	logger.Infow("Entity registered from bootsequence", "worker", w.Name(), "entity_id", entityID)
}

func (w *EventWorker) handleShutdown(entityID string) {
	if entityID == "" {
		logger.Warnw("Shutdown event missing entity_id (already checked in handleEvent)", "worker", w.Name())
		return
	}

	// Only unregister if it was actually registered
	if w.registry.IsRegistered(entityID) {
		w.registry.Unregister(entityID)
		logger.Infow("Entity unregistered from shutdown", "worker", w.Name(), "entity_id", entityID)
	} else {
		logger.Debugw("Entity not in registry, skipping unregister", "worker", w.Name(), "entity_id", entityID)
	}
}
