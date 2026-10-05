package editor

// Scenario: Lo que el popup OFRECE, no solo la clase de contexto que el detector calcula.
//
// The other autocomplete test asserts the KIND that detectContext returns for a cursor
// position. That is half the contract, and the half that fails loudly: a wrong kind opens a
// popup full of the wrong things, which the user sees immediately.
//
// This file asserts the other half — the ITEMS the popup ends up holding — because that is
// where a missing arm hides. rebuildContextItems is a switch over the kind, and each arm
// fills the list differently:
//
//	schema   → every schema in the catalogue
//	table    → the tables of that schema
//	column   → the columns of the qualified or referenced tables
//	value    → the literal placeholders
//	keyword  → every keyword and function
//
// An arm that is never taken is not a coverage gap, it is a feature that does not work. So
// the table below names which arms are reachable and which are not, because "uncovered"
// alone does not distinguish the two and the difference matters when deciding what to do
// about it.

import (
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/buble/dbx/internal/theme"
)

// popupAt drives the whole path — detectContext, rebuildContextItems, applyFilter — and
// returns what the popup is holding. Driving detectContext alone, as the other test does,
// cannot reach any of it.
func popupAt(t *testing.T, sql string) *AutocompleteState {
	t.Helper()
	col := strings.Index(sql, "|")
	if col < 0 {
		t.Fatalf("the case has no | cursor marker: %q", sql)
	}
	a := newTestAutocomplete()
	a.currentCtx = a.detectContext(sql[:col]+sql[col+1:], col)
	a.prefix = a.currentCtx.prefix
	a.rebuildContextItems()
	a.applyFilter()
	return a
}

func kindsIn(a *AutocompleteState) map[CompletionKind]int {
	out := map[CompletionKind]int{}
	for _, item := range a.filtered {
		out[item.Kind]++
	}
	return out
}

func TestThePopupOffersTheItemsTheContextAsksFor(t *testing.T) {
	// One case per reachable kind, asserting on what is OFFERED rather than on the kind,
	// because the kind is what the previous file checks and offering nothing for it would
	// be invisible there.
	for _, tc := range []struct {
		name string
		at   string
		// wantKind is the kind detectContext must have decided.
		wantKind CompletionKind
		// wantNamed are items that MUST be offered. A subset on purpose: what matters is
		// that the right KIND of thing is there, not the whole catalogue.
		wantNamed []string
		// wantNoneOf are names from the wrong kind, which must NOT appear even though the
		// fixture's catalogue contains them. This is the half that catches an arm that
		// falls through to "everything".
		wantNoneOf []string
	}{
		{
			name:      "a schema dot offers that schema's TABLES",
			at:        "SELECT * FROM analytics.|",
			wantKind:  CompletionTable,
			wantNamed: []string{"events"},
			// A column of that table is in the catalogue and must not be offered here.
			wantNoneOf: []string{"wh_start"},
		},
		{
			name:      "a table dot offers that table's COLUMNS",
			at:        "SELECT users.| FROM users",
			wantKind:  CompletionColumn,
			wantNamed: []string{"id", "name"},
			// events' column, from the OTHER schema.
			wantNoneOf: []string{"wh_start"},
		},
		{
			name:      "an unknown qualifier offers ITS OWN columns, which is none",
			at:        "SELECT ghost.|",
			wantKind:  CompletionColumn,
			wantNamed: nil,
			// The whole point of the fallback: an unknown word is read as a table name, so
			// the popup is empty rather than dumping the catalogue. Without this arm
			// columnItems answers nil for want of a table in scope, and a user typing
			// `SELECT schema.|` before the schema exists would see everything.
			wantNoneOf: []string{"id", "name", "wh_start", "events", "users"},
		},
		{
			name:      "the select list offers the star only at the start",
			at:        "SELECT | FROM users",
			wantKind:  CompletionSelectList,
			wantNamed: []string{"*", "DISTINCT", "id", "name"},
		},
		{
			name:      "a predicate offers columns",
			at:        "SELECT * FROM users WHERE |",
			wantKind:  CompletionColumn,
			wantNamed: []string{"id", "name"},
		},
		{
			name:     "IS offers the literal placeholders",
			at:       "SELECT * FROM users WHERE id IS |",
			wantKind: CompletionValue,
			// A column is not a value and must not be offered here: completing `id` after
			// IS would produce `WHERE id IS id`.
			wantNoneOf: []string{"name"},
		},
		{
			name:     "BETWEEN offers the literal placeholders too",
			at:       "SELECT * FROM users WHERE id BETWEEN |",
			wantKind: CompletionValue,
		},
		{
			name:       "a keyword offers keywords and functions",
			at:         "SELECT * FROM users WHER|",
			wantKind:   CompletionKeyword,
			wantNamed:  []string{"WHERE"},
			wantNoneOf: []string{"events"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := popupAt(t, tc.at)
			if a.currentCtx.kind != tc.wantKind {
				t.Fatalf("the context at %q is %s, want %s", tc.at, kindName(a.currentCtx.kind), kindName(tc.wantKind))
			}

			names := suggestionNames(a)
			for _, want := range tc.wantNamed {
				if !hasName(names, want) {
					t.Errorf("%q is not offered; the popup has %v", want, names)
				}
			}
			for _, unwanted := range tc.wantNoneOf {
				if hasName(names, unwanted) {
					t.Errorf("%q is offered as a %s, which it is not", unwanted, kindName(a.currentCtx.kind))
				}
			}
			// And the popup must be OPEN: a correct empty list that never becomes visible
			// is the same failure as a wrong one.
			a.visible = true
			if len(a.filtered) == 0 && len(tc.wantNamed) > 0 {
				t.Errorf("the popup is empty for a context that has %d expected items", len(tc.wantNamed))
			}

			// EVERY item is of an allowed kind, which is the assertion the membership
			// checks above cannot make: a popup can offer every name in the table AND a
			// hundred keywords, and both halves pass a "does it contain users" test.
			//
			// The select list is excluded because it deliberately mixes kinds — `*` and
			// DISTINCT are keywords, and the functions beside them are functions.
			if allowed, ok := map[CompletionKind][]CompletionKind{
				CompletionTable:  {CompletionTable},
				CompletionColumn: {CompletionColumn},
				CompletionValue:  {CompletionValue},
			}[tc.wantKind]; ok {
				for kind, n := range kindsIn(a) {
					if !slicesContains(allowed, kind) {
						t.Errorf("the popup offers %d %s item(s) in a %s context", n, kindName(kind), kindName(tc.wantKind))
					}
				}
			}
		})
	}
}

