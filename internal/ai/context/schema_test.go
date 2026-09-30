package context

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/buble/dbx/internal/testsupport/pgxfake"
)

// exportSteps is the standard script for a one-schema, one-table database, which
// is the smallest thing that exercises every query ExportSchema makes.
func exportSteps(schema, table string, tables [][]any, columns [][]any) []pgxfake.Step {
	return []pgxfake.Step{
		{Match: "information_schema.schemata", Result: pgxfake.Result{
			Columns: []pgxfake.Column{{Name: "schema_name"}},
			Rows:    [][]any{{schema}},
		}},
		{Match: "information_schema.tables", Result: pgxfake.Result{
			Columns: []pgxfake.Column{{Name: "table_name"}},
			Rows:    tables,
		}},
		{Match: "information_schema.columns", Result: pgxfake.Result{
			Columns: []pgxfake.Column{{Name: "column_name"}},
			Rows:    columns,
		}},
		{Match: "pg_am", Result: pgxfake.Result{
			Columns: []pgxfake.Column{{Name: "index_name"}},
			Rows: [][]any{
				{"users_pkey", "btree", "CREATE UNIQUE INDEX ...", true, true, []string{"id"}},
			},
		}},
		{Match: "constraint_column_usage", Result: pgxfake.Result{
			Columns: []pgxfake.Column{{Name: "constraint_name"}},
			Rows: [][]any{
				{"orders_user_fkey", "user_id", "public", "users", "id"},
			},
		}},
	}
}

// --- ExportSchema: the shape the model is given ------------------------------

// Scenario: La exportación lleva base, esquemas, tablas, columnas, índices y
// claves foráneas.
//
// This JSON is what a model is asked to write SQL against, so every field in it is
// load-bearing: a missing index means it invents a slow query, a missing foreign
// key means it invents a join that does not exist. The whole tree is asserted, not
// just that it is non-empty.
func TestExportSchema_CarriesTheWholeTree(t *testing.T) {
	conn := &pgxfake.Conn{Steps: exportSteps("public", "orders",
		[][]any{{"orders", "BASE TABLE", 120}},
		[][]any{
			{"id", "integer", "NO", "nextval('s'::regclass)", 1},
			{"user_id", "integer", "YES", nil, 2},
		})}

	got, err := ExportSchema(context.Background(), conn, "shop")
	if err != nil {
		t.Fatalf("ExportSchema: %v", err)
	}

	if got.Database != "shop" {
		t.Errorf("Database = %q, want shop", got.Database)
	}
	if len(got.Schemas) != 1 {
		t.Fatalf("got %d schemas, want 1", len(got.Schemas))
	}
	s := got.Schemas[0]
	if s.Name != "public" {
		t.Errorf("schema = %q, want public", s.Name)
	}
	if len(s.Tables) != 1 {
		t.Fatalf("got %d tables, want 1", len(s.Tables))
	}

	tbl := s.Tables[0]
	if tbl.Name != "orders" || tbl.Type != "BASE TABLE" {
		t.Errorf("table = %q/%q, want orders/BASE TABLE", tbl.Name, tbl.Type)
	}
	if tbl.RowCount != 120 {
		t.Errorf("RowCount = %d, want 120", tbl.RowCount)
	}

	if len(tbl.Columns) != 2 {
		t.Fatalf("got %d columns, want 2", len(tbl.Columns))
	}
	id := tbl.Columns[0]
	if id.Name != "id" || id.DataType != "integer" {
		t.Errorf("first column = %+v, want id integer", id)
	}
	if id.OrdinalPos != 1 {
		t.Errorf("OrdinalPos = %d, want 1: the position is what tells the model the column order", id.OrdinalPos)
	}
	if id.IsNullable {
		t.Error("a NOT NULL column came out nullable")
	}
	if id.DefaultValue == nil || *id.DefaultValue != "nextval('s'::regclass)" {
		t.Errorf("DefaultValue = %v, want the sequence expression", id.DefaultValue)
	}
	// A nullable column with no default: both facts have to survive, because a
	// generated INSERT that omits a nullable column and one that writes NULL are
	// different statements.
	user := tbl.Columns[1]
	if !user.IsNullable {
		t.Error("a YES nullable column came out not nullable")
	}
	if user.DefaultValue != nil {
		t.Errorf("DefaultValue = %v, want nil for a column with no default", *user.DefaultValue)
	}

	if len(tbl.Indexes) != 1 {
		t.Fatalf("got %d indexes, want 1", len(tbl.Indexes))
	}
	idx := tbl.Indexes[0]
	if idx.Name != "users_pkey" || !idx.IsUnique || !idx.IsPrimary {
		t.Errorf("index = %+v, want users_pkey unique and primary", idx)
	}
	if len(idx.Columns) != 1 || idx.Columns[0] != "id" {
		t.Errorf("index columns = %v, want [id]", idx.Columns)
	}

	if len(tbl.FKs) != 1 {
		t.Fatalf("got %d foreign keys, want 1", len(tbl.FKs))
	}
	fk := tbl.FKs[0]
	if fk.Name != "orders_user_fkey" || fk.Columns != "user_id" {
		t.Errorf("foreign key = %+v, want orders_user_fkey on user_id", fk)
	}
	if fk.RefSchema != "public" || fk.RefTable != "users" || fk.RefColumns != "id" {
		t.Errorf("foreign key target = %s.%s(%s), want public.users(id)", fk.RefSchema, fk.RefTable, fk.RefColumns)
	}
}

