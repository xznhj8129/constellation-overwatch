package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	embeddednats "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/embedded-nats"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/logger"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/datastar"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/templates"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/shared"
	"github.com/nats-io/nats.go"
)

type VideoHandler struct {
	natsEmbedded *embeddednats.EmbeddedNATS
}

func NewVideoHandler(natsEmbedded *embeddednats.EmbeddedNATS) *VideoHandler {
	return &VideoHandler{
		natsEmbedded: natsEmbedded,
	}
}

// HandleAPIVideoList handles the Datastar SSE stream for the video list
func (h *VideoHandler) HandleAPIVideoList(w http.ResponseWriter, r *http.Request) {
	// Verify we have a flusher
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	sse := datastar.NewServerSentEventGenerator(w, r)

	// Send initial connection signal
	fmt.Fprintf(w, ": SSE connection established\n\n")
	sse.PatchSignals(map[string]interface{}{
		"_isConnected": true,
	})
	flusher.Flush()

	// Track active streams
	activeStreams := make(map[string]time.Time)
	knownStreams := make(map[string]bool)
	var streamsMutex sync.Mutex

	nc := h.natsEmbedded.Connection()
	if nc == nil {
		logger.Errorw("NATS not connected", "component", "VideoHandler")
		return
	}

	// Subscribe to all video subjects
	sub, err := nc.Subscribe(shared.SubjectVideoAll, func(msg *nats.Msg) {
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
		return
	}
	defer sub.Unsubscribe()

	ctx := r.Context()

	// Ticker for updates
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
			streamsMutex.Lock()
			cutoff := time.Now().Add(-5 * time.Second)

			// Check for new streams and updates
			var currentActive []string

			for entityID, lastSeen := range activeStreams {
				if lastSeen.After(cutoff) {
					currentActive = append(currentActive, entityID)

					// Only create card if not already known
					if !knownStreams[entityID] {
						// Fetch entity details from KV
						var entityName string
						kv := h.natsEmbedded.KeyValue()
						if kv != nil {
							// Try to find entity name in KV
							keys, _ := kv.Keys()
							for _, key := range keys {
								if strings.HasPrefix(key, entityID+".") {
									entry, _ := kv.Get(key)
									var data map[string]interface{}
									if err := json.Unmarshal(entry.Value(), &data); err == nil {
										if name, ok := data["name"].(string); ok && name != "" {
											entityName = name
											break
										}
									}
								}
							}
						}

						var cardHTML strings.Builder
						// Create a minimal EntityState for the card
						entityState := shared.EntityState{
							EntityID: entityID,
							Name:     entityName,
						}

						if err := templates.VideoCard(entityState).Render(context.Background(), &cardHTML); err == nil {
							// Remove empty state if it's the first stream
							if len(knownStreams) == 0 {
								sse.PatchElements("", datastar.WithSelector(".empty-state"), datastar.WithMode(datastar.ElementPatchModeRemove))
							}

							sse.PatchElements(cardHTML.String(),
								datastar.WithSelector("#video-grid"),
								datastar.WithMode(datastar.ElementPatchModeAppend))

							knownStreams[entityID] = true
						}
					}
				} else {
					// Stream stale - remove card
					if knownStreams[entityID] {
						sse.PatchElements("",
							datastar.WithSelector(fmt.Sprintf("#video-card-%s", entityID)),
							datastar.WithMode(datastar.ElementPatchModeRemove))
						delete(knownStreams, entityID)
						delete(activeStreams, entityID)
					}
				}
			}
			streamsMutex.Unlock()

			// Update signals
			sse.PatchSignals(map[string]interface{}{
				"activeStreams": currentActive,
				"streamCount":   len(currentActive),
				"lastUpdate":    time.Now().Format("15:04:05"),
			})
			flusher.Flush()
		}
	}
}