// TestTheKindsWithNoArmsAreNamedHere pins which arms of rebuildContextItems can never be
// taken, because the code says they can.
//
// rebuildContextItems switches over the context kind, and two of its arms — CompletionSchema
// and CompletionOperator — are unreachable: detectContext assigns Empty, Table, SelectList,
// Column, Keyword and Value, and never those two. So:
//
//   - the schema arm scans the whole catalogue for schemas and hands them to a caller that
//     can never ask for them
//
//   - the operator arm feeds `defaultOperators` — thirteen entries with human-readable
//     Detail strings like "equals" and "greater than" — to a caller that can never ask for
//     them. The operator list is built, formatted and never offered.
//
// That second one is a FEATURE that does not work, not dead code in the usual sense: the
// data exists, the dispatch arm exists, and the only thing missing is one clause in
// detectContext. It is recorded here rather than wired because adding it changes what the
// popup shows, which is a product decision rather than a coverage fix. The wiring point is
// the clause switch in detectContext, alongside `ctx.kind = CompletionColumn` for the
// predicate arms.
func TestTheKindsWithNoArmsAreNamedHere(t *testing.T) {
	assigned := map[CompletionKind]bool{}
	for _, sql := range []string{
		"SELECT | FROM users", "SELECT * FROM users WHERE |", "SELECT * FROM users WHERE a = 1 AND |",
		"SELECT * FROM users WHERE id IS |", "SELECT * FROM users LIMIT |",
		"SELECT * FROM analytics.|", "SELECT users.|", "SELECT * FROM users WHER|",
		"SELECT |", "SELECT a AS | FROM t", "SELECT * FROM users WHERE a IN |",
		"SELECT DISTINCT | FROM users", "SELECT a FROM t GROUP BY a HAVING |",
		"INSERT INTO |", "UPDATE |", "CREATE TABLE |", "DROP TABLE |", "ALTER TABLE |",
		"TRUNCATE TABLE |", "SELECT * FROM a JOIN b ON |",
	} {
		a := newTestAutocomplete()
		col := strings.Index(sql, "|")
		ctx := a.detectContext(sql[:col]+sql[col+1:], col)
		assigned[ctx.kind] = true
	}

	// The two kinds the switch has arms for and the detector cannot produce.
	for _, unreachable := range []CompletionKind{CompletionSchema, CompletionOperator} {
		if assigned[unreachable] {
			t.Errorf("%s IS reachable from a statement, so this note is out of date and the arm should be tested instead", kindName(unreachable))
		}
	}

	// And the ones it does produce, so the note above cannot rot by omission: if a new kind
	// starts being assigned, the set below is what has to grow with it.
	for _, reachable := range []CompletionKind{
		CompletionEmpty, CompletionTable, CompletionSelectList,
		CompletionColumn, CompletionKeyword, CompletionValue,
	} {
		if !assigned[reachable] {
			t.Errorf("no statement in the list above produces %s, so either the list is out of date or the arm is dead", kindName(reachable))
		}
	}

	// The operator items really are there, which is what makes the dead arm a missing
	// feature rather than dead code. Thirteen of them, each with a description.
	if len(defaultOperators) < 5 {
		t.Errorf("the operator list holds %d entries, so it is not the populated list the note describes", len(defaultOperators))
	}
	for _, op := range defaultOperators {
		if op.Kind != CompletionOperator {
			t.Errorf("the operator %q is not an operator", op.Name)
		}
		if op.Detail == "" {
			t.Errorf("the operator %q has no description", op.Name)
		}
	}
}

