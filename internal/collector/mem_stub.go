//go:build !windows

package collector

func collectMemNative() (MemSnapshot, error) {
	return collectMemFallback()
}

func augmentMemSnapshot(s *MemSnapshot) {}
