//go:build windows

package collector

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procPdhGetFormattedCounterArrayW = modPdh.NewProc("PdhGetFormattedCounterArrayW")

	modDxgi                = windows.NewLazySystemDLL("dxgi.dll")
	procCreateDXGIFactory1 = modDxgi.NewProc("CreateDXGIFactory1")
)

type LUID struct {
	LowPart  uint32
	HighPart int32
}

type dxgiAdapterDesc1 struct {
	Description           [128]uint16
	VendorId              uint32
	DeviceId              uint32
	SubSysId              uint32
	Revision              uint32
	DedicatedVideoMemory  uintptr
	DedicatedSystemMemory uintptr
	SharedSystemMemory    uintptr
	AdapterLuid           LUID
	Flags                 uint32
}

type pdhFmtCounterValueDouble struct {
	CStatus uint32
	_       uint32
	Double  float64
}

type pdhFmtCounterValueItemW struct {
	SzName   *uint16
	FmtValue pdhFmtCounterValueDouble
}

type gpuPDHQuery struct {
	query    uintptr
	hCounter uintptr
}

var (
	gpuPDHOnce sync.Once
	gpuPDHInst *gpuPDHQuery
)

func getGpuPDH() *gpuPDHQuery {
	gpuPDHOnce.Do(func() {
		var q uintptr
		if r, _, _ := procPdhOpenQuery.Call(0, 0, uintptr(unsafe.Pointer(&q))); r != 0 {
			return
		}
		path, _ := windows.UTF16PtrFromString(`\GPU Engine(*engtype_3D)\Utilization Percentage`)
		var hCounter uintptr
		if r, _, _ := procPdhAddEnglishCounterW.Call(q, uintptr(unsafe.Pointer(path)), 0, uintptr(unsafe.Pointer(&hCounter))); r != 0 {
			_, _, _ = procPdhCloseQuery.Call(q)
			return
		}
		// Prime the counter
		_, _, _ = procPdhCollectQueryData.Call(q)
		gpuPDHInst = &gpuPDHQuery{query: q, hCounter: hCounter}
	})
	return gpuPDHInst
}

// collectAllGPUs queries NVIDIA GPUs via nvidia-smi and all other GPUs via
// native DXGI/PDH APIs, running both in parallel and merging the results.
func collectAllGPUs() []GPUSnapshot {
	nvidiaCh := make(chan []GPUSnapshot, 1)
	otherCh := make(chan []GPUSnapshot, 1)
	go func() { nvidiaCh <- queryAllNvidiaSmi() }()
	go func() { otherCh <- queryNonNvidiaGPUs() }()
	return append(<-nvidiaCh, <-otherCh...)
}

// nvidia-smi query fields — order must match parseNvidiaRow below.
const nvidiaSmiQuery = "name,utilization.gpu,utilization.memory,memory.used,memory.total,temperature.gpu,power.draw,power.limit,clocks.current.graphics,clocks.current.memory,fan.speed,pstate"

// queryAllNvidiaSmi runs nvidia-smi once and returns one GPUSnapshot per GPU.
func queryAllNvidiaSmi() []GPUSnapshot {
	if _, err := exec.LookPath("nvidia-smi"); err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	//nolint:gosec // nvidia-smi is a fixed system binary located via LookPath
	out, err := exec.CommandContext(ctx, "nvidia-smi",
		"--query-gpu="+nvidiaSmiQuery,
		"--format=csv,noheader,nounits").Output()
	if err != nil {
		return nil
	}
	var snaps []GPUSnapshot
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if s, ok := parseNvidiaRow(line); ok {
			snaps = append(snaps, s)
		}
	}
	return snaps
}

func parseNvidiaRow(line string) (GPUSnapshot, bool) {
	f := strings.Split(line, ", ")
	if len(f) < 12 {
		return GPUSnapshot{}, false
	}
	snap := GPUSnapshot{
		Name:        strings.TrimSpace(f[0]),
		UtilPct:     parseFloat(f[1]),
		MemUtilPct:  parseFloat(f[2]),
		MemUsedMiB:  parseUint(f[3]),
		MemTotMiB:   parseUint(f[4]),
		TempC:       parseFloat(f[5]),
		PowerDrawW:  parseFloat(f[6]),
		PowerLimitW: parseFloat(f[7]),
		GfxClockMHz: parseUint(f[8]),
		MemClockMHz: parseUint(f[9]),
		FanPct:      parseFloat(f[10]),
		PState:      strings.TrimSpace(f[11]),
		Source:      GPUSourceNvidiaSmi,
	}
	return snap, true
}

func queryNonNvidiaGPUs() []GPUSnapshot {
	excludeNvidia := false
	if _, err := exec.LookPath("nvidia-smi"); err == nil {
		excludeNvidia = true
	}

	gpus, err := getDxgiGPUs()
	if err != nil || len(gpus) == 0 {
		return nil
	}

	utils := queryGpuUtils()
	var snaps []GPUSnapshot

	for _, g := range gpus {
		if excludeNvidia && strings.Contains(strings.ToUpper(g.Name), "NVIDIA") {
			continue
		}

		luid := g.PState // We stored the LUID string in PState temporarily
		var util float64
		if utils != nil {
			util = utils[strings.ToLower(luid)]
		}

		snaps = append(snaps, GPUSnapshot{
			Name:      g.Name,
			UtilPct:   util,
			MemTotMiB: g.MemTotMiB,
			Source:    GPUSourcePowerShell, // Keep as GPUSourcePowerShell so panels render it as fallback GPU
		})
	}

	return snaps
}

