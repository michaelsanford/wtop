package collector

import (
	"fmt"
	"testing"
	"time"
)

// Benchmarks in this file come in two flavours.
//
// Plain BenchmarkXxx are deterministic: no syscalls, no gopsutil, no scheduler
// noise.  Their allocation counts are the durable signal and are comparable
// across machines and across commits.
//
// BenchmarkLive_Xxx measure real system calls.  They are useful for tracking the
// wall-clock cost of a collection tick, but the numbers depend on how many
// processes and interfaces the host happens to have, so they are only meaningful
// against another run on the same machine.  The prefix exists so the two sets can
// be run separately:
//
//	go test -run='^$' -bench='^Benchmark[^L]' ./...   # deterministic only
//
// Every benchmark writes to a package-level sink; without that the compiler is
// free to delete the call it is supposed to be measuring.
var (
	sinkChain map[int32]struct{}
	sinkRate  float64
	sinkProcs []ProcSnapshot
	sinkCPU   CPUSnapshot
	sinkNet   []NetSnapshot
	sinkSnap  Snapshot
)

// benchChainStart is the PID benchPPIDMaps hands to selfChainPIDs.  It sits at the
// bottom of the synthetic ancestry chain, so the walk climbs the full depth.
const benchChainStart int32 = 1000

// benchPPIDMaps builds the two maps collectProcs feeds to selfChainPIDs: a
// depth-deep ancestry chain rooted at explorer.exe (so the chainStopNames lookup
// is exercised on the final hop rather than being skipped) padded out with
// n unrelated processes, which is what makes the map lookups realistic.
func benchPPIDMaps(n, depth int) (map[int32]int32, map[int32]string) {
	ppidOf := make(map[int32]int32, n+depth+1)
	nameOf := make(map[int32]string, n+depth+1)

	// The session root the walk must stop below.
	ppidOf[1] = 0
	nameOf[1] = "explorer.exe"

	// benchChainStart → benchChainStart+1 → … → 1.
	for i := range depth {
		pid := benchChainStart + int32(i)
		parent := pid + 1
		if i == depth-1 {
			parent = 1
		}
		ppidOf[pid] = parent
		nameOf[pid] = fmt.Sprintf("host%d.exe", i)
	}

	// Filler: a flat forest hanging off explorer, never visited by the walk but
	// present in the maps the walk probes.
	for i := range n {
		pid := int32(10000 + i)
		ppidOf[pid] = 1
		nameOf[pid] = fmt.Sprintf("proc%d.exe", i)
	}
	return ppidOf, nameOf
}

func BenchmarkSelfChainPIDs(b *testing.B) {
	for _, depth := range []int{4, 8, 16} {
		b.Run(fmt.Sprintf("depth=%d", depth), func(b *testing.B) {
			ppidOf, nameOf := benchPPIDMaps(4000, depth)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				sinkChain = selfChainPIDs(benchChainStart, ppidOf, nameOf)
			}
		})
	}
}

// BenchmarkNetRate guards a property rather than a cost: netRate is pure
// arithmetic and should stay at zero allocations.
func BenchmarkNetRate(b *testing.B) {
	b.ReportAllocs()
	for i := range b.N {
		sinkRate = netRate(uint64(i)+5_000_000, 5_000_000, 1.0)
	}
}

func BenchmarkLive_CollectProcs(b *testing.B) {
	b.ReportAllocs()
	for range b.N {
		sinkProcs, _ = collectProcs()
	}
}

func BenchmarkLive_CollectCPU(b *testing.B) {
	// Prime gopsutil's PDH counters the way New() does; cpu.Percent(0, …) diffs
	// against internal state, so the first call is not representative.
	_, _ = collectCPU()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		sinkCPU, _ = collectCPU()
	}
}

func BenchmarkLive_CollectNet(b *testing.B) {
	// Thread the counter maps across iterations exactly as defaultCollector does,
	// so each call takes the rate-computing path rather than the first-sample one.
	_, prevBytes, prevTime := collectNet(nil, time.Time{})
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		sinkNet, prevBytes, prevTime = collectNet(prevBytes, prevTime)
	}
}

// BenchmarkLive_Collect measures a whole tick.  GPU is not part of it: Collect
// only ever reads gpuCache, and the refresh happens in a detached goroutine.
func BenchmarkLive_Collect(b *testing.B) {
	// New() spawns warmup(); constructing one per iteration would measure warmup.
	c := New()
	time.Sleep(200 * time.Millisecond) // let warmup land before the first sample
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		sinkSnap, _ = c.Collect()
	}
}
