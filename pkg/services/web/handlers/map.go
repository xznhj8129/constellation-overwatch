package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Constellation-Overwatch/constellation-overwatch/api/services"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/ontology"
	embeddednats "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/embedded-nats"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/logger"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/datastar"
	common_components "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/features/common/components"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/signals"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/shared"

	"github.com/nats-io/nats.go"
)

type MapHandler struct {
	natsEmbedded *embeddednats.EmbeddedNATS
	orgSvc       *services.OrganizationService
	entitySvc    *services.EntityService
}

func NewMapHandler(natsEmbedded *embeddednats.EmbeddedNATS, orgSvc *services.OrganizationService, entitySvc *services.EntityService) *MapHandler {
	return &MapHandler{
		natsEmbedded: natsEmbedded,
		orgSvc:       orgSvc,
		entitySvc:    entitySvc,
	}
}

// HandleAPIMapSSE is the dedicated SSE endpoint for the Map page
// This is separate from the Overwatch SSE to avoid conflicts
func (h *MapHandler) HandleAPIMapSSE(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Verify we have a flusher (required for SSE)
	flusher, ok := w.(http.Flusher)
	if !ok {
		logger.Infow("[Map] ERROR: ResponseWriter does not support flushing (SSE won't work)")
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// CRITICAL: Set SSE headers BEFORE creating SSE generator or writing anything
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	logger.Infow("[Map] SSE headers set, establishing connection", "remote_addr", r.RemoteAddr)

	// Create SSE generator AFTER setting headers
	sse := datastar.NewSSE(w, r)

	// Mutex to synchronize writes to ResponseWriter from multiple goroutines
	var writeMutex sync.Mutex

	// Send an immediate comment to establish the SSE stream in the browser
	writeMutex.Lock()
	fmt.Fprintf(w, ": Map SSE connection established\n\n")

	// Reset the entities container to empty state to prevent duplicates on reconnection
	emptyState := `<div class="empty-state" style="color: #888; padding: 40px; text-align: center;">
					<p>No entity states in global store. Waiting for telemetry data...</p>
					<p style="font-size: 10px; margin-top: 10px;">Server-side rendering via SSE</p>
				</div>`
	sse.PatchElements(emptyState,
		datastar.WithSelector("#entity-list"),
		datastar.WithModeInner())

	flusher.Flush()
	writeMutex.Unlock()
	logger.Infow("[Map] SSE client connected", "remote_addr", r.RemoteAddr)

	// Local cache of all KV data: entityID -> key -> data
	localEntityCache := make(map[string]map[string][]byte)

	// Track known entities (ID -> OrgID) to handle cleanup
	knownEntities := make(map[string]string)

	// DB entity cache for VideoConfig lookup (same pattern as video handler)
	// VideoConfig lives in the DB, not KV, so we must look it up separately.
	videoConfigCache := make(map[string]*ontology.VideoConfig)
	var videoConfigCacheTime time.Time
	refreshVideoConfigCache := func() {
		if time.Since(videoConfigCacheTime) < 30*time.Second {
			return
		}
		if h.entitySvc == nil {
			return
		}
		entities, err := h.entitySvc.ListAllEntities()
		if err != nil {
			logger.Warnw("[Map] Failed to refresh video config cache", "error", err)
			return
		}
		next := make(map[string]*ontology.VideoConfig, len(entities))
		for _, ent := range entities {
			if ent.VideoConfig == "" || ent.VideoConfig == "{}" {
				continue
			}
			var vc ontology.VideoConfig
			if json.Unmarshal([]byte(ent.VideoConfig), &vc) == nil {
				next[ent.EntityID] = &vc
			}
		}
		videoConfigCache = next
		videoConfigCacheTime = time.Now()
		logger.Debugw("[Map] Refreshed video config cache", "entries", len(next))
	}
	refreshVideoConfigCache()

	// Struct to pass data to renderer
	type RenderPayload struct {
		Snapshot      []shared.EntityState
		RemovedIDs    []string
		TotalEntities int
	}

	// Channel to buffer updates from NATS to SSE
	updateChan := make(chan nats.KeyValueEntry, 10000)

	// Channel to send snapshots to the renderer
	renderChan := make(chan RenderPayload, 1)

	// Load initial KV data before starting watcher
	ctx := r.Context()
	go func() {
		defer close(updateChan)

		// STEP 1: Load existing KV data on initial connection
		logger.Infow("[Map] Loading initial KV state...")
		if initialEntries, err := h.natsEmbedded.GetAllKVEntries(); err == nil {
			logger.Infow("[Map] Loaded initial state", "kv_entries", len(initialEntries))

			// Send existing entries through the update channel to populate initial state
			for _, entry := range initialEntries {
				select {
				case updateChan <- entry:
					// Successfully queued initial entry
				case <-ctx.Done():
					return
				case <-time.After(100 * time.Millisecond):
					// Skip if channel is backed up during initial load
					logger.Debugw("[Map] Skipped initial entry due to channel backup", "key", entry.Key())
				}
			}
			logger.Infow("[Map] Initial state load completed", "entities_loaded", len(initialEntries))
		} else {
			logger.Warnw("[Map] Failed to load initial KV state", "error", err)
		}

		// STEP 2: Start watching for real-time changes
		for {
			if ctx.Err() != nil {
				return
			}

			logger.Infow("[Map] KV watcher goroutine started, waiting for changes...")

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
				logger.Warnw("[Map] KV watcher stopped unexpectedly, restarting...", "error", watchErr)
			} else {
				logger.Warnw("[Map] KV watcher channel closed unexpectedly, restarting...")
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
				logger.Debugw("[Map] Renderer goroutine stopping - context canceled")
				return
			case payload, ok := <-renderChan:
				if !ok {
					logger.Debugw("[Map] Renderer goroutine stopping - render channel closed")
					return
				}

				// Check if context is still valid before rendering
				if ctx.Err() != nil {
					logger.Debugw("[Map] Context canceled, skipping render")
					return
				}

				// Render and Flush
				h.renderAndFlushSnapshot(w, flusher, &writeMutex, sse, payload.Snapshot, payload.RemovedIDs, payload.TotalEntities, knownEntities, videoConfigCache)
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
	for {
		select {
		case <-ctx.Done():
			logger.Infow("[Map] Client disconnected", "remote_addr", r.RemoteAddr)
			return

		case <-ticker.C:
			// Refresh DB video config cache on heartbeat interval
			refreshVideoConfigCache()
			// Send heartbeat only if connection is still valid
			if flusher != nil {
				writeMutex.Lock()
				if flusher != nil {
					fmt.Fprintf(w, ": heartbeat\n\n")
					func() {
						defer func() {
							if r := recover(); r != nil {
								logger.Debugw("[Map] Recovered from heartbeat flush panic", "panic", r)
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
				// Create snapshot and track removals
				var snapshot []shared.EntityState
				var removedIDs []string

				for entityID := range dirtyEntities {
					entityData, exists := localEntityCache[entityID]
					if !exists {
						removedIDs = append(removedIDs, entityID)
						continue
					}
					// Reconstruct state
					entityState := h.mergeEntityData(entityID, entityData)
					snapshot = append(snapshot, entityState)
				}

				// Try to send to renderer (non-blocking)
				payload := RenderPayload{
					Snapshot:      snapshot,
					RemovedIDs:    removedIDs,
					TotalEntities: len(localEntityCache),
				}

				select {
				case renderChan <- payload:
					// Success, renderer will handle it
					dirtyEntities = make(map[string]bool)
				default:
					// Renderer is busy, skip this frame (conflation)
					logger.Debugw("[Map] Renderer busy, skipping frame (conflation)", "pending_entities", len(dirtyEntities))
				}
			}

		case entry, ok := <-updateChan:
			if !ok {
				logger.Infow("[Map] Update channel closed, stopping SSE stream")
				return
			}

			// Process KV entry
			key := entry.Key()
			if key == "" {
				logger.Warnw("[Map] Received entry with empty key, skipping")
				continue
			}

			parts := strings.Split(key, ".")
			if len(parts) == 0 {
				logger.Warnw("[Map] Invalid key format", "key", key)
				continue
			}

			entityID := parts[0]
			if entityID == "" {
				logger.Warnw("[Map] Empty entity ID", "key", key)
				continue
			}

			// Initialize entity cache if needed
			if localEntityCache[entityID] == nil {
				localEntityCache[entityID] = make(map[string][]byte)
				logger.Debugw("[Map] Initialized cache for new entity", "entity_id", entityID)
			}

			// Handle delete operations
			if entry.Operation() == nats.KeyValueDelete || entry.Operation() == nats.KeyValuePurge {
				delete(localEntityCache[entityID], key)
				if len(localEntityCache[entityID]) == 0 {
					delete(localEntityCache, entityID)
					logger.Debugw("[Map] Removed entity from cache", "entity_id", entityID)
				} else {
					logger.Debugw("[Map] Removed signal from entity", "entity_id", entityID, "key", key)
				}
			} else {
				// Handle create/update operations
				value := entry.Value()
				if len(value) == 0 {
					logger.Debugw("[Map] Received empty value", "key", key)
					localEntityCache[entityID][key] = []byte("{}")
				} else {
					// Validate JSON before storing
					var testJSON interface{}
					if err := json.Unmarshal(value, &testJSON); err != nil {
						logger.Warnw("[Map] Invalid JSON in KV entry, storing as-is", "key", key, "error", err)
					}
					localEntityCache[entityID][key] = value
					logger.Debugw("[Map] Updated entity signal", "entity_id", entityID, "key", key, "size", len(value))
				}
			}

			// Mark entity as dirty for rendering
			dirtyEntities[entityID] = true
		}
	}
}

// Helper to render and flush a snapshot for Map view
func (h *MapHandler) renderAndFlushSnapshot(w http.ResponseWriter, flusher http.Flusher, writeMutex *sync.Mutex, sse *datastar.ServerSentEventGenerator, snapshot []shared.EntityState, removedIDs []string, totalEntities int, knownEntities map[string]string, videoConfigCache map[string]*ontology.VideoConfig) {
	if flusher == nil {
		logger.Debugw("[Map] Flusher is nil, connection likely closed, skipping render")
		return
	}

	writeMutex.Lock()
	defer writeMutex.Unlock()

	if flusher == nil {
		logger.Debugw("[Map] Flusher became nil while waiting for mutex, connection closed")
		return
	}

	updatesSent := 0
	containerSelector := "#entity-list"

	for _, entityState := range snapshot {
		// Render card using C4EntityCard
		var cardHTML strings.Builder
		if err := common_components.C4EntityCard(entityState, false).Render(context.Background(), &cardHTML); err != nil {
			logger.Errorw("[Map] Error rendering C4 entity card", "error", err)
			continue
		}

		// Determine patch mode
		entityID := entityState.EntityID
		isNew := false

		if _, exists := knownEntities[entityID]; !exists {
			// New entity - remove empty state if first entity
			if len(knownEntities) == 0 {
				if err := sse.PatchElements("", datastar.WithSelector(containerSelector+" .empty-state"), datastar.WithModeRemove()); err != nil {
					// Ignore error as empty state might not exist
				}
			}

			isNew = true
			knownEntities[entityID] = entityState.OrgID
		} else {
			knownEntities[entityID] = entityState.OrgID
		}

		// Patch Element
		var err error
		if isNew {
			err = sse.PatchElements(cardHTML.String(), datastar.WithSelector(containerSelector), datastar.WithModeAppend())
		} else {
			err = sse.PatchElements(cardHTML.String(), datastar.WithSelectorf("#c4-entity-%s", entityID), datastar.WithModeOuter())
		}
		if err != nil {
			logger.Debugw("[Map] Failed to patch entity, connection may be closed", "entity_id", entityID, "error", err)
			return
		}

		// For new entities (append mode), also append the video player component separately
		// This ensures video is only added once and never morphed, preventing connection duplication
		if isNew {
			// VideoConfig lives in the DB, not KV — look it up from the DB cache
			webrtcURL := ""
			if vc, ok := videoConfigCache[entityID]; ok {
				webrtcURL = vc.PreferredWebRTCURL()
			}
			var videoHTML strings.Builder
			if err := common_components.VideoPlayer(entityID, webrtcURL).Render(context.Background(), &videoHTML); err == nil {
				videoSelector := fmt.Sprintf("#video-section-%s", entityID)
				if err := sse.PatchElements(videoHTML.String(), datastar.WithSelector(videoSelector), datastar.WithModeInner()); err != nil {
					logger.Debugw("[Map] Failed to append video player", "entity_id", entityID, "error", err)
				}
			}
		}

		// Patch Signal with typed entity metadata (for map marker updates)
		// Full entity data is already rendered in the card HTML
		entitySignal := buildEntitySignal(entityID, entityState)

		if err := sse.MarshalAndPatchSignals(map[string]interface{}{
			fmt.Sprintf("entityStatesByOrg.%s.%s", entityState.OrgID, entityID): entitySignal,
		}); err != nil {
			logger.Debugw("[Map] Failed to patch entity signals, connection may be closed", "entity_id", entityID, "error", err)
			return
		}

		updatesSent++
	}

	// Process Removed IDs
	for _, entityID := range removedIDs {
		if orgID, known := knownEntities[entityID]; known {
			logger.Infow("[Map] Removing entity", "entity_id", entityID)

			// Remove from DOM
			selector := fmt.Sprintf("#c4-entity-%s", entityID)
			if err := sse.RemoveElement(selector); err != nil {
				logger.Debugw("[Map] Failed to remove entity element", "error", err)
			}

			// Remove signal
			if err := sse.MarshalAndPatchSignals(map[string]interface{}{
				fmt.Sprintf("entityStatesByOrg.%s.%s", orgID, entityID): nil,
			}); err != nil {
				logger.Debugw("[Map] Failed to remove entity signal", "error", err)
			}

			delete(knownEntities, entityID)
		}
	}

	// Update total entities signal using typed struct
	mapSig := signals.MapSignals{
		TotalEntities: totalEntities,
		IsConnected:   true,
		LastUpdate:    time.Now().Format("15:04:05"),
	}
	if err := sse.MarshalAndPatchSignals(mapSig); err != nil {
		logger.Debugw("[Map] Failed to patch stats signals", "error", err)
	}

	// Flush
	func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Debugw("[Map] Recovered from flush panic", "panic", r)
			}
		}()
		flusher.Flush()
	}()

	if updatesSent > 0 {
		logger.Debugw("[Map] Rendered snapshot", "entities", updatesSent, "removed", len(removedIDs))
	}
}

// mergeEntityData merges separate KV entries into a single EntityState
// This is a simplified version for the map view
func (h *MapHandler) mergeEntityData(entityID string, dataMap map[string][]byte) shared.EntityState {
	state := shared.EntityState{
		EntityID: entityID,
		IsLive:   true,
	}

	// Process each key and merge data
	for key, rawValue := range dataMap {
		if len(rawValue) == 0 {
			continue
		}

		var data map[string]interface{}
		if err := json.Unmarshal(rawValue, &data); err != nil {
			logger.Debugw("[Map] Failed to unmarshal value", "key", key, "error", err)
			continue
		}

		// Extract basic entity info
		if name, ok := data["name"].(string); ok && state.Name == "" {
			state.Name = name
		}
		if entityType, ok := data["entity_type"].(string); ok && state.EntityType == "" {
			state.EntityType = entityType
		}
		if status, ok := data["status"].(string); ok && state.Status == "" {
			state.Status = status
		}
		if orgID, ok := data["org_id"].(string); ok && state.OrgID == "" {
			state.OrgID = orgID
		}
		if orgID, ok := data["organization_id"].(string); ok && state.OrgID == "" {
			state.OrgID = orgID
		}
		if orgName, ok := data["org_name"].(string); ok && state.OrgName == "" {
			state.OrgName = orgName
		}

		// Determine data type from key suffix and merge accordingly
		if strings.HasSuffix(key, ".mavlink") {
			h.mergeMAVLinkData(&state, data)
		} else if strings.HasSuffix(key, ".state") || !strings.Contains(key, ".") {
			h.mergeFullState(&state, data)
		}
	}

	// Ensure entity has a name
	if state.Name == "" {
		state.Name = entityID
	}

	return state
}

// mergeMAVLinkData merges MAVLink telemetry data
func (h *MapHandler) mergeMAVLinkData(state *shared.EntityState, data map[string]interface{}) {
	// Extract position data
	// MAVLink sends lat/lng as degE7 (multiply by 1e7), altitude in mm
	if lat, ok := getFloat64(data, "latitude"); ok {
		if lng, ok := getFloat64(data, "longitude"); ok {
			if state.Position == nil {
				state.Position = &shared.PositionState{}
			}
			if state.Position.Global == nil {
				state.Position.Global = &shared.GlobalPosition{}
			}
			// Convert from degE7 to degrees
			state.Position.Global.Latitude = lat / 1e7
			state.Position.Global.Longitude = lng / 1e7

			// Altitude is in mm, convert to meters
			if alt, ok := getFloat64(data, "altitude"); ok {
				state.Position.Global.AltitudeMSL = alt / 1000.0
			}
			if altRel, ok := getFloat64(data, "relative_alt"); ok {
				state.Position.Global.AltitudeRelative = altRel / 1000.0
			}
		}
	}

	// Extract VFR data
	// Try both naming conventions (snake_case from mavlink2constellation, camelCase from legacy)
	if heading, ok := getFloat64(data, "heading"); ok {
		if state.VFR == nil {
			state.VFR = &shared.VFRState{}
		}
		// Heading is in cdeg (centidegrees), convert to degrees
		state.VFR.Heading = int16(heading / 100.0)
	}
	// Try ground_speed first (mavlink2constellation format), then groundspeed
	if groundspeed, ok := getFloat64(data, "ground_speed"); ok {
		if state.VFR == nil {
			state.VFR = &shared.VFRState{}
		}
		state.VFR.Groundspeed = groundspeed
	} else if groundspeed, ok := getFloat64(data, "groundspeed"); ok {
		if state.VFR == nil {
			state.VFR = &shared.VFRState{}
		}
		state.VFR.Groundspeed = groundspeed
	}
	if airspeed, ok := getFloat64(data, "airspeed"); ok {
		if state.VFR == nil {
			state.VFR = &shared.VFRState{}
		}
		state.VFR.Airspeed = airspeed
	}
	if climb, ok := getFloat64(data, "climb_rate"); ok {
		if state.VFR == nil {
			state.VFR = &shared.VFRState{}
		}
		state.VFR.ClimbRate = climb
	}
	if throttle, ok := getFloat64(data, "throttle"); ok {
		if state.VFR == nil {
			state.VFR = &shared.VFRState{}
		}
		state.VFR.Throttle = uint16(throttle)
	}

	// Extract battery data
	// voltage_battery is in mV, convert to volts
	if voltage, ok := getFloat64(data, "voltage_battery"); ok {
		if state.Power == nil {
			state.Power = &shared.PowerState{}
		}
		state.Power.Voltage = voltage / 1000.0 // mV to V
	} else if voltage, ok := getFloat64(data, "battery_voltage"); ok {
		if state.Power == nil {
			state.Power = &shared.PowerState{}
		}
		state.Power.Voltage = voltage
	}
	if remaining, ok := getFloat64(data, "battery_remaining"); ok {
		if state.Power == nil {
			state.Power = &shared.PowerState{}
		}
		state.Power.BatteryRemain = int8(remaining)
	}
	if load, ok := getFloat64(data, "load"); ok {
		if state.VehicleStatus == nil {
			state.VehicleStatus = &shared.VehicleStatusState{}
		}
		state.VehicleStatus.Load = uint16(load)
	}

	// Extract status info
	if flightMode, ok := data["flight_mode"].(string); ok {
		if state.VehicleStatus == nil {
			state.VehicleStatus = &shared.VehicleStatusState{}
		}
		state.VehicleStatus.Mode = flightMode
	}
	if armed, ok := data["armed"].(bool); ok {
		if state.VehicleStatus == nil {
			state.VehicleStatus = &shared.VehicleStatusState{}
		}
		state.VehicleStatus.Armed = armed
	}

	// Extract source as entity name if not already set
	if source, ok := data["source"].(string); ok && state.Name == "" {
		state.Name = source
	}

	// Extract vehicle type for icon mapping
	if vehicleType, ok := data["vehicle_type"].(string); ok {
		// Map MAVLink vehicle types to entity types if not set
		if state.EntityType == "" {
			state.EntityType = mapMAVLinkVehicleType(vehicleType)
		}
	}
}

// mergeFullState merges full entity state data
func (h *MapHandler) mergeFullState(state *shared.EntityState, data map[string]interface{}) {
	// Basic info
	if name, ok := data["name"].(string); ok && state.Name == "" {
		state.Name = name
	}
	if entityType, ok := data["entity_type"].(string); ok && state.EntityType == "" {
		state.EntityType = entityType
	}
	if status, ok := data["status"].(string); ok && state.Status == "" {
		state.Status = status
	}

	// Position from nested structure
	if posData, ok := data["position"].(map[string]interface{}); ok {
		if globalData, ok := posData["global"].(map[string]interface{}); ok {
			if state.Position == nil {
				state.Position = &shared.PositionState{}
			}
			if state.Position.Global == nil {
				state.Position.Global = &shared.GlobalPosition{}
			}

			if lat, ok := getFloat64(globalData, "latitude"); ok {
				state.Position.Global.Latitude = lat
			}
			if lng, ok := getFloat64(globalData, "longitude"); ok {
				state.Position.Global.Longitude = lng
			}
			if alt, ok := getFloat64(globalData, "altitude_msl"); ok {
				state.Position.Global.AltitudeMSL = alt
			}
		}
	}

	// VFR from nested structure
	if vfrData, ok := data["vfr"].(map[string]interface{}); ok {
		if state.VFR == nil {
			state.VFR = &shared.VFRState{}
		}
		if heading, ok := getFloat64(vfrData, "heading"); ok {
			state.VFR.Heading = int16(heading)
		}
		if groundspeed, ok := getFloat64(vfrData, "groundspeed"); ok {
			state.VFR.Groundspeed = groundspeed
		}
	}

	// VideoConfig from nested structure
	if vcData, ok := data["video_config"].(map[string]interface{}); ok {
		vcJSON, err := json.Marshal(vcData)
		if err == nil {
			var vc ontology.VideoConfig
			if json.Unmarshal(vcJSON, &vc) == nil {
				state.VideoConfig = &vc
			}
		}
	}
}

// getFloat64 safely extracts a float64 from interface{}
func getFloat64(data map[string]interface{}, key string) (float64, bool) {
	val, ok := data[key]
	if !ok {
		return 0, false
	}
	switch v := val.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	default:
		return 0, false
	}
}

// mapMAVLinkVehicleType maps MAVLink vehicle type strings to entity types
func mapMAVLinkVehicleType(mavType string) string {
	switch mavType {
	case "MAV_TYPE_HEXAROTOR", "MAV_TYPE_QUADROTOR", "MAV_TYPE_OCTOROTOR", "MAV_TYPE_TRICOPTER":
		return shared.EntityTypeAircraftMultirotor
	case "MAV_TYPE_FIXED_WING":
		return shared.EntityTypeAircraftFixedWing
	case "MAV_TYPE_HELICOPTER":
		return shared.EntityTypeAircraftHelicopter
	case "MAV_TYPE_VTOL_DUOROTOR", "MAV_TYPE_VTOL_QUADROTOR", "MAV_TYPE_VTOL_TILTROTOR":
		return shared.EntityTypeAircraftVTOL
	case "MAV_TYPE_AIRSHIP":
		return shared.EntityTypeAircraftAirship
	case "MAV_TYPE_GROUND_ROVER":
		return shared.EntityTypeGroundVehicleWheeled
	case "MAV_TYPE_SURFACE_BOAT":
		return shared.EntityTypeSurfaceVesselUSV
	case "MAV_TYPE_SUBMARINE":
		return shared.EntityTypeUnderwaterVehicle
	case "MAV_TYPE_GCS":
		return shared.EntityTypeOperatorStation
	default:
		return shared.EntityTypeAircraftMultirotor // Default fallback
	}
}
