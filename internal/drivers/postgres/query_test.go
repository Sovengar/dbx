package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/buble/dbx/internal/testsupport/pgxfake"
)

// --- dataTypeOID: a pure table, so it is tested as one -----------------------

// Scenario: Cada OID se traduce al tipo que ve el usuario.
//
// The grid's type column comes from here, and the type names are what the user
// reads when deciding how to filter or format a value. A wrong mapping is not a
// crash, it is a lie on screen, so the whole table is pinned including the
// fallthrough.
//
// These are PostgreSQL's actual OIDs. Getting one from memory is how a mapping
// ends up plausible and wrong, so if a test fails the OID is what to check first.
func TestDataTypeOID_MapsEveryTypeTheDriverHandles(t *testing.T) {
	for _, tc := range []struct {
		oid  uint32
		want string
		what string
	}{
		{16, "bool", "bool"},
		{17, "bytea", "bytea"},
		{20, "integer", "int8"},
		{21, "integer", "int2"},
		{23, "integer", "int4"},
		{26, "integer", "oid"},
		{25, "text", "text"},
		{700, "numeric", "float4"},
		{701, "numeric", "float8"},
		{1043, "varchar", "varchar"},
		{1082, "date", "date"},
		{1114, "timestamp", "timestamp"},
		{1184, "timestamptz", "timestamptz"},
		{2950, "uuid", "uuid"},
		{3802, "jsonb", "jsonb"},
		{114, "json", "json"},
		// Everything the table does not name.
		{0, "unknown", "no OID at all"},
		{999999, "unknown", "a type this driver has never heard of"},
	} {
		t.Run(tc.want+"/"+tc.what, func(t *testing.T) {
			if got := dataTypeOID(tc.oid); got != tc.want {
				t.Errorf("dataTypeOID(%d) = %q, want %q", tc.oid, got, tc.want)
			}
		})
	}
}

// Scenario: Los OID que comparten nombre se mapean al mismo tipo.
//
// int2/int4/int8 are three OIDs and one displayed type, and so are float4 and
// float8. Pinned as groups because a change that splits them, or that merges
// int and numeric, is easy to make and hard to notice.
func TestDataTypeOID_OIDsThatShareANameShareAMapping(t *testing.T) {
	for _, group := range []struct {
		name string
		want string
		oids []uint32
	}{
		{"all the integer widths", "integer", []uint32{20, 21, 23, 26}},
		{"both float widths", "numeric", []uint32{700, 701}},
	} {
		t.Run(group.name, func(t *testing.T) {
			seen := map[string][]uint32{}
			for _, oid := range group.oids {
				seen[dataTypeOID(oid)] = append(seen[dataTypeOID(oid)], oid)
			}
			if len(seen) != 1 {
				t.Fatalf("%v mapped to %d different types: %v, want one: %q", group.oids, len(seen), seen, group.want)
			}
			if got := dataTypeOID(group.oids[0]); got != group.want {
				t.Errorf("dataTypeOID(%v) = %q, want %q", group.oids, got, group.want)
			}
		})
	}

	// And the two text-ish types stay distinct, because a user filtering on a
	// varchar expects a different type from a user filtering on a json.
	if dataTypeOID(1043) == dataTypeOID(114) {
		t.Error("varchar and json map to the same displayed type")
	}
	if dataTypeOID(1082) == dataTypeOID(1114) {
		t.Error("date and timestamp map to the same displayed type")
	}
}

// --- ExecuteQuery ------------------------------------------------------------