// Scenario: Los esquemas del sistema no se exportan.
//
// They are filtered in SQL and again in Go. The Go-side filter is the one that
// matters here, because it is the second line of defence: if the SQL ever loses
// its WHERE, an LLM prompt suddenly contains four hundred tables of pg_catalog
// and no room for the user's actual schema.
func TestExportSchema_SystemSchemasAreNotExported(t *testing.T) {
	conn := &pgxfake.Conn{Steps: []pgxfake.Step{
		{Match: "information_schema.schemata", Result: pgxfake.Result{
			Columns: []pgxfake.Column{{Name: "schema_name"}},
			Rows:    [][]any{{"pg_catalog"}, {"information_schema"}, {"pg_toast"}, {"public"}},
		}},
		{Match: "information_schema.tables", Result: pgxfake.Result{
			Columns: []pgxfake.Column{{Name: "table_name"}},
			Rows:    [][]any{{"users", "BASE TABLE", 1}},
		}},
		{Match: "information_schema.columns", Result: pgxfake.Result{
			Columns: []pgxfake.Column{{Name: "column_name"}},
		}},
		{Match: "pg_am", Result: pgxfake.Result{Columns: []pgxfake.Column{{Name: "index_name"}}}},
		{Match: "constraint_column_usage", Result: pgxfake.Result{Columns: []pgxfake.Column{{Name: "constraint_name"}}}},
	}}

	got, err := ExportSchema(context.Background(), conn, "shop")
	if err != nil {
		t.Fatalf("ExportSchema: %v", err)
	}
	if len(got.Schemas) != 1 {
		t.Fatalf("got %d schemas, want only public: %v", len(got.Schemas), got.Schemas)
	}
	if got.Schemas[0].Name != "public" {
		t.Errorf("the surviving schema is %q, want public", got.Schemas[0].Name)
	}
}

