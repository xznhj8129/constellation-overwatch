package tui

import (
	"time"

	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/tui/datasource"
)

// TickMsg is sent on each refresh tick
type TickMsg time.Time

// RefreshMsg signals a manual refresh request
type RefreshMsg struct{}

// MetricsUpdateMsg contains updated system metrics
type MetricsUpdateMsg = datasource.MetricsSnapshot

// LogEntryMsg contains a new log entry
type LogEntryMsg struct {
	Time    time.Time
	Level   string
	Message string
	Fields  map[string]interface{}
}

// WorkersUpdateMsg contains updated worker statuses
type WorkersUpdateMsg struct {
	Workers []datasource.WorkerStatus
}

// EntitiesUpdateMsg contains updated entity data
type EntitiesUpdateMsg struct {
	Entities []datasource.EntitySummary
}

// StreamsUpdateMsg contains updated NATS stream stats
type StreamsUpdateMsg struct {
	Streams []datasource.StreamStat
}

// WindowSizeMsg is sent when the terminal window is resized
type WindowSizeMsg struct {
	Width  int
	Height int
}

// ErrorMsg represents an error that occurred during data fetching
type ErrorMsg struct {
	Error error
}

// QuitMsg signals the app should quit
type QuitMsg struct{}

// DataSourcesReadyMsg signals that data sources are now available
type DataSourcesReadyMsg struct {
	WorkerManager interface{} // *workers.Manager - use interface to avoid import cycle
	JetStream     interface{} // nats.JetStreamContext
	KeyValue      interface{} // nats.KeyValue
}
