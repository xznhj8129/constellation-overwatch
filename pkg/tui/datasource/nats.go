package datasource

import (
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/shared"

	"github.com/nats-io/nats.go"
)

// NATSStats monitors NATS JetStream statistics
type NATSStats struct {
	js nats.JetStreamContext
}

// NewNATSStats creates a new NATS stats monitor
func NewNATSStats(js nats.JetStreamContext) *NATSStats {
	return &NATSStats{
		js: js,
	}
}

// GetStreamStats returns statistics for all constellation streams
func (n *NATSStats) GetStreamStats() []StreamStat {
	streams := []string{
		shared.StreamEntities,
		shared.StreamEvents,
		shared.StreamTelemetry,
		shared.StreamCommands,
	}

	stats := make([]StreamStat, 0, len(streams))

	for _, name := range streams {
		info, err := n.js.StreamInfo(name)
		if err != nil {
			// Stream might not exist yet
			stats = append(stats, StreamStat{
				Name:     name,
				Messages: 0,
				Bytes:    0,
			})
			continue
		}

		stats = append(stats, StreamStat{
			Name:     name,
			Messages: info.State.Msgs,
			Bytes:    info.State.Bytes,
		})
	}

	return stats
}
