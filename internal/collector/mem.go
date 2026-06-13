package collector

import (
	"github.com/shirou/gopsutil/v4/mem"
)

func collectMem() (MemSnapshot, error) {
	vm, err := mem.VirtualMemory()
	if err != nil {
		return MemSnapshot{}, err
	}

	sw, err := mem.SwapMemory()
	if err != nil {
		sw = &mem.SwapMemoryStat{}
	}

	snap := MemSnapshot{
		RAMUsedBytes:    vm.Used,
		RAMTotalBytes:   vm.Total,
		RAMPct:          vm.UsedPercent,
		RAMCachedBytes:  vm.Cached,
		RAMBuffersBytes: vm.Buffers,
		SwapUsedBytes:   sw.Used,
		SwapTotalBytes:  sw.Total,
		SwapPct:         sw.UsedPercent,
	}
	augmentMemSnapshot(&snap)
	return snap, nil
}
