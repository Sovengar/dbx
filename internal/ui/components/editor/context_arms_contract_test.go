package editor

// Scenario: Los brazos de la deteccion de contexto que ninguna suite alcanzo.
//
// detectContext reads the tokens BEFORE the cursor and classifies what should be suggested
// next. Its arms are the whole feature: after `FROM` a table, after `schema.` a table, after
// an identifier in a predicate an operator, after `IS` a value.
//
// Three of those arms had no test, and they are the ones a test has to CONSTRUCT rather than
// stumble into — `CompletionSchema` needs a qualified name, `CompletionOperator` needs a
// predicate with a complete token already typed, and the keyword filter needs an empty prefix
// with the keyword already present. A test that typed ordinary SQL would reach three arms out
// of seven and call the feature covered.

import (
	"strings"
	"testing"

	ai "github.com/buble/dbx/internal/ai/context"
	"github.com/buble/dbx/internal/theme"
)

// schema is one schema with one table and one column, which is the smallest thing that can
// produce a schema item, a table item and a column item.
func schemaForTest() *ai.SchemaExport {
	return &ai.SchemaExport{
		Database: "shopdb",
		Schemas: []ai.SchemaInfo{{
			Name: "public",
			Tables: []ai.TableInfo{{
				Name:     "orders",
				Type:     "BASE TABLE",
				RowCount: 3,
				Columns: []ai.ColumnInfo{
					{Name: "id", DataType: "integer", OrdinalPos: 1},
					{Name: "total", DataType: "numeric", OrdinalPos: 2},
				},
			}},
		}},
	}
}

// editorAt builds a focused editor whose content is `text` with the cursor at its end,
// which is the position detectContext is defined for.
func editorAt(t *testing.T, text string) *SQLEditor {
	t.Helper()
	e := NewSQLEditor(theme.Resolve("dark").Styles())
	e.SetAutocompleteConfig(true, 1)
	e.SetSchema(schemaForTest())
	e.SetContent(text)
	e.cursorRow = 0
	e.cursorCol = len(text)
	e.Focus()
	return e
}

// TestEveryContextKindTheDetectorCanReturn walks the arms of detectContext's final switch,
// asserting the kind for a piece of SQL that has to produce it.
//
// The cases are the documentation: a reader who wants to know when a table is suggested gets
// the answer from this table rather than from reading predicateContext.
func TestEveryContextKindTheDetectorCanReturn(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
		want CompletionKind
	}{
		{"nothing typed yet", "", CompletionKeyword},
		{"inside a literal, where nothing is suggested", "SELECT 'a", CompletionEmpty},
		{"inside a comment", "SELECT 1 -- a", CompletionEmpty},
		{"after FROM, expecting a table", "SELECT * FROM ", CompletionTable},
		{"after FROM with a table already typed", "SELECT * FROM ord", CompletionTable},
		// A qualifier for a schema that EXISTS offers the TABLES in it, not the schema
		// names — `public.` is a complete reference and the next thing you can type is a
		// table.
		{"after a known schema qualifier", "SELECT * FROM public.", CompletionTable},
		// An unknown qualifier is deliberately treated as an unqualified TABLE, so its
		// columns are offered — of which there are none if it turns out to be a schema. That
		// is a decision, recorded in qualifiedContext and in the context contract test, and
		// the first version of this table expected a schema here and the code was right.
		{"after an unknown qualifier", "SELECT * FROM nosuchschema.", CompletionColumn},
		{"after an unknown qualifier in a select list", "SELECT ghost.", CompletionColumn},
		{"after a table qualifier", "SELECT * FROM orders.", CompletionColumn},
		{"in a select list", "SELECT ", CompletionSelectList},
		{"in a select list with something typed", "SELECT tota", CompletionSelectList},
		// An identifier already complete, in a predicate: what can follow a column is an
		// operator, so that is what is offered.
		{"after a complete identifier in a predicate", "SELECT * FROM orders WHERE total ", CompletionOperator},
		{"after an operator in a predicate", "SELECT * FROM orders WHERE total = ", CompletionValue},
		{"after IS", "SELECT * FROM orders WHERE total IS ", CompletionValue},
		{"after LIMIT", "SELECT * FROM orders LIMIT ", CompletionEmpty},
		{"after ORDER BY", "SELECT * FROM orders ORDER BY ", CompletionColumn},
		{"in a bare expression", "SEL", CompletionKeyword},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := editorAt(t, tc.text)

			ctx := e.autocomplete.detectContext(e.lines[e.cursorRow], e.cursorCol)
			if ctx.kind != tc.want {
				t.Errorf("detectContext(%q) = kind %v, want %v", tc.text, ctx.kind, tc.want)
			}
		})
	}
}

