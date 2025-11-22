# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

This project uses [Task](https://taskfile.dev/) instead of Make for task automation.

### Development
```bash
# List all available tasks
task --list

# Development mode (with templ auto-rebuild and server)
task dev

# Run the server
go run ./cmd/microlith/main.go

# Build the binary
task build

# Run the built binary
task run

# Run tests
go test ./...

# Install dependencies
go mod download

# Tidy dependencies
go mod tidy

# Format code
go fmt ./...

# Run go vet
go vet ./...

# Generate templ templates
task templ-generate

# Watch templ files for changes
task templ-watch

# Clean build artifacts and data
task clean

# Generate development certificates
task generate-certs

# Docker commands
task docker-build
task docker-run
task docker-stop
```

### Environment Setup
Create a `.env` file from the example:
```bash
cp .env.example .env
```

Default ports:
- API Server & Web UI: 8080
- NATS Server: 4222 (embedded)

## Architecture

### Overview
Constellation Overwatch is a C4ISR (Command, Control, Communications, and Intelligence) server mesh for drone/robotic communication. It's built as a monolithic "microlith" with embedded NATS JetStream for messaging.

### Core Components

#### 1. API Layer (`/api`)
- **handlers.go**: HTTP endpoints for organizations and entities
- **middleware/auth.go**: Bearer token authentication
- **services/**: Business logic for entity and organization management

#### 2. Database Layer (`/db`)
- SQLite database with auto-initialization
- Schema defined in `schema.sql`
- Service pattern with connection management

#### 3. Event System (`/pkg/services/workers`)
- **manager.go**: Orchestrates all workers
- **entity.go**: Processes entity CRUD events
- **command.go**: Handles command distribution
- **telemetry.go**: Processes telemetry streams
- **event.go**: General event processing

#### 4. NATS Integration (`/pkg/services/embedded-nats`)
- Embedded NATS server with JetStream
- Streams:
  - CONSTELLATION_ENTITIES: Entity lifecycle events
  - CONSTELLATION_COMMANDS: Command messages
  - CONSTELLATION_TELEMETRY: Telemetry data
  - CONSTELLATION_EVENTS: General events

### Key Patterns

#### Event-Driven Architecture
All state changes publish events to NATS subjects:
- `constellation.entities.{org_id}.created`
- `constellation.entities.{org_id}.updated`
- `constellation.entities.{org_id}.deleted`
- `constellation.telemetry.{org_id}.{entity_id}`
- `constellation.commands.{org_id}.{entity_id}`

#### API Authentication
All API requests require Bearer token authentication:
```bash
Authorization: Bearer {token}
```

#### Database Auto-Initialization
The database service automatically:
- Creates the SQLite file if missing
- Initializes schema from `schema.sql`
- Verifies schema on startup

### Entity Types
Defined in `prd/design/CONSTELLATION_TAK_ONTOLOGY.md`:
- `aircraft_multirotor`: Drones/UAVs
- `ground_vehicle`: Ground robots/vehicles
- `sensor_fixed`: Stationary sensors
- `control_station`: Command centers
- And many more...

### Testing Approach
- Unit tests: `go test ./...`
- No specific test framework configured
- Test files should follow Go convention: `*_test.go`

### Important Files
- `cmd/microlith/main.go`: Entry point, initializes all services
- `pkg/shared/types.go`: Shared message types for NATS
- `pkg/shared/subjects.go`: NATS subject definitions
- `db/schema.sql`: Database schema definition

### Development Workflow
1. Make changes to relevant services
2. Run `go fmt ./...` to format code
3. Run `go vet ./...` to check for issues
4. Test locally with `go run ./cmd/microlith/main.go`
5. Build binary with `go build`

### Common Tasks
- **Add new entity type**: Update ontology definitions and entity service
- **Add new API endpoint**: Update `api/handlers.go` and corresponding service
- **Add new event type**: Define in `pkg/shared/types.go` and create worker
- **Modify database schema**: Update `db/schema.sql` and migration logic