package web

import (
	"constellation-overwatch/pkg/services/web/datastar"
	"constellation-overwatch/pkg/services/web/templates"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/nats-io/nats.go"
)

// SSEHandler handles Server-Sent Events streaming
type SSEHandler struct {
	nc *nats.Conn
	js nats.JetStreamContext
}

// NewSSEHandler creates a new SSE handler
func NewSSEHandler(nc *nats.Conn, js nats.JetStreamContext) *SSEHandler {
	return &SSEHandler{
		nc: nc,
		js: js,
	}
}

// StreamMessages streams NATS messages to the client via SSE
func (h *SSEHandler) StreamMessages(w http.ResponseWriter, r *http.Request) {
	// Create SSE generator
	sse := datastar.NewServerSentEventGenerator(w, r)

	// Subscribe to all constellation subjects
	sub, err := h.nc.Subscribe("constellation.>", func(msg *nats.Msg) {
		// Parse the message data
		var data interface{}
		if err := json.Unmarshal(msg.Data, &data); err != nil {
			log.Printf("[SSE] Error unmarshaling message: %v", err)
			return
		}

		// Format the data as pretty JSON
		prettyJSON, err := json.MarshalIndent(data, "", "  ")
		if err != nil {
			prettyJSON = msg.Data
		}

		// Create a stream message element
		timestamp := time.Now().Format("15:04:05")
		messageHTML := renderStreamMessage(msg.Subject, timestamp, string(prettyJSON))

		// Patch the element into the stream
		err = sse.PatchElements(messageHTML,
			datastar.WithSelector("#stream-messages"),
			datastar.WithMode(datastar.ElementPatchModePrepend))
		if err != nil {
			log.Printf("[SSE] Error patching elements: %v", err)
			return
		}
	})

	if err != nil {
		log.Printf("[SSE] Error subscribing to NATS: %v", err)
		http.Error(w, "Failed to subscribe to stream", http.StatusInternalServerError)
		return
	}
	defer sub.Unsubscribe()

	log.Printf("[SSE] Client connected from %s", r.RemoteAddr)

	// Send initial connection message
	initialHTML := `<div class="empty-state">Connected to stream. Waiting for messages...</div>`
	sse.PatchElements(initialHTML,
		datastar.WithSelector("#stream-messages"),
		datastar.WithMode(datastar.ElementPatchModeInner))

	// Keep the connection alive and send heartbeats
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			log.Printf("[SSE] Client disconnected from %s", r.RemoteAddr)
			return
		case <-ticker.C:
			// Send a comment as heartbeat to keep connection alive
			fmt.Fprintf(w, ": heartbeat\n\n")
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		}
	}
}

// renderStreamMessage renders a stream message HTML
func renderStreamMessage(subject, timestamp, data string) string {
	// Use the templ component to render
	// For now, we'll do it manually - later we can use templ properly
	return fmt.Sprintf(`
		<div class="stream-message" data-subject="%s">
			<div class="msg-header">
				<span class="msg-subject">%s</span>
				<span class="msg-time">%s</span>
			</div>
			<div class="msg-body">
				<div class="msg-data"><pre>%s</pre></div>
			</div>
		</div>
	`, subject, subject, timestamp, data)
}

