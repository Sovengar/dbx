package app

// Scenario: Las cargas que van a la base de datos traen la fila que se pidio.
//
// The cache-hit branch of the FK sidebar was already covered without a database, because it
// returns before the fetch is built. Everything else in this file is the MISS side: the
// actual query, the message it produces, and the shape of that message. Those need a real
// PostgreSQL, and the loader's own queries are tested in the driver package — what is
// untested is the glue, and the glue is where the previous two rounds found their bugs.
//
// What is asserted is not "the command returned something" but the fields the handlers
// read. An empty message that a handler treats as "loaded, nothing there" is the failure
// this file is shaped to catch.

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/drivers/postgres"
)

// ---------------------------------------------------------------------------
// loadTableData
// ---------------------------------------------------------------------------

func TestLoadTableDataReturnsTheRows(t *testing.T) {
	dsn := testDSN(t)
	conn := connectTestDB(t, dsn)
	schema, table := metadataFixture(t, conn)

	t.Run("the whole table", func(t *testing.T) {
		m := routerModelLoaded(t)
		m.conn = conn

		msg, ok := m.loadTableData(schema, table)().(tableDataLoadedMsg)
		if !ok {
			t.Fatal("the command did not produce a tableDataLoadedMsg")
		}
		if msg.err != nil {
			t.Fatalf("loadTableData: %v", msg.err)
		}
		if msg.schema != schema || msg.table != table {
			t.Errorf("the message is for %s.%s", msg.schema, msg.table)
		}
		if msg.result == nil {
			t.Fatal("the message carries no result")
		}
		if msg.result.Count != 2 {
			t.Errorf("the result has %d rows, want 2", msg.result.Count)
		}
		if len(msg.result.Columns) != 6 {
			t.Errorf("the result has %d columns, want 6", len(msg.result.Columns))
		}
	})

	t.Run("a WHERE clause narrows it", func(t *testing.T) {
		// This is the where-filter the grid builds from the user's filter row. A filter
		// that returns the whole table looks like a filter that was ignored.
		m := routerModelLoaded(t)
		m.conn = conn

		msg, ok := m.loadTableDataWithWhere(schema, table, "id = 1")().(tableDataLoadedMsg)
		if !ok {
			t.Fatal("the command did not produce a tableDataLoadedMsg")
		}
		if msg.err != nil {
			t.Fatalf("loadTableDataWithWhere: %v", msg.err)
		}
		if msg.result.Count != 1 {
			t.Errorf("the filter returned %d rows, want 1", msg.result.Count)
		}
		if msg.where != "id = 1" {
			t.Errorf("the message reports the filter as %q; the grid needs it to rebuild the state", msg.where)
		}
	})

	t.Run("a trailing semicolon in the filter does not break the query", func(t *testing.T) {
		// The grid appends ";" when the user finishes typing a filter, so a statement that
		// arrives already terminated has to work: the loader appends its own, and two of
		// them is a syntax error the user cannot act on.
		m := routerModelLoaded(t)
		m.conn = conn

		for _, where := range []string{"id = 1;", "id = 1 ;", "  id = 1  ", "  id = 1;  "} {
			msg := m.loadTableDataWithWhere(schema, table, where)().(tableDataLoadedMsg)
			if msg.err != nil {
				t.Errorf("the filter %q failed: %v", where, msg.err)
				continue
			}
			if msg.result.Count != 1 {
				t.Errorf("the filter %q returned %d rows, want 1", where, msg.result.Count)
			}
		}
	})

	t.Run("a sort orders the rows", func(t *testing.T) {
		m := routerModelLoaded(t)
		m.conn = conn

		ascending, ok := m.loadTableDataWithSortAndWhere(schema, table, "id", "asc", "")().(tableDataLoadedMsg)
		if !ok {
			t.Fatal("the command did not produce a tableDataLoadedMsg")
		}
		if ascending.err != nil {
			t.Fatalf("ascending: %v", ascending.err)
		}
		if first := fmt.Sprintf("%v", ascending.result.Rows[0][0]); first != "1" {
			t.Errorf("ascending starts at %v, want 1", first)
		}

		descending := m.loadTableDataWithSortAndWhere(schema, table, "id", "desc", "")().(tableDataLoadedMsg)
		if descending.err != nil {
			t.Fatalf("descending: %v", descending.err)
		}
		if first := fmt.Sprintf("%v", descending.result.Rows[0][0]); first != "2" {
			t.Errorf("descending starts at %v, want 2", first)
		}
	})

	t.Run("a table that does not exist reports the failure", func(t *testing.T) {
		// The error has to survive to the message: the grid shows it, and "SELECT failed"
		// with the database's own text is the difference between a typo the user can fix
		// and a blank grid they cannot explain.
		m := routerModelLoaded(t)
		m.conn = conn

		msg := m.loadTableData(schema, "no_such_table")().(tableDataLoadedMsg)
		if msg.err == nil {
			t.Fatal("selecting a table that does not exist returned no error")
		}
		if !strings.Contains(msg.err.Error(), "SELECT failed") {
			t.Errorf("the error is %q, want it to say which operation failed", msg.err)
		}
		if msg.result != nil {
			t.Errorf("a failed select carried a result: %+v", msg.result)
		}
	})

	t.Run("a column that does not exist reports the failure", func(t *testing.T) {
		// The sort column comes from a header the user clicked, so a stale one reaches
		// here — a table refreshed between the click and the query, or a column dropped.
		m := routerModelLoaded(t)
		m.conn = conn

		msg := m.loadTableDataWithSortAndWhere(schema, table, "dropped_column", "asc", "")().(tableDataLoadedMsg)
		if msg.err == nil {
			t.Error("sorting by a column that does not exist returned no error")
		}
	})
}

