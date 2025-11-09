# MAVLink to NATS JetStream Publisher Specification

## Document Version

- **Version**: 1.0.0
- **Last Updated**: 2025-11-06
- **Status**: Verified against Constellation Overwatch v1

## Overview

RF Streams
- Telemetry
- Video
- RTK

This specification provides the complete technical blueprint for publishing MAVLink telemetry from PX4-based systems to Constellation Overwatch's NATS JetStream infrastructure. This document has been verified against the existing codebase and provides exact message formats, subjects, and schemas required for integration.

## System Requirements

### NATS Server Configuration
- **Port**: 4222 (default)
- **WebSocket Port**: 8222 (if TLS enabled)
- **JetStream Domain**: constellation
- **Connection URL**: `nats://localhost:4222`

### JetStream Streams (Verified from embedded-nats/nats.go)

| Stream Name | Subjects | Retention Policy | Max Messages | Max Age | Max Msg Size |
|------------|----------|------------------|--------------|---------|--------------|
| CONSTELLATION_TELEMETRY | constellation.telemetry.> | InterestPolicy | 25,000 | 1 hour | 64 KB |
| CONSTELLATION_ENTITIES | constellation.entities.> | LimitsPolicy | 100,000 | 7 days | 1 MB |
| CONSTELLATION_EVENTS | constellation.events.> | WorkQueuePolicy | 50,000 | 24 hours | 256 KB |
| CONSTELLATION_COMMANDS | constellation.commands.> | WorkQueuePolicy | 10,000 | 15 minutes | 32 KB |

## MAVLink Connection Configuration

### UDP Configuration (Primary)
```yaml
connection:
  type: udp
  port: 14550          # Default PX4 UDP port
  bind_address: 0.0.0.0
  buffer_size: 65535   # Maximum UDP packet size
  timeout_ms: 1000
```

### TCP Configuration (Alternative)
```yaml
connection:
  type: tcp
  port: 5760           # Default PX4 TCP port
  bind_address: 0.0.0.0
  keepalive: true
  nodelay: true
```

### Serial Configuration (Ground Station)
```yaml
connection:
  type: serial
  device: /dev/ttyUSB0  # or /dev/ttyACM0
  baudrate: 57600       # Common rates: 57600, 115200, 921600
  databits: 8
  stopbits: 1
  parity: none
```

## NATS Subject Patterns (Verified)

### Telemetry Subjects
```
constellation.telemetry.{org_id}.{entity_id}
```

### Entity Lifecycle Subjects
```
constellation.entities.{org_id}.created
constellation.entities.{org_id}.updated
constellation.entities.{org_id}.deleted
constellation.entities.{org_id}.status
```

### Event Subjects
```
constellation.events.{org_id}.{entity_id}.{event_type}
```

### Command Subjects
```
constellation.commands.{org_id}.{entity_id}
constellation.commands.{org_id}.broadcast
```

## MAVLink Message to NATS Mapping

### 1. HEARTBEAT (#0) → Entity Status

**MAVLink Message Structure:**
```c
typedef struct __mavlink_heartbeat_t {
    uint32_t custom_mode;     // Autopilot-specific mode
    uint8_t type;            // MAV_TYPE
    uint8_t autopilot;       // MAV_AUTOPILOT
    uint8_t base_mode;       // MAV_MODE_FLAG
    uint8_t system_status;   // MAV_STATE
    uint8_t mavlink_version;
} mavlink_heartbeat_t;
```

**NATS Publish Subject:**
```
constellation.telemetry.{org_id}.{entity_id}
```

**JSON Schema:**
```json
{
  "entity_id": "string",        // e.g., "mav_1"
  "org_id": "string",           // e.g., "org_default"
  "timestamp": "string",        // RFC3339 format
  "message_type": "heartbeat",
  "data": {
    "type": "string",           // aircraft_multirotor|ground_vehicle|etc
    "autopilot": "string",      // PX4|ArduPilot|etc
    "armed": "boolean",         // from base_mode & 0x80
    "mode": "string",           // decoded flight mode
    "custom_mode": "number",    // raw custom mode value
    "system_status": "string",  // active|standby|critical|emergency
    "mavlink_version": "number"
  }
}
```

