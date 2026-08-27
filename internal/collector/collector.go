package collector

import (
	"sync"
	"time"
)

// Snapshot is an immutable point-in-time capture of all system metrics.
type Snapshot struct {
	CollectedAt time.Time
	CPU         CPUSnapshot
	Mem         MemSnapshot
	GPUs        []GPUSnapshot
	Net         []NetSnapshot
	Procs       []ProcSnapshot
	Disks       []DiskSnapshot
	Volumes     []VolumeSnapshot
	Host        HostSnapshot
	Power       PowerSnapshot
}

// CPUSnapshot holds per-core and aggregate CPU utilisation (0–100).
type CPUSnapshot struct {
	CorePcts []float64
	AggrPct  float64

	// BaseMHz is the CPU's nominal clock; PerfPct is "% Processor Performance",
	// which exceeds 100 under turbo.  EffectiveMHz is the product — the clock the
	// machine is actually running at.  All three are zero when unavailable.
	BaseMHz      uint64
	PerfPct      float64
	EffectiveMHz float64
}

// MemSnapshot holds RAM and swap utilisation.
type MemSnapshot struct {
	RAMUsedBytes    uint64
	RAMTotalBytes   uint64
	RAMPct          float64
	RAMCachedBytes  uint64 // standby/cached pages (Windows) or page cache (Linux)
	RAMBuffersBytes uint64 // kernel buffers (Linux); typically 0 on Windows
	SwapUsedBytes   uint64
	SwapTotalBytes  uint64
	SwapPct         float64

	// Windows memory composition (zero on non-Windows).
	CommittedBytes   uint64 // virtual memory committed (RAM + page file in use)
	CommitLimitBytes uint64 // commit ceiling (physical RAM + page file total)
	ModifiedBytes    uint64 // modified page list — dirty pages awaiting write
	StandbyBytes     uint64 // standby file cache (\Memory\Cache Bytes)
	FreeBytes        uint64 // free + zero page list
}

// GPUSource indicates which backend provided GPU data.
type GPUSource int

const (
	GPUSourceNone       GPUSource = iota
	GPUSourceNvidiaSmi            // nvidia-smi / NVML
	GPUSourcePowerShell           // PowerShell / DXGI fallback
)

// GPUSnapshot holds best-effort GPU utilisation.
type GPUSnapshot struct {
	Name        string
	UtilPct     float64 // 3D/Compute engine utilisation, 0–100 %
	MemUtilPct  float64 // memory bus utilisation, 0–100 %
	MemUsedMiB  uint64
	MemTotMiB   uint64
	TempC       float64 // core temperature, °C (0 = unavailable)
	PowerDrawW  float64 // current power draw, W
	PowerLimitW float64 // configured TDP limit, W
	GfxClockMHz uint64  // current graphics clock, MHz
	MemClockMHz uint64  // current memory clock, MHz
	FanPct      float64 // fan speed, 0–100 %
	// FanKnown distinguishes "the fan is stopped" from "the driver will not say".
	// Laptop dGPUs are commonly driver-controlled and report no fan at all, where
	// rendering a confident "passive" would be wrong.
	FanKnown bool
	PState   string // performance state, e.g. "P0", "P2"
	Source   GPUSource
}

// NetSnapshot holds I/O rate data for one network interface.
type NetSnapshot struct {
	Name            string
	BytesSentPerSec float64
	BytesRecvPerSec float64
}

// DiskSnapshot holds throughput and busy time for one physical disk.
//
// There is no temperature field.  A spike established that
// IOCTL_STORAGE_QUERY_PROPERTY / StorageDeviceTemperatureProperty returns
// ERROR_INVALID_FUNCTION on a current Samsung NVMe even though the handle opens
// unelevated and StorageDeviceProperty on the same handle returns data — the
// storage driver simply does not implement it.  WMI SMART needs administrator,
// so there is no portable unprivileged source for drive temperature.
type DiskSnapshot struct {
	// Instance is the raw PDH instance name, e.g. "0 c: d:".
	Instance string
	// Index is the physical drive number, matching \\.\PhysicalDriveN.  -1 marks
	// the synthetic "_Total" row.
	Index            int
	Volumes          []string // drive letters backed by this disk, e.g. "C:"
	ReadBytesPerSec  float64
	WriteBytesPerSec float64
	BusyPct          float64
}

// VolumeSnapshot holds capacity for one mounted fixed volume.
//
// Capacity is deliberately separate from DiskSnapshot: it changes over minutes
// rather than seconds so it lives on a slow cache, and the disk-to-volume
// relation is many-to-many, so folding it into a physical disk would force a
// meaningless sum.
type VolumeSnapshot struct {
	Mount      string // "C:"
	TotalBytes uint64
	FreeBytes  uint64
	UsedBytes  uint64
	UsedPct    float64
}

