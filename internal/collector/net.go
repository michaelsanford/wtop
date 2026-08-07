package collector

import (
	"time"

	psnet "github.com/shirou/gopsutil/v4/net"
)

func collectNet(
	prevBytes map[string][2]uint64,
	prevTime time.Time,
) ([]NetSnapshot, map[string][2]uint64, time.Time) {
	now := time.Now()
	counters, err := psnet.IOCounters(true)
	if err != nil {
		return nil, prevBytes, prevTime
	}

	newBytes := make(map[string][2]uint64, len(counters))
	var snaps []NetSnapshot

	elapsed := now.Sub(prevTime).Seconds()
	if elapsed <= 0 {
		elapsed = 1
	}

	for _, c := range counters {
		newBytes[c.Name] = [2]uint64{c.BytesSent, c.BytesRecv}

		var sentPerSec, recvPerSec float64
		if prev, ok := prevBytes[c.Name]; ok && !prevTime.IsZero() {
			sentPerSec = netRate(c.BytesSent, prev[0], elapsed)
			recvPerSec = netRate(c.BytesRecv, prev[1], elapsed)
		}

		// skip loopback and interfaces with no traffic ever
		if c.BytesSent == 0 && c.BytesRecv == 0 {
			continue
		}

		snaps = append(snaps, NetSnapshot{
			Name:            c.Name,
			BytesSentPerSec: sentPerSec,
			BytesRecvPerSec: recvPerSec,
		})
	}

	return snaps, newBytes, now
}

// netRate returns the per-second transfer rate between two counter samples.
//
// Interface byte counters are monotonic, so cur < prev means the counter was
// reset (adapter disable/reconnect, driver reload).  The comparison must happen
// before the subtraction: cur-prev is unsigned arithmetic and would wrap to
// ~1.8e19 rather than producing a negative that a later sign check could catch.
func netRate(cur, prev uint64, elapsed float64) float64 {
	if elapsed <= 0 || cur < prev {
		return 0
	}
	return float64(cur-prev) / elapsed
}
