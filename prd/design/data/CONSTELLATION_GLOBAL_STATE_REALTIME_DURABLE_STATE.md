# Constellation Global State: Real-time Durable State Management

## Overview

The Constellation Global State system provides a real-time, durable key-value store for maintaining the complete operational state of all entities in the fleet. This design enables:

- **Single Source of Truth**: Unified entity state accessible across all services
- **Real-time Updates**: Sub-second state synchronization from telemetry streams
- **Durable Storage**: Persistent state survives service restarts
- **Efficient Queries**: Fast lookups without database round-trips
- **Event Sourcing**: State changes can be replayed and audited

## Architecture

### KV Store Technology

**NATS JetStream KeyValue Store**
- Backed by JetStream streams for durability
- Automatic replication and persistence
- History tracking (configurable retention)
- Watch/Subscribe capabilities for state changes
- Optimized for high-throughput updates

### State Flow

```
MAVLink Messages → Telemetry Stream → Global State Worker → KV Store → Consumers
     ↓                    ↓                    ↓                ↓           ↓
  Device            NATS Subject         State Aggregation   Storage    API/UI
```

## Data Model

### Key Structure

```
entity:<entity_id>
```

**Examples:**
```
entity:2f160b52-37fa-4746-882e-e08ffc395e16
entity:drone-001
entity:sensor-alpha-1
```

### Value Structure (JSON)

The value is a comprehensive JSON object containing all entity state:

```json
{
  // ═══════════════════════════════════════════════════════════
  // CORE ENTITY IDENTITY (from entities table)
  // ═══════════════════════════════════════════════════════════
  "entity_id": "2f160b52-37fa-4746-882e-e08ffc395e16",
  "org_id": "org-alpha",
  "entity_type": "aircraft_multirotor",
  "status": "active",
  "priority": "normal",
  "is_live": true,
  "expiry_time": null,

  // ═══════════════════════════════════════════════════════════
  // POSITION STATE
  // ═══════════════════════════════════════════════════════════
  "position": {
    "global": {
      "latitude": 37.0746863,
      "longitude": 126.8984223,
      "altitude_msl": 194.45886,
      "altitude_relative": 13.482539,
      "accuracy_h": 41.35,
      "accuracy_v": 0.39,
      "timestamp": "2025-11-18T22:27:27.959220Z"
    },
    "local": {
      "x": -0.027861081,
      "y": 0.02613387,
      "z": -13.482539,
      "vx": 0.037202112,
      "vy": 0.031698756,
      "vz": -0.032255556,
      "timestamp": "2025-11-18T22:27:27.980889Z"
    }
  },

  // ═══════════════════════════════════════════════════════════
  // ATTITUDE STATE
  // ═══════════════════════════════════════════════════════════
  "attitude": {
    "euler": {
      "roll": -0.0065508625,
      "pitch": 0.002233883,
      "yaw": 2.8599913,
      "rollspeed": 0.010235973,
      "pitchspeed": 0.02417505,
      "yawspeed": 0.01153238,
      "timestamp": "2025-11-18T22:27:29.823867Z"
    },
    "quaternion": {
      "q1": 0.14007995,
      "q2": -0.0020208368,
      "q3": -0.003149333,
      "q4": 0.9901332,
      "rollspeed": -0.021508677,
      "pitchspeed": -0.00033572392,
      "yawspeed": -0.0019110515,
      "timestamp": "2025-11-18T22:27:29.855997Z"
    }
  },

  // ═══════════════════════════════════════════════════════════
  // VEHICLE STATUS
  // ═══════════════════════════════════════════════════════════
  "vehicle_status": {
    "armed": true,
    "mode": "GUIDED",
    "custom_mode": 50593792,
    "autopilot": 12,
    "system_status": 3,
    "vehicle_type": 13,
    "landed_state": 1,
    "load": 453,
    "sensors_enabled": 35717133,
    "sensors_health": 572555327,
    "timestamp": "2025-11-18T22:27:28.980245Z"
  },

  // ═══════════════════════════════════════════════════════════
  // POWER SYSTEM
  // ═══════════════════════════════════════════════════════════
  "power": {
    "voltage": 13.525,
    "current": 1.51,
    "battery_remaining": 0,
    "consumed": -1744830465,
    "energy_consumed": 16855040,
    "cells": [65535, 49919, 54539, 65332, 65535, 65535, 65535, 65535, 65535, 65535],
    "temperature": -256,
    "timestamp": "2025-11-18T22:27:29.006560Z"
  },

  // ═══════════════════════════════════════════════════════════
  // FLIGHT PERFORMANCE (VFR HUD)
  // ═══════════════════════════════════════════════════════════
  "vfr": {
    "airspeed": 0,
    "groundspeed": 0.022684656,
    "heading": 164,
    "climb_rate": 0,
    "throttle": 17218,
    "altitude": -0.012521066,
    "timestamp": "2025-11-18T22:27:29.431718Z"
  },

  // ═══════════════════════════════════════════════════════════
  // MISSION STATE
  // ═══════════════════════════════════════════════════════════
  "mission": {
    "current_waypoint": 0,
    "timestamp": "2025-11-18T22:27:29.800993Z"
  },

  // ═══════════════════════════════════════════════════════════
  // CONTROL OUTPUTS (Actuators)
  // ═══════════════════════════════════════════════════════════
  "actuators": {
    "servos": [900, 900, 900, 900, 900, 900, 900, 900],
    "timestamp": "2025-11-18T22:27:28.969624Z"
  },

  // ═══════════════════════════════════════════════════════════
  // ENVIRONMENTAL
  // ═══════════════════════════════════════════════════════════
  "environment": {
    "pressure_abs": 990.06,
    "pressure_diff": 0,
    "temperature": 4117,
    "timestamp": "2025-11-18T22:27:28.002422Z"
  },

  // ═══════════════════════════════════════════════════════════
  // DATABASE FIELDS (from entities table)
  // ═══════════════════════════════════════════════════════════
  "components": {},
  "aliases": {},
  "tags": [],
  "source": "mavlink",
  "created_by": "system",
  "classification": null,
  "metadata": {},

  // ═══════════════════════════════════════════════════════════
  // TIMESTAMPS
  // ═══════════════════════════════════════════════════════════
  "created_at": "2025-11-18T22:00:00Z",
  "updated_at": "2025-11-18T22:27:29.855997Z"
}
```

