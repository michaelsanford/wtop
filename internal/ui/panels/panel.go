package panels

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// RenderPanel draws a panel box around lines of content.
func RenderPanel(lines []string, width, height int) string {
	if height > 0 && len(lines) > height {
		lines = lines[:height]
	}
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