// Scenario: Una tabla sin columnas sigue apareciendo, y el error se descarta.
//
// Columns, indexes and foreign keys are read with their errors DISCARDED, unlike
// the schema and table lists where a failure skips the schema. That asymmetry is
// deliberate: a table that exists but whose columns cannot be read is still a
// table, and an LLM told about a table with no columns writes worse SQL than one
// told about a table that is not there at all. KNOWN BEHAVIOUR, pinned because the
// discarded `_` is otherwise invisible.
func TestExportSchema_ATableWithUnreadableColumnsStillAppears(t *testing.T) {
	conn := &pgxfake.Conn{Steps: []pgxfake.Step{
		{Match: "information_schema.schemata", Result: pgxfake.Result{
			Columns: []pgxfake.Column{{Name: "schema_name"}},
			Rows:    [][]any{{"public"}},
		}},
		{Match: "information_schema.tables", Result: pgxfake.Result{
			Columns: []pgxfake.Column{{Name: "table_name"}},
			Rows:    [][]any{{"opaque", "BASE TABLE", 5}},
		}},
		{Match: "information_schema.columns", Err: errors.New("permission denied for table opaque")},
		{Match: "pg_am", Err: errors.New("permission denied")},
		{Match: "constraint_column_usage", Err: errors.New("permission denied")},
	}}

	got, err := ExportSchema(context.Background(), conn, "shop")
	if err != nil {
		t.Fatalf("an unreadable table's columns must not fail the export: %v", err)
	}
	if len(got.Schemas) != 1 || len(got.Schemas[0].Tables) != 1 {
		t.Fatalf("the table did not survive: %+v", got.Schemas)
	}
	tbl := got.Schemas[0].Tables[0]
	if tbl.Name != "opaque" || tbl.RowCount != 5 {
		t.Errorf("table = %q/%d, want opaque/5", tbl.Name, tbl.RowCount)
	}
	if len(tbl.Columns) != 0 || len(tbl.Indexes) != 0 || len(tbl.FKs) != 0 {
		t.Errorf("something came back from a failed query: %d columns, %d indexes, %d fks",
			len(tbl.Columns), len(tbl.Indexes), len(tbl.FKs))
	}
}

// Scenario: Un esquema ilegible se salta entero.
//
// One more level up, the opposite choice: a schema whose TABLES cannot be listed is
// dropped, because a schema with no tables reads as an empty schema, which is a
// different and wrong statement.
func TestExportSchema_ASchemaWithUnreadableTablesIsDropped(t *testing.T) {
	conn := &pgxfake.Conn{Steps: []pgxfake.Step{
		{Match: "information_schema.schemata", Result: pgxfake.Result{
			Columns: []pgxfake.Column{{Name: "schema_name"}},
			Rows:    [][]any{{"public"}, {"secret"}},
		}},
		// Refuse only the second schema, keyed on the bound argument.
		{Match: "information_schema.tables", ArgContains: "secret", Err: errors.New("permission denied")},
		{Match: "information_schema.tables", Result: pgxfake.Result{
			Columns: []pgxfake.Column{{Name: "table_name"}},
			Rows:    [][]any{{"users", "BASE TABLE", 1}},
		}},
		{Match: "information_schema.columns", Result: pgxfake.Result{
			Columns: []pgxfake.Column{{Name: "column_name"}},
		}},
		{Match: "pg_am", Result: pgxfake.Result{Columns: []pgxfake.Column{{Name: "index_name"}}}},
		{Match: "constraint_column_usage", Result: pgxfake.Result{Columns: []pgxfake.Column{{Name: "constraint_name"}}}},
	}}

	got, err := ExportSchema(context.Background(), conn, "shop")
	if err != nil {
		t.Fatalf("ExportSchema: %v", err)
	}
	if len(got.Schemas) != 1 || got.Schemas[0].Name != "public" {
		t.Errorf("schemas = %+v, want only public", got.Schemas)
	}
}

// Scenario: No poder ni listar los esquemas sí es un error.
//
// Without the schema list there is nothing to export, and an empty export would be
// indistinguishable from an empty database.
func TestExportSchema_ACannotListSchemasIsFatal(t *testing.T) {
	conn := &pgxfake.Conn{Steps: []pgxfake.Step{
		{Match: "information_schema.schemata", Err: errors.New("permission denied for schema information_schema")},
	}}

	_, err := ExportSchema(context.Background(), conn, "shop")
	if err == nil {
		t.Fatal("a schema-list failure returned no error")
	}
	if !strings.Contains(err.Error(), "failed to list schemas") {
		t.Errorf("error = %v, want it to name the schema list", err)
	}
	if !errors.Is(err, conn.Steps[0].Err) {
		t.Errorf("error = %v, want it to wrap the driver error", err)
	}
}

