//go:build !windows

package collector

func collectHostNative() HostSnapshot { return collectHostFallback() }