// TestTheColumnFilterIsCaseInsensitive covers the dedup key's own folding, which is the
// one place the popup compares names rather than filtering them.
//
// The catalogue can hold the same column twice under different case — `ID` from one query,
// `id` from another — and offering both would make the cursor jump when one is chosen,
// because the completion replaces a token with a name that differs only in case.
func TestTheColumnFilterIsCaseInsensitive(t *testing.T) {
	a := NewAutocompleteState()
	a.all = []CompletionItem{
		{Kind: CompletionColumn, Name: "user_id", Schema: "public", Table: "orders"},
		{Kind: CompletionColumn, Name: "User_ID", Schema: "Public", Table: "Orders"},
	}

	got := a.columnItems(completionContext{kind: CompletionColumn, schema: "public", table: "orders"})
	if len(got) != 1 {
		names := make([]string, len(got))
		for i, item := range got {
			names[i] = item.Name
		}
		t.Errorf("the same column in two cases produced %d items (%v), want 1", len(got), names)
	}
}

// TestAColumnOutsideTheReferencedTablesIsNotOffered is the negative of the FROM-list branch
// of columnMatches, and it is the branch that keeps one table's column out of another
// table's completion.
func TestAColumnOutsideTheReferencedTablesIsNotOffered(t *testing.T) {
	a := NewAutocompleteState()
	a.all = []CompletionItem{
		{Kind: CompletionColumn, Name: "name", Schema: "public", Table: "users"},
		{Kind: CompletionColumn, Name: "wh_start", Schema: "analytics", Table: "events"},
	}

	t.Run("a referenced table's column is offered", func(t *testing.T) {
		got := a.columnItems(completionContext{
			kind:   CompletionColumn,
			tables: []tableRef{{table: "users", schema: "public"}},
		})
		if !hasName(suggestionNames2(got), "name") {
			t.Errorf("users' own column is not offered: %v", suggestionNames2(got))
		}
	})

	t.Run("an unreferenced table's column is NOT", func(t *testing.T) {
		// The case that matters: `SELECT * FROM users WHERE |` must not offer events' column
		// just because it is in the catalogue. Offering it produces a query against a table
		// the statement never mentioned.
		got := a.columnItems(completionContext{
			kind:   CompletionColumn,
			tables: []tableRef{{table: "users", schema: "public"}},
		})
		if hasName(suggestionNames2(got), "wh_start") {
			t.Errorf("a column of an unreferenced table was offered: %v", suggestionNames2(got))
		}
	})

	t.Run("a SCHEMA in the reference still matches a table without one in the catalogue", func(t *testing.T) {
		// The `ref.schema == "" ||` half of the condition: an unqualified reference accepts
		// the column whatever schema the catalogue says it is in, because the statement did
		// not say and guessing wrong would hide a column that IS right.
		got := a.columnItems(completionContext{
			kind:   CompletionColumn,
			tables: []tableRef{{table: "events"}},
		})
		if !hasName(suggestionNames2(got), "wh_start") {
			t.Errorf("an unqualified reference did not match the column: %v", suggestionNames2(got))
		}
	})

	t.Run("a WRONG schema on the reference excludes the column", func(t *testing.T) {
		got := a.columnItems(completionContext{
			kind:   CompletionColumn,
			tables: []tableRef{{table: "events", schema: "public"}},
		})
		if hasName(suggestionNames2(got), "wh_start") {
			t.Errorf("a column from another schema matched a reference that named one: %v", suggestionNames2(got))
		}
	})
}