// Scenario: "YES" es lo único que significa nulable.
//
// PostgreSQL reports YES or NO, and the export turns that into a bool. A loose
// compare would mark an empty or lowercase value as nullable, and the model would
// then assume a column can be NULL when it cannot.
func TestExportSchema_OnlyYesBecomesNullable(t *testing.T) {
	for _, tc := range []struct {
		nullable string
		want     bool
	}{
		{"YES", true},
		{"NO", false},
		{"", false},
		{"yes", false},
		{"YES ", false},
	} {
		t.Run("is_nullable="+tc.nullable, func(t *testing.T) {
			conn := &pgxfake.Conn{Steps: exportSteps("public", "t",
				[][]any{{"t", "BASE TABLE", 0}},
				[][]any{{"c", "text", tc.nullable, nil, 1}})}

			got, err := ExportSchema(context.Background(), conn, "shop")
			if err != nil {
				t.Fatalf("ExportSchema: %v", err)
			}
			if len(got.Schemas[0].Tables[0].Columns) != 1 {
				t.Fatalf("the column is missing: %+v", got.Schemas[0].Tables[0])
			}
			if g := got.Schemas[0].Tables[0].Columns[0].IsNullable; g != tc.want {
				t.Errorf("is_nullable %q became %v, want %v", tc.nullable, g, tc.want)
			}
		})
	}
}

// --- the five list helpers: the shared contract ------------------------------

// Scenario: Cada listado falla como un listado, no como un silencio.
//
// The five helpers are the same function five times, so the same three failure
// points have to hold for all of them: the query, the row scan, and the iteration.
// ExportSchema then decides per call site whether to swallow the error, so the
// helper has to REPORT it faithfully first.
func TestTheListHelpers_ReportEveryFailure(t *testing.T) {
	type call struct {
		name string
		run  func() (int, error)
		// step is the matcher for the one query this helper makes.
		step string
	}

	calls := []call{
		{"listSchemas", func() (int, error) { g, e := listSchemas(context.Background(), &pgxfake.Conn{}); return len(g), e }, "information_schema.schemata"},
		{"listTables", func() (int, error) {
			g, e := listTables(context.Background(), &pgxfake.Conn{}, "public")
			return len(g), e
		}, "information_schema.tables"},
		{"listColumns", func() (int, error) {
			g, e := listColumns(context.Background(), &pgxfake.Conn{}, "public", "t")
			return len(g), e
		}, "information_schema.columns"},
		{"listIndexes", func() (int, error) {
			g, e := listIndexes(context.Background(), &pgxfake.Conn{}, "public", "t")
			return len(g), e
		}, "pg_am"},
		{"listFKs", func() (int, error) {
			g, e := listFKs(context.Background(), &pgxfake.Conn{}, "public", "t")
			return len(g), e
		}, "constraint_column_usage"},
	}

	for _, tc := range calls {
		t.Run(tc.name+"/the query fails", func(t *testing.T) {
			boom := errors.New("permission denied")
			conn := &pgxfake.Conn{Steps: []pgxfake.Step{{Match: tc.step, Err: boom}}}
			got, err := callWith(t, tc, conn)
			if err == nil {
				t.Fatalf("%s turned a driver failure into no error", tc.name)
			}
			if !errors.Is(err, boom) {
				t.Errorf("error = %v, want it to wrap the driver error", err)
			}
			if got != 0 {
				t.Errorf("%s returned %d items alongside the error, want none", tc.name, got)
			}
		})

		t.Run(tc.name+"/a row cannot be read", func(t *testing.T) {
			conn := &pgxfake.Conn{Steps: []pgxfake.Step{{
				Match: tc.step,
				Result: pgxfake.Result{
					Columns: []pgxfake.Column{{Name: "a"}, {Name: "b"}},
					Rows:    [][]any{{"x", "y"}},
					ScanErr: errors.New("cannot convert"),
				},
			}}}
			got, err := callWith(t, tc, conn)
			if err == nil {
				t.Fatalf("%s turned a failed scan into no error", tc.name)
			}
			if !strings.Contains(err.Error(), "cannot convert") {
				t.Errorf("error = %v, want the scan failure", err)
			}
			if got != 0 {
				t.Errorf("%s returned %d items alongside the error, want none", tc.name, got)
			}
		})

		t.Run(tc.name+"/the connection drops mid-read", func(t *testing.T) {
			conn := &pgxfake.Conn{Steps: []pgxfake.Step{{
				Match: tc.step,
				Result: pgxfake.Result{
					Columns: []pgxfake.Column{{Name: "a"}, {Name: "b"}},
					Rows:    [][]any{{"x", "y"}},
					IterErr: errors.New("connection reset by peer"),
				},
			}}}
			got, err := callWith(t, tc, conn)
			if err == nil {
				t.Fatalf("%s reported a truncated read as a complete one", tc.name)
			}
			if !strings.Contains(err.Error(), "connection reset") {
				t.Errorf("error = %v, want the iteration failure", err)
			}
			if got != 0 {
				t.Errorf("%s returned %d items alongside the error, want none", tc.name, got)
			}
		})
	}
}

