package cli

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

type bulkConn struct {
	fakeConn
	toData  string
	toSQL   string
	fromSQL string
	from    string
	copyErr error
}

func (c *bulkConn) CopyTo(_ context.Context, dst io.Writer, sql string) (pgconn.CommandTag, error) {
	c.toSQL = sql
	if c.copyErr != nil {
		return pgconn.CommandTag{}, c.copyErr
	}
	_, _ = io.WriteString(dst, c.toData)
	return pgconn.NewCommandTag("COPY 2"), nil
}

func (c *bulkConn) CopyFrom(_ context.Context, src io.Reader, sql string) (pgconn.CommandTag, error) {
	c.fromSQL = sql
	data, _ := io.ReadAll(src)
	c.from = string(data)
	if c.copyErr != nil {
		return pgconn.CommandTag{}, c.copyErr
	}
	return pgconn.NewCommandTag("COPY 3"), nil
}

func TestExportTableAndReportStreamsToStdout(t *testing.T) {
	conn := &bulkConn{toData: "id,name\n1,Ada\n2,Grace\n"}
	var out bytes.Buffer
	if err := exportTableAndReport(&out, conn, "users", "", nil, false); err != nil {
		t.Fatalf("exportTableAndReport: %v", err)
	}
	if out.String() != conn.toData {
		t.Errorf("stdout = %q, want the CSV stream", out.String())
	}
	if !strings.Contains(conn.toSQL, `COPY "public"."users" TO STDOUT`) {
		t.Errorf("COPY SQL = %q", conn.toSQL)
	}
}

func TestExportTableAndReportWritesAtomicGzipAndJSONSummary(t *testing.T) {
	conn := &bulkConn{toData: "id\n1\n2\n"}
	path := filepath.Join(t.TempDir(), "users.csv.gz")
	var out bytes.Buffer
	if err := exportTableAndReport(&out, conn, "sales.users", path, []string{"id"}, true); err != nil {
		t.Fatalf("exportTableAndReport: %v", err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	data, _ := io.ReadAll(reader)
	if string(data) != conn.toData {
		t.Errorf("decompressed file = %q, want %q", data, conn.toData)
	}
	for _, want := range []string{`"table": "sales.users"`, `"rows": 2`, path} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("summary does not contain %q: %s", want, out.String())
		}
	}
}

func TestImportTableAndReportReadsGzipAndReportsRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.csv.gz")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := gzip.NewWriter(file)
	_, _ = io.WriteString(writer, "id,name\n1,Ada\n2,Grace\n3,Linus\n")
	_ = writer.Close()
	_ = file.Close()

	conn := &bulkConn{}
	var out bytes.Buffer
	if err := importTableAndReport(&out, nil, conn, "public.users", path, []string{"id", "name"}, false); err != nil {
		t.Fatalf("importTableAndReport: %v", err)
	}
	if conn.from != "id,name\n1,Ada\n2,Grace\n3,Linus\n" {
		t.Errorf("COPY input = %q", conn.from)
	}
	if !strings.Contains(conn.fromSQL, `("id","name") FROM STDIN`) {
		t.Errorf("COPY SQL = %q", conn.fromSQL)
	}
	if out.String() != "Imported 3 rows into public.users\n" {
		t.Errorf("summary = %q", out.String())
	}
}

func TestBulkCommandsRejectAmbiguousOrUnsafeOutputShapes(t *testing.T) {
	conn := &bulkConn{}
	for _, tc := range []struct {
		name string
		run  func() error
	}{
		{name: "JSON cannot share stdout with CSV", run: func() error {
			return exportTableAndReport(io.Discard, conn, "users", "", nil, true)
		}},
		{name: "too many table separators", run: func() error {
			return exportTableAndReport(io.Discard, conn, "db.public.users", "", nil, false)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(); err == nil {
				t.Fatal("invalid invocation was accepted")
			}
		})
	}
}

func TestFailedBulkExportDoesNotInstallAPartialFile(t *testing.T) {
	conn := &bulkConn{copyErr: errors.New("connection lost")}
	path := filepath.Join(t.TempDir(), "users.csv")
	if _, err := exportTableToFile(conn, "public", "users", path, nil); err == nil {
		t.Fatal("failed COPY returned no error")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("partial export exists at %s: %v", path, err)
	}
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".dbx-copy-*"))
	if len(leftovers) != 0 {
		t.Errorf("temporary exports remain: %v", leftovers)
	}
}
