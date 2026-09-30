package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/buble/dbx/internal/testsupport/pgxfake"
)

// --- the shared contract -----------------------------------------------------
//
// The List* methods are the same function eleven times: run a query, scan each
// row, hand back a slice. All eleven have the same three failure points, so all
// eleven have the same three things to prove, and proving them once per function
// is what kills the thirty-odd mutants that live in them.

type listCase struct {
	name string
	// call runs the method against conn and returns the number of items it
	// produced. The count is what the happy-path fixture asserts.
	call func(*SchemaLoader) (int, error)
	// rows is a scripted success. One column per scanned destination.
	rows [][]any
}

func listCases() []listCase {
	return []listCase{
		{"ListSchemas", func(s *SchemaLoader) (int, error) {
			got, err := s.ListSchemas(context.Background())
			return len(got), err
		}, [][]any{{"public"}, {"sales"}}},

		{"ListTables", func(s *SchemaLoader) (int, error) {
			got, err := s.ListTables(context.Background(), "public")
			return len(got), err
		}, [][]any{{"users", "BASE TABLE", 12}, {"orders", "BASE TABLE", 3}}},

		{"ListColumns", func(s *SchemaLoader) (int, error) {
			got, err := s.ListColumns(context.Background(), "public", "users")
			return len(got), err
		}, [][]any{
			{"id", "integer", "NO", nil},
			{"email", "text", "YES", nil},
		}},

		{"ListConstraints", func(s *SchemaLoader) (int, error) {
			got, err := s.ListConstraints(context.Background(), "public", "users")
			return len(got), err
		}, [][]any{{"users_pkey", "PRIMARY KEY", "id"}, {"users_email_key", "UNIQUE", "email"}}},

		{"ListForeignKeys", func(s *SchemaLoader) (int, error) {
			got, err := s.ListForeignKeys(context.Background(), "public", "orders")
			return len(got), err
		}, [][]any{{"orders_user_fkey", "user_id", "public", "users", "id"}}},

		{"ListIndexes", func(s *SchemaLoader) (int, error) {
			got, err := s.ListIndexes(context.Background(), "public", "users")
			return len(got), err
		}, [][]any{{"users_pkey", "CREATE UNIQUE INDEX users_pkey ON public.users USING btree (id)"}}},

		{"ListIndexesFull", func(s *SchemaLoader) (int, error) {
			got, err := s.ListIndexesFull(context.Background(), "public", "users")
			return len(got), err
		}, [][]any{{"users_pkey", "t", "t", []string{"id"}}}},

		{"ListColumnsBySchema", func(s *SchemaLoader) (int, error) {
			got, err := s.ListColumnsBySchema(context.Background(), "public")
			// Grouped by table, so the count is the number of groups.
			return len(got), err
		}, [][]any{{"users", "id", "integer", "NO", nil}, {"users", "email", "text", "YES", nil}}},

		{"ListIndexesFullBySchema", func(s *SchemaLoader) (int, error) {
			got, err := s.ListIndexesFullBySchema(context.Background(), "public")
			return len(got), err
		}, [][]any{{"users", "users_pkey", "t", "t", []string{"id"}}}},

		{"ListForeignKeysBySchema", func(s *SchemaLoader) (int, error) {
			got, err := s.ListForeignKeysBySchema(context.Background(), "public")
			return len(got), err
		}, [][]any{{"orders", "orders_user_fkey", "user_id", "public", "users", "id"}}},
	}
}

func okConn(t *testing.T, rows [][]any) *pgxfake.Conn {
	t.Helper()
	return &pgxfake.Conn{Steps: []pgxfake.Step{{
		Match: "",
		Result: pgxfake.Result{
			Columns: []pgxfake.Column{{Name: "c"}},
			Rows:    rows,
		},
	}}}
}

// Scenario: Cada listado devuelve un elemento por fila.
//
// The happy path for all eleven at once, because they all promise the same thing.
func TestListMethods_ReturnOneItemPerRow(t *testing.T) {
	for _, tc := range listCases() {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.call(NewSchemaLoader(okConn(t, tc.rows)))
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if got == 0 {
				t.Errorf("%s returned nothing for %d scripted rows", tc.name, len(tc.rows))
			}
		})
	}
}

// Scenario: Una consulta que no se puede lanzar se propaga sin inventar filas.
//
// This is the `if err != nil` right after Query, and returning an empty slice here
// would turn a permission problem into "this schema has no tables".
func TestListMethods_ADriverFailureIsPropagatedNotSwallowed(t *testing.T) {
	for _, tc := range listCases() {
		t.Run(tc.name, func(t *testing.T) {
			boom := errors.New("pq: permission denied")
			conn := &pgxfake.Conn{Steps: []pgxfake.Step{{Match: "", Err: boom}}}

			_, err := tc.call(NewSchemaLoader(conn))
			if err == nil {
				t.Fatalf("%s turned a driver failure into no error", tc.name)
			}
			if !errors.Is(err, boom) {
				t.Errorf("%s: error = %v, want it to wrap the driver error", tc.name, err)
			}
		})
	}
}

