//go:build windows

package collector

import "time"

// x/sys/windows keeps GetTickCount64 unexported, so declare our own.
var procGetTickCount64 = modKernel32.NewProc("GetTickCount64")

// collectHostNative reads uptime from GetTickCount64.
//
// On x64 this reads KUSER_SHARED_DATA without a ring transition, so it is free
// enough to call every tick.  Reading it live rather than caching a boot time at
// startup also keeps the figure honest across sleep and resume.
func collectHostNative() HostSnapshot {
	if err := procGetTickCount64.Find(); err != nil {
		return collectHostFallback()
	}
	ms, _, _ := procGetTickCount64.Call()
	if ms == 0 {
		return collectHostFallback()
	}
	up, ok := scaleDuration(uint64(ms), time.Millisecond)
	if !ok {
		return collectHostFallback()
	}
	return HostSnapshot{Uptime: up, BootTime: time.Now().Add(-up)}
}
