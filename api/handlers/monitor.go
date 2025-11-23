package handlers

import (
	"net/http"
	"runtime"
	"time"

	"github.com/starfederation/datastar-go/datastar"
)

type MonitorHandler struct{}

func NewMonitorHandler() *MonitorHandler {
	return &MonitorHandler{}
}

func (h *MonitorHandler) SSE(w http.ResponseWriter, r *http.Request) {
	// Multiple metric tickers
	memTicker := time.NewTicker(time.Second)
	cpuTicker := time.NewTicker(time.Second)
	// diskTicker := time.NewTicker(5 * time.Second)
	// netTicker := time.NewTicker(2 * time.Second)

	defer func() {
		memTicker.Stop()
		cpuTicker.Stop()
		// diskTicker.Stop()
		// netTicker.Stop()
	}()

	sse := datastar.NewSSE(w, r)

	for {
		select {
		case <-r.Context().Done():
			return
		case <-memTicker.C:
			// Collect & send memory metrics
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			sse.MarshalAndPatchSignals(map[string]interface{}{
				"memTotal":       m.Sys,
				"memAlloc":       m.Alloc,
				"memHeapAlloc":   m.HeapAlloc,
				"memHeapSys":     m.HeapSys,
				"memStackInUse":  m.StackInuse,
				"memStackSys":    m.StackSys,
				"memMSpanInUse":  m.MSpanInuse,
				"memMSpanSys":    m.MSpanSys,
				"memMCacheInUse": m.MCacheInuse,
				"memMCacheSys":   m.MCacheSys,
				"memBuckHashSys": m.BuckHashSys,
				"memGCSys":       m.GCSys,
				"memOtherSys":    m.OtherSys,
				"memNumGC":       m.NumGC,
			})
		case <-cpuTicker.C:
			// Collect & send CPU metrics (placeholder for now as runtime doesn't provide CPU usage directly)
			sse.MarshalAndPatchSignals(map[string]interface{}{
				"numGoroutine": runtime.NumGoroutine(),
				"numCPU":       runtime.NumCPU(),
			})
		}
	}
}
