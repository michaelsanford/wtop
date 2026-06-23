package collector

import (
	"testing"
	"time"
)

func TestSnapshotZeroValue(t *testing.T) {
	var s Snapshot
	if !s.CollectedAt.IsZero() {
		t.Error("expected zero CollectedAt")
	}
	if len(s.GPUs) != 0 {
		t.Error("expected empty GPUs slice")
	}
}

func TestNewCollector(t *testing.T) {
	c := New()
	if c == nil {
		t.Fatal("New() returned nil")
	}
}

func TestCollect_NoPanic(t *testing.T) {
	c := New()
	snap, _ := c.Collect()
	// Just check it returns a timestamped snapshot without panicking.
	if snap.CollectedAt.Before(time.Now().Add(-5 * time.Second)) {
		t.Error("CollectedAt is unexpectedly old")
	}
}

func BenchmarkCollectProcs(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, _ = collectProcs()
	}
}
