package collector

import (
	"sync"

	"github.com/shirou/gopsutil/v4/cpu"
)

func collectCPU() (CPUSnapshot, error) {
	var (
		perCore    []float64
		aggrPct    float64
		perCoreErr error
		wg         sync.WaitGroup
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		perCore, perCoreErr = cpu.Percent(0, true)
	}()
	go func() {
		defer wg.Done()
		aggr, err := cpu.Percent(0, false)
		if err == nil && len(aggr) > 0 {
			aggrPct = aggr[0]
		}
	}()
	wg.Wait()
	if perCoreErr != nil {
		return CPUSnapshot{}, perCoreErr
	}
	return CPUSnapshot{
		CorePcts: perCore,
		AggrPct:  aggrPct,
	}, nil
}
