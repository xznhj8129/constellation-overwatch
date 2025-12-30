<p align="center">
  <img src="pkg/services/web/static/images/favicon.svg" alt="Constellation Overwatch" width="120"/>
  <h1 align="center">Constellation Overwatch</h1>
</p>

<p align="center">
  Open source Edge C4ISR Server Mesh for drone/robotic communication, telemetry streaming, and real-time command & control.
</p>

<p align="center">
  <a title="Build Status" target="_blank" href="https://github.com/Constellation-Overwatch/constellation-overwatch/actions"><img src="https://img.shields.io/github/actions/workflow/status/Constellation-Overwatch/constellation-overwatch/go.yml?style=flat-square"></a>
  <a title="Go Report Card" target="_blank" href="https://goreportcard.com/report/github.com/Constellation-Overwatch/constellation-overwatch"><img src="https://goreportcard.com/badge/github.com/Constellation-Overwatch/constellation-overwatch?style=flat-square"></a>
  <a title="Go Version" target="_blank" href="https://go.dev/"><img src="https://img.shields.io/github/go-mod/go-version/Constellation-Overwatch/constellation-overwatch?style=flat-square"></a>
  <a title="License" target="_blank" href="https://github.com/Constellation-Overwatch/constellation-overwatch/blob/main/LICENSE"><img src="http://img.shields.io/badge/license-MIT-orange.svg?style=flat-square"></a>
  <br>
  <a title="GitHub Pull Requests" target="_blank" href="https://github.com/Constellation-Overwatch/constellation-overwatch/pulls"><img src="https://img.shields.io/github/issues-pr-closed/Constellation-Overwatch/constellation-overwatch.svg?style=flat-square&color=FF9966"></a>
  <a title="GitHub Commits" target="_blank" href="https://github.com/Constellation-Overwatch/constellation-overwatch/commits/main"><img src="https://img.shields.io/github/commit-activity/m/Constellation-Overwatch/constellation-overwatch.svg?style=flat-square"></a>
  <a title="Last Commit" target="_blank" href="https://github.com/Constellation-Overwatch/constellation-overwatch/commits/main"><img src="https://img.shields.io/github/last-commit/Constellation-Overwatch/constellation-overwatch.svg?style=flat-square&color=FF9900"></a>
</p>

---

## About

Constellation Overwatch is a distributed, event-driven C4ISR (Command, Control, Communications, and Intelligence) server mesh for managing fleets of autonomous systems including drones, robots, IoT sensors, and edge computing devices. Built on NATS JetStream for reliable, low-latency messaging with atomic operations and durable streams.

> **⚠️ Warning:** This software is under active development. While functional, it may contain bugs and undergo breaking changes. Use caution with production deployments and ensure you have proper backups.

## Features and Roadmap

* **Real-time Pub/Sub Messaging** for low-latency communication between edge devices and control systems
* **Durable Event Streams** using NATS JetStream for reliable message delivery and persistence
* **Multi-Entity Fleet Support** for managing drones, robots, sensors, and other autonomous systems
* **RESTful API** with bearer token authentication for secure HTTP access
* **Embedded NATS Server** providing self-contained messaging with no external dependencies
* **High-Frequency Telemetry** streaming with efficient handling of sensor data
* **Real-time Web Dashboard** powered by Server-Sent Events (SSE) and Datastar framework
* **Type-Safe Templates** using Templ for reactive Go-based web components
* **libSQL Database** with auto-initialization and schema management
* **Event-Driven Architecture** with workers for entities, commands, telemetry, and events
* **Interactive Maps** using MapLibre web components with global KV watcher

The following features are on our current roadmap:

* **Embedded AI Assistant** Context aware private / local AI assistant
* **Background Mavlink Bidirectional Routing** for QGroundControl + TAK Support
* **Video Stream Proxy** for 1:n video streaming to web UI
* **TLS 1.3 Encryption** for enhanced NATS security
* **Logging Stream UI** for centralized log viewing
* **Kubernetes Deployment** manifests and Helm charts and maximum availability + durability
* **Prometheus Metrics** integration for observability
* **Edge Client SDKs** for Go, Python, and Rust
* **Various Autonomoy and Service Support** for drones, robots, and other autonomous systems i.e VSLOAM, Flight Loader, Mission Recap, etc - Needs further input

## Architecture

### API Service Diagram