// ---------------------------------------------------------------------------
// loadExplorerPreviewData
// ---------------------------------------------------------------------------

func TestLoadExplorerPreviewDataBringsEveryTab(t *testing.T) {
	dsn := testDSN(t)
	conn := connectTestDB(t, dsn)
	schema, table := metadataFixture(t, conn)

	m := routerModelLoaded(t)
	m.conn = conn

	msg, ok := m.loadExplorerPreviewData(schema, table)().(explorerPreviewDataMsg)
	if !ok {
		t.Fatal("the command did not produce an explorerPreviewDataMsg")
	}

	if msg.schema != schema || msg.table != table {
		t.Errorf("the message is for %s.%s", msg.schema, msg.table)
	}
	if len(msg.columns) != 6 {
		t.Errorf("the Columns tab has %d columns, want 6", len(msg.columns))
	}
	// The six tabs are Columns, Constraints, Foreign Keys, Indexes and Overview. Four of
	// the five are filled from this message, so an empty one is a tab the user opens and
	// finds blank.
	if len(msg.constraints) == 0 {
		t.Error("the Constraints tab is empty")
	}
	if len(msg.foreignKeys) == 0 {
		t.Error("the Foreign Keys tab is empty")
	}
	if len(msg.indexes) == 0 {
		t.Error("the Indexes tab is empty")
	}
	if msg.overview == nil {
		t.Error("the Overview tab has no overview")
	}
}

func TestLoadExplorerPreviewDataOnAMissingTableIsEmptyNotFatal(t *testing.T) {
	// Same shape as the metadata load, and the same reasoning: the explorer offers a table
	// that another session dropped, and a blank preview is the honest answer. Each of the
	// five loads swallows its own error independently, so the assertion is that ONE
	// failing does not take the others down with it.
	dsn := testDSN(t)
	conn := connectTestDB(t, dsn)
	schema, _ := metadataFixture(t, conn)

	m := routerModelLoaded(t)
	m.conn = conn

	msg := m.loadExplorerPreviewData(schema, "no_such_table")().(explorerPreviewDataMsg)
	if len(msg.columns) != 0 || len(msg.constraints) != 0 || len(msg.foreignKeys) != 0 || len(msg.indexes) != 0 {
		t.Errorf("a missing table brought data: %+v", msg)
	}
	if msg.overview != nil {
		t.Errorf("a missing table produced an overview: %+v", msg.overview)
	}
	if msg.schema != schema || msg.table != "no_such_table" {
		t.Errorf("the message names %s.%s, so the pane cannot say which table came back empty", msg.schema, msg.table)
	}
}

// ---------------------------------------------------------------------------
// loadAutocompleteData
// ---------------------------------------------------------------------------

