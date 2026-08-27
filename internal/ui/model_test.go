package ui

import (
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/michaelsanford/wtop/internal/collector"
	"github.com/michaelsanford/wtop/internal/ui/panels"
)

func procsFixture() []collector.ProcSnapshot {
	return []collector.ProcSnapshot{
		{PID: 300, Name: "banana", CPUPct: 5, MemMB: 300},
		{PID: 100, Name: "Cherry", CPUPct: 50, MemMB: 100},
		{PID: 200, Name: "apple", CPUPct: 25, MemMB: 900},
	}
}

func TestBuildSortedRows(t *testing.T) {
	tests := []struct {
		name      string
		sortBy    SortField
		ascending bool
		wantPIDs  []string
	}{
		{"cpu descending", SortByCPU, false, []string{"100", "200", "300"}},
		{"cpu ascending", SortByCPU, true, []string{"300", "200", "100"}},
		{"mem descending", SortByMem, false, []string{"200", "300", "100"}},
		{"mem ascending", SortByMem, true, []string{"100", "300", "200"}},
		{"pid ascending", SortByPID, true, []string{"100", "200", "300"}},
		{"pid descending", SortByPID, false, []string{"300", "200", "100"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rows := buildSortedRows(procsFixture(), tc.sortBy, tc.ascending)
			got := make([]string, 0, len(rows))
			for _, r := range rows {
				got = append(got, r[0])
			}
			if !slices.Equal(got, tc.wantPIDs) {
				t.Errorf("got %v, want %v", got, tc.wantPIDs)
			}
		})
	}
}

// buildSortedRows copies before sorting; the caller's snapshot must be untouched.
func TestBuildSortedRows_DoesNotMutateInput(t *testing.T) {
	procs := procsFixture()
	before := make([]int32, len(procs))
	for i, p := range procs {
		before[i] = p.PID
	}

	buildSortedRows(procs, SortByCPU, false)

	for i, p := range procs {
		if p.PID != before[i] {
			t.Fatalf("input reordered at index %d: %d became %d", i, before[i], p.PID)
		}
	}
}

func TestBuildSortedRows_Empty(t *testing.T) {
	if got := buildSortedRows(nil, SortByCPU, false); len(got) != 0 {
		t.Errorf("expected no rows, got %d", len(got))
	}
}

// Cycling with `s` must visit every field and return to the start.
func TestSortFieldCycle_IsComplete(t *testing.T) {
	seen := map[SortField]bool{}
	f := SortByCPU
	for i := 0; i < int(sortFieldCount); i++ {
		if seen[f] {
			t.Fatalf("sort cycle repeated %v after %d steps", f, i)
		}
		seen[f] = true
		f = (f + 1) % sortFieldCount
	}
	if f != SortByCPU {
		t.Errorf("cycle ended on %v, want to wrap back to SortByCPU", f)
	}
	if len(seen) != int(sortFieldCount) {
		t.Errorf("cycle visited %d fields, want %d", len(seen), sortFieldCount)
	}
}

// Every field needs a label and a default direction, or the status bar and the
// initial sort silently fall back to a zero value.
func TestSortFieldTablesCoverEveryField(t *testing.T) {
	if len(sortLabels) != int(sortFieldCount) {
		t.Errorf("sortLabels has %d entries, want %d", len(sortLabels), sortFieldCount)
	}
	if len(sortDefaultAsc) != int(sortFieldCount) {
		t.Errorf("sortDefaultAsc has %d entries, want %d", len(sortDefaultAsc), sortFieldCount)
	}
	for f := SortField(0); f < sortFieldCount; f++ {
		if sortLabels[f] == "" {
			t.Errorf("sort field %d has an empty label", f)
		}
	}
	// Resource columns should lead with the heaviest consumer; PID and Name ascend.
	if sortDefaultAsc[SortByCPU] || sortDefaultAsc[SortByMem] {
		t.Error("CPU and Mem should default to descending")
	}
	if !sortDefaultAsc[SortByPID] || !sortDefaultAsc[SortByName] {
		t.Error("PID and Name should default to ascending")
	}
}

