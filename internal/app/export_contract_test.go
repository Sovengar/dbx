package app

// Scenario: Exportar y copiar tienen que producir lo que el usuario cree.
//
// The export formatters and the clipboard are the last thing between a query and the
// outside world, and every one of them is a pure function except the clipboard — which is
// exactly why they were uncovered. A query result in memory, rendered to text, handed to
// another process: no database, no TUI, and three dozen statements of logic.
//
// Three things are pinned here, and only one of them is a bug:
//
//  1. The clipboard must receive the CONTENT, not an empty stdin. On X11 the xclip
//     attempt runs before Stdin is assigned, so the child reads EOF, copies nothing and
//     exits successfully — a success toast over an empty clipboard.
//  2. A SQL export must be runnable. The column list and the value list are built from
//     two different sources, so a row shorter than the column list produces an INSERT
//     whose arity does not match its columns, and the user's next statement fails at the
//     database with a parse error they cannot connect to this code.
//  3. A row longer or shorter than the column list must not PANIC. JSON indexes the row
//     by column index; CSV iterates the row; SQL iterates the row. Three copies of one
//     assumption and three different answers, which is the shape this repository keeps
//     arriving at.

import (
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/buble/dbx/internal/drivers/postgres"
)

// exportResult is a two-column, three-row result with the values the formatters have to
// get right: a string containing a quote, and a NULL.
//
// Named apart from the package's own sampleResult, which is a three-column two-row
// fixture for the message sweep: a third column and a second NULL-free row would not
// test the escaping or the NULL branch, which is the whole point here.
func exportResult() *postgres.QueryResult {
	return &postgres.QueryResult{
		Columns: []postgres.ColumnInfo{
			{Name: "id", DataType: "integer"},
			{Name: "name", DataType: "text"},
		},
		Rows: [][]interface{}{
			{int64(1), "ada"},
			{int64(2), "it's here"},
			{int64(3), nil},
		},
		Count: 3,
	}
}

// ---------------------------------------------------------------------------
// clipboard
// ---------------------------------------------------------------------------

// fakeClipboard puts a stub named `name` at the front of PATH. The stub writes its stdin
// to the file named by CLIP_OUT, which is how the test sees what the child was actually
// given — as opposed to what the parent meant to give it.
func fakeClipboard(t *testing.T, dir, name string) {
	t.Helper()
	script := "#!/bin/sh\ncat > \"$CLIP_OUT\"\n"
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
}

