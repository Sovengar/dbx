package config

// Scenario: canonicalPath cuando no puede hacer su trabajo, y lo que el identidad de repo
// hace con ese resultado.
//
// canonicalPath has two fallbacks, and the comment above it promises a conservative identity
// rather than a panic. The second fallback — a path that cannot be resolved through symlinks —
// is exercised by every missing-path test in the package. The FIRST one is a failed
// absolute-path step, which needs os.Getwd to fail; os.Getwd fails only when the working
// directory no longer exists.
//
// That state is arranged by INJECTION rather than by deleting the test binary's own
// directory: getwd is a package-level variable, the test replaces it with one that
// fails, and no syscall outlives the test. Deleting the real directory would change
// the working directory of every other test in the binary.

import (
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestCanonicalPathWithADeletedWorkingDirectory(t *testing.T) {
	// The premise, because a test that cannot reach the state would pass for the wrong reason.
	deleted := func() (string, error) { return "", syscall.ENOENT }
	old := getwd
	getwd = deleted
	t.Cleanup(func() { getwd = old })

	// The branch: the absolute-path step cannot make a relative path absolute without a
	// working directory, so it errors and the fallback cleans what it was given.
	//
	// A RELATIVE path, which is the only kind that can fail: the absolute-path step of an
	// already-absolute path never consults the working directory, so an absolute one would
	// sail past the branch.
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
		// The counterweight, and it is the interesting half: the absolute-path step of an
		// absolute path does NOT need the working directory, so this is what actually
		// happens in the state above for the paths the app has — which are absolute,
		// because they came from a directory walk.
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
		// The other relative input there is, and the absolute-path step of "" is exactly
		// what canonicalPath is called with when a project path comes back empty.
		got := canonicalPath("")

		if got == "" {
			t.Error("canonicalPath(\"\") returned an empty identity, which every caller would treat as a match")
		}
	})
}
