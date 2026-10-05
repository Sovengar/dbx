package store

// Scenario: El historial se comporta cuando el disco no coopera.
//
// The store's happy path is well covered: add, dedupe, favourite, filter. What was not
// covered is everything where the FILESYSTEM says no, and that is where a query-history
// feature earns its keep or loses it — because the whole point of keeping history is
// surviving a restart, and a restart is exactly when the disk may be read-only, full, or
// holding a file somebody else wrote.
//
// Three decisions are pinned here, and each is a decision rather than an accident:
//
//	a corrupt history file is IGNORED, not propagated — the store starts empty and the
//	user loses their history instead of the application
//	a failed Save is swallowed inside Add, because the in-memory list is already correct
//	and a query that ran must not be reported as a failure
//	the limit is applied on LOAD as well as on add, so lowering ui.history_size takes
//	effect at once instead of after enough queries to push the oldest out
//
// And one migration: the first project opened after upgrading gets the global history,
// and the global file is kept as a .bak rather than deleted.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeHistory(t *testing.T, dir string, body string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "query_history.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// Scenario: Un fichero que no se puede usar se ignora en vez de propagar el error.
func TestTheStoreIgnoresAHistoryFileItCannotUse(t *testing.T) {
	t.Run("a CORRUPT file yields an empty store, not an error", func(t *testing.T) {
		dir := t.TempDir()
		writeHistory(t, dir, `{"not":"a list"}`)

		s := NewQueryStore(dir)
		if got := len(s.All()); got != 0 {
			t.Errorf("a corrupt history file loaded %d entries", got)
		}
		// And the store is usable, which is the point of swallowing it.
		s.Add("SELECT 1")
		if got := len(s.All()); got != 1 {
			t.Errorf("the store holds %d entries after one add", got)
		}
	})

	t.Run("a truncated file yields an empty store", func(t *testing.T) {
		dir := t.TempDir()
		writeHistory(t, dir, `[{"sql":"SELECT 1","timestamp":"2020-`)

		s := NewQueryStore(dir)
		if got := len(s.All()); got != 0 {
			t.Errorf("a truncated history file loaded %d entries", got)
		}
	})

	t.Run("an EMPTY file yields an empty store", func(t *testing.T) {
		dir := t.TempDir()
		writeHistory(t, dir, "")

		s := NewQueryStore(dir)
		if got := len(s.All()); got != 0 {
			t.Errorf("an empty history file loaded %d entries", got)
		}
	})

	t.Run("a missing file yields an empty store", func(t *testing.T) {
		s := NewQueryStore(t.TempDir())
		if got := len(s.All()); got != 0 {
			t.Errorf("a store with no file holds %d entries", got)
		}
	})
}

