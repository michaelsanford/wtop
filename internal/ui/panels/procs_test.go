package panels

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/table"
	"github.com/michaelsanford/wtop/internal/collector"
)

// BuildTreeRows and sortTreeNodes take a ui.SortField, NOT one of the Col*
// column indices — the two numberings differ and are bridged only by SortColFor.
// Mixing them up silently sorts by the wrong field, so spell them out here.
const (
	sortCPU   = 0
	sortMem   = 1
	sortPID   = 2
	sortName  = 3
	sortDiskR = 4
	sortDiskW = 5
)

func proc(pid, ppid int32, name string, cpu, memMB float64) collector.ProcSnapshot {
	return collector.ProcSnapshot{PID: pid, PPID: ppid, Name: name, CPUPct: cpu, MemMB: memMB}
}

// rowPIDs extracts the PID column, which unlike Name carries no tree connectors.
func rowPIDs(rows []table.Row) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r[0])
	}
	return out
}

func TestBuildTreeRows_Empty(t *testing.T) {
	if got := BuildTreeRows(nil, sortCPU, false); got != nil {
		t.Errorf("expected nil for empty input, got %v", got)
	}
}

// Every process must appear exactly once, whatever the PPID graph looks like.
func TestBuildTreeRows_AllProcessesAppearExactlyOnce(t *testing.T) {
	tests := []struct {
		name  string
		procs []collector.ProcSnapshot
	}{
		{"simple forest", []collector.ProcSnapshot{
			proc(1, 0, "init", 0, 0),
			proc(2, 1, "child", 0, 0),
			proc(3, 2, "grandchild", 0, 0),
		}},
		{"absent parent is treated as a root", []collector.ProcSnapshot{
			proc(10, 999, "orphan", 0, 0),
			proc(11, 0, "root", 0, 0),
		}},
		// PID reuse can leave a stale PPID pointing at a recycled PID.
		{"two-node parent cycle", []collector.ProcSnapshot{
			proc(1, 0, "root", 0, 0),
			proc(2, 3, "cycleA", 0, 0),
			proc(3, 2, "cycleB", 0, 0),
		}},
		{"self parent", []collector.ProcSnapshot{
			proc(1, 0, "root", 0, 0),
			proc(4, 4, "self", 0, 0),
		}},
		{"three-node cycle", []collector.ProcSnapshot{
			proc(5, 6, "a", 0, 0),
			proc(6, 7, "b", 0, 0),
			proc(7, 5, "c", 0, 0),
		}},
		{"cycle alongside a real root", []collector.ProcSnapshot{
			proc(1, 0, "root", 0, 0),
			proc(2, 1, "child", 0, 0),
			proc(8, 9, "x", 0, 0),
			proc(9, 8, "y", 0, 0),
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rows := BuildTreeRows(tc.procs, sortPID, true)
			if len(rows) != len(tc.procs) {
				t.Fatalf("got %d rows for %d processes; some were dropped", len(rows), len(tc.procs))
			}
			seen := map[string]int{}
			for _, r := range rows {
				seen[r[0]]++
			}
			for _, p := range tc.procs {
				pid := strconv.Itoa(int(p.PID))
				if seen[pid] != 1 {
					t.Errorf("PID %s appeared %d times, want exactly 1", pid, seen[pid])
				}
			}
		})
	}
}

// A self-parent or cycle must not recurse forever; the failure mode is a stack
// overflow, so the assertion is essentially that this returns at all.
func TestBuildTreeRows_CycleTerminates(t *testing.T) {
	procs := []collector.ProcSnapshot{
		proc(1, 2, "a", 0, 0),
		proc(2, 1, "b", 0, 0),
	}
	done := make(chan int, 1)
	go func() { done <- len(BuildTreeRows(procs, sortPID, true)) }()
	if n := <-done; n != 2 {
		t.Errorf("got %d rows, want 2", n)
	}
}

func TestBuildTreeRows_Connectors(t *testing.T) {
	procs := []collector.ProcSnapshot{
		proc(1, 0, "root", 0, 0),
		proc(2, 1, "first", 0, 0),
		proc(3, 1, "second", 0, 0),
		proc(4, 2, "nested", 0, 0),
	}
	rows := BuildTreeRows(procs, sortPID, true)

	names := map[string]string{}
	for _, r := range rows {
		names[r[0]] = r[1]
	}

	if got := names["1"]; got != "root" {
		t.Errorf("root should have no connector, got %q", got)
	}
	// PID 2 has a sibling after it, so it gets a tee; PID 3 is last, so an elbow.
	if got := names["2"]; !strings.HasPrefix(got, "├─ ") {
		t.Errorf("non-final child should use ├─, got %q", got)
	}
	if got := names["3"]; !strings.HasPrefix(got, "└─ ") {
		t.Errorf("final child should use └─, got %q", got)
	}
	// PID 4 descends from a non-final child, so it inherits a continuation bar.
	if got := names["4"]; !strings.HasPrefix(got, "│  └─ ") {
		t.Errorf("child of a non-final parent should inherit │, got %q", got)
	}
}

