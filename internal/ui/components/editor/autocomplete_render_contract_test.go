package editor

// Scenario: El popup de sugerencias tiene que verse bien con lo que se le pone delante.
//
// Render was the largest uncovered function in the file and every uncovered branch in it
// is a LAYOUT decision: how wide the popup is, which items fit, what happens when the
// popup is narrower than the name it has to show, and what happens when the selection is
// below the window. None of those is reachable from a fixture with four short column
// names, which is what every pre-existing autocomplete test used.
//
// The failure modes are all silent. A popup one row too tall covers the text being typed. A
// name truncated with the ellipsis style that is not a tail reads as a different column.
// An item list that stops one short of the selection shows a popup with no highlighted
// line in it.
//
// The other half of this file is the pure predicates — isOperatorToken, predicateContext,
// fuzzyMatchAutocomplete, lastIndexText — which are cheap to cover exhaustively and are
// the sort of thing that gets rewritten by a well-meaning refactor without anyone noticing
// that "WHERE" stopped being recognised as a clause.

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	aiContext "github.com/buble/dbx/internal/ai/context"

	"github.com/buble/dbx/internal/theme"
)

// ---------------------------------------------------------------------------
// KindLabel
// ---------------------------------------------------------------------------

// Scenario: Cada tipo de sugerencia dice como se llama, y uno desconocido no miente.
func TestKindLabelNamesEveryKind(t *testing.T) {
	for _, tc := range []struct {
		kind   CompletionKind
		detail string
		want   string
	}{
		{CompletionSchema, "", "schema"},
		{CompletionTable, "", "table"},
		{CompletionKeyword, "", "keyword"},
		{CompletionFunction, "", "function"},
		{CompletionOperator, "", "operator"},
		{CompletionValue, "", "value"},
		// A column's label is its DETAIL — the data type. Which is the only one of
		// the seven whose label is not a fixed word, and the reason Detail exists.
		{CompletionColumn, "integer", "integer"},
		{CompletionColumn, "", ""},
	} {
		got := CompletionItem{Kind: tc.kind, Detail: tc.detail}.KindLabel()
		if got != tc.want {
			t.Errorf("a %v item labelled %q, want %q", tc.kind, got, tc.want)
		}
	}

	t.Run("an unknown kind labels as empty rather than panicking", func(t *testing.T) {
		// The switch has no default and falls off the end. A future kind added to the
		// enum without a case here must not make the popup panic on the first frame.
		got := CompletionItem{Kind: CompletionKind(999), Detail: "text"}.KindLabel()
		if got != "" {
			t.Errorf("an unknown kind labelled %q, want empty", got)
		}
	})
}

// ---------------------------------------------------------------------------
// pure predicates
// ---------------------------------------------------------------------------

// Scenario: Los predicados puros se prueban como puros: entrada, salida.
//
// Exhaustive rather than representative, because each is a small switch that a refactor
// can quietly change and because "is this operator?" has a list someone typed by hand.
func TestTheOperatorPredicateKnowsEveryOperator(t *testing.T) {
	for _, op := range []string{
		"=", "!=", "<>", "<", "<=", ">", ">=",
		"LIKE", "like", "ILIKE", "ilike", "In", "in",
		"IS", "is", "BETWEEN", "between", "NOT", "not",
	} {
		if !isOperatorToken(sqlToken{text: op}) {
			t.Errorf("%q is not recognised as an operator", op)
		}
	}

	for _, word := range []string{
		"", "SELECT", "WHERE", "AND", "OR", "NULL", "ANDROID", "ISLAND",
		"==", "=>", "!", "~~", "!", "NOT_LIKE", "INNER", "ISOLATION",
	} {
		if isOperatorToken(sqlToken{text: word}) {
			t.Errorf("%q is recognised as an operator but is not one", word)
		}
	}
}

