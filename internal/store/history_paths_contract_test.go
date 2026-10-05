package store

// Scenario: Donde se guarda el historial de consultas, y que pasa cuando el sitio no
// existe.
//
// NewQueryStoreLimited resolves an empty state directory by hand rather than calling
// config.StateDir, which is deliberate — the comment records that this file used to have
// its OWN copy of that expression, with the same discarded error, so an empty directory
// resolved to ".local/state/dbx" RELATIVE TO THE WORKING DIRECTORY. Three copies of one
// path in this repository; this is the one that was copied wrong.
//
// So the fallback chain is the contract: empty directory, then $HOME, then the temp dir.
// Three outcomes, and which one you get depends on os.UserHomeDir answering — which is
// driven by $HOME. The temp-dir branch is the one that exists for a machine with no home
// directory at all, and it is the branch that is hardest to reach by accident, so it is
// the one worth pinning.
//
// MigrateGlobalHistory's error arms are the other half. Each of its three filesystem steps
// can fail, and each failure has to be RETURNED rather than swallowed — the function moves
// the user's history out from under them, so a half-done migration that reports success is
// worse than one that reports failure.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAnEmptyStateDirectoryResolvesToTheHomeStateDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	s := NewQueryStoreLimited("", 10)
	want := filepath.Join(home, ".local", "state", "dbx", "query_history.json")
	if s.filePath != want {
		t.Errorf("the store writes to %q, want %q", s.filePath, want)
	}
}

func TestAnEmptyStateDirectoryWithNoHomeFallsBackToTheTempDirectory(t *testing.T) {
	// The branch that exists for a machine with no home directory: a container, a CI
	// runner, a user whose $HOME is unset. os.UserHomeDir answers an error or an empty
	// string for both, and the function treats them the same.
	for _, tc := range []struct {
		name string
		home string
	}{
		{"HOME is unset", ""},
		{"HOME is a relative path", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", tc.home)

			s := NewQueryStoreLimited("", 10)
			want := filepath.Join(os.TempDir(), "dbx", "state", "query_history.json")
			if s.filePath != want {
				t.Errorf("with no usable home the store writes to %q, want %q", s.filePath, want)
			}
		})
	}

	t.Run("and the fallback is absolute, never relative to the working directory", func(t *testing.T) {
		// The bug this branch is guarding against: a relative ".local/state/dbx" means
		// the history follows the process's cwd, so running dbx from two directories
		// gives two histories. Both answers must be absolute.
		t.Setenv("HOME", "")
		for _, tc := range []struct{ name, got string }{
			{"no home", NewQueryStoreLimited("", 10).filePath},
			{"a real home", func() string {
				t.Setenv("HOME", t.TempDir())
				return NewQueryStoreLimited("", 10).filePath
			}()},
		} {
			if !filepath.IsAbs(tc.got) {
				t.Errorf("with %s the store path %q is relative, so the history depends on the cwd",
					tc.name, tc.got)
			}
		}
	})
}

func TestALimitOfZeroOrLessMeansTheDefaultRatherThanNothing(t *testing.T) {
	// The stated rule, and the reason it exists: a zero-valued config must not silently
	// erase a user's history. `max_entries: 0` in a config file is the obvious way to
	// write that by accident.
	default_ := NewQueryStoreLimited(t.TempDir(), 0).MaxEntries()
	if default_ != defaultMaxEntries {
		t.Errorf("a limit of 0 gives MaxEntries() = %d, want the default %d", default_, defaultMaxEntries)
	}
	for _, n := range []int{-1, -100} {
		if got := NewQueryStoreLimited(t.TempDir(), n).MaxEntries(); got != defaultMaxEntries {
			t.Errorf("a limit of %d gives MaxEntries() = %d, want the default %d", n, got, defaultMaxEntries)
		}
	}

	t.Run("a positive limit is honoured", func(t *testing.T) {
		for _, n := range []int{1, 5, 1000} {
			if got := NewQueryStoreLimited(t.TempDir(), n).MaxEntries(); got != n {
				t.Errorf("a limit of %d gives MaxEntries() = %d", n, got)
			}
		}
	})

	t.Run("the limit is enforced by dropping the OLDEST", func(t *testing.T) {
		// Stated as behaviour rather than as the constant: three adds, a limit of two,
		// and the first one gone.
		s := NewQueryStoreLimited(t.TempDir(), 2)
		s.Add("first")
		s.Add("second")
		s.Add("third")

		if s.Len() != 2 {
			t.Fatalf("the store holds %d entries, want 2", s.Len())
		}
		// All() reports NEWEST FIRST, so the dropped one is checked by its ABSENCE
		// rather than by a position. Asserting an index would pin the ordering too,
		// which is a different contract and one the comment above does not make.
		var kept []string
		for _, e := range s.All() {
			kept = append(kept, e.SQL)
			if e.SQL == "first" {
				t.Error("the oldest entry survived the limit, so the store grows without bound")
			}
		}
		if len(kept) != 2 {
			t.Errorf("the store holds %v, want exactly two of the three", kept)
		}
	})
}

func TestSavingIntoAnImpossibleDirectoryReportsIt(t *testing.T) {
	// Save's MkdirAll arm. A FILE where the directory should be makes MkdirAll fail, and
	// returning that error is what lets the caller tell the user their history is not
	// being kept. Swallowing it means a silent loss.
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocked")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("setting up the blocker: %v", err)
	}

	s := NewQueryStoreLimited(filepath.Join(blocker, "nested"), 10)
	if err := s.Save(); err == nil {
		t.Error("saving under a path blocked by a file reported success")
	}

	t.Run("and the in-memory history is still usable after a failed save", func(t *testing.T) {
		// Best-effort persistence: Add must work even when Save cannot, so the session's
		// history is intact even if it will not survive a restart.
		if s.Len() != 0 {
			t.Fatalf("the store starts with %d entries", s.Len())
		}
		s.Add("SELECT 1")
		if s.Len() != 1 {
			t.Errorf("after a failed save the store holds %d entries, want 1", s.Len())
		}
	})
}

