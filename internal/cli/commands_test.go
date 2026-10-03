package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/buble/dbx/internal/drivers/postgres"
)

// --- queryAndReport / pipe ---------------------------------------------------

// Scenario: Una consulta se ejecuta y se imprime en texto.
//
// This is the whole of `dbx query` and `dbx pipe` below the connection: the SQL
// goes to the driver unchanged and the result comes back out in the text shape.
func TestQueryAndReport_RunsTheStatementAndPrintsTheTextShape(t *testing.T) {
	conn := &fakeConn{steps: []step{{
		contains: "SELECT",
		spec: rowsSpec{
			rows: [][]any{{1, "ada"}, {2, "grace"}},
		},
	}}}

	var out bytes.Buffer
	if err := queryAndReport(&out, conn, "SELECT id, name FROM users", false); err != nil {
		t.Fatalf("queryAndReport: %v", err)
	}

	if len(conn.queried) != 1 {
		t.Fatalf("%d queries were sent, want 1", len(conn.queried))
	}
	if !strings.Contains(conn.queried[0], "SELECT id, name FROM users") {
		t.Errorf("the statement sent was %q, want the SQL it was given", conn.queried[0])
	}
	// Without column metadata the header is empty, but the rows and the count
	// must still be there.
	if !strings.Contains(out.String(), "ada") || !strings.Contains(out.String(), "grace") {
		t.Errorf("output = %q, want both rows", out.String())
	}
	if !strings.Contains(out.String(), "2 rows") {
		t.Errorf("output = %q, want the row count", out.String())
	}
}

// Scenario: Una consulta que falla dice "query failed" y no imprime nada.
//
// A syntax error is the common case for `dbx pipe`, and the message is what the
// user reads to fix it.
func TestQueryAndReport_AFailedQueryIsReportedAndPrintsNothing(t *testing.T) {
	boom := errors.New(`syntax error at or near "SELCT"`)
	conn := &fakeConn{steps: []step{{contains: "SELCT", err: boom}}}

	var out bytes.Buffer
	err := queryAndReport(&out, conn, "SELCT 1", false)
	if err == nil {
		t.Fatal("a failing query returned no error")
	}
	if !strings.Contains(err.Error(), "query failed") {
		t.Errorf("error = %v, want it to say the query failed", err)
	}
	if !errors.Is(err, boom) {
		t.Errorf("error = %v, want it to wrap the driver error %v", err, boom)
	}
	if out.Len() != 0 {
		t.Errorf("output = %q, want nothing printed for a failed query", out.String())
	}
}

