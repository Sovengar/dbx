package config

// Scenario: canonicalPath cuando no puede hacer su trabajo, y lo que el identidad de repo
// hace con ese resultado.
//
// canonicalPath has two fallbacks, and the comment above it promises a conservative identity
// rather than a panic. The second fallback — a path that cannot be resolved through symlinks —
// is exercised by every missing-path test in the package. The FIRST one is not reachable from
// anything a test can arrange the obvious way: filepath.Abs fails only when os.Getwd fails,
// and os.Getwd fails only when the working directory no longer exists.
//
// Which is arrangeable: change into a directory, then delete it. That is not a contrivance —
// it is what happens to a shell in a directory that someone removed underneath it, and it is
// the only state in which the app's project detection can find a repo and then be unable to
// name it absolutely.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCanonicalPathWithADeletedWorkingDirectory(t *testing.T) {
	// The premise, because a test that cannot reach the state would pass for the wrong reason.
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

	// The branch: filepath.Abs cannot make a relative path absolute without a working
	// directory, so it errors and the fallback cleans what it was given.
	//
	// A RELATIVE path, which is the only kind that can fail: Abs of an already-absolute path
	// never consults the working directory, so an absolute one would sail past the branch.
	const relative = "some/repo"
	got := canonicalPath(relative)

	if want := filepath.Clean(relative); got != want {
		t.Errorf("canonicalPath(%q) = %q, want the cleaned relative path %q", relative, got, want)
	}
	// And it did not panic, which is the promise the comment makes.
	if strings.ContainsRune(got, 0) {
		t.Errorf("canonicalPath returned a NUL: %q", got)
	}

	t.Run("and an absolute path still works without a working directory", func(t *testing.T) {
		// The counterweight, and it is the interesting half: Abs of an absolute path does NOT
		// need the working directory, so this is what actually happens in the state above for
		// the paths the app has — which are absolute, because they came from a directory walk.
		abs := filepath.Join(string(filepath.Separator), "tmp", "some", "repo")

		got := canonicalPath(abs)

		if !strings.HasPrefix(got, string(filepath.Separator)) {
			t.Errorf("canonicalPath(%q) = %q, which is not absolute", abs, got)
		}
		// It resolves through symlinks when it can, which is what the function is for: /tmp is
		// a symlink on macOS and a real directory on Linux, so only the absoluteness is
		// asserted and the exact path is left to the platform.
		if filepath.IsAbs(got) != true {
			t.Errorf("canonicalPath(%q) = %q, want an absolute path", abs, got)
		}
	})

	t.Run("and an empty path does not panic", func(t *testing.T) {
		// The other relative input there is, and Abs("") is exactly what canonicalPath is
		// called with when a project path comes back empty.
		got := canonicalPath("")

		if got == "" {
			t.Error("canonicalPath(\"\") returned an empty identity, which every caller would treat as a match")
		}
	})
}
