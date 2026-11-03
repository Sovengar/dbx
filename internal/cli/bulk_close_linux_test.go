//go:build linux

package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// fdClosingBulkConn finishes the COPY and then closes the temporary export descriptor
// behind os.File's back, which is the only way a regular file's Close can fail in a test.
type fdClosingBulkConn struct {
	bulkConn
	dir string
	t   *testing.T
}

func (c *fdClosingBulkConn) CopyTo(ctx context.Context, dst io.Writer, sql string) (pgconn.CommandTag, error) {
	tag, err := c.bulkConn.CopyTo(ctx, dst, sql)
	if err != nil {
		return tag, err
	}
	closeTempDescriptor(c.t, c.dir)
	return tag, nil
}

func closeTempDescriptor(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatalf("reading /proc/self/fd: %v", err)
	}
	wantDir := filepath.Clean(dir)
	for _, entry := range entries {
		target, err := os.Readlink(filepath.Join("/proc/self/fd", entry.Name()))
		if err != nil || filepath.Clean(filepath.Dir(target)) != wantDir {
			continue
		}
		if !strings.HasPrefix(filepath.Base(target), ".dbx-copy-") {
			continue
		}
		fd, err := strconv.Atoi(entry.Name())
		if err != nil {
			t.Fatalf("descriptor name %q: %v", entry.Name(), err)
		}
		if err := syscall.Close(fd); err != nil {
			t.Fatalf("closing descriptor for %s: %v", target, err)
		}
		return
	}
	t.Fatalf("no open export descriptor under %s", dir)
}

func TestExportTableToFileReportsCloseFailures(t *testing.T) {
	for _, tc := range []struct {
		name      string
		output    string
		wantError string
	}{
		{name: "plain file", output: "users.csv", wantError: "failed to finish export"},
		{name: "gzip stream", output: "users.csv.gz", wantError: "failed to finish gzip export"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			conn := &fdClosingBulkConn{
				bulkConn: bulkConn{toData: "id\n1\n2\n"},
				dir:      dir,
				t:        t,
			}
			if _, err := exportTableToFile(conn, "public", "users", filepath.Join(dir, tc.output), nil); err == nil {
				t.Fatal("a stream that could not be closed reported success")
			} else if !strings.Contains(err.Error(), tc.wantError) {
				t.Errorf("error = %q, want it to mention %q", err, tc.wantError)
			}
		})
	}
}