// Scenario: Donde el cursor esta dentro de un literal, el autocomplete se calla.
func TestInsideLiteralOrComment(t *testing.T) {
	for _, tc := range []struct {
		name string
		line string
		col  int
		want bool
		note string
	}{
		// The string token spans [4,9) for 'abc', so the answer turns on where the
		// cursor sits RELATIVE TO THE TOKEN, not on whether a string is nearby.
		{"inside a closed string", "x = 'abc'", 8, true, "cursor before the closing quote"},
		{"just past the closing quote", "x = 'abc'", 9, false, "cursor at the token end"},
		{"a space past the closing quote", "x = 'abc' ", 10, false, ""},
		{"past a closed string entirely", "x = 'abc' || y", 14, false, ""},
		{"inside an empty string literal", "x = ''", 5, true, "the quotes span [4,6), so 5 is inside them"},
		{"past an empty string literal", "x = ''", 6, false, ""},
		{"inside a line comment", "-- a comment", 5, true, "a line comment runs to the end"},
		{"inside a block comment", "/* hi */", 4, true, ""},
		{"inside a block comment before its close", "/* hi */", 2, true, ""},
		{"after a closed block comment", "/* hi */ x", 10, false, ""},
		{"plain code", "SELECT 1", 5, false, ""},
		{"at the very start", "SELECT 1", 0, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tokens := tokenizeSQL(tc.line)
			if got := insideLiteralOrComment(tokens, tc.col); got != tc.want {
				t.Errorf("insideLiteralOrComment(%q, %d) is %t, want %t — %s (tokens %+v)",
					tc.line, tc.col, got, tc.want, tc.note, tokens)
			}
		})
	}
}

// Scenario: Una palabra vacia no es prefijo de nada, pero el contexto la ofrece igual.
func TestIsSQLKeywordPrefix(t *testing.T) {
	for _, word := range []string{"s", "se", "sel", "SEL", "w", "wh", "b"} {
		if !isSQLKeywordPrefix(word) {
			t.Errorf("%q is not recognised as a keyword prefix", word)
		}
	}
	for _, word := range []string{"x", "1", "id", "users", "total"} {
		if isSQLKeywordPrefix(word) {
			t.Errorf("%q is recognised as a keyword prefix but is a column name", word)
		}
	}
}

// Scenario: El predicado de predicados decide columna, operador o valor.
func TestPredicateContext(t *testing.T) {
	col := func(text string) sqlToken { return sqlToken{text: text} }

	for _, tc := range []struct {
		name string
		tail []sqlToken
		want CompletionKind
		note string
	}{
		{"nothing typed yet suggests a column", nil, CompletionColumn,
			"the only case that is a column: no tokens at all"},
		{"after WHERE suggests an operator", []sqlToken{col("WHERE")}, CompletionOperator,
			"a clause keyword in the tail is not a column, so the next thing is an operator"},
		{"after WHERE and a column suggests an operator", []sqlToken{col("WHERE"), col("name")}, CompletionOperator, ""},
		{"after a comparison suggests a value", []sqlToken{col("WHERE"), col("name"), col("=")}, CompletionValue, ""},
		{"after LIKE suggests a value", []sqlToken{col("WHERE"), col("name"), col("LIKE")}, CompletionValue, ""},
		{"an operator EARLIER in the tail suggests a keyword", []sqlToken{col("="), col("name")}, CompletionKeyword,
			"the LAST token is not an operator, so the earlier one wins"},
		{"a bare clause keyword suggests an operator", []sqlToken{col("AND")}, CompletionOperator, ""},
		{"column then AND suggests an operator", []sqlToken{col("id"), col("AND")}, CompletionOperator, ""},
		{"operator then clause keyword suggests a keyword", []sqlToken{col("="), col("AND")}, CompletionKeyword,
			"AND is not an operator token, so the last-token check fails and the loop finds the ="},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := predicateContext(tc.tail); got != tc.want {
				t.Errorf("predicateContext(%v) is %v, want %v — %s", tc.tail, got, tc.want, tc.note)
			}
		})
	}
}

// Scenario: El fuzzy match es de PREFIJO, no de subcadena, y case-insensitive en la query.
func TestFuzzyMatchAutocomplete(t *testing.T) {
	// A SUBSEQUENCE match, not a prefix match: the query's characters have to appear
	// in order but need not be adjacent. The first version of this asserted "ac"
	// against "abc" was false and "usr" against "users" was false, having read the
	// loop as a prefix comparison. It is not — it advances the query cursor on any
	// match and keeps walking the target — which is why typing "usr" finds "users".
	for _, tc := range []struct {
		query, target string
		want          bool
	}{
		{"", "", true},
		{"", "anything", true},
		{"a", "abc", true},
		{"ab", "abc", true},
		{"abc", "abc", true},
		{"abc", "abcd", true},
		{"ac", "abc", true},  // in order, with a gap
		{"ba", "abc", false}, // out of order
		{"b", "abc", true},
		{"A", "abc", false}, // case matters: the target is a database name
		{"id", "id", true},
		{"zz", "abc", false},
		{"abc", "ab", false},
		{"user", "users", true},
		{"usr", "users", true},
		{"ue", "users", true},
	} {
		if got := fuzzyMatchAutocomplete(tc.query, tc.target); got != tc.want {
			t.Errorf("fuzzyMatchAutocomplete(%q, %q) is %t, want %t", tc.query, tc.target, got, tc.want)
		}
	}
}

