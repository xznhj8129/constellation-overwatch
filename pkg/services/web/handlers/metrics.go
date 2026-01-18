package handlers

import (
	"net/http"
	"runtime"
	"time"

	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/metrics"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/datastar"
	metrics_pages "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/features/metrics/pages"
)

// MetricsHandler handles metrics-related HTTP requests
type MetricsHandler struct{}

// NewMetricsHandler creates a new metrics handler
func NewMetricsHandler() *MetricsHandler {
	return &MetricsHandler{}
}

// MetricsSignals represents the signals sent to the frontend
type MetricsSignals struct {
	// Runtime metrics
	MemTotal      uint64 `json:"memTotal"`
	MemAlloc      uint64 `json:"memAlloc"`
	MemHeapAlloc  uint64 `json:"memHeapAlloc"`
	MemHeapSys    uint64 `json:"memHeapSys"`
	MemStackInUse uint64 `json:"memStackInUse"`
	NumGoroutines int    `json:"numGoroutines"`
	NumCPU        int    `json:"numCPU"`
	NumGC         uint32 `json:"numGC"`
	GCPauseNs     uint64 `json:"gcPauseNs"`

	// Custom metrics
	HTTPRequestsTotal float64 `json:"httpRequestsTotal"`

	// Metadata
	Timestamp string `json:"timestamp"`
}

// HandleSSE streams metrics via Server-Sent Events using Datastar
func (h *MetricsHandler) HandleSSE(w http.ResponseWriter, r *http.Request) {
	// CRITICAL: Set SSE headers BEFORE creating SSE generator or writing anything
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	sse := datastar.NewServerSentEventGenerator(w, r)

	// Send initial connection signal
	sse.PatchSignals(map[string]interface{}{
		"_isConnected": true,
	})

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			// Collect runtime metrics
			var m runtime.MemStats
			runtime.ReadMemStats(&m)

			// Build signals map
			signals := map[string]interface{}{
				// Runtime metrics
				"memTotal":      m.Sys,
				"memAlloc":      m.Alloc,
				"memHeapAlloc":  m.HeapAlloc,
				"memHeapSys":    m.HeapSys,
				"memStackInUse": m.StackInuse,
				"numGoroutines": runtime.NumGoroutine(),
				"numCPU":        runtime.NumCPU(),
				"numGC":         m.NumGC,
				"gcPauseNs":            m.PauseNs[(m.NumGC+255)%256],
				"httpRequestsTotal":    0,
				"httpRequestsInFlight": 0,
				"timestamp":            time.Now().Format("15:04:05"),
			}

			// Add custom metrics from Prometheus registry
			mfs, err := metrics.Gather()
			if err == nil {
				for _, mf := range mfs {
					name := mf.GetName()
					if name == "overwatch_http_requests_total" {
						// Sum all HTTP request counters
						var total float64
						for _, metric := range mf.GetMetric() {
							if metric.GetCounter() != nil {
								total += metric.GetCounter().GetValue()
							}
						}
						signals["httpRequestsTotal"] = total
					}
					if name == "overwatch_http_requests_in_flight" {
						if len(mf.GetMetric()) > 0 && mf.GetMetric()[0].GetGauge() != nil {
							signals["httpRequestsInFlight"] = mf.GetMetric()[0].GetGauge().GetValue()
						}
					}
				}
			}

			sse.PatchSignals(signals)
		}
	}
}

// HandleMetricsPage renders the metrics dashboard page
func (h *MetricsHandler) HandleMetricsPage(w http.ResponseWriter, r *http.Request) {
	metrics_pages.MetricsPage().Render(r.Context(), w)
}