// callWith re-runs a call against a specific connection, by rebuilding the closure
// with the connection swapped in. The closures above take no connection, so this
// is the seam: each entry is a function of a connection, and the table supplies it.
func callWith(t *testing.T, tc struct {
	name string
	run  func() (int, error)
	step string
}, conn *pgxfake.Conn,
) (int, error) {
	t.Helper()
	_ = conn
	// The table's closures capture their own zero-valued connection, so they are
	// re-created here against the one under test. Written out per helper rather
	// than reflected over, because a table of closures over a variable is exactly
	// the kind of thing that silently tests the wrong thing.
	switch tc.name {
	case "listSchemas":
		g, e := listSchemas(context.Background(), conn)
		return len(g), e
	case "listTables":
		g, e := listTables(context.Background(), conn, "public")
		return len(g), e
	case "listColumns":
		g, e := listColumns(context.Background(), conn, "public", "t")
		return len(g), e
	case "listIndexes":
		g, e := listIndexes(context.Background(), conn, "public", "t")
		return len(g), e
	case "listFKs":
		g, e := listFKs(context.Background(), conn, "public", "t")
		return len(g), e
	default:
		t.Fatalf("no case for %q", tc.name)
		return 0, nil
	}
}

// Scenario: Ninguna consulta interpola el esquema ni la tabla.
//
// Everything is a bound parameter, and `users' OR '1'='1` has to arrive at the
// server as a value. This JSON is fed to a model, so an injection here would mean
// arbitrary SQL executed by `dbx ask`.
func TestTheListHelpers_PassTheSchemaAndTableAsParameters(t *testing.T) {
	conn := &pgxfake.Conn{Steps: exportSteps("public", "t", nil, nil)}
	for _, run := range []func(){
		func() { _, _ = listSchemas(context.Background(), conn) },
		func() { _, _ = listTables(context.Background(), conn, "public") },
		func() { _, _ = listColumns(context.Background(), conn, "public", "users") },
		func() { _, _ = listIndexes(context.Background(), conn, "public", "users") },
		func() { _, _ = listFKs(context.Background(), conn, "public", "users") },
	} {
		run()
	}
	for _, q := range conn.Queries {
		if strings.Contains(q, "'public'") || strings.Contains(q, `"public"`) {
			t.Errorf("a query interpolated the schema: %s", oneline(q))
		}
		if strings.Contains(q, "'users'") || strings.Contains(q, `"users"`) {
			t.Errorf("a query interpolated the table: %s", oneline(q))
		}
	}
}

// Scenario: Los esquemas de sistema se filtran también en SQL.
//
// The Go-side filter is the second line of defence; this is the first. Both are
// asserted, and they are separate tests on purpose: a fix to one that silently
// drops the other would leave the export working and the prompt enormous.
func TestListSchemas_TheQueryExcludesSystemSchemas(t *testing.T) {
	conn := &pgxfake.Conn{Steps: exportSteps("public", "t", nil, nil)}
	if _, err := listSchemas(context.Background(), conn); err != nil {
		t.Fatalf("listSchemas: %v", err)
	}
	if !strings.Contains(conn.Queries[0], "NOT LIKE 'pg_%'") {
		t.Errorf("query = %q, want the pg_%% exclusion", oneline(conn.Queries[0]))
	}
}

