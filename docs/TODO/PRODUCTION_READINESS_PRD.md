# Production Readiness PRD — Implementation Plan

> **Current Readiness: ~80-85%** (Phase 1 complete)
> **Generated: 2026-02-25** | **Updated: 2026-02-25**
> **Branch: feat-production-tuneups**
> **Total Tasks: 25 | Completed: 16 (Phase 1) | Remaining: 9 (Phases 2-4 + Quick Wins)**

---

## Table of Contents

- [Execution Overview](#execution-overview)
- [Dependency Graph](#dependency-graph)
- [Agent Team Assignments](#agent-team-assignments)
- [Phase 1: Security Hardening (14 tasks)](#phase-1-security-hardening)
- [Phase 2: Kubernetes Foundation (1 task)](#phase-2-kubernetes-foundation)
- [Phase 3: NATS Clustering (3 tasks)](#phase-3-nats-clustering)
- [Phase 4: Operational Hardening (4 tasks)](#phase-4-operational-hardening)
- [Quick Wins (3 tasks)](#quick-wins)
- [Critical Path](#critical-path)
- [Essential Files Reference](#essential-files-reference)

---

## Execution Overview

```
PHASE 1: Security Hardening         20 of 25 tasks can start immediately
├── 3 CRITICAL (production blockers)
├── 8 HIGH
├── 5 MEDIUM
└── 3 LOW

PHASE 2: Kubernetes Foundation       Blocked by Phase 1 HTTP chain
PHASE 3: NATS Clustering            2 immediate, 1 blocked
PHASE 4: Operational Hardening      2 immediate, 1 blocked
```

---

## Dependency Graph

```
                    INDEPENDENT (start immediately)
    ┌──────────────────────────────────────────────────────┐
    │  #1  SQL injection fix          (tiny)               │
    │  #3  Body size limits           (small)              │
    │  #4  CORS deny default          (small)              │
    │  #6  API scope enforcement      (small)              │
    │  #7  Error sanitization         (small)              │
    │  #8  API key HMAC upgrade       (medium)             │
    │  #9  Session persistence        (medium)             │
    │  #13 WebAuthn race fix          (small)              │
    │  #14 Metrics/pprof protection   (small)              │
    │  #15 Invite org mismatch fix    (small)              │
    │  #16 Real /health endpoint      (small)              │
    │  #22 Request ID middleware      (small)              │
    │  #23 Shutdown timeout config    (tiny)               │
    │  #24 DB dir permissions         (tiny)               │
    │  #25 Bootstrap URL scheme       (tiny)               │
    └──────────────────────────────────────────────────────┘

                    SEQUENTIAL CHAINS
    ┌──────────────────────────────────────────────────────┐
    │  Lane A: HTTP Server                                 │
    │  #2  HTTP timeouts                                   │
    │    └──> #5  Security headers middleware              │
    │           └──> #12 Panic recovery middleware         │
    │                  └──> #20 Helm chart (Phase 2)       │
    │                                                      │
    │  Lane B: NATS                                        │
    │  #10 NATS bind + auth ──┐                            │
    │  #11 NATS TLS fix ──────┤                            │
    │  #17 Subject bug fix ───┼──> #19 External NATS      │
    │  #18 KV writes enable ──┘                            │
    │                                                      │
    │  Lane E: Health                                      │
    │  #16 Wire /health                                    │
    │    └──> #21 /ready + /live endpoints                 │
    └──────────────────────────────────────────────────────┘
```

---

## Agent Team Assignments

| Team | Focus | Tasks | Parallelism |
|---|---|---|---|
| **Team 1** | HTTP / Middleware | #2 → #5 → #12, #22 | Sequential chain + 1 independent |
| **Team 2** | NATS Security & Clustering | #10, #11, #17, #18 → #19 | 4 parallel, then 1 blocked |
| **Team 3** | Auth & Sessions | #9, #8, #13 | All independent |
| **Team 4** | API Security | #1, #3, #4, #6, #7, #14, #15 | All independent (7 quick tasks) |
| **Team 5** | Health, K8s & Ops | #16 → #21, #23, #24, #25, #20 | Mixed: quick wins + Helm chart |

---

## Phase 1: Security Hardening — COMPLETED

### Task #1 — P1-C3: Fix SQL injection in columnExists — DONE

| Field | Value |
|---|---|
| **Priority** | CRITICAL |
| **Effort** | Small |
| **Risk** | Low |
| **Dependencies** | None |
| **Team** | 4 |

**Current State**

`db/service.go:354-355`:
```go
func (s *Service) columnExists(table, column string) bool {
    rows, err := s.DB.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
```

The `table` parameter is interpolated directly into SQL via `fmt.Sprintf`. Currently only called with the hardcoded string `"webauthn_sessions"` at line 342, but the function signature accepts arbitrary input.

**Implementation**

Replace the entire function with a parameterized query (preferred — eliminates the vulnerability class entirely):

```go
func (s *Service) columnExists(table, column string) bool {
    var count int
    err := s.DB.QueryRow(
        "SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?",
        table, column,
    ).Scan(&count)
    if err != nil {
        return false
    }
    return count > 0
}
```

Verify `pragma_table_info()` works with `modernc.org/sqlite` (it is standard SQLite3, should work).

**Files to Modify**
- `db/service.go` — replace `columnExists` implementation (lines 354-375)

**Testing**
- `go test ./...`
- Start server, verify schema migration still works (fresh DB creates `user_ref` column)
- Verify `webauthn_sessions.user_ref` column detection on existing DB

---

### Task #2 — P1-H1: Add HTTP server timeouts — DONE

| Field | Value |
|---|---|
| **Priority** | HIGH |
| **Effort** | Small |
| **Risk** | Low |
| **Dependencies** | None |
| **Team** | 1 |
| **Blocks** | #5 (security headers), #20 (Helm chart) |

**Current State**

`pkg/services/web/server.go:121-124`:
```go
s.server = &http.Server{
    Addr:    s.bindAddr,
    Handler: s.mux,
}
```

No `ReadTimeout`, `WriteTimeout`, `ReadHeaderTimeout`, or `IdleTimeout`. Vulnerable to Slowloris and connection exhaustion.

**Implementation**

```go
s.server = &http.Server{
    Addr:              s.bindAddr,
    Handler:           s.mux,
    ReadHeaderTimeout: 5 * time.Second,
    ReadTimeout:       30 * time.Second,
    WriteTimeout:      0,              // Disabled — SSE endpoints are long-lived
    IdleTimeout:       120 * time.Second,
}
```

`WriteTimeout: 0` is intentional. The app has three SSE endpoints (`/api/streams/sse`, `/api/overwatch/kv/watch`, `/api/metrics/sse`) that are long-lived connections. Setting a server-level `WriteTimeout` would kill these connections. Traefik handles upstream timeouts in production. The `ReadHeaderTimeout` and `ReadTimeout` protect against Slowloris.

**Files to Modify**
- `pkg/services/web/server.go:121-124`

**Testing**
- Start server, verify all pages load
- Verify SSE streams (`/api/streams/sse`) work and stay open
- Verify `curl --limit-rate 1` for headers gets dropped after 5s

---

### Task #3 — P1-H2: Add request body size limits — DONE

| Field | Value |
|---|---|
| **Priority** | HIGH |
| **Effort** | Small |
| **Risk** | Low |
| **Dependencies** | None |
| **Team** | 4 |

**Current State**

All `json.NewDecoder(r.Body).Decode()` calls in `api/handlers/entity.go:46-48`, `api/handlers/organization.go:39-41`, and `entity.go:135` operate on raw `r.Body` with no size cap. A client can send gigabytes to exhaust server memory.

**Implementation**

Create middleware and apply to all API routes:

```go
// api/middleware/bodylimit.go
package middleware

import "net/http"

// MaxBodySize limits the request body to the given number of bytes.
func MaxBodySize(maxBytes int64) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
            next.ServeHTTP(w, r)
        })
    }
}
```

Apply in `api/router.go`:
```go
r.Use(middleware.MaxBodySize(1 << 20)) // 1 MB limit
```

Size rationale: 1 MB is generous for JSON API payloads. NATS max payload is 512KB, so 1MB covers any NATS-bound payload with headroom.

**Files to Create**
- `api/middleware/bodylimit.go`

**Files to Modify**
- `api/router.go` — add `r.Use(middleware.MaxBodySize(1 << 20))`

**Testing**
- Send request with >1MB body, verify 413 response
- Normal entity/org CRUD still works
- `go test ./...`

---

### Task #4 — P1-H3: Fix CORS to deny by default — DONE

| Field | Value |
|---|---|
| **Priority** | HIGH |
| **Effort** | Small |
| **Risk** | Medium (breaks dev setups without `.env`) |
| **Dependencies** | None |
| **Team** | 4 |

**Current State**

`api/middleware/auth.go:232-244`:
```go
func IsOriginAllowed(origin string) bool {
    origins := os.Getenv("ALLOWED_ORIGINS")
    if origins == "" || origins == "*" {
        return true  // WIDE OPEN when not configured
    }
    // ...
}
```

Also `GetAllowedOrigins()` at line 218-230 returns empty slice when unset, which the CORS middleware in `api/middleware/cors.go:8-25` treats as "allow all". The CORS middleware (`r.Use(middleware.CORS)`) is applied globally in `api/router.go:32` to the REST API chi router.

**Implementation**

```go
func IsOriginAllowed(origin string) bool {
    origins := os.Getenv("ALLOWED_ORIGINS")
    if origins == "" {
        return false // Deny when not configured
    }
    if origins == "*" {
        return true // Explicit wildcard still allowed for dev
    }
    for _, allowed := range strings.Split(origins, ",") {
        if strings.TrimSpace(allowed) == origin {
            return true
        }
    }
    return false
}
```

Update `.env.example`:
```env
ALLOWED_ORIGINS=http://localhost:8080
```

**Files to Modify**
- `api/middleware/auth.go:232-244` — change `IsOriginAllowed`
- `.env.example` — add `ALLOWED_ORIGINS=http://localhost:8080`

**Testing**
- Server WITHOUT `ALLOWED_ORIGINS` → cross-origin requests rejected
- Server WITH `ALLOWED_ORIGINS=http://localhost:8080` → allowed
- Server WITH `ALLOWED_ORIGINS=*` → all allowed (dev mode)

---

### Task #5 — P1-H4: Add security response headers middleware — DONE

| Field | Value |
|---|---|
| **Priority** | HIGH |
| **Effort** | Small |
| **Risk** | Low |
| **Dependencies** | #2 (HTTP timeouts — both modify `server.go` handler chain) |
| **Team** | 1 |
| **Blocks** | #12 (panic recovery) |

**Current State**

`pkg/services/web/router.go` — no security headers anywhere. The `http.ServeMux` has no global middleware chain. Pages can be embedded in iframes (clickjacking), browsers will MIME-sniff responses, no HSTS.

**Implementation**

```go
// pkg/services/web/middleware.go (new file)
package web

import "net/http"

// SecurityHeaders adds security response headers to all responses.
func SecurityHeaders(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("X-Content-Type-Options", "nosniff")
        w.Header().Set("X-Frame-Options", "DENY")
        w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
        w.Header().Set("Permissions-Policy", "geolocation=(), camera=(), microphone=()")
        w.Header().Set("X-XSS-Protection", "0") // Disabled per modern best practice
        next.ServeHTTP(w, r)
    })
}
```

Apply in `pkg/services/web/server.go` when creating the `http.Server`:
```go
s.server = &http.Server{
    Addr:    s.bindAddr,
    Handler: SecurityHeaders(s.mux),
    // ... timeouts from #2
}
```

HSTS is intentionally NOT set here — Traefik sets it in production. CSP deferred to Traefik to avoid breaking inline scripts during development.

**Files to Create**
- `pkg/services/web/middleware.go`

**Files to Modify**
- `pkg/services/web/server.go:121-124` — wrap `s.mux` with `SecurityHeaders()`

**Testing**
- `curl -I http://localhost:8080/login` — verify `X-Content-Type-Options`, `X-Frame-Options` present
- All pages still render correctly

---

### Task #6 — P1-H5: Enforce API key scopes on routes — DONE

| Field | Value |
|---|---|
| **Priority** | HIGH |
| **Effort** | Small |
| **Risk** | Medium (may break keys with wrong scopes) |
| **Dependencies** | None |
| **Team** | 4 |

**Current State**

`api/router.go:43-58` — all routes use `apiKeyAuth.Authenticate` but none use `RequireScope`. The `RequireScope` middleware exists and works (`api/middleware/apikey.go:108-119`) — it's just never applied. A read-only key with `scopes=["entities:read"]` has silent write access to everything.

**Implementation**

```go
// Organizations
r.Route("/organizations", func(r chi.Router) {
    r.Use(apiKeyAuth.Authenticate)
    r.With(middleware.RequireScope("organizations:read")).Get("/", orgHandler.List)
    r.With(middleware.RequireScope("organizations:write")).Post("/", orgHandler.Create)
    r.With(middleware.RequireScope("organizations:write")).Delete("/", orgHandler.Delete)
})

// Entities
r.Route("/entities", func(r chi.Router) {
    r.Use(apiKeyAuth.Authenticate)
    r.With(middleware.RequireScope("entities:read")).Get("/", entityHandler.ListOrGet)
    r.With(middleware.RequireScope("entities:write")).Post("/", entityHandler.Create)
    r.With(middleware.RequireScope("entities:write")).Put("/", entityHandler.Update)
    r.With(middleware.RequireScope("entities:write")).Delete("/", entityHandler.Delete)
})
```

Note: `hasScope()` at `apikey.go:162-169` already treats `"admin"` as a super-scope, so admin keys continue to work everywhere.

**Pre-flight check:** Before deploying, verify all existing API keys in the database have correct scopes. Keys created via the admin UI default to `"admin"` scope — confirm this in `api/services/apikey.service.go`.

**Files to Modify**
- `api/router.go:43-58` — add `RequireScope` middleware to each route

**Testing**
- Create key with `scopes=["entities:read"]` → GET works, POST returns 403
- Create key with `scopes=["admin"]` → everything works
- `go test ./...`

---

### Task #7 — P1-M1: Sanitize error responses — DONE

| Field | Value |
|---|---|
| **Priority** | MEDIUM |
| **Effort** | Small |
| **Risk** | Low |
| **Dependencies** | None |
| **Team** | 4 |

**Current State**

Raw `err.Error()` strings passed directly to API clients on 500 responses across multiple files. SQLite errors expose column names, constraint names, table names. Example: `UNIQUE constraint failed: users.email`.

**Leaking call sites identified:**

`api/handlers/entity.go`:
- Line 53: `responses.SendError(w, http.StatusInternalServerError, "CREATE_FAILED", err.Error())`
- Line 91: `"GET_FAILED", err.Error()`
- Line 102: `"LIST_FAILED", err.Error()`
- Line 145: `"UPDATE_FAILED", err.Error()`
- Line 182: `"DELETE_FAILED", err.Error()`

`api/handlers/organization.go`:
- Line 46: `"CREATE_FAILED", err.Error()`
- Line 67: `"LIST_FAILED", err.Error()`
- Line 86: `"GET_FAILED", err.Error()`
- Line 120: `"DELETE_FAILED", err.Error()`

`pkg/services/web/handlers/datastar.go`: ~20 sites using `http.Error(w, err.Error(), 500)`

`pkg/services/web/handlers/pages.go`: ~6 sites

`pkg/services/web/handlers/overwatch.go`: ~4 sites

**Implementation**

Pattern to apply everywhere:
```go
// BEFORE (leaks):
responses.SendError(w, http.StatusInternalServerError, "CREATE_FAILED", err.Error())

// AFTER (safe):
logger.Errorw("Failed to create entity", "error", err, "org_id", orgID)
responses.SendError(w, http.StatusInternalServerError, "CREATE_FAILED", "An internal error occurred")
```

For web handlers:
```go
// BEFORE:
http.Error(w, err.Error(), http.StatusInternalServerError)

// AFTER:
logger.Errorw("Handler error", "error", err, "path", r.URL.Path)
http.Error(w, "Internal server error", http.StatusInternalServerError)
```

Note: `api/handlers/entity.go:48` and `organization.go:41` already correctly return `err.Error()` for 400 (bad request) JSON parse errors — these are safe to keep since they only contain JSON syntax errors, not internal state.

**Files to Modify**
- `api/handlers/entity.go` — all 500 error paths
- `api/handlers/organization.go` — all 500 error paths
- `pkg/services/web/handlers/datastar.go` — all `http.Error(w, err.Error(), 500)` calls
- `pkg/services/web/handlers/pages.go` — all error paths
- `pkg/services/web/handlers/overwatch.go` — all error paths

**Testing**
- Trigger a 500 error → verify response has generic message, not internal error
- Check server logs → verify real error IS logged with context

---

### Task #8 — P1-H6: Upgrade API key hashing to HMAC-SHA256 — DONE

| Field | Value |
|---|---|
| **Priority** | HIGH |
| **Effort** | Medium |
| **Risk** | Medium (migration needed for existing keys) |
| **Dependencies** | None |
| **Team** | 3 |

**Current State**

Two separate SHA-256 hash sites that must stay in sync:
1. `api/middleware/apikey.go:142-145` — `hashKey()` used on every auth request
2. `api/services/apikey.service.go:66-67` — used at key creation time

Plain SHA-256 is vulnerable to GPU brute force if DB is exfiltrated.

**Implementation**

```go
// api/middleware/apikey.go
import "crypto/hmac"

func hashKey(raw string) string {
    secret := os.Getenv("OVERWATCH_KEY_HASH_SECRET")
    if secret == "" {
        // Fallback for development — log a warning on first call
        logger.Warn("OVERWATCH_KEY_HASH_SECRET not set, using insecure SHA-256 hash")
        h := sha256.Sum256([]byte(raw))
        return hex.EncodeToString(h[:])
    }
    mac := hmac.New(sha256.New, []byte(secret))
    mac.Write([]byte(raw))
    return hex.EncodeToString(mac.Sum(nil))
}
```

**Migration strategy (dual-check with transparent upgrade):**

Add a `hash_version` column to `api_keys`:
```sql
ALTER TABLE api_keys ADD COLUMN hash_version INTEGER NOT NULL DEFAULT 1;
-- 1 = SHA-256, 2 = HMAC-SHA256
```

In `authenticateDBKey()`, if the HMAC lookup fails, try plain SHA-256:
```go
hash := hashKey(raw) // HMAC
row := m.db.QueryRow(`SELECT ... FROM api_keys WHERE key_hash = ?`, hash)
if errors.Is(err, sql.ErrNoRows) && os.Getenv("OVERWATCH_KEY_HASH_SECRET") != "" {
    // Try legacy SHA-256
    legacyHash := sha256Hex(raw)
    row = m.db.QueryRow(`SELECT ... FROM api_keys WHERE key_hash = ? AND hash_version = 1`, legacyHash)
    if err == nil {
        // Transparently upgrade to HMAC
        go func() {
            m.db.Exec(`UPDATE api_keys SET key_hash = ?, hash_version = 2 WHERE key_hash = ?`, hash, legacyHash)
        }()
    }
}
```

**Files to Modify**
- `api/middleware/apikey.go` — update `hashKey`, add migration in `authenticateDBKey`
- `api/services/apikey.service.go` — use shared `hashKey` for creation
- `db/service.go` — add migration for `hash_version` column
- `.env.example` — add `OVERWATCH_KEY_HASH_SECRET=`

**Testing**
- Create key with `OVERWATCH_KEY_HASH_SECRET` set → verify auth works
- Verify old SHA-256 keys still work via fallback
- Verify old keys get transparently migrated on use
- Unset `OVERWATCH_KEY_HASH_SECRET` → verify fallback warning + plain SHA-256

---

### Task #9 — P1-C1: Persist sessions to SQLite — DONE

| Field | Value |
|---|---|
| **Priority** | CRITICAL |
| **Effort** | Medium |
| **Risk** | Medium |
| **Dependencies** | None |
| **Team** | 3 |

**Current State**

`api/middleware/auth.go:29-37`:
```go
type SessionAuth struct {
    sessions map[string]*session
    mu       sync.RWMutex
}
```

All sessions in a Go map — lost on restart. No audit trail. No cross-restart revocation.

Methods to reimplement: `CreateSessionForUser`, `ValidateSession`, `DestroySession`, `getSession`, `ClearPasskeySetup`, `IsPasskeySetup`, `cleanupExpiredSessions`.

Callers of `NewSessionAuth()`: `pkg/services/web/server.go:54` (single call site).

Callers of `CreateSessionForUser`: `handlers/invite.go`, `handlers/auth.go`, `cmd/microlith/main.go:~404`.

**Implementation**

### 1. Add `sessions` table to `db/schema.sql`
```sql
CREATE TABLE IF NOT EXISTS sessions (
    session_token TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'viewer',
    org_id TEXT NOT NULL DEFAULT '',
    needs_passkey_setup INTEGER NOT NULL DEFAULT 0,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    FOREIGN KEY (user_id) REFERENCES users(user_id)
);
CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);
```

### 2. Refactor `SessionAuth`
```go
type SessionAuth struct {
    db *sql.DB
}

func NewSessionAuth(db *sql.DB) *SessionAuth {
    return &SessionAuth{db: db}
}
```

### 3. Rewrite methods
- `CreateSessionForUser` → `INSERT INTO sessions`
- `ValidateSession` → `SELECT 1 WHERE token = ? AND expires_at > datetime('now')`
- `DestroySession` → `DELETE WHERE session_token = ?`
- `getSession` → `SELECT` with expiry check
- `ClearPasskeySetup` → `UPDATE needs_passkey_setup = 0`
- `IsPasskeySetup` → `SELECT needs_passkey_setup`
- `cleanupExpiredSessions` → `DELETE WHERE expires_at < datetime('now')` (periodic goroutine)

### 4. Update callers
`pkg/services/web/server.go:54`:
```go
// Before:
sessionAuth := middleware.NewSessionAuth()
// After:
sessionAuth := middleware.NewSessionAuth(dbService.GetDB())
```

### 5. Periodic cleanup
Start a goroutine in web server `Start()` that runs cleanup every 10 minutes.

**Files to Modify**
- `db/schema.sql` — add `sessions` table
- `api/middleware/auth.go` — rewrite `SessionAuth` to use `*sql.DB`
- `pkg/services/web/server.go:54` — pass `*sql.DB`

**Testing**
- Login, restart server, verify session survives restart
- Verify session expiry works
- Verify passkey setup flow works
- `go test ./...`

---

### Task #10 — P1-C2: Secure NATS server — DONE

| Field | Value |
|---|---|
| **Priority** | CRITICAL |
| **Effort** | Medium |
| **Risk** | Medium |
| **Dependencies** | None |
| **Team** | 2 |
| **Blocks** | #19 (external NATS) |

**Current State**

`pkg/services/embedded-nats/nats.go`:
- `DefaultConfig()` line 85: `Host: shared.GetEnv("NATS_HOST", "0.0.0.0")` — binds all interfaces
- `StartEmbedded()` lines 132-155: no `Authorization`, `Username`, `Password`, `Nkeys`, or `Accounts` in server options
- NKey users added AFTER server starts via `ReloadOptions()` — window of unauthenticated access
- `connect()` line 235: `nats://localhost:{port}` — no auth credentials on internal client

**Implementation**

### 1. Default bind to localhost
```go
Host: shared.GetEnv("NATS_HOST", "127.0.0.1"), // was "0.0.0.0"
```

### 2. Generate auth token for internal connections
```go
func (en *EmbeddedNATS) StartEmbedded(ctx context.Context) error {
    // Generate internal auth token
    tokenBytes := make([]byte, 32)
    crypto_rand.Read(tokenBytes)
    en.authToken = hex.EncodeToString(tokenBytes)

    opts := &server.Options{
        // ... existing options ...
        Authorization: en.authToken,
    }
    // ...
}

func (en *EmbeddedNATS) connect() error {
    url := fmt.Sprintf("nats://localhost:%d", en.config.Port)
    en.connectOpts = append(en.connectOpts, nats.Token(en.authToken))
    // ...
}
```

### 3. Expose auth token for external clients
```go
func (en *EmbeddedNATS) AuthToken() string {
    return en.authToken
}
```

### 4. Load NKeys before server accepts connections (optional, stretch)
Move `RestoreNKeyUsers` call from `web/server.go:69-80` to before `ReadyForConnections` in `StartEmbedded()`.

**Files to Modify**
- `pkg/services/embedded-nats/nats.go` — `DefaultConfig()`, `StartEmbedded()`, `connect()`
- `.env.example` — add `NATS_HOST=0.0.0.0` (commented, for edge device access)

**Testing**
- Start server → verify NATS binds to 127.0.0.1
- `nats sub '>'` from terminal → should FAIL without auth token
- Internal streams/workers still function
- Edge device NKey auth still works

---

### Task #11 — P1-NATS-TLS: Fix NATS TLS config bug — DONE

| Field | Value |
|---|---|
| **Priority** | HIGH |
| **Effort** | Small |
| **Risk** | Low |
| **Dependencies** | None |
| **Team** | 2 |
| **Blocks** | #19 (external NATS) |

**Current State**

`pkg/services/embedded-nats/nats.go`:
```go
// DefaultConfig() reads the flag but NEVER reads cert paths:
EnableTLS: shared.GetEnv("NATS_ENABLE_TLS", "false") == "true",
// TLSCert and TLSKey are ALWAYS empty strings

// Guard at line ~165 is ALWAYS FALSE:
if en.config.EnableTLS && en.config.TLSCert != "" && en.config.TLSKey != "" {
```

The TLS feature is completely dead code.

**Implementation**

In `DefaultConfig()` add:
```go
TLSCert: shared.GetEnv("NATS_TLS_CERT", ""),
TLSKey:  shared.GetEnv("NATS_TLS_KEY", ""),
```

Update `.env.example`:
```env
# NATS TLS (for edge device connections)
# NATS_ENABLE_TLS=true
# NATS_TLS_CERT=/path/to/nats.crt
# NATS_TLS_KEY=/path/to/nats.key
```

Verify the existing TLS setup code at the guard condition actually creates proper `tls.Config` and sets `opts.TLSConfig`.

**Files to Modify**
- `pkg/services/embedded-nats/nats.go` — `DefaultConfig()`
- `.env.example`

**Testing**
- Generate certs with `task generate-certs`
- Set `NATS_ENABLE_TLS=true`, `NATS_TLS_CERT=...`, `NATS_TLS_KEY=...`
- Verify NATS starts with TLS
- Verify internal client connects with TLS

---

### Task #12 — P1-PANIC: Add panic recovery middleware — DONE

| Field | Value |
|---|---|
| **Priority** | HIGH |
| **Effort** | Small |
| **Risk** | Low |
| **Dependencies** | #5 (security headers — shares middleware.go and handler chain) |
| **Team** | 1 |

**Current State**

No panic recovery anywhere in the HTTP stack. A panic in any handler kills the entire process.

**Implementation**

Add to `pkg/services/web/middleware.go`:

```go
import "runtime/debug"

// RecoverPanic recovers from panics in HTTP handlers, logs the stack trace,
// and returns a 500 response instead of crashing the process.
func RecoverPanic(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        defer func() {
            if err := recover(); err != nil {
                logger.Errorw("Panic recovered in HTTP handler",
                    "error", err,
                    "path", r.URL.Path,
                    "method", r.Method,
                    "stack", string(debug.Stack()),
                )
                http.Error(w, "Internal Server Error", http.StatusInternalServerError)
            }
        }()
        next.ServeHTTP(w, r)
    })
}
```

Apply as outermost wrapper in `pkg/services/web/server.go`:
```go
Handler: RecoverPanic(SecurityHeaders(s.mux)),
```

**Files to Modify**
- `pkg/services/web/middleware.go` — add `RecoverPanic`
- `pkg/services/web/server.go` — update handler chain

**Testing**
- Temporarily add `panic("test")` in a handler → verify 500 response, no crash
- Verify stack trace logged

---

### Task #13 — P1-H7: Fix WebAuthn session race condition — DONE

| Field | Value |
|---|---|
| **Priority** | HIGH |
| **Effort** | Small |
| **Risk** | Low |
| **Dependencies** | None |
| **Team** | 3 |

**Current State**

`api/services/auth.service.go:364-405`:
```go
func (s *AuthService) GetWebAuthnSession(ceremony, key string) (*webauthn.SessionData, string, error) {
    // SELECT session
    err := s.db.QueryRow(`SELECT ... FROM webauthn_sessions WHERE ceremony = ? AND session_key = ?`, ...)

    // DELETE immediately (line 383) — BEFORE expiry check
    _, _ = s.db.Exec(`DELETE FROM webauthn_sessions WHERE ceremony = ? AND session_key = ?`, ceremony, key)

    // Check expiry (line 388) — AFTER delete
    if time.Now().After(exp) {
        return nil, "", fmt.Errorf("webauthn session: %w", shared.ErrExpired)
    }
}
```

Problems:
1. Session deleted before expiry check — error type reveals validity (timing oracle)
2. Race condition: two concurrent requests both read the row, first DELETE succeeds, second is no-op, both proceed past expiry check

**Implementation (atomic approach)**

```go
func (s *AuthService) GetWebAuthnSession(ceremony, key string) (*webauthn.SessionData, string, error) {
    var dataJSON string
    var expiresAt string
    var userRef sql.NullString

    // Read session
    err := s.db.QueryRow(
        `SELECT session_data, user_ref, expires_at FROM webauthn_sessions
         WHERE ceremony = ? AND session_key = ?`,
        ceremony, key,
    ).Scan(&dataJSON, &userRef, &expiresAt)

    if errors.Is(err, sql.ErrNoRows) {
        return nil, "", fmt.Errorf("webauthn session: %w", shared.ErrNotFound)
    }
    if err != nil {
        return nil, "", fmt.Errorf("failed to query webauthn session: %w", err)
    }

    // Check expiry BEFORE deleting
    exp, parseErr := time.Parse(time.RFC3339, expiresAt)
    if parseErr == nil && time.Now().After(exp) {
        // Clean up expired row
        s.db.Exec(`DELETE FROM webauthn_sessions WHERE ceremony = ? AND session_key = ?`, ceremony, key)
        return nil, "", fmt.Errorf("webauthn session: %w", shared.ErrNotFound) // same error as not-found
    }

    // Delete (single use)
    s.db.Exec(`DELETE FROM webauthn_sessions WHERE ceremony = ? AND session_key = ?`, ceremony, key)

    // ... unmarshal and return
}
```

Key: Return `ErrNotFound` (not `ErrExpired`) for expired sessions to prevent timing oracle.

**Files to Modify**
- `api/services/auth.service.go` — reorder expiry check and delete (~lines 382-391)

**Testing**
- WebAuthn login flow end-to-end
- Expired sessions return same error as non-existent sessions

---

### Task #14 — P1-M4: Protect metrics and pprof behind admin role — DONE

| Field | Value |
|---|---|
| **Priority** | MEDIUM |
| **Effort** | Small |
| **Risk** | Low |
| **Dependencies** | None |
| **Team** | 4 |

**Current State**

`pkg/services/web/router.go`:
- Line 49: `/metrics` completely unauthenticated — exposes Go runtime, GC, memory, goroutine counts
- Line 78: pprof wrapped with `protect` (session only, no role check) — any viewer can dump heap, goroutines, CPU profiles

**Implementation**

**For pprof:** Add admin role check:
```go
adminProtect := func(h http.HandlerFunc) http.Handler {
    return sessionAuth.RequireSession(requireAdminRole(http.HandlerFunc(h)))
}
metrics.RegisterPProf(mux, adminProtect)
```

May need a `requireAdminRole` middleware. Check if `middleware.RequireRole` exists; if not, create it:
```go
func requireAdminRole(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        role := middleware.UserRoleFromContext(r.Context())
        if role != "admin" {
            http.Error(w, "Forbidden", http.StatusForbidden)
            return
        }
        next.ServeHTTP(w, r)
    })
}
```

**For `/metrics`:** Leave unauthenticated. It will be protected by `NetworkPolicy` in K8s (Phase 2). Add a comment documenting this decision:
```go
// /metrics is intentionally unauthenticated for Prometheus ServiceMonitor scraping.
// Access is restricted via NetworkPolicy in Kubernetes (see deploy/helm/).
```

**Files to Modify**
- `pkg/services/web/router.go` — add role check to pprof, add comment to metrics

**Testing**
- Login as viewer → pprof returns 403
- Login as admin → pprof works
- `/metrics` still accessible without auth

---

### Task #15 — P1-M6: Fix invite org ID mismatch — DONE

| Field | Value |
|---|---|
| **Priority** | MEDIUM |
| **Effort** | Small |
| **Risk** | Low |
| **Dependencies** | None |
| **Team** | 4 |

**Current State**

`pkg/services/web/handlers/invite.go:83-151`:
When an existing user accepts an invite from a different org:
1. `existing, err := h.userSvc.GetByEmail(invite.Email)` finds user from Org A
2. `user = existing` uses their record (Org A)
3. Session created with `invite.OrgID` (Org B) — mismatch
4. Invited role silently ignored — `existing.Role` used instead

**Implementation**

```go
if existing != nil {
    if existing.OrgID != invite.OrgID {
        http.Error(w, "User already belongs to another organization", http.StatusConflict)
        return
    }
    user = existing
} else {
    user = &services.User{
        OrgID: invite.OrgID,
        Role:  invite.Role,
        // ...
    }
}
```

**Files to Modify**
- `pkg/services/web/handlers/invite.go` — add org ID validation in `HandleFinalizeInvite`

**Testing**
- Create user in Org A, invite from Org B for same email → verify rejection
- Normal invite flow (new user) still works
- Bootstrap admin flow still works

---

### Task #16 — P1-HEALTH: Wire real health check to /health — DONE

| Field | Value |
|---|---|
| **Priority** | HIGH |
| **Effort** | Small |
| **Risk** | Low |
| **Dependencies** | None |
| **Team** | 5 |
| **Blocks** | #21 (/ready + /live) |

**Current State**

Two health endpoints with critical discrepancy:
1. `GET /health` (`router.go:52-55`) — **always returns `{"status":"ok"}`**, checks nothing
2. `GET /api/v1/health` (`handlers/health.go`) — real check with DB ping + NATS health
3. `Server.HandleHealthCheck()` (`server.go:162-194`) — checks DB + NATS, **never registered as a route**

K8s liveness/readiness probes hit `/health` → always 200 even if DB/NATS is down.

**Implementation**

Replace the stub in `pkg/services/web/router.go:52-55`:
```go
mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    if err := natsEmbedded.HealthCheck(); err != nil {
        w.WriteHeader(http.StatusServiceUnavailable)
        fmt.Fprintf(w, `{"status":"unhealthy","error":"nats"}`)
        return
    }
    w.Write([]byte(`{"status":"ok"}`))
})
```

The `natsEmbedded` parameter is already available in `NewRouter()`.

**Files to Modify**
- `pkg/services/web/router.go:52-55` — replace stub

**Testing**
- Normal operation → 200
- Stop NATS → 503

---

## Phase 2: Kubernetes Foundation

### Task #20 — P2-HELM: Create Helm chart

| Field | Value |
|---|---|
| **Priority** | HIGH |
| **Effort** | Large |
| **Risk** | Low (all new files) |
| **Dependencies** | #2 (timeouts), #5 (security headers) |
| **Team** | 5 |

**Current State**

The `deploy/` directory does not exist. Everything from scratch.

**Implementation**

Full chart structure:
```
deploy/
  helm/
    constellation-overwatch/
      Chart.yaml
      values.yaml                    # Canonical defaults
      values-dev.yaml                # kind/k3d
      values-staging.yaml
      values-production.yaml         # External secrets
      templates/
        _helpers.tpl
        statefulset.yaml             # replicaCount: 1
        service.yaml                 # ClusterIP + Headless
        ingress.yaml                 # Nginx fallback
        ingressroute.yaml            # Traefik CRD
        traefik-middlewares.yaml     # Headers, rate limit, compress
        configmap.yaml
        secret.yaml                  # + ExternalSecret CRD
        networkpolicy.yaml
        serviceaccount.yaml
        rbac.yaml
        poddisruptionbudget.yaml     # minAvailable: 0
        servicemonitor.yaml
        prometheusrule.yaml
  k8s/
    namespace.yaml                   # PSS restricted
    cert-manager/
      cluster-issuer-staging.yaml
      cluster-issuer-production.yaml
  argocd/
    project.yaml
    application.yaml
```

Key decisions:
- **StatefulSet** (not Deployment) — PVC for SQLite + NATS JetStream
- **replicaCount: 1** enforced — SQLite `MaxOpenConns: 1`
- **PVC**: RWO, 5Gi dev / 10Gi staging / 50Gi production, mounted at `/data`
- **Pod Security**: non-root 65532, read-only rootfs, drop ALL, RuntimeDefault seccomp
- **Probes**: startup (60s window), liveness (`/live`), readiness (`/ready`)
- **Ports**: 8080 (HTTP), 4222 (NATS)
- **Traefik IngressRoute**: HTTPS with Let's Encrypt, SSE-safe compression
- **Traefik IngressRouteTCP**: NATS passthrough port 4222
- **NetworkPolicy**: ingress controller + Prometheus + NATS clients only
- **Resource sizing**: per environment (see TODO table)

Refer to PRODUCTION_READINESS_TODO.md sections "Kubernetes + Helm Architecture" and "Traefik Ingress Configuration" for full specifications.

**Files to Create**
- All files in the chart structure above

**Testing**
- `helm lint deploy/helm/constellation-overwatch`
- `helm template test deploy/helm/constellation-overwatch -f values-dev.yaml`
- Deploy to kind/k3d, verify pod starts, health probes pass

---

## Phase 3: NATS Clustering

### Task #17 — P3-SUBJ: Fix entity subject mismatch bug

| Field | Value |
|---|---|
| **Priority** | MEDIUM |
| **Effort** | Small |
| **Risk** | Low |
| **Dependencies** | None |
| **Team** | 2 |
| **Blocks** | #19 (external NATS) |

**Current State**

`api/services/entity.service.go:294-333`:
```go
event := shared.Event{
    Subject: shared.EntityCreatedSubject(entity.OrgID),  // BUG: always ".created"
}
```

All entity events publish to `constellation.entities.{orgID}.created` regardless of actual event type. The correct helpers exist but are never called:
- `shared.EntityUpdatedSubject()` (subjects.go:79)
- `shared.EntityDeletedSubject()` (subjects.go:83)
- `shared.EntityStatusSubject()` (subjects.go:87)

**Implementation**

```go
var subject string
switch eventType {
case "created":
    subject = shared.EntityCreatedSubject(entity.OrgID)
case "updated":
    subject = shared.EntityUpdatedSubject(entity.OrgID)
case "deleted":
    subject = shared.EntityDeletedSubject(entity.OrgID)
case "status_changed":
    subject = shared.EntityStatusSubject(entity.OrgID)
default:
    subject = shared.EntityCreatedSubject(entity.OrgID)
}
event.Subject = subject
```

Check `pkg/shared/types.go` for event type constants. Define them if missing.

**Files to Modify**
- `api/services/entity.service.go` — fix `publishEntityEvent`
- `pkg/shared/types.go` — add EventType constants if missing

**Testing**
- Create, update, delete entity → verify NATS subjects are correct
- `go test ./...`

---

### Task #18 — P3-KV: Re-enable KV writes in TelemetryWorker

| Field | Value |
|---|---|
| **Priority** | MEDIUM |
| **Effort** | Small |
| **Risk** | Medium (coordinate with mavlink2constellation) |
| **Dependencies** | None |
| **Team** | 2 |
| **Blocks** | #19 (external NATS) |

**Current State**

`pkg/services/workers/telemetry.go:110-114`:
```go
// NOTE: KV writing is disabled - mavlink2constellation handles KV state updates
w.updateCache(state)
```

The full `saveEntityState()` method exists at lines 340-408 with optimistic locking (3 retry attempts on revision conflict) but is never called. Without KV writes, Go-originated telemetry is invisible to KV consumers (e.g., the Overwatch dashboard SSE watch).

**Implementation**

Replace `updateCache` with `saveEntityState`:
```go
if err := w.saveEntityState(state); err != nil {
    logger.Warnw("Failed to save entity state to KV", "error", err)
}
```

Add configurable flag:
```go
kvWriteEnabled := shared.GetEnv("TELEMETRY_KV_WRITE", "true") == "true"
if kvWriteEnabled {
    if err := w.saveEntityState(state); err != nil {
        logger.Warnw("Failed to save entity state to KV", "error", err)
    }
} else {
    w.updateCache(state)
}
```

**Files to Modify**
- `pkg/services/workers/telemetry.go:110-114`
- `.env.example` — add `TELEMETRY_KV_WRITE=true`

**Testing**
- Send telemetry via NATS → verify KV entries updated
- Open `/overwatch` → verify entity states appear in KV watch
- Monitor for revision conflict warnings under load

---

### Task #19 — P3-NATS-EXT: Add external NATS connection support

| Field | Value |
|---|---|
| **Priority** | HIGH |
| **Effort** | Large |
| **Risk** | High (architectural change) |
| **Dependencies** | #10 (NATS auth), #11 (TLS fix), #17 (subject bug), #18 (KV writes) |
| **Team** | 2 |

**Current State**

- `Start()` always calls `StartEmbedded()` — no external path (`nats.go:117-119`)
- Client always connects to `nats://localhost:{port}` — hardcoded (`nats.go:235`)
- All streams `Replicas: 1` — hardcoded
- NKey management requires `en.server != nil` — panics if nil

**Implementation**

### 1. Config additions
```go
type Config struct {
    // ... existing
    ExternalURL    string // NATS_URL
    StreamReplicas int    // NATS_STREAM_REPLICAS
}
```

### 2. Branch Start()
```go
func (en *EmbeddedNATS) Start(ctx context.Context) error {
    if en.config.ExternalURL != "" {
        return en.StartExternal(ctx)
    }
    return en.StartEmbedded(ctx)
}
```

### 3. Guard NKey methods
```go
func (en *EmbeddedNATS) AddNKeyUser(...) error {
    if en.server == nil {
        return fmt.Errorf("NKey management not supported in external NATS mode")
    }
    // ...
}
```

### 4. Configurable Replicas
Replace all `Replicas: 1` with `Replicas: en.config.StreamReplicas`.

Full details in PRODUCTION_READINESS_TODO.md "Clustering Implementation Path — Phase A".

**Files to Modify**
- `pkg/services/embedded-nats/nats.go`
- `.env.example`
- `cmd/microlith/main.go` — conditional NKey restore

**Testing**
- `NATS_URL=""` → embedded mode still works
- Start external NATS (`nats-server -js`), `NATS_URL=nats://localhost:4222` → streams created
- Workers process messages
- NKey methods return graceful errors

---

## Phase 4: Operational Hardening

### Task #21 — P4-HEALTH: Add /ready and /live endpoints

| Field | Value |
|---|---|
| **Priority** | MEDIUM |
| **Effort** | Small |
| **Risk** | Low |
| **Dependencies** | #16 (wire real /health) |
| **Team** | 5 |

**Implementation**

- `/live` — lightweight, always 200 if process is alive (liveness probe)
- `/ready` — deep check: DB ping + NATS health (readiness probe)
- `/health` — same as `/ready` (backward compat)

K8s probes:
```yaml
startupProbe:    { httpGet: { path: /ready }, failureThreshold: 12 }
livenessProbe:   { httpGet: { path: /live } }
readinessProbe:  { httpGet: { path: /ready } }
```

**Files to Modify**
- `pkg/services/web/router.go` — add `/live` and `/ready` endpoints

---

### Task #22 — P4-REQID: Add request ID middleware

| Field | Value |
|---|---|
| **Priority** | MEDIUM |
| **Effort** | Small |
| **Risk** | Low |
| **Dependencies** | None |
| **Team** | 1 |

**Implementation**

```go
func RequestID(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        id := r.Header.Get("X-Request-ID")
        if id == "" {
            id = uuid.New().String()
        }
        w.Header().Set("X-Request-ID", id)
        ctx := context.WithValue(r.Context(), ContextKeyRequestID, id)
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}
```

Apply in handler chain: `RecoverPanic(SecurityHeaders(RequestID(s.mux)))`

**Files to Modify**
- `pkg/services/web/middleware.go` — add `RequestID`
- `pkg/services/web/server.go` — add to chain

---

### Task #23 — P4-SHUTDOWN: Configurable shutdown timeout

| Field | Value |
|---|---|
| **Priority** | LOW |
| **Effort** | Small |
| **Risk** | Low |
| **Dependencies** | None |
| **Team** | 5 |

**Current State**

`cmd/microlith/main.go:266`: hardcoded `5 * time.Second`. Too short for production SSE draining.

**Implementation**

```go
shutdownTimeout := shared.GetEnvDuration("SHUTDOWN_TIMEOUT", 5*time.Second)
ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
```

Add `GetEnvDuration` to `pkg/shared/env.go` if needed.

**Files to Modify**
- `cmd/microlith/main.go`
- `pkg/shared/` — add `GetEnvDuration` if missing
- `.env.example` — add `SHUTDOWN_TIMEOUT=30s` (commented)

---

## Quick Wins

### Task #24 — P1-L1: Fix DB directory permissions

| Field | Value |
|---|---|
| **Priority** | LOW |
| **Effort** | Tiny |
| **Risk** | None |

`db/service.go:60` — change `os.MkdirAll(dir, 0755)` to `os.MkdirAll(dir, 0700)`.

---

### Task #25 — P1-L2: Fix bootstrap invite URL scheme

| Field | Value |
|---|---|
| **Priority** | LOW |
| **Effort** | Tiny |
| **Risk** | None |

`cmd/microlith/main.go:404` — use `OVERWATCH_BASE_URL` when set:
```go
baseURL := shared.GetEnv("OVERWATCH_BASE_URL", fmt.Sprintf("http://localhost:%s", port))
inviteURL := fmt.Sprintf("%s/invite/%s", baseURL, rawToken)
```

---

## Critical Path

The longest dependency chain determines minimum calendar time:

```
#2 HTTP timeouts (day 1)
  └──> #5 Security headers (day 1-2)
         └──> #12 Panic recovery (day 2)
                └──> #20 Helm chart (day 3-5)
```

**Everything else can run in parallel with this chain.** If 5 agent teams execute simultaneously, all 25 tasks can complete in approximately 5 working days.

---

## Essential Files Reference

| File | Tasks | Role |
|---|---|---|
| `cmd/microlith/main.go` | #23, #25 | Entry point, shutdown, bootstrap |
| `pkg/services/embedded-nats/nats.go` | #10, #11, #17, #18, #19 | NATS server, streams, KV, NKey |
| `pkg/services/web/server.go` | #2, #5, #12, #22 | HTTP server, handler chain |
| `pkg/services/web/router.go` | #14, #16, #21 | Routes, middleware, health |
| `pkg/services/web/middleware.go` | #5, #12, #22 | Security headers, panic, request ID (NEW) |
| `api/middleware/auth.go` | #4, #9 | Sessions, CORS, cookies |
| `api/middleware/apikey.go` | #6, #8 | API key auth, scopes, hashing |
| `api/middleware/bodylimit.go` | #3 | Body size limit (NEW) |
| `api/router.go` | #3, #6 | REST API routes, scope enforcement |
| `api/handlers/entity.go` | #7 | Error sanitization |
| `api/handlers/organization.go` | #7 | Error sanitization |
| `api/services/entity.service.go` | #17 | Subject mismatch bug |
| `api/services/auth.service.go` | #13 | WebAuthn session race |
| `api/services/apikey.service.go` | #8 | API key hashing |
| `db/service.go` | #1, #8, #24 | SQL injection, migrations, permissions |
| `db/schema.sql` | #9 | Sessions table |
| `pkg/services/workers/telemetry.go` | #18 | KV writes |
| `pkg/services/web/handlers/invite.go` | #15 | Org ID mismatch |
| `pkg/services/web/handlers/datastar.go` | #7 | Error sanitization |
| `pkg/shared/subjects.go` | #17 | NATS subject helpers |
| `.env.example` | #4, #8, #10, #11, #18, #23 | Env var documentation |
| `deploy/helm/` | #20 | Helm chart (NEW directory) |