func TestComputeTableHeight(t *testing.T) {
	const cpuH = 7

	// Reserved chrome: CPU panel + metrics row (inner+border) + status bar.  The
	// table header is not reserved here — bubbles/table accounts for it inside
	// SetHeight, so counting it again would shrink the table by a row.
	reserved := cpuH + (metricsPanelInnerH + 2) + statusBarH

	if got, want := computeTableHeight(reserved+10, cpuH), 10; got != want {
		t.Errorf("got %d, want %d", got, want)
	}

	// Below the floor the table must not collapse or go negative, or the table
	// component will be handed an unusable height.
	for _, termH := range []int{0, 5, 10, reserved, reserved + 2} {
		if got := computeTableHeight(termH, cpuH); got < 3 {
			t.Errorf("termH=%d: got %d, want at least the floor of 3", termH, got)
		}
	}
}

func TestComputeTableHeight_GrowsWithTerminal(t *testing.T) {
	const cpuH = 7
	prev := computeTableHeight(60, cpuH)
	for h := 61; h < 100; h++ {
		got := computeTableHeight(h, cpuH)
		if got < prev {
			t.Fatalf("height shrank from %d to %d as the terminal grew to %d", prev, got, h)
		}
		prev = got
	}
}

func TestCurrentGPU(t *testing.T) {
	gpus := []collector.GPUSnapshot{
		{Name: "gpu0", Source: collector.GPUSourceNvidiaSmi},
		{Name: "gpu1", Source: collector.GPUSourceNvidiaSmi},
	}

	t.Run("empty slice yields a zero snapshot", func(t *testing.T) {
		snap, idx, total := currentGPU(nil, 0)
		if snap.Source != collector.GPUSourceNone || idx != 0 || total != 0 {
			t.Errorf("got (%v, %d, %d), want (GPUSourceNone, 0, 0)", snap.Source, idx, total)
		}
	})

	t.Run("selects the requested GPU", func(t *testing.T) {
		snap, idx, total := currentGPU(gpus, 1)
		if snap.Name != "gpu1" || idx != 1 || total != 2 {
			t.Errorf("got (%s, %d, %d), want (gpu1, 1, 2)", snap.Name, idx, total)
		}
	})

	// The index survives across snapshots, so a GPU disappearing must not panic.
	t.Run("out-of-range index falls back to the first GPU", func(t *testing.T) {
		snap, idx, total := currentGPU(gpus, 7)
		if snap.Name != "gpu0" || idx != 0 || total != 2 {
			t.Errorf("got (%s, %d, %d), want (gpu0, 0, 2)", snap.Name, idx, total)
		}
	})
}

