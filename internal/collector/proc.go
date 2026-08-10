package collector

import (
	"math"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/shirou/gopsutil/v4/process"
)

const maxProcs = 128

func collectProcs() ([]ProcSnapshot, error) {
	return collectProcsNative()
}

func collectProcsFallback() ([]ProcSnapshot, error) {
	procs, err := process.Processes()
	if err != nil {
		return nil, err
	}

	numWorkers := 16
	if len(procs) < numWorkers {
		numWorkers = len(procs)
	}

	jobs := make(chan *process.Process, len(procs))
	results := make(chan ProcSnapshot, len(procs))

	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range jobs {
				// Read the parent before the name check: unnamed (protected or
				// exited) processes are dropped from the display, but their PPID
				// edge is still needed so the self-chain walk below can traverse
				// through them.
				ppid, _ := p.Ppid()

				name, _ := p.Name()
				if name == "" {
					results <- ProcSnapshot{PID: p.Pid, PPID: ppid}
					continue
				}

				cpuPct, _ := p.CPUPercent()
				memPct, _ := p.MemoryPercent()
				memInfo, _ := p.MemoryInfo()

				var memMB float64
				if memInfo != nil {
					memMB = float64(memInfo.RSS) / (1024 * 1024)
				}

				results <- ProcSnapshot{
					PID:    p.Pid,
					PPID:   ppid,
					Name:   name,
					CPUPct: cpuPct,
					MemPct: memPct,
					MemMB:  memMB,
				}
			}
		}()
	}

	for _, p := range procs {
		jobs <- p
	}
	close(jobs)

	// Wait for workers to finish in a separate goroutine and close results
	go func() {
		wg.Wait()
		close(results)
	}()

	all := make([]ProcSnapshot, 0, len(procs))
	for s := range results {
		all = append(all, s)
	}

	// Build the parent and name maps from *every* enumerated process, including
	// the unnamed ones, then mark wtop and its ancestors before anything is
	// filtered away.
	ppidOf := make(map[int32]int32, len(all))
	nameOf := make(map[int32]string, len(all))
	for _, s := range all {
		ppidOf[s.PID] = s.PPID
		nameOf[s.PID] = s.Name
	}
	// os.Getpid returns an int; gopsutil PIDs are int32.  Windows PIDs sit far
	// below the int32 ceiling, but bound the conversion rather than assume it —
	// out of range simply means nothing gets marked.
	var chain map[int32]struct{}
	if self := os.Getpid(); self > 0 && self <= math.MaxInt32 {
		chain = selfChainPIDs(int32(self), ppidOf, nameOf)
	}

	snaps := make([]ProcSnapshot, 0, len(all))
	for _, s := range all {
		if s.Name == "" {
			continue
		}
		if _, ok := chain[s.PID]; ok {
			s.Self = true
		}
		snaps = append(snaps, s)
	}

	// sort by CPU% descending, limit to maxProcs
	sort.Slice(snaps, func(i, j int) bool {
		return snaps[i].CPUPct > snaps[j].CPUPct
	})
	if len(snaps) > maxProcs {
		cut := snaps[maxProcs:]
		snaps = snaps[:maxProcs:maxProcs]
		// Pin the self-chain back in: the terminal host and shell are usually idle
		// and would otherwise fall off the list, hiding the marker and breaking the
		// chain in tree view.  This can push the slice a few rows past maxProcs,
		// which is fine — the UI re-sorts from scratch either way.
		for _, s := range cut {
			if s.Self {
				snaps = append(snaps, s)
			}
		}
	}

	return snaps, nil
}

// chainStopNames are the session roots the self-chain walk refuses to climb past.
// Nearly every interactive process descends from explorer.exe and every service
// from services.exe, so including them would mark a near-universal ancestor and
// say nothing about which session is hosting wtop.  Stopping here keeps the chain
// to the part that is actually specific: the terminal host and the shells below
// it.  Compared lowercased — Windows process names are case-insensitive.
var chainStopNames = map[string]struct{}{
	"explorer.exe": {}, // desktop shell — parent of everything interactive
	"services.exe": {}, // SCM — parent of everything running as a service
	"wininit.exe":  {},
	"winlogon.exe": {},
	"smss.exe":     {},
	"csrss.exe":    {},
	"system":       {},
	"idle":         {},
}

// selfChainPIDs walks the parent chain upward from pid and returns every PID
// traversed, inclusive of pid.  The walk stops at PPID 0, at a parent absent from
// ppidOf, or just below a chainStopNames entry — the stop process itself is never
// included.  Membership doubles as the visited set, so a self-parent or an A→B→A
// loop — possible when Windows recycles a PID — terminates instead of spinning,
// the same hazard panels.dfsTreeRows guards against.
func selfChainPIDs(pid int32, ppidOf map[int32]int32, nameOf map[int32]string) map[int32]struct{} {
	chain := make(map[int32]struct{}, 8)
	for {
		if _, seen := chain[pid]; seen {
			return chain
		}
		ppid, ok := ppidOf[pid]
		if !ok {
			return chain
		}
		if isChainStop(nameOf[pid]) {
			return chain
		}
		chain[pid] = struct{}{}
		if ppid == 0 {
			return chain
		}
		pid = ppid
	}
}

func isChainStop(name string) bool {
	_, stop := chainStopNames[strings.ToLower(name)]
	return stop
}
