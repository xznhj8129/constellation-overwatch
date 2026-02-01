# Northstar Design Pattern Enhancements

> Reference: [github.com/zangster300/northstar](https://github.com/zangster300/northstar)
>
> This document outlines patterns from the northstar repository that can simplify and improve our `pkg/services/web/` implementation.

---

## Table of Contents

1. [Typed Signal Structs](#1-typed-signal-structs)
2. [Direct Datastar SDK Template Helpers](#2-direct-datastar-sdk-template-helpers)
3. [Feature-Based Route Registration](#3-feature-based-route-registration)
4. [Development Hot Reload](#4-development-hot-reload)
5. [Chi Router Migration](#5-chi-router-migration)
6. [Simplified NATS Embedded Setup](#6-simplified-nats-embedded-setup)
7. [SSE Indicator Components](#7-sse-indicator-components)
8. [Signal Initialization in Templates](#8-signal-initialization-in-templates)

---

## 1. Typed Signal Structs

### Current Problem

Our signal patching uses untyped `map[string]interface{}` which is error-prone and lacks IDE support:

```go
// pkg/services/web/handlers/overwatch.go:536-543
minimalEntitySignal := map[string]interface{}{
    "entityId":   entityID,
    "orgId":      entityState.OrgID,
    "name":       entityState.Name,
    "entityType": entityState.EntityType,
    "status":     entityState.Status,
    "isLive":     entityState.IsLive,
}
```

### Northstar Pattern

Northstar defines typed structs with JSON tags:

```go
// northstar/features/monitor/pages/monitor_templ.go:16-23
type SystemMonitorSignals struct {
    MemTotal       string `json:"memTotal,omitempty"`
    MemUsed        string `json:"memUsed,omitempty"`
    MemUsedPercent string `json:"memUsedPercent,omitempty"`
    CpuUser        string `json:"cpuUser,omitempty"`
    CpuSystem      string `json:"cpuSystem,omitempty"`
    CpuIdle        string `json:"cpuIdle,omitempty"`
}

// Usage in handler - northstar/features/monitor/handlers.go:53-61
memStats := pages.SystemMonitorSignals{
    MemTotal:       humanize.Bytes(vm.Total),
    MemUsed:        humanize.Bytes(vm.Used),
    MemUsedPercent: fmt.Sprintf("%.2f%%", vm.UsedPercent),
}
if err := sse.MarshalAndPatchSignals(memStats); err != nil {
    http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}
```

### Implementation

#### Step 1: Create Signal Types File

**Create:** `pkg/services/web/features/overwatch/signals/signals.go`

```go
package signals

import "time"

// EntitySignal represents the minimal entity state pushed to the frontend
type EntitySignal struct {
    EntityID   string  `json:"entityId"`
    OrgID      string  `json:"orgId"`
    Name       string  `json:"name,omitempty"`
    EntityType string  `json:"entityType,omitempty"`
    Status     string  `json:"status,omitempty"`
    IsLive     bool    `json:"isLive"`
    Lat        float64 `json:"lat,omitempty"`
    Lng        float64 `json:"lng,omitempty"`
    Alt        float64 `json:"alt,omitempty"`
}

// OverwatchSignals represents the global overwatch dashboard signals
type OverwatchSignals struct {
    LastUpdate    string `json:"lastUpdate,omitempty"`
    TotalEntities int    `json:"totalEntities"`
    TotalOrgs     int    `json:"totalOrgs"`
    IsConnected   bool   `json:"_isConnected"`
}

// AnalyticsSignals represents aggregated analytics data
type AnalyticsSignals struct {
    TypeCounts   map[string]int    `json:"typeCounts,omitempty"`
    StatusCounts map[string]int    `json:"statusCounts,omitempty"`
    Threats      ThreatSignals     `json:"threats,omitempty"`
    Vision       VisionSignals     `json:"vision,omitempty"`
}

// ThreatSignals represents threat-related signals
type ThreatSignals struct {
    Active   int                `json:"active"`
    Priority map[string]int     `json:"priority,omitempty"`
}

// VisionSignals represents computer vision signals
type VisionSignals struct {
    Tracked    int `json:"tracked"`
    Detections int `json:"detections"`
}

// MetricsSignals for the metrics dashboard
type MetricsSignals struct {
    CPUPercent    float64 `json:"cpuPercent"`
    MemoryPercent float64 `json:"memoryPercent"`
    MemoryUsed    string  `json:"memoryUsed"`
    MemoryTotal   string  `json:"memoryTotal"`
    Goroutines    int     `json:"goroutines"`
    HeapAlloc     string  `json:"heapAlloc"`
    LastUpdate    string  `json:"lastUpdate"`
}
```

#### Step 2: Update Overwatch Handler

**Edit:** `pkg/services/web/handlers/overwatch.go`

```go
// Add import
import (
    "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/features/overwatch/signals"
)

// Replace lines 536-556 with:
func (h *OverwatchHandler) buildEntitySignal(entityState shared.EntityState) signals.EntitySignal {
    sig := signals.EntitySignal{
        EntityID:   entityState.EntityID,
        OrgID:      entityState.OrgID,
        Name:       entityState.Name,
        EntityType: entityState.EntityType,
        Status:     entityState.Status,
        IsLive:     entityState.IsLive,
    }

    // Add position if available
    if entityState.Position != nil && entityState.Position.Global != nil {
        sig.Lat = entityState.Position.Global.Latitude
        sig.Lng = entityState.Position.Global.Longitude
        sig.Alt = entityState.Position.Global.AltitudeMSL
    }

    return sig
}

// Then use with MarshalAndPatchSignals:
entitySignal := h.buildEntitySignal(entityState)
if err := sse.sse.MarshalAndPatchSignals(map[string]interface{}{
    fmt.Sprintf("entityStatesByOrg.%s.%s", entityState.OrgID, entityID): entitySignal,
}); err != nil {
    logger.Debugw("Failed to patch entity signals", "error", err)
    return
}
```

#### Step 3: Update Metrics Handler

**Edit:** `pkg/services/web/handlers/metrics.go`

Replace signal map with typed struct:

```go
// Before (current):
signals := map[string]interface{}{
    "cpuPercent":    cpuPercent,
    "memoryPercent": memPercent,
    // ...
}

// After (with typed struct):
metricsSignals := signals.MetricsSignals{
    CPUPercent:    cpuPercent,
    MemoryPercent: memPercent,
    MemoryUsed:    humanize.Bytes(memStats.Used),
    MemoryTotal:   humanize.Bytes(memStats.Total),
    Goroutines:    runtime.NumGoroutine(),
    HeapAlloc:     humanize.Bytes(memStats.HeapAlloc),
    LastUpdate:    time.Now().Format("15:04:05"),
}
if err := sse.sse.MarshalAndPatchSignals(metricsSignals); err != nil {
    // handle error
}
```

---

## 2. Direct Datastar SDK Template Helpers

### Current Problem

We manually construct Datastar attribute strings in templates:

```go
// Current approach in templates
data-on-click="@get('/api/overwatch/kv')"
```

### Northstar Pattern

Northstar uses SDK helper functions that generate proper Datastar attributes:

```go
// northstar/features/index/components/todo_templ.go:104
data-on:click={datastar.PostSSE("/api/todos/%d/toggle", i)}

// northstar/features/monitor/pages/monitor_templ.go:67
data-init={datastar.GetSSE("/monitor/events")}
```

The SDK provides these helpers:
- `datastar.GetSSE(url, args...)` - generates `@get('/url')`
- `datastar.PostSSE(url, args...)` - generates `@post('/url')`
- `datastar.PutSSE(url, args...)` - generates `@put('/url')`
- `datastar.DeleteSSE(url, args...)` - generates `@delete('/url')`

### Implementation

#### Step 1: Create Datastar Helpers Package

**Create:** `pkg/services/web/datastar/helpers.go`

```go
package datastar

import (
    "fmt"

    ds "github.com/starfederation/datastar-go/datastar"
)

// Re-export SDK helpers for use in templates
var (
    GetSSE    = ds.GetSSE
    PostSSE   = ds.PostSSE
    PutSSE    = ds.PutSSE
    DeleteSSE = ds.DeleteSSE
    PatchSSE  = ds.PatchSSE
)

// Custom helpers for common patterns

// GetSSEWithFilter generates a GET SSE call with filter parameter
func GetSSEWithFilter(url string, filter string) string {
    if filter != "" {
        return fmt.Sprintf("@get('%s?filter=%s')", url, filter)
    }
    return ds.GetSSE(url)
}

// SSEWithIndicator wraps an SSE call with a loading indicator
func SSEWithIndicator(sseCall string, indicatorName string) string {
    return fmt.Sprintf("%s; $%s = true", sseCall, indicatorName)
}
```

#### Step 2: Update Template Files

**Edit:** `pkg/services/web/features/overwatch/pages/overwatch.templ`

```templ
package pages

import (
    "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/datastar"
    "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/features/common/layouts"
)

templ OverwatchPage() {
    @layouts.Base("Overwatch Dashboard") {
        <div
            id="overwatch-container"
            data-signals="{lastUpdate:'', totalEntities:0, totalOrgs:0, _isConnected:false, entityStatesByOrg:{}, analytics:{}}"
            data-init={ datastar.GetSSE("/api/overwatch/kv/watch") }
        >
            // ... rest of template
        </div>
    }
}
```

**Edit:** `pkg/services/web/features/streams/pages/streams.templ`

```templ
package pages

import (
    "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/datastar"
)

templ StreamsPage() {
    <div
        id="streams-container"
        data-init={ datastar.GetSSE("/api/streams/sse") }
    >
        <div id="stream-messages"></div>
    </div>
}
```

**Edit:** `pkg/services/web/features/metrics/pages/metrics.templ`

```templ
package pages

import (
    "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/datastar"
)

templ MetricsPage() {
    <div
        id="metrics-container"
        data-signals="{cpuPercent:0, memoryPercent:0, memoryUsed:'', memoryTotal:'', goroutines:0, heapAlloc:'', lastUpdate:''}"
        data-init={ datastar.GetSSE("/api/metrics/sse") }
    >
        // ... metric displays using data-text="$cpuPercent" etc.
    </div>
}
```

#### Step 3: Update Organization Components

**Edit:** `pkg/services/web/features/organizations/components/organizations_table.templ`

```templ
package components

import (
    "fmt"
    "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/datastar"
)

templ OrganizationRow(org Organization, isEditing bool) {
    if isEditing {
        <tr id={ fmt.Sprintf("org-row-%s", org.ID) }>
            <td>
                <input
                    type="text"
                    data-bind:orgName
                    value={ org.Name }
                />
            </td>
            <td>
                <button
                    data-on:click={ datastar.PutSSE("/api/organizations/update") }
                >
                    Save
                </button>
                <button
                    data-on:click={ datastar.GetSSE("/organizations/cancel/%s", org.ID) }
                >
                    Cancel
                </button>
            </td>
        </tr>
    } else {
        <tr id={ fmt.Sprintf("org-row-%s", org.ID) }>
            <td>{ org.Name }</td>
            <td>
                <button
                    data-on:click={ datastar.GetSSE("/organizations/edit/%s", org.ID) }
                >
                    Edit
                </button>
                <button
                    data-on:click={ datastar.DeleteSSE("/api/organizations/%s", org.ID) }
                >
                    Delete
                </button>
            </td>
        </tr>
    }
}
```

---

## 3. Feature-Based Route Registration

### Current Problem

All routes are registered in a single monolithic function in `router.go`:

```go
// pkg/services/web/router.go - 117 lines, all routes in one place
func NewRouter(...) *http.ServeMux {
    mux := http.NewServeMux()
    // ... 80+ route registrations
}
```

### Northstar Pattern

Each feature registers its own routes:

```go
// northstar/router/router.go:32-38
if err := errors.Join(
    indexFeature.SetupRoutes(router, sessionStore, ns),
    counterFeature.SetupRoutes(router, sessionStore),
    monitorFeature.SetupRoutes(router),
    sortableFeature.SetupRoutes(router),
    reverseFeature.SetupRoutes(router),
); err != nil {
    return fmt.Errorf("error setting up routes: %w", err)
}

// northstar/features/monitor/routes.go
package monitor

func SetupRoutes(router chi.Router) error {
    handlers := NewHandlers()

    router.Get("/monitor", handlers.MonitorPage)
    router.Get("/monitor/events", handlers.MonitorEvents)

    return nil
}
```

### Implementation

#### Step 1: Create Route Setup Functions Per Feature

**Create:** `pkg/services/web/features/overwatch/routes.go`

```go
package overwatch

import (
    "net/http"

    "github.com/Constellation-Overwatch/constellation-overwatch/api/middleware"
    "github.com/Constellation-Overwatch/constellation-overwatch/api/services"
    embeddednats "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/embedded-nats"
)

type Feature struct {
    handlers *Handlers
}

func NewFeature(natsEmbedded *embeddednats.EmbeddedNATS, orgSvc *services.OrganizationService) *Feature {
    return &Feature{
        handlers: NewHandlers(natsEmbedded, orgSvc),
    }
}

func (f *Feature) SetupRoutes(mux *http.ServeMux, protect func(http.HandlerFunc) http.Handler) error {
    // Pages
    mux.Handle("/overwatch", protect(f.handlers.HandleOverwatchPage))

    // API endpoints
    mux.Handle("/api/overwatch/kv", protect(f.handlers.HandleAPIOverwatchKV))
    mux.Handle("/api/overwatch/kv/watch", protect(f.handlers.HandleAPIOverwatchKVWatch))
    mux.Handle("/api/overwatch/kv/debug", protect(f.handlers.HandleAPIOverwatchKVDebug))

    return nil
}
```

**Create:** `pkg/services/web/features/metrics/routes.go`

```go
package metrics

import (
    "net/http"
)

type Feature struct {
    handlers *Handlers
}

func NewFeature() *Feature {
    return &Feature{
        handlers: NewHandlers(),
    }
}

func (f *Feature) SetupRoutes(mux *http.ServeMux, protect func(http.HandlerFunc) http.Handler) error {
    mux.Handle("/metrics-ui", protect(f.handlers.HandleMetricsPage))
    mux.Handle("/api/metrics/sse", protect(f.handlers.HandleSSE))

    return nil
}
```

**Create:** `pkg/services/web/features/streams/routes.go`

```go
package streams

import (
    "net/http"

    "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web"
)

type Feature struct {
    sseHandler *web.SSEHandler
}

func NewFeature(sseHandler *web.SSEHandler) *Feature {
    return &Feature{
        sseHandler: sseHandler,
    }
}

func (f *Feature) SetupRoutes(mux *http.ServeMux, protect func(http.HandlerFunc) http.Handler) error {
    mux.Handle("/streams", protect(f.HandleStreamsPage))
    mux.Handle("/api/streams/sse", protect(func(w http.ResponseWriter, r *http.Request) {
        f.sseHandler.StreamMessages(w, r)
    }))

    return nil
}
```

#### Step 2: Refactor Router

**Edit:** `pkg/services/web/router.go`

```go
package web

import (
    "errors"
    "net/http"

    "github.com/Constellation-Overwatch/constellation-overwatch/api/middleware"
    "github.com/Constellation-Overwatch/constellation-overwatch/api/services"
    "github.com/Constellation-Overwatch/constellation-overwatch/pkg/metrics"
    embeddednats "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/embedded-nats"

    // Feature imports
    authFeature "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/features/auth"
    docsFeature "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/features/docs"
    fleetFeature "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/features/fleet"
    mapFeature "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/features/map"
    metricsFeature "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/features/metrics"
    orgsFeature "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/features/organizations"
    overwatchFeature "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/features/overwatch"
    streamsFeature "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/features/streams"
    videoFeature "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/features/video"
)

func NewRouter(
    orgSvc *services.OrganizationService,
    entitySvc *services.EntityService,
    natsEmbedded *embeddednats.EmbeddedNATS,
    sseHandler *SSEHandler,
    apiHandler http.Handler,
    sessionAuth *middleware.SessionAuth,
) *http.ServeMux {
    mux := http.NewServeMux()

    // Helper to wrap handlers with session auth
    protect := func(h http.HandlerFunc) http.Handler {
        return sessionAuth.RequireSession(http.HandlerFunc(h))
    }

    // ============================================
    // Public Routes (no auth required)
    // ============================================

    // Static files
    mux.Handle("/static/", http.StripPrefix("/static/", StaticFileServer()))

    // Health & metrics endpoints
    mux.Handle("/metrics", metrics.Handler())
    mux.HandleFunc("/health", handleHealth)

    // OpenAPI spec
    mux.Handle("/api/openapi.json", NewSpecHandler())

    // ============================================
    // Feature Route Registration
    // ============================================

    // Initialize features
    auth := authFeature.NewFeature(sessionAuth)
    docs := docsFeature.NewFeature()
    fleet := fleetFeature.NewFeature(entitySvc)
    mapF := mapFeature.NewFeature()
    metricsF := metricsFeature.NewFeature()
    orgs := orgsFeature.NewFeature(orgSvc, entitySvc)
    overwatch := overwatchFeature.NewFeature(natsEmbedded, orgSvc)
    streams := streamsFeature.NewFeature(sseHandler)
    video := videoFeature.NewFeature(natsEmbedded)

    // Register all feature routes
    if err := errors.Join(
        auth.SetupRoutes(mux, protect),
        docs.SetupRoutes(mux, protect),
        fleet.SetupRoutes(mux, protect),
        mapF.SetupRoutes(mux, protect),
        metricsF.SetupRoutes(mux, protect),
        orgs.SetupRoutes(mux, protect),
        overwatch.SetupRoutes(mux, protect),
        streams.SetupRoutes(mux, protect),
        video.SetupRoutes(mux, protect),
    ); err != nil {
        // Log error but don't fail - routes are critical
        logger.Errorw("Failed to setup some feature routes", "error", err)
    }

    // Protected pprof endpoints
    metrics.RegisterPProf(mux, protect)

    // Root redirect
    mux.Handle("/", protect(handleRootRedirect))

    // Mount REST API (has its own Bearer token auth)
    if apiHandler != nil {
        mux.Handle("/api/", http.StripPrefix("/api", apiHandler))
    }

    return mux
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    w.Write([]byte(`{"status":"ok"}`))
}

func handleRootRedirect(w http.ResponseWriter, r *http.Request) {
    if r.URL.Path != "/" {
        http.NotFound(w, r)
        return
    }
    http.Redirect(w, r, "/map", http.StatusFound)
}
```

---

## 4. Development Hot Reload

### Northstar Pattern

Northstar implements a clever SSE-based hot reload:

```go
// northstar/router/router.go:45-68
func setupReload(router chi.Router) {
    reloadChan := make(chan struct{}, 1)
    var hotReloadOnce sync.Once

    router.Get("/reload", func(w http.ResponseWriter, r *http.Request) {
        sse := datastar.NewSSE(w, r)
        reload := func() { sse.ExecuteScript("window.location.reload()") }
        hotReloadOnce.Do(reload)
        select {
        case <-reloadChan:
            reload()
        case <-r.Context().Done():
        }
    })

    router.Get("/hotreload", func(w http.ResponseWriter, r *http.Request) {
        select {
        case reloadChan <- struct{}{}:
        default:
        }
        w.WriteHeader(http.StatusOK)
        w.Write([]byte("OK"))
    })
}

// In base template (dev mode only):
// <div data-init="@get('/reload', {retryMaxCount: 1000, retryInterval:20, retryMaxWaitMs:200})"></div>
```

### Implementation

#### Step 1: Create Hot Reload Handler

**Create:** `pkg/services/web/features/dev/hotreload.go`

```go
package dev

import (
    "net/http"
    "sync"

    ds "github.com/starfederation/datastar-go/datastar"
)

// HotReload manages development hot reload functionality
type HotReload struct {
    reloadChan    chan struct{}
    hotReloadOnce sync.Once
    mu            sync.Mutex
}

// NewHotReload creates a new hot reload manager
func NewHotReload() *HotReload {
    return &HotReload{
        reloadChan: make(chan struct{}, 1),
    }
}

// HandleReloadSSE handles the SSE connection that triggers page reload
func (h *HotReload) HandleReloadSSE(w http.ResponseWriter, r *http.Request) {
    sse := ds.NewSSE(w, r)

    reload := func() {
        sse.ExecuteScript("window.location.reload()")
    }

    // Reload once on initial connection (for reconnects after server restart)
    h.hotReloadOnce.Do(reload)

    select {
    case <-h.reloadChan:
        reload()
    case <-r.Context().Done():
        return
    }
}

// HandleTriggerReload triggers a reload for all connected clients
func (h *HotReload) HandleTriggerReload(w http.ResponseWriter, r *http.Request) {
    h.mu.Lock()
    defer h.mu.Unlock()

    // Reset the once so next connection triggers reload
    h.hotReloadOnce = sync.Once{}

    select {
    case h.reloadChan <- struct{}{}:
    default:
        // Channel full, reload already pending
    }

    w.WriteHeader(http.StatusOK)
    w.Write([]byte("OK"))
}

// SetupRoutes registers hot reload routes (only call in dev mode)
func (h *HotReload) SetupRoutes(mux *http.ServeMux) {
    mux.HandleFunc("/dev/reload", h.HandleReloadSSE)
    mux.HandleFunc("/dev/trigger-reload", h.HandleTriggerReload)
}
```

#### Step 2: Update Base Layout

**Edit:** `pkg/services/web/features/common/layouts/base.templ`

```templ
package layouts

import (
    "os"
    "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/datastar"
)

func isDev() bool {
    return os.Getenv("GO_ENV") == "development" || os.Getenv("GO_ENV") == ""
}

templ Base(title string) {
    <!DOCTYPE html>
    <html lang="en">
        <head>
            <title>{ title } | Constellation Overwatch</title>
            <meta name="viewport" content="width=device-width, initial-scale=1"/>
            <link rel="stylesheet" href="/static/css/main.css"/>
            <script defer type="module" src="/static/js/datastar.js"></script>
        </head>
        <body>
            if isDev() {
                <!-- Hot reload connection (dev only) -->
                <div
                    data-init={ datastar.GetSSE("/dev/reload") + ", {retryMaxCount: 1000, retryInterval: 100, retryMaxWaitMs: 500}" }
                    style="display: none;"
                ></div>
            }
            { children... }
        </body>
    </html>
}
```

#### Step 3: Register in Router (Dev Mode Only)

**Edit:** `pkg/services/web/router.go`

```go
import (
    "os"
    devFeature "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/features/dev"
)

func NewRouter(...) *http.ServeMux {
    // ... existing code ...

    // Development-only routes
    if os.Getenv("GO_ENV") == "development" || os.Getenv("GO_ENV") == "" {
        hotReload := devFeature.NewHotReload()
        hotReload.SetupRoutes(mux)
    }

    // ... rest of router ...
}
```

#### Step 4: Add Taskfile Integration

**Edit:** `Taskfile.yml`

```yaml
tasks:
  dev:
    desc: Run with hot reload
    cmds:
      - |
        # Start templ watch in background
        templ generate --watch &
        TEMPL_PID=$!

        # Start server with air (or custom watcher)
        GO_ENV=development air -c .air.toml

        # Cleanup
        kill $TEMPL_PID 2>/dev/null || true
    env:
      GO_ENV: development

  trigger-reload:
    desc: Trigger hot reload for all connected clients
    cmds:
      - curl -s http://localhost:8080/dev/trigger-reload
```

---

## 5. Chi Router Migration

### Current State

Using standard `http.ServeMux` which lacks:
- Route parameters (`/users/{id}`)
- Route groups
- Middleware chaining
- Method-specific routing

### Northstar Pattern

Chi provides cleaner routing:

```go
// northstar/features/index/routes.go
router.Route("/api", func(apiRouter chi.Router) {
    apiRouter.Route("/todos", func(todosRouter chi.Router) {
        todosRouter.Get("/", handlers.TodosSSE)
        todosRouter.Put("/reset", handlers.ResetTodos)
        todosRouter.Put("/cancel", handlers.CancelEdit)
        todosRouter.Put("/mode/{mode}", handlers.SetMode)

        todosRouter.Route("/{idx}", func(todoRouter chi.Router) {
            todoRouter.Post("/toggle", handlers.ToggleTodo)
            todoRouter.Route("/edit", func(editRouter chi.Router) {
                editRouter.Get("/", handlers.StartEdit)
                editRouter.Put("/", handlers.SaveEdit)
            })
            todoRouter.Delete("/", handlers.DeleteTodo)
        })
    })
})

// Accessing URL params
idx := chi.URLParam(r, "idx")
```

### Implementation

#### Step 1: Add Chi Dependency

```bash
go get github.com/go-chi/chi/v5
```

#### Step 2: Update Router

**Edit:** `pkg/services/web/router.go`

```go
package web

import (
    "errors"
    "net/http"

    "github.com/go-chi/chi/v5"
    "github.com/go-chi/chi/v5/middleware"

    appMiddleware "github.com/Constellation-Overwatch/constellation-overwatch/api/middleware"
    // ... other imports
)

func NewRouter(
    orgSvc *services.OrganizationService,
    entitySvc *services.EntityService,
    natsEmbedded *embeddednats.EmbeddedNATS,
    sseHandler *SSEHandler,
    apiHandler http.Handler,
    sessionAuth *appMiddleware.SessionAuth,
) chi.Router {
    r := chi.NewRouter()

    // Global middleware
    r.Use(middleware.Logger)
    r.Use(middleware.Recoverer)
    r.Use(middleware.RealIP)

    // Public routes
    r.Group(func(r chi.Router) {
        r.Handle("/static/*", http.StripPrefix("/static/", StaticFileServer()))
        r.Handle("/metrics", metrics.Handler())
        r.Get("/health", handleHealth)
        r.Get("/api/openapi.json", NewSpecHandler().ServeHTTP)
        r.Get("/login", authHandler.HandleLogin)
    })

    // Protected routes
    r.Group(func(r chi.Router) {
        r.Use(sessionAuth.RequireSessionMiddleware)

        // Pages
        r.Get("/", handleRootRedirect)
        r.Get("/map", pageHandler.HandleMapPage)
        r.Get("/overwatch", pageHandler.HandleOverwatchPage)
        r.Get("/streams", pageHandler.HandleStreamsPage)
        r.Get("/fleet", pageHandler.HandleFleetPage)
        r.Get("/docs", docsHandler.HandleDocsPage)
        r.Get("/metrics-ui", metricsHandler.HandleMetricsPage)

        // Organizations
        r.Route("/organizations", func(r chi.Router) {
            r.Get("/", pageHandler.HandleEntitiesPage)
            r.Get("/new", pageHandler.HandleOrganizationForm)
            r.Get("/edit/{id}", datastarHandler.HandleOrganizationEdit)
            r.Get("/cancel/{id}", datastarHandler.HandleOrganizationCancel)
            r.Get("/entities/new", pageHandler.HandleEntityForm)
            r.Get("/entities/edit", pageHandler.HandleEntityForm)
        })

        // API routes
        r.Route("/api", func(r chi.Router) {
            // Organizations API
            r.Route("/organizations", func(r chi.Router) {
                r.Get("/", datastarHandler.HandleAPIOrganizations)
                r.Put("/update", datastarHandler.HandleAPIOrganizationUpdate)
                r.Get("/{id}", datastarHandler.HandleAPIOrganization)
                r.Delete("/{id}", datastarHandler.HandleAPIOrganization)
            })

            // Entities API
            r.Route("/entities", func(r chi.Router) {
                r.Get("/", datastarHandler.HandleAPIEntities)
                r.Get("/{id}", datastarHandler.HandleAPIEntity)
            })

            // Fleet API
            r.Route("/fleet", func(r chi.Router) {
                r.Get("/", datastarHandler.HandleAPIFleet)
                r.Put("/update", datastarHandler.HandleAPIFleetUpdate)
                r.Get("/{id}", datastarHandler.HandleAPIFleetEntity)
            })

            // Overwatch API
            r.Route("/overwatch", func(r chi.Router) {
                r.Get("/kv", overwatchHandler.HandleAPIOverwatchKV)
                r.Get("/kv/watch", overwatchHandler.HandleAPIOverwatchKVWatch)
                r.Get("/kv/debug", overwatchHandler.HandleAPIOverwatchKVDebug)
            })

            // Metrics SSE
            r.Get("/metrics/sse", metricsHandler.HandleSSE)

            // Streams SSE
            r.Get("/streams/sse", sseHandler.StreamMessages)

            // Video API
            r.Get("/video/list", videoHandler.HandleAPIVideoList)
        })
    })

    // Mount REST API with Bearer auth
    if apiHandler != nil {
        r.Mount("/api", http.StripPrefix("/api", apiHandler))
    }

    return r
}

// Update session auth to work as chi middleware
func (s *SessionAuth) RequireSessionMiddleware(next http.Handler) http.Handler {
    return s.RequireSession(next)
}
```

#### Step 3: Update Handlers for Chi URL Params

**Edit:** `pkg/services/web/handlers/datastar.go`

```go
import "github.com/go-chi/chi/v5"

func (h *DatastarHandler) HandleOrganizationEdit(w http.ResponseWriter, r *http.Request) {
    // Before (manual parsing):
    // parts := strings.Split(r.URL.Path, "/")
    // orgID := parts[len(parts)-1]

    // After (chi):
    orgID := chi.URLParam(r, "id")

    // ... rest of handler
}

func (h *DatastarHandler) HandleAPIOrganization(w http.ResponseWriter, r *http.Request) {
    orgID := chi.URLParam(r, "id")

    switch r.Method {
    case http.MethodGet:
        h.getOrganization(w, r, orgID)
    case http.MethodDelete:
        h.deleteOrganization(w, r, orgID)
    default:
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
    }
}
```

---

## 6. Simplified NATS Embedded Setup

### Northstar Pattern

Northstar uses `delaneyj/toolbelt/embeddednats` for simpler NATS setup:

```go
// northstar/nats/nats.go
package nats

import (
    "context"
    "github.com/delaneyj/toolbelt/embeddednats"
    natsserver "github.com/nats-io/nats-server/v2/server"
)

func SetupNATS(ctx context.Context) (*embeddednats.Server, error) {
    natsPort, err := getFreeNatsPort()
    if err != nil {
        return nil, fmt.Errorf("error obtaining NATS port: %w", err)
    }

    ns, err := embeddednats.New(ctx, embeddednats.WithNATSServerOptions(&natsserver.Options{
        JetStream: true,
        NoSigs:    true,
        Port:      natsPort,
        StoreDir:  "data/nats",
    }))
    if err != nil {
        return nil, fmt.Errorf("error creating embedded nats server: %w", err)
    }

    ns.WaitForServer()
    return ns, nil
}
```

### Implementation (Optional)

Our current implementation is more feature-rich. Consider keeping it but adding the toolbelt helpers for:

```go
// Add to go.mod
go get github.com/delaneyj/toolbelt

// Use for port allocation
import "github.com/delaneyj/toolbelt"

func getFreePort() (int, error) {
    return toolbelt.FreePort()
}
```

---

## 7. SSE Indicator Components

### Northstar Pattern

Northstar has a reusable SSE loading indicator component:

```go
// northstar/features/common/components/shared_templ.go
func SseIndicator(signalName string) templ.Component {
    // Renders: <div class="loading-dots" data-class="{'loading': $signalName}"></div>
}
```

Used with `data-indicator` attribute:

```html
<button
    data-on:click="@post('/api/todos/1/toggle')"
    data-indicator="fetching1"
>
    Toggle
</button>
@SseIndicator("fetching1")
```

### Implementation

**Create:** `pkg/services/web/features/common/components/indicators.templ`

```templ
package components

import "fmt"

// SSEIndicator shows a loading spinner while an SSE request is in flight
// Usage: data-indicator="myFetchingSignal" on the trigger element
templ SSEIndicator(signalName string) {
    <span
        class="sse-indicator"
        data-show={ fmt.Sprintf("$%s", signalName) }
    >
        <svg class="animate-spin h-4 w-4" viewBox="0 0 24 24">
            <circle
                class="opacity-25"
                cx="12" cy="12" r="10"
                stroke="currentColor"
                stroke-width="4"
                fill="none"
            />
            <path
                class="opacity-75"
                fill="currentColor"
                d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"
            />
        </svg>
    </span>
}

// SSEIndicatorDots shows loading dots while fetching
templ SSEIndicatorDots(signalName string) {
    <span
        class="sse-indicator-dots"
        data-class={ fmt.Sprintf("{'loading': $%s}", signalName) }
    >
        <span class="dot"></span>
        <span class="dot"></span>
        <span class="dot"></span>
    </span>
}

// SSEButton wraps a button with automatic loading state
templ SSEButton(label string, sseAction string, indicatorName string) {
    <button
        class="btn"
        data-on:click={ sseAction }
        data-indicator={ indicatorName }
        data-attrs-disabled={ fmt.Sprintf("$%s", indicatorName) }
    >
        <span data-show={ fmt.Sprintf("!$%s", indicatorName) }>{ label }</span>
        <span data-show={ fmt.Sprintf("$%s", indicatorName) }>Loading...</span>
    </button>
}
```

**Add CSS:** `pkg/services/web/static/css/indicators.css`

```css
.sse-indicator {
    display: none;
    margin-left: 0.5rem;
}

.sse-indicator[data-show="true"] {
    display: inline-flex;
}

.sse-indicator-dots {
    display: none;
}

.sse-indicator-dots.loading {
    display: inline-flex;
    gap: 2px;
}

.sse-indicator-dots .dot {
    width: 4px;
    height: 4px;
    border-radius: 50%;
    background: currentColor;
    animation: dot-pulse 1.4s infinite ease-in-out both;
}

.sse-indicator-dots .dot:nth-child(1) { animation-delay: -0.32s; }
.sse-indicator-dots .dot:nth-child(2) { animation-delay: -0.16s; }
.sse-indicator-dots .dot:nth-child(3) { animation-delay: 0; }

@keyframes dot-pulse {
    0%, 80%, 100% { transform: scale(0); }
    40% { transform: scale(1); }
}
```

---

## 8. Signal Initialization in Templates

### Northstar Pattern

Signals are initialized declaratively in HTML with default values:

```html
<!-- northstar/features/monitor/pages/monitor.templ -->
<div
    id="container"
    data-init="@get('/monitor/events')"
    data-signals="{memTotal:'', memUsed:'', memUsedPercent:'', cpuUser:'', cpuSystem:'', cpuIdle:''}"
>
    <p>Total: <span data-text="$memTotal"></span></p>
    <p>Used: <span data-text="$memUsed"></span></p>
</div>
```

### Implementation

**Update:** `pkg/services/web/features/overwatch/pages/overwatch.templ`

```templ
package pages

import (
    "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/datastar"
    "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/features/common/layouts"
    "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/features/common/components"
)

// Define signal structure as JSON for initialization
const overwatchSignals = `{
    "lastUpdate": "",
    "totalEntities": 0,
    "totalOrgs": 0,
    "_isConnected": false,
    "entityStatesByOrg": {},
    "analytics": {
        "typeCounts": {},
        "statusCounts": {"active": 0, "maintenance": 0, "unknown": 0},
        "threats": {"active": 0, "priority": {"critical": 0, "high": 0}},
        "vision": {"tracked": 0, "detections": 0}
    }
}`

templ OverwatchPage() {
    @layouts.Base("Overwatch Dashboard") {
        <div
            id="overwatch-dashboard"
            data-signals={ overwatchSignals }
            data-init={ datastar.GetSSE("/api/overwatch/kv/watch") }
            class="dashboard-container"
        >
            <!-- Connection Status -->
            <div class="connection-status">
                <span data-show="$_isConnected" class="status-connected">Connected</span>
                <span data-show="!$_isConnected" class="status-disconnected">Connecting...</span>
                <span class="last-update" data-text="$lastUpdate"></span>
            </div>

            <!-- Stats Summary -->
            <div class="stats-bar">
                <div class="stat">
                    <span class="stat-label">Organizations</span>
                    <span class="stat-value" data-text="$totalOrgs"></span>
                </div>
                <div class="stat">
                    <span class="stat-label">Entities</span>
                    <span class="stat-value" data-text="$totalEntities"></span>
                </div>
                <div class="stat">
                    <span class="stat-label">Active Threats</span>
                    <span class="stat-value threat" data-text="$analytics.threats.active"></span>
                </div>
                <div class="stat">
                    <span class="stat-label">Tracked Objects</span>
                    <span class="stat-value" data-text="$analytics.vision.tracked"></span>
                </div>
            </div>

            <!-- Entities Container (populated via SSE) -->
            <div id="entities-container" class="entities-grid">
                <div class="empty-state">
                    Connecting to entity stream...
                </div>
            </div>
        </div>
    }
}
```

**Update:** `pkg/services/web/features/metrics/pages/metrics.templ`

```templ
package pages

import (
    "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/datastar"
    "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/features/common/layouts"
)

const metricsSignals = `{
    "cpuPercent": 0,
    "memoryPercent": 0,
    "memoryUsed": "0 B",
    "memoryTotal": "0 B",
    "goroutines": 0,
    "heapAlloc": "0 B",
    "lastUpdate": "",
    "_connected": false
}`

templ MetricsPage() {
    @layouts.Base("System Metrics") {
        <div
            id="metrics-dashboard"
            data-signals={ metricsSignals }
            data-init={ datastar.GetSSE("/api/metrics/sse") }
            class="metrics-container"
        >
            <h1>System Metrics</h1>

            <div class="metrics-grid">
                <!-- CPU -->
                <div class="metric-card">
                    <h3>CPU Usage</h3>
                    <div class="metric-value">
                        <span data-text="$cpuPercent"></span>%
                    </div>
                    <div class="metric-bar">
                        <div
                            class="metric-bar-fill"
                            data-style="{ width: $cpuPercent + '%' }"
                        ></div>
                    </div>
                </div>

                <!-- Memory -->
                <div class="metric-card">
                    <h3>Memory Usage</h3>
                    <div class="metric-value">
                        <span data-text="$memoryUsed"></span> / <span data-text="$memoryTotal"></span>
                    </div>
                    <div class="metric-bar">
                        <div
                            class="metric-bar-fill"
                            data-style="{ width: $memoryPercent + '%' }"
                        ></div>
                    </div>
                </div>

                <!-- Goroutines -->
                <div class="metric-card">
                    <h3>Goroutines</h3>
                    <div class="metric-value" data-text="$goroutines"></div>
                </div>

                <!-- Heap -->
                <div class="metric-card">
                    <h3>Heap Allocated</h3>
                    <div class="metric-value" data-text="$heapAlloc"></div>
                </div>
            </div>

            <div class="metrics-footer">
                Last updated: <span data-text="$lastUpdate"></span>
            </div>
        </div>
    }
}
```

---

## Implementation Checklist

### Phase 1: Quick Wins (Low Risk)
- [ ] Create typed signal structs (`pkg/services/web/features/*/signals/`)
- [ ] Add SSE indicator components
- [ ] Add Datastar SDK helpers re-export

### Phase 2: Template Updates (Medium Risk)
- [ ] Update templates to use `datastar.GetSSE()` helpers
- [ ] Add signal initialization in templates
- [ ] Add development hot reload

### Phase 3: Architecture (Higher Risk)
- [ ] Migrate to feature-based route registration
- [ ] Consider Chi router migration
- [ ] Consolidate handlers into feature directories

---

## References

- [Northstar Repository](https://github.com/zangster300/northstar)
- [Datastar Go SDK](https://github.com/starfederation/datastar/tree/develop/sdk/go)
- [Chi Router](https://github.com/go-chi/chi)
- [Templ Guide](https://templ.guide/)
- [Delaney's Toolbelt](https://github.com/delaneyj/toolbelt)
