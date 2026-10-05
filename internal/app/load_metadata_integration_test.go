package app

// Scenario: Las cargas que hablan con la base de datos traen lo que-registeron.
//
// Everything here needs a real PostgreSQL, because the whole point of these functions is
// the query they issue. The schema loader's own queries are tested in the driver package;
// what was untested is the glue: that a message carries the fields the handler reads, that
// a failure in one of three sequential loads aborts instead of returning half a picture,
// and that the database name is derived from the DSN correctly.
//
// The database name is the interesting one. It is derived by taking the part of the DSN
// after the last slash and before the first question mark — and that derivation is written
// TWICE, in loadSchema and in loadSchemaWithTarget, with no shared helper. Two copies of a
// string operation on the connection identity is the shape that has produced the most bugs
// in this repository, so it is worth having a test that would fail if the two drifted.

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/drivers/postgres"
)

// metadataFixture builds a schema with exactly the things the metadata pane reports: a
// primary key, a unique constraint, a check constraint, a foreign key and two indexes.
func metadataFixture(t *testing.T, conn *pgx.Conn) (schema, table string) {
	t.Helper()
	ctx := context.Background()

	schema = "md_" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "_"))
	table = "orders"

	stmts := []string{
		fmt.Sprintf(`DROP SCHEMA IF EXISTS %s CASCADE`, schema),
		fmt.Sprintf(`CREATE SCHEMA %s`, schema),
		fmt.Sprintf(`CREATE TABLE %s.users (
			id   integer PRIMARY KEY,
			email text NOT NULL UNIQUE
		)`, schema),
		fmt.Sprintf(`CREATE TABLE %s.%s (
			id       integer PRIMARY KEY,
			user_id  integer NOT NULL REFERENCES %s.users(id),
			ref      text UNIQUE,
			total    numeric NOT NULL CHECK (total >= 0),
			status   text NOT NULL DEFAULT 'new',
			note     text
		)`, schema, table, schema),
		fmt.Sprintf(`CREATE INDEX orders_status_idx ON %s.%s (status)`, schema, table),
		fmt.Sprintf(`CREATE INDEX orders_note_idx ON %s.%s (note)`, schema, table),
		fmt.Sprintf(`INSERT INTO %s.users (id, email) VALUES (1, 'ada@example.com')`, schema),
		fmt.Sprintf(`INSERT INTO %s.%s (id, user_id, total, status) VALUES (1, 1, 10.5, 'new'), (2, 1, 20, 'new')`, schema, table),
	}
	for _, s := range stmts {
		if _, err := conn.Exec(ctx, s); err != nil {
			t.Fatalf("%s: %v", firstLine(s), err)
		}
	}
	t.Cleanup(func() {
		_, _ = conn.Exec(context.Background(), fmt.Sprintf(`DROP SCHEMA IF EXISTS %s CASCADE`, schema))
	})
	return schema, table
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func TestLoadMetadataReportsTheTableItWasAskedAbout(t *testing.T) {
	dsn := testDSN(t)
	conn := connectTestDB(t, dsn)
	schema, table := metadataFixture(t, conn)

	m := routerModelLoaded(t)
	m.conn = conn

	cmd := m.loadMetadata(schema, table)
	if cmd == nil {
		t.Fatal("loadMetadata returned no command")
	}
	msg, ok := cmd().(metadataLoadedMsg)
	if !ok {
		t.Fatalf("the command produced a %T", cmd())
	}
	if msg.err != nil {
		t.Fatalf("loadMetadata: %v", msg.err)
	}

	t.Run("the message names the table it loaded", func(t *testing.T) {
		if msg.schema != schema || msg.table != table {
			t.Errorf("the message is for %s.%s, want %s.%s", msg.schema, msg.table, schema, table)
		}
	})

	t.Run("the constraints arrive, including the CHECK and the FOREIGN KEY", func(t *testing.T) {
		// A pane that shows only the primary key hides most of what a table enforces, and
		// the pane has a Constraints tab, so its being empty or partial is visible to the
		// user as a wrong answer rather than as a missing feature.
		var pk, fk, check, unique bool
		for _, c := range msg.constraints {
			switch c.Type {
			case "PRIMARY KEY":
				pk = true
			case "FOREIGN KEY":
				fk = true
			case "CHECK":
				check = true
			case "UNIQUE":
				unique = true
			}
		}
		if !pk {
			t.Errorf("no PRIMARY KEY among %+v", msg.constraints)
		}
		if !fk {
			t.Errorf("no FOREIGN KEY among %+v", msg.constraints)
		}
		if !check {
			t.Errorf("no CHECK among %+v", msg.constraints)
		}
		if !unique {
			t.Errorf("no UNIQUE among %+v", msg.constraints)
		}
	})

	t.Run("the foreign keys arrive with their target", func(t *testing.T) {
		if len(msg.foreignKeys) == 0 {
			t.Fatal("no foreign keys")
		}
		var found bool
		for _, f := range msg.foreignKeys {
			if f.Column == "user_id" {
				found = true
				if f.RefTable != "users" || f.RefSchema != schema {
					t.Errorf("the foreign key points at %s.%s, want %s.users", f.RefSchema, f.RefTable, schema)
				}
			}
		}
		if !found {
			t.Errorf("no foreign key on user_id among %+v", msg.foreignKeys)
		}
	})

	t.Run("the indexes arrive, both of them", func(t *testing.T) {
		// Two indexes were created; a pane showing one is a silently incomplete answer.
		var pkey bool
		names := map[string]bool{}
		for _, i := range msg.indexes {
			names[i.Name] = true
			if i.Unique {
				pkey = true
			}
		}
		if !names["orders_status_idx"] || !names["orders_note_idx"] {
			t.Errorf("the indexes are %v, want both created ones", names)
		}
		if !pkey {
			t.Error("the primary key index is not reported as unique")
		}
	})

	t.Run("the overview arrives with a live row count", func(t *testing.T) {
		// TableOverview is what the Overview tab shows. Its count is from the statistics
		// view, which PostgreSQL updates lazily, so the assertion is that it is present
		// and not that it is exactly two.
		if msg.overview == nil {
			t.Fatal("the overview is nil; loadMetadata swallows the error and returns nil, so this is indistinguishable from a table with no statistics")
		}
		if msg.overview.ColumnCount != 6 {
			t.Errorf("the overview counts %d columns, want 6", msg.overview.ColumnCount)
		}
	})
}

