# NATS Integration Guide

This guide provides instructions for connecting to the Constellation Overwatch NATS cluster.

## Connection Details

- **Protocol**: NATS (TCP)
- **Default Port**: `4222`
- **Websocket Port**: `8222` (if enabled)

### Authentication

The NATS cluster uses token-based authentication.

**Credentials:**
- **Token**: Provided by administrator (e.g., `your-secret-token`)

## Standard Streams & Subjects

| Stream Name | Subjects | Retention | Description |
| :--- | :--- | :--- | :--- |
| `CONSTELLATION_ENTITIES` | `constellation.entities.>` | Limits | Entity state updates |
| `CONSTELLATION_EVENTS` | `constellation.events.>` | WorkQueue | System events |
| `CONSTELLATION_TELEMETRY` | `constellation.telemetry.>` | Limits | High-frequency telemetry |
| `CONSTELLATION_COMMANDS` | `constellation.commands.>` | WorkQueue | Command and control |

## Code Examples

### Go

```go
import (
    "github.com/nats-io/nats.go"
    "log"
)

func main() {
    // Connect to NATS
    nc, err := nats.Connect("nats://localhost:4222", 
        nats.Token("your-secret-token"))
    if err != nil {
        log.Fatal(err)
    }
    defer nc.Close()

    // Publish a message
    err = nc.Publish("constellation.telemetry.drone1.position", []byte("..."))
    if err != nil {
        log.Fatal(err)
    }
}
```

### Python

```python
import asyncio
import nats

async def main():
    # Connect to NATS
    nc = await nats.connect("nats://localhost:4222", token="your-secret-token")

    # Publish a message
    await nc.publish("constellation.telemetry.drone1.position", b"...")

    await nc.close()

if __name__ == '__main__':
    asyncio.run(main())
```

### CLI (nats-box)

```bash
# Context setup
nats context save overwatch --server nats://localhost:4222 --token your-secret-token

# Select context
nats context select overwatch

# Publish
nats pub constellation.telemetry.drone1.position "..."

# Subscribe
nats sub constellation.telemetry.>
```
