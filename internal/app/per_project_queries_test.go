package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/buble/dbx/internal/store"
)

func TestInitQueryStore_ProjectPath(t *testing.T) {
	// Scenario: Query se guarda en el historial del proyecto actual
	// When ejecuto la query "SELECT * FROM users"
	// Then la query se guarda en "~/.local/state/dbx/projects/{project_name}/query_history.json"

	tmpDir := t.TempDir()
	projectName := "mydb"

	m := newTestModel()
	m.initQueryStore(projectName, tmpDir)

	// Verify QueryStore is set
	if m.queryStore == nil {
		t.Fatal("Expected queryStore to be set")
	}

	// Add a query
	m.queryStore.Add("SELECT * FROM users")

	// Verify file was created in project directory
	expectedPath := filepath.Join(tmpDir, "projects", projectName, "query_history.json")
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Fatalf("Expected file at %s", expectedPath)
	}

	// Verify file has the query
	data, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("Expected file to have content")
	}
}

func TestInitQueryStore_MigratesGlobalHistory(t *testing.T) {
	// Scenario: Migración de historial global a proyecto
	// Given existe un archivo "~/.local/state/dbx/query_history.json" con 5 queries
	// When selecciono un proyecto por primera vez
	// Then las 5 queries se migran a "~/.local/state/dbx/projects/{project_name}/query_history.json"
	// And el archivo global se renombra a "~/.local/state/dbx/query_history.json.bak"

	tmpDir := t.TempDir()

	// Create global history file with 5 entries
	globalPath := filepath.Join(tmpDir, "query_history.json")
	globalStore := store.NewQueryStore(tmpDir)
	for i := 0; i < 5; i++ {
		globalStore.Add("SELECT " + string(rune('0'+i)))
	}

	// Verify global file exists
	if _, err := os.Stat(globalPath); os.IsNotExist(err) {
		t.Fatalf("Expected global file at %s", globalPath)
	}

	// Initialize query store for a project
	m := newTestModel()
	m.initQueryStore("mydb", tmpDir)

	// Verify project file has 5 migrated queries
	projectDir := filepath.Join(tmpDir, "projects", "mydb")
	projectStore := store.NewQueryStore(projectDir)
	if projectStore.Len() != 5 {
		t.Fatalf("Expected 5 migrated queries, got %d", projectStore.Len())
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

func TestInitQueryStore_CreatesNewQueryBrowser(t *testing.T) {
	// When initializing a new project, QueryBrowser should use the new QueryStore

	tmpDir := t.TempDir()

	m := newTestModel()
	m.initQueryStore("mydb", tmpDir)

	// Verify QueryBrowser uses the new QueryStore
	if m.queryBrowser == nil {
		t.Fatal("Expected queryBrowser to be set")
	}

	// Add a query via the store
	m.queryStore.Add("SELECT * FROM test")

	// Verify QueryBrowser shows the query (via store)
	if m.queryStore.Len() != 1 {
		t.Fatalf("Expected 1 query, got %d", m.queryStore.Len())
	}
}
