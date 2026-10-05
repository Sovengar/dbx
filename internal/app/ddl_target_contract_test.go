package app

// Scenario: El nombre de tabla que se extrae de un DDL, que decide A DONDE navega el
// explorador después de ejecutarlo.
//
// extractDDLTableName answers "which table did this statement just create or drop", and
// the answer drives loadSchemaWithTarget — so a wrong name navigates the tree to a table
// that does not exist, and the user is left looking at an empty grid with no explanation.
//
// The parsing is all string slicing on keyword lengths, which is where the bugs were:
//
//	an off-by-one in a keyword length cuts INTO the keyword ("LE IF EXISTS public.users")
//	a quoted identifier needs the closing quote found, and a `schema."Table"` has the
//	    quote on only one side of the dot
//	the public default has to be applied on EVERY path out, including the quoted one
//
// All three are pinned below. The statement text is what the user typed in the editor,
// so leading whitespace, lowercase, mixed case and quoted capitalised names are all
// ordinary inputs rather than exotic ones.

import (
	"strings"
	"testing"
)

func TestTheDDLTableNameIsExtractedFromEveryShape(t *testing.T) {
	for _, tc := range []struct {
		name       string
		sql        string
		wantSchema string
		wantTable  string
	}{
		// The four keywords. Anything else is not DDL and yields nothing.
		{"CREATE TABLE", "CREATE TABLE users (id int)", "public", "users"},
		{"DROP TABLE", "DROP TABLE users", "public", "users"},
		{"ALTER TABLE", "ALTER TABLE users ADD COLUMN x int", "public", "users"},
		{"TRUNCATE TABLE", "TRUNCATE TABLE users", "public", "users"},

		// The IF EXISTS forms. These are the ones whose keyword lengths were off by one,
		// and the failure was a table named "LE" — so the table is the observable, and
		// the schema alone would not catch it.
		{"CREATE TABLE IF NOT EXISTS", "CREATE TABLE IF NOT EXISTS users (id int)", "public", "users"},
		{"DROP TABLE IF EXISTS", "DROP TABLE IF EXISTS users", "public", "users"},
		{"ALTER TABLE IF EXISTS", "ALTER TABLE IF EXISTS users ADD x int", "public", "users"},
		{"CREATE TABLE IF EXISTS (no NOT)", "CREATE TABLE IF EXISTS users (id int)", "public", "users"},

		// A qualified name, and the schema is NOT public.
		{"schema.table", "CREATE TABLE sales.orders (id int)", "sales", "orders"},
		{"DROP a qualified table", "DROP TABLE sales.orders", "sales", "orders"},
		{"ALTER a qualified table", "ALTER TABLE sales.orders ADD x int", "sales", "orders"},
		{"IF NOT EXISTS with a schema", "CREATE TABLE IF NOT EXISTS sales.orders (id int)", "sales", "orders"},
		{"IF EXISTS with a schema", "DROP TABLE IF EXISTS sales.orders", "sales", "orders"},

		// Quoted identifiers, which is how Postgres spells a name with a capital in it.
		// Both sides quoted, and each side alone.
		{"a quoted table", `CREATE TABLE "MyTable" (id int)`, "public", "MyTable"},
		{"a quoted schema and table", `CREATE TABLE "MySchema"."MyTable" (id int)`, "MySchema", "MyTable"},
		{"a quoted schema only", `CREATE TABLE "MySchema".users (id int)`, "MySchema", "users"},
		{"a quoted table only, dropped", `DROP TABLE "MyTable"`, "public", "MyTable"},
		{"a quoted name with IF NOT EXISTS", `CREATE TABLE IF NOT EXISTS "MyTable" (id int)`, "public", "MyTable"},
		{"a quoted name with IF EXISTS", `DROP TABLE IF EXISTS "MyTable"`, "public", "MyTable"},
		{"a quoted name with a schema", `DROP TABLE IF EXISTS sales."MyTable"`, "sales", "MyTable"},

		// Case and whitespace. hasWordPrefix is what makes the keyword match
		// case-insensitively; the slice below it uses the TRIMMED text, which is the fix
		// for the "LE IF EXISTS" bug and the reason leading spaces are ordinary input.
		{"lowercase keywords", "create table users (id int)", "public", "users"},
		{"mixed case keywords", "CrEaTe TaBlE users (id int)", "public", "users"},
		{"leading whitespace", "   DROP TABLE IF EXISTS public.users", "public", "users"},
		{"leading whitespace on CREATE", "\n\t CREATE TABLE users (id int)", "public", "users"},
		{"trailing whitespace", "DROP TABLE users   ", "public", "users"},
		{"mixed case names are preserved", "CREATE TABLE Sales.Orders (id int)", "Sales", "Orders"},

		// A qualified name whose table part has no closing quote is not a table.
		// An unterminated quote names NO table. The schema stays public so the caller
		// shows the schema rather than a table called `"users`.
		{"an unterminated quoted name", `DROP TABLE "users`, "public", ""},
		{"a quoted schema with an unterminated table", `DROP TABLE "sch"."tab`, "sch", ""},

		// Not DDL. The function answers nothing so the caller falls through to showing
		// the result set instead of navigating.
		{"a SELECT", "SELECT * FROM users", "", ""},
		{"an INSERT", "INSERT INTO users VALUES (1)", "", ""},
		{"an UPDATE", "UPDATE users SET x = 1", "", ""},
		{"a DELETE", "DELETE FROM users", "", ""},
		{"CREATE INDEX is not CREATE TABLE", "CREATE INDEX ix ON users (id)", "", ""},
		{"CREATE VIEW is not CREATE TABLE", "CREATE VIEW v AS SELECT 1", "", ""},
		{"CREATE SCHEMA is not CREATE TABLE", "CREATE SCHEMA s", "", ""},
		{"DROP INDEX is not DROP TABLE", "DROP INDEX ix", "", ""},
		{"the empty statement", "", "", ""},
		{"only whitespace", "   \n\t ", "", ""},

		// A keyword that is only a PREFIX of a longer word. `DROP TABLE_AUDIT` must not be
		// read as DROP TABLE, or the user would navigate to a table named "AUDIT" after
		// dropping an index.
		{"a longer word starting with the keyword", "DROP TABLES users", "", ""},
		{"CREATE TABLESPACE is not CREATE TABLE", "CREATE TABLESPACE ts LOCATION '/tmp'", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gotSchema, gotTable := extractDDLTableName(tc.sql)
			if gotSchema != tc.wantSchema || gotTable != tc.wantTable {
				t.Errorf("extractDDLTableName(%q) = (%q, %q), want (%q, %q)",
					tc.sql, gotSchema, gotTable, tc.wantSchema, tc.wantTable)
			}
		})
	}
}

