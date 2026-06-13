package collector

import (
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
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
}

// GPUSource indicates which backend provided GPU data.
type GPUSource int

const (
	GPUSourceNone        GPUSource = iota
	GPUSourceNvidiaSmi             // nvidia-smi subprocess
	GPUSourcePowerShell            // PowerShell Get-Counter fallback
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

// New returns a new Collector and immediately begins pre-warming Windows PDH
// CPU counters and the network baseline in the background, so the first
// visible Collect() call returns quickly.
func New() Collector {
	c := &defaultCollector{
		prevNetBytes: make(map[string][2]uint64),
	}
	go c.warmup()
	return c
}

// warmup primes gopsutil's internal PDH CPU counters and captures the initial
// network baseline.  It runs once in the background immediately after New().
func (c *defaultCollector) warmup() {
	cpu.Percent(0, true)  //nolint:errcheck
	cpu.Percent(0, false) //nolint:errcheck
	_, newBytes, newTime := collectNet(nil, time.Time{})
	c.mu.Lock()
	c.prevNetBytes = newBytes
	c.prevNetTime = newTime
	c.mu.Unlock()
}

type netResult struct {
	net      []NetSnapshot
	newBytes map[string][2]uint64
	newTime  time.Time
}

// Collect gathers all sensors in parallel.  GPU metrics are refreshed at most
// every 5 seconds (subprocess overhead); the refresh runs in the background so
// it never blocks the other collectors.  On the first call the GPU snapshot is
// empty and fills in ~3–5 s later.
func (c *defaultCollector) Collect() (Snapshot, error) {
	cpuCh := make(chan CPUSnapshot, 1)
	memCh := make(chan MemSnapshot, 1)
	netCh := make(chan netResult, 1)
	procsCh := make(chan []ProcSnapshot, 1)

	go func() { s, _ := collectCPU(); cpuCh <- s }()
	go func() { s, _ := collectMem(); memCh <- s }()
	go func() { s, _ := collectProcs(); procsCh <- s }()

	go func() {
		c.mu.Lock()
		prev, prevT := c.prevNetBytes, c.prevNetTime
		c.mu.Unlock()
		net, newBytes, newTime := collectNet(prev, prevT)
		netCh <- netResult{net, newBytes, newTime}
	}()

	// GPU: refresh every 5 s in a background goroutine so the slow subprocess
	// (PowerShell/nvidia-smi) never blocks CPU, Mem, Net, or Procs display.
	// gpuRefreshing prevents a second goroutine launching before the first
	// completes.
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

	cpuSnap := <-cpuCh
	mem := <-memCh
	nr := <-netCh
	procs := <-procsCh

	c.mu.Lock()
	c.prevNetBytes = nr.newBytes
	c.prevNetTime = nr.newTime
	c.mu.Unlock()

	return Snapshot{
		CollectedAt: time.Now(),
		CPU:         cpuSnap,
		Mem:         mem,
		GPUs:        gpus,
		Net:         nr.net,
		Procs:       procs,
	}, nil
}
