//go:build !windows

package collector

// Both sensors are Windows-only.  gopsutil's disk.IOCounters is not a usable
// fallback: it reports cumulative counters per *volume*, so two volumes on one
// spindle double-count, and it returns a partial map on any unexpected error.
func collectDisksNative() []DiskSnapshot { return nil }

func collectVolumesNative() []VolumeSnapshot { return nil }
