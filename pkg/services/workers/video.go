package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/logger"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/shared"
	"github.com/nats-io/nats.go"
)

// VideoWorker processes video frame messages from vision2constellation agents
type VideoWorker struct {
	*BaseWorker
	registry *EntityRegistry
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

	// Parse video frame
	var frame shared.VideoFrame
	if err := json.Unmarshal(msg.Data, &frame); err != nil {
		logger.Errorw("Failed to unmarshal video frame", "worker", w.Name(), "error", err)
		return
	}

	// Validate entity_id in message matches subject
	if frame.EntityID != "" && frame.EntityID != entityID {
		logger.Warnw("Entity ID mismatch between subject and message",
			"worker", w.Name(),
			"subject_entity_id", entityID,
			"message_entity_id", frame.EntityID)
		// Use the subject's entity_id as authoritative
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
		"timestamp", frame.Timestamp.Format(time.RFC3339),
	)

	// TODO: Future enhancements:
	// 1. Broadcast to WebSocket clients for real-time viewing
	// 2. Update entity state with latest frame metadata (not full frame)
	// 3. Forward to analytics pipeline if needed
	// 4. Implement frame sampling for high-frequency streams
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
