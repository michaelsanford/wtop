package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	colorBorder = lipgloss.Color("240")
	colorMuted  = lipgloss.Color("243")
	colorGreen  = lipgloss.Color("10")
	colorYellow = lipgloss.Color("11")
	colorRed    = lipgloss.Color("9")
	colorBlue   = lipgloss.Color("12")
	colorPurple = lipgloss.Color("13")
	colorCyan   = lipgloss.Color("14")
	colorWhite  = lipgloss.Color("15")
	colorAccent = lipgloss.Color("57")

	stylePanelBorder = lipgloss.NewStyle().
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(colorBorder).
				Padding(0, 1)

	styleHeader = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorWhite)

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

	styleSentArrow = lipgloss.NewStyle().Foreground(colorCyan).Bold(true)
	styleRecvArrow = lipgloss.NewStyle().Foreground(colorGreen).Bold(true)
)

// renderBar renders a Unicode block-character progress bar.
// pct is 0–100; barColor is the fill colour.
func renderBar(pct float64, width int, barColor lipgloss.Color) string {
	if width <= 0 {
		return ""
	}
	filled := int(pct / 100.0 * float64(width))
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	return lipgloss.NewStyle().Foreground(barColor).Render(bar)
}

// cpuBarColor returns the colour for a given CPU utilisation percentage.
func cpuBarColor(pct float64) lipgloss.Color {
	switch {
	case pct > 85:
		return colorRed
	case pct > 60:
		return colorYellow
	default:
		return colorGreen
	}
}

// fmtBytesPerSec converts bytes/s to a human-readable rate string.
func fmtBytesPerSec(bps float64) string {
	switch {
	case bps >= 1024*1024:
		return fmt.Sprintf("%.1f MB/s", bps/(1024*1024))
	case bps >= 1024:
		return fmt.Sprintf("%.1f KB/s", bps/1024)
	default:
		return fmt.Sprintf("%.0f B/s", bps)
	}
}

// fmtGiB formats bytes as GiB with one decimal.
func fmtGiB(b uint64) string {
	return fmt.Sprintf("%.1f GiB", float64(b)/(1024*1024*1024))
}