// Scenario: Buscar un token de atras hacia adelante.
func TestLastIndexText(t *testing.T) {
	toks := []sqlToken{{text: "SELECT"}, {text: "FROM"}, {text: "users"}, {text: "WHERE"}, {text: "users"}}

	if got := lastIndexText(toks, "users"); got != 4 {
		t.Errorf("lastIndexText for the repeated token is %d, want the LAST index 4", got)
	}
	if got := lastIndexText(toks, "SELECT"); got != 0 {
		t.Errorf("lastIndexText for a unique token is %d, want 0", got)
	}
	if got := lastIndexText(toks, "ORDER"); got != -1 {
		t.Errorf("lastIndexText for an absent token is %d, want -1", got)
	}
	if got := lastIndexText(nil, "SELECT"); got != -1 {
		t.Errorf("lastIndexText over no tokens is %d, want -1", got)
	}
}

// ---------------------------------------------------------------------------
// detectContext
// ---------------------------------------------------------------------------

// Scenario: El contexto depende de DONDE esta el cursor, no de donde esta la linea.
func TestDetectContextIsAboutTheCursorNotTheLine(t *testing.T) {
	for _, tc := range []struct {
		name   string
		line   string
		col    int
		kind   CompletionKind
		schema string
		table  string
		note   string
	}{
		{"an empty line offers keywords", "", 0, CompletionKeyword, "", "", ""},
		{"right after SELECT offers the select list", "SELECT ", 7, CompletionSelectList, "", "", ""},
		{"in the select list offers columns", "SELECT id, na", 12, CompletionSelectList, "", "", ""},
		{"a qualified table takes the schema", "SELECT * FROM analytics.", 24, CompletionTable, "analytics", "", ""},
		{"a partially typed qualified table", "SELECT * FROM analytics.ev", 25, CompletionTable, "analytics", "", ""},
		{"inside a WHERE offers columns", "SELECT * FROM users WHERE ", 26, CompletionColumn, "", "", ""},
		{"after a comparison offers values", "SELECT * FROM users WHERE id = ", 33, CompletionValue, "", "", ""},
		// These three record a decision rather than an accident.
		{"a space after a table name falls back to keywords", "SELECT * FROM users ", 20, CompletionKeyword, "", "",
			"columns are NOT offered here: isSQLKeywordPrefix lets any unrecognised position fall back to keywords, so FROM-then-space is keywords. Worth knowing before anyone calls it a bug."},
		{"a comment offers nothing", "SELECT * FROM users -- and ", 27, CompletionEmpty, "", "", ""},
		{"a cursor past the end is clamped to the line", "SELECT * FROM users", 500, CompletionTable, "", "",
			"clamping to the end of the line leaves the last word as the token being typed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestAutocomplete()
			ctx := a.detectContext(tc.line, tc.col)
			if ctx.kind != tc.kind {
				t.Errorf("the kind is %v, want %v", ctx.kind, tc.kind)
			}
			if ctx.schema != tc.schema {
				t.Errorf("the schema is %q, want %q", ctx.schema, tc.schema)
			}
			if tc.table != "" && ctx.table != tc.table {
				t.Errorf("the table is %q, want %q", ctx.table, tc.table)
			}
			if tc.note != "" {
				t.Log(tc.note)
			}
		})
	}

	t.Run("inside a string literal the clause still drives the kind, but nothing is offered", func(t *testing.T) {
		// detectContext does not stop at the literal: it sees WHERE name = and reports
		// CompletionValue. What suppresses the popup is insideLiteralOrComment, which
		// the caller checks before rendering. So the kind alone is not proof that
		// suggestions would appear — and a test asserting on the kind alone would
		// claim the popup is open when it is not.
		//
		// The first version of this asserted the kind was NOT CompletionColumn and
		// passed, which proved nothing at all: five of the seven kinds are not
		// CompletionColumn.
		a := newTestAutocomplete()
		line := "SELECT * FROM users WHERE name = 'us"
		// 34, not 33. The literal's token starts at 33 and insideLiteralOrComment
		// breaks on `token.start >= col`, so the cursor must be strictly PAST the
		// opening quote to count as inside it. At 33 the cursor is at the START of
		// the literal, which is a legal position that is not inside it — and the
		// first version of this test used 33 and read the false as the popup being
		// shown, when the cursor simply was not in the string.
		col := 34
		if !insideLiteralOrComment(tokenizeSQL(line), col) {
			t.Fatal("the cursor is not reported as inside the literal; the rest of this case cannot hold")
		}
		// detectContext checks the literal itself and reports CompletionEmpty, so the
		// popup is suppressed here and not downstream. The first version expected
		// CompletionValue on the reasoning that the clause is still tracked; the
		// clause IS still tracked, but the literal check comes first and wins.
		if got := a.detectContext(line, col).kind; got != CompletionEmpty {
			t.Errorf("the kind inside the literal is %v, want CompletionEmpty", got)
		}
		// And just before the quote, the clause does drive it — the same cursor on
		// the same line, one column to the left.
		if got := a.detectContext(line, col-2).kind; got != CompletionValue {
			t.Errorf("two columns before the literal the kind is %v, want CompletionValue", got)
		}
	})

	t.Run("a cursor inside a comment offers nothing", func(t *testing.T) {
		a := newTestAutocomplete()
		ctx := a.detectContext("SELECT * FROM users -- and ", 27)
		if ctx.kind != CompletionEmpty {
			t.Errorf("inside a comment the kind is %v, want empty", ctx.kind)
		}
	})
}

