package datasource

import (
	"runtime"

	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/metrics"
	dto "github.com/prometheus/client_model/go"
)

// PrometheusAdapter collects metrics from Prometheus registry for TUI display
type PrometheusAdapter struct{}

// NewPrometheusAdapter creates a new Prometheus adapter
func NewPrometheusAdapter() *PrometheusAdapter {
	return &PrometheusAdapter{}
}

// Collect gathers metrics from the Prometheus registry
func (a *PrometheusAdapter) Collect() MetricsSnapshot {
	mfs, err := metrics.Registry.Gather()
	if err != nil {
		// Fallback to direct runtime collection
		return a.collectDirect()
	}

	snapshot := MetricsSnapshot{
		NumCPU: runtime.NumCPU(),
	}

	for _, mf := range mfs {
		switch mf.GetName() {
		case "go_memstats_sys_bytes":
			snapshot.MemTotal = uint64(getGaugeValue(mf))
		case "go_memstats_alloc_bytes":
			snapshot.MemAlloc = uint64(getGaugeValue(mf))
		case "go_memstats_heap_alloc_bytes":
			snapshot.HeapAlloc = uint64(getGaugeValue(mf))
		case "go_goroutines":
			snapshot.NumGoroutines = int(getGaugeValue(mf))
		case "go_gc_cycles_total_gc_cycles_total":
			snapshot.NumGC = uint32(getCounterValue(mf))
		}
	}

	// Fallback for NumGC if not found in Prometheus metrics
	if snapshot.NumGC == 0 {
		var stats runtime.MemStats
		runtime.ReadMemStats(&stats)
		snapshot.NumGC = stats.NumGC
	}

	return snapshot
}

// collectDirect falls back to direct runtime metrics collection
func (a *PrometheusAdapter) collectDirect() MetricsSnapshot {
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

// getGaugeValue extracts gauge value from MetricFamily
func getGaugeValue(mf *dto.MetricFamily) float64 {
	if len(mf.GetMetric()) > 0 && mf.GetMetric()[0].GetGauge() != nil {
		return mf.GetMetric()[0].GetGauge().GetValue()
	}
	return 0
}

// getCounterValue extracts counter value from MetricFamily
func getCounterValue(mf *dto.MetricFamily) float64 {
	if len(mf.GetMetric()) > 0 && mf.GetMetric()[0].GetCounter() != nil {
		return mf.GetMetric()[0].GetCounter().GetValue()
	}
	return 0
}
