package collector

import (
	"math"
	"time"

	"github.com/shirou/gopsutil/v4/host"
)

// HostSnapshot holds machine-wide facts that are not tied to a single device.
type HostSnapshot struct {
	Uptime   time.Duration
	BootTime time.Time
}

func collectHost() HostSnapshot {
	return collectHostNative()
}

// scaleDuration converts a raw counter of unit-sized ticks into a Duration.
//
// time.Duration counts int64 nanoseconds, so it saturates a little past 292
// years and a large enough counter would wrap into a negative uptime.  That is
// unreachable with a real clock, but the reading comes from outside the process
// and a wild value should yield no uptime rather than a nonsensical one.
func scaleDuration(n uint64, unit time.Duration) (time.Duration, bool) {
	if unit <= 0 || n > uint64(math.MaxInt64)/uint64(unit) {
		return 0, false
	}
	//nolint:gosec // G115: the bound above is exactly what makes this conversion
	// safe; gosec does not track it.
	return time.Duration(n) * unit, true
}

// collectHostFallback is the portable path.  Unlike the disk and power sensors
// there is a real cross-platform implementation here worth using, so the stub
// delegates to gopsutil rather than returning nothing.
func collectHostFallback() HostSnapshot {
	secs, err := host.Uptime()
	if err != nil {
		return HostSnapshot{}
	}
	up, ok := scaleDuration(secs, time.Second)
	if !ok {
		return HostSnapshot{}
	}
	return HostSnapshot{Uptime: up, BootTime: time.Now().Add(-up)}
}
