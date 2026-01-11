package panels

import (
	"fmt"
	"strings"

	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/tui/datasource"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/tui/styles"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ViewMode represents the current view mode
type ViewMode int

const (
	ViewEntities ViewMode = iota
	ViewStreams
)

// EntitiesUpdateMsg contains updated entity data
type EntitiesUpdateMsg struct {
	Entities []datasource.EntitySummary
}

// StreamsUpdateMsg contains updated stream stats
type StreamsUpdateMsg struct {
	Streams []datasource.StreamStat
}

// EntitiesModel displays entities or NATS streams
type EntitiesModel struct {
	entities  []datasource.EntitySummary
	streams   []datasource.StreamStat
	viewMode  ViewMode
	cursor    int
	scrollPos int
	width     int
	height    int
	focused   bool
}

// NewEntitiesModel creates a new entities panel
func NewEntitiesModel() EntitiesModel {
	return EntitiesModel{
		entities: make([]datasource.EntitySummary, 0),
		streams:  make([]datasource.StreamStat, 0),
		viewMode: ViewEntities,
	}
}

func (m EntitiesModel) Init() tea.Cmd {
	return nil
}

func (m EntitiesModel) Update(msg tea.Msg) (EntitiesModel, tea.Cmd) {
	switch msg := msg.(type) {
	case EntitiesUpdateMsg:
		m.entities = msg.Entities
	case StreamsUpdateMsg:
		m.streams = msg.Streams
	case tea.KeyMsg:
		if m.focused {
			switch msg.String() {
			case "up", "k":
				if m.cursor > 0 {
					m.cursor--
					if m.cursor < m.scrollPos {
						m.scrollPos = m.cursor
					}
				}
			case "down", "j":
				maxItems := m.maxItems()
				if m.cursor < maxItems-1 {
					m.cursor++
					visibleRows := m.visibleRows()
					if m.cursor >= m.scrollPos+visibleRows {
						m.scrollPos = m.cursor - visibleRows + 1
					}
				}
			}
		}
	}
	return m, nil
}

func (m EntitiesModel) maxItems() int {
	if m.viewMode == ViewEntities {
		return len(m.entities)
	}
	return len(m.streams)
}

func (m EntitiesModel) visibleRows() int {
	// Account for header row
	rows := m.height - 4
	if rows < 1 {
		rows = 1
	}
	return rows
}

func (m EntitiesModel) View() string {
	var content strings.Builder

	// View mode indicator
	modeHint := styles.StatusMutedStyle.Render("[press 'v' to toggle view]")
	content.WriteString(modeHint + "\n")

	if m.viewMode == ViewEntities {
		content.WriteString(m.renderEntities())
	} else {
		content.WriteString(m.renderStreams())
	}

	return content.String()
}

func (m EntitiesModel) renderEntities() string {
	var sb strings.Builder

	// Header
	header := fmt.Sprintf("%-12s %-10s %-6s", "ID", "Type", "Live")
	sb.WriteString(styles.TableHeaderStyle.Render(header) + "\n")

	if len(m.entities) == 0 {
		sb.WriteString(styles.StatusMutedStyle.Render("No entities"))
		return sb.String()
	}

	// Determine visible range
	visibleRows := m.visibleRows()
	endIdx := m.scrollPos + visibleRows
	if endIdx > len(m.entities) {
		endIdx = len(m.entities)
	}

	for i := m.scrollPos; i < endIdx; i++ {
		entity := m.entities[i]

		// Truncate ID if needed
		id := entity.ID
		if len(id) > 12 {
			id = id[:9] + "..."
		}

		// Get short type name
		typeName := m.shortTypeName(entity.EntityType)

		// Live indicator
		live := styles.LiveIndicator(entity.IsLive)

		row := fmt.Sprintf("%-12s %-10s %s", id, typeName, live)

		// Highlight if selected
		if i == m.cursor && m.focused {
			row = styles.TableSelectedStyle.Render(row)
		}

		sb.WriteString(row + "\n")
	}

	return strings.TrimSuffix(sb.String(), "\n")
}

func (m EntitiesModel) renderStreams() string {
	var sb strings.Builder

	// Header
	header := fmt.Sprintf("%-25s %10s %12s", "Stream", "Messages", "Size")
	sb.WriteString(styles.TableHeaderStyle.Render(header) + "\n")

	if len(m.streams) == 0 {
		sb.WriteString(styles.StatusMutedStyle.Render("No streams"))
		return sb.String()
	}

	// Determine visible range
	visibleRows := m.visibleRows()
	endIdx := m.scrollPos + visibleRows
	if endIdx > len(m.streams) {
		endIdx = len(m.streams)
	}

	for i := m.scrollPos; i < endIdx; i++ {
		stream := m.streams[i]

		// Format size
		size := formatBytes(stream.Bytes)

		row := fmt.Sprintf("%-25s %10d %12s", stream.Name, stream.Messages, size)

		// Highlight if selected
		if i == m.cursor && m.focused {
			row = styles.TableSelectedStyle.Render(row)
		}

		sb.WriteString(row + "\n")
	}

	return strings.TrimSuffix(sb.String(), "\n")
}

func (m EntitiesModel) shortTypeName(entityType string) string {
	// Map entity types to short display names
	names := map[string]string{
		"aircraft_multirotor":    "Drone",
		"aircraft_fixed_wing":    "FixedWing",
		"aircraft_vtol":          "VTOL",
		"aircraft_helicopter":    "Heli",
		"ground_vehicle_wheeled": "GndVeh",
		"ground_vehicle_tracked": "Tracked",
		"surface_vessel_usv":     "USV",
		"underwater_vehicle":     "UUV",
		"sensor_platform":        "Sensor",
		"payload_system":         "Payload",
		"operator_station":       "OpSta",
	}

	if name, ok := names[entityType]; ok {
		return name
	}
	if len(entityType) > 10 {
		return entityType[:7] + "..."
	}
	return entityType
}

func formatBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := uint64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// SetSize sets the panel dimensions
func (m *EntitiesModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// SetFocused sets whether this panel is focused
func (m *EntitiesModel) SetFocused(focused bool) {
	m.focused = focused
}

// Focused returns whether this panel is focused
func (m EntitiesModel) Focused() bool {
	return m.focused
}

// Title returns the panel title
func (m EntitiesModel) Title() string {
	if m.viewMode == ViewEntities {
		return "ENTITIES"
	}
	return "NATS STREAMS"
}

// ToggleView toggles between entities and streams view
func (m *EntitiesModel) ToggleView() {
	if m.viewMode == ViewEntities {
		m.viewMode = ViewStreams
	} else {
		m.viewMode = ViewEntities
	}
	m.cursor = 0
	m.scrollPos = 0
}

// RenderWithBorder renders the panel with its border
func (m EntitiesModel) RenderWithBorder() string {
	style := styles.PanelStyle
	titleStyle := styles.TitleStyle
	if m.focused {
		style = styles.FocusedPanelStyle
		titleStyle = styles.FocusedTitleStyle
	}

	title := titleStyle.Render(m.Title())
	content := m.View()

	// Apply size constraints
	style = style.Width(m.width).Height(m.height)

	return lipgloss.JoinVertical(lipgloss.Left,
		title,
		style.Render(content),
	)
}
