# Build Stage
FROM golang:1.24-bookworm AS builder

WORKDIR /app

# Install build dependencies
RUN apt-get update && apt-get install -y git make build-essential

# Copy go mod and sum files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=1 GOOS=linux go build -o /app/bin/overwatch ./cmd/microlith

# Run Stage
FROM debian:bookworm-slim

WORKDIR /app

# Install runtime dependencies
RUN apt-get update && apt-get install -y ca-certificates tzdata && rm -rf /var/lib/apt/lists/*

# Create non-root user
RUN groupadd -r constellation && useradd -r -g constellation constellation

# Create necessary directories with correct permissions
RUN mkdir -p /app/data/nats /app/logs /app/certs && \
    chown -R constellation:constellation /app

# Copy binary from builder
COPY --from=builder /app/bin/overwatch /app/overwatch

# Copy configuration files
COPY nats.conf /app/nats.conf

# Copy static assets
COPY pkg/services/web/static /app/pkg/services/web/static

# Switch to non-root user
USER constellation

# Expose ports
# 4222: NATS Client
# 8222: NATS HTTP/WS
# 8080: App HTTP (if applicable)
EXPOSE 4222 8222 8080

# Set entrypoint
ENTRYPOINT ["/app/overwatch"]
