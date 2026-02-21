# MediaMTX Refactor & Cleanup — Task Tracker

> **Branch**: `feat-production-tuneups`
> **Date**: 2026-02-20
> **Objective**: Remove NATS video pipeline entirely. Replace with MediaMTX WHEP integration. Add per-entity video config to DB/fleet registry.

---

## Phase 1: Remove NATS Video Pipeline

### Task 1.1 — Delete Obsolete Video Files
- [ ] Delete `api/handlers/webrtc.go` (~587 lines — pion WebRTC handler + signaling)
- [ ] Delete `api/handlers/video.go` (~265 lines — REST MJPEG stream + video list endpoint)
- [ ] Delete `pkg/services/transcoder/transcoder.go` (~379 lines — ffmpeg MPEG-TS→JPEG pipeline)
- [ ] Delete `pkg/services/workers/video.go` (~188 lines — NATS VideoWorker)

### Task 1.2 — Remove Video/WebRTC References from `api/router.go`
- [ ] Line 27: Delete `videoHandler := handlers.NewVideoHandler(nats)`
- [ ] Line 28: Delete `webrtcHandler := handlers.NewWebRTCHandler(nats)`
- [ ] Lines 29-35: Delete `go func() { webrtcHandler.Start()... }` goroutine block
- [ ] Lines 66-69: Delete `r.Route("/video", func(r chi.Router) { ... })` block
- [ ] Line 72: Delete `r.Post("/webrtc/signal", webrtcHandler.Signal)`

### Task 1.3 — Remove Transcoder from `cmd/microlith/main.go`
- [ ] Line 38: Delete import `"...pkg/services/transcoder"`
- [ ] Lines 206-213: Delete transcoder initialization + goroutine
- [ ] Line 350: Delete `NATS WS:    ws://localhost:8222` from help text

### Task 1.4 — Remove VideoWorker from `pkg/services/workers/manager.go`
- [ ] Line 73: Delete `NewVideoWorker(nc, js, registry),` from workers array

### Task 1.5 — Remove WebSocket + Video Stream from `pkg/services/embedded-nats/nats.go`
- [ ] Lines 22-23: Delete `WSPort int` and `WSAllowedOrigins []string` from Config struct
- [ ] Line 105: Delete `WSPort: getEnvInt("NATS_WS_PORT", 8222),` from DefaultConfig()
- [ ] Line 106: Delete `WSAllowedOrigins: getEnvStringSlice("ALLOWED_ORIGINS"),` from DefaultConfig()
- [ ] Lines 179-189: Delete entire `opts.Websocket = server.WebsocketOpts{...}` block
- [ ] Line 236: Delete `zap.Int("ws_port", en.config.WSPort)` from startup log
- [ ] Line 262: Delete `{shared.StreamVideoFrames, shared.ConsumerVideoProcessor, shared.SubjectVideoAll},` from consumers array
- [ ] Lines 419-433: Delete `CONSTELLATION_VIDEO_FRAMES` stream definition from streams array

### Task 1.6 — Remove Video Subjects from `pkg/shared/subjects.go`
- [ ] Lines 39-42: Delete `SubjectVideo`, `SubjectVideoAll`, `SubjectVideoEntity` constants
- [ ] Line 51: Delete `StreamVideoFrames` constant
- [ ] Line 60: Delete `ConsumerVideoProcessor` constant
- [ ] Lines 140-143: Delete `VideoFrameSubject()` helper function

### Task 1.7 — Remove Video Stream from Metrics + TUI
- [ ] `pkg/metrics/collectors/nats.go` line 56: Delete `"CONSTELLATION_VIDEO_FRAMES"` from streams array
- [ ] `pkg/tui/datasource/nats.go` line 28: Delete `shared.StreamVideoFrames` from streams array

### Task 1.8 — Remove WS Port from Config/Docker
- [ ] `.env.example` lines 36, 39: Remove `NATS_WS_PORT` comment and value
- [ ] `docker-compose.yml` line 9: Delete `"8222:8222"` port mapping
- [ ] `docker-compose.yml` line 19: Delete `NATS_WS_PORT=8222` env var
- [ ] `docker-compose.yml` line 31: Change healthcheck URL from `http://localhost:8222/varz` to `http://localhost:8080/health`
- [ ] `Dockerfile` line 35: Change `EXPOSE 4222 8222 8080` to `EXPOSE 4222 8080`