// sortTreeNodes switches on bare integers while buildSortedRows switches on the
// typed SortField constants; nothing but these tests keeps the two numberings in
// step, so every field needs coverage in the tree view too.
func TestBuildTreeRows_SiblingsSortedByDiskIO(t *testing.T) {
	// Read and write rankings are inverted so sorting by the wrong one of the two
	// produces a visibly different order rather than an accidental match.
	procs := []collector.ProcSnapshot{
		{PID: 1, PPID: 0, Name: "root"},
		{PID: 2, PPID: 1, Name: "reader", ReadBps: 900, WriteBps: 100},
		{PID: 3, PPID: 1, Name: "writer", ReadBps: 100, WriteBps: 900},
		{PID: 4, PPID: 1, Name: "middle", ReadBps: 500, WriteBps: 500},
	}
	tests := []struct {
		name      string
		sortBy    int
		ascending bool
		want      []string
	}{
		{"disk read descending", sortDiskR, false, []string{"1", "2", "4", "3"}},
		{"disk read ascending", sortDiskR, true, []string{"1", "3", "4", "2"}},
		{"disk write descending", sortDiskW, false, []string{"1", "3", "4", "2"}},
		{"disk write ascending", sortDiskW, true, []string{"1", "2", "4", "3"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := BuildTreeRows(procs, tt.sortBy, tt.ascending)
			got := make([]string, len(rows))
			for i, r := range rows {
				got[i] = r[ColPID]
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildTreeRows_SiblingsSorted(t *testing.T) {
	// CPU and memory rankings are deliberately inverted, so sorting by the wrong
	// field produces a visibly different order rather than an accidental match.
	procs := []collector.ProcSnapshot{
		proc(1, 0, "root", 0, 0),
		proc(2, 1, "low-cpu-high-mem", 10, 900),
		proc(3, 1, "high-cpu-low-mem", 90, 100),
		proc(4, 1, "mid", 50, 300),
	}

	tests := []struct {
		name      string
		sortBy    int
		ascending bool
		want      []string
	}{
		{"cpu descending", sortCPU, false, []string{"1", "3", "4", "2"}},
		{"cpu ascending", sortCPU, true, []string{"1", "2", "4", "3"}},
		{"mem descending", sortMem, false, []string{"1", "2", "4", "3"}},
		{"mem ascending", sortMem, true, []string{"1", "3", "4", "2"}},
		{"pid descending", sortPID, false, []string{"1", "4", "3", "2"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := rowPIDs(BuildTreeRows(procs, tc.sortBy, tc.ascending))
			if !slices.Equal(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSortTreeNodes_ByName_IsCaseInsensitive(t *testing.T) {
	nodes := []*treeNode{
		{proc: proc(1, 0, "zebra", 0, 0)},
		{proc: proc(2, 0, "Apple", 0, 0)},
		{proc: proc(3, 0, "mango", 0, 0)},
	}
	sortTreeNodes(nodes, sortName, true)

	got := []string{nodes[0].proc.Name, nodes[1].proc.Name, nodes[2].proc.Name}
	if want := []string{"Apple", "mango", "zebra"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSortColFor_MapsEverySortFieldToADistinctColumn(t *testing.T) {
	// Mirrors ui.SortField: 0=CPU, 1=Mem, 2=PID, 3=Name, 4=DiskR, 5=DiskW. The two
	// numberings are bridged only by this function, so drift here silently
	// mislabels headers.
	const sortFieldCount = 6

	seen := map[int]int{}
	for f := 0; f < sortFieldCount; f++ {
		col := SortColFor(f)
		if col < 0 || col >= MaxCols {
			t.Errorf("SortColFor(%d) = %d, outside valid column range [0,%d)", f, col, MaxCols)
		}
		if prev, dup := seen[col]; dup {
			t.Errorf("SortColFor(%d) and SortColFor(%d) both map to column %d", prev, f, col)
		}
		seen[col] = f
	}

	if got := SortColFor(sortFieldCount + 5); got != ColCPU {
		t.Errorf("out-of-range sort field should fall back to ColCPU, got %d", got)
	}
}

func TestBuildColumns_ConsumesExactlyTerminalWidth(t *testing.T) {
	for _, termW := range []int{80, 100, 119, 120, 121, 200, 400} {
		cols := BuildColumns(termW, ColCPU, false)
		// The column count is fixed; columns that do not fit are zero-width, which
		// bubbles/table skips entirely.  A varying count would desync from the
		// fixed-width rows and panic in renderRow.
		if len(cols) != MaxCols {
			t.Fatalf("got %d columns, want %d", len(cols), MaxCols)
		}
		total, visible := 0, 0
		for _, c := range cols {
			if c.Width <= 0 {
				continue
			}
			total += c.Width
			visible++
		}
		// bubbles/table pads every rendered cell with one space on each side.
		if got := total + visible*cellPad; got != termW {
			t.Errorf("termW=%d: columns occupy %d, want %d", termW, got, termW)
		}
	}
}

// The two disk columns must appear and disappear together: one without the other
// is asymmetric, and it would strand a sort field on an unrendered column.
func TestBuildColumns_DiskColumnsAppearTogetherAtThreshold(t *testing.T) {
	for _, tc := range []struct {
		termW int
		want  bool
	}{
		{80, false}, {119, false}, {DiskColsMinWidth, true}, {200, true},
	} {
		cols := BuildColumns(tc.termW, ColCPU, false)
		gotR := cols[ColDiskR].Width > 0
		gotW := cols[ColDiskW].Width > 0
		if gotR != gotW {
			t.Errorf("termW=%d: disk columns disagree (read=%v write=%v)", tc.termW, gotR, gotW)
		}
		if gotR != tc.want {
			t.Errorf("termW=%d: disk columns visible=%v, want %v", tc.termW, gotR, tc.want)
		}
	}
}

// renderRow walks the row's cells and indexes m.cols by the same offset, so a row
// carrying more cells than there are columns panics.  Rows are fixed-width for
// exactly this reason; this test is the guard.
func TestBuildRows_AlwaysEmitsMaxColsCells(t *testing.T) {
	procs := []collector.ProcSnapshot{
		{PID: 1, PPID: 0, Name: "root.exe", CPUPct: 1, MemMB: 10, ReadBps: 2048, WriteBps: 0},
		{PID: 2, PPID: 1, Name: "child.exe", CPUPct: 3, MemMB: 20, ReadBps: 0, WriteBps: 1 << 20},
		{PID: 3, PPID: 1, Name: "other.exe", CPUPct: 2, MemMB: 30},
	}
	check := func(what string, rows []table.Row) {
		t.Helper()
		if len(rows) == 0 {
			t.Fatalf("%s: no rows", what)
		}
		for i, r := range rows {
			if len(r) != MaxCols {
				t.Errorf("%s row %d has %d cells, want %d", what, i, len(r), MaxCols)
			}
		}
	}
	check("BuildRows", BuildRows(procs))
	check("BuildTreeRows", BuildTreeRows(procs, 0, false))

	// And the invariant that matters at runtime: cells never outnumber columns.
	for _, termW := range []int{20, 80, 119, 120, 200} {
		if got, want := len(BuildColumns(termW, ColCPU, false)), len(BuildRows(procs)[0]); got != want {
			t.Errorf("termW=%d: %d columns but %d cells per row", termW, got, want)
		}
	}
}

func TestFmtRateCell(t *testing.T) {
	tests := []struct {
		bps  float64
		want string
	}{
		{0, "0"},
		{-1, "0"},
		{512, "512B"},
		{1 << 10, "1.0K"},
		{1536, "1.5K"},
		{1 << 20, "1.0M"},
		{1 << 30, "1.0G"},
	}
	for _, tt := range tests {
		if got := fmtRateCell(tt.bps); got != tt.want {
			t.Errorf("fmtRateCell(%v) = %q, want %q", tt.bps, got, tt.want)
		}
		if got := len(fmtRateCell(tt.bps)); got > diskW {
			t.Errorf("fmtRateCell(%v) is %d chars, wider than the %d-cell column", tt.bps, got, diskW)
		}
	}
}

func TestBuildColumns_NameFloorsOnNarrowTerminals(t *testing.T) {
	for _, termW := range []int{0, 10, 30, 40} {
		cols := BuildColumns(termW, ColCPU, false)
		if cols[ColName].Width < 10 {
			t.Errorf("termW=%d: name column %d fell below the floor of 10", termW, cols[ColName].Width)
		}
	}
}

func TestBuildColumns_SortArrowIsPlainText(t *testing.T) {
	// Styling headers with lipgloss breaks bubbles/table alignment, because the
	// ANSI bytes count against the column width.
	cols := BuildColumns(120, ColMemMB, false)
	if !strings.HasSuffix(cols[ColMemMB].Title, "↓") {
		t.Errorf("descending sort should mark the column with ↓, got %q", cols[ColMemMB].Title)
	}
	if strings.Contains(cols[ColMemMB].Title, "\x1b") {
		t.Errorf("header contains ANSI escapes: %q", cols[ColMemMB].Title)
	}

	asc := BuildColumns(120, ColMemMB, true)
	if !strings.HasSuffix(asc[ColMemMB].Title, "↑") {
		t.Errorf("ascending sort should mark the column with ↑, got %q", asc[ColMemMB].Title)
	}

	for i, c := range cols {
		if i != ColMemMB && (strings.Contains(c.Title, "↑") || strings.Contains(c.Title, "↓")) {
			t.Errorf("column %d should carry no arrow, got %q", i, c.Title)
		}
	}
}

func TestBuildRows_FormatsOneRowPerProcess(t *testing.T) {
	procs := []collector.ProcSnapshot{
		{PID: 42, Name: "test.exe", CPUPct: 12.34, MemPct: 5.67, MemMB: 89.1, ReadBps: 1536, WriteBps: 0},
	}
	rows := BuildRows(procs)
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	// Rows always carry the disk cells, whether or not the current width shows them.
	want := []string{"42", "test.exe", "12.3", "5.7", "89.1", "1.5K", "0"}
	if !slices.Equal([]string(rows[0]), want) {
		t.Errorf("got %v, want %v", []string(rows[0]), want)
	}
}

func TestBuildRows_Empty(t *testing.T) {
	if got := BuildRows(nil); len(got) != 0 {
		t.Errorf("expected no rows, got %d", len(got))
	}
}

func TestBuildRows_MarksOnlySelfProcesses(t *testing.T) {
	procs := []collector.ProcSnapshot{
		{PID: 1, Name: "wtop.exe", Self: true},
		{PID: 2, Name: "chrome.exe"},
	}
	rows := BuildRows(procs)

	if got, want := rows[0][ColName], selfMarker+"wtop.exe"; got != want {
		t.Errorf("self process: got %q, want %q", got, want)
	}
	// Unmarked rows get no compensating pad — names stay flush left.
	if got, want := rows[1][ColName], "chrome.exe"; got != want {
		t.Errorf("non-self process: got %q, want %q", got, want)
	}
}

func TestBuildRows_MarkerIsPlainText(t *testing.T) {
	// Same constraint as the sort arrow: ANSI inside a cell breaks bubbles/table's
	// width accounting.
	rows := BuildRows([]collector.ProcSnapshot{{PID: 1, Name: "wtop.exe", Self: true}})
	if strings.Contains(rows[0][ColName], "\x1b") {
		t.Errorf("marker contains ANSI escapes: %q", rows[0][ColName])
	}
}

func TestBuildTreeRows_MarkerFollowsTheConnector(t *testing.T) {
	procs := []collector.ProcSnapshot{
		proc(1, 0, "root", 0, 0),
		{PID: 2, PPID: 1, Name: "pwsh.exe", Self: true},
		proc(3, 1, "other", 0, 0),
	}
	rows := BuildTreeRows(procs, sortPID, true)

	names := map[string]string{}
	for _, r := range rows {
		names[r[ColPID]] = r[ColName]
	}

	// The marker belongs between the connector and the name, so the box-drawing
	// column stays aligned with the unmarked rows around it.
	if got, want := names["2"], "├─ "+selfMarker+"pwsh.exe"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := names["3"], "└─ other"; got != want {
		t.Errorf("non-self child: got %q, want %q", got, want)
	}
	if got, want := names["1"], "root"; got != want {
		t.Errorf("non-self root: got %q, want %q", got, want)
	}
}

func TestBuildTreeRows_Live(t *testing.T) {
	c := collector.New()
	snap, err := c.Collect()
	if err != nil {
		t.Fatal(err)
	}
	rows := BuildTreeRows(snap.Procs, sortCPU, false)
	t.Logf("Total tree rows: %d", len(rows))
	for i, r := range rows {
		if i < 40 {
			t.Logf("[%3d] PID=%-6s Name=%s", i, r[0], r[1])
		}
	}
}
