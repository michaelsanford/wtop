package ui

import "github.com/charmbracelet/bubbles/key"

// KeyMap defines all keybindings for the application.
type KeyMap struct {
	Up         key.Binding
	Down       key.Binding
	Sort       key.Binding
	SortInvert key.Binding
	CycleGPU   key.Binding
	Tree       key.Binding
	Kill       key.Binding
	Confirm    key.Binding
	Cancel     key.Binding
	Quit       key.Binding
}

var defaultKeyMap = KeyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "down"),
	),
	Sort: key.NewBinding(
		key.WithKeys("s"),
		key.WithHelp("s", "cycle sort"),
	),
	SortInvert: key.NewBinding(
		key.WithKeys("d"),
		key.WithHelp("d", "invert sort"),
	),
	CycleGPU: key.NewBinding(
		key.WithKeys("g"),
		key.WithHelp("g", "cycle GPU"),
	),
	Tree: key.NewBinding(
		key.WithKeys("t"),
		key.WithHelp("t", "tree view"),
	),
	Kill: key.NewBinding(
		key.WithKeys("x"),
		key.WithHelp("x", "kill process"),
	),
	Confirm: key.NewBinding(
		key.WithKeys("y"),
		key.WithHelp("y", "confirm"),
	),
	Cancel: key.NewBinding(
		key.WithKeys("n", "esc"),
		key.WithHelp("n/esc", "cancel"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
		key.WithHelp("q", "quit"),
	),
}
