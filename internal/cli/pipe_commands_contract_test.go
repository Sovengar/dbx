package cli

// Scenario: `dbx pipe` de principio a fin, con la conexion de verdad.
//
// The other suite in this package drives the PIECES — replayEntries, askAndReport — through
// the seam helpers. This one drives the COMMAND, because the interesting half of a
// pipeline command is the part nobody calls directly: what it does with stdin.
//
// Three guards stand between the user's SQL and the database, and each one is a mistake
// only someone makes once:
//
//	stdin is a TTY, not a pipe — the user typed `dbx pipe` and forgot to pipe
//	stdin is a pipe but carries nothing — the shell sent an empty heredoc
//	readSQL failed halfway — a truncated statement is worse than a refused one
//
// The first is the one a regular file cannot reach, because a regular file is not a
// character device. /dev/null is, which is the whole trick. That also means the existing
// suite's case reaches the check and passes through it: it covers the empty-pipe side and
// nothing else, which its own comment already says.
//
// The success path needs a real server, because the command takes its connection from
// getConnection and there is no injection point below that. The DSN goes in through
// ${env:...}, which is how a project file is supposed to keep a password out of itself.

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buble/dbx/internal/config"
)

// pipeProject writes a .dbx.toml whose DSN comes from the environment and moves into its
// directory. ONE directory for both, because t.TempDir returns a NEW one per call and
// writing into one while reading from another is a bug that works until it does not.
func pipeProject(t *testing.T) {
	t.Helper()
	if os.Getenv("DBX_TEST_DSN") == "" {
		t.Skip("DBX_TEST_DSN is not set, so there is no database to pipe into")
	}
	dir := t.TempDir()
	toml := "[connections.test]\ndsn = \"${env:DBX_TEST_DSN}\"\n"
	if err := os.WriteFile(filepath.Join(dir, ".dbx.toml"), []byte(toml), 0o644); err != nil {
		t.Fatalf("writing the project file: %v", err)
	}
	t.Chdir(dir)
}

// withStdin points os.Stdin at f for the duration of the test. os.Stdin is a package-level
// *os.File, so this is process-wide; nothing in this package runs in parallel.
func withStdin(t *testing.T, f *os.File) {
	t.Helper()
	old := os.Stdin
	os.Stdin = f
	t.Cleanup(func() {
		os.Stdin = old
		_ = f.Close()
	})
}

// captureStdout redirects os.Stdout to a temporary file for the duration of the test and
// returns a reader over what was written.
//
// A file rather than an os.Pipe: the commands write to os.Stdout synchronously, so the
// bytes are on disk the moment runPipe returns, and reading them needs no goroutine. The
// first version used a pipe and copied in a Cleanup, which means the assertion ran BEFORE
// the copy — so every case reported an empty output and the failures looked like the
// command printing nothing.
func captureStdout(t *testing.T) func() string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stdout")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("creating the capture file: %v", err)
	}
	old := os.Stdout
	os.Stdout = f
	t.Cleanup(func() {
		os.Stdout = old
		_ = f.Close()
	})
	return func() string {
		out, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading the captured output: %v", err)
		}
		return string(out)
	}
}

// stdinFrom writes content to a temp file and points os.Stdin at it. A REGULAR FILE, so it
// passes the character-device check.
func stdinFrom(t *testing.T, content string) *os.File {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stdin")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing stdin: %v", err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening stdin: %v", err)
	}
	return f
}

func TestPipeRefusesATerminalAndAnEmptyPipe(t *testing.T) {
	pipeProject(t)
	withGlobalConfig(t, &config.Config{})

	t.Run("a TERMINAL is refused with the usage line", func(t *testing.T) {
		// /dev/null is a character device on every Unix, which is the only way to reach
		// this branch from a test — a regular file and a pipe are both off the bit.
		//
		// The usage line matters as much as the refusal: the user typed a command and
		// forgot what it wanted, so the answer is how to use it rather than an error
		// about a file mode they cannot see.
		tty, err := os.Open(os.DevNull)
		if err != nil {
			t.Skipf("cannot open %s: %v", os.DevNull, err)
		}
		withStdin(t, tty)

		err = runPipe(runCmd(t), nil)
		if err == nil {
			t.Fatal("a terminal was accepted as piped input")
		}
		if !strings.Contains(err.Error(), "no input provided") {
			t.Errorf("the error is %q, want it to say there was no input", err)
		}
		if !strings.Contains(err.Error(), "dbx pipe") {
			t.Errorf("the error %q does not carry the usage line", err)
		}
	})

	t.Run("a pipe carrying nothing is refused as EMPTY", func(t *testing.T) {
		// The other side of the check, and the one the existing suite reaches. Distinct
		// messages on purpose: one says "you forgot to pipe", the other says "you piped
		// nothing", and a user who ran `cat empty.sql | dbx pipe` needs the second.
		withStdin(t, stdinFrom(t, ""))

		err := runPipe(runCmd(t), nil)
		if err == nil {
			t.Fatal("an empty pipe was accepted")
		}
		if !strings.Contains(err.Error(), "empty SQL input") {
			t.Errorf("the error is %q, want it to say the input was empty", err)
		}
	})

	t.Run("whitespace alone is ALSO empty", func(t *testing.T) {
		// readSQL trims, so "   \n\t " is the empty string by the time it is tested. Not
		// pinning it leaves the question open, and the answer matters: a file of blank
		// lines from a templating step should not reach the database as an empty
		// statement.
		withStdin(t, stdinFrom(t, "   \n\t\n  "))

		err := runPipe(runCmd(t), nil)
		if err == nil || !strings.Contains(err.Error(), "empty SQL input") {
			t.Errorf("whitespace input gave %v, want the empty-input refusal", err)
		}
	})

	t.Run("a stdin that cannot be STATed is an error", func(t *testing.T) {
		// The stat arm. A closed file has no descriptor to stat, which is the closest a
		// test gets to the real failure — a redirect from a process that exited between
		// the fork and the exec.
		f, err := os.Open(os.DevNull)
		if err != nil {
			t.Skipf("cannot open %s: %v", os.DevNull, err)
		}
		if err := f.Close(); err != nil {
			t.Fatalf("closing: %v", err)
		}
		old := os.Stdin
		os.Stdin = f
		t.Cleanup(func() { os.Stdin = old })

		if err := runPipe(runCmd(t), nil); err == nil {
			t.Error("a stdin that cannot be stat'ed was accepted")
		}
	})
}

