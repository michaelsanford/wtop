package ui

import (
	"github.com/charmbracelet/lipgloss"
)

var (
	colorMuted  = lipgloss.Color("243")
	colorYellow = lipgloss.Color("11")
	colorWhite  = lipgloss.Color("15")
	colorAccent = lipgloss.Color("57")

	styleMuted = lipgloss.NewStyle().Foreground(colorMuted)

	styleStatusBar = lipgloss.NewStyle().
			Background(colorAccent).
			Foreground(colorWhite).
			Padding(0, 1)

	styleConfirmBox = lipgloss.NewStyle().
			BorderStyle(lipgloss.DoubleBorder()).
			BorderForeground(colorYellow).
			Padding(1, 3)

	styleConfirmTitle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorYellow)
)

