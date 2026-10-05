package debuglog

// Scenario: El unico sitio donde dbx escribe sus logs de depuracion.
//
// This package exists because the thing it does was written nine times, and the copies had
// drifted: three spellings of the OpenFile flags, two error styles, and four copies whose
// error arm could not be reached from a test because it only fires when /tmp is unwritable.
//
// The path being an ARGUMENT is what fixes the second half. A caller who cannot be tested —
// because the only way to make it fail is a filesystem that misbehaves — can now be tested by
// naming a path that cannot be opened. That is the whole contract, and the rest of this file
// is the proof that it holds.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The naming convention is load-bearing: an investigation starts with
// `ls /tmp/dbx_*_debug.log`, so a component that invents its own filename disappears from
// that listing.
func TestThePathFollowsTheConvention(t *testing.T) {
	dir := t.TempDir()
	old := DefaultDir
	DefaultDir = dir
	t.Cleanup(func() { DefaultDir = old })

	for _, component := range []string{"app", "grid", "ere", "qb", "ask", "autocomplete", "mode"} {
		want := filepath.Join(dir, "dbx_"+component+"_debug.log")
		if got := Path(component); got != want {
			t.Errorf("Path(%q) = %q, want %q", component, got, want)
		}
	}
}

// A line lands in the file, with the prefix, and the arguments are formatted rather than
// printed as a slice of interfaces.
func TestWriteAppendsOnePrefixedLinePerCall(t *testing.T) {
	dir := t.TempDir()
	old := DefaultDir
	DefaultDir = dir
	t.Cleanup(func() { DefaultDir = old })

	Write("app", "App", "KeyPress: key=%q focus=%q", "j", "grid")
	Write("app", "App", "rows=%d", 42)

	data, err := os.ReadFile(Path("app"))
	if err != nil {
		t.Fatalf("the log was not written: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("the log holds %d lines, want 2: %q", len(lines), data)
	}
	// Two calls, two lines — not one line with an embedded newline and not two lines from one
	// call. Append is what makes a log readable while the program is still running.
	if lines[0] != `App: KeyPress: key="j" focus="grid"` {
		t.Errorf("the first line is %q", lines[0])
	}
	if lines[1] != "App: rows=42" {
		t.Errorf("the second line is %q", lines[1])
	}

	t.Run("and a component's log is its own file", func(t *testing.T) {
		Write("grid", "Grid", "a grid line")
		data, err := os.ReadFile(Path("grid"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "KeyPress") {
			t.Errorf("the grid log holds the app's lines: %q", data)
		}
		if !strings.Contains(string(data), "a grid line") {
			t.Errorf("the grid log does not hold its own line: %q", data)
		}
	})
}

// The arm that could not be reached, now reachable: a log file that cannot be opened.
//
// This is the reason the package exists. Four of the nine copies had exactly this arm, and
// none of them could be tested, because the only way to make it fire was a /tmp that does
// not work — and no test may assume that about the machine it runs on.
func TestALogThatCannotBeOpenedIsSilentlySkipped(t *testing.T) {
	// A path whose parent is a FILE. os.OpenFile returns ENOTDIR, which is the ordinary
	// "cannot open" error, and the one a full disk or a permission change would also produce.
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// No return value to assert and no panic: that IS the assertion. A logger that could
	// fail a render would be worse than a lost log line, which is why Write has no error
	// and why this case can only check that the program kept going.
	Write("app", "App", "this line cannot be written")
	Append(filepath.Join(blocker, "dbx_app_debug.log"), "App", "neither can this")

	// And the counterweight: with a path that CAN be opened, the same calls write. Without
	// it, a Write that silently did nothing would satisfy the case above.
	ok := filepath.Join(t.TempDir(), "fine.log")
	Append(ok, "App", "written")
	data, err := os.ReadFile(ok)
	if err != nil {
		t.Fatalf("Append did not write to a good path: %v", err)
	}
	if !strings.Contains(string(data), "written") {
		t.Errorf("the file holds %q", data)
	}
}

// An empty prefix writes the line unadorned rather than leaving a dangling separator.
func TestAnEmptyPrefixLeavesNoSeparator(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bare.log")
	Append(path, "", "just the %s", "line")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimRight(string(data), "\n"); got != "just the line" {
		t.Errorf("the line is %q", got)
	}
}
