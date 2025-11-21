package workers

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"

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
	log.Printf("[%s] Starting with entity lifecycle management...", w.Name())
	return w.processMessages(ctx, w.handleEvent)
}

func (w *EventWorker) handleEvent(msg *nats.Msg) {
	var event map[string]interface{}
	if err := json.Unmarshal(msg.Data, &event); err != nil {
		log.Printf("[%s] Failed to unmarshal event: %v", w.Name(), err)
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
	if log.Default().Writer() != nil {
		prettyJSON, _ := json.MarshalIndent(event, "", "  ")
		log.Printf("[%s] Event: %s | entity_id: %s\n%s", w.Name(), eventType, entityID, string(prettyJSON))
	}

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
			log.Printf("[%s] No entity_id in bootsequence, using device_id from root: %s", w.Name(), deviceID)
			entityID = deviceID
		} else if hasSource {
			// Try device_id inside source
			if deviceID, ok := source["device_id"].(string); ok && deviceID != "" {
				log.Printf("[%s] No entity_id in bootsequence, using device_id from source: %s", w.Name(), deviceID)
				entityID = deviceID
			}
		}
	}

	if entityID == "" {
		log.Printf("[%s] Bootsequence event missing both entity_id and device_id, cannot register", w.Name())
		return
	}

	// Validate entity_id format
	if err := validateEntityID(entityID); err != nil {
		log.Printf("[%s] Invalid entity_id in bootsequence '%s': %v", w.Name(), entityID, err)
		return
	}

	// Register in memory
	w.registry.Register(entityID)

	// TODO: Optionally persist to database if needed
	// For now, we assume entities are created via API and this just tracks online status

	log.Printf("[%s] Entity %s registered from bootsequence", w.Name(), entityID)
}

func (w *EventWorker) handleShutdown(entityID string) {
	if entityID == "" {
		log.Printf("[%s] Shutdown event missing entity_id (already checked in handleEvent)", w.Name())
		return
	}

	// Only unregister if it was actually registered
	if w.registry.IsRegistered(entityID) {
		w.registry.Unregister(entityID)
		log.Printf("[%s] Entity %s unregistered from shutdown", w.Name(), entityID)
	} else {
		log.Printf("[%s] Entity %s not in registry, skipping unregister", w.Name(), entityID)
	}
}