### Task 1.9 — Clean Up Dependencies
- [ ] Run `go mod tidy` to remove pion/webrtc, yapingcat/gomedia, and ~15 indirect pion deps
- [ ] Verify build: `go build ./...`

---

## Phase 2: Add Video Config to DB & Entity Service

### Task 2.1 — Add `video_config` Column to Schema
- [ ] `db/schema.sql` — After line 57 (`metadata TEXT DEFAULT '{}'`), add:
  ```sql
  video_config TEXT DEFAULT '{}',
  ```
- [ ] Verify JSON structure supports:
  ```json
  {
    "stream_path": "org-alpha/drone-1",
    "stream_protocol": "rtsp",
    "stream_credentials": { "username": "...", "password": "..." },
    "mediamtx_source_url": "rtsp://192.168.1.100:8554/cam1",
    "enabled": true
  }
  ```

### Task 2.2 — Add `VideoConfig` Field to Go Structs
- [ ] `pkg/ontology/entity.go` — Add to `Entity` struct:
  ```go
  VideoConfig string `json:"video_config,omitempty" db:"video_config"`
  ```
- [ ] `pkg/ontology/entity.go` — Add to `CreateEntityRequest`:
  ```go
  VideoConfig map[string]interface{} `json:"video_config,omitempty"`
  ```
- [ ] `pkg/ontology/entity.go` — Add to `UpdateEntityRequest`:
  ```go
  VideoConfig map[string]interface{} `json:"video_config,omitempty"`
  ```
- [ ] `pkg/shared/types.go` — Add to `EntityState` struct (near `StreamPort` ~line 135):
  ```go
  VideoConfig map[string]interface{} `json:"video_config,omitempty"`
  ```

### Task 2.3 — Update Entity Service Queries
- [ ] `api/services/entity.service.go` `CreateEntity()` (line 29): Marshal `req.VideoConfig` to JSON, include in INSERT
- [ ] `api/services/entity.service.go` `ListEntities()` (line 98): Add `video_config` to SELECT columns
- [ ] `api/services/entity.service.go` `ListAllEntities()` (line 122): Add `video_config` to SELECT columns
- [ ] `api/services/entity.service.go` `GetEntity()` (line 146): Add `video_config` to SELECT columns
- [ ] `api/services/entity.service.go` `UpdateEntity()` (line 166): Add `"video_config"` case at line 192 (same JSON marshal as `metadata`)
- [ ] `api/services/entity.service.go` `scanEntity()` (line 313): Add `&entity.VideoConfig` to Scan call

### Task 2.4 — Update KV Sync for Video Config
- [ ] `api/services/entity.service.go` `syncToKV()` (line 357): After setting static fields, parse and set:
  ```go
  if entity.VideoConfig != "" && entity.VideoConfig != "{}" {
      var vc map[string]interface{}
      if json.Unmarshal([]byte(entity.VideoConfig), &vc) == nil {
          state.VideoConfig = vc
      }
  }
  ```

---

## Phase 3: Create MediaMTX API Client

### Task 3.1 — New File `pkg/services/mediamtx/client.go`
- [ ] Create `pkg/services/mediamtx/` directory
- [ ] Implement `Config` struct with `getEnv("MEDIAMTX_API_URL", "")` pattern (match `embedded-nats/nats.go:60-115`)
- [ ] Implement `Client` struct with:
  - `http.Client` (reused, Go stdlib connection pool)
  - `sync.RWMutex` protecting `map[string]PathStatus` cache
  - `context.WithCancel` + `sync.WaitGroup` for goroutine lifecycle