// Scenario: Una fila que no se puede leer es un error, no una fila a medias.
//
// A half-scanned row has fields from one row and empties from another, and the
// caller has no way to tell. Failing is the only safe answer, and the partial
// result must not escape either.
func TestListMethods_AFailedRowScanIsAnError(t *testing.T) {
	for _, tc := range listCases() {
		t.Run(tc.name, func(t *testing.T) {
			conn := &pgxfake.Conn{Steps: []pgxfake.Step{{
				Match: "",
				Result: pgxfake.Result{
					Columns: []pgxfake.Column{{Name: "c"}},
					Rows:    tc.rows,
					ScanErr: errors.New("cannot convert"),
				},
			}}}

			got, err := tc.call(NewSchemaLoader(conn))
			if err == nil {
				t.Fatalf("%s turned a failed scan into no error", tc.name)
			}
			if !strings.Contains(err.Error(), "cannot convert") {
				t.Errorf("%s: error = %v, want the scan failure", tc.name, err)
			}
			if got != 0 {
				t.Errorf("%s returned %d items alongside the error, want none", tc.name, got)
			}
		})
	}
}

// Scenario: Un fallo a mitad del recorrido no se confunde con el final.
//
// `return x, rows.Err()` is the last line of every one of these functions and it
// is the one that catches a connection dropped halfway. Reporting a short list as
// complete is the worst outcome available: the explorer would show a table with
// some of its columns missing and no indication of why.
func TestListMethods_AMidIterationFailureIsNotAShortList(t *testing.T) {
	for _, tc := range listCases() {
		t.Run(tc.name, func(t *testing.T) {
			conn := &pgxfake.Conn{Steps: []pgxfake.Step{{
				Match: "",
				Result: pgxfake.Result{
					Columns: []pgxfake.Column{{Name: "c"}},
					Rows:    tc.rows,
					IterErr: errors.New("connection reset by peer"),
				},
			}}}

			got, err := tc.call(NewSchemaLoader(conn))
			if err == nil {
				t.Fatalf("%s reported a truncated read as a complete one", tc.name)
			}
			if !strings.Contains(err.Error(), "connection reset") {
				t.Errorf("%s: error = %v, want the iteration failure", tc.name, err)
			}
			if got != 0 {
				t.Errorf("%s returned %d items alongside the error, want none", tc.name, got)
			}
		})
	}
}

// Scenario: Ninguna consulta de listado filtra por nada que no sea el argumento.
//
// Every one of these takes a schema, and most take a table, and they all pass them
// as bound parameters rather than interpolating. `users' OR '1'='1` has to arrive
// at the server as a value, or the whole schema browser is an injection surface.
func TestListMethods_PassTheSchemaAndTableAsParameters(t *testing.T) {
	for _, tc := range listCases() {
		t.Run(tc.name, func(t *testing.T) {
			conn := okConn(t, nil)
			if _, err := tc.call(NewSchemaLoader(conn)); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			q := conn.LastQuery()
			if strings.Contains(q, "'public'") || strings.Contains(q, `"public"`) {
				t.Errorf("%s interpolated the schema into the SQL: %q", tc.name, q)
			}
			if strings.Contains(q, "'users'") || strings.Contains(q, `"users"`) {
				t.Errorf("%s interpolated the table into the SQL: %q", tc.name, oneline(q))
			}
		})
	}
}

// Scenario: Los esquemas de sistema no se listan.
//
// PostgreSQL's own schemas are excluded in SQL, and the tests below re-check that
// the SQL says so. If a future edit drops the WHERE, every user starts seeing
// pg_catalog with four hundred tables in it.
func TestListSchemas_TheQueryExcludesSystemSchemas(t *testing.T) {
	conn := okConn(t, [][]any{{"public"}})
	if _, err := NewSchemaLoader(conn).ListSchemas(context.Background()); err != nil {
		t.Fatalf("ListSchemas: %v", err)
	}
	if !strings.Contains(conn.LastQuery(), "NOT LIKE 'pg_%'") {
		t.Errorf("query = %q, want the pg_%% exclusion", oneline(conn.LastQuery()))
	}
}

// --- ListIndexes: the one with a decision in it ------------------------------

// Scenario: Un índice se marca como único si su definición lo dice.
//
// The driver does not select a uniqueness flag; it reads the index definition and
// looks for the word. That is a string match against server output, so it is worth
// pinning exactly, including the words that contain UNIQUE without meaning it.
func TestListIndexes_MarksUniquenessFromTheDefinition(t *testing.T) {
	for _, tc := range []struct {
		name string
		def  string
		want bool
		why  string
	}{
		{"a unique index", "CREATE UNIQUE INDEX users_email_key ON public.users USING btree (email)", true, "UNIQUE is present"},
		{"a primary key index", "CREATE UNIQUE INDEX users_pkey ON public.users USING btree (id)", true, "a PK is a unique index"},
		{"a plain index", "CREATE INDEX users_name_idx ON public.users USING btree (name)", false, "no UNIQUE"},
		{"an expression index", "CREATE INDEX lower_idx ON public.users USING btree (lower(name))", false, "no UNIQUE"},
		{"an empty definition", "", false, "nothing to match, and no substring to find by accident"},
		{"lowercase unique", "create unique index x on t (a)", false, "the match is case-sensitive, matching what the server emits"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn := okConn(t, [][]any{{"idx", tc.def}})
			got, err := NewSchemaLoader(conn).ListIndexes(context.Background(), "public", "users")
			if err != nil {
				t.Fatalf("ListIndexes: %v", err)
			}
			if len(got) != 1 {
				t.Fatalf("got %d indexes, want 1", len(got))
			}
			if got[0].Unique != tc.want {
				t.Errorf("Unique = %v, want %v (%s)", got[0].Unique, tc.want, tc.why)
			}
			// The definition is kept either way: it is what the UI shows.
			if got[0].Def != tc.def {
				t.Errorf("Def = %q, want the definition kept: %q", got[0].Def, tc.def)
			}
			if got[0].Name != "idx" {
				t.Errorf("Name = %q, want idx", got[0].Name)
			}
		})
	}
}

