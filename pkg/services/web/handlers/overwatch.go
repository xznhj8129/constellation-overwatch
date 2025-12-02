package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Constellation-Overwatch/constellation-overwatch/api/services"
	embeddednats "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/embedded-nats"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/logger"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/datastar"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/templates"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/shared"

	"github.com/nats-io/nats.go"
)

type OverwatchHandler struct {
	natsEmbedded *embeddednats.EmbeddedNATS
	orgSvc       *services.OrganizationService
}

func NewOverwatchHandler(natsEmbedded *embeddednats.EmbeddedNATS, orgSvc *services.OrganizationService) *OverwatchHandler {
	return &OverwatchHandler{
		natsEmbedded: natsEmbedded,
		orgSvc:       orgSvc,
	}
}

// API handler for Overwatch KV store
func (h *OverwatchHandler) HandleAPIOverwatchKV(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get all keys from the KV store
	kv := h.natsEmbedded.KeyValue()
	if kv == nil {
		http.Error(w, "KV store not initialized", http.StatusInternalServerError)
		return
	}

	// Get all keys using Keys() method
	keys, err := kv.Keys()
	if err != nil {
		logger.Infow("Error fetching KV keys: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Fetch all entries
	var kvEntries []templates.KVEntry
	for _, key := range keys {
		entry, err := kv.Get(key)
		if err != nil {
			logger.Infow("Error getting key %s: %v", key, err)
			continue
		}

		kvEntries = append(kvEntries, templates.KVEntry{
			Key:      key,
			Value:    string(entry.Value()),
			Revision: fmt.Sprintf("%d", entry.Revision()),
			Updated:  entry.Created().Format("15:04:05"),
		})
	}

	// If this is a Datastar request, return SSE format
	if r.Header.Get("Accept") == "text/event-stream" {
		sse := datastar.NewServerSentEventGenerator(w, r)
		component := templates.KVStateTable(kvEntries)
		err := sse.PatchComponent(r.Context(), component,
			datastar.WithSelector("#kv-content"),
			datastar.WithMode(datastar.ElementPatchModeInner))
		if err != nil {
			logger.Infow("Error patching KV content: %v", err)
		}
		return
	}

	// Otherwise return JSON
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"entries": kvEntries,
	})
}