func TestLoadAutocompleteDataBuildsTheExportTheAIAndThePopupUse(t *testing.T) {
	dsn := testDSN(t)
	conn := connectTestDB(t, dsn)
	schema, table := metadataFixture(t, conn)

	// A model that has finished a schema load: dbName and schemaDetail are what
	// loadAutocompleteData reads, and the handler that consumes the message is what puts
	// the export into the editor's autocomplete.
	m := routerModelLoaded(t)
	m.conn = conn
	m.dbName = "dbx_test"
	m.schemaDetail = []postgres.SchemaDetail{{
		Name:   schema,
		Tables: []postgres.TableDetail{{Name: table, Columns: []postgres.ColumnInfo{{Name: "id"}}}},
	}}

	msg, ok := m.loadAutocompleteData()().(autocompleteDataLoadedMsg)
	if !ok {
		t.Fatal("the command did not produce an autocompleteDataLoadedMsg")
	}
	if msg.schemaExport == nil {
		t.Fatal("the message carries no export")
	}
	if msg.schemaExport.Database != "dbx_test" {
		t.Errorf("the export names the database %q", msg.schemaExport.Database)
	}
	if len(msg.schemaExport.Schemas) != 1 || msg.schemaExport.Schemas[0].Name != schema {
		t.Fatalf("the export has schemas %+v", msg.schemaExport.Schemas)
	}

	t.Run("the indexes are in the export, which is where the AI learns them", func(t *testing.T) {
		// The autocomplete needs them to rank columns, and the AI prompt uses them to know
		// which column is the key. A missing index means the AI cannot tell a unique column
		// from any other.
		tables := msg.schemaExport.Schemas[0].Tables
		if len(tables) != 1 {
			t.Fatalf("the schema has %d tables", len(tables))
		}
		if len(tables[0].Indexes) == 0 {
			t.Error("the table has no indexes in the export")
		}
	})

	t.Run("the foreign keys are in the export", func(t *testing.T) {
		tables := msg.schemaExport.Schemas[0].Tables
		if len(tables[0].FKs) == 0 {
			t.Error("the table has no foreign keys in the export")
		}
	})

	t.Run("the keys are flattened for the ERE diagram, keyed by TABLE", func(t *testing.T) {
		// The ERE diagram needs every table's keys in one map keyed by table name, because
		// it draws edges between boxes and does not care which schema each is in. Keyed by
		// schema instead, the diagram would show one isolated box.
		if len(msg.schemaForeignKeys) == 0 {
			t.Fatal("no foreign keys were flattened")
		}
		found := false
		for tableName, fks := range msg.schemaForeignKeys {
			if tableName == table {
				found = true
				if len(fks) == 0 {
					t.Errorf("the table %q has an empty key list", tableName)
				}
			}
		}
		if !found {
			t.Errorf("the flattened map is keyed %v, want the table name %q", keysOfStringMap(msg.schemaForeignKeys), table)
		}
	})
}

func TestLoadAutocompleteDataWithNoSchemaLoaded(t *testing.T) {
	// Between starting up and the schema arriving, this runs with nothing. It must produce
	// an empty export rather than a nil one: the handler assigns it into the editor and a
	// nil export is a nil dereference on the next keystroke.
	dsn := testDSN(t)
	conn := connectTestDB(t, dsn)

	m := routerModelLoaded(t)
	m.conn = conn
	m.schemaDetail = nil

	msg, ok := m.loadAutocompleteData()().(autocompleteDataLoadedMsg)
	if !ok {
		t.Fatal("the command did not produce an autocompleteDataLoadedMsg")
	}
	if msg.schemaExport == nil {
		t.Fatal("the export is nil with no schema loaded")
	}
	if len(msg.schemaExport.Schemas) != 0 {
		t.Errorf("the export has %d schemas", len(msg.schemaExport.Schemas))
	}
	// Empty, not nil: the map is made before the loop, so the handler can append to it
	// without a guard. Ranging over a nil map is legal; writing to one is not.
	if msg.schemaForeignKeys == nil {
		t.Error("the flattened key map is nil; the handler appends to it")
	}
}

// ---------------------------------------------------------------------------
// fetchGridSidebarFKPreview — the MISS side, which actually queries
// ---------------------------------------------------------------------------

