package editor

// Scenario: El detector de contexto, que decide QUE se sugiere a partir de DONDE esta el
// cursor.
//
// detectContext is a switch over the SQL clause the cursor sits in, and each arm decides
// a different KIND of suggestion: a column, a table, a keyword, a value, or nothing at
// all. Most of the arms were untested, which means most of the ways for the popup to open
// offering the wrong thing — a table name where a column belongs is not a cosmetic bug,
// it is a statement the user then finishes wrong.
//
// Three inputs decide the answer and all three have to line up:
//
//	the TEXT before the cursor, tokenised
//	the clause that token stream ends in
//	what is REFERENCED earlier in the statement (the FROM list)
//
// So every case here is a full statement with the cursor placed, and the assertion is the
// kind. The prefix matters too: the same clause with a prefix filters, without one offers
// everything, and those are different code paths.
//
// The other half is the statement boundary. A `;` resets everything: after a semicolon the
// FROM list of the statement that just ended must not constrain the suggestions for the
// statement that is starting. That is a slice-truncation line and it is easy to get
// backwards.

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/buble/dbx/internal/theme"
)

// kindName renders a CompletionKind for a failure message. KindLabel is not it: it is the
// label the POPUP shows, which is empty for two of the kinds and the column's data type for
// a third, so it cannot identify a kind in a test failure.
func kindName(k CompletionKind) string {
	names := map[CompletionKind]string{
		CompletionKeyword:    "keyword",
		CompletionFunction:   "function",
		CompletionSchema:     "schema",
		CompletionTable:      "table",
		CompletionColumn:     "column",
		CompletionOperator:   "operator",
		CompletionValue:      "value",
		CompletionEmpty:      "empty",
		CompletionSelectList: "select-list",
	}
	if n, ok := names[k]; ok {
		return n
	}
	return "unknown"
}

// at places the cursor at the `|` marker in the SQL and asks what would be suggested. The
// marker is removed afterwards, so a case reads as the statement with the cursor visible
// rather than as a byte offset.
func at(t *testing.T, sql string) completionContext {
	t.Helper()
	idx := strings.Index(sql, "|")
	if idx < 0 {
		t.Fatalf("the case has no | cursor marker: %q", sql)
	}
	plain := sql[:idx] + sql[idx+1:]
	return newTestAutocomplete().detectContext(plain, idx)
}