// API handler for real-time KV watching via SSE
func (h *OverwatchHandler) HandleAPIOverwatchKVWatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Verify we have a flusher (required for SSE)
	flusher, ok := w.(http.Flusher)
	if !ok {
		logger.Infow("[Overwatch] ERROR: ResponseWriter does not support flushing (SSE won't work)")
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// CRITICAL: Set SSE headers BEFORE creating SSE generator or writing anything
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	logger.Infow("[Overwatch] ✓ SSE headers set, establishing connection", "remote_addr", r.RemoteAddr)

	// Create SSE generator AFTER setting headers
	sse := datastar.NewServerSentEventGenerator(w, r)

	// Mutex to synchronize writes to ResponseWriter from multiple goroutines
	var writeMutex sync.Mutex

	// Send an immediate comment to establish the SSE stream in the browser
	writeMutex.Lock()
	fmt.Fprintf(w, ": SSE connection established\n\n")
	flusher.Flush()
	writeMutex.Unlock()
	logger.Infow("SSE client connected", "component", "Overwatch", "remote_addr", r.RemoteAddr)

	// Local cache of all KV data: entityID -> key -> data
	// This allows us to reconstruct a single entity's state without fetching everything
	localEntityCache := make(map[string]map[string][]byte)

	// Track known entities and orgs to determine patch strategy
	knownEntities := make(map[string]bool)
	knownOrgs := make(map[string]bool)

	// Struct to pass data to renderer
	type RenderPayload struct {
		Snapshot      []shared.EntityState
		TotalEntities int
	}

	// Channel to buffer updates from NATS to SSE
	// Increased buffer size to handle initial state dump and high throughput
	updateChan := make(chan nats.KeyValueEntry, 10000)

	// Channel to send snapshots to the renderer
	// Buffer of 1 allows us to have one snapshot pending while the renderer is busy.
	// If the renderer is too slow, we drop intermediate snapshots (conflation).
	renderChan := make(chan RenderPayload, 1)

	// Load initial KV data before starting watcher
	ctx := r.Context()
	go func() {
		defer close(updateChan)

		// STEP 1: Load existing KV data on initial connection
		logger.Infow("[Overwatch] Loading initial KV state...")
		if initialEntries, err := h.natsEmbedded.GetAllKVEntries(); err == nil {
			logger.Infow("[Overwatch] Loaded initial state", "kv_entries", len(initialEntries))

			// Send existing entries through the update channel to populate initial state
			for _, entry := range initialEntries {
				select {
				case updateChan <- entry:
					// Successfully queued initial entry
				case <-ctx.Done():
					return
				case <-time.After(100 * time.Millisecond):
					// Skip if channel is backed up during initial load
					logger.Debugw("[Overwatch] Skipped initial entry due to channel backup", "key", entry.Key())
				}
			}
			logger.Infow("[Overwatch] Initial state load completed", "entities_loaded", len(initialEntries))
		} else {
			logger.Warnw("[Overwatch] Failed to load initial KV state", "error", err)
		}

		// STEP 2: Start watching for real-time changes
		// Retry loop for the watcher
		for {
			if ctx.Err() != nil {
				return
			}

			logger.Infow("[Overwatch] KV watcher goroutine started, waiting for changes...")

			watchErr := h.natsEmbedded.WatchKV(ctx, func(key string, entry nats.KeyValueEntry) error {
				select {
				case updateChan <- entry:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})

			if ctx.Err() != nil {
				return
			}

			if watchErr != nil {
				logger.Warnw("[Overwatch] KV watcher stopped unexpectedly, restarting...", "error", watchErr)
			} else {
				logger.Warnw("[Overwatch] KV watcher channel closed unexpectedly, restarting...")
			}

			select {
			case <-time.After(1 * time.Second):
				continue
			case <-ctx.Done():
				return
			}
		}
	}()

	// Start Renderer Goroutine
	go func() {
		for {
			select {
			case <-ctx.Done():
				logger.Debugw("Renderer goroutine stopping - context canceled")
				return
			case payload, ok := <-renderChan:
				if !ok {
					logger.Debugw("Renderer goroutine stopping - render channel closed")
					return
				}

				// Check if context is still valid before rendering
				if ctx.Err() != nil {
					logger.Debugw("Context canceled, skipping render")
					return
				}

				// Render and Flush
				h.renderAndFlushSnapshot(w, flusher, &writeMutex, sse, payload.Snapshot, payload.TotalEntities, knownEntities, knownOrgs)
			}
		}
	}()

	// Keep the connection alive with heartbeats
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	// Flush ticker for batching
	flushTicker := time.NewTicker(50 * time.Millisecond)
	defer flushTicker.Stop()

	dirtyEntities := make(map[string]bool)

	// State Manager Loop (Main Goroutine)
	// This loop MUST be fast to keep up with NATS.
	// It does NO rendering.
	for {
		select {
		case <-ctx.Done():
			logger.Infow("[Overwatch] Client disconnected", "remote_addr", r.RemoteAddr)
			return

		case <-ticker.C:
			// Send heartbeat only if connection is still valid
			if flusher != nil {
				writeMutex.Lock()
				if flusher != nil { // Double-check after acquiring lock
					fmt.Fprintf(w, ": heartbeat\n\n")
					func() {
						defer func() {
							if r := recover(); r != nil {
								logger.Debugw("Recovered from heartbeat flush panic", "panic", r)
							}
						}()
						flusher.Flush()
					}()
				}
				writeMutex.Unlock()
			}

		case <-flushTicker.C:
			// Periodic flush of dirty entities
			if len(dirtyEntities) > 0 {
				// Create snapshot
				var snapshot []shared.EntityState
				for entityID := range dirtyEntities {
					entityData, exists := localEntityCache[entityID]
					if !exists {
						continue
					}
					// Reconstruct state (fast, just map lookups and struct creation)
					entityState := h.mergeEntityData(entityID, entityData)
					snapshot = append(snapshot, entityState)
				}

				// Try to send to renderer (non-blocking)
				payload := RenderPayload{
					Snapshot:      snapshot,
					TotalEntities: len(localEntityCache),
				}

				select {
				case renderChan <- payload:
					// Success, renderer will handle it
					dirtyEntities = make(map[string]bool)
				default:
					// Renderer is busy, skip this frame (conflation)
					// We keep the entities dirty so they are included in the next snapshot
					logger.Debugw("[Overwatch] Renderer busy, skipping frame (conflation)", "pending_entities", len(dirtyEntities))
				}
			}

		case entry, ok := <-updateChan:
			if !ok {
				logger.Infow("[Overwatch] Update channel closed, stopping SSE stream")
				return
			}

			// Process KV entry with enhanced signal handling
			key := entry.Key()
			if key == "" {
				logger.Warnw("[Overwatch] Received entry with empty key, skipping")
				continue
			}

			parts := strings.Split(key, ".")
			if len(parts) == 0 {
				logger.Warnw("[Overwatch] Invalid key format", "key", key)
				continue
			}

			entityID := parts[0]
			if entityID == "" {
				logger.Warnw("[Overwatch] Empty entity ID", "key", key)
				continue
			}

			// Initialize entity cache if needed
			if localEntityCache[entityID] == nil {
				localEntityCache[entityID] = make(map[string][]byte)
				logger.Debugw("[Overwatch] Initialized cache for new entity", "entity_id", entityID)
			}

			// Handle delete operations
			if entry.Operation() == nats.KeyValueDelete || entry.Operation() == nats.KeyValuePurge {
				delete(localEntityCache[entityID], key)
				if len(localEntityCache[entityID]) == 0 {
					delete(localEntityCache, entityID)
					logger.Debugw("[Overwatch] Removed entity from cache", "entity_id", entityID)
				} else {
					logger.Debugw("[Overwatch] Removed signal from entity", "entity_id", entityID, "key", key)
				}
			} else {
				// Handle create/update operations
				value := entry.Value()
				if len(value) == 0 {
					logger.Debugw("[Overwatch] Received empty value", "key", key)
					// Store empty value to indicate signal exists but has no data
					localEntityCache[entityID][key] = []byte("{}")
				} else {
					// Validate JSON before storing
					var testJSON interface{}
					if err := json.Unmarshal(value, &testJSON); err != nil {
						logger.Warnw("[Overwatch] Invalid JSON in KV entry, storing as-is", "key", key, "error", err)
						// Store anyway - merge functions will handle gracefully
					}
					localEntityCache[entityID][key] = value
					logger.Debugw("[Overwatch] Updated entity signal", "entity_id", entityID, "key", key, "size", len(value))
				}
			}

			// Mark entity as dirty for rendering
			dirtyEntities[entityID] = true
		}
	}
}

// Helper to render and flush a snapshot
func (h *OverwatchHandler) renderAndFlushSnapshot(w http.ResponseWriter, flusher http.Flusher, writeMutex *sync.Mutex, sse *datastar.ServerSentEventGenerator, snapshot []shared.EntityState, totalEntities int, knownEntities map[string]bool, knownOrgs map[string]bool) {
	// Check if flusher is nil to prevent panic
	if flusher == nil {
		logger.Debugw("Flusher is nil, connection likely closed, skipping render")
		return
	}

	writeMutex.Lock()
	defer writeMutex.Unlock()

	// Double-check flusher after acquiring mutex
	if flusher == nil {
		logger.Debugw("Flusher became nil while waiting for mutex, connection closed")
		return
	}

	updatesSent := 0

	for _, entityState := range snapshot {
		// Render card
		var cardHTML strings.Builder
		if err := templates.EntityCard(entityState).Render(context.Background(), &cardHTML); err != nil {
			logger.Errorw("Error rendering entity card", "error", err)
			continue
		}

		// Determine patch mode
		var patchMode datastar.PatchElementMode
		var selector string
		entityID := entityState.EntityID

		if !knownEntities[entityID] {
			// New entity
			if !knownOrgs[entityState.OrgID] {
				// Create Org Container
				if len(knownOrgs) == 0 {
					if err := sse.PatchElements("", datastar.WithSelector(".empty-state"), datastar.WithMode(datastar.ElementPatchModeRemove)); err != nil {
						logger.Debugw("Failed to patch empty state, connection may be closed", "error", err)
						return
					}
				}

				var orgHTML strings.Builder
				orgName := entityState.OrgID
				if entityState.OrgName != "" {
					orgName = entityState.OrgName
				}
				orgHTML.WriteString(fmt.Sprintf(`<div style="margin-bottom: 30px;"><h3 style="color: #0ff; border-bottom: 2px solid #444; padding-bottom: 10px; margin-bottom: 15px;">Organization: %s</h3><div id="org-cards-%s" class="entity-cards-container" style="display: grid; grid-template-columns: repeat(auto-fill, minmax(420px, 1fr)); gap: 15px;"></div></div>`, orgName, entityState.OrgID))

				if err := sse.PatchElements(orgHTML.String(), datastar.WithSelector("#entities-container"), datastar.WithMode(datastar.ElementPatchModeAppend)); err != nil {
					logger.Debugw("Failed to patch org container, connection may be closed", "error", err)
					return
				}
				knownOrgs[entityState.OrgID] = true

				// Initialize signal
				if err := sse.PatchSignals(map[string]interface{}{
					fmt.Sprintf("entityStatesByOrg.%s", entityState.OrgID): map[string]interface{}{},
				}); err != nil {
					logger.Debugw("Failed to patch org signals, connection may be closed", "error", err)
					return
				}
			}

			patchMode = datastar.ElementPatchModeAppend
			selector = fmt.Sprintf("#org-cards-%s", entityState.OrgID)
			knownEntities[entityID] = true
		} else {
			patchMode = datastar.ElementPatchModeMorph
			selector = fmt.Sprintf("#entity-%s", entityID)
		}

		// Patch Element
		if err := sse.PatchElements(cardHTML.String(), datastar.WithSelector(selector), datastar.WithMode(patchMode)); err != nil {
			logger.Debugw("Failed to patch entity, connection may be closed", "entity_id", entityID, "error", err)
			return
		}

		// Patch Signal
		if err := sse.PatchSignals(map[string]interface{}{
			fmt.Sprintf("entityStatesByOrg.%s.%s", entityState.OrgID, entityID): entityState,
		}); err != nil {
			logger.Debugw("Failed to patch entity signals, connection may be closed", "entity_id", entityID, "error", err)
			return
		}

		updatesSent++
	}

	if updatesSent > 0 {
		// Get total orgs from DB for accurate count
		orgs, _ := h.orgSvc.ListOrganizations()
		totalOrgs := len(orgs)

		if err := sse.PatchSignals(map[string]interface{}{
			"lastUpdate":    time.Now().Format("15:04:05"),
			"totalEntities": totalEntities,
			"totalOrgs":     totalOrgs,
			"_isConnected":  true,
		}); err != nil {
			logger.Debugw("Failed to patch final signals, connection may be closed", "error", err)
			return
		}

		// Safe flush with recovery
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Debugw("Recovered from flush panic, connection likely closed", "panic", r)
				}
			}()
			if flusher != nil {
				flusher.Flush()
			}
		}()
	}
}