## MAVLink Message → State Mapping

### Message Processing Strategy

Each MAVLink message type updates specific sections of the entity state:

| MAVLink Message | State Section | Fields Updated | Update Frequency |
|----------------|---------------|----------------|------------------|
| `HEARTBEAT` | `vehicle_status` | armed, mode, custom_mode, autopilot, system_status, vehicle_type | 1 Hz |
| `SYS_STATUS` | `vehicle_status`, `power` | load, sensors_enabled, sensors_health, voltage, current, battery_remaining | 1 Hz |
| `GPS_RAW_INT` | `position.global` | latitude, longitude, altitude_msl, accuracy_h, accuracy_v | 1-5 Hz |
| `ATTITUDE` | `attitude.euler` | roll, pitch, yaw, rollspeed, pitchspeed, yawspeed | 10-50 Hz |
| `ATTITUDE_QUATERNION` | `attitude.quaternion` | q1, q2, q3, q4, rollspeed, pitchspeed, yawspeed | 10-50 Hz |
| `LOCAL_POSITION_NED` | `position.local` | x, y, z, vx, vy, vz | 10-50 Hz |
| `ALTITUDE` | `position.global` | altitude_msl, altitude_relative | 1-5 Hz |
| `VFR_HUD` | `vfr` | airspeed, groundspeed, heading, climb_rate, throttle, altitude | 4 Hz |
| `MISSION_CURRENT` | `mission` | current_waypoint | Event-driven |
| `BATTERY_STATUS` | `power` | battery_remaining, current, consumed, energy_consumed, cells, temperature | 1 Hz |
| `SERVO_OUTPUT_RAW` | `actuators` | servos[0-7] | 10 Hz |
| `SCALED_PRESSURE` | `environment` | pressure_abs, pressure_diff, temperature | 1 Hz |
| `EXTENDED_SYS_STATE` | `vehicle_status` | landed_state, vtol_state | 1 Hz |

