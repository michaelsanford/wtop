package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/michaelsanford/wtop/internal/collector"
	"github.com/muesli/termenv"
)

// These benchmarks cover the per-frame and per-tick paths of the root model.
// They are deterministic — benchCollector replaces the real collector, so no
// syscall noise leaks in.  See internal/collector/bench_test.go for the
// BenchmarkLive_ convention used for the syscall-backed ones.
var (
	sinkRows  []table.Row
	sinkView  string
	sinkModel tea.Model
)

// forceColor makes lipgloss emit real escape sequences for the duration of one
// benchmark.
//
// Under `go test` stdout is not a terminal, so lipgloss detects no colour support
// and Render() returns its input unchanged.  Two things then go unmeasured: the
// style application itself, and — because nothing emits ANSI — the regex work in
// stripANSI that extendSelectedRow does on every line of the table.  Forcing
// ANSI256 (the palette internal/ui/styles.go uses) restores both.
//
// Scoped per-benchmark rather than set in a TestMain, so the package's tests keep
// running against the profile they were written for.
func forceColor(b *testing.B) {
	b.Helper()
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	b.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}

// benchCollector is a stand-in for defaultCollector.  collector.Collector is an
// exported single-method interface, which is what makes a full View() benchmark
// possible without touching the OS.
type benchCollector struct{ snap collector.Snapshot }

func (c benchCollector) Collect() (collector.Snapshot, error) { return c.snap, nil }

// benchProcs mirrors the generator in package panels.  It is duplicated rather
// than shared because panels' proc() helper is unexported and there is no test
// fixture package — the same split that already exists between procsFixture here
// and proc() there.
func benchProcs(n int) []collector.ProcSnapshot {
	const branch = 4
	procs := make([]collector.ProcSnapshot, 0, n)
	for i := range n {
		var ppid int32
		if i > 0 {
			ppid = int32(1000 + (i-1)/branch)
		}
		name := fmt.Sprintf("Process%d.exe", i)
		if i%2 == 0 {
			name = strings.ToUpper(name)
		}
		procs = append(procs, collector.ProcSnapshot{
			PID: int32(1000 + i), PPID: ppid, Name: name,
			CPUPct: float64(i % 100), MemPct: float32(i % 100),
			MemMB: float64(i % 2048), Self: i == 0 || i == 1,
		})
	}
	return procs
}

// benchProcCount matches collector.maxProcs — the number of rows a real tick
// delivers, and therefore the only size worth modelling at this level.  The
// panels benchmarks sweep the process count; here it stays fixed so the layout
// dimensions are the variable.
const benchProcCount = 128

// benchSnapshot is a fully populated tick: 16 cores, Windows-style memory
// composition, one nvidia GPU and four interfaces, so no panel takes an
// early-return path.
func benchSnapshot() collector.Snapshot {
	cpuSnap := collector.CPUSnapshot{CorePcts: make([]float64, 16), AggrPct: 42}
	for i := range cpuSnap.CorePcts {
		cpuSnap.CorePcts[i] = float64(i % 101)
	}
	net := make([]collector.NetSnapshot, 0, 4)
	for i := range 4 {
		net = append(net, collector.NetSnapshot{
			Name:            fmt.Sprintf("Ethernet %d", i),
			BytesSentPerSec: float64(i) * 1024 * 512,
			BytesRecvPerSec: float64(i) * 1024 * 1024,
		})
	}
	return collector.Snapshot{
		CPU: cpuSnap,
		Mem: collector.MemSnapshot{
			RAMUsedBytes: 21 << 30, RAMTotalBytes: 32 << 30, RAMPct: 65,
			SwapUsedBytes: 3 << 30, SwapTotalBytes: 8 << 30, SwapPct: 37,
			CommittedBytes: 24 << 30, CommitLimitBytes: 40 << 30,
			ModifiedBytes: 1 << 30, StandbyBytes: 6 << 30, FreeBytes: 4 << 30,
		},
		GPUs: []collector.GPUSnapshot{{
			Name: "NVIDIA GeForce RTX 4080", UtilPct: 72, MemUtilPct: 45,
			MemUsedMiB: 8192, MemTotMiB: 16384, TempC: 61, PowerDrawW: 210,
			PowerLimitW: 320, GfxClockMHz: 2505, MemClockMHz: 10501,
			PState: "P0", Source: collector.GPUSourceNvidiaSmi,
		}},
		Net:   net,
		Procs: benchProcs(benchProcCount),
	}
}

