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

	inUseN := toChars(inUse)
	modN := toChars(modified)
	stbyN := toChars(standby)

	if inUseN+modN+stbyN > width {
		excess := inUseN + modN + stbyN - width
		if stbyN >= excess {
			stbyN -= excess
		} else {
			excess -= stbyN
			stbyN = 0
			if modN >= excess {
				modN -= excess
			} else {
				modN = 0
			}
		}
	}
	freeN := width - inUseN - modN - stbyN

	return lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render(strings.Repeat("█", inUseN)) +
		lipgloss.NewStyle().Foreground(lipgloss.Color("208")).Render(strings.Repeat("█", modN)) +
		lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Render(strings.Repeat("█", stbyN)) +
		strings.Repeat("░", freeN)
}

// renderMemBar draws Used (green) | Cached (blue) | Buffers (yellow) | Free (░).
// Used on Linux/non-Windows where the composition data is not available.
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
