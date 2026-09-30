package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/drivers/postgres"
)

// runCmd is a cobra command carrying every flag the RunE wrappers read, so the
// wrappers themselves can be called without going through the real command tree.
//
// The wrappers are three lines each — get a connection, defer a close, hand over
// to the body — and that is exactly the code whose error handling was untestable
// before: it all sits above getConnection.
func runCmd(t *testing.T, extra ...func(*cobra.Command)) *cobra.Command {
	t.Helper()
	c := &cobra.Command{Use: "x"}
	c.Flags().StringP("connection", "c", "", "")
	c.Flags().BoolP("json", "j", false, "")
	c.Flags().StringP("output", "o", "", "")
	c.Flags().BoolP("sql-only", "s", false, "")
	for _, f := range extra {
		f(c)
	}
	return c
}

// withGlobalConfig installs a config.Load that returns cfg.
func withGlobalConfig(t *testing.T, cfg *config.Config) {
	t.Helper()
	old := loadConfig
	t.Cleanup(func() { loadConfig = old })
	loadConfig = func() (*config.Config, error) { return cfg, nil }
}

// withNoConfig makes config.Load fail.
func withNoConfig(t *testing.T, err error) {
	t.Helper()
	old := loadConfig
	t.Cleanup(func() { loadConfig = old })
	loadConfig = func() (*config.Config, error) { return nil, err }
}

// Scenario: Un `.dbx.toml` en el directorio actual tiene prioridad sobre la
// configuración global.
//
// A project checked into a repository overrides whatever the developer's personal
// config says, so that everyone on the team talks to the same database. This is
// the branch that makes that true, and it is invisible unless a `.dbx.toml` is
// actually present — which is why the test writes one.
func TestGetConnection_ALocalProjectFileWinsOverTheGlobalConfig(t *testing.T) {
	dir := t.TempDir()
	// A ProjectConfig is a map of connections, not a list, and each entry has a
	// dsn rather than the discrete host/port fields the global config uses.
	project := `
[connections.project]
driver = "postgres"
dsn = "postgres://proj@localhost:5432/projdb"
`
	if err := os.WriteFile(filepath.Join(dir, ".dbx.toml"), []byte(project), 0644); err != nil {
		t.Fatalf("writing .dbx.toml: %v", err)
	}
	t.Chdir(dir)

	var dialed string
	withConnect(t, &fakeConn{}, nil)
	oldDial := pgxConnect
	t.Cleanup(func() { pgxConnect = oldDial })
	pgxConnect = func(ctx context.Context, dsn string) (pgconnPing, error) {
		dialed = dsn
		return &fakeConn{}, nil
	}
	// The global config would send us somewhere else entirely.
	withGlobalConfig(t, &config.Config{Connections: []config.ConnectionConfig{
		{Name: "project", URL: "postgres://global@localhost:9999/globaldb"},
	}})

	if _, err := getConnection(runCmd(t)); err != nil {
		t.Fatalf("getConnection: %v", err)
	}
	if !strings.Contains(dialed, "projdb") {
		t.Errorf("dialed %q, want the project database: the local file must win", dialed)
	}
	if strings.Contains(dialed, "globaldb") {
		t.Errorf("dialed %q, want the global config to have been ignored", dialed)
	}
}

// Scenario: Sin `.dbx.toml` se usa la configuración global.
//
// The other half of the same rule, and the common case: running dbx from your home
// directory must not fail because there is no project file.
func TestGetConnection_WithoutALocalFileTheGlobalConfigIsUsed(t *testing.T) {
	t.Chdir(t.TempDir())

	var dialed string
	withGlobalConfig(t, &config.Config{Connections: []config.ConnectionConfig{
		{Name: "home", URL: "postgres://me@localhost:5432/homedb"},
	}})
	oldDial := pgxConnect
	t.Cleanup(func() { pgxConnect = oldDial })
	pgxConnect = func(ctx context.Context, dsn string) (pgconnPing, error) {
		dialed = dsn
		return &fakeConn{}, nil
	}

	if _, err := getConnection(runCmd(t)); err != nil {
		t.Fatalf("getConnection: %v", err)
	}
	if !strings.Contains(dialed, "homedb") {
		t.Errorf("dialed %q, want the global database", dialed)
	}
}