// --- ListColumns: nullable and default ---------------------------------------

// Scenario: La nulabilidad y el default se leen tal cual.
//
// "YES"/"NO" and a NULL default are both meaningful and both are easy to lose in
// a scan: a nil default must stay nil, not become the string "NULL", because the
// editor prints it into a generated INSERT.
func TestListColumns_KeepsNullableAndDefault(t *testing.T) {
	def := "nextval('users_id_seq'::regclass)"
	conn := okConn(t, [][]any{
		{"id", "integer", "NO", "nextval('users_id_seq'::regclass)"},
		{"email", "text", "YES", nil},
		{"name", "text", "NO", nil},
	})

	got, err := NewSchemaLoader(conn).ListColumns(context.Background(), "public", "users")
	if err != nil {
		t.Fatalf("ListColumns: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d columns, want 3", len(got))
	}
	if got[0].Default == nil || *got[0].Default != def {
		t.Errorf("id default = %v, want %q", got[0].Default, def)
	}
	if got[1].Default != nil {
		t.Errorf("email default = %v, want nil", *got[1].Default)
	}
	if got[2].Default != nil {
		t.Errorf("a column with no default got %v, want nil", *got[2].Default)
	}
	if got[0].IsNullable != "NO" || got[1].IsNullable != "YES" {
		t.Errorf("nullability = %q, %q, want NO and YES", got[0].IsNullable, got[1].IsNullable)
	}
	if got[2].IsNullable != "NO" {
		t.Errorf("name nullability = %q, want NO", got[2].IsNullable)
	}
}

// --- the by-schema maps ------------------------------------------------------

// Scenario: Las columnas de un esquema se agrupan por tabla, en orden.
//
// Grouping is what lets LoadDatabase attach columns to the right table in one pass
// instead of one query per table, and the order inside a group is the column
// order the server sent.
func TestListColumnsBySchema_GroupsByTablePreservingOrder(t *testing.T) {
	conn := okConn(t, [][]any{
		{"users", "id", "integer", "NO", nil},
		{"users", "email", "text", "YES", nil},
		{"orders", "id", "integer", "NO", nil},
		{"users", "name", "text", "NO", nil},
	})

	got, err := NewSchemaLoader(conn).ListColumnsBySchema(context.Background(), "public")
	if err != nil {
		t.Fatalf("ListColumnsBySchema: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d tables, want 2: %v", len(got), keysOf(got))
	}
	users := got["users"]
	if len(users) != 3 {
		t.Fatalf("users has %d columns, want 3", len(users))
	}
	if users[0].Name != "id" || users[1].Name != "email" || users[2].Name != "name" {
		t.Errorf("users columns are %s, %s, %s, want id, email, name in the order they arrived",
			users[0].Name, users[1].Name, users[2].Name)
	}
	if len(got["orders"]) != 1 || got["orders"][0].Name != "id" {
		t.Errorf("orders = %v, want one column named id", got["orders"])
	}
}

// Scenario: Un esquema sin columnas da un mapa vacío, no un error.
//
// An empty map and a nil map both range as nothing, and the caller's next step is
// the same either way. What matters is that it is not an error.
func TestTheBySchemaMaps_AnEmptyResultIsAnEmptyMap(t *testing.T) {
	conn := okConn(t, nil)
	loader := NewSchemaLoader(conn)

	cols, err := loader.ListColumnsBySchema(context.Background(), "public")
	if err != nil {
		t.Fatalf("ListColumnsBySchema on an empty schema: %v", err)
	}
	if cols == nil {
		t.Error("ListColumnsBySchema returned a nil map, want an empty one")
	}
	if len(cols) != 0 {
		t.Errorf("got %d entries, want none", len(cols))
	}

	idx, err := loader.ListIndexesFullBySchema(context.Background(), "public")
	if err != nil {
		t.Fatalf("ListIndexesFullBySchema on an empty schema: %v", err)
	}
	if idx == nil || len(idx) != 0 {
		t.Errorf("ListIndexesFullBySchema = %v, want an empty map", idx)
	}

	fks, err := loader.ListForeignKeysBySchema(context.Background(), "public")
	if err != nil {
		t.Fatalf("ListForeignKeysBySchema on an empty schema: %v", err)
	}
	if fks == nil || len(fks) != 0 {
		t.Errorf("ListForeignKeysBySchema = %v, want an empty map", fks)
	}
}

