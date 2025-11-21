# Overwatch Publisher Payload Specification
**Version:** 1.0.0
**Last Updated:** 2025-11-21

## Overview

This document defines the **minimum required payload structure** for all clients publishing to Constellation Overwatch. These requirements ensure proper entity registration, lifecycle management, and global state synchronization.

## Critical Requirements

### 1. Entity Identification

**Every entity MUST have a unique identifier** that is:
- **Immutable** for the entity's lifetime
- **Valid for NATS KV keys** (no dots, asterisks, spaces, or special characters)
- **Consistent** across all message types

#### Identifier Priority (Fallback Chain)

Overwatch extracts entity identity in this order:

```
1. event.entity_id          (preferred)
2. event.device_id          (fallback for bootsequence)
3. event.source.entity_id   (nested fallback)
4. event.source.device_id   (last resort)
```

#### Valid Identifier Format

```regex
^[a-zA-Z0-9_-]+$
```

**Valid Examples:**
- `1048bff5-5b97-4fa8-a0f1-061662b32163` (UUID)
- `b546cd5c6dc0b878` (device hash)
- `drone-alpha-01` (human-readable)
- `isr_camera_001` (descriptive)

**Invalid Examples:**
- `device.123` ❌ (contains dots)
- `sensor*01` ❌ (contains asterisk)
- `unit 5` ❌ (contains space)
- `.hidden` ❌ (starts with dot)
- `_internal` ❌ (starts with underscore - reserved)

---

## Message Types

### 1. Bootsequence (Entity Registration)

**Purpose:** Register an entity with Overwatch when it comes online.

**NATS Subject:**
```
constellation.events.{category}.{org_id}.{entity_id}
constellation.events.isr.2f160b52-37fa-4746-882e-e08ffc395e16.1048bff5-5b97-4fa8-a0f1-061662b32163
```

**Minimum Required Payload:**

```json
{
  "event_type": "bootsequence",
  "entity_id": "1048bff5-5b97-4fa8-a0f1-061662b32163",
  "device_id": "b546cd5c6dc0b878",
  "organization_id": "2f160b52-37fa-4746-882e-e08ffc395e16",
  "timestamp": "2025-11-21T14:00:20.306128+00:00",
  "message": "Overwatch ISR component initialized",
  "source": {
    "component": {
      "name": "constellation-isr",
      "type": "yoloe-c4isr-threat-detection",
      "version": "1.0.0"
    },
    "device_id": "b546cd5c6dc0b878",
    "entity_id": "1048bff5-5b97-4fa8-a0f1-061662b32163",
    "fingerprinted_at": "2025-11-21T14:00:20.280029+00:00"
  }
}
```

**Field Requirements:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `event_type` | string | **YES** | Must be `"bootsequence"` |
| `entity_id` | string | **RECOMMENDED** | Primary entity identifier (UUID/slug) |
| `device_id` | string | **YES** | Fallback if entity_id not set |
| `organization_id` | string | NO | Organization UUID |
| `timestamp` | ISO8601 | **YES** | Event timestamp |
| `source.device_id` | string | **YES** | Device fingerprint (backup identifier) |
| `source.entity_id` | string | NO | Entity ID in source (backup) |
| `source.component.name` | string | **YES** | Component name |
| `source.component.type` | string | **YES** | Component type/mode |

**Behavior:**
- Overwatch registers the entity in the in-memory registry
- Entity is now authorized to send telemetry and detections
- If entity_id is missing, device_id is used as the entity_id

---

### 2. Shutdown (Entity Deregistration)

**Purpose:** Unregister an entity when it goes offline.

**NATS Subject:**
```
constellation.events.{category}.{org_id}.{entity_id}
```

**Minimum Required Payload:**

```json
{
  "event_type": "shutdown",
  "entity_id": "1048bff5-5b97-4fa8-a0f1-061662b32163",
  "device_id": "b546cd5c6dc0b878",
  "timestamp": "2025-11-21T14:05:56.906340+00:00",
  "message": "Overwatch ISR component shutting down gracefully",
  "final_analytics": {
    "total_frames_processed": 1250,
    "total_unique_objects": 42
  },
  "source": {
    "device_id": "b546cd5c6dc0b878",
    "entity_id": "1048bff5-5b97-4fa8-a0f1-061662b32163"
  }
}
```

**Field Requirements:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `event_type` | string | **YES** | Must be `"shutdown"` or `"delete"` |
| `entity_id` | string | **RECOMMENDED** | Entity being deregistered |
| `device_id` | string | **YES** | Fallback identifier |
| `timestamp` | ISO8601 | **YES** | Event timestamp |

**Behavior:**
- Overwatch removes the entity from the in-memory registry
- Future telemetry from this entity will be **rejected**
- Entity must send new bootsequence to re-register

---

### 3. Detection Events

**Purpose:** Report object detection/tracking results.

**NATS Subject:**
```
constellation.events.{category}.{org_id}.{entity_id}
```

**Minimum Required Payload:**