- [ ] Implement `New(cfg *Config) *Client` — returns `nil` when `cfg.APIURL == ""`
- [ ] Implement `Start(ctx context.Context)` — spawns polling goroutine
- [ ] Implement `Stop()` — cancels context, waits for goroutine
- [ ] Implement `fetchAndCache(ctx)` — `GET /v3/paths/list`, builds new map, swaps under write lock
- [ ] Implement `GetStreamStatus(entityID string) (PathStatus, bool)` — RLock map lookup, zero allocs
- [ ] Implement `GetAllStreams() []PathStatus` — allocates slice for JSON endpoint
- [ ] Implement `WHEPEndpoint(streamPath string) string` — returns `{WebRTCURL}/{path}/whep`
- [ ] Implement `extractEntityID(pathName string) string` — `strings.LastIndex("/")`, O(1)
- [ ] All public methods nil-receiver safe (zero-cost when client disabled)

### Task 3.1a — PathStatus Type
- [ ] Define `PathStatus` struct:
  ```go
  type PathStatus struct {
      Name        string
      EntityID    string
      Ready       bool
      ReadyTime   time.Time
      Tracks      []string
      ReaderCount int
      BytesReceived, BytesSent int64
  }
  ```

---

## Phase 4: Rewrite Video Handler + Templates for WHEP

### Task 4.1 — Rewrite `pkg/services/web/handlers/video.go`
- [ ] Replace `VideoHandler` struct: remove `natsEmbedded`, add `mtxClient *mediamtx.Client` and `entitySvc *services.EntityService`
- [ ] Rewrite `NewVideoHandler(mtxClient, entitySvc)` constructor
- [ ] Rewrite `HandleAPIVideoList()` — SSE stream using MediaMTX polling:
  - [ ] No NATS subscription — ticker polls `mtxClient.GetAllStreams()` every 2s
  - [ ] Track `knownStreams map[string]bool` for add/remove detection
  - [ ] New stream: `sse.PatchElements` with Append mode, render `VideoCard(entity, status)`
  - [ ] Removed stream: `sse.PatchElements` with Remove mode
  - [ ] Existing stream: morph `#video-status-{entityID}` with `VideoStatusBadge`
  - [ ] PatchSignals: `activeStreams`, `streamCount`, `lastUpdate`, `_isConnected`
- [ ] Add `HandleAPIVideoStatus()` — JSON endpoint returning `mtxClient.GetAllStreams()`

### Task 4.2 — Rewrite `pkg/services/web/features/video/pages/video.templ`
- [ ] `VideoPage()`: Change signature from `(entityIDs []string, natsAuthToken string)` to `(entityIDs []string, webrtcBaseURL string)`
- [ ] `VideoPanel()`: Keep `data-init="@get('/api/video/list')"`, same signals
- [ ] `VideoCard()`: Change signature to `(entity shared.EntityState, status mediamtx.PathStatus)`
  - [ ] Replace MJPEG `<img>` with `<video>` element (autoplay, muted, playsinline)
  - [ ] Add `data-whep-url` attribute for WHEP connection
  - [ ] Add `data-ignore-morph` on video container
  - [ ] Add inline `<script>` to auto-connect WHEP on render
- [ ] Add new `VideoStatusBadge(entityID, isLive, readerCount)` component
  - [ ] Renders LIVE (green pulse) or OFFLINE (red) badge
  - [ ] Shows viewer count when > 0
  - [ ] Own `id="video-status-{entityID}"` for independent SSE morph
- [ ] `VideoPageStyles()` — Replace JS:
  - [ ] Remove all MJPEG/FPS tracking JS
  - [ ] Add WHEP client: `connectWHEP(entityId, whepUrl)`, `disconnectWHEP(entityId)`
  - [ ] Auto-reconnect on connection failure (3s/5s timeout)
  - [ ] `openVideoFullscreen()` using native `requestFullscreen()` API
- [ ] `VideoPageStyles()` — Replace CSS:
  - [ ] `.video-mjpeg` → `.video-whep`
  - [ ] Add `.badge-live`, `.badge-offline`, `.badge-viewers` styles
  - [ ] Add `@keyframes pulse-live` animation

### Task 4.3 — Update `c4_entity_card.templ` for WHEP
- [ ] `C4VideoPlayer()` (lines 191-207): Change signature to `(entityID string, whepURL string)`
  - [ ] Replace `<img class="video-mjpeg" src=.../>` with `<video id class="video-whep" autoplay muted playsinline>`
  - [ ] Keep `data-ignore-morph` on container