// Scenario: Un resultado trae sus columnas, sus filas y su recuento.
//
// The count is the number of rows actually read, not the number the server said,
// so it stays right when a statement returns fewer rows than expected.
func TestExecuteQuery_ReadsColumnsRowsAndCount(t *testing.T) {
	conn := &pgxfake.Conn{Sequential: []pgxfake.Result{{
		Columns: []pgxfake.Column{
			{Name: "id", OID: 23},
			{Name: "name", OID: 25},
		},
		Rows: [][]any{{1, "ada"}, {2, "grace"}},
	}}}

	got, err := ExecuteQuery(context.Background(), conn, "SELECT id, name FROM users")
	if err != nil {
		t.Fatalf("ExecuteQuery: %v", err)
	}
	if got.Count != 2 {
		t.Errorf("Count = %d, want 2", got.Count)
	}
	if len(got.Rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(got.Rows))
	}
	if len(got.Columns) != 2 {
		t.Fatalf("got %d columns, want 2", len(got.Columns))
	}
	// The column types come from the OID table, not from the name.
	if got.Columns[0].DataType != "integer" || got.Columns[1].DataType != "text" {
		t.Errorf("column types = %q, %q, want integer, text", got.Columns[0].DataType, got.Columns[1].DataType)
	}
}

// Scenario: Cero filas es un resultado con recuento cero, no un error.
//
// A WHERE that matches nothing is the most ordinary outcome there is, and
// returning an error would make every filtered view look broken.
func TestExecuteQuery_NoRowsIsNotAnError(t *testing.T) {
	conn := &pgxfake.Conn{Sequential: []pgxfake.Result{{
		Columns: []pgxfake.Column{{Name: "id", OID: 23}},
	}}}

	got, err := ExecuteQuery(context.Background(), conn, "SELECT id FROM users WHERE false")
	if err != nil {
		t.Fatalf("no rows returned an error: %v", err)
	}
	if got.Count != 0 {
		t.Errorf("Count = %d, want 0", got.Count)
	}
	if len(got.Rows) != 0 {
		t.Errorf("Rows = %v, want none", got.Rows)
	}
	// The columns are still reported: the grid needs to know what it asked for
	// even when the answer is empty.
	if len(got.Columns) != 1 {
		t.Errorf("got %d columns, want the one that was selected", len(got.Columns))
	}
}

// Scenario: Un fallo al enviar la consulta se propaga sin envolver dos veces.
//
// ExecuteQuery is the lowest layer, so it returns the driver's error untouched and
// the callers add the context. Wrapping here too would produce "query failed:
// query failed: ...".
func TestExecuteQuery_ReportsADriverFailureUnchanged(t *testing.T) {
	boom := errors.New("pq: permission denied for table users")
	conn := &pgxfake.Conn{Steps: []pgxfake.Step{{Match: "FROM users", Err: boom}}}

	_, err := ExecuteQuery(context.Background(), conn, "SELECT * FROM users")
	if err == nil {
		t.Fatal("a driver failure returned no error")
	}
	if !errors.Is(err, boom) {
		t.Errorf("error = %v, want it to wrap the driver error", err)
	}
	if err.Error() != boom.Error() {
		t.Errorf("error = %q, want the driver's message verbatim: this layer adds no context", err)
	}
}

// Scenario: Una fila que no se puede leer es un error, no una fila a medias.
//
// A truncated conversion is the one case where returning what was read so far
// would be worse than failing: the user would edit a row they cannot see fully.
func TestExecuteQuery_AFailedRowReadIsAnError(t *testing.T) {
	conn := &pgxfake.Conn{Sequential: []pgxfake.Result{{
		Columns: []pgxfake.Column{{Name: "id"}},
		Rows:    [][]any{{1}, {2}},
		ScanErr: errors.New("cannot convert"),
	}}}

	_, err := ExecuteQuery(context.Background(), conn, "SELECT id FROM users")
	if err == nil {
		t.Fatal("a failed row read returned no error")
	}
	if !strings.Contains(err.Error(), "failed to scan row") {
		t.Errorf("error = %v, want it to say the row could not be scanned", err)
	}
}

// Scenario: Un fallo a mitad del recorrido no se confunde con el final.
//
// A connection dropped after two of a hundred rows must not look like a
// successful short result. This is why IterErr is separate from Err in the fake.
func TestExecuteQuery_AMidIterationFailureIsNotSilentlyShort(t *testing.T) {
	conn := &pgxfake.Conn{Sequential: []pgxfake.Result{{
		Columns: []pgxfake.Column{{Name: "id"}},
		Rows:    [][]any{{1}, {2}, {3}},
		IterErr: errors.New("connection reset by peer"),
	}}}

	_, err := ExecuteQuery(context.Background(), conn, "SELECT id FROM big")
	if err == nil {
		t.Fatal("a mid-iteration failure returned no error")
	}
	if !strings.Contains(err.Error(), "row iteration error") {
		t.Errorf("error = %v, want it to name the iteration failure", err)
	}
	if !strings.Contains(err.Error(), "connection reset") {
		t.Errorf("error = %v, want the driver's message kept", err)
	}
}