func TestTheClauseTheCursorIsInDecidesWhatIsSuggested(t *testing.T) {
	// Every arm of the clause switch, with the cursor where a user would actually leave
	// it. The `want` is the KIND, and it is the whole point: a popup that opens with the
	// wrong kind suggests identifiers that do not belong in that position.
	for _, tc := range []struct {
		name string
		sql  string
		want CompletionKind
	}{
		// FROM and the JOIN family want a TABLE, because what follows them is a table
		// name. expectingTable(tail) decides it, and it goes false once something
		// non-comma has already been typed — the alias case below is that flip, and it
		// is the only reason this arm is interesting.
		{"FROM wants a table", "SELECT * FROM |", CompletionTable},
		{"FROM after a comma still wants a table", "SELECT * FROM a, |", CompletionTable},
		{"JOIN wants a table", "SELECT * FROM a JOIN |", CompletionTable},
		{"INNER JOIN wants a table", "SELECT * FROM a INNER JOIN |", CompletionTable},
		{"LEFT JOIN wants a table", "SELECT * FROM a LEFT JOIN |", CompletionTable},
		{"RIGHT JOIN wants a table", "SELECT * FROM a RIGHT JOIN |", CompletionTable},
		{"FULL JOIN wants a table", "SELECT * FROM a FULL JOIN |", CompletionTable},
		{"CROSS JOIN wants a table", "SELECT * FROM a CROSS JOIN |", CompletionTable},
		{"LEFT OUTER JOIN wants a table", "SELECT * FROM a LEFT OUTER JOIN |", CompletionTable},
		{"RIGHT OUTER JOIN wants a table", "SELECT * FROM a RIGHT OUTER JOIN |", CompletionTable},
		{"FULL OUTER JOIN wants a table", "SELECT * FROM a FULL OUTER JOIN |", CompletionTable},
		{"INTO wants a table", "INSERT INTO |", CompletionTable},
		{"UPDATE wants a table", "UPDATE |", CompletionTable},
		{"CREATE TABLE wants a table", "CREATE TABLE |", CompletionTable},
		{"DROP TABLE wants a table", "DROP TABLE |", CompletionTable},
		{"ALTER TABLE wants a table", "ALTER TABLE |", CompletionTable},
		{"TRUNCATE TABLE wants a table", "TRUNCATE TABLE |", CompletionTable},
		{"FROM then an alias wants a keyword", "SELECT * FROM users u|", CompletionKeyword},

		{"SELECT wants the select list", "SELECT | FROM users", CompletionSelectList},
		{"a second column in the select list", "SELECT id, | FROM users", CompletionSelectList},

		{"WHERE wants a predicate", "SELECT * FROM users WHERE |", CompletionColumn},
		{"AND continues the predicate", "SELECT * FROM users WHERE a = 1 AND |", CompletionColumn},
		{"OR continues the predicate", "SELECT * FROM users WHERE a = 1 OR |", CompletionColumn},
		{"ON wants a predicate", "SELECT * FROM a JOIN b ON |", CompletionColumn},
		{"HAVING wants a predicate", "SELECT a FROM t GROUP BY a HAVING |", CompletionColumn},

		{"ORDER BY wants a column", "SELECT * FROM users ORDER BY |", CompletionColumn},
		{"GROUP BY wants a column", "SELECT * FROM users GROUP BY |", CompletionColumn},
		{"SET wants a column", "UPDATE users SET |", CompletionColumn},
		{"RETURNING wants a column", "INSERT INTO users (a) VALUES (1) RETURNING |", CompletionColumn},
		{"DISTINCT wants a column", "SELECT DISTINCT | FROM users", CompletionColumn},
		{"AS wants a column", "SELECT a AS | FROM users", CompletionColumn},

		// Three arms that are functions of the PREVIOUS token rather than of the clause.
		// `x IN (` needs a value list; `x IS NULL` needs a value; `LIMIT 10` needs a
		// number and offers nothing at all.
		{"IN wants a keyword", "SELECT * FROM t WHERE a IN |", CompletionKeyword},
		{"NOT IN wants a keyword", "SELECT * FROM t WHERE a NOT IN |", CompletionKeyword},
		{"EXISTS wants a keyword", "SELECT * FROM t WHERE EXISTS |", CompletionKeyword},
		{"IS wants a value", "SELECT * FROM t WHERE a IS |", CompletionValue},
		{"IS NOT wants a value", "SELECT * FROM t WHERE a IS NOT |", CompletionValue},
		{"BETWEEN wants a value", "SELECT * FROM t WHERE a BETWEEN |", CompletionValue},
		{"LIKE wants a value", "SELECT * FROM t WHERE a LIKE |", CompletionValue},
		{"ILIKE wants a value", "SELECT * FROM t WHERE a ILIKE |", CompletionValue},
		{"LIMIT offers NOTHING", "SELECT * FROM users LIMIT |", CompletionEmpty},
		{"OFFSET offers NOTHING", "SELECT * FROM users LIMIT 10 OFFSET |", CompletionEmpty},

		// The default arm, and the one easiest to reach by accident: any clause the switch
		// does not name falls here, which is what keeps a misspelled clause working.
		{"an unnamed clause wants a keyword", "SELECT * FROM users SETT |", CompletionKeyword},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := at(t, tc.sql)
			if got.kind != tc.want {
				t.Errorf("at %q the kind is %s, want %s\n  full context: %+v",
					tc.sql, kindName(got.kind), kindName(tc.want), got)
			}
		})
	}
}

// TestTheSelectListKnowsWhetherItIsAtTheStart pins the one thing CompletionSelectList
// carries besides its kind: whether anything has been selected yet.
//
// It matters because the suggestion set differs. At the start of the list the user wants
// everything — a table name is legitimate there. After a comma they want another column,
// and offering table names then is what produces `SELECT id, users FROM`.
func TestTheSelectListKnowsWhetherItIsAtTheStart(t *testing.T) {
	for _, tc := range []struct {
		name    string
		sql     string
		atStart bool
	}{
		{"nothing typed yet", "SELECT | FROM users", true},
		// True, and not by accident: the token the cursor is INSIDE is the token being
		// completed, so it is excluded from the tail — which leaves the tail empty and
		// therefore "at the start". The first version of this case expected false here,
		// having read selectContext as "something is already selected".
		{"the cursor is inside the first column", "SELECT id| FROM users", true},
		// False, and the contrast is the point: `*` is not a word token, so it is NOT
		// treated as the token being completed and it stays in the tail.
		{"a star is already in the list", "SELECT *| FROM users", false},
		{"after a comma", "SELECT id, | FROM users", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := at(t, tc.sql)
			if ctx.kind != CompletionSelectList {
				t.Fatalf("the kind is %s, want a select list", kindName(ctx.kind))
			}
			if ctx.selectContext != tc.atStart {
				t.Errorf("selectContext is %t, want %t", ctx.selectContext, tc.atStart)
			}
		})
	}
}

