package workers

import (
	"context"
	"database/sql"
	"encoding/json"

	"constellation-overwatch/pkg/services/logger"
	"constellation-overwatch/pkg/shared"
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

func (w *EventWorker) handleEvent(msg *nats.Msg) {
	var event map[string]interface{}
	if err := json.Unmarshal(msg.Data, &event); err != nil {
		logger.Errorw("Failed to unmarshal event", "worker", w.Name(), "error", err)
		return
	}

	eventType, _ := event["event_type"].(string)
	entityID, _ := event["entity_id"].(string)

	// Try to get entity_id from various locations if empty
	if entityID == "" {
		// Try root level device_id
		if deviceID, ok := event["device_id"].(string); ok && deviceID != "" {
			entityID = deviceID
		} else if source, ok := event["source"].(map[string]interface{}); ok {
			// Try source.entity_id
			if srcEntityID, ok := source["entity_id"].(string); ok && srcEntityID != "" {
				entityID = srcEntityID
			} else if srcDeviceID, ok := source["device_id"].(string); ok && srcDeviceID != "" {
				// Try source.device_id
				entityID = srcDeviceID
			}
		}
	}

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
}

func (w *EventWorker) handleBootSequence(event map[string]interface{}) {
	// Extract entity_id - could be in the event or source
	entityID, _ := event["entity_id"].(string)

	// Get source object for fallbacks
	source, hasSource := event["source"].(map[string]interface{})

	// If not in event, check source object
	if entityID == "" && hasSource {
		entityID, _ = source["entity_id"].(string)
	}

	// If still no entity_id, try device_id as fallback (check both root and source)
	if entityID == "" {
		// Try root level device_id first
		if deviceID, ok := event["device_id"].(string); ok && deviceID != "" {
			logger.Infow("No entity_id in bootsequence, using device_id from root", "worker", w.Name(), "device_id", deviceID)
			entityID = deviceID
		} else if hasSource {
			// Try device_id inside source
			if deviceID, ok := source["device_id"].(string); ok && deviceID != "" {
				logger.Infow("No entity_id in bootsequence, using device_id from source", "worker", w.Name(), "device_id", deviceID)
				entityID = deviceID
			}
		}
	}

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