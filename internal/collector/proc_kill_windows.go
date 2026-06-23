//go:build windows

package collector

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// KillProcess forcefully terminates a process by PID on Windows.
func KillProcess(pid int32) error {
	if pid < 0 {
		return fmt.Errorf("invalid PID %d", pid)
	}
	handle, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return fmt.Errorf("open process %d: %w", pid, err)
	}
	defer windows.CloseHandle(handle) //nolint:errcheck
	if err := windows.TerminateProcess(handle, 1); err != nil {
		return fmt.Errorf("terminate process %d: %w", pid, err)
	}
	return nil
}
