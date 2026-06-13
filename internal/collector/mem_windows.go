//go:build windows

package collector

import (
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modKernel32                 = windows.NewLazySystemDLL("kernel32.dll")
	procGlobalMemoryStatusEx    = modKernel32.NewProc("GlobalMemoryStatusEx")

	modPdh                          = windows.NewLazySystemDLL("pdh.dll")
	procPdhOpenQuery                = modPdh.NewProc("PdhOpenQuery")
	procPdhAddEnglishCounterW       = modPdh.NewProc("PdhAddEnglishCounterW")
	procPdhCollectQueryData         = modPdh.NewProc("PdhCollectQueryData")
	procPdhGetFormattedCounterValue = modPdh.NewProc("PdhGetFormattedCounterValue")
	procPdhCloseQuery               = modPdh.NewProc("PdhCloseQuery")
)

// memoryStatusEx mirrors MEMORYSTATUSEX from kernel32.
type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

const pdhFmtLarge = 0x00000400

// pdhFmtCounterValue mirrors PDH_FMT_COUNTERVALUE for the LARGE (int64) union member.
type pdhFmtCounterValue struct {
	CStatus uint32
	_       uint32 // alignment padding to match the C union
	Large   int64
}

type memPDHQuery struct {
	query    uintptr
	hMod     uintptr
	hStandby uintptr
	hFree    uintptr
}

var (
	memPDHOnce sync.Once
	memPDHInst *memPDHQuery
)

func getMemPDH() *memPDHQuery {
	memPDHOnce.Do(func() {
		var q uintptr
		if r, _, _ := procPdhOpenQuery.Call(0, 0, uintptr(unsafe.Pointer(&q))); r != 0 {
			return
		}
		m := &memPDHQuery{query: q}

		add := func(path string) uintptr {
			p, _ := windows.UTF16PtrFromString(path)
			var h uintptr
			procPdhAddEnglishCounterW.Call(q, uintptr(unsafe.Pointer(p)), 0, uintptr(unsafe.Pointer(&h)))
			return h
		}
		m.hMod = add(`\Memory\Modified Page List Bytes`)
		m.hStandby = add(`\Memory\Cache Bytes`)
		m.hFree = add(`\Memory\Free & Zero Page List Bytes`)
		memPDHInst = m
	})
	return memPDHInst
}

func augmentMemSnapshot(s *MemSnapshot) {
	// Committed / commit limit via GlobalMemoryStatusEx.
	var ms memoryStatusEx
	ms.Length = uint32(unsafe.Sizeof(ms))
	if r, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&ms))); r != 0 {
		s.CommitLimitBytes = ms.TotalPageFile
		s.CommittedBytes = ms.TotalPageFile - ms.AvailPageFile
	}

	// Memory composition via persistent PDH counters.
	pdh := getMemPDH()
	if pdh == nil {
		return
	}
	if r, _, _ := procPdhCollectQueryData.Call(pdh.query); r != 0 {
		return
	}

	readLarge := func(h uintptr) uint64 {
		var v pdhFmtCounterValue
		r, _, _ := procPdhGetFormattedCounterValue.Call(h, pdhFmtLarge, 0, uintptr(unsafe.Pointer(&v)))
		if r != 0 || v.CStatus != 0 || v.Large < 0 {
			return 0
		}
		return uint64(v.Large)
	}

	s.ModifiedBytes = readLarge(pdh.hMod)
	s.StandbyBytes = readLarge(pdh.hStandby)
	s.FreeBytes = readLarge(pdh.hFree)
}
