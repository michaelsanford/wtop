//go:build !windows

package collector

func collectProcsNative() ([]ProcSnapshot, error) {
	return collectProcsFallback()
}
