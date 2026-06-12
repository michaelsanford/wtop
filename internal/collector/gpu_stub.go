//go:build !windows

package collector

func collectAllGPUs() []GPUSnapshot { return nil }
