package collector

import (
	"sync"

	"github.com/shirou/gopsutil/v4/cpu"
)

func collectCPU() (CPUSnapshot, error) {
	return collectCPUNative()
}

// effectiveMHz converts the "% Processor Performance" counter into an actual
// clock speed.
//
// The percentage is relative to the base clock and legitimately exceeds 100
// under turbo — 191% was measured on a machine sustaining boost — so it must not
// be clamped the way utilisation percentages are.  Clamping here would silently
// report every boosting CPU as running at its base clock.
func effectiveMHz(baseMHz uint64, perfPct float64) float64 {
	if baseMHz == 0 || perfPct <= 0 {
		return 0
	}
	return float64(baseMHz) * perfPct / 100.0
}

func collectCPUFallback() (CPUSnapshot, error) {
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