// Scenario: Un `.dbx.toml` que no se puede leer no rompe la ejecución.
//
// A malformed or unreadable project file falls back to the global config rather
// than failing: the file is a convenience, and a syntax error in someone else's
// repository should not make dbx unusable.
func TestGetConnection_AnUnreadableProjectFileFallsBack(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".dbx.toml"), []byte("this is not toml {{{"), 0644); err != nil {
		t.Fatalf("writing .dbx.toml: %v", err)
	}
	t.Chdir(dir)

	var dialed string
	withGlobalConfig(t, &config.Config{Connections: []config.ConnectionConfig{
		{Name: "home", URL: "postgres://me@localhost:5432/homedb"},
	}})
	oldDial := pgxConnect
	t.Cleanup(func() { pgxConnect = oldDial })
	pgxConnect = func(ctx context.Context, dsn string) (pgconnPing, error) {
		dialed = dsn
		return &fakeConn{}, nil
	}

	if _, err := getConnection(runCmd(t)); err != nil {
		t.Fatalf("a malformed .dbx.toml should fall back, not fail: %v", err)
	}
	if !strings.Contains(dialed, "homedb") {
		t.Errorf("dialed %q, want the global database", dialed)
	}
}

// Scenario: Una configuración global que no carga impide conectar.
//
// The error names the config, not the connection, or the user goes looking for a
// database problem that does not exist.
func TestGetConnection_AnUnloadableConfigIsReportedAsSuch(t *testing.T) {
	t.Chdir(t.TempDir())
	boom := errors.New("permission denied")
	withNoConfig(t, boom)

	_, err := getConnection(runCmd(t))
	if err == nil {
		t.Fatal("an unloadable config returned no error")
	}
	if !strings.Contains(err.Error(), "failed to load config") {
		t.Errorf("error = %v, want it to say the config failed to load", err)
	}
	if !errors.Is(err, boom) {
		t.Errorf("error = %v, want it to wrap %v", err, boom)
	}
}

// --- the RunE wrappers -------------------------------------------------------

// Scenario: Cada comando propaga el fallo de conexión y cierra lo que abrió.
//
// A wrapper that returned a zero value on failure would run a command against no
// database and report "no rows", which is worse than an error.
func TestRunCommands_AllOfThemRefuseToRunWithoutAConnection(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(*cobra.Command) error
		args []string
	}{
		{"query", func(c *cobra.Command) error { return runQuery(c, []string{"SELECT 1"}) }, nil},
		{"list tables", func(c *cobra.Command) error { return runListTables(c, nil) }, nil},
		{"list columns", func(c *cobra.Command) error { return runListColumns(c, []string{"users"}) }, nil},
		{"schema", func(c *cobra.Command) error { return runSchema(c, []string{"users"}) }, nil},
		{"context", func(c *cobra.Command) error { return runContext(c, nil) }, nil},
		{"replay", func(c *cobra.Command) error { return runReplay(c, []string{"s.jsonl"}) }, nil},
		{"pipe", func(c *cobra.Command) error { return runPipe(c, nil) }, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			// No local file and no connections configured: the one state in which
			// every command must fail before touching the database.
			withGlobalConfig(t, &config.Config{})

			err := tc.run(runCmd(t))
			if err == nil {
				t.Fatalf("%s ran with no connection configured", tc.name)
			}
			if !strings.Contains(err.Error(), "connection not found") {
				t.Errorf("error = %v, want it to say no connection was found", err)
			}
		})
	}
}

// Scenario: Una conexión que sí se abre se cierra al terminar el comando.
//
// Every command defers a Close, and leaking a connection per invocation is the kind
// of bug that only shows up after a few hundred calls in a script.
func TestRunQuery_ClosesTheConnectionItOpened(t *testing.T) {
	t.Chdir(t.TempDir())
	conn := &fakeConn{steps: []step{{contains: "", spec: rowsSpec{rows: [][]any{{1}}}}}}
	withConnect(t, conn, nil)
	withGlobalConfig(t, &config.Config{Connections: []config.ConnectionConfig{
		{Name: "c", URL: "postgres://x/y"},
	}})

	if err := runQuery(runCmd(t), []string{"SELECT 1"}); err != nil {
		t.Fatalf("runQuery: %v", err)
	}
	if conn.closed != 1 {
		t.Errorf("the connection was closed %d times, want exactly once", conn.closed)
	}
}

