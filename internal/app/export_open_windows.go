//go:build windows

package app

import (
	"errors"
	"os"
)

// errSymlinkDestination is the Windows stand-in for the kernel's ELOOP: Windows has
// no O_NOFOLLOW, so openNoFollow has to refuse the link itself.
var errSymlinkDestination = errors.New("export destination is a symlink")

// openNoFollow opens path for writing and refuses a symlink at the destination.
// Windows has no O_NOFOLLOW, so the destination is inspected with Lstat first and a
// symlink is refused before the normal open. The check and the open are two steps,
// which leaves a narrow race on Windows that the Unix O_NOFOLLOW path does not have;
// the lexical containment check still runs before both.
func openNoFollow(path string) (*os.File, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
	} else if info.Mode()&os.ModeSymlink != 0 {
		return nil, errSymlinkDestination
	}
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
}

// isSymlinkRefusal reports whether err is openNoFollow refusing the destination
// symlink, so it takes the same path as a lexical escape.
func isSymlinkRefusal(err error) bool {
	return errors.Is(err, errSymlinkDestination)
}
