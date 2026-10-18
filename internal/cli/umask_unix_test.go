//go:build !windows

package cli

import (
	"syscall"
	"testing"
)

// setUmaskForTest installs mask for the duration of the test and returns the
// function that restores the previous process umask.
func setUmaskForTest(t *testing.T, mask int) func() {
	t.Helper()
	old := syscall.Umask(mask)
	return func() { syscall.Umask(old) }
}