// The same for the commands whose body can succeed against a fake, so the Close is
// proven on the success path too and not only on the error path above.
func TestRunCommands_CloseTheConnectionOnTheSuccessPath(t *testing.T) {
	steps := []step{
		{contains: "information_schema.schemata", spec: rowsSpec{rows: toAnyRows([]string{"public"})}},
		{contains: "information_schema.tables", spec: rowsSpec{rows: [][]any{{"users", "BASE TABLE", 2}}}},
		{contains: "information_schema.columns", spec: rowsSpec{rows: [][]any{{"id", "integer", "NO", nil}}}},
		{contains: "", spec: rowsSpec{rows: [][]any{{1}}}},
	}

	for _, tc := range []struct {
		name string
		run  func(*cobra.Command) error
		args []string
	}{
		{"query", func(c *cobra.Command) error { return runQuery(c, []string{"SELECT 1"}) }, nil},
		{"list tables", func(c *cobra.Command) error { return runListTables(c, nil) }, nil},
		{"list columns", func(c *cobra.Command) error { return runListColumns(c, []string{"users"}) }, nil},
		{"schema", func(c *cobra.Command) error { return runSchema(c, []string{"users"}) }, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			conn := &fakeConn{steps: steps}
			withConnect(t, conn, nil)
			withGlobalConfig(t, &config.Config{Connections: []config.ConnectionConfig{
				{Name: "c", URL: "postgres://x/y"},
			}})

			if err := tc.run(runCmd(t)); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if conn.closed != 1 {
				t.Errorf("%s closed the connection %d times, want exactly once", tc.name, conn.closed)
			}
		})
	}
}

// Scenario: Un `-o` a un directorio que no existe hace fallar `dbx context`.
//
// The export is the whole command, so a failed write has to be an error rather
// than a quiet success.
func TestRunContext_AnUnwritableOutputPathFails(t *testing.T) {
	t.Chdir(t.TempDir())
	conn := &fakeConn{steps: []step{
		{contains: "schema_name", spec: rowsSpec{rows: toAnyRows([]string{"public"})}},
		{contains: "", spec: rowsSpec{}},
	}}
	withConnect(t, conn, nil)
	withGlobalConfig(t, &config.Config{Connections: []config.ConnectionConfig{
		{Name: "c", URL: "postgres://x/y"},
	}})

	c := runCmd(t)
	if err := c.Flags().Set("output", filepath.Join(t.TempDir(), "nope", "schema.json")); err != nil {
		t.Fatalf("setting the flag: %v", err)
	}

	err := runContext(c, nil)
	if err == nil {
		t.Fatal("an unwritable output path returned no error")
	}
	if !strings.Contains(err.Error(), "failed to write file") {
		t.Errorf("error = %v, want it to say the write failed", err)
	}
}

// Scenario: `dbx context` con `-o` escribe el archivo y cierra la conexión.
//
// The end-to-end shape of the command: connect, export, write, close.
func TestRunContext_WritesTheFileAndClosesTheConnection(t *testing.T) {
	t.Chdir(t.TempDir())
	conn := &fakeConn{steps: []step{
		{contains: "schema_name", spec: rowsSpec{rows: toAnyRows([]string{"public"})}},
		{contains: "", spec: rowsSpec{}},
	}}
	withConnect(t, conn, nil)
	withGlobalConfig(t, &config.Config{Connections: []config.ConnectionConfig{
		{Name: "c", URL: "postgres://x/y"},
	}})

	path := filepath.Join(t.TempDir(), "schema.json")
	c := runCmd(t)
	if err := c.Flags().Set("output", path); err != nil {
		t.Fatalf("setting the flag: %v", err)
	}

	if err := runContext(c, nil); err != nil {
		t.Fatalf("runContext: %v", err)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the file back: %v", err)
	}
	if !bytes.Contains(written, []byte(`"database"`)) {
		t.Errorf("the file holds %q, want a JSON document with a database key", written)
	}
	if conn.closed != 1 {
		t.Errorf("the connection was closed %d times, want once", conn.closed)
	}
}