// TestPipeRunsTheStatementItWasGiven is the whole point of the command, driven against a
// real server. Both output shapes are asserted, because the JSON flag is what a script
// uses and a flag read into the wrong variable still has the right value.
func TestPipeRunsTheStatementItWasGiven(t *testing.T) {
	pipeProject(t)
	withGlobalConfig(t, &config.Config{})

	t.Run("human readable", func(t *testing.T) {
		withStdin(t, stdinFrom(t, "SELECT 2 + 2\n"))
		read := captureStdout(t)

		if err := runPipe(runCmd(t), nil); err != nil {
			t.Fatalf("the piped query failed: %v", err)
		}
		if got := read(); !strings.Contains(got, "rows") {
			t.Errorf("the output does not report a row count:\n%s", got)
		}
	})

	t.Run("JSON", func(t *testing.T) {
		// Asserted on the OUTPUT rather than on the flag's value, because a flag read
		// into the wrong variable still reports true and produces nothing.
		withStdin(t, stdinFrom(t, "SELECT 2 + 2\n"))
		read := captureStdout(t)

		cmd := runCmd(t)
		if err := cmd.Flags().Set("json", "true"); err != nil {
			t.Fatalf("setting the flag: %v", err)
		}
		if err := runPipe(cmd, nil); err != nil {
			t.Fatalf("the piped query failed: %v", err)
		}

		got := read()
		var decoded map[string]any
		if err := json.Unmarshal([]byte(got), &decoded); err != nil {
			t.Fatalf("the output is not JSON with --json: %v\n%s", err, got)
		}
		if _, ok := decoded["rows"]; !ok {
			t.Errorf("the JSON carries no rows key: %s", got)
		}
	})

	t.Run("a query that fails is an error, not empty output", func(t *testing.T) {
		// The failure path. A pipe that exits 0 with no output is the worst outcome for a
		// script: it reads "done" from the exit code and nothing from the output.
		withStdin(t, stdinFrom(t, "SELECT * FROM a_table_that_is_not_there\n"))
		captureStdout(t)

		if err := runPipe(runCmd(t), nil); err == nil {
			t.Error("a failing query reported success")
		}
	})
}

// readSQL is where a truncated statement would be produced, so its two properties are
// pinned directly: it must survive a reader that hands back the last chunk together with
// io.EOF — the normal way a pipe ends — and it must report a real read error rather than
// returning what it managed to collect.
func TestReadingStdinReportsAnErrorInsteadOfATruncatedStatement(t *testing.T) {
	boom := errors.New("the pipe broke")

	t.Run("data arriving together with EOF is not lost", func(t *testing.T) {
		// io.Copy handles this correctly and a hand-rolled loop does not, which is what
		// the function's own comment says it exists to avoid. The fixture is a reader
		// that returns bytes AND io.EOF from the same call.
		got, err := readSQL(&dataThenEOF{"SELECT 1"})
		if err != nil {
			t.Fatalf("reading failed: %v", err)
		}
		if got != "SELECT 1" {
			t.Errorf("read the last chunk as %q, want %q — a statement truncated here is executed truncated",
				got, "SELECT 1")
		}
	})

	t.Run("a mid-read failure is reported and nothing is returned", func(t *testing.T) {
		// The property that matters most: returning the partial read would execute a
		// statement the user never finished typing.
		got, err := readSQL(&failingReader{after: "SELECT ", err: boom})
		if err == nil {
			t.Fatal("a broken pipe reported success")
		}
		if got != "" {
			t.Errorf("the error path returned %q, want nothing — a partial statement is worse than none", got)
		}
		if !errors.Is(err, boom) {
			t.Errorf("the error is %v, want the read error itself", err)
		}
	})
}

// dataThenEOF returns its payload and io.EOF from the same call, which is what a pipe does
// at the end of a stream and what a naive read loop drops.
type dataThenEOF struct{ s string }

func (r *dataThenEOF) Read(p []byte) (int, error) {
	if r.s == "" {
		return 0, io.EOF
	}
	n := copy(p, r.s)
	r.s = r.s[n:]
	if r.s == "" {
		return n, io.EOF
	}
	return n, nil
}

// failingReader hands back `after` and an error together, so a partial read is possible.
type failingReader struct {
	after string
	err   error
	done  bool
}

func (r *failingReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, r.err
	}
	r.done = true
	return copy(p, r.after), r.err
}
