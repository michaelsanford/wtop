package collector

import (
	"slices"
	"testing"
)

// Instance names are the drive number followed by every volume mounted on that
// spindle, so they cannot be split naively — and the leading number is the join
// key to \.\PhysicalDriveN.
func TestParsePhysicalDiskInstance(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		wantIdx  int
		wantVols []string
		wantOK   bool
	}{
		{"single volume", "0 C:", 0, []string{"C:"}, true},
		{"lowercase is normalised", "1 p:", 1, []string{"P:"}, true},
		{"several volumes on one spindle", "0 C: D: E:", 0, []string{"C:", "D:", "E:"}, true},
		{"aggregate row", "_Total", totalDiskIndex, nil, true},
		{"aggregate row is case-insensitive", "_total", totalDiskIndex, nil, true},
		{"data disk with no mounted volume", "3", 3, nil, true},
		{"double-digit index", "10 Z:", 10, []string{"Z:"}, true},
		{"empty", "", 0, nil, false},
		{"not a number", "sda C:", 0, nil, false},
		{"negative", "-1 C:", 0, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idx, vols, ok := parsePhysicalDiskInstance(tt.in)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if idx != tt.wantIdx {
				t.Errorf("index = %d, want %d", idx, tt.wantIdx)
			}
			if !slices.Equal(vols, tt.wantVols) {
				t.Errorf("volumes = %v, want %v", vols, tt.wantVols)
			}
		})
	}
}

// Rate counters occasionally format a hair outside 0-100.
func TestDiskBusyPct(t *testing.T) {
	tests := []struct{ idle, want float64 }{
		{100, 0},
		{0, 100},
		{40, 60},
		{100.4, 0},  // idle slightly over 100
		{-0.3, 100}, // idle slightly negative
	}
	for _, tt := range tests {
		if got := diskBusyPct(tt.idle); got != tt.want {
			t.Errorf("diskBusyPct(%v) = %v, want %v", tt.idle, got, tt.want)
		}
	}
}

// Google Drive's mount reports DRIVE_FIXED and mirrors the system drive's exact
// size, so filtering on drive type alone shows a convincing duplicate of C:.
// Only volumes a physical disk claims are real.
func TestReconcileVolumes_DropsVirtualFilesystems(t *testing.T) {
	disks := []DiskSnapshot{
		{Index: 0, Volumes: []string{"C:"}},
		{Index: 1, Volumes: []string{"P:"}},
		{Index: 2, Volumes: []string{"D:"}},
	}
	vols := []VolumeSnapshot{
		{Mount: "C:", TotalBytes: 2017588146176},
		{Mount: "D:", TotalBytes: 999958757376},
		{Mount: "G:", TotalBytes: 2017588146176}, // virtual, mirrors C: exactly
		{Mount: "P:", TotalBytes: 214681255936},
	}

	got := reconcileVolumes(disks, vols)
	mounts := make([]string, len(got))
	for i, v := range got {
		mounts[i] = v.Mount
	}
	if want := []string{"C:", "D:", "P:"}; !slices.Equal(mounts, want) {
		t.Errorf("got %v, want %v", mounts, want)
	}
}

// With nothing to reconcile against, hiding every volume would be worse than
// showing a spurious one.
func TestReconcileVolumes_KeepsEverythingWhenDiskListIsEmpty(t *testing.T) {
	vols := []VolumeSnapshot{{Mount: "C:"}, {Mount: "G:"}}
	if got := reconcileVolumes(nil, vols); len(got) != 2 {
		t.Errorf("got %d volumes, want all %d", len(got), len(vols))
	}
	if got := reconcileVolumes([]DiskSnapshot{{Index: 0}}, vols); len(got) != 2 {
		t.Errorf("disks with no volume names should not filter; got %d", len(got))
	}
}

func TestReconcileVolumes_MatchesCaseInsensitively(t *testing.T) {
	disks := []DiskSnapshot{{Index: 0, Volumes: []string{"c:"}}}
	vols := []VolumeSnapshot{{Mount: "C:"}}
	if got := reconcileVolumes(disks, vols); len(got) != 1 {
		t.Error("case difference dropped a real volume")
	}
}

func TestUsedPct(t *testing.T) {
	if got := usedPct(0, 0); got != 0 {
		t.Errorf("empty volume: got %v, want 0", got)
	}
	if got := usedPct(50, 200); got != 25 {
		t.Errorf("got %v, want 25", got)
	}
}
