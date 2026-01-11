package logger

import (
	"sync"
	"time"

	"go.uber.org/zap/zapcore"
)

// LogEntry represents a log entry for the TUI
type LogEntry struct {
	Time    time.Time
	Level   string
	Message string
	Fields  map[string]interface{}
}

// TUIHook captures log entries for the TUI display
type TUIHook struct {
	entries chan LogEntry
	mu      sync.Mutex
	closed  bool
}

// NewTUIHook creates a new TUI log hook with the specified buffer size
func NewTUIHook(bufferSize int) *TUIHook {
	return &TUIHook{
		entries: make(chan LogEntry, bufferSize),
	}
}

// Channel returns the read-only channel of log entries
func (h *TUIHook) Channel() <-chan LogEntry {
	return h.entries
}

// Write sends a log entry to the TUI (non-blocking)
func (h *TUIHook) Write(entry LogEntry) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return
	}

	select {
	case h.entries <- entry:
	default:
		// Drop if full (non-blocking)
	}
}

// Close closes the log entry channel
func (h *TUIHook) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.closed {
		h.closed = true
		close(h.entries)
	}
}

// tuiCore wraps a zapcore.Core to also send entries to the TUI hook
type tuiCore struct {
	zapcore.Core
	hook *TUIHook
}

// newTUICore creates a new core that tees output to both the original core and the TUI hook
func newTUICore(original zapcore.Core, hook *TUIHook) zapcore.Core {
	return &tuiCore{
		Core: original,
		hook: hook,
	}
}

// Write intercepts log writes to also send them to the TUI
func (c *tuiCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	// Send to TUI hook
	fieldsMap := make(map[string]interface{})
	for _, f := range fields {
		switch f.Type {
		case zapcore.StringType:
			fieldsMap[f.Key] = f.String
		case zapcore.Int64Type, zapcore.Int32Type, zapcore.Int16Type, zapcore.Int8Type:
			fieldsMap[f.Key] = f.Integer
		case zapcore.Float64Type, zapcore.Float32Type:
			fieldsMap[f.Key] = f.Integer
		case zapcore.BoolType:
			fieldsMap[f.Key] = f.Integer != 0
		default:
			if f.Interface != nil {
				fieldsMap[f.Key] = f.Interface
			}
		}
	}

	c.hook.Write(LogEntry{
		Time:    entry.Time,
		Level:   entry.Level.String(),
		Message: entry.Message,
		Fields:  fieldsMap,
	})

	// Write to original core
	return c.Core.Write(entry, fields)
}

// Check wraps the original Check
func (c *tuiCore) Check(entry zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(entry.Level) {
		return ce.AddCore(entry, c)
	}
	return ce
}

// With wraps the original With to maintain the TUI hook
func (c *tuiCore) With(fields []zapcore.Field) zapcore.Core {
	return &tuiCore{
		Core: c.Core.With(fields),
		hook: c.hook,
	}
}
