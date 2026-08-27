//go:build windows

package collector

import (
	"math"
	"os"
	"runtime"
	"sort"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modNtdll                     = windows.NewLazySystemDLL("ntdll.dll")
	procNtQuerySystemInformation = modNtdll.NewProc("NtQuerySystemInformation")
	procGetSystemTimes           = modKernel32.NewProc("GetSystemTimes")
)

const (
	systemProcessInformation = 5
	statusInfoLengthMismatch = 0xC0000004
)

type unicodeString struct {
	Length        uint16
	MaximumLength uint16
	_             uint32 // padding for 8-byte pointer alignment
	Buffer        *uint16
}

type systemProcessInformationStruct struct {
	NextEntryOffset              uint32
	NumberOfThreads              uint32
	WorkingSetPrivateSize        int64
	FaultCount                   uint32
	HardFaultCount               uint32
	CycleTime                    uint64
	CreateTime                   int64
	UserTime                     int64
	KernelTime                   int64
	ImageName                    unicodeString
	BasePriority                 int32
	_                            uint32
	UniqueProcessId              uintptr
	InheritedFromUniqueProcessId uintptr
	HandleCount                  uint32
	SessionId                    uint32
	UniqueProcessKey             uintptr
	PeakVirtualSize              uintptr
	VirtualSize                  uintptr
	PageFaultCount               uint32
	_                            uint32
	PeakWorkingSetSize           uintptr
	WorkingSetSize               uintptr
	QuotaPeakPagedPoolUsage      uintptr
	QuotaPagedPoolUsage          uintptr
	QuotaPeakNonPagedPoolUsage   uintptr
	QuotaNonPagedPoolUsage       uintptr
	PagefileUsage                uintptr
	PeakPagefileUsage            uintptr
	PrivatePageCount             uintptr
	ReadOperationCount           int64
	WriteOperationCount          int64
	OtherOperationCount          int64
	ReadTransferCount            int64
	WriteTransferCount           int64
	OtherTransferCount           int64
}

// procSample is the previous tick's cumulative counters for one PID.  createTime
// is carried so a recycled PID is recognised: without it the new process inherits
// the dead one's totals and reports a garbage spike on its first tick.
type procSample struct {
	totalTime  int64
	createTime int64
	readBytes  int64
	writeBytes int64
}

type procTracker struct {
	mu          sync.Mutex
	prevProcs   map[int32]procSample
	prevSysTime int64
	prevTime    time.Time
	buf         []byte
}

var globalProcTracker = &procTracker{
	prevProcs: make(map[int32]procSample, 512),
	buf:       make([]byte, 256*1024),
}

// collectProcsNative queries all system processes in a single NtQuerySystemInformation syscall.
func collectProcsNative() ([]ProcSnapshot, error) {
	return globalProcTracker.collect()
}