**Entity Type Mapping:**
| MAV_TYPE | Entity Type |
|----------|------------|
| 0 (GENERIC) | device |
| 1 (FIXED_WING) | aircraft_fixed_wing |
| 2 (QUADROTOR) | aircraft_multirotor |
| 3 (COAXIAL) | aircraft_multirotor |
| 4 (HELICOPTER) | aircraft_rotorcraft |
| 5 (ANTENNA_TRACKER) | sensor_fixed |
| 6 (GCS) | control_station |
| 10 (GROUND_ROVER) | ground_vehicle |
| 11 (SURFACE_BOAT) | maritime_surface |
| 12 (SUBMARINE) | maritime_subsurface |
| 13 (HEXAROTOR) | aircraft_multirotor |
| 14 (OCTOROTOR) | aircraft_multirotor |
| 15 (TRICOPTER) | aircraft_multirotor |

### 2. GLOBAL_POSITION_INT (#33) → Position Telemetry

**MAVLink Message Structure:**
```c
typedef struct __mavlink_global_position_int_t {
    uint32_t time_boot_ms;  // Timestamp (ms since boot)
    int32_t lat;           // Latitude (degE7)
    int32_t lon;           // Longitude (degE7)
    int32_t alt;           // Altitude MSL (mm)
    int32_t relative_alt;  // Altitude above ground (mm)
    int16_t vx;           // Ground X velocity (cm/s)
    int16_t vy;           // Ground Y velocity (cm/s)
    int16_t vz;           // Ground Z velocity (cm/s)
    uint16_t hdg;         // Heading (cdeg, 0-35999)
} mavlink_global_position_int_t;
```

**NATS Publish Subject:**
```
constellation.telemetry.{org_id}.{entity_id}
```

**JSON Schema:**
```json
{
  "entity_id": "string",
  "org_id": "string",
  "timestamp": "string",
  "message_type": "position",
  "data": {
    "latitude": "number",         // decimal degrees
    "longitude": "number",        // decimal degrees
    "altitude": "number",         // meters MSL
    "altitude_relative": "number", // meters AGL
    "velocity": {
      "north": "number",         // m/s
      "east": "number",          // m/s
      "down": "number",          // m/s (positive down)
      "ground_speed": "number"   // m/s calculated
    },
    "heading": "number",         // degrees (0-360)
    "time_boot_ms": "number"
  }
}
```

**Conversion Formulas:**
```javascript
latitude = lat / 1e7                    // degE7 to degrees
longitude = lon / 1e7                   // degE7 to degrees
altitude = alt / 1000                   // mm to meters
altitude_relative = relative_alt / 1000 // mm to meters
velocity_north = vx / 100               // cm/s to m/s
velocity_east = vy / 100                // cm/s to m/s
velocity_down = vz / 100                // cm/s to m/s
heading = hdg / 100                     // cdeg to degrees
ground_speed = Math.sqrt(vx*vx + vy*vy) / 100
```

### 3. ATTITUDE (#30) → Orientation Telemetry

**MAVLink Message Structure:**
```c
typedef struct __mavlink_attitude_t {
    uint32_t time_boot_ms;  // Timestamp (ms)
    float roll;            // Roll (rad, -pi..+pi)
    float pitch;           // Pitch (rad, -pi..+pi)
    float yaw;             // Yaw (rad, -pi..+pi)
    float rollspeed;       // Roll rate (rad/s)
    float pitchspeed;      // Pitch rate (rad/s)
    float yawspeed;        // Yaw rate (rad/s)
} mavlink_attitude_t;
```

**NATS Publish Subject:**
```
constellation.telemetry.{org_id}.{entity_id}
```

**JSON Schema:**
```json
{
  "entity_id": "string",
  "org_id": "string",
  "timestamp": "string",
  "message_type": "attitude",
  "data": {
    "roll": "number",           // degrees
    "pitch": "number",          // degrees
    "yaw": "number",            // degrees
    "roll_rate": "number",      // rad/s
    "pitch_rate": "number",     // rad/s
    "yaw_rate": "number",       // rad/s
    "time_boot_ms": "number"
  }
}
```

**Conversion Formula:**
```javascript
degrees = radians * 180 / Math.PI
```

### 4. BATTERY_STATUS (#147) → Battery Telemetry

**MAVLink Message Structure:**
```c
typedef struct __mavlink_battery_status_t {
    int32_t current_consumed;    // mAh consumed
    int32_t energy_consumed;     // hJ consumed
    int16_t temperature;         // Battery temp (cdegC)
    uint16_t voltages[10];       // Cell voltages (mV)
    int16_t current_battery;     // Battery current (cA)
    uint8_t id;                  // Battery ID
    uint8_t battery_function;    // Function
    uint8_t type;               // Chemistry
    int8_t battery_remaining;    // Remaining (%)
} mavlink_battery_status_t;
```

