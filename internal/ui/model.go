package ui

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/michaelsanford/wtop/internal/collector"
	"github.com/michaelsanford/wtop/internal/ui/panels"
	"github.com/michaelsanford/wtop/internal/version"
)

var reANSI = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func stripANSI(s string) string {
	return reANSI.ReplaceAllString(s, "")
}

// Layout: CPU full-width on top; Mem + GPU side-by-side below; process table.
const (
	metricsPanelInnerH = 9 // inner content lines for the Mem / GPU row (NVIDIA fills all 9)
	statusBarH         = 1
	tableHeaderH       = 2 // header row + bottom border line
)

// sortDefaultAsc controls the initial sort direction when cycling to each field.
var sortDefaultAsc = [sortFieldCount]bool{
	SortByCPU:  false, // highest CPU first
	SortByMem:  false, // highest mem first
	SortByPID:  true,  // lowest PID first
	SortByName: true,  // A-Z
}

// SortField controls the process list sort order.
type SortField int

const (
	SortByCPU SortField = iota
	SortByMem
	SortByPID
	SortByName
	sortFieldCount
)

var sortLabels = [...]string{"CPU%", "MEM MB", "PID", "Name"}

// Messages exchanged within the Bubble Tea update loop.
type tickMsg time.Time

type snapshotMsg struct{ s collector.Snapshot }

type snapshotErrMsg struct{ err error }

type killResultMsg struct {
	pid int32
	err error
}

// Model is the root Bubble Tea model.
type Model struct {
	snap     collector.Snapshot
	lastErr  error
	sortBy   SortField
	sortAsc  bool
	gpuIdx   int
	treeView bool
	tbl      table.Model
	width    int
	height   int
	tableH   int
	hostname string

	confirming bool
	confirm    confirmState

	coll collector.Collector
	keys KeyMap
}

// New returns an initialised Model.
func New(coll collector.Collector) Model {
	host, _ := os.Hostname()
	initAsc := sortDefaultAsc[SortByCPU]
	tbl := table.New(
		table.WithColumns(panels.BuildColumns(120, panels.SortColFor(int(SortByCPU)), initAsc)),
		table.WithFocused(true),
		table.WithHeight(10),
	)
	tbl.SetStyles(panels.TableStyles())

	return Model{
		coll:     coll,
		keys:     defaultKeyMap,
		hostname: host,
		tbl:      tbl,
		tableH:   10,
		sortAsc:  initAsc,
	}
}

