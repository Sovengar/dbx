package main

// Scenario: El punto de entrada, que es una sentencia y parece no poder probarse.
//
// main() is one statement: cli.Execute(). The usual reason a test cannot call it is that
// os.Exit would take the test process with it — and the reason this one CAN be called is
// that cli.Execute only exits when the command returns an error, which is exactly what the
// file's own comment says when it separates execute() from Execute().
//
// So the test calls main() in-process with `--help`: the one invocation that prints usage
// and returns nil. If a future change made Execute exit on that path, this test would take
// the process down, which is loud and immediate rather than a coverage number quietly
// going back to zero.

import (
	"os"
	"strings"
	"testing"
)

func TestTheEntryPointHandsOffToTheCLI(t *testing.T) {
	old := os.Args
	t.Cleanup(func() { os.Args = old })
	os.Args = []string{"dbx", "--help"}

	out := captureStdout(t, func() { main() })

	// The assertion is that the program's own output came out of main(), not that it looks
	// a particular way — the shape of cobra's help is cobra's business, and asserting on it
	// here would make this test a change detector for a dependency.
	if strings.TrimSpace(out) == "" {
		t.Error("main() produced no output; it did not reach the CLI")
	}
	if !strings.Contains(out, "dbx") && !strings.Contains(out, "Usage") {
		t.Errorf("`dbx --help` printed %q, which is neither the program name nor a usage line", out)
	}
}

// captureStdout runs fn with os.Stdout replaced by a TEMPORARY FILE and returns what was
// written.
//
// Not an os.Pipe copied in a Cleanup: the copy goroutine would not have run by the time the
// assertion executes, so every caller would read an empty string and pass. A file is
// already complete when fn returns. The cost is a file per call, which is the right trade
// against a test that cannot fail.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	f, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatalf("temp file: %v", err)
	}
	prev := os.Stdout
	os.Stdout = f
	defer func() { os.Stdout = prev }()

	fn()

	if err := f.Close(); err != nil {
		t.Fatalf("closing the capture: %v", err)
	}
	data, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatalf("reading the capture: %v", err)
	}
	return string(data)
}
