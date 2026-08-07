package panels

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
	"github.com/michaelsanford/wtop/internal/collector"
	"github.com/muesli/termenv"
)

// Everything in this package is a pure render function, so every benchmark here
// is deterministic and its allocation count is the number worth watching.  See
// internal/collector/bench_test.go for the BenchmarkLive_ convention.
//
// Results go to a package-level sink; without one the compiler can delete the
// call being measured.
var (
	sinkRows    []table.Row
	sinkCols    []table.Column
	sinkPanel   string
	sinkBarStr  string
	benchWidths = []int{80, 160}
)

// forceColor makes lipgloss emit real escape sequences for the duration of one
// benchmark.
//
// Under `go test` stdout is not a terminal, so lipgloss detects no colour support
// and every Render() call becomes a near-no-op that returns the input unchanged.
// That skips exactly the work these benchmarks exist to measure — wtop always
// runs in a colour terminal.  ANSI256 matches the 256-colour palette in
// internal/ui/styles.go.
//
// Scoped to the benchmark rather than set in a TestMain so the package's tests,
// several of which assert on plain-text output, keep running against the profile
// they were written for.
func forceColor(b *testing.B) {
	b.Helper()
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	b.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}

// benchProcs synthesises n processes.
//
// With tree=true the PPIDs form a branching-factor-4 forest, which is what
// BuildTreeRows actually walks — a flat list would skip the recursion entirely
// and measure nothing.  Names are deliberately mixed-case so the strings.ToLower
// inside sortTreeNodes' comparator does real work on the sort-by-name path.
// Two entries carry Self so markSelf is exercised.
func benchProcs(n int, tree bool) []collector.ProcSnapshot {
	const branch = 4
	procs := make([]collector.ProcSnapshot, 0, n)
	for i := range n {
		pid := int32(1000 + i)
		var ppid int32
		if tree && i > 0 {
			ppid = int32(1000 + (i-1)/branch)
		}
		name := fmt.Sprintf("Process%d.exe", i)
		if i%2 == 0 {
			name = strings.ToUpper(name)
		}
		p := proc(pid, ppid, name, float64(i%100), float64(i%2048))
		p.MemPct = float32(i % 100)
		p.Self = i == 0 || i == 1
		procs = append(procs, p)
	}
	return procs
}

// benchCPUSnap mirrors the fixture idiom in layout_test.go: a full spread of
// utilisation values so every branch of cpuBarColor gets taken.
func benchCPUSnap(cores int) collector.CPUSnapshot {
	snap := collector.CPUSnapshot{CorePcts: make([]float64, cores), AggrPct: 50}
	for i := range snap.CorePcts {
		snap.CorePcts[i] = float64(i % 101)
	}
	return snap
}

func BenchmarkBuildRows(b *testing.B) {
	// 128 is collector.maxProcs — the realistic per-tick size.  512 is there to
	// show the cost is linear and not hiding a quadratic.
	for _, n := range []int{128, 512} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			procs := benchProcs(n, false)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				sinkRows = BuildRows(procs)
			}
		})
	}
}

func BenchmarkBuildTreeRows(b *testing.B) {
	sorts := []struct {
		name string
		by   int
	}{
		{"cpu", sortCPU},
		// Name sorting lowercases inside the comparator, so this variant should
		// allocate visibly more than the CPU one.  If it does not, the comparator
		// is not being reached and the fixture is wrong.
		{"name", sortName},
	}
	for _, n := range []int{128, 512} {
		for _, s := range sorts {
			b.Run(fmt.Sprintf("n=%d/sort=%s", n, s.name), func(b *testing.B) {
				procs := benchProcs(n, true)
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					sinkRows = BuildTreeRows(procs, s.by, false)
				}
			})
		}
	}
}

// BuildColumns runs on resize and on sort changes rather than every frame, but
// it is cheap to cover and the arrow concatenation is easy to regress.
func BenchmarkBuildColumns(b *testing.B) {
	b.ReportAllocs()
	for range b.N {
		sinkCols = BuildColumns(160, ColCPU, false)
	}
}

func BenchmarkCPU(b *testing.B) {
	for _, cores := range []int{8, 16, 64, 128} {
		for _, w := range benchWidths {
			b.Run(fmt.Sprintf("cores=%d/w=%d", cores, w), func(b *testing.B) {
				forceColor(b)
				snap := benchCPUSnap(cores)
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					sinkPanel = CPU(snap, w)
				}
			})
		}
	}
}

// renderBar is the hottest leaf on the render path — once per core, plus the Avg
// bar, plus three more from the Mem panel and up to three from GPU.
func BenchmarkRenderBar(b *testing.B) {
	forceColor(b)
	b.ReportAllocs()
	for i := range b.N {
		sinkBarStr = renderBar(float64(i%101), 40, colorGreen)
	}
}

func BenchmarkMem(b *testing.B) {
	// CommittedBytes > 0 selects the Windows composition layout, which is the one
	// that actually ships and the more expensive of the two.
	snap := collector.MemSnapshot{
		RAMUsedBytes: 21 << 30, RAMTotalBytes: 32 << 30, RAMPct: 65,
		SwapUsedBytes: 3 << 30, SwapTotalBytes: 8 << 30, SwapPct: 37,
		CommittedBytes: 24 << 30, CommitLimitBytes: 40 << 30,
		ModifiedBytes: 1 << 30, StandbyBytes: 6 << 30, FreeBytes: 4 << 30,
	}
	for _, w := range []int{40, 80} {
		b.Run(fmt.Sprintf("w=%d", w), func(b *testing.B) {
			forceColor(b)
			b.ReportAllocs()
			for range b.N {
				sinkPanel = Mem(snap, w, 0)
			}
		})
	}
}

func BenchmarkGPU(b *testing.B) {
	// The nvidia-smi source renders the most lines, so it bounds the cost.
	snap := collector.GPUSnapshot{
		Name: "NVIDIA GeForce RTX 4080", UtilPct: 72, MemUtilPct: 45,
		MemUsedMiB: 8192, MemTotMiB: 16384, TempC: 61, PowerDrawW: 210,
		PowerLimitW: 320, GfxClockMHz: 2505, MemClockMHz: 10501, FanPct: 40,
		PState: "P0", Source: collector.GPUSourceNvidiaSmi,
	}
	forceColor(b)
	b.ReportAllocs()
	for range b.N {
		sinkPanel = GPU(snap, 0, 2, 40, 0)
	}
}

func BenchmarkNet(b *testing.B) {
	for _, n := range []int{1, 4, 16} {
		b.Run(fmt.Sprintf("ifaces=%d", n), func(b *testing.B) {
			forceColor(b)
			snaps := make([]collector.NetSnapshot, 0, n)
			for i := range n {
				snaps = append(snaps, collector.NetSnapshot{
					Name:            fmt.Sprintf("Ethernet %d", i),
					BytesSentPerSec: float64(i) * 1024 * 512,
					BytesRecvPerSec: float64(i) * 1024 * 1024,
				})
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				sinkPanel = Net(snaps, 40, 0)
			}
		})
	}
}

// RenderPanel builds a bordered lipgloss style from scratch on every call, and
// every panel calls it once per frame.
func BenchmarkRenderPanel(b *testing.B) {
	for _, n := range []int{10, 40} {
		b.Run(fmt.Sprintf("lines=%d", n), func(b *testing.B) {
			forceColor(b)
			lines := make([]string, n)
			for i := range lines {
				lines[i] = fmt.Sprintf("line %d with some representative content", i)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				sinkPanel = RenderPanel(lines, 80, 0)
			}
		})
	}
}