// TestTheQualificationDecidesWhetherAColumnOrATableIsOffered covers qualifiedContext, the
// walk over the last four tokens that handles `schema.`, `table.`, `schema.table.` and a
// bare word.
//
// The indices are the risk: passed[len-1] has to be the dot, passed[len-2] the identifier
// before it, and for the three-part form passed[len-3] is ANOTHER dot with a word at
// len-4. A statement with only two tokens before the dot must not read len-3 or len-4, and
// the `len(passed) >= 4` guard is the only thing stopping it.
func TestTheQualificationDecidesWhetherAColumnOrATableIsOffered(t *testing.T) {
	for _, tc := range []struct {
		name       string
		sql        string
		wantKind   CompletionKind
		wantSchema string
		wantTable  string
	}{
		{"a known schema dot wants its tables", "SELECT * FROM analytics.|", CompletionTable, "analytics", ""},
		{"a known table dot resolves schema and table", "SELECT users.| FROM users", CompletionColumn, "public", "users"},
		{"a table dot with no FROM still resolves", "SELECT users.|", CompletionColumn, "public", "users"},
		// The fallback, and the reason the resolved-schema assertions above matter: an
		// unknown qualifier is offered as a TABLE with no schema. That is what lets a
		// schema the catalog has not loaded yet still get suggestions.
		{"an unknown qualifier is treated as an unqualified table", "SELECT ghost.|", CompletionColumn, "", "ghost"},
		// Three-part form: read straight out of the tokens with no existence check, so a
		// schema that is not in the catalog still names its table.
		{"schema.table dot names both", "SELECT sales.orders.|", CompletionColumn, "sales", "orders"},

		// The boundaries. A number before the dot is not a word token, so there is no
		// qualification and the clause switch answers instead.
		{"a number before the dot is not a qualification", "SELECT 1.|", CompletionSelectList, "", ""},

		// The wart, pinned because it is a wart and not a decision: isWordToken accepts
		// KEYWORDS, so a keyword before a dot is taken for a table qualifier and the popup
		// opens an (empty) column list for a table named "SELECT". It cannot arise in
		// valid SQL, so it is left alone — but it is the reason the "unknown qualifier"
		// fallback above is not free of consequences.
		{"a keyword before the dot becomes the table name", "SELECT .|", CompletionColumn, "", "SELECT"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := at(t, tc.sql)
			if ctx.kind != tc.wantKind {
				t.Errorf("at %q the kind is %s, want %s", tc.sql, kindName(ctx.kind), kindName(tc.wantKind))
			}
			if ctx.schema != tc.wantSchema {
				t.Errorf("at %q the schema is %q, want %q", tc.sql, ctx.schema, tc.wantSchema)
			}
			if ctx.table != tc.wantTable {
				t.Errorf("at %q the table is %q, want %q", tc.sql, ctx.table, tc.wantTable)
			}
		})
	}
}

