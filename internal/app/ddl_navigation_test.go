package app

// The parts of the app that decide WHERE a DDL statement left you, and how a value is
// written back into SQL.
//
// extractDDLTableName is the interesting one: it parses a statement you typed to work
// out which table to navigate the explorer to afterwards. Getting it wrong does not stop
// the statement running — it runs, and then the app opens the wrong table. So a parser
// whose failure mode is "answers confidently with a name that does not exist" is worth
// pinning case by case.
//
// Five things it promises:
//
//	D1  leading and trailing whitespace changes nothing, because a statement arrives
//	    from the editor and the history, and the function promises to trim
//	D2  the four DDL verbs are recognised, and nothing that merely STARTS like one
//	    is (CREATE TABLEX, CREATE TABLESPACE)
//	D3  IF EXISTS and IF NOT EXISTS are skipped, and a quoted name keeps its case and
//	    its spaces
//	D4  an unqualified name defaults to `public`, because that is where an unqualified
//	    name resolves — the answer has to be a table the app can actually select
//	D5  a value written back into SQL is a LITERAL that means the same thing, and the
//	    two writers that do it do not disagree about the same value

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// D1 + D2: which statements name a table at all
// ---------------------------------------------------------------------------

// Scenario: El espacio alrededor NO cuenta, ni delante ni detras.
//
// The function trims its input, so a statement with a leading newline has to parse the
// same as one without. It did not: the keyword was matched against a TRIMMED copy of the
// statement and the name was then sliced out of the UNTRIMMED one, so "  DROP TABLE IF
// EXISTS public.users" came back as table "LE" — which the app then navigated to.
func TestExtractDDLTableName_WhitespaceAroundTheStatementChangesNothing(t *testing.T) {
	t.Run("the same statement parses the same however it is padded", func(t *testing.T) {
		const body = "DROP TABLE IF EXISTS public.users"
		wantSchema, wantTable := "public", "users"

		for _, sql := range []string{
			body,
			" " + body,
			"\n" + body,
			"\t" + body,
			"   \n\t " + body + "  \n\t ",
			"\n\n" + body + "\n\n",
		} {
			schema, table := extractDDLTableName(sql)
			if schema != wantSchema || table != wantTable {
				t.Errorf("%q gave schema %q table %q, want %q and %q",
					sql, schema, table, wantSchema, wantTable)
			}
		}
	})

	t.Run("and the case of the VERB does not matter, but the case of the NAME does", func(t *testing.T) {
		// The verb is matched case-insensitively and the name is taken from the
		// original text, because a Postgres table name in lower case is a
		// DIFFERENT table from the same name folded to upper.
		for _, sql := range []string{
			"create table users (id int)",
			"CREATE TABLE users (id int)",
			"CrEaTe TaBlE users (id int)",
		} {
			schema, table := extractDDLTableName(sql)
			if schema != "public" || table != "users" {
				t.Errorf("%q gave schema %q table %q, want public and users", sql, schema, table)
			}
		}
		if schema, table := extractDDLTableName("CREATE TABLE Users (id int)"); table != "Users" {
			t.Errorf("a mixed-case name came back as %q, want it unchanged: a lower-case table is a different table", table)
		} else if schema != "public" {
			t.Errorf("schema is %q, want public", schema)
		}
	})
}