// Scenario: El modo JSON imprime un objeto y no el texto.
//
// The two shapes are selected by one flag and must not blur into each other.
func TestQueryAndReport_JSONModeProducesTheObject(t *testing.T) {
	conn := &fakeConn{steps: []step{{contains: "SELECT", spec: rowsSpec{rows: [][]any{{7}}}}}}

	var out bytes.Buffer
	if err := queryAndReport(&out, conn, "SELECT 7", true); err != nil {
		t.Fatalf("queryAndReport: %v", err)
	}
	var got struct {
		Rows  [][]any `json:"rows"`
		Count int     `json:"count"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, out.String())
	}
	if got.Count != 1 || len(got.Rows) != 1 {
		t.Errorf("count=%d rows=%v, want one row", got.Count, got.Rows)
	}
	if strings.Contains(out.String(), "rows\n") {
		t.Errorf("the JSON output contains the text shape's trailing line: %s", out.String())
	}
}

// --- listTablesAndReport -----------------------------------------------------

// listConn scripts the two queries `dbx list tables` makes.
func listConn(schemas []string, tables [][]any, failTables error) *fakeConn {
	return &fakeConn{steps: []step{
		{contains: "information_schema.schemata", spec: rowsSpec{rows: toAnyRows(schemas)}},
		{contains: "information_schema.tables", spec: rowsSpec{rows: tables}, err: failTables},
	}}
}

// Scenario: `dbx list tables` imprime una línea por tabla con esquema, tipo y filas.
//
// The exact format is what a user greps for and what a script might parse.
func TestListTablesAndReport_PrintsOneLinePerTable(t *testing.T) {
	conn := listConn([]string{"public"}, [][]any{
		{"users", "BASE TABLE", 1200},
		{"orders", "BASE TABLE", 7},
	}, nil)

	var out bytes.Buffer
	if err := listTablesAndReport(&out, conn, false); err != nil {
		t.Fatalf("listTablesAndReport: %v", err)
	}

	want := "public.users (BASE TABLE, 1200 rows)\npublic.orders (BASE TABLE, 7 rows)\n"
	if out.String() != want {
		t.Errorf("output =\n%q\nwant\n%q", out.String(), want)
	}
}

// Scenario: Un esquema sin permiso se salta y los demás se imprimen.
//
// This is the reason the per-schema error is a `continue` and not a return: on a
// shared server one unreadable schema would otherwise make the command useless.
func TestListTablesAndReport_AnUnreadableSchemaIsSkipped(t *testing.T) {
	// The schema list succeeds, the table list fails, so nothing is listed —
	// which is the same visible outcome as an empty schema, and must not be an error.
	conn := listConn([]string{"public"}, nil, errors.New("permission denied for schema x"))

	var out bytes.Buffer
	if err := listTablesAndReport(&out, conn, false); err != nil {
		t.Fatalf("an unreadable schema was fatal: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("output = %q, want nothing", out.String())
	}
}

// Scenario: No poder ni listar los esquemas sí es un error.
//
// Without the schema list there is nothing to report at all, so failing is the
// only honest outcome. This is the error the whole command hangs on.
func TestListTablesAndReport_ACannotListSchemasIsFatal(t *testing.T) {
	boom := errors.New("connection reset")
	conn := &fakeConn{steps: []step{{contains: "information_schema.schemata", err: boom}}}

	var out bytes.Buffer
	err := listTablesAndReport(&out, conn, false)
	if err == nil {
		t.Fatal("listing schemas is required, so its failure must be fatal")
	}
	if !strings.Contains(err.Error(), "failed to list schemas") {
		t.Errorf("error = %v, want it to say the schema list failed", err)
	}
	if !errors.Is(err, boom) {
		t.Errorf("error = %v, want it to wrap %v", err, boom)
	}
}

// Scenario: El modo JSON acumula todas las tablas en un solo objeto.
//
// One array rather than a stream of objects, so `dbx list tables -j | jq` works.
func TestListTablesAndReport_JSONModeAccumulatesEveryTable(t *testing.T) {
	conn := listConn([]string{"public"}, [][]any{{"users", "BASE TABLE", 5}}, nil)

	var out bytes.Buffer
	if err := listTablesAndReport(&out, conn, true); err != nil {
		t.Fatalf("listTablesAndReport: %v", err)
	}

	var got []struct {
		Schema   string `json:"schema"`
		Name     string `json:"name"`
		Type     string `json:"type"`
		RowCount int    `json:"row_count"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, out.String())
	}
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1: %s", len(got), out.String())
	}
	if got[0].Schema != "public" || got[0].Name != "users" || got[0].Type != "BASE TABLE" || got[0].RowCount != 5 {
		t.Errorf("entry = %+v, want the table with its schema, name, type and row count", got[0])
	}
}

