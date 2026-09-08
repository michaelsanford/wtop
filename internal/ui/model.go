package ui

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/michaelsanford/wtop/internal/collector"
	"github.com/michaelsanford/wtop/internal/ui/panels"
	"github.com/michaelsanford/wtop/internal/version"
	"github.com/michaelsanford/wtop/internal/websearch"
)

func stripANSI(s string) string {
	if !strings.Contains(s, "\x1b") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	inEscape := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inEscape {
			if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
				inEscape = false
			}
			continue
		}
		if c == 0x1b {
			inEscape = true
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// Layout: CPU full-width on top; Mem + GPU side-by-side below; process table.
const (
	metricsPanelInnerH = 9 // inner content lines for the Mem / GPU row (NVIDIA fills all 9)
	statusBarH         = 1
)

// sortDefaultAsc controls the initial sort direction when cycling to each field.
var sortDefaultAsc = [sortFieldCount]bool{
	SortByCPU:   false, // highest CPU first
	SortByMem:   false, // highest mem first
	SortByPID:   true,  // lowest PID first
	SortByName:  true,  // A-Z
	SortByDiskR: false, // busiest reader first
	SortByDiskW: false, // busiest writer first
}

// SortField controls the process list sort order.
type SortField int

const (
	SortByCPU SortField = iota
	SortByMem
	SortByPID
	SortByName
	SortByDiskR
	SortByDiskW
	sortFieldCount
)

var sortLabels = [...]string{"CPU%", "MEM MB", "PID", "Name", "DISK R", "DISK W"}

// Messages exchanged within the Bubble Tea update loop.
type tickMsg time.Time

type snapshotMsg struct{ s collector.Snapshot }

type snapshotErrMsg struct{ err error }

type killResultMsg struct {
	pid int32
	err error
}

type searchResultMsg struct{ err error }

// Model is the root Bubble Tea model.
type Model struct {
	snap     collector.Snapshot
	lastErr  error
	sortBy   SortField
	rotIdx   rotationSlot
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

// New returns an initialised Model with an initial snapshot pre-populated for instant display.
func New(coll collector.Collector) Model {
	host, _ := os.Hostname()
	initAsc := sortDefaultAsc[SortByCPU]
	tbl := table.New(
		table.WithColumns(panels.BuildColumns(120, panels.SortColFor(int(SortByCPU)), initAsc)),
		table.WithFocused(true),
		table.WithHeight(10),
	)
	tbl.SetStyles(panels.TableStyles())

	m := Model{
		coll:     coll,
		keys:     defaultKeyMap,
		hostname: host,
		tbl:      tbl,
		tableH:   10,
		sortAsc:  initAsc,
	}

	if coll != nil {
		if snap, err := coll.Collect(); err == nil {
			m.snap = snap
			m.tbl.SetRows(m.buildRows())
		}
	}

	return m
}

func (m Model) Init() tea.Cmd {
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

func searchCmd(query string) tea.Cmd {
	return func() tea.Msg {
		return searchResultMsg{err: websearch.Search(query)}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	// Forward all messages (e.g. mouse scroll, mouse click, native table keys)
	// to the table component if we are not in a confirmation overlay.
	if !m.confirming {
		var tblCmd tea.Cmd
		m.tbl, tblCmd = m.tbl.Update(msg)
		if tblCmd != nil {
			cmds = append(cmds, tblCmd)
		}
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		cpuH := panels.CPUHeight(len(m.snap.CPU.CorePcts), m.width)
		m.tableH = computeTableHeight(m.height, cpuH)
		m.tbl.SetHeight(m.tableH)
		m.tbl.SetColumns(panels.BuildColumns(msg.Width, panels.SortColFor(int(m.sortBy)), m.sortAsc))
		return m, tea.Batch(cmds...)

	case tickMsg:
		return m, tea.Batch(append(cmds, collectCmd(m.coll), tick())...)

	case snapshotMsg:
		m.snap = msg.s
		m.lastErr = nil
		m.tbl.SetRows(m.buildRows())
		// Recompute table height now that we know the actual core count.
		cpuH := panels.CPUHeight(len(msg.s.CPU.CorePcts), m.width)
		m.tableH = computeTableHeight(m.height, cpuH)
		m.tbl.SetHeight(m.tableH)
		return m, tea.Batch(cmds...)

	case snapshotErrMsg:
		m.lastErr = msg.err
		return m, tea.Batch(cmds...)

	case killResultMsg:
		return m, tea.Batch(cmds...)

	case searchResultMsg:
		// Surfaced in the status bar rather than dropped: a failed launch opens no
		// window, so without this the key looks dead.  The next snapshot clears it.
		if msg.err != nil {
			m.lastErr = msg.err
		}
		return m, tea.Batch(cmds...)

	case tea.KeyMsg:
		var newModel tea.Model
		var keyCmd tea.Cmd
		if m.confirming {
			newModel, keyCmd = m.handleConfirmKey(msg)
		} else {
			newModel, keyCmd = m.handleNormalKey(msg)
		}
		resModel := newModel.(Model)
		if keyCmd != nil {
			cmds = append(cmds, keyCmd)
		}
		return resModel, tea.Batch(cmds...)
	}
	return m, tea.Batch(cmds...)
}

func (m Model) handleNormalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit

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

	case key.Matches(msg, m.keys.CycleIO):
		// A no-op when every slot is already on screen, exactly as [g] is a no-op
		// with a single GPU.  The rotation set is a compile-time constant, so the
		// index can never point at something that has gone away.
		if rotatingSlots(m.width) < int(rotationCount) {
			m.rotIdx = (m.rotIdx + 1) % rotationCount
		}

	case key.Matches(msg, m.keys.Kill):
		if pid, row, ok := m.selectedRow(); ok {
			m.confirming = true
			m.confirm = confirmState{pid: pid, name: row[1]}
		}

	case key.Matches(msg, m.keys.Search):
		// The name is taken from the snapshot, not row[1]: the rendered cell
		// carries the tree prefix and the self marker.
		if pid, _, ok := m.selectedRow(); ok {
			if q := searchQuery(m.snap.Procs, pid); q != "" {
				return m, searchCmd(q)
			}
		}
	}
	return m, nil
}

// selectedRow returns the PID and the rendered cells of the highlighted row.
// The PID is parsed back out of the rendered cell because the table cursor is
// the only selection state the model keeps.
func (m Model) selectedRow() (int32, table.Row, bool) {
	rows := m.tbl.Rows()
	cur := m.tbl.Cursor()
	if cur < 0 || cur >= len(rows) {
		return 0, nil, false
	}
	row := rows[cur]
	var pid int32
	if _, err := fmt.Sscanf(row[0], "%d", &pid); err != nil {
		return 0, nil, false
	}
	return pid, row, true
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

	// Row 2: Memory, then the rotating slots.
	metricsRow := m.metricsRow()

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

// metricsRow lays out the Memory panel plus the visible rotating slots.
func (m Model) metricsRow() string {
	k := rotatingSlots(m.width)
	boxes := k + 1 // Mem is always present

	// Integer division leaves a remainder; the last box absorbs it, matching the
	// existing "netW := width - memW - gpuW" idiom.
	each := m.width / boxes
	rendered := make([]string, 0, boxes)
	rendered = append(rendered, panels.Mem(m.snap.Mem, each, metricsPanelInnerH))
	used := each

	for i := 0; i < k; i++ {
		w := each
		if i == k-1 {
			w = m.width - used
		}
		used += each

		slot := rotationSlot((int(m.rotIdx) + i) % int(rotationCount))
		rendered = append(rendered, m.renderSlot(slot, w))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, rendered...)
}

func (m Model) renderSlot(slot rotationSlot, width int) string {
	switch slot {
	case slotNet:
		return panels.Net(m.snap.Net, width, metricsPanelInnerH)
	case slotDisk:
		return panels.Disk(m.snap.Disks, m.snap.Volumes, width, metricsPanelInnerH)
	default:
		gpuSnap, gpuIdx, gpuTotal := currentGPU(m.snap.GPUs, m.gpuIdx)
		return panels.GPU(gpuSnap, gpuIdx, gpuTotal, width, metricsPanelInnerH)
	}
}

func (m Model) statusBar() string {
	sortLabel := fmt.Sprintf("sort:%s", sortLabels[m.sortBy])
	gpuHint := ""
	if len(m.snap.GPUs) > 1 {
		gpuHint = "  [g] gpu"
	}
	ioHint := ""
	if rotatingSlots(m.width) < int(rotationCount) && m.width >= 100 {
		// Gated at 100 rather than the 90 the wide hint tier uses: the left cluster
		// is already ~78 cells there, and another hint would push it into the gap.
		ioHint = "  [i] io"
	}
	treeHint := "  [t] tree"
	if m.treeView {
		treeHint = "  [t] tree●"
	}
	searchHint := ""
	if m.width >= 100 {
		// Gated at 100 for the same reason as ioHint: at 90 the left cluster is
		// already ~78 cells and another hint closes the gap to the right cluster.
		searchHint = "  [?] search"
	}

	var hint string
	if m.width >= 90 {
		hint = fmt.Sprintf("[q] quit  [↑↓/jk] scroll  [s] %s  [d] invert  [x] kill%s%s%s%s", sortLabel, gpuHint, ioHint, treeHint, searchHint)
	} else if m.width >= 70 {
		hint = fmt.Sprintf("[q] quit  [s] %s  [d] inv  [x] kill%s%s%s", sortLabel, gpuHint, ioHint, treeHint)
	} else {
		hint = fmt.Sprintf("[q] quit  [s] %s  [x] kill", sortLabels[m.sortBy])
	}

	errStr := ""
	if m.lastErr != nil {
		errStr = "  err:" + m.lastErr.Error()
	}

	left := hint + errStr

	// styleStatusBar pads one cell on each side, so the text budget is two cells
	// narrower than the terminal.  Clipping to m.width instead overflows that
	// budget and wraps the bar onto a second line, pushing the view past the
	// terminal height.
	inner := m.width - 2
	if inner < 0 {
		inner = 0
	}

	leftLen := lipgloss.Width(left)
	// -1 leaves room for the single-space minimum gap between the clusters.
	right := m.statusRight(inner - leftLen - 1)
	rightLen := lipgloss.Width(right)

	gap := inner - leftLen - rightLen
	if gap < 1 {
		gap = 1
	}

	bar := left + fmt.Sprintf("%*s", gap, "") + right
	if ansi.StringWidth(bar) > inner {
		bar = ansi.Truncate(bar, inner, "")
	}
	return styleStatusBar.Width(m.width).Render(bar)
}

// statusRight builds the status bar's right-hand cluster to fit in budget cells.
//
// Rather than pinning each field to a hard width breakpoint, the cluster is
// assembled from everything eligible and then thinned until it fits: terminal
// width is only half the equation, since the left cluster and the hostname both
// vary in length. Fixed breakpoints alone would leave a 15-character hostname
// colliding with the hints on the very machines where the bar matters most.
//
// dropOrder encodes what is given up first. The clock is the least actionable
// number; uptime is next. Then the hostname, which is static and usually already
// known to whoever is looking at the screen. Battery outlives all of them: on a
// laptop the charge state is the one thing worth knowing at any width, and it is
// the only field here that can be urgent. The version is never dropped.
func (m Model) statusRight(budget int) string {
	if budget <= 0 || m.width < 50 {
		return ""
	}

	const (
		iUptime = iota
		iClock
		iPower
		iHost
		iVersion
		nParts
	)
	long := m.width >= 130

	parts := [nParts]string{
		iUptime:  fmtUptime(m.snap.Host.Uptime, long),
		iClock:   fmtClock(m.snap.CPU.EffectiveMHz),
		iPower:   fmtPower(m.snap.Power, long),
		iVersion: "wtop " + version.Version,
	}
	if m.width >= 80 {
		parts[iHost] = m.hostname
	}

	dropOrder := [...]int{iClock, iUptime, iHost, iPower}
	for i := 0; ; i++ {
		if joined := joinStatus(parts[:]...); lipgloss.Width(joined) <= budget {
			return joined
		}
		if i >= len(dropOrder) {
			// Only the version is left and it still does not fit; the caller clips.
			return joinStatus(parts[:]...)
		}
		parts[dropOrder[i]] = ""
	}
}

// joinStatus joins the non-empty parts of the status bar's right cluster.  Parts
// go missing routinely — no battery, no clock counter, uptime not yet read — and
// skipping them here keeps every caller from having to check.
func joinStatus(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "  ")
}

// fmtUptime renders uptime as "up 3d 04:12" (long) or "up 3d04h" (short).
func fmtUptime(d time.Duration, long bool) string {
	if d <= 0 {
		return ""
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	if long {
		if days > 0 {
			return fmt.Sprintf("up %dd %02d:%02d", days, hours, mins)
		}
		return fmt.Sprintf("up %02d:%02d", hours, mins)
	}
	if days > 0 {
		return fmt.Sprintf("up %dd%02dh", days, hours)
	}
	return fmt.Sprintf("up %dh%02dm", hours, mins)
}

func fmtClock(mhz float64) string {
	if mhz <= 0 {
		return ""
	}
	return fmt.Sprintf("%.2fGHz", mhz/1000)
}

// fmtPower renders the AC/battery state, or "" when no battery is installed —
// a desktop should not carry a permanently empty slot in its status bar.
//
// Urgency is spelled out in words rather than colour on purpose: the bar is
// clipped to width as a plain string, and an escape sequence cut in half would
// corrupt the terminal.  Same reason the column headers stay plain text.
func fmtPower(p collector.PowerSnapshot, long bool) string {
	if !p.Present {
		return ""
	}

	label := "BAT"
	if p.AC == collector.ACOnline {
		label = "AC"
	}

	out := label
	if p.PctValid {
		out = fmt.Sprintf("%s %.0f%%", label, p.Pct)
	}

	switch p.State {
	case collector.BatteryCharging:
		out += "▲"
	case collector.BatteryDischarging:
		out += "▼"
	}

	if long && p.RemainingValid && p.State == collector.BatteryDischarging {
		h := int(p.Remaining.Hours())
		m := int(p.Remaining.Minutes()) % 60
		out += fmt.Sprintf(" %d:%02d", h, m)
	}
	if p.PctValid && p.Pct < 10 && p.State != collector.BatteryCharging {
		out += " LOW"
	}
	if p.Saver {
		out += " SAVER"
	}
	return out
}

// The metrics row is a fixed Memory column plus as many rotating slots as the
// terminal can hold.  Slots page through rotationSet with [i].
//
// At the default offset the rendering is identical to what earlier versions
// showed at every width — Mem+GPU below 110, Mem+GPU+Net at 110 — with Disk
// added as a fourth box once there is room.  Nothing moves for existing users;
// [i] simply reaches panels that were previously unreachable at that width.
type rotationSlot int

const (
	slotGPU rotationSlot = iota
	slotNet
	slotDisk
	rotationCount
)

// Widths at which a further rotating slot fits.  160/4 = 40 outer = 36 inner is
// the point at which the GPU clocks line and the Net interface lines stop being
// truncated, so the threshold is derived rather than picked.
const (
	threeSlotMinWidth = 110
	fourSlotMinWidth  = 160
)

// rotatingSlots is how many of the rotation set are on screen at this width.
func rotatingSlots(width int) int {
	switch {
	case width >= fourSlotMinWidth:
		return 3
	case width >= threeSlotMinWidth:
		return 2
	default:
		return 1
	}
}

func computeTableHeight(termH, cpuOuterH int) int {
	// bubbles/table SetHeight(h) renders exactly h lines: it subtracts the header
	// internally (viewport.Height = h - headerHeight), so the header must NOT be
	// reserved again here or the view comes up short and wastes process rows.
	reserved := cpuOuterH + (metricsPanelInnerH + 2) + statusBarH
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

	// Clip before styling: below ~50 columns bubbles/table's own columns are wider
	// than the terminal, and handing an over-long line to a Width() style wraps it
	// onto a second row, making the view taller than the terminal.
	plain := stripANSI(lines[lineIdx])
	if ansi.StringWidth(plain) > m.width {
		plain = ansi.Truncate(plain, m.width, "")
	}
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
				return panels.CaseFoldLess(sorted[i].Name, sorted[j].Name)
			}
			return panels.CaseFoldLess(sorted[j].Name, sorted[i].Name)
		})
	case SortByDiskR:
		sort.Slice(sorted, func(i, j int) bool {
			if ascending {
				return sorted[i].ReadBps < sorted[j].ReadBps
			}
			return sorted[i].ReadBps > sorted[j].ReadBps
		})
	case SortByDiskW:
		sort.Slice(sorted, func(i, j int) bool {
			if ascending {
				return sorted[i].WriteBps < sorted[j].WriteBps
			}
			return sorted[i].WriteBps > sorted[j].WriteBps
		})
	default:
		// No default sort, leave sorted as-is
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
