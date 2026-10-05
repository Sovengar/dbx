package cli

// Scenario: Las seams de la CLI, y lo que se alcanza a traves de ellas.
//
// root.go declares three variables for the three things a test cannot supply: the config file,
// the TUI model, and the terminal. Two of them are replaced in EVERY existing test, which
// means the defaults — the closures that build a real model and run a real program — are never
// executed. They are two lines of production code that exist only to be the thing a test
// replaces, and nothing checks that they still work. The comment above them claims "nothing
// here changes behaviour", which is a claim about code no test runs.
//
// The third seam, pgxConnect, is what makes the rest of this file possible: it returns
// postgres.Conn plus Ping, which is exactly what pgxfake.Conn is. So every error arm in the
// connect path is reachable by handing the CLI a fake that fails on demand — no server, and no
// test that passes because a database happened to be up.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/testsupport/pgxfake"
)

// A .dbx.toml in a temporary directory, and the process pointed at it. findLocalDSN reads the
// working directory, so reaching the local branch means changing directory — the only way to
// ask "is there a project file here" the way the CLI asks it.
func inProjectWithAlias(t *testing.T, alias string) {
	t.Helper()

	dir := t.TempDir()
	body := "[connections." + alias + "]\ndriver = \"postgres\"\ndsn = \"postgres://dbx@127.0.0.1:1/db\"\n"
	if err := os.WriteFile(filepath.Join(dir, ".dbx.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
}

// getConnectionThroughLocalDSN drives the branch that asks findLocalDSN first, which is the
// branch every command takes when the user is inside a project.
func getConnectionThroughLocalDSN(t *testing.T, alias string) (postgres.Conn, error) {
	t.Helper()
	inProjectWithAlias(t, alias)

	cmd := &cobraCommandForTest{t: t}
	return getConnection(cmd.command())
}

// The connect path's two refusals, through the pgxConnect seam.
//
// Both are what a user hits with a wrong DSN, and they have to say WHICH step failed: "could
// not connect" and "connected but the server is not answering" send a user to different
// places, and a single message for both sends them to the wrong one.
func TestTheConnectPathDistinguishesConnectingFromPinging(t *testing.T) {
	oldConnect := pgxConnect
	t.Cleanup(func() { pgxConnect = oldConnect })

	t.Run("a DSN that cannot be connected", func(t *testing.T) {
		pgxConnect = func(context.Context, string) (pgconnPing, error) {
			return nil, errors.New("connection refused")
		}

		conn, err := getConnectionThroughLocalDSN(t, "shopdb")
		if err == nil {
			t.Fatal("a refused connection reported success")
		}
		if conn != nil {
			t.Errorf("a refused connection also returned %v", conn)
		}
		if !strings.Contains(err.Error(), "failed to connect") {
			t.Errorf("the error is %q, want it to say the CONNECTION failed", err)
		}
	})

	t.Run("a connection that cannot be pinged", func(t *testing.T) {
		fake := pgxfake.New()
		fake.PingErr = errors.New("server is starting up")
		pgxConnect = func(context.Context, string) (pgconnPing, error) { return fake, nil }

		conn, err := getConnectionThroughLocalDSN(t, "shopdb")
		if err == nil {
			t.Fatal("a failed ping reported success")
		}
		if conn != nil {
			t.Errorf("a failed ping also returned %v", conn)
		}
		if !strings.Contains(err.Error(), "failed to ping") {
			t.Errorf("the error is %q, want it to say the PING failed", err)
		}
		// And the connection is closed rather than leaked: the ping is what tells us the
		// connection is unusable, and returning it would leave a dead socket on the floor.
		if fake.Closed() != 1 {
			t.Errorf("the unusable connection was closed %d times, want 1", fake.Closed())
		}
	})

	t.Run("and a working one is returned", func(t *testing.T) {
		// The counterweight, because a connect path that always failed would satisfy both cases
		// above.
		fake := pgxfake.New()
		pgxConnect = func(context.Context, string) (pgconnPing, error) { return fake, nil }

		conn, err := getConnectionThroughLocalDSN(t, "shopdb")
		if err != nil {
			t.Fatalf("a working connection reported %v", err)
		}
		if conn == nil {
			t.Fatal("a working connection returned nothing")
		}
		if fake.Pinged() != 1 {
			t.Errorf("the connection was pinged %d times, want 1", fake.Pinged())
		}
		_ = conn.Close(context.Background())
	})
}

// The default model builder. Not overridden here on purpose: overriding buildModel while
// asserting buildModel would prove nothing about the default.
//
// Its default body is `app.NewModel(cfg)`, which needs a config it can read and a terminal it
// does not need until it renders — so constructing it is enough, and asserting on the type is
// what distinguishes the default from the stub every other test installs.
func TestTheDefaultModelBuilderBuildsTheRealModel(t *testing.T) {
	// The captured production closure, not the variable — see productionBuildModel for why.
	m := productionBuildModel(&config.Config{})
	if m == nil {
		t.Fatal("the default buildModel returned no model")
	}
	// The stub is what the other tests install; getting it back means the restore did not
	// happen and this case is asserting against a test double.
	if _, isStub := m.(stubModel); isStub {
		t.Fatal("the default buildModel returned a stub: the test's own override is still in place")
	}
	if got := fmt.Sprintf("%T", m); got != "app.Model" {
		t.Errorf("the default buildModel returned a %s, want app.Model", got)
	}
}

// The default program runner, run for real.
//
// This was the last uncovered line in the repository, and it was uncovered because a bubbletea
// program opens /dev/tty: the default body could only execute on a machine with a controlling
// terminal. Redirecting os.Stdin does not help — bubbletea goes to /dev/tty regardless — and
// its sibling default, buildModel, was exercised every round because constructing a model needs
// no terminal. That asymmetry is what let one line sit there.
//
// So the options are named (programOpts), and this case sets them to bubbletea's own
// "there is no input" and runs the PRODUCTION closure. Nothing is stubbed: the default body
// executes, against the same code a user runs, with only the terminal swapped for a file.
func TestTheDefaultProgramRunnerRunsWithoutATerminal(t *testing.T) {
	oldOpts, oldRun := programOpts, runProgram
	t.Cleanup(func() { programOpts, runProgram = oldOpts, oldRun })

	// The production default, captured rather than written out — a copy would prove nothing
	// about the default, which is the mistake this whole case exists to correct.
	production := runProgram

	programOpts = []tea.ProgramOption{
		tea.WithInput(nil), // no input to read
		tea.WithOutput(io.Discard),
	}

	// A model that quits on its first update, so a real program ends immediately.
	model, err := production(quittingModel{})
	if err != nil {
		t.Fatalf("the default runProgram: %v", err)
	}
	if model == nil {
		t.Fatal("the default runProgram returned no model")
	}
	if _, isStub := model.(quittingModel); !isStub {
		t.Errorf("the default runProgram returned a %T, want the model it was given", model)
	}

	t.Run("and the same closure with real terminal options asks for a TTY", func(t *testing.T) {
		// The counterweight, and the assertion that the option is what changed: with the
		// production options — none — the same body reaches for the terminal and fails, which
		// is precisely the failure the seam was introduced to route around.
		//
		// It is asserted rather than assumed because the reverse would be bad news: if an empty
		// option slice did NOT mean "use the terminal", the default would not be the default.
		programOpts = nil

		if _, err := production(quittingModel{}); err == nil {
			t.Skip("this machine has a terminal the program could use, so the two paths are indistinguishable here")
		} else if !strings.Contains(err.Error(), "TTY") && !strings.Contains(err.Error(), "tty") {
			t.Errorf("the failure is %q, want it to be about the terminal", err)
		}
	})

}

// listColumns' refusal. findColumns is what fails, and it fails for a table that is not there
// — the most common mistake with this command, and the one whose error has to name what it
// could not find.
func TestListingTheColumnsOfATableThatIsNotThere(t *testing.T) {
	// Every metadata query answers empty, so the column lookup finds nothing. The fake's
	// unmatched-query error would also do, but "the schema has no such column" is the honest
	// answer and it is the one the driver would give.
	fake := pgxfake.New()
	fake.Steps = []pgxfake.Step{{Match: "information_schema", Result: pgxfake.Result{}}}

	var out strings.Builder
	err := listColumnsAndReport(&out, fake, "nosuchtable", false, false)
	if err == nil {
		t.Fatal("listing a table that does not exist reported success")
	}
	// The error has to NAME the table, because the user's next question is "which table did it
	// mean" and a bare "not found" does not answer it.
	if !strings.Contains(err.Error(), "nosuchtable") {
		t.Errorf("the error is %q, want it to name the table", err)
	}
}

// pipe reads os.Stdin twice — once to stat it, once to read it — and both refusals are about
// that file. Replacing os.Stdin is the only way to reach either, and a project directory is
// needed so getConnection reaches the pgxConnect seam at all.
func TestPipeRefusesStdinItCannotUse(t *testing.T) {
	inProjectWithAlias(t, "shopdb")

	oldConnect := pgxConnect
	t.Cleanup(func() { pgxConnect = oldConnect })
	fake := pgxfake.New()
	pgxConnect = func(context.Context, string) (pgconnPing, error) { return fake, nil }

	withStdin := func(t *testing.T, f *os.File) {
		t.Helper()
		prev := os.Stdin
		os.Stdin = f
		t.Cleanup(func() { os.Stdin = prev })
	}

	t.Run("a stdin that cannot be stat'ed", func(t *testing.T) {
		// A CLOSED file: Stat on it fails with EBADF, which is the same refusal a pipe whose
		// writer has gone produces.
		f, err := os.CreateTemp(t.TempDir(), "stdin")
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		withStdin(t, f)

		err = runPipe(pipeCmdForTest(t), nil)
		if err == nil {
			t.Fatal("a stdin that cannot be stat'ed reported success")
		}
		if !strings.Contains(err.Error(), "stdin") {
			t.Errorf("the error is %q, want it to mention stdin", err)
		}
	})

	t.Run("and SQL it cannot read", func(t *testing.T) {
		// A DIRECTORY stats fine and is not a character device, so it gets past the first
		// check — and then every read on it fails with EISDIR. That is the second refusal,
		// and it is the one that used to be unreachable: it needs a stdin that looks like
		// input and is not.
		withStdin(t, mustOpenDir(t, t.TempDir()))

		err := runPipe(pipeCmdForTest(t), nil)
		if err == nil {
			t.Fatal("a stdin that cannot be read reported success")
		}
	})
}

func mustOpenDir(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

// `dbx pipe <session-file>` — the replay half, which is a different function from the stdin
// half and whose first act after connecting is to read the config for the session directory.
//
// The config is loaded AFTER the connection, which is the order worth pinning: a user with a
// working database and a broken config gets "failed to load config", not a connection error
// they did not have.
func TestPipeReplayRefusesAConfigItCannotLoad(t *testing.T) {
	inProjectWithAlias(t, "shopdb")

	oldConnect := pgxConnect
	t.Cleanup(func() { pgxConnect = oldConnect })
	fake := pgxfake.New()
	pgxConnect = func(context.Context, string) (pgconnPing, error) { return fake, nil }

	oldLoad := loadConfig
	t.Cleanup(func() { loadConfig = oldLoad })
	loadConfig = func() (*config.Config, error) {
		return nil, errors.New("the config file is unreadable")
	}

	err := runReplay(pipeCmdForTest(t), []string{"some-session.jsonl"})
	if err == nil {
		t.Fatal("a config that cannot be loaded reported success")
	}
	if !strings.Contains(err.Error(), "config") {
		t.Errorf("the error is %q, want it to mention the config", err)
	}
	// And the connection it opened is closed rather than leaked — the deferred close runs on
	// this path, and the whole point of asserting the order is that the connection exists by
	// the time the config fails.
	if fake.Closed() != 1 {
		t.Errorf("the connection was closed %d times, want 1", fake.Closed())
	}
}

// findLocalDSN with a deleted working directory.
//
// findLocalDSN's first act is os.Getwd, and every other test in this package passes through
// that call with a directory that exists. The refusal only fires when the working directory is
// gone — which is not a contrivance: it is what a shell finds after the directory it was in has
// been removed, and it is the state in which dbx cannot tell which project the user is in.
//
// So the case changes into a temporary directory and then deletes it, and the answer has to be
// the empty string rather than a panic or a stale path.
func TestFindLocalDSNWithNoWorkingDirectory(t *testing.T) {
	gone := t.TempDir()
	restore, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd before: %v", err)
	}
	if err := os.Chdir(gone); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(restore) })

	if err := os.RemoveAll(gone); err != nil {
		t.Fatalf("removing the working directory: %v", err)
	}
	if _, err := os.Getwd(); err == nil {
		t.Skip("Getwd still succeeds after the directory was removed, so the state is not reachable here")
	}

	// The refusal, and it is a refusal rather than a guess: returning a path the app cannot
	// resolve would make it look for a project file somewhere that does not exist.
	if dsn := findLocalDSN("anything"); dsn != "" {
		t.Errorf("findLocalDSN returned %q with no working directory", dsn)
	}

	t.Run("and with a working directory it reads the project file", func(t *testing.T) {
		// The counterweight: a findLocalDSN that always returned "" would satisfy the case
		// above and the local-connection branch of getConnection would be unreachable.
		dir := t.TempDir()
		body := "[connections.shopdb]\ndriver = \"postgres\"\ndsn = \"postgres://dbx@127.0.0.1:1/db\"\n"
		if err := os.WriteFile(filepath.Join(dir, ".dbx.toml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chdir(dir); err != nil {
			t.Fatal(err)
		}

		dsn := findLocalDSN("shopdb")
		if !strings.Contains(dsn, "127.0.0.1") {
			t.Errorf("findLocalDSN returned %q, want the DSN from the project file", dsn)
		}
	})
}
