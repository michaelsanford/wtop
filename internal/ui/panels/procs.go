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
	ColDiskR  = 5
	ColDiskW  = 6
)

// Fixed column widths. Name is the only variable column and fills remaining space.
const (
	PidW       = 6
	cpuW       = 7                              // "100.0" = 5 + padding
	memPctW    = 7                              // "100.0" = 5 + padding
	memMBW     = 10                             // "99999.9" = 7 + padding; header "MEM MB" = 6
	diskW      = 9                              // "1234.5M" = 7; header "DISK R" + arrow = 8
	fixedTotal = PidW + cpuW + memPctW + memMBW // 30
	// fixedTotalIO is fixedTotal plus the two disk columns.
	fixedTotalIO = fixedTotal + 2*diskW // 48
	numCols      = 5
	// MaxCols is the widest the table ever gets: the five base columns plus the
	// two disk columns.  Rows always carry this many cells — see BuildRows.
	MaxCols = 7
	// bubbles/table Cell and Header styles both have Padding(0,1): each column
	// renders 2 chars wider than col.Width, so the budget shrinks by 2 per column
	// actually shown.  This is per-column, not a constant, because the column
	// count now varies with terminal width.
	cellPad = 2
	// DiskColsMinWidth is the terminal width at which the two disk columns appear.
	// They appear and disappear together: showing reads without writes would be
	// asymmetric and would strand a sort field on a column that is not rendered.
	DiskColsMinWidth = 120
)

// selfMarker prefixes the Name cell of wtop and the processes hosting it.  Plain
// text only — ANSI inside a cell breaks bubbles/table's byte-vs-rune width
// accounting, the same reason the sort arrow is a bare rune.
const selfMarker = "◆ "

// markSelf prefixes name with selfMarker when the process is part of wtop's own
// ancestry.  Unmarked rows get no compensating pad; names stay flush left.
func markSelf(name string, self bool) string {
	if self {
		return selfMarker + name
	}
	return name
}

// BuildColumns returns table columns.  The active sort column gets a plain-text
// arrow appended (no ANSI codes — those break table cell alignment).
func BuildColumns(termW, sortCol int, ascending bool) []table.Column {
	// The disk columns are always present but collapse to zero width when they do
	// not fit.  bubbles/table skips zero-width columns in both headersView and
	// renderRow, so this hides them without changing the column count — which
	// matters because renderRow walks the row's cells and indexes m.cols by the
	// same offset.  A row with more cells than there are columns panics, so the
	// counts must never diverge.
	showDisk := termW >= DiskColsMinWidth
	visibleCols, fixed := numCols, fixedTotal
	if showDisk {
		visibleCols, fixed = MaxCols, fixedTotalIO
	}

	nameW := termW - fixed - visibleCols*cellPad
	if nameW < 10 {
		nameW = 10
	}

	dW := 0
	if showDisk {
		dW = diskW
	}

	arrow := "↓"
	if ascending {
		arrow = "↑"
	}
	titles := [MaxCols]string{"PID", "Name", "CPU%", "MEM%", "MEM MB", "DISK R", "DISK W"}
	if sortCol >= 0 && sortCol < len(titles) {
		titles[sortCol] += " " + arrow // plain text — no lipgloss, avoids byte-vs-rune width mismatch
	}

	return []table.Column{
		{Title: titles[ColPID], Width: PidW},
		{Title: titles[ColName], Width: nameW},
		{Title: titles[ColCPU], Width: cpuW},
		{Title: titles[ColMemPct], Width: memPctW},
		{Title: titles[ColMemMB], Width: memMBW},
		{Title: titles[ColDiskR], Width: dW},
		{Title: titles[ColDiskW], Width: dW},
	}
}

// fmtRateCell renders a bytes-per-second rate compactly enough for a 9-cell
// column.  Idle processes are the overwhelming majority, so they collapse to a
// bare "0" rather than "0.0 B/s" — a column of zeroes should recede, not shout.
func fmtRateCell(bps float64) string {
	switch {
	case bps <= 0:
		return "0"
	case bps >= 1<<30:
		return fmt.Sprintf("%.1fG", bps/(1<<30))
	case bps >= 1<<20:
		return fmt.Sprintf("%.1fM", bps/(1<<20))
	case bps >= 1<<10:
		return fmt.Sprintf("%.1fK", bps/(1<<10))
	default:
		return fmt.Sprintf("%.0fB", bps)
	}
}