func TestFetchGridSidebarFKPreviewResolvesTheReference(t *testing.T) {
	dsn := testDSN(t)
	conn := connectTestDB(t, dsn)
	schema, table := metadataFixture(t, conn)

	fk := postgres.ForeignKeyInfo{
		Column: "user_id", RefSchema: schema, RefTable: "users", RefColumn: "id",
	}

	t.Run("the referenced row comes back", func(t *testing.T) {
		m := routerModelLoaded(t)
		m.conn = conn
		m.prevSchema = schema

		cmd := m.fetchGridSidebarFKPreview(&fk, int64(1), 7)
		if cmd == nil {
			t.Fatal("no command was produced")
		}
		msg, ok := cmd().(GridSidebarFKPreviewLookupResultMsg)
		if !ok {
			t.Fatal("the command did not produce a lookup result")
		}
		if msg.Err != nil {
			t.Fatalf("the lookup failed: %v", msg.Err)
		}
		if msg.Token != 7 {
			t.Errorf("the message carries token %d, want 7 — the token is what makes a slow reply attachable to the right cursor position", msg.Token)
		}
		if msg.RefTable != "users" {
			t.Errorf("the message names the table %q", msg.RefTable)
		}
		if len(msg.Row) == 0 {
			t.Fatal("the message carries no row")
		}
		if got := fmt.Sprintf("%v", msg.Row[0]); got != "1" {
			t.Errorf("the row starts at %v, want the id 1", got)
		}
		// The columns come from the result, and a sidebar with rows but no column names
		// shows raw JSON keys instead of a table.
		if len(msg.Columns) != len(msg.Row) {
			t.Errorf("the message has %d columns and %d row values", len(msg.Columns), len(msg.Row))
		}
		if len(msg.Columns) == 0 || msg.Columns[0] != "id" {
			t.Errorf("the columns are %v, want the referenced table's own", msg.Columns)
		}
	})

	t.Run("the cache key is the one the hit path looks up", func(t *testing.T) {
		// The miss fills the cache under this key and the hit path reads it. If the two
		// built the key differently, the cache would fill and never hit — the sidebar
		// would query on every cursor move and no test would fail.
		m := routerModelLoaded(t)
		m.conn = conn
		m.prevSchema = schema
		m.grid.SetData(&postgres.QueryResult{
			Columns: []postgres.ColumnInfo{{Name: "user_id"}},
			Rows:    [][]interface{}{{int64(1)}},
		}, schema, table)
		m.grid.SetWidth(80)
		m.grid.SetHeight(20)

		msg, ok := m.fetchGridSidebarFKPreview(&fk, int64(1), 1)().(GridSidebarFKPreviewLookupResultMsg)
		if !ok {
			t.Fatal("the command did not produce a lookup result")
		}
		want := gridSidebarFKPreviewCacheKey(schema, table, "user_id")
		if msg.CacheKey != want {
			t.Errorf("the message carries the key %q, want %q", msg.CacheKey, want)
		}
		if msg.CacheVal != int64(1) {
			t.Errorf("the message carries the cache value %v, want the value that was looked up", msg.CacheVal)
		}
	})

	t.Run("a value with NO referenced row is an error, not an empty pane", func(t *testing.T) {
		// The case that happens for real: a foreign key whose target row was deleted, or a
		// value from a table loaded before the key was added. Returning an empty result
		// would show a blank sidebar with no explanation, which reads as "there is no such
		// user" rather than "this lookup found nothing".
		m := routerModelLoaded(t)
		m.conn = conn
		m.prevSchema = schema

		msg, ok := m.fetchGridSidebarFKPreview(&fk, int64(999), 1)().(GridSidebarFKPreviewLookupResultMsg)
		if !ok {
			t.Fatal("the command did not produce a lookup result")
		}
		if msg.Err == nil {
			t.Fatal("looking up a value with no referenced row returned no error")
		}
		if !strings.Contains(msg.Err.Error(), "referenced row not found") {
			t.Errorf("the error is %q, want it to say the referenced row was not found", msg.Err)
		}
		if len(msg.Row) != 0 {
			t.Errorf("a failed lookup carried a row: %v", msg.Row)
		}
	})

	t.Run("a STRING value against a TEXT column is quoted and escaped", func(t *testing.T) {
		// The WHERE is built with formatFKValue, so a value containing a quote has to be
		// doubled or the clause is malformed — and the user never sees the clause, so the
		// only symptom is a lookup that fails for every row.
		//
		// Against the INTEGER `id` a string value fails at the type level
		// ("invalid input syntax for type integer") before escaping matters at all, which
		// is what the first version of this test did and misread as a broken clause.
		textFK := postgres.ForeignKeyInfo{
			Column: "email", RefSchema: schema, RefTable: "users", RefColumn: "email",
		}
		m := routerModelLoaded(t)
		m.conn = conn
		m.prevSchema = schema

		// The row that exists.
		found, ok := m.fetchGridSidebarFKPreview(&textFK, "ada@example.com", 1)().(GridSidebarFKPreviewLookupResultMsg)
		if !ok {
			t.Fatal("the command did not produce a lookup result")
		}
		if found.Err != nil {
			t.Fatalf("looking up an existing text value failed: %v", found.Err)
		}
		if len(found.Row) == 0 {
			t.Error("no row came back for a value that exists")
		}

		// A value with a quote in it: doubled, so the clause parses and comes back empty
		// rather than as a syntax error.
		quoted, ok := m.fetchGridSidebarFKPreview(&textFK, "o'brien@example.com", 1)().(GridSidebarFKPreviewLookupResultMsg)
		if !ok {
			t.Fatal("the command did not produce a lookup result")
		}
		if quoted.Err == nil || !strings.Contains(quoted.Err.Error(), "referenced row not found") {
			t.Errorf("a value with a quote gave %v, want a clean 'not found' rather than a malformed clause", quoted.Err)
		}

		// And an ordinary value that is not there.
		missing, ok := m.fetchGridSidebarFKPreview(&textFK, "no@example.com", 1)().(GridSidebarFKPreviewLookupResultMsg)
		if !ok {
			t.Fatal("the command did not produce a lookup result")
		}
		if missing.Err == nil || !strings.Contains(missing.Err.Error(), "referenced row not found") {
			t.Errorf("a missing text value gave %v", missing.Err)
		}
	})
}

