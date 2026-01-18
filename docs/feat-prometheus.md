# Feature: Prometheus Metrics & Enhanced Profiling

## Overview

Add Prometheus metrics collection, pprof profiling endpoints, and enhanced metrics streaming via Datastar SSE. This integrates deeply with both the TUI dashboard and the web UI.

## Architecture

```
pkg/
├── metrics/                          # NEW: Centralized metrics package
│   ├── registry.go                   # Custom Prometheus registry with Go runtime collectors
│   ├── collectors/
│   │   ├── runtime.go                # Enhanced runtime metrics (replaces datasource/metrics.go)
│   │   ├── nats.go                   # NATS JetStream stream collector
│   │   └── workers.go                # Worker health & processing metrics
│   ├── http.go                       # HTTP request middleware instrumentation
│   └── pprof.go                      # pprof endpoint registration
├── tui/
│   └── datasource/
│       └── prometheus.go             # NEW: Adapter from Prometheus registry → TUI
└── services/
    └── web/
        ├── handlers/
        │   └── metrics.go            # NEW: SSE metrics handler for web UI
        └── features/
            └── metrics/              # NEW: Metrics page templ components
                ├── page.templ
                └── components/
                    ├── gauges.templ
                    └── charts.templ
```

## Implementation Phases

### Phase 1: Core Metrics Registry

**Goal**: Centralized Prometheus registry with Go runtime metrics

**Files to Create**:
- `pkg/metrics/registry.go`
- `pkg/metrics/collectors/runtime.go`

**Implementation**:

```go
// pkg/metrics/registry.go
package metrics

import (
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/collectors"
)

var (
    // Global registry - initialized once
    Registry *prometheus.Registry
)

func init() {
    Registry = NewRegistry()
}

func NewRegistry() *prometheus.Registry {
    reg := prometheus.NewRegistry()

    // Go runtime collectors
    reg.MustRegister(collectors.NewGoCollector(
        collectors.WithGoCollectorRuntimeMetrics(
            collectors.MetricsGC,
            collectors.MetricsMemory,
            collectors.MetricsScheduler,
        ),
    ))
    reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

    return reg
}
```

**Testing Phase 1**:
```bash
# Build and run
task build && ./bin/overwatch

# Verify metrics endpoint works (after Phase 2)
curl -s http://localhost:8080/metrics | grep go_
```

---

### Phase 2: Metrics HTTP Endpoint

**Goal**: Expose `/metrics` for Prometheus scraping

**Files to Create**:
- `pkg/metrics/server.go`

**Files to Modify**:
- `pkg/services/web/router.go` - Add `/metrics` route
- `go.mod` - Add prometheus dependency

**Implementation**:

```go
// pkg/metrics/server.go
package metrics

import (
    "net/http"
    "github.com/prometheus/client_golang/prometheus/promhttp"
)

// Handler returns the Prometheus metrics handler
func Handler() http.Handler {
    return promhttp.HandlerFor(Registry, promhttp.HandlerOpts{
        EnableOpenMetrics: true,
    })
}
```

**Router Integration** (`pkg/services/web/router.go`):
```go
// Add after static files, before auth routes (no auth required for metrics scraping)
mux.Handle("/metrics", metrics.Handler())
```

**Testing Phase 2**:
```bash
task build && ./bin/overwatch &

# Test metrics endpoint
curl -s http://localhost:8080/metrics | head -30

# Expected output includes:
# go_goroutines
# go_memstats_alloc_bytes
# go_gc_duration_seconds
# process_cpu_seconds_total
```

---

### Phase 3: Application Metrics

**Goal**: Custom overwatch metrics for HTTP, NATS, and workers

**Files to Create**:
- `pkg/metrics/http.go`
- `pkg/metrics/collectors/nats.go`
- `pkg/metrics/collectors/workers.go`

**Metrics to Add**:

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `overwatch_http_requests_total` | Counter | method, path, status | Total HTTP requests |
| `overwatch_http_request_duration_seconds` | Histogram | method, path | Request latency |
| `overwatch_nats_stream_messages` | Gauge | stream | Messages per stream |
| `overwatch_nats_stream_bytes` | Gauge | stream | Bytes per stream |
| `overwatch_worker_healthy` | Gauge | worker | Worker health (1/0) |
| `overwatch_worker_messages_total` | Counter | worker, status | Messages processed |
| `overwatch_worker_processing_seconds` | Histogram | worker | Processing duration |
| `overwatch_entities_total` | Gauge | type, org_id | Entity count |
| `overwatch_entities_live` | Gauge | type, org_id | Live entities |

