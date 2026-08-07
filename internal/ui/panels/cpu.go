package panels

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/michaelsanford/wtop/internal/collector"
)

var (
	colorBorder = lipgloss.Color("240")
	colorGreen  = lipgloss.Color("10")
	colorYellow = lipgloss.Color("11")
	colorRed    = lipgloss.Color("9")
	colorWhite  = lipgloss.Color("15")
)

// cellSep is the visual gap printed between cells in a row.
const cellSep = "   " // 3 spaces keeps 2-digit IDs readable

// minCellW is the narrowest a core cell can be before we stop adding columns.
// Anatomy: "NN" + "[" + bar(≥1) + " " + "NNN%" + "]" = 9 + bar ≥ 10.
const minCellW = 14

// CPUHeight returns the outer height (including border) the CPU panel needs at
// the given outer terminal width.  Call this to reserve space in layout maths.
func CPUHeight(numCores, outerW int) int {
	innerW := outerW - 4
	if innerW < minCellW {
		innerW = minCellW
	}
	if numCores == 0 {
		return 3 // border(2) + avg(1)
	}
	sepW := len(cellSep)
	numCols := (numCores + 3) / 4
	for numCols > 1 {
		cellW := (innerW - (numCols-1)*sepW) / numCols
		if cellW >= minCellW {
			break
		}
		numCols--
	}
	numRows := (numCores + numCols - 1) / numCols
	return numRows + 3 // border(2) + avg(1) + grid rows
}

// CPU renders the CPU panel as a compact grid — at most 4 rows — with a
// full-width Avg bar underneath.  Height adapts to content automatically.
func CPU(snap collector.CPUSnapshot, width int) string {
	innerW := width - 4 // rounded border (2) + padding each side (1+1)
	if innerW < minCellW {
		innerW = minCellW
	}

	numCores := len(snap.CorePcts)
	var lines []string

	if numCores > 0 {
		// Determine number of columns so we get ≤ 4 rows.
		sepW := len(cellSep)
		numCols := (numCores + 3) / 4

		// Shrink columns until each cell is wide enough to be readable.
		for numCols > 1 {
			cellW := (innerW - (numCols-1)*sepW) / numCols
			if cellW >= minCellW {
				break
			}
			numCols--
		}

		numRows := (numCores + numCols - 1) / numCols
		cellW := (innerW - (numCols-1)*sepW) / numCols

		// Core labels are 1-based, so the widest is len("128") on a 128-core box.
		// Sizing every cell to the same label width keeps rows exactly cellW wide;
		// a fixed %2d would overflow the panel and wrap once cores reach 100.
		labelW := len(strconv.Itoa(numCores))
		if labelW < 2 {
			labelW = 2
		}

		for row := 0; row < numRows; row++ {
			parts := make([]string, numCols)
			for col := 0; col < numCols; col++ {
				idx := col*numRows + row // column-major: cores run top→bottom per column
				if idx >= numCores {
					parts[col] = strings.Repeat(" ", cellW)
				} else {
					parts[col] = renderCoreCell(idx, snap.CorePcts[idx], cellW, labelW)
				}
			}
			lines = append(lines, strings.Join(parts, cellSep))
		}
	}

	// Full-width Avg bar below the grid.
	// Anatomy: "Avg " (4) + "[" (1) + bar + " NNN%]" (6) = overhead 11
	const aggrOverhead = 11
	aggrBarW := innerW - aggrOverhead
	if aggrBarW < 1 {
		aggrBarW = 1
	}
	aggrColor := cpuBarColor(snap.AggrPct)
	aggrBar := renderBar(snap.AggrPct, aggrBarW, aggrColor)
	lines = append(lines,
		fmt.Sprintf("%s[%s %3.0f%%]",
			lipgloss.NewStyle().Bold(true).Foreground(colorWhite).Render("Avg "),
			aggrBar, snap.AggrPct))

	return RenderPanel(lines, width, 0)
}

// renderCoreCell formats one core as "NN[████░ NNN%]" in exactly cellW visible chars.
// Overhead beyond the label: "["(1) + " "(1) + "NNN%"(4) + "]"(1) = 7 chars.
func renderCoreCell(idx int, pct float64, cellW, labelW int) string {
	const fixedOverhead = 7
	barW := cellW - labelW - fixedOverhead
	if barW < 1 {
		barW = 1
	}
	color := cpuBarColor(pct)
	bar := renderBar(pct, barW, color)
	return fmt.Sprintf("%*d[%s %3.0f%%]", labelW, idx+1, bar, pct)
}

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

func renderBar(pct float64, width int, color lipgloss.Color) string {
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
	return lipgloss.NewStyle().Foreground(color).Render(bar)
}
