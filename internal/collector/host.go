package collector

import (
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

// collectHostFallback is the portable path.  Unlike the disk and power sensors
// there is a real cross-platform implementation here worth using, so the stub
// delegates to gopsutil rather than returning nothing.
func collectHostFallback() HostSnapshot {
	secs, err := host.Uptime()
	if err != nil {
		return HostSnapshot{}
	}
	up := time.Duration(secs) * time.Second
	return HostSnapshot{Uptime: up, BootTime: time.Now().Add(-up)}
}
