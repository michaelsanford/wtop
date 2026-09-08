package panels

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/michaelsanford/wtop/internal/collector"
)

// CPUHeight is what the root model subtracts when sizing the process table. If
// it disagrees with what CPU() actually renders, every panel below it is
// misplaced — so the two must be checked against each other, not in isolation.
func TestCPUHeight_MatchesRenderedPanel(t *testing.T) {
	widths := []int{40, 60, 80, 110, 160, 240}
	coreCounts := []int{0, 1, 2, 3, 4, 6, 8, 12, 16, 24, 32, 64, 128}

	for _, w := range widths {
		for _, cores := range coreCounts {
			snap := collector.CPUSnapshot{
				CorePcts: make([]float64, cores),
				AggrPct:  50,
			}
			for i := range snap.CorePcts {
				snap.CorePcts[i] = float64(i % 101) // spread across colour thresholds
			}

			want := CPUHeight(cores, w)
			got := lipgloss.Height(CPU(snap, w))
			if got != want {
				t.Errorf("width=%d cores=%d: CPU() rendered %d lines, CPUHeight() reserved %d",
					w, cores, got, want)
			}
		}
	}
}

func TestCPUHeight_CapsAtFourGridRowsWhenWidthAllows(t *testing.T) {
	// The grid targets at most 4 rows, but columns are dropped (adding rows) when
	// cells would fall below minCellW — so the cap only holds once the terminal is
	// wide enough to fit every column at a legible width.
	const maxGridRows = 4
	for _, cores := range []int{1, 4, 8, 16, 32, 64, 128, 256} {
		if got := CPUHeight(cores, 1200); got > maxGridRows+3 {
			t.Errorf("cores=%d at width 1200: height %d exceeds %d grid rows + border/avg",
				cores, got, maxGridRows)
		}
	}
}

// The inverse of the above: on a narrow terminal the panel is allowed to grow
// past 4 rows, and CPUHeight must still predict it exactly (covered broadly by
// TestCPUHeight_MatchesRenderedPanel; pinned here as the documented trade-off).
func TestCPUHeight_TradesRowsForLegibilityWhenNarrow(t *testing.T) {
	const cores = 64
	wide := CPUHeight(cores, 1200)
	narrow := CPUHeight(cores, 60)
	if narrow <= wide {
		t.Errorf("narrow terminal should need more rows: narrow=%d wide=%d", narrow, wide)
	}
}

func TestCPU_NoCoresStillRendersAvgBar(t *testing.T) {
	out := CPU(collector.CPUSnapshot{AggrPct: 0}, 80)
	if !strings.Contains(out, "Avg") {
		t.Error("expected an Avg bar even with no per-core data")
	}
	if got, want := lipgloss.Height(out), CPUHeight(0, 80); got != want {
		t.Errorf("height %d, want %d", got, want)
	}
}

// scaleComposition feeds strings.Repeat, which panics on a negative count. The
// segments must therefore always be non-negative and sum to exactly width.
func TestScaleComposition_SegmentsAlwaysFillWidthExactly(t *testing.T) {
	tests := []struct {
		name                    string
		val1, val2, val3, total uint64
		width                   int
	}{
		{"even thirds", 30, 30, 30, 90, 30},
		{"all in first", 100, 0, 0, 100, 40},
		{"empty", 0, 0, 0, 100, 40},
		{"rounding leaves a remainder", 33, 33, 33, 100, 7},
		{"values sum beyond total", 80, 80, 80, 100, 40},
		{"single value exceeds total", 120, 10, 10, 100, 40},
		{"wildly oversized first value", 1 << 40, 1, 1, 100, 20},
		{"tiny width", 50, 25, 25, 100, 1},
		{"typical ram split", 8 << 30, 1 << 30, 4 << 30, 16 << 30, 32},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			n1, n2, n3, free := scaleComposition(tc.val1, tc.val2, tc.val3, tc.total, tc.width)

			for name, n := range map[string]int{"n1": n1, "n2": n2, "n3": n3, "free": free} {
				if n < 0 {
					t.Errorf("%s = %d; strings.Repeat would panic", name, n)
				}
			}
			if sum := n1 + n2 + n3 + free; sum != tc.width {
				t.Errorf("segments sum to %d, want exactly width %d", sum, tc.width)
			}
		})
	}
}