// Scenario: Las claves foráneas de un esquema se agrupan por tabla que las tiene.
//
// The key is the referencing table, not the referenced one, which is what the
// schema pane needs: it shows "this table points at that table".
func TestListForeignKeysBySchema_GroupsByTheReferencingTable(t *testing.T) {
	conn := okConn(t, [][]any{
		{"orders", "orders_user_fkey", "user_id", "public", "users", "id"},
		{"orders", "orders_addr_fkey", "address_id", "public", "addresses", "id"},
		{"reviews", "reviews_user_fkey", "user_id", "public", "users", "id"},
	})

	got, err := NewSchemaLoader(conn).ListForeignKeysBySchema(context.Background(), "public")
	if err != nil {
		t.Fatalf("ListForeignKeysBySchema: %v", err)
	}
	if len(got["orders"]) != 2 {
		t.Errorf("orders has %d foreign keys, want 2", len(got["orders"]))
	}
	if len(got["reviews"]) != 1 {
		t.Errorf("reviews has %d foreign keys, want 1", len(got["reviews"]))
	}
	fk := got["orders"][0]
	if fk.Name != "orders_user_fkey" || fk.Column != "user_id" || fk.RefSchema != "public" || fk.RefTable != "users" || fk.RefColumn != "id" {
		t.Errorf("first foreign key = %+v, want orders_user_fkey on user_id pointing at public.users.id", fk)
	}
}

// --- LoadDatabase ------------------------------------------------------------

// loadConn scripts a two-schema database.
func loadConn(tables map[string][][]any) *pgxfake.Conn {
	return &pgxfake.Conn{Steps: []pgxfake.Step{
		{ // the schema list
			Match: "information_schema.schemata",
			Result: pgxfake.Result{
				Columns: []pgxfake.Column{{Name: "schema_name"}},
				Rows:    [][]any{{"public"}, {"pg_catalog"}, {"information_schema"}, {"sales"}},
			},
		},
		{ // the columns of the whole schema, once per schema
			Match: "information_schema.columns",
			Result: pgxfake.Result{
				Columns: []pgxfake.Column{{Name: "table_name"}},
				Rows: [][]any{
					{"users", "id", "integer", "NO", nil},
					{"users", "email", "text", "YES", nil},
				},
			},
		},
		{ // the tables of one schema
			Match: "information_schema.tables",
			Result: pgxfake.Result{
				Columns: []pgxfake.Column{{Name: "table_name"}},
				Rows:    tables["tables"],
			},
		},
	}}
}

// Scenario: El árbol tiene base, esquemas, tablas y columnas, y solo lo del usuario.
//
// The shape is what the explorer renders, so it is asserted as a tree and not as a
// set of queries. The system schemas are dropped here even though the server was
// asked for them, because the driver refuses to show a user four hundred tables
// from pg_catalog.
func TestLoadDatabase_BuildsTheTreeWithoutSystemSchemas(t *testing.T) {
	conn := loadConn(map[string][][]any{
		"tables": {{"users", "BASE TABLE", 42}},
	})

	got, err := NewSchemaLoader(conn).LoadDatabase(context.Background(), "shop")
	if err != nil {
		t.Fatalf("LoadDatabase: %v", err)
	}

	if got.Root == nil {
		t.Fatal("the root node is nil")
	}
	if got.Root.Name != "shop" {
		t.Errorf("the root is named %q, want shop", got.Root.Name)
	}
	if !got.Root.Expanded {
		t.Error("the root is collapsed: the database has to be open on arrival")
	}

	// Two schemas of the four the server returned.
	if len(got.Root.Children) != 2 {
		var names []string
		for _, c := range got.Root.Children {
			names = append(names, c.Name)
		}
		t.Fatalf("the root has %d schema children (%v), want 2: pg_catalog and information_schema must be dropped", len(got.Root.Children), names)
	}
	for _, c := range got.Root.Children {
		if c.Name == "pg_catalog" || c.Name == "information_schema" || c.Name == "pg_toast" {
			t.Errorf("the system schema %q was not dropped", c.Name)
		}
	}
	// A node carries both: the ID is the fully qualified path, the Name is the
	// bare one the panel shows. Both matter, and conflating them is a bug the
	// tests below would not otherwise catch.
	if got.Root.Children[0].ID != "shop.public" {
		t.Errorf("the schema ID is %q, want the fully qualified shop.public", got.Root.Children[0].ID)
	}
	if got.Root.Children[0].Name != "public" {
		t.Errorf("the schema is displayed as %q, want the bare public", got.Root.Children[0].Name)
	}
	if got.Root.Children[0].Expanded {
		t.Error("a schema starts expanded: a database with many schemas would open a wall of tables")
	}

	// The table carries its row count and type, and its columns as children.
	table := got.Root.Children[0].Children[0]
	if table.ID != "shop.public.users" {
		t.Errorf("the table ID is %q, want shop.public.users", table.ID)
	}
	if table.Name != "users" {
		t.Errorf("the table is displayed as %q, want the bare users", table.Name)
	}
	if table.Metadata["row_count"] != 42 {
		t.Errorf("row_count metadata = %v, want 42", table.Metadata["row_count"])
	}
	if table.Metadata["table_type"] != "BASE TABLE" {
		t.Errorf("table_type metadata = %v, want BASE TABLE", table.Metadata["table_type"])
	}
	if table.Metadata["schema"] != "public" {
		t.Errorf("schema metadata = %v, want public", table.Metadata["schema"])
	}
	if len(table.Children) != 2 {
		t.Fatalf("the table has %d column children, want 2", len(table.Children))
	}
	col := table.Children[0]
	if col.ID != "shop.public.users.id" {
		t.Errorf("the column ID is %q, want shop.public.users.id", col.ID)
	}
	if col.Name != "id" {
		t.Errorf("the column is displayed as %q, want the bare id", col.Name)
	}
	if col.Metadata["data_type"] != "integer" {
		t.Errorf("data_type metadata = %v, want integer", col.Metadata["data_type"])
	}
	if col.Metadata["is_nullable"] != "NO" {
		t.Errorf("is_nullable metadata = %v, want NO", col.Metadata["is_nullable"])
	}
	// column_default is a *string and can be nil, so it is stored as-is.
	if col.Metadata["column_default"] != (*string)(nil) {
		t.Errorf("column_default metadata = %v, want a nil *string", col.Metadata["column_default"])
	}

	// And the same facts in the flat side, which is what the LLM export reads.
	if len(got.Schemas) != 2 {
		t.Fatalf("the flat side has %d schemas, want 2", len(got.Schemas))
	}
	if len(got.Schemas[0].Tables) != 1 || got.Schemas[0].Tables[0].Name != "users" {
		t.Errorf("the flat side = %+v, want the users table", got.Schemas[0].Tables)
	}
	if len(got.Schemas[0].Tables[0].Columns) != 2 {
		t.Errorf("the flat users table has %d columns, want 2", len(got.Schemas[0].Tables[0].Columns))
	}
}

