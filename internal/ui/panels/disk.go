package panels

import (
	"fmt"
	"sort"

	"github.com/charmbracelet/lipgloss"
	"github.com/michaelsanford/wtop/internal/collector"
)

// Line budget inside the panel: one header plus at most this many of each
// section.  Physical disks come first because throughput is the live number;
// capacity is the slower-moving context underneath it.
const (
	maxDiskRows   = 3
	maxVolumeRows = 5
)

// Overheads, in the same style as the comments in mem.go.
const (
	// "C:  [" (5) + " NNN%]" (6) + "  " (2) + "340G/476G" (9)
	volLineOverhead = 22
	// Below this there is no room for a bar; the line degrades to plain numbers.
	volBarMinInner = 26
)

// Disk renders per-physical-disk throughput and per-volume capacity.
//
// Read and write are labelled R and W rather than the ↑/↓ arrows the Network
// panel uses: the two panels share a rotating slot, so they keep the same colour
// language, but a vertical arrow does not have an obvious direction for disk I/O
// the way it does for a network link.
func Disk(disks []collector.DiskSnapshot, vols []collector.VolumeSnapshot, width, height int) string {
	inner := width - panelChromeW
	if inner < 10 {
		inner = 10
	}

	lines := make([]string, 0, height)
	lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(colorWhite).Render("Disk"))

	if len(disks) == 0 && len(vols) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(colorBorder).Render("no disks"))
		return RenderPanel(lines, width, height)
	}

	readGlyph := lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true).Render("R")
	writeGlyph := lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true).Render("W")

	shown, hidden := trimDisks(disks, maxDiskRows)
	for _, d := range shown {
		lines = append(lines, diskLine(d, inner, readGlyph, writeGlyph))
	}
	if hidden > 0 {
		lines = append(lines, dimMore(hidden, "disk"))
	}

	// Whatever the disks did not use is available to volumes, so a single-disk
	// machine shows more of them.
	volBudget := height - len(lines)
	if volBudget > maxVolumeRows {
		volBudget = maxVolumeRows
	}
	vshown, vhidden := trimVolumes(vols, volBudget)
	for _, v := range vshown {
		lines = append(lines, volumeLine(v, inner))
	}
	if vhidden > 0 {
		lines = append(lines, dimMore(vhidden, "volume"))
	}

	return RenderPanel(lines, width, height)
}

func dimMore(n int, noun string) string {
	s := fmt.Sprintf("+%d more %s", n, noun)
	if n != 1 {
		s += "s"
	}
	return lipgloss.NewStyle().Foreground(colorBorder).Render(s)
}

// trimDisks keeps the busiest disks.  Sorting by busy% then throughput puts the
// device that is actually a bottleneck on screen rather than whichever one
// happens to be disk 0.
func trimDisks(disks []collector.DiskSnapshot, max int) (shown []collector.DiskSnapshot, hidden int) {
	if len(disks) == 0 {
		return nil, 0
	}
	sorted := make([]collector.DiskSnapshot, len(disks))
	copy(sorted, disks)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.BusyPct != b.BusyPct {
			return a.BusyPct > b.BusyPct
		}
		at := a.ReadBytesPerSec + a.WriteBytesPerSec
		bt := b.ReadBytesPerSec + b.WriteBytesPerSec
		if at != bt {
			return at > bt
		}
		return a.Index < b.Index
	})
	if max < 1 {
		return nil, len(sorted)
	}
	if len(sorted) <= max {
		return sorted, 0
	}
	// The "+N more" line costs a row of its own, so one more disk has to go.
	return sorted[:max-1], len(sorted) - (max - 1)
}

// trimVolumes keeps the fullest volumes, with the system volume pinned first.
// Sorting by drive letter would show C: D: E: and hide the 97%-full one, which
// is the only volume the user needed to see.
func trimVolumes(vols []collector.VolumeSnapshot, max int) (shown []collector.VolumeSnapshot, hidden int) {
	if len(vols) == 0 || max < 1 {
		return nil, len(vols)
	}
	sorted := make([]collector.VolumeSnapshot, len(vols))
	copy(sorted, vols)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if sysA, sysB := a.Mount == "C:", b.Mount == "C:"; sysA != sysB {
			return sysA
		}
		return a.UsedPct > b.UsedPct
	})
	if len(sorted) <= max {
		return sorted, 0
	}
	return sorted[:max-1], len(sorted) - (max - 1)
}

func diskLine(d collector.DiskSnapshot, inner int, readGlyph, writeGlyph string) string {
	name := diskLabel(d)
	// "name(6) R rate(7)  W rate(7)  busy(4)" ≈ 32 cells
	line := fmt.Sprintf("%-6s %s %7s  %s %7s", truncate(name, 6),
		readGlyph, fmtRateCell(d.ReadBytesPerSec),
		writeGlyph, fmtRateCell(d.WriteBytesPerSec))
	if inner >= 32 {
		line += fmt.Sprintf(" %3.0f%%", d.BusyPct)
	}
	return line
}

// diskLabel prefers the drive letters, which mean something to the user, and
// falls back to the disk number for a spindle with nothing mounted.
func diskLabel(d collector.DiskSnapshot) string {
	if len(d.Volumes) > 0 {
		label := d.Volumes[0]
		if len(d.Volumes) > 1 {
			label += "+"
		}
		return label
	}
	return fmt.Sprintf("disk%d", d.Index)
}

func volumeLine(v collector.VolumeSnapshot, inner int) string {
	cap := fmtCapacity(v.UsedBytes, v.TotalBytes)
	if inner < volBarMinInner {
		return fmt.Sprintf("%-3s %3.0f%%  %s", v.Mount, v.UsedPct, cap)
	}
	barW := inner - volLineOverhead
	if barW < 4 {
		barW = 4
	}
	bar := renderBar(v.UsedPct, barW, capBarColor(v.UsedPct))
	return fmt.Sprintf("%-3s [%s %3.0f%%]  %s", v.Mount, bar, v.UsedPct, cap)
}

// capBarColor uses higher thresholds than cpuBarColor.  Fullness is a standing
// condition where 80 % is merely worth noting, while utilisation is instantaneous
// and 80 % already means the device is struggling.
func capBarColor(pct float64) lipgloss.Color {
	switch {
	case pct >= 90:
		return lipgloss.Color("9")
	case pct >= 75:
		return lipgloss.Color("11")
	default:
		return lipgloss.Color("10")
	}
}

// fmtCapacity renders used/total in a fixed 9 cells.  mem.go's fmtMemVal cannot
// be reused: its %5.1fG overflows once a volume passes a terabyte.
func fmtCapacity(used, total uint64) string {
	return fmt.Sprintf("%s/%s", fmtCapVal(used), fmtCapVal(total))
}

func fmtCapVal(b uint64) string {
	const (
		gib = 1 << 30
		tib = 1 << 40
	)
	switch {
	case b >= tib:
		return fmt.Sprintf("%.1fT", float64(b)/tib)
	case b >= gib:
		return fmt.Sprintf("%.0fG", float64(b)/gib)
	default:
		return fmt.Sprintf("%.0fM", float64(b)/(1<<20))
	}
}
