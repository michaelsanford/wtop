//go:build windows

package main

import (
	"golang.org/x/sys/windows"
)

func init() {
	// CurrentProcess returns a pseudo-handle carrying full access rights; it
	// avoids an OpenProcess PID round-trip and must not be passed to CloseHandle.
	_ = windows.SetPriorityClass(windows.CurrentProcess(), windows.BELOW_NORMAL_PRIORITY_CLASS)
}
