package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestQueryStore_ProjectIsolation(t *testing.T) {
	// Scenario: QueryBrowser muestra solo queries del proyecto actual
	// Given el proyecto "mydb" tiene 3 queries guardadas
	// And el proyecto "otherdb" tiene 2 queries guardadas
	// When abro el QueryBrowser con "Q"
	// Then veo solo las 3 queries del proyecto "mydb"

	tmpDir := t.TempDir()
	mydbDir := filepath.Join(tmpDir, "projects", "mydb")
	otherdbDir := filepath.Join(tmpDir, "projects", "otherdb")

	// Create stores for each project
	mydbStore := NewQueryStore(mydbDir)
	otherdbStore := NewQueryStore(otherdbDir)

	// Add 3 queries to mydb
	mydbStore.Add("SELECT * FROM users")
	mydbStore.Add("SELECT * FROM orders")
	mydbStore.Add("SELECT * FROM products")

	// Add 2 queries to otherdb
	otherdbStore.Add("SELECT 1")
	otherdbStore.Add("SELECT 2")

	// Verify mydb has 3 queries
	if mydbStore.Len() != 3 {
		t.Fatalf("Expected mydb to have 3 queries, got %d", mydbStore.Len())
	}

	// Verify otherdb has 2 queries
	if otherdbStore.Len() != 2 {
		t.Fatalf("Expected otherdb to have 2 queries, got %d", otherdbStore.Len())
	}

	// Verify mydb queries don't contain otherdb queries
	for _, e := range mydbStore.All() {
		if e.SQL == "SELECT 1" || e.SQL == "SELECT 2" {
			t.Fatalf("mydb store should not contain otherdb query: %s", e.SQL)
		}
	}

	// Verify otherdb queries don't contain mydb queries
	for _, e := range otherdbStore.All() {
		if e.SQL == "SELECT * FROM users" || e.SQL == "SELECT * FROM orders" || e.SQL == "SELECT * FROM products" {
			t.Fatalf("otherdb store should not contain mydb query: %s", e.SQL)
		}
	}
}

func TestQueryStore_FavoritesPerProject(t *testing.T) {
	// Scenario: Favoritos se mantienen por proyecto
	// Given el proyecto "mydb" tiene una query favorita "SELECT count(*) FROM orders"
	// When cambio al proyecto "otherdb"
	// And abro el QueryBrowser con "Q"
	// Then no veo la query favorita del proyecto "mydb"

	tmpDir := t.TempDir()
	mydbDir := filepath.Join(tmpDir, "projects", "mydb")
	otherdbDir := filepath.Join(tmpDir, "projects", "otherdb")

	// Create stores
	mydbStore := NewQueryStore(mydbDir)
	otherdbStore := NewQueryStore(otherdbDir)

	// Add query to mydb and mark as favorite
	mydbStore.Add("SELECT count(*) FROM orders")
	mydbStore.ToggleFavorite(0)

	// Verify mydb has 1 favorite
	mydbFavs := mydbStore.Favorites()
	if len(mydbFavs) != 1 {
		t.Fatalf("Expected mydb to have 1 favorite, got %d", len(mydbFavs))
	}

	// Verify otherdb has 0 favorites
	otherdbFavs := otherdbStore.Favorites()
	if len(otherdbFavs) != 0 {
		t.Fatalf("Expected otherdb to have 0 favorites, got %d", len(otherdbFavs))
	}

	// Verify mydb favorite is not in otherdb
	for _, e := range otherdbStore.All() {
		if e.SQL == "SELECT count(*) FROM orders" {
			t.Fatal("otherdb should not contain mydb's favorite query")
		}
	}
}

func TestQueryStore_SavePath(t *testing.T) {
	// Scenario: Query se guarda en el historial del proyecto actual
	// When ejecuto la query "SELECT * FROM users"
	// Then la query se guarda en "~/.local/state/dbx/projects/{project_name}/query_history.json"

	tmpDir := t.TempDir()
	store := NewQueryStore(tmpDir)

	// Add a query
	store.Add("SELECT * FROM users")

	// Verify file was created at the correct path
	expectedPath := filepath.Join(tmpDir, "query_history.json")
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Fatalf("Expected file to exist at %s", expectedPath)
	}

	// Verify file content
	data, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("Expected file to have content")
	}
}

