//go:build !windows

package websearch

import "errors"

// open is not supported on non-Windows platforms.
func open(_ string) error {
	return errors.New("web search not supported on this platform")
}