func (m Model) Init() tea.Cmd {
	// Collect immediately on startup rather than waiting for the first tick,
	// so data appears as soon as the terminal is ready.
	return tea.Batch(collectCmd(m.coll), tick())
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func collectCmd(coll collector.Collector) tea.Cmd {
	return func() tea.Msg {
		snap, err := coll.Collect()
		if err != nil {
			return snapshotErrMsg{err}
		}
		return snapshotMsg{snap}
	}
}

func killCmd(pid int32) tea.Cmd {
	return func() tea.Msg {
		err := collector.KillProcess(pid)
		return killResultMsg{pid: pid, err: err}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		cpuH := panels.CPUHeight(len(m.snap.CPU.CorePcts), m.width)
		m.tableH = computeTableHeight(m.height, cpuH)
		m.tbl.SetHeight(m.tableH)
		m.tbl.SetColumns(panels.BuildColumns(msg.Width, panels.SortColFor(int(m.sortBy)), m.sortAsc))
		return m, nil

	case tickMsg:
		return m, tea.Batch(collectCmd(m.coll), tick())

	case snapshotMsg:
		m.snap = msg.s
		m.lastErr = nil
		m.tbl.SetRows(m.buildRows())
		// Recompute table height now that we know the actual core count.
		cpuH := panels.CPUHeight(len(msg.s.CPU.CorePcts), m.width)
		m.tableH = computeTableHeight(m.height, cpuH)
		m.tbl.SetHeight(m.tableH)
		return m, nil

	case snapshotErrMsg:
		m.lastErr = msg.err
		return m, nil

	case killResultMsg:
		return m, nil

	case tea.KeyMsg:
		if m.confirming {
			return m.handleConfirmKey(msg)
		}
		return m.handleNormalKey(msg)
	}
	return m, nil
}

func (m Model) handleNormalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit

	case key.Matches(msg, m.keys.Up):
		m.tbl.MoveUp(1)

	case key.Matches(msg, m.keys.Down):
		m.tbl.MoveDown(1)

	case key.Matches(msg, m.keys.SortInvert):
		m.sortAsc = !m.sortAsc
		m.tbl.SetColumns(panels.BuildColumns(m.width, panels.SortColFor(int(m.sortBy)), m.sortAsc))
		m.tbl.SetRows(m.buildRows())

	case key.Matches(msg, m.keys.Sort):
		m.sortBy = (m.sortBy + 1) % sortFieldCount
		m.sortAsc = sortDefaultAsc[m.sortBy]
		m.tbl.SetColumns(panels.BuildColumns(m.width, panels.SortColFor(int(m.sortBy)), m.sortAsc))
		m.tbl.SetRows(m.buildRows())

	case key.Matches(msg, m.keys.Tree):
		m.treeView = !m.treeView
		m.tbl.SetRows(m.buildRows())

	case key.Matches(msg, m.keys.CycleGPU):
		if n := len(m.snap.GPUs); n > 1 {
			m.gpuIdx = (m.gpuIdx + 1) % n
		}

	case key.Matches(msg, m.keys.Kill):
		rows := m.tbl.Rows()
		cur := m.tbl.Cursor()
		if cur >= 0 && cur < len(rows) {
			row := rows[cur]
			var pid int32
			fmt.Sscanf(row[0], "%d", &pid)
			m.confirming = true
			m.confirm = confirmState{pid: pid, name: row[1]}
		}
	}
	return m, nil
}

func (m Model) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Confirm):
		m.confirming = false
		return m, killCmd(m.confirm.pid)
	case key.Matches(msg, m.keys.Cancel):
		m.confirming = false
	}
	return m, nil
}

func (m Model) View() string {
	if m.width == 0 {
		return "Initialising…"
	}

	// Row 1: CPU full width — height adapts to core count automatically
	cpuRow := panels.CPU(m.snap.CPU, m.width)

	// Row 2: Memory (left half) + GPU (right half)
	memW := m.width / 2
	gpuW := m.width - memW
	gpuSnap, gpuIdx, gpuTotal := currentGPU(m.snap.GPUs, m.gpuIdx)
	metricsRow := lipgloss.JoinHorizontal(lipgloss.Top,
		panels.Mem(m.snap.Mem, memW, metricsPanelInnerH),
		panels.GPU(gpuSnap, gpuIdx, gpuTotal, gpuW, metricsPanelInnerH),
	)

	// Row 3: Process table — re-render the selected row at full terminal width so
	// the highlight background isn't broken by per-cell ANSI resets.
	tblView := m.extendSelectedRow(m.tbl.View())

	// Row 4: Status bar
	status := m.statusBar()

	full := lipgloss.JoinVertical(lipgloss.Left, cpuRow, metricsRow, tblView, status)

	if m.confirming {
		return renderConfirmOverlay(m.confirm, m.width, m.height)
	}
	return full
}

func (m Model) statusBar() string {
	sortLabel := fmt.Sprintf("sort:%s", sortLabels[m.sortBy])
	gpuHint := ""
	if len(m.snap.GPUs) > 1 {
		gpuHint = "  [g] gpu"
	}
	treeHint := "  [t] tree"
	if m.treeView {
		treeHint = "  [t] tree●"
	}
	hint := fmt.Sprintf("[q] quit  [↑↓/jk] scroll  [s] %s  [d] invert  [x] kill%s%s", sortLabel, gpuHint, treeHint)

	errStr := ""
	if m.lastErr != nil {
		errStr = "  err:" + m.lastErr.Error()
	}

	left := hint + errStr
	right := fmt.Sprintf("%s  wtop %s", m.hostname, version.Version)

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}

	bar := left + fmt.Sprintf("%*s", gap, "") + right
	return styleStatusBar.Width(m.width).Render(bar)
}

