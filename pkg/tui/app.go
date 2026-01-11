package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/logger"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/workers"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/tui/datasource"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/tui/panels"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/tui/styles"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/nats-io/nats.go"
)

// Panel indices
const (
	PanelSystem = iota
	PanelWorkers
	PanelLogs
	PanelEntities
	PanelCount
)

// Tick intervals
const (
	tickInterval = 1 * time.Second
)

// DataSources holds all data source dependencies
type DataSources struct {
	WorkerManager *workers.Manager
	JetStream     nats.JetStreamContext
	KeyValue      nats.KeyValue
	LogHook       *logger.TUIHook
}

// AppModel is the root TUI model
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

	// Tick counters for different refresh rates
	tickCount int

	// Quit channel
	quitting bool
}

// NewApp creates a new TUI application
func NewApp(sources DataSources) *AppModel {
	// Create data sources
	metricsSource := datasource.NewRuntimeMetrics()
	workersMonitor := datasource.NewWorkersMonitor(sources.WorkerManager)
	entityMonitor := datasource.NewEntityMonitor(sources.WorkerManager.GetRegistry(), sources.KeyValue)
	natsStats := datasource.NewNATSStats(sources.JetStream)
	logSource := datasource.NewLogSource(sources.LogHook)

	return &AppModel{
		systemPanel:    panels.NewSystemModel(),
		workersPanel:   panels.NewWorkersModel(),
		logsPanel:      panels.NewLogsModel(),
		entitiesPanel:  panels.NewEntitiesModel(),
		metricsSource:  metricsSource,
		workersMonitor: workersMonitor,
		entityMonitor:  entityMonitor,
		natsStats:      natsStats,
		logSource:      logSource,
		focusedPanel:   PanelLogs,
		keys:           DefaultKeyMap(),
	}
}

// Init initializes the model
func (m AppModel) Init() tea.Cmd {
	return tea.Batch(
		m.tickCmd(),
		m.listenForLogs(),
	)
}

// tickCmd returns a command that ticks periodically
func (m AppModel) tickCmd() tea.Cmd {
	return tea.Tick(tickInterval, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}

// listenForLogs returns a command that listens for log entries
func (m AppModel) listenForLogs() tea.Cmd {
	return func() tea.Msg {
		entry, ok := <-m.logSource.Channel()
		if !ok {
			return nil
		}
		return panels.LogEntryMsg{
			Time:    entry.Time,
			Level:   entry.Level,
			Message: entry.Message,
			Fields:  entry.Fields,
		}
	}
}

// Update handles messages
func (m AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.updatePanelSizes()
		m.ready = true

	case tea.KeyMsg:
		// Global key handling
		switch {
		case key.Matches(msg, m.keys.Quit):
			m.quitting = true
			return m, tea.Quit

		case key.Matches(msg, m.keys.Help):
			m.showHelp = !m.showHelp

		case key.Matches(msg, m.keys.NextTab):
			m.focusedPanel = (m.focusedPanel + 1) % PanelCount
			m.updateFocus()

		case key.Matches(msg, m.keys.PrevTab):
			m.focusedPanel = (m.focusedPanel - 1 + PanelCount) % PanelCount
			m.updateFocus()

		case key.Matches(msg, m.keys.Toggle):
			if m.focusedPanel == PanelEntities {
				m.entitiesPanel.ToggleView()
			}

		case key.Matches(msg, m.keys.Refresh):
			cmds = append(cmds, m.refreshAll())
		}

		// Panel-specific key handling
		switch m.focusedPanel {
		case PanelLogs:
			newLogs, cmd := m.logsPanel.Update(msg)
			m.logsPanel = newLogs
			cmds = append(cmds, cmd)
		case PanelEntities:
			newEntities, cmd := m.entitiesPanel.Update(msg)
			m.entitiesPanel = newEntities
			cmds = append(cmds, cmd)
		}

	case TickMsg:
		m.tickCount++

		// Always update metrics (every tick = 1s)
		metricsMsg := m.metricsSource.Collect()
		m.systemPanel, _ = m.systemPanel.Update(metricsMsg)

		// Update workers every 5 ticks (5s)
		if m.tickCount%5 == 0 {
			statuses := m.workersMonitor.GetStatuses()
			workersMsg := panels.WorkersUpdateMsg{Workers: statuses}
			m.workersPanel, _ = m.workersPanel.Update(workersMsg)
		}

		// Update entities every 3 ticks (3s)
		if m.tickCount%3 == 0 {
			entities := m.entityMonitor.GetEntities()
			entitiesMsg := panels.EntitiesUpdateMsg{Entities: entities}
			m.entitiesPanel, _ = m.entitiesPanel.Update(entitiesMsg)
		}

		// Update streams every 5 ticks (5s)
		if m.tickCount%5 == 0 {
			streams := m.natsStats.GetStreamStats()
			streamsMsg := panels.StreamsUpdateMsg{Streams: streams}
			m.entitiesPanel, _ = m.entitiesPanel.Update(streamsMsg)
		}

		// Continue ticking
		cmds = append(cmds, m.tickCmd())

	case panels.LogEntryMsg:
		m.logsPanel, _ = m.logsPanel.Update(msg)
		// Continue listening for logs
		cmds = append(cmds, m.listenForLogs())
	}

	return m, tea.Batch(cmds...)
}