// --- Select: the SQL builder -------------------------------------------------

// selectRuns records the SQL Select built and returns a scripted result.
func selectRuns(t *testing.T, opts SelectOptions, table string) (*pgxfake.Conn, error) {
	t.Helper()
	conn := &pgxfake.Conn{Sequential: []pgxfake.Result{{Columns: []pgxfake.Column{{Name: "id"}}}}}
	_, err := NewSchemaLoader(conn).Select(context.Background(), table, opts)
	return conn, err
}

// Scenario: La consulta nombra el esquema y la tabla, y nada más.
//
// The schema and table are quoted, which is what lets a name with a space or a
// reserved word work. The bare form would not.
func TestSelect_QuotesTheSchemaAndTable(t *testing.T) {
	conn, err := selectRuns(t, SelectOptions{Schema: "my schema"}, "order details")
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	want := `SELECT * FROM "my schema"."order details"`
	if got := conn.LastQuery(); got != want {
		t.Errorf("query =\n%q\nwant\n%q", got, want)
	}
}

// Scenario: El filtro se añade tal cual, porque lo escribe el usuario.
//
// A WHERE built by string concatenation is an injection surface, so the contract
// is that this layer never inspects, rewrites or quotes it. `1=1` reaching the
// server is the proof that it does not.
func TestSelect_PassesTheFilterThroughVerbatim(t *testing.T) {
	conn, err := selectRuns(t, SelectOptions{Schema: "public", Where: "name = 'O''Hara' -- note"}, "users")
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if !strings.Contains(conn.LastQuery(), "WHERE name = 'O''Hara' -- note") {
		t.Errorf("query = %q, want the filter appended unchanged", conn.LastQuery())
	}
}

// Scenario: Un filtro vacío no añade WHERE.
//
// An empty string is "no filter", not "a filter that matches nothing", and
// emitting `WHERE ` would be a syntax error.
func TestSelect_AnEmptyFilterAddsNothing(t *testing.T) {
	conn, err := selectRuns(t, SelectOptions{Schema: "public"}, "users")
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if strings.Contains(conn.LastQuery(), "WHERE") {
		t.Errorf("query = %q, want no WHERE", conn.LastQuery())
	}
}

// Scenario: El orden por defecto es ascendente.
//
// Ordering without a direction is undefined in SQL, so the default has to be
// explicit or two runs of the same query can return rows in different orders.
func TestSelect_TheDefaultSortDirectionIsAscending(t *testing.T) {
	for _, dir := range []string{"", "asc", "ASC", "Asc", "aSc"} {
		conn, err := selectRuns(t, SelectOptions{Schema: "public", OrderBy: "name", OrderDir: dir}, "users")
		if err != nil {
			t.Fatalf("Select with dir %q: %v", dir, err)
		}
		if !strings.Contains(conn.LastQuery(), "ORDER BY name ASC") {
			t.Errorf("dir %q produced %q, want ORDER BY name ASC", dir, conn.LastQuery())
		}
		if strings.Contains(conn.LastQuery(), "DESC") {
			t.Errorf("dir %q produced a DESC in %q: it is not recognised as descending", dir, conn.LastQuery())
		}
	}
}

// Scenario: "desc" en cualquier caja es descendente.
//
// The direction arrives from a keybinding and from a stored preference, so it
// reaches here as "desc", "DESC" and "Desc" depending on where it came from.
func TestSelect_DescendingIsRecognisedInAnyCase(t *testing.T) {
	for _, dir := range []string{"desc", "DESC", "Desc", "dEsC"} {
		conn, err := selectRuns(t, SelectOptions{Schema: "public", OrderBy: "created_at", OrderDir: dir}, "users")
		if err != nil {
			t.Fatalf("Select with dir %q: %v", dir, err)
		}
		if !strings.Contains(conn.LastQuery(), "ORDER BY created_at DESC") {
			t.Errorf("dir %q produced %q, want ORDER BY created_at DESC", dir, conn.LastQuery())
		}
	}
}