**HTTP Middleware** (`pkg/metrics/http.go`):
```go
package metrics

import (
    "net/http"
    "strconv"
    "time"

    "github.com/prometheus/client_golang/prometheus"
)

var (
    httpRequestsTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Namespace: "overwatch",
            Name:      "http_requests_total",
            Help:      "Total HTTP requests",
        },
        []string{"method", "path", "status"},
    )

    httpRequestDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Namespace: "overwatch",
            Name:      "http_request_duration_seconds",
            Help:      "HTTP request duration",
            Buckets:   []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5},
        },
        []string{"method", "path"},
    )
)

func init() {
    Registry.MustRegister(httpRequestsTotal, httpRequestDuration)
}

// Middleware wraps an http.Handler with metrics collection
func Middleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        start := time.Now()
        wrapped := &statusRecorder{ResponseWriter: w, status: 200}

        next.ServeHTTP(wrapped, r)

        duration := time.Since(start).Seconds()
        path := r.URL.Path // Use exact path for now

        httpRequestsTotal.WithLabelValues(r.Method, path, strconv.Itoa(wrapped.status)).Inc()
        httpRequestDuration.WithLabelValues(r.Method, path).Observe(duration)
    })
}

type statusRecorder struct {
    http.ResponseWriter
    status int
}

func (r *statusRecorder) WriteHeader(status int) {
    r.status = status
    r.ResponseWriter.WriteHeader(status)
}
```

**NATS Collector** (`pkg/metrics/collectors/nats.go`):
```go
package collectors

import (
    "github.com/nats-io/nats.go"
    "github.com/prometheus/client_golang/prometheus"
)

type NATSCollector struct {
    js        nats.JetStreamContext
    msgDesc   *prometheus.Desc
    bytesDesc *prometheus.Desc
}

func NewNATSCollector(js nats.JetStreamContext) *NATSCollector {
    return &NATSCollector{
        js: js,
        msgDesc: prometheus.NewDesc(
            "overwatch_nats_stream_messages",
            "Number of messages in NATS stream",
            []string{"stream"}, nil,
        ),
        bytesDesc: prometheus.NewDesc(
            "overwatch_nats_stream_bytes",
            "Size of NATS stream in bytes",
            []string{"stream"}, nil,
        ),
    }
}

func (c *NATSCollector) Describe(ch chan<- *prometheus.Desc) {
    ch <- c.msgDesc
    ch <- c.bytesDesc
}

func (c *NATSCollector) Collect(ch chan<- prometheus.Metric) {
    streams := []string{
        "CONSTELLATION_ENTITIES",
        "CONSTELLATION_EVENTS",
        "CONSTELLATION_TELEMETRY",
        "CONSTELLATION_COMMANDS",
        "CONSTELLATION_VIDEO_FRAMES",
    }

    for _, name := range streams {
        info, err := c.js.StreamInfo(name)
        if err != nil {
            continue
        }

        ch <- prometheus.MustNewConstMetric(
            c.msgDesc, prometheus.GaugeValue,
            float64(info.State.Msgs), name,
        )
        ch <- prometheus.MustNewConstMetric(
            c.bytesDesc, prometheus.GaugeValue,
            float64(info.State.Bytes), name,
        )
    }
}
```

**Testing Phase 3**:
```bash
task build && ./bin/overwatch &

# Generate traffic
for i in {1..10}; do curl -s http://localhost:8080/health > /dev/null; done
curl -s http://localhost:8080/api/v1/organizations -H "Authorization: Bearer reindustrialize-dev-token"

# Check custom metrics
curl -s http://localhost:8080/metrics | grep overwatch_

# Expected:
# overwatch_http_requests_total{method="GET",path="/health",status="200"} 10
# overwatch_nats_stream_messages{stream="CONSTELLATION_TELEMETRY"} 0
```

---

### Phase 4: pprof Endpoints

**Goal**: Add profiling endpoints for debugging

**Files to Create**:
- `pkg/metrics/pprof.go`

**Files to Modify**:
- `pkg/services/web/router.go` - Add debug routes