**NATS Publish Subject:**
```
constellation.telemetry.{org_id}.{entity_id}
```

**JSON Schema:**
```json
{
  "entity_id": "string",
  "org_id": "string",
  "timestamp": "string",
  "message_type": "battery",
  "data": {
    "battery_id": "number",
    "remaining_percent": "number",
    "voltage": "number",           // total voltage in volts
    "current": "number",           // amperes
    "current_consumed": "number",  // mAh
    "energy_consumed": "number",   // joules (hJ/100)
    "temperature": "number",       // celsius
    "cells": [                    // array of cell voltages
      "number"                     // volts per cell
    ],
    "cell_count": "number"
  }
}
```

### 5. SYS_STATUS (#1) → System Health

**MAVLink Message Structure:**
```c
typedef struct __mavlink_sys_status_t {
    uint32_t onboard_control_sensors_present;
    uint32_t onboard_control_sensors_enabled;
    uint32_t onboard_control_sensors_health;
    uint16_t load;              // CPU load (0.1%)
    uint16_t voltage_battery;   // Battery voltage (mV)
    int16_t current_battery;    // Battery current (cA)
    uint16_t drop_rate_comm;    // Comm drop rate (0.01%)
    uint16_t errors_comm;       // Comm errors
    uint16_t errors_count[4];   // Autopilot errors
    int8_t battery_remaining;   // Battery remaining (%)
} mavlink_sys_status_t;
```

**NATS Publish Subject:**
```
constellation.telemetry.{org_id}.{entity_id}
```

**JSON Schema:**
```json
{
  "entity_id": "string",
  "org_id": "string",
  "timestamp": "string",
  "message_type": "sys_status",
  "data": {
    "cpu_load": "number",          // percentage
    "voltage": "number",           // volts
    "current": "number",           // amperes
    "battery_remaining": "number", // percentage
    "comm_drop_rate": "number",    // percentage
    "comm_errors": "number",
    "sensors": {
      "present": "number",         // bitmask
      "enabled": "number",         // bitmask
      "health": "number"           // bitmask
    }
  }
}
```

### 6. STATUSTEXT (#253) → Event Messages

**MAVLink Message Structure:**
```c
typedef struct __mavlink_statustext_t {
    uint8_t severity;    // MAV_SEVERITY
    char text[50];      // Status text
} mavlink_statustext_t;
```

**NATS Publish Subject:**
```
constellation.events.{org_id}.{entity_id}.status_text
```

**JSON Schema:**
```json
{
  "entity_id": "string",
  "org_id": "string",
  "timestamp": "string",
  "message_type": "status_text",
  "severity": "string",  // emergency|alert|critical|error|warning|notice|info|debug
  "text": "string"
}
```

**Severity Mapping:**
| MAV_SEVERITY | String Value |
|-------------|-------------|
| 0 | emergency |
| 1 | alert |
| 2 | critical |
| 3 | error |
| 4 | warning |
| 5 | notice |
| 6 | info |
| 7 | debug |

### 7. GPS_RAW_INT (#24) → GPS Telemetry

**MAVLink Message Structure:**
```c
typedef struct __mavlink_gps_raw_int_t {
    uint64_t time_usec;         // Timestamp (μs)
    int32_t lat;               // Latitude (degE7)
    int32_t lon;               // Longitude (degE7)
    int32_t alt;               // Altitude MSL (mm)
    uint16_t eph;              // HDOP * 100
    uint16_t epv;              // VDOP * 100
    uint16_t vel;              // Ground speed (cm/s)
    uint16_t cog;              // Course over ground (cdeg)
    uint8_t fix_type;          // GPS fix type
    uint8_t satellites_visible; // Number of satellites
} mavlink_gps_raw_int_t;
```

**NATS Publish Subject:**
```
constellation.telemetry.{org_id}.{entity_id}
```

**JSON Schema:**
```json
{
  "entity_id": "string",
  "org_id": "string",
  "timestamp": "string",
  "message_type": "gps",
  "data": {
    "fix_type": "string",         // no_fix|2d|3d|dgps|rtk_float|rtk_fixed
    "satellites": "number",
    "latitude": "number",
    "longitude": "number",
    "altitude": "number",
    "ground_speed": "number",     // m/s
    "course": "number",           // degrees
    "hdop": "number",            // horizontal dilution
    "vdop": "number",            // vertical dilution
    "time_usec": "number"
  }
}
```

## PX4 Flight Mode Decoding

