package config

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// The integration half of the fd story: the real binary, the real flags, the real output.
//
// The unit suite does NOT belong here. Its discovery tests use withFakeFD, a script that
// calls find with the same shape scanWithFD uses, so they run the same way on a machine
// with fd, without fd, or on CI — which is what lets the coverage floor be 100% in both.
// A test that skips depending on what is installed cannot hold a floor.
//
// What only the real binary can prove is the contract with it: that `fd --hidden --type f
// <regex> <root>` is accepted, exits the way the code expects and prints paths the parser
// reads. That is this file, and it skips when fd is absent rather than passing quietly.

// TestScanWithFD_TheRealBinaryDiscoversTheProjects runs scanWithFD against an installed fd.
func TestScanWithFD_TheRealBinaryDiscoversTheProjects(t *testing.T) {
	if _, err := exec.LookPath("fd"); err != nil {
		t.Skip("fd is not installed: scanWithFD would only exercise its failure branch")
	}

	root := t.TempDir()
	writeConnections(t, filepath.Join(root, "one"), map[string]string{"p": "postgres://x/y"})

	got := NewScanner(root).scanWithFD(root)
	if len(got) != 1 {
		t.Fatalf("fd found %d configs, want 1: %v", len(got), got)
	}
	if filepath.Base(got[0]) != ".dbx.toml" {
		t.Errorf("fd returned %q, want a .dbx.toml", got[0])
	}
}
