//go:build windows

package collector

import (
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const systemProcessorPerformanceInformation = 8

type systemProcessorPerformanceInformationStruct struct {
	IdleTime       int64
	KernelTime     int64
	UserTime       int64
	DpcTime        int64
	InterruptTime  int64
	InterruptCount uint32
	_              uint32
}

type cpuTracker struct {
	mu        sync.Mutex
	prevCores []systemProcessorPerformanceInformationStruct
	prevTime  time.Time
	buf       []byte
}

// cpuInfoSize is the size of one per-core entry.  The processor-performance class
// rejects any buffer length that is not a whole number of entries with
// STATUS_INFO_LENGTH_MISMATCH — padding the buffer "for safety" makes every call
// fail — so the buffer is always sized in multiples of this.
const cpuInfoSize = int(unsafe.Sizeof(systemProcessorPerformanceInformationStruct{}))

// maxCPUBufCores bounds the retry loop.  It is far above any real machine and
// keeps the buffer under 200 KB if the kernel ever keeps answering "mismatch".
const maxCPUBufCores = 4096

var globalCPUTracker = &cpuTracker{
	buf: make([]byte, runtime.NumCPU()*cpuInfoSize),
}

func collectCPUNative() (CPUSnapshot, error) {
	return globalCPUTracker.collect()
}

func (ct *cpuTracker) collect() (CPUSnapshot, error) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	var returnLength uint32
	buf := ct.buf

	for {
		r, _, _ := procNtQuerySystemInformation.Call(
			uintptr(systemProcessorPerformanceInformation),
			uintptr(unsafe.Pointer(&buf[0])),
			uintptr(len(buf)),
			uintptr(unsafe.Pointer(&returnLength)),
		)
		if r == 0 {
			break
		}
		if r != uintptr(statusInfoLengthMismatch) {
			return CPUSnapshot{}, syscall.Errno(r)
		}
		// Trust the kernel's size when it is a whole number of entries;
		// otherwise double the entry count.  Either way stay on a multiple of
		// cpuInfoSize, or the next call fails the same way.
		newSize := 2 * len(buf)
		if rl := int(returnLength); rl > len(buf) && rl%cpuInfoSize == 0 {
			newSize = rl
		}
		newSize = (newSize + cpuInfoSize - 1) / cpuInfoSize * cpuInfoSize
		if newSize > maxCPUBufCores*cpuInfoSize {
			return CPUSnapshot{}, syscall.Errno(r)
		}
		buf = make([]byte, newSize)
		ct.buf = buf
	}

	numCores := int(returnLength) / cpuInfoSize
	if numCores == 0 {
		return CPUSnapshot{}, nil
	}

	curCores := unsafe.Slice((*systemProcessorPerformanceInformationStruct)(unsafe.Pointer(&buf[0])), numCores)
	now := time.Now()

	corePcts := make([]float64, numCores)
	totalBusyPct := 0.0

	if len(ct.prevCores) == numCores && !ct.prevTime.IsZero() {
		for i := 0; i < numCores; i++ {
			cur := &curCores[i]
			prev := &ct.prevCores[i]

			deltaKernel := cur.KernelTime - prev.KernelTime
			deltaUser := cur.UserTime - prev.UserTime
			deltaIdle := cur.IdleTime - prev.IdleTime

			deltaTotal := deltaKernel + deltaUser
			deltaBusy := deltaTotal - deltaIdle

			if deltaTotal > 0 && deltaBusy > 0 {
				pct := (float64(deltaBusy) / float64(deltaTotal)) * 100.0
				if pct > 100.0 {
					pct = 100.0
				}
				corePcts[i] = pct
			}
			totalBusyPct += corePcts[i]
		}
	}

	// Update previous state
	if len(ct.prevCores) != numCores {
		ct.prevCores = make([]systemProcessorPerformanceInformationStruct, numCores)
	}
	copy(ct.prevCores, curCores)
	ct.prevTime = now

	aggrPct := 0.0
	if numCores > 0 {
		aggrPct = totalBusyPct / float64(numCores)
	}

	snap := CPUSnapshot{
		CorePcts: corePcts,
		AggrPct:  aggrPct,
	}
	augmentCPUClock(&snap)
	return snap, nil
}