func computeTableHeight(termH, cpuOuterH int) int {
	reserved := cpuOuterH + (metricsPanelInnerH + 2) + statusBarH + tableHeaderH
	h := termH - reserved
	if h < 3 {
		h = 3
	}
	return h
}

// extendSelectedRow finds the selected row in the rendered table view, strips its
// ANSI codes, and re-renders it with a solid full-width background highlight.
// Per-cell \x1b[m resets in bubbles/table prevent the Selected style from covering
// the whole row; replacing the line with a clean plain-text render avoids that.
func (m Model) extendSelectedRow(view string) string {
	if m.width == 0 {
		return view
	}
	selectedRow := m.tbl.SelectedRow()
	if len(selectedRow) == 0 {
		return view
	}
	selectedPID := selectedRow[0]

	lines := strings.Split(view, "\n")
	lineIdx := -1
	for i := 2; i < len(lines); i++ {
		plain := stripANSI(lines[i])
		if len(plain) > 1+panels.PidW {
			pidPart := strings.TrimSpace(plain[1 : 1+panels.PidW])
			if pidPart == selectedPID {
				lineIdx = i
				break
			}
		}
	}

	if lineIdx == -1 || lineIdx >= len(lines) {
		return view
	}

	plain := stripANSI(lines[lineIdx])
	lines[lineIdx] = lipgloss.NewStyle().
		Background(lipgloss.Color("57")).
		Foreground(lipgloss.Color("229")).
		Width(m.width).
		Render(plain)
	return strings.Join(lines, "\n")
}

// buildRows returns table rows for the current snapshot, using either flat or
// tree layout depending on m.treeView.
func (m Model) buildRows() []table.Row {
	if m.treeView {
		return panels.BuildTreeRows(m.snap.Procs, int(m.sortBy), m.sortAsc)
	}
	return buildSortedRows(m.snap.Procs, m.sortBy, m.sortAsc)
}

func buildSortedRows(procs []collector.ProcSnapshot, sortBy SortField, ascending bool) []table.Row {
	sorted := make([]collector.ProcSnapshot, len(procs))
	copy(sorted, procs)

	switch sortBy {
	case SortByCPU:
		sort.Slice(sorted, func(i, j int) bool {
			if ascending {
				return sorted[i].CPUPct < sorted[j].CPUPct
			}
			return sorted[i].CPUPct > sorted[j].CPUPct
		})
	case SortByMem:
		sort.Slice(sorted, func(i, j int) bool {
			if ascending {
				return sorted[i].MemMB < sorted[j].MemMB
			}
			return sorted[i].MemMB > sorted[j].MemMB
		})
	case SortByPID:
		sort.Slice(sorted, func(i, j int) bool {
			if ascending {
				return sorted[i].PID < sorted[j].PID
			}
			return sorted[i].PID > sorted[j].PID
		})
	case SortByName:
		sort.Slice(sorted, func(i, j int) bool {
			if ascending {
				return sorted[i].Name < sorted[j].Name
			}
			return sorted[i].Name > sorted[j].Name
		})
	}

	return panels.BuildRows(sorted)
}

// currentGPU returns the snapshot, 0-based index, and total count to pass to
// panels.GPU. Returns a zero snapshot (GPUSourceNone) when the slice is empty.
func currentGPU(gpus []collector.GPUSnapshot, idx int) (collector.GPUSnapshot, int, int) {
	if len(gpus) == 0 {
		return collector.GPUSnapshot{}, 0, 0
	}
	if idx >= len(gpus) {
		idx = 0
	}
	return gpus[idx], idx, len(gpus)
}
