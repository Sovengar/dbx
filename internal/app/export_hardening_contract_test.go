package app

// Hardening addendum to the export feature: the executor's follow-up on the security
// findings left on record by the review, plus the fixes the second review filed against
// them.
//
// The feature was reviewed and approved; these are the security-shaped ones that were
// filed against `main` and are fixed here before the PR merges:
//
//	H1  a schema/table name is database-controlled, and `exportFilename` interpolated it
//	    straight into a path joined onto the project directory, so `x/../../y` escaped
//	    the project (reproduced: it wrote `/home/u/y_t.csv`).
//	H2  a delimited cell beginning with `=`, `+`, `-`, `@`, TAB or CR is executed as a
//	    formula when the file is opened in a spreadsheet. dbx exports data the user did
//	    not author, so the cell is neutralised with a leading apostrophe.
//	H3  exported files were world-readable (`0644`); they are written owner-only (`0600`).
//
// The second review then found the first fix incomplete, and those findings are pinned
// here too:
//
//	F1  the containment check was lexical but the write followed a symlink at the
//	    destination; the open now refuses to follow one (O_NOFOLLOW, ELOOP).
//	F2  the 0600 permission was creation-only, so re-exporting over a 0644 file kept it.
//	F3  sanitised components collided (`a/b`, `a_b` and `a<TAB>b`) and overwrote each
//	    other; a changed component now carries a short digest suffix.
//	F4  the formula escape leaked into the clipboard; it moved to the file boundary.
//	F5  the robustness and log-assertion gaps around all of the above.
//
// Each scenario below is the real test, named after the `behavior.feature` scenario it
// covers.

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buble/dbx/internal/debuglog"
	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/ui/components/grid"
)

// shortDigest is the digest the sanitiser is required to append to a component it had
// to change, computed HERE from the same definition but independently of the
// implementation, so a change to the hex length or the hash is not self-confirming.
func shortDigest(original string) string {
	sum := sha256.Sum256([]byte(original))
	return hex.EncodeToString(sum[:])[:6]
}

// redirectAppLog points the app component's debug log at a fresh temp dir for the rest
// of the test, and returns the path. The package variable exists precisely so a log
// line that production writes can be asserted (see debuglog_contract_test.go).
func redirectAppLog(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old := debuglog.DefaultDir
	debuglog.DefaultDir = dir
	t.Cleanup(func() { debuglog.DefaultDir = old })
	return debuglog.Path("app")
}