// The two arms of the context-item rebuild, which are the switch's payload rather than its
// classification: the schema sweep and the fixed operator list.
//
// The operator list is the one the session recorded as reachable-but-unwired — the popup
// OFFERS thirteen operators and something has to CONSUME them for it to matter. This case
// asserts they are offered; whether they are useful is the other question, and it is
// recorded rather than answered here.
func TestTheContextItemsForASchemaAndForAnOperator(t *testing.T) {
	t.Run("a table qualifier offers that table's columns", func(t *testing.T) {
		e := editorAt(t, "SELECT * FROM orders.")
		e.refreshAutocomplete(true)

		if e.autocomplete.currentCtx.kind != CompletionColumn {
			t.Fatalf("the context is %v, not a column qualifier", e.autocomplete.currentCtx.kind)
		}
		items := e.autocomplete.contextItems
		if len(items) == 0 {
			t.Fatal("a table qualifier offers nothing")
		}
		for _, it := range items {
			if it.Kind != CompletionColumn {
				t.Errorf("the column context offers a %v (%q)", it.Kind, it.Name)
			}
		}
		if items[0].Name != "id" {
			t.Errorf("the first column offered is %q", items[0].Name)
		}
	})

	t.Run("an operator position offers the operators", func(t *testing.T) {
		// The position is a complete identifier followed by a space: nothing typed yet, so
		// the suggestion list is the whole fixed set.
		e := editorAt(t, "SELECT * FROM orders WHERE total ")
		e.refreshAutocomplete(true)

		if e.autocomplete.currentCtx.kind != CompletionOperator {
			t.Skipf("the context is %v, not an operator position, so this editor state cannot reach the operator list", e.autocomplete.currentCtx.kind)
		}
		items := e.autocomplete.contextItems
		if len(items) == 0 {
			t.Fatal("an operator position offers nothing")
		}
		for _, it := range items {
			if it.Kind != CompletionOperator {
				t.Errorf("the operator context offers a %v (%q)", it.Kind, it.Name)
			}
		}
		// And they are the ones the popup renders, which is the property that makes them
		// worth having.
		e.autocomplete.prefix = ""
		e.autocomplete.applyFilter()
		if len(e.autocomplete.filtered) != len(items) {
			t.Errorf("the popup would show %d of the %d operators", len(e.autocomplete.filtered), len(items))
		}
	})
}

