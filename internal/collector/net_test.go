package collector

import "testing"

func TestNetRate(t *testing.T) {
	tests := []struct {
		name    string
		cur     uint64
		prev    uint64
		elapsed float64
		want    float64
	}{
		{"steady one second", 2000, 1000, 1, 1000},
		{"steady half second", 2000, 1000, 0.5, 2000},
		{"no traffic", 1000, 1000, 1, 0},
		{"first sample from zero", 500, 0, 1, 500},

		// Interface counters are monotonic; a lower current value means the
		// counter reset.  Subtracting first would wrap in uint64 and report
		// ~1.8e19 B/s, so the guard has to precede the subtraction.
		{"counter reset", 500, 1000, 1, 0},
		{"counter reset to zero", 0, 1 << 40, 1, 0},

		{"zero elapsed", 2000, 1000, 0, 0},
		{"negative elapsed", 2000, 1000, -1, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := netRate(tc.cur, tc.prev, tc.elapsed)
			if got != tc.want {
				t.Errorf("netRate(%d, %d, %v) = %g, want %g", tc.cur, tc.prev, tc.elapsed, got, tc.want)
			}
		})
	}
}

// A wrapped uint64 delta converts to a positive float, so any sign check after
// the subtraction is dead code.  Pin the magnitude to catch a regression that
// reintroduces that ordering.
func TestNetRate_ResetDoesNotProduceAbsurdRate(t *testing.T) {
	const plausibleCeiling = 1e12 // 1 TB/s — far above any real NIC
	got := netRate(500, 1000, 1)
	if got > plausibleCeiling {
		t.Errorf("counter reset produced %g B/s; uint64 delta wrapped instead of being clamped", got)
	}
}
