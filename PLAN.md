# Plan: Add CONSTELLATION_VIDEO_FRAMES Stream

## Overview

Add a new NATS JetStream stream for video frame data from vision2constellation agents, using a simplified entity-centric subject pattern.

## Subject Pattern
```
constellation.video.{entity_id}
```

Simplified from other streams - entity_id is sufficient for video routing since frames are always entity-specific.

## Stream Configuration

| Setting | Value | Rationale |
|---------|-------|-----------|
| Storage | Memory | Video frames need fast access, not persistence |
| Retention | LimitsPolicy | Keep frames until age/size limits |
| Max Age | 30s | Short retention - video is ephemeral |
| Max Bytes | 512MB | Swarm scale capacity |
| Max Msg Size | 2MB | HD frames with metadata |
| Discard | DiscardOld | Drop oldest frames when full |
| Replicas | 1 | Single node |
| Allow Direct | true | Enable direct get for latest frame |
| Allow Rollup | true | KV-style latest frame access |
| Duplicate Window | 5s | Short window for high-frequency frames |
| Max Msgs Per Subject | 30 | ~1 second of frames per entity at 30fps |

## Message Type: VideoFrame

```go
type VideoFrame struct {
    // Identity
    EntityID    string    `json:"entity_id"`

    // Frame metadata
    FrameID     string    `json:"frame_id"`
    Timestamp   time.Time `json:"timestamp"`
    SequenceNum uint64    `json:"sequence_num"`

    // Source info
    CameraID    string    `json:"camera_id"`
    StreamID    string    `json:"stream_id"`

    // Frame properties
    Width       int       `json:"width"`
    Height      int       `json:"height"`
    Format      string    `json:"format"`   // "jpeg", "h264", "raw"
    Encoding    string    `json:"encoding"` // "base64", "binary"

    // Frame data
    Data        []byte    `json:"data"`

    // Optional: Detection overlay
    Detections  []DetectionBBox `json:"detections,omitempty"`

    // Quality/Priority
    Priority    int       `json:"priority"`
    Quality     int       `json:"quality"`
}

type DetectionBBox struct {
    ClassID    int     `json:"class_id"`
    ClassName  string  `json:"class_name"`
    Confidence float64 `json:"confidence"`
    X          int     `json:"x"`
    Y          int     `json:"y"`
    Width      int     `json:"width"`
    Height     int     `json:"height"`
}
```

## Implementation Steps

### Step 1: Add Subject Definitions
**File:** `pkg/shared/subjects.go`

```go
// Stream name
StreamVideoFrames = "CONSTELLATION_VIDEO_FRAMES"

// Consumer name
ConsumerVideoProcessor = "video-processor"

// Subject base
SubjectVideoBase = "constellation.video"

// Helper function
func VideoFrameSubject(entityID string) string {
    return fmt.Sprintf("constellation.video.%s", entityID)
}
```

### Step 2: Add Message Types
**File:** `pkg/shared/types.go`

Add `VideoFrame` and `DetectionBBox` structs.

### Step 3: Create Stream
**File:** `pkg/services/embedded-nats/nats.go`

Add in `CreateConstellationStreams()`:
```go
videoFramesCfg := &nats.StreamConfig{
    Name:              shared.StreamVideoFrames,
    Description:       "Video frames from vision2constellation agents",
    Subjects:          []string{"constellation.video.>"},
    Storage:           nats.MemoryStorage,
    Retention:         nats.LimitsPolicy,
    MaxAge:            30 * time.Second,
    MaxBytes:          512 * 1024 * 1024,
    MaxMsgSize:        2 * 1024 * 1024,
    Discard:           nats.DiscardOld,
    MaxMsgsPerSubject: 30,
    Replicas:          1,
    DuplicateWindow:   5 * time.Second,
    AllowRollup:       true,
    AllowDirect:       true,
}
```

Add durable consumer creation.

### Step 4: Create VideoWorker
**File:** `pkg/services/workers/video.go` (new)

Worker that:
- Subscribes to `CONSTELLATION_VIDEO_FRAMES`
- Parses `constellation.video.{entity_id}` subjects
- Updates entity state with frame metadata
- Optionally broadcasts to WebSocket

### Step 5: Register Worker
**File:** `pkg/services/workers/manager.go`

Add VideoWorker to manager.

## Files Summary

| File | Action |
|------|--------|
| `pkg/shared/subjects.go` | Modify |
| `pkg/shared/types.go` | Modify |
| `pkg/services/embedded-nats/nats.go` | Modify |
| `pkg/services/workers/video.go` | Create |
| `pkg/services/workers/manager.go` | Modify |