```json
{
  "event_type": "detection",
  "entity_id": "1048bff5-5b97-4fa8-a0f1-061662b32163",
  "device_id": "b546cd5c6dc0b878",
  "timestamp": "2025-11-21T14:00:20.431589+00:00",
  "detection": {
    "label": "person",
    "confidence": 0.95,
    "bbox": {
      "x_min": 0.195,
      "y_min": 0.077,
      "x_max": 0.870,
      "y_max": 0.997
    },
    "track_id": "thzbil22q1dmzd8dxqffkto2",
    "timestamp": "2025-11-21T14:00:20.431589+00:00"
  }
}
```

**Field Requirements:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `event_type` | string | **YES** | Must be `"detection"` |
| `entity_id` | string | **YES** | Must match registered entity |
| `device_id` | string | NO | Device fingerprint |
| `timestamp` | ISO8601 | **YES** | Event timestamp |
| `detection.label` | string | **YES** | Object class label |
| `detection.confidence` | float | **YES** | Detection confidence (0-1) |
| `detection.track_id` | string | **YES** | Unique tracking ID |

**Behavior:**
- Only accepted if entity_id is registered (via bootsequence)
- Detection events do NOT register entities
- Rejected if entity not in registry

---

### 4. Telemetry (MAVLink/Vehicle State)

**Purpose:** Report real-time vehicle telemetry (position, attitude, status).

**NATS Subject:**
```
constellation.telemetry.{org_id}.{entity_id}
constellation.telemetry.isr.2f160b52-37fa-4746-882e-e08ffc395e16.1048bff5-5b97-4fa8-a0f1-061662b32163
```

**Minimum Required Payload:**

```json
{
  "message_type": "HEARTBEAT",
  "system_id": 1,
  "component_id": 1,
  "timestamp": "2025-11-21T14:00:30.123456+00:00",
  "data": {
    "custom_mode": 4,
    "base_mode": 128,
    "system_status": 4,
    "autopilot": 3,
    "type": 2
  }
}
```

**Field Requirements:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `message_type` | string | **YES** | MAVLink message type |
| `system_id` | int | **YES** | MAVLink system ID |
| `timestamp` | ISO8601 | **YES** | Message timestamp |
| `data` | object | **YES** | MAVLink message data |