### PX4 Custom Mode Structure
```javascript
// PX4 packs mode as: [sub_mode(8) | main_mode(8) | reserved(16)]
const mainMode = (customMode >> 16) & 0xFF;
const subMode = (customMode >> 24) & 0xFF;
```

### Main Mode Mapping
| Main Mode | Sub Mode | Flight Mode Name |
|-----------|----------|-----------------|
| 1 | 0 | MANUAL |
| 1 | 1 | ALTCTL |
| 1 | 2 | POSCTL |
| 2 | 0 | AUTO_LOITER |
| 2 | 1 | AUTO_RTL |
| 2 | 2 | AUTO_MISSION |
| 3 | 0 | ACRO |
| 4 | 0 | OFFBOARD |
| 5 | 0 | STABILIZED |
| 6 | 0 | RATTITUDE |

## Entity Management

### Entity Registration Flow
1. Receive first HEARTBEAT from system
2. Extract system_id and component_id
3. Create entity_id: `mav_{system_id}` or configured prefix
4. Determine entity_type from MAV_TYPE
5. Publish to `constellation.entities.{org_id}.created`

### Entity Status Updates
- Online: HEARTBEAT received within 30 seconds
- Offline: No HEARTBEAT for > 30 seconds
- Status published to: `constellation.entities.{org_id}.status`

## Implementation Checklist

### Required Dependencies
```bash
# Go dependencies
go get github.com/bluenviron/gomavlib/v2
go get github.com/bluenviron/gomavlib/v2/pkg/dialects/ardupilotmega
go get github.com/nats-io/nats.go
```

### Environment Variables
```bash
# NATS Configuration
NATS_URL=nats://localhost:4222
NATS_CONNECT_TIMEOUT=10s
NATS_MAX_RECONNECTS=-1

# MAVLink Configuration
MAVLINK_UDP_PORT=14550
MAVLINK_TCP_PORT=5760
MAVLINK_SYSTEM_ID=255       # GCS ID
MAVLINK_COMPONENT_ID=0

# Entity Configuration
MAVLINK_ORG_ID=default_org
MAVLINK_ENTITY_PREFIX=mav_

# Publishing Rates (Hz)
TELEMETRY_RATE_LIMIT=10
POSITION_RATE_LIMIT=5
ATTITUDE_RATE_LIMIT=20
```

## Message Publishing Guidelines

### Deduplication
Use NATS message ID header for deduplication:
```go
msg := nats.NewMsg(subject)
msg.Data = jsonData
msg.Header.Set("Nats-Msg-Id", fmt.Sprintf("%s-%d-%d", entityID, messageType, timestamp))
```

### Rate Limiting
Implement per-message-type rate limiting:
- HEARTBEAT: 1 Hz
- GLOBAL_POSITION_INT: 5 Hz
- ATTITUDE: 10 Hz
- BATTERY_STATUS: 0.2 Hz
- SYS_STATUS: 1 Hz

### Error Handling
1. **Connection Loss**: Implement exponential backoff reconnection
2. **Parse Errors**: Log and skip malformed messages
3. **Publish Failures**: Buffer locally with circular buffer
4. **Stream Full**: Drop oldest telemetry, preserve events

## Testing Endpoints

### SITL (Software In The Loop) Testing
```bash
# Start PX4 SITL
make px4_sitl gazebo

# MAVLink will be available on:
# UDP: 14550 (broadcast)
# TCP: 5760
```

### MAVProxy Testing
```bash
# Forward MAVLink from serial to UDP
mavproxy.py --master=/dev/ttyUSB0 --baudrate=57600 --out=udp:127.0.0.1:14550
```

### Verification Commands
```bash
# Subscribe to telemetry
nats sub "constellation.telemetry.>"

# Check stream info
nats stream info CONSTELLATION_TELEMETRY

# Monitor specific entity
nats sub "constellation.telemetry.default_org.mav_1"
```

## Security Notes

1. **No Authentication**: MAVLink protocol has no built-in authentication
2. **Message Validation**: Always validate ranges and data types
3. **System ID Filtering**: Only accept configured system IDs
4. **Rate Limiting**: Enforce rate limits to prevent flooding
5. **Network Isolation**: Use VPN or private networks for production

## Performance Targets

- Support 100+ concurrent MAVLink connections
- Process 1000+ messages/second aggregate
- Maintain < 100ms publish latency
- Buffer 1000 messages during disconnection
- Achieve 99.9% message delivery rate

## Compliance Notes

This specification complies with:
- MAVLink 2.0 Protocol
- NATS JetStream Best Practices
- Constellation Overwatch Entity Model
- PX4 Flight Stack Standards