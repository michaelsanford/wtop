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
