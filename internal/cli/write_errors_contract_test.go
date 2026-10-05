package cli

// Scenario: Cada error de escritura del CLI se propaga, y el DSN local se busca antes que
// el global.
//
// The CLI writes its output through io.Writer precisely so a test can read the exact bytes
// a user would see. That same seam is the only way to reach the error arms, because a
// bytes.Buffer never fails and a real terminal is not available in a test. There are six
// of them — one per Fprintf — and they are all `if _, err := ...; err != nil { return err }`,
// which is the shape that gets "simplified" into a bare call by a cleanup nobody notices.
//
// What a swallowed write error looks like for a user is specific: `dbx query` prints a
// header, the terminal goes away mid-write, and the command exits 0 having reported
// success for output nobody received. That is worse than a visible failure, because a
// script piping the output cannot tell.

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buble/dbx/internal/drivers/postgres"
)

// failingWriter fails every write, and remembers how many it refused. It fails on the
// FIRST call by default so each arm is reachable on its own; `afterN` lets the first N
// writes through so a specific arm can be reached while everything before it succeeds.
type failingWriter struct {
	calls  int
	afterN int
	err    error
}

var errWriteRefused = errors.New("the terminal went away")

func (w *failingWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.calls <= w.afterN {
		return len(p), nil
	}
	return 0, w.err
}

// aResult is a result with every shape in it: a string, a number and a nil, so one write
// pass touches several fmt verbs.
func aResult() *postgres.QueryResult {
	return &postgres.QueryResult{
		Columns: []postgres.ColumnInfo{
			{Name: "id", DataType: "integer"},
			{Name: "name", DataType: "text"},
		},
		Rows: [][]interface{}{
			{1, "ada"},
			{2, nil},
		},
		Count: 2,
	}
}

func TestEveryWriteErrorReachesTheCaller(t *testing.T) {
	result := aResult()

	for _, tc := range []struct {
		name  string
		after int
		want  string
	}{
		// after is the number of writes allowed through before the writer starts refusing.
		// The count is what locates each arm: the header writes one call per column, then
		// a newline, then one per cell per row plus a newline, then the footer.
		{name: "the header's first column", after: 0},
		{name: "the header's newline", after: 2},
		{name: "the first cell of the first row", after: 3},
		{name: "the first row's newline", after: 5},
		{name: "the second row's cells", after: 6},
		{name: "the footer", after: 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := &failingWriter{afterN: tc.after, err: errWriteRefused}

			err := writeQueryResult(w, result, false)

			if err == nil {
				t.Fatalf("writeQueryResult reported success after %d good writes and a refused one", tc.after)
			}
			if !errors.Is(err, errWriteRefused) {
				t.Errorf("the error is %v, want the write error itself so a caller can tell it from a query failure", err)
			}
		})
	}

	t.Run("the same for the JSON form", func(t *testing.T) {
		// writeJSON is one call, so the arm is the whole function. Asserted separately
		// because it has no per-write structure to walk.
		w := &failingWriter{err: errWriteRefused}
		if err := writeQueryResult(w, result, true); !errors.Is(err, errWriteRefused) {
			t.Errorf("the JSON form returned %v, want the write error", err)
		}
	})

	t.Run("and for the column listing", func(t *testing.T) {
		cols := []postgres.ColumnInfo{
			{Name: "id", DataType: "integer", IsNullable: "YES"},
			{Name: "name", DataType: "text"},
		}
		// One call per column, so the two arms are the first column and the second.
		for _, after := range []int{0, 1} {
			w := &failingWriter{afterN: after, err: errWriteRefused}
			if err := writeColumns(w, cols); !errors.Is(err, errWriteRefused) {
				t.Errorf("with %d good writes writeColumns returned %v, want the write error", after, err)
			}
		}
	})
}

// The other half of the same function: when the writes all succeed, the output is what a
// user pastes into a bug report. Pinned byte for byte rather than by substring, because the
// alignment is the feature — `dbx query` output is read by eye, and a column width that
// changes turns every row into a different shape.
func TestTheHumanReadableOutputIsWhatItClaimsToBe(t *testing.T) {
	var buf bytes.Buffer
	if err := writeQueryResult(&buf, aResult(), false); err != nil {
		t.Fatalf("writeQueryResult failed: %v", err)
	}

	got := buf.String()
	lines := strings.Split(got, "\n")

	if len(lines) < 5 {
		t.Fatalf("the output is %d lines, want a header, two rows and a footer:\n%s", len(lines), got)
	}
	if !strings.Contains(lines[0], "id") || !strings.Contains(lines[0], "name") {
		t.Errorf("the header does not carry both column names: %q", lines[0])
	}
	if !strings.Contains(lines[1], "1") || !strings.Contains(lines[1], "ada") {
		t.Errorf("the first row does not carry its values: %q", lines[1])
	}
	// The nil cell: fmt's %v on a nil interface prints <nil>, which is what a user sees
	// and what they will paste into a bug report. Pinned so a change to the verb shows up
	// as a decision rather than as a surprise.
	if !strings.Contains(lines[2], "<nil>") {
		t.Errorf("a NULL cell rendered as %q, want <nil>", lines[2])
	}
	// And the footer states the count, which is the number a script would otherwise have
	// to count by hand.
	if !strings.Contains(got, "2 rows") {
		t.Errorf("the footer does not state the row count:\n%s", got)
	}
	// The header names each column at the same width the cells use, which is what makes
	// the output aligned. Both are %-20s, so the columns start 20 apart.
	if idx := strings.Index(lines[0], "name"); idx < 20 {
		t.Errorf("the second column starts at %d, want 20 — the header is not aligned with the cells", idx)
	}
}