func (pt *procTracker) collect() ([]ProcSnapshot, error) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	var returnLength uint32
	buf := pt.buf

	for {
		r, _, _ := procNtQuerySystemInformation.Call(
			uintptr(systemProcessInformation),
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
			pt.buf = buf
			continue
		}
		return nil, syscall.Errno(r)
	}

	// Read total physical memory for MemPct calculation
	var ms memoryStatusEx
	ms.Length = uint32(unsafe.Sizeof(ms))
	totalPhys := uint64(1)
	if r, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&ms))); r != 0 && ms.TotalPhys > 0 {
		totalPhys = ms.TotalPhys
	}

	// Read system total CPU times (Idle + Kernel + User across all cores)
	now := time.Now()
	var sysIdle, sysKernel, sysUser int64
	_, _, _ = procGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&sysIdle)),
		uintptr(unsafe.Pointer(&sysKernel)),
		uintptr(unsafe.Pointer(&sysUser)),
	)
	curSysTime := sysKernel + sysUser // In Windows, Kernel includes Idle
	deltaSysTime := curSysTime - pt.prevSysTime
	if pt.prevSysTime == 0 || deltaSysTime <= 0 {
		// Fallback: estimate from elapsed wall time and CPU core count
		elapsed := now.Sub(pt.prevTime).Seconds()
		if elapsed <= 0 {
			elapsed = 1.0
		}
		deltaSysTime = int64(elapsed * float64(runtime.NumCPU()) * 10_000_000)
	}

	// I/O is a wall-clock rate, unlike CPU% which is a share of busy CPU time.
	// On the first tick there is no baseline; leave the rates at zero rather than
	// inventing a denominator, which would produce a plausible-looking lie.
	ioElapsed := 0.0
	if !pt.prevTime.IsZero() {
		ioElapsed = now.Sub(pt.prevTime).Seconds()
	}

	newProcs := make(map[int32]procSample, len(pt.prevProcs)+64)
	rawProcs := make([]ProcSnapshot, 0, 512)

	offset := uint32(0)
	for {
		procInfo := (*systemProcessInformationStruct)(unsafe.Pointer(&buf[offset]))
		//nolint:gosec // G115: Windows process IDs fit in positive int32
		pid := int32(procInfo.UniqueProcessId & 0x7FFFFFFF)
		//nolint:gosec // G115: Windows process IDs fit in positive int32
		ppid := int32(procInfo.InheritedFromUniqueProcessId & 0x7FFFFFFF)
		curTotalProcTime := procInfo.UserTime + procInfo.KernelTime

		cur := procSample{
			totalTime:  curTotalProcTime,
			createTime: procInfo.CreateTime,
			readBytes:  procInfo.ReadTransferCount,
			writeBytes: procInfo.WriteTransferCount,
		}
		newProcs[pid] = cur

		var name string
		if procInfo.ImageName.Length > 0 && procInfo.ImageName.Buffer != nil {
			name = windows.UTF16ToString(unsafe.Slice(procInfo.ImageName.Buffer, procInfo.ImageName.Length/2))
		} else if pid == 0 {
			name = "System Idle Process"
		} else if pid == 4 {
			name = "System"
		}

		var cpuPct, readBps, writeBps float64
		// Only compare against the previous tick when it described this same
		// process; a recycled PID has a different create time.
		if prev, ok := pt.prevProcs[pid]; ok && prev.createTime == cur.createTime {
			if deltaSysTime > 0 {
				deltaProc := curTotalProcTime - prev.totalTime
				if deltaProc > 0 {
					cpuPct = (float64(deltaProc) / float64(deltaSysTime)) * 100.0
				}
			}
			readBps = ioRate(cur.readBytes, prev.readBytes, ioElapsed)
			writeBps = ioRate(cur.writeBytes, prev.writeBytes, ioElapsed)
		}

		rss := uint64(procInfo.WorkingSetSize)
		memMB := float64(rss) / (1024 * 1024)
		memPct := float32((float64(rss) / float64(totalPhys)) * 100.0)

		rawProcs = append(rawProcs, ProcSnapshot{
			PID:      pid,
			PPID:     ppid,
			Name:     name,
			CPUPct:   cpuPct,
			MemPct:   memPct,
			MemMB:    memMB,
			ReadBps:  readBps,
			WriteBps: writeBps,
		})

		if procInfo.NextEntryOffset == 0 {
			break
		}
		offset += procInfo.NextEntryOffset
	}

	pt.prevProcs = newProcs
	pt.prevSysTime = curSysTime
	pt.prevTime = now

	// Build parent and name maps from all enumerated processes
	ppidOf := make(map[int32]int32, len(rawProcs))
	nameOf := make(map[int32]string, len(rawProcs))
	for _, s := range rawProcs {
		ppidOf[s.PID] = s.PPID
		nameOf[s.PID] = s.Name
	}

	var chain map[int32]struct{}
	if self := os.Getpid(); self > 0 && self <= math.MaxInt32 {
		chain = selfChainPIDs(int32(self), ppidOf, nameOf)
	}

	snaps := make([]ProcSnapshot, 0, len(rawProcs))
	for _, s := range rawProcs {
		if s.Name == "" {
			continue
		}
		if _, ok := chain[s.PID]; ok {
			s.Self = true
		}
		snaps = append(snaps, s)
	}

	// Sort by CPU% descending
	sort.Slice(snaps, func(i, j int) bool {
		return snaps[i].CPUPct > snaps[j].CPUPct
	})

	return snaps, nil
}
