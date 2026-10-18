//go:build !windows

package app

import (
	"errors"
	"os"
	"syscall"
)

// openNoFollow opens path for writing and refuses to follow a symlink at the
// destination. O_NOFOLLOW makes the kernel return ELOOP instead, so the refusal
// happens at open time and the write goes through the descriptor that was checked.
func openNoFollow(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW, 0o600)
}

// isSymlinkRefusal reports whether err is the kernel refusing to follow the
// destination symlink. It shares writeExportFile's refusal path with a lexical
// escape.
func isSymlinkRefusal(err error) bool {
	return errors.Is(err, syscall.ELOOP)
}
