package panels

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/michaelsanford/wtop/internal/collector"
)

// Mem renders the memory panel.  RAM and Swap each occupy one line with their
// bar and values inline (no sub-lines).  Colours: green=used, blue=cached,
// yellow=buffers (Linux), remainder=free.
func Mem(snap collector.MemSnapshot, width, height int) string {
	innerW := width - 4
	if innerW < 10 {
		innerW = 10
	}

	// Fixed overhead per bar line: "RAM  [" (6) + " ] " (2) + "XX.XG/XX.XG" (11) + "  NNN%" (6) = 25
	// Swap line has same overhead ("Swap [" = 6 chars too).
	const lineOverhead = 25
	barW := innerW - lineOverhead
	if barW < 4 {
		barW = 4
	}

	ramBar := renderMemBar(snap.RAMUsedBytes, snap.RAMCachedBytes, snap.RAMBuffersBytes, snap.RAMTotalBytes, barW)
	swapBar := renderBar(snap.SwapPct, barW, lipgloss.Color("14"))

	legend := fmt.Sprintf("%s used  %s cached  %s buffers",
		lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render("█"),
		lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Render("█"),
		lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render("█"),
	)

	lines := []string{
		fmt.Sprintf("RAM  [%s]  %s  %3.0f%%", ramBar, fmtMemVal(snap.RAMUsedBytes, snap.RAMTotalBytes), snap.RAMPct),
		fmt.Sprintf("Swap [%s]  %s  %3.0f%%", swapBar, fmtMemVal(snap.SwapUsedBytes, snap.SwapTotalBytes), snap.SwapPct),
		legend,
	}

	content := strings.Join(lines, "\n")
	return lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(colorBorder).
		Padding(0, 1).
		Width(width - 2).
		Height(height).
		Render(content)
}

// fmtMemVal returns "X.XG/XX.XG" — used/total in GiB, always 11 chars wide.
func fmtMemVal(used, total uint64) string {
	u := float64(used) / (1024 * 1024 * 1024)
	t := float64(total) / (1024 * 1024 * 1024)
	return fmt.Sprintf("%5.1fG/%5.1fG", u, t)
}

// renderMemBar draws three coloured segments: used (green) | cached (blue) | buffers (yellow) | free (░).
func renderMemBar(used, cached, buffers, total uint64, width int) string {
	if total == 0 || width <= 0 {
		return strings.Repeat("░", width)
	}

	toChars := func(bytes uint64) int {
		n := int(float64(bytes) / float64(total) * float64(width))
		if n < 0 {
			return 0
		}
		return n
	}

	usedN := toChars(used)
	cachedN := toChars(cached)
	bufN := toChars(buffers)

	if usedN+cachedN+bufN > width {
		excess := usedN + cachedN + bufN - width
		if bufN >= excess {
			bufN -= excess
		} else {
			excess -= bufN
			bufN = 0
			if cachedN >= excess {
				cachedN -= excess
			} else {
				cachedN = 0
			}
		}
	}
	freeN := width - usedN - cachedN - bufN

	return lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render(strings.Repeat("█", usedN)) +
		lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Render(strings.Repeat("█", cachedN)) +
		lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render(strings.Repeat("█", bufN)) +
		strings.Repeat("░", freeN)
}