// ---------------------------------------------------------------------------
// connectToDB
// ---------------------------------------------------------------------------

func TestConnectToDBReachesTheDatabaseOrSaysWhyNot(t *testing.T) {
	dsn := testDSN(t)

	t.Run("a DSN that connects", func(t *testing.T) {
		m := routerModelLoaded(t)
		project := config.FoundProject{
			Name:       "fixture",
			Active:     true,
			Connection: config.ProjectConnection{DSN: dsn},
		}

		msg, ok := m.connectToDB(project)().(dbConnectedMsg)
		if !ok {
			t.Fatal("the command did not produce a dbConnectedMsg")
		}
		if msg.err != nil {
			t.Fatalf("connectToDB: %v", msg.err)
		}
		if msg.conn == nil {
			t.Fatal("the message carries no connection")
		}
		t.Cleanup(func() { _ = msg.conn.Close(context.Background()) })
		if msg.project == nil || msg.project.Name != "fixture" {
			t.Errorf("the message carries the project %+v; the schema load needs it", msg.project)
		}
	})

	t.Run("an EMPTY DSN is refused before any network attempt", func(t *testing.T) {
		// The guard exists so the error names the missing DSN instead of PostgreSQL's.
		// A user who wrote a .dbx.toml with no dsn line should be told that, not told
		// that the connection was refused.
		m := routerModelLoaded(t)
		msg, ok := m.connectToDB(config.FoundProject{Name: "p"})().(dbConnectedMsg)
		if !ok {
			t.Fatal("the command did not produce a dbConnectedMsg")
		}
		if msg.err == nil {
			t.Fatal("an empty DSN connected")
		}
		if !strings.Contains(msg.err.Error(), "DSN") {
			t.Errorf("the error is %q, want it to name the missing DSN", msg.err)
		}
		if msg.conn != nil {
			t.Error("a refused connection carried a connection")
		}
	})

	t.Run("a DSN that cannot connect says so and keeps the error", func(t *testing.T) {
		// Port 1 on localhost: nothing listens there. The handler puts this error on
		// screen, so it has to survive to the message.
		m := routerModelLoaded(t)
		project := config.FoundProject{
			Name:       "bad",
			Active:     true,
			Connection: config.ProjectConnection{DSN: "postgres://nobody@127.0.0.1:1/nothing?sslmode=disable"},
		}

		cmd := m.connectToDB(project)
		if cmd == nil {
			t.Fatal("no command was produced")
		}
		msg, ok := cmd().(dbConnectedMsg)
		if !ok {
			t.Fatal("the command did not produce a dbConnectedMsg")
		}
		if msg.err == nil {
			t.Fatal("connecting to a dead port returned no error")
		}
		if !strings.Contains(msg.err.Error(), "failed to connect") {
			t.Errorf("the error is %q, want it to say the connection failed", msg.err)
		}
	})
}

// ---------------------------------------------------------------------------

func keysOfStringMap[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
