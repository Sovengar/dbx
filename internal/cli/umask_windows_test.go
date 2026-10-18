//go:build windows

package cli

import "testing"

// setUmaskForTest is a no-op on Windows, which has no process umask.
func setUmaskForTest(t *testing.T, _ int) func() {
	t.Helper()
	return func() {}
}
