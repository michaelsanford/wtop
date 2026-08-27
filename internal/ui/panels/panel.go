package panels

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// panelChromeW is the horizontal overhead RenderPanel adds around its content:
// one border cell and one padding cell on each side.
const panelChromeW = 4

// RenderPanel draws a panel box around lines of content.
//
// Content is clipped to the box on both axes.  Clipping the width is not
// cosmetic: lipgloss Width() word-wraps and Height() is only a minimum, so a
// single over-long line silently renders the panel taller than the caller
// reserved and pushes everything below it off the bottom of the terminal.
// Doing it here rather than in each panel makes that failure structurally
// impossible to reintroduce.
func RenderPanel(lines []string, width, height int) string {
	if height > 0 && len(lines) > height {
		lines = lines[:height]
	}
	lines = fit(lines, width-panelChromeW)

	content := strings.Join(lines, "\n")
	style := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(colorBorder).
		Padding(0, 1).
		Width(width - 2)
	if height > 0 {
		style = style.Height(height)
	}
	return style.Render(content)
}

// fit truncates each line to inner visible cells, measuring display width
// rather than bytes so ANSI escapes and wide runes are accounted for.  The
// input slice is returned untouched when nothing overflows, which is the common
// case and keeps the render path allocation-free.
func fit(lines []string, inner int) []string {
	if inner < 1 {
		inner = 1
	}
	over := -1
	for i, l := range lines {
		if ansi.StringWidth(l) > inner {
			over = i
			break
		}
	}
	if over < 0 {
		return lines
	}
	out := make([]string, len(lines))
	copy(out, lines[:over])
	for i := over; i < len(lines); i++ {
		out[i] = truncate(lines[i], inner)
	}
	return out
}