```mermaid
graph LR
    subgraph "Client Layer"
        C1[Web Dashboard]
        C2[Mobile App]
        C3[CLI Tools]
        C4[Edge Devices]
    end
    
    subgraph "API Gateway"
        API[REST API<br/>:8080]
        AUTH[Bearer Auth<br/>Middleware]
    end
    
    subgraph "Core Services"
        OS[Organization<br/>Service]
        ES[Entity<br/>Service]
    end
    
    subgraph "Data Layer"
        DB[(libSQL/Turso DB)]
        NATS[(NATS JetStream<br/>CONSTELLATION_GLOBAL_STATE KV:entity_id)]
    end
    
    subgraph "NATS Streams"
        S1[CONSTELLATION_ENTITIES]
        S2[CONSTELLATION_EVENTS]
        S3[CONSTELLATION_TELEMETRY]
        S4[CONSTELLATION_COMMANDS]
    end
    
    C1 & C2 & C3 --> API
    C4 <--> NATS
    API --> AUTH
    AUTH --> OS & ES
    OS & ES --> DB
    ES --> NATS
    NATS --> S1 & S2 & S3 & S4
    
    style API fill:#4CAF50
    style NATS fill:#2196F3
    style DB fill:#FF9800
```

### Data Flow Sequence Diagram

```mermaid
sequenceDiagram
    participant D as Drone/Robot
    participant N as NATS JetStream
    participant KV as KV Store<br/>(CONSTELLATION_GLOBAL_STATE)
    participant W as Workers<br/>(Entity/Telemetry/Event)
    participant A as API Service
    participant DB as libSQL DB
    participant UI as Web UI<br/>(SSE)

    Note over D,UI: Entity Registration Flow
    UI->>A: POST /api/v1/entities
    A->>DB: INSERT entity
    A->>N: Publish entity.created event
    A->>KV: Sync entity to KV[entity_id]
    A-->>UI: Return Entity
    N->>W: EntityWorker processes event
    W->>DB: Log entity creation
    W-->>UI: SSE update (if subscribed)

    Note over D,UI: Telemetry Publishing Flow
    D->>N: Publish to constellation.telemetry.{org_id}.{entity_id}
    N->>N: Store in CONSTELLATION_TELEMETRY stream
    N->>W: TelemetryWorker receives message
    W->>KV: Update entity state with latest telemetry
    KV-->>UI: KV watcher triggers SSE update
    UI->>UI: Datastar patches UI elements

    Note over D,UI: Real-time State Sync
    D->>N: Publish status/position update
    N->>W: EventWorker processes update
    W->>DB: Update entity record
    W->>KV: Sync to KV[entity_id]
    KV-->>UI: SSE PatchElements with new state

    Note over D,UI: Command Flow
    UI->>A: POST command
    A->>N: Publish to constellation.commands.{org_id}.{entity_id}
    N->>N: Store in CONSTELLATION_COMMANDS stream
    N->>D: CommandWorker delivers command
    D->>N: Publish command.ack
    N->>W: Process acknowledgment
    W->>KV: Update command status
    KV-->>UI: SSE notification

    Note over D,UI: Web Dashboard Live Updates
    UI->>A: GET /sse/stream (SSE connection)
    A->>N: Subscribe to constellation.>
    loop Real-time Updates
        N->>A: New message on any subject
        A->>UI: SSE PatchElements (Datastar)
        UI->>UI: Update DOM without reload
    end
```

## Getting Started