// procCells renders one process as a row.  Every row carries MaxCols cells,
// matching the fixed MaxCols columns BuildColumns always returns; the columns
// that do not fit are zero-width rather than absent.  Rows are therefore
// width-independent: a resize changes column widths only, never the cell count,
// so rows never need rebuilding and can never disagree with the column set.
func procCells(p collector.ProcSnapshot, name string) table.Row {
	return table.Row{
		fmt.Sprintf("%d", p.PID),
		name,
		fmt.Sprintf("%.1f", p.CPUPct),
		fmt.Sprintf("%.1f", p.MemPct),
		fmt.Sprintf("%.1f", p.MemMB),
		fmtRateCell(p.ReadBps),
		fmtRateCell(p.WriteBps),
	}
}

// BuildRows converts process snapshots to table rows.
func BuildRows(procs []collector.ProcSnapshot) []table.Row {
	rows := make([]table.Row, 0, len(procs))
	for _, p := range procs {
		rows = append(rows, procCells(p, markSelf(p.Name, p.Self)))
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

// SortColFor maps ui.SortField (0–5) to the column index in BuildColumns.
//
// Sorting by a column that the current width does not render is deliberately
// allowed: the rows stay correctly ordered, BuildColumns simply draws no arrow,
// and the status bar remains the authoritative sort indicator.  Silently
// reverting the sort on resize would be an invisible state change.
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
	case 4:
		return ColDiskR
	case 5:
		return ColDiskW
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
	visited := make(map[int32]struct{}, len(procs))
	for _, r := range roots {
		// Roots get no leading connector; pass empty connector and prefix.
		dfsTreeRows(r, "", "", sortBy, ascending, visited, &rows)
	}

	// PID reuse can leave a process pointing at a recycled PPID, forming a parent
	// cycle (or a self-parent).  No member of such a cycle qualifies as a root, so
	// none is reachable from the walk above.  Promote whatever is left to a root
	// so that no process silently disappears from the tree view.
	if len(visited) < len(nodes) {
		stranded := make([]*treeNode, 0, len(nodes)-len(visited))
		for _, p := range procs {
			if _, seen := visited[p.PID]; !seen {
				stranded = append(stranded, nodes[p.PID])
			}
		}
		sortTreeNodes(stranded, sortBy, ascending)
		for _, s := range stranded {
			dfsTreeRows(s, "", "", sortBy, ascending, visited, &rows)
		}
	}
	return rows
}

// dfsTreeRows appends one row per node in depth-first order, prepending
// box-drawing connectors to the Name field.
//
// connector is the branch symbol for this node ("├─ ", "└─ ", or "" for roots).
// prefix is the accumulated indentation string inherited from ancestors.
//
// visited guards against parent cycles introduced by PID reuse; without it a
// self-parent or A→B→A loop would recurse until the stack blew.
func dfsTreeRows(n *treeNode, prefix, connector string, sortBy int, ascending bool, visited map[int32]struct{}, rows *[]table.Row) {
	p := n.proc
	if _, seen := visited[p.PID]; seen {
		return
	}
	visited[p.PID] = struct{}{}

	*rows = append(*rows, procCells(p, prefix+connector+markSelf(p.Name, p.Self)))

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
		dfsTreeRows(c, childPrefix, childConnector, sortBy, ascending, visited, rows)
	}
}

// sortTreeNodes sorts a slice of treeNodes in-place by sortBy/ascending.
// sortBy values mirror ui.SortField: 0=CPU, 1=MemMB, 2=PID, 3=Name, 4=DiskR,
// 5=DiskW.  These are bare integers rather than the typed constants, so this
// switch and buildSortedRows must be kept in step by hand.
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
		case 4:
			less = a.ReadBps < b.ReadBps
		case 5:
			less = a.WriteBps < b.WriteBps
		default:
			less = a.CPUPct < b.CPUPct
		}
		if ascending {
			return less
		}
		return !less
	})
}