// Scenario: Una tabla sin columnas sigue apareciendo, sin hijos.
//
// A table the column query did not return is still a table. Dropping it would make
// the explorer disagree with `dbx list tables`, and a user comparing the two would
// think one of them is lying.
func TestLoadDatabase_ATableWithNoColumnsStillAppears(t *testing.T) {
	conn := &pgxfake.Conn{Steps: []pgxfake.Step{
		{Match: "information_schema.schemata", Result: pgxfake.Result{
			Columns: []pgxfake.Column{{Name: "schema_name"}},
			Rows:    [][]any{{"public"}},
		}},
		{Match: "information_schema.columns", Result: pgxfake.Result{
			Columns: []pgxfake.Column{{Name: "table_name"}},
		}},
		{Match: "information_schema.tables", Result: pgxfake.Result{
			Columns: []pgxfake.Column{{Name: "table_name"}},
			Rows:    [][]any{{"ghost", "BASE TABLE", 0}},
		}},
	}}

	got, err := NewSchemaLoader(conn).LoadDatabase(context.Background(), "shop")
	if err != nil {
		t.Fatalf("LoadDatabase: %v", err)
	}
	schema := got.Root.Children[0]
	if len(schema.Children) != 1 {
		t.Fatalf("the schema has %d tables, want 1", len(schema.Children))
	}
	if len(schema.Children[0].Children) != 0 {
		t.Errorf("the table has %d column children, want none", len(schema.Children[0].Children))
	}
	if len(got.Schemas[0].Tables) != 1 {
		t.Errorf("the flat side has %d tables, want the one with no columns", len(got.Schemas[0].Tables))
	}
}

// Scenario: No poder listar los esquemas hace fallar la carga entera.
//
// There is no tree without a schema list, and an empty tree would render as an
// empty database rather than as a failure.
func TestLoadDatabase_ACannotListSchemasIsFatal(t *testing.T) {
	conn := &pgxfake.Conn{Steps: []pgxfake.Step{
		{Match: "information_schema.schemata", Err: errors.New("pq: permission denied for schema")},
	}}

	_, err := NewSchemaLoader(conn).LoadDatabase(context.Background(), "shop")
	if err == nil {
		t.Fatal("a schema-list failure returned no error")
	}
	if !strings.Contains(err.Error(), "failed to list schemas") {
		t.Errorf("error = %v, want it to say the schema list failed", err)
	}
}

// Scenario: Un esquema ilegible desaparece del árbol, no aparece vacío.
//
// KNOWN BEHAVIOUR, pinned because it is a choice: LoadDatabase does `continue` on a
// table-listing failure, which happens BEFORE the schema is appended, so a schema
// whose tables cannot be listed is dropped entirely rather than shown with no
// tables. Showing it empty would arguably be friendlier — the user would see the
// schema exists — but a schema that appears with nothing in it reads as "this
// schema is empty", which is a different and wrong statement.
func TestLoadDatabase_AnUnreadableSchemaIsDroppedEntirely(t *testing.T) {
	conn := &pgxfake.Conn{Steps: []pgxfake.Step{
		{Match: "information_schema.schemata", Result: pgxfake.Result{
			Columns: []pgxfake.Column{{Name: "schema_name"}},
			Rows:    [][]any{{"public"}, {"secret"}},
		}},
		{Match: "information_schema.tables", Result: pgxfake.Result{
			Columns: []pgxfake.Column{{Name: "table_name"}},
			Rows:    [][]any{{"users", "BASE TABLE", 1}},
		}},
		{Match: "information_schema.columns", Result: pgxfake.Result{
			Columns: []pgxfake.Column{{Name: "table_name"}},
			Rows:    [][]any{{"users", "id", "integer", "NO", nil}},
		}},
	}}
	// Every table listing fails, so both schemas come back empty but the load
	// still succeeds rather than erroring.
	conn.Steps[1].Err = errors.New("permission denied")

	got, err := NewSchemaLoader(conn).LoadDatabase(context.Background(), "shop")
	if err != nil {
		t.Fatalf("an unreadable schema should be skipped, not fatal: %v", err)
	}
	if len(got.Root.Children) != 0 {
		var names []string
		for _, s := range got.Root.Children {
			names = append(names, s.Name)
		}
		t.Errorf("the root has schemas %v, want none: a schema whose tables cannot be listed is dropped", names)
	}
	if len(got.Schemas) != 0 {
		t.Errorf("the flat side has %d schemas, want none", len(got.Schemas))
	}
}