func TestQueryStore_HasTimestamp(t *testing.T) {
	// Scenario: Query se guarda en el historial del proyecto actual
	// And la query tiene timestamp actual

	tmpDir := t.TempDir()
	store := NewQueryStore(tmpDir)

	before := time.Now().Truncate(time.Second)
	store.Add("SELECT 1")
	after := time.Now().Add(time.Second).Truncate(time.Second)

	entries := store.All()
	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(entries))
	}

	ts := entries[0].Timestamp
	if ts.Before(before) || ts.After(after) {
		t.Fatalf("Expected timestamp between %v and %v, got %v", before, after, ts)
	}
}

func TestQueryStore_NotFavoriteByDefault(t *testing.T) {
	// Scenario: Query se guarda en el historial del proyecto actual
	// And la query no está marcada como favorita

	tmpDir := t.TempDir()
	store := NewQueryStore(tmpDir)

	store.Add("SELECT * FROM users")

	entries := store.All()
	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(entries))
	}

	if entries[0].Favorite {
		t.Fatal("New query should not be marked as favorite")
	}
}

func TestQueryStore_GlobalMigration(t *testing.T) {
	// Scenario: Migración de historial global a proyecto
	// Given existe un archivo "~/.local/state/dbx/query_history.json" con 5 queries
	// When selecciono un proyecto por primera vez
	// Then las 5 queries se migran a "~/.local/state/dbx/projects/{project_name}/query_history.json"
	// And el archivo global se renombra a "~/.local/state/dbx/query_history.json.bak"

	tmpDir := t.TempDir()

	// Create global history file
	globalPath := filepath.Join(tmpDir, "query_history.json")
	entries := []QueryEntry{
		{SQL: "SELECT 1", Timestamp: time.Now()},
		{SQL: "SELECT 2", Timestamp: time.Now()},
		{SQL: "SELECT 3", Timestamp: time.Now()},
		{SQL: "SELECT 4", Timestamp: time.Now()},
		{SQL: "SELECT 5", Timestamp: time.Now()},
	}

	// Write global file
	data := mustMarshal(t, entries)
	if err := os.WriteFile(globalPath, data, 0o644); err != nil {
		t.Fatalf("Failed to write global file: %v", err)
	}

	// Create project directory
	projectDir := filepath.Join(tmpDir, "projects", "mydb")

	// Run migration
	err := MigrateGlobalHistory(tmpDir, projectDir)
	if err != nil {
		t.Fatalf("Migration failed: %v", err)
	}

	// Verify project file exists
	projectPath := filepath.Join(projectDir, "query_history.json")
	if _, err := os.Stat(projectPath); os.IsNotExist(err) {
		t.Fatalf("Expected project file at %s", projectPath)
	}

	// Verify project file has 5 entries
	store := NewQueryStore(projectDir)
	if store.Len() != 5 {
		t.Fatalf("Expected 5 migrated queries, got %d", store.Len())
	}

	// Verify global file was renamed to .bak
	bakPath := globalPath + ".bak"
	if _, err := os.Stat(bakPath); os.IsNotExist(err) {
		t.Fatalf("Expected .bak file at %s", bakPath)
	}

	// Verify original global file no longer exists
	if _, err := os.Stat(globalPath); !os.IsNotExist(err) {
		t.Fatalf("Expected original global file to be removed")
	}
}