**Implementation** (`pkg/metrics/pprof.go`):
```go
package metrics

import (
    "net/http"
    "net/http/pprof"
)

// RegisterPProf registers pprof handlers on the given mux
func RegisterPProf(mux *http.ServeMux, protect func(http.HandlerFunc) http.Handler) {
    // Index
    mux.Handle("/debug/pprof/", protect(pprof.Index))
    mux.Handle("/debug/pprof/cmdline", protect(pprof.Cmdline))
    mux.Handle("/debug/pprof/profile", protect(pprof.Profile))
    mux.Handle("/debug/pprof/symbol", protect(pprof.Symbol))
    mux.Handle("/debug/pprof/trace", protect(pprof.Trace))

    // Individual profiles
    mux.Handle("/debug/pprof/goroutine", protect(pprof.Handler("goroutine").ServeHTTP))
    mux.Handle("/debug/pprof/heap", protect(pprof.Handler("heap").ServeHTTP))
    mux.Handle("/debug/pprof/allocs", protect(pprof.Handler("allocs").ServeHTTP))
    mux.Handle("/debug/pprof/block", protect(pprof.Handler("block").ServeHTTP))
    mux.Handle("/debug/pprof/mutex", protect(pprof.Handler("mutex").ServeHTTP))
    mux.Handle("/debug/pprof/threadcreate", protect(pprof.Handler("threadcreate").ServeHTTP))
}
```

**Testing Phase 4**:
```bash
task build && ./bin/overwatch &

# Test pprof index (requires session auth if WEB_UI_PASSWORD set)
curl http://localhost:8080/debug/pprof/

# Fetch heap profile
go tool pprof http://localhost:8080/debug/pprof/heap

# Fetch CPU profile (30 seconds)
go tool pprof http://localhost:8080/debug/pprof/profile?seconds=30

# View goroutine stacks
curl http://localhost:8080/debug/pprof/goroutine?debug=1
```

---

### Phase 5: TUI Prometheus Integration

**Goal**: TUI uses Prometheus registry for metrics display

**Files to Create**:
- `pkg/tui/datasource/prometheus.go`

**Files to Modify**:
- `pkg/tui/app.go` - Use PrometheusAdapter

**Implementation** (`pkg/tui/datasource/prometheus.go`):
```go
package datasource

import (
    "runtime"

    "github.com/Constellation-Overwatch/constellation-overwatch/pkg/metrics"
    dto "github.com/prometheus/client_model/go"
)

// PrometheusAdapter collects metrics from Prometheus registry for TUI display
type PrometheusAdapter struct{}

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

    // Fallback for NumGC if not found
    if snapshot.NumGC == 0 {
        var stats runtime.MemStats
        runtime.ReadMemStats(&stats)
        snapshot.NumGC = stats.NumGC
    }

    return snapshot
}

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

func getGaugeValue(mf *dto.MetricFamily) float64 {
    if len(mf.GetMetric()) > 0 && mf.GetMetric()[0].GetGauge() != nil {
        return mf.GetMetric()[0].GetGauge().GetValue()
    }
    return 0
}

func getCounterValue(mf *dto.MetricFamily) float64 {
    if len(mf.GetMetric()) > 0 && mf.GetMetric()[0].GetCounter() != nil {
        return mf.GetMetric()[0].GetCounter().GetValue()
    }
    return 0
}
```

**Testing Phase 5**:
```bash
# Run with TUI
task tui

# Verify system panel shows metrics
# Compare with /metrics endpoint values
curl -s http://localhost:8080/metrics | grep -E "(go_goroutines|go_memstats_alloc_bytes)"
```

---

### Phase 6: Web UI Metrics Page (Datastar SSE)

**Goal**: Real-time metrics page using Datastar signals (following existing patterns)

**Files to Create**:
- `pkg/services/web/handlers/metrics.go` - SSE handler
- `pkg/services/web/features/metrics/page.templ` - Page template
- `pkg/services/web/features/metrics/components/gauges.templ` - Gauge components

**Files to Modify**:
- `pkg/services/web/router.go` - Add routes
- `pkg/services/web/handlers/pages.go` - Add page handler

**SSE Handler** (`pkg/services/web/handlers/metrics.go`):
```go
package handlers

import (
    "net/http"
    "runtime"
    "time"

    "github.com/Constellation-Overwatch/constellation-overwatch/pkg/metrics"
    "github.com/starfederation/datastar-go/datastar"
)

type MetricsHandler struct{}

func NewMetricsHandler() *MetricsHandler {
    return &MetricsHandler{}
}

// SSE streams metrics via Datastar signals (follows monitor.go pattern)
func (h *MetricsHandler) SSE(w http.ResponseWriter, r *http.Request) {
    ticker := time.NewTicker(time.Second)
    defer ticker.Stop()

    sse := datastar.NewSSE(w, r)

    for {
        select {
        case <-r.Context().Done():
            return
        case <-ticker.C:
            // Collect runtime metrics
            var m runtime.MemStats
            runtime.ReadMemStats(&m)

            // Collect custom metrics from Prometheus registry
            mfs, _ := metrics.Registry.Gather()

            // Build signals map
            signals := map[string]interface{}{
                // Runtime metrics
                "memTotal":       m.Sys,
                "memAlloc":       m.Alloc,
                "memHeapAlloc":   m.HeapAlloc,
                "memHeapSys":     m.HeapSys,
                "memStackInUse":  m.StackInuse,
                "numGoroutines":  runtime.NumGoroutine(),
                "numCPU":         runtime.NumCPU(),
                "numGC":          m.NumGC,
                "gcPauseNs":      m.PauseNs[(m.NumGC+255)%256],
            }

            // Add custom metrics from registry
            for _, mf := range mfs {
                name := mf.GetName()
                if len(mf.GetMetric()) > 0 {
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
                }
            }

            sse.MarshalAndPatchSignals(signals)
        }
    }
}
```

