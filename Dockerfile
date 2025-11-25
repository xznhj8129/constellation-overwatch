# Build Stage
# NOTE: Must use glibc-based image (not Alpine/musl) because go-libsql is compiled against glibc
FROM golang:1.24-bookworm AS builder

WORKDIR /app

# Install build dependencies (combined and cleaned)
RUN apt-get update && apt-get install -y --no-install-recommends \
    git make build-essential \
    && rm -rf /var/lib/apt/lists/*

# Copy go mod files first (better layer caching)
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Copy source code
COPY . .

# Build the application with optimizations
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o /app/bin/overwatch ./cmd/microlith

# Run Stage
FROM debian:bookworm-slim

WORKDIR /app

# Install runtime dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates tzdata \
    && rm -rf /var/lib/apt/lists/*

# Create non-root user
RUN groupadd -r constellation && useradd -r -g constellation constellation

# Create necessary directories
RUN mkdir -p /data && chown -R constellation:constellation /data /app

# Copy binary from builder
COPY --from=builder --chown=constellation:constellation /app/bin/overwatch /app/overwatch

# Copy configuration and static assets
COPY --chown=constellation:constellation nats.conf /app/nats.conf
COPY --chown=constellation:constellation pkg/services/web/static /app/pkg/services/web/static

# Switch to non-root user
USER constellation

# Expose ports
# 4222: NATS Client
# 8222: NATS HTTP/WS
# 8080: App HTTP (if applicable)
EXPOSE 4222 8222 8080

ENTRYPOINT ["/app/overwatch"]