// Scenario: Lo que se copia al portapapeles LLEGA al proceso que lo recibe.
func TestCopyToClipboardGivesTheContentToTheProcess(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skipf("the clipboard path under test is the X11 one; this is %s", runtime.GOOS)
	}

	t.Run("the FIRST tool tried receives the content", func(t *testing.T) {
		// xclip is the first X11 attempt and it is the one that runs before Stdin is
		// assigned. exec.Cmd with a nil Stdin gives the child the null device, so
		// xclip reads EOF, copies nothing and exits 0 — and copyToClipboard returns
		// success having put NOTHING on the clipboard. The user sees a success toast
		// and pastes an empty string.
		dir := t.TempDir()
		fakeClipboard(t, dir, "xclip")
		// The stub PREPENDED to the real PATH rather than replacing it: the stub is a
		// shell script that runs `cat`, and a PATH containing only the stub directory
		// cannot resolve it. The script exits 127, which reads as "xclip failed" and
		// made the first version of this test fail for a reason that had nothing to do
		// with the bug.
		t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
		t.Setenv("WAYLAND_DISPLAY", "")

		got := filepath.Join(dir, "clip")
		t.Setenv("CLIP_OUT", got)

		const content = "SELECT * FROM users WHERE id = 1"
		if err := copyToClipboard(content); err != nil {
			t.Fatalf("copyToClipboard: %v", err)
		}

		delivered, err := os.ReadFile(got)
		if err != nil {
			t.Fatalf("the stub wrote nothing: %v", err)
		}
		if string(delivered) != content {
			t.Errorf("the clipboard process received %q, want %q", delivered, content)
		}
	})

	t.Run("the SECOND tool is used when the first is missing, and also gets the content", func(t *testing.T) {
		// The fallback is the path most X11 users actually take, and it works only
		// because Stdin happens to be assigned before the final Run. Asserted so the
		// two are not quietly swapped.
		dir := t.TempDir()
		fakeClipboard(t, dir, "xsel")
		t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
		t.Setenv("WAYLAND_DISPLAY", "")

		got := filepath.Join(dir, "clip")
		t.Setenv("CLIP_OUT", got)

		const content = "second"
		if err := copyToClipboard(content); err != nil {
			t.Fatalf("copyToClipboard: %v", err)
		}
		delivered, err := os.ReadFile(got)
		if err != nil {
			t.Fatalf("the stub wrote nothing: %v", err)
		}
		if string(delivered) != content {
			t.Errorf("the clipboard process received %q, want %q", delivered, content)
		}
	})

	t.Run("with NO tool at all the failure is reported", func(t *testing.T) {
		// Not a panic and not a false success. A missing clipboard tool is an error
		// the user should see, because otherwise they believe they copied something.
		// A PATH with nothing on it at all — including no shell and no cat, because
		// nothing is going to run.
		t.Setenv("PATH", t.TempDir())
		t.Setenv("WAYLAND_DISPLAY", "")

		err := copyToClipboard("x")
		if err == nil {
			t.Fatal("copyToClipboard returned no error with no clipboard tool on PATH")
		}
		// The message has to name a program, or the user is left guessing what to
		// install. Only the LAST attempt's error is reported, so it names one of them
		// rather than all — asserting both would be asserting a design the function
		// never claimed.
		if !strings.Contains(err.Error(), "xclip") && !strings.Contains(err.Error(), "xsel") {
			t.Errorf("the error %q names no clipboard program", err)
		}
	})

	t.Run("content with newlines and quotes survives", func(t *testing.T) {
		// The content is SQL, and SQL contains both. A stub that got a truncated or
		// mangled payload would show up here.
		dir := t.TempDir()
		fakeClipboard(t, dir, "xclip")
		t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
		t.Setenv("WAYLAND_DISPLAY", "")

		got := filepath.Join(dir, "clip")
		t.Setenv("CLIP_OUT", got)

		content := "SELECT 'it''s'\nFROM users\nWHERE name = \"a\""
		if err := copyToClipboard(content); err != nil {
			t.Fatal(err)
		}
		delivered, err := os.ReadFile(got)
		if err != nil {
			t.Fatal(err)
		}
		if string(delivered) != content {
			t.Errorf("the clipboard process received %q, want %q", delivered, content)
		}
	})
}

// ---------------------------------------------------------------------------
// SQL
// ---------------------------------------------------------------------------

