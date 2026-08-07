package ui

import (
	"slices"
	"testing"

	"github.com/michaelsanford/wtop/internal/collector"
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

	// Reserved chrome: CPU panel + metrics row (inner+border) + status + header.
	reserved := cpuH + (metricsPanelInnerH + 2) + statusBarH + tableHeaderH

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