// Scenario: Los índices descartan el tipo y la definición, no las inventan.
//
// The query selects amname and pg_get_indexdef and the scan sends them to
// &struct{}{}, because IndexInfo has no field for them. The two columns are still
// SELECTed, which is why the scan has to account for all six: sending five
// destinations for six columns would read the wrong values into the wrong fields.
func TestListIndexes_SkipsTheTypeAndTheDefinition(t *testing.T) {
	conn := &pgxfake.Conn{Steps: []pgxfake.Step{
		{Match: "pg_am", Result: pgxfake.Result{
			Columns: []pgxfake.Column{{Name: "a"}, {Name: "b"}, {Name: "c"}, {Name: "d"}, {Name: "e"}, {Name: "f"}},
			Rows: [][]any{
				{"users_pkey", "btree", "CREATE UNIQUE INDEX users_pkey ON public.users USING btree (id)", true, true, []string{"id"}},
				{"users_email_idx", "btree", "CREATE INDEX users_email_idx ON public.users USING btree (email)", false, false, []string{"email"}},
			},
		}},
	}}

	got, err := listIndexes(context.Background(), conn, "public", "users")
	if err != nil {
		t.Fatalf("listIndexes: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d indexes, want 2", len(got))
	}
	if got[0].Name != "users_pkey" || !got[0].IsUnique || !got[0].IsPrimary {
		t.Errorf("first index = %+v, want users_pkey unique and primary", got[0])
	}
	if len(got[0].Columns) != 1 || got[0].Columns[0] != "id" {
		t.Errorf("first index columns = %v, want [id]", got[0].Columns)
	}
	if got[1].Name != "users_email_idx" || got[1].IsUnique || got[1].IsPrimary {
		t.Errorf("second index = %+v, want users_email_idx neither unique nor primary", got[1])
	}
	if len(got[1].Columns) != 1 || got[1].Columns[0] != "email" {
		t.Errorf("second index columns = %v, want [email]: a shifted scan would read the wrong field", got[1].Columns)
	}
}

// --- the JSON shape ----------------------------------------------------------

// Scenario: El JSON usa los nombres de clave que consume el modelo.
//
// The keys are a contract with whatever reads this file. `is_nullable` as a bool
// rather than a "YES" string is the difference between a model that writes
// `WHERE x IS NULL` correctly and one that does not.
func TestExportSchemaString_UsesTheDocumentedKeys(t *testing.T) {
	conn := &pgxfake.Conn{Steps: exportSteps("public", "orders",
		[][]any{{"orders", "BASE TABLE", 3}},
		[][]any{{"id", "integer", "NO", nil, 1}})}

	got, err := ExportSchemaString(context.Background(), conn, "shop")
	if err != nil {
		t.Fatalf("ExportSchemaString: %v", err)
	}

	var decoded struct {
		Database string `json:"database"`
		Schemas  []struct {
			Name   string `json:"name"`
			Tables []struct {
				Name     string `json:"name"`
				Type     string `json:"type"`
				RowCount int64  `json:"row_count"`
				Columns  []struct {
					Name         string  `json:"name"`
					DataType     string  `json:"data_type"`
					IsNullable   bool    `json:"is_nullable"`
					DefaultValue *string `json:"default_value"`
					OrdinalPos   int     `json:"ordinal_position"`
				} `json:"columns"`
			} `json:"tables"`
		} `json:"schemas"`
	}
	if err := json.Unmarshal([]byte(got), &decoded); err != nil {
		t.Fatalf("the output is not valid JSON: %v\n%s", err, got)
	}
	if decoded.Database != "shop" || len(decoded.Schemas) != 1 {
		t.Fatalf("decoded = %+v, want one schema named public under shop", decoded)
	}
	if len(decoded.Schemas[0].Tables) != 1 || len(decoded.Schemas[0].Tables[0].Columns) != 1 {
		t.Fatalf("the tree did not survive the round trip: %+v", decoded)
	}
	c := decoded.Schemas[0].Tables[0].Columns[0]
	if c.Name != "id" || c.DataType != "integer" || c.IsNullable || c.OrdinalPos != 1 {
		t.Errorf("column = %+v, want id/integer/not-null/position 1", c)
	}
	// And the output is indented, because it is a file a human reads when a
	// prompt is not doing what they expected.
	if !strings.Contains(got, "\n  \"database\"") {
		t.Errorf("the JSON is not indented with two spaces: %s", got)
	}
}

