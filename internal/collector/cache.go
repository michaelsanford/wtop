package collector

import (
	"sync"
	"time"
)

// cachedSensor holds the most recent value of a sensor too slow to sit on the
// per-tick critical path.
//
// The contract Collect() depends on is that get never blocks: it returns
// whatever is cached — the zero value on the first few ticks — and, when that
// value has aged past ttl, kicks off a detached refresh for a later tick to pick
// up. A refresh in flight is never waited on and never duplicated.
//
// Each sensor owns its own mutex. Sharing one lock across every cached sensor
// would make each of them contend with the unrelated per-tick state the
// collector keeps under its own mutex.
//
// There is no Close: refreshes still running at exit die with the process, which
// is the behaviour the GPU path has always had and the reason Collector needs no
// lifecycle.
type cachedSensor[T any] struct {
	mu         sync.Mutex
	val        T
	last       time.Time
	refreshing bool
}

// get returns the cached value, starting a background refresh if it is older
// than ttl and no refresh is already running.
func (c *cachedSensor[T]) get(ttl time.Duration, refresh func() T) T {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.refreshing && time.Since(c.last) > ttl {
		c.refreshing = true
		go func() {
			v := refresh()
			c.mu.Lock()
			c.val, c.last, c.refreshing = v, time.Now(), false
			c.mu.Unlock()
		}()
	}
	return c.val
}
