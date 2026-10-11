package editor

// Scenario: Los supervivientes aritméticos del sweep no-Boundary de autocomplete.go.
//
// Every test in this file exists because a specific mutant of autocomplete.go survived a
// full mutation run. They come in two shapes:
//
//   - Context leaks: rebuildContextItems assembles the candidate list per context kind, and
//     the keyword/function arm is guarded by `Kind == Keyword || Kind == Function`. A
//     negated guard sweeps tables, columns and schemas into a keyword context, and the user
//     then sees a column where a keyword belongs.
//   - Render geometry: popupW, the window arithmetic and the two paddings are four small
//     formulas that all fail SILENTLY — the popup is still a box, it is just the wrong
//     width, shows the wrong rows, or wraps into a staircase. The border's Width() pads
//     short rows, so several off-by-ones are invisible and the assertions here are about
//     the things that ARE visible: total width, row count, one selection marker.

import (
	"strconv"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/buble/dbx/internal/theme"
)

// ---------------------------------------------------------------------------
// rebuildContextItems: the keyword/function arm must not leak other kinds
// ---------------------------------------------------------------------------

// Scenario: En un contexto de palabra clave sólo se ofrecen palabras clave y funciones.
//
// Kills CONDITIONALS_NEGATION 278 (`item.Kind == CompletionKeyword || …` -> `!=`): the
// first conjunct negated turns the filter into "append everything", so the table `orders`
// would be offered after "GROUP o" — a prefix that has nothing to do with the clause.
func TestAKeywordContextOffersOnlyKeywordsAndFunctions(t *testing.T) {
	a := newTestAutocomplete()
	a.UpdateContext(a.detectContext("SELECT * FROM users GROUP o", len("SELECT * FROM users GROUP o")))
	if !a.Visible() {
		t.Fatal("no popup opened")
	}
	if hasName(suggestionNames(a), "orders") {
		t.Fatalf("the table orders leaked into a keyword context: %v", suggestionNames(a))
	}
	if !hasName(suggestionNames(a), "ON") {
		t.Fatalf("the keyword ON should match the prefix o: %v", suggestionNames(a))
	}
}

// Scenario: Alcanzar una palabra clave desde una cláusula no arrastra columns.
//
// Kills CONDITIONALS_NEGATION 291 (same negation in the "keyword reachable from any
// clause" block): after "FROM u" the block appends keywords so UNION/UPDATE stay
// reachable; negated it appends the whole catalog, and the column user_id prefix-matches.
func TestKeywordReachFromAClauseDoesNotLeakColumns(t *testing.T) {
	a := newTestAutocomplete()
	a.UpdateContext(a.detectContext("SELECT * FROM u", len("SELECT * FROM u")))
	if !a.Visible() {
		t.Fatal("no popup opened")
	}
	if hasName(suggestionNames(a), "user_id") {
		t.Fatalf("the column user_id leaked into a table context: %v", suggestionNames(a))
	}
	if !hasName(suggestionNames(a), "users") {
		t.Fatalf("the table users should match the prefix u: %v", suggestionNames(a))
	}
}

// ---------------------------------------------------------------------------
// columnItems guard
// ---------------------------------------------------------------------------

// Scenario: Un calificador que el catálogo no puede resolver ofrece sus columns.
//
// Kills CONDITIONALS_NEGATION 324 col 35 (`ctx.table == ""` -> `!=`): the guard returns
// NOTHING when no table is in play, and the negated guard kills the case where a table IS
// in play without a schema — the fallback of qualifiedContext, which treats an unknown
// qualifier as an unqualified table on purpose. The fixture appends a column whose table
// has no table ITEM, which is the only shape of catalog where that fallback is reachable
// with a result: "SELECT phantom." cannot resolve phantom, and its column must still be
// offered. (resolveTable falls back to the catalog itself, so a real table like users
// always arrives here with a schema and cannot tell the two guards apart.)
func TestAnUnreachableQualifierStillOffersItsColumns(t *testing.T) {
	a := newTestAutocomplete()
	a.all = append(a.all, CompletionItem{
		Name: "phantom_col", Kind: CompletionColumn, Table: "phantom", Detail: "text",
	})
	line := "SELECT phantom."
	a.UpdateContext(a.detectContext(line, len(line)))
	if !a.Visible() {
		t.Fatal("no popup opened for a qualifier that owns columns")
	}
	if !hasName(suggestionNames(a), "phantom_col") {
		t.Fatalf("the column of the unresolved qualifier is missing: %v", suggestionNames(a))
	}
}

// ---------------------------------------------------------------------------
// dedupeItems
// ---------------------------------------------------------------------------

