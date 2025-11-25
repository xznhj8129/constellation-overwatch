package handlers

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	embeddednats "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/embedded-nats"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/logger"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/shared"

	"github.com/nats-io/nats.go"
)

// Image magic bytes for validation
var (
	jpegMagic = []byte{0xFF, 0xD8, 0xFF}
	pngMagic  = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
)

// VideoHandler handles video streaming API endpoints
type VideoHandler struct {
	natsEmbedded *embeddednats.EmbeddedNATS
}

// NewVideoHandler creates a new video handler
func NewVideoHandler(natsEmbedded *embeddednats.EmbeddedNATS) *VideoHandler {
	return &VideoHandler{
		natsEmbedded: natsEmbedded,
	}
}

// Stream serves an MJPEG stream for a specific entity
// GET /api/v1/video/stream/{entity_id}
// This subscribes to constellation.video.{entity_id} and streams frames as multipart/x-mixed-replace
func (h *VideoHandler) Stream(w http.ResponseWriter, r *http.Request) {
	// Extract entity_id from URL path
	path := r.URL.Path
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		http.Error(w, "Missing entity_id in path", http.StatusBadRequest)
		return
	}
	entityID := parts[len(parts)-1]

	if entityID == "" {
		http.Error(w, "entity_id required", http.StatusBadRequest)
		return
	}

	// Verify we have a flusher
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Set MJPEG headers
	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=frame")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	logger.Infow("MJPEG stream started", "component", "VideoHandler", "entity_id", entityID, "remote_addr", r.RemoteAddr)

	nc := h.natsEmbedded.Connection()
	if nc == nil {
		http.Error(w, "NATS not connected", http.StatusInternalServerError)
		return
	}

	// Subscribe to video frames for this entity
	subject := shared.VideoFrameSubject(entityID)

	// Channel to receive frames - buffer for burst tolerance
	frameChan := make(chan []byte, 15)
	var writeMutex sync.Mutex

	sub, err := nc.Subscribe(subject, func(msg *nats.Msg) {
		// Validate raw image frames (JPEG or PNG)
		// Publishers MUST send raw image bytes directly - no JSON wrapping
		if len(msg.Data) < 8 {
			logger.Debugw("Frame too small", "entity_id", entityID, "len", len(msg.Data))
			return
		}

		isJPEG := bytes.HasPrefix(msg.Data, jpegMagic)
		isPNG := bytes.HasPrefix(msg.Data, pngMagic)

		if !isJPEG && !isPNG {
			// Log first bytes to help debug publisher issues
			preview := msg.Data
			if len(preview) > 16 {
				preview = preview[:16]
			}
			logger.Debugw("Invalid frame format - expected raw JPEG/PNG",
				"entity_id", entityID,
				"len", len(msg.Data),
				"header", fmt.Sprintf("%X", preview))
			return
		}

		select {
		case frameChan <- msg.Data:
		default:
			// Drop frame if buffer is full (prevents backpressure)
			logger.Debugw("Dropping video frame (buffer full)", "entity_id", entityID)
		}
	})

	if err != nil {
		logger.Errorw("Failed to subscribe to video stream", "component", "VideoHandler", "error", err)
		http.Error(w, "Failed to subscribe to video stream", http.StatusInternalServerError)
		return
	}
	defer sub.Unsubscribe()

	ctx := r.Context()

	// Frame timeout - send placeholder if no frames received
	lastFrame := time.Now()
	frameTimeout := 5 * time.Second
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Infow("MJPEG stream ended", "component", "VideoHandler", "entity_id", entityID, "remote_addr", r.RemoteAddr)
			return

		case frame := <-frameChan:
			lastFrame = time.Now()
			writeMutex.Lock()
			// Detect content type from magic bytes
			contentType := "image/jpeg"
			if bytes.HasPrefix(frame, pngMagic) {
				contentType = "image/png"
			}
			// Write multipart frame boundary and headers
			fmt.Fprintf(w, "--frame\r\n")
			fmt.Fprintf(w, "Content-Type: %s\r\n", contentType)
			fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(frame))
			w.Write(frame)
			fmt.Fprintf(w, "\r\n")
			flusher.Flush()
			writeMutex.Unlock()

		case <-ticker.C:
			// Check for frame timeout
			if time.Since(lastFrame) > frameTimeout {
				// Could send a "no signal" placeholder image here
				logger.Debugw("No frames received", "component", "VideoHandler", "entity_id", entityID, "timeout", frameTimeout)
			}
		}
	}
}

// List returns list of entities with active video streams via SSE
// GET /api/v1/video/list
func (h *VideoHandler) List(w http.ResponseWriter, r *http.Request) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Track active video streams
	activeStreams := make(map[string]time.Time)
	var streamsMutex sync.Mutex

	nc := h.natsEmbedded.Connection()
	if nc == nil {
		http.Error(w, "NATS not connected", http.StatusInternalServerError)
		return
	}

	// Subscribe to all video subjects to detect active streams
	sub, err := nc.Subscribe(shared.SubjectVideoAll, func(msg *nats.Msg) {
		// Extract entity_id from subject
		parts := strings.Split(msg.Subject, ".")
		if len(parts) < 3 {
			return
		}
		entityID := parts[len(parts)-1]

		streamsMutex.Lock()
		activeStreams[entityID] = time.Now()
		streamsMutex.Unlock()
	})

	if err != nil {
		logger.Errorw("Failed to subscribe to video subjects", "component", "VideoHandler", "error", err)
		http.Error(w, "Failed to subscribe", http.StatusInternalServerError)
		return
	}
	defer sub.Unsubscribe()

	ctx := r.Context()

	// Send initial connection signal
	fmt.Fprintf(w, "event: connected\ndata: {\"connected\": true}\n\n")
	flusher.Flush()

	// Periodically send active streams list
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-heartbeat.C:
			fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()

		case <-ticker.C:
			// Build list of active streams (seen in last 5 seconds)
			streamsMutex.Lock()
			var active []string
			cutoff := time.Now().Add(-5 * time.Second)
			for entityID, lastSeen := range activeStreams {
				if lastSeen.After(cutoff) {
					active = append(active, entityID)
				} else {
					delete(activeStreams, entityID)
				}
			}
			streamsMutex.Unlock()

			// Send SSE event with active streams
			fmt.Fprintf(w, "event: streams\ndata: {\"streams\": [")
			for i, id := range active {
				if i > 0 {
					fmt.Fprintf(w, ",")
				}
				fmt.Fprintf(w, "\"%s\"", id)
			}
			fmt.Fprintf(w, "], \"count\": %d, \"timestamp\": \"%s\"}\n\n", len(active), time.Now().Format("15:04:05"))
			flusher.Flush()
		}
	}
}