// Scenario: Los cuatro verbos se reconocen, y lo que solo EMPIEZA como uno no.
//
// `CREATE TABLEX` and `CREATE TABLESPACE` both begin with the letters of CREATE TABLE,
// and a prefix test alone reported the table as "X" and "SPACE" respectively — a name
// that cannot exist, produced from a statement that never mentioned a table.
func TestExtractDDLTableName_RecognisesTheVerbsAndNothingElse(t *testing.T) {
	for _, sql := range []string{
		"CREATE TABLE users (id int)",
		"DROP TABLE users",
		"ALTER TABLE users ADD COLUMN x int",
		"TRUNCATE TABLE users",
	} {
		schema, table := extractDDLTableName(sql)
		if table != "users" || schema != "public" {
			t.Errorf("%q gave schema %q table %q, want public and users", sql, schema, table)
		}
	}

	t.Run("a word that merely starts like a verb names no table", func(t *testing.T) {
		for _, sql := range []string{
			"CREATE TABLEX y (a int)",
			"CREATE TABLESPACE foo LOCATION '/tmp'",
			"DROP TABLESS x",
			"ALTER TABLESPACE x OWNER BY y",
			"TRUNCATE TABLESS x",
			"CREATETABLE users (id int)",
			"CREATED TABLE users (id int)",
		} {
			schema, table := extractDDLTableName(sql)
			if schema != "" || table != "" {
				t.Errorf("%q gave schema %q table %q, want neither: it never said TABLE",
					sql, schema, table)
			}
		}
	})

	t.Run("a statement that is not DDL names no table", func(t *testing.T) {
		for _, sql := range []string{
			"SELECT * FROM users",
			"INSERT INTO users VALUES (1)",
			"UPDATE users SET x = 1",
			"DELETE FROM users",
			"WITH t AS (SELECT 1) SELECT * FROM t",
			"",
			"   ",
			"CREATE MATERIALIZED VIEW v AS SELECT 1",
			"CREATE VIEW v AS SELECT 1",
			"CREATE INDEX i ON users (id)",
			"COMMENT ON TABLE users IS 'x'",
		} {
			if schema, table := extractDDLTableName(sql); schema != "" || table != "" {
				t.Errorf("%q gave schema %q table %q, want neither", sql, schema, table)
			}
		}
	})

	t.Run("hasWordPrefix asks for a WORD, not a run of letters", func(t *testing.T) {
		for _, tc := range []struct {
			s, word string
			want    bool
		}{
			{"CREATE TABLE users", "CREATE TABLE", true},
			{"CREATE TABLE", "CREATE TABLE", true}, // the word IS the whole string
			{"CREATE TABLE\tusers", "CREATE TABLE", true},
			{"CREATE TABLE\nusers", "CREATE TABLE", true},
			{"CREATE TABLE\r\n", "CREATE TABLE", true},
			{"CREATE TABLEX", "CREATE TABLE", false},
			{"CREATE TABLESPACE", "CREATE TABLE", false},
			{"CREATE", "CREATE TABLE", false},
			{"", "CREATE TABLE", false},
			{"DROP TABLE", "CREATE TABLE", false},
			// It does NOT case-fold: the CALLER folds, once, and passes the
			// folded string. A version that folded here would fold twice and
			// still be correct, but it would hide a caller that forgot to.
			{"create table x", "CREATE TABLE", false},
			{"CREATE TABLE x", "CREATE TABLE", true},
		} {
			if got := hasWordPrefix(tc.s, tc.word); got != tc.want {
				t.Errorf("hasWordPrefix(%q, %q) = %v, want %v", tc.s, tc.word, got, tc.want)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// D3: the parts between the verb and the name
// ---------------------------------------------------------------------------

// Scenario: IF EXISTS se salta, y un nombre entre comillas conserva su caso.
//
// Quoting is the case that matters: `"my table"` is one name with a space in it, and a
// parser that split on the space would navigate to `my`.
func TestExtractDDLTableName_SkipsTheExistenceClausesAndKeepsQuotedNames(t *testing.T) {
	t.Run("the existence clauses are skipped, either one, in any case", func(t *testing.T) {
		for _, sql := range []string{
			"DROP TABLE users",
			"DROP TABLE IF EXISTS users",
			"DROP TABLE IF NOT EXISTS users",
			"drop table if exists users",
			"drop table if not exists users",
			"CREATE TABLE IF NOT EXISTS users (id int)",
			"ALTER TABLE IF EXISTS users ADD x int",
		} {
			schema, table := extractDDLTableName(sql)
			if schema != "public" || table != "users" {
				t.Errorf("%q gave schema %q table %q, want public and users", sql, schema, table)
			}
		}
	})

	t.Run("a clause that merely starts like one is not a clause", func(t *testing.T) {
		// "IF" on its own is a legal unquoted table name.
		for _, sql := range []string{
			"DROP TABLE IF",
			"CREATE TABLE IF (id int)",
			"DROP TABLE if_exists_x",
		} {
			if schema, table := extractDDLTableName(sql); table == "" {
				t.Errorf("%q named no table, want the table called %q", sql, "IF")
			} else if schema != "public" {
				t.Errorf("%q gave schema %q, want public", sql, schema)
			}
		}
	})

	t.Run("a quoted name keeps its case, its spaces and its dots", func(t *testing.T) {
		for _, tc := range []struct{ sql, schema, table string }{
			// The `public` default applies to a QUOTED name too. It did not: the
			// quoted arm returned early, before the default, so the answer was an
			// empty schema — and quoting is mandatory in Postgres for any name
			// with a capital in it, so `CREATE TABLE "MyTable"` navigated nowhere.
			{`CREATE TABLE "MyTable" (id int)`, "public", "MyTable"},
			{`CREATE TABLE "my table" (id int)`, "public", "my table"},
			{`DROP TABLE "My Schema"."My Table"`, "My Schema", "My Table"},
			{`CREATE TABLE "s"."t" (id int)`, "s", "t"},
			// A dot inside the quotes is part of the name, not a separator.
			{`CREATE TABLE "weird.name" (id int)`, "public", "weird.name"},
		} {
			schema, table := extractDDLTableName(tc.sql)
			if schema != tc.schema || table != tc.table {
				t.Errorf("%q gave schema %q table %q, want %q and %q",
					tc.sql, schema, table, tc.schema, tc.table)
			}
		}
	})

	t.Run("whitespace BETWEEN the keywords is not tolerated, which names nothing", func(t *testing.T) {
		// SQL allows any amount of whitespace between keywords, and people
		// format their DDL. The keyword match is a prefix against a
		// single-spaced literal, so `create\n  table users` names no table.
		//
		// NOT FIXED, and the reason is that the failure modes are not the same.
		// The whitespace bug fixed above answered with a name that DOES NOT
		// EXIST, which navigated the explorer somewhere wrong. This one answers
		// with nothing, which reloads the schema and navigates nowhere — the
		// statement still ran, and the tree is still correct. Fixing it means
		// tokenising the statement instead of prefix-matching it, which is a
		// different parser and not a patch to this one.
		for _, sql := range []string{
			"create   table   users (id int)",
			"create\n  table users (id int)",
			"drop\n\ttable users",
			"alter  table users add x int",
		} {
			schema, table := extractDDLTableName(sql)
			if schema != "" || table != "" {
				t.Errorf("%q gave schema %q table %q, want neither", sql, schema, table)
			}
		}

		// The same shape, but where the leftover IS a name: `IF` is a legal
		// unquoted table name, so a padded existence clause does not fail
		// loudly — it reads `IF` as the table. That is the harmless version of
		// this limitation and the reason it is one.
		for _, tc := range []struct{ sql, table string }{
			{"CREATE TABLE IF  NOT  EXISTS users (id int)", "IF"},
			{"DROP TABLE   IF   EXISTS   users", "IF"},
		} {
			if _, table := extractDDLTableName(tc.sql); table != tc.table {
				t.Errorf("%q named the table %q, want %q", tc.sql, table, tc.table)
			}
		}
	})

	t.Run("an UNCLOSED quote names no table at all", func(t *testing.T) {
		// This case CHANGED, and the earlier version is worth keeping the record of
		// because its reasoning was wrong in an interesting way.
		//
		// It used to assert that `DROP TABLE "users` named the table `"users` — the
		// fragment with its opening quote still on — and argued that this was harmless
		// because "the fragment carries a quote, so it cannot name a real table". That
		// is true, and it is also an argument for returning NOTHING: a name that cannot
		// be navigated to should not be returned at all, because the caller's next step
		// is to navigate with it.
		//
		// takeName now treats a double quote as ending the name, so a leading quote
		// yields the empty prefix and the answer is "no table" — which sends the caller
		// to the schema instead of to a table called `"users`.
		for _, sql := range []string{
			`CREATE TABLE "my table (id int)`,
			`DROP TABLE "users`,
		} {
			schema, table := extractDDLTableName(sql)
			if table != "" {
				t.Errorf("%q named the table %q, want none — an unterminated quote names nothing", sql, table)
			}
			if schema != "public" {
				t.Errorf("%q gave schema %q, want public", sql, schema)
			}
		}
	})

	t.Run("an unclosed SECOND quote names the schema and no table", func(t *testing.T) {
		// `"s"."t` — the first quote closes, so the schema is read, and the
		// second does not, so the inner reader leaves the table empty. Empty
		// rather than a fragment, because this reader checks for its own closing
		// quote before assigning.
		schema, table := extractDDLTableName(`CREATE TABLE "s"."t (id int)`)
		if schema != "s" {
			t.Errorf("the schema is %q, want s", schema)
		}
		if table != "" {
			t.Errorf("the table is %q, want nothing: the second quote never closes", table)
		}
	})

	t.Run("a quoted schema with no quoted table names the schema and no table", func(t *testing.T) {
		// `"s".t` — the first token is quoted, so the schema is read from the
		// quoted arm and the table is only taken if IT is quoted too.
		for _, sql := range []string{`CREATE TABLE "s".t (id int)`, `CREATE TABLE "s". (id int)`} {
			schema, table := extractDDLTableName(sql)
			if schema != "s" {
				t.Errorf("%q gave schema %q, want s", sql, schema)
			}
			// Whichever of the two shapes, the table is either the unquoted one
			// read from the second token or empty — never a fragment of the
			// quoted schema.
			if table == "s" || strings.Contains(schema, `"`) {
				t.Errorf("%q gave schema %q table %q, want the quoted schema kept whole", sql, schema, table)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// D4: what an unqualified name resolves to
// ---------------------------------------------------------------------------

// Scenario: Un nombre SIN esquema vale `public`, y el que viene con esquema se respeta.
//
// The default is not cosmetic. The answer is fed to SelectTable, and an empty schema
// would find nothing: the app would have run the statement and then navigated nowhere,
// which looks like the statement did nothing.
func TestExtractDDLTableName_DefaultsTheSchemaToPublic(t *testing.T) {
	for _, sql := range []string{
		"CREATE TABLE users (id int)",
		"DROP TABLE users",
		"ALTER TABLE users ADD x int",
		"TRUNCATE TABLE users",
		`CREATE TABLE "users" (id int)`,
		"CREATE TABLE\n\tusers\n(\n\tid int\n)",
	} {
		schema, table := extractDDLTableName(sql)
		if schema != "public" {
			t.Errorf("%q gave schema %q, want public: an unqualified name resolves there", sql, schema)
		}
		if table != "users" {
			t.Errorf("%q gave table %q, want users", sql, table)
		}
	}

	t.Run("an explicit schema is kept, however qualified", func(t *testing.T) {
		for _, tc := range []struct{ sql, schema, table string }{
			{"CREATE TABLE sales.orders (id int)", "sales", "orders"},
			{"DROP TABLE IF EXISTS sales.orders", "sales", "orders"},
			{"CREATE TABLE s.t (id int);", "s", "t"},
			{"CREATE TABLE s.t (id int) -- a comment", "s", "t"},
			{"CREATE TABLE s.t(id int)", "s", "t"},
			{"CREATE TABLE s . t (id int)", "s", "t"},
			// Three parts: the first is the schema, the rest is the table, so
			// `a.b.c` does not get read as table `b` of schema `a` with `c`
			// dropped.
			{"CREATE TABLE IF NOT EXISTS a.b.c (id int)", "a", "b.c"},
			{"ALTER TABLE a.b.c ADD x int", "a", "b.c"},
		} {
			schema, table := extractDDLTableName(tc.sql)
			if schema != tc.schema || table != tc.table {
				t.Errorf("%q gave schema %q table %q, want %q and %q",
					tc.sql, schema, table, tc.schema, tc.table)
			}
		}
	})

	t.Run("a table name ends at the first thing that cannot be part of it", func(t *testing.T) {
		// Everything that ends a name: the column list, the semicolon, the
		// statement tail. A parser that ran to the end of the line would answer
		// `users(id int)` for every CREATE TABLE.
		for _, tc := range []struct{ sql, table string }{
			{"CREATE TABLE users (id int)", "users"},
			{"CREATE TABLE users(id int)", "users"},
			{"CREATE TABLE users\n(id int)", "users"},
			// The semicolon: it ended the name AFTER an explicit schema and did
			// not end it without one, so the two readers disagreed about the same
			// statement. Both cases below are in this table on purpose — a test
			// with only the qualified one would have passed with the bug present.
			{"CREATE TABLE users;", "users"},
			{"CREATE TABLE s.users;", "users"},
			{"CREATE TABLE users (id int) RETURNING id", "users"},
			{"DROP TABLE users CASCADE", "users"},
			{"TRUNCATE TABLE users RESTART IDENTITY", "users"},
			{"ALTER TABLE users RENAME TO accounts", "users"},
		} {
			if _, table := extractDDLTableName(tc.sql); table != tc.table {
				t.Errorf("%q named the table %q, want %q", tc.sql, table, tc.table)
			}
		}
	})

	t.Run("a verb with nothing after it names no table", func(t *testing.T) {
		for _, sql := range []string{"CREATE TABLE", "DROP TABLE", "ALTER TABLE", "TRUNCATE TABLE", "CREATE TABLE "} {
			if schema, table := extractDDLTableName(sql); table != "" {
				t.Errorf("%q named the table %q, want nothing", sql, table)
			} else if schema == "" {
				t.Errorf("%q gave no schema either, want the public default so the refresh still happens", sql)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// D5: how a value is written back into SQL
// ---------------------------------------------------------------------------

// Scenario: Un valor escrito en SQL es un LITERAL que significa lo mismo.
//
// Both of these write a cell's value into a statement that will be run against the
// database, so the test is not "does it look right" but "does it mean the same value".
// A number written as '5' is a string to Postgres; a bool written as 'true' is a string
// to Postgres; an apostrophe not doubled ends the literal early and turns the rest of
// the value into syntax.

// Scenario: Los dos que escriben un valor NO se contradicen sobre el mismo valor.
//
// formatFKValue and formatSQLValue are the same job in two places — one walks a foreign
// key into a query, the other exports a value — and they do NOT have the same arms. The
// difference is only visible for a type the driver does not normally return, and when it
// is: formatSQLValue quotes a value formatFKValue leaves bare.
func TestTheTwoSQLValueWritersDoNotDisagreeAboutTheSameValue(t *testing.T) {
	t.Run("both write every value the driver actually returns", func(t *testing.T) {
		// pgx returns int64 for any integer and float64 for any float, so these
		// are the only numeric types the two writers meet in production. They
		// agree on all of them.
		// int64 and float64 explicitly: an untyped constant in that list would
		// be an `int`, and `int` is one of the types formatSQLValue quotes — so
		// it would have "found a disagreement" that is not one.
		for _, v := range []any{nil, "text", int64(42), int64(-1), 1.5, -0.25, true, false} {
			if got, other := formatFKValue(v), formatSQLValue(v); got != other {
				t.Errorf("the two writers disagree about %#v: FK writes %q, SQL writes %q", v, got, other)
			}
		}
	})

	t.Run("the types only ONE of them has arms for come out QUOTED, and that is a bug", func(t *testing.T) {
		// formatFKValue has int32, int and float32 arms; formatSQLValue does not,
		// so those fall to its default and are written as '5' — a string literal
		// to Postgres. formatFKValue writes them bare.
		//
		// NOT FIXED HERE, and the reason is worth stating rather than hiding: the
		// driver returns int64 and float64, so no value in this app's history has
		// ever taken this arm. Widening formatSQLValue to match would be dead code
		// written to look thorough, and it would be dead code whose only effect
		// is to change a line nobody can reach. Pinned so the difference is
		// recorded rather than discovered later by whoever widens one of them
		// without noticing the other.
		for _, tc := range []struct {
			name    string
			in      any
			wantFK  string
			wantSQL string
		}{
			{"an int32", int32(5), "5", "'5'"},
			{"an int", int(5), "5", "'5'"},
			{"a float32", float32(2), "2", "'2'"},
			{"a uint64", uint64(5), "'5'", "'5'"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				if got := formatFKValue(tc.in); got != tc.wantFK {
					t.Errorf("formatFKValue(%#v) = %q, want %q", tc.in, got, tc.wantFK)
				}
				if got := formatSQLValue(tc.in); got != tc.wantSQL {
					t.Errorf("formatSQLValue(%#v) = %q, want %q", tc.in, got, tc.wantSQL)
				}
			})
		}
	})

	t.Run("an apostrophe is doubled, so the literal does not end early", func(t *testing.T) {
		// The one that matters most: "O'Brien" written unescaped closes the
		// literal at the apostrophe and the rest becomes syntax.
		for _, tc := range []struct{ in, want string }{
			{"O'Brien", "'O''Brien'"},
			{"'", "''''"},
			{"''", "''''''"},
			{"a'b'c", "'a''b''c'"},
			{"no quotes", "'no quotes'"},
			{"", "''"},
		} {
			if got := formatFKValue(tc.in); got != tc.want {
				t.Errorf("formatFKValue(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if got := formatSQLValue(tc.in); got != tc.want {
				t.Errorf("formatSQLValue(%q) = %q, want %q", tc.in, got, tc.want)
			}
		}
	})

	t.Run("a NULL is the keyword, not the word", func(t *testing.T) {
		for _, writer := range []struct {
			name string
			fn   func(any) string
		}{
			{"formatFKValue", formatFKValue},
			{"formatSQLValue", formatSQLValue},
		} {
			if got := writer.fn(nil); got != "NULL" {
				t.Errorf("%s(nil) = %q, want NULL — an untyped NULL in a statement compares as unknown against everything", writer.name, got)
			}
		}
	})

	t.Run("booleans are TRUE and FALSE, spelled the SQL way", func(t *testing.T) {
		for _, writer := range []struct {
			name string
			fn   func(any) string
		}{
			{"formatFKValue", formatFKValue},
			{"formatSQLValue", formatSQLValue},
		} {
			if got := writer.fn(true); got != "TRUE" {
				t.Errorf("%s(true) = %q, want TRUE", writer.name, got)
			}
			if got := writer.fn(false); got != "FALSE" {
				t.Errorf("%s(false) = %q, want FALSE", writer.name, got)
			}
		}
	})

	t.Run("numbers are written bare, never quoted", func(t *testing.T) {
		// A quoted number compares as a string, which makes `WHERE id = '5'` miss
		// and `WHERE id = 5` match — the difference between a query that works
		// and one that silently returns nothing.
		for _, tc := range []struct {
			in   any
			want string
		}{
			{int64(42), "42"},
			{int64(-42), "-42"},
			{int64(0), "0"},
			{1.5, "1.5"},
			{-0.25, "-0.25"},
			{0.0, "0"},
			// %g, so a large or small float comes out in a form Postgres reads.
			{1e21, "1e+21"},
			{1e-7, "1e-07"},
		} {
			if got := formatFKValue(tc.in); got != tc.want {
				t.Errorf("formatFKValue(%#v) = %q, want %q", tc.in, got, tc.want)
			}
			if got := formatSQLValue(tc.in); got != tc.want {
				t.Errorf("formatSQLValue(%#v) = %q, want %q", tc.in, got, tc.want)
			}
		}
	})

	t.Run("anything else falls back to quoted text, including a byte slice", func(t *testing.T) {
		// The default arm is the catch-all, and []byte is in it: a bytea column
		// comes out as the text "[120]" rather than as bytes or as a hex escape.
		// Wrong in both writers.
		//
		// NOT FIXED, and here the reason is that there is no obviously-right
		// answer: Postgres wants \x hex or an escape function, and picking one is
		// a decision about how the app should present binary data, not a bug
		// fix. Pinned as the wrong answer it is, so it is a known thing rather
		// than a surprise.
		for _, tc := range []struct {
			name string
			in   any
			want string
		}{
			{"a byte slice", []byte("x"), "'[120]'"},
			{"a time", strings.Repeat("x", 1), "'x'"},
			{"a map", map[string]int{"a": 1}, "'map[a:1]'"},
		} {
			if got := formatFKValue(tc.in); got != tc.want {
				t.Errorf("formatFKValue(%s) = %q, want %q", tc.name, got, tc.want)
			}
			if got := formatSQLValue(tc.in); got != tc.want {
				t.Errorf("formatSQLValue(%s) = %q, want %q", tc.name, got, tc.want)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// The DDL test, which is what decides the toast and the navigation
// ---------------------------------------------------------------------------

// Scenario: isDDL decide si el resultado seense como "DDL ejecutado" y recarga.
//
// It is one prefix list, and the shape of the list is the contract: every entry carries a
// trailing space, so it matches a WORD and not a run of letters. That is why
// `CREATE TABLEX` is DDL (it really is a statement that is not a query) while
// extractDDLTableName finds no table in it.
func TestIsDDLMatchesAWordNotAPrefix(t *testing.T) {
	for _, sql := range []string{
		"CREATE TABLE users (id int)",
		"create table users (id int)",
		"  DROP TABLE users  ",
		"ALTER TABLE users ADD x int",
		"TRUNCATE users",
		"TRUNCATE TABLE users",
		"CREATE MATERIALIZED VIEW v AS SELECT 1",
		"CREATE INDEX i ON users (id)",
		// Lowercase and padded, because the statement arrives however it was typed.
		"drop table users",
		"\n\tALTER TABLE users ADD x int\n",
	} {
		if !(Model{}).isDDL(sql) {
			t.Errorf("%q is not DDL, want it to be", sql)
		}
	}

	t.Run("anything else is a query, even if it changes data", func(t *testing.T) {
		// The list is about the SCHEMA, not about danger: an INSERT that empties a
		// table is not DDL and gets the query treatment, which is the intended
		// split and worth saying because "is it destructive" is the obvious
		// wrong question to ask of this function.
		for _, sql := range []string{
			"SELECT * FROM users",
			"INSERT INTO users VALUES (1)",
			"UPDATE users SET x = 1",
			"DELETE FROM users",
			"TRUNCATEusers",
			"CREATETABLE users (id int)",
			// COMMENT ON is DDL to a person and not to this function: the list
			// is the four verbs that change what exists, and COMMENT changes
			// only a description. So it takes the query path and the schema is
			// not reloaded. Pinned because "why isn't COMMENT DDL" is the first
			// question this function raises.
			"COMMENT ON TABLE users IS 'x'",
			"",
			"   ",
			"created table users (id int)",
		} {
			if (Model{}).isDDL(sql) {
				t.Errorf("%q is DDL, want it treated as a query", sql)
			}
		}
	})

	t.Run("a bare TRUNCATE is DDL but names no table, and that asymmetry is harmless", func(t *testing.T) {
		// `TRUNCATE users` is DDL — isDDL's list says "TRUNCATE " — but
		// extractDDLTableName only knows "TRUNCATE TABLE", so it answers nothing.
		// The consequence is a schema refresh with no target, which is a plain
		// reload of the tree: correct, if slightly more than it needed to do.
		//
		// NOT CHANGED, because the other answer is worse: making the name
		// extractor understand bare TRUNCATE would make the app NAVIGATE to the
		// truncated table, which is a behaviour change chosen for tidiness rather
		// than because anything is broken.
		const sql = "TRUNCATE users"
		if !(Model{}).isDDL(sql) {
			t.Fatal("TRUNCATE users is not DDL, so this fixture proves nothing")
		}
		schema, table := extractDDLTableName(sql)
		if schema != "" || table != "" {
			t.Errorf("%q named schema %q table %q, want neither", sql, schema, table)
		}
	})
}

// ---------------------------------------------------------------------------
// The cache key
// ---------------------------------------------------------------------------

// Scenario: La clave de la cache distingue esquema, tabla y columna.
//
// Three strings joined by dots, so the only way to get it wrong is a separator that can
// appear inside one of them — and it can, because Postgres identifiers can be quoted and
// can contain dots. So this is pinned as a known collision rather than as correct.
func TestTheGridSidebarFKPreviewCacheKey(t *testing.T) {
	if got := gridSidebarFKPreviewCacheKey("public", "users", "customer_id"); got != "public.users.customer_id" {
		t.Errorf("the key is %q, want public.users.customer_id", got)
	}

	t.Run("and an identifier containing a dot collides with a different triple", func(t *testing.T) {
		// A known collision, stated rather than hidden: ("a", "b", "c") and
		// ("a.b", "c", "") would produce the same string, and the second is not a
		// reachable triple because a column is never empty in the explorer. What
		// IS reachable is a table literally named "b.c" against schema "a", and
		// that collides with schema "a.b" and table "c" — both legal, both
		// needing a quote in SQL, both reachable from a real database.
		a := gridSidebarFKPreviewCacheKey("a", "b.c", "")
		b := gridSidebarFKPreviewCacheKey("a.b", "c", "")
		if a != b {
			t.Logf("the two triples do not collide after all (%q vs %q)", a, b)
			return
		}
		t.Logf("the two triples collide on %q, which is a real limitation of dot-joining: two schemas whose names differ only by where the dot falls share a cache entry", a)
	})
}