Please see the [Quick Start Guide](#quick-start-examples) below for detailed usage examples.

<details>
<summary>📋 Prerequisites</summary>
<br>

* Go 1.24 or higher
* [Task](https://taskfile.dev/) - Task runner (optional, recommended)

</details>

<details>
<summary>⚡ Quick Start</summary>
<br>

Clone the repository and start the server:

```bash
# Clone the repository
git clone https://github.com/Constellation-Overwatch/constellation-overwatch.git
cd constellation-overwatch

# Install dependencies
go mod download

# Run in development mode (recommended)
task dev

# OR run directly
go run ./cmd/microlith/main.go
```

The server will start:

* **API Server & Web UI**: `http://localhost:8080`
* **Embedded NATS**: `nats://localhost:4222`

</details>

<details>
<summary>🛠️ Installation (Task Runner)</summary>
<br>

Install Task for enhanced development workflow:

```bash
# macOS
brew install go-task/tap/go-task

# Linux
sh -c "$(curl --location https://taskfile.dev/install.sh)" -- -d -b ~/.local/bin

# Windows (using Scoop)
scoop install task
```

</details>

<details>
<summary>🌐 Web Dashboard</summary>
<br>

Access the real-time web interface at `http://localhost:8080`

**Features:**

* View organizations and entities in real-time
* Monitor NATS streams and key-value stores
* Create and manage fleet entities
* Watch live telemetry data

**Technology Stack:**

* **Templ** - Type-safe Go HTML templates
* **Datastar** - Hypermedia framework for reactive UI
* **Server-Sent Events (SSE)** - Real-time data streaming

**Development Mode:**

```bash
task dev  # Auto-rebuilds templ templates on changes
```

</details>

<details>
<summary>🐳 Docker Deployment</summary>
<br>

Build and run with Docker:

```bash
# Build image
task docker-build

# Run with Docker Compose
task docker-run

# Stop service
task docker-stop
```

</details>

### Configuration

Create a `.env` file in the project root (copy from `.env.example`):

```bash
cp .env.example .env
```

Configuration options:

* `OVERWATCH_TOKEN` - Unified token for API and NATS authentication (default: `reindustrialize-dev-token`)
* `PORT` - HTTP server port (default: `8080`)
* `DB_PATH` - libSQL database path (default: `./db/constellation.db`)
* `NATS_PORT` - NATS server port (default: `4222`)
* `NATS_DATA_DIR` - NATS data directory (default: `./data/overwatch`)
* `WEB_UI_PASSWORD` - Password for Web UI access (leave empty to disable)

Example `.env` file:

```bash
OVERWATCH_TOKEN=reindustrialize-dev-token
PORT=8080
DB_PATH=./db/constellation.db
NATS_PORT=4222
NATS_DATA_DIR=./data/overwatch
WEB_UI_PASSWORD=your-secure-password
```

### Web UI Authentication

The Web UI supports optional password-based authentication. When `WEB_UI_PASSWORD` is set in your `.env` file, users must authenticate before accessing the dashboard.

**To enable Web UI authentication:**

```bash
# In your .env file
WEB_UI_PASSWORD=your-secure-password
```

When enabled:

* Accessing any protected route redirects to `/login`
* Sessions are stored in-memory with 24-hour expiration
* Logout is available at `/logout`

**To disable Web UI authentication:**

Leave `WEB_UI_PASSWORD` empty or unset. The dashboard will be accessible without login.

### API Authentication

All REST API endpoints (`/api/v1/*`) require Bearer token authentication:

```bash
curl -H "Authorization: Bearer reindustrialize-dev-token" \
     http://localhost:8080/api/v1/organizations
```

## Quick Start Examples

<details>
<summary>🚀 API Quickstart with curl</summary>
<br>

Once the server is running, provision an organization and create entities:

**Step 1: Set your API token**

```bash
export TOKEN="reindustrialize-dev-token"  # or your custom token from .env
```

**Step 2: Create an organization**

```bash
curl -s -X POST http://localhost:8080/api/v1/organizations \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "My Fleet",
    "org_type": "civilian",
    "description": "Test drone fleet"
  }'
```

**Allowed `org_type` values:** `military`, `civilian`, `commercial`, `ngo`

**Example Response:**

```json
{
  "success": true,
  "data": {
    "org_id": "ae9c65d0-b5f3-4cec-8ffa-68ff1173e050",
    "name": "My Fleet",
    "org_type": "civilian",
    "metadata": "{}",
    "created_at": "2025-10-22T11:34:29.195678-05:00",
    "updated_at": "2025-10-22T11:34:29.195678-05:00"
  }
}
```

**Step 3: Register entities to the organization**

Extract the `org_id` from the response above:

```sh
export ORG_ID='ae9c65d0-b5f3-4cec-8ffa-68ff1173e050'
curl -s -X POST "http://localhost:8080/api/v1/entities?org_id=$ORG_ID" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Drone-001",
    "entity_type": "aircraft_multirotor",
    "description": "Primary vegetation inspection drone",
    "metadata": {
      "model": "DJI-M300",
      "serial": "ABC123456"
    }
  }'
```

**Example Response:**

```json
{
  "success": true,
  "data": {
    "entity_id": "5458eec0-b0e3-4290-8db5-17936dbbfc64",
    "org_id": "ae9c65d0-b5f3-4cec-8ffa-68ff1173e050",
    "entity_type": "aircraft_multirotor",
    "status": "unknown",
    "priority": "normal",
    "is_live": false
  }
}
```

**Step 4: Query and manage entities**

```bash
# Extract entity_id from response
export ENTITY_ID='5458eec0-b0e3-4290-8db5-17936dbbfc64'

# List all entities in organization
curl -s "http://localhost:8080/api/v1/entities?org_id=$ORG_ID" \
  -H "Authorization: Bearer $TOKEN" | jq

# Get specific entity details
curl -s "http://localhost:8080/api/v1/entities?org_id=$ORG_ID&entity_id=$ENTITY_ID" \
  -H "Authorization: Bearer $TOKEN" | jq

# Update entity status
curl -s -X PUT "http://localhost:8080/api/v1/entities?org_id=$ORG_ID&entity_id=$ENTITY_ID" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "status": "active",
    "metadata": {
      "location": "lat:37.7749,lon:-122.4194",
      "battery": "85%"
    }
  }' | jq
```

</details>

## API Endpoints

### Organizations

* `POST /api/v1/organizations` - Create organization

* `GET /api/v1/organizations` - List organizations
* `GET /api/v1/organizations?org_id=xxx` - Get organization

### Entities

* `POST /api/v1/entities?org_id=xxx` - Create entity

* `GET /api/v1/entities?org_id=xxx` - List entities
* `GET /api/v1/entities?org_id=xxx&entity_id=yyy` - Get entity
* `PUT /api/v1/entities?org_id=xxx&entity_id=yyy` - Update entity
* `DELETE /api/v1/entities?org_id=xxx&entity_id=yyy` - Delete entity

### Health Check

* `GET /health` - Service health status

## NATS Subjects

### Entity Events

* `constellation.entities.{org_id}.created`

* `constellation.entities.{org_id}.updated`
* `constellation.entities.{org_id}.deleted`
* `constellation.entities.{org_id}.status`

### Telemetry

* `constellation.telemetry.{org_id}.{entity_id}`

### Commands

* `constellation.commands.{org_id}.{entity_id}`

* `constellation.commands.{org_id}.broadcast`

## Project Structure

```
constellation-overwatch/
├── cmd/
│   └── microlith/              # Main application entry point
├── api/
│   ├── handlers/               # API-specific handlers (health, orgs, entities)
│   ├── middleware/             # HTTP middleware (auth, CORS, logging)
│   ├── services/               # Business logic services (entities, organizations)
│   └── router.go               # API router definition
├── db/
│   ├── service.go              # Database service with auto-initialization
│   ├── schema.sql              # libSQL database schema
│   └── constellation.db        # libSQL database (auto-created)
├── pkg/
│   ├── ontology/               # Core domain models and entity types
│   ├── shared/                 # Shared types, constants, and NATS subjects
│   └── services/
│       ├── embedded-nats/      # Embedded NATS JetStream server
│       ├── logger/             # Centralized logging service
│       ├── workers/            # Background event processors (entity, command, telemetry, event)
│       └── web/                # Web UI and SSE services
│           ├── handlers/       # Web-specific handlers (pages, datastar, overwatch)
│           ├── datastar/       # Datastar framework integration
│           ├── templates/      # Templ templates (*.templ files)
│           ├── static/         # Static assets (CSS, JS, images)
│           ├── router.go       # Web router and API mounting
│           ├── server.go       # HTTP server lifecycle
│           └── sse_handler.go  # Server-Sent Events handler
├── prd/
│   └── design/                 # Product requirements and design docs
│       ├── API.md              # API specification
│       ├── CLIENT_DESIGN.md    # Client design documentation
│       ├── CONSTELLATION_TAK_ONTOLOGY.md  # Entity ontology definitions
│       └── VENDOR_CAMERA.md    # Camera vendor specifications
├── tests/
│   └── publish-simulations/    # NATS publish simulation tests
├── bin/                        # Compiled binaries (auto-generated)
├── data/                       # NATS JetStream data directory (auto-generated)
├── logs/                       # Application logs (auto-generated)
└── nats.conf                   # NATS server configuration
```

## Development

<details>
<summary>🔨 Building and Running</summary>
<br>

```bash
# Development mode (auto-rebuild templ templates)
task dev

# Generate templ templates
task templ-generate

# Watch templ files for changes
task templ-watch

# Build the binary
task build
# OR manually: go build -o bin/overwatch ./cmd/microlith

# Run the binary
task run
# OR manually: ./bin/overwatch

# Run tests
go test ./...

# Format code
go fmt ./...

# Run go vet
go vet ./...

# List all available tasks
task --list
```

</details>

## Security

<details>
EXPERIMENTAL
<summary>🔒 NATS Authentication</summary>
<br>

Enable NATS authentication with environment variables:

```bash
NATS_AUTH_ENABLED=true
NATS_USER=your_username
NATS_PASSWORD=your_password
```

When enabled, all NATS clients must authenticate using these credentials.

</details>

<details>
<summary>🔐 TLS Encryption</summary>
<br>

Enable TLS 1.3 for NATS connections:

**Step 1: Generate certificates (development)**

```bash
task generate-certs
```

**Step 2: Configure environment variables**

```bash
NATS_TLS_ENABLED=true
NATS_TLS_CERT=/path/to/server.crt
NATS_TLS_KEY=/path/to/server.key
```

</details>

## Contributing

We welcome contributions to Constellation Overwatch! Please check out our [contribution guidelines](CONTRIBUTING.md) to get started.

### How to Contribute

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

### Development Guidelines

* Follow Go best practices and conventions
* Add tests for new features
* Update documentation as needed
* Run `go fmt ./...` and `go vet ./...` before committing
* Ensure all tests pass with `go test ./...`

## License

This project is licensed under the [MIT License](LICENSE).

### Contribution

Unless you explicitly state otherwise, any contribution intentionally submitted for inclusion in Constellation Overwatch by you shall be licensed as MIT, without any additional terms or conditions.