func TestQueryStore_ProjectNameAsDirectory(t *testing.T) {
	// Scenario: Nombre de proyecto válido para directorio
	// Given el proyecto tiene conexión name "my-project_db"
	// When creo el directorio de almacenamiento
	// Then el directorio es "~/.local/state/dbx/projects/my-project_db/"

	tmpDir := t.TempDir()
	projectName := "my-project_db"

	// Create project state directory
	projectDir := filepath.Join(tmpDir, "projects", projectName)
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("Failed to create project directory: %v", err)
	}

	// Verify directory was created
	if _, err := os.Stat(projectDir); os.IsNotExist(err) {
		t.Fatalf("Expected directory at %s", projectDir)
	}

	// Create store in that directory
	store := NewQueryStore(projectDir)
	store.Add("SELECT 1")

	// Verify file was created
	expectedPath := filepath.Join(projectDir, "query_history.json")
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Fatalf("Expected file at %s", expectedPath)
	}
}

// Scenario: El historial y los favoritos se muestran del más reciente al más
// antiguo. Reversal is only observable with an odd number of entries, and the
// favorites list is the only one the browser can reorder.
func TestQueryStore_FavoritesAreNewestFirst(t *testing.T) {
	// Scenario: 5 queries, all marked favorite
	// When consulto Favorites()
	// Then veo la última insertada primero

	tmpDir := t.TempDir()
	store := NewQueryStore(tmpDir)

	want := make([]string, 0, 5)
	for i := 1; i <= 5; i++ {
		sql := "SELECT " + strconv.Itoa(i)
		store.Add(sql)
		want = append([]string{sql}, want...) // newest first
	}
	for i := 0; i < 5; i++ {
		store.ToggleFavorite(i)
	}

	favs := store.Favorites()
	if len(favs) != 5 {
		t.Fatalf("len(Favorites) = %d, want 5", len(favs))
	}
	for i, f := range favs {
		if f.SQL != want[i] {
			t.Errorf("Favorites[%d] = %q, want %q (newest first)", i, f.SQL, want[i])
		}
	}

	// The same contract for the unfiltered history.
	all := store.All()
	for i, e := range all {
		if e.SQL != want[i] {
			t.Errorf("All()[%d] = %q, want %q (newest first)", i, e.SQL, want[i])
		}
	}
}

// Scenario: Un índice fuera de rango no marca nada como favorito. The index
// arrives from the rendered list, so an off-by-one must be a no-op, not a crash.
func TestQueryStore_ToggleFavoriteOutOfRangeIsNoOp(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewQueryStore(tmpDir)
	store.Add("SELECT 1")

	store.ToggleFavorite(-1)              // before the start
	store.ToggleFavorite(store.Len())     // one past the end
	store.ToggleFavorite(store.Len() + 7) // far out of range

	if favs := store.Favorites(); len(favs) != 0 {
		t.Fatalf("Favorites = %+v, want none (out-of-range index must be a no-op)", favs)
	}
	if store.Len() != 1 {
		t.Fatalf("Len = %d, want 1 (the history must be untouched)", store.Len())
	}

	// A rejected index must not leave the store in a state that breaks the
	// next valid one.
	store.ToggleFavorite(0)
	favs := store.Favorites()
	if len(favs) != 1 || favs[0].SQL != "SELECT 1" {
		t.Fatalf("Favorites = %+v, want [SELECT 1] after a valid index following rejected ones", favs)
	}
}

// Scenario: Si el rename del historial global falla, la migración debe
// reportarlo. The caller decides what to do about a half-migrated project, and
// a silent nil here means a lost history with no signal.
func TestMigrateGlobalHistory_FailedRenameIsReported(t *testing.T) {
	tmpDir := t.TempDir()
	globalPath := filepath.Join(tmpDir, "query_history.json")
	projectDir := filepath.Join(tmpDir, "projects", "mydb")

	if err := os.WriteFile(globalPath, mustMarshal(t, []QueryEntry{{SQL: "SELECT 1"}}), 0o644); err != nil {
		t.Fatalf("WriteFile(global): %v", err)
	}
	// Make os.Rename fail: the destination already exists as a non-empty dir.
	bakPath := globalPath + ".bak"
	if err := os.MkdirAll(bakPath, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", bakPath, err)
	}
	if err := os.WriteFile(filepath.Join(bakPath, "blocker"), []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile(blocker): %v", err)
	}

	if err := MigrateGlobalHistory(tmpDir, projectDir); err == nil {
		t.Fatal("MigrateGlobalHistory returned nil after a failed rename, swallowing the error")
	}
}

