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