// TestTheSemicolonResetsWhatTheStatementReferences is the statement boundary.
//
// Everything after the LAST semicolon is the current statement. `passed = passed[idx+1:]`
// — one index off would either keep the dead statement's FROM list or drop the current
// one's. The assertion is on the tables the context carries, because that is what the
// column filter uses: a query after a `;` must see the tables IT references and not the
// ones the finished statement did.
func TestTheSemicolonResetsWhatTheStatementReferences(t *testing.T) {
	t.Run("the second statement does not inherit the first one's FROM list", func(t *testing.T) {
		// After `; SELECT |` there is no table in scope, so tables is empty. If the slice
		// truncation were `passed[:idx]` instead, the context would carry users and the
		// popup would offer users' columns inside a statement that cannot see them.
		ctx := at(t, "SELECT id FROM users; SELECT |")
		if len(ctx.tables) != 0 {
			t.Errorf("the context carries %d tables (%+v), want none — the first statement ended at the semicolon",
				len(ctx.tables), ctx.tables)
		}
	})

	t.Run("the LAST semicolon wins with several in the batch", func(t *testing.T) {
		// Three statements. Only what follows the last one is current — and what follows
		// it here is a bare SELECT with no FROM yet, so the answer is NO tables. The
		// first version of this case expected orders, having read the middle statement's
		// FROM as the current one.
		ctx := at(t, "SELECT * FROM users; SELECT * FROM orders; SELECT |")
		if len(ctx.tables) != 0 {
			t.Errorf("the context carries %+v, want none — the current statement has no FROM yet", ctx.tables)
		}
	})

	t.Run("the current statement's OWN FROM list is what is offered", func(t *testing.T) {
		// The other half, and the one a `passed[:idx]` truncation would break: after a
		// semicolon the NEW statement's FROM has to be found. Getting this wrong in the
		// other direction — truncating too far — would leave the user with no columns at
		// all in the statement they are actually writing.
		ctx := at(t, "SELECT * FROM users; SELECT | FROM orders")
		if len(ctx.tables) != 1 || ctx.tables[0].table != "orders" {
			t.Errorf("the context carries %+v, want the current statement's table (orders)", ctx.tables)
		}
	})

	t.Run("a statement with no semicolon keeps its own FROM list", func(t *testing.T) {
		// The other half: the truncation must not fire when there is no semicolon. A
		// guard that ran unconditionally would empty the table list for every query.
		ctx := at(t, "SELECT | FROM users")
		if len(ctx.tables) != 1 || ctx.tables[0].table != "users" {
			t.Errorf("the context carries %+v, want users", ctx.tables)
		}
	})
}

// TestTheCursorWalksAFilteredListAndAnEmptyOneIsHarmless covers SelectNext and SelectPrev
// on a list that has been filtered down to nothing.
//
// The cursors WRAP in this app — eleven of them do — so moving past the end is not a
// clamp. What matters on an empty list is that the cursor does not go to -1 or 1 and stay
// there, because SelectedItem() indexes with it.
func TestTheCursorWalksAFilteredListAndAnEmptyOneIsHarmless(t *testing.T) {
	a := newTestAutocomplete()

	t.Run("an empty list leaves the cursor where it was", func(t *testing.T) {
		a.filtered = nil
		a.selected = 0

		a.SelectNext()
		if a.selected != 0 {
			t.Errorf("SelectNext on an empty list moved the cursor to %d", a.selected)
		}
		a.SelectPrev()
		if a.selected != 0 {
			t.Errorf("SelectPrev on an empty list moved the cursor to %d", a.selected)
		}
		// And SelectedItem must still answer nil rather than indexing.
		if got := a.SelectedItem(); got != nil {
			t.Errorf("SelectedItem on an empty list is %+v, want nil", got)
		}
	})

	t.Run("moving past the last item wraps to the first", func(t *testing.T) {
		a.filtered = []CompletionItem{{Name: "one"}, {Name: "two"}, {Name: "three"}}
		a.selected = 2
		a.SelectNext()
		if a.selected != 0 {
			t.Errorf("past the last item the cursor is at %d, want 0", a.selected)
		}
	})

	t.Run("moving before the first item wraps to the last", func(t *testing.T) {
		a.filtered = []CompletionItem{{Name: "one"}, {Name: "two"}, {Name: "three"}}
		a.selected = 0
		a.SelectPrev()
		if a.selected != 2 {
			t.Errorf("before the first item the cursor is at %d, want the last (2)", a.selected)
		}
	})

	t.Run("SelectedItem answers nil when the cursor is past the list", func(t *testing.T) {
		// The other guard in SelectedItem. A filter that shrinks the list while the cursor
		// sits at the old position leaves selected >= len(filtered), and indexing with
		// that is a panic.
		a.filtered = []CompletionItem{{Name: "one"}}
		a.selected = 7
		a.visible = true

		if got := a.SelectedItem(); got != nil {
			t.Errorf("SelectedItem with the cursor past the end is %+v, want nil", got)
		}
	})

	t.Run("SelectedItem answers nil when the popup is hidden", func(t *testing.T) {
		// The first half of the same guard, and the one that matters after Cancel():
		// Cancel sets filtered = nil but a caller that read SelectedItem first would
		// still get an item and accept it into the buffer.
		a.filtered = []CompletionItem{{Name: "one"}}
		a.selected = 0
		a.visible = false

		if got := a.SelectedItem(); got != nil {
			t.Errorf("SelectedItem on a hidden popup is %+v, want nil", got)
		}
	})
}