- [ ] `C4EntityCardScript()` (lines 210-335): Update JS
  - [ ] Replace `initFpsTracker` MJPEG logic with `connectWHEP()` call
  - [ ] `toggleEntityDetail()`: expand → `connectWHEP()`, collapse → `disconnectWHEP()`
  - [ ] `openVideoFullscreen()`: use `videoEl.requestFullscreen()` instead of MJPEG clone modal
- [ ] CSS (lines 336-462): `.video-mjpeg` → `.video-whep`, update fullscreen styles for `<video>`

### Task 4.4 — Update `HandleVideoPage` in `pages.go`
- [ ] `pkg/services/web/handlers/pages.go` lines 205-239:
  - [ ] Delete `natsAuthToken := os.Getenv("OVERWATCH_TOKEN")` (line 220)
  - [ ] Add `webrtcBaseURL := os.Getenv("MEDIAMTX_WEBRTC_URL")`
  - [ ] Change both `video_pages.VideoPage(entityIDs, natsAuthToken)` calls to `video_pages.VideoPage(entityIDs, webrtcBaseURL)`

---

## Phase 5: Wire MediaMTX Client Through Server Lifecycle

### Task 5.1 — Update `pkg/services/web/server.go`
- [ ] Add `mtxClient *mediamtx.Client` field to `Server` struct (after line 31)
- [ ] In `NewServer()` (after line 43): Create client:
  ```go
  mtxCfg := mediamtx.DefaultConfig()
  var mtxClient *mediamtx.Client
  if mtxCfg.APIURL != "" {
      mtxClient = mediamtx.New(mtxCfg)
  }
  s.mtxClient = mtxClient
  ```
- [ ] Line 50: Pass `s.mtxClient` to `NewRouter()`
- [ ] In `Start()` (before HTTP serve): `if s.mtxClient != nil { s.mtxClient.Start(ctx) }`
- [ ] In `Stop()` (before HTTP shutdown): `if s.mtxClient != nil { s.mtxClient.Stop() }`

### Task 5.2 — Update `pkg/services/web/router.go`
- [ ] Add `mtxClient *mediamtx.Client` parameter to `NewRouter()` signature
- [ ] Add import for `mediamtx` package
- [ ] Update line 28: `videoHandler := handlers.NewVideoHandler(mtxClient, entitySvc)`
- [ ] Add route after line 106: `mux.Handle("/api/video/status", protect(videoHandler.HandleAPIVideoStatus))`

---

## Phase 6: Update Environment Config

### Task 6.1 — Update `.env.example`
- [ ] After CORS section (line 45), add MediaMTX section:
  ```
  # -----------------------------------------------------------------------------
  # MediaMTX Video Integration (Optional)
  # -----------------------------------------------------------------------------
  # URL for MediaMTX REST API. Enables LIVE/OFFLINE stream status and viewer counts.
  # Leave empty/unset to disable MediaMTX integration (video page shows no streams).
  # MEDIAMTX_API_URL=http://localhost:9997

  # Base URL for MediaMTX WebRTC (WHEP) endpoint.
  # Used to build WHEP URLs for browser video playback.
  # MEDIAMTX_WEBRTC_URL=http://localhost:8889
  ```

---

## Phase 7: Build & Verify

### Task 7.1 — Build Verification
- [ ] `task templ-generate` — Regenerate templ output files
- [ ] `go fmt ./...` — Format all Go files
- [ ] `go vet ./...` — Static analysis
- [ ] `go build ./...` — Compile all packages
- [ ] `go mod tidy` — Remove unused deps (pion, gomedia)
- [ ] `go test ./...` — Run existing tests

### Task 7.2 — Manual Verification (Without MediaMTX)
- [ ] Server starts normally with `MEDIAMTX_API_URL` unset
- [ ] No errors in startup logs
- [ ] Video page loads, shows "No active video streams" empty state
- [ ] No polling goroutines running
- [ ] All other pages (map, fleet, overwatch, streams) work normally