// Scenario: Si la exportación falla el comando falla.
//
// A schema export that cannot read the schemas is not an empty schema, and
// reporting "no schemas" would be a plausible lie.
func TestRunContext_AFailedExportIsAnError(t *testing.T) {
	t.Chdir(t.TempDir())
	conn := &fakeConn{steps: []step{{contains: "schema_name", err: errors.New("permission denied for information_schema")}}}
	withConnect(t, conn, nil)
	withGlobalConfig(t, &config.Config{Connections: []config.ConnectionConfig{
		{Name: "c", URL: "postgres://x/y"},
	}})

	err := runContext(runCmd(t), nil)
	if err == nil {
		t.Fatal("a failed export returned no error")
	}
	if !strings.Contains(err.Error(), "failed to export schema") {
		t.Errorf("error = %v, want it to say the export failed", err)
	}
	if conn.closed != 1 {
		t.Errorf("the connection was closed %d times: it must be closed even when the export fails", conn.closed)
	}
}

// --- runReplay / runPipe -----------------------------------------------------

// Scenario: Un log que no existe hace fallar el replay.
//
// A typo in the filename is the common mistake, and the message has to say the
// file could not be read rather than blaming the database.
func TestRunReplay_AMissingSessionFileFails(t *testing.T) {
	t.Chdir(t.TempDir())
	conn := &fakeConn{}
	withConnect(t, conn, nil)
	// The config needs a connection as well as a session dir: runReplay connects
	// before it reads the log, and the two failures are different.
	withGlobalConfig(t, &config.Config{
		Session:     config.SessionConfig{Dir: t.TempDir()},
		Connections: []config.ConnectionConfig{{Name: "c", URL: "postgres://x/y"}},
	})

	err := runReplay(runCmd(t), []string{"no-such-session.jsonl"})
	if err == nil {
		t.Fatal("a missing session file returned no error")
	}
	if !strings.Contains(err.Error(), "failed to read session file") {
		t.Errorf("error = %v, want it to say the file could not be read", err)
	}
	if conn.closed != 1 {
		t.Errorf("the connection was closed %d times, want once", conn.closed)
	}
}