// Scenario: Sin tablas, el JSON es una lista vacía y no un null.
//
// `null` breaks `jq '.[] | .name'` with a confusing message, and an empty database
// is an ordinary state.
func TestListTablesAndReport_JSONModeWithNoTablesIsAnEmptyList(t *testing.T) {
	conn := listConn([]string{"public"}, nil, nil)

	var out bytes.Buffer
	if err := listTablesAndReport(&out, conn, true); err != nil {
		t.Fatalf("listTablesAndReport: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "[]" {
		t.Errorf("output = %q, want []", got)
	}
}

// --- findColumns / listColumnsAndReport --------------------------------------

// columnsConn scripts the schema list and the column query.
func columnsConn(schemas []string, columns [][]any, failColumns error) *fakeConn {
	return &fakeConn{steps: []step{
		{contains: "information_schema.schemata", spec: rowsSpec{rows: toAnyRows(schemas)}},
		{contains: "information_schema.columns", spec: rowsSpec{rows: columns}, err: failColumns},
	}}
}

// Scenario: `dbx schema` encuentra la tabla buscando entre los esquemas.
//
// The user does not have to know or say which schema the table is in; the search
// is what makes `dbx schema users` work.
func TestFindColumns_SearchesEverySchemaAndReturnsTheFirstMatch(t *testing.T) {
	conn := columnsConn([]string{"public", "sales"}, [][]any{
		{"id", "integer", "NO", nil},
		{"email", "text", "YES", nil},
	}, nil)

	columns, schema, err := findColumns(context.Background(), conn, "users")
	if err != nil {
		t.Fatalf("findColumns: %v", err)
	}
	if schema != "public" {
		t.Errorf("schema = %q, want public, the first schema the list returns", schema)
	}
	if len(columns) != 2 {
		t.Fatalf("got %d columns, want 2", len(columns))
	}
	if columns[0].Name != "id" || columns[0].DataType != "integer" || columns[0].IsNullable != "NO" {
		t.Errorf("first column = %+v, want id integer NO", columns[0])
	}
	if columns[1].Name != "email" || columns[1].IsNullable != "YES" {
		t.Errorf("second column = %+v, want email YES", columns[1])
	}
	if columns[0].Default != nil {
		t.Errorf("default = %v, want nil for a column with no default", *columns[0].Default)
	}
}

// Scenario: Una tabla que no está en ningún esquema es un error que la nombra.
//
// "table not found" is the whole answer, and it has to carry the name, because the
// user typed it.
func TestFindColumns_AnAbsentTableIsAnErrorNamingIt(t *testing.T) {
	conn := columnsConn([]string{"public"}, nil, nil)

	_, _, err := findColumns(context.Background(), conn, "ghosts")
	if err == nil {
		t.Fatal("a table in no schema returned no error")
	}
	if !strings.Contains(err.Error(), "ghosts") {
		t.Errorf("error = %v, want it to name the table", err)
	}
}

// Scenario: No poder listar los esquemas es un error antes de buscar la tabla.
//
// The search has nothing to iterate without the schema list, so this fails rather
// than reporting "table not found" for a connection problem.
func TestFindColumns_ACannotListSchemasIsReportedAsSuch(t *testing.T) {
	conn := &fakeConn{steps: []step{{contains: "information_schema.schemata", err: errors.New("reset")}}}

	_, _, err := findColumns(context.Background(), conn, "users")
	if err == nil {
		t.Fatal("a schema-list failure returned no error")
	}
	if !strings.Contains(err.Error(), "failed to list schemas") {
		t.Errorf("error = %v, want it to say the schema list failed, not that the table is missing", err)
	}
}

// Scenario: Un error al leer las columnas de un esquema se salta al siguiente.
//
// Same reasoning as the table listing: one bad schema must not end the search.
func TestFindColumns_AnUnreadableColumnListMovesOn(t *testing.T) {
	// The column query fails, so the search finds nothing — and reports the table
	// as absent rather than crashing or hanging.
	conn := columnsConn([]string{"public", "sales"}, nil, errors.New("permission denied"))

	_, _, err := findColumns(context.Background(), conn, "users")
	if err == nil || !strings.Contains(err.Error(), "table not found") {
		t.Errorf("error = %v, want the table reported as not found", err)
	}
}

// Scenario: `dbx list columns` imprime el bloque de columnas con sus anotaciones.
//
// NULL marks a nullable column and DEFAULT prints the default expression; both are
// what a user needs to write an INSERT by hand.
func TestListColumnsAndReport_PrintsNullAndDefaultAnnotations(t *testing.T) {
	def := "now()"
	conn := columnsConn([]string{"public"}, [][]any{
		{"id", "integer", "NO", nil},
		{"email", "text", "YES", nil},
		{"created_at", "timestamp with time zone", "NO", def},
	}, nil)

	var out bytes.Buffer
	if err := listColumnsAndReport(&out, conn, "users", false, false); err != nil {
		t.Fatalf("listColumnsAndReport: %v", err)
	}

	got := out.String()
	if !strings.HasPrefix(got, "Table: public.users\n") {
		t.Errorf("output does not open with the table heading: %q", got)
	}
	if strings.Contains(got, "Columns:") {
		t.Errorf("the `dbx list columns` shape must not print the schema-style heading: %q", got)
	}
	if !strings.Contains(got, "  id") || !strings.Contains(got, "integer") {
		t.Errorf("output = %q, want the id column", got)
	}
	if !strings.Contains(got, "email") || !strings.Contains(got, "NULL") {
		t.Errorf("output = %q, want the nullable column marked NULL", got)
	}
	if !strings.Contains(got, "DEFAULT now()") {
		t.Errorf("output = %q, want the default printed", got)
	}
}

// Scenario: `dbx schema` y `dbx list columns` se diferencian solo en la cabecera.
//
// They describe the same thing and used to be two copies of the same loop. Now
// they share it, and the test says which is which, so a change to one header is a
// deliberate change rather than a copy-paste accident.
func TestListColumnsAndReport_TheSchemaFormAddsTheColumnsHeading(t *testing.T) {
	conn := func() *fakeConn {
		return columnsConn([]string{"public"}, [][]any{{"id", "integer", "NO", nil}}, nil)
	}

	var plain bytes.Buffer
	if err := listColumnsAndReport(&plain, conn(), "users", false, false); err != nil {
		t.Fatalf("listColumnsAndReport: %v", err)
	}
	var schemaForm bytes.Buffer
	if err := listColumnsAndReport(&schemaForm, conn(), "users", false, true); err != nil {
		t.Fatalf("listColumnsAndReport: %v", err)
	}

	if !strings.HasPrefix(schemaForm.String(), "Table: public.users\n\nColumns:\n") {
		t.Errorf("the schema form opens with %q, want the heading then a blank line then Columns:", schemaForm.String())
	}
	if strings.Contains(plain.String(), "Columns:") {
		t.Errorf("the plain form must not print the Columns heading: %q", plain.String())
	}
	// Below the heading the two are byte-identical, which is the point of sharing.
	if !strings.HasSuffix(schemaForm.String(), strings.TrimPrefix(plain.String(), "Table: public.users\n")) {
		t.Errorf("the two forms differ below the heading:\nplain:  %q\nschema: %q", plain.String(), schemaForm.String())
	}
}

// Scenario: Una columna sin default no imprime "DEFAULT".
//
// `DEFAULT <nil>` in the output would be a bug report in itself, so the default
// segment is conditional on there being a default.
func TestWriteColumns_OnlyPrintsTheAnnotationsThatApply(t *testing.T) {
	def := "'unknown'::character varying"
	conn := columnsConn([]string{"public"}, [][]any{
		{"id", "integer", "NO", nil},
		{"name", "text", "YES", def},
	}, nil)

	var out bytes.Buffer
	if err := listColumnsAndReport(&out, conn, "t", false, false); err != nil {
		t.Fatalf("listColumnsAndReport: %v", err)
	}
	got := out.String()

	if strings.Contains(got, "DEFAULT <nil>") || strings.Contains(got, "DEFAULT  ") {
		t.Errorf("output = %q, want no empty DEFAULT segment", got)
	}
	if !strings.Contains(got, "DEFAULT 'unknown'::character varying") {
		t.Errorf("output = %q, want the default value printed", got)
	}
	if !strings.Contains(got, "NULL") {
		t.Errorf("output = %q, want the nullable column marked", got)
	}
	// NOT NULL must not gain a "NULL" marker of its own.
	idLine := ""
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "id") && strings.Contains(line, "integer") {
			idLine = line
		}
	}
	if strings.Contains(idLine, "NULL") {
		t.Errorf("the id line is %q, want no NULL marker on a NOT NULL column", idLine)
	}
}

// Scenario: El modo JSON describe la tabla encontrada, con su esquema.
//
// The schema is included because a bare table name is ambiguous once the search
// has looked through several.
func TestListColumnsAndReport_JSONModeCarriesTheSchemaItFound(t *testing.T) {
	conn := columnsConn([]string{"sales"}, [][]any{{"id", "integer", "NO", nil}}, nil)

	var out bytes.Buffer
	if err := listColumnsAndReport(&out, conn, "orders", true, false); err != nil {
		t.Fatalf("listColumnsAndReport: %v", err)
	}

	var got struct {
		Schema  string                `json:"schema"`
		Table   string                `json:"table"`
		Columns []postgres.ColumnInfo `json:"columns"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, out.String())
	}
	if got.Schema != "sales" || got.Table != "orders" {
		t.Errorf("got %s.%s, want sales.orders", got.Schema, got.Table)
	}
	if len(got.Columns) != 1 || got.Columns[0].Name != "id" {
		t.Errorf("columns = %+v, want just the id column", got.Columns)
	}
}