// Scenario: A DB-controlled schema or table cannot write outside the project.
//
// `exportFilename` owns the name, so the first layer is there: every path separator and
// control character in either component becomes `_`, a component that sanitises to
// exactly `.` or `..` becomes `_`, and a component that had to be changed carries a `-`
// and the first six hex characters of the sha256 of the ORIGINAL name so two different
// originals cannot collide on one file. An unchanged identifier keeps its historical
// byte-for-byte name.
func TestADBControlledExportNameIsSanitised(t *testing.T) {
	for _, tc := range []struct {
		name   string
		schema string
		table  string
		want   string
	}{
		{"a slash traversal in the schema", "x/../../y", "t", "x_.._.._y-" + shortDigest("x/../../y") + "_t.csv"},
		{"a bare dot-dot table", "public", "..", "public__-" + shortDigest("..") + ".csv"},
		{"a slash in the table", "public", "a/b", "public_a_b-" + shortDigest("a/b") + ".csv"},
		{"a backslash traversal in the schema", `x\..\..\y`, "t", "x_.._.._y-" + shortDigest(`x\..\..\y`) + "_t.csv"},
		{"a control character in the schema", "pub\nlic", "users", "pub_lic-" + shortDigest("pub\nlic") + "_users.csv"},
		{"an absolute-looking path in the table", "public", "/etc/passwd", "public__etc_passwd-" + shortDigest("/etc/passwd") + ".csv"},
		{"an empty table name was not changed and is left alone", "public", "", "public_.csv"},
		{"a three-dot table is not a traversal and is left alone", "public", "...", "public_....csv"},
		{"a non-ASCII identifier is unchanged", "public", "üsers", "public_üsers.csv"},
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
// F3: `a/b`, `a_b` and `a<TAB>b` all sanitise to the same visible text. The digest
// suffix is what keeps the second export from silently overwriting the first, and the
// unchanged identifier still gets its historical name.
func TestSanitisedComponentsDoNotCollide(t *testing.T) {
	seen := map[string]string{}
	for _, table := range []string{"a/b", "a_b", "a\tb"} {
		name := exportFilename("public", table, grid.ExportCSV, ',', false)
		if prev, ok := seen[name]; ok {
			t.Errorf("%q and %q both export as %q", prev, table, name)
		}
		seen[name] = table
	}
	if got := exportFilename("public", "users", grid.ExportCSV, ',', false); got != "public_users.csv" {
		t.Errorf("public/users = %q, want the historical public_users.csv", got)
	}
}

// Scenario: A DB-controlled schema or table cannot write outside the project.
//
// The second, independent layer: after joining, the resolved destination must stay
// under the project directory. `filepath.Rel` is the test — a result of `..` or one
// starting with `..` and a separator is outside — and a violation is refused, not
// written. Tested directly with a traversal name because sanitisation would otherwise
// stop it one layer earlier.
func TestAWriteOutsideTheProjectDirectoryIsRefused(t *testing.T) {
	logPath := redirectAppLog(t)

	dir := t.TempDir()
	outside := filepath.Join(filepath.Dir(dir), "evil.csv")

	if _, err := writeExportFile(dir, "../evil.csv", []byte("x")); err == nil {
		t.Fatal("writing ../evil.csv was accepted")
	}
	if _, err := os.Stat(outside); err == nil {
		t.Errorf("the traversal wrote %s, outside the project", outside)
	}

	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("the refusal was not logged: %v", err)
	}
	for _, want := range []string{"refusing", "../evil.csv", dir} {
		if !strings.Contains(string(log), want) {
			t.Errorf("the log does not contain %q:\n%s", want, log)
		}
	}
}

// Scenario: A DB-controlled schema or table cannot write outside the project.
//
// F1: the lexical check passes for a name that is inside the project, but the file at
// that name is a symlink pointing outside. The open refuses to follow it (O_NOFOLLOW
// gives ELOOP), so the outside target is untouched and the refusal is logged with both
// path operands.
func TestAnExportDestinationSymlinkIsRefusedAndLogged(t *testing.T) {
	logPath := redirectAppLog(t)

	project := t.TempDir()
	outside := filepath.Join(t.TempDir(), "authorized_keys")
	if err := os.WriteFile(outside, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(project, "public_users.csv")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}

	if _, err := writeExportFile(project, "public_users.csv", []byte("evil")); err == nil {
		t.Fatal("an export onto a symlink pointing outside the project was accepted")
	}

	kept, err := os.ReadFile(outside)
	if err != nil {
		t.Fatalf("the outside target vanished: %v", err)
	}
	if string(kept) != "original" {
		t.Errorf("the symlink target was overwritten through the link: %q", kept)
	}
	if target, err := os.Readlink(link); err != nil {
		t.Fatalf("the link was replaced: %v", err)
	} else if target != outside {
		t.Errorf("the link now points at %q, want %q", target, outside)
	}

	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("the refusal was not logged: %v", err)
	}
	for _, want := range []string{"refusing", "public_users.csv", project} {
		if !strings.Contains(string(log), want) {
			t.Errorf("the log does not contain %q:\n%s", want, log)
		}
	}
}

// Scenario: A DB-controlled schema or table cannot write outside the project.
//
// End to end: the reproduced attack name reaches handleExport and the export still lands
// inside the project, under a name whose separators are gone and whose changed component
// carries the digest.
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
	want := "x_.._.._y-" + shortDigest("x/../../y") + "_t.csv"
	if _, err := os.Stat(filepath.Join(project, want)); err != nil {
		t.Errorf("the sanitised export %q was not written inside the project: %v", want, err)
	}
}

