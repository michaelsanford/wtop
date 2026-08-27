package collector

import (
	"strconv"
	"strings"
)

// totalDiskIndex marks the synthetic "_Total" row PDH reports alongside the real
// spindles.
const totalDiskIndex = -1

func collectDisks() []DiskSnapshot     { return collectDisksNative() }
func collectVolumes() []VolumeSnapshot { return collectVolumesNative() }

// parsePhysicalDiskInstance splits a PDH \PhysicalDisk instance name.
//
// Instance names are the drive number followed by every volume mounted on that
// spindle — "0 c:", "1 p:", "0 c: d:" — or the literal "_Total".  A data disk
// with no mounted volume is just its number.  The leading number is also the N
// in \\.\PhysicalDriveN, which makes it the join key to anything addressed that
// way.
func parsePhysicalDiskInstance(inst string) (index int, volumes []string, ok bool) {
	inst = strings.TrimSpace(inst)
	if inst == "" {
		return 0, nil, false
	}
	if strings.EqualFold(inst, "_Total") {
		return totalDiskIndex, nil, true
	}

	fields := strings.Fields(inst)
	n, err := strconv.Atoi(fields[0])
	if err != nil || n < 0 {
		return 0, nil, false
	}

	for _, f := range fields[1:] {
		if len(f) == 2 && f[1] == ':' {
			volumes = append(volumes, strings.ToUpper(f))
		}
	}
	return n, volumes, true
}

// diskBusyPct converts the "% Idle Time" counter into a busy percentage.  Rate
// counters occasionally format slightly outside 0-100, so the result is clamped.
func diskBusyPct(idlePct float64) float64 {
	busy := 100 - idlePct
	if busy < 0 {
		return 0
	}
	if busy > 100 {
		return 100
	}
	return busy
}

// reconcileVolumes drops volumes that no physical disk claims.
//
// Some software presents a virtual filesystem as a fixed NTFS/FAT volume: Google
// Drive's mount reports DRIVE_FIXED and mirrors the system drive's exact byte
// size, so filtering on drive type alone shows a convincing duplicate of C: that
// is not a disk at all.  Anything real is backed by a \PhysicalDisk instance, so
// that set is the authority.
//
// When the disk list is empty — PDH unavailable, or a platform with no
// implementation — there is nothing to reconcile against and every volume is
// kept rather than silently hiding them all.
func reconcileVolumes(disks []DiskSnapshot, vols []VolumeSnapshot) []VolumeSnapshot {
	if len(disks) == 0 || len(vols) == 0 {
		return vols
	}
	backed := make(map[string]struct{}, len(vols))
	for _, d := range disks {
		for _, v := range d.Volumes {
			backed[strings.ToUpper(v)] = struct{}{}
		}
	}
	if len(backed) == 0 {
		return vols
	}

	out := vols[:0:0]
	for _, v := range vols {
		if _, ok := backed[strings.ToUpper(v.Mount)]; ok {
			out = append(out, v)
		}
	}
	return out
}

// usedPct is the fill percentage of a volume, guarding the empty-volume case.
func usedPct(used, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return float64(used) / float64(total) * 100
}