// mustMarshal encodes a history fixture, failing the test rather than returning
// an empty buffer: a silent marshal error would surface as a confusing
// assertion failure in the caller.
func mustMarshal(t *testing.T, entries []QueryEntry) []byte {
	t.Helper()
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent: %v", err)
	}
	return data
}

// --- the three operations that look an entry up by position ------------------

// Scenario: Poner nombre y borrar por indice, y un indice invalido no hace nada.
//
// Both take an index into All(), which is newest-first, and both return silently
// on an index that is not there. A stale index is ordinary: the list changes
// underneath a selection when a query is re-run, and panicking on a stale click is
// not an option.
func TestQueryStore_SetNameAndDelete_IgnoreAnIndexOutOfRange(t *testing.T) {
	s := NewQueryStore(t.TempDir())
	s.Add("SELECT 1")
	s.Add("SELECT 2")
	s.Add("SELECT 3")
	before := s.All()

	// The list deliberately includes 0 and 1, which are VALID indices, and the
	// next test proves they still work. A guard written as `idx < 0 ||` that
	// relaxed to `idx <= 0` would reject 0 and silently refuse to rename the
	// newest query, which is the one a user renames most.
	for _, idx := range []int{-1, -100, 3, 4, 1000} {
		// The calls are wrapped rather than made directly because the bug this
		// guards is an INDEX, and an out-of-range index panics instead of
		// failing. A panicking test still fails the run, but it fails as a crash:
		// the mutation harness reads a crash as "the mutant survived" and the
		// guard would look untested while in fact it is what stops the panic.
		// Recovering and failing turns the same signal into a normal assertion.
		if p := callCatchingPanic(func() { s.SetName(idx, "should not stick") }); p != nil {
			t.Errorf("SetName(%d) panicked: %v", idx, p)
		}
		if got := s.All(); len(got) != len(before) {
			t.Fatalf("SetName(%d) changed the entry count to %d", idx, len(got))
		}
		if p := callCatchingPanic(func() { s.Delete(idx) }); p != nil {
			t.Errorf("Delete(%d) panicked: %v", idx, p)
		}
		if got := s.All(); len(got) != len(before) {
			t.Fatalf("Delete(%d) changed the entry count to %d", idx, len(got))
		}
	}
	// Nothing was renamed either.
	for _, e := range s.All() {
		if e.Name == "should not stick" {
			t.Errorf("a name stuck to %q from an out-of-range index", e.SQL)
		}
	}
}

// Scenario: El índice 0 y el 1 son válidos y funcionan.
//
// The out-of-range guard above has to reject what is out of range and NOTHING
// else. A `>= 0` relaxed to `> 0` would reject 0, and 0 is the newest query —
// the one a user is most likely to name after running it.
func TestQueryStore_SetNameAndDelete_WorkOnTheFirstTwoIndices(t *testing.T) {
	s := NewQueryStore(t.TempDir())
	s.Add("one")
	s.Add("two")

	// All() is newest-first, so index 0 is "two" and index 1 is "one".
	if got := s.All()[0].SQL; got != "two" {
		t.Fatalf("fixture is wrong: index 0 is %q, want two", got)
	}

	s.SetName(0, "newest")
	s.SetName(1, "oldest")
	after := s.All()
	if after[0].Name != "newest" {
		t.Errorf("index 0 is named %q, want newest", after[0].Name)
	}
	if after[1].Name != "oldest" {
		t.Errorf("index 1 is named %q, want oldest", after[1].Name)
	}

	// And index 0 deletes, which would panic if the guard let it through as
	// "one past the end".
	if p := callCatchingPanic(func() { s.Delete(0) }); p != nil {
		t.Errorf("Delete(0) panicked: %v", p)
	}
	left := s.All()
	if len(left) != 1 || left[0].SQL != "one" {
		t.Errorf("after Delete(0) the survivors are %q, want just one", sqlOf(left))
	}
}

