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
			sentDelta := float64(c.BytesSent - prev[0])
			recvDelta := float64(c.BytesRecv - prev[1])
			if sentDelta < 0 {
				sentDelta = 0
			}
			if recvDelta < 0 {
				recvDelta = 0
			}
			sentPerSec = sentDelta / elapsed
			recvPerSec = recvDelta / elapsed
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