func TestStripANSI(t *testing.T) {
	tests := []struct{ in, want string }{
		{"plain", "plain"},
		{"\x1b[31mred\x1b[0m", "red"},
		{"\x1b[38;5;229m\x1b[48;5;57mstyled\x1b[m", "styled"},
		{"", ""},
	}
	for _, tc := range tests {
		if got := stripANSI(tc.in); got != tc.want {
			t.Errorf("stripANSI(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// --- view containment ---------------------------------------------------------

type fixedCollector struct{ s collector.Snapshot }

func (f fixedCollector) Collect() (collector.Snapshot, error) { return f.s, nil }

func viewFixture() collector.Snapshot {
	procs := make([]collector.ProcSnapshot, 300)
	for i := range procs {
		procs[i] = collector.ProcSnapshot{PID: int32(i + 1), Name: "somelongprocessname.exe", CPUPct: 1}
	}
	return collector.Snapshot{
		CPU: collector.CPUSnapshot{CorePcts: make([]float64, 16), AggrPct: 20},
		Mem: collector.MemSnapshot{
			RAMUsedBytes: 20 << 30, RAMTotalBytes: 64 << 30, RAMPct: 31,
			CommittedBytes: 30 << 30, CommitLimitBytes: 70 << 30,
		},
		GPUs: []collector.GPUSnapshot{{
			Name: "NVIDIA GeForce RTX 5070 Laptop GPU", Source: collector.GPUSourceNvidiaSmi,
			UtilPct: 12, MemUtilPct: 8, MemUsedMiB: 1234, MemTotMiB: 8188,
			TempC: 49, PowerDrawW: 110, PowerLimitW: 115, PState: "P0",
			GfxClockMHz: 2505, MemClockMHz: 10501,
		}},
		Net: []collector.NetSnapshot{
			{Name: "Intel(R) Wi-Fi 6E AX211 160MHz", BytesSentPerSec: 1 << 20, BytesRecvPerSec: 1 << 22},
		},
		Procs: procs,
	}
}

// TestView_FitsTerminal is the end-to-end invariant behind every panel height
// constant: whatever the terminal size, the rendered view occupies exactly the
// rows it was given.  One row too many and the status bar scrolls off; one too
// few and a process row is wasted.
func TestView_FitsTerminal(t *testing.T) {
	snap := viewFixture()

	for _, w := range []int{20, 30, 40, 50, 60, 62, 70, 80, 110, 120, 160, 200, 300} {
		for _, h := range []int{20, 24, 30, 40, 60} {
			m := New(fixedCollector{snap})
			updated, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
			mm := updated.(Model)
			updated, _ = mm.Update(snapshotMsg{snap})
			mm = updated.(Model)

			got := lipgloss.Height(mm.View())
			if got == h {
				continue
			}
			// A terminal too short to hold the chrome plus the three-row table
			// floor legitimately overflows; nothing else may.
			if computeTableHeight(h, panels.CPUHeight(len(snap.CPU.CorePcts), w)) == 3 {
				continue
			}
			t.Errorf("terminal %dx%d: view is %d rows, want %d", w, h, got, h)
		}
	}
}

func TestStatusBar_NeverExceedsTerminalWidth(t *testing.T) {
	snap := viewFixture()
	for _, w := range []int{20, 40, 49, 50, 69, 70, 79, 80, 89, 90, 120, 200} {
		m := New(fixedCollector{snap})
		updated, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: 40})
		mm := updated.(Model)
		bar := mm.statusBar()
		if got := lipgloss.Height(bar); got != statusBarH {
			t.Errorf("width %d: status bar is %d rows, want %d", w, got, statusBarH)
		}
		if got := lipgloss.Width(bar); got > w {
			t.Errorf("width %d: status bar is %d cells wide", w, got)
		}
	}
}

// --- status bar fields --------------------------------------------------------

func TestFmtPower(t *testing.T) {
	tests := []struct {
		name string
		p    collector.PowerSnapshot
		long bool
		want string
	}{
		{
			name: "absent battery renders nothing so desktops keep a clean bar",
			p:    collector.PowerSnapshot{AC: collector.ACOnline, Present: false, State: collector.BatteryAbsent},
			want: "",
		},
		{
			name: "unknown also renders nothing",
			p:    collector.PowerSnapshot{AC: collector.ACUnknown, State: collector.BatteryUnknown},
			want: "",
		},
		{
			name: "discharging short",
			p: collector.PowerSnapshot{AC: collector.ACOffline, Present: true, State: collector.BatteryDischarging,
				Pct: 84, PctValid: true, Remaining: 2*time.Hour + 15*time.Minute, RemainingValid: true},
			want: "BAT 84%▼",
		},
		{
			name: "discharging long includes the estimate",
			p: collector.PowerSnapshot{AC: collector.ACOffline, Present: true, State: collector.BatteryDischarging,
				Pct: 84, PctValid: true, Remaining: 2*time.Hour + 15*time.Minute, RemainingValid: true},
			long: true,
			want: "BAT 84%▼ 2:15",
		},
		{
			name: "charging on mains",
			p: collector.PowerSnapshot{AC: collector.ACOnline, Present: true, State: collector.BatteryCharging,
				Pct: 62, PctValid: true},
			want: "AC 62%▲",
		},
		{
			name: "low battery is spelled out, never coloured",
			p: collector.PowerSnapshot{AC: collector.ACOffline, Present: true, State: collector.BatteryDischarging,
				Pct: 7, PctValid: true},
			want: "BAT 7%▼ LOW",
		},
		{
			name: "saver mode",
			p: collector.PowerSnapshot{AC: collector.ACOffline, Present: true, State: collector.BatteryDischarging,
				Pct: 40, PctValid: true, Saver: true},
			want: "BAT 40%▼ SAVER",
		},
		{
			name: "percentage unknown still reports the source",
			p:    collector.PowerSnapshot{AC: collector.ACOffline, Present: true, State: collector.BatteryDischarging},
			want: "BAT▼",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fmtPower(tt.p, tt.long); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// The bar is clipped as a plain string, so an escape sequence sliced in half
// would corrupt the terminal.  Same hazard the column headers guard against.
func TestStatusBarFields_ContainNoANSI(t *testing.T) {
	p := collector.PowerSnapshot{AC: collector.ACOffline, Present: true, State: collector.BatteryDischarging,
		Pct: 5, PctValid: true, Saver: true, Remaining: time.Hour, RemainingValid: true}
	for _, s := range []string{
		fmtPower(p, true), fmtPower(p, false),
		fmtUptime(50*time.Hour, true), fmtUptime(50*time.Hour, false),
		fmtClock(4404),
	} {
		if strings.Contains(s, "\x1b") {
			t.Errorf("%q contains an escape sequence", s)
		}
	}
}

func TestFmtUptime(t *testing.T) {
	tests := []struct {
		d    time.Duration
		long bool
		want string
	}{
		{0, true, ""},
		{-1, false, ""},
		{90 * time.Minute, true, "up 01:30"},
		{90 * time.Minute, false, "up 1h30m"},
		{80*time.Hour + 26*time.Minute, true, "up 3d 08:26"},
		{80*time.Hour + 26*time.Minute, false, "up 3d08h"},
	}
	for _, tt := range tests {
		if got := fmtUptime(tt.d, tt.long); got != tt.want {
			t.Errorf("fmtUptime(%v, %v) = %q, want %q", tt.d, tt.long, got, tt.want)
		}
	}
}

func TestFmtClock(t *testing.T) {
	if got := fmtClock(0); got != "" {
		t.Errorf("unavailable clock should render nothing, got %q", got)
	}
	if got := fmtClock(4404); got != "4.40GHz" {
		t.Errorf("got %q, want %q", got, "4.40GHz")
	}
}

// The right cluster must survive a long left cluster and a long hostname without
// spilling; it gives up fields in priority order instead.
func TestStatusRight_DropsFieldsInPriorityOrder(t *testing.T) {
	snap := viewFixture()
	snap.Host = collector.HostSnapshot{Uptime: 80 * time.Hour}
	snap.CPU.EffectiveMHz = 4404
	snap.Power = collector.PowerSnapshot{AC: collector.ACOffline, Present: true,
		State: collector.BatteryDischarging, Pct: 7, PctValid: true}

	for _, w := range []int{50, 60, 70, 80, 90, 100, 110, 120, 130, 160, 200} {
		m := New(fixedCollector{snap})
		updated, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: 30})
		mm := updated.(Model)
		updated, _ = mm.Update(snapshotMsg{snap})
		mm = updated.(Model)

		bar := mm.statusBar()
		if got := lipgloss.Width(bar); got != w {
			t.Errorf("width %d: bar rendered %d cells", w, got)
		}
		// The version is the one field that is never given up.
		if !strings.Contains(stripANSI(bar), "wtop ") {
			t.Errorf("width %d: version was dropped: %q", w, stripANSI(bar))
		}
	}
}

func TestStatusRight_OmitsPowerWhenBatteryAbsent(t *testing.T) {
	snap := viewFixture()
	snap.Power = collector.PowerSnapshot{AC: collector.ACOnline, Present: false, State: collector.BatteryAbsent}

	m := New(fixedCollector{snap})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 30})
	mm := updated.(Model)
	updated, _ = mm.Update(snapshotMsg{snap})
	mm = updated.(Model)

	bar := stripANSI(mm.statusBar())
	if strings.Contains(bar, "BAT") || strings.Contains(bar, "AC ") {
		t.Errorf("desktop bar should carry no power field: %q", bar)
	}
}

// --- rotating metrics row -----------------------------------------------------

// The rotation must not change the geometry: whatever is in the slots, the row
// is the same height, or the process table below it is mis-sized.
func TestRotation_PreservesViewHeight(t *testing.T) {
	snap := viewFixture()
	snap.Disks = []collector.DiskSnapshot{
		{Index: 0, Volumes: []string{"C:"}, ReadBytesPerSec: 1 << 20, BusyPct: 12},
	}
	snap.Volumes = []collector.VolumeSnapshot{
		{Mount: "C:", UsedBytes: 340 << 30, TotalBytes: 476 << 30, UsedPct: 72},
	}

	for _, w := range []int{60, 80, 109, 110, 159, 160, 200, 300} {
		for _, h := range []int{24, 30, 40, 60} {
			m := New(fixedCollector{snap})
			updated, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
			mm := updated.(Model)
			updated, _ = mm.Update(snapshotMsg{snap})
			mm = updated.(Model)

			for rot := 0; rot < int(rotationCount); rot++ {
				got := lipgloss.Height(mm.View())
				floored := computeTableHeight(h, panels.CPUHeight(len(snap.CPU.CorePcts), w)) == 3
				if got != h && !floored {
					t.Errorf("terminal %dx%d rotation %d: view is %d rows, want %d", w, h, rot, got, h)
				}
				updated, _ = mm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
				mm = updated.(Model)
			}
		}
	}
}

// At the default offset the layout must match what earlier versions rendered, so
// upgrading does not rearrange anyone's screen.
func TestRotation_DefaultOffsetMatchesHistoricalLayout(t *testing.T) {
	if got := rotatingSlots(109); got != 1 {
		t.Errorf("below 110 should show Mem+GPU only, got %d rotating slots", got)
	}
	if got := rotatingSlots(110); got != 2 {
		t.Errorf("at 110 should show Mem+GPU+Net, got %d rotating slots", got)
	}
	if got := rotatingSlots(159); got != 2 {
		t.Errorf("below 160 should still be 2 rotating slots, got %d", got)
	}
	if got := rotatingSlots(160); got != 3 {
		t.Errorf("at 160 every slot fits, got %d", got)
	}
}

// [i] is a no-op once every slot is on screen, the same way [g] is with a single
// GPU — otherwise pressing it would shuffle panels for no reason.
func TestRotation_IsANoOpWhenEverySlotIsVisible(t *testing.T) {
	snap := viewFixture()

	m := New(fixedCollector{snap})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 200, Height: 40})
	mm := updated.(Model)
	before := mm.rotIdx
	updated, _ = mm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	if got := updated.(Model).rotIdx; got != before {
		t.Errorf("rotation advanced to %d at a width where nothing is hidden", got)
	}

	m = New(fixedCollector{snap})
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	mm = updated.(Model)
	updated, _ = mm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	if got := updated.(Model).rotIdx; got == before {
		t.Error("rotation did not advance at a width where panels are hidden")
	}
}

func TestRotation_SurvivesAnEmptySnapshot(t *testing.T) {
	var empty collector.Snapshot
	m := New(fixedCollector{empty})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	mm := updated.(Model)
	for i := 0; i < int(rotationCount)*2; i++ {
		updated, _ = mm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
		mm = updated.(Model)
		if got := mm.View(); got == "" {
			t.Fatal("view collapsed with no sensors present")
		}
	}
}