**Supported Message Types:**
- `HEARTBEAT` - System status and mode
- `GPS_RAW_INT` - GPS position
- `ATTITUDE` - Euler angles
- `BATTERY_STATUS` - Power system
- `VFR_HUD` - Flight performance
- See [telemetry.go:296-327](../../pkg/services/workers/telemetry.go#L296-L327) for full list

**Behavior:**
- Only accepted if entity_id is registered
- Updates global state in KV store
- Rejected if entity not in registry

---

## Global State Mapping

### Entity Lifecycle States

```
┌─────────────────────────────────────────────────────┐
│                  UNREGISTERED                       │
│          (Not in registry or database)              │
└─────────────────────────────────────────────────────┘
                        │
                        │ bootsequence event
                        ▼
┌─────────────────────────────────────────────────────┐
│                    REGISTERED                       │
│         (In-memory registry + optional DB)          │
│                                                     │
│  ✓ Can send telemetry                             │
│  ✓ Can send detections                            │
│  ✓ Global state tracked in KV                     │
└─────────────────────────────────────────────────────┘
                        │
                        │ shutdown/delete event
                        ▼
┌─────────────────────────────────────────────────────┐
│                  UNREGISTERED                       │
│              (Removed from registry)                │
│                                                     │
│  ✗ Telemetry rejected                             │
│  ✗ Detections ignored                             │
└─────────────────────────────────────────────────────┘
```

### KV Store Mapping

**Key Format:**
```
entity:{entity_id}
```

**Example Keys:**
```
entity:1048bff5-5b97-4fa8-a0f1-061662b32163
entity:b546cd5c6dc0b878
entity:drone-alpha-01
```

**Value Structure:**
```json
{
  "entity_id": "1048bff5-5b97-4fa8-a0f1-061662b32163",
  "org_id": "2f160b52-37fa-4746-882e-e08ffc395e16",
  "entity_type": "aircraft_multirotor",
  "status": "active",
  "is_live": true,
  "position": { /* GPS coordinates */ },
  "attitude": { /* Euler/quaternion */ },
  "vehicle_status": { /* Armed, mode, etc */ },
  "power": { /* Battery state */ },
  "updated_at": "2025-11-21T14:00:30.123456+00:00"
}
```

---

## Subject Naming Conventions

### Events
```
constellation.events.{category}.{org_id}.{entity_id}
```

**Examples:**
```
constellation.events.isr.org-123.device-456
constellation.events.c2.mil-unit-5.drone-alpha
```

### Telemetry
```
constellation.telemetry.{org_id}.{entity_id}
constellation.telemetry.{category}.{org_id}.{entity_id}
```

**Examples:**
```
constellation.telemetry.org-123.device-456
constellation.telemetry.isr.org-123.device-456
```

### Commands
```
constellation.commands.{org_id}.{entity_id}
```

---

## Validation Rules

### Entity ID Validation

**Enforced by:** `validateEntityID()` in [telemetry.go:712-740](../../pkg/services/workers/telemetry.go#L712-L740)

```go
// INVALID characters:
- Dots (.)
- Asterisks (*)
- Greater-than (>)
- Spaces ( )
- Leading underscore (_)

// VALID characters:
- Letters (a-z, A-Z)
- Numbers (0-9)
- Hyphens (-)
- Underscores (_) (except leading)
```

### Message Validation

1. **Bootsequence:**
   - Must have `event_type = "bootsequence"`
   - Must have valid entity_id OR device_id
   - Timestamp required

2. **Shutdown:**
   - Must have `event_type = "shutdown"` or `"delete"`
   - Must have valid entity_id OR device_id
   - Timestamp required

3. **Detection:**
   - Must have `event_type = "detection"`
   - Entity must be registered (bootsequence sent first)
   - Must have valid `entity_id`

4. **Telemetry:**
   - Entity must be registered (bootsequence sent first)
   - Must have valid MAVLink message structure
   - Subject must contain valid entity_id

---

## Publisher Implementation Checklist

### ✓ Required Steps

1. **On Startup:**
   ```python
   # Generate or retrieve entity_id
   entity_id = get_entity_id()  # UUID or device fingerprint

   # Validate entity_id format
   assert re.match(r'^[a-zA-Z0-9_-]+$', entity_id)

   # Send bootsequence
   publish_bootsequence(entity_id, device_id, org_id)
   ```

2. **During Operation:**
   ```python
   # Only send telemetry/detections AFTER bootsequence
   if registered:
       publish_telemetry(entity_id, telemetry_data)
       publish_detection(entity_id, detection_data)
   ```

3. **On Shutdown:**
   ```python
   # Always send shutdown before exiting
   publish_shutdown(entity_id, final_stats)
   ```

### ✓ Best Practices

- **Cache entity_id** - Don't regenerate on every message
- **Include device_id** - Provides fallback identification
- **Use ISO8601 timestamps** - Ensures timezone consistency
- **Send heartbeats** - Keep entity alive in Overwatch
- **Handle reconnection** - Send new bootsequence after disconnect

---

## Error Handling

### Registry Rejection

**Symptom:**
```
[TelemetryWorker] Rejecting telemetry for unregistered entity: xyz-123 (not in registry)
```

**Cause:** Entity not registered via bootsequence

**Solution:** Send bootsequence event before telemetry

### Invalid Key Error

**Symptom:**
```
[TelemetryWorker] Failed to save entity state: nats: invalid key
```

**Cause:** entity_id contains invalid characters (dots, spaces, etc)

**Solution:** Use only alphanumeric, hyphens, and underscores

### Missing Entity ID

**Symptom:**
```
[EventWorker] Bootsequence event missing both entity_id and device_id
```

**Cause:** Neither entity_id nor device_id provided

**Solution:** Always include at least device_id in bootsequence

---

## Examples

### Complete Publisher Flow

```python
import nats
import json
from datetime import datetime

class OverwatchPublisher:
    def __init__(self, entity_id, device_id, org_id):
        self.entity_id = entity_id
        self.device_id = device_id
        self.org_id = org_id
        self.nc = None

    async def connect(self):
        self.nc = await nats.connect("nats://localhost:4222")
        await self.send_bootsequence()

    async def send_bootsequence(self):
        subject = f"constellation.events.isr.{self.org_id}.{self.entity_id}"
        payload = {
            "event_type": "bootsequence",
            "entity_id": self.entity_id,
            "device_id": self.device_id,
            "organization_id": self.org_id,
            "timestamp": datetime.utcnow().isoformat() + "Z",
            "message": "Publisher initialized",
            "source": {
                "device_id": self.device_id,
                "entity_id": self.entity_id,
                "component": {
                    "name": "my-publisher",
                    "type": "isr-camera",
                    "version": "1.0.0"
                }
            }
        }
        await self.nc.publish(subject, json.dumps(payload).encode())

    async def send_telemetry(self, telemetry_data):
        subject = f"constellation.telemetry.{self.org_id}.{self.entity_id}"
        await self.nc.publish(subject, json.dumps(telemetry_data).encode())

    async def send_shutdown(self):
        subject = f"constellation.events.isr.{self.org_id}.{self.entity_id}"
        payload = {
            "event_type": "shutdown",
            "entity_id": self.entity_id,
            "device_id": self.device_id,
            "timestamp": datetime.utcnow().isoformat() + "Z",
            "message": "Publisher shutting down"
        }
        await self.nc.publish(subject, json.dumps(payload).encode())
```

---

## Version History

| Version | Date | Changes |
|---------|------|---------|
| 1.0.0 | 2025-11-21 | Initial specification |

---

## References

- [Entity Registry Implementation](../../pkg/services/workers/registry.go)
- [Event Worker](../../pkg/services/workers/event.go)
- [Telemetry Worker](../../pkg/services/workers/telemetry.go)
- [Global State Types](../../pkg/shared/types.go)
- [NATS Subjects](../../pkg/shared/subjects.go)
