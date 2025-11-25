package workers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/logger"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/shared"
	"github.com/nats-io/nats.go"
)

// JPEG magic bytes: FF D8 FF
var jpegMagic = []byte{0xFF, 0xD8, 0xFF}

// PNG magic bytes: 89 50 4E 47 0D 0A 1A 0A
var pngMagic = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}

// VideoWorker processes video frame messages from vision2constellation agents
type VideoWorker struct {
	*BaseWorker
	registry     *EntityRegistry
	frameCounter uint64 // Atomic counter for generating sequence numbers
}

// NewVideoWorker creates a new video frame worker
func NewVideoWorker(nc *nats.Conn, js nats.JetStreamContext, registry *EntityRegistry) *VideoWorker {
	return &VideoWorker{
		BaseWorker: NewBaseWorker(
			"VideoWorker",
			nc,
			js,
			shared.StreamVideoFrames,
			shared.ConsumerVideoProcessor,
			shared.SubjectVideoAll,
		),
		registry: registry,
	}
}

func (w *VideoWorker) Start(ctx context.Context) error {
	logger.Infow("Starting video frame processing", "worker", w.Name())
	return w.processMessages(ctx, w.handleVideoFrame)
}

// handleVideoFrame processes a single video frame message
func (w *VideoWorker) handleVideoFrame(msg *nats.Msg) {
	// Parse subject: constellation.video.{entity_id}
	entityID, err := w.parseSubject(msg.Subject)
	if err != nil {
		logger.Errorw("Failed to parse video subject", "worker", w.Name(), "subject", msg.Subject, "error", err)
		return
	}

	var frame shared.VideoFrame

	// Detect message format: raw image bytes or JSON-wrapped VideoFrame
	if w.isRawImage(msg.Data) {
		// Raw image bytes (JPEG/PNG) - wrap in VideoFrame structure
		frame = w.wrapRawImage(entityID, msg.Data)
	} else {
		// Try JSON unmarshal
		if err := json.Unmarshal(msg.Data, &frame); err != nil {
			// Log first few bytes to help debug
			preview := msg.Data
			if len(preview) > 20 {
				preview = preview[:20]
			}
			logger.Warnw("Failed to unmarshal video frame (not JSON or raw image)",
				"worker", w.Name(),
				"error", err,
				"data_length", len(msg.Data),
				"first_bytes", fmt.Sprintf("%x", preview))
			return
		}
	}

	// Validate entity_id in message matches subject
	if frame.EntityID != "" && frame.EntityID != entityID {
		logger.Warnw("Entity ID mismatch between subject and message",
			"worker", w.Name(),
			"subject_entity_id", entityID,
			"message_entity_id", frame.EntityID)
	}
	frame.EntityID = entityID

	// Check if entity is registered (optional - may want to accept unregistered for flexibility)
	if w.registry != nil && !w.registry.IsRegistered(entityID) {
		logger.Debugw("Received video frame for unregistered entity",
			"worker", w.Name(),
			"entity_id", entityID)
		// Continue processing - video may come before entity registration
	}

	// Log frame metadata (not the actual frame data to avoid log spam)
	logger.Debugw("Received video frame",
		"worker", w.Name(),
		"entity_id", entityID,
		"frame_id", frame.FrameID,
		"sequence", frame.SequenceNum,
		"size_bytes", len(frame.Data),
		"format", frame.Format,
		"dimensions", fmt.Sprintf("%dx%d", frame.Width, frame.Height),
		"detections", len(frame.Detections),
	)

	// TODO: Future enhancements:
	// 1. Broadcast to WebSocket clients for real-time viewing
	// 2. Update entity state with latest frame metadata (not full frame)
	// 3. Forward to analytics pipeline if needed
	// 4. Implement frame sampling for high-frequency streams
}

// isRawImage checks if the data starts with known image magic bytes
func (w *VideoWorker) isRawImage(data []byte) bool {
	if len(data) < 8 {
		return false
	}
	return bytes.HasPrefix(data, jpegMagic) || bytes.HasPrefix(data, pngMagic)
}

// wrapRawImage wraps raw image bytes in a VideoFrame structure
func (w *VideoWorker) wrapRawImage(entityID string, data []byte) shared.VideoFrame {
	format := "unknown"
	if bytes.HasPrefix(data, jpegMagic) {
		format = "jpeg"
	} else if bytes.HasPrefix(data, pngMagic) {
		format = "png"
	}

	seq := atomic.AddUint64(&w.frameCounter, 1)

	return shared.VideoFrame{
		EntityID:    entityID,
		FrameID:     fmt.Sprintf("%s-%d", entityID, seq),
		Timestamp:   time.Now(),
		SequenceNum: seq,
		Format:      format,
		Encoding:    "binary",
		Data:        data,
		Priority:    1, // Normal priority
	}
}

// parseSubject extracts entity_id from NATS subject
func (w *VideoWorker) parseSubject(subject string) (entityID string, err error) {
	// Subject format: constellation.video.{entity_id}
	parts := strings.Split(subject, ".")

	// Must have at least constellation.video.entity_id (3 parts)
	if len(parts) < 3 {
		return "", fmt.Errorf("invalid subject format (too few parts): %s", subject)
	}

	// The entity_id is the last part
	entityID = parts[len(parts)-1]

	if entityID == "" {
		return "", fmt.Errorf("entity_id is empty in subject: %s", subject)
	}

	return entityID, nil
}
