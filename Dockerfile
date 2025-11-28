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

# Debug: Check module name and imports
RUN go list -m
RUN grep -A 20 "import (" cmd/microlith/main.go

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
RUN mkdir -p /data /app && \
    chown -R constellation:constellation /data /app

# Copy binary from builder
COPY --from=builder /app/bin/overwatch /app/overwatch

# Copy configuration files
COPY nats.conf /app/nats.conf

# Note: Static assets are embedded in the binary via go:embed, no separate copy needed

# Expose ports
# 4222: NATS Client
# 8222: NATS HTTP/WS
# 8080: App HTTP (if applicable)
EXPOSE 4222 8222 8080

# Set entrypoint
ENTRYPOINT ["/app/overwatch"]