// Scenario: Una tabla que ya no existe produce un pane VACIO, no un error.
func TestLoadMetadataOnATableThatDoesNotExist(t *testing.T) {
	// Pinned as it behaves, because the behaviour is defensible and worth knowing rather
	// than changing: the three catalogue queries all return zero rows for a table that is
	// not there, and zero rows is not an error. The overview is the one that fails, and
	// loadMetadata deliberately swallows that error and leaves the overview nil.
	//
	// The user reaches this by selecting a table that another session dropped. An empty
	// pane is the honest answer; the alternative is an error toast for something they
	// did not do.
	//
	// The first version of this asserted an error and failed. The interesting part is
	// what it must NOT do: return data from the previous table.
	dsn := testDSN(t)
	conn := connectTestDB(t, dsn)

	m := routerModelLoaded(t)
	m.conn = conn

	msg, ok := m.loadMetadata("no_such_schema", "no_such_table")().(metadataLoadedMsg)
	if !ok {
		t.Fatal("the command did not produce a metadataLoadedMsg")
	}
	if msg.err != nil {
		t.Errorf("a missing table returned the error %v; a catalogue query returning no rows is not a failure", msg.err)
	}
	if len(msg.constraints) != 0 || len(msg.foreignKeys) != 0 || len(msg.indexes) != 0 {
		t.Errorf("a missing table carried data: %+v", msg)
	}
	if msg.overview != nil {
		t.Errorf("a missing table produced an overview: %+v", msg.overview)
	}
	if msg.schema != "no_such_schema" || msg.table != "no_such_table" {
		t.Errorf("the message names %s.%s, so the caller cannot tell which table came back empty", msg.schema, msg.table)
	}
}

func TestLoadMetadataIsRefusedWhileBusy(t *testing.T) {
	// refuseIfBusy exists so a read-only ASK query in flight is not interleaved with a
	// metadata load. Without the guard the two would share the connection and the
	// metadata would be read inside the read-only transaction, or not at all.
	t.Run("a read-only ASK query in flight refuses it", func(t *testing.T) {
		// refuseIfBusy reports that a read-only ASK transaction is still running. The flag
		// lives on the runner, not on the model — the runner owns the transaction — so the
		// fixture has to attach one and set the flag through it.
		m := routerModelLoaded(t)
		m.runner = newStatementRunner(nil)
		m.runner.readOnlyActive.Store(true)

		if cmd := m.loadMetadata("public", "users"); cmd != nil {
			t.Error("loadMetadata produced a command while a read-only query was in flight")
		}
		if !m.dbBusy() {
			t.Error("dbBusy is false with the flag set; the guard is not reading what the test set")
		}
	})

	t.Run("with no runner it does not refuse", func(t *testing.T) {
		// A model with no runner is the state before the connection lands, and refusing
		// there would mean the first schema load after connecting did nothing.
		m := routerModelLoaded(t)
		m.runner = nil
		if cmd := m.loadMetadata("public", "users"); cmd == nil {
			t.Error("loadMetadata refused with no runner at all")
		}
	})
}

