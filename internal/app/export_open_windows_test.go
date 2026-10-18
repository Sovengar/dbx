//go:build windows

package app

import (
	"os"
	"path/filepath"
	"testing"
)

// Scenario: A symlink at the export destination is refused.
//
// The Windows fallback has no O_NOFOLLOW, so openNoFollow inspects the destination
// with Lstat first. This test compiles under GOOS=windows; it runs on a Windows host
// and skips only when that host cannot create a symlink.
func TestOpenNoFollowRefusesAWindowsDestinationSymlink(t *testing.T) {
	project := t.TempDir()
	outside := filepath.Join(t.TempDir(), "authorized_keys")
	if err := os.WriteFile(outside, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(project, "public_users.csv")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("this Windows host cannot create symlinks: %v", err)
	}

	file, err := openNoFollow(link)
	if file != nil {
		_ = file.Close()
		t.Fatal("openNoFollow accepted a destination symlink")
	}
	if !isSymlinkRefusal(err) {
		t.Fatalf("openNoFollow error = %v, want a symlink refusal", err)
	}
	if kept, err := os.ReadFile(outside); err != nil || string(kept) != "original" {
		t.Errorf("the outside target became %q (err %v), want the original bytes", kept, err)
	}
}

// Scenario: Exported files are readable only by their owner.
//
// The Windows fallback still opens an ordinary destination normally after the Lstat
// check, so the write path is not weakened for the common case.
func TestOpenNoFollowWritesARegularWindowsDestination(t *testing.T) {
	path := filepath.Join(t.TempDir(), "public_users.csv")

	file, err := openNoFollow(path)
	if err != nil {
		t.Fatalf("openNoFollow: %v", err)
	}
	if _, err := file.Write([]byte("id\n1\n")); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "id\n1\n" {
		t.Errorf("the destination holds %q (err %v), want the written bytes", got, err)
	}
}