// TestDetectContextClampsAColumnOutsideTheLine is the setter's contract at the detector.
// The app always passes a real offset, so both clamps are for a caller that does not —
// which is exactly the kind of guard that gets deleted as unreachable and then needed.
func TestDetectContextClampsAColumnOutsideTheLine(t *testing.T) {
	a := newTestAutocomplete()
	const line = "SELECT * FROM users"

	for _, col := range []int{-1, -1000} {
		ctx := a.detectContext(line, col)
		if ctx.tokenStart < 0 || ctx.tokenEnd < 0 {
			t.Errorf("a column of %d produced token bounds %d..%d, want both non-negative",
				col, ctx.tokenStart, ctx.tokenEnd)
		}
	}

	t.Run("a column past the end of the line is the end of the line", func(t *testing.T) {
		atEnd := a.detectContext(line, len(line))
		past := a.detectContext(line, len(line)+1000)
		if past.tokenStart != atEnd.tokenStart || past.tokenEnd != atEnd.tokenEnd {
			t.Errorf("a column past the end gave %d..%d, want the end of the line at %d..%d",
				past.tokenStart, past.tokenEnd, atEnd.tokenStart, atEnd.tokenEnd)
		}
	})

	t.Run("an EMPTY line is no suggestions rather than a panic", func(t *testing.T) {
		for _, line := range []string{"", "   ", "-- just a comment", "'a string'"} {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("detectContext(%q) panicked: %v", line, r)
					}
				}()
				ctx := a.detectContext(line, len(line))
				if ctx.tokenStart > ctx.tokenEnd {
					t.Errorf("detectContext(%q) produced inverted bounds %d..%d",
						line, ctx.tokenStart, ctx.tokenEnd)
				}
			}()
		}
	})
}

// slicesContains avoids importing slices for one three-element membership test.
func slicesContains(hay []CompletionKind, needle CompletionKind) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

// suggestionNames2 is suggestionNames for a plain slice, since that helper takes the state.
func suggestionNames2(items []CompletionItem) []string {
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = item.Name
	}
	return out
}

// TestThePopupDoesNotOfferTheWordYouAreAlreadyTyping is the one item applyFilter removes,
// and it is the only place the filter touches what is ALREADY a statement keyword.
//
// It exists because accepting that suggestion would leave the line unchanged: the user
// presses tab, nothing moves, and the popup is still open. So `WHERE` is excluded while
// `WHERE` is being typed — which means the popup is EMPTY at that position, because no
// other keyword fuzzy-matches the whole word. Asserted as an emptiness rather than as a
// membership, because the emptiness IS the behaviour.
//
// The pair below is what makes it falsifiable: a PARTIAL word offers its match, so a
// filter that dropped everything would pass the first case and fail the second.
func TestThePopupDoesNotOfferTheWordYouAreAlreadyTyping(t *testing.T) {
	t.Run("a COMPLETE keyword offers nothing", func(t *testing.T) {
		a := popupAt(t, "SELECT * FROM users WHERE|")
		if a.prefix != "WHERE" || a.currentCtx.tokenText != "WHERE" {
			t.Fatalf("the prefix is %q and the token text is %q, want them equal and complete — the exact-keyword skip needs both",
				a.prefix, a.currentCtx.tokenText)
		}
		if hasName(suggestionNames(a), "WHERE") {
			t.Errorf("WHERE is offered while WHERE is being typed, so accepting it would change nothing")
		}
		if len(a.filtered) != 0 {
			t.Errorf("the popup holds %v, want nothing: no other keyword fuzzy-matches the whole word",
				suggestionNames(a))
		}
	})

	t.Run("a PARTIAL keyword offers its match", func(t *testing.T) {
		// The other half. WHER does not equal any keyword's name, so the skip cannot fire
		// and the one match is offered — which is what makes tab useful.
		a := popupAt(t, "SELECT * FROM users WHER|")
		if !hasName(suggestionNames(a), "WHERE") {
			t.Errorf("WHER does not complete to WHERE: %v", suggestionNames(a))
		}
	})

	t.Run("a partial COLUMN name still offers the column", func(t *testing.T) {
		// The skip is keyed on Kind == Keyword, so a COLUMN whose name happens to equal the
		// typed word is still offered — and `name` is one of users' columns, so this is
		// not a contrived name.
		//
		// The FROM has to be before the cursor: the tables in scope are the ones named
		// BEFORE it, so `SELECT na|FROM users` offers nothing at all. That limit is
		// pinned in the other autocomplete file.
		a := popupAt(t, "SELECT * FROM users WHERE name|")
		if !hasName(suggestionNames(a), "name") {
			t.Errorf("the column is not offered while it is being typed: %v", suggestionNames(a))
		}
	})
}