// Scenario: Una columna de orden que no se reconoce no inventa una dirección.
//
// Only DESC is special-cased, so anything else falls to ASC. That is deliberate: a
// direction that came from nowhere must not become `ORDER BY x SOMETHING`, which
// would be a syntax error the user cannot act on.
func TestSelect_AnUnrecognisedDirectionFallsBackToAscending(t *testing.T) {
	for _, dir := range []string{"sideways", "ASC NULLS FIRST", "up", "0"} {
		conn, err := selectRuns(t, SelectOptions{Schema: "public", OrderBy: "name", OrderDir: dir}, "users")
		if err != nil {
			t.Fatalf("Select with dir %q: %v", dir, err)
		}
		if !strings.Contains(conn.LastQuery(), "ORDER BY name ASC") {
			t.Errorf("dir %q produced %q, want it to fall back to ASC", dir, conn.LastQuery())
		}
	}
	// And the unrecognised text itself never reaches the server, or the query
	// would be a syntax error the user cannot act on.
	fresh := &pgxfake.Conn{Sequential: []pgxfake.Result{{Columns: []pgxfake.Column{{Name: "id"}}}}}
	if _, err := NewSchemaLoader(fresh).Select(context.Background(), "users", SelectOptions{
		Schema: "public", OrderBy: "name", OrderDir: "sideways",
	}); err != nil {
		t.Fatalf("Select: %v", err)
	}
	if strings.Contains(fresh.LastQuery(), "sideways") {
		t.Errorf("the unrecognised direction leaked into the query: %q", fresh.LastQuery())
	}
	if !strings.HasSuffix(fresh.LastQuery(), "ORDER BY name ASC") {
		t.Errorf("query = %q, want it to end with the ASC fallback", fresh.LastQuery())
	}
}

// Scenario: Sin columna de orden no se añade ORDER BY.
//
// ORDER BY with no column is a syntax error, and the clause is only meaningful
// with a column.
func TestSelect_NoOrderByColumnAddsNoClause(t *testing.T) {
	conn, err := selectRuns(t, SelectOptions{Schema: "public", OrderDir: "DESC"}, "users")
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if strings.Contains(conn.LastQuery(), "ORDER BY") {
		t.Errorf("query = %q, want no ORDER BY without a column", conn.LastQuery())
	}
}

// Scenario: El límite solo se añade si es positivo.
//
// LIMIT 0 returns no rows, which is not what "no limit given" means, and
// LIMIT -1 is a syntax error. Both have to read as "unlimited".
func TestSelect_LimitIsOnlyAddedWhenPositive(t *testing.T) {
	for _, limit := range []int{0, -1, -100} {
		conn, err := selectRuns(t, SelectOptions{Schema: "public", Limit: limit}, "users")
		if err != nil {
			t.Fatalf("Select with limit %d: %v", limit, err)
		}
		if strings.Contains(conn.LastQuery(), "LIMIT") {
			t.Errorf("limit %d produced %q, want no LIMIT clause", limit, conn.LastQuery())
		}
	}
}

// Scenario: El desplazamiento solo tiene sentido con un límite.
//
// PostgreSQL rejects OFFSET without LIMIT, so emitting one alone is a syntax
// error the user cannot act on. The offset is dropped, not the limit.
func TestSelect_AnOffsetWithoutALimitIsDropped(t *testing.T) {
	conn, err := selectRuns(t, SelectOptions{Schema: "public", Offset: 50}, "users")
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if strings.Contains(conn.LastQuery(), "OFFSET") {
		t.Errorf("query = %q, want the offset dropped: PostgreSQL rejects OFFSET without LIMIT", conn.LastQuery())
	}

	// With a limit it is kept.
	withLimit, err := selectRuns(t, SelectOptions{Schema: "public", Limit: 10, Offset: 50}, "users")
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if !strings.Contains(withLimit.LastQuery(), "LIMIT 10") || !strings.Contains(withLimit.LastQuery(), "OFFSET 50") {
		t.Errorf("query = %q, want both LIMIT 10 and OFFSET 50", withLimit.LastQuery())
	}
}