// Scenario: El INSERT exportado se puede ejecutar tal cual.
func TestTheExportedSQLIsRunnable(t *testing.T) {
	t.Run("the column count matches the value count", func(t *testing.T) {
		out := exportAsSQL("public", "users", exportResult())

		// Parse it back rather than comparing to a golden string: what has to hold is
		// that the statement is well formed, not that it is formatted the way this
		// version formats it.
		if !strings.HasPrefix(out, `INSERT INTO "public"."users" (`) {
			t.Errorf("the statement does not start with the INSERT:\n%s", out)
		}
		if !strings.HasSuffix(strings.TrimSpace(out), ";") {
			t.Errorf("the statement does not end with a semicolon:\n%s", out)
		}
		assertEveryTupleMatchesColumnCount(t, out, len(exportResult().Columns))
	})

	t.Run("a value containing a quote is escaped, not broken", func(t *testing.T) {
		out := exportAsSQL("public", "users", exportResult())
		if !strings.Contains(out, "'it''s here'") {
			t.Errorf("the quote in a value was not doubled:\n%s", out)
		}
		// And the number of quotes is even, which is what "escaped" means for a
		// statement a database will parse.
		assertEvenQuotes(t, out)
	})

	t.Run("a NULL is NULL and not a quoted empty string", func(t *testing.T) {
		// The difference is real in SQL: NULL means "no value" and '' means "the empty
		// string", and a row that had NULL comes back with '' if it is exported wrong.
		out := exportAsSQL("public", "users", exportResult())
		if !strings.Contains(out, "NULL") {
			t.Errorf("no NULL in the output:\n%s", out)
		}
		if strings.Contains(out, "''") {
			// '' appears in the escaped quote case too, so check the NULL row itself.
			if strings.Contains(out, "(3, NULL)") == false && strings.Contains(out, "(3,NULL)") == false {
				t.Errorf("the NULL row is not (3, NULL):\n%s", out)
			}
		}
	})

	t.Run("zero rows is still a valid statement", func(t *testing.T) {
		out := exportAsSQL("public", "users", &postgres.QueryResult{
			Columns: []postgres.ColumnInfo{{Name: "id"}},
		})
		if !strings.HasSuffix(strings.TrimSpace(out), ";") {
			t.Errorf("an empty export is not a valid statement:\n%s", out)
		}
	})

	t.Run("a row SHORTER than the column list does not produce a broken statement", func(t *testing.T) {
		// The header used to be built from result.Columns and the values from the row,
		// so a short row produced an INSERT naming three columns with one value, which
		// does not parse. Nothing in the TUI stops it: the grid accepts a result of any
		// shape.
		//
		// Truncating to the width every row has is the fix, and truncating rather than
		// padding with NULLs is the honest half of it — a column the row does not have
		// was never read from the database, so a NULL for it would put a value in the
		// export that the query never produced.
		out := exportAsSQL("public", "users", &postgres.QueryResult{
			Columns: []postgres.ColumnInfo{{Name: "a"}, {Name: "b"}, {Name: "c"}},
			Rows:    [][]interface{}{{int64(1)}},
		})

		if strings.Contains(out, `"b"`) || strings.Contains(out, `"c"`) {
			t.Errorf("the statement names a column the row has no value for:\n%s", out)
		}
		if strings.Contains(out, "NULL") {
			t.Errorf("the statement invents a NULL for a column the row does not have:\n%s", out)
		}
		// One column, one value.
		assertEveryTupleMatchesColumnCount(t, out, 1)
		assertEvenQuotes(t, out)
	})

	t.Run("the width is the NARROWEST row, not the first", func(t *testing.T) {
		// Taking the first row's width would emit a tuple too short for the second
		// row, which is the same broken statement one row later.
		out := exportAsSQL("public", "users", &postgres.QueryResult{
			Columns: []postgres.ColumnInfo{{Name: "a"}, {Name: "b"}, {Name: "c"}},
			Rows: [][]interface{}{
				{int64(1), int64(2), int64(3)},
				{int64(4)},
			},
		})
		assertEveryTupleMatchesColumnCount(t, out, 1)
		if strings.Contains(out, `"c"`) {
			t.Errorf("the statement names a column only the first row has:\n%s", out)
		}
	})

	t.Run("a rectangular result is untouched by the truncation", func(t *testing.T) {
		// The narrowing must be invisible for anything PostgreSQL returns, or every
		// export changes shape.
		r := exportResult()
		out := exportAsSQL("public", "users", r)
		for _, want := range []string{`"id"`, `"name"`} {
			if !strings.Contains(out, want) {
				t.Errorf("a rectangular result lost the column %s:\n%s", want, out)
			}
		}
		assertEveryTupleMatchesColumnCount(t, out, len(r.Columns))
	})

	t.Run("the single-row form matches the multi-row form for one row", func(t *testing.T) {
		// Two functions build the same statement, one for the whole result and one for
		// a single row the user has selected. If they disagree the same data exports
		// differently depending on how it was copied.
		r := exportResult()
		whole := exportAsSQL("public", "users", &postgres.QueryResult{
			Columns: r.Columns,
			Rows:    r.Rows[:1],
		})
		single := exportRowAsSQL("public", "users", []string{"id", "name"}, r.Rows[0])
		if !sameSQLStatement(t, whole, single) {
			t.Errorf("the two exporters disagree:\n--- whole ---\n%s\n--- single ---\n%s", whole, single)
		}
	})
}