// TestTheColumnSuggestionsAreDeduplicated pins the `seen` map in columnItems.
//
// The key is name + schema + table, all lowercased. Two things depend on it: a table
// listed twice in the FROM offers each column twice, and the same column name in two
// different tables is NOT a duplicate — those are different columns. So both halves get
// asserted, or a de-duplication that is too aggressive silently removes columns that were
// needed.
func TestTheColumnSuggestionsAreDeduplicated(t *testing.T) {
	a := NewAutocompleteState()
	a.all = []CompletionItem{
		{Kind: CompletionColumn, Name: "id", Schema: "public", Table: "users"},
		{Kind: CompletionColumn, Name: "id", Schema: "public", Table: "users"}, // exact repeat
		{Kind: CompletionColumn, Name: "ID", Schema: "PUBLIC", Table: "USERS"}, // same, other case
		{Kind: CompletionColumn, Name: "id", Schema: "public", Table: "orders"},
		{Kind: CompletionKeyword, Name: "id"}, // shares the name, is not a column
	}

	t.Run("the exact repeat collapses to one, case-insensitively", func(t *testing.T) {
		got := a.columnItems(completionContext{kind: CompletionColumn, table: "users", schema: "public"})
		if len(got) != 1 {
			names := make([]string, len(got))
			for i, item := range got {
				names[i] = item.Schema + "." + item.Table + "." + item.Name
			}
			t.Errorf("the de-duplicated list is %d items (%v), want 1", len(got), names)
		}
	})

	t.Run("a keyword sharing a column's name is never offered as a column", func(t *testing.T) {
		// The `item.Kind != CompletionColumn` continue. A keyword named "id" reaching a
		// column position would let the user complete `SELECT id,` with a keyword, which
		// SQL reads differently from a column reference.
		for _, item := range a.columnItems(completionContext{kind: CompletionColumn, table: "users", schema: "public"}) {
			if item.Kind != CompletionColumn {
				t.Errorf("a %s item named %q was offered as a column", kindName(item.Kind), item.Name)
			}
		}
	})

	t.Run("a column matching across the FROM list keeps every table's copy", func(t *testing.T) {
		// With no schema and no table, columnMatches falls to the FROM-list branch, which
		// accepts a name from ANY referenced table. So public.users.id qualifies — and it
		// is a different column from public.orders.id, so only one is offered here.
		got := a.columnItems(completionContext{
			kind: CompletionColumn,
			tables: []tableRef{
				{table: "users", schema: "public"},
				{table: "orders", schema: "public"},
			},
		})
		if len(got) != 2 {
			names := make([]string, len(got))
			for i, item := range got {
				names[i] = item.Table + "." + item.Name
			}
			t.Errorf("two referenced tables offer %d copies of id (%v), want 2", len(got), names)
		}
	})

	t.Run("no table in scope offers nothing rather than the whole catalog", func(t *testing.T) {
		// The first guard in columnItems. Dumping the entire catalog here would bury the
		// handful of columns the statement can actually see.
		if got := a.columnItems(completionContext{kind: CompletionColumn}); got != nil {
			t.Errorf("with no table in scope the popup offers %d items, want none", len(got))
		}
	})
}

// TestAnEmptyWordIsNotAKeywordPrefix pins the one-word guard.
//
// isSQLKeywordPrefix is what lets ANY clause fall back to keyword completion, so a typo
// there stops the popup opening on a half-typed word in every clause at once. The empty
// string is the case that matters: with no prefix typed, EVERY keyword starts with it, so
// the guard is the only thing standing between "the user asked for suggestions" and "the
// popup shows four hundred keywords every time they move the cursor".
func TestAnEmptyWordIsNotAKeywordPrefix(t *testing.T) {
	if isSQLKeywordPrefix("") {
		t.Error("the empty string is a keyword prefix, which would flood the popup on every cursor move")
	}
	if !isSQLKeywordPrefix("SEL") {
		t.Error("SEL is not a keyword prefix")
	}
	if isSQLKeywordPrefix("zzzz") {
		t.Error("zzzz is a keyword prefix")
	}
	// Case-insensitive, because the popup would be useless on a lowercase keyboard.
	if !isSQLKeywordPrefix("sel") {
		t.Error("a lowercase keyword prefix does not match")
	}

	// And the boolean it feeds: isSQLKeyword. Same reason, opposite direction.
	if !isSQLKeyword("select") {
		t.Error("select is not recognised as a keyword in lowercase")
	}
	if isSQLKeyword("selct") {
		t.Error("a misspelling is recognised as a keyword")
	}
}