// Scenario: A DB-controlled schema or table cannot write outside the project.
//
// F5: an empty project directory used to fall through to `filepath.Join("", name)` and
// write into the process's current working directory. It is refused now.
func TestAnEmptyProjectDirectoryIsRefused(t *testing.T) {
	if _, err := writeExportFile("", "public_users.csv", []byte("x")); err == nil {
		t.Fatal("an empty project directory was accepted")
	}
}

// Scenario: Exported files are readable only by their owner.
//
// F2: the open-time permission applies only when the file is created, so the
// owner-only guarantee has to be applied to the file that is actually there. This is
// the population the review named: a pre-existing 0644 export.
func TestReExportingOverAPreexistingFileTightensItsMode(t *testing.T) {
	project := t.TempDir()
	target := filepath.Join(project, "public_users.csv")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := writeExportFile(project, "public_users.csv", []byte("new")); err != nil {
		t.Fatalf("the export failed: %v", err)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("re-exporting over a 0644 file left mode %o, want 600", got)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "new" {
		t.Errorf("the file holds %q (err %v), want the new export", data, err)
	}
}

// Scenario: Exported files are readable only by their owner.
//
// The two operations after the open — chmod and write — each return an error on a
// descriptor the code owns, which a test cannot produce with a healthy regular file.
// Their failure arms are driven through the package seams and report through the same
// write path the user already sees.
func TestAFailureAfterTheOpenIsReported(t *testing.T) {
	boom := errors.New("the disk is full")

	t.Run("the chmod fails", func(t *testing.T) {
		old := chmodExportFile
		chmodExportFile = func(*os.File, os.FileMode) error { return boom }
		t.Cleanup(func() { chmodExportFile = old })

		if _, err := writeExportFile(t.TempDir(), "public_users.csv", []byte("x")); !errors.Is(err, boom) {
			t.Errorf("error = %v, want it to wrap %v", err, boom)
		}
	})

	t.Run("the write fails", func(t *testing.T) {
		old := writeExportFileData
		writeExportFileData = func(*os.File, []byte) error { return boom }
		t.Cleanup(func() { writeExportFileData = old })

		if _, err := writeExportFile(t.TempDir(), "public_users.csv", []byte("x")); !errors.Is(err, boom) {
			t.Errorf("error = %v, want it to wrap %v", err, boom)
		}
	})
}

// Scenario: A DB-controlled schema or table cannot write outside the project.
//
// F5 length cap: a ~300-byte identifier would exceed the 255-byte limit a filesystem
// puts on one name. It is truncated on a rune boundary and, because it changed, gets
// the short digest, so the export still writes instead of failing with ENAMETOOLONG.
func TestALongComponentStillExports(t *testing.T) {
	long := strings.Repeat("a", 300)

	name := exportFilename("public", long, grid.ExportCSV, ',', false)
	base := strings.TrimSuffix(name, ".csv")
	component := strings.TrimPrefix(base, "public_")
	if !strings.HasSuffix(component, "-"+shortDigest(long)) {
		t.Errorf("the truncated component %q does not end in the digest of the original", component)
	}
	if len(component) > 107 {
		t.Errorf("the component is %d bytes, too close to the filesystem limit: %q", len(component), component)
	}

	// And it really writes, rather than failing with ENAMETOOLONG.
	t.Setenv("PATH", t.TempDir())
	t.Setenv("WAYLAND_DISPLAY", "")
	m := exportModel(t)
	done, ok := m.handleExport(grid.ExportSelectedMsg{
		Format: grid.ExportCSV, Schema: "public", Table: long,
		Columns: []string{"id"}, Rows: [][]interface{}{{int64(1)}},
	})().(exportDoneMsg)
	if !ok {
		t.Fatal("the command did not produce an exportDoneMsg")
	}
	if done.err != nil {
		t.Fatalf("a long table name failed the export: %v", done.err)
	}
	if _, err := os.Stat(done.filename); err != nil {
		t.Errorf("the long-name export was not written: %v", err)
	}
}

// Scenario: Delimited cells cannot inject a spreadsheet formula.
//
// F4 moved the escape to the FILE boundary. `exportAsEscapedCSV` is the file
// serialiser (the one handleExport calls before gzip); it escapes every cell, header
// included, after stringification. SQL, JSON and Markdown never go through it.
func TestDelimitedCellsCannotInjectAFormula(t *testing.T) {
	for _, tc := range []struct{ name, value string }{
		{"equals", "=1+1"},
		{"plus", "+1+1"},
		{"minus", "-1"},
		{"at", "@SUM(A1:A9)"},
		{"tab", "\tcmd"},
		{"carriage return", "\rcmd"},
	} {
		t.Run("a file cell with a leading "+tc.name, func(t *testing.T) {
			result := &postgres.QueryResult{
				Columns: []postgres.ColumnInfo{{Name: "v"}},
				Rows:    [][]interface{}{{tc.value}},
			}
			records, err := csv.NewReader(strings.NewReader(exportAsEscapedCSV(result, ','))).ReadAll()
			if err != nil {
				t.Fatalf("the export does not parse as CSV: %v", err)
			}
			if records[1][0] != "'"+tc.value {
				t.Errorf("the file cell is %q, want the apostrophe escape %q", records[1][0], "'"+tc.value)
			}
		})
	}

	t.Run("a formula-leading column name is escaped in the file serialiser", func(t *testing.T) {
		result := &postgres.QueryResult{
			Columns: []postgres.ColumnInfo{{Name: "=evil"}},
			Rows:    [][]interface{}{{"x"}},
		}
		records, err := csv.NewReader(strings.NewReader(exportAsEscapedCSV(result, ','))).ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		if records[0][0] != "'=evil" {
			t.Errorf("the file header is %q, want the apostrophe escape", records[0][0])
		}
	})

	t.Run("a value with an equals in the middle is unchanged", func(t *testing.T) {
		result := &postgres.QueryResult{
			Columns: []postgres.ColumnInfo{{Name: "v"}},
			Rows:    [][]interface{}{{"a=b"}},
		}
		records, err := csv.NewReader(strings.NewReader(exportAsEscapedCSV(result, ','))).ReadAll()
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
		records, err := csv.NewReader(strings.NewReader(exportAsEscapedCSV(result, ','))).ReadAll()
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

// Scenario: A tab delimiter writes real tab bytes and a .tsv file.
//
// F5: the tab is a delimiter AND a formula lead-in, so the file boundary has to keep
// the two apart — a leading tab is escaped, the separators stay raw tab bytes.
func TestTheTabDelimiterEscapesFormulaCellsInTheFile(t *testing.T) {
	result := &postgres.QueryResult{
		Columns: []postgres.ColumnInfo{{Name: "=evil"}, {Name: "v"}},
		Rows:    [][]interface{}{{"=1+1", "\tcmd"}},
	}
	out := exportAsEscapedCSV(result, '\t')
	reader := csv.NewReader(strings.NewReader(out))
	reader.Comma = '\t'
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("the TSV does not parse: %v\n%q", err, out)
	}
	if len(records) != 2 || len(records[0]) != 2 {
		t.Fatalf("the TSV is %q", records)
	}
	if records[0][0] != "'=evil" {
		t.Errorf("the header is %q, want '=evil", records[0][0])
	}
	if records[1][0] != "'=1+1" {
		t.Errorf("the formula cell is %q, want '=1+1", records[1][0])
	}
	if records[1][1] != "'\tcmd" {
		t.Errorf("the tab-leading cell is %q, want the escaped tab", records[1][1])
	}
}

// Scenario: Delimited cells cannot inject a spreadsheet formula.
//
// F4: the escape belongs to the FILE only. The clipboard and yank payloads are
// byte-faithful again, so `y` on a row with `-1` puts `-1` — not `'-1` — on the
// clipboard, while the CSV file a spreadsheet opens still carries `'-1`.
func TestTheClipboardPayloadIsNotFormulaEscaped(t *testing.T) {
	t.Run("a single row's serialiser is byte-faithful", func(t *testing.T) {
		got := exportRowAsCSV([]string{"v"}, []interface{}{"-1"}, ',')
		if got != "v\n-1\n" {
			t.Errorf("exportRowAsCSV = %q, want the byte-faithful %q", got, "v\n-1\n")
		}
	})

	t.Run("a formula-leading header stays literal on the clipboard", func(t *testing.T) {
		got := exportRowAsCSV([]string{"=evil"}, []interface{}{"x"}, ',')
		if got != "=evil\nx\n" {
			t.Errorf("exportRowAsCSV header = %q, want the byte-faithful =evil", got)
		}
	})

	t.Run("the whole-result serialiser is byte-faithful", func(t *testing.T) {
		result := &postgres.QueryResult{
			Columns: []postgres.ColumnInfo{{Name: "=evil"}},
			Rows:    [][]interface{}{{"-1"}},
		}
		if got := exportAsCSV(result, ','); got != "=evil\n-1\n" {
			t.Errorf("exportAsCSV = %q, want byte-faithful", got)
		}
	})

	t.Run("a yank of several rows puts -1 on the clipboard", func(t *testing.T) {
		dir := t.TempDir()
		clip := clipboardStub(t, dir, "xclip")
		m := exportModel(t)

		done, ok := m.handleExport(grid.ExportSelectedMsg{
			Format: grid.ExportCSV, Schema: "public", Table: "users", YankMode: true,
			Columns: []string{"id"},
			Rows:    [][]interface{}{{int64(-1)}, {int64(2)}},
		})().(exportDoneMsg)
		if !ok {
			t.Fatal("the command did not produce an exportDoneMsg")
		}
		if done.err != nil {
			t.Fatalf("the yank failed: %v", done.err)
		}
		if got := clipboardContents(t, clip); got != "id\n-1\n2\n" {
			t.Errorf("the yank payload is %q, want the byte-faithful export", got)
		}
	})

	t.Run("the CSV FILE is escaped before it is written", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		t.Setenv("WAYLAND_DISPLAY", "")

		m := exportModel(t)
		done, ok := m.handleExport(grid.ExportSelectedMsg{
			Format: grid.ExportCSV, Schema: "public", Table: "users",
			Columns: []string{"id"},
			Rows:    [][]interface{}{{int64(-1)}, {int64(2)}},
		})().(exportDoneMsg)
		if !ok {
			t.Fatal("the command did not produce an exportDoneMsg")
		}
		if done.err != nil {
			t.Fatalf("the export failed: %v", done.err)
		}
		got := clipboardContents(t, done.filename)
		if !strings.Contains(got, "'-1") {
			t.Errorf("the CSV file %q does not contain the formula escape", got)
		}
		if strings.Contains(got, "\n-1\n") {
			t.Errorf("the CSV file %q has an unescaped -1", got)
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

// Scenario: A DB-controlled schema or table cannot write outside the project.
//
// F5: the refusal has to reach the USER, not just the helper. handleExport returns the
// error in its message so the toast shows it, and the outside file is untouched.
func TestARefusedExportShowsTheUserAnError(t *testing.T) {
	project := t.TempDir()
	outside := filepath.Join(t.TempDir(), "authorized_keys")
	if err := os.WriteFile(outside, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(project, "public_users.csv")); err != nil {
		t.Fatal(err)
	}

	m := routerModelLoaded(t)
	m.project = foundProjectIn(project)

	done, ok := m.handleExport(grid.ExportSelectedMsg{
		Format: grid.ExportCSV, Schema: "public", Table: "users",
		Columns: []string{"id"},
		Rows:    [][]interface{}{{int64(1)}},
	})().(exportDoneMsg)
	if !ok {
		t.Fatal("the command did not produce an exportDoneMsg")
	}
	if done.err == nil {
		t.Fatal("a refused write reported success to the user")
	}
	if done.filename != "" {
		t.Errorf("a refused write reported the filename %q", done.filename)
	}
	if kept, err := os.ReadFile(outside); err != nil || string(kept) != "keep" {
		t.Errorf("the outside target is %q (err %v), want it untouched", kept, err)
	}
}