// Scenario: Cada tipo de valor se formatea como SQL, no como Go.
func TestFormatSQLValue(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   interface{}
		want string
	}{
		{"a nil is NULL", nil, "NULL"},
		{"a plain string", "ada", "'ada'"},
		{"a string with a quote", "it's", "'it''s'"},
		{"a string with several quotes", "'''", "''''''''"},
		{"an empty string", "", "''"},
		{"an int64", int64(-42), "-42"},
		{"a float64", 3.5, "3.5"},
		{"a float with no fraction", 2.0, "2"},
		{"a true", true, "TRUE"},
		{"a false", false, "FALSE"},
		{"an int rather than int64", 7, "'7'"},
		{"a time", "2020-01-01", "'2020-01-01'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatSQLValue(tc.in); got != tc.want {
				t.Errorf("formatSQLValue(%#v) is %s, want %s", tc.in, got, tc.want)
			}
		})
	}

	t.Run("an int that is not int64 is quoted, which is legal but worth knowing", func(t *testing.T) {
		// PostgreSQL accepts '7' as an integer literal, so this is not broken SQL. It
		// IS a difference from the int64 case, and it exists because the switch lists
		// the concrete types pgx returns and nothing else. Pinned so a change to the
		// switch is a decision.
		if got := formatSQLValue(7); got != "'7'" {
			t.Errorf("formatSQLValue(7) is %s, want '7' — an unhandled type falls to the default branch", got)
		}
	})
}

func TestQuoteColumnsAndColumnNamesAgree(t *testing.T) {
	// Two functions that quote the same names, one taking []ColumnInfo and one
	// []string. If they ever diverge, the same column is quoted two ways in one
	// statement family.
	cols := []postgres.ColumnInfo{{Name: "id"}, {Name: "weird name"}}
	got := quoteColumns(cols)
	want := quoteColumnNames([]string{"id", "weird name"})

	if len(got) != len(want) {
		t.Fatalf("quoteColumns gave %v, quoteColumnNames gave %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("column %d is %s one way and %s the other", i, got[i], want[i])
		}
		if !strings.HasPrefix(got[i], `"`) || !strings.HasSuffix(got[i], `"`) {
			t.Errorf("column %q is quoted as %s, which has no quotes around it", cols[i].Name, got[i])
		}
	}
}

// ---------------------------------------------------------------------------
// JSON
// ---------------------------------------------------------------------------

// Scenario: El JSON exportado se puede volver a leer, y una fila corta no revienta.
func TestTheExportedJSONIsReadable(t *testing.T) {
	t.Run("the whole result round trips", func(t *testing.T) {
		r := exportResult()
		out := exportAsJSON(r)

		var rows []map[string]interface{}
		if err := json.Unmarshal([]byte(out), &rows); err != nil {
			t.Fatalf("the export is not a JSON list of objects: %v\n%s", err, out)
		}
		if len(rows) != len(r.Rows) {
			t.Fatalf("the export has %d rows, want %d", len(rows), len(r.Rows))
		}
		// Every column is present under its own name, which is the whole point.
		for i, row := range rows {
			for _, col := range r.Columns {
				if _, ok := row[col.Name]; !ok {
					t.Errorf("row %d has no key %q: %v", i, col.Name, row)
				}
			}
		}
		// And the values survive: a NULL stays null rather than becoming "null".
		if rows[2]["name"] != nil {
			t.Errorf("the NULL came back as %#v, want nil", rows[2]["name"])
		}
	})

	t.Run("a row SHORTER than the column list does not panic", func(t *testing.T) {
		// The loop is `for j, col := range result.Columns { rows[i][col.Name] = row[j] }`
		// so a short row indexes past the end. pgx returns rectangular rows, but the
		// grid accepts a result of any shape and the export is reachable from it, and
		// a panic here loses the row the user was copying.
		r := &postgres.QueryResult{
			Columns: []postgres.ColumnInfo{{Name: "a"}, {Name: "b"}},
			Rows:    [][]interface{}{{int64(1)}},
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("exportAsJSON panicked on a short row: %v", r)
				}
			}()
			_ = exportAsJSON(r)
		}()
	})

	t.Run("a row LONGER than the column list is not exported at all, which is right", func(t *testing.T) {
		// Extra cells have no name, so there is nowhere to put them. Silently dropping
		// them is the only option, and it is what the column-keyed loop does.
		r := &postgres.QueryResult{
			Columns: []postgres.ColumnInfo{{Name: "a"}},
			Rows:    [][]interface{}{{int64(1), "extra"}},
		}
		var rows []map[string]interface{}
		if err := json.Unmarshal([]byte(exportAsJSON(r)), &rows); err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || len(rows[0]) != 1 {
			t.Errorf("a one-column result exported %v", rows)
		}
	})

	t.Run("the single-row form produces an object, not a list", func(t *testing.T) {
		out := exportRowAsJSON([]string{"id", "name"}, []interface{}{int64(1), "ada"})

		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(out), &obj); err != nil {
			t.Fatalf("the row export is not a JSON object: %v\n%s", err, out)
		}
		if obj["name"] != "ada" {
			t.Errorf("the object is %v, want name=ada", obj)
		}
	})

	t.Run("a row SHORTER than the name list does not panic", func(t *testing.T) {
		// The same indexing as the whole-result form, and the same crash.
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("exportRowAsJSON panicked on a short row: %v", r)
				}
			}()
			_ = exportRowAsJSON([]string{"a", "b"}, []interface{}{int64(1)})
		}()
	})
}