### Message Processing Flow

```go
1. Receive MAVLink message on NATS subject
   → constellation.telemetry.{org_id}.{entity_id}

2. Parse message type and data payload

3. Get current entity state from KV store
   → kv.Get("entity:{entity_id}")

4. Update relevant state sections based on message type
   → Partial update strategy (only changed fields)

5. Set state.updated_at = now()

6. Atomically write back to KV store
   → kv.Put("entity:{entity_id}", updated_state)

7. (Optional) Publish state snapshot to global stream
   → constellation.state.{org_id}.{entity_id}
```

## State Coherency Strategy

### Timestamp-based Staleness Detection

Each subsection of the state includes its own timestamp. This enables:

1. **Staleness Detection**: Identify outdated state sections
2. **Partial State Validation**: Know which subsystems are reporting
3. **Temporal Correlation**: Match state across different update rates

```go
// Example: Detect stale GPS data
if time.Since(state.Position.Global.Timestamp) > 5*time.Second {
    log.Warn("GPS data is stale")
    state.Position.Global = nil // Clear stale data
}
```

### Atomic Updates

NATS KV provides atomic put operations. The update strategy is:

```go
// Optimistic concurrency pattern
1. Get current state
2. Modify specific fields
3. Put updated state (atomic)
4. Retry on conflict (optional)
```

For high-frequency updates (e.g., ATTITUDE at 50 Hz), we batch updates:

```go
// Batch strategy for high-frequency messages
accumulator := NewStateAccumulator(100ms)
accumulator.Add(attitudeMsg)
// After 100ms, flush accumulated changes to KV
```

## NATS JetStream Configuration

### KV Bucket Configuration

```go
kv, err := js.CreateKeyValue(&nats.KeyValueConfig{
    Bucket:      "CONSTELLATION_GLOBAL_STATE",
    Description: "Real-time global state for all entities",
    MaxBytes:    512 * 1024 * 1024, // 512MB
    TTL:         0,                  // No TTL - persist until deleted
    History:     10,                 // Keep last 10 versions
    Replicas:    1,                  // Single replica (increase for HA)
    Storage:     nats.FileStorage,   // Durable file storage
})
```

### State Change Stream (Optional)

For consumers that need state change events:

```go
js.AddStream(&nats.StreamConfig{
    Name:        "CONSTELLATION_STATE_CHANGES",
    Subjects:    []string{"constellation.state.>"},
    Retention:   nats.LimitsPolicy,
    MaxAge:      1 * time.Hour,      // Keep 1 hour of state changes
    MaxBytes:    128 * 1024 * 1024,  // 128MB
    MaxMsgSize:  64 * 1024,          // 64KB per state snapshot
    Duplicates:  30 * time.Second,   // Deduplication window
    AllowDirect: true,               // Allow direct gets
    Discard:     nats.DiscardOld,
})
```

## Update Rate Management

### Throttling Strategy

To prevent KV store saturation from high-frequency telemetry:

| State Section | Raw Message Rate | KV Update Rate | Strategy |
|--------------|------------------|----------------|----------|
| `position.global` | 5 Hz | 5 Hz | Direct update |
| `position.local` | 50 Hz | 10 Hz | Batch + Sample |
| `attitude.euler` | 50 Hz | 10 Hz | Batch + Sample |
| `attitude.quaternion` | 50 Hz | 10 Hz | Batch + Sample |
| `vfr` | 4 Hz | 4 Hz | Direct update |
| `vehicle_status` | 1 Hz | 1 Hz | Direct update |
| `power` | 1 Hz | 1 Hz | Direct update |
| `mission` | Event | Event | Direct update |

**Batching Implementation:**

```go
type StateUpdateBatcher struct {
    entityID     string
    batchWindow  time.Duration  // e.g., 100ms
    pendingState *EntityState
    lastFlush    time.Time
}

func (b *StateUpdateBatcher) Add(msg MAVLinkMessage) {
    // Accumulate changes in memory
    b.pendingState.Update(msg)

    // Flush after batch window
    if time.Since(b.lastFlush) > b.batchWindow {
        b.Flush()
    }
}

func (b *StateUpdateBatcher) Flush() {
    kv.Put("entity:"+b.entityID, b.pendingState)
    b.lastFlush = time.Now()
}
```