// ---------------------------------------------------------------------------
// table resolution
// ---------------------------------------------------------------------------

// Scenario: Un alias manda, luego el nombre de la tabla, y si no se busca en el esquema.
func TestResolveTablePrefersTheAlias(t *testing.T) {
	a := newTestAutocomplete()
	refs := []tableRef{
		{schema: "public", table: "users", alias: "u"},
		{schema: "public", table: "orders", alias: "o"},
	}

	for _, tc := range []struct{ name, wantSchema, wantTable string }{
		{"u", "public", "users"},
		{"U", "public", "users"}, // case insensitive
		{"o", "public", "orders"},
		{"users", "public", "users"},
		{"USERS", "public", "users"},
	} {
		t.Run("by "+tc.name, func(t *testing.T) {
			schema, table, ok := a.resolveTable(tc.name, refs)
			if !ok {
				t.Fatalf("%q did not resolve", tc.name)
			}
			if schema != tc.wantSchema || table != tc.wantTable {
				t.Errorf("%q resolved to %s.%s, want %s.%s", tc.name, schema, table, tc.wantSchema, tc.wantTable)
			}
		})
	}

	t.Run("given RAW tokens the whitespace becomes the table name", func(t *testing.T) {
		// The trap, pinned so it is a decision on the record rather than a surprise
		// for the next caller. isWordToken rejects "", "(", ")", ",", ";", "." and
		// operator and string tokens — but not a space. So the token list from
		// tokenizeSQL, which keeps whitespace, makes the function record the space
		// as the table and the real name as the alias.
		a := newTestAutocomplete()
		got := a.findTablesInStatement(tokenizeSQL("SELECT * FROM users"))
		if len(got) != 1 {
			t.Fatalf("found %+v, want one reference", got)
		}
		if got[0].table != " " || got[0].alias != "users" {
			t.Errorf("raw tokens gave %+v; if this now returns users correctly, isWordToken has learned to reject whitespace and this note is stale", got[0])
		}
		// And the filtered path, which is the one the app uses, is right.
		filtered := a.findTablesInStatement(significantTokens(tokenizeSQL("SELECT * FROM users")))
		if len(filtered) != 1 || filtered[0].table != "users" || filtered[0].alias != "" {
			t.Errorf("filtered tokens gave %+v, want table=users with no alias", filtered)
		}
	})

	t.Run("a name that is only a table falls through to the schema", func(t *testing.T) {
		schema, table, ok := a.resolveTable("events", nil)
		if !ok {
			t.Fatal("events did not resolve with no refs")
		}
		if schema != "analytics" || table != "events" {
			t.Errorf("events resolved to %s.%s, want analytics.events", schema, table)
		}
	})

	t.Run("a name nobody knows does not resolve", func(t *testing.T) {
		schema, table, ok := a.resolveTable("nosuchtable", nil)
		if ok {
			t.Errorf("nosuchtable resolved to %s.%s", schema, table)
		}
		if schema != "" || table != "" {
			t.Errorf("an unresolved name returned %q/%q, want both empty", schema, table)
		}
	})

	t.Run("hasTablesInSchema answers about the schema it was given", func(t *testing.T) {
		if !a.hasTablesInSchema("public") {
			t.Error("public has no tables")
		}
		if a.hasTablesInSchema("nosuchschema") {
			t.Error("nosuchschema has tables")
		}
		// A schema NAME that is also a table name must not be confused with it.
		if a.hasTablesInSchema("users") {
			t.Error("users reported as a schema")
		}
	})
}

