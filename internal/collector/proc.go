package collector

import (
	"sort"

	"github.com/shirou/gopsutil/v4/process"
)

const maxProcs = 128

func collectProcs() ([]ProcSnapshot, error) {
	procs, err := process.Processes()
	if err != nil {
		return nil, err
	}

	snaps := make([]ProcSnapshot, 0, len(procs))
	for _, p := range procs {
		name, _ := p.Name()
		if name == "" {
			continue
		}

		cpuPct, _ := p.CPUPercent()
		memPct, _ := p.MemoryPercent()
		memInfo, _ := p.MemoryInfo()

		var memMB float64
		if memInfo != nil {
			memMB = float64(memInfo.RSS) / (1024 * 1024)
		}

		snaps = append(snaps, ProcSnapshot{
			PID:    p.Pid,
			Name:   name,
			CPUPct: cpuPct,
			MemPct: memPct,
			MemMB:  memMB,
		})
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
