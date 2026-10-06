package cli

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/buble/dbx/internal/testsupport/pgxfake"
)

// The paths of runPipe that need a connection but NOT a database: the refusal, the empty
// input and the success.
//
// They used to live only in pipe_commands_contract_test.go, which starts every test with
// pipeProject — and that helper skips when DBX_TEST_DSN is unset. So on CI, and on any
// machine without a DSN, these branches were never executed: three statements of
// internal/cli stayed uncovered and the project total could not reach 100%. The
// live-server versions stay where they are; this file pins the same behaviour against
// the pgxConnect seam, which is what makes it run everywhere.

// withFakeConnection points the production connector at a connection that never dials.
func withFakeConnection(t *testing.T, fake *pgxfake.Conn) {
	t.Helper()
	previous := pgxConnect
	t.Cleanup(func() { pgxConnect = previous })
	pgxConnect = func(context.Context, string) (pgconnPing, error) { return fake, nil }
}

// A terminal is refused before anything is read, and before anything is dialled: the
// answer has to be the usage line even when the database is unreachable, because the user
// forgot the pipe, not the credentials.
func TestPipeRefusesATerminalWithoutADatabase(t *testing.T) {
	inProjectWithAlias(t, "shopdb")
	withFakeConnection(t, pgxfake.New())

	// /dev/null is a character device on every Unix, which is the only way to reach this
	// branch from a test — a regular file and a pipe are both off the bit.
	tty, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("cannot open %s: %v", os.DevNull, err)
	}
	withStdin(t, tty)

	err = runPipe(pipeCmdForTest(t), nil)
	if err == nil {
		t.Fatal("a terminal was accepted as piped input")
	}
	if !strings.Contains(err.Error(), "no input provided") {
		t.Errorf("the error is %q, want the refusal to say there was no input", err)
	}
	if !strings.Contains(err.Error(), "dbx pipe") {
		t.Errorf("the error %q does not carry the usage line", err)
	}
}

// The success path against a connection that answers: the statement reaches the connection
// and the rows reach the terminal. Asserted on the output rather than on the call, because
// a reporter that prints nothing and returns nil is the failure a script cannot see.
func TestPipeRunsTheStatementWithoutADatabase(t *testing.T) {
	inProjectWithAlias(t, "shopdb")
	fake := pgxfake.New()
	fake.Steps = []pgxfake.Step{{
		Result: pgxfake.Result{
			Columns: []pgxfake.Column{{Name: "?column?"}},
			Rows:    [][]any{{int64(4)}},
		},
	}}
	withFakeConnection(t, fake)
	withStdin(t, stdinFrom(t, "SELECT 2 + 2\n"))
	read := captureStdout(t)

	if err := runPipe(pipeCmdForTest(t), nil); err != nil {
		t.Fatalf("the piped query failed: %v", err)
	}
	if got := read(); !strings.Contains(got, "rows") {
		t.Errorf("the output does not report a row count:\n%s", got)
	}
	if got := fake.LastQuery(); got != "SELECT 2 + 2" {
		t.Errorf("the connection was asked %q, want the statement the pipe was given", got)
	}
}

// root.go's production connector: the one wiring every other test replaces. Calling it
// once with a DSN that cannot parse is the whole of what a unit test can assert about it —
// it must not accept garbage, and it must not need a server to say so.
func TestTheProductionConnectorRefusesWhatItCannotParse(t *testing.T) {
	if _, err := pgxConnect(context.Background(), ":// not a DSN"); err == nil {
		t.Error("the production connector accepted a DSN it cannot parse")
	}
}