// Scenario: La deduplicación colapsa nombres repetidos de la misma clase.
//
// Kills CONDITIONALS_NEGATION 381 (`len(items) == 0` -> `!=`): negated, the early return
// happens only for the NON-empty case, so duplicates pass through untouched.
func TestDedupeItemsCollapsesRepeatedNames(t *testing.T) {
	got := dedupeItems([]CompletionItem{
		{Name: "SELECT", Kind: CompletionKeyword},
		{Name: "select", Kind: CompletionKeyword},
		{Name: "COUNT", Kind: CompletionFunction},
	})
	if len(got) != 2 {
		t.Fatalf("dedupeItems returned %d items, want 2: %v", len(got), got)
	}
}

// ---------------------------------------------------------------------------
// statement truncation at the last semicolon
// ---------------------------------------------------------------------------

// Scenario: Una sentencia que empieza en punto y coma no rompe el análisis.
//
// Kills ARITHMETIC_BASE 486 (`tokens[idx+1:]` -> `idx-1:`): with the semicolon as the
// FIRST token the index is 0 and `tokens[-1:]` panics. The dead code keeps the tokens
// after the semicolon — here just SELECT — and offers keywords for it.
func TestAStatementStartingWithASemicolonIsAnalysed(t *testing.T) {
	a := newTestAutocomplete()
	line := "; SELECT"
	a.UpdateContext(a.detectContext(line, len(line))) // must not panic
	// The dead code keeps the token after the semicolon — SELECT itself — and the filter
	// drops the keyword the user already typed, so there is nothing left to suggest.
	if a.Visible() {
		t.Fatalf("typing SELECT after a semicolon restates it: %v", suggestionNames(a))
	}
}

// ---------------------------------------------------------------------------
// hasTablesInSchema
// ---------------------------------------------------------------------------

// Scenario: Un esquema que sólo tiene tablas — sin ítem de esquema en el catálogo — se
//
//	reconoce como esquema al calificar.
//
// Kills CONDITIONALS_NEGATION 620 (`item.Kind == CompletionTable` -> `!=`): negated, the
// scan only matches NON-table items carrying the schema name, which never happens, so
// "audit." falls through to an unqualified-column context and offers nothing. The fixture
// appends a table whose schema has no schema item of its own, which is what LoadSchema
// cannot express through the shared export.
func TestATablesOnlySchemaIsStillAQualifier(t *testing.T) {
	a := newTestAutocomplete()
	a.all = append(a.all, CompletionItem{Name: "payments", Kind: CompletionTable, Schema: "audit"})
	line := "SELECT * FROM audit."
	a.UpdateContext(a.detectContext(line, len(line)))
	if !a.Visible() {
		t.Fatal("no popup opened after audit.")
	}
	if !hasName(suggestionNames(a), "payments") {
		t.Fatalf("the table audit.payments is missing: %v", suggestionNames(a))
	}
}

// ---------------------------------------------------------------------------
// Render geometry
//
// All four fixtures below build the state directly, like TestThePopupScrollsToTheSelection
// does: Render reads visible + filtered + selected, and a context-driven fixture cannot
// pin the label lengths these formulas are about.
// ---------------------------------------------------------------------------

// suggestionsOf builds a popup whose labels are exactly the given widths.
func suggestionsOf(widths ...int) *AutocompleteState {
	a := NewAutocompleteState()
	a.visible = true
	for _, w := range widths {
		a.filtered = append(a.filtered, CompletionItem{
			Kind:   CompletionColumn,
			Detail: "text",
			Name:   strings.Repeat("n", w),
		})
	}
	return a
}

// longSuggestions is a list past the window, for the scroll arithmetic.
func longSuggestions(n int) *AutocompleteState {
	a := NewAutocompleteState()
	a.visible = true
	for i := range n {
		a.filtered = append(a.filtered, CompletionItem{
			Kind:   CompletionColumn,
			Detail: "text",
			Name:   "col_" + strconv.Itoa(i),
		})
	}
	return a
}

// Scenario: El ancho del popup sigue al etiqueta más larga, con su mínimo.
//
// Kills ARITHMETIC_BASE 876 (`maxNameLen + 25` -> `- 25`, floor keeps 40), CONDITIONALS_
// NEGATION 871 (`l > maxNameLen` -> `<=`, the maximum never grows and every popup is the
// minimum) and CONDITIONALS_NEGATION 877 (`popupW < 40` -> `>=`, a wide popup is clamped
// DOWN to the minimum). All three land on the same observable: a 20-character label wants
// popupW = 45, the border's Width() makes every line exactly 45 columns, and no variant
// of those three formulas can produce 45.
func TestThePopupWidthFollowsTheLongestLabel(t *testing.T) {
	a := suggestionsOf(20)
	const wantW = 20 + 25
	styles := theme.Resolve("dark").Styles()
	for i, line := range strings.Split(a.Render(styles, 200), "\n") {
		if w := lipgloss.Width(line); w != wantW {
			t.Errorf("line %d is %d columns wide, want %d:\n%s", i, w, wantW, a.Render(styles, 200))
		}
	}
}