func TestScaleComposition_DegenerateInputs(t *testing.T) {
	// total == 0 cannot be divided by; the whole bar is free space.
	if n1, n2, n3, free := scaleComposition(1, 2, 3, 0, 25); n1 != 0 || n2 != 0 || n3 != 0 || free != 25 {
		t.Errorf("zero total: got (%d,%d,%d,%d), want (0,0,0,25)", n1, n2, n3, free)
	}
	// Non-positive widths must not produce negative repeats.
	for _, w := range []int{0, -5} {
		n1, n2, n3, free := scaleComposition(50, 25, 25, 100, w)
		if n1 != 0 || n2 != 0 || n3 != 0 || free != w {
			t.Errorf("width=%d: got (%d,%d,%d,%d)", w, n1, n2, n3, free)
		}
	}
}

// The composition bar is the direct consumer of scaleComposition; exercise the
// real call path so a regression surfaces as a failure rather than a panic in
// the running TUI.
func TestRenderMemCompositionBar_DoesNotPanic(t *testing.T) {
	cases := []struct{ inUse, mod, standby, total uint64 }{
		{8 << 30, 1 << 30, 4 << 30, 16 << 30},
		{0, 0, 0, 16 << 30},
		{16 << 30, 0, 0, 16 << 30},
		{20 << 30, 5 << 30, 5 << 30, 16 << 30}, // inconsistent inputs
		{1, 1, 1, 0},                           // zero total
	}
	for _, c := range cases {
		for _, w := range []int{0, 1, 8, 40} {
			bar := renderMemCompositionBar(c.inUse, c.mod, c.standby, c.total, w)
			if got := lipgloss.Width(bar); got != w && w > 0 {
				t.Errorf("inUse=%d total=%d width=%d: bar rendered %d visible chars",
					c.inUse, c.total, w, got)
			}
		}
	}
}

func TestRenderMemBar_DoesNotPanic(t *testing.T) {
	for _, w := range []int{0, 1, 8, 40} {
		bar := renderMemBar(4<<30, 2<<30, 1<<30, 8<<30, w)
		if got := lipgloss.Width(bar); got != w && w > 0 {
			t.Errorf("width=%d: bar rendered %d visible chars", w, got)
		}
	}
}

func TestRenderBar_ClampsPercentage(t *testing.T) {
	for _, pct := range []float64{-50, 0, 50, 100, 150} {
		bar := renderBar(pct, 20, colorGreen)
		if got := lipgloss.Width(bar); got != 20 {
			t.Errorf("pct=%v: width %d, want 20", pct, got)
		}
	}
	if got := renderBar(50, 0, colorGreen); got != "" {
		t.Errorf("zero width should render empty, got %q", got)
	}
}

// --- panel height containment -------------------------------------------------
//
// lipgloss Width() word-wraps and Height() is only a minimum, so an over-long
// content line silently makes a panel taller than the caller reserved and pushes
// the rest of the view off the bottom of the terminal.  RenderPanel clips every
// line to guarantee this can't happen; these tests are the guard.

func gpuFixture(src collector.GPUSource) collector.GPUSnapshot {
	return collector.GPUSnapshot{
		Name: "NVIDIA GeForce RTX 5070 Laptop GPU", Source: src,
		UtilPct: 12, MemUtilPct: 8, MemUsedMiB: 1234, MemTotMiB: 8188,
		TempC: 49, PowerDrawW: 110, PowerLimitW: 115, PState: "P0",
		GfxClockMHz: 2505, MemClockMHz: 10501, FanPct: 40,
	}
}

