package datasource

import (
	"runtime"
)

// RuntimeMetrics collects Go runtime metrics
type RuntimeMetrics struct{}

// NewRuntimeMetrics creates a new runtime metrics collector
func NewRuntimeMetrics() *RuntimeMetrics {
	return &RuntimeMetrics{}
}

// Collect gathers current runtime metrics
func (m *RuntimeMetrics) Collect() MetricsSnapshot {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)

	return MetricsSnapshot{
		MemTotal:      stats.Sys,
		MemAlloc:      stats.Alloc,
		HeapAlloc:     stats.HeapAlloc,
		NumGoroutines: runtime.NumGoroutine(),
		NumCPU:        runtime.NumCPU(),
		NumGC:         stats.NumGC,
	}
}
