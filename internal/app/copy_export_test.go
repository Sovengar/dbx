package app

import (
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/ui/components/grid"
)

type copyExportConn struct {
	postgres.Conn
	data string
	sql  string
	err  error
}

func (c *copyExportConn) CopyTo(_ context.Context, dst io.Writer, sql string) (pgconn.CommandTag, error) {
	c.sql = sql
	_, _ = io.WriteString(dst, c.data)
	if c.err != nil {
		return pgconn.CommandTag{}, c.err
	}
	return pgconn.NewCommandTag("COPY 2"), nil
}

func (c *copyExportConn) CopyFrom(context.Context, io.Reader, string) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func TestGridCopyExportStreamsTheWholeTableToAProtectedFile(t *testing.T) {
	project := t.TempDir()
	conn := &copyExportConn{data: "id,name\n1,Ada\n2,Grace\n"}
	m := routerModelLoaded(t)
	m.project = foundProjectIn(project)
	m.conn = conn

	msg := m.handleExport(grid.ExportSelectedMsg{
		Format: grid.ExportCopyCSV,
		Schema: "public",
		Table:  "users",
	})().(exportDoneMsg)
	if msg.err != nil {
		t.Fatalf("COPY export: %v", msg.err)
	}
	if msg.rowCount != 2 {
		t.Errorf("row count = %d, want 2", msg.rowCount)
	}
	if filepath.Base(msg.filename) != "public_users.csv" {
		t.Errorf("filename = %q", msg.filename)
	}
	data, err := os.ReadFile(msg.filename)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != conn.data {
		t.Errorf("file = %q, want the streamed CSV", data)
	}
	if !strings.Contains(conn.sql, `COPY "public"."users" TO STDOUT`) {
		t.Errorf("COPY SQL = %q", conn.sql)
	}
	info, err := os.Stat(msg.filename)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o, want 600", info.Mode().Perm())
	}
}

func TestGridCopyExportHonoursFileCompression(t *testing.T) {
	project := t.TempDir()
	conn := &copyExportConn{data: "id\n1\n2\n"}
	m := routerModelLoaded(t)
	m.config = &config.Config{UI: config.UIConfig{ExportCompress: true}}
	m.project = foundProjectIn(project)
	m.conn = conn

	msg := m.handleCopyExport(grid.ExportSelectedMsg{
		Format: grid.ExportCopyCSV,
		Schema: "public",
		Table:  "users",
	})().(exportDoneMsg)
	if msg.err != nil {
		t.Fatalf("COPY export: %v", msg.err)
	}
	if filepath.Base(msg.filename) != "public_users.csv.gz" {
		t.Errorf("filename = %q", msg.filename)
	}
	file, err := os.Open(msg.filename)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(reader)
	if string(data) != conn.data {
		t.Errorf("gzip payload = %q", data)
	}
}

func TestFailedGridCopyExportRemovesThePartialFile(t *testing.T) {
	project := t.TempDir()
	m := routerModelLoaded(t)
	m.project = foundProjectIn(project)
	m.conn = &copyExportConn{data: "id\n1\n", err: errors.New("connection lost")}

	msg := m.handleCopyExport(grid.ExportSelectedMsg{
		Format: grid.ExportCopyCSV,
		Schema: "public",
		Table:  "users",
	})().(exportDoneMsg)
	if msg.err == nil {
		t.Fatal("failed COPY was reported as successful")
	}
	if _, err := os.Stat(filepath.Join(project, "public_users.csv")); !os.IsNotExist(err) {
		t.Errorf("partial file remains after failure: %v", err)
	}
}