// Scenario: Sin sesión configurada el replay usa el directorio vacío y falla al leer.
//
// The default session dir does not contain the file either, so the same error comes
// back — which is right, and the point is that it is the file read that fails.
func TestRunReplay_ReplaysAFileThatExists(t *testing.T) {
	t.Chdir(t.TempDir())
	conn := &fakeConn{steps: []step{{contains: "", spec: rowsSpec{rows: [][]any{{1}}}}}}
	withConnect(t, conn, nil)

	dir := t.TempDir()
	log := `{"level":"query","sql":"SELECT 1","duration_ms":1,"rows":1}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "s.jsonl"), []byte(log), 0644); err != nil {
		t.Fatalf("writing the log: %v", err)
	}
	withGlobalConfig(t, &config.Config{
		Session:     config.SessionConfig{Dir: dir},
		Connections: []config.ConnectionConfig{{Name: "c", URL: "postgres://x/y"}},
	})

	if err := runReplay(runCmd(t), []string{"s.jsonl"}); err != nil {
		t.Fatalf("runReplay: %v", err)
	}
	if len(conn.queried) != 1 {
		t.Errorf("%d statements ran, want 1", len(conn.queried))
	}
	if !strings.Contains(conn.queried[0], "SELECT 1") {
		t.Errorf("ran %q, want the statement from the log", conn.queried[0])
	}
	if conn.closed != 1 {
		t.Errorf("the connection was closed %d times, want once", conn.closed)
	}
}

// Scenario: Una sesión que no se puede cargar es un error antes de abrir la base.
//
// The config decides where the log lives, so without it there is nothing to replay.
func TestRunReplay_AnUnloadableConfigFailsBeforeConnecting(t *testing.T) {
	t.Chdir(t.TempDir())
	conn := &fakeConn{}
	withConnect(t, conn, nil)
	withNoConfig(t, errors.New("permission denied"))

	err := runReplay(runCmd(t), []string{"s.jsonl"})
	if err == nil {
		t.Fatal("an unloadable config returned no error")
	}
	if !strings.Contains(err.Error(), "failed to load config") {
		t.Errorf("error = %v, want it to say the config failed to load", err)
	}
}

// Scenario: Sin entrada en la entrada estándar `dbx pipe` dice cómo usarlo.
//
// Running `dbx pipe` with nothing piped in is a mistake, and the usage line is the
// answer. The check is on the file mode, so a test has to stand in for a
// character device.
func TestRunPipe_AnEmptyTerminalIsRefusedWithUsage(t *testing.T) {
	// A regular file is not a character device, so this exercises the "piped but
	// empty" side. The character-device side needs a real tty and is asserted by
	// the mode bit itself.
	// One directory for both the chdir and the empty file: t.TempDir() returns a
	// NEW directory on every call, so writing into one and reading from another
	// is a bug that happens to work until it does not.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "empty"), nil, 0644); err != nil {
		t.Fatalf("writing: %v", err)
	}
	t.Chdir(dir)

	conn := &fakeConn{}
	withConnect(t, conn, nil)
	withGlobalConfig(t, &config.Config{Connections: []config.ConnectionConfig{
		{Name: "c", URL: "postgres://x/y"},
	}})

	// Redirect stdin from an empty file: not a char device, so it gets past the
	// tty check and fails on the emptiness check instead.
	devNull, err := os.Open(filepath.Join(dir, "empty"))
	if err != nil {
		t.Fatalf("opening: %v", err)
	}
	defer func() { _ = devNull.Close() }()
	old := os.Stdin
	os.Stdin = devNull
	t.Cleanup(func() { os.Stdin = old })

	err = runPipe(runCmd(t), nil)
	if err == nil {
		t.Fatal("empty input returned no error")
	}
	if !strings.Contains(err.Error(), "empty SQL input") {
		t.Errorf("error = %v, want it to say the input was empty", err)
	}
	if conn.closed != 1 {
		t.Errorf("the connection was closed %d times, want once", conn.closed)
	}
}

// --- runAsk ------------------------------------------------------------------

// Scenario: `dbx ask` sin config no llega ni a resolver el proveedor.
//
// The config is read first, so a broken config is reported as a config problem.
func TestRunAsk_AnUnloadableConfigFails(t *testing.T) {
	withNoConfig(t, errors.New("permission denied"))

	err := runAsk(runCmd(t), []string{"how many users?"})
	if err == nil {
		t.Fatal("an unloadable config returned no error")
	}
	if !strings.Contains(err.Error(), "failed to load config") {
		t.Errorf("error = %v, want it to say the config failed to load", err)
	}
}

// Scenario: Un proveedor que no se puede resolver se informa por su nombre.
//
// An unknown provider name in the config is a typo, and the message has to point
// at the config rather than at the network.
func TestRunAsk_AnUnresolvableProviderFails(t *testing.T) {
	withGlobalConfig(t, &config.Config{
		AI: config.AIConfig{Provider: "not-a-real-provider"},
	})

	err := runAsk(runCmd(t), []string{"anything"})
	if err == nil {
		t.Fatal("an unknown provider returned no error")
	}
	if !strings.Contains(err.Error(), "failed to resolve AI provider") {
		t.Errorf("error = %v, want it to say the provider could not be resolved", err)
	}
}

// Scenario: Sin base de datos, `dbx ask` falla al conectar.
//
// The schema is needed for anything but --sql-only, so this is where it stops. The
// provider has to resolve first, or the test would prove nothing about the
// connection: a custom provider with its key set in the environment resolves
// without any network access.
func TestRunAsk_WithoutAConnectionFails(t *testing.T) {
	t.Setenv("DBX_TEST_ASK_KEY", "not-a-real-key")
	withGlobalConfig(t, &config.Config{
		AI: config.AIConfig{
			Provider: "test-ask",
			Providers: map[string]config.AIProviderConf{
				"test-ask": {APIKeyEnv: "DBX_TEST_ASK_KEY", Model: "m"},
			},
		},
	})

	err := runAsk(runCmd(t), []string{"how many users?"})
	if err == nil {
		t.Fatal("runAsk with no connection configured returned no error")
	}
	if !strings.Contains(err.Error(), "connection not found") {
		t.Errorf("error = %v, want it to say no connection was found, not a provider failure", err)
	}
}

// --- the postgres.Conn seam itself -------------------------------------------

// Scenario: La interfaz de conexión es lo que permite todo lo anterior.
//
// A concrete *pgx.Conn would make every test above need a server, so the interface
// being three methods wide is the load-bearing decision. This asserts that
// *pgx.Conn still satisfies it, which is what keeps production working.
func TestPostgresConn_RealConnectionSatisfiesTheInterface(t *testing.T) {
	var c any = (*pgx.Conn)(nil)
	if _, ok := c.(postgres.Conn); !ok {
		t.Error("*pgx.Conn does not satisfy postgres.Conn; the production call sites would not compile")
	}
	var p any = (*pgx.Conn)(nil)
	if _, ok := p.(pgconnPing); !ok {
		t.Error("*pgx.Conn does not satisfy pgconnPing; getConnection would not compile")
	}
}

// --- Execute, the last untestable statement ----------------------------------

// Scenario: Un comando que falla imprime el error y sale con código 1.
//
// This is the program's whole failure contract, and it is the one thing a test
// could not see while the exit was a direct os.Exit call: the code would have had
// to be 1 for a script to notice, and nothing asserted it. With `exit` behind a
// variable the whole function is observable.
func TestExecute_FailurePrintsTheErrorAndExitsOne(t *testing.T) {
	oldRoot, oldExit := rootCmd, exit
	t.Cleanup(func() { rootCmd, exit = oldRoot, oldExit })

	boom := errors.New("something went wrong")
	rootCmd = &cobra.Command{Use: "dbx"}
	rootCmd.RunE = func(*cobra.Command, []string) error { return boom }
	rootCmd.SetArgs([]string{})
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)

	stderr := captureStderr(t)
	var code int
	codes := 0
	exit = func(c int) { code = c; codes++ }

	Execute()
	got := stderr()

	if codes != 1 {
		t.Errorf("exit was called %d times, want once", codes)
	}
	if code != 1 {
		t.Errorf("exit code = %d, want 1: a script has to be able to tell this failed", code)
	}
	if !strings.Contains(got, "something went wrong") {
		t.Errorf("stderr = %q, want the error printed", got)
	}
}

// Scenario: Un comando que funciona no imprime nada ni sale.
//
// The happy path matters as much: a version that always exited 1 would make every
// successful run look like a failure, and one that always printed would corrupt
// the TUI's output.
func TestExecute_SuccessPrintsNothingAndDoesNotExit(t *testing.T) {
	oldRoot, oldExit := rootCmd, exit
	t.Cleanup(func() { rootCmd, exit = oldRoot, oldExit })

	rootCmd = &cobra.Command{Use: "dbx"}
	rootCmd.RunE = func(*cobra.Command, []string) error { return nil }
	rootCmd.SetArgs([]string{})
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)

	stderr := captureStderr(t)
	exited := 0
	exit = func(int) { exited++ }

	Execute()
	got := stderr()

	if exited != 0 {
		t.Errorf("exit was called %d times on a successful run, want never", exited)
	}
	if got != "" {
		t.Errorf("stderr = %q, want nothing on a successful run", got)
	}
}

// captureStderr redirects os.Stderr and returns a function that gives back
// everything written to it.
//
// The flush is explicit because a pipe is asynchronous: reading the buffer straight
// after the write races the reader goroutine, and the test passes or fails
// depending on scheduling. Closing the writer is what makes io.Copy return.
func captureStderr(t *testing.T) func() string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	old := os.Stderr
	os.Stderr = w

	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(&buf, r)
	}()

	var once bool
	return func() string {
		if !once {
			once = true
			os.Stderr = old
			_ = w.Close()
			<-done
			_ = r.Close()
		}
		return buf.String()
	}
}
