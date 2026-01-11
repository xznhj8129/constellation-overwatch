package panels

import (
	"fmt"
	"strings"
	"time"

	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/tui/styles"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const maxLogEntries = 500

// LogEntry represents a single log entry
type LogEntry struct {
	Time    time.Time
	Level   string
	Message string
	Fields  map[string]interface{}
}

// LogEntryMsg is a message containing a new log entry
type LogEntryMsg LogEntry

// LogsModel displays scrollable log output
type LogsModel struct {
	viewport viewport.Model
	entries  []LogEntry
	width    int
	height   int
	focused  bool
	ready    bool
}

// NewLogsModel creates a new logs panel
func NewLogsModel() LogsModel {
	return LogsModel{
		entries: make([]LogEntry, 0, maxLogEntries),
	}
}

func (m LogsModel) Init() tea.Cmd {
	return nil
}

func (m LogsModel) Update(msg tea.Msg) (LogsModel, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case LogEntryMsg:
		// Add new entry
		entry := LogEntry(msg)
		m.entries = append(m.entries, entry)

		// Trim if over max
		if len(m.entries) > maxLogEntries {
			m.entries = m.entries[len(m.entries)-maxLogEntries:]
		}

		// Update viewport content
		m.viewport.SetContent(m.renderEntries())

		// Auto-scroll to bottom if we were at the bottom
		if m.viewport.AtBottom() || m.viewport.YOffset == 0 {
			m.viewport.GotoBottom()
		}

	case tea.KeyMsg:
		if m.focused {
			m.viewport, cmd = m.viewport.Update(msg)
		}
	}

	return m, cmd
}

func (m LogsModel) View() string {
	if !m.ready {
		return "Loading logs..."
	}
	return m.viewport.View()
}

func (m LogsModel) renderEntries() string {
	var sb strings.Builder

	for _, entry := range m.entries {
		line := m.formatEntry(entry)
		sb.WriteString(line)
		sb.WriteString("\n")
	}

	return sb.String()
}

func (m LogsModel) formatEntry(entry LogEntry) string {
	// Format timestamp (short form)
	timeStr := entry.Time.Format("15:04:05")

	// Format level with color
	var levelStr string
	switch strings.ToUpper(entry.Level) {
	case "DEBUG", "DBG":
		levelStr = styles.LogDebugStyle.Render("[DBG]")
	case "INFO", "INF":
		levelStr = styles.LogInfoStyle.Render("[INF]")
	case "WARN", "WRN", "WARNING":
		levelStr = styles.LogWarnStyle.Render("[WRN]")
	case "ERROR", "ERR":
		levelStr = styles.LogErrorStyle.Render("[ERR]")
	case "FATAL", "FTL":
		levelStr = styles.LogFatalStyle.Render("[FTL]")
	default:
		levelStr = styles.StatusMutedStyle.Render(fmt.Sprintf("[%s]", entry.Level[:3]))
	}

	// Truncate message if too long
	msg := entry.Message
	maxMsgLen := m.width - 20 // Account for timestamp and level
	if maxMsgLen > 0 && len(msg) > maxMsgLen {
		msg = msg[:maxMsgLen-3] + "..."
	}

	return fmt.Sprintf("%s %s %s", styles.StatusMutedStyle.Render(timeStr), levelStr, msg)
}

// SetSize sets the panel dimensions
func (m *LogsModel) SetSize(width, height int) {
	m.width = width
	m.height = height

	// Initialize or update viewport
	if !m.ready {
		m.viewport = viewport.New(width, height-2)
		m.viewport.SetContent(m.renderEntries())
		m.ready = true
	} else {
		m.viewport.Width = width
		m.viewport.Height = height - 2
		m.viewport.SetContent(m.renderEntries())
	}
}

// SetFocused sets whether this panel is focused
func (m *LogsModel) SetFocused(focused bool) {
	m.focused = focused
}

// Focused returns whether this panel is focused
func (m LogsModel) Focused() bool {
	return m.focused
}

// Title returns the panel title
func (m LogsModel) Title() string {
	return "LOGS"
}

// AddEntry adds a log entry directly (used by log hook)
func (m *LogsModel) AddEntry(entry LogEntry) {
	m.entries = append(m.entries, entry)
	if len(m.entries) > maxLogEntries {
		m.entries = m.entries[len(m.entries)-maxLogEntries:]
	}
	if m.ready {
		m.viewport.SetContent(m.renderEntries())
		m.viewport.GotoBottom()
	}
}

// RenderWithBorder renders the panel with its border
func (m LogsModel) RenderWithBorder() string {
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