### Task 7.3 — Manual Verification (With MediaMTX)
- [ ] Set `MEDIAMTX_API_URL=http://localhost:9997`
- [ ] Set `MEDIAMTX_WEBRTC_URL=http://localhost:8889`
- [ ] Server starts, MediaMTX client begins polling
- [ ] Video page shows LIVE badges for active MediaMTX streams
- [ ] WHEP player connects and renders WebRTC video in `<video>` element
- [ ] Fullscreen works via native `requestFullscreen()`
- [ ] Stream going offline: card removed from grid, badge updates
- [ ] Stream coming online: card appears in grid with LIVE badge

### Task 7.4 — Entity Video Config Verification
- [ ] Create entity with `video_config` field via REST API:
  ```bash
  curl -X POST http://localhost:8080/api/v1/entities/ \
    -H "Authorization: Bearer <token>" \
    -H "Content-Type: application/json" \
    -d '{"entity_type":"aircraft_multirotor","name":"drone-1","video_config":{"stream_path":"org/drone-1","enabled":true}}'
  ```
- [ ] Verify `video_config` stored in SQLite DB
- [ ] Verify entity state in KV includes `video_config`
- [ ] Update entity video config via PUT
- [ ] Verify fleet page shows entity with video config

---

## Execution Order

```
Phase 1 (Tasks 1.1-1.9) — Remove old video pipeline       ███░░░░░░░
  ↓
Phase 2 (Tasks 2.1-2.4) — Add video_config to DB/service   ██░░░░░░░░
  ↓
Phase 3 (Task 3.1)      — Create MediaMTX API client        ██░░░░░░░░
  ↓
Phase 4 (Tasks 4.1-4.4) — Rewrite handler + templates       ████░░░░░░
  ↓
Phase 5 (Tasks 5.1-5.2) — Wire through server/router        █░░░░░░░░░
  ↓
Phase 6 (Task 6.1)      — Update env config                 █░░░░░░░░░
  ↓
Phase 7 (Tasks 7.1-7.4) — Build, verify, test              ██░░░░░░░░
```

## Files Summary

### Files to DELETE (4)
| File | Reason |
|------|--------|
| `api/handlers/webrtc.go` | Replaced by MediaMTX WHEP |
| `api/handlers/video.go` | Replaced by MediaMTX WHEP |
| `pkg/services/transcoder/transcoder.go` | MediaMTX handles transcoding |
| `pkg/services/workers/video.go` | No more NATS video pipeline |

### Files to CREATE (1)
| File | Purpose |
|------|---------|
| `pkg/services/mediamtx/client.go` | MediaMTX API client + cache + WHEP URL builder |

### Files to MODIFY (16)
| File | Changes |
|------|---------|
| `api/router.go` | Remove video/webrtc routes and handlers |
| `cmd/microlith/main.go` | Remove transcoder init, WS help text |
| `pkg/services/workers/manager.go` | Remove VideoWorker |
| `pkg/services/embedded-nats/nats.go` | Remove WS config, video stream, consumer |
| `pkg/shared/subjects.go` | Remove video subjects/stream/consumer constants |
| `pkg/shared/types.go` | Add VideoConfig to EntityState |
| `pkg/metrics/collectors/nats.go` | Remove video stream from metrics |
| `pkg/tui/datasource/nats.go` | Remove video stream from TUI |
| `pkg/ontology/entity.go` | Add VideoConfig fields |
| `api/services/entity.service.go` | Add video_config to CRUD + KV sync |
| `db/schema.sql` | Add video_config column |
| `pkg/services/web/handlers/video.go` | Full rewrite for MediaMTX |
| `pkg/services/web/handlers/pages.go` | Update HandleVideoPage params |
| `pkg/services/web/features/video/pages/video.templ` | WHEP player + status badges |
| `pkg/services/web/features/common/components/c4_entity_card.templ` | WHEP replaces MJPEG |
| `pkg/services/web/server.go` | Add MediaMTX client lifecycle |
| `pkg/services/web/router.go` | Wire MediaMTX client + new route |
| `.env.example` | Add MEDIAMTX vars, remove WS vars |
| `docker-compose.yml` | Remove 8222 port + WS env |
| `Dockerfile` | Remove EXPOSE 8222 |
