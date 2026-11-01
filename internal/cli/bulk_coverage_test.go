package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func bulkCommand(t *testing.T, values map[string]string, booleans map[string]bool) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "bulk"}
	cmd.Flags().StringP("connection", "c", "", "")
	cmd.Flags().StringP("output", "o", "", "")
	cmd.Flags().String("columns", "", "")
	cmd.Flags().BoolP("json", "j", false, "")
	for name, value := range values {
		if err := cmd.Flags().Set(name, value); err != nil {
			t.Fatalf("setting --%s: %v", name, err)
		}
	}
	for name, value := range booleans {
		if err := cmd.Flags().Set(name, strconv.FormatBool(value)); err != nil {
			t.Fatalf("setting --%s: %v", name, err)
		}
	}
	return cmd
}

func withBulkConnection(t *testing.T, conn *bulkConn) {
	t.Helper()
	inProjectWithAlias(t, "shopdb")
	old := pgxConnect
	t.Cleanup(func() { pgxConnect = old })
	pgxConnect = func(context.Context, string) (pgconnPing, error) { return conn, nil }
}

func TestRunBulkExportWritesAFileAndReportsIt(t *testing.T) {
	conn := &bulkConn{toData: "id,name\n1,Ada\n2,Grace\n"}
	withBulkConnection(t, conn)
	read := captureStdout(t)

	output := filepath.Join(t.TempDir(), "users.csv")
	cmd := bulkCommand(t, map[string]string{"output": output, "columns": "id, name"}, nil)
	if err := runBulkExport(cmd, []string{"sales.users"}); err != nil {
		t.Fatalf("runBulkExport: %v", err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != conn.toData {
		t.Errorf("file = %q, want the streamed CSV", data)
	}
	if !strings.Contains(conn.toSQL, `("id","name")`) {
		t.Errorf("COPY SQL = %q, want the requested columns", conn.toSQL)
	}
	if got := read(); !strings.Contains(got, "Exported 2 rows from sales.users to "+output) {
		t.Errorf("summary = %q", got)
	}
	if conn.closed != 1 {
		t.Errorf("connection closed %d times, want 1", conn.closed)
	}
}

func TestRunBulkImportReadsTheNamedFileAndReportsIt(t *testing.T) {
	conn := &bulkConn{}
	withBulkConnection(t, conn)
	read := captureStdout(t)

	input := filepath.Join(t.TempDir(), "users.csv")
	content := "id,name\n1,Ada\n2,Grace\n"
	if err := os.WriteFile(input, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := bulkCommand(t, map[string]string{"columns": "id, name"}, nil)
	if err := runBulkImport(cmd, []string{"public.users", input}); err != nil {
		t.Fatalf("runBulkImport: %v", err)
	}
	if conn.from != content {
		t.Errorf("COPY input = %q, want %q", conn.from, content)
	}
	if !strings.Contains(conn.fromSQL, `("id","name")`) {
		t.Errorf("COPY SQL = %q, want the requested columns", conn.fromSQL)
	}
	if got := read(); !strings.Contains(got, "Imported 3 rows into public.users") {
		t.Errorf("summary = %q", got)
	}
	if conn.closed != 1 {
		t.Errorf("connection closed %d times, want 1", conn.closed)
	}
}

func TestBulkCommandsRefuseAConnectionTheyCannotOpen(t *testing.T) {
	inProjectWithAlias(t, "shopdb")
	old := pgxConnect
	t.Cleanup(func() { pgxConnect = old })
	pgxConnect = func(context.Context, string) (pgconnPing, error) {
		return nil, errors.New("connection refused")
	}

	if err := runBulkExport(bulkCommand(t, nil, nil), []string{"users"}); err == nil {
		t.Fatal("export without a connection reported success")
	}
	if err := runBulkImport(bulkCommand(t, nil, nil), []string{"users"}); err == nil {
		t.Fatal("import without a connection reported success")
	}
}

func TestBulkCommandsRefuseAnEmptyColumnName(t *testing.T) {
	withBulkConnection(t, &bulkConn{})

	cmd := bulkCommand(t, map[string]string{"columns": "id,,name"}, nil)
	if err := runBulkExport(cmd, []string{"users"}); err == nil {
		t.Fatal("export accepted an empty column name")
	}
	cmd = bulkCommand(t, map[string]string{"columns": "id,,name"}, nil)
	if err := runBulkImport(cmd, []string{"users"}); err == nil {
		t.Fatal("import accepted an empty column name")
	}
}

func TestCopyColumnsParsesTheFlag(t *testing.T) {
	for _, tc := range []struct {
		name    string
		raw     string
		want    []string
		wantErr bool
	}{
		{name: "unset", want: nil},
		{name: "only whitespace", raw: "   ", want: nil},
		{name: "trimmed names", raw: " id , name ", want: []string{"id", "name"}},
		{name: "one name", raw: "id", want: []string{"id"}},
		{name: "an empty component", raw: "id, ,name", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := bulkCommand(t, map[string]string{"columns": tc.raw}, nil)
			got, err := copyColumns(cmd)
			if tc.wantErr {
				if err == nil {
					t.Fatal("an empty column name was accepted")
				}
				return
			}
			if err != nil {
				t.Fatalf("copyColumns: %v", err)
			}
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("columns = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestExportTableAndReportPropagatesACopyFailure(t *testing.T) {
	conn := &bulkConn{copyErr: errors.New("connection lost")}
	if err := exportTableAndReport(io.Discard, conn, "users", "", nil, false); err == nil {
		t.Fatal("a failed COPY reported success")
	}
}

func TestExportTableAndReportPropagatesASummaryWriteFailure(t *testing.T) {
	output := filepath.Join(t.TempDir(), "users.csv")
	err := exportTableAndReport(errWriter{err: errors.New("write refused")}, &bulkConn{toData: "id\n1\n"}, "users", output, nil, false)
	if err == nil {
		t.Fatal("a refused summary write reported success")
	}
}

func TestExportTableToFileRefusesAnUnusableDestination(t *testing.T) {
	t.Run("the directory does not exist", func(t *testing.T) {
		output := filepath.Join(t.TempDir(), "missing", "users.csv")
		if _, err := exportTableToFile(&bulkConn{}, "public", "users", output, nil); err == nil {
			t.Fatal("export into a missing directory reported success")
		}
	})

	t.Run("the destination is an existing directory", func(t *testing.T) {
		output := filepath.Join(t.TempDir(), "taken")
		if err := os.Mkdir(output, 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := exportTableToFile(&bulkConn{toData: "id\n1\n"}, "public", "users", output, nil); err == nil {
			t.Fatal("export onto a directory reported success")
		}
	})
}

func TestImportTableAndReportRefusesInputItCannotUse(t *testing.T) {
	conn := &bulkConn{}

	t.Run("an invalid table reference", func(t *testing.T) {
		if err := importTableAndReport(io.Discard, nil, conn, "a.b.c", "-", nil, false); err == nil {
			t.Fatal("an invalid table reference was accepted")
		}
	})

	t.Run("a missing input file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "missing.csv")
		if err := importTableAndReport(io.Discard, nil, conn, "users", path, nil, false); err == nil {
			t.Fatal("a missing input file was accepted")
		}
	})

	t.Run("a file that is not gzip", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "users.csv.gz")
		if err := os.WriteFile(path, []byte("not gzip"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := importTableAndReport(io.Discard, nil, conn, "users", path, nil, false); err == nil {
			t.Fatal("an invalid gzip file was accepted")
		}
	})

	t.Run("a failed COPY", func(t *testing.T) {
		failing := &bulkConn{copyErr: errors.New("connection lost")}
		if err := importTableAndReport(io.Discard, strings.NewReader("id\n"), failing, "users", "-", nil, false); err == nil {
			t.Fatal("a failed COPY reported success")
		}
	})
}

func TestImportTableAndReportWritesAJSONSummary(t *testing.T) {
	var out bytes.Buffer
	if err := importTableAndReport(&out, strings.NewReader("id\n1\n2\n"), &bulkConn{}, "public.users", "-", nil, true); err != nil {
		t.Fatalf("importTableAndReport: %v", err)
	}
	for _, want := range []string{`"table": "public.users"`, `"rows": 3`, `"file": "-"`} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("summary does not contain %q: %s", want, out.String())
		}
	}
}

func TestImportTableAndReportPropagatesASummaryWriteFailure(t *testing.T) {
	err := importTableAndReport(errWriter{err: errors.New("write refused")}, strings.NewReader("id\n"), &bulkConn{}, "users", "-", nil, false)
	if err == nil {
		t.Fatal("a refused summary write reported success")
	}
}

func TestExportTableToFileReportsAFileItCannotSecure(t *testing.T) {
	old := chmodBulkExportFile
	t.Cleanup(func() { chmodBulkExportFile = old })
	chmodBulkExportFile = func(*os.File, os.FileMode) error { return errors.New("chmod refused") }

	output := filepath.Join(t.TempDir(), "users.csv")
	if _, err := exportTableToFile(&bulkConn{toData: "id\n1\n"}, "public", "users", output, nil); err == nil {
		t.Fatal("an export file that could not be secured reported success")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Errorf("an unsecured export was installed: %v", err)
	}
}