**Router Integration**:
```go
// Protected metrics page and SSE
metricsHandler := handlers.NewMetricsHandler()
mux.Handle("/metrics-ui", protect(pageHandler.HandleMetricsPage))
mux.Handle("/api/metrics/sse", protect(metricsHandler.SSE))
```

**Testing Phase 6**:
```bash
task dev  # Start with templ watching

# Visit http://localhost:8080/metrics-ui in browser
# Verify real-time gauge updates via Datastar

# Test SSE endpoint directly
curl -N http://localhost:8080/api/metrics/sse
```

---

### Phase 7: Unit Tests

**Goal**: Test coverage for metrics package

**Files to Create**:
- `pkg/metrics/registry_test.go`
- `pkg/metrics/http_test.go`
- `pkg/metrics/collectors/nats_test.go`

**Test Patterns**:
```go
// pkg/metrics/registry_test.go
package metrics_test

import (
    "testing"

    "github.com/Constellation-Overwatch/constellation-overwatch/pkg/metrics"
)

func TestNewRegistry(t *testing.T) {
    reg := metrics.NewRegistry()

    mfs, err := reg.Gather()
    if err != nil {
        t.Fatalf("Failed to gather metrics: %v", err)
    }

    // Verify Go runtime metrics are present
    found := make(map[string]bool)
    for _, mf := range mfs {
        found[mf.GetName()] = true
    }

    required := []string{"go_goroutines", "go_memstats_alloc_bytes"}
    for _, name := range required {
        if !found[name] {
            t.Errorf("Expected metric %q not found", name)
        }
    }
}

func TestHTTPMiddleware(t *testing.T) {
    // Use httptest.NewRecorder
    // Verify counter increments
}
```

**Testing**:
```bash
go test ./pkg/metrics/... -v
```

---

## Taskfile Integration

Add to `Taskfile.yml`:

```yaml
  test:
    desc: Run all tests
    cmds:
      - go test ./... -v

  test-metrics:
    desc: Run metrics package tests
    cmds:
      - go test ./pkg/metrics/... -v -cover

  metrics-check:
    desc: Verify metrics endpoint
    cmds:
      - |
        ./bin/overwatch &
        PID=$!
        sleep 2
        curl -s http://localhost:8080/metrics | head -20
        kill $PID
```

---

## Dependencies

Add to `go.mod`:
```
github.com/prometheus/client_golang v1.19.0
github.com/prometheus/client_model v0.6.0
```

---

## Verification Checklist

### Phase Completion Criteria

- [ ] **Phase 1**: `go build ./...` passes
- [ ] **Phase 2**: `curl localhost:8080/metrics` returns go_* metrics
- [ ] **Phase 3**: `curl localhost:8080/metrics | grep overwatch_` shows custom metrics
- [ ] **Phase 4**: `go tool pprof http://localhost:8080/debug/pprof/heap` works
- [ ] **Phase 5**: TUI system panel shows metrics from Prometheus registry
- [ ] **Phase 6**: Web UI metrics page updates in real-time
- [ ] **Phase 7**: `go test ./pkg/metrics/...` passes

### Integration Tests

```bash
# Full integration test
task build
./bin/overwatch &
sleep 3

# Test all endpoints
curl -s http://localhost:8080/metrics | grep -c "^overwatch_"
curl -s http://localhost:8080/debug/pprof/ | grep -c "profile"
curl -s http://localhost:8080/health

# Cleanup
pkill overwatch
```

---

## Security Considerations

1. **Metrics endpoint**: No auth (standard for Prometheus scraping)
2. **pprof endpoints**: Protected by session auth (sensitive debugging info)
3. **Metrics SSE**: Protected by session auth (internal dashboard)
4. **Cardinality**: Limit label values to prevent metric explosion

---

## Future Enhancements

1. **Alertmanager integration** - Define alerting rules
2. **Grafana dashboards** - Pre-built dashboard JSON
3. **Continuous profiling** - Pyroscope/Parca integration
4. **Custom entity metrics** - Per-entity telemetry aggregation