// Scenario: Un Save que falla no convierte una query correcta en un error.
func TestAFailedSaveDoesNotBreakAdd(t *testing.T) {
	// The list is held in memory and written through. A full disk or a read-only mount
	// makes the write fail, but the query HAS ALREADY RUN — reporting a failure would
	// tell the user their query did not work when it did.
	dir := t.TempDir()
	s := NewQueryStore(dir)

	// Make the file unwritable by replacing the directory with a file after the store
	// has resolved its path.
	if err := os.WriteFile(filepath.Join(dir, "block"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	blocked := NewQueryStore(dir)
	blocked.filePath = filepath.Join(dir, "block", "query_history.json")

	blocked.Add("SELECT 1")

	// In memory it is there, which is the assertion that matters.
	if got := len(blocked.All()); got != 1 {
		t.Errorf("the store holds %d entries after a failed write", got)
	}
	_ = s

	t.Run("Save reports the error to a caller that wants it", func(t *testing.T) {
		// The direct API does report it. It is only Add that swallows it, and only
		// because Add has no caller that could act on it.
		bad := NewQueryStore(dir)
		bad.filePath = filepath.Join(dir, "block", "query_history.json")
		if err := bad.Save(); err == nil {
			t.Error("Save returned no error writing into a file used as a directory")
		}
	})

	t.Run("Save creates the state directory it needs", func(t *testing.T) {
		deep := filepath.Join(t.TempDir(), "a", "b", "c")
		s := NewQueryStore(deep)
		s.Add("SELECT 1")
		if _, err := os.Stat(filepath.Join(deep, "query_history.json")); err != nil {
			t.Errorf("Add did not create the directory: %v", err)
		}
	})
}

// Scenario: El limite se aplica al LEER, no solo al anadir.
func TestTheLimitAppliesWhenTheFileIsRead(t *testing.T) {
	t.Run("a file larger than the limit is trimmed on load", func(t *testing.T) {
		dir := t.TempDir()
		var entries []QueryEntry
		for i := range 20 {
			entries = append(entries, QueryEntry{SQL: "SELECT " + itoa(i)})
		}
		data, err := json.Marshal(entries)
		if err != nil {
			t.Fatal(err)
		}
		writeHistory(t, dir, string(data))

		s := NewQueryStoreLimited(dir, 5)
		if got := len(s.All()); got != 5 {
			t.Errorf("loading a 20-entry file with a limit of 5 kept %d", got)
		}
		// The survivors are the NEWEST five, not the first five.
		all := s.All()
		if all[0].SQL != "SELECT 19" {
			t.Errorf("the newest entry is %q, want SELECT 19", all[0].SQL)
		}
		if all[len(all)-1].SQL != "SELECT 15" {
			t.Errorf("the oldest survivor is %q, want SELECT 15", all[len(all)-1].SQL)
		}
	})

	t.Run("a file SMALLER than the limit is kept whole", func(t *testing.T) {
		dir := t.TempDir()
		var entries []QueryEntry
		for i := range 3 {
			entries = append(entries, QueryEntry{SQL: "SELECT " + itoa(i)})
		}
		data, _ := json.Marshal(entries)
		writeHistory(t, dir, string(data))

		s := NewQueryStoreLimited(dir, 100)
		if got := len(s.All()); got != 3 {
			t.Errorf("a three-entry file with a limit of 100 loaded %d entries", got)
		}
	})

	t.Run("a limit of zero or less means the default, never nothing", func(t *testing.T) {
		for _, limit := range []int{0, -1} {
			s := NewQueryStoreLimited(t.TempDir(), limit)
			if s.MaxEntries() != defaultMaxEntries {
				t.Errorf("a limit of %d produced a cap of %d, want the default %d", limit, s.MaxEntries(), defaultMaxEntries)
			}
			s.Add("SELECT 1")
			if got := len(s.All()); got != 1 {
				t.Errorf("a limit of %d held %d entries after one add", limit, got)
			}
		}
	})

	t.Run("the limit applies to a file written by an older version", func(t *testing.T) {
		// A real sequence: the store was built with no limit (500), the user then sets
		// ui.history_size = 2. The next start has to honour it without the user running
		// a single query first.
		dir := t.TempDir()
		s := NewQueryStore(dir) // no limit: 500
		for i := range 10 {
			s.Add("SELECT " + itoa(i))
		}
		if got := len(s.All()); got != 10 {
			t.Fatalf("the unlimited store holds %d entries", got)
		}

		reopened := NewQueryStoreLimited(dir, 2)
		if got := len(reopened.All()); got != 2 {
			t.Errorf("reopening with a limit of 2 gave %d entries", got)
		}
	})
}

// Scenario: La migracion del historial global al primer proyecto.
func TestMigrateGlobalHistory(t *testing.T) {
	stateDir := t.TempDir()
	projectDir := filepath.Join(stateDir, "projects", "myproj")

	t.Run("the global file moves into the project and is kept as a backup", func(t *testing.T) {
		global := writeHistory(t, stateDir, `[{"sql":"SELECT 1","timestamp":"2020-01-01T00:00:00Z"}]`)

		if err := MigrateGlobalHistory(stateDir, projectDir); err != nil {
			t.Fatalf("MigrateGlobalHistory: %v", err)
		}

		// The project now has the content.
		moved := filepath.Join(projectDir, "query_history.json")
		data, err := os.ReadFile(moved)
		if err != nil {
			t.Fatalf("the migrated file is not there: %v", err)
		}
		if !strings.Contains(string(data), "SELECT 1") {
			t.Errorf("the migrated file is %s, want the original content", data)
		}

		// The global is renamed, not deleted: the migration copies and then renames,
		// so a failure between the two would lose the original.
		if _, err := os.Stat(global); !os.IsNotExist(err) {
			t.Error("the global file is still there after migrating")
		}
		backup := global + ".bak"
		if _, err := os.Stat(backup); err != nil {
			t.Errorf("there is no backup at %s: %v", backup, err)
		}
		// And the migrated store reads it.
		s := NewQueryStore(projectDir)
		if got := len(s.All()); got != 1 {
			t.Errorf("the migrated store holds %d entries", got)
		}
	})

	t.Run("a project that ALREADY has a file is left alone", func(t *testing.T) {
		// The case that must not fire: the user opened the project before, so its file
		// is newer and the global one is a stale leftover. Overwriting would throw away
		// the queries that were actually run.
		writeHistory(t, stateDir, `[{"sql":"SELECT global","timestamp":"2020-01-01T00:00:00Z"}]`)
		writeHistory(t, projectDir, `[{"sql":"SELECT project","timestamp":"2020-01-02T00:00:00Z"}]`)

		if err := MigrateGlobalHistory(stateDir, projectDir); err != nil {
			t.Fatalf("MigrateGlobalHistory: %v", err)
		}

		data, err := os.ReadFile(filepath.Join(projectDir, "query_history.json"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "SELECT project") {
			t.Errorf("the project's own file was overwritten: %s", data)
		}
		if strings.Contains(string(data), "SELECT global") {
			t.Errorf("the global content was merged in: %s", data)
		}
		// And the global file survives, since nothing was migrated.
		if _, err := os.Stat(filepath.Join(stateDir, "query_history.json")); err != nil {
			t.Errorf("the global file was removed when nothing was migrated: %v", err)
		}
	})

	t.Run("no global file is not an error", func(t *testing.T) {
		// Its own state directory, not the shared one: an earlier subtest left a project
		// file there, and reusing it made this pass for the wrong reason and then fail
		// for the right one, which is worse than not writing it at all.
		freshState := t.TempDir()
		freshProject := filepath.Join(freshState, "projects", "p")

		if err := MigrateGlobalHistory(freshState, freshProject); err != nil {
			t.Errorf("migrating with no global file returned %v", err)
		}
		if _, err := os.Stat(filepath.Join(freshProject, "query_history.json")); err == nil {
			t.Error("a project file was created when there was nothing to migrate")
		}
		if _, err := os.Stat(freshProject); err == nil {
			t.Error("a project directory was created when there was nothing to migrate")
		}
	})

	t.Run("a global file that cannot be read is reported", func(t *testing.T) {
		// Unlike a corrupt file inside the project, this one IS reported: the caller has
		// not written anything yet, so a failure here means the history is still in the
		// global file and the user should hear about it.
		dir := t.TempDir()
		// A directory where the file should be: os.Stat succeeds, os.ReadFile fails.
		if err := os.MkdirAll(filepath.Join(dir, "query_history.json"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := MigrateGlobalHistory(dir, filepath.Join(dir, "p")); err == nil {
			t.Error("migrating an unreadable global file returned no error")
		}
	})
}

// Scenario: Una entrada en blanco no se guarda, y una repetida se mueve al final.
func TestAddIgnoresBlankQueriesAndMovesRepeatsToTheEnd(t *testing.T) {
	s := NewQueryStore(t.TempDir())

	for _, blank := range []string{"", "   ", "\n", "\t \n"} {
		s.Add(blank)
	}
	if got := len(s.All()); got != 0 {
		t.Errorf("blank queries produced %d entries: %v", got, s.All())
	}

	s.Add("SELECT 1")
	s.Add("SELECT 2")
	s.Add("SELECT 1")

	all := s.All()
	if len(all) != 2 {
		t.Fatalf("the history holds %d entries, want 2 after a repeat", len(all))
	}
	if all[0].SQL != "SELECT 1" {
		t.Errorf("the newest entry is %q, want the repeated SELECT 1 moved to the end", all[0].SQL)
	}

	t.Run("a repeat with different spacing is the same query", func(t *testing.T) {
		ss := NewQueryStore(t.TempDir())
		ss.Add("  SELECT 1  ")
		ss.Add("SELECT 1")
		if got := len(ss.All()); got != 1 {
			t.Errorf("the same query in different spacing produced %d entries", got)
		}
	})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	return string(d)
}
