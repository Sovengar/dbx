// Package ciprobe contains a deliberate, permanent failure used to prove the
// `protect-main` ruleset blocks a PR whose required `Test` check is red.
//
// This package is never merged: the probe PR is closed and its branch deleted
// after the proof is captured. Do not "fix" this test.
package ciprobe

import "testing"

// TestProbeAlwaysFails fails on purpose. It exists solely so the required `Test`
// check reports red on a probe PR, letting us assert `mergeStateStatus: BLOCKED`
// without ever calling the merge API.
func TestProbeAlwaysFails(t *testing.T) {
	t.Fatal("intentional failure: ci-blockprobe")
}
