package store

// Scenario: El historial de consultas al escribirlo, y al migrarlo.
//
// Two refusals in Save and MigrateGlobalHistory, and they are the two a user hits last:
//
//   - MigrateGlobalHistory's project write, which fails when the project directory is not
//     writable — and the migration is what the user ASKED for by having a project, so a silent
//     skip would look like "the history moved" when it did not.
//   - Save's marshal, which cannot fail. QueryEntry is a string, a time, a bool and another
//     string, so encoding/json has nothing it cannot encode; the guard was a shape every other
//     marshal in this codebase had, copied without asking whether it could fire.
//
// The second is here so the first has a neighbour: a file with two refusals in it, one real and
// one not, says which is which better than either does alone.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Save refuses a file it cannot write.
func TestSaveRefusesAFileItCannotWrite(t *testing.T) {
	dir := t.TempDir()
	s := NewQueryStore(dir)
	s.Add("SELECT 1")

	// A DIRECTORY where the file should be. The store already wrote its file on Add, so the
	// file has to go first — which is the state this is really about: something else in the
	// state directory occupying the name the history wants.
	if err := os.Remove(s.filePath); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(s.filePath, 0o755); err != nil {
		t.Fatal(err)
	}

	err := s.Save()
	if err == nil {
		t.Fatal("writing the history over a directory reported success")
	}
	if !strings.Contains(err.Error(), "is a directory") && !strings.Contains(err.Error(), "directory") {
		t.Errorf("the error is %q, want it to say what went wrong with the path", err)
	}

	t.Run("and the store is unchanged by the failed save", func(t *testing.T) {
		// The refusal has to leave the in-memory list alone: a Save that cleared the entries
		// before writing would lose the user's history to a permission error.
		if s.Len() != 1 {
			t.Errorf("the store holds %d entries after a failed save, want the one it had", s.Len())
		}
		if got := s.All(); len(got) != 1 || got[0].SQL != "SELECT 1" {
			t.Errorf("the store holds %v after a failed save", got)
		}
	})
}

// Save creates the directory it needs.
func TestSaveCreatesItsDirectory(t *testing.T) {
	// The other half of Save's contract, and the counterweight: MkdirAll is not decoration,
	// and a state directory that does not exist yet is the normal first run.
	dir := filepath.Join(t.TempDir(), "not", "there", "yet")
	s := NewQueryStore(dir)
	s.Add("SELECT 1")

	if err := s.Save(); err != nil {
		t.Fatalf("the first save: %v", err)
	}

	if _, err := os.Stat(s.filePath); err != nil {
		t.Errorf("the history file is missing after a successful save: %v", err)
	}
	// And it round-trips: a store built over the same directory reads its own write back,
	// which is what makes the marshal's output worth checking.
	reloaded := NewQueryStore(dir)
	entries := reloaded.All()
	if len(entries) != 1 || entries[0].SQL != "SELECT 1" {
		t.Errorf("the reloaded history is %v, want the one entry that was saved", entries)
	}
	// The timestamp survives as a time, which is the field most likely to be mangled by a
	// marshal/round trip through JSON.
	if !entries[0].Timestamp.After(time.Time{}) {
		t.Errorf("the reloaded entry has no timestamp: %+v", entries[0])
	}
}

// MigrateGlobalHistory refuses a project directory it cannot write to.
//
// The order matters and is worth pinning: the global file is read, the project directory is
// created, and only then is the project file written — so a failure here has already read the
// data and must NOT have renamed the global file. A migration that renamed first and wrote
// second would lose the history on a full disk.
func TestMigrateGlobalHistoryRefusesAProjectDirectoryItCannotWrite(t *testing.T) {
	stateDir := t.TempDir()
	globalPath := filepath.Join(stateDir, "query_history.json")
	body := `[{"sql":"SELECT 1","timestamp":"2024-01-01T00:00:00Z"}]`
	if err := os.WriteFile(globalPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	// A project directory that cannot be written to, and NOT one where the project FILE is a
	// directory: os.Stat on a directory succeeds, so the migration's own "already exists, skip"
	// guard would return before the write — which is correct behaviour and the wrong fixture.
	// My first version used it and the migration reported success, having skipped.
	//
	// Read-only is the shape that reaches the write: the path does not exist, so the skip
	// guard passes, and creating it inside a directory nobody can write to is the failure.
	projectDir := t.TempDir()
	if err := os.Chmod(projectDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(projectDir, 0o755) })

	err := MigrateGlobalHistory(stateDir, projectDir)
	if err == nil {
		t.Fatal("a migration into an unwritable path reported success")
	}

	// And the global file is still there, NOT renamed: the data has to survive a failed
	// migration, which is the difference between this and a destructive operation.
	if _, statErr := os.Stat(globalPath); statErr != nil {
		t.Errorf("the global history is gone after a failed migration: %v", statErr)
	}
	if _, statErr := os.Stat(globalPath + ".bak"); statErr == nil {
		t.Error("the global history was renamed to .bak even though the migration failed")
	}

	t.Run("and nothing was written at the project path", func(t *testing.T) {
		// A migration that reported success here would leave the user believing their history
		// moved into the project, which it did not.
		if _, statErr := os.Stat(filepath.Join(projectDir, "query_history.json")); statErr == nil {
			t.Error("a file was created at the project path in a directory nobody can write to")
		}
	})

	t.Run("and a successful migration DOES move it", func(t *testing.T) {
		// The counterweight: the two cases above would both pass if the function always
		// failed.
		cleanState := t.TempDir()
		cleanGlobal := filepath.Join(cleanState, "query_history.json")
		if err := os.WriteFile(cleanGlobal, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		cleanProject := filepath.Join(t.TempDir(), "project")

		if err := MigrateGlobalHistory(cleanState, cleanProject); err != nil {
			t.Fatalf("the migration: %v", err)
		}

		moved, err := os.ReadFile(filepath.Join(cleanProject, "query_history.json"))
		if err != nil {
			t.Fatalf("the project file is missing after a successful migration: %v", err)
		}
		if !strings.Contains(string(moved), "SELECT 1") {
			t.Errorf("the migrated file is %q", moved)
		}
		if _, err := os.Stat(cleanGlobal + ".bak"); err != nil {
			t.Errorf("the global file was not kept as .bak: %v", err)
		}
		if _, err := os.Stat(cleanGlobal); err == nil {
			t.Error("the global file was left in place after a successful migration")
		}
	})
}