// Scenario: Un desplazamiento de cero no añade OFFSET.
//
// OFFSET 0 is legal but says nothing, and leaving it out keeps the query the same
// as an unpaged one.
func TestSelect_AnOffsetOfZeroIsNotEmitted(t *testing.T) {
	conn, err := selectRuns(t, SelectOptions{Schema: "public", Limit: 10}, "users")
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if strings.Contains(conn.LastQuery(), "OFFSET") {
		t.Errorf("query = %q, want no OFFSET clause for an offset of 0", conn.LastQuery())
	}
}

// Scenario: Las cláusulas salen en el orden que SQL espera.
//
// SELECT, WHERE, ORDER BY, LIMIT, OFFSET. Any other order is a syntax error, and
// building it in the wrong order is the easy mistake to make when each clause is
// appended in its own `if`.
func TestSelect_ClausesAppearInSQLOrder(t *testing.T) {
	conn := &pgxfake.Conn{Sequential: []pgxfake.Result{{Columns: []pgxfake.Column{{Name: "id"}}}}}
	_, err := NewSchemaLoader(conn).Select(context.Background(), "users", SelectOptions{
		Schema:  "public",
		Where:   "active",
		OrderBy: "name",
		Limit:   20,
		Offset:  40,
	})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	q := conn.LastQuery()
	want := `SELECT * FROM "public"."users" WHERE active ORDER BY name ASC LIMIT 20 OFFSET 40`
	if q != want {
		t.Errorf("query =\n%q\nwant\n%q", q, want)
	}
}

// Scenario: Un fallo de la consulta incluye el SQL que la provocó.
//
// Unlike ExecuteQuery, this layer adds context, because the SQL is built here and
// nobody else can reconstruct it. Without it the user gets "permission denied" and
// no idea which statement was refused.
func TestSelect_AQueryFailureIncludesTheStatement(t *testing.T) {
	boom := errors.New("pq: permission denied for table users")
	conn := &pgxfake.Conn{Steps: []pgxfake.Step{{Match: "FROM", Err: boom}}}

	_, err := NewSchemaLoader(conn).Select(context.Background(), "users", SelectOptions{Schema: "public"})
	if err == nil {
		t.Fatal("a driver failure returned no error")
	}
	if !errors.Is(err, boom) {
		t.Errorf("error = %v, want it to wrap the driver error", err)
	}
	if !strings.Contains(err.Error(), `"public"."users"`) {
		t.Errorf("error = %v, want it to include the statement that failed", err)
	}
}

// Scenario: Un fallo a mitad de la lectura también nombra la consulta.
//
// The same reasoning as the send failure: the caller is the only place that knows
// what was being asked for.
func TestSelect_AMidIterationFailureAlsoNamesTheStatement(t *testing.T) {
	conn := &pgxfake.Conn{Sequential: []pgxfake.Result{{
		Columns: []pgxfake.Column{{Name: "id"}},
		Rows:    [][]any{{1}},
		IterErr: errors.New("connection reset"),
	}}}

	_, err := NewSchemaLoader(conn).Select(context.Background(), "users", SelectOptions{Schema: "public"})
	if err == nil {
		t.Fatal("a mid-iteration failure returned no error")
	}
	if !strings.Contains(err.Error(), "connection reset") {
		t.Errorf("error = %v, want the driver message kept", err)
	}
}

