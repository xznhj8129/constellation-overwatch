package datasource

import (
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/logger"
)

// LogSource wraps the logger TUI hook for the TUI
type LogSource struct {
	hook *logger.TUIHook
}

// NewLogSource creates a new log source from the logger hook
func NewLogSource(hook *logger.TUIHook) *LogSource {
	return &LogSource{
		hook: hook,
	}
}

// Channel returns the log entry channel
func (l *LogSource) Channel() <-chan logger.LogEntry {
	return l.hook.Channel()
}