// benchModel returns a Model that has been through the setup View() depends on:
// a window size (View short-circuits while width is 0) and a snapshot (which is
// what populates the table rows and the final table height).
func benchModel(w, h int, tree bool) Model {
	snap := benchSnapshot()
	m := New(benchCollector{snap: snap})
	m.treeView = tree
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m = next.(Model)
	next, _ = m.Update(snapshotMsg{s: snap})
	return next.(Model)
}

// BenchmarkModelView is the headline number: one complete frame, which is
// re-rendered on every keystroke as well as every tick.  The two widths straddle
// the 110-column threshold where View switches from the 2-column layout to the
// 3-column one that also renders the Net panel.
func BenchmarkModelView(b *testing.B) {
	for _, tree := range []bool{false, true} {
		for _, dim := range []struct{ w, h int }{{80, 24}, {160, 50}} {
			name := fmt.Sprintf("flat/%dx%d", dim.w, dim.h)
			if tree {
				name = fmt.Sprintf("tree/%dx%d", dim.w, dim.h)
			}
			b.Run(name, func(b *testing.B) {
				forceColor(b)
				m := benchModel(dim.w, dim.h, tree)
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					sinkView = m.View()
				}
			})
		}
	}
}

// BenchmarkModelUpdate_Snapshot covers the once-per-second branch: rebuild every
// row, recompute the CPU panel height, resize the table.
func BenchmarkModelUpdate_Snapshot(b *testing.B) {
	for _, tree := range []bool{false, true} {
		name := "flat"
		if tree {
			name = "tree"
		}
		b.Run(name, func(b *testing.B) {
			m := benchModel(160, 50, tree)
			msg := snapshotMsg{s: benchSnapshot()}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				sinkModel, _ = m.Update(msg)
			}
		})
	}
}

func BenchmarkBuildSortedRows(b *testing.B) {
	sorts := []struct {
		name string
		by   SortField
	}{
		{"cpu", SortByCPU},
		{"mem", SortByMem},
		{"pid", SortByPID},
		{"name", SortByName},
	}
	procs := benchProcs(benchProcCount)
	for _, s := range sorts {
		b.Run(s.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				sinkRows = buildSortedRows(procs, s.by, false)
			}
		})
	}
}

// BenchmarkExtendSelectedRow measures the ANSI-stripping scan that locates the
// highlighted row.  The cost is positional — the scan runs stripANSI over each
// line and stops at the first match — so the gap between the two sub-benchmarks
// is the interesting number.
//
// "bottom" is the last row the table actually renders, not the last row it holds.
// The rendered view is header + border + (tableH - 2) data rows, and SetCursor on
// its own does not scroll the viewport, so a cursor past that range leaves the
// selected row off-screen.  extendSelectedRow would then find nothing and return
// early — measuring the miss path rather than the work.  Hence deriving the row
// from the rendered view, and the guard below.
func BenchmarkExtendSelectedRow(b *testing.B) {
	base := benchModel(160, 50, false)
	lastRendered := len(strings.Split(base.tbl.View(), "\n")) - 3

	positions := []struct {
		name string
		row  int
	}{
		{"top", 0},
		{"bottom", lastRendered},
	}
	for _, p := range positions {
		b.Run(p.name, func(b *testing.B) {
			// Set before the view is captured: it has to contain the per-cell ANSI
			// resets that are the whole reason this function exists.
			forceColor(b)
			m := base
			m.tbl.SetCursor(p.row)
			view := m.tbl.View()

			// A benchmark that silently measures the early return is worse than no
			// benchmark, so prove the row is found before timing anything.
			if m.extendSelectedRow(view) == view {
				b.Fatalf("row %d not found in the rendered view; benchmark would measure the miss path", p.row)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				sinkView = m.extendSelectedRow(view)
			}
		})
	}
}

func BenchmarkStatusBar(b *testing.B) {
	forceColor(b)
	m := benchModel(160, 50, false)
	b.ReportAllocs()
	for range b.N {
		sinkView = m.statusBar()
	}
}
