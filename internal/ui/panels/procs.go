package panels

import (
	"fmt"
	"sort"
	"strings"

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
	PidW       = 6
	cpuW       = 7                              // "100.0" = 5 + padding
	memPctW    = 7                              // "100.0" = 5 + padding
	memMBW     = 10                             // "99999.9" = 7 + padding; header "MEM MB" = 6
	fixedTotal = PidW + cpuW + memPctW + memMBW // 30
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
		{Title: titles[ColPID], Width: PidW},
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

// treeNode is an internal node used while building the process forest.
type treeNode struct {
	proc     collector.ProcSnapshot
	children []*treeNode
}

// BuildTreeRows builds a process forest from procs, renders parent→child
// relationships with htop-style box-drawing connectors, and returns table rows
// in depth-first order.  Siblings at every level are sorted by sortBy/ascending.
// Processes whose parent is absent in the snapshot (or whose PPID is 0) are
// treated as roots and rendered without a connector prefix.
func BuildTreeRows(procs []collector.ProcSnapshot, sortBy int, ascending bool) []table.Row {
	if len(procs) == 0 {
		return nil
	}

	// PID presence set — O(n) lookup when linking children to parents.
	pidSet := make(map[int32]struct{}, len(procs))
	for _, p := range procs {
		pidSet[p.PID] = struct{}{}
	}

	// Build one node per process.
	nodes := make(map[int32]*treeNode, len(procs))
	for i := range procs {
		nodes[procs[i].PID] = &treeNode{proc: procs[i]}
	}

	// Link children to parents; collect orphans/roots.
	roots := make([]*treeNode, 0, 8)
	for _, p := range procs {
		_, parentPresent := pidSet[p.PPID]
		if p.PPID == 0 || !parentPresent {
			roots = append(roots, nodes[p.PID])
		} else {
			parent := nodes[p.PPID]
			parent.children = append(parent.children, nodes[p.PID])
		}
	}

	sortTreeNodes(roots, sortBy, ascending)

	rows := make([]table.Row, 0, len(procs))
	for _, r := range roots {
		// Roots get no leading connector; pass empty connector and prefix.
		dfsTreeRows(r, "", "", sortBy, ascending, &rows)
	}
	return rows
}

// dfsTreeRows appends one row per node in depth-first order, prepending
// box-drawing connectors to the Name field.
//
// connector is the branch symbol for this node ("├─ ", "└─ ", or "" for roots).
// prefix is the accumulated indentation string inherited from ancestors.
func dfsTreeRows(n *treeNode, prefix, connector string, sortBy int, ascending bool, rows *[]table.Row) {
	p := n.proc
	*rows = append(*rows, table.Row{
		fmt.Sprintf("%d", p.PID),
		prefix + connector + p.Name,
		fmt.Sprintf("%.1f", p.CPUPct),
		fmt.Sprintf("%.1f", p.MemPct),
		fmt.Sprintf("%.1f", p.MemMB),
	})

	// Derive the prefix that children of this node will inherit.
	// A "└─ " connector means no more siblings follow, so children get spaces.
	// A "├─ " connector means siblings follow, so children get a continuation bar.
	// A "" connector means this is a root; children start fresh connectors.
	var childPrefix string
	switch connector {
	case "└─ ":
		childPrefix = prefix + "   "
	case "├─ ":
		childPrefix = prefix + "│  "
	default:
		childPrefix = prefix
	}

	sortTreeNodes(n.children, sortBy, ascending)
	for i, c := range n.children {
		var childConnector string
		if i == len(n.children)-1 {
			childConnector = "└─ "
		} else {
			childConnector = "├─ "
		}
		dfsTreeRows(c, childPrefix, childConnector, sortBy, ascending, rows)
	}
}

// sortTreeNodes sorts a slice of treeNodes in-place by sortBy/ascending.
// sortBy values mirror ui.SortField: 0=CPU, 1=MemMB, 2=PID, 3=Name.
func sortTreeNodes(nodes []*treeNode, sortBy int, ascending bool) {
	if len(nodes) < 2 {
		return
	}
	sort.Slice(nodes, func(i, j int) bool {
		a, b := nodes[i].proc, nodes[j].proc
		var less bool
		switch sortBy {
		case 1:
			less = a.MemMB < b.MemMB
		case 2:
			less = a.PID < b.PID
		case 3:
			less = strings.ToLower(a.Name) < strings.ToLower(b.Name)
		default:
			less = a.CPUPct < b.CPUPct
		}
		if ascending {
			return less
		}
		return !less
	})
}
