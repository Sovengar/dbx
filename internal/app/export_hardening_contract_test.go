package app

// Hardening addendum to the export feature: the executor's follow-up on three findings
// left on record by the close review.
//
// The feature was reviewed and approved; these three are the security-shaped ones that
// were filed against `main` and are fixed here before the PR merges:
//
//	H1  a schema/table name is database-controlled, and `exportFilename` interpolated it
//	    straight into a path joined onto the project directory, so `x/../../y` escaped
//	    the project (reproduced: it wrote `/home/u/y_t.csv`).
//	H2  a delimited cell beginning with `=`, `+`, `-`, `@`, TAB or CR is executed as a
//	    formula when the file is opened in a spreadsheet. dbx exports data the user did
//	    not author, so the cell is neutralised with a leading apostrophe.
//	H3  exported files were world-readable (`0644`); they are written owner-only (`0600`).
//
// Each scenario below is the real test, named after the `behavior.feature` scenario it
// covers.

import (
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/ui/components/grid"
)

// Scenario: A DB-controlled schema or table cannot write outside the project.
//
// `exportFilename` owns the name, so the first layer is there: every path separator and
// control character in either component becomes `_`, and a component that sanitises to
// exactly `.` or `..` becomes `_` so it can never be a traversal component on its own.
func TestADBControlledExportNameIsSanitised(t *testing.T) {
	for _, tc := range []struct {
		name   string
		schema string
		table  string
		want   string
	}{
		{"a slash traversal in the schema", "x/../../y", "t", "x_.._.._y_t.csv"},
		{"a bare dot-dot table", "public", "..", "public__.csv"},
		{"a slash in the table", "public", "a/b", "public_a_b.csv"},
		{"a backslash traversal in the schema", `x\..\..\y`, "t", "x_.._.._y_t.csv"},
		{"a control character in the schema", "pub\nlic", "users", "pub_lic_users.csv"},
		{"an ordinary name is unchanged", "public", "users", "public_users.csv"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := exportFilename(tc.schema, tc.table, grid.ExportCSV, ',', false); got != tc.want {
				t.Errorf("exportFilename(%q, %q) = %q, want %q", tc.schema, tc.table, got, tc.want)
			}
		})
	}
}

// Scenario: A DB-controlled schema or table cannot write outside the project.
//
// The second, independent layer: after joining, the resolved destination must stay under
// the project directory. `filepath.Rel` is the test — an absolute result or one starting
// with `..` is outside — and a violation is refused, not written. Tested directly with a
// traversal name because sanitisation would otherwise stop it one layer earlier.
func TestAWriteOutsideTheProjectDirectoryIsRefused(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(filepath.Dir(dir), "evil.csv")

	if _, err := writeExportFile(dir, "../evil.csv", []byte("x")); err == nil {
		t.Fatal("writing ../evil.csv was accepted")
	}
	if _, err := os.Stat(outside); err == nil {
		t.Errorf("the traversal wrote %s, outside the project", outside)
	}
}

// Scenario: A DB-controlled schema or table cannot write outside the project.
//
// End to end: the reproduced attack name reaches handleExport and the export still lands
// inside the project, under a name whose separators are gone.
func TestTheReproducedTraversalSchemaWritesInsideTheProject(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("WAYLAND_DISPLAY", "")

	project := t.TempDir()
	m := routerModelLoaded(t)
	m.project = foundProjectIn(project)

	done, ok := m.handleExport(grid.ExportSelectedMsg{
		Format: grid.ExportCSV, Schema: "x/../../y", Table: "t",
		Columns: []string{"id"},
		Rows:    [][]interface{}{{int64(1)}},
	})().(exportDoneMsg)
	if !ok {
		t.Fatal("the command did not produce an exportDoneMsg")
	}
	if done.err != nil {
		t.Fatalf("the export failed: %v", done.err)
	}
	if filepath.Dir(done.filename) != project {
		t.Errorf("the export landed in %s, want the project directory %s", filepath.Dir(done.filename), project)
	}
	if _, err := os.Stat(filepath.Join(project, "x_.._.._y_t.csv")); err != nil {
		t.Errorf("the sanitised export was not written inside the project: %v", err)
	}
}