// refreshAll forces a refresh of all data
func (m *AppModel) refreshAll() tea.Cmd {
	return func() tea.Msg {
		return RefreshMsg{}
	}
}

// updateFocus updates the focused state of all panels
func (m *AppModel) updateFocus() {
	m.systemPanel.SetFocused(m.focusedPanel == PanelSystem)
	m.workersPanel.SetFocused(m.focusedPanel == PanelWorkers)
	m.logsPanel.SetFocused(m.focusedPanel == PanelLogs)
	m.entitiesPanel.SetFocused(m.focusedPanel == PanelEntities)
}

// updatePanelSizes calculates and sets panel sizes based on terminal dimensions
func (m *AppModel) updatePanelSizes() {
	if m.width == 0 || m.height == 0 {
		return
	}

	// Calculate dimensions
	// Layout: 2 columns, 2 rows + help bar
	halfWidth := m.width / 2
	topHeight := (m.height - 3) / 3        // Top row (System + Workers)
	bottomHeight := (m.height - 3) * 2 / 3 // Bottom row (Logs + Entities)

	// Set panel sizes (accounting for borders and padding)
	m.systemPanel.SetSize(halfWidth-4, topHeight-3)
	m.workersPanel.SetSize(halfWidth-4, topHeight-3)
	m.logsPanel.SetSize(halfWidth-4, bottomHeight-3)
	m.entitiesPanel.SetSize(halfWidth-4, bottomHeight-3)

	// Update focus state
	m.updateFocus()
}

// View renders the UI
func (m AppModel) View() string {
	if !m.ready {
		return "Initializing..."
	}

	if m.quitting {
		return "Shutting down...\n"
	}

	// Calculate dimensions
	halfWidth := m.width / 2
	topHeight := (m.height - 3) / 3
	bottomHeight := (m.height - 3) * 2 / 3

	// Render panels with borders
	systemView := m.renderPanel("SYSTEM METRICS", m.systemPanel.View(), halfWidth-2, topHeight, m.focusedPanel == PanelSystem)
	workersView := m.renderPanel("WORKERS", m.workersPanel.View(), halfWidth-2, topHeight, m.focusedPanel == PanelWorkers)
	logsView := m.renderPanel("LOGS", m.logsPanel.View(), halfWidth-2, bottomHeight, m.focusedPanel == PanelLogs)
	entitiesView := m.renderPanel(m.entitiesPanel.Title(), m.entitiesPanel.View(), halfWidth-2, bottomHeight, m.focusedPanel == PanelEntities)

	// Compose top row
	topRow := lipgloss.JoinHorizontal(lipgloss.Top, systemView, workersView)

	// Compose bottom row
	bottomRow := lipgloss.JoinHorizontal(lipgloss.Top, logsView, entitiesView)

	// Compose main content
	mainContent := lipgloss.JoinVertical(lipgloss.Left, topRow, bottomRow)

	// Help bar
	helpBar := m.renderHelpBar()

	// Final composition
	return lipgloss.JoinVertical(lipgloss.Left, mainContent, helpBar)
}

// renderPanel renders a panel with title and border
func (m AppModel) renderPanel(title, content string, width, height int, focused bool) string {
	style := styles.PanelStyle.Copy().Width(width - 4).Height(height - 2)
	titleStyle := styles.TitleStyle

	if focused {
		style = styles.FocusedPanelStyle.Copy().Width(width - 4).Height(height - 2)
		titleStyle = styles.FocusedTitleStyle
	}

	// Render title
	renderedTitle := titleStyle.Render(title)

	// Render content in panel
	renderedContent := style.Render(content)

	return lipgloss.JoinVertical(lipgloss.Left, renderedTitle, renderedContent)
}

// renderHelpBar renders the help bar at the bottom
func (m AppModel) renderHelpBar() string {
	if m.showHelp {
		return m.renderFullHelp()
	}

	var helpItems []string
	for _, binding := range m.keys.ShortHelp() {
		keyStr := styles.HelpKeyStyle.Render(binding.Help().Key)
		descStr := styles.HelpDescStyle.Render(binding.Help().Desc)
		helpItems = append(helpItems, fmt.Sprintf("%s:%s", keyStr, descStr))
	}

	return styles.HelpBarStyle.Render(strings.Join(helpItems, "  "))
}

// renderFullHelp renders the full help overlay
func (m AppModel) renderFullHelp() string {
	var sb strings.Builder
	sb.WriteString("Keybindings:\n")

	for _, row := range m.keys.FullHelp() {
		for _, binding := range row {
			keyStr := styles.HelpKeyStyle.Render(fmt.Sprintf("%-12s", binding.Help().Key))
			descStr := styles.HelpDescStyle.Render(binding.Help().Desc)
			sb.WriteString(fmt.Sprintf("  %s %s\n", keyStr, descStr))
		}
	}

	sb.WriteString("\nPress ? to close help")
	return styles.HelpBarStyle.Render(sb.String())
}

// Run starts the TUI application
func Run(sources DataSources) error {
	app := NewApp(sources)
	p := tea.NewProgram(app, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
