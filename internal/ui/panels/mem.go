package panels

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/michaelsanford/wtop/internal/collector"
)

// Mem renders the memory panel.
//
// On Windows (CommittedBytes > 0) it shows three bar lines:
//   - RAM   — In Use (green) | Modified (orange) | Standby (blue) | Free (░)
//   - Cmt   — Committed / commit limit (magenta)
//   - Swap  — swap utilisation (cyan)
//
// On Linux/other it falls back to the htop-style Used | Cached | Buffers layout.
func Mem(snap collector.MemSnapshot, width, height int) string {
	innerW := width - 4
	if innerW < 10 {
		innerW = 10
	}

	// Fixed overhead per bar line: "RAM  [" (6) + " NNN%]" (6) + " " (1) + "%5.1fG/%5.1fG" (13) = 26
	// Swap and Cmt labels are the same width ("Swap [" = "Cmt  [" = 6 chars).
	const lineOverhead = 26
	barW := innerW - lineOverhead
	if barW < 4 {
		barW = 4
	}

	var lines []string
	var legend string

	if snap.CommittedBytes > 0 {
		// ── Windows composition view ─────────────────────────────────────────
		other := snap.ModifiedBytes + snap.StandbyBytes + snap.FreeBytes
		var inUse uint64
		if snap.RAMTotalBytes > other {
			inUse = snap.RAMTotalBytes - other
		}

		commitPct := 0.0
		if snap.CommitLimitBytes > 0 {
			commitPct = float64(snap.CommittedBytes) / float64(snap.CommitLimitBytes) * 100
		}

		ramBar := renderMemCompositionBar(inUse, snap.ModifiedBytes, snap.StandbyBytes, snap.RAMTotalBytes, barW)
		cmtBar := renderBar(commitPct, barW, "13")
		swapBar := renderBar(snap.SwapPct, barW, "14")

		legend = fmt.Sprintf("%s in use  %s modified  %s standby",
			lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render("█"),
			lipgloss.NewStyle().Foreground(lipgloss.Color("208")).Render("█"),
			lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Render("█"),
		)

		lines = []string{
			fmt.Sprintf("RAM  [%s %3.0f%%] %s", ramBar, snap.RAMPct, fmtMemVal(snap.RAMUsedBytes, snap.RAMTotalBytes)),
			fmt.Sprintf("Cmt  [%s %3.0f%%] %s", cmtBar, commitPct, fmtMemVal(snap.CommittedBytes, snap.CommitLimitBytes)),
			fmt.Sprintf("Swap [%s %3.0f%%] %s", swapBar, snap.SwapPct, fmtMemVal(snap.SwapUsedBytes, snap.SwapTotalBytes)),
			legend,
		}
	} else {
		// ── Linux / other: used | cached | buffers ───────────────────────────
		ramBar := renderMemBar(snap.RAMUsedBytes, snap.RAMCachedBytes, snap.RAMBuffersBytes, snap.RAMTotalBytes, barW)
		swapBar := renderBar(snap.SwapPct, barW, "14")

		legend = fmt.Sprintf("%s used  %s cached  %s buffers",
			lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render("█"),
			lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Render("█"),
			lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render("█"),
		)

		lines = []string{
			fmt.Sprintf("RAM  [%s %3.0f%%] %s", ramBar, snap.RAMPct, fmtMemVal(snap.RAMUsedBytes, snap.RAMTotalBytes)),
			fmt.Sprintf("Swap [%s %3.0f%%] %s", swapBar, snap.SwapPct, fmtMemVal(snap.SwapUsedBytes, snap.SwapTotalBytes)),
			legend,
		}
	}

	return RenderPanel(lines, width, height)
}

// fmtMemVal returns "XX.XG/XX.XG" — used/total in GiB, always 13 chars wide.
func fmtMemVal(used, total uint64) string {
	u := float64(used) / (1024 * 1024 * 1024)
	t := float64(total) / (1024 * 1024 * 1024)
	return fmt.Sprintf("%5.1fG/%5.1fG", u, t)
}

// renderMemCompositionBar draws In Use (green) | Modified (orange) | Standby (blue) | Free (░).
func renderMemCompositionBar(inUse, modified, standby, total uint64, width int) string {
	inUseN, modN, stbyN, freeN := scaleComposition(inUse, modified, standby, total, width)
	return lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render(strings.Repeat("█", inUseN)) +
		lipgloss.NewStyle().Foreground(lipgloss.Color("208")).Render(strings.Repeat("█", modN)) +
		lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Render(strings.Repeat("█", stbyN)) +
		strings.Repeat("░", freeN)
}

// renderMemBar draws Used (green) | Cached (blue) | Buffers (yellow) | Free (░).
// Used on Linux/non-Windows where the composition data is not available.
func renderMemBar(used, cached, buffers, total uint64, width int) string {
	usedN, cachedN, bufN, freeN := scaleComposition(used, cached, buffers, total, width)
	return lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render(strings.Repeat("█", usedN)) +
		lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Render(strings.Repeat("█", cachedN)) +
		lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render(strings.Repeat("█", bufN)) +
		strings.Repeat("░", freeN)
}

// scaleComposition splits width into three counts representing val1, val2, and val3.
// It ensures the total sum does not exceed width, scaling down from right-to-left if it does.
func scaleComposition(val1, val2, val3, total uint64, width int) (int, int, int, int) {
	if total == 0 || width <= 0 {
		return 0, 0, 0, width
	}
	toChars := func(bytes uint64) int {
		n := int(float64(bytes) / float64(total) * float64(width))
		if n < 0 {
			return 0
		}
		return n
	}

	n1 := toChars(val1)
	n2 := toChars(val2)
	n3 := toChars(val3)

	if n1+n2+n3 > width {
		excess := n1 + n2 + n3 - width
		if n3 >= excess {
			n3 -= excess
		} else {
			excess -= n3
			n3 = 0
			if n2 >= excess {
				n2 -= excess
			} else {
				n2 = 0
			}
		}
	}
	freeN := width - n1 - n2 - n3
	return n1, n2, n3, freeN
}
