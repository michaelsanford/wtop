//go:build !windows

package collector

import "time"

func collectNetNative(
	prevBytes map[string][2]uint64,
	prevTime time.Time,
) ([]NetSnapshot, map[string][2]uint64, time.Time) {
	return collectNetFallback(prevBytes, prevTime)
}
