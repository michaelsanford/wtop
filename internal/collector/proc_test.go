package collector

import (
	"os"
	"slices"
	"testing"
)

// pids returns the chain's members sorted, so comparisons don't depend on map order.
func pids(chain map[int32]struct{}) []int32 {
	out := make([]int32, 0, len(chain))
	for p := range chain {
		out = append(out, p)
	}
	slices.Sort(out)
	return out
}

func TestSelfChainPIDs(t *testing.T) {
	tests := []struct {
		name   string
		start  int32
		ppidOf map[int32]int32
		nameOf map[int32]string
		want   []int32
	}{
		{
			// The ordinary case: wtop → shell → terminal host → root.
			name:   "walks to a root at PPID 0",
			start:  40,
			ppidOf: map[int32]int32{40: 30, 30: 20, 20: 10, 10: 0},
			want:   []int32{10, 20, 30, 40},
		},
		{
			// Windows exposes processes whose parent has already exited, leaving a
			// dangling PPID.  The walk stops there rather than reporting nothing.
			name:   "stops at an absent parent",
			start:  40,
			ppidOf: map[int32]int32{40: 30, 30: 999},
			want:   []int32{30, 40},
		},
		{
			name:   "start PID absent yields an empty chain",
			start:  40,
			ppidOf: map[int32]int32{1: 0},
			want:   []int32{},
		},
		{
			name:   "single root",
			start:  1,
			ppidOf: map[int32]int32{1: 0},
			want:   []int32{1},
		},
		// Nearly everything interactive descends from explorer.exe, so it carries no
		// information about which session hosts wtop and must stay unmarked.
		{
			name:   "stops below the desktop shell",
			start:  40,
			ppidOf: map[int32]int32{40: 30, 30: 20, 20: 10, 10: 0},
			nameOf: map[int32]string{40: "wtop.exe", 30: "pwsh.exe", 20: "WindowsTerminal.exe", 10: "explorer.exe"},
			want:   []int32{20, 30, 40},
		},
		{
			name:   "stop names are matched case-insensitively",
			start:  2,
			ppidOf: map[int32]int32{2: 1, 1: 0},
			nameOf: map[int32]string{2: "wtop.exe", 1: "EXPLORER.EXE"},
			want:   []int32{2},
		},
		{
			// Launched from a scheduled task or service, the chain climbs the SCM
			// side instead; that root is just as uninformative.
			name:   "stops below the service control manager",
			start:  3,
			ppidOf: map[int32]int32{3: 2, 2: 1, 1: 0},
			nameOf: map[int32]string{3: "wtop.exe", 2: "svchost.exe", 1: "services.exe"},
			want:   []int32{2, 3},
		},
		{
			// Double-clicked from Explorer: wtop is the only informative member.
			name:   "self is kept when its own parent is a stop",
			start:  2,
			ppidOf: map[int32]int32{2: 1, 1: 0},
			nameOf: map[int32]string{2: "wtop.exe", 1: "explorer.exe"},
			want:   []int32{2},
		},
		{
			name:   "a stop process at the start yields an empty chain",
			start:  1,
			ppidOf: map[int32]int32{1: 0},
			nameOf: map[int32]string{1: "explorer.exe"},
			want:   []int32{},
		},
		// PID reuse can point a process at a recycled PPID, forming a loop.  These
		// must terminate; the failure mode without the visited check is a hang.
		{
			name:   "self parent terminates",
			start:  7,
			ppidOf: map[int32]int32{7: 7},
			want:   []int32{7},
		},
		{
			name:   "two-node cycle terminates",
			start:  2,
			ppidOf: map[int32]int32{2: 3, 3: 2},
			want:   []int32{2, 3},
		},
		{
			name:   "cycle reached partway up terminates",
			start:  1,
			ppidOf: map[int32]int32{1: 2, 2: 3, 3: 2},
			want:   []int32{1, 2, 3},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Run off-goroutine so a regression that loops forever fails the test
			// binary's timeout rather than wedging silently.
			done := make(chan map[int32]struct{}, 1)
			go func() { done <- selfChainPIDs(tc.start, tc.ppidOf, tc.nameOf) }()
			got := pids(<-done)
			if !slices.Equal(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// End-to-end check against the live process table: the test binary is itself a
// real process, so collectProcs must mark it and pin its ancestors in regardless
// of where they rank on CPU.
func TestCollectProcs_MarksAndPinsSelfChain(t *testing.T) {
	snaps, err := collectProcs()
	if err != nil {
		t.Fatalf("collectProcs: %v", err)
	}

	byPID := make(map[int32]ProcSnapshot, len(snaps))
	marked := 0
	for _, s := range snaps {
		byPID[s.PID] = s
		if s.Self {
			marked++
		}
	}

	self, ok := byPID[int32(os.Getpid())]
	if !ok {
		t.Fatalf("the test binary (PID %d) is missing from the snapshot", os.Getpid())
	}
	if !self.Self {
		t.Errorf("the test binary (PID %d, %q) should be marked as self", self.PID, self.Name)
	}
	// The parent (`go test`, or the shell) is typically idle and would fall off a
	// pure top-CPU cut; pinning is what keeps it here.
	if marked < 2 {
		t.Errorf("got %d self-marked processes, want the whole ancestry chain", marked)
	}

	// The chain must be one unbroken run: exactly one marked process may have an
	// unmarked parent (the top, which sits just below the session root). More than
	// one means a gap, which would render as two disconnected runs in tree view.
	tops := 0
	for _, s := range snaps {
		if !s.Self {
			continue
		}
		if isChainStop(s.Name) {
			t.Errorf("session root %d (%q) should never be marked", s.PID, s.Name)
		}
		if parent, present := byPID[s.PPID]; !present || !parent.Self {
			tops++
		}
	}
	if tops != 1 {
		t.Errorf("got %d chain tops, want exactly 1 — the marked set is not contiguous", tops)
	}
}

func TestSelfChainPIDs_NilMaps(t *testing.T) {
	if got := selfChainPIDs(1, nil, nil); len(got) != 0 {
		t.Errorf("expected an empty chain, got %v", pids(got))
	}
}