// API handler for debugging KV data structure
func (h *OverwatchHandler) HandleAPIOverwatchKVDebug(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get all KV entries
	entries, err := h.natsEmbedded.GetAllKVEntries()
	if err != nil {
		logger.Infof("[Overwatch Debug] Error fetching KV entries: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Parse into entity states
	entityStatesByOrg := h.parseKVEntriesToEntityStates(entries)

	// Create the same structure we send via SSE
	response := map[string]interface{}{
		"entityStatesByOrg": entityStatesByOrg,
		"lastUpdate":        time.Now().Format("15:04:05"),
		"_isConnected":      true,
		"totalOrgs":         len(entityStatesByOrg),
		"totalEntities":     0,
	}

	for _, entities := range entityStatesByOrg {
		response["totalEntities"] = response["totalEntities"].(int) + len(entities)
	}

	// Return as JSON
	w.Header().Set("Content-Type", "application/json")
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(response); err != nil {
		logger.Infof("[Overwatch Debug] Error encoding JSON: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// parseKVEntriesToEntityStates parses KV entries and aggregates them by entity_id
func (h *OverwatchHandler) parseKVEntriesToEntityStates(entries []nats.KeyValueEntry) map[string][]shared.EntityState {
	// First, group entries by entity_id
	entitiesByID := make(map[string]map[string][]byte)

	for _, entry := range entries {
		key := entry.Key()

		// Extract entity_id from key (before first dot or entire key if no dot)
		parts := strings.Split(key, ".")
		entityID := parts[0]

		if entitiesByID[entityID] == nil {
			entitiesByID[entityID] = make(map[string][]byte)
		}

		// Store raw data keyed by full key for later processing
		entitiesByID[entityID][key] = entry.Value()
	}

	// Now build consolidated EntityState objects
	entityStatesByOrg := make(map[string][]shared.EntityState)

	logger.Infow("[Overwatch] Aggregating entities from KV entries", "entity_count", len(entitiesByID), "kv_entry_count", len(entries))

	for entityID, dataMap := range entitiesByID {
		logger.Infow("[Overwatch] Processing entity", "entity_id", entityID, "kv_entry_count", len(dataMap))
		entityState := h.mergeEntityData(entityID, dataMap)

		// Group by org_id
		orgID := entityState.OrgID
		if orgID == "" {
			orgID = "unknown"
		}

		entityStatesByOrg[orgID] = append(entityStatesByOrg[orgID], entityState)
	}

	logger.Infow("[Overwatch] Built entities", "total_entities", len(entitiesByID), "org_count", len(entityStatesByOrg))
	return entityStatesByOrg
}

// mergeEntityData merges separate KV entries into a single EntityState
func (h *OverwatchHandler) mergeEntityData(entityID string, dataMap map[string][]byte) shared.EntityState {
	state := shared.EntityState{
		EntityID:   entityID,
		EntityType: "sensor", // Default type for detection entities
		Status:     "active",
		Priority:   "normal",
		IsLive:     true,
		Components: make(map[string]interface{}),
		Aliases:    make(map[string]string),
		Tags:       []string{},
		Metadata:   make(map[string]interface{}),
		UpdatedAt:  time.Now(),
	}

	// Process each key and merge data
	for key, data := range dataMap {
		// Skip empty data
		if len(data) == 0 {
			continue
		}

		var rawData map[string]interface{}
		if err := json.Unmarshal(data, &rawData); err != nil {
			logger.Warnf("[Overwatch] Failed to unmarshal key %s: %v", key, err)
			continue
		}

		// Extract org_id (check both org_id and organization_id)
		if orgID, ok := rawData["org_id"].(string); ok && orgID != "" {
			state.OrgID = orgID
		}
		if orgID, ok := rawData["organization_id"].(string); ok && orgID != "" {
			state.OrgID = orgID
		}

		// Extract device_id if present
		if deviceID, ok := rawData["device_id"].(string); ok && deviceID != "" {
			state.DeviceID = deviceID
		}

		// Determine data type from key suffix and merge accordingly
		if strings.Contains(key, ".detections.objects") {
			h.mergeDetections(&state, rawData)
		} else if strings.Contains(key, ".analytics.summary") || strings.Contains(key, ".analytics.c4isr_summary") {
			h.mergeAnalytics(&state, rawData)
		} else if strings.Contains(key, ".c4isr.threat_intelligence") {
			h.mergeThreatIntel(&state, rawData)
		} else if strings.Contains(key, ".mavlink.heartbeat") {
			h.mergeMAVLinkHeartbeat(&state, rawData)
		} else if strings.Contains(key, ".mavlink.position") {
			h.mergeMAVLinkPosition(&state, rawData)
		} else if strings.Contains(key, ".mavlink.power") {
			h.mergeMAVLinkPower(&state, rawData)
		} else if strings.Contains(key, ".mavlink.attitude") {
			h.mergeMAVLinkAttitude(&state, rawData)
		} else if strings.Contains(key, ".mavlink.status") {
			h.mergeMAVLinkStatus(&state, rawData)
		} else if strings.Contains(key, ".mavlink.flight") {
			h.mergeMAVLinkFlight(&state, rawData)
		} else if !strings.Contains(key, ".") {
			// Single-key entity state (like device.1.1 or full EntityState)
			h.mergeFullState(&state, rawData)
		}
	}

	return state
}

// mergeDetections merges detection data into EntityState
func (h *OverwatchHandler) mergeDetections(state *shared.EntityState, data map[string]interface{}) {
	if trackedObjects, ok := data["tracked_objects"].(map[string]interface{}); ok {
		detectionState := &shared.DetectionState{
			TrackedObjects: make(map[string]shared.TrackedObject),
			Timestamp:      time.Now(),
		}

		for trackID, objData := range trackedObjects {
			if objMap, ok := objData.(map[string]interface{}); ok {
				trackedObj := shared.TrackedObject{
					TrackID:  trackID,
					IsActive: false,
				}

				if label, ok := objMap["label"].(string); ok {
					trackedObj.Label = label
				}
				if conf, ok := objMap["avg_confidence"].(float64); ok {
					trackedObj.AvgConfidence = conf
				}
				if active, ok := objMap["is_active"].(bool); ok {
					trackedObj.IsActive = active
				}
				if threat, ok := objMap["threat_level"].(string); ok {
					trackedObj.ThreatLevel = threat
				}
				if frames, ok := objMap["frame_count"].(float64); ok {
					trackedObj.FrameCount = int(frames)
				}

				detectionState.TrackedObjects[trackID] = trackedObj
			}
		}

		state.Detections = detectionState
	}
}

// mergeAnalytics merges analytics data into EntityState
func (h *OverwatchHandler) mergeAnalytics(state *shared.EntityState, data map[string]interface{}) {
	analyticsState := &shared.AnalyticsState{
		Timestamp: time.Now(),
	}

	if val, ok := data["total_unique_objects"].(float64); ok {
		analyticsState.TotalUniqueObjects = int(val)
	}
	if val, ok := data["total_frames_processed"].(float64); ok {
		analyticsState.TotalFramesProcessed = int(val)
	}
	if val, ok := data["active_objects_count"].(float64); ok {
		analyticsState.ActiveObjectsCount = int(val)
	}
	if val, ok := data["tracked_objects_count"].(float64); ok {
		analyticsState.TrackedObjectsCount = int(val)
	}
	if val, ok := data["active_threat_count"].(float64); ok {
		analyticsState.ActiveThreatCount = int(val)
	}
	if labels, ok := data["label_distribution"].(map[string]interface{}); ok {
		analyticsState.LabelDistribution = make(map[string]int)
		for k, v := range labels {
			if num, ok := v.(float64); ok {
				analyticsState.LabelDistribution[k] = int(num)
			}
		}
	}
	if threats, ok := data["threat_distribution"].(map[string]interface{}); ok {
		analyticsState.ThreatDistribution = make(map[string]int)
		for k, v := range threats {
			if num, ok := v.(float64); ok {
				analyticsState.ThreatDistribution[k] = int(num)
			}
		}
	}
	if ids, ok := data["active_track_ids"].([]interface{}); ok {
		for _, id := range ids {
			if str, ok := id.(string); ok {
				analyticsState.ActiveTrackIDs = append(analyticsState.ActiveTrackIDs, str)
			}
		}
	}

	state.Analytics = analyticsState
}

// mergeThreatIntel merges threat intelligence data into EntityState
func (h *OverwatchHandler) mergeThreatIntel(state *shared.EntityState, data map[string]interface{}) {
	threatIntel := &shared.ThreatIntelState{
		Timestamp: time.Now(),
	}

	if mission, ok := data["mission"].(string); ok {
		threatIntel.Mission = mission
	}

	if summary, ok := data["threat_summary"].(map[string]interface{}); ok {
		threatSummary := &shared.ThreatSummary{}

		if total, ok := summary["total_threats"].(float64); ok {
			threatSummary.TotalThreats = int(total)
		}
		if alert, ok := summary["alert_level"].(string); ok {
			threatSummary.AlertLevel = alert
		}
		if dist, ok := summary["threat_distribution"].(map[string]interface{}); ok {
			threatSummary.ThreatDistribution = make(map[string]int)
			for k, v := range dist {
				if num, ok := v.(float64); ok {
					threatSummary.ThreatDistribution[k] = int(num)
				}
			}
		}

		threatIntel.ThreatSummary = threatSummary
	}

	state.ThreatIntel = threatIntel
}

// mergeFullState merges full entity state data (Python + TelemetryWorker consolidated format)
func (h *OverwatchHandler) mergeFullState(state *shared.EntityState, data map[string]interface{}) {
	// Extract core fields (check both org_id and organization_id)
	if orgID, ok := data["org_id"].(string); ok && orgID != "" {
		state.OrgID = orgID
	}
	if orgID, ok := data["organization_id"].(string); ok && orgID != "" {
		state.OrgID = orgID
	}

	// Extract Name and OrgName if present
	if name, ok := data["name"].(string); ok && name != "" {
		state.Name = name
	}
	if orgName, ok := data["org_name"].(string); ok && orgName != "" {
		state.OrgName = orgName
	}

	if state.OrgID == "" {
		logger.Infow("[Overwatch] mergeFullState: WARNING - No org_id or organization_id in data")
	}

	// Python detection service format (NEW): detections.tracked_objects
	if detectionsData, ok := data["detections"].(map[string]interface{}); ok {
		// Check for tracked_objects (new format)
		if trackedObjects, ok := detectionsData["tracked_objects"].(map[string]interface{}); ok {
			h.mergeDetections(state, map[string]interface{}{"tracked_objects": trackedObjects})
			logger.Infof("[Overwatch] Merged detections.tracked_objects with %d tracked objects", len(trackedObjects))
		} else if objectsData, ok := detectionsData["objects"].(map[string]interface{}); ok {
			// Fallback to old format: detections.objects
			h.mergeDetections(state, map[string]interface{}{"tracked_objects": objectsData})
			logger.Infof("[Overwatch] Merged detections.objects with %d tracked objects", len(objectsData))
		}

		// Check for analytics nested inside detections (new format)
		if analyticsData, ok := detectionsData["analytics"].(map[string]interface{}); ok {
			h.mergeAnalytics(state, analyticsData)
			logger.Infow("[Overwatch] Merged detections.analytics")
		}
	}

	// Python analytics format (OLD): top-level analytics.summary
	if analyticsData, ok := data["analytics"].(map[string]interface{}); ok {
		if summaryData, ok := analyticsData["summary"].(map[string]interface{}); ok {
			h.mergeAnalytics(state, summaryData)
			logger.Infow("[Overwatch] Merged analytics.summary")
		}
	}

	// Python threat intelligence format (NEW): top-level threat_intelligence
	if threatData, ok := data["threat_intelligence"].(map[string]interface{}); ok {
		h.mergeThreatIntel(state, threatData)
		logger.Infow("[Overwatch] Merged threat_intelligence")
	}

	// Python C4ISR format (OLD): c4isr.threat_intelligence
	if c4isrData, ok := data["c4isr"].(map[string]interface{}); ok {
		if threatData, ok := c4isrData["threat_intelligence"].(map[string]interface{}); ok {
			h.mergeThreatIntel(state, threatData)
			logger.Infow("[Overwatch] Merged c4isr.threat_intelligence")
		}
	}

	// Try to unmarshal entire object for telemetry fields (from TelemetryWorker)
	jsonData, _ := json.Marshal(data)
	var fullState shared.EntityState
	if err := json.Unmarshal(jsonData, &fullState); err == nil {
		// Merge telemetry fields
		if fullState.Position != nil {
			state.Position = fullState.Position
		}
		if fullState.Attitude != nil {
			state.Attitude = fullState.Attitude
		}
		if fullState.Power != nil {
			state.Power = fullState.Power
		}
		if fullState.VFR != nil {
			state.VFR = fullState.VFR
		}
		if fullState.VehicleStatus != nil {
			state.VehicleStatus = fullState.VehicleStatus
		}
		if fullState.Mission != nil {
			state.Mission = fullState.Mission
		}
	}
}

// MAVLink signal merge functions for modular telemetry streams

// mergeMAVLinkHeartbeat merges heartbeat data into VehicleStatus
func (h *OverwatchHandler) mergeMAVLinkHeartbeat(state *shared.EntityState, data map[string]interface{}) {
	if state.VehicleStatus == nil {
		state.VehicleStatus = &shared.VehicleStatusState{}
	}

	if mode, ok := data["mode"].(string); ok {
		state.VehicleStatus.Mode = mode
	}
	if armed, ok := data["armed"].(bool); ok {
		state.VehicleStatus.Armed = armed
	}
	if autopilot, ok := data["autopilot"].(float64); ok {
		state.VehicleStatus.Autopilot = uint8(autopilot)
	}
	if systemStatus, ok := data["system_status"].(float64); ok {
		state.VehicleStatus.SystemStatus = uint8(systemStatus)
	}
	if customMode, ok := data["custom_mode"].(float64); ok {
		state.VehicleStatus.CustomMode = uint32(customMode)
	}
	if vehicleType, ok := data["type"].(float64); ok {
		state.VehicleStatus.VehicleType = uint8(vehicleType)
	}

	// Extract SystemID and ComponentID from mavlink2constellation data
	if systemID, ok := data["system_id"].(float64); ok {
		state.SystemID = uint8(systemID)
	}
	if componentID, ok := data["component_id"].(float64); ok {
		state.ComponentID = uint8(componentID)
	}

	if timestamp, ok := data["timestamp"].(string); ok {
		if ts, err := time.Parse(time.RFC3339, timestamp); err == nil {
			state.VehicleStatus.Timestamp = ts
		}
	}

	logger.Debugw("[Overwatch] Merged MAVLink heartbeat", "entity_id", state.EntityID, "mode", state.VehicleStatus.Mode, "armed", state.VehicleStatus.Armed, "system_id", state.SystemID, "component_id", state.ComponentID)
}

// mergeMAVLinkPosition merges GPS position data
func (h *OverwatchHandler) mergeMAVLinkPosition(state *shared.EntityState, data map[string]interface{}) {
	if state.Position == nil {
		state.Position = &shared.PositionState{}
	}
	if state.Position.Global == nil {
		state.Position.Global = &shared.GlobalPosition{}
	}

	if lat, ok := data["latitude"].(float64); ok {
		state.Position.Global.Latitude = lat
	}
	if lon, ok := data["longitude"].(float64); ok {
		state.Position.Global.Longitude = lon
	}
	if alt, ok := data["altitude"].(float64); ok {
		state.Position.Global.AltitudeMSL = alt
	}
	if sats, ok := data["satellites"].(float64); ok {
		state.Position.Global.SatellitesVisible = int(sats)
	}
	if fixType, ok := data["fix_type"].(float64); ok {
		state.Position.Global.FixType = int(fixType)
	}

	if timestamp, ok := data["timestamp"].(string); ok {
		if ts, err := time.Parse(time.RFC3339, timestamp); err == nil {
			state.Position.Global.Timestamp = ts
		}
	}

	logger.Debugw("[Overwatch] Merged MAVLink position", "entity_id", state.EntityID, "lat", state.Position.Global.Latitude, "lon", state.Position.Global.Longitude)
}

// mergeMAVLinkPower merges battery/power data
func (h *OverwatchHandler) mergeMAVLinkPower(state *shared.EntityState, data map[string]interface{}) {
	if state.Power == nil {
		state.Power = &shared.PowerState{}
	}

	if voltage, ok := data["voltage"].(float64); ok {
		state.Power.Voltage = voltage
	}
	if current, ok := data["current"].(float64); ok {
		state.Power.Current = current
	}
	if remaining, ok := data["battery_remaining"].(float64); ok {
		state.Power.BatteryRemain = int8(remaining)
	}

	if timestamp, ok := data["timestamp"].(string); ok {
		if ts, err := time.Parse(time.RFC3339, timestamp); err == nil {
			state.Power.Timestamp = ts
		}
	}

	logger.Debugw("[Overwatch] Merged MAVLink power", "entity_id", state.EntityID, "voltage", state.Power.Voltage, "battery", state.Power.BatteryRemain)
}

// mergeMAVLinkAttitude merges attitude (orientation) data - handles both Euler and Quaternion
func (h *OverwatchHandler) mergeMAVLinkAttitude(state *shared.EntityState, data map[string]interface{}) {
	if state.Attitude == nil {
		state.Attitude = &shared.AttitudeState{}
	}

	// Handle Euler angles (roll, pitch, yaw)
	if roll, hasRoll := data["roll"].(float64); hasRoll {
		if state.Attitude.Euler == nil {
			state.Attitude.Euler = &shared.EulerAttitude{}
		}
		state.Attitude.Euler.Roll = roll

		if pitch, ok := data["pitch"].(float64); ok {
			state.Attitude.Euler.Pitch = pitch
		}
		if yaw, ok := data["yaw"].(float64); ok {
			state.Attitude.Euler.Yaw = yaw
		}
		if rollspeed, ok := data["rollspeed"].(float64); ok {
			state.Attitude.Euler.RollSpeed = rollspeed
		}
		if pitchspeed, ok := data["pitchspeed"].(float64); ok {
			state.Attitude.Euler.PitchSpeed = pitchspeed
		}
		if yawspeed, ok := data["yawspeed"].(float64); ok {
			state.Attitude.Euler.YawSpeed = yawspeed
		}

		if timestamp, ok := data["timestamp"].(string); ok {
			if ts, err := time.Parse(time.RFC3339, timestamp); err == nil {
				state.Attitude.Euler.Timestamp = ts
			}
		}

		logger.Debugw("[Overwatch] Merged MAVLink Euler attitude", "entity_id", state.EntityID, "roll", state.Attitude.Euler.Roll, "pitch", state.Attitude.Euler.Pitch, "yaw", state.Attitude.Euler.Yaw)
	}

	// Handle Quaternion data (q1, q2, q3, q4)
	if q1, hasQ1 := data["q1"].(float64); hasQ1 {
		if state.Attitude.Quaternion == nil {
			state.Attitude.Quaternion = &shared.QuaternionAttitude{}
		}
		state.Attitude.Quaternion.Q1 = q1

		if q2, ok := data["q2"].(float64); ok {
			state.Attitude.Quaternion.Q2 = q2
		}
		if q3, ok := data["q3"].(float64); ok {
			state.Attitude.Quaternion.Q3 = q3
		}
		if q4, ok := data["q4"].(float64); ok {
			state.Attitude.Quaternion.Q4 = q4
		}
		if rollspeed, ok := data["rollspeed"].(float64); ok {
			state.Attitude.Quaternion.RollSpeed = rollspeed
		}
		if pitchspeed, ok := data["pitchspeed"].(float64); ok {
			state.Attitude.Quaternion.PitchSpeed = pitchspeed
		}
		if yawspeed, ok := data["yawspeed"].(float64); ok {
			state.Attitude.Quaternion.YawSpeed = yawspeed
		}

		if timestamp, ok := data["timestamp"].(string); ok {
			if ts, err := time.Parse(time.RFC3339, timestamp); err == nil {
				state.Attitude.Quaternion.Timestamp = ts
			}
		}

		// Convert quaternion to Euler for display purposes
		h.convertQuaternionToEuler(state)

		logger.Debugw("[Overwatch] Merged MAVLink Quaternion attitude", "entity_id", state.EntityID, "q1", state.Attitude.Quaternion.Q1, "q2", state.Attitude.Quaternion.Q2, "q3", state.Attitude.Quaternion.Q3, "q4", state.Attitude.Quaternion.Q4)
	}
}

// mergeMAVLinkStatus merges system status data
func (h *OverwatchHandler) mergeMAVLinkStatus(state *shared.EntityState, data map[string]interface{}) {
	if state.VehicleStatus == nil {
		state.VehicleStatus = &shared.VehicleStatusState{}
	}

	if load, ok := data["load"].(float64); ok {
		state.VehicleStatus.Load = uint16(load)
	}
	if sensorsEnabled, ok := data["sensors_enabled"].(float64); ok {
		state.VehicleStatus.SensorsEnabled = uint32(sensorsEnabled)
	}
	if sensorsHealth, ok := data["sensors_health"].(float64); ok {
		state.VehicleStatus.SensorsHealth = uint32(sensorsHealth)
	}

	if timestamp, ok := data["timestamp"].(string); ok {
		if ts, err := time.Parse(time.RFC3339, timestamp); err == nil {
			state.VehicleStatus.Timestamp = ts
		}
	}

	logger.Debugw("[Overwatch] Merged MAVLink status", "entity_id", state.EntityID, "load", state.VehicleStatus.Load)
}

// mergeMAVLinkFlight merges VFR_HUD flight data
func (h *OverwatchHandler) mergeMAVLinkFlight(state *shared.EntityState, data map[string]interface{}) {
	if state.VFR == nil {
		state.VFR = &shared.VFRState{}
	}

	if airspeed, ok := data["airspeed"].(float64); ok {
		state.VFR.Airspeed = airspeed
	}
	if groundspeed, ok := data["groundspeed"].(float64); ok {
		state.VFR.Groundspeed = groundspeed
	}
	if heading, ok := data["heading"].(float64); ok {
		state.VFR.Heading = int16(heading)
	}
	if climb, ok := data["climb"].(float64); ok {
		state.VFR.ClimbRate = climb
	}
	if throttle, ok := data["throttle"].(float64); ok {
		state.VFR.Throttle = uint16(throttle)
	}
	if alt, ok := data["alt"].(float64); ok {
		state.VFR.Altitude = alt
	}

	if timestamp, ok := data["timestamp"].(string); ok {
		if ts, err := time.Parse(time.RFC3339, timestamp); err == nil {
			state.VFR.Timestamp = ts
		}
	}

	logger.Debugw("[Overwatch] Merged MAVLink flight", "entity_id", state.EntityID, "airspeed", state.VFR.Airspeed, "groundspeed", state.VFR.Groundspeed, "heading", state.VFR.Heading)
}

// convertQuaternionToEuler converts quaternion attitude to Euler angles for display
func (h *OverwatchHandler) convertQuaternionToEuler(state *shared.EntityState) {
	if state.Attitude == nil || state.Attitude.Quaternion == nil {
		return
	}

	q := state.Attitude.Quaternion

	// Ensure Euler structure exists
	if state.Attitude.Euler == nil {
		state.Attitude.Euler = &shared.EulerAttitude{}
	}

	// Convert quaternion to Euler angles (roll, pitch, yaw)
	// Standard aerospace/MAVLink quaternion to Euler conversion
	q1, q2, q3, q4 := q.Q1, q.Q2, q.Q3, q.Q4

	// Roll (x-axis rotation)
	sinr_cosp := 2 * (q4*q1 + q2*q3)
	cosr_cosp := 1 - 2*(q1*q1+q2*q2)
	roll := math.Atan2(sinr_cosp, cosr_cosp)

	// Pitch (y-axis rotation)
	sinp := 2 * (q4*q2 - q3*q1)
	var pitch float64
	if math.Abs(sinp) >= 1 {
		pitch = math.Copysign(math.Pi/2, sinp) // use 90 degrees if out of range
	} else {
		pitch = math.Asin(sinp)
	}

	// Yaw (z-axis rotation)
	siny_cosp := 2 * (q4*q3 + q1*q2)
	cosy_cosp := 1 - 2*(q2*q2+q3*q3)
	yaw := math.Atan2(siny_cosp, cosy_cosp)

	state.Attitude.Euler.Roll = roll
	state.Attitude.Euler.Pitch = pitch
	state.Attitude.Euler.Yaw = yaw
	state.Attitude.Euler.Timestamp = q.Timestamp

	logger.Debugw("[Overwatch] Converted quaternion to Euler", "entity_id", state.EntityID,
		"roll_rad", roll, "pitch_rad", pitch, "yaw_rad", yaw,
		"roll_deg", roll*180/math.Pi, "pitch_deg", pitch*180/math.Pi, "yaw_deg", yaw*180/math.Pi)
}