// TestThePopupScrollsToTheSelection is the window arithmetic in Render, and it is the only
// thing that keeps a long list usable.
//
// maxItems is fifteen, so a list longer than that scrolls. The interesting case is a
// selection PAST the window — the user held down the arrow key — where the window has to
// move to follow it. Two clamps do that, and the second one exists because a short list
// followed by a reset selection would otherwise compute a negative start.
func TestThePopupScrollsToTheSelection(t *testing.T) {
	for _, tc := range []struct {
		name      string
		nItems    int
		selected  int
		wantRendr bool
	}{
		{"a short list needs no scrolling", 3, 2, true},
		{"a list exactly at the cap", 15, 7, true},
		{"one past the cap", 16, 15, true},
		{"a long list with the selection at the end", 60, 59, true},
		{"a long list with the selection in the middle", 60, 30, true},
		{"a long list with the selection at zero", 60, 0, true},
		// The clamp: a reset selection on a list shorter than the window computes
		// start = end - maxItems, which is negative.
		{"a selection past the end of a short list", 3, 12, true},
		// An empty list renders NOTHING, which is right: a bordered box with no rows in
		// it reads as a broken panel rather than as "nothing to suggest".
		{"an empty list", 0, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := NewAutocompleteState()
			a.visible = true
			a.maxItems = 15
			for i := range tc.nItems {
				a.filtered = append(a.filtered, CompletionItem{
					Kind: CompletionColumn,
					Name: strings.Repeat("x", i%5+1) + strconv.Itoa(i),
				})
			}
			a.selected = tc.selected

			out := a.Render(theme.Resolve("dark").Styles(), 80)
			if tc.wantRendr && out == "" {
				t.Fatal("the popup rendered nothing")
			}
			if !tc.wantRendr && out != "" {
				t.Fatalf("an empty suggestion list rendered a box:\n%s", out)
			}
			if !tc.wantRendr {
				return
			}
			// Every row is a line, and none of them may be a fragment of a name — a window
			// that starts mid-list would show half a suggestion.
			for i, line := range strings.Split(out, "\n") {
				if strings.Contains(line, "\x1b[") {
					continue
				}
				if strings.TrimSpace(line) == "" {
					continue
				}
				// The bordered box draws its own borders; anything else is a row.
				if strings.ContainsAny(line, "┌┐└┘│├┤") {
					continue
				}
				if !strings.Contains(out, line) {
					t.Errorf("line %d is not in the render: %q", i, line)
				}
			}
		})
	}

	t.Run("and the selected item is always among the rows shown", func(t *testing.T) {
		// The property the window arithmetic exists for. A selection scrolled out of view
		// means the user presses enter and accepts something they cannot see.
		a := NewAutocompleteState()
		a.visible = true
		a.maxItems = 15
		for i := range 60 {
			a.filtered = append(a.filtered, CompletionItem{
				Kind: CompletionColumn,
				Name: "col_" + strconv.Itoa(i),
			})
		}

		for _, sel := range []int{0, 7, 14, 15, 30, 58, 59} {
			a.selected = sel
			out := ansi.Strip(a.Render(theme.Resolve("dark").Styles(), 100))
			if !strings.Contains(out, "col_"+strconv.Itoa(sel)) {
				t.Errorf("with item %d selected it is not in the visible window:\n%s", sel, out)
			}
		}
	})
}