// Scenario: Poner nombre y borrar por indice valido, y el indice es el de All.
//
// All() is newest-first, so index 0 is the most recent query. Getting that
// backwards would rename or delete the wrong row, which is silent and then
// permanent once it is saved.
func TestQueryStore_SetNameAndDelete_ActOnTheIndexAllReports(t *testing.T) {
	s := NewQueryStore(t.TempDir())
	s.Add("oldest")
	s.Add("middle")
	s.Add("newest")

	all := s.All()
	if all[0].SQL != "newest" || all[2].SQL != "oldest" {
		t.Fatalf("All() = %q, %q, %q, want newest first", all[0].SQL, all[1].SQL, all[2].SQL)
	}

	s.SetName(1, "renamed")
	after := s.All()
	if after[1].Name != "renamed" {
		t.Errorf("index 1 is %q named %q, want it named renamed", after[1].SQL, after[1].Name)
	}
	if after[0].Name != "" || after[2].Name != "" {
		t.Errorf("the other entries were named too: %q / %q", after[0].Name, after[2].Name)
	}

	s.Delete(1)
	rest := s.All()
	if len(rest) != 2 {
		t.Fatalf("after Delete there are %d entries, want 2", len(rest))
	}
	for _, e := range rest {
		if e.SQL == "middle" {
			t.Error("Delete(1) removed the wrong entry: middle is still there")
		}
	}
	if rest[0].SQL != "newest" || rest[1].SQL != "oldest" {
		t.Errorf("the survivors are %q, %q, want newest and oldest", rest[0].SQL, rest[1].SQL)
	}
}

// Scenario: La búsqueda del objetivo salta las entradas que no encajan.
//
// SetName and Delete find their target by matching SQL AND timestamp, walking
// entries in insertion order. Every entry before the target has to be compared
// and rejected, so a store with several entries is what exercises the loop rather
// than just its first iteration.
func TestQueryStore_SetNameAndDelete_ScanPastNonMatchingEntries(t *testing.T) {
	s := NewQueryStore(t.TempDir())
	for _, q := range []string{"a", "b", "c", "d", "e"} {
		s.Add(q)
	}
	// "b" is at index 3 of 5 in All(), which is the FOURTH newest-first, so the
	// scan has to reject three entries before it matches.
	if got := s.All()[3].SQL; got != "b" {
		t.Fatalf("fixture is wrong: index 3 is %q, want b", got)
	}

	s.SetName(3, "found-it")
	for i, e := range s.All() {
		if e.SQL == "b" {
			if e.Name != "found-it" {
				t.Errorf("b is at index %d and named %q, want found-it", i, e.Name)
			}
		} else if e.Name != "" {
			t.Errorf("the scan named the wrong entry: %q got %q", e.SQL, e.Name)
		}
	}

	s.Delete(3)
	for _, e := range s.All() {
		if e.SQL == "b" {
			t.Error("the scan deleted the wrong entry")
		}
	}
	if len(s.All()) != 4 {
		t.Errorf("after Delete there are %d entries, want 4", len(s.All()))
	}
}

// --- the dedupe and the cap --------------------------------------------------