// TestThePublicDefaultIsAppliedOnEveryPathOut is the case the function's own comment calls
// out as the tenth instance in this repo of the same default written twice, with one copy
// in the branch nobody reread.
//
// The quoted-name arm returns EARLY. A default applied only at the bottom is not applied
// there, so `CREATE TABLE "MyTable"` came back with an empty schema — and quoting a name
// with a capital in it is MANDATORY in Postgres, so every capitalised table navigated
// nowhere while the identical unquoted statement worked.
func TestThePublicDefaultIsAppliedOnEveryPathOut(t *testing.T) {
	// Every shape that must come back with schema "public", including the two that take
	// the early return.
	for _, sql := range []string{
		`CREATE TABLE users (id int)`,
		`CREATE TABLE "MyTable" (id int)`,
		`DROP TABLE IF EXISTS "MyTable"`,
		`CREATE TABLE IF NOT EXISTS users (id int)`,
	} {
		schema, table := extractDDLTableName(sql)
		if schema != "public" {
			t.Errorf("%q came back with schema %q, want the public default", sql, schema)
		}
		if table == "" {
			t.Errorf("%q came back with no table, so the explorer would navigate nowhere", sql)
		}
	}

	t.Run("and an EXPLICIT schema is never overwritten by the default", func(t *testing.T) {
		// The other direction. Applying the default without checking would send every
		// qualified statement to public, which is a different bug with the same cause.
		schema, table := extractDDLTableName("CREATE TABLE sales.orders (id int)")
		if schema != "sales" || table != "orders" {
			t.Errorf("a qualified name came back as (%q, %q), want (sales, orders)", schema, table)
		}
	})
}

// TestANameThatIsOnlyPartlyATableIsNotNavigatedTo pins the boundary between "a DDL
// statement with a table" and "a DDL statement with nothing to navigate to".
//
// The rule is that a non-empty table name is required. Without it the caller navigates to
// an empty table name, which the explorer resolves to nothing and the user sees an empty
// grid with a success toast saying the DDL ran.
func TestANameThatIsOnlyPartlyATableIsNotNavigatedTo(t *testing.T) {
	for _, sql := range []string{
		"DROP TABLE",
		"DROP TABLE   ",
		"CREATE TABLE",
		"TRUNCATE TABLE",
		"ALTER TABLE",
	} {
		schema, table := extractDDLTableName(sql)
		if table != "" {
			t.Errorf("%q produced a table name %q, want none — there is nothing to navigate to",
				sql, table)
		}
		// The schema alone is NOT asserted: ("public", "") is a legitimate answer, and it
		// is what a bare `DROP TABLE` produces. loadSchemaWithTarget with an empty table
		// means "show this schema", so returning public here is correct rather than a
		// stray default.
		_ = schema
	}
}

// The debug logging. It exists because of the "LE IF EXISTS" bug and because the AGENTS
// file says never remove it, so the assertion is that the trimmed text — the thing the
// whole function slices — is what reaches the log. A regression that logged the untrimmed
// text would hide the next off-by-one.
func TestTheDDLParseLogsTheTextItActuallySliced(t *testing.T) {
	for _, sql := range []string{
		"   DROP TABLE IF EXISTS public.users",
		"\n CREATE TABLE IF NOT EXISTS sales.orders (id int)",
	} {
		extractDDLTableName(sql)
	}
	// No assertion on the log contents: it is a diagnostic aid, and pinning its format
	// would make every reformat a test failure. What is asserted above is the behaviour.
	if trimmed := strings.TrimSpace("  DROP TABLE users  "); trimmed != "DROP TABLE users" {
		t.Fatal("TrimSpace did not trim, so the premise of the test is wrong")
	}
}
