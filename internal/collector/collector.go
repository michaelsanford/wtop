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
}

// CPUSnapshot holds per-core and aggregate CPU utilisation (0–100).
type CPUSnapshot struct {
	CorePcts []float64
	AggrPct  float64
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
	FanPct      float64 // fan speed, 0–100 % (0 = passive/unavailable)
	PState      string  // performance state, e.g. "P0", "P2"
	Source      GPUSource
}

// NetSnapshot holds I/O rate data for one network interface.
type NetSnapshot struct {
	Name            string
	BytesSentPerSec float64
	BytesRecvPerSec float64
}

// ProcSnapshot holds per-process resource usage.
type ProcSnapshot struct {
	PID    int32
	PPID   int32
	Name   string
	CPUPct float64
	MemPct float32
	MemMB  float64
	// Self marks wtop's own process and every ancestor up to the root — i.e. the
	// shell and terminal session hosting it.  Those entries are pinned into the
	// snapshot even when they fall outside the top-CPU cut.
	Self bool
}

// Collector gathers a Snapshot. Implementations are safe for concurrent use.
type Collector interface {
	Collect() (Snapshot, error)
}

type defaultCollector struct {
	mu           sync.Mutex
	prevNetBytes map[string][2]uint64 // iface → [sent, recv]
	prevNetTime  time.Time

	gpuLastQueried time.Time
	gpuCache       []GPUSnapshot
	gpuRefreshing  bool // true while a background GPU query is in flight
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

// Collect gathers all sensors in parallel. GPU metrics are refreshed in the
// background every 5 seconds so slow queries never block the UI.
func (c *defaultCollector) Collect() (Snapshot, error) {
	var (
		cpuSnap  CPUSnapshot
		memSnap  MemSnapshot
		netSnap  []NetSnapshot
		newBytes map[string][2]uint64
		newTime  time.Time
		procs    []ProcSnapshot
		wg       sync.WaitGroup
	)

	wg.Add(4)
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

	c.mu.Lock()
	stale := time.Since(c.gpuLastQueried) > 5*time.Second
	gpus := c.gpuCache
	if stale && !c.gpuRefreshing {
		c.gpuRefreshing = true
		go func() {
			g := collectAllGPUs()
			c.mu.Lock()
			c.gpuCache = g
			c.gpuLastQueried = time.Now()
			c.gpuRefreshing = false
			c.mu.Unlock()
		}()
	}
	c.mu.Unlock()

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
	}, nil
}