// ---------------------------------------------------------------------------
// CSV
// ---------------------------------------------------------------------------

// Scenario: El CSV exportado es CSV de verdad, con cabecera.
func TestTheExportedCSVParses(t *testing.T) {
	t.Run("the whole result has a header and one record per row", func(t *testing.T) {
		r := exportResult()
		out := exportAsCSV(r)

		records, err := csv.NewReader(strings.NewReader(out)).ReadAll()
		if err != nil {
			t.Fatalf("the export does not parse as CSV: %v\n%q", err, out)
		}
		if len(records) != len(r.Rows)+1 {
			t.Fatalf("the export has %d records, want a header plus %d rows", len(records), len(r.Rows))
		}
		if got := records[0]; len(got) != 2 || got[0] != "id" || got[1] != "name" {
			t.Errorf("the header is %v, want [id name]", got)
		}
	})

	t.Run("a value containing a comma and a quote is quoted, not split", func(t *testing.T) {
		// The single most common thing that breaks a hand-rolled CSV export.
		r := &postgres.QueryResult{
			Columns: []postgres.ColumnInfo{{Name: "text"}},
			Rows:    [][]interface{}{{`a,b "c" d`}},
		}
		out := exportAsCSV(r)

		records, err := csv.NewReader(strings.NewReader(out)).ReadAll()
		if err != nil {
			t.Fatalf("the export does not parse as CSV: %v\n%q", err, out)
		}
		if len(records) != 2 {
			t.Fatalf("the export has %d records, want a header and one row:\n%q", len(records), out)
		}
		if records[1][0] != `a,b "c" d` {
			t.Errorf("the value came back as %q, want %q", records[1][0], `a,b "c" d`)
		}
	})

	t.Run("a value with a newline stays in one record", func(t *testing.T) {
		r := &postgres.QueryResult{
			Columns: []postgres.ColumnInfo{{Name: "text"}},
			Rows:    [][]interface{}{{"one\ntwo"}},
		}
		records, err := csv.NewReader(strings.NewReader(exportAsCSV(r))).ReadAll()
		if err != nil {
			t.Fatalf("the export does not parse as CSV: %v", err)
		}
		if len(records) != 2 || records[1][0] != "one\ntwo" {
			t.Errorf("the value was split across records: %q", records)
		}
	})

	t.Run("the header and every record have the SAME number of fields", func(t *testing.T) {
		// A short row used to write fewer fields than the header named, and
		// encoding/csv REJECTS that — so the export was not CSV at all, in the sense
		// that no CSV reader would accept it. Three copies of one assumption and three
		// answers: CSV produced something unreadable, JSON panicked, SQL did not parse.
		//
		// The header is truncated to the same width as the records, so a ragged result
		// exports as a well-formed CSV with fewer columns — honest, and accepted.
		r := &postgres.QueryResult{
			Columns: []postgres.ColumnInfo{{Name: "a"}, {Name: "b"}, {Name: "c"}},
			Rows:    [][]interface{}{{int64(1)}},
		}
		out := exportAsCSV(r)
		records, err := csv.NewReader(strings.NewReader(out)).ReadAll()
		if err != nil {
			t.Fatalf("the export does not parse as CSV: %v\n%q", err, out)
		}
		for i, rec := range records {
			if len(rec) != len(records[0]) {
				t.Errorf("record %d has %d fields and the header has %d:\n%q", i, len(rec), len(records[0]), out)
			}
		}
		if len(records[0]) != 1 || records[0][0] != "a" {
			t.Errorf("the header is %v, want just [a] — the width every row has", records[0])
		}
		if strings.Contains(out, "b") || strings.Contains(out, "c") {
			t.Errorf("the export names a column no row has:\n%q", out)
		}
	})

	t.Run("a rectangular result keeps every column", func(t *testing.T) {
		out := exportAsCSV(exportResult())
		records, err := csv.NewReader(strings.NewReader(out)).ReadAll()
		if err != nil {
			t.Fatalf("the export does not parse as CSV: %v", err)
		}
		if len(records[0]) != 2 || records[0][0] != "id" || records[0][1] != "name" {
			t.Errorf("the header is %v, want [id name]", records[0])
		}
	})

	t.Run("the single-row form has a header and one record", func(t *testing.T) {
		out := exportRowAsCSV([]string{"id", "name"}, []interface{}{int64(1), "ada"})
		records, err := csv.NewReader(strings.NewReader(out)).ReadAll()
		if err != nil {
			t.Fatalf("the row export does not parse as CSV: %v\n%q", err, out)
		}
		if len(records) != 2 {
			t.Fatalf("the row export has %d records, want 2:\n%q", len(records), out)
		}
		if records[0][0] != "id" || records[1][1] != "ada" {
			t.Errorf("the row export is %q", records)
		}
	})

	t.Run("a NULL in CSV is the four characters <nil>", func(t *testing.T) {
		// Correct for CSV — there is no NULL in CSV, and inventing an empty field would
		// be indistinguishable from the empty string. Pinned because it looks wrong and
		// is not.
		r := &postgres.QueryResult{
			Columns: []postgres.ColumnInfo{{Name: "a"}},
			Rows:    [][]interface{}{{nil}},
		}
		records, err := csv.NewReader(strings.NewReader(exportAsCSV(r))).ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		if records[1][0] != "<nil>" {
			t.Errorf("the NULL exported as %q, want <nil>", records[1][0])
		}
	})
}