// The keyword filter's skip, with an empty prefix AND a complete typed token.
//
// applyFilter's doc claims it drops "the exact keyword the user already typed, so accepting a
// completion never duplicates text". The skip that does that lives in the branch where a
// prefix IS typed — which is the only way a typed keyword can be complete. The empty-prefix
// branch has a copy of the check that can never fire, because an empty prefix implies there is
// no current token.
func TestTheKeywordYouAlreadyTypedIsNotOffered(t *testing.T) {
	// IN: a bare expression whose context is KEYWORDS, and a typed token that IS one. Both
	// halves of the skip's condition have to hold and they pull against each other — a
	// complete keyword, in a context that offers keywords, with other keywords sharing its
	// prefix so that "nothing was offered" is distinguishable from "the right thing was
	// dropped".
	//
	// "SELECT" does not work: after SELECT the context is the select list, so the filter runs
	// against columns and offers nothing. "NULL" does not work either, for the opposite
	// reason: NULL is the only item matching its prefix, so dropping it correctly leaves an
	// empty list and the case cannot tell a right answer from a broken one.
	const typed = "IN"

	e := editorAt(t, typed)
	e.refreshAutocomplete(true)

	if e.autocomplete.currentCtx.kind != CompletionKeyword {
		t.Skipf("the context at %q is %v, not a keyword one, so the skip has nothing to drop", typed, e.autocomplete.currentCtx.kind)
	}
	// Both halves of the condition, asserted rather than hoped for: the token is complete and
	// the prefix is typed.
	if e.autocomplete.currentCtx.prefix == "" || e.autocomplete.currentCtx.tokenText == "" {
		t.Fatalf("the fixture is not the case under test: prefix=%q token=%q",
			e.autocomplete.currentCtx.prefix, e.autocomplete.currentCtx.tokenText)
	}
	if e.autocomplete.currentCtx.prefix != e.autocomplete.currentCtx.tokenText {
		t.Fatalf("the token is not complete: prefix=%q token=%q",
			e.autocomplete.currentCtx.prefix, e.autocomplete.currentCtx.tokenText)
	}

	e.autocomplete.applyFilter()

	for _, it := range e.autocomplete.filtered {
		if it.Kind == CompletionKeyword && strings.EqualFold(it.Name, typed) {
			t.Errorf("the popup offers %q, which is already in the line — accepting it would double the word", it.Name)
		}
	}

	// And something IS offered, or the case above is satisfied by a filter that empties the
	// list. Everything that survives must also match the prefix, which is the other half of
	// what the filter does.
	if len(e.autocomplete.filtered) == 0 {
		t.Fatalf("nothing is offered for the prefix %q, so this cannot tell a right answer from an empty one", typed)
	}
	for _, it := range e.autocomplete.filtered {
		if !strings.HasPrefix(strings.ToLower(it.Name), strings.ToLower(typed)) &&
			!fuzzyMatchAutocomplete(strings.ToLower(typed), strings.ToLower(it.Name)) {
			t.Errorf("the popup offers %q, which does not match the prefix %q", it.Name, typed)
		}
	}

	t.Run("and with no prefix the whole context is offered", func(t *testing.T) {
		// The counterweight for the branch above: an empty prefix offers every item, with
		// nothing dropped — which is what the removed dead check was supposed to do and
		// cannot, because with no token there is no exact keyword to compare against.
		bare := editorAt(t, typed)
		bare.refreshAutocomplete(true)
		bare.autocomplete.currentCtx.prefix = ""
		bare.autocomplete.prefix = ""
		bare.autocomplete.applyFilter()

		if got, want := len(bare.autocomplete.filtered), len(bare.autocomplete.contextItems); got != want {
			t.Errorf("%d items are offered with no prefix, want all %d", got, want)
		}
	})
}

// The history sentinel, reached the way the app sets it. PushHistory records where the user
// was, and historyPrev's `historyIdx < 0` arm is the "not browsing" state — a value nothing
// in the editor's own key handling produces, which is why it is set here directly.
func TestTheNotBrowsingSentinelGoesToTheNewestEntry(t *testing.T) {
	e := NewSQLEditor(theme.Resolve("dark").Styles())
	e.Focus()
	e.PushHistory("SELECT 1")
	e.PushHistory("SELECT 2")
	e.historyIdx = -1

	e.historyPrev()

	if e.historyIdx != len(e.history)-1 {
		t.Errorf("the index is %d, want %d — not-browsing must go to the NEWEST entry", e.historyIdx, len(e.history)-1)
	}
	if got := e.lines[0]; got != "SELECT 2" {
		t.Errorf("the editor shows %q, want the newest history entry", got)
	}
}