// Scenario: La ventana de filas nunca excede maxItems.
//
// Kills CONDITIONALS_NEGATION 887 (`end > a.maxItems` -> `<=`): negated, a list of 60 is
// NOT capped to 15 and every row is rendered. The panic an oversized end could cause is
// caught by the third clamp (end > len), which is exactly why this survives — the only
// observable is the row count.
func TestTheWindowNeverShowsMoreThanMaxItemsRows(t *testing.T) {
	a := longSuggestions(60)
	styles := theme.Resolve("dark").Styles()
	lines := strings.Split(a.Render(styles, 200), "\n")
	// 15 rows + the scroll counter (which wraps to its own line: a full-width row plus
	// "  1/60" is wider than the popup) + the top and bottom border. An uncapped window
	// would render all 60 rows instead.
	if want := a.maxItems + 3; len(lines) != want {
		t.Errorf("the popup has %d lines, want %d (a window of %d rows plus the counter and the border):\n%s",
			len(lines), want, a.maxItems, a.Render(styles, 200))
	}
}

// Scenario: Una selección más allá del final de una lista corta muestra la lista entera.
//
// Kills ARITHMETIC_BASE and INVERT_NEGATIVES 899 (`start = end - a.maxItems` -> `+`):
// start jumps past end, the row loop draws nothing, and the surviving assertions only
// check that A box exists. The rows themselves are the observable.
func TestASelectionPastTheEndStillShowsTheRows(t *testing.T) {
	a := suggestionsOf(2, 4, 6)
	a.selected = 12 // as if the selection was reset on a shorter list
	styles := theme.Resolve("dark").Styles()
	out := stripEscape(a.Render(styles, 200))
	if !strings.Contains(out, "nn") {
		t.Errorf("the rows vanished with the selection past the end:\n%s", out)
	}
}

// Scenario: Cada fila cabe en el ancho del popup, sin envolver.
//
// Kills the four 910 mutants (`popupW - kindW - 5`, both tokens, each to `+`: nameMaxW
// grows past the popup) and the two 919 mutants (`nameMaxW - nameW` -> `+`: the padding
// doubles the name). A row wider than popupW is hard-WRAPPED by the border's Width(), so
// the popup turns into a staircase — the line count is the observable, and the popup's
// own arithmetic (3 + nameMaxW + kindW = popupW - 2) is what the assertion pins.
func TestEveryRowFitsThePopupWithoutWrapping(t *testing.T) {
	a := suggestionsOf(2, 4, 6)
	styles := theme.Resolve("dark").Styles()
	out := a.Render(styles, 200)
	if want := len(a.filtered) + 2; len(strings.Split(out, "\n")) != want {
		t.Errorf("the popup has %d lines, want %d (one per row plus the border):\n%s",
			len(strings.Split(out, "\n")), want, out)
	}
}

// Scenario: Exactamente una fila lleva el marcador de selección.
//
// Kills CONDITIONALS_NEGATION 957 (`i == a.selected` -> `!=`): the marker moves to every
// row EXCEPT the selected one, and a popup with two highlighted lines reads as broken.
func TestOnlyTheSelectedRowIsMarked(t *testing.T) {
	a := suggestionsOf(2, 4, 6)
	a.selected = 1
	styles := theme.Resolve("dark").Styles()
	if got := strings.Count(stripEscape(a.Render(styles, 200)), "▸"); got != 1 {
		t.Errorf("the popup shows %d selection markers, want exactly 1:\n%s", got, a.Render(styles, 200))
	}
}

// Scenario: El contador de scroll sólo aparece cuando la lista no cabe, y cuenta desde 1.
//
// Kills CONDITIONALS_NEGATION 967 (`len(a.filtered) > a.maxItems` -> `<=`: the counter is
// shown for a list that fits and hidden for one that does not) and ARITHMETIC_BASE 969
// (`a.selected + 1` -> `-`: a selection on item 3 reads as 1). The fixture names carry no
// slash, so a "/" in the render can only be the counter.
func TestScrollInfoAppearsOnlyForLongListsAndCountsFromOne(t *testing.T) {
	styles := theme.Resolve("dark").Styles()

	short := suggestionsOf(2, 4, 6)
	if out := stripEscape(short.Render(styles, 200)); strings.Contains(out, "/") {
		t.Errorf("a list that fits shows scroll info:\n%s", out)
	}

	long := longSuggestions(60)
	long.selected = 2
	out := stripEscape(long.Render(styles, 200))
	if !strings.Contains(out, "3/60") {
		t.Errorf("with item 3 of 60 selected the counter is missing or wrong:\n%s", out)
	}
}
