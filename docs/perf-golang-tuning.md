# Go Performance Tuning Guide

Comprehensive performance optimization guide for Constellation Overwatch. This document covers profiling techniques, memory optimization, concurrency patterns, and specific tuning recommendations for the codebase.

---

## Table of Contents

1. [Quick Start: Profiling](#quick-start-profiling)
2. [Profiling Tools](#profiling-tools)
3. [Memory Optimization](#memory-optimization)
4. [Concurrency Patterns](#concurrency-patterns)
5. [Codebase-Specific Optimizations](#codebase-specific-optimizations)
6. [NATS & JetStream Tuning](#nats--jetstream-tuning)
7. [SQLite Performance](#sqlite-performance)
8. [Benchmarking](#benchmarking)
9. [Production Monitoring](#production-monitoring)
10. [Go Version Considerations](#go-version-considerations)

---

## Quick Start: Profiling

### Enable pprof Endpoints

Add profiling endpoints to your development build:

```bash
# Run with profiling (requires feat-prometheus implementation)
go run ./cmd/microlith/main.go

# Access profiling endpoints
curl http://localhost:8080/debug/pprof/           # Index
curl http://localhost:8080/debug/pprof/heap       # Memory profile
curl http://localhost:8080/debug/pprof/goroutine  # Goroutine stacks
```

### Quick CPU Profile

```bash
# Collect 30-second CPU profile
go tool pprof http://localhost:8080/debug/pprof/profile?seconds=30

# Interactive commands in pprof:
(pprof) top 20          # Top 20 functions by CPU
(pprof) web             # Open flame graph in browser
(pprof) list funcName   # Show annotated source
```

### Quick Memory Profile

```bash
# Collect heap profile
go tool pprof http://localhost:8080/debug/pprof/heap

# Show allocations
(pprof) top 20 -cum     # Top allocators by cumulative allocation
(pprof) alloc_space     # Switch to allocation view
```

---

## Profiling Tools

### Built-in pprof Profiles

| Profile | Endpoint | Use Case |
|---------|----------|----------|
| `heap` | `/debug/pprof/heap` | Memory allocations, leak detection |
| `goroutine` | `/debug/pprof/goroutine` | Goroutine count, deadlock detection |
| `profile` | `/debug/pprof/profile?seconds=N` | CPU hotspots |
| `allocs` | `/debug/pprof/allocs` | Past memory allocations |
| `block` | `/debug/pprof/block` | Blocking on synchronization |
| `mutex` | `/debug/pprof/mutex` | Mutex contention |
| `trace` | `/debug/pprof/trace?seconds=N` | Execution trace |

### Compiler Diagnostics

```bash
# Escape analysis - see what allocates on heap
go build -gcflags='-m=2' ./... 2>&1 | grep 'escapes to heap'

# Bounds check elimination
go build -gcflags='-d=ssa/check_bce/debug' ./...

# Inlining decisions
go build -gcflags='-m' ./... 2>&1 | grep 'inlining'

# Generate SSA HTML for specific function
GOSSAFUNC=handleTelemetryMessage go build ./pkg/services/workers/
```

### Profile-Guided Optimization (PGO)

Go 1.20+ supports PGO for ~2-7% performance improvement:

```bash
# Step 1: Collect production profile
curl -o cpu.pprof http://localhost:8080/debug/pprof/profile?seconds=60

# Step 2: Build with PGO
go build -pgo=cpu.pprof -o overwatch ./cmd/microlith/

# Step 3: Verify PGO was applied
go version -m ./overwatch | grep pgo
```

### Execution Trace

For detailed runtime behavior analysis:

```bash
# Collect trace
curl -o trace.out http://localhost:8080/debug/pprof/trace?seconds=5

# Analyze with trace tool
go tool trace trace.out

# Opens browser with:
# - Goroutine analysis
# - Network blocking
# - Syscall blocking
# - Scheduler latency
```

---

## Memory Optimization

### Allocation Patterns

| Pattern | Problem | Solution |
|---------|---------|----------|
| Slice append in loop | Multiple reallocations | Pre-allocate with `make([]T, 0, expectedCap)` |
| Map without size hint | Rehashing overhead | Use `make(map[K]V, expectedSize)` |
| String concatenation | New allocation per concat | Use `strings.Builder` |
| Interface boxing | Heap allocation | Use concrete types in hot paths |
| Pointer indirection | Cache misses | Value types where possible |

### sync.Pool for Hot Paths

Use `sync.Pool` for frequently allocated objects:

```go
// Good: Pool for telemetry message buffers
var telemetryBufferPool = sync.Pool{
    New: func() interface{} {
        return make([]byte, 0, 4096)
    },
}

func processTelemetry(data []byte) {
    buf := telemetryBufferPool.Get().([]byte)
    buf = buf[:0] // Reset length, keep capacity
    defer telemetryBufferPool.Put(buf)

    // Use buf for processing...
}
```

### String Interning (Go 1.23+)

For duplicate strings (entity IDs, org IDs, message types):

```go
import "unique"

// Intern frequently repeated strings
type TelemetryWorker struct {
    // Instead of: entityCache map[string]*EntityState
    entityCache map[unique.Handle[string]]*EntityState
}

func (w *TelemetryWorker) cacheKey(entityID string) unique.Handle[string] {
    return unique.Make(entityID) // Returns canonical handle
}
```

### Struct Field Alignment

Optimize struct memory layout for cache efficiency:

```go
// Before: 40 bytes (with padding)
type BadLayout struct {
    a bool      // 1 byte + 7 padding
    b int64     // 8 bytes
    c bool      // 1 byte + 7 padding
    d int64     // 8 bytes
    e bool      // 1 byte + 7 padding
}

// After: 32 bytes (no padding)
type GoodLayout struct {
    b int64     // 8 bytes
    d int64     // 8 bytes
    f int64     // 8 bytes
    a bool      // 1 byte
    c bool      // 1 byte
    e bool      // 1 byte + 5 padding
}
```

Check alignment with:

```bash
# Install structlayout tools
go install honnef.co/go/tools/cmd/structlayout@latest
go install honnef.co/go/tools/cmd/structlayout-optimize@latest

# Analyze struct
structlayout -json ./pkg/shared EntityState | structlayout-pretty
```

### Escape Analysis Tips

Keep allocations on stack (faster) by:

1. **Avoid returning pointers to locals**
```go
// Bad: escapes to heap
func newEntity() *Entity {
    e := Entity{ID: "123"}
    return &e  // Forces heap allocation
}

// Good: let caller decide
func initEntity(e *Entity) {
    e.ID = "123"
}
```

2. **Avoid interface{} in hot paths**
```go
// Bad: boxes value
func process(v interface{}) { ... }

// Good: use generics (Go 1.18+)
func process[T any](v T) { ... }
```

3. **Avoid large stack allocations**
```go
// Bad: large array on stack may escape
func bad() {
    var buf [1 << 20]byte // 1MB - may escape
}

// Good: explicit heap allocation
func good() {
    buf := make([]byte, 1<<20) // Clear intent
}
```

---

## Concurrency Patterns

### Goroutine Lifecycle Management

**Problem in Codebase**: Fire-and-forget goroutines in `api/services/entity.service.go`:

```go
// Current (problematic):
go s.publishEntityEvent(entity, shared.EventTypeCreated)
go s.syncToKV(entity)
```

**Solution**: Use errgroup for coordinated goroutines:

```go
import "golang.org/x/sync/errgroup"

func (s *EntityService) CreateEntity(ctx context.Context, ...) (*Entity, error) {
    entity, err := s.insertEntity(...)
    if err != nil {
        return nil, err
    }

    // Background work with proper lifecycle
    g, ctx := errgroup.WithContext(ctx)

    g.Go(func() error {
        return s.publishEntityEvent(ctx, entity, shared.EventTypeCreated)
    })

    g.Go(func() error {
        return s.syncToKV(ctx, entity)
    })

    // Don't wait - but track errors via channel or metrics
    go func() {
        if err := g.Wait(); err != nil {
            logger.Errorw("Background task failed", "error", err)
            // Increment error metric
        }
    }()

    return entity, nil
}
```

### Worker Pool Pattern

For bounded concurrency:

```go
type WorkerPool struct {
    tasks   chan func()
    workers int
    wg      sync.WaitGroup
}

func NewWorkerPool(workers, queueSize int) *WorkerPool {
    p := &WorkerPool{
        tasks:   make(chan func(), queueSize),
        workers: workers,
    }

    for i := 0; i < workers; i++ {
        p.wg.Add(1)
        go p.worker()
    }

    return p
}

func (p *WorkerPool) worker() {
    defer p.wg.Done()
    for task := range p.tasks {
        task()
    }
}

func (p *WorkerPool) Submit(task func()) {
    p.tasks <- task
}

func (p *WorkerPool) Shutdown() {
    close(p.tasks)
    p.wg.Wait()
}
```

### Mutex Optimization

**Current Issue**: `TelemetryWorker.cacheMutex` uses `sync.RWMutex` but stores pointers:

```go
// Problematic: storing pointer allows data races
func (w *TelemetryWorker) updateCache(state *shared.EntityState) {
    w.cacheMutex.Lock()
    w.entityCache[state.EntityID] = state // Pointer stored
    w.cacheMutex.Unlock()
}
// Caller might modify state after this, causing race
```

**Solution**: Deep copy or use immutable updates:

```go
func (w *TelemetryWorker) updateCache(state *shared.EntityState) {
    // Option 1: Deep copy
    stateCopy := *state
    w.cacheMutex.Lock()
    w.entityCache[state.EntityID] = &stateCopy
    w.cacheMutex.Unlock()

    // Option 2: Use sync.Map for simple cases
    // w.entityCache.Store(state.EntityID, state)
}
```

### Atomic Operations

For simple counters, prefer atomics over mutexes:

```go
import "sync/atomic"

type Metrics struct {
    messagesProcessed atomic.Uint64
    bytesReceived     atomic.Uint64
    errors            atomic.Uint64
}

func (m *Metrics) IncrementMessages() {
    m.messagesProcessed.Add(1)
}
```

---

## Codebase-Specific Optimizations

### TelemetryWorker Optimizations

**Location**: `pkg/services/workers/telemetry.go`

| Issue | Line | Impact | Fix |
|-------|------|--------|-----|
| JSON unmarshal to `map[string]any` | 62 | Allocation per message | Use typed struct |
| String parsing with `fmt.Sscanf` | 816 | Slow for hot path | Use `strconv.ParseFloat` |
| Retry loop without backoff | 324 | CPU spinning | Add exponential backoff |
| Cache stores pointer | 432 | Potential data race | Deep copy or sync.Map |

**Optimized JSON Parsing**:

```go
// Current: generic map (slow)
var telemetry shared.MAVLinkTelemetry
if err := json.Unmarshal(msg.Data, &telemetry); err != nil { ... }

// Better: use json.Decoder with buffer reuse
var decoder = json.NewDecoder(nil)

func (w *TelemetryWorker) handleMessage(msg *nats.Msg) error {
    decoder.Reset(bytes.NewReader(msg.Data))
    var telemetry shared.MAVLinkTelemetry
    if err := decoder.Decode(&telemetry); err != nil { ... }
}

// Best: use faster JSON library for hot paths
import "github.com/goccy/go-json"
```

**Optimized Float Parsing**:

```go
// Current (slow):
func parseFloat(s string) float64 {
    var f float64
    fmt.Sscanf(s, "%f", &f)
    return f
}

// Better (10x faster):
func parseFloat(s string) float64 {
    f, _ := strconv.ParseFloat(s, 64)
    return f
}
```

### BaseWorker Fetch Optimization

**Location**: `pkg/services/workers/base.go:82`

```go
// Current: fixed batch size
msgs, err := sub.Fetch(10, nats.MaxWait(2*time.Second))

// Better: adaptive batch size based on load
const (
    minBatch = 10
    maxBatch = 100
)

func (w *BaseWorker) adaptiveFetch(sub *nats.Subscription, pending int) int {
    if pending > 1000 {
        return maxBatch
    }
    return minBatch
}
```

### Entity Service Allocation Reduction

**Location**: `api/services/entity.service.go`

```go
// Current: allocates new slice per call
func (s *EntityService) ListEntities(orgID string) ([]*Entity, error) {
    rows, err := s.db.Query(query, orgID)
    var entities []*Entity  // Unknown size
    for rows.Next() {
        entities = append(entities, &entity) // Multiple reallocs
    }
}

// Better: pre-allocate with estimated capacity
func (s *EntityService) ListEntities(orgID string) ([]*Entity, error) {
    // First, get count (or use cached estimate)
    var count int
    s.db.QueryRow("SELECT COUNT(*) FROM entities WHERE org_id = ?", orgID).Scan(&count)

    entities := make([]*Entity, 0, count)
    // ... rest of query
}
```

---

## NATS & JetStream Tuning

### Current Configuration Analysis

**Location**: `pkg/services/embedded-nats/nats.go`

| Setting | Current | Recommendation | Rationale |
|---------|---------|----------------|-----------|
| MaxPayload | 512KB | Keep | Good for video chunks |
| MaxPending | 512MB | Consider 256MB | Reduce memory under burst |
| MaxConn | 2000 | Appropriate | C4ISR scale |
| PingInterval | 30s | Consider 15s | Faster failure detection |
| WriteDeadline | 2s | Good | Real-time requirements |

### JetStream Stream Tuning

```go
// Telemetry stream - high throughput
{
    Name:            "CONSTELLATION_TELEMETRY",
    MaxMsgs:         100000,            // OK
    MaxBytes:        256 * 1024 * 1024, // OK
    MaxAge:          2 * time.Hour,     // Consider 30 min for memory
    MaxMsgSize:      128 * 1024,        // OK for MAVLink
    DuplicateWindow: 30 * time.Second,  // Consider 10s for telemetry
}

// Video stream - optimize for ephemeral data
{
    Name:     "CONSTELLATION_VIDEO_FRAMES",
    Storage:  nats.MemoryStorage, // Good - ephemeral
    MaxAge:   10 * time.Second,   // Good - prevent stale
    MaxBytes: 1024 * 1024 * 1024, // Consider per-entity limits
}
```

### Consumer Tuning

```go
// Current consumer config
config := &nats.ConsumerConfig{
    AckWait:       30 * time.Second,  // Consider 10s for fast retry
    MaxDeliver:    3,                 // OK
    MaxAckPending: 1000,              // Consider 5000 for throughput
}

// Add flow control for high-volume consumers
config.FlowControl = true
config.Heartbeat = 5 * time.Second
```

### Publish Optimization

```go
// Batch publishing for high-throughput
func (w *Worker) publishBatch(messages []Message) error {
    futures := make([]nats.PubAckFuture, len(messages))

    for i, msg := range messages {
        future, err := w.js.PublishAsync(msg.Subject, msg.Data)
        if err != nil {
            return err
        }
        futures[i] = future
    }

    // Wait for all acks
    for _, f := range futures {
        select {
        case <-f.Ok():
            // Success
        case err := <-f.Err():
            return err
        }
    }
    return nil
}
```

---

## SQLite Performance

### Current Configuration

**Location**: `db/service.go`

```go
// Current settings
db.SetMaxOpenConns(1)      // Correct for SQLite
db.SetMaxIdleConns(1)      // Correct
db.SetConnMaxLifetime(0)   // Issue: unlimited lifetime
```

### Recommended Configuration

```go
func configureDB(db *sql.DB) {
    db.SetMaxOpenConns(1)
    db.SetMaxIdleConns(1)
    db.SetConnMaxLifetime(time.Hour) // Refresh stale connections

    // Enable WAL mode for better concurrency
    db.Exec("PRAGMA journal_mode=WAL")
    db.Exec("PRAGMA synchronous=NORMAL")
    db.Exec("PRAGMA cache_size=-64000") // 64MB cache
    db.Exec("PRAGMA busy_timeout=5000") // 5s timeout
}
```

### Query Optimization

```go
// Use prepared statements for repeated queries
type EntityService struct {
    db          *sql.DB
    stmtGetByID *sql.Stmt
    stmtList    *sql.Stmt
}

func NewEntityService(db *sql.DB) (*EntityService, error) {
    s := &EntityService{db: db}

    var err error
    s.stmtGetByID, err = db.Prepare(`
        SELECT entity_id, org_id, name, entity_type, status
        FROM entities WHERE entity_id = ?
    `)
    if err != nil {
        return nil, err
    }

    return s, nil
}

// Use EXPLAIN QUERY PLAN to verify index usage
// sqlite> EXPLAIN QUERY PLAN SELECT * FROM entities WHERE org_id = ?;
```

### Index Recommendations

Check `db/schema.sql` has appropriate indexes:

```sql
-- Essential indexes for common queries
CREATE INDEX IF NOT EXISTS idx_entities_org_id ON entities(org_id);
CREATE INDEX IF NOT EXISTS idx_entities_status ON entities(org_id, status);
CREATE INDEX IF NOT EXISTS idx_telemetry_entity_timestamp
    ON telemetry(entity_id, timestamp DESC);

-- Covering index for list queries
CREATE INDEX IF NOT EXISTS idx_entities_list
    ON entities(org_id, entity_id, name, status, entity_type);
```

---

## Benchmarking

### Writing Benchmarks

```go
// pkg/services/workers/telemetry_test.go
func BenchmarkTelemetryParsing(b *testing.B) {
    data := []byte(`{"message_type":"GPS_RAW_INT","data":{"lat":123456789}}`)

    b.ResetTimer()
    b.ReportAllocs()

    for i := 0; i < b.N; i++ {
        var t shared.MAVLinkTelemetry
        json.Unmarshal(data, &t)
    }
}

func BenchmarkTelemetryParsingParallel(b *testing.B) {
    data := []byte(`{"message_type":"GPS_RAW_INT","data":{"lat":123456789}}`)

    b.RunParallel(func(pb *testing.PB) {
        for pb.Next() {
            var t shared.MAVLinkTelemetry
            json.Unmarshal(data, &t)
        }
    })
}
```

### Running Benchmarks

```bash
# Run all benchmarks
go test -bench=. -benchmem ./...

# Run specific benchmark with CPU profile
go test -bench=BenchmarkTelemetry -benchmem -cpuprofile=cpu.prof ./pkg/services/workers/

# Compare benchmarks (requires benchstat)
go install golang.org/x/perf/cmd/benchstat@latest

go test -bench=. -count=10 ./... > old.txt
# Make changes
go test -bench=. -count=10 ./... > new.txt
benchstat old.txt new.txt
```

### Benchmark Best Practices

```go
// 1. Reset timer after setup
func BenchmarkWithSetup(b *testing.B) {
    expensive := setupTestData()
    b.ResetTimer() // Don't count setup time

    for i := 0; i < b.N; i++ {
        process(expensive)
    }
}

// 2. Prevent compiler optimization
var result int

func BenchmarkCompute(b *testing.B) {
    var r int
    for i := 0; i < b.N; i++ {
        r = compute(i)
    }
    result = r // Prevent dead code elimination
}

// 3. Use sub-benchmarks for variants
func BenchmarkJSON(b *testing.B) {
    sizes := []int{100, 1000, 10000}

    for _, size := range sizes {
        b.Run(fmt.Sprintf("size=%d", size), func(b *testing.B) {
            data := generateJSON(size)
            b.ResetTimer()
            for i := 0; i < b.N; i++ {
                json.Unmarshal(data, &target)
            }
        })
    }
}
```

---

## Production Monitoring

### Key Metrics to Track

| Metric | Source | Alert Threshold |
|--------|--------|-----------------|
| Goroutine count | `runtime.NumGoroutine()` | > 10,000 |
| Heap alloc | `runtime.MemStats.HeapAlloc` | > 80% of limit |
| GC pause | `runtime.MemStats.PauseNs` | > 10ms p99 |
| GC frequency | `runtime.MemStats.NumGC` | > 10/sec |
| NATS pending | JetStream consumer info | > 10,000 |
| HTTP latency | Middleware | > 100ms p99 |

### Runtime Metrics Collection

```go
import "runtime"

func collectRuntimeMetrics() map[string]interface{} {
    var m runtime.MemStats
    runtime.ReadMemStats(&m)

    return map[string]interface{}{
        "goroutines":     runtime.NumGoroutine(),
        "heap_alloc":     m.HeapAlloc,
        "heap_sys":       m.HeapSys,
        "heap_objects":   m.HeapObjects,
        "gc_num":         m.NumGC,
        "gc_pause_ns":    m.PauseNs[(m.NumGC+255)%256],
        "gc_cpu_percent": m.GCCPUFraction * 100,
    }
}
```

### GOMAXPROCS Tuning

```go
import "runtime"

func init() {
    // For containerized deployments, detect CPU quota
    // Default GOMAXPROCS uses all available CPUs

    // Option 1: Set explicitly for known deployment
    // runtime.GOMAXPROCS(4)

    // Option 2: Use automaxprocs for containers
    // import _ "go.uber.org/automaxprocs"
}
```

### Memory Limit (Go 1.19+)

```go
import "runtime/debug"

func init() {
    // Set soft memory limit for GC pressure
    // Useful in containers with memory limits
    debug.SetMemoryLimit(512 << 20) // 512MB
}
```

---

## Go Version Considerations

### Go 1.22+ Features

| Feature | Impact | Use Case |
|---------|--------|----------|
| Loop variable fix | Bug prevention | All `for` loops with closures |
| `for range N` | Cleaner code | Integer iteration |
| Enhanced ServeMux | Simpler routing | May replace chi for simple routes |

### Go 1.23+ Features

| Feature | Impact | Use Case |
|---------|--------|----------|
| `unique` package | Memory optimization | String interning for IDs |
| Iterator functions | Cleaner APIs | Custom collection iteration |

### Go 1.24+ Features

| Feature | Impact | Use Case |
|---------|--------|----------|
| Weak pointers | Cache efficiency | Advanced caching patterns |

### Build Tags for Version-Specific Code

```go
//go:build go1.23

package workers

import "unique"

// Use unique.Handle for Go 1.23+
type EntityCache struct {
    entries map[unique.Handle[string]]*EntityState
}
```

---

## Performance Checklist

### Before Production Deployment

- [ ] Run with `-race` flag in CI to detect races
- [ ] Profile under realistic load with pprof
- [ ] Check goroutine count doesn't grow unbounded
- [ ] Verify no memory leaks with heap profile
- [ ] Run benchmarks and compare to baseline
- [ ] Enable PGO with production profile
- [ ] Set appropriate `GOMAXPROCS` for deployment
- [ ] Configure memory limits for containers
- [ ] Review escape analysis output for hot paths
- [ ] Check SQL queries use indexes (EXPLAIN QUERY PLAN)

### Monitoring Checklist

- [ ] Prometheus metrics endpoint (`/metrics`)
- [ ] pprof endpoints protected but accessible
- [ ] Goroutine count metric with alerting
- [ ] GC pause time metric with alerting
- [ ] Request latency histograms
- [ ] NATS consumer lag metrics
- [ ] SQLite connection pool stats

---

## Additional Resources

### Official Documentation

- [Go Performance Wiki](https://go.dev/wiki/Performance)
- [pprof Documentation](https://pkg.go.dev/runtime/pprof)
- [Go Blog: Profiling Go Programs](https://go.dev/blog/pprof)
- [NATS Performance](https://docs.nats.io/nats-concepts/subject_mapping)

### Community Resources

- [Go Optimization Guide](https://goperf.dev/) - Comprehensive patterns
- [go-perfbook](https://github.com/dgryski/go-perfbook) - Performance tips
- [High Performance Go Workshop](https://dave.cheney.net/high-performance-go-workshop/dotgo-paris.html)

### Tools

- `go tool pprof` - Built-in profiler
- `go tool trace` - Execution tracer
- `benchstat` - Benchmark comparison
- `structlayout` - Struct memory analysis
- `golangci-lint` - Includes performance linters

---

## Changelog

### Version 1.0.0 (January 2026)

- Initial performance tuning guide
- Codebase-specific analysis for Constellation Overwatch
- NATS/JetStream tuning recommendations
- SQLite optimization guidelines
- Profiling quick start guide
