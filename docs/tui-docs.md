# TUI Dashboard Documentation

Constellation Overwatch includes a Terminal User Interface (TUI) dashboard for real-time system monitoring. This document covers installation, usage, architecture, and customization of the TUI.

---

## Table of Contents

1. [Quick Start](#quick-start)
2. [Features](#features)
3. [Keyboard Controls](#keyboard-controls)
4. [Panel Reference](#panel-reference)
5. [Architecture](#architecture)
6. [Configuration](#configuration)
7. [Troubleshooting](#troubleshooting)
8. [Development Guide](#development-guide)

---

## Quick Start

### Running the TUI

```bash
# Using Task (recommended)
task tui

# Direct execution
go run ./cmd/microlith/main.go --tui

# Built binary
./constellation-overwatch --tui
```

### Minimum Requirements

- Terminal size: 120x40 characters (recommended)
- Terminal with Unicode support (for box-drawing characters)
- True color support (for optimal color rendering)

### Exiting

Press `q` or `Ctrl+C` to gracefully shutdown the TUI and server.

---

## Features

### Real-Time Monitoring

The TUI provides live monitoring of:

- **System Resources**: CPU usage, memory consumption, goroutine count, uptime
- **Worker Health**: Status of all registered background workers
- **Application Logs**: Color-coded, scrollable log output
- **Entity Registry**: Connected entities with live/stale status
- **NATS Streams**: Message counts, consumer status, storage usage

### Visual Design

The TUI uses a military/C4ISR-inspired color theme:

| Element | Color | Hex Code |
|---------|-------|----------|
| Primary | Cyan | `#00d4ff` |
| Secondary | Teal | `#0fbfbf` |
| Accent (Focused) | Red | `#ff6b6b` |
| Success/OK | Green | `#00ff00` |
| Warning | Yellow | `#ffff00` |
| Error | Red | `#ff0000` |
| Muted | Gray | `#888888` |

### Panel Layout

```
┌─────────────────────────────────┬─────────────────────────────────┐
│         SYSTEM METRICS          │            WORKERS              │
│                                 │                                 │
│  CPU:  ████████░░░░░░░░  45%   │  EntityWorker      [OK]         │
│  MEM:  ██████░░░░░░░░░░  35%   │  TelemetryWorker   [OK]         │
│  GOR:  156                      │  CommandWorker     [OK]         │
│  UP:   2h 34m 12s              │  EventWorker       [OK]         │
│                                 │                                 │
├─────────────────────────────────┼─────────────────────────────────┤
│              LOGS               │           ENTITIES              │
│                                 │                                 │
│  14:32:05 [INF] Server started │  drone-alpha-1     LIVE         │
│  14:32:06 [INF] NATS connected │  drone-beta-2      LIVE         │
│  14:32:07 [DBG] Worker init    │  sensor-fixed-3    ----         │
│  14:32:08 [WRN] High latency   │  control-station   LIVE         │
│  14:32:09 [INF] Entity created │                                 │
│                                 │                                 │
└─────────────────────────────────┴─────────────────────────────────┘
  Tab:next  Shift+Tab:prev  ?:help  q:quit
```

---

## Keyboard Controls

### Global Navigation

| Key | Action | Description |
|-----|--------|-------------|
| `Tab` | Next Panel | Move focus to the next panel (clockwise) |
| `Shift+Tab` | Previous Panel | Move focus to the previous panel |
| `q` | Quit | Gracefully shutdown TUI and server |
| `Ctrl+C` | Force Quit | Immediately exit |
| `?` | Toggle Help | Show/hide full keybinding help overlay |
| `r` | Refresh | Force immediate refresh of all data |

### Panel-Specific Controls

#### Logs Panel

| Key | Action |
|-----|--------|
| `Up` / `k` | Scroll up one line |
| `Down` / `j` | Scroll down one line |
| `Page Up` | Scroll up one page |
| `Page Down` | Scroll down one page |
| `Home` / `g` | Jump to oldest log |
| `End` / `G` | Jump to newest log |

#### Entities Panel

| Key | Action |
|-----|--------|
| `Up` / `k` | Select previous entity |
| `Down` / `j` | Select next entity |
| `t` | Toggle between Entities and Streams view |
| `Enter` | View entity details (future) |

---

## Panel Reference

### System Metrics Panel

Displays real-time system resource utilization.

**Metrics:**

| Metric | Description | Update Rate |
|--------|-------------|-------------|
| CPU | Current CPU utilization percentage | 1 second |
| MEM | Allocated memory in MB/GB | 1 second |
| GOR | Number of active goroutines | 1 second |
| UP | Server uptime since start | 1 second |

**Visual Elements:**

- ASCII gauge bars show resource utilization
- Color changes from green to yellow to red based on thresholds
- Uptime formatted as `Xh Xm Xs`

### Workers Panel

Displays health status of all registered background workers.

**Status Indicators:**

| Badge | Meaning |
|-------|---------|
| `[OK]` (green) | Worker is healthy |
| `[ERR]` (red) | Worker health check failed |

**Workers Monitored:**

- EntityWorker - Processes entity CRUD events
- TelemetryWorker - Handles telemetry streams
- CommandWorker - Distributes commands to entities
- EventWorker - General event processing

### Logs Panel

Scrollable viewport displaying application logs in real-time.

**Log Levels:**

| Level | Color | Badge |
|-------|-------|-------|
| DEBUG | Cyan | `[DBG]` |
| INFO | Green | `[INF]` |
| WARN | Yellow | `[WRN]` |
| ERROR | Red | `[ERR]` |
| FATAL | Bold Red | `[FTL]` |

**Format:**

```
HH:MM:SS [LVL] Message text...
```

**Buffer:**

- Maximum 500 log entries retained
- Oldest entries automatically removed when buffer is full
- Auto-scroll to bottom when new logs arrive (if already at bottom)

### Entities Panel

Toggleable view showing either Entity Registry or NATS Stream statistics.

#### Entities View

| Column | Description |
|--------|-------------|
| Name | Entity identifier |
| Type | Entity type (drone, sensor, etc.) |
| Org | Organization ID |
| Status | `LIVE` (green) or `----` (gray) |

**Live Status:**

Entities are considered "live" if they have sent telemetry within the last 30 seconds.

#### Streams View

| Column | Description |
|--------|-------------|
| Stream | NATS stream name |
| Messages | Total message count |
| Consumers | Active consumer count |
| Storage | Bytes stored |

**Streams Monitored:**

- `CONSTELLATION_ENTITIES`
- `CONSTELLATION_COMMANDS`
- `CONSTELLATION_TELEMETRY`
- `CONSTELLATION_EVENTS`

---

## Architecture

### Package Structure

```
pkg/tui/
├── app.go              # Root application model (AppModel)
├── keys.go             # KeyMap definitions
├── messages.go         # Tea message types (TickMsg, RefreshMsg)
├── styles/
│   └── styles.go       # Lipgloss styles and color theme
├── panels/
│   ├── system.go       # SystemModel - metrics display
│   ├── workers.go      # WorkersModel - worker health
│   ├── logs.go         # LogsModel - scrollable logs
│   └── entities.go     # EntitiesModel - entities/streams
└── datasource/
    ├── types.go        # Shared data types
    ├── metrics.go      # RuntimeMetrics collector
    ├── logs.go         # LogSource adapter
    ├── workers.go      # WorkersMonitor
    ├── entities.go     # EntityMonitor
    └── nats.go         # NATSStats collector
```

### Data Flow

```
┌──────────────────────────────────────────────────────────────────┐
│                        AppModel (app.go)                         │
│                                                                  │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────┐ │
│  │ SystemPanel │  │WorkersPanel │  │  LogsPanel  │  │Entities │ │
│  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘  └────┬────┘ │
│         │                │                │              │       │
└─────────┼────────────────┼────────────────┼──────────────┼───────┘
          │                │                │              │
    ┌─────▼─────┐    ┌─────▼─────┐    ┌─────▼─────┐  ┌────▼─────┐
    │  Runtime  │    │  Workers  │    │    Log    │  │  Entity  │
    │  Metrics  │    │  Monitor  │    │   Source  │  │  Monitor │
    └─────┬─────┘    └─────┬─────┘    └─────┬─────┘  └────┬─────┘
          │                │                │              │
    ┌─────▼─────┐    ┌─────▼─────┐    ┌─────▼─────┐  ┌────▼─────┐
    │  runtime  │    │  Manager  │    │  TUIHook  │  │ Registry │
    │  package  │    │  (workers)│    │  (logger) │  │   (KV)   │
    └───────────┘    └───────────┘    └───────────┘  └──────────┘
```

### Update Cycle

The TUI uses a tick-based update system with staggered refresh rates:

| Data Source | Refresh Rate | Tick Interval |
|-------------|--------------|---------------|
| System Metrics | 1 second | Every tick |
| Workers Status | 5 seconds | Every 5 ticks |
| Entity Registry | 3 seconds | Every 3 ticks |
| NATS Streams | 5 seconds | Every 5 ticks |
| Logs | Real-time | Channel-based |

### Log Capture

Logs are captured via a custom Zap logger hook:

```go
// pkg/services/logger/hook.go
type TUIHook struct {
    entries chan LogEntry  // Buffered channel (1000 entries)
    mu      sync.Mutex
    closed  bool
}
```

**Flow:**

1. Logger writes to TUIHook via wrapped Zap core
2. Hook sends entry to buffered channel (non-blocking)
3. LogSource adapter reads from channel
4. AppModel forwards entries to LogsPanel
5. LogsPanel updates viewport and auto-scrolls

---

## Configuration

### Environment Variables

Currently, the TUI does not support environment variable configuration. All settings are compiled defaults.

### Future Configuration Options

Planned configuration options for future releases:

```yaml
tui:
  refresh_rate: 1s
  log_buffer_size: 500
  theme: military  # military, dark, light
  panels:
    system: true
    workers: true
    logs: true
    entities: true
```

---

## Troubleshooting

### Common Issues

#### TUI displays garbled characters

**Cause:** Terminal doesn't support Unicode box-drawing characters.

**Solution:** Use a modern terminal emulator (iTerm2, Alacritty, Windows Terminal, Kitty).

#### Colors look wrong or missing

**Cause:** Terminal doesn't support true color (24-bit color).

**Solution:**
- Enable true color in your terminal settings
- Set `COLORTERM=truecolor` environment variable
- Use a terminal that supports true color

#### Panel content is truncated

**Cause:** Terminal window is too small.

**Solution:** Resize terminal to at least 120x40 characters.

#### Logs panel shows "Loading logs..."

**Cause:** Panel hasn't received size information yet.

**Solution:** This should resolve automatically. If persistent, resize the terminal window.

#### No workers showing in Workers panel

**Cause:** Workers haven't been registered yet.

**Solution:** Wait for server initialization to complete. Workers register during boot sequence.

### Debug Mode

To debug TUI issues, run without TUI mode and check standard log output:

```bash
go run ./cmd/microlith/main.go  # Standard mode with console logging
```

---

## Development Guide

### Adding a New Panel

1. **Create panel model** in `pkg/tui/panels/`:

```go
package panels

import (
    "github.com/Constellation-Overwatch/constellation-overwatch/pkg/tui/styles"
    tea "github.com/charmbracelet/bubbletea"
)

type NewPanelModel struct {
    width   int
    height  int
    focused bool
    // ... panel-specific state
}

func NewNewPanelModel() NewPanelModel {
    return NewPanelModel{}
}

func (m NewPanelModel) Init() tea.Cmd { return nil }

func (m NewPanelModel) Update(msg tea.Msg) (NewPanelModel, tea.Cmd) {
    // Handle messages
    return m, nil
}

func (m NewPanelModel) View() string {
    // Render panel content
    return "Panel content"
}

func (m *NewPanelModel) SetSize(width, height int) {
    m.width = width
    m.height = height
}

func (m *NewPanelModel) SetFocused(focused bool) {
    m.focused = focused
}
```

2. **Create data source** in `pkg/tui/datasource/` (if needed)

3. **Register panel** in `pkg/tui/app.go`:
   - Add field to `AppModel`
   - Initialize in `NewApp()`
   - Add to `updatePanelSizes()`
   - Add to `View()` layout
   - Handle in `Update()` switch

4. **Update panel count** in `app.go`:

```go
const (
    PanelSystem = iota
    PanelWorkers
    PanelLogs
    PanelEntities
    PanelNewPanel  // Add new panel
    PanelCount
)
```

### Adding a New Data Source

1. **Define types** in `pkg/tui/datasource/types.go`

2. **Create collector** in `pkg/tui/datasource/`:

```go
package datasource

type NewDataSource struct {
    // Dependencies
}

func NewNewDataSource(/* deps */) *NewDataSource {
    return &NewDataSource{}
}

func (s *NewDataSource) Collect() NewDataMsg {
    // Collect and return data
    return NewDataMsg{}
}
```

3. **Create message type** in `pkg/tui/panels/` or panel file:

```go
type NewDataMsg struct {
    // Message fields
}
```

4. **Wire in AppModel** to call collector and route messages

### Styling Guidelines

Use styles from `pkg/tui/styles/styles.go`:

```go
import "github.com/Constellation-Overwatch/constellation-overwatch/pkg/tui/styles"

// Use predefined styles
content := styles.StatusOKStyle.Render("[OK]")
title := styles.TitleStyle.Render("PANEL TITLE")

// Use color constants
myStyle := lipgloss.NewStyle().Foreground(styles.PrimaryColor)
```

### Testing Panels

Panels can be tested independently:

```go
func TestSystemPanel(t *testing.T) {
    panel := panels.NewSystemModel()
    panel.SetSize(80, 20)

    // Send update message
    msg := panels.MetricsUpdateMsg{
        CPUPercent: 45.0,
        MemAlloc:   1024 * 1024 * 100,
        NumGoroutine: 50,
        Uptime:     time.Hour,
    }

    panel, _ = panel.Update(msg)
    view := panel.View()

    // Assert view contains expected content
    if !strings.Contains(view, "45%") {
        t.Error("CPU percentage not displayed")
    }
}
```

---

## API Reference

### AppModel

The root model that orchestrates all panels and data sources.

```go
type AppModel struct {
    // Panels
    systemPanel   panels.SystemModel
    workersPanel  panels.WorkersModel
    logsPanel     panels.LogsModel
    entitiesPanel panels.EntitiesModel

    // Data sources
    metricsSource  *datasource.RuntimeMetrics
    workersMonitor *datasource.WorkersMonitor
    entityMonitor  *datasource.EntityMonitor
    natsStats      *datasource.NATSStats
    logSource      *datasource.LogSource

    // State
    focusedPanel int
    showHelp     bool
    keys         KeyMap
    width        int
    height       int
    ready        bool
    tickCount    int
    quitting     bool
}
```

### DataSources

Dependency injection struct for TUI initialization:

```go
type DataSources struct {
    WorkerManager *workers.Manager
    JetStream     nats.JetStreamContext
    KeyValue      nats.KeyValue
    LogHook       *logger.TUIHook
}
```

### Run Function

Entry point for starting the TUI:

```go
func Run(sources DataSources) error
```

---

## Changelog

### Version 1.1.0 (January 2026)

- Initial TUI dashboard implementation
- Four-panel layout: System, Workers, Logs, Entities
- Keyboard navigation with Tab cycling
- Real-time log capture via Zap hook
- Entity/Streams toggle view
- Military/C4ISR color theme