func getDxgiGPUs() ([]GPUSnapshot, error) {
	if procCreateDXGIFactory1.Find() != nil {
		return nil, fmt.Errorf("CreateDXGIFactory1 not found")
	}

	var factory unsafe.Pointer
	iidIDXGIFactory1 := windows.GUID{
		Data1: 0x770aae78,
		Data2: 0xf26f,
		Data3: 0x4dba,
		Data4: [8]byte{0xa8, 0x29, 0x25, 0x3c, 0x83, 0xd1, 0xb3, 0x87},
	}

	r, _, _ := procCreateDXGIFactory1.Call(
		uintptr(unsafe.Pointer(&iidIDXGIFactory1)),
		uintptr(unsafe.Pointer(&factory)),
	)
	if r != 0 {
		return nil, fmt.Errorf("CreateDXGIFactory1 failed: 0x%x", r)
	}
	defer releaseCOM(factory)

	var gpus []GPUSnapshot
	for i := uintptr(0); ; i++ {
		var adapter unsafe.Pointer
		// IDXGIFactory1::EnumAdapters1 is vtable index 12
		r := callVirtualMethod(factory, 12, i, uintptr(unsafe.Pointer(&adapter)))
		if r != 0 {
			break
		}

		var desc dxgiAdapterDesc1
		// IDXGIAdapter1::GetDesc1 is vtable index 11
		r = callVirtualMethod(adapter, 11, uintptr(unsafe.Pointer(&desc)))
		if r == 0 {
			// Skip software adapters (DXGI_ADAPTER_FLAG_SOFTWARE = 2)
			if desc.Flags&2 == 0 {
				gpuName := windows.UTF16ToString(desc.Description[:])
				vramBytes := uint64(desc.DedicatedVideoMemory)
				luidStr := fmt.Sprintf("luid_0x%08x_0x%08x", desc.AdapterLuid.HighPart, desc.AdapterLuid.LowPart)

				gpus = append(gpus, GPUSnapshot{
					Name:      gpuName,
					MemTotMiB: vramBytes / (1024 * 1024),
					PState:    luidStr,
				})
			}
		}
		releaseCOM(adapter)
	}
	return gpus, nil
}

func queryGpuUtils() map[string]float64 {
	pdh := getGpuPDH()
	if pdh == nil {
		return nil
	}

	if r, _, _ := procPdhCollectQueryData.Call(pdh.query); r != 0 {
		return nil
	}

	var bufSize uint32
	var itemCount uint32
	const pdhFmtDouble = 0x00000200

	r, _, _ := procPdhGetFormattedCounterArrayW.Call(
		pdh.hCounter,
		pdhFmtDouble,
		uintptr(unsafe.Pointer(&bufSize)),
		uintptr(unsafe.Pointer(&itemCount)),
		0,
	)
	if r != 0 && r != 0x800007d2 { // PDH_MORE_DATA
		return nil
	}
	if bufSize == 0 {
		return nil
	}

	buffer := make([]byte, bufSize)
	r, _, _ = procPdhGetFormattedCounterArrayW.Call(
		pdh.hCounter,
		pdhFmtDouble,
		uintptr(unsafe.Pointer(&bufSize)),
		uintptr(unsafe.Pointer(&itemCount)),
		uintptr(unsafe.Pointer(&buffer[0])),
	)
	if r != 0 {
		return nil
	}

	utils := make(map[string]float64)
	items := (*[1 << 20]pdhFmtCounterValueItemW)(unsafe.Pointer(&buffer[0]))[:itemCount:itemCount]
	for _, item := range items {
		if item.FmtValue.CStatus == 0 {
			name := strings.ToLower(windows.UTF16PtrToString(item.SzName))
			if idx := strings.Index(name, "luid_"); idx != -1 {
				if len(name) >= idx+26 {
					luid := name[idx : idx+26]
					utils[luid] += item.FmtValue.Double
				}
			}
		}
	}
	return utils
}

func callVirtualMethod(obj unsafe.Pointer, index int, args ...uintptr) uintptr {
	vtablePtr := *(*unsafe.Pointer)(obj)
	method := (*[100]uintptr)(vtablePtr)[index]

	callArgs := make([]uintptr, len(args)+1)
	callArgs[0] = uintptr(obj)
	copy(callArgs[1:], args)

	r, _, _ := syscall.SyscallN(method, callArgs...)
	return r
}

func releaseCOM(obj unsafe.Pointer) {
	if obj == nil {
		return
	}
	callVirtualMethod(obj, 2)
}

func parseFloat(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasPrefix(s, "[") {
		return 0
	}
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

func parseUint(s string) uint64 {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasPrefix(s, "[") {
		return 0
	}
	v, _ := strconv.ParseUint(s, 10, 64)
	return v
}