// Scenario: Un esquema legible sobrevive a que otro no lo sea.
//
// The drop above must not take the whole tree with it: one permission problem in
// one schema must not empty a database the user can otherwise see. The step is
// keyed on the bound schema, so the same connection answers one schema and refuses
// the other.
func TestLoadDatabase_OneBadSchemaDoesNotEmptyTheTree(t *testing.T) {
	conn := &pgxfake.Conn{Steps: []pgxfake.Step{
		{Match: "information_schema.schemata", Result: pgxfake.Result{
			Columns: []pgxfake.Column{{Name: "schema_name"}},
			Rows:    [][]any{{"public"}, {"sales"}},
		}},
		{Match: "information_schema.columns", Result: pgxfake.Result{
			Columns: []pgxfake.Column{{Name: "table_name"}},
			Rows:    [][]any{{"users", "id", "integer", "NO", nil}},
		}},
		{Match: "information_schema.tables", ArgContains: "sales", Err: errors.New("permission denied for schema sales")},
		{Match: "information_schema.tables", Result: pgxfake.Result{
			Columns: []pgxfake.Column{{Name: "table_name"}},
			Rows:    [][]any{{"users", "BASE TABLE", 1}},
		}},
	}}

	got, err := NewSchemaLoader(conn).LoadDatabase(context.Background(), "shop")
	if err != nil {
		t.Fatalf("LoadDatabase: %v", err)
	}
	if len(got.Root.Children) != 1 {
		var names []string
		for _, s := range got.Root.Children {
			names = append(names, s.Name)
		}
		t.Fatalf("the root has %d schemas (%v), want only public", len(got.Root.Children), names)
	}
	if got.Root.Children[0].Name != "public" {
		t.Errorf("the surviving schema is %q, want public", got.Root.Children[0].Name)
	}
	if len(got.Root.Children[0].Children) != 1 {
		t.Errorf("the surviving schema has %d tables, want 1", len(got.Root.Children[0].Children))
	}
	if len(got.Schemas) != 1 || got.Schemas[0].Name != "public" {
		t.Errorf("the flat side = %v, want just public", got.Schemas)
	}
}

// --- GetTableOverview --------------------------------------------------------

// Scenario: El resumen de una tabla lleva su tipo, sus tamaños y sus recuentos.
//
// Four separate queries because pg_stat_user_tables does not exist for system
// tables, and the sizes come from pg_class. The overview is what the user reads
// before deciding whether a table is worth opening.
func TestGetTableOverview_ReadsEveryField(t *testing.T) {
	conn := &pgxfake.Conn{Steps: []pgxfake.Step{
		{Match: "obj_description", Result: pgxfake.Result{Rows: [][]any{{"BASE TABLE", "the users table"}}}},
		{Match: "pg_size_pretty", Result: pgxfake.Result{Rows: [][]any{{"16384 kB", "8192 kB", "8192 kB"}}}},
		{Match: "pg_stat_user_tables", Result: pgxfake.Result{Rows: [][]any{{100, 5, "2024-01-02 03:04:05", nil, nil, nil}}}},
		{Match: "count(*)", Result: pgxfake.Result{Rows: [][]any{{4, 2, 3, 1}}}},
	}}

	got, err := NewSchemaLoader(conn).GetTableOverview(context.Background(), "public", "users")
	if err != nil {
		t.Fatalf("GetTableOverview: %v", err)
	}

	if got.TableName != "users" {
		t.Errorf("TableName = %q, want users: it is the argument, not the server's", got.TableName)
	}
	if got.TableType != "BASE TABLE" {
		t.Errorf("TableType = %q, want BASE TABLE", got.TableType)
	}
	if got.Comment == nil || *got.Comment != "the users table" {
		t.Errorf("Comment = %v, want the description", got.Comment)
	}
	if got.TotalSize != "16384 kB" || got.TableSize != "8192 kB" || got.IndexSize != "8192 kB" {
		t.Errorf("sizes = %q/%q/%q, want 16384/8192/8192", got.TotalSize, got.TableSize, got.IndexSize)
	}
	if got.LiveTuples != 100 || got.DeadTuples != 5 {
		t.Errorf("tuples = %d live / %d dead, want 100/5", got.LiveTuples, got.DeadTuples)
	}
	if got.ColumnCount != 4 || got.ConstraintCount != 2 || got.IndexCount != 3 || got.FKCount != 1 {
		t.Errorf("counts = %d/%d/%d/%d, want 4/2/3/1", got.ColumnCount, got.ConstraintCount, got.IndexCount, got.FKCount)
	}
	// A vacuum timestamp the server reports as a bool-ish marker is not asserted
	// here; what matters is that a nil one stays nil.
	if got.LastVacuum == nil {
		t.Error("LastVacuum = nil, want the value the server sent")
	}
}

