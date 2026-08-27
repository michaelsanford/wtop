package panels

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/michaelsanford/wtop/internal/collector"
)

func vol(mount string, usedPct float64) collector.VolumeSnapshot {
	return collector.VolumeSnapshot{
		Mount: mount, UsedPct: usedPct,
		UsedBytes: uint64(usedPct) << 30, TotalBytes: 100 << 30,
	}
}

// Sorting volumes by drive letter would show C: D: E: F: and hide the 97 %-full
// one, which is the only volume the user actually needed to see.
func TestTrimVolumes_KeepsTheFullestAndPinsTheSystemVolume(t *testing.T) {
	vols := []collector.VolumeSnapshot{
		vol("C:", 10), vol("D:", 97), vol("E:", 80), vol("F:", 5), vol("G:", 60),
	}
	shown, hidden := trimVolumes(vols, 4)

	got := make([]string, len(shown))
	for i, v := range shown {
		got[i] = v.Mount
	}
	// C: pinned first despite being the emptiest, then fullest-first.
	want := []string{"C:", "D:", "E:"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v, want %v", got, want)
	}
	// Three shown of five leaves two hidden; the "+N more" line costs a row.
	if hidden != 2 {
		t.Errorf("hidden = %d, want 2", hidden)
	}
}

func TestTrimVolumes_NoTrimWhenEverythingFits(t *testing.T) {
	vols := []collector.VolumeSnapshot{vol("C:", 10), vol("D:", 20)}
	shown, hidden := trimVolumes(vols, 5)
	if len(shown) != 2 || hidden != 0 {
		t.Errorf("shown=%d hidden=%d, want 2 and 0", len(shown), hidden)
	}
}

// The disk that is actually a bottleneck should be on screen, not whichever one
// happens to be disk 0.
func TestTrimDisks_KeepsTheBusiest(t *testing.T) {
	disks := []collector.DiskSnapshot{
		{Index: 0, BusyPct: 1},
		{Index: 1, BusyPct: 90},
		{Index: 2, BusyPct: 40},
		{Index: 3, BusyPct: 0},
	}
	shown, hidden := trimDisks(disks, 3)
	if len(shown) != 2 || hidden != 2 {
		t.Fatalf("shown=%d hidden=%d, want 2 and 2", len(shown), hidden)
	}
	if shown[0].Index != 1 || shown[1].Index != 2 {
		t.Errorf("got disks %d,%d, want the busiest 1,2", shown[0].Index, shown[1].Index)
	}
}

func TestTrimDisks_TiesBreakByThroughputThenIndex(t *testing.T) {
	disks := []collector.DiskSnapshot{
		{Index: 2, BusyPct: 0},
		{Index: 0, BusyPct: 0, ReadBytesPerSec: 1 << 20},
		{Index: 1, BusyPct: 0},
	}
	shown, _ := trimDisks(disks, 3)
	if shown[0].Index != 0 {
		t.Errorf("busiest-by-throughput disk should lead, got %d", shown[0].Index)
	}
	if shown[1].Index != 1 || shown[2].Index != 2 {
		t.Errorf("idle disks should fall back to index order, got %d,%d", shown[1].Index, shown[2].Index)
	}
}

// mem.go's fmtMemVal uses %5.1fG, which overflows past a terabyte; capacity needs
// its own formatter that stays inside the column.
func TestFmtCapacity(t *testing.T) {
	tests := []struct {
		used, total uint64
		want        string
	}{
		{340 << 30, 476 << 30, "340G/476G"},
		{1<<40 + 614<<30, 9 << 40, "1.6T/9.0T"},
		{512 << 20, 2 << 30, "512M/2G"},
		{0, 0, "0M/0M"},
	}
	for _, tt := range tests {
		got := fmtCapacity(tt.used, tt.total)
		if got != tt.want {
			t.Errorf("fmtCapacity(%d, %d) = %q, want %q", tt.used, tt.total, got, tt.want)
		}
		if len(got) > 10 {
			t.Errorf("%q is %d chars, too wide for the panel line budget", got, len(got))
		}
	}
}

// Capacity thresholds are deliberately higher than cpuBarColor's 60/85: a disk
// that is 80 % full is worth noting, while a CPU at 80 % is already struggling.
func TestCapBarColor(t *testing.T) {
	if capBarColor(70) == capBarColor(80) {
		t.Error("75 % should change the colour")
	}
	if capBarColor(80) == capBarColor(95) {
		t.Error("90 % should change the colour")
	}
	if capBarColor(50) != capBarColor(10) {
		t.Error("an empty-ish volume should not change colour")
	}
}

// Absent capability must never collapse the box, or the row above the process
// table changes height and the table is mis-sized.
func TestDisk_EmptyRendersPlaceholderAtFullHeight(t *testing.T) {
	out := Disk(nil, nil, 40, 9)
	if got := lipgloss.Height(out); got != 11 {
		t.Errorf("height %d, want 11", got)
	}
	if !strings.Contains(stripANSIForTest(out), "no disks") {
		t.Errorf("expected a placeholder line, got:\n%s", out)
	}
}

func TestDisk_ReportsWhatItTrimmed(t *testing.T) {
	vols := []collector.VolumeSnapshot{
		vol("C:", 10), vol("D:", 20), vol("E:", 30), vol("F:", 40), vol("G:", 50), vol("H:", 60),
	}
	out := stripANSIForTest(Disk([]collector.DiskSnapshot{{Index: 0}}, vols, 44, 9))
	if !strings.Contains(out, "more volume") {
		t.Errorf("trimmed volumes should be reported, got:\n%s", out)
	}
}

// A disk with no mounted volume still needs a label.
func TestDiskLabel(t *testing.T) {
	if got := diskLabel(collector.DiskSnapshot{Index: 3}); got != "disk3" {
		t.Errorf("got %q, want disk3", got)
	}
	if got := diskLabel(collector.DiskSnapshot{Index: 0, Volumes: []string{"C:"}}); got != "C:" {
		t.Errorf("got %q, want C:", got)
	}
	if got := diskLabel(collector.DiskSnapshot{Index: 0, Volumes: []string{"C:", "D:"}}); got != "C:+" {
		t.Errorf("multi-volume disk should be marked, got %q", got)
	}
}

func stripANSIForTest(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			inEsc = true
		case inEsc && (r == 'm' || r == 'K'):
			inEsc = false
		case !inEsc:
			b.WriteRune(r)
		}
	}
	return b.String()
}