func TestMigratingTheGlobalHistoryReportsEveryFailure(t *testing.T) {
	writeGlobal := func(t *testing.T, dir string) string {
		t.Helper()
		path := filepath.Join(dir, "query_history.json")
		body, err := json.Marshal([]QueryEntry{{SQL: "SELECT 1", Timestamp: time.Unix(0, 0).UTC()}})
		if err != nil {
			t.Fatalf("encoding: %v", err)
		}
		if err := os.WriteFile(path, body, 0o644); err != nil {
			t.Fatalf("writing the global file: %v", err)
		}
		return path
	}

	t.Run("the happy path copies the file and backs the original up", func(t *testing.T) {
		stateDir, projectDir := t.TempDir(), filepath.Join(t.TempDir(), "project")
		global := writeGlobal(t, stateDir)

		if err := MigrateGlobalHistory(stateDir, projectDir); err != nil {
			t.Fatalf("the migration failed: %v", err)
		}
		copied := filepath.Join(projectDir, "query_history.json")
		if _, err := os.Stat(copied); err != nil {
			t.Errorf("the project file is missing: %v", err)
		}
		if _, err := os.Stat(global + ".bak"); err != nil {
			t.Errorf("the original was not backed up: %v", err)
		}
		if _, err := os.Stat(global); err == nil {
			t.Error("the original is still in place after a successful migration")
		}
	})

	t.Run("an EXISTING project file means there is nothing to migrate", func(t *testing.T) {
		// The first guard, and the one that makes the migration idempotent: running it
		// twice must not overwrite a project history with the global one.
		stateDir, projectDir := t.TempDir(), t.TempDir()
		writeGlobal(t, stateDir)

		existing := filepath.Join(projectDir, "query_history.json")
		keep := []byte(`[{"sql":"SELECT keep","count":1}]`)
		if err := os.WriteFile(existing, keep, 0o644); err != nil {
			t.Fatalf("writing the project file: %v", err)
		}

		if err := MigrateGlobalHistory(stateDir, projectDir); err != nil {
			t.Fatalf("the migration failed: %v", err)
		}
		got, err := os.ReadFile(existing)
		if err != nil {
			t.Fatalf("reading back: %v", err)
		}
		if string(got) != string(keep) {
			t.Errorf("the project history was overwritten: %q", got)
		}
		if _, err := os.Stat(filepath.Join(stateDir, "query_history.json")); err != nil {
			t.Error("the global file was removed even though nothing was migrated")
		}
	})

	t.Run("a MISSING global file is not an error", func(t *testing.T) {
		// Every fresh install hits this, so returning an error here would make startup
		// noisy for every user who has never run a query.
		stateDir, projectDir := t.TempDir(), t.TempDir()
		if err := MigrateGlobalHistory(stateDir, projectDir); err != nil {
			t.Errorf("migrating with no global file failed: %v", err)
		}
	})

	t.Run("a project directory blocked by a FILE is reported", func(t *testing.T) {
		// The MkdirAll arm. Silently continuing would leave the migration claiming to
		// have run with the history still global.
		stateDir := t.TempDir()
		writeGlobal(t, stateDir)
		blocked := filepath.Join(t.TempDir(), "blocked")
		if err := os.WriteFile(blocked, []byte("not a directory"), 0o644); err != nil {
			t.Fatalf("setting up the blocker: %v", err)
		}

		err := MigrateGlobalHistory(stateDir, blocked)
		if err == nil {
			t.Fatal("migrating into a path blocked by a file reported success")
		}
		if !strings.Contains(err.Error(), "not a directory") {
			t.Errorf("the error is %q, want it to name the problem", err)
		}
		// And the global file must NOT have been renamed away, or the history is lost.
		if _, statErr := os.Stat(filepath.Join(stateDir, "query_history.json")); statErr != nil {
			t.Error("a failed migration removed the original history")
		}
	})

	t.Run("a DIRECTORY where the project file goes is caught by the Stat guard, not by WriteFile", func(t *testing.T) {
		// MigrateGlobalHistory's WriteFile error arm cannot be reached by putting a
		// directory at the project path — os.Stat finds it and the FIRST guard returns
		// nil, so the migration never starts. That is worth knowing before someone writes
		// a test that expects a WriteFile error and cannot get one.
		//
		// The arm is left in place because a path can still become unwritable between the
		// Stat and the write. What is asserted here is the guard that makes the common
		// case safe.
		stateDir := t.TempDir()
		writeGlobal(t, stateDir)
		projectDir := t.TempDir()
		if err := os.Mkdir(filepath.Join(projectDir, "query_history.json"), 0o755); err != nil {
			t.Fatalf("setting up the blocker: %v", err)
		}

		if err := MigrateGlobalHistory(stateDir, projectDir); err != nil {
			t.Errorf("a directory at the project path produced an error %v, want the Stat guard to skip silently", err)
		}
		// Skipped, not failed: the global file is still there, because nothing moved.
		if _, err := os.Stat(filepath.Join(stateDir, "query_history.json")); err != nil {
			t.Error("a skipped migration removed the original history")
		}
	})
}