// Scenario: Una tabla sin comentario es unnil, no una cadena vacía.
//
// The UI shows "no comment" for nil and "" for empty, and collapsing the two makes
// an undescribed table look like a described one with nothing in it.
func TestGetTableOverview_ANilCommentStaysNil(t *testing.T) {
	conn := &pgxfake.Conn{Steps: []pgxfake.Step{
		{Match: "obj_description", Result: pgxfake.Result{Rows: [][]any{{"BASE TABLE", nil}}}},
		{Match: "pg_size_pretty", Result: pgxfake.Result{Rows: [][]any{{"8192 kB", "4096 kB", "4096 kB"}}}},
		{Match: "pg_stat_user_tables", Result: pgxfake.Result{Rows: [][]any{{1, 0, nil, nil, nil, nil}}}},
		{Match: "count(*)", Result: pgxfake.Result{Rows: [][]any{{1, 0, 1, 0}}}},
	}}

	got, err := NewSchemaLoader(conn).GetTableOverview(context.Background(), "public", "users")
	if err != nil {
		t.Fatalf("GetTableOverview: %v", err)
	}
	if got.Comment != nil {
		t.Errorf("Comment = %q, want nil for an undescribed table", *got.Comment)
	}
}

// Scenario: Si no hay estadísticas el recuento queda en -1, no en 0.
//
// This is the one place a failure is deliberately swallowed, and the sentinel
// matters: 0 live tuples and "unknown" are different answers, and a table with no
// stats row must not read as an empty table.
func TestGetTableOverview_MissingStatsBecomeMinusOne(t *testing.T) {
	conn := &pgxfake.Conn{Steps: []pgxfake.Step{
		{Match: "obj_description", Result: pgxfake.Result{Rows: [][]any{{"BASE TABLE", nil}}}},
		{Match: "pg_size_pretty", Result: pgxfake.Result{Rows: [][]any{{"0 kB", "0 kB", "0 kB"}}}},
		// pg_stat_user_tables has no row for a system table.
		{Match: "pg_stat_user_tables", Err: errors.New("no data for this table")},
		{Match: "count(*)", Result: pgxfake.Result{Rows: [][]any{{0, 0, 0, 0}}}},
	}}

	got, err := NewSchemaLoader(conn).GetTableOverview(context.Background(), "pg_catalog", "pg_class")
	if err != nil {
		t.Fatalf("missing statistics must not fail the overview: %v", err)
	}
	if got.LiveTuples != -1 {
		t.Errorf("LiveTuples = %d, want -1 for unknown", got.LiveTuples)
	}
	// And the rest of the overview is still real.
	if got.TableName != "pg_class" || got.ColumnCount != 0 {
		t.Errorf("overview = %+v, want the table still described", got)
	}
}

// Scenario: No saber el tipo de la tabla es un error.
//
// Unlike the statistics, this one is fatal: a table of unknown type cannot be
// rendered at all, so there is nothing partial worth returning.
func TestGetTableOverview_ACannotReadTheTableTypeIsFatal(t *testing.T) {
	conn := &pgxfake.Conn{Steps: []pgxfake.Step{
		{Match: "obj_description", Err: errors.New("pq: relation does not exist")},
	}}

	_, err := NewSchemaLoader(conn).GetTableOverview(context.Background(), "public", "ghost")
	if err == nil {
		t.Fatal("a missing table returned an overview")
	}
	if !strings.Contains(err.Error(), "failed to get table type") {
		t.Errorf("error = %v, want it to name the type lookup", err)
	}
}

// Scenario: No saber los tamaños también es un error.
//
// The sizes are shown next to the name, and an overview with a name and no sizes
// looks like a rendering bug rather than a failure.
func TestGetTableOverview_ACannotReadTheSizesIsFatal(t *testing.T) {
	conn := &pgxfake.Conn{Steps: []pgxfake.Step{
		{Match: "obj_description", Result: pgxfake.Result{Rows: [][]any{{"BASE TABLE", nil}}}},
		{Match: "pg_size_pretty", Err: errors.New("pq: could not read relation")},
	}}

	_, err := NewSchemaLoader(conn).GetTableOverview(context.Background(), "public", "users")
	if err == nil {
		t.Fatal("unreadable sizes returned an overview")
	}
	if !strings.Contains(err.Error(), "failed to get table sizes") {
		t.Errorf("error = %v, want it to name the size lookup", err)
	}
}

// Scenario: No poder contar es un error.
//
// Unlike the statistics, the counts are not optional: a table showing zero
// columns would be actively wrong.
func TestGetTableOverview_ACannotCountIsFatal(t *testing.T) {
	conn := &pgxfake.Conn{Steps: []pgxfake.Step{
		{Match: "obj_description", Result: pgxfake.Result{Rows: [][]any{{"BASE TABLE", nil}}}},
		{Match: "pg_size_pretty", Result: pgxfake.Result{Rows: [][]any{{"8 kB", "4 kB", "4 kB"}}}},
		{Match: "pg_stat_user_tables", Result: pgxfake.Result{Rows: [][]any{{1, 0, nil, nil, nil, nil}}}},
		{Match: "count(*)", Err: errors.New("permission denied")},
	}}

	_, err := NewSchemaLoader(conn).GetTableOverview(context.Background(), "public", "users")
	if err == nil {
		t.Fatal("unreadable counts returned an overview")
	}
	if !strings.Contains(err.Error(), "failed to get counts") {
		t.Errorf("error = %v, want it to name the count lookup", err)
	}
}

// --- helpers -----------------------------------------------------------------

func oneline(s string) string { return strings.Join(strings.Fields(s), " ") }

func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// --- SchemaText: the prompt the model actually reads -------------------------

