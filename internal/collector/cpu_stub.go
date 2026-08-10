//go:build !windows

package collector

func collectCPUNative() (CPUSnapshot, error) {
	return collectCPUFallback()
}