// StreamMessagesWithFilter streams filtered NATS messages
func (h *SSEHandler) StreamMessagesWithFilter(w http.ResponseWriter, r *http.Request) {
	// Get filter from query params
	filter := r.URL.Query().Get("filter")
	if filter == "" {
		filter = "all"
	}

	// Create SSE generator
	sse := datastar.NewServerSentEventGenerator(w, r)

	// Determine which subjects to subscribe to based on filter
	var subjects []string
	switch filter {
	case "entities":
		subjects = []string{"constellation.entities.>"}
	case "events":
		subjects = []string{"constellation.events.>"}
	case "telemetry":
		subjects = []string{"constellation.telemetry.>"}
	case "commands":
		subjects = []string{"constellation.commands.>"}
	default:
		subjects = []string{"constellation.>"}
	}

	// Subscribe to the filtered subjects
	var subs []*nats.Subscription
	for _, subj := range subjects {
		sub, err := h.nc.Subscribe(subj, func(msg *nats.Msg) {
			// Parse the message data
			var data interface{}
			if err := json.Unmarshal(msg.Data, &data); err != nil {
				log.Printf("[SSE] Error unmarshaling message: %v", err)
				return
			}

			// Format the data as pretty JSON
			prettyJSON, err := json.MarshalIndent(data, "", "  ")
			if err != nil {
				prettyJSON = msg.Data
			}

			// Determine message type for styling
			msgType := getMessageType(msg.Subject)

			// Create a stream message element with type
			timestamp := time.Now().Format("15:04:05")
			messageHTML := renderStreamMessageWithType(msg.Subject, timestamp, msgType, string(prettyJSON))

			// Patch the element into the stream
			err = sse.PatchElements(messageHTML,
				datastar.WithSelector("#stream-messages"),
				datastar.WithMode(datastar.ElementPatchModePrepend))
			if err != nil {
				log.Printf("[SSE] Error patching elements: %v", err)
				return
			}
		})

		if err != nil {
			log.Printf("[SSE] Error subscribing to NATS: %v", err)
			http.Error(w, "Failed to subscribe to stream", http.StatusInternalServerError)
			return
		}
		subs = append(subs, sub)
	}

	// Cleanup subscriptions on disconnect
	defer func() {
		for _, sub := range subs {
			sub.Unsubscribe()
		}
	}()

	log.Printf("[SSE] Client connected from %s with filter: %s", r.RemoteAddr, filter)

	// Send initial connection message
	initialHTML := fmt.Sprintf(`<div class="empty-state">Connected to stream (filter: %s). Waiting for messages...</div>`, filter)
	sse.PatchElements(initialHTML,
		datastar.WithSelector("#stream-messages"),
		datastar.WithMode(datastar.ElementPatchModeInner))

	// Keep the connection alive
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			log.Printf("[SSE] Client disconnected from %s", r.RemoteAddr)
			return
		case <-ticker.C:
			// Heartbeat
			fmt.Fprintf(w, ": heartbeat\n\n")
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		}
	}
}

// getMessageType extracts message type from subject
func getMessageType(subject string) string {
	// Parse subject to determine type
	// constellation.entities.{org_id}.created -> "Entity Created"
	// constellation.telemetry.{org_id}.{entity_id} -> "Telemetry"

	if len(subject) > 21 && subject[:21] == "constellation.entities" {
		if len(subject) > 22 {
			parts := subject[22:]
			if len(parts) > 0 {
				// Find the last part after org_id
				lastDot := -1
				for i := len(parts) - 1; i >= 0; i-- {
					if parts[i] == '.' {
						lastDot = i
						break
					}
				}
				if lastDot > 0 {
					action := parts[lastDot+1:]
					return "Entity " + action
				}
			}
		}
		return "Entity Event"
	} else if len(subject) > 22 && subject[:22] == "constellation.telemetry" {
		return "Telemetry"
	} else if len(subject) > 21 && subject[:21] == "constellation.commands" {
		return "Command"
	} else if len(subject) > 19 && subject[:19] == "constellation.events" {
		return "Event"
	}

	return "Message"
}

// renderStreamMessageWithType renders a stream message with type
func renderStreamMessageWithType(subject, timestamp, msgType, data string) string {
	// Render with the templates.StreamMessage component
	// For simplicity, we'll render manually here
	return fmt.Sprintf(`
		<div class="stream-message" data-subject="%s">
			<div class="msg-header">
				<span class="msg-subject">%s</span>
				<span class="msg-time">%s</span>
			</div>
			<div class="msg-type">%s</div>
			<div class="msg-body">
				<div class="msg-data"><pre>%s</pre></div>
			</div>
		</div>
	`, subject, subject, timestamp, msgType, data)
}

// Ensure template is used (even if not actively called)
var _ = templates.StreamMessage