// Scenario: Una consulta repetida se mueve al final en vez de duplicarse.
//
// History is a recency list, so re-running a query has to make it the most recent
// entry. Appending a second copy would show the same SQL twice and make the list
// grow for no reason.
func TestQueryStore_Add_MovesARepeatedQueryToTheEnd(t *testing.T) {
	s := NewQueryStore(t.TempDir())
	s.Add("one")
	s.Add("two")
	s.Add("three")

	// Re-run the oldest one, which is the furthest from the end.
	s.Add("one")

	all := s.All()
	if len(all) != 3 {
		t.Fatalf("after re-running there are %d entries, want 3: %q", len(all), sqlOf(all))
	}
	if all[0].SQL != "one" {
		t.Errorf("the re-run query is at index %d, want it most recent: %q", indexOfSQL(all, "one"), sqlOf(all))
	}
	// And the other two kept their relative order.
	if all[1].SQL != "three" || all[2].SQL != "two" {
		t.Errorf("the survivors are %q, %q, want three then two", all[1].SQL, all[2].SQL)
	}

	// Re-running something already most recent changes nothing.
	s.Add("one")
	if got := s.All(); len(got) != 3 || got[0].SQL != "one" {
		t.Errorf("re-running the most recent gave %q, want it unchanged at three entries", sqlOf(got))
	}
}

// Scenario: La comparación del dedupe ignora los espacios de los extremos.
//
// Add trims what it stores and compares trimmed, so "  SELECT 1  " and "SELECT 1"
// are the same query. Without that, whitespace differences would silently fill the
// history with duplicates of the same statement.
func TestQueryStore_Add_DedupeIgnoresSurroundingWhitespace(t *testing.T) {
	s := NewQueryStore(t.TempDir())
	s.Add("SELECT 1")
	s.Add("  SELECT 1  ")
	s.Add("\tSELECT 1\n")

	all := s.All()
	if len(all) != 1 {
		t.Fatalf("three spellings of one query produced %d entries: %q", len(all), sqlOf(all))
	}
	if all[0].SQL != "SELECT 1" {
		t.Errorf("the stored SQL is %q, want it trimmed", all[0].SQL)
	}

	// And an empty or whitespace-only query is not stored at all.
	for _, empty := range []string{"", "   ", "\n\t "} {
		s.Add(empty)
	}
	if got := s.All(); len(got) != 1 {
		t.Errorf("an empty query was stored: %q", sqlOf(got))
	}
}

// Scenario: El historial se corta a 500 entradas, y se corta por el final.
//
// The cap keeps the file from growing without bound. What must be kept is the
// RECENT half: dropping the oldest is the only sensible choice, and keeping the
// oldest would silently make the most recent queries disappear.
func TestQueryStore_Add_CapsAtFiveHundredKeepingTheNewest(t *testing.T) {
	s := NewQueryStore(t.TempDir())
	// 500 is slow through the file system, so the cap is exercised through the
	// in-memory path and the assertions are about WHICH entries survive.
	for i := 0; i < 499; i++ {
		s.Add("q" + strconv.Itoa(i))
	}
	if got := s.All(); len(got) != 499 {
		t.Fatalf("after 499 adds there are %d entries, want 499", len(got))
	}

	// One more is still fine: the cap is "more than 500", not "500 or more".
	s.Add("q499")
	if got := s.All(); len(got) != 500 {
		t.Fatalf("after 500 adds there are %d entries, want 500", len(got))
	}

	// The 501st evicts exactly one, the oldest.
	s.Add("q500")
	all := s.All()
	if len(all) != 500 {
		t.Fatalf("after 501 adds there are %d entries, want 500", len(all))
	}
	if all[0].SQL != "q500" {
		t.Errorf("the newest entry is %q, want q500", all[0].SQL)
	}
	if all[len(all)-1].SQL != "q1" {
		t.Errorf("the oldest surviving entry is %q, want q1: the cap must drop the oldest, not the newest", all[len(all)-1].SQL)
	}
	for _, e := range all {
		if e.SQL == "q0" {
			t.Error("q0 survived: the cap kept the oldest instead of the newest")
		}
	}
}

// callCatchingPanic runs f and returns whatever it panicked with, or nil. It
// exists so an index bug is reported as a test failure rather than as a crash.
func callCatchingPanic(f func()) (recovered any) {
	defer func() { recovered = recover() }()
	f()
	return nil
}

func sqlOf(entries []QueryEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.SQL
	}
	return out
}

func indexOfSQL(entries []QueryEntry, sql string) int {
	for i, e := range entries {
		if e.SQL == sql {
			return i
		}
	}
	return -1
}