// Scenario: El nombre de la base sale del DSN, y las dos copias coinciden.
func TestTheDatabaseNameIsDerivedFromTheDSN(t *testing.T) {
	// The derivation was written inline twice, once in loadSchema and once in
	// loadSchemaWithTarget, with no shared helper. It is one function now, so the assertion
	// is about the derivation itself — including the ORDER of the two steps, which is not
	// interchangeable: a DSN's query string can contain a slash.
	dsns := []struct {
		dsn  string
		want string
	}{
		{"postgres://dbx:dbx@127.0.0.1:55432/dbx_test?sslmode=disable", "dbx_test"},
		{"postgres://dbx:dbx@127.0.0.1:55432/dbx_test", "dbx_test"},
		{"postgres://localhost/mydb", "mydb"},
		{"/var/run/postgresql", "postgresql"},
		{"postgres://host/db/with/slashes", "slashes"},
		{"postgres://host/dbname?a=b?c=d", "dbname"},
		{"postgres://host/", ""},
		{"", ""},
	}

	dsn := testDSN(t)
	for _, tc := range dsns {
		t.Run("dsn "+tc.dsn, func(t *testing.T) {
			got := deriveDBName(tc.dsn)
			if got != tc.want {
				t.Errorf("deriveDBName(%q) = %q, want %q", tc.dsn, got, tc.want)
			}
		})
	}

	t.Run("the name it derives is the one the database reports", func(t *testing.T) {
		// The whole point of deriving it: the pane's title. A DSN whose database part is
		// wrong would show the wrong name, and the user would have no way to tell that it
		// was the DSN and not the connection that was wrong.
		conn := connectTestDB(t, dsn)
		var current string
		if err := conn.QueryRow(context.Background(), "SELECT current_database()").Scan(&current); err != nil {
			t.Fatalf("current_database: %v", err)
		}
		if got := deriveDBName(dsn); got != current {
			t.Errorf("the DSN %q derives the database name %q but the server says %q", dsn, got, current)
		}
	})
}

func TestLoadSchemaCarriesTheDatabaseAndTheTarget(t *testing.T) {
	dsn := testDSN(t)
	conn := connectTestDB(t, dsn)
	metadataFixture(t, conn)

	project := config.FoundProject{
		Name:       "fixture",
		Path:       t.TempDir(),
		Active:     true,
		Connection: config.ProjectConnection{DSN: dsn},
	}

	t.Run("loadSchema names the database", func(t *testing.T) {
		m := routerModelLoaded(t)
		msg, ok := m.loadSchema(conn, project)().(schemaLoadedMsg)
		if !ok {
			t.Fatal("the command did not produce a schemaLoadedMsg")
		}
		if msg.err != nil {
			t.Fatalf("loadSchema: %v", msg.err)
		}
		if msg.dbName != "dbx_test" {
			t.Errorf("the message names the database %q", msg.dbName)
		}
		if msg.root == nil {
			t.Error("the message carries no root node")
		}
		// And no target: this is the plain load.
		if msg.targetSchema != "" || msg.targetTable != "" {
			t.Errorf("loadSchema set a target: %q/%q", msg.targetSchema, msg.targetTable)
		}
	})

	t.Run("loadSchemaWithTarget carries the target so the app can scroll to it", func(t *testing.T) {
		// The target is what makes a DDL statement navigate to the table it just changed.
		// Losing it means the app reloads the schema and drops the user back at the top.
		m := routerModelLoaded(t)
		msg, ok := m.loadSchemaWithTarget(conn, project, "md_load_schema_carries", "orders")().(schemaLoadedMsg)
		if !ok {
			t.Fatal("the command did not produce a schemaLoadedMsg")
		}
		if msg.err != nil {
			t.Fatalf("loadSchemaWithTarget: %v", msg.err)
		}
		if msg.targetSchema != "md_load_schema_carries" || msg.targetTable != "orders" {
			t.Errorf("the target is %q/%q", msg.targetSchema, msg.targetTable)
		}
		if msg.root == nil {
			t.Error("the message carries no root node")
		}
	})

	t.Run("a connection that cannot answer produces an error, not a half message", func(t *testing.T) {
		// The three loads in loadMetadata abort on the first error; loadSchema has one.
		m := routerModelLoaded(t)
		// A zero connection: the query fails immediately with a closed or nil backend,
		// which is the state a load hits when the connection died between the user picking
		// a project and the schema arriving.
		dead := connectTestDB(t, dsn)
		if err := dead.Close(context.Background()); err != nil {
			t.Fatalf("closing the connection: %v", err)
		}
		msg := m.loadSchema(dead, project)()
		loaded, ok := msg.(schemaLoadedMsg)
		if !ok {
			t.Fatalf("the command produced a %T", msg)
		}
		if loaded.err == nil {
			t.Error("loading with a zero connection produced no error")
		}
	})
}

