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

var globalCPUTracker = &cpuTracker{
	buf: make([]byte, runtime.NumCPU()*int(unsafe.Sizeof(systemProcessorPerformanceInformationStruct{}))+4096),
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
		if r == uintptr(statusInfoLengthMismatch) || int(returnLength) > len(buf) {
			newSize := len(buf) * 2
			if int(returnLength) > newSize {
				newSize = int(returnLength) + 4096
			}
			buf = make([]byte, newSize)
			ct.buf = buf
			continue
		}
		return CPUSnapshot{}, syscall.Errno(r)
	}

	elemSize := int(unsafe.Sizeof(systemProcessorPerformanceInformationStruct{}))
	numCores := int(returnLength) / elemSize
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

	return CPUSnapshot{
		CorePcts: corePcts,
		AggrPct:  aggrPct,
	}, nil
}