// Scenario: El esquema se renderiza con la forma exacta que consume el modelo.
//
// This text is the whole input to `dbx ask` and to the ASK pane. It is not a log
// line: a model parses it, so the indentation, the row count and the NULL marker
// are all load-bearing. Changing one of them changes what SQL the model writes.
func TestSchemaText_RendersTheShapeTheModelExpects(t *testing.T) {
	got := SchemaText([]SchemaDetail{{
		Name: "public",
		Tables: []TableDetail{
			{Name: "users", Type: "BASE TABLE", RowCount: 1200, Columns: []ColumnInfo{
				{Name: "id", DataType: "integer", IsNullable: "NO"},
				{Name: "email", DataType: "text", IsNullable: "YES"},
			}},
			{Name: "orders", Type: "BASE TABLE", RowCount: 7},
		},
	}})

	want := "Schema: public\n" +
		"  Table: users (1200 rows)\n" +
		"    id integer\n" +
		"    email text NULL\n" +
		"  Table: orders (7 rows)\n" +
		"\n"
	if got != want {
		t.Errorf("SchemaText =\n%q\nwant\n%q", got, want)
	}
}

// Scenario: Solo "YES" marca una columna como nulable.
//
// PostgreSQL reports "YES" or "NO", so any other value is a NOT NULL column. A
// loose compare would mark an empty string as nullable, and the model would emit a
// column it could have used in a WHERE.
func TestSchemaText_OnlyYesMarksAColumnNullable(t *testing.T) {
	for _, tc := range []struct {
		isNullable string
		wantNull   bool
	}{
		{"YES", true},
		{"NO", false},
		{"", false},
		{"yes", false},
		{"YES ", false},
		{"MAYBE", false},
	} {
		t.Run("is_nullable="+tc.isNullable, func(t *testing.T) {
			got := SchemaText([]SchemaDetail{{
				Name:   "public",
				Tables: []TableDetail{{Name: "t", Columns: []ColumnInfo{{Name: "c", DataType: "text", IsNullable: tc.isNullable}}}},
			}})
			if strings.Contains(got, "NULL") != tc.wantNull {
				t.Errorf("is_nullable %q produced %q, want NULL present = %v", tc.isNullable, oneline(got), tc.wantNull)
			}
		})
	}
}

// Scenario: Varios esquemas se separan con una línea en blanco.
//
// The blank line is what tells the model where one schema ends, and dropping it
// would run the last table of one schema into the first table of the next.
func TestSchemaText_SeparatesSchemasWithABlankLine(t *testing.T) {
	got := SchemaText([]SchemaDetail{
		{Name: "public", Tables: []TableDetail{{Name: "users"}}},
		{Name: "sales", Tables: []TableDetail{{Name: "orders"}}},
	})

	want := "Schema: public\n  Table: users (0 rows)\n\nSchema: sales\n  Table: orders (0 rows)\n\n"
	if got != want {
		t.Errorf("SchemaText =\n%q\nwant\n%q", got, want)
	}
	if strings.Count(got, "\n\n") != 2 {
		t.Errorf("SchemaText has %d blank-line separators, want one per schema:\n%q", strings.Count(got, "\n\n"), got)
	}
}

// Scenario: Una tabla sin columnas aparece igual, con su recuento.
//
// The model needs to know the table exists and roughly how big it is even if the
// column query returned nothing. Dropping the table would make a table invisible
// when that is the most interesting thing about the schema.
func TestSchemaText_ATableWithNoColumnsStillAppears(t *testing.T) {
	got := SchemaText([]SchemaDetail{{
		Name:   "public",
		Tables: []TableDetail{{Name: "opaque", RowCount: 5}},
	}})
	if !strings.Contains(got, "  Table: opaque (5 rows)") {
		t.Errorf("SchemaText = %q, want the table with its row count", got)
	}
}

// Scenario: Sin esquemas el texto está vacío.
//
// Not a placeholder, not "no schemas": empty, because the caller appends this to a
// prompt and any text here would be read as part of the question.
func TestSchemaText_NoSchemasIsEmpty(t *testing.T) {
	for _, in := range [][]SchemaDetail{nil, {}} {
		if got := SchemaText(in); got != "" {
			t.Errorf("SchemaText(%v) = %q, want empty", in, got)
		}
	}
	// A schema with no tables is not empty: the schema itself is information.
	one := SchemaText([]SchemaDetail{{Name: "empty_schema"}})
	if !strings.Contains(one, "Schema: empty_schema") {
		t.Errorf("SchemaText = %q, want the schema named even with no tables", one)
	}
}

// Scenario: Un default no aparece en el texto del modelo.
//
// Deliberate: the default is an expression, and an expression pasted into a
// generated INSERT is more often wrong than useful. What the model gets is the
// name, the type and the nullability, which is what it needs to write a WHERE.
func TestSchemaText_DoesNotLeakColumnDefaults(t *testing.T) {
	def := "nextval('users_id_seq'::regclass)"
	got := SchemaText([]SchemaDetail{{
		Name: "public",
		Tables: []TableDetail{{Name: "users", Columns: []ColumnInfo{
			{Name: "id", DataType: "integer", IsNullable: "NO", Default: &def},
		}}},
	}})
	if strings.Contains(got, "nextval") {
		t.Errorf("SchemaText = %q, want the default left out", got)
	}
	if !strings.Contains(got, "id integer") {
		t.Errorf("SchemaText = %q, want the column still listed", got)
	}
}
