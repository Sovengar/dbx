package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/spf13/cobra"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/drivers/postgres"
)

// --- the fake connection -----------------------------------------------------
//
// postgres.Conn is three methods wide and pgx.Rows is nine, so a whole fake
// database is about a hundred lines. That is the whole cost of testing every
// command above the connection, and it is why the interface is worth having.

// rowsSpec is a scripted answer: some columns and some rows.
type rowsSpec struct {
	rows [][]any
	err  error
	// iterErr is the error Next() stops with, which is how a mid-iteration
	// failure is reproduced.
	iterErr error
}

type fakeRows struct {
	spec rowsSpec
	pos  int
}

func (r *fakeRows) Close()                                       {}
func (r *fakeRows) Err() error                                   { return r.spec.iterErr }
func (r *fakeRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *fakeRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *fakeRows) RawValues() [][]byte                          { return nil }
func (r *fakeRows) Conn() *pgx.Conn                              { return nil }
func (r *fakeRows) TypeMap() *pgtype.Map                         { return nil }

func (r *fakeRows) Next() bool {
	if r.spec.iterErr != nil {
		return false
	}
	if r.pos >= len(r.spec.rows) {
		return false
	}
	r.pos++
	return true
}

func (r *fakeRows) Values() ([]any, error) {
	if r.spec.err != nil {
		return nil, r.spec.err
	}
	return r.spec.rows[r.pos-1], nil
}

func (r *fakeRows) Scan(dest ...any) error {
	if r.spec.err != nil {
		return r.spec.err
	}
	row := r.spec.rows[r.pos-1]
	if len(dest) != len(row) {
		return fmt.Errorf("fakeRows: %d destinations for %d columns", len(dest), len(row))
	}
	for i, d := range dest {
		switch p := d.(type) {
		case *string:
			*p = fmt.Sprint(row[i])
		case *int:
			*p = toInt(row[i])
		case **string:
			if row[i] == nil {
				*p = nil
			} else {
				s := fmt.Sprint(row[i])
				*p = &s
			}
		default:
			return fmt.Errorf("fakeRows: cannot scan into %T", d)
		}
	}
	return nil
}

func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	default:
		return 0
	}
}

// fakeConn answers each Query by picking the first step whose matcher hits.
type step struct {
	// contains is matched against the SQL. An empty matcher matches anything,
	// so it works as a catch-all last step.
	contains string
	spec     rowsSpec
	err      error
}

// queryAnswer is one statement's scripted result, used when the answer depends on
// WHICH statement is being run rather than on its shape.
type queryAnswer struct {
	rows [][]any
	err  error
}

type fakeConn struct {
	steps []step
	// perStatement answers a query that no `steps` entry matched, using the Nth
	// entry for the Nth such query. The schema queries are always answered by
	// `steps`, so a fixture scripts only the statements whose outcome it cares
	// about and does not have to count the setup queries.
	perStatement []queryAnswer
	name         string
	scanErr      error
	closed       int
	queried      []string
	missed       []string
	unmatched    int
	pingErr      error
	pinged       int
}

func (f *fakeConn) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	f.queried = append(f.queried, sql)
	for _, s := range f.steps {
		if s.contains == "" || strings.Contains(sql, s.contains) {
			if s.err != nil {
				return nil, s.err
			}
			return &fakeRows{spec: s.spec}, nil
		}
	}
	// Nothing matched by shape, so this statement is one whose outcome depends on
	// WHICH statement it is rather than on what it looks like. Scripted in order.
	f.unmatched++
	if n := f.unmatched; n <= len(f.perStatement) {
		a := f.perStatement[n-1]
		if a.err != nil {
			return nil, a.err
		}
		return &fakeRows{spec: rowsSpec{rows: a.rows}}, nil
	}
	f.missed = append(f.missed, sql)
	return nil, fmt.Errorf("fakeConn: no step matches this query")
}

func (f *fakeConn) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return fakeRow{name: f.name, err: f.scanErr}
}

