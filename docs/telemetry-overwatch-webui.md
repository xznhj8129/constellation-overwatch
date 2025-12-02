# MAVLink Telemetry → Overwatch WebUI Integration

This document describes how MAVLink telemetry data is integrated with the Constellation Overwatch real-time web interface using modular signal streams and Datastar reactive signals.

## Architecture Overview

```
MAVLink Devices → mavlink2constellation → KV Store → Overwatch Handler → Web UI
                                          ↓
                                  Vision/C4ISR Services
```

## KV Store Signal Tree Structure

### Core Design Pattern
```
{entity_id}                               # Core entity record
{entity_id}.mavlink.{signal_type}         # MAVLink telemetry streams
{entity_id}.vision.{signal_type}          # Vision detection streams  
{entity_id}.c4isr.{signal_type}           # C4ISR intelligence streams
```

### MAVLink Signal Types
```bash
# Vehicle status and control
e6fe8488-3c7f-4632-97c0-6d6b9b8562fe.mavlink.heartbeat
e6fe8488-3c7f-4632-97c0-6d6b9b8562fe.mavlink.status

# Position and navigation
e6fe8488-3c7f-4632-97c0-6d6b9b8562fe.mavlink.position
e6fe8488-3c7f-4632-97c0-6d6b9b8562fe.mavlink.attitude

# Power and health
e6fe8488-3c7f-4632-97c0-6d6b9b8562fe.mavlink.power

# Vision system (existing)
e6fe8488-3c7f-4632-97c0-6d6b9b8562fe.vision.detections
e6fe8488-3c7f-4632-97c0-6d6b9b8562fe.vision.analytics
```

## Data Flow & Publishing Strategy

### 1. MAVLink2Constellation Publisher (Recommended)

**Direct KV Publishing** - mavlink2constellation publishes modular signals directly:

```go
func (p *Publisher) publishTelemetry(entityID string, msg MAVLinkMessage) {
    switch msg.MessageType {
    case "HEARTBEAT":
        key := fmt.Sprintf("%s.mavlink.heartbeat", entityID)
        data := map[string]interface{}{
            "timestamp": time.Now().Format(time.RFC3339),
            "mode": decodeMode(msg.Data["custom_mode"]),
            "armed": (msg.Data["base_mode"].(uint8) & 128) != 0,
            "system_status": msg.Data["system_status"],
            "autopilot": msg.Data["autopilot"],
        }
        kv.Put(key, json.Marshal(data))
        
    case "GPS_RAW_INT":
        key := fmt.Sprintf("%s.mavlink.position", entityID) 
        data := map[string]interface{}{
            "timestamp": time.Now().Format(time.RFC3339),
            "latitude": msg.Data["lat"].(float64) / 1e7,
            "longitude": msg.Data["lon"].(float64) / 1e7,
            "altitude": msg.Data["alt"].(float64) / 1000,
            "satellites": msg.Data["satellites_visible"],
            "fix_type": msg.Data["fix_type"],
        }
        kv.Put(key, json.Marshal(data))
        
    case "SYS_STATUS":
        key := fmt.Sprintf("%s.mavlink.power", entityID)
        data := map[string]interface{}{
            "timestamp": time.Now().Format(time.RFC3339),
            "voltage": msg.Data["voltage_battery"],
            "current": msg.Data["current_battery"], 
            "battery_remaining": msg.Data["battery_remaining"],
        }
        kv.Put(key, json.Marshal(data))
    }
}
```

### 2. Update Frequency Strategy

```go
type PublishingStrategy struct {
    // High-frequency streams (10Hz)
    HeartbeatInterval: 100 * time.Millisecond,
    PositionInterval:  100 * time.Millisecond,
    
    // Medium-frequency streams (1Hz)
    PowerInterval:    1 * time.Second,
    StatusInterval:   1 * time.Second,
    
    // Low-frequency consolidation (0.2Hz) 
    CoreEntityUpdate: 5 * time.Second,
}
```

## Overwatch Handler Signal Processing

### Automatic Signal Detection
The `mergeEntityData()` function automatically detects and processes modular signals:

```go
// In pkg/services/web/handlers/overwatch.go
func (h *OverwatchHandler) mergeEntityData(entityID string, dataMap map[string][]byte) shared.EntityState {
    // Process each KV key and merge data
    for key, data := range dataMap {
        if strings.Contains(key, ".mavlink.heartbeat") {
            h.mergeMAVLinkHeartbeat(&state, rawData)
        } else if strings.Contains(key, ".mavlink.position") {
            h.mergeMAVLinkPosition(&state, rawData)  
        } else if strings.Contains(key, ".mavlink.power") {
            h.mergeMAVLinkPower(&state, rawData)
        } else if strings.Contains(key, ".vision.detections") {
            h.mergeDetections(&state, rawData)
        }
        // etc.
    }
}
```

