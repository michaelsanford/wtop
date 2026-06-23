package collector

import (
	"sort"
	"sync"

	"github.com/shirou/gopsutil/v4/process"
)

const maxProcs = 128

func collectProcs() ([]ProcSnapshot, error) {
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
				name, _ := p.Name()
				if name == "" {
					continue
				}

				ppid, _ := p.Ppid()
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

	snaps := make([]ProcSnapshot, 0, len(procs))
	for s := range results {
		snaps = append(snaps, s)
	}

	// sort by CPU% descending, limit to maxProcs
	sort.Slice(snaps, func(i, j int) bool {
		return snaps[i].CPUPct > snaps[j].CPUPct
	})
	if len(snaps) > maxProcs {
		snaps = snaps[:maxProcs]
	}

	return snaps, nil
}