func TestPanelsNeverExceedReservedHeight(t *testing.T) {
	const inner = 9
	want := inner + 2 // + top and bottom border

	mem := collector.MemSnapshot{
		RAMUsedBytes: 20 << 30, RAMTotalBytes: 64 << 30, RAMPct: 31,
		CommittedBytes: 30 << 30, CommitLimitBytes: 70 << 30,
		ModifiedBytes: 1 << 30, StandbyBytes: 8 << 30, FreeBytes: 2 << 30,
		SwapUsedBytes: 1 << 30, SwapTotalBytes: 8 << 30,
	}
	nets := []collector.NetSnapshot{}
	for i := 0; i < 16; i++ {
		nets = append(nets, collector.NetSnapshot{
			Name: "Some Very Long Adapter Name #" + string(rune('A'+i)), BytesSentPerSec: 1 << 20, BytesRecvPerSec: 1 << 22,
		})
	}

	disks := []collector.DiskSnapshot{
		{Index: 0, Volumes: []string{"C:"}, ReadBytesPerSec: 1 << 20, WriteBytesPerSec: 1 << 22, BusyPct: 41},
		{Index: 1, Volumes: []string{"D:", "E:"}, BusyPct: 3},
		{Index: 2}, {Index: 3}, {Index: 4},
	}
	vols := []collector.VolumeSnapshot{
		{Mount: "C:", UsedBytes: 340 << 30, TotalBytes: 476 << 30, UsedPct: 72},
		{Mount: "D:", UsedBytes: 1<<40 + 600<<30, TotalBytes: 9 << 40, UsedPct: 18},
		{Mount: "E:", UsedBytes: 88 << 30, TotalBytes: 932 << 30, UsedPct: 9},
		{Mount: "F:", UsedPct: 97, UsedBytes: 900 << 30, TotalBytes: 928 << 30},
		{Mount: "G:", UsedPct: 50, UsedBytes: 1 << 30, TotalBytes: 2 << 30},
		{Mount: "H:", UsedPct: 20, UsedBytes: 1 << 30, TotalBytes: 5 << 30},
	}

	panelsUnderTest := map[string]func(w int) string{
		"Mem":        func(w int) string { return Mem(mem, w, inner) },
		"Disk/empty": func(w int) string { return Disk(nil, nil, w, inner) },
		"Disk/full":  func(w int) string { return Disk(disks, vols, w, inner) },
		"Disk/one":   func(w int) string { return Disk(disks[:1], vols[:1], w, inner) },
		"GPU/none":   func(w int) string { return GPU(collector.GPUSnapshot{}, 0, 0, w, inner) },
		"GPU/nvml":   func(w int) string { return GPU(gpuFixture(collector.GPUSourceNvidiaSmi), 0, 2, w, inner) },
		"GPU/pdh":    func(w int) string { return GPU(gpuFixture(collector.GPUSourcePowerShell), 0, 0, w, inner) },
		"Net/empty":  func(w int) string { return Net(nil, w, inner) },
		"Net/many":   func(w int) string { return Net(nets, w, inner) },
	}

	for _, w := range []int{12, 20, 26, 30, 36, 40, 55, 80, 110, 200} {
		for name, render := range panelsUnderTest {
			if got := lipgloss.Height(render(w)); got != want {
				t.Errorf("%s at width %d: height %d, want %d", name, w, got, want)
			}
		}
	}
}

func TestRenderPanel_ClipsOverlongLinesToTheBox(t *testing.T) {
	lines := make([]string, 8)
	for i := range lines {
		lines[i] = strings.Repeat("x", 200)
	}
	const width, height = 40, 9

	out := RenderPanel(lines, width, height)
	if got := lipgloss.Height(out); got != height+2 {
		t.Fatalf("height %d, want %d", got, height+2)
	}
	for i, l := range strings.Split(out, "\n") {
		if got := lipgloss.Width(l); got > width {
			t.Errorf("line %d is %d cells wide, want at most %d", i, got, width)
		}
	}
}

func TestFit_LeavesFittingLinesAlone(t *testing.T) {
	in := []string{"a", "bb", "ccc"}
	out := fit(in, 10)
	if &in[0] != &out[0] {
		t.Error("fit copied a slice that already fits; the render path should not allocate here")
	}
}

func TestTruncate_DoesNotSplitRunesOrCountEscapes(t *testing.T) {
	// Wide runes are two cells each: five of them plus the ellipsis is 11.
	if got := lipgloss.Width(truncate("温度温度温度温度温度", 11)); got != 11 {
		t.Errorf("wide runes: width %d, want 11", got)
	}
	// A byte-indexed truncate would split these and emit replacement characters.
	if got := truncate("héllo wörld", 100); got != "héllo wörld" {
		t.Errorf("fitting string was altered: %q", got)
	}
	if got := truncate("abc", 0); got != "" {
		t.Errorf("zero budget: %q, want empty", got)
	}
}

func TestGPUNameLine_NarrowAndUnicode(t *testing.T) {
	// Must not panic on narrow widths, high total counts, or non-ASCII characters
	for _, inner := range []int{0, 1, 2, 3, 5, 10, 20, 40} {
		for _, total := range []int{0, 1, 2, 10, 100} {
			_ = gpuNameLine("NVIDIA GeForce RTX™ 4090", 0, total, inner)
			_ = gpuNameLine("GPU (integrated)", 0, total, inner)
			_ = gpuNameLine("", 0, total, inner)
		}
	}
}
