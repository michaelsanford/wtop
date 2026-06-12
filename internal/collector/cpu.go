package collector

import (
	"github.com/shirou/gopsutil/v4/cpu"
)

func collectCPU() (CPUSnapshot, error) {
	perCore, err := cpu.Percent(0, true)
	if err != nil {
		return CPUSnapshot{}, err
	}

	aggr, err := cpu.Percent(0, false)
	if err != nil {
		return CPUSnapshot{}, err
	}

	aggrPct := 0.0
	if len(aggr) > 0 {
		aggrPct = aggr[0]
	}

	return CPUSnapshot{
		CorePcts: perCore,
		AggrPct:  aggrPct,
	}, nil
}