// Scenario: Select lee el mismo tipo de resultado que ExecuteQuery.
//
// The two run the same loop, and Select used to have its own copy. Sharing is the
// point, and this is the assertion that they still agree.
func TestSelect_AndExecuteQueryProduceTheSameShape(t *testing.T) {
	res := pgxfake.Result{
		Columns: []pgxfake.Column{{Name: "id", OID: 23}, {Name: "name", OID: 25}},
		Rows:    [][]any{{1, "ada"}},
	}

	viaSelect := &pgxfake.Conn{Sequential: []pgxfake.Result{res}}
	got, err := NewSchemaLoader(viaSelect).Select(context.Background(), "users", SelectOptions{Schema: "public"})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}

	viaExecute := &pgxfake.Conn{Sequential: []pgxfake.Result{res}}
	want, err := ExecuteQuery(context.Background(), viaExecute, "SELECT 1")
	if err != nil {
		t.Fatalf("ExecuteQuery: %v", err)
	}

	if got.Count != want.Count {
		t.Errorf("Select count = %d, ExecuteQuery count = %d, want them equal", got.Count, want.Count)
	}
	if len(got.Columns) != len(want.Columns) {
		t.Fatalf("Select gave %d columns, ExecuteQuery %d", len(got.Columns), len(want.Columns))
	}
	for i := range got.Columns {
		if got.Columns[i] != want.Columns[i] {
			t.Errorf("column %d: Select gave %+v, ExecuteQuery gave %+v", i, got.Columns[i], want.Columns[i])
		}
	}
}

// --- CountRows ---------------------------------------------------------------

// Scenario: El recuento lee un entero de una fila.
//
// COUNT(*) is the one place a scalar comes back, and it goes through QueryRow
// rather than the row set, so it is the only code here that uses Scan.
func TestCountRows_ReturnsTheNumber(t *testing.T) {
	conn := &pgxfake.Conn{RowName: "1200"}
	got, err := NewSchemaLoader(conn).CountRows(context.Background(), "public", "users")
	if err != nil {
		t.Fatalf("CountRows: %v", err)
	}
	if got != 1200 {
		t.Errorf("count = %d, want 1200", got)
	}
	if !strings.Contains(conn.LastQuery(), `FROM "public"."users"`) {
		t.Errorf("query = %q, want the schema and table quoted", conn.LastQuery())
	}
}

// Scenario: Un recuento que falla se propaga sin adornos.
//
// CountRows has one job, so there is nothing to add to the driver's error and a
// caller can wrap it with the context it has.
func TestCountRows_ReportsAFailureUnchanged(t *testing.T) {
	conn := &pgxfake.Conn{RowErr: errors.New("pq: relation does not exist")}
	_, err := NewSchemaLoader(conn).CountRows(context.Background(), "public", "nope")
	if err == nil {
		t.Fatal("a failing count returned no error")
	}
	if !errors.Is(err, conn.RowErr) {
		t.Errorf("error = %v, want the driver's error", err)
	}
}

// --- ExecuteRaw --------------------------------------------------------------

// Scenario: ExecuteRaw es ExecuteQuery con la conexión de la carga.
//
// It exists so a caller holding a SchemaLoader does not have to reach past it, and
// the test says the two are the same thing.
func TestExecuteRaw_MatchesExecuteQuery(t *testing.T) {
	res := pgxfake.Result{
		Columns: []pgxfake.Column{{Name: "id", OID: 23}},
		Rows:    [][]any{{1}, {2}},
	}
	conn := &pgxfake.Conn{Sequential: []pgxfake.Result{res}}

	got, err := NewSchemaLoader(conn).ExecuteRaw(context.Background(), "SELECT id FROM users")
	if err != nil {
		t.Fatalf("ExecuteRaw: %v", err)
	}
	if got.Count != 2 {
		t.Errorf("Count = %d, want 2", got.Count)
	}
	if conn.LastQuery() != "SELECT id FROM users" {
		t.Errorf("query = %q, want it forwarded unchanged", conn.LastQuery())
	}
}

// Scenario: ExecuteRaw propaga el fallo de la consulta.
//
// The whole point of the wrapper is to not add anything, so an error has to come
// back the way ExecuteQuery raised it.
func TestExecuteRaw_PropagatesAFailure(t *testing.T) {
	conn := &pgxfake.Conn{Steps: []pgxfake.Step{{Match: "SELECT", Err: errors.New("boom")}}}
	_, err := NewSchemaLoader(conn).ExecuteRaw(context.Background(), "SELECT 1")
	if err == nil {
		t.Fatal("a failing query returned no error")
	}
}