// Scenario: Las tablas de una sentencia se encuentran, con alias o sin él.
func TestFindTablesInStatement(t *testing.T) {
	a := newTestAutocomplete()

	for _, tc := range []struct {
		name     string
		stmt     string
		wantRefs []tableRef
	}{
		{"an unqualified table", "SELECT * FROM users", []tableRef{{schema: "public", table: "users"}}},
		{"an alias", "SELECT * FROM users u", []tableRef{{schema: "public", table: "users", alias: "u"}}},
		{"AS is allowed", "SELECT * FROM users AS u", []tableRef{{schema: "public", table: "users", alias: "u"}}},
		{"a qualified table", "SELECT * FROM analytics.events", []tableRef{{schema: "analytics", table: "events"}}},
		{"two tables", "SELECT * FROM users u JOIN orders o ON u.id = o.user_id",
			[]tableRef{{schema: "public", table: "users", alias: "u"}, {schema: "public", table: "orders", alias: "o"}}},
		{"no tables", "SELECT 1", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// significantTokens, because that is what the one caller passes. The
			// raw token list contains a whitespace token after FROM, and
			// isWordToken does not reject it — so given raw tokens the function
			// returns a table named " " with the real table name in the alias
			// field. Not a bug: detectContext filters first. Worth a subtest
			// below, because it is a trap for the next caller.
			got := a.findTablesInStatement(significantTokens(tokenizeSQL(tc.stmt)))
			if len(got) != len(tc.wantRefs) {
				t.Fatalf("found %+v, want %+v", got, tc.wantRefs)
			}
			for i := range tc.wantRefs {
				if got[i].schema != tc.wantRefs[i].schema || got[i].table != tc.wantRefs[i].table || got[i].alias != tc.wantRefs[i].alias {
					t.Errorf("ref %d is %+v, want %+v", i, got[i], tc.wantRefs[i])
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// selection
// ---------------------------------------------------------------------------

func TestTheSelectionMovesWithinTheFilteredList(t *testing.T) {
	// The selection WRAPS in both directions, the same as the grid's column cursor.
	// The first version asserted it stopped at the ends and reported SelectNext
	// jumping from the last item to 0 — which is not a clamp failing, it is a wrap.
	// Which one it is matters: a clamp means you cannot reach an item by cycling,
	// and a wrap means the whole list is reachable with one key.
	t.Run("next walks the list and wraps at the end", func(t *testing.T) {
		a := openSuggestions(t, "SELECT ", 80)
		n := len(a.filtered)
		if n < 3 {
			t.Fatalf("only %d suggestions; the fixture is too small", n)
		}
		for i := range n - 1 {
			a.SelectNext()
			if a.selected != i+1 {
				t.Fatalf("after %d SelectNext the selection is %d", i+1, a.selected)
			}
		}
		a.SelectNext()
		if a.selected != 0 {
			t.Errorf("SelectNext at the last item moved the selection to %d, want a wrap to 0", a.selected)
		}
	})

	t.Run("previous walks back and wraps at the start", func(t *testing.T) {
		a := openSuggestions(t, "SELECT ", 80)
		a.SelectNext()
		a.SelectPrev()
		if a.selected != 0 {
			t.Errorf("SelectPrev from 1 left the selection at %d, want 0", a.selected)
		}
		n := len(a.filtered)
		a.SelectPrev()
		if a.selected != n-1 {
			t.Errorf("SelectPrev at the first item moved the selection to %d, want a wrap to %d", a.selected, n-1)
		}
	})

	t.Run("SelectedItem names what is selected", func(t *testing.T) {
		a := openSuggestions(t, "SELECT ", 80)
		first := a.SelectedItem()
		if first == nil {
			t.Fatal("SelectedItem is nil with suggestions showing")
		}
		want := first.Label()
		a.SelectNext()
		got := a.SelectedItem()
		if got == nil {
			t.Fatal("SelectedItem is nil after moving")
		}
		if got.Label() == want {
			t.Errorf("SelectedItem is still %q after moving the selection", got.Label())
		}
	})

	t.Run("SelectedItem is nil with nothing to select", func(t *testing.T) {
		// nil and not a zero item: a caller that does `if item := SelectedItem(); item != nil`
		// would otherwise get a zero item and read its name as the empty string.
		a := newTestAutocomplete()
		if got := a.SelectedItem(); got != nil {
			t.Errorf("SelectedItem on a hidden popup is %+v, want nil", got)
		}
	})
}

// ---------------------------------------------------------------------------
// Render
// ---------------------------------------------------------------------------

// openSuggestions is a popup with the cursor where it would be after typing `line`.
//
// Built the way the editor builds it — detectContext then UpdateContext — rather than by
// reaching into the state, because the two are separate decisions and a test that skipped
// the first would pass on a popup the app can never produce. `width` is the pane width,
// which Render is given; the state itself holds no width.
func openSuggestions(t *testing.T, line string, width int) *AutocompleteState {
	t.Helper()
	if width <= 0 {
		t.Fatal("the fixture needs a positive width")
	}
	a := newTestAutocomplete()
	a.UpdateContext(a.detectContext(line, len(line)))
	if !a.Visible() {
		t.Fatalf("no popup opened for %q", line)
	}
	return a
}

func TestRenderFitsItsContents(t *testing.T) {
	t.Run("the popup is never wider than the pane", func(t *testing.T) {
		// popupW is clamped to width-4. Past that the popup's border sits off the
		// right edge and the names are cut mid-character by the terminal.
		for _, width := range []int{20, 30, 40, 60, 100, 200} {
			a := openSuggestions(t, "SELECT ", width)
			out := a.Render(theme.Resolve("dark").Styles(), width)
			if out == "" {
				t.Errorf("at width %d the popup rendered nothing", width)
				continue
			}
			for i, line := range strings.Split(out, "\n") {
				if w := lipgloss.Width(line); w > width {
					t.Errorf("at width %d line %d is %d columns wide:\n%q", width, i, w, line)
				}
			}
		}
	})

	t.Run("the popup has at least one item in it", func(t *testing.T) {
		a := openSuggestions(t, "SELECT ", 80)
		out := a.Render(theme.Resolve("dark").Styles(), 80)
		if len(a.filtered) == 0 {
			t.Fatal("the fixture has no suggestions")
		}
		if !strings.Contains(stripEscape(out), a.filtered[0].Label()) {
			t.Errorf("the popup does not show the first suggestion %q:\n%s", a.filtered[0].Label(), out)
		}
	})

	t.Run("a hidden popup renders nothing", func(t *testing.T) {
		a := newTestAutocomplete()
		if got := a.Render(theme.Resolve("dark").Styles(), 80); got != "" {
			t.Errorf("a hidden popup rendered %q", got)
		}
	})

	t.Run("the selected item is ALWAYS visible", func(t *testing.T) {
		// The one that matters most. maxItems caps how many rows the popup has, so a
		// selection below the window needs the list scrolled — and the scroll is
		// computed in three separate steps in Render. If any of them is off by one,
		// the popup is open with no highlighted line in it, and the user cannot see
		// what Enter will accept.
		for _, width := range []int{40, 80, 120} {
			a := newSuggestionsFor(t, width)
			n := len(a.filtered)
			if n <= a.maxItems {
				t.Fatalf("only %d suggestions with a window of %d; the scroll is not reachable", n, a.maxItems)
			}
			styles := theme.Resolve("dark").Styles()

			for _, sel := range []int{a.maxItems, a.maxItems + 1, n - 1, n / 2} {
				if sel >= n {
					continue
				}
				a.selected = sel
				out := a.Render(styles, width)
				lines := strings.Split(stripEscape(out), "\n")
				if len(lines) == 0 {
					t.Fatalf("width %d selection %d rendered nothing", width, sel)
				}
				// The selected line is the one carrying the highlight marker, and it
				// must be inside the popup.
				found := false
				for _, line := range lines {
					if strings.Contains(line, a.filtered[sel].Label()) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("at width %d with the selection on item %d of %d (%q) the popup does not show it:\n%s",
						width, sel, n, a.filtered[sel].Label(), out)
				}
			}
		}
	})

	t.Run("every line of the popup is the same width", func(t *testing.T) {
		// A ragged popup looks broken, and it means one of the two paddings (the
		// name and the kind) was skipped for some item.
		a := openSuggestions(t, "SELECT ", 80)
		styles := theme.Resolve("dark").Styles()
		lines := strings.Split(stripEscape(a.Render(styles, 80)), "\n")
		if len(lines) < 2 {
			t.Skip("the popup has fewer than two lines")
		}
		first := lipgloss.Width(lines[0])
		for i, line := range lines {
			if w := lipgloss.Width(line); w != first {
				t.Errorf("line %d is %d columns and line 0 is %d", i, w, first)
			}
		}
	})

	t.Run("a very long name is truncated with a tail and does not push the popup wide", func(t *testing.T) {
		// A column named after a long expression is ordinary in a database with
		// generated columns, so the truncation path is not exotic.
		a := newTestAutocomplete()
		a.SetKeywords(sqlKeywords, sqlFunctions)
		a.LoadSchema(&aiContext.SchemaExport{Schemas: []aiContext.SchemaInfo{{
			Name: "public",
			Tables: []aiContext.TableInfo{{
				Name:    "wide",
				Columns: []aiContext.ColumnInfo{{Name: "a_generated_column_name_that_is_considerably_longer_than_the_popup_is_wide", DataType: "text"}},
			}},
		}}})
		// In a WHERE clause, not right after SELECT: the SelectList context with no
		// table offers keywords and functions, so the column this fixture is about
		// is never in the list and the truncation path is not reached. The first
		// version used "SELECT " and asserted the truncation without ever having a
		// long name to truncate.
		a.UpdateContext(a.detectContext("SELECT * FROM wide WHERE ", 26))
		if !a.Visible() {
			t.Fatal("no popup")
		}
		found := false
		for _, item := range a.filtered {
			if strings.HasPrefix(item.Label(), "a_generated") {
				found = true
			}
		}
		if !found {
			t.Fatalf("the long column is not among the %d suggestions: %v", len(a.filtered), suggestionNames(a))
		}
		styles := theme.Resolve("dark").Styles()
		out := a.Render(styles, 60)
		for i, line := range strings.Split(out, "\n") {
			if w := lipgloss.Width(line); w > 60 {
				t.Errorf("line %d is %d columns in a 60-column pane:\n%s", i, w, line)
			}
		}
		// The name is cut, not dropped.
		if !strings.Contains(stripEscape(out), "..") {
			t.Errorf("the long name was not marked as truncated:\n%s", out)
		}
	})
}

// ---------------------------------------------------------------------------

// stripEscape removes the SGR sequences so an assertion about text is not an assertion
// about colour.
func stripEscape(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			in = true
		case in && r == 'm':
			in = false
		case in:
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// newSuggestionsFor is a popup with more suggestions than fit in its window, so the
// scroll in Render is reachable.
func newSuggestionsFor(t *testing.T, width int) *AutocompleteState {
	t.Helper()
	a := newTestAutocomplete()
	// A cursor right after SELECT keeps every item: the shortest context matches the
	// most, which is what puts the selection below the window.
	a.UpdateContext(a.detectContext("SELECT ", 7))
	if !a.Visible() {
		t.Fatal("no popup opened")
	}
	if len(a.filtered) <= a.maxItems {
		t.Fatalf("only %d suggestions for a window of %d; this fixture cannot reach the scroll",
			len(a.filtered), a.maxItems)
	}
	return a
}