func (f *fakeConn) Close(ctx context.Context) error {
	f.closed++
	return nil
}

func (f *fakeConn) Ping(ctx context.Context) error {
	f.pinged++
	return f.pingErr
}

type fakeRow struct {
	name string
	err  error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) == 1 {
		if p, ok := dest[0].(*string); ok {
			*p = r.name
			return nil
		}
	}
	return fmt.Errorf("fakeRow: cannot scan into %T", dest)
}

func toAnyRows(vals []string) [][]any {
	out := make([][]any, len(vals))
	for i, v := range vals {
		out[i] = []any{v}
	}
	return out
}

// --- writeQueryResult --------------------------------------------------------

// Scenario: La salida de texto son columnas de 20 celdas y un recuento.
//
// The exact bytes matter: this format is what scripts parsing `dbx query` read,
// so a padding change or a lost trailing newline is a breaking change with no
// error anywhere.
func TestWriteQueryResult_TextShapeIsPaddedToTwentyColumns(t *testing.T) {
	result := &postgres.QueryResult{
		Columns: []postgres.ColumnInfo{{Name: "id"}, {Name: "name"}},
		Rows:    [][]interface{}{{1, "ada"}, {2, "grace"}},
		Count:   2,
	}

	var out bytes.Buffer
	if err := writeQueryResult(&out, result, false); err != nil {
		t.Fatalf("writeQueryResult: %v", err)
	}

	want := "id                  name                \n" +
		"1                   ada                 \n" +
		"2                   grace               \n" +
		"\n2 rows\n"
	if out.String() != want {
		t.Errorf("output =\n%q\nwant\n%q", out.String(), want)
	}
}

// Scenario: La salida de texto usa %v para los valores, así que no los entrecomilla.
//
// The text mode is for a human; a program reads the JSON mode. Pinning it stops
// someone "fixing" the quoting and silently breaking every consumer.
func TestWriteQueryResult_TextShapeRendersValuesWithFmtVerbs(t *testing.T) {
	result := &postgres.QueryResult{
		Columns: []postgres.ColumnInfo{{Name: "v"}},
		Rows:    [][]interface{}{{nil}, {true}, {3.5}, {"txt"}},
		Count:   4,
	}

	var out bytes.Buffer
	if err := writeQueryResult(&out, result, false); err != nil {
		t.Fatalf("writeQueryResult: %v", err)
	}

	want := "v                   \n" +
		"<nil>               \n" +
		"true                \n" +
		"3.5                 \n" +
		"txt                 \n" +
		"\n4 rows\n"
	if out.String() != want {
		t.Errorf("output =\n%q\nwant\n%q", out.String(), want)
	}
}

// Scenario: Un resultado vacío imprime la cabecera y un cero.
//
// An empty result is a normal answer, not an error, and a script reading the last
// line needs the "0 rows" line to be there. The header line is still printed even
// with no columns, so the output opens with a blank line.
func TestWriteQueryResult_TextShapeHandlesAnEmptyResult(t *testing.T) {
	var out bytes.Buffer
	if err := writeQueryResult(&out, &postgres.QueryResult{}, false); err != nil {
		t.Fatalf("writeQueryResult: %v", err)
	}
	if got, want := out.String(), "\n\n0 rows\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}

	var headerOnly bytes.Buffer
	if err := writeQueryResult(&headerOnly, &postgres.QueryResult{
		Columns: []postgres.ColumnInfo{{Name: "id"}},
		Count:   0,
	}, false); err != nil {
		t.Fatalf("writeQueryResult: %v", err)
	}
	if !strings.Contains(headerOnly.String(), "id") {
		t.Errorf("output = %q, want the column header", headerOnly.String())
	}
}