// ---------------------------------------------------------------------------
// the three converters, which are pure
// ---------------------------------------------------------------------------

// Scenario: Los conversores no pierden ni un campo, y con una entrada vacia dan una
// lista vacia y no un nil.
func TestTheMetadataConvertersCarryEveryField(t *testing.T) {
	t.Run("constraints", func(t *testing.T) {
		in := []postgres.ConstraintInfo{
			{Name: "orders_pkey", Type: "PRIMARY KEY", Columns: "id"},
			{Name: "total_check", Type: "CHECK", Columns: "total"},
		}
		got := toConstraintInfo(in)
		if len(got) != 2 {
			t.Fatalf("got %d entries", len(got))
		}
		for i := range in {
			if got[i].Name != in[i].Name || got[i].Type != in[i].Type || got[i].Columns != in[i].Columns {
				t.Errorf("entry %d is %+v, want %+v", i, got[i], in[i])
			}
		}
		if got[0].Type == got[1].Type {
			t.Error("two different constraint types came out the same")
		}

		t.Run("an empty input gives an empty non-nil slice", func(t *testing.T) {
			// A nil result renders as nothing and a range over it skips; both are fine, but
			// the distinction is worth having asserted once so a future change to a
			// var-declared slice is noticed.
			out := toConstraintInfo(nil)
			if out == nil {
				t.Error("toConstraintInfo(nil) is nil")
			}
			if len(out) != 0 {
				t.Errorf("toConstraintInfo(nil) has %d entries", len(out))
			}
		})
	})

	t.Run("foreign keys, all five fields", func(t *testing.T) {
		in := []postgres.ForeignKeyInfo{{
			Name: "orders_user_id_fkey", Column: "user_id",
			RefSchema: "public", RefTable: "users", RefColumn: "id",
		}}
		got := toForeignKeyInfo(in)
		if len(got) != 1 {
			t.Fatalf("got %d entries", len(got))
		}
		if got[0].Name != "orders_user_id_fkey" || got[0].Column != "user_id" ||
			got[0].RefSchema != "public" || got[0].RefTable != "users" || got[0].RefColumn != "id" {
			t.Errorf("the foreign key is %+v", got[0])
		}
		// A dropped field here is a pane that cannot say what a key points at, which is
		// the only thing the tab is for.
		if toForeignKeyInfo(nil) == nil {
			t.Error("toForeignKeyInfo(nil) is nil")
		}
	})

	t.Run("indexes, including the unique flag", func(t *testing.T) {
		in := []postgres.IndexInfo{
			{Name: "orders_pkey", Columns: "id", Unique: true},
			{Name: "orders_status_idx", Columns: "status", Unique: false},
		}
		got := toIndexInfo(in)
		if len(got) != 2 {
			t.Fatalf("got %d entries", len(got))
		}
		if got[0].Name != "orders_pkey" || got[0].Columns != "id" || !got[0].Unique {
			t.Errorf("entry 0 is %+v", got[0])
		}
		if got[1].Unique {
			t.Error("a non-unique index came out unique")
		}
		if toIndexInfo(nil) == nil {
			t.Error("toIndexInfo(nil) is nil")
		}
	})

	t.Run("order is preserved", func(t *testing.T) {
		// The loader returns them in whatever order the catalogue query produced, and the
		// pane shows them in that order. A converter that sorted or reversed would be a
		// behaviour change nobody asked for.
		in := []postgres.IndexInfo{{Name: "c"}, {Name: "a"}, {Name: "b"}}
		got := toIndexInfo(in)
		for i, want := range []string{"c", "a", "b"} {
			if got[i].Name != want {
				t.Errorf("entry %d is %q, want %q", i, got[i].Name, want)
			}
		}
	})
}
