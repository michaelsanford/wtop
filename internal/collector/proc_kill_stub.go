//go:build !windows

package collector

import "errors"

// KillProcess is not supported on non-Windows platforms.
func KillProcess(_ int32) error {
	return errors.New("kill not supported on this platform")
}