### Signal Merge Functions
Each signal type has a dedicated merge function that populates the EntityState:

```go
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
    // etc.
}
```

## Datastar Reactive Rendering

### Signal-Scoped Updates
The web UI uses server-sent events (SSE) to push real-time updates with intelligent batching:

```javascript
// In overwatch.templ - Datastar signals automatically updated
data-signals="{
    entityStatesByOrg: {},           // Hierarchical entity organization
    lastUpdate: '',                 // Last update timestamp
    totalEntities: 0,               // Real-time entity count
    _isConnected: false             // Connection status
}"
```

### Template Conditional Rendering
The EntityCard template conditionally renders sections based on available data:

```html
<!-- MAVLink Vehicle Status -->
if entity.VehicleStatus != nil {
    <div class="entity-section">
        <h4>Vehicle Status</h4>
        <div>Mode: {{entity.VehicleStatus.Mode}}</div>
        <div>Armed: {{if entity.VehicleStatus.Armed}}ARMED{{else}}DISARMED{{end}}</div>
    </div>
}

<!-- MAVLink Position -->  
if entity.Position != nil && entity.Position.Global != nil {
    <div class="entity-section">
        <h4>Position</h4>
        <div>Lat: {{entity.Position.Global.Latitude}}</div>
        <div>Lon: {{entity.Position.Global.Longitude}}</div>
        <div>GPS Sats: {{entity.Position.Global.SatellitesVisible}}</div>
    </div>
}
```

## Real-Time Update Flow

### 1. KV Change Detection
```
KV Store Change → Overwatch Handler.WatchKV() → updateChan → dirtyEntities
```

### 2. Batch Processing
```go
// Every 50ms, process dirty entities
flushTicker := time.NewTicker(50 * time.Millisecond)

for entityID := range dirtyEntities {
    // Reconstruct EntityState from all signals
    entityState := h.mergeEntityData(entityID, localEntityCache[entityID])
    snapshot = append(snapshot, entityState)
}
```

### 3. SSE Rendering
```go
// Send to renderer (with conflation if busy)
select {
case renderChan <- snapshot:
    // Success - render in background goroutine
    dirtyEntities = make(map[string]bool)
default:
    // Renderer busy - skip frame (conflation)
}
```

### 4. Browser Update
```
SSE → Datastar PatchElements → DOM Morphing → Visual Update
```

## Benefits of Modular Architecture

### 1. **Performance**
- High-frequency MAVLink (10Hz) doesn't interfere with slower vision data
- Selective updates - only changed subsystems trigger re-rendering
- Intelligent conflation prevents browser overwhelm

### 2. **Scalability** 
- Each service owns its signal namespace
- Independent update frequencies per signal type
- Horizontal scaling of publishers

### 3. **Maintainability**
- Clean separation of concerns
- Easy to add new signal types without code changes
- Debugging isolation per signal stream

### 4. **Reliability**
- Signal-level fault isolation
- Partial state updates preserve working systems
- Graceful degradation when services are offline

## Visual Indicators

### Entity Card Border Colors
- **Red (#f00)**: Active threats detected
- **Yellow (#ff0)**: Armed vehicles
- **Green (#0f0)**: Positioned/healthy entities  
- **Magenta (#f0f)**: Default/unknown status

### MAVLink-Specific Colors
- **Cyan (#0aa)**: MAVLink section headers
- **Green**: Good status (GPS >8 sats, battery >50%, voltage >12V)
- **Yellow**: Warning status (GPS 5-7 sats, battery 20-50%, voltage 11-12V)
- **Red**: Critical status (GPS <5 sats, battery <20%, voltage <11V, ARMED)

## Future Extensions

### Additional Signal Types
```
{entity_id}.mavlink.mission          # Mission/waypoint data
{entity_id}.mavlink.actuators        # Servo/motor control
{entity_id}.mavlink.sensors          # Environmental sensors
{entity_id}.swarm.formation          # Swarm coordination
{entity_id}.weather.conditions       # Weather integration
```

### Enhanced Datastar Signals  
```javascript
// Proposed signal extensions
mavlinkHealth: {},                   // Per-entity health metrics
swarmFormation: {},                  // Swarm coordination data
missionProgress: {},                 // Mission completion status
threatAssessment: {}                 // Real-time threat analysis
```

This modular architecture provides a robust foundation for real-time C4ISR operations with seamless integration of telemetry, vision, and intelligence data streams.