// Scenario: La salida JSON lleva columnas, filas y recuento.
//
// The keys are the contract with whatever consumes `dbx query -j`.
func TestWriteQueryResult_JSONShapeCarriesColumnsRowsAndCount(t *testing.T) {
	result := &postgres.QueryResult{
		Columns: []postgres.ColumnInfo{{Name: "id"}, {Name: "name"}},
		Rows:    [][]interface{}{{1, "ada"}},
		Count:   1,
	}

	var out bytes.Buffer
	if err := writeQueryResult(&out, result, true); err != nil {
		t.Fatalf("writeQueryResult: %v", err)
	}

	var got struct {
		Columns []postgres.ColumnInfo `json:"columns"`
		Rows    [][]interface{}       `json:"rows"`
		Count   int                   `json:"count"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("the output is not valid JSON: %v\n%s", err, out.String())
	}
	if len(got.Columns) != 2 || got.Columns[0].Name != "id" || got.Columns[1].Name != "name" {
		t.Errorf("columns = %+v, want id and name", got.Columns)
	}
	if len(got.Rows) != 1 || got.Rows[0][1] != "ada" {
		t.Errorf("rows = %v, want one row with ada in it", got.Rows)
	}
	if got.Count != 1 {
		t.Errorf("count = %d, want 1", got.Count)
	}
	if !strings.Contains(out.String(), "\n  \"columns\"") {
		t.Errorf("the output is not indented with two spaces: %s", out.String())
	}
	// The two shapes must not have drifted together.
	if strings.Contains(out.String(), "rows\n") {
		t.Errorf("the JSON output contains the text mode's trailing line: %s", out.String())
	}
}

// Scenario: Un fallo de escritura se propaga, no se traga.
//
// The output is os.Stdout, which is a pipe when dbx is used in a script, and a
// pipe whose reader has gone away fails on write. Reporting that as success means
// the command exits 0 having printed nothing.
func TestWriteQueryResult_ReportsAWriteFailure(t *testing.T) {
	boom := errors.New("broken pipe")
	result := &postgres.QueryResult{
		Columns: []postgres.ColumnInfo{{Name: "id"}},
		Rows:    [][]interface{}{{1}},
		Count:   1,
	}
	for _, asJSON := range []bool{false, true} {
		err := writeQueryResult(errWriter{err: boom}, result, asJSON)
		if err == nil {
			t.Errorf("json=%v: a failing writer returned no error", asJSON)
			continue
		}
		if !errors.Is(err, boom) {
			t.Errorf("json=%v: error = %v, want it to wrap %v", asJSON, err, boom)
		}
	}
}

type errWriter struct{ err error }

func (w errWriter) Write(p []byte) (int, error) { return 0, w.err }

// --- readSQL -----------------------------------------------------------------

// Scenario: La entrada se lee entera y se recorta.
//
// `echo "SELECT 1" | dbx pipe` sends a trailing newline, which is not valid SQL on
// its own, so the trim is what makes the documented invocation work.
func TestReadSQL_ReadsTheWholeStreamAndTrimsIt(t *testing.T) {
	for _, tc := range []struct {
		name, in, want string
	}{
		{"a single line", "SELECT 1", "SELECT 1"},
		{"a trailing newline", "SELECT 1\n", "SELECT 1"},
		{"a trailing newline and spaces", "  SELECT 1  \n\n", "SELECT 1"},
		{"a leading newline", "\nSELECT 1", "SELECT 1"},
		{"several statements", "SELECT 1;\nSELECT 2;\n", "SELECT 1;\nSELECT 2;"},
		{"only whitespace", "   \n\t\n  ", ""},
		{"empty", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := readSQL(strings.NewReader(tc.in))
			if err != nil {
				t.Fatalf("readSQL: %v", err)
			}
			if got != tc.want {
				t.Errorf("readSQL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// Scenario: La última parte de la entrada llega junto con el fin del flujo.
//
// A pipe delivers its final chunk together with io.EOF, so a loop that breaks on
// the error before keeping the bytes it just read silently truncates the last
// statement — usually the one being piped. This is the exact case the previous
// hand-rolled loop got wrong.
func TestReadSQL_KeepsTheDataThatArrivesWithEOF(t *testing.T) {
	r := &chunkReader{chunks: [][]byte{
		[]byte("SELECT "),
		[]byte("1"),
		[]byte(" FROM "),
		[]byte("users"),
	}}
	got, err := readSQL(r)
	if err != nil {
		t.Fatalf("readSQL: %v", err)
	}
	if want := "SELECT 1 FROM users"; got != want {
		t.Errorf("readSQL = %q, want %q", got, want)
	}
}

// Scenario: Una lectura de un byte cada vez da el mismo resultado.
//
// A pipe is a stream, not a buffer: a large statement arrives in many reads, and
// anything that assumes a full buffer loses most of it.
func TestReadSQL_SurvivesInputArrivingOneByteAtATime(t *testing.T) {
	long := "SELECT " + strings.Repeat("a", 5000) + " FROM t"
	r := &chunkReader{}
	for i := 0; i < len(long); i++ {
		r.chunks = append(r.chunks, []byte(long[i:i+1]))
	}
	got, err := readSQL(r)
	if err != nil {
		t.Fatalf("readSQL: %v", err)
	}
	if got != long {
		t.Errorf("readSQL returned %d bytes, want all %d", len(got), len(long))
	}
}

// Scenario: Un fallo real de lectura se informa.
//
// Executing a truncated statement is worse than refusing to run it.
func TestReadSQL_ReportsAReadFailure(t *testing.T) {
	boom := errors.New("input device is gone")
	r := &chunkReader{chunks: [][]byte{[]byte("SELECT 1")}, err: boom}

	got, err := readSQL(r)
	if err == nil {
		t.Fatalf("readSQL returned %q and no error, want the read failure", got)
	}
	if !errors.Is(err, boom) {
		t.Errorf("error = %v, want it to wrap %v", err, boom)
	}
	if got != "" {
		t.Errorf("readSQL returned %q alongside the error, want nothing", got)
	}
}

type chunkReader struct {
	chunks [][]byte
	err    error
}

func (c *chunkReader) Read(p []byte) (int, error) {
	if len(c.chunks) == 0 {
		if c.err != nil {
			return 0, c.err
		}
		return 0, io.EOF
	}
	n := copy(p, c.chunks[0])
	c.chunks[0] = c.chunks[0][n:]
	if len(c.chunks[0]) == 0 {
		c.chunks = c.chunks[1:]
	}
	return n, nil
}

// --- writeSchemaOutput -------------------------------------------------------

// Scenario: Sin `-o` el JSON va a la salida estándar.
//
// This is the default and the documented behaviour: the export is meant to be
// piped into an LLM, so the JSON has to be the only thing on stdout.
func TestWriteSchemaOutput_WithoutAFileWritesToTheStream(t *testing.T) {
	data := []byte(`{"database":"app"}`)

	var stdout, stderr bytes.Buffer
	if err := writeSchemaOutput(data, "", &stdout, &stderr); err != nil {
		t.Fatalf("writeSchemaOutput: %v", err)
	}
	if got := stdout.String(); got != string(data)+"\n" {
		t.Errorf("stdout = %q, want the JSON and a newline", got)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want nothing: the confirmation is for the file branch only", stderr.String())
	}
}

// Scenario: Con `-o` el JSON va al archivo y se confirma por stderr.
//
// The confirmation goes to stderr so that `dbx context > x` and `dbx context -o x`
// both produce clean output on whichever stream the user is redirecting.
func TestWriteSchemaOutput_WithAFileWritesTheFileAndConfirmsOnStderr(t *testing.T) {
	data := []byte(`{"database":"app","schemas":[]}`)
	path := filepath.Join(t.TempDir(), "schema.json")

	var stdout, stderr bytes.Buffer
	if err := writeSchemaOutput(data, path, &stdout, &stderr); err != nil {
		t.Fatalf("writeSchemaOutput: %v", err)
	}

	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the file back: %v", err)
	}
	if string(written) != string(data) {
		t.Errorf("the file holds %q, want %q", written, data)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want nothing when writing to a file", stdout.String())
	}
	if !strings.Contains(stderr.String(), path) {
		t.Errorf("stderr = %q, want it to name the file that was written", stderr.String())
	}
}

// Scenario: Un archivo que no se puede escribir es un error.
//
// The command is the only thing standing between a mistyped `-o` and a silent no-op
// that looks like a successful export.
func TestWriteSchemaOutput_AnUnwritablePathIsAnError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-dir", "schema.json")

	var stdout, stderr bytes.Buffer
	err := writeSchemaOutput([]byte("{}"), missing, &stdout, &stderr)
	if err == nil {
		t.Fatal("writing under a missing directory returned no error")
	}
	if !strings.Contains(err.Error(), "failed to write file") {
		t.Errorf("error = %v, want it to say the write failed", err)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want nothing when the write failed", stdout.String())
	}
}

// Scenario: Escribir a una ruta que es un directorio es un error.
//
// `-o /tmp` looks plausible and is not a file, so this is a mistake a user
// actually makes.
func TestWriteSchemaOutput_ADirectoryIsNotAFile(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := writeSchemaOutput([]byte("{}"), t.TempDir(), &stdout, &stderr); err == nil {
		t.Fatal("writing to a directory returned no error")
	}
}

// --- getDatabaseName ---------------------------------------------------------

// Scenario: El nombre de la base viene de la propia conexión.
//
// Read back from the server rather than taken from the config, so an aliased DSN
// still reports what is actually connected.
func TestGetDatabaseName_ReadsTheNameFromTheConnection(t *testing.T) {
	if got := getDatabaseName(&fakeConn{name: "shop"}); got != "shop" {
		t.Errorf("getDatabaseName = %q, want %q", got, "shop")
	}
}

// Scenario: Si la consulta falla el nombre es "unknown", y la exportación sigue.
//
// current_database() failing means the connection is not usable, but the export is
// still worth attempting: the name is metadata inside the JSON and the schema is
// the payload. Giving up here turns a cosmetic problem into a failed command.
func TestGetDatabaseName_FallsBackToUnknown(t *testing.T) {
	for _, err := range []error{errors.New("connection closed"), pgx.ErrNoRows} {
		if got := getDatabaseName(&fakeConn{scanErr: err}); got != "unknown" {
			t.Errorf("getDatabaseName with a failing scan = %q, want %q", got, "unknown")
		}
	}
	if got := getDatabaseName(&fakeConn{name: ""}); got != "" {
		t.Errorf("getDatabaseName with an empty name = %q, want the empty name, not %q", got, "unknown")
	}
}

// --- runTUI ------------------------------------------------------------------

type stubModel struct{}

func (stubModel) Init() tea.Cmd                       { return nil }
func (stubModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return stubModel{}, nil }
func (stubModel) View() tea.View                      { return tea.NewView("") }

// withSeams swaps the process-level dependencies for the duration of a test and
// puts them back, so the tests cannot leak into each other.
func withSeams(t *testing.T, cfg *config.Config, cfgErr error, run func(tea.Model) (tea.Model, error)) {
	t.Helper()
	oldLoad, oldBuild, oldRun := loadConfig, buildModel, runProgram
	t.Cleanup(func() { loadConfig, buildModel, runProgram = oldLoad, oldBuild, oldRun })

	loadConfig = func() (*config.Config, error) { return cfg, cfgErr }
	buildModel = func(*config.Config) tea.Model { return stubModel{} }
	runProgram = run
}

// Scenario: Un config que no carga impide arrancar la TUI.
//
// Failing before the terminal is taken over is the whole point: an error printed
// after the alternate screen is entered leaves the terminal broken.
func TestRunTUI_RefusesToStartWithoutAConfig(t *testing.T) {
	boom := errors.New("permission denied")
	started := false
	withSeams(t, nil, boom, func(tea.Model) (tea.Model, error) {
		started = true
		return nil, nil
	})

	err := runTUI(rootCmd, nil)
	if err == nil {
		t.Fatal("runTUI with an unloadable config returned no error")
	}
	if !strings.Contains(err.Error(), "failed to load config") {
		t.Errorf("error = %v, want it to say the config failed to load", err)
	}
	if !errors.Is(err, boom) {
		t.Errorf("error = %v, want it to wrap %v", err, boom)
	}
	if started {
		t.Error("the TUI was started even though the config could not be loaded")
	}
}

// Scenario: Un programa que sale con error se informa como fallo de la TUI.
//
// tea.Program.Run reports a model error here, and losing it would mean dbx exits 0
// after a crash.
func TestRunTUI_ReportsAFailedProgram(t *testing.T) {
	boom := errors.New("terminal too small")
	withSeams(t, &config.Config{}, nil, func(tea.Model) (tea.Model, error) {
		return nil, boom
	})

	err := runTUI(rootCmd, nil)
	if err == nil {
		t.Fatal("runTUI with a failing program returned no error")
	}
	if !strings.Contains(err.Error(), "failed to run TUI") {
		t.Errorf("error = %v, want it to say the TUI failed to run", err)
	}
	if !errors.Is(err, boom) {
		t.Errorf("error = %v, want it to wrap %v", err, boom)
	}
}

// Scenario: Cerrar la TUI limpiamente no es un error.
//
// Quitting normally is the common case and must exit 0, otherwise every ordinary
// quit looks like a crash in a script.
func TestRunTUI_ACleanExitIsNotAnError(t *testing.T) {
	withSeams(t, &config.Config{}, nil, func(m tea.Model) (tea.Model, error) { return m, nil })
	if err := runTUI(rootCmd, nil); err != nil {
		t.Errorf("runTUI = %v, want nil on a clean exit", err)
	}
}

// Scenario: El modelo se construye a partir del config cargado.
//
// The config is loaded once and handed to the model; the TUI does not reload it,
// so a change between the two would show a config the user never chose.
func TestRunTUI_BuildsTheModelFromTheLoadedConfig(t *testing.T) {
	cfg := &config.Config{}
	oldLoad, oldBuild, oldRun := loadConfig, buildModel, runProgram
	t.Cleanup(func() { loadConfig, buildModel, runProgram = oldLoad, oldBuild, oldRun })

	loads := 0
	loadConfig = func() (*config.Config, error) { loads++; return cfg, nil }
	var built *config.Config
	buildModel = func(c *config.Config) tea.Model { built = c; return stubModel{} }
	runProgram = func(m tea.Model) (tea.Model, error) { return m, nil }

	if err := runTUI(rootCmd, nil); err != nil {
		t.Fatalf("runTUI: %v", err)
	}
	if loads != 1 {
		t.Errorf("the config was loaded %d times, want once", loads)
	}
	if built != cfg {
		t.Error("the model was built from a different config than the one that was loaded")
	}
}

// --- execute -----------------------------------------------------------------

// Scenario: El error de la línea de comandos sale de execute().
//
// This is why execute exists: `Execute` wraps it in os.Exit(1), which no test can
// survive, so the decision has to be observable before the exit.
func TestExecute_ReturnsTheCommandsErrorInsteadOfExiting(t *testing.T) {
	old := rootCmd
	defer func() { rootCmd = old }()

	boom := errors.New("connection not found: ghost")
	rootCmd = &cobra.Command{Use: "dbx"}
	rootCmd.RunE = func(*cobra.Command, []string) error { return boom }
	rootCmd.SetArgs([]string{})
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)

	err := execute()
	if err == nil {
		t.Fatal("execute swallowed the command's error")
	}
	if !errors.Is(err, boom) {
		t.Errorf("error = %v, want the command's own error %v, unmodified", err, boom)
	}
}

// Scenario: Una invocación válida no es un error.
//
// The happy path matters as much as the failure: execute returning an error for a
// command that worked would make every successful run look like a failure.
func TestExecute_AValidInvocationIsNotAnError(t *testing.T) {
	old := rootCmd
	defer func() { rootCmd = old }()

	ran := false
	rootCmd = &cobra.Command{Use: "dbx"}
	rootCmd.RunE = func(*cobra.Command, []string) error { ran = true; return nil }
	rootCmd.SetArgs([]string{})
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)

	if err := execute(); err != nil {
		t.Fatalf("execute = %v, want nil", err)
	}
	if !ran {
		t.Error("execute did not run the command")
	}
}

// --- dsnFor ------------------------------------------------------------------

// Scenario: La configuración global aporta el DSN, y una URL gana a las partes.
//
// A user who pasted a full URL into their config means it, and rebuilding one from
// the discrete fields would quietly drop the sslmode or the password.
func TestDSNFor_PrefersAConfiguredURL(t *testing.T) {
	cfg := &config.Config{Connections: []config.ConnectionConfig{
		{Name: "one", URL: "postgres://u:p@h:5432/db?sslmode=require"},
		{Name: "two", User: "u", Host: "h", Port: 5432, Database: "db"},
	}}

	dsn, err := dsnFor("one", cfg)
	if err != nil {
		t.Fatalf("dsnFor: %v", err)
	}
	if want := "postgres://u:p@h:5432/db?sslmode=require"; dsn != want {
		t.Errorf("dsn = %q, want the configured URL %q", dsn, want)
	}
}

// Scenario: Sin URL el DSN se arma con las partes sueltas.
//
// This is what a config written by hand with host/port/database looks like, and
// getting the shape wrong connects to the wrong database.
func TestDSNFor_BuildsADSNFromTheDiscreteFields(t *testing.T) {
	cfg := &config.Config{Connections: []config.ConnectionConfig{
		{Name: "local", User: "ada", Host: "localhost", Port: 5433, Database: "shop"},
	}}

	dsn, err := dsnFor("local", cfg)
	if err != nil {
		t.Fatalf("dsnFor: %v", err)
	}
	if want := "postgres://ada@localhost:5433/shop"; dsn != want {
		t.Errorf("dsn = %q, want %q", dsn, want)
	}
}

// Scenario: Sin nombre se usa la primera conexión, no ninguna.
//
// `dbx query` with no `-c` is the documented shorthand, so an empty name has to
// select the first entry rather than fail.
func TestDSNFor_AnEmptyNamePicksTheFirstConnection(t *testing.T) {
	cfg := &config.Config{Connections: []config.ConnectionConfig{
		{Name: "first", URL: "postgres://first/db"},
		{Name: "second", URL: "postgres://second/db"},
	}}

	dsn, err := dsnFor("", cfg)
	if err != nil {
		t.Fatalf("dsnFor with no name: %v", err)
	}
	if want := "postgres://first/db"; dsn != want {
		t.Errorf("dsn = %q, want the first connection %q", dsn, want)
	}
}

// Scenario: Un nombre que no existe es un error que nombra el nombre.
//
// The message has to include what was asked for, or the user has to guess which
// of their connections is misspelled.
func TestDSNFor_AnUnknownNameIsAnErrorNamingIt(t *testing.T) {
	cfg := &config.Config{Connections: []config.ConnectionConfig{{Name: "real", URL: "postgres://x/db"}}}

	_, err := dsnFor("imaginary", cfg)
	if err == nil {
		t.Fatal("dsnFor with an unknown name returned no error")
	}
	if !strings.Contains(err.Error(), "imaginary") {
		t.Errorf("error = %v, want it to name the connection that was asked for", err)
	}
}

// Scenario: Una configuración sin conexiones es un error, no un DSN vacío.
//
// An empty DSN would be passed to pgx, which fails with a message about a
// connection string instead of about the missing configuration.
func TestDSNFor_NoConnectionsAtAllIsAnError(t *testing.T) {
	_, err := dsnFor("", &config.Config{})
	if err == nil {
		t.Fatal("dsnFor with no connections returned no error")
	}
	if !strings.Contains(err.Error(), "connection not found") {
		t.Errorf("error = %v, want it to say the connection was not found", err)
	}
}

// --- the connection path -----------------------------------------------------

// Scenario: Un ping que falla cierra la conexión y no la devuelve.
//
// A connection that connected but cannot answer a ping is not usable, and leaving
// it open leaks a socket. The error has to name ping, not connect, or the user
// chases the wrong problem.
func TestGetConnection_AFailedPingClosesTheConnectionAndFails(t *testing.T) {
	boom := errors.New("server is starting up")
	conn := &fakeConn{pingErr: boom}
	withConnect(t, conn, nil)

	_, err := getConnection(testCommand(t))
	if err == nil {
		t.Fatal("getConnection with a failing ping returned no error")
	}
	if !strings.Contains(err.Error(), "failed to ping") {
		t.Errorf("error = %v, want it to say the ping failed", err)
	}
	if conn.closed != 1 {
		t.Errorf("the connection was closed %d times, want once", conn.closed)
	}
}

// Scenario: Conectar bien y hacer ping bien devuelve la conexión.
//
// The happy path, asserted so the clamp above is not the only thing pinned.
func TestGetConnection_APassingPingReturnsTheConnection(t *testing.T) {
	conn := &fakeConn{}
	withConnect(t, conn, nil)

	got, err := getConnection(testCommand(t))
	if err != nil {
		t.Fatalf("getConnection: %v", err)
	}
	if got != postgres.Conn(conn) {
		t.Error("getConnection returned a different connection")
	}
	if conn.pinged != 1 {
		t.Errorf("the connection was pinged %d times, want once", conn.pinged)
	}
	if conn.closed != 0 {
		t.Errorf("a live connection was closed %d times", conn.closed)
	}
}

// Scenario: Si conectar falla no se hace ping.
//
// A ping against a connection that does not exist would be a second, more
// confusing error on top of the first.
func TestGetConnection_ADialFailureIsNotPinged(t *testing.T) {
	boom := errors.New("connection refused")
	withConnect(t, nil, boom)

	_, err := getConnection(testCommand(t))
	if err == nil {
		t.Fatal("getConnection with a failing dial returned no error")
	}
	if !strings.Contains(err.Error(), "failed to connect") {
		t.Errorf("error = %v, want it to say the connection failed", err)
	}
	if !errors.Is(err, boom) {
		t.Errorf("error = %v, want it to wrap %v", err, boom)
	}
}

// withConnect installs a fake pgx dialer and a config with one connection, so
// getConnection gets past its two lookups and reaches the dial.
//
// Both seams are needed: findLocalDSN reads a real .dbx.toml off disk, and
// without a config entry the function would stop at "connection not found" before
// it ever dialled, which is exactly the state these tests are not about.
func withConnect(t *testing.T, conn *fakeConn, dialErr error) {
	t.Helper()
	oldDial, oldLoad := pgxConnect, loadConfig
	t.Cleanup(func() { pgxConnect, loadConfig = oldDial, oldLoad })

	pgxConnect = func(ctx context.Context, dsn string) (pgconnPing, error) {
		if dialErr != nil {
			return nil, dialErr
		}
		return conn, nil
	}
	loadConfig = func() (*config.Config, error) {
		return &config.Config{Connections: []config.ConnectionConfig{
			{Name: "test", URL: "postgres://u@h/db"},
		}}, nil
	}
}

// testCommand is a bare cobra command carrying the -c flag the code reads.
func testCommand(t *testing.T) *cobra.Command {
	t.Helper()
	c := &cobra.Command{Use: "x"}
	c.Flags().StringP("connection", "c", "", "")
	return c
}
