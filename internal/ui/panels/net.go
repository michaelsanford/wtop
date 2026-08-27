package panels

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/michaelsanford/wtop/internal/collector"
)

// Net renders the network panel showing per-interface I/O rates.
func Net(snaps []collector.NetSnapshot, width, height int) string {
	cyanColor := lipgloss.Color("14")
	greenColor := lipgloss.Color("10")

	upArrow := lipgloss.NewStyle().Foreground(cyanColor).Bold(true).Render("↑")
	downArrow := lipgloss.NewStyle().Foreground(greenColor).Bold(true).Render("↓")

	var lines []string
	lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(colorWhite).Render("Network"))

	if len(snaps) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(colorBorder).Render("no interfaces"))
	}

	for _, s := range snaps {
		name := fmt.Sprintf("%-12s", truncate(s.Name, 12))
		sent := fmtBytesPerSec(s.BytesSentPerSec)
		recv := fmtBytesPerSec(s.BytesRecvPerSec)
		lines = append(lines, fmt.Sprintf("%s  %s %-12s  %s %s", name, upArrow, sent, downArrow, recv))
	}

	return RenderPanel(lines, width, height)
}

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

// truncate clips s to max visible cells.  It measures display width rather than
// bytes so multi-byte runes are never split and ANSI escapes are not counted
// against the budget.
func truncate(s string, max int) string {
	if max < 1 {
		return ""
	}
	if ansi.StringWidth(s) <= max {
		return s
	}
	return ansi.Truncate(s, max, "…")
}