// ---------------------------------------------------------------------------

// assertEveryTupleMatchesColumnCount checks the arity of an INSERT's value tuples
// against its column list. Counting the tuples by scanning for "(" at the start of a
// line is fragile, so this is deliberately simple: it counts the commas inside each
// parenthesised group.
func assertEveryTupleMatchesColumnCount(t *testing.T, stmt string, want int) {
	t.Helper()
	for _, line := range strings.Split(stmt, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "(") || !strings.HasSuffix(line, "),") && !strings.HasSuffix(line, ")") {
			continue
		}
		inner := strings.TrimSuffix(strings.TrimSuffix(line, ","), ")")
		inner = strings.TrimPrefix(inner, "(")
		if inner == "" {
			if want != 0 {
				t.Errorf("an empty tuple in a %d-column statement:\n%s", want, stmt)
			}
			continue
		}
		if got := len(splitTopLevel(inner)); got != want {
			t.Errorf("a value tuple has %d values for %d columns:\n%s", got, want, stmt)
		}
	}
}

// splitTopLevel splits on commas that are not inside a quoted string, so a comma inside
// a quoted value does not count as a separator.
func splitTopLevel(s string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	for _, r := range s {
		switch {
		case r == '\'':
			inQuote = !inQuote
			cur.WriteRune(r)
		case r == ',' && !inQuote:
			out = append(out, cur.String())
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	out = append(out, cur.String())
	return out
}

// assertEvenQuotes checks that a statement has an even number of single quotes, which is
// what "every quote is escaped" means for anything a database has to parse.
func assertEvenQuotes(t *testing.T, stmt string) {
	t.Helper()
	if n := strings.Count(stmt, "'"); n%2 != 0 {
		t.Errorf("the statement has %d single quotes, an odd number, so one is unescaped:\n%s", n, stmt)
	}
}

// sameSQLStatement compares two statements ignoring the trailing comma of a tuple, since
// the whole-result form emits `),` between tuples and the single-row form has only one.
func sameSQLStatement(t *testing.T, a, b string) bool {
	t.Helper()
	norm := func(s string) string {
		s = strings.ReplaceAll(s, "),\n", ")\n")
		s = strings.TrimSuffix(strings.TrimSpace(s), ";")
		return strings.TrimSpace(s)
	}
	return norm(a) == norm(b)
}
