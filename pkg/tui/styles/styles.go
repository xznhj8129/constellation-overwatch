package styles

import (
	"github.com/charmbracelet/lipgloss"
)

// Color theme - Military/C4ISR inspired
var (
	PrimaryColor   = lipgloss.Color("#00d4ff") // Cyan
	SecondaryColor = lipgloss.Color("#0fbfbf") // Teal
	AccentColor    = lipgloss.Color("#ff6b6b") // Red accent
	SuccessColor   = lipgloss.Color("#00ff00") // Green (OK status)
	WarningColor   = lipgloss.Color("#ffff00") // Yellow
	ErrorColor     = lipgloss.Color("#ff0000") // Red
	MutedColor     = lipgloss.Color("#888888") // Gray
	DimColor       = lipgloss.Color("#444444") // Dark gray
	BGColor        = lipgloss.Color("#1a1a2e") // Dark blue-black background
)

// Panel styles
var (
	// Base panel style with border
	PanelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(PrimaryColor).
			Padding(0, 1)

	// Focused panel has brighter border
	FocusedPanelStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(AccentColor).
				Padding(0, 1)

	// Panel title style
	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(PrimaryColor).
			Padding(0, 1)

	// Focused panel title
	FocusedTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(AccentColor).
				Padding(0, 1)
)

// Status indicator styles
var (
	StatusOKStyle = lipgloss.NewStyle().
			Foreground(SuccessColor).
			Bold(true)

	StatusWarnStyle = lipgloss.NewStyle().
			Foreground(WarningColor).
			Bold(true)

	StatusErrorStyle = lipgloss.NewStyle().
				Foreground(ErrorColor).
				Bold(true)

	StatusMutedStyle = lipgloss.NewStyle().
				Foreground(MutedColor)
)

// Log level styles
var (
	LogDebugStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00d4ff"))

	LogInfoStyle = lipgloss.NewStyle().
			Foreground(SuccessColor)

	LogWarnStyle = lipgloss.NewStyle().
			Foreground(WarningColor)

	LogErrorStyle = lipgloss.NewStyle().
			Foreground(ErrorColor)

	LogFatalStyle = lipgloss.NewStyle().
			Foreground(ErrorColor).
			Bold(true)
)

// Gauge styles
var (
	GaugeEmptyStyle = lipgloss.NewStyle().
			Foreground(DimColor)

	GaugeFilledStyle = lipgloss.NewStyle().
				Foreground(SecondaryColor)

	GaugeLabelStyle = lipgloss.NewStyle().
			Foreground(MutedColor).
			Width(6)

	GaugeValueStyle = lipgloss.NewStyle().
			Foreground(PrimaryColor)
)

// Table styles
var (
	TableHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(PrimaryColor).
				BorderBottom(true).
				BorderStyle(lipgloss.NormalBorder()).
				BorderForeground(DimColor)

	TableRowStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ffffff"))

	TableSelectedStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#2a2a4e")).
				Foreground(PrimaryColor)
)

// Help bar style
var (
	HelpBarStyle = lipgloss.NewStyle().
			Foreground(MutedColor).
			Padding(0, 1)

	HelpKeyStyle = lipgloss.NewStyle().
			Foreground(PrimaryColor).
			Bold(true)

	HelpDescStyle = lipgloss.NewStyle().
			Foreground(MutedColor)
)

// EntityStatusStyle returns the appropriate style for an entity status
func EntityStatusStyle(status string, isLive bool) lipgloss.Style {
	if !isLive {
		return StatusMutedStyle
	}

	switch status {
	case "active", "online":
		return StatusOKStyle
	case "warning":
		return StatusWarnStyle
	case "error", "offline":
		return StatusErrorStyle
	default:
		return StatusMutedStyle
	}
}

// WorkerStatusBadge returns a styled status badge
func WorkerStatusBadge(healthy bool) string {
	if healthy {
		return StatusOKStyle.Render("[OK]")
	}
	return StatusErrorStyle.Render("[ERR]")
}

// LiveIndicator returns a styled live indicator
func LiveIndicator(isLive bool) string {
	if isLive {
		return StatusOKStyle.Render("LIVE")
	}
	return StatusMutedStyle.Render("----")
}