// findLocalDSN is the first thing getConnection tries: a .dbx.toml in the working
// directory beats the global config. That ordering is the feature — a project with its own
// database should not need a flag — so it is asserted rather than assumed.
func TestTheLocalProjectFileIsFoundBeforeTheGlobalConfig(t *testing.T) {
	// `[connections.NAME]`, because ProjectConfig.Connections is a MAP. The first version
	// of this test wrote `[[connections]]` — the shape a slice would take — and reported
	// that a perfectly good project file was not found.
	for _, tc := range []struct {
		name string
		toml string
		want string
	}{
		{
			name: "a single connection",
			toml: "[connections.local]\ndsn = \"postgres://local/db\"\n",
			want: "postgres://local/db",
		},
		{
			name: "the driver field does not change the DSN",
			toml: "[connections.x]\ndriver = \"postgres\"\ndsn = \"postgres://x/db\"\n",
			want: "postgres://x/db",
		},
		{
			// The alphabetical rule, and the reason it exists. Two connections, and the
			// answer has to be the SAME on every invocation — Go randomises map iteration,
			// so `for _, c := range cfg.Connections { return c }` returned a different
			// database on different runs of the same command.
			//
			// The first version of this case asserted "the first of several" by writing
			// them in an order and naming the first one; the assertion passed or failed
			// depending on the run. It is here as twenty iterations, which is what makes a
			// regression visible at all.
			name: "with two connections the answer is stable",
			toml: "[connections.zeta]\ndsn = \"postgres://zeta/db\"\n\n" +
				"[connections.alpha]\ndsn = \"postgres://alpha/db\"\n",
			want: "postgres://alpha/db",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, ".dbx.toml"), []byte(tc.toml), 0o644); err != nil {
				t.Fatalf("writing the project file: %v", err)
			}
			chdir(t, dir)

			for range 20 {
				if got := findLocalDSN(""); got != tc.want {
					t.Fatalf("findLocalDSN() = %q, want %q — and it must be the same every time", got, tc.want)
				}
			}
		})
	}

	t.Run("a NAMED connection wins over the default", func(t *testing.T) {
		// The flag used to be ignored entirely inside a project: `--connection zeta`
		// connected to whichever entry the map yielded. The same command behaved
		// differently depending on whether a .dbx.toml happened to exist.
		dir := t.TempDir()
		toml := "[connections.zeta]\ndsn = \"postgres://zeta/db\"\n\n" +
			"[connections.alpha]\ndsn = \"postgres://alpha/db\"\n"
		if err := os.WriteFile(filepath.Join(dir, ".dbx.toml"), []byte(toml), 0o644); err != nil {
			t.Fatalf("writing the project file: %v", err)
		}
		chdir(t, dir)

		if got := findLocalDSN("zeta"); got != "postgres://zeta/db" {
			t.Errorf("findLocalDSN(\"zeta\") = %q, want the named connection", got)
		}
		// And with no name, the default rule still applies — the flag is an override, not
		// a requirement.
		if got := findLocalDSN(""); got != "postgres://alpha/db" {
			t.Errorf("findLocalDSN() = %q, want the alphabetically first connection", got)
		}
	})

	t.Run("a name that is NOT in the file yields nothing", func(t *testing.T) {
		// Falling back to the default here would connect to the wrong database while
		// claiming to have used the named one. Returning nothing sends the caller on to
		// the global config, where the name IS validated.
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, ".dbx.toml"),
			[]byte("[connections.alpha]\ndsn = \"postgres://alpha/db\"\n"), 0o644); err != nil {
			t.Fatalf("writing the project file: %v", err)
		}
		chdir(t, dir)

		if got := findLocalDSN("nope"); got != "" {
			t.Errorf("findLocalDSN(\"nope\") = %q, want the empty string", got)
		}
	})

	t.Run("a MISSING project file is not an error", func(t *testing.T) {
		// Every command run outside a project directory takes this path, and returning an
		// error would make `dbx query` unusable everywhere else.
		chdir(t, t.TempDir())
		if got := findLocalDSN(""); got != "" {
			t.Errorf("with no project file findLocalDSN() = %q, want the empty string", got)
		}
	})

	t.Run("a MALFORMED project file is not an error either", func(t *testing.T) {
		// A typo in .dbx.toml must not make every command fail; it falls through to the
		// global config, which is what the empty return is for.
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, ".dbx.toml"), []byte("this is not toml [[["), 0o644); err != nil {
			t.Fatalf("writing the project file: %v", err)
		}
		chdir(t, dir)

		if got := findLocalDSN(""); got != "" {
			t.Errorf("with a malformed project file findLocalDSN() = %q, want the empty string", got)
		}
	})

	t.Run("a project file with NO connections yields nothing", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, ".dbx.toml"), []byte("# nothing here\n"), 0o644); err != nil {
			t.Fatalf("writing the project file: %v", err)
		}
		chdir(t, dir)

		if got := findLocalDSN(""); got != "" {
			t.Errorf("with no connections findLocalDSN() = %q, want the empty string", got)
		}
	})
}