// ProcSnapshot holds per-process resource usage.
type ProcSnapshot struct {
	PID    int32
	PPID   int32
	Name   string
	CPUPct float64
	MemPct float32
	MemMB  float64
	// ReadBps and WriteBps are disk+file transfer rates in bytes per second,
	// derived from the cumulative per-process transfer counters.  They cover all
	// I/O the process performs, not just physical disk, which is what Windows
	// itself reports.  Zero on the portable fallback path — see collectProcsFallback.
	ReadBps  float64
	WriteBps float64
	// Self marks wtop's own process and every ancestor up to the root — i.e. the
	// shell and terminal session hosting it.  Those entries are pinned into the
	// snapshot even when they fall outside the top-CPU cut.
	Self bool
}

// Collector gathers a Snapshot. Implementations are safe for concurrent use.
type Collector interface {
	Collect() (Snapshot, error)
}

// Refresh intervals for the sensors that are too slow for the per-tick path.
const (
	gpuTTL = 5 * time.Second
	// Capacity moves over minutes, not seconds; polling every tick would mean a
	// GetDiskFreeSpaceEx per volume per second forever for a number that barely
	// changes.
	volumeTTL = 30 * time.Second
)

type defaultCollector struct {
	mu           sync.Mutex
	prevNetBytes map[string][2]uint64 // iface → [sent, recv]
	prevNetTime  time.Time

	gpus    cachedSensor[[]GPUSnapshot]
	volumes cachedSensor[[]VolumeSnapshot]
}

// New returns a new Collector and immediately begins pre-warming counters
// and the network baseline in the background, so the first
// visible Collect() call returns quickly.
func New() Collector {
	c := &defaultCollector{
		prevNetBytes: make(map[string][2]uint64),
	}
	go c.warmup()
	return c
}

// warmup primes CPU counters and captures the initial network baseline.
// It runs once in the background immediately after New().
func (c *defaultCollector) warmup() {
	_, _ = collectCPU()
	_, newBytes, newTime := collectNet(nil, time.Time{})
	c.mu.Lock()
	c.prevNetBytes = newBytes
	c.prevNetTime = newTime
	c.mu.Unlock()
}

// Collect gathers all sensors in parallel.
//
// Six cheap sensors are fanned out and joined each tick.  The two slow ones —
// GPU and volume capacity — are served from cachedSensor instead, so a
// subprocess or a per-volume stat can never stall the UI; see cache.go.  The
// rule for adding a sensor is that it joins the WaitGroup only if its worst case
// is bounded and well under a millisecond; anything IOCTL-, WMI- or
// subprocess-shaped belongs on the cache.
func (c *defaultCollector) Collect() (Snapshot, error) {
	var (
		cpuSnap  CPUSnapshot
		memSnap  MemSnapshot
		netSnap  []NetSnapshot
		newBytes map[string][2]uint64
		newTime  time.Time
		procs    []ProcSnapshot
		disks    []DiskSnapshot
		hostSnap HostSnapshot
		powSnap  PowerSnapshot
		wg       sync.WaitGroup
	)

	wg.Add(6)
	go func() {
		defer wg.Done()
		cpuSnap, _ = collectCPU()
	}()
	go func() {
		defer wg.Done()
		memSnap, _ = collectMem()
	}()
	go func() {
		defer wg.Done()
		c.mu.Lock()
		prev, prevT := c.prevNetBytes, c.prevNetTime
		c.mu.Unlock()
		netSnap, newBytes, newTime = collectNet(prev, prevT)
	}()
	go func() {
		defer wg.Done()
		procs, _ = collectProcs()
	}()
	go func() {
		defer wg.Done()
		disks = collectDisks()
	}()
	// Uptime and power are two cheap cached-state reads; they share a goroutine
	// rather than each paying for their own just to save a couple of microseconds.
	go func() {
		defer wg.Done()
		hostSnap = collectHost()
		powSnap = collectPower()
	}()

	gpus := c.gpus.get(gpuTTL, collectAllGPUs)
	volumes := c.volumes.get(volumeTTL, collectVolumes)

	wg.Wait()

	c.mu.Lock()
	c.prevNetBytes = newBytes
	c.prevNetTime = newTime
	c.mu.Unlock()

	return Snapshot{
		CollectedAt: time.Now(),
		CPU:         cpuSnap,
		Mem:         memSnap,
		GPUs:        gpus,
		Net:         netSnap,
		Procs:       procs,
		Disks:       disks,
		Volumes:     reconcileVolumes(disks, volumes),
		Host:        hostSnap,
		Power:       powSnap,
	}, nil
}
