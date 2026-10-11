package editor

// Scenario: El sweep de mutación de autocomplete.go.
//
// Of the non-Boundary mutants that survived a whole-module run, three are
// equivalent and are being proposed for .mutation-allowlist with their proofs:
// ARITHMETIC_BASE 174 (a pure capacity hint), CONDITIONALS_NEGATION 324 first
// conjunct (both sides of the guard answer empty), and the ARITHMETIC_BASE /
// INVERT_NEGATIVES twins on 677 (same token substitution, extra loop iterations
// that skip before touching the slice). This file kills the one mutant of the
// set that is observable in this snapshot: the SECOND conjunct of the
// columnItems guard.

import "testing"

// Scenario: A schema-less table still offers its columns.
//
// Kills CONDITIONALS_NEGATION internal/ui/components/editor/autocomplete.go:324
// (second conjunct, `ctx.table == ""` -> `!= ""`). Flipped, the guard of
// columnItems returns nil for exactly the schema-less table state — table set,
// schema empty, no FROM list — and the table's columns vanish from the popup.
// The existing suite never builds that state, so the mutant rides along; the
// sibling contract subtests pin the other guard states directly and this one
// follows their shape.
func TestColumnItemsOffersTheColumnsOfASchemaLessTable(t *testing.T) {
	a := NewAutocompleteState()
	a.all = []CompletionItem{
		{Name: "public", Kind: CompletionSchema},
		{Name: "users", Kind: CompletionTable, Schema: "public"},
		{Name: "id", Kind: CompletionColumn, Detail: "int", Schema: "public", Table: "users"},
	}

	// A bare table with no schema and no FROM list: columnMatches must still
	// match by table name alone, so the column is offered either way the guard
	// is written.
	got := a.columnItems(completionContext{kind: CompletionColumn, table: "users"})
	if len(got) != 1 || got[0].Name != "id" || got[0].Kind != CompletionColumn {
		names := make([]string, len(got))
		for i, item := range got {
			names[i] = item.KindLabel() + " " + item.Name
		}
		t.Errorf("a schema-less table context offers %d items %v, want its one column", len(got), names)
	}
}
