package collector

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The invariant Collect() is built on: a slow sensor never delays the tick.  If
// get ever waited for the refresh it starts, one slow device would stall the
// whole UI.
func TestCachedSensor_GetDoesNotWaitForTheRefreshItStarts(t *testing.T) {
	var c cachedSensor[int]
	release := make(chan struct{})
	done := make(chan struct{})

	go func() {
		defer close(done)
		if got := c.get(time.Nanosecond, func() int {
			<-release // refresh blocks until the test says otherwise
			return 42
		}); got != 0 {
			t.Errorf("first get returned %d, want the zero value while the refresh is in flight", got)
		}
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("get blocked on its own refresh")
	}
	close(release)
}

// A refresh already in flight must not be duplicated, or a slow sensor would
// spawn a new goroutine on every tick and pile up.
func TestCachedSensor_SingleFlight(t *testing.T) {
	var c cachedSensor[int]
	var calls atomic.Int32
	release := make(chan struct{})
	started := make(chan struct{}, 1)

	refresh := func() int {
		calls.Add(1)
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
		return 1
	}

	// Kick off the first refresh and wait until it is genuinely running.
	c.get(time.Nanosecond, refresh)
	<-started

	for i := 0; i < 50; i++ {
		c.get(time.Nanosecond, refresh)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("refresh ran %d times while one was in flight, want 1", got)
	}
	close(release)
}

func TestCachedSensor_ServesCachedValueUntilTTLExpires(t *testing.T) {
	var c cachedSensor[int]
	var calls atomic.Int32
	refresh := func() int { return int(calls.Add(1)) }

	c.get(time.Hour, refresh) // starts the first refresh
	waitFor(t, func() bool { return c.get(time.Hour, refresh) == 1 })

	for i := 0; i < 20; i++ {
		if got := c.get(time.Hour, refresh); got != 1 {
			t.Fatalf("got %d, want the cached 1", got)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("refresh ran %d times inside the TTL, want 1", got)
	}
}

func TestCachedSensor_RefreshesAfterTTL(t *testing.T) {
	var c cachedSensor[int]
	var calls atomic.Int32
	refresh := func() int { return int(calls.Add(1)) }

	c.get(time.Millisecond, refresh)
	waitFor(t, func() bool { return c.get(time.Millisecond, refresh) >= 2 })
}

// Concurrent callers must be safe; this is the case `go test -race` exists for.
func TestCachedSensor_ConcurrentGets(t *testing.T) {
	var c cachedSensor[int]
	refresh := func() int { return 7 }

	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = c.get(time.Microsecond, refresh)
			}
		}()
	}
	wg.Wait()
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not met within the deadline")
}