// TestEveryKindGetsItsOwnStyle pins the seven-arm switch in Render.
//
// The failure is a kind falling through to `default` and rendering unstyled: the row is
// still there and still correct, so nothing fails — the popup just quietly stops
// distinguishing a keyword from a column. One row per kind, each asserted to carry
// styling, is the only way to catch it.
//
// Two kinds are DELIBERATELY unstyled and are excluded: CompletionEmpty has nothing to
// style it with, and CompletionSelectList is not something that ever reaches Render as a
// row kind. Asserting they are styled would be asserting a change nobody asked for.
func TestEveryKindGetsItsOwnStyle(t *testing.T) {
	styled := []CompletionKind{
		CompletionKeyword,
		CompletionFunction,
		CompletionSchema,
		CompletionTable,
		CompletionColumn,
		CompletionOperator,
		CompletionValue,
	}

	for _, k := range styled {
		t.Run(kindName(k), func(t *testing.T) {
			a := NewAutocompleteState()
			a.visible = true
			a.filtered = []CompletionItem{{Kind: k, Name: "thing", Detail: "text"}}
			a.selected = 0

			row := theRowWith(t, a, "thing")
			if !strings.Contains(row, "\x1b[") {
				t.Errorf("the %s row has no styling in it: %q", kindName(k), row)
			}
		})
	}

	t.Run("an unstyled kind still renders its name", func(t *testing.T) {
		// CompletionEmpty falls through the switch to the bare name, which is correct —
		// but it must still be VISIBLE. A kind that renders as nothing would look like a
		// missing row.
		a := NewAutocompleteState()
		a.visible = true
		a.filtered = []CompletionItem{{Kind: CompletionEmpty, Name: "thing"}}
		a.selected = 0

		row := theRowWith(t, a, "thing")
		if !strings.Contains(ansi.Strip(row), "thing") {
			t.Errorf("the empty-kind row renders as %q, want the name in it", row)
		}
	})
}

// theRowWith returns the single popup row containing name, failing if there is not
// exactly one.
func theRowWith(t *testing.T, a *AutocompleteState, name string) string {
	t.Helper()
	out := a.Render(theme.Resolve("dark").Styles(), 100)
	var found []string
	for _, row := range strings.Split(out, "\n") {
		if strings.Contains(ansi.Strip(row), name) {
			found = append(found, row)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%d rows contain %q, want exactly one:\n%s", len(found), name, out)
	}
	return found[0]
}

func TestThePopupClampsItsWidths(t *testing.T) {
	t.Run("a narrow popup still shows part of the name", func(t *testing.T) {
		// nameMaxW = popupW - kindW - 5 with a floor of 10. Below that floor a name would
		// be truncated to nothing and the popup would show kind labels with no names at
		// all — the user sees a list and cannot tell what is in it.
		a := NewAutocompleteState()
		a.visible = true
		a.filtered = []CompletionItem{{Kind: CompletionColumn, Name: "a_very_long_column_name", Detail: "text"}}
		a.selected = 0

		for _, width := range []int{14, 20, 40, 80, 200} {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("Render panicked at width %d: %v", width, r)
					}
				}()
				stripped := ansi.Strip(a.Render(theme.Resolve("dark").Styles(), width))
				if !strings.Contains(stripped, "a_very") {
					t.Errorf("at width %d the popup shows no part of the name:\n%s", width, stripped)
				}
			}()
		}
	})

	t.Run("a kind label wider than its column is truncated", func(t *testing.T) {
		// kindW is a hardcoded 10 and a column's kind label is its DATA TYPE, which can
		// be longer. Without the truncation the label's own width pushes every row's
		// padding out of alignment and the two columns stop lining up.
		a := NewAutocompleteState()
		a.visible = true
		a.filtered = []CompletionItem{{
			Kind:   CompletionColumn,
			Name:   "id",
			Detail: "an extremely long type name",
		}}
		a.selected = 0

		out := ansi.Strip(a.Render(theme.Resolve("dark").Styles(), 100))
		if !strings.Contains(out, "..") {
			t.Errorf("the long kind label was not truncated:\n%s", out)
		}
	})
}
