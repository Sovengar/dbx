package nl2sql

// Scenario: La valla de codigo que envuelve la respuesta de un modelo, y la linea que
// decides si es una etiqueta o parte de la sentencia.
//
// Every model that answers with SQL puts it in a fence, and the fence's first line is
// either a language tag or the statement itself:
//
//	```sql
//	SELECT 1
//
//	```SELECT 1
//	FROM t
//
// The two have IDENTICAL shape — a word, then a newline — so no amount of parsing the
// fence's syntax tells them apart. Only the content does.
//
// Which is why the reader used to eat statements. It asked "is the first word a tag",
// answered "yes" for DELETE and SELECT the same as for sql, and the caller obeyed. The
// measured consequence:
//
//	```DELETE\nFROM t    ->  "FROM t"
//	```SELECT\n1        ->  "1"
//
// The app then ran FROM t, reported success, and deleted nothing. The user asked for a
// delete and the app said it did one — and every check on the way passed, because a valid
// statement is what came back, just not the one that was asked for.
//
// The rule is now one predicate: the first line stays if it looks like a statement. So the
// cases below come in PAIRS — a tag that must be stripped and a verb that must be kept —
// because testing only one of each pair would pass on the old code for the tag and fail on
// it for the verb, and nobody would notice which side was which.

import (
	"strings"
	"testing"
)

// TestTheFirstLineOfAFenceIsStrippedOnlyWhenItIsATag is the pair matrix.
func TestTheFirstLineOfAFenceIsStrippedOnlyWhenItIsATag(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		// --- stripped: the first line is a tag, or empty ---
		{"a lowercase tag", "```sql\nSELECT 1\n```", "SELECT 1"},
		{"an uppercase tag", "```SQL\nSELECT 1\n```", "SELECT 1"},
		{"a vendor tag", "```postgresql\nSELECT 1\n```", "SELECT 1"},
		{"a procedural tag", "```plpgsql\nBEGIN; END;\n```", "BEGIN; END;"},
		// json is not a SQL language and its content is not SQL either. Stripping the tag
		// is what lets the SQL the model also emitted be read; this case predates the fix
		// and is the reason a rule based on "is it SQL-shaped" had to keep stripping
		// non-SQL tags rather than only recognising SQL ones.
		{"a json tag", "```json\nSELECT 1\n```", "SELECT 1"},
		{"a json tag in caps", "```JSON\nSELECT 1\n```", "SELECT 1"},
		{"an empty first line", "```\nSELECT 1\n```", "SELECT 1"},
		{"a mis-tagged fence", "```rust\nfn main\n```", "fn main"},
		{"a one-line fence that is only a tag", "```sql", ""},

		// --- kept: the first line is the statement ---
		//
		// THIS IS THE FIX. Each of these lost its first word before it, and each produced
		// a valid statement that was the wrong one.
		{"a SELECT", "```SELECT\n1\n```", "SELECT\n1"},
		{"a SELECT with a clause on the same line", "```SELECT 1\nFROM t\n```", "SELECT 1\nFROM t"},
		{"a DELETE", "```DELETE\nFROM t\n```", "DELETE\nFROM t"},
		{"an INSERT", "```INSERT INTO t\nVALUES (1)\n```", "INSERT INTO t\nVALUES (1)"},
		{"an UPDATE", "```UPDATE t SET x=1\nWHERE id=2\n```", "UPDATE t SET x=1\nWHERE id=2"},
		{"a TRUNCATE", "```TRUNCATE\nt\n```", "TRUNCATE\nt"},
		// The one that makes the rule necessary rather than merely safer: a CTE's first
		// line is "WITH ... AS (", which is both a statement start and, to a naive tag
		// reader, a word followed by a space. A reader that strips any first word loses
		// the WITH and runs a query with a dangling "(".
		{"a common table expression", "```WITH a AS (\nSELECT 1\n)\nSELECT * FROM a\n```",
			"WITH a AS (\nSELECT 1\n)\nSELECT * FROM a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := cleanSQL(tc.in); got != tc.want {
				t.Errorf("cleanSQL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestTheStatementThatComesOutIsTheStatementThatWentIn is the property, stated over the
// fences a model actually emits, because a per-case table proves the cases and not the
// property.
//
// The assertion is not "the text is right" — it is "the first WORD of the statement
// survives", because that is the part the bug ate, and because a statement that lost its
// first word is still a statement. It is what makes the bug survivable: `DELETE FROM t`
// became `FROM t`, which runs, which reports rows, and which nobody notices until the data
// is still there.
func TestTheStatementThatComesOutIsTheStatementThatWentIn(t *testing.T) {
	for _, stmt := range []string{
		"SELECT 1",
		"SELECT * FROM users WHERE id = 1",
		"DELETE FROM orders",
		"INSERT INTO orders (id, total) VALUES (1, 10)",
		"UPDATE orders SET total = 0",
		"TRUNCATE orders",
		"WITH recent AS (SELECT * FROM orders) SELECT * FROM recent",
		"DROP TABLE orders",
		"CREATE TABLE t (id int)",
	} {
		t.Run(stmt, func(t *testing.T) {
			first := strings.Fields(stmt)[0]

			// Every fence shape a model emits, plus the prose one: a sentence before the
			// fence, which is the most common shape of all.
			for _, tc := range []struct {
				shape  string
				render func() string
			}{
				{"a tag on its own line", func() string { return "```sql\n" + stmt + "\n```" }},
				{"no tag", func() string { return "```\n" + stmt + "\n```" }},
				{"the statement straight after the backticks", func() string { return "```" + stmt + "\n```" }},
				{"a tag on the same line", func() string { return "```sql " + stmt + " ```" }},
				{"prose before the fence", func() string { return "Here you go:\n```sql\n" + stmt + "\n```" }},
				{"prose after the fence", func() string { return "```sql\n" + stmt + "\n```\nThat is all." }},
				{"no fence at all", func() string { return stmt }},
			} {
				got := cleanSQL(tc.render())
				if !strings.HasPrefix(got, first) {
					t.Errorf("%s: cleanSQL gave %q, which does not start with %q — the first word of the statement was lost",
						tc.shape, got, first)
				}
				if !looksLikeSQL(got) {
					t.Errorf("%s: cleanSQL gave %q, which is not recognisable as a statement", tc.shape, got)
				}
			}
		})
	}
}

// TestAFenceThatCarriesNoStatementIsRefusedAtTheGate is the other half of the contract:
// the fix must not turn "drop the first line" into "always keep it", or a fence holding
// something that is not SQL would reach the database.
//
// The gate is looksLikeSQL, reached through requireSQL, because that is where a wrong
// answer becomes a wrong query.
func TestAFenceThatCarriesNoStatementIsRefusedAtTheGate(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"a rust program", "```rust\nfn main() {}\n```"},
		{"a json payload", "```json\n{\"rows\": 3}\n```"},
		{"prose in a fence", "```\nI cannot help with that.\n```"},
		{"an empty fence", "```\n```"},
		{"prose around an empty fence", "Here you go:\n```\n```\nHope that helps."},
		{"just the backticks", "```"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sql, err := requireSQL("test-provider", tc.body)
			if err == nil {
				t.Fatalf("a fence with no statement in it produced SQL %q with no error", sql)
			}
			if sql != "" {
				t.Errorf("the refusal still returned SQL %q", sql)
			}
			if !strings.Contains(err.Error(), "no SQL") {
				t.Errorf("the error is %q, want it to say there was no SQL", err)
			}
		})
	}
}
