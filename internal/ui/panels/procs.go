package panels

import (
	"fmt"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
	"github.com/michaelsanford/wtop/internal/collector"
)

// Sort column indices — must match the column order returned by BuildColumns.
const (
	ColPID    = 0
	ColName   = 1
	ColCPU    = 2
	ColMemPct = 3
	ColMemMB  = 4
)

// Fixed column widths. Name is the only variable column and fills remaining space.
const (
	pidW       = 6
	cpuW       = 7  // "100.0" = 5 + padding
	memPctW    = 7  // "100.0" = 5 + padding
	memMBW     = 10 // "99999.9" = 7 + padding; header "MEM MB" = 6
	fixedTotal = pidW + cpuW + memPctW + memMBW // 30
	numCols    = 5
	// bubbles/table Cell and Header styles both have Padding(0,1): each column
	// renders 2 chars wider than col.Width.  Subtract the overhead from the budget.
	cellPadOverhead = numCols * 2 // 10
)

// BuildColumns returns table columns.  The active sort column gets a plain-text
// arrow appended (no ANSI codes — those break table cell alignment).
func BuildColumns(termW, sortCol int, ascending bool) []table.Column {
	nameW := termW - fixedTotal - cellPadOverhead
	if nameW < 10 {
		nameW = 10
	}

	arrow := "↓"
	if ascending {
		arrow = "↑"
	}
	titles := [5]string{"PID", "Name", "CPU%", "MEM%", "MEM MB"}
	if sortCol >= 0 && sortCol < len(titles) {
		titles[sortCol] += " " + arrow // plain text — no lipgloss, avoids byte-vs-rune width mismatch
	}

	return []table.Column{
		{Title: titles[ColPID], Width: pidW},
		{Title: titles[ColName], Width: nameW},
		{Title: titles[ColCPU], Width: cpuW},
		{Title: titles[ColMemPct], Width: memPctW},
		{Title: titles[ColMemMB], Width: memMBW},
	}
}

// BuildRows converts process snapshots to table rows.
func BuildRows(procs []collector.ProcSnapshot) []table.Row {
	rows := make([]table.Row, 0, len(procs))
	for _, p := range procs {
		rows = append(rows, table.Row{
			fmt.Sprintf("%d", p.PID),
			p.Name,
			fmt.Sprintf("%.1f", p.CPUPct),
			fmt.Sprintf("%.1f", p.MemPct),
			fmt.Sprintf("%.1f", p.MemMB),
		})
	}
	return rows
}

// TableStyles returns table styles matching the wtop colour theme.
func TableStyles() table.Styles {
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(true).
		Foreground(lipgloss.Color("15"))
	s.Selected = lipgloss.NewStyle().
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)
	s.Cell = s.Cell.Foreground(lipgloss.Color("252"))
	return s
}

// SortColFor maps ui.SortField (0–3) to the column index in BuildColumns.
func SortColFor(sortBy int) int {
	switch sortBy {
	case 0:
		return ColCPU
	case 1:
		return ColMemMB
	case 2:
		return ColPID
	case 3:
		return ColName
	default:
		return ColCPU
	}
}
