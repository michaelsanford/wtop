//go:build windows

package collector

import (
	"runtime"
	"testing"
)

// TestCPUBufferStaysBounded guards against the retry loop running away.  The NT
// processor-performance class rejects any length that is not a whole number of
// entries, so a buffer that is not a multiple of cpuInfoSize is answered with
// STATUS_INFO_LENGTH_MISMATCH forever; doubling it never fixes that, and v1.3.0
// grew it to ~5 GB before a ULONG truncation happened to land on a multiple.
func TestCPUBufferStaysBounded(t *testing.T) {
	snap, err := collectCPU()
	if err != nil {
		t.Fatalf("collectCPU: %v", err)
	}

	globalCPUTracker.mu.Lock()
	n := len(globalCPUTracker.buf)
	globalCPUTracker.mu.Unlock()

	if n%cpuInfoSize != 0 {
		t.Errorf("buffer length %d is not a multiple of %d", n, cpuInfoSize)
	}
	if n > maxCPUBufCores*cpuInfoSize {
		t.Errorf("buffer length %d exceeds cap %d", n, maxCPUBufCores*cpuInfoSize)
	}
	if got, want := len(snap.CorePcts), runtime.NumCPU(); got != want {
		t.Errorf("CorePcts has %d entries, want %d", got, want)
	}
}
