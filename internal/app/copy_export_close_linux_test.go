//go:build linux

package app

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// fdClosingCopyConn finishes the COPY and then closes the destination's descriptor behind
// os.File's back. That is the only state in which Close can fail without a machine-specific
// filesystem, and it is exactly what the caller's cleanup arms report.
type fdClosingCopyConn struct {
	copyExportConn
	path string
	t    *testing.T
}

func (c *fdClosingCopyConn) CopyTo(ctx context.Context, dst io.Writer, sql string) (pgconn.CommandTag, error) {
	tag, err := c.copyExportConn.CopyTo(ctx, dst, sql)
	if err != nil {
		return tag, err
	}
	closeDescriptorForPath(c.t, c.path)
	return tag, nil
}

func closeDescriptorForPath(t *testing.T, path string) {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatalf("reading /proc/self/fd: %v", err)
	}
	want := filepath.Clean(path)
	for _, entry := range entries {
		target, err := os.Readlink(filepath.Join("/proc/self/fd", entry.Name()))
		if err != nil || filepath.Clean(target) != want {
			continue
		}
		fd, err := strconv.Atoi(entry.Name())
		if err != nil {
			t.Fatalf("descriptor name %q: %v", entry.Name(), err)
		}
		if err := syscall.Close(fd); err != nil {
			t.Fatalf("closing descriptor for %s: %v", path, err)
		}
		return
	}
	t.Fatalf("no open descriptor points at %s", path)
}