## API Access Patterns

### Read Operations

```go
// Get single entity state
GET /api/v1/state/entities/{entity_id}

// Get multiple entity states
GET /api/v1/state/entities?ids=id1,id2,id3

// Get all entities for an organization
GET /api/v1/state/organizations/{org_id}/entities

// Watch entity state changes (WebSocket)
WS /api/v1/state/entities/{entity_id}/watch
```

### Write Operations (Restricted)

```go
// Manual state update (for non-telemetry sources)
PATCH /api/v1/state/entities/{entity_id}
{
  "status": "maintenance",
  "metadata": {"note": "Scheduled maintenance"}
}
```

## Monitoring & Observability

### Metrics to Track

1. **Update Rates**
   - KV put operations per second
   - Messages processed per entity
   - Batch flush frequency

2. **State Freshness**
   - Time since last update per subsection
   - Stale state detection events

3. **Storage**
   - KV bucket size
   - Number of entities
   - Average state size per entity

4. **Performance**
   - KV get/put latency (p50, p95, p99)
   - Message processing latency
   - Batch accumulation time

### Health Checks

```go
// Check KV store health
func (w *GlobalStateWorker) HealthCheck() error {
    // 1. Verify KV connection
    if w.kv == nil {
        return errors.New("KV store not initialized")
    }

    // 2. Test read/write
    testKey := "health:check"
    if _, err := w.kv.Put(testKey, []byte("ok")); err != nil {
        return err
    }

    // 3. Check bucket status
    status, err := w.kv.Status()
    if err != nil {
        return err
    }

    // 4. Verify not full
    if status.Bytes() > status.MaxBytes()*0.9 {
        return errors.New("KV store nearly full")
    }

    return nil
}
```

## Migration Strategy

### Initial Sync

When deploying the global state system to an existing fleet:

```go
// Populate KV from database
func InitialSync(db *sql.DB, kv nats.KeyValue) error {
    entities, err := db.Query("SELECT * FROM entities WHERE status='active'")
    if err != nil {
        return err
    }
    defer entities.Close()

    for entities.Next() {
        var e EntityState
        // Scan entity from DB
        entities.Scan(&e)

        // Write to KV
        data, _ := json.Marshal(e)
        kv.Put("entity:"+e.EntityID, data)
    }

    return nil
}
```

### Reconciliation

Periodic reconciliation between database and KV store:

```go
// Every 5 minutes, verify KV state matches database
func Reconcile(db *sql.DB, kv nats.KeyValue) error {
    // Compare entity IDs
    // Update KV if DB has changes
    // Update DB if KV has changes
    // Log discrepancies
}
```

## Security Considerations

### Access Control

```go
// KV bucket permissions
- Read: All authenticated services
- Write: Only global-state-worker
- Admin: System administrators

// NATS subject permissions
constellation.state.{org_id}.>
  - Publish: global-state-worker only
  - Subscribe: Any service in same org
```

### Data Encryption

```go
// TLS for NATS connections
// Encryption at rest (if required)
// Field-level encryption for sensitive data
```

## Future Enhancements

1. **Multi-Region Replication**: Replicate KV across geographic regions
2. **Time-Series Export**: Periodic snapshots to TimescaleDB for analytics
3. **State Compression**: Compress large state objects
4. **Delta Updates**: Only send changed fields to reduce bandwidth
5. **State Versioning**: Track state version numbers for conflict resolution
6. **Alerting**: Trigger alerts on state anomalies (e.g., sudden position jumps)

## References

- [NATS JetStream KV Documentation](https://docs.nats.io/nats-concepts/jetstream/key-value-store)
- [MAVLink Message Definitions](https://mavlink.io/en/messages/common.html)
- [Constellation TAK Ontology](../../prd/design/CONSTELLATION_TAK_ONTOLOGY.md)
- [Database Schema](../../../db/schema.sql)
