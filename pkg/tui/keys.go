package tui

import (
	"github.com/charmbracelet/bubbles/key"
)

// KeyMap defines all keybindings for the TUI
type KeyMap struct {
	// Navigation
	Up       key.Binding
	Down     key.Binding
	Left     key.Binding
	Right    key.Binding
	NextTab  key.Binding
	PrevTab  key.Binding
	PageUp   key.Binding
	PageDown key.Binding
	Home     key.Binding
	End      key.Binding

	// Actions
	Toggle  key.Binding
	Refresh key.Binding
	Help    key.Binding
	Quit    key.Binding
}

// DefaultKeyMap returns the default keybindings
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("k/up", "scroll up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("j/down", "scroll down"),
		),
		Left: key.NewBinding(
			key.WithKeys("left", "h"),
			key.WithHelp("h/left", "scroll left"),
		),
		Right: key.NewBinding(
			key.WithKeys("right", "l"),
			key.WithHelp("l/right", "scroll right"),
		),
		NextTab: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "next panel"),
		),
		PrevTab: key.NewBinding(
			key.WithKeys("shift+tab"),
			key.WithHelp("shift+tab", "prev panel"),
		),
		PageUp: key.NewBinding(
			key.WithKeys("pgup", "ctrl+u"),
			key.WithHelp("pgup", "page up"),
		),
		PageDown: key.NewBinding(
			key.WithKeys("pgdown", "ctrl+d"),
			key.WithHelp("pgdn", "page down"),
		),
		Home: key.NewBinding(
			key.WithKeys("home", "g"),
			key.WithHelp("g/home", "go to top"),
		),
		End: key.NewBinding(
			key.WithKeys("end", "G"),
			key.WithHelp("G/end", "go to bottom"),
		),
		Toggle: key.NewBinding(
			key.WithKeys("v"),
			key.WithHelp("v", "toggle view"),
		),
		Refresh: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "refresh"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
	}
}

// ShortHelp returns a short help string for the help bar
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.NextTab, k.Toggle, k.Refresh, k.Help, k.Quit}
}

// FullHelp returns the full help for the help overlay
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.PageUp, k.PageDown},
		{k.NextTab, k.PrevTab, k.Toggle},
		{k.Refresh, k.Help, k.Quit},
	}
}
