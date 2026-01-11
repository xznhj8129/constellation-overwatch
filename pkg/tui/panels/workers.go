package panels

import (
	"fmt"
	"strings"

	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/tui/datasource"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/tui/styles"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// WorkersUpdateMsg contains updated worker statuses
type WorkersUpdateMsg struct {
	Workers []datasource.WorkerStatus
}

// WorkersModel displays worker status
type WorkersModel struct {
	workers []datasource.WorkerStatus
	width   int
	height  int
	focused bool
}

// NewWorkersModel creates a new workers panel
func NewWorkersModel() WorkersModel {
	return WorkersModel{
		workers: make([]datasource.WorkerStatus, 0),
	}
}

func (m WorkersModel) Init() tea.Cmd {
	return nil
}

func (m WorkersModel) Update(msg tea.Msg) (WorkersModel, tea.Cmd) {
	switch msg := msg.(type) {
	case WorkersUpdateMsg:
		m.workers = msg.Workers
	}
	return m, nil
}

func (m WorkersModel) View() string {
	var content strings.Builder

	if len(m.workers) == 0 {
		content.WriteString(styles.StatusMutedStyle.Render("No workers"))
		return content.String()
	}

	// Calculate max name length for alignment
	maxNameLen := 0
	for _, w := range m.workers {
		if len(w.Name) > maxNameLen {
			maxNameLen = len(w.Name)
		}
	}

	// Render each worker
	for _, worker := range m.workers {
		name := worker.Name
		// Pad name for alignment
		name = fmt.Sprintf("%-*s", maxNameLen, name)

		status := styles.WorkerStatusBadge(worker.Healthy)

		content.WriteString(fmt.Sprintf("%s %s\n", name, status))
	}

	return strings.TrimSuffix(content.String(), "\n")
}

// SetSize sets the panel dimensions
func (m *WorkersModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// SetFocused sets whether this panel is focused
func (m *WorkersModel) SetFocused(focused bool) {
	m.focused = focused
}

// Focused returns whether this panel is focused
func (m WorkersModel) Focused() bool {
	return m.focused
}

// Title returns the panel title
func (m WorkersModel) Title() string {
	return "WORKERS"
}

// RenderWithBorder renders the panel with its border
func (m WorkersModel) RenderWithBorder() string {
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