// Scenario: Los campos vacíos se omiten del JSON.
//
// Indexes, foreign_keys and default_value are all `omitempty`, so a table with
// none of them does not carry three empty arrays and a null. That is what keeps
// the prompt small, and it is why they are tagged rather than always emitted.
func TestExportSchemaString_OmitsEmptyOptionalFields(t *testing.T) {
	conn := &pgxfake.Conn{Steps: []pgxfake.Step{
		{Match: "information_schema.schemata", Result: pgxfake.Result{
			Columns: []pgxfake.Column{{Name: "schema_name"}},
			Rows:    [][]any{{"public"}},
		}},
		{Match: "information_schema.tables", Result: pgxfake.Result{
			Columns: []pgxfake.Column{{Name: "table_name"}},
			Rows:    [][]any{{"bare", "BASE TABLE", 0}},
		}},
		{Match: "information_schema.columns", Result: pgxfake.Result{
			Columns: []pgxfake.Column{{Name: "column_name"}},
			Rows:    [][]any{{"c", "text", "NO", nil, 1}},
		}},
		{Match: "pg_am", Result: pgxfake.Result{Columns: []pgxfake.Column{{Name: "index_name"}}}},
		{Match: "constraint_column_usage", Result: pgxfake.Result{Columns: []pgxfake.Column{{Name: "constraint_name"}}}},
	}}

	got, err := ExportSchemaString(context.Background(), conn, "shop")
	if err != nil {
		t.Fatalf("ExportSchemaString: %v", err)
	}
	// Checked as JSON KEYS rather than as substrings: "null" is a substring of
	// "is_nullable" and "nullable" is a substring of nothing useful, so a
	// substring check for the literal would either false-positive or miss.
	var raw map[string]any
	if err := json.Unmarshal([]byte(got), &raw); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, got)
	}
	tables := raw["schemas"].([]any)[0].(map[string]any)["tables"].([]any)
	tbl := tables[0].(map[string]any)
	for _, key := range []string{"indexes", "foreign_keys"} {
		if _, present := tbl[key]; present {
			t.Errorf("the table carries %q, want it omitted: %s", key, oneline(got))
		}
	}
	col := tbl["columns"].([]any)[0].(map[string]any)
	if _, present := col["default_value"]; present {
		t.Errorf("the column carries default_value, want it omitted: %s", oneline(got))
	}
	// A bare JSON null anywhere would be a value where a field was expected.
	if strings.Contains(got, ": null") {
		t.Errorf("the JSON contains a bare null: %s", oneline(got))
	}
	// And the fields that ARE always present are still there.
	for _, wanted := range []string{"database", "name", "type", "row_count", "ordinal_position", "is_nullable"} {
		if !strings.Contains(got, wanted) {
			t.Errorf("the JSON is missing %q: %s", wanted, oneline(got))
		}
	}
}

// Scenario: Un fallo al exportar hace fallar también la variante de texto.
//
// ExportSchemaString adds no context of its own, so the error has to arrive
// intact. Wrapping it here would produce "failed to list schemas: failed to
// export schema: ..." from a caller that already says the first part.
func TestExportSchemaString_PropagatesTheExportFailureUnchanged(t *testing.T) {
	boom := errors.New("permission denied for schema information_schema")
	conn := &pgxfake.Conn{Steps: []pgxfake.Step{
		{Match: "information_schema.schemata", Err: boom},
	}}

	got, err := ExportSchemaString(context.Background(), conn, "shop")
	if err == nil {
		t.Fatal("a failed export returned no error")
	}
	if !errors.Is(err, boom) {
		t.Errorf("error = %v, want it to wrap the driver error", err)
	}
	if got != "" {
		t.Errorf("output = %q, want nothing alongside the error", got)
	}
}

func oneline(s string) string { return strings.Join(strings.Fields(s), " ") }
