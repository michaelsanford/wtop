//go:build windows

package collector

import (
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// CallNtPowerInformation InformationLevel for the per-CPU clock table.
const processorInformation = 11

// processorPowerInformation mirrors PROCESSOR_POWER_INFORMATION, one entry per
// logical CPU.
type processorPowerInformation struct {
	Number           uint32
	MaxMhz           uint32
	CurrentMhz       uint32
	MhzLimit         uint32
	MaxIdleState     uint32
	CurrentIdleState uint32
}

var (
	baseMHzOnce sync.Once
	baseMHzVal  uint64
)

// baseMHz returns the CPU's nominal clock.  It never changes at runtime, so it
// is resolved once.
//
// CurrentMhz from the same table is deliberately ignored for the live figure:
// on modern Windows it reflects the requested P-state and routinely reads back
// as the base clock even while the part is boosting.  The live number comes from
// the PDH performance counter instead.
func baseMHz() uint64 {
	baseMHzOnce.Do(func() {
		if v := baseMHzFromPowerInfo(); v > 0 {
			baseMHzVal = v
			return
		}
		baseMHzVal = baseMHzFromRegistry()
	})
	return baseMHzVal
}

func baseMHzFromPowerInfo() uint64 {
	if err := procCallNtPowerInformation.Find(); err != nil {
		return 0
	}
	n := runtime.NumCPU()
	if n <= 0 {
		return 0
	}
	buf := make([]processorPowerInformation, n)
	size := unsafe.Sizeof(buf[0]) * uintptr(n)
	r, _, _ := procCallNtPowerInformation.Call(
		uintptr(processorInformation),
		0, 0,
		uintptr(unsafe.Pointer(&buf[0])),
		size,
	)
	if r != 0 { // STATUS_SUCCESS
		return 0
	}
	return uint64(buf[0].MaxMhz)
}

// baseMHzFromRegistry is the fallback for machines where the power API declines.
// x/sys/windows/registry ships inside the x/sys module already in go.mod, so
// this costs no new dependency.
func baseMHzFromRegistry() uint64 {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`HARDWARE\DESCRIPTION\System\CentralProcessor\0`, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return 0
	}
	defer func() { _ = k.Close() }()
	v, _, err := k.GetIntegerValue("~MHz")
	if err != nil {
		return 0
	}
	return v
}

// cpuPerfPDH is a persistent PDH query for the effective-clock counter.
//
// It is deliberately separate from the memory query in mem_windows.go: the two
// are read from different goroutines and have unrelated lifetimes, and sharing
// one PDH query handle across them would serialise the collection fan-out.
type cpuPerfPDH struct {
	query uintptr
	hPerf uintptr
}

var (
	cpuPerfOnce sync.Once
	cpuPerfInst *cpuPerfPDH
)

func getCPUPerfPDH() *cpuPerfPDH {
	cpuPerfOnce.Do(func() {
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
		h := add(`\Processor Information(_Total)\% Processor Performance`)
		if h == 0 {
			_, _, _ = procPdhCloseQuery.Call(q)
			return
		}
		// "% Processor Performance" is a rate counter: it needs two samples before
		// it will format, so prime it here rather than losing the first tick.
		_, _, _ = procPdhCollectQueryData.Call(q)
		cpuPerfInst = &cpuPerfPDH{query: q, hPerf: h}
	})
	return cpuPerfInst
}

// cpuPerfPct reads "% Processor Performance", or 0 when unavailable.
func cpuPerfPct() float64 {
	pdh := getCPUPerfPDH()
	if pdh == nil {
		return 0
	}
	if r, _, _ := procPdhCollectQueryData.Call(pdh.query); r != 0 {
		return 0
	}
	const pdhFmtDouble = 0x00000200
	var v pdhFmtCounterValueDouble
	if r, _, _ := procPdhGetFormattedCounterValue.Call(pdh.hPerf, pdhFmtDouble, 0,
		uintptr(unsafe.Pointer(&v))); r != 0 {
		return 0
	}
	return v.Double
}

// augmentCPUClock fills in the clock fields.  Best-effort: on failure the fields
// stay zero and the UI omits them.
func augmentCPUClock(snap *CPUSnapshot) {
	base := baseMHz()
	pct := cpuPerfPct()
	snap.BaseMHz = base
	snap.PerfPct = pct
	snap.EffectiveMHz = effectiveMHz(base, pct)
}
