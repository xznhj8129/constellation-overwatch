# Production Readiness TODO

> **Current Readiness: ~60-65%**
> **Last Audited: 2026-02-25**
> **Branch: feat-production-tuneups**

---

## Table of Contents

- [Security Audit Findings](#security-audit-findings)
- [TLS & Encryption Posture](#tls--encryption-posture)
- [NATS Clustering Readiness](#nats-clustering-readiness)
- [Kubernetes + Helm Architecture](#kubernetes--helm-architecture)
- [Traefik Ingress Configuration](#traefik-ingress-configuration)
- [Production Readiness Checklist](#production-readiness-checklist)
- [Architecture Evolution Path](#architecture-evolution-path)

---

## Security Audit Findings

### CRITICAL (Production Blockers)

- [ ] **C-1: In-memory session store** — All sessions lost on restart, no audit trail, no cross-restart revocation
  - File: `api/middleware/auth.go:29-37`
  - Sessions stored in plain Go `map[string]*session` — only survives as long as the process
  - **Fix:** Persist sessions to SQLite (`sessions` table with `session_token`, `user_id`, `role`, `org_id`, `expires_at`, `created_at`) or use a Redis-backed store

- [ ] **C-2: NATS server unauthenticated** — Binds `0.0.0.0:4222` with no auth, open to the network
  - File: `pkg/services/embedded-nats/nats.go:83-93, 132-155`
  - No `Username`, `Password`, `Authorization`, `AuthTimeout`, or `Accounts` fields set in server options
  - NKey users are additive (appended after startup) — server is reachable unauthenticated at boot
  - Any process that can reach port 4222 can subscribe to all telemetry, commands, and entity streams
  - **Fix:**
    1. Bind to `127.0.0.1` by default (change default from `0.0.0.0`)
    2. Set `opts.Authorization` token for the internal server-to-self connection
    3. Enable NATS TLS + NKey from server startup rather than via additive `ReloadOptions` path

- [ ] **C-3: SQL injection via unparameterized PRAGMA** — `columnExists` formats table name directly into SQL
  - File: `db/service.go:354-355`
  - `fmt.Sprintf("PRAGMA table_info(%s)", table)` bypasses parameterized query protection
  - Currently only called with hardcoded string literals but the function accepts arbitrary input
  - **Fix:** Validate `table` against an allowlist of known table names, or use `SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`

### HIGH (Address Before Production)

- [ ] **H-1: No HTTP server timeouts** — Slowloris DoS vulnerability
  - File: `pkg/services/web/server.go:121-124`
  - `http.Server` created with no `ReadTimeout`, `WriteTimeout`, `ReadHeaderTimeout`, or `IdleTimeout`
  - **Fix:**
    ```go
    s.server = &http.Server{
        Addr:              s.bindAddr,
        Handler:           s.mux,
        ReadHeaderTimeout: 5 * time.Second,
        ReadTimeout:       30 * time.Second,
        WriteTimeout:      60 * time.Second,  // Longer for SSE endpoints
        IdleTimeout:       120 * time.Second,
    }
    ```
  - Note: SSE endpoints need per-request context timeouts, not server-level `WriteTimeout`

- [ ] **H-2: No request body size limit** — Memory exhaustion via large payloads
  - Files: `api/handlers/entity.go:46-48`, `api/handlers/organization.go:39-41`
  - All `json.NewDecoder(r.Body).Decode()` calls operate on raw `r.Body` with no size cap
  - **Fix:** Apply as middleware for all API routes:
    ```go
    r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB limit
    ```

- [ ] **H-3: CORS wildcard by default** — All origins allowed when `ALLOWED_ORIGINS` unset
  - File: `api/middleware/auth.go:233-244`
  - `IsOriginAllowed` returns `true` when `ALLOWED_ORIGINS` is empty or `"*"`
  - `.env.example` does not set `ALLOWED_ORIGINS`
  - **Fix:** Default to deny when not configured:
    ```go
    if origins == "" {
        return false // deny when not configured
    }
    ```

- [ ] **H-4: Zero security response headers** — No HSTS, CSP, X-Frame-Options, X-Content-Type-Options, Referrer-Policy
  - File: `pkg/services/web/router.go`
  - Pages can be embedded in iframes (clickjacking), browsers will MIME-sniff responses
  - **Fix:** Add security headers middleware:
    ```go
    func SecurityHeaders(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            w.Header().Set("X-Content-Type-Options", "nosniff")
            w.Header().Set("X-Frame-Options", "DENY")
            w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
            w.Header().Set("Permissions-Policy", "geolocation=(), camera=(), microphone=()")
            next.ServeHTTP(w, r)
        })
    }
    ```

- [ ] **H-5: API key scopes defined but never enforced** — All keys get full access regardless of scopes
  - File: `api/router.go:43-58`
  - `RequireScope` middleware exists but is never used on any route
  - A read-only key with `scopes=["entities:read"]` has silent write access
  - **Fix:**
    ```go
    r.With(middleware.RequireScope("entities:read")).Get("/", entityHandler.ListOrGet)
    r.With(middleware.RequireScope("entities:write")).Post("/", entityHandler.Create)
    r.With(middleware.RequireScope("entities:write")).Put("/", entityHandler.Update)
    r.With(middleware.RequireScope("entities:write")).Delete("/", entityHandler.Delete)
    ```

- [ ] **H-6: API keys hashed with plain SHA-256** — Vulnerable to GPU brute force if DB compromised
  - Files: `api/middleware/apikey.go:142-146`, `api/services/apikey.service.go:66-67`
  - Modern GPUs can compute billions of SHA-256 hashes per second
  - **Fix:** Use HMAC-SHA256 with a server-side secret (`OVERWATCH_KEY_HASH_SECRET`):
    ```go
    func hashKey(raw string) string {
        secret := os.Getenv("OVERWATCH_KEY_HASH_SECRET")
        mac := hmac.New(sha256.New, []byte(secret))
        mac.Write([]byte(raw))
        return hex.EncodeToString(mac.Sum(nil))
    }
    ```

- [ ] **H-7: WebAuthn session deleted before expiry check** — Race condition / timing attack
  - File: `api/services/auth.service.go:382-391`
  - Session row deleted before checking expiry — error type reveals validity information
  - **Fix:** Check expiry before deleting, return consistent error

### MEDIUM

- [ ] **M-1: Internal error messages leaked to API clients** — 500 responses expose implementation details
  - Files: `api/handlers/entity.go:47,52-53`, `api/handlers/organization.go:40,45-46`
  - Raw `err.Error()` strings passed directly to client on 500s
  - **Fix:** Log full error internally, return generic message to client

- [ ] **M-2: No rate limiting on any endpoint** — Credential stuffing, brute force, abuse
  - All route registrations in `api/router.go` and `pkg/services/web/router.go`
  - Protected endpoints needing rate limiting: `/api/v1/*`, `/auth/*`, `/invite/*`
  - **Fix:** Token-bucket rate limiting per IP using `golang.org/x/time/rate` or `go-chi/httprate`
  - Auth endpoints: 5 requests/minute/IP

- [ ] **M-3: Session cookie uses `SameSite: Lax`** — Should be `Strict` for C4ISR system
  - File: `api/middleware/auth.go:192-201`
  - `Lax` permits cookie on top-level cross-site GET navigations
  - Operational pages (`/map`, `/fleet`, `/overwatch`) accessible via cross-site links

- [ ] **M-4: Prometheus metrics and pprof exposed without role check**
  - File: `pkg/services/web/router.go:49,78`
  - `/metrics` is completely unauthenticated
  - pprof is session-only but no admin role check (viewers can access profiling data)
  - **Fix:** Require admin role for both, or restrict to localhost-only internal listener

- [ ] **M-5: WebAuthn RPID defaults to `localhost`** — Silent failure in production
  - File: `api/services/auth.service.go:69-84`
  - If `OVERWATCH_RPID` not set, credentials bind to `localhost` and cannot authenticate against real domain
  - **Fix:** Make `OVERWATCH_RPID` required in production; refuse to start if RPID is `localhost` and base URL is not localhost

- [ ] **M-6: Invite token acceptance allows org ID mismatch**
  - File: `pkg/services/web/handlers/invite.go:83-151`
  - Existing user found by email is used regardless of whether their `OrgID` matches invite's `OrgID`
  - **Fix:** Validate `existing.OrgID == invite.OrgID` before granting session

### LOW

- [ ] **L-1: Database directory created with `0755`** — World-readable, should be `0700`
  - File: `db/service.go:60`

- [ ] **L-2: Bootstrap admin invite URL hardcodes `http://`** — Should respect TLS config
  - File: `cmd/microlith/main.go:404`

- [ ] **L-3: Async `last_used_at` goroutine competes for SQLite single-writer**
  - File: `api/middleware/apikey.go:89-94`
  - Each API request launches a goroutine competing for `MaxOpenConns: 1`

---

## TLS & Encryption Posture

### Current State: Completely Unencrypted

| Layer | Status | Details |
|---|---|---|
| HTTP Server | No TLS | `server.Serve(listener)` — plain TCP, no `ServeTLS()` call |
| NATS Server | TLS config broken | Reads `NATS_ENABLE_TLS` but never reads cert/key paths — guard always false |
| NATS Client | Plain TCP | Connects to `nats://localhost:4222` — never `tls://` |
| MediaMTX | No TLS | `http.Client{}` with no TLS enforcement |
| Session Cookies | Insecure by default | `Secure` flag only set when `OVERWATCH_BASE_URL` starts with `https://` |
| Data at Rest | Unencrypted | `modernc.org/sqlite` — not SQLCipher, no encryption |

### What IS Properly Secured

- All tokens generated with `crypto/rand` (cryptographically secure)
- API keys stored as SHA-256 hashes (never plaintext)
- Invite tokens stored as SHA-256 hashes
- No passwords exist (passkey-only auth is a strength)
- NKey cryptography for edge device NATS auth

### Critical NATS TLS Bug

```go
// nats.go DefaultConfig() - reads the flag but NEVER reads cert paths
EnableTLS: shared.GetEnv("NATS_ENABLE_TLS", "false") == "true",
// TLSCert and TLSKey are ALWAYS empty strings — never read from env

// This guard at line 165 is therefore ALWAYS FALSE:
if en.config.EnableTLS && en.config.TLSCert != "" && en.config.TLSKey != "" {
```

- [ ] **Fix:** Add `NATS_TLS_CERT` and `NATS_TLS_KEY` env var reads to `DefaultConfig()`

### Required TLS Environment Variables (not yet implemented)

```env
# Web Server TLS (not yet supported in code)
TLS_CERT_FILE=/etc/certs/server.crt
TLS_KEY_FILE=/etc/certs/server.key
TLS_MIN_VERSION=1.2

# NATS TLS (config bug — paths never read)
NATS_ENABLE_TLS=true
NATS_TLS_CERT=/etc/certs/nats.crt
NATS_TLS_KEY=/etc/certs/nats.key
```

### Production TLS Strategy (with Traefik)

```
[Browser] --HTTPS--> [Traefik :443] --HTTP--> [Overwatch Pod :8080]
[Edge Device] --TLS/TCP--> [Traefik :4222] --TCP passthrough--> [NATS :4222]
```

| Layer | Mechanism | Provider |
|---|---|---|
| Browser <-> Traefik | TLS 1.2+ (Let's Encrypt) | Traefik ACME or cert-manager |
| Traefik <-> Pod HTTP | Plain HTTP (cluster-internal) | N/A (trusted network) |
| Edge <-> NATS | TLS passthrough or mTLS | NATS built-in TLS (fix config bug first) |
| Data at rest (SQLite) | Filesystem encryption | LUKS/dm-crypt on PVC StorageClass |
| Data at rest (NATS JS) | Filesystem encryption | Same PVC, same volume encryption |
| Secrets | External Secrets Operator | Vault / AWS Secrets Manager |

---

## NATS Clustering Readiness

### Current Architecture: Single-Process Embedded

```
cmd/microlith/main.go
    |
    +--> embeddednats.NewService()
    |       +--> server.NewServer(opts)    [in-process nats-server/v2]
    |       +--> nats.Connect("nats://localhost:4222")
    |       +--> 4 streams (Replicas=1), 1 KV bucket (Replicas=1)
    |       +--> 4 durable pull consumers
    |
    +--> workers.NewManager(*EmbeddedNATS)
    |       +--> TelemetryWorker   -> PullSubscribe(CONSTELLATION_TELEMETRY)
    |       +--> EntityWorker      -> PullSubscribe(CONSTELLATION_ENTITIES)
    |       +--> EventWorker       -> PullSubscribe(CONSTELLATION_EVENTS)
    |       +--> CommandWorker     -> PullSubscribe(CONSTELLATION_COMMANDS)
    |
    +--> api.NewRouter(*sql.DB, *EmbeddedNATS)
    +--> web.NewWebService(*nats.Conn, *EmbeddedNATS)
```

### Stream Configuration Summary

| Stream | Subjects | Retention | MaxMsgs | MaxAge | Replicas |
|---|---|---|---|---|---|
| CONSTELLATION_ENTITIES | `constellation.entities.>` | Limits | 100,000 | 7 days | **1** |
| CONSTELLATION_EVENTS | `constellation.events.>` | WorkQueue | 50,000 | 24 hours | **1** |
| CONSTELLATION_TELEMETRY | `constellation.telemetry.>` | Limits | 100,000 | 2 hours | **1** |
| CONSTELLATION_COMMANDS | `constellation.commands.>` | WorkQueue | 10,000 | 15 min | **1** |
| KV: GLOBAL_STATE | (bucket) | — | — | No TTL | **1** |

### Clustering Blockers

| # | Blocker | Location | Severity |
|---|---|---|---|
| 1 | **Embedded-only** — `Start()` always calls `StartEmbedded()`, no external connect path | `nats.go:117-119` | Critical |
| 2 | **Hardcoded localhost** — client always connects to `nats://localhost:{port}` | `nats.go:235` | Critical |
| 3 | **All streams Replicas=1** — no replication | `nats.go:327,341,355,369` | Critical |
| 4 | **KV bucket Replicas=1** | `nats.go:399` | Critical |
| 5 | **Concrete type dependency** — all services use `*embeddednats.EmbeddedNATS` directly | Throughout | High |
| 6 | **NKey management is embedded-only** — `ReloadOptions()` panics if `server == nil` | `nats.go:642` | High |
| 7 | **No cluster route config** — zero `Cluster` options in server config | `nats.go:132-155` | High |
| 8 | **EntityRegistry is in-memory** — per-process only, not shared across nodes | `workers/registry.go:17-21` | Medium |
| 9 | **Subject mismatch bug** — all entity events published to `.created` regardless of type | `entity.service.go:300-313` | Medium |
| 10 | **KV writes disabled** in TelemetryWorker | `workers/telemetry.go:112-113` | Medium |
| 11 | **No dead-letter stream** — messages failing 3x silently disappear | Consumer config | Low |
| 12 | **No cluster env vars** in `.env.example` | `.env.example` | Low |

### Clustering Implementation Path

#### Phase A: External NATS Support (minimum viable)

- [ ] Add `NATS_URL` env var to `Config` struct
- [ ] Branch `Start()`: if `NATS_URL` set, skip `StartEmbedded()`, connect externally
- [ ] Guard NKey methods (`AddNKeyUser`, `RemoveNKeyUser`, `RestoreNKeyUsers`) for `server == nil`
- [ ] Make stream `Replicas` configurable via `NATS_STREAM_REPLICAS` env var
- [ ] Fix subject mismatch bug in `publishEntityEvent`
- [ ] Re-enable KV writes in TelemetryWorker

#### Phase B: Embedded Cluster Mode (for edge deployments)

- [ ] Add `NATS_CLUSTER_NAME`, `NATS_CLUSTER_HOST`, `NATS_CLUSTER_PORT`, `NATS_CLUSTER_ROUTES` to Config
- [ ] Set `opts.Cluster` and `opts.Routes` in `StartEmbedded()`
- [ ] Add cluster TLS config (`NATS_CLUSTER_TLS_CERT`, `NATS_CLUSTER_TLS_KEY`)

#### Phase C: JWT-Based Auth (for external clusters)

- [ ] Replace `ReloadOptions` NKey management with NATS operator/account JWTs
- [ ] Use `nsc` tooling for user provisioning
- [ ] Store user JWTs in database instead of public keys
- [ ] Replace `BuildNATSPermissions` + `ReloadOptions` with JWT issuance at API key creation

### Required Config Additions

```go
type Config struct {
    // ... existing fields ...
    ExternalURL    string // NATS_URL — if set, connect externally instead of embedded
    StreamReplicas int    // NATS_STREAM_REPLICAS — 1 for dev, 3 for production
    ClusterName    string // NATS_CLUSTER_NAME
    ClusterHost    string // NATS_CLUSTER_HOST
    ClusterPort    int    // NATS_CLUSTER_PORT (default 6222)
    ClusterRoutes  string // NATS_CLUSTER_ROUTES (comma-separated route URLs)
}
```

---

## Kubernetes + Helm Architecture

### Architecture Decision

**StatefulSet with `replicaCount: 1`** because:
- SQLite uses `MaxOpenConns: 1` (single-writer, corrupts under concurrent access)
- Embedded NATS JetStream uses file-based storage (single-process)
- HA via fast failover (pod rescheduling), not horizontal scaling
- PVC uses `ReadWriteOnce` access mode

### Helm Chart Structure

```
deploy/
  helm/
    constellation-overwatch/
      Chart.yaml
      values.yaml                    # Canonical defaults
      values-dev.yaml                # Local cluster (kind/k3d)
      values-staging.yaml            # Staging environment
      values-production.yaml         # Production with external-secrets
      templates/
        _helpers.tpl                 # Name, labels, image helpers
        statefulset.yaml             # Single-replica StatefulSet
        service.yaml                 # ClusterIP + Headless + optional NATS LB
        ingress.yaml                 # Standard Ingress (nginx fallback)
        ingressroute.yaml            # Traefik IngressRoute CRD
        traefik-middlewares.yaml     # Security headers, rate limit, compress
        configmap.yaml               # Non-sensitive env vars
        secret.yaml                  # Sensitive vars + ExternalSecret CRD
        networkpolicy.yaml           # Ingress/NATS/Prometheus isolation
        serviceaccount.yaml
        rbac.yaml                    # Empty rules (no K8s API access needed)
        poddisruptionbudget.yaml     # minAvailable: 0 for single replica
        servicemonitor.yaml          # Prometheus scraping
        prometheusrule.yaml          # Alert rules
  k8s/
    namespace.yaml                   # PSS restricted enforcement
    cert-manager/
      cluster-issuer-staging.yaml
      cluster-issuer-production.yaml
  argocd/
    project.yaml
    application.yaml
```

### Key Design Decisions

| Concern | Decision | Rationale |
|---|---|---|
| Workload type | StatefulSet | PVC for SQLite + NATS JetStream data |
| Replicas | 1 (enforced) | SQLite single-writer, embedded NATS |
| PVC | ReadWriteOnce, 50Gi production | Unified `/data` dir for DB + NATS store |
| Ingress | Traefik IngressRoute CRD | SSE-friendly, native TCP routing for NATS |
| Secrets | External Secrets Operator (production) | Vault/AWS Secrets Manager integration |
| Pod security | Restricted PSS | Non-root (65532), read-only rootfs, drop ALL caps |
| NATS external | Optional LoadBalancer service | Edge devices outside cluster |
| Monitoring | ServiceMonitor for Prometheus | `/metrics` endpoint already exists |

### Pod Security Context

```yaml
podSecurityContext:
  runAsNonRoot: true
  runAsUser: 65532
  runAsGroup: 65532
  fsGroup: 65532
  seccompProfile:
    type: RuntimeDefault

securityContext:
  allowPrivilegeEscalation: false
  readOnlyRootFilesystem: true
  capabilities:
    drop: [ALL]
```

### Resource Sizing

| Environment | CPU Request | CPU Limit | Memory Request | Memory Limit | PVC |
|---|---|---|---|---|---|
| Dev | 100m | 500m | 128Mi | 512Mi | 5Gi |
| Staging | 200m | 750m | 256Mi | 768Mi | 10Gi |
| Production | 500m | 2000m | 512Mi | 2Gi | 50Gi |

Rationale: NATS JetStream with 1GB max memory + Go runtime + SQLite. 512KB NATS payload limit sized for video chunk streaming. CPU limits account for telemetry burst processing through all four workers simultaneously.

### Health Probes

```yaml
startupProbe:
  httpGet: { path: /health, port: http }
  initialDelaySeconds: 10
  periodSeconds: 5
  failureThreshold: 12     # 60s total startup window

livenessProbe:
  httpGet: { path: /health, port: http }
  periodSeconds: 15
  failureThreshold: 3

readinessProbe:
  httpGet: { path: /health, port: http }
  periodSeconds: 10
  failureThreshold: 3
```

> **Note:** The current `/health` endpoint is too basic (`{"status":"ok"}`). It should check DB connectivity, NATS connectivity, and worker health. Add `/ready` and `/live` endpoints separately.

### Critical SSE Configuration

The `/api/streams/sse`, `/api/overwatch/kv/watch`, and `/api/metrics/sse` endpoints are SSE long-lived connections. The ingress **must** have:
- `proxy-buffering: off` (or Traefik compress middleware excluding `text/event-stream`)
- Timeouts of at least 3600s for read/send
- The `X-Accel-Buffering: no` header is already set by SSE handlers

### Data Flow Diagram

```
[Edge Drone/Robot]
     |  NATS TCP :4222 (NKey auth)
     v
[Traefik IngressRouteTCP :4222] or [K8s LoadBalancer]
     |
     v
[StatefulSet Pod: overwatch-0]
  +-----------------------------------------+
  |  /app/overwatch (single binary)         |
  |                                         |
  |  Web Server :8080                       |
  |    /health  <- liveness/readiness       |
  |    /metrics <- Prometheus scrape        |
  |    /api/v1/ <- REST API (Bearer auth)   |
  |    /        <- Web UI (session auth)    |
  |    /api/streams/sse <- SSE long-poll    |
  |                                         |
  |  Embedded NATS Server :4222             |
  |    JetStream: /data/overwatch/          |
  |    KV: CONSTELLATION_GLOBAL_STATE       |
  |    Streams: 4x                          |
  |                                         |
  |  Workers (goroutines x4)                |
  |  SQLite: /data/db/constellation.db      |
  +-----------------------------------------+
     |           |
     v           v
  [PVC: data]  [ClusterIP Service: http :8080]
  RWO 50Gi          |
                     v
              [Traefik IngressRoute :443]
                   | TLS terminated (Let's Encrypt)
                   v
              [Browser / API Client]
```

---

## Traefik Ingress Configuration

### Why Traefik Over Nginx for This App

| Feature | Traefik | Nginx Ingress |
|---|---|---|
| SSE support | Native, no buffering by default | Requires `proxy-buffering: off` annotation |
| NATS TCP routing | `IngressRouteTCP` CRD | Requires TCP ConfigMap + reload |
| Let's Encrypt | Built-in ACME resolver | Requires cert-manager |
| Rate limiting | Middleware CRD | Annotation-based |
| Dashboard | Built-in web UI | None |
| K8s CRD-native | Yes (`IngressRoute`) | Standard `Ingress` resource |

### IngressRoute (HTTPS)

```yaml
apiVersion: traefik.io/v1alpha1
kind: IngressRoute
metadata:
  name: constellation-overwatch
spec:
  entryPoints:
    - websecure
  routes:
    - match: Host(`overwatch.example.com`)
      kind: Rule
      services:
        - name: constellation-overwatch
          port: 8080
      middlewares:
        - name: constellation-overwatch-headers
        - name: constellation-overwatch-ratelimit
        - name: constellation-overwatch-compress
  tls:
    certResolver: letsencrypt
```

### Security Headers Middleware

```yaml
apiVersion: traefik.io/v1alpha1
kind: Middleware
metadata:
  name: constellation-overwatch-headers
spec:
  headers:
    stsSeconds: 31536000
    stsIncludeSubdomains: true
    stsPreload: true
    forceSTSHeader: true
    contentTypeNosniff: true
    frameDeny: true
    browserXssFilter: true
    referrerPolicy: "strict-origin-when-cross-origin"
    customResponseHeaders:
      X-Powered-By: ""
      Server: ""
```

### Rate Limiting Middleware

```yaml
apiVersion: traefik.io/v1alpha1
kind: Middleware
metadata:
  name: constellation-overwatch-ratelimit
spec:
  rateLimit:
    average: 100
    burst: 200
    period: 1m
```

### Compression Middleware (SSE-safe)

```yaml
apiVersion: traefik.io/v1alpha1
kind: Middleware
metadata:
  name: constellation-overwatch-compress
spec:
  compress:
    excludedContentTypes:
      - text/event-stream    # CRITICAL: never compress SSE streams
```

### NATS TCP Passthrough (Edge Devices)

```yaml
apiVersion: traefik.io/v1alpha1
kind: IngressRouteTCP
metadata:
  name: constellation-overwatch-nats
spec:
  entryPoints:
    - nats    # Requires TCP entrypoint on port 4222 in Traefik static config
  routes:
    - match: HostSNI(`*`)
      services:
        - name: constellation-overwatch
          port: 4222
  tls:
    passthrough: true   # Let NATS handle its own TLS
```

### Traefik Static Config Requirements

```yaml
# Traefik Helm values (traefik/traefik chart)
additionalArguments:
  - "--serversTransport.forwardingTimeouts.dialTimeout=30s"
  - "--serversTransport.forwardingTimeouts.responseHeaderTimeout=0s"   # Disable for SSE
  - "--serversTransport.forwardingTimeouts.idleConnTimeout=3600s"

ports:
  nats:
    port: 4222
    expose:
      default: true
    exposedPort: 4222
    protocol: TCP
```

---

## Production Readiness Checklist

### Phase 1: Security Hardening (Week 1) — CRITICAL

- [ ] Persist sessions to SQLite (add `sessions` table)
- [ ] Fix NATS auth (bind `127.0.0.1` by default, require NKey from start)
- [ ] Fix SQL injection in `columnExists`
- [ ] Add HTTP server timeouts (`ReadHeaderTimeout: 5s`, `WriteTimeout: 60s`)
- [ ] Add `http.MaxBytesReader` (1MB limit on all API endpoints)
- [ ] Fix CORS default (deny when `ALLOWED_ORIGINS` unset)
- [ ] Add security headers middleware (HSTS, CSP, X-Frame-Options)
- [ ] Enforce API key scopes (add `RequireScope()` to routes)
- [ ] Upgrade API key hashing to HMAC-SHA256 with server-side secret
- [ ] Fix NATS TLS config bug (read `NATS_TLS_CERT`/`NATS_TLS_KEY` env vars)
- [ ] Sanitize error responses (don't leak internal errors on 500s)
- [ ] Add panic recovery middleware for HTTP handlers

### Phase 2: Kubernetes Foundation (Week 2)

- [ ] Create Helm chart structure (`deploy/helm/constellation-overwatch/`)
- [ ] StatefulSet with PVC (20Gi dev, 50Gi production)
- [ ] Traefik IngressRoute with security headers middleware
- [ ] Traefik IngressRouteTCP for NATS edge access
- [ ] ConfigMap + Secret separation
- [ ] NetworkPolicy (ingress controller, Prometheus, NATS clients)
- [ ] ServiceMonitor for Prometheus scraping
- [ ] Pod Security Standards (restricted)
- [ ] Non-root container (`runAsUser: 65532`)
- [ ] Namespace with PSS enforcement labels
- [ ] cert-manager ClusterIssuer for Let's Encrypt

### Phase 3: NATS Clustering Readiness (Week 3)

- [ ] Add `NATS_URL` env var for external NATS connect path
- [ ] Guard NKey methods for nil server
- [ ] Make stream `Replicas` configurable
- [ ] Fix entity subject mismatch bug
- [ ] Re-enable KV writes in TelemetryWorker
- [ ] Add dead-letter stream for failed messages
- [ ] Add `NATS_CLUSTER_*` env vars for embedded cluster mode

### Phase 4: Operational Hardening (Week 4)

- [ ] Add rate limiting (per-IP via Traefik middleware + per-API-key in app)
- [ ] Implement database migration framework (`golang-migrate`)
- [ ] Enhanced `/health` endpoint (DB + NATS + worker status)
- [ ] Add `/ready` and `/live` endpoints
- [ ] Improve logging (JSON output mode for aggregation, lumberjack rotation)
- [ ] Schedule WebAuthn session cleanup (periodic goroutine)
- [ ] External Secrets Operator for production secrets
- [ ] PVC backup strategy (Velero or CSI snapshots, RPO < 1 hour)
- [ ] ArgoCD GitOps application manifest
- [ ] Configurable shutdown timeout (currently hardcoded 5s, should be 30s for production)
- [ ] Add request ID correlation for log tracing

### Phase 5: Observability & Scale (Ongoing)

- [ ] OpenTelemetry instrumentation
- [ ] Grafana dashboards for NATS streams, entity counts, API latency
- [ ] Alert rules (OverwatchDown, HighMemory, NATSLag)
- [ ] Log aggregation (Loki/Vector)
- [ ] External NATS cluster (3-node) for true HA
- [ ] SQLite -> PostgreSQL/LibSQL migration for horizontal scaling
- [ ] Load testing under production conditions

---

## Architecture Evolution Path

```
CURRENT (Single Node)          PHASE 2 (K8s Single)           PHASE 5 (K8s Clustered)
+-----------------+           +-----------------+           +----------------------+
|  microlith      |           |  StatefulSet(1)  |           |  Deployment(3)       |
|  +- HTTP :8080  |  ------>  |  +- HTTP :8080   |  ------>  |  +- HTTP :8080       |
|  +- NATS :4222  |           |  +- NATS :4222   |           |  +- Workers          |
|  +- Workers     |           |  +- Workers      |           |                      |
|  +- SQLite      |           |  +- SQLite (PVC) |           |  External NATS (3)   |
|  +- JetStream   |           |  +- JetStream    |           |  PostgreSQL (HA)     |
|                 |           |                  |           |                      |
|  Traefik -------+-->        |  Traefik --------+-->        |  Traefik ------------+-->
+-----------------+           +-----------------+           +----------------------+
```

### Migration Gates

| Gate | Requirement | Unblocks |
|---|---|---|
| External NATS | `NATS_URL` env var + nil server guards | Multi-node NATS cluster |
| Stream replication | `NATS_STREAM_REPLICAS=3` | Message durability across nodes |
| Database migration | SQLite -> PostgreSQL | `replicaCount > 1`, horizontal scaling |
| JWT auth | NATS operator/account model | External cluster user management |

---

## Essential Files Reference

| File | Role |
|---|---|
| `cmd/microlith/main.go` | Entry point, service init, bootstrap admin, graceful shutdown |
| `pkg/services/embedded-nats/nats.go` | NATS server, client, streams, KV, NKey auth |
| `pkg/services/web/server.go` | HTTP server construction (where TLS must be added) |
| `pkg/services/web/router.go` | Route registration, middleware wiring, public endpoints |
| `api/middleware/auth.go` | Session cookies, CORS origin check, `secureCookies()` |
| `api/middleware/apikey.go` | API key validation, SHA-256 hash lookup |
| `api/router.go` | REST API routes (where scope enforcement is missing) |
| `api/handlers/entity.go` | Entity CRUD handlers (body size, error leakage) |
| `api/services/auth.service.go` | WebAuthn relying party, session data storage |
| `api/services/apikey.service.go` | API key generation, NKey pairing, scope management |
| `api/services/entity.service.go` | NATS publish on entity CRUD, KV sync |
| `db/service.go` | SQLite driver, schema init, migration, SQL injection |
| `db/schema.sql` | 408-line schema (12 tables, indexes, constraints) |
| `pkg/shared/subjects.go` | NATS subject patterns, stream/consumer names |
| `pkg/shared/types.go` | Message type definitions |
| `pkg/services/workers/base.go` | PullSubscribe pattern, fetch loop, ack/nak |
| `pkg/services/workers/telemetry.go` | KV read/write, entity state, MAVLink parsing |
| `pkg/services/workers/registry.go` | In-memory entity registry |
| `pkg/services/web/handlers/overwatch.go` | KV watch -> SSE push to browsers |
| `pkg/services/mediamtx/client.go` | MediaMTX polling (no TLS enforcement) |
| `.env.example` | Documented env vars (TLS vars absent) |
| `Taskfile.yml` | Build/dev tasks, `generate-certs` (disconnected from server code) |
| `Dockerfile` | Multi-stage, CGO_ENABLED=0, Alpine 3.21 |
