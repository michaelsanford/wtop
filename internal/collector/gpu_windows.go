//go:build windows

package collector

import (
	"context"
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// collectAllGPUs queries NVIDIA GPUs via nvidia-smi and all other GPUs via a
// single PowerShell command, running both in parallel and merging the results.
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

// psGPUEntry is the JSON shape returned by queryNonNvidiaGPUs.
type psGPUEntry struct {
	Name    string  `json:"name"`
	VRAMMiB int     `json:"vramMiB"`
	UtilPct float64 `json:"utilPct"`
}

// nonNvidiaPS is the PowerShell script that enumerates non-NVIDIA GPUs.
// It maps adapter LUIDs (from the display-class registry key) to adapter names
// from WMI, sums 3D-engine utilization per LUID from PDH counters (with retry
// to handle Windows PDH warm-up delay), and outputs a JSON array.
//
//nolint:gosec // powershell.exe is a fixed system binary
const nonNvidiaPS = `
$ErrorActionPreference='SilentlyContinue'

$ln=@{}
Get-ChildItem 'HKLM:\SYSTEM\CurrentControlSet\Control\Class\{4d36e968-e325-11ce-bfc1-08002be10318}' |
  ForEach-Object {
    $p=Get-ItemProperty $_.PSPath
    if($p.DriverDesc -and $p.AdapterLUID){
      $b=[byte[]]$p.AdapterLUID
      if($b.Length -ge 8){
        $lo=[BitConverter]::ToUInt32($b,0); $hi=[BitConverter]::ToUInt32($b,4)
        $ln["luid_0x$([string]::Format('{0:x8}',$hi))_0x$([string]::Format('{0:x8}',$lo))"]=$p.DriverDesc
      }
    }
  }

$vm=@{}
Get-CimInstance Win32_VideoController |
  Where-Object {$_.Name -notmatch 'NVIDIA'} |
  ForEach-Object { $vm[$_.Name]=[math]::Round([long]$_.AdapterRAM/1MB) }

if($vm.Count -eq 0){'[]';exit}

$um=@{}
for($i=0;$i -lt 3;$i++){
  $s=(Get-Counter '\GPU Engine(*engtype_3D)\Utilization Percentage').CounterSamples
  if($s){
    $s | ForEach-Object {
      if($_.InstanceName -match 'luid_(0x[0-9a-f]+_0x[0-9a-f]+)'){
        $k='luid_'+$Matches[1]
        $um[$k]=($um[$k] -as [double])+$_.CookedValue
      }
    }
    break
  }
  Start-Sleep -Milliseconds 150
}

$r=foreach($name in $vm.Keys){
  $luid=($ln.GetEnumerator() | Where-Object {$_.Value -eq $name} | Select-Object -First 1).Key
  $util=if($luid){[math]::Round(($um[$luid] -as [double]),1)}else{0.0}
  [PSCustomObject]@{name=$name;vramMiB=[int]$vm[$name];utilPct=$util}
}
if(-not $r){'[]';exit}
ConvertTo-Json -InputObject @($r) -Compress
`

func queryNonNvidiaGPUs() []GPUSnapshot {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	//nolint:gosec // powershell.exe is a fixed system binary
	out, err := exec.CommandContext(ctx, "powershell.exe",
		"-NoProfile", "-NonInteractive", "-Command", nonNvidiaPS).Output()
	if err != nil {
		return nil
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" || raw == "[]" {
		return nil
	}
	var entries []psGPUEntry
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return nil
	}
	snaps := make([]GPUSnapshot, 0, len(entries))
	for _, e := range entries {
		snaps = append(snaps, GPUSnapshot{
			Name:       e.Name,
			MemTotMiB:  uint64(e.VRAMMiB),
			UtilPct:    e.UtilPct,
			Source:     GPUSourcePowerShell,
		})
	}
	return snaps
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