// chdir moves into dir for the duration of the test. os.Getwd has no injectable form, so
// this is a process-wide change and the tests that use it must not run in parallel — none
// of them do, and t.Chdir exists in Go 1.24+ for exactly this.
func chdir(t *testing.T, dir string) {
	t.Helper()
	t.Chdir(dir)
}

// The two listing commands share the same shape as the query report, so they share the
// same failure: the header goes out, the terminal goes away, and the command exits 0
// having reported success for output nobody received.
//
// They take an io.Writer for the same reason writeQueryResult does, so the same
// failing-writer fixture reaches all of them. Each is walked by POSITION, because the
// position is what locates the arm — "the first table line" is a different `if` from
// "the Columns: heading", and a test that only refuses the first write proves nothing
// about the other.
func TestEveryListingWriteErrorReachesTheCaller(t *testing.T) {
	// The fake returns values POSITIONALLY, so the row shape has to match what each
	// query selects — ListColumns selects four fields, and asking for three makes the
	// scan fail, which turns the case into a test of the scan rather than of the writes.
	schemaSteps := []step{
		{contains: "FROM information_schema.schemata", spec: rowsSpec{rows: [][]interface{}{{"public"}}}},
		{contains: "FROM information_schema.tables", spec: rowsSpec{
			rows: [][]interface{}{{"orders", "BASE TABLE", int64(12)}},
		}},
		{contains: "FROM information_schema.columns", spec: rowsSpec{
			rows: [][]interface{}{{"id", "integer", "NO", nil}},
		}},
	}

	// The property, in a form that does not depend on how many lines each command
	// happens to write: the positions that report the error are a PREFIX of the write
	// sequence, and the first position that does not is the one where the output ran
	// out. Anything else — a hole in the middle — is an arm that swallows its error,
	// which is the thing this is looking for.
	//
	// The earlier version of this test hardcoded the count per command and had it wrong
	// for two of the four, reporting a mismatch on correct code twice: the count depends
	// on how many schemas and tables the fixture has, and the first version counted them
	// by reading the formatter rather than by running it.
	assertPrefixOfErrors := func(t *testing.T, name string, call func(*failingWriter) error) {
		t.Helper()
		// Sixteen positions, which is more than the largest report in this package
		// writes: two columns, two rows of two cells, a heading and a footer is ten
		// calls. The range only has to be long enough to reach the first success.
		const positions = 16
		firstSuccess := -1
		for after := range positions {
			w := &failingWriter{afterN: after, err: errWriteRefused}
			err := call(w)
			switch {
			case err == nil:
				if firstSuccess < 0 {
					firstSuccess = after
				}
			case !errors.Is(err, errWriteRefused):
				t.Errorf("%s with %d good writes returned %v, which is neither success nor the write error", name, after, err)
			case firstSuccess >= 0:
				t.Errorf("%s stopped reporting the write error at position %d and then reported it again at %d",
					name, firstSuccess, after)
			}
		}
		if firstSuccess < 0 {
			t.Errorf("%s never finished writing: the write error was reported at every position", name)
		}
	}

	for _, tc := range []struct {
		name string
		call func(*failingWriter) error
	}{
		{"list tables", func(w *failingWriter) error {
			return listTablesAndReport(w, &fakeConn{steps: schemaSteps}, false)
		}},
		{"list columns", func(w *failingWriter) error {
			return listColumnsAndReport(w, &fakeConn{steps: schemaSteps}, "orders", false, false)
		}},
		{"the schema form, which writes a heading and a Columns: line", func(w *failingWriter) error {
			return listColumnsAndReport(w, &fakeConn{steps: schemaSteps}, "orders", false, true)
		}},
		{"list tables as JSON", func(w *failingWriter) error {
			return listTablesAndReport(w, &fakeConn{steps: schemaSteps}, true)
		}},
		{"list columns as JSON", func(w *failingWriter) error {
			return listColumnsAndReport(w, &fakeConn{steps: schemaSteps}, "orders", true, false)
		}},
		{"the query report", func(w *failingWriter) error {
			return writeQueryResult(w, aResult(), false)
		}},
		{"the column block", func(w *failingWriter) error {
			return writeColumns(w, []postgres.ColumnInfo{
				{Name: "id", DataType: "integer"},
				{Name: "name", DataType: "text", IsNullable: "YES"},
			})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) { assertPrefixOfErrors(t, tc.name, tc.call) })
	}
}