// Scenario: Delimited cells cannot inject a spreadsheet formula.
//
// A spreadsheet treats a cell that starts with `=`, `+`, `-`, `@`, TAB or CR as a
// formula and executes it. Prefixing an apostrophe makes the cell literal text. The
// escaping runs in BOTH delimited exporters, after the value is stringified and before
// it reaches csv.Writer; the cost is the accepted one, `-1` exports as `'-1`.
func TestDelimitedCellsCannotInjectAFormula(t *testing.T) {
	for _, tc := range []struct{ name, value string }{
		{"equals", "=1+1"},
		{"plus", "+1+1"},
		{"minus", "-1"},
		{"at", "@SUM(A1:A9)"},
		{"tab", "\tcmd"},
		{"carriage return", "\rcmd"},
	} {
		t.Run("several rows with a leading "+tc.name, func(t *testing.T) {
			result := &postgres.QueryResult{
				Columns: []postgres.ColumnInfo{{Name: "v"}},
				Rows:    [][]interface{}{{tc.value}},
			}
			records, err := csv.NewReader(strings.NewReader(exportAsCSV(result, ','))).ReadAll()
			if err != nil {
				t.Fatalf("the export does not parse as CSV: %v", err)
			}
			if records[1][0] != "'"+tc.value {
				t.Errorf("the cell is %q, want the apostrophe escape %q", records[1][0], "'"+tc.value)
			}
		})

		t.Run("a single row with a leading "+tc.name, func(t *testing.T) {
			records, err := csv.NewReader(strings.NewReader(
				exportRowAsCSV([]string{"v"}, []interface{}{tc.value}, ','))).ReadAll()
			if err != nil {
				t.Fatalf("the row export does not parse as CSV: %v", err)
			}
			if records[1][0] != "'"+tc.value {
				t.Errorf("the cell is %q, want the apostrophe escape %q", records[1][0], "'"+tc.value)
			}
		})
	}

	t.Run("a formula-leading column name is escaped too", func(t *testing.T) {
		result := &postgres.QueryResult{
			Columns: []postgres.ColumnInfo{{Name: "=evil"}},
			Rows:    [][]interface{}{{"x"}},
		}
		records, err := csv.NewReader(strings.NewReader(exportAsCSV(result, ','))).ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		if records[0][0] != "'=evil" {
			t.Errorf("the header is %q, want the apostrophe escape", records[0][0])
		}
	})

	t.Run("a value with an equals in the middle is unchanged", func(t *testing.T) {
		result := &postgres.QueryResult{
			Columns: []postgres.ColumnInfo{{Name: "v"}},
			Rows:    [][]interface{}{{"a=b"}},
		}
		records, err := csv.NewReader(strings.NewReader(exportAsCSV(result, ','))).ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		if records[1][0] != "a=b" {
			t.Errorf("the cell is %q, want the value untouched", records[1][0])
		}
	})

	t.Run("a NULL is unchanged", func(t *testing.T) {
		result := &postgres.QueryResult{
			Columns: []postgres.ColumnInfo{{Name: "v"}},
			Rows:    [][]interface{}{{nil}},
		}
		records, err := csv.NewReader(strings.NewReader(exportAsCSV(result, ','))).ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		if records[1][0] != "<nil>" {
			t.Errorf("the NULL exported as %q, want the historical <nil> with no apostrophe", records[1][0])
		}
	})

	t.Run("an empty value is unchanged", func(t *testing.T) {
		// value[0] would panic on an empty string, so the helper has an explicit
		// empty arm; it is reached with a real empty field too.
		if got := escapeCSVFormula(" "); got != " " {
			t.Errorf("escapeCSVFormula(space) = %q, want unchanged", got)
		}
		if got := escapeCSVFormula(""); got != "" {
			t.Errorf("escapeCSVFormula(%q) = %q, want empty", "", got)
		}
	})
}

// Scenario: Delimited cells cannot inject a spreadsheet formula.
//
// The escape belongs to the delimited formats only. SQL, JSON and Markdown are not
// spreadsheet input, and prefixing them would corrupt the payload (and, for SQL, break
// the statement).
func TestFormulaEscapingLeavesSQLJSONAndMarkdownUntouched(t *testing.T) {
	result := &postgres.QueryResult{
		Columns: []postgres.ColumnInfo{{Name: "v"}},
		Rows:    [][]interface{}{{"=1+1"}},
	}

	t.Run("Markdown keeps the literal cell", func(t *testing.T) {
		got := exportAsMarkdown(result)
		if !strings.Contains(got, "| =1+1 |") {
			t.Errorf("the Markdown table lost the value:\n%s", got)
		}
		if strings.Contains(got, "'=1+1") {
			t.Errorf("the Markdown export was formula-escaped:\n%s", got)
		}
	})

	t.Run("JSON keeps the literal string", func(t *testing.T) {
		got := exportAsJSON(result)
		if !strings.Contains(got, `"=1+1"`) {
			t.Errorf("the JSON export lost the value:\n%s", got)
		}
		if strings.Contains(got, `"'=1+1"`) {
			t.Errorf("the JSON export was formula-escaped:\n%s", got)
		}
	})

	t.Run("SQL keeps its own quoting", func(t *testing.T) {
		got := exportAsSQL("public", "users", result)
		if !strings.Contains(got, "'=1+1'") {
			t.Errorf("the SQL export lost the value:\n%s", got)
		}
		if strings.Contains(got, "'''") {
			t.Errorf("the SQL export was formula-escaped:\n%s", got)
		}
	})

	t.Run("the JSON encoding of the result is still valid", func(t *testing.T) {
		var rows []map[string]interface{}
		if err := json.Unmarshal([]byte(exportAsJSON(result)), &rows); err != nil {
			t.Fatalf("the JSON export is not valid JSON: %v", err)
		}
	})
}

// Scenario: Exported files are readable only by their owner.
//
// An export can hold anything the query returned, so it is owner-only (`0600`) rather
// than world-readable (`0644`). `dbx context -o` is a different surface and is not
// changed.
func TestAnExportedFileIsWrittenOwnerOnly(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("WAYLAND_DISPLAY", "")

	m := exportModel(t)
	done, ok := m.handleExport(grid.ExportSelectedMsg{
		Format: grid.ExportCSV, Schema: "public", Table: "users",
		Columns: []string{"id", "name"},
		Rows:    [][]interface{}{{int64(1), "ada"}},
	})().(exportDoneMsg)
	if !ok {
		t.Fatal("the command did not produce an exportDoneMsg")
	}
	if done.err != nil {
		t.Fatalf("the export failed: %v", done.err)
	}

	info, err := os.Stat(done.filename)
	if err != nil {
		t.Fatalf("the file was not written: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("the exported file mode is %o, want 600", got)
	}
}
