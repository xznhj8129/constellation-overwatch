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
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/logger"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/mediamtx"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/datastar"
	video_pages "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/features/video/pages"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/shared"
)

type VideoHandler struct {
	mtxClient *mediamtx.Client
	entitySvc *services.EntityService
}

func NewVideoHandler(mtxClient *mediamtx.Client, entitySvc *services.EntityService) *VideoHandler {
	return &VideoHandler{
		mtxClient: mtxClient,
		entitySvc: entitySvc,
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

	// Reset the grid to empty state to prevent duplicates on reconnection
	emptyState := `<div class="empty-state" style="color: #888; padding: 40px; text-align: center; grid-column: 1 / -1;">
						<div style="font-size: 48px; margin-bottom: 10px;">📹</div>
						<p>No active video streams detected.</p>
						<p style="font-size: 12px; margin-top: 10px;">Waiting for MediaMTX streams...</p>
					</div>`
	sse.PatchElements(emptyState,
		datastar.WithSelector("#video-grid"),
		datastar.WithMode(datastar.ElementPatchModeInner))

	flusher.Flush()

	// Track known streams for add/remove detection
	knownStreams := make(map[string]bool)
	var streamsMutex sync.Mutex

	ctx := r.Context()

	// Ticker for polling MediaMTX
	ticker := time.NewTicker(2 * time.Second)
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
			// If MediaMTX client is nil (disabled), skip
			if h.mtxClient == nil {
				continue
			}

			streams := h.mtxClient.GetAllStreams()

			streamsMutex.Lock()

			// Build current active set
			currentActive := make(map[string]bool)
			var activeIDs []string

			for _, status := range streams {
				if !status.Ready {
					continue
				}
				entityID := status.EntityID
				currentActive[entityID] = true
				activeIDs = append(activeIDs, entityID)

				// New stream: render and append card
				if !knownStreams[entityID] {
					entityState := shared.EntityState{
						EntityID: entityID,
						Name:     entityID,
					}

					var cardHTML strings.Builder
					if err := video_pages.VideoCard(entityState, status).Render(context.Background(), &cardHTML); err == nil {
						if len(knownStreams) == 0 {
							sse.PatchElements("", datastar.WithSelector("#video-grid .empty-state"), datastar.WithMode(datastar.ElementPatchModeRemove))
						}

						sse.PatchElements(cardHTML.String(),
							datastar.WithSelector("#video-grid"),
							datastar.WithMode(datastar.ElementPatchModeAppend))

						knownStreams[entityID] = true
					}
				}

				// Update status badge for existing streams
				var badgeHTML strings.Builder
				if err := video_pages.VideoStatusBadge(entityID, true, status.ReaderCount).Render(context.Background(), &badgeHTML); err == nil {
					sse.PatchElements(badgeHTML.String(),
						datastar.WithSelector(fmt.Sprintf("#video-status-%s", entityID)),
						datastar.WithMode(datastar.ElementPatchModeMorph))
				}
			}

			// Remove stale streams
			for entityID := range knownStreams {
				if !currentActive[entityID] {
					sse.PatchElements("",
						datastar.WithSelector(fmt.Sprintf("#video-card-%s", entityID)),
						datastar.WithMode(datastar.ElementPatchModeRemove))
					delete(knownStreams, entityID)
				}
			}

			streamsMutex.Unlock()

			// Update signals
			sse.PatchSignals(map[string]interface{}{
				"activeStreams": activeIDs,
				"streamCount":   len(activeIDs),
				"lastUpdate":    time.Now().Format("15:04:05"),
			})
			flusher.Flush()
		}
	}
}

// HandleAPIVideoStatus returns JSON status of all MediaMTX streams
func (h *VideoHandler) HandleAPIVideoStatus(w http.ResponseWriter, r *http.Request) {
	if h.mtxClient == nil {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"streams":[]}`))
		return
	}

	streams := h.mtxClient.GetAllStreams()
	w.Header().Set("Content-Type", "application/json")
	resp := map[string]interface{}{
		"streams": streams,
	}
	data, err := json.Marshal(resp)
	if err != nil {
		logger.Errorw("Failed to marshal video status", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	w.Write(data)
}
