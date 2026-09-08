//go:build windows

package collector

import (
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// diskPDH is a persistent PDH query over the \PhysicalDisk(*) counter set.
//
// PDH is used rather than gopsutil's disk.IOCounters for three reasons: PDH
// computes the rate itself, so no cumulative-counter state has to be carried
// across ticks in defaultCollector; the cost is one collect call regardless of
// how many disks there are, instead of three syscalls per volume; and "% Idle
// Time", which is what a busy bar actually wants, has no gopsutil equivalent.
type diskPDH struct {
	query  uintptr
	hRead  uintptr
	hWrite uintptr
	hIdle  uintptr
}

var (
	diskPDHOnce sync.Once
	diskPDHInst *diskPDH
)

func getDiskPDH() *diskPDH {
	diskPDHOnce.Do(func() {
		var q uintptr
		if r, _, _ := procPdhOpenQuery.Call(0, 0, uintptr(unsafe.Pointer(&q))); r != 0 {
			return
		}
		add := func(path string) uintptr {
			var h uintptr
			p, err := windows.UTF16PtrFromString(path)
			if err != nil {
				return 0
			}
			if r, _, _ := procPdhAddEnglishCounterW.Call(q, uintptr(unsafe.Pointer(p)), 0,
				uintptr(unsafe.Pointer(&h))); r != 0 {
				return 0
			}
			return h
		}
		d := &diskPDH{
			query:  q,
			hRead:  add(`\PhysicalDisk(*)\Disk Read Bytes/sec`),
			hWrite: add(`\PhysicalDisk(*)\Disk Write Bytes/sec`),
			hIdle:  add(`\PhysicalDisk(*)\% Idle Time`),
		}
		if d.hRead == 0 || d.hWrite == 0 || d.hIdle == 0 {
			_, _, _ = procPdhCloseQuery.Call(q)
			return
		}
		// These are rate counters: a single sample formats as PDH_INVALID_DATA, so
		// prime the query here rather than losing the first tick.
		_, _, _ = procPdhCollectQueryData.Call(q)
		diskPDHInst = d
	})
	return diskPDHInst
}

// readCounterArray reads one wildcard counter into instance-name -> value.
func readCounterArray(h uintptr) map[string]float64 {
	const pdhFmtDouble = 0x00000200
	const pdhMoreData = 0x800007d2

	var bufSize, itemCount uint32
	r, _, _ := procPdhGetFormattedCounterArrayW.Call(h, pdhFmtDouble,
		uintptr(unsafe.Pointer(&bufSize)), uintptr(unsafe.Pointer(&itemCount)), 0)
	if (r != 0 && r != pdhMoreData) || bufSize == 0 {
		return nil
	}

	buf := make([]byte, bufSize)
	r, _, _ = procPdhGetFormattedCounterArrayW.Call(h, pdhFmtDouble,
		uintptr(unsafe.Pointer(&bufSize)), uintptr(unsafe.Pointer(&itemCount)),
		uintptr(unsafe.Pointer(&buf[0])))
	if r != 0 || itemCount == 0 {
		return nil
	}

	out := make(map[string]float64, itemCount)
	items := (*[1 << 20]pdhFmtCounterValueItemW)(unsafe.Pointer(&buf[0]))[:itemCount:itemCount]
	for _, item := range items {
		if item.FmtValue.CStatus != 0 {
			continue
		}
		// UTF16PtrToString copies, so the name outlives buf.
		out[windows.UTF16PtrToString(item.SzName)] = item.FmtValue.Double
	}
	return out
}

// collectDisksNative returns per-physical-disk throughput and busy time.
//
// No error return: an unavailable counter set is indistinguishable from a
// machine with no disks as far as the UI is concerned, and unparam would reject
// an error that is always nil.
func collectDisksNative() []DiskSnapshot {
	pdh := getDiskPDH()
	if pdh == nil {
		return nil
	}
	if r, _, _ := procPdhCollectQueryData.Call(pdh.query); r != 0 {
		return nil
	}

	reads := readCounterArray(pdh.hRead)
	if reads == nil {
		return nil
	}
	writes := readCounterArray(pdh.hWrite)
	idles := readCounterArray(pdh.hIdle)

	out := make([]DiskSnapshot, 0, len(reads))
	for inst, rd := range reads {
		idx, vols, ok := parsePhysicalDiskInstance(inst)
		if !ok || idx == totalDiskIndex {
			continue // the _Total row is an aggregate, not a device
		}
		busyPct := 0.0
		if idles != nil {
			if idleVal, ok := idles[inst]; ok {
				busyPct = diskBusyPct(idleVal)
			}
		}
		out = append(out, DiskSnapshot{
			Instance:         inst,
			Index:            idx,
			Volumes:          vols,
			ReadBytesPerSec:  rd,
			WriteBytesPerSec: writes[inst],
			BusyPct:          busyPct,
		})
	}
	return out
}

// collectVolumesNative returns capacity for every fixed volume.
//
// GetLogicalDriveStrings plus GetDriveType is used rather than gopsutil's
// disk.Partitions, which enumerates and stats removable and optical devices too
// and can block on empty card readers.
func collectVolumesNative() []VolumeSnapshot {
	n, err := windows.GetLogicalDriveStrings(0, nil)
	if err != nil || n == 0 {
		return nil
	}
	bufU16 := make([]uint16, n+1)
	if _, err := windows.GetLogicalDriveStrings(n, &bufU16[0]); err != nil {
		return nil
	}

	var out []VolumeSnapshot
	for _, root := range splitNulSeparated(bufU16) {
		rootPtr, err := windows.UTF16PtrFromString(root)
		if err != nil {
			continue
		}
		if windows.GetDriveType(rootPtr) != windows.DRIVE_FIXED {
			continue
		}
		var free, total, totalFree uint64
		if err := windows.GetDiskFreeSpaceEx(rootPtr, &free, &total, &totalFree); err != nil {
			continue
		}
		if total == 0 {
			continue
		}
		used := total - totalFree
		out = append(out, VolumeSnapshot{
			Mount:      strings.TrimSuffix(strings.ToUpper(root), `\`),
			TotalBytes: total,
			FreeBytes:  totalFree,
			UsedBytes:  used,
			UsedPct:    usedPct(used, total),
		})
	}
	return out
}

// splitNulSeparated splits the NUL-separated, double-NUL-terminated string list
// that GetLogicalDriveStrings returns.
func splitNulSeparated(b []uint16) []string {
	var out []string
	start := 0
	for i, c := range b {
		if c != 0 {
			continue
		}
		if i > start {
			out = append(out, windows.UTF16ToString(b[start:i]))
		}
		start = i + 1
		if i+1 < len(b) && b[i+1] == 0 {
			break
		}
	}
	return out
}
