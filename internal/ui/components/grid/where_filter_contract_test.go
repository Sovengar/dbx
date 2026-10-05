// The contracts of the WHERE filter, asserted as properties over matrices of
// inputs and states rather than one scenario at a time.
//
// The filter is a self-contained widget: it takes column names and a width, and its
// whole behaviour is "what does it suggest for what I have typed". There is no I/O,
// no app wiring, and the in-package test file can reach every field — which is why
// this file tests the POPUP and the SUGGESTION LIST directly instead of only the text
// the user can see.
//
// The three things this widget actually promises, in one place:
//
//	I1  the cursor is a RUNE index and every key that moves or edits it stays inside
//	    the value
//	I2  the suggestion list follows the CONTEXT of the clause — a column name gets
//	    operators, an operator gets values, `IS` gets NULL — and the popup only
//	    filters what the word being typed can match
//	I3  the selected suggestion is always INSIDE the visible window of the popup
//
// Two facts about the code that shaped the assertions, both read rather than
// assumed:
//
//	The suggestion lists are PACKAGE VARIABLES (`sqlOperators`, `sqlKeywords`,
//	`isSuggestions`, ...) shared by every filter instance, and they are NOT copied
//	before being handed out — `filterSuggestions` assigns `wf.suggestions =
//	wf.allSuggestions` when nothing is being typed. A test that appended to one of
//	them would corrupt every later test in the package, so nothing here touches them.
//
//	A key is inserted only when `len(key) == 1`, i.e. when the key's STRING is one
//	byte long. That is a real limitation — see the test that names it.
package grid

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/theme"
)

// strKey is a key the filter can read. It is a bare string so a test can send any
// key name the filter switches on — including "space", which the filter normalises
// itself, and the two-word chords like "ctrl+u".
type strKey string

func (k strKey) String() string { return string(k) }

// wfColumns is the column set the fixtures use: three ordinary columns, which is
// enough for a popup and small enough that the suggestion list is readable in a
// failure message.
var wfColumns = []postgres.ColumnInfo{
	{Name: "id", DataType: "integer"},
	{Name: "name", DataType: "text"},
	{Name: "age", DataType: "integer"},
}

// newWF builds a shown filter over wfColumns with the given input already typed.
func newWF(t *testing.T, input string) *WhereFilter {
	t.Helper()
	wf := NewWhereFilter(theme.Resolve("dark").Styles(), wfColumns, 80)
	wf.Show()
	if input != "" {
		wf.SetInput(input)
	}
	return wf
}

// newWFBig builds a filter with enough columns that the popup has to SCROLL:
// twelve columns plus four keywords is sixteen suggestions against a window of
// maxPopupHeight (eight), so scrolling is reachable rather than hypothetical.
func newWFBig(t *testing.T) *WhereFilter {
	t.Helper()
	var cols []postgres.ColumnInfo
	for i := range 12 {
		cols = append(cols, postgres.ColumnInfo{Name: "column_" + string(rune('a'+i)), DataType: "text"})
	}
	wf := NewWhereFilter(theme.Resolve("dark").Styles(), cols, 80)
	wf.Show()
	return wf
}

// labels returns the visible suggestion labels, which is what a test mostly asserts
// on: the labels ARE the contract, and the details are decoration.
func labels(sugs []Suggestion) []string {
	out := make([]string, 0, len(sugs))
	for _, s := range sugs {
		out = append(out, s.Label)
	}
	return out
}

func has(sugs []Suggestion, label string) bool {
	for _, s := range sugs {
		if s.Label == label {
			return true
		}
	}
	return false
}

func joinL(sugs []Suggestion) string { return strings.Join(labels(sugs), "|") }

// ---------------------------------------------------------------------------
// I1: the cursor
// ---------------------------------------------------------------------------

// Scenario: El cursor de edicion NUNCA sale del valor.
//
// The cursor indexes RUNES, not bytes — backspace slices a []rune — so every key that
// moves or edits it has to stay inside, and the clamps are checked by pressing each
// key twice rather than once: a single press cannot tell a clamp from a move.
func TestWhereFilter_TheEditCursorNeverLeavesTheValue(t *testing.T) {
	for _, initial := range []string{"", "a", "abc", "áé日"} {
		t.Run(initial, func(t *testing.T) {
			wf := newWF(t, initial)
			runes := utf8.RuneCountInString(initial)
			if wf.cursor != runes {
				t.Fatalf("SetInput(%q) left the cursor at %d, want %d", initial, wf.cursor, runes)
			}

			// Home and ctrl+a are the same key, and both are absolute.
			for _, key := range []strKey{"home", "ctrl+a"} {
				wf.HandleKey(key)
				if wf.cursor != 0 {
					t.Errorf("%s left the cursor at %d, want 0", key, wf.cursor)
				}
				wf.HandleKey(strKey("left")) // already at the start
				if wf.cursor != 0 {
					t.Errorf("left at the start moved the cursor to %d", wf.cursor)
				}
			}

			// End and ctrl+e likewise.
			for _, key := range []strKey{"end", "ctrl+e"} {
				wf.HandleKey(key)
				if wf.cursor != runes {
					t.Errorf("%s left the cursor at %d, want %d", key, wf.cursor, runes)
				}
				wf.HandleKey(strKey("right")) // already at the end
				if wf.cursor != runes {
					t.Errorf("right at the end moved the cursor to %d", wf.cursor)
				}
			}

			// Right walks the whole way and stops.
			wf.HandleKey(strKey("home"))
			for range runes + 3 {
				wf.HandleKey(strKey("right"))
				if wf.cursor > runes {
					t.Fatalf("right moved the cursor to %d in a %d-rune value", wf.cursor, runes)
				}
			}
			if wf.cursor != runes {
				t.Errorf("the cursor stopped at %d, want %d", wf.cursor, runes)
			}
			// And left walks back to zero.
			for range runes + 3 {
				wf.HandleKey(strKey("left"))
				if wf.cursor < 0 {
					t.Fatalf("left moved the cursor to %d", wf.cursor)
				}
			}
			if wf.cursor != 0 {
				t.Errorf("the cursor stopped at %d walking left, want 0", wf.cursor)
			}
		})
	}
}

// Scenario: Escribir INSERTA en el cursor, y el cursor avanza por la entrada.
//
// Insertion is at the cursor rather than at the end, which is what makes the filter
// usable for editing the middle of a clause. And the cursor advances by the input's
// length — one character here, since only single-byte keys are accepted (see the
// test below), so a fixture that typed several characters at once would not test it.
func TestWhereFilter_TypingInsertsAtTheCursor(t *testing.T) {
	wf := newWF(t, "id = 1")
	if wf.cursor != 6 {
		t.Fatalf("SetInput(%q) left the cursor at %d, want 6", wf.input, wf.cursor)
	}
	for range 2 {
		wf.HandleKey(strKey("left"))
	}
	if wf.cursor != 4 {
		t.Fatalf("two presses of left left the cursor at %d, want 4", wf.cursor)
	}

	// Insert AT the cursor, not at the end: a user fixing a typo in the
	// middle of a clause has to be able to.
	wf.HandleKey(strKey("7"))
	if !strings.HasPrefix(wf.input, "id =7") {
		t.Fatalf("typing at offset 4 gave %q, want the 7 at offset 4", wf.input)
	}
	if got := len([]rune(wf.input)); got != 7 {
		t.Errorf("the value is %d runes, want 7", got)
	}
	if wf.cursor != 5 {
		t.Errorf("the cursor is at %d after inserting one character at 4, want 5 — just past what was typed", wf.cursor)
	}
}

// Scenario: Borrar quita UN CARACTER, y no hace nada en los extremos.
//
// This one is right in the source — `[]rune(wf.input)` — and the test is here to
// keep it right, because the SAME widget shape was byte-based in two other files in
// this project (the cell editor and the ASK pane's editor) and both were real bugs.
//
// The multi-byte fixture is the point: a byte-wise delete on "á" would leave invalid
// UTF-8 in the clause the user is about to run against their database.
func TestWhereFilter_BackspaceAndDeleteRemoveOneCharacter(t *testing.T) {
	// Each case is one key on one value with its own expected string, rather
	// than index arithmetic over a shared prefix: that arithmetic was wrong
	// for the empty value, and the wrong fixture crashed before it could
	// fail usefully.
	for _, tc := range []struct {
		name  string
		start string
		key   strKey
		at    strKey // "home" or "end"
		want  string
	}{
		{"backspace at the end removes the last character", "abc", "backspace", "end", "ab"},
		{"backspace at the start does nothing", "abc", "backspace", "home", "abc"},
		{"backspace on an empty line does nothing", "", "backspace", "home", ""},
		{"backspace on a one-character line empties it", "a", "backspace", "end", ""},
		{"delete at the start removes the first character", "abc", "delete", "home", "bc"},
		{"delete at the end does nothing", "abc", "delete", "end", "abc"},
		{"delete on an empty line does nothing", "", "delete", "end", ""},
		{"delete on a one-character line empties it", "a", "delete", "home", ""},
		// The multi-byte ones are the point. A byte-wise delete leaves
		// invalid UTF-8 in the clause the user is about to run against
		// their database, and the cursor lands inside the broken sequence.
		{"backspace removes a two-byte character whole", "áé", "backspace", "end", "á"},
		{"backspace removes a three-byte character whole", "日", "backspace", "end", ""},
		{"delete removes a three-byte character whole", "日x", "delete", "home", "x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wf := newWF(t, tc.start)
			wf.HandleKey(tc.at)
			if _, handled := wf.HandleKey(tc.key); !handled {
				t.Fatalf("%s on %q was not handled", tc.key, tc.start)
			}
			if wf.input != tc.want {
				t.Errorf("%s at the %s of %q gave %q, want %q", tc.key, tc.at, tc.start, wf.input, tc.want)
			}
			if !utf8.ValidString(wf.input) {
				t.Errorf("%s left invalid UTF-8: %q", tc.key, wf.input)
			}
			// The cursor stays a valid position: never negative, never
			// past the end.
			n := utf8.RuneCountInString(wf.input)
			if wf.cursor < 0 || wf.cursor > n {
				t.Errorf("the cursor is at %d in a %d-rune value %q", wf.cursor, n, wf.input)
			}
		})
	}
}

// Scenario: ctrl+u VACIA la entrada entera, no un caracter.
//
// It is the shell's "clear line" spelling, and it has to leave the cursor at zero as
// well — a cleared line with the cursor at the old position would make the next
// character land past the end of an empty string.
func TestWhereFilter_CtrlUClearsTheLineAndTheCursor(t *testing.T) {
	for _, initial := range []string{"", "a", "id = 1 AND "} {
		wf := newWF(t, initial)
		if _, handled := wf.HandleKey(strKey("ctrl+u")); !handled {
			t.Errorf("ctrl+u on %q was not handled", initial)
		}
		if wf.input != "" {
			t.Errorf("ctrl+u left %q, want an empty line", wf.input)
		}
		if wf.cursor != 0 {
			t.Errorf("ctrl+u left the cursor at %d, want 0", wf.cursor)
		}
	}
}

// Scenario: Una tecla de mas de un byte NO se escribe, y no se reporta como manejada.
//
// KNOWN LIMITATION, pinned rather than quietly worked around.
//
// The default branch of HandleKey inserts a character only when the key's string is
// one BYTE long (`len(key) == 1`), and then tests `rune(key[0])` — the first byte — for
// printability. So an accented letter, an emoji or a CJK glyph is not one byte, the
// guard rejects it, and the key falls through as UNHANDLED. In the grid that means
// the character is dropped: the clause the user is typing cannot contain a non-ASCII
// literal.
//
// That is a real defect and it is not fixed here, because the fix touches the key
// plumbing this widget shares with the cell editor and it deserves its own commit.
// What belongs in the gate is the fact, so the next reader knows the behaviour is
// pinned and not assumed.
func TestWhereFilter_ANonASCIICharacterIsDroppedAndReportedUnhandled(t *testing.T) {
	for _, ch := range []string{"é", "ñ", "日", "🙂"} {
		t.Run(ch, func(t *testing.T) {
			wf := newWF(t, "")
			handled, changed := wf.HandleKey(strKey(ch))
			if handled {
				t.Errorf("a %q key was reported as handled, so it must have been inserted", ch)
			}
			if changed {
				t.Error("a dropped key reported a change")
			}
			if wf.input != "" {
				t.Errorf("a %q key wrote %q into the line", ch, wf.input)
			}
		})
	}

	// The ASCII case is the control: it IS inserted and IS handled.
	wf := newWF(t, "")
	if handled, _ := wf.HandleKey(strKey("x")); !handled {
		t.Error("an ASCII key was reported as unhandled")
	}
	if wf.input != "x" {
		t.Errorf("an ASCII key wrote %q, want %q", wf.input, "x")
	}

	// And a multi-byte string of two ASCII characters is not one key but
	// two, so it is not inserted either.
	two := newWF(t, "")
	if handled, _ := two.HandleKey(strKey("ab")); handled {
		t.Error("a two-character key was reported as handled")
	}
}

// Scenario: El popup decide si enter, tab y las flechas hacen algo.
//
// Four keys behave in two completely different ways depending on whether the popup is
// up: with it, they act on the SUGGESTION LIST; without it, they act on the CLAUSE
// (enter applies it) or do nothing at all (tab, up, down). The difference is reported
// through both return values, and that is what the grid above uses to decide whether
// a key reached the filter or fell through to itself.
func TestWhereFilter_ThePopupDecidesWhatEnterTabAndArrowsDo(t *testing.T) {
	t.Run("with a popup, enter and tab accept the suggestion", func(t *testing.T) {
		for _, key := range []strKey{"enter", "tab"} {
			wf := newWF(t, "")
			if !wf.showPopup {
				t.Fatalf("the fixture has no popup to act on (%s)", joinL(wf.suggestions))
			}
			before := wf.input
			if handled, changed := wf.HandleKey(key); !handled || !changed {
				t.Errorf("%s with a popup reported handled=%v changed=%v, want both true", key, handled, changed)
			}
			if wf.input == before {
				t.Errorf("%s did not change the line, so no suggestion was accepted", key)
			}
			if wf.ShouldApply() {
				t.Errorf("%s with a popup applied the clause instead of accepting a suggestion", key)
			}
		}
	})

	t.Run("without a popup, enter applies the clause", func(t *testing.T) {
		// An input that matches nothing leaves no suggestions.
		wf := newWF(t, "zzzzz")
		if wf.showPopup {
			t.Fatalf("the fixture HAS a popup (%s)", joinL(wf.suggestions))
		}
		handled, _ := wf.HandleKey(strKey("enter"))
		if !handled {
			t.Error("enter without a popup was not handled")
		}
		if !wf.ShouldApply() {
			t.Error("enter without a popup did not arm the apply")
		}
		if wf.Input() != "zzzzz" {
			t.Errorf("the applied clause is %q, want what was typed", wf.Input())
		}
		if wf.Visible() {
			t.Error("applying left the filter visible")
		}
	})

	t.Run("without a popup, tab and the arrows fall through", func(t *testing.T) {
		for _, key := range []strKey{"tab", "up", "k", "down", "j"} {
			wf := newWF(t, "zzzzz")
			handled, changed := wf.HandleKey(key)
			if handled || changed {
				t.Errorf("%s without a popup reported handled=%v changed=%v, want both false so the key reaches the grid", key, handled, changed)
			}
			if wf.ShouldApply() {
				t.Errorf("%s without a popup applied the clause", key)
			}
		}
	})

	t.Run("with a popup, the arrows move the selection", func(t *testing.T) {
		for _, key := range []strKey{"up", "k", "down", "j"} {
			wf := newWF(t, "")
			if !wf.showPopup {
				t.Fatalf("the fixture has no popup to act on")
			}
			before := wf.selected
			if handled, changed := wf.HandleKey(key); !handled || !changed {
				t.Errorf("%s with a popup reported handled=%v changed=%v, want both true", key, handled, changed)
			}
			if wf.selected == before {
				t.Errorf("%s did not move the selection from %d", key, before)
			}
		}
	})
}

// Scenario: Escape se cierra por PASOS: popup, linea, y por ultimo el filtro.
//
// Three steps, in that order, and a user typing a clause cannot lose it by pressing
// escape twice — which is the failure mode a single-escape implementation has. The
// middle step is the one worth testing: with the popup already closed, the first
// escape clears the line and leaves the filter up.
func TestWhereFilter_EscapeClosesThePopupThenTheLineThenTheFilter(t *testing.T) {
	wf := newWF(t, "na")
	if !wf.showPopup {
		t.Fatalf("the fixture has no popup to close (%s)", joinL(wf.suggestions))
	}

	// 1. The popup.
	wf.HandleKey(strKey("esc"))
	if wf.showPopup {
		t.Error("escape did not close the popup")
	}
	if !wf.Visible() {
		t.Error("escape closed the filter instead of just the popup")
	}
	if wf.input != "na" {
		t.Errorf("closing the popup changed the line to %q", wf.input)
	}

	// 2. The line, with the popup already down.
	wf.HandleKey(strKey("esc"))
	if wf.input != "" {
		t.Errorf("the second escape left the line as %q, want it cleared", wf.input)
	}
	if wf.cursor != 0 {
		t.Errorf("clearing the line left the cursor at %d", wf.cursor)
	}
	if !wf.Visible() {
		t.Error("clearing the line closed the filter")
	}

	// 3. The filter — but only after the popup that CLEARING the line
	// brought back. An empty line has suggestions again (every column and
	// keyword), so the popup reopens and the next escape closes that
	// first. Worth pinning: it is why closing a fully typed clause takes
	// three escapes and not two.
	if !wf.showPopup {
		t.Fatal("clearing the line did not bring the popup back, so the escape ladder is not three steps")
	}
	wf.HandleKey(strKey("esc"))
	if !wf.Visible() {
		t.Error("escape closed the popup and reported the filter as hidden too")
	}
	wf.HandleKey(strKey("esc"))
	if wf.Visible() {
		t.Error("escape on an empty line and no popup did not close the filter")
	}
}

// ---------------------------------------------------------------------------
// I2: context detection and the suggestion list
// ---------------------------------------------------------------------------

// Scenario: Las sugerencias dependen de DONDE ESTAS en la clausula.
//
// The whole point of the widget: what you are offered depends on what you have typed
// and WHERE. After a column name, operators. After an operator, values. After IS, NULL
// and NOT. After AND or OR, a fresh clause. Everything else, columns and keywords.
//
// So the contexts are pinned separately, because a change that made one of them
// return the general list would still "work" — it would just offer the user forty
// things to type when there was one right answer.
func TestWhereFilter_TheSuggestionListFollowsTheClauseContext(t *testing.T) {
	for _, tc := range []struct {
		name     string
		input    string
		wantKind string // the kind every suggestion must carry
		wantSome []string
		wantNone []string
	}{
		{
			name: "an empty clause offers columns and keywords", input: "",
			wantSome: []string{"id", "name", "age", "AND ", "OR ", "NOT "},
			wantNone: []string{"=  "},
		},
		{
			name: "after a column name it offers operators", input: "id ",
			wantSome: []string{"=  ", "!= ", "IS ", "IS NOT ", "LIKE ", "BETWEEN "},
			wantNone: []string{"id", "NULL"},
		},
		{
			name: "after an operator it offers values", input: "id = ",
			wantSome: []string{"NULL", "TRUE", "FALSE"},
			wantNone: []string{"=  ", "IS "},
		},
		{
			name: "after IS it offers NULL and NOT", input: "id IS ",
			wantSome: []string{"NULL", "NOT "},
			wantNone: []string{"TRUE", "=  "},
		},
		{
			name: "after IS NOT it offers only NULL", input: "id IS NOT ",
			wantSome: []string{"NULL"},
			wantNone: []string{"NOT ", "TRUE", "FALSE"},
		},
		{
			name: "after AND it starts a new clause", input: "id = 1 AND ",
			wantSome: []string{"id", "name", "AND ", "OR "},
			wantNone: []string{"=  ", "NULL"},
		},
		{
			name: "after OR it starts a new clause", input: "id = 1 OR ",
			wantSome: []string{"id", "name"},
			wantNone: []string{"=  "},
		},
		{
			// A bare NOT counts only at the END of a clause. After
			// `id NOT ` the last token is the operator and the second to
			// last is the column, so this is an operator context and the
			// answer is values. That is not obvious and it is not what a
			// reader would guess, so it is pinned rather than assumed:
			// `NOT` as a prefix wants a value, `NOT` as a negation wants a
			// fresh clause.
			name: "after NOT and a space it is an operator context", input: "id NOT ",
			wantSome: []string{"NULL", "TRUE", "FALSE"},
			wantNone: []string{"=  ", "IS "},
		},
		{
			// A clause that ENDS on NOT is a negation, and offers a fresh
			// clause. This is the case that reaches the NOT branch at all —
			// the input is trimmed before the keyword is looked for, so a
			// keyword with nothing after it is the only way there.
			name: "a clause ending on NOT starts a new one", input: "x AND NOT",
			wantSome: []string{"id", "name", "NOT "},
			wantNone: []string{"=  ", "NULL"},
		},
		{
			name: "inside IN ( it offers values", input: "id IN (",
			wantSome: []string{"NULL", "TRUE", "FALSE"},
			wantNone: []string{"=  ", "IS "},
		},
		{
			name: "inside NOT IN ( it offers values", input: "id NOT IN (",
			wantSome: []string{"NULL", "TRUE", "FALSE"},
			wantNone: []string{"IS "},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wf := newWF(t, tc.input)
			for _, want := range tc.wantSome {
				if !has(wf.allSuggestions, want) {
					t.Errorf("the context list is %s, which does not offer %q", joinL(wf.allSuggestions), want)
				}
			}
			for _, none := range tc.wantNone {
				if has(wf.allSuggestions, none) {
					t.Errorf("the context list is %s, which offers %q", joinL(wf.allSuggestions), none)
				}
			}
			// Every suggestion in a context list carries a kind and an
			// insert text — the popup draws both, and a missing one would
			// render a blank column.
			for _, sug := range wf.allSuggestions {
				if sug.Kind == "" {
					t.Errorf("the suggestion %q has no kind", sug.Label)
				}
				if sug.InsertText == "" {
					t.Errorf("the suggestion %q inserts nothing", sug.Label)
				}
			}
		})
	}
}

// Scenario: El popup FILTRA por la palabra que se esta escribiendo, no por la linea.
//
// The filter is on the token under the cursor, so typing "id = na" narrows the list by
// "na" and not by the whole clause — otherwise nothing would ever match once a clause
// had a word in it. And the filter is a SUBSEQUENCE match, not a prefix one, so "nm"
// finds "name" — which is what makes the popup useful for a column whose spelling the
// user half-remembers.
//
// An empty token offers everything, which is the case a filter written as "keep what
// matches" would get wrong.
func TestWhereFilter_ThePopupFiltersByTheWordUnderTheCursor(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
		want  []string // exactly these, in order
	}{
		{"a prefix of a column name", "na", []string{"name"}},
		{"a subsequence of a column name", "nm", []string{"name"}},
		{"a prefix of a keyword", "an", []string{"AND "}},
		{"nothing matches", "zzzzz", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wf := newWF(t, tc.input)
			got := labels(wf.suggestions)
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("the popup for %q is %s, want %s", tc.input, joinL(wf.suggestions), strings.Join(tc.want, "|"))
			}
			// And the popup is up exactly when there is something to show.
			if wf.showPopup != (len(wf.suggestions) > 0) {
				t.Errorf("with %d suggestions the popup is %v", len(wf.suggestions), wf.showPopup)
			}
		})
	}

	// The token is the word the cursor is INSIDE, so the filter follows the
	// cursor rather than the end of the line: "id = na" narrows by "na" —
	// which is a column name, and a word typed after an operator does not
	// make the context a value context. The value context is what you get
	// when the operator is the LAST thing typed.
	wf := newWF(t, "id = na")
	if got := labels(wf.suggestions); len(got) != 1 || got[0] != "name" {
		t.Errorf("with the cursor at the end of %q the popup is %s, want just the name column", wf.input, joinL(wf.suggestions))
	}
	// Moving the cursor does NOT re-filter, and that is worth pinning: the
	// popup is recomputed when the INPUT changes, not when the cursor moves.
	// So arrowing back into an earlier word leaves the list narrowed by the
	// word the cursor left, which is stale rather than wrong — the next
	// character typed recomputes it.
	stale := newWF(t, "id = na")
	stale.HandleKey(strKey("home"))
	for range 2 {
		stale.HandleKey(strKey("right"))
	}
	if got := stale.currentInputToken(); got != "id" {
		t.Errorf("with the cursor at offset 2 the token is %q, want %q — so the cursor does move independently", got, "id")
	}
	if got := labels(stale.suggestions); len(got) != 1 || got[0] != "name" {
		t.Errorf("after moving the cursor the popup is %s, want it still narrowed by the word the cursor LEFT", joinL(stale.suggestions))
	}
	// Typing recomputes it, and now the token is "idx" — nothing matches.
	stale.HandleKey(strKey("x"))
	if got := labels(stale.suggestions); len(got) != 0 {
		t.Errorf("after typing the popup is %s, want nothing: the token is now %q", joinL(stale.suggestions), stale.currentInputToken())
	}
	// The token itself is the word between the last space and the cursor,
	// which is what makes the filter follow the cursor rather than the line.
	after := newWF(t, "id = na")
	after.HandleKey(strKey("home"))
	for range 4 {
		after.HandleKey(strKey("right"))
	}
	if got := after.currentInputToken(); got != "=" {
		t.Errorf("with the cursor after \"id =\" the token is %q, want %q", got, "=")
	}
	if got := after.input; got != "id = na" {
		t.Errorf("moving the cursor changed the line to %q", got)
	}
}

// Scenario: La busqueda difusa es una SUBSEQUENCIA HACIA DELANTE.
//
// It matches characters in order but not adjacently, and an empty query matches
// everything — which is what makes the popup appear at all on a fresh filter.
//
// Two things it is NOT, and both are worth stating because a reader would assume
// them: it is not an UNORDERED match, so "en" does not find "name" while "ne" does;
// and it is not case-insensitive by itself, so `fuzzyMatch("name", "NAME")` is false.
// The popup gets its case-insensitivity from `filterSuggestions`, which uppercases
// both sides before calling.
func TestWhereFilter_FuzzyMatchingIsASubsequence(t *testing.T) {
	for _, tc := range []struct {
		text, query string
		want        bool
	}{
		{"", "", true},
		{"anything", "", true},
		{"NAME", "", true},
		{"name", "name", true},
		{"name", "nam", true},
		// FORWARD subsequence, not an unordered one: the characters have
		// to appear in the order they are queried. "ne" matches "name";
		// "en" does not.
		{"name", "ne", true},
		{"name", "ae", true},
		{"name", "en", false},
		{"name", "mn", false},
		{"name", "am", true},
		// The matcher is case-SENSITIVE on its own; the CALLER uppercases
		// both sides, which is where the case-insensitivity of the popup
		// comes from. Asserting it here would be asserting a property the
		// function does not have.
		{"name", "NAME", false},
		{"NAME", "name", false},
		{"name", "zzz", false},
		{"name", "eman", false},
		{"name", "namex", false},
		{"a", "ab", false},
		{"", "a", false},
	} {
		t.Run(tc.text+"/"+tc.query, func(t *testing.T) {
			if got := fuzzyMatch(tc.text, tc.query); got != tc.want {
				t.Errorf("fuzzyMatch(%q, %q) = %v, want %v", tc.text, tc.query, got, tc.want)
			}
		})
	}
}

// Scenario: La ultima clausula es la que va DESPUES del ultimo AND, OR o NOT.
//
// The keyword has to be a STANDALONE word, so a column called "android_id" does not
// split the clause at its "nd". And the LATER keyword wins, so "a AND b OR c" is
// treated as a clause after the OR — which is what makes the context detection correct
// for a clause with more than one connective in it.
//
// The boundary is the LAST of the three, so a fixture with only an AND in it cannot
// tell "last" from "first".
func TestWhereFilter_TheLastClauseIsTheOneAfterTheLastKeyword(t *testing.T) {
	wf := newWFBig(t)
	for _, tc := range []struct {
		name, input, want string
	}{
		{"no keyword at all", "id = 1", "id = 1"},
		{"after AND", "id = 1 AND name = 2", "name = 2"},
		{"after OR", "id = 1 OR name = 2", "name = 2"},
		{"after NOT", "id = 1 NOT name = 2", "name = 2"},
		// The later keyword wins, whichever it is.
		{"AND then OR", "id = 1 AND name = 2 OR age = 3", "age = 3"},
		{"OR then AND", "id = 1 OR name = 2 AND age = 3", "age = 3"},
		// A keyword inside a word does not split the clause.
		{"AND inside a word does not split", "android_id = 1", "android_id = 1"},
		{"NOT inside a word does not split", "notable = 1", "notable = 1"},
		// A trailing keyword leaves an EMPTY clause: the position is
		// just past the keyword's trailing space, which is the end of the
		// input. That is what makes `id = 1 AND ` a fresh clause.
		{"a trailing keyword leaves an empty clause", "id = 1 AND ", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := wf.extractLastClause(tc.input); got != tc.want {
				t.Errorf("extractLastClause(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// Scenario: El tokenizado parte por ESPACIOS y descarta los pedazos vacios.
//
// It is what decides whether the second-to-last token is a column name, so an extra
// space must not produce an empty token that shifts the position — otherwise
// "id  = 1" (two spaces, which a paste produces) would stop offering values.
func TestWhereFilter_TokenizingSplitsOnSpacesAndDropsEmpties(t *testing.T) {
	wf := newWFBig(t)
	for _, tc := range []struct {
		name, input string
		want        []string
	}{
		{"one word", "id", []string{"id"}},
		{"two words", "id =", []string{"id", "="}},
		{"three words", "id = 1", []string{"id", "=", "1"}},
		{"surrounding space", "  id = 1  ", []string{"id", "=", "1"}},
		{"a doubled space", "id  =", []string{"id", "="}},
		{"leading separators", " = 1", []string{"=", "1"}},
		{"a tab separates too", "id\t=\t1", []string{"id", "=", "1"}},
		{"only spaces", "   ", nil},
		{"nothing", "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := wf.tokenize(tc.input)
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("tokenize(%q) = %q, want %q", tc.input, got, tc.want)
			}
			if len(got) != len(tc.want) {
				t.Errorf("tokenize(%q) produced %d tokens, want %d", tc.input, len(got), len(tc.want))
			}
		})
	}
}

// Scenario: Una columna se encuentra por NOMBRE y sin distinguir mayusculas.
//
// The lookup is what decides between "you have typed a column, here are operators" and
// "you have typed a word, here is the general list", so a case-sensitive miss would
// stop offering operators after a capitalised column name — which is how every column
// in the schema is actually written.
func TestWhereFilter_ColumnsAreFoundByNameInAnyCase(t *testing.T) {
	// The three-column fixture, not the twelve-column one: this test names
	// columns that only exist there.
	wf := newWF(t, "")
	for _, tc := range []struct {
		name string
		want int
	}{
		{"id", 0},
		{"name", 1},
		{"age", 2},
		{"ID", 0},
		{"NaMe", 1},
		{"nope", -1},
		{"", -1},
		{"i", -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := wf.findColumn(tc.name); got != tc.want {
				t.Errorf("findColumn(%q) = %d, want %d", tc.name, got, tc.want)
			}
		})
	}
}

// Scenario: Los valores distintos se(ZIPAN) los repetidos y saltan los nulos.
//
// The list backs the value suggestions, so it has to be short, unique, and in the order
// the rows arrive — a LIMIT that counted rows rather than distinct values would cut the
// list short of its stated length and quietly hide values the user could have picked.
//
// NULL is skipped rather than offered as the text "NULL": it has its own suggestion, and
// offering it twice with different meanings would be worse.
func TestWhereFilter_DistinctValuesUniquesSkipsNullsAndRespectsTheLimit(t *testing.T) {
	// Column 0 only, in row order: b, a, (b again), (nil skipped), c.
	rows := [][]interface{}{
		{"b", "x"},
		{"a", "y"},
		{"b", "z"},
		{nil, "w"},
		{"c", "v"},
	}

	for _, tc := range []struct {
		name  string
		limit int
		want  string
	}{
		{"no limit takes everything distinct", 10, "b|a|c"},
		{"a limit of one takes the first", 1, "b"},
		{"a limit of two takes two", 2, "b|a"},
		{"a limit past the end takes all", 99, "b|a|c"},
		// The limit is checked AFTER the first push, so a limit of zero
		// returns ONE value rather than none. Pinned because it reads as a
		// bug and a reader deserves to know which way the code behaves.
		{"a limit of zero still returns one value", 0, "b"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := DistinctValues(0, rows, tc.limit)
			if strings.Join(got, "|") != tc.want {
				t.Errorf("DistinctValues = %q, want %q", got, tc.want)
			}
		})
	}

	// Column 1 is a different list, so the index really selects.
	if got := DistinctValues(1, rows, 10); strings.Join(got, "|") != "x|y|z|w|v" {
		t.Errorf("DistinctValues(1) = %q, want the second column's values", got)
	}

	// A column index past the end of a row is SKIPPED, not fatal: a row
	// with fewer values than the result has columns is normal for a
	// projection.
	ragged := [][]interface{}{{"a"}, {"b"}}
	if got := DistinctValues(5, ragged, 10); len(got) != 0 {
		t.Errorf("an out-of-range column index returned %q, want nothing", got)
	}

	// No rows at all.
	if got := DistinctValues(0, nil, 10); len(got) != 0 {
		t.Errorf("no rows returned %q", got)
	}

	// A nil value in EVERY row leaves nothing, rather than a list of the
	// string "NULL".
	allNil := [][]interface{}{{nil}, {nil}}
	if got := DistinctValues(0, allNil, 10); len(got) != 0 {
		t.Errorf("an all-NULL column returned %q, want nothing", got)
	}

	// Values of different types are formatted, not compared: 1 and "1"
	// are the same text and therefore one entry.
	mixed := [][]interface{}{{1}, {"1"}, {1}}
	if got := DistinctValues(0, mixed, 10); strings.Join(got, "|") != "1" {
		t.Errorf("DistinctValues over mixed types returned %q, want one %q", got, "1")
	}
}

// paintOfRun returns the colours in force over the named text inside a rendered
// string. It reads the escape sequences rather than a style, so it works for a
// fragment as well as for a whole line.
func paintOfRun(rendered, text string) string {
	for _, c := range cells(rendered) {
		if s, ok := c[0].(string); ok && strings.Contains(s, text) {
			return c[1].(paint).String()
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// I3: the selection and the popup window
// ---------------------------------------------------------------------------

// Scenario: La seleccion DA LA VUELTA por los dos extremos.
//
// Up from the first suggestion goes to the LAST and down from the last goes to the
// first. A cursor that stopped at the ends would trap the user on the first row of a
// long list, which is the shape of "I can't get to the thing I want".
//
// The list is walked all the way round rather than pressed once, because a single
// press cannot tell a wrap from a move.
func TestWhereFilter_TheSelectionWrapsAtBothEnds(t *testing.T) {
	for _, n := range []int{1, 2, 3, 7, 20} {
		t.Run(fmt.Sprintf("%d suggestions", n), func(t *testing.T) {
			wf := newWF(t, "")
			wf.suggestions = make([]Suggestion, n)
			wf.showPopup = true

			// Walk down all the way round and more. After k presses the
			// selection is k mod n, which is what makes the wrap visible:
			// k == n is back at the first.
			for k := 1; k <= n+3; k++ {
				wf.moveSelection(1)
				if wf.selected < 0 || wf.selected >= n {
					t.Fatalf("moving down left the selection at %d of %d suggestions", wf.selected, n)
				}
				if want := k % n; wf.selected != want {
					t.Fatalf("after %d downs the selection is at %d, want %d", k, wf.selected, want)
				}
			}

			// And up, from the first, which is the other wrap. Reset
			// explicitly: the walk above ended wherever the modulo left
			// it, and a wrap is only observable from an end.
			wf.selected = 0
			wf.moveSelection(-1)
			if wf.selected != n-1 {
				t.Errorf("one up from the first suggestion is %d, want the last (%d)", wf.selected, n-1)
			}
			if n >= 2 {
				wf.moveSelection(-1)
				if want := n - 2; wf.selected != want {
					t.Errorf("two up from the first is %d, want %d", wf.selected, want)
				}
			}
			// And down from the last wraps to the first.
			wf.selected = n - 1
			wf.moveSelection(1)
			if wf.selected != 0 {
				t.Errorf("one down from the last is %d, want 0", wf.selected)
			}

			// An empty list has nothing to select, and moving is a no-op
			// rather than a wrap onto index -1.
			empty := newWF(t, "")
			empty.suggestions = nil
			empty.selected = 3
			empty.moveSelection(1)
			empty.moveSelection(-1)
			if empty.selected != 3 {
				t.Errorf("moving in an empty list left the selection at %d", empty.selected)
			}
		})
	}
}

// Scenario: La seleccion esta SIEMPRE dentro de la ventana del popup.
//
// The popup shows maxPopupHeight rows and scrolls. If the selection could sit outside
// the visible slice, the user would be navigating a list they cannot see — arrowing
// down and watching the highlight not appear.
//
// Both directions are needed: walking UP past the top has to pull the window up, and
// walking DOWN past the bottom has to push it down by exactly enough to show the last
// row rather than scrolling one row at a time.
func TestWhereFilter_TheSelectedSuggestionIsAlwaysVisible(t *testing.T) {
	const n = 20 // well past maxPopupHeight, so the popup has to scroll

	check := func(t *testing.T, wf *WhereFilter) {
		t.Helper()
		if wf.scrollOffset < 0 {
			t.Fatalf("the scroll offset is negative: %d", wf.scrollOffset)
		}
		if wf.selected < wf.scrollOffset {
			t.Fatalf("the selection at %d is above the window at %d", wf.selected, wf.scrollOffset)
		}
		if wf.selected >= wf.scrollOffset+maxPopupHeight {
			t.Fatalf("the selection at %d is below the window at %d+%d", wf.selected, wf.scrollOffset, maxPopupHeight)
		}
		if wf.scrollOffset+maxPopupHeight > n && wf.scrollOffset > n-maxPopupHeight {
			t.Fatalf("the window at %d leaves blank rows below a list of %d", wf.scrollOffset, n)
		}
	}

	t.Run("walking down past the bottom", func(t *testing.T) {
		wf := newWFBig(t)
		wf.suggestions = make([]Suggestion, n)
		wf.showPopup = true
		for k := 1; k <= n+5; k++ {
			wf.moveSelection(1)
			check(t, wf)
			if want := k % n; wf.selected != want {
				t.Errorf("after %d downs the selection is %d, want %d", k, wf.selected, want)
			}
		}
	})

	t.Run("walking up past the top", func(t *testing.T) {
		wf := newWFBig(t)
		wf.suggestions = make([]Suggestion, n)
		wf.showPopup = true
		for range n + 5 {
			wf.moveSelection(-1)
			check(t, wf)
		}
	})

	t.Run("a big jump scrolls by exactly enough", func(t *testing.T) {
		wf := newWFBig(t)
		wf.suggestions = make([]Suggestion, n)
		wf.showPopup = true
		// Straight to the last one: the window has to end on it, not
		// somewhere past it.
		wf.selected = n - 1
		wf.ensureVisible()
		if wf.scrollOffset != n-maxPopupHeight {
			t.Errorf("with the selection on the last of %d the window is at %d, want %d", n, wf.scrollOffset, n-maxPopupHeight)
		}
		// And back to the first.
		wf.selected = 0
		wf.ensureVisible()
		if wf.scrollOffset != 0 {
			t.Errorf("with the selection on the first the window is at %d, want 0", wf.scrollOffset)
		}
		// A selection already inside the window does not move it.
		wf.selected = maxPopupHeight - 1
		wf.ensureVisible()
		if wf.scrollOffset != 0 {
			t.Errorf("a visible selection moved the window to %d", wf.scrollOffset)
		}
	})

	t.Run("a list shorter than the window never scrolls", func(t *testing.T) {
		wf := newWFBig(t)
		wf.suggestions = make([]Suggestion, 3)
		for i := range 3 {
			wf.selected = i
			wf.ensureVisible()
			if wf.scrollOffset != 0 {
				t.Errorf("a list of 3 scrolled to %d on selection %d", wf.scrollOffset, i)
			}
		}
	})
}

// Scenario: Aceptar una sugerencia INSERTA su texto en el cursor.
//
// InsertText, not Label: the labels carry trailing spaces and ellipses for the popup's
// own layout ("=  ", "LIKE "), and inserting a label would put a two-space operator
// into the clause. This is the difference between a suggestion that works and one that
// produces SQL the user then has to fix.
//
// And the cursor advances by the INSERTED text's rune count, so the next keystroke
// continues after what was inserted rather than in the middle of it.
func TestWhereFilter_AcceptingASuggestionInsertsItsInsertText(t *testing.T) {
	for _, tc := range []struct {
		name  string
		start string
		index int
		want  string
		at    int // cursor offset before the press; -1 means the end
	}{
		{"a column inserts its name and a space", "", 0, "id ", -1},
		{"an operator inserts its text", "id ", 0, "id = ", -1},
		{"the equals operator, selected among many", "id ", 0, "id = ", -1},
		{"a keyword inserts itself", "", 3, "AND ", -1},
		{"NOT IN opens a parenthesis", "id ", 10, "id NOT IN (", -1},
		{"a value inserts its literal", "id = ", 0, "id = NULL", -1},
		// Insertion is AT the cursor, not at the end.
		{"insertion happens at the cursor", "id = na", 0, "id name = na", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wf := newWF(t, tc.start)
			if len(wf.suggestions) == 0 {
				t.Fatalf("the fixture has no suggestions for %q (%s)", tc.start, joinL(wf.allSuggestions))
			}
			if tc.index >= len(wf.suggestions) {
				t.Fatalf("the fixture has %d suggestions, want more than %d", len(wf.suggestions), tc.index)
			}
			wf.showPopup = true
			wf.selected = tc.index
			inserted := wf.suggestions[tc.index].InsertText

			if tc.at >= 0 {
				wf.HandleKey(strKey("home"))
				for range tc.at {
					wf.HandleKey(strKey("right"))
				}
			}
			before := wf.cursor
			wf.acceptSuggestion()

			if wf.input != tc.want {
				t.Errorf("accepting %q in %q gave %q, want %q", wf.suggestions[tc.index].Label, tc.start, wf.input, tc.want)
			}
			// The cursor lands just past what was inserted.
			wantCursor := before + utf8.RuneCountInString(inserted)
			if tc.at < 0 {
				wantCursor = utf8.RuneCountInString(tc.want)
			}
			if wf.cursor != wantCursor {
				t.Errorf("the cursor is at %d, want %d — just past the inserted text", wf.cursor, wantCursor)
			}
		})
	}

	// A selection outside the list accepts NOTHING rather than panicking.
	// The list is recomputed on every edit, so a stale index is a real state.
	for _, bad := range []int{-1, 99} {
		wf := newWF(t, "id ")
		wf.selected = bad
		before := wf.input
		wf.acceptSuggestion()
		if wf.input != before {
			t.Errorf("accepting with the selection at %d changed the line to %q", bad, wf.input)
		}
	}
}

// ---------------------------------------------------------------------------
// Rendering
// ---------------------------------------------------------------------------

// Scenario: La linea del filtro lleva el cursor EN MEDIO, y se rellena al ancho.
//
// The bar is the WHERE clause the user is typing, drawn with the cursor block spliced
// in at the cursor's position — so the text before and after it is the line, split.
// And it is padded out to the pane so the border under it is a straight line.
//
// A padding that is one cell off is visible as a gap at the right edge, so the width
// is asserted exactly, and the cursor's position is asserted by where the block sits.
func TestWhereFilter_TheInputBarShowsTheCursorAndFillsTheWidth(t *testing.T) {
	const maxWidth = 80
	for _, input := range []string{"", "id", "id = 1", "id = 1 AND name = 'x'"} {
		for _, at := range []int{0, -1} {
			t.Run(fmt.Sprintf("%q at %d", input, at), func(t *testing.T) {
				wf := NewWhereFilter(theme.Resolve("dark").Styles(), wfColumns, maxWidth)
				wf.Show()
				wf.SetInput(input)
				if at >= 0 {
					wf.HandleKey(strKey("home"))
					for range at {
						wf.HandleKey(strKey("right"))
					}
				}

				bar := ansi.Strip(wf.RenderInput())
				if !strings.HasPrefix(bar, " WHERE │ ") {
					t.Errorf("the bar is %q, want it to start with the WHERE prefix", bar)
				}
				// The line is drawn after the prefix with the cursor block
				// spliced in, so stripping the block leaves the input
				// exactly.
				body := strings.TrimPrefix(bar, " WHERE │ ")
				if got := strings.ReplaceAll(body, "█", ""); strings.TrimRight(got, " ") != input {
					t.Errorf("the bar's body is %q, want the input %q", strings.TrimRight(got, " "), input)
				}
				// And the cursor sits where the line says it does.
				block := strings.Index(body, "█")
				wantBlock := strings.Index(input, "") + at
				if at < 0 {
					wantBlock = len([]rune(input))
				}
				if got := utf8.RuneCountInString(body[:block]); got != wantBlock {
					t.Errorf("the cursor block is at cell %d of the line, want %d", got, wantBlock)
				}
				// Padded to the pane: the prefix, the line, the block and
				// the padding together are maxWidth-4.
				if got := ansi.StringWidth(wf.RenderInput()); got != maxWidth-4 {
					t.Errorf("the bar is %d cells wide, want %d", got, maxWidth-4)
				}
			})
		}
	}

	// A line already wider than the pane is NOT truncated and NOT padded: it
	// runs long rather than losing characters the user typed.
	long := strings.Repeat("x", 200)
	wf := NewWhereFilter(theme.Resolve("dark").Styles(), wfColumns, maxWidth)
	wf.Show()
	wf.SetInput(long)
	bar := ansi.Strip(wf.RenderInput())
	if !strings.Contains(bar, long) {
		t.Error("an over-long line was truncated: the text the user typed has to survive")
	}
	if got := ansi.StringWidth(wf.RenderInput()); got != ansi.StringWidth(" WHERE │ ")+len(long)+1 {
		t.Errorf("an over-long bar is %d cells, want the prefix plus the line plus the block", got)
	}
}

// Scenario: El popup no dibuja NADA cuando no hay nada que sugerir.
//
// An empty string rather than an empty box: the grid lays the popup into the view and
// counts its lines, so a box with no content would still take a line and shift the
// rows under it.
func TestWhereFilter_AnEmptyPopupRendersNothingAtAll(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(*WhereFilter)
	}{
		{"no suggestions", func(wf *WhereFilter) { wf.suggestions = nil }},
		{"the popup is hidden", func(wf *WhereFilter) { wf.showPopup = false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wf := newWF(t, "")
			tc.set(wf)
			if got := wf.RenderPopup(); got != "" {
				t.Errorf("the popup rendered %q, want nothing", ansi.Strip(got))
			}
		})
	}
}

// Scenario: El popup MARCA la seleccion y dibuja las flechas de scroll.
//
// Three facts in one render, and each is the user's only way of knowing where they are
// in a long list: the selected row is painted in the selection colour, an UP arrow
// appears above the window when there is anything above it, and a DOWN arrow below
// when there is anything below.
//
// The arrows are the reason a fixture cannot use a short list — with three suggestions
// there is never anything to scroll, so both arrows are absent and a test that only
// used one would prove nothing.
func TestWhereFilter_ThePopupMarksTheSelectionAndShowsScrollArrows(t *testing.T) {
	popupLines := func(wf *WhereFilter) []string {
		out := wf.RenderPopup()
		if out == "" {
			return nil
		}
		return strings.Split(out, "\n")
	}

	t.Run("a list that fits shows no arrows", func(t *testing.T) {
		wf := newWFBig(t)
		wf.suggestions = make([]Suggestion, 3)
		for i := range 3 {
			wf.suggestions[i] = Suggestion{Label: "row" + string(rune('a'+i)), Kind: "column", Detail: "text"}
		}
		wf.showPopup = true
		lines := popupLines(wf)
		// The popup is drawn inside a border, so there are two more lines
		// than there are rows. A count that ignored the border would be
		// measuring the frame.
		if len(lines) != 3+2 {
			t.Fatalf("the popup has %d lines, want 3 rows plus the 2 border lines and no arrows:\n%s", len(lines), strings.Join(lines, "\n"))
		}
		for i, l := range lines {
			if strings.Contains(ansi.Strip(l), "↑") || strings.Contains(ansi.Strip(l), "↓") {
				t.Errorf("line %d is a scroll arrow in a list that fits: %q", i, ansi.Strip(l))
			}
		}
	})

	t.Run("walking down brings in the up arrow and eventually the down one", func(t *testing.T) {
		wf := newWFBig(t)
		lines := popupLines(wf)
		if strings.Contains(ansi.Strip(lines[1]), "↑") {
			t.Error("the popup shows an up arrow before anything has been scrolled")
		}
		// The last CONTENT line, not the last line: the popup ends in a
		// border, so the down arrow is the line above it.
		if !strings.Contains(ansi.Strip(lines[len(lines)-2]), "↓") {
			t.Errorf("the popup has no down arrow with a long list and nothing scrolled:\n%s", strings.Join(lines, "\n"))
		}
		// The window does NOT scroll until the selection passes the last
		// visible row, so a single move down brings in no arrow at all.
		// That is the boundary: an implementation that scrolled on every
		// move would put the arrow up too early and shift the list under
		// the user.
		wf.moveSelection(1)
		if wf.scrollOffset != 0 {
			t.Errorf("one move down scrolled the window to %d, want it left at 0", wf.scrollOffset)
		}
		lines = popupLines(wf)
		if strings.Contains(ansi.Strip(lines[1]), "↑") {
			t.Errorf("one move down already brought in the up arrow:\n%s", strings.Join(lines, "\n"))
		}
		// Past the window it does.
		for range maxPopupHeight - 1 {
			wf.moveSelection(1)
		}
		if wf.scrollOffset == 0 {
			t.Fatalf("moving to the last visible row did not scroll the window")
		}
		lines = popupLines(wf)
		if !strings.Contains(ansi.Strip(lines[1]), "↑") {
			t.Errorf("after scrolling the first content line is %q, want the up arrow", ansi.Strip(lines[1]))
		}
		// Walking all the way round comes back to the first, and the
		// window follows it back to the top — so the down arrow is
		// there again. An implementation that left the window where it
		// was would put the selection above the visible slice.
		wf.selected = 0
		wf.ensureVisible()
		for range len(wf.suggestions) {
			wf.moveSelection(1)
		}
		if wf.selected != 0 {
			t.Errorf("a full lap of down ended at %d, want 0", wf.selected)
		}
		if wf.scrollOffset != 0 {
			t.Errorf("a full lap left the window at %d, want it back at 0", wf.scrollOffset)
		}
		lines = popupLines(wf)
		if !strings.Contains(ansi.Strip(lines[len(lines)-2]), "↓") {
			t.Error("back at the top of the list, the down arrow is gone")
		}

		// And the arrow is gone exactly when the LAST suggestion is the
		// last visible row.
		wf.selected = len(wf.suggestions) - 1
		wf.scrollOffset = len(wf.suggestions) - maxPopupHeight
		lines = popupLines(wf)
		for i, l := range lines {
			if strings.Contains(ansi.Strip(l), "↓") {
				t.Errorf("with the last suggestion on the last visible row, line %d is still a down arrow: %q", i, ansi.Strip(l))
			}
		}
	})

	t.Run("the selected row is painted differently from the others", func(t *testing.T) {
		wf := newWFBig(t)
		for _, sel := range []int{0, 1, 3, 7} {
			wf.selected = sel
			wf.scrollOffset = 0
			lines := popupLines(wf)

			// The selected ROW is located by its label, not by an index:
			// the number of lines above the rows depends on whether the
			// scroll arrows are showing, so an index is a guess about the
			// frame rather than an observation of the selection.
			want := paintOf(wf.styles.Selected)
			painted := 0
			for _, l := range lines {
				if !strings.Contains(ansi.Strip(l), wf.suggestions[sel].Label) {
					continue
				}
				for _, c := range cells(l) {
					if c[1].(paint) == want {
						painted++
					}
				}
			}
			if painted == 0 {
				t.Errorf("with the selection at %d no part of its row is painted in the selection colour:\n%s",
					sel, strings.Join(lines, "\n"))
			}
			// And no OTHER row is.
			others := 0
			for i, sug := range wf.suggestions {
				if i == sel {
					continue
				}
				for _, l := range lines {
					if !strings.Contains(ansi.Strip(l), sug.Label) {
						continue
					}
					for _, c := range cells(l) {
						if c[1].(paint) == want {
							others++
						}
					}
				}
			}
			if others > 0 {
				t.Errorf("with the selection at %d, %d cells of OTHER rows are painted as selected too", sel, others)
			}
		}
	})
}

// Scenario: La etiqueta se CORTA con puntos, y el tipo se rellena a su columna.
//
// Three fixed columns in the popup: the label (which can be arbitrarily long and is
// truncated so it cannot push the others off the line), the kind (always padded, so
// the details line up), and the detail (truncated if it is too long).
//
// The label is the one that matters: a column name is whatever the schema says, and a
// very long one would otherwise reflow the whole popup.
func TestWhereFilter_LabelsTruncateAndKindsPad(t *testing.T) {
	wf := newWFBig(t)

	t.Run("a long label is cut with an ellipsis", func(t *testing.T) {
		long := strings.Repeat("z", 80)
		got := ansi.Strip(wf.renderLabel(long, 15))
		if ansi.StringWidth(wf.renderLabel(long, 15)) != 15 {
			t.Errorf("a long label rendered %d cells wide, want exactly 15", ansi.StringWidth(wf.renderLabel(long, 15)))
		}
		if !strings.HasSuffix(got, "...") {
			t.Errorf("the truncated label is %q, want it to end with an ellipsis", got)
		}
	})

	t.Run("a short label is padded to the column", func(t *testing.T) {
		got := wf.renderLabel("id", 15)
		if ansi.StringWidth(got) != 15 {
			t.Errorf("a short label rendered %d cells wide, want 15", ansi.StringWidth(got))
		}
		if strings.TrimRight(ansi.Strip(got), " ") != "id" {
			t.Errorf("the padded label is %q, want it to read %q", ansi.Strip(got), "id")
		}
		// A label exactly as wide as the column is neither cut nor
		// padded.
		exact := wf.renderLabel(strings.Repeat("q", 15), 15)
		if ansi.Strip(exact) != strings.Repeat("q", 15) {
			t.Errorf("an exact-width label became %q", ansi.Strip(exact))
		}
		// And one cell over IS cut, because the truncation is strict.
		over := ansi.Strip(wf.renderLabel(strings.Repeat("q", 16), 15))
		if !strings.HasSuffix(over, "...") {
			t.Errorf("a label one cell over the column rendered %q, want it cut", over)
		}
	})

	t.Run("each kind has its own colour, and an unknown one falls back", func(t *testing.T) {
		// Four known kinds, four DIFFERENT colours. The test is a
		// permutation rather than a table of expected colours, so it keeps
		// working when the theme changes — and it still fails if two kinds
		// are painted the same, which is the mistake a table would hide.
		seen := map[string]string{}
		for _, kind := range []string{"column", "operator", "keyword", "value"} {
			got := wf.renderKind(kind, 10)
			if ansi.StringWidth(got) != 10 {
				t.Errorf("the kind %q rendered %d cells wide, want the column's 10", kind, ansi.StringWidth(got))
			}
			if !strings.Contains(ansi.Strip(got), kind) {
				t.Errorf("the kind %q rendered %q, want it to contain its own name", kind, ansi.Strip(got))
			}
			// Read the colour off the middle of the rendered run rather
			// than off the whole line, which also carries the border.
			colour := paintOfRun(got, kind)
			if other, dup := seen[colour]; dup {
				t.Errorf("the kinds %q and %q render in the same colour %q", other, kind, colour)
			}
			seen[colour] = kind
		}
		// An unknown kind falls back to the muted style AND prints its own
		// name, so an unexpected kind is visible rather than blank.
		unknown := wf.renderKind("something-else", 10)
		if !strings.Contains(ansi.Strip(unknown), "something-else") {
			t.Errorf("an unknown kind rendered %q, want its own name", ansi.Strip(unknown))
		}
		if got, want := paintOfRun(unknown, "something-else"), paintOfRun(wf.styles.TextMuted.Render("something-else"), "something-else"); got != want {
			t.Errorf("an unknown kind is painted %q, want the muted style %q", got, want)
		}
		// A kind wider than the column is left long rather than cut: the
		// source only pads. The kinds are fixed strings, so this is
		// unreachable in production and is here to say so.
		if got := ansi.StringWidth(wf.renderKind("a-very-long-kind", 4)); got != len("a-very-long-kind") {
			t.Errorf("an over-wide kind rendered %d cells, want it left long (%d): the source pads and never truncates",
				got, len("a-very-long-kind"))
		}
	})
}

// Scenario: Solo se escribe lo IMPRIMIBLE.
//
// The default branch of HandleKey filters on `unicode.IsPrint`, which excludes control
// characters — a terminal escape, a newline — from a clause that is about to be run
// against a database. The check is on the key's first BYTE, which is a separate and
// narrower thing (see the non-ASCII test above).
func TestWhereFilter_OnlyPrintableCharactersAreWritten(t *testing.T) {
	for _, tc := range []struct {
		name    string
		key     string
		wantIn  string
		handled bool
	}{
		{"a letter", "a", "a", true},
		{"a digit", "7", "7", true},
		{"a space, spelled out", "space", " ", true},
		{"an underscore", "_", "_", true},
		{"a quote", "'", "'", true},
		{"a newline is not printable", "\n", "", false},
		{"a tab is not printable", "\t", "", false},
		{"a bell is not printable", "\a", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wf := newWF(t, "")
			handled, _ := wf.HandleKey(strKey(tc.key))
			if handled != tc.handled {
				t.Errorf("the key %q was reported as handled=%v, want %v", tc.key, handled, tc.handled)
			}
			if wf.input != tc.wantIn {
				t.Errorf("the key %q wrote %q, want %q", tc.key, wf.input, tc.wantIn)
			}
		})
	}

	// And the predicate itself, which is what the branch above uses.
	for _, tc := range []struct {
		r    rune
		want bool
	}{
		{'a', true}, {'7', true}, {'_', true}, {'\'', true}, {' ', true},
		{'\n', false}, {'\t', false}, {0, false}, {0x7f, false},
	} {
		if got := isPrintable(tc.r); got != tc.want {
			t.Errorf("isPrintable(%q) = %v, want %v", tc.r, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// I5: the boundaries the arithmetic actually has
// ---------------------------------------------------------------------------

// Scenario: Las flechas se mueven HACIA su propia direccion, no simplemente cambian.
//
// The key handler does not wrap — moveSelection does. So the handler's own contribution
// is the SIGN of the delta it passes, and a handler that passed the wrong sign would
// still be caught by the test above, which only asserts the selection CHANGED. From the
// first row, up and down both move; from the middle, up and down differ. Both positions
// are needed, because the first only shows that a wrap happens and the second only that
// something moved: only the pair pins the direction.
func TestWhereFilter_TheArrowKeysMoveInTheirOwnDirection(t *testing.T) {
	for _, tc := range []struct {
		key  strKey
		want int // the delta the key must apply
	}{
		{"up", -1}, {"k", -1},
		{"down", 1}, {"j", 1},
	} {
		t.Run(string(tc.key), func(t *testing.T) {
			wf := newWFBig(t) // sixteen suggestions, so a wrap is not the only option
			if !wf.showPopup {
				t.Fatalf("the fixture has no popup to navigate (%s)", joinL(wf.suggestions))
			}
			n := len(wf.suggestions)

			// From the FIRST row, where up wraps to the last and down steps in.
			wf.selected = 0
			if handled, _ := wf.HandleKey(tc.key); !handled {
				t.Fatalf("%s with a popup was not handled", tc.key)
			}
			if want := (n + tc.want) % n; wf.selected != want {
				t.Errorf("from the first row %s left the selection at %d of %d, want %d",
					tc.key, wf.selected, n, want)
			}

			// From the MIDDLE, where nothing wraps and the two directions part.
			wf.selected = n / 2
			if handled, _ := wf.HandleKey(tc.key); !handled {
				t.Fatalf("%s from the middle was not handled", tc.key)
			}
			if want := n/2 + tc.want; wf.selected != want {
				t.Errorf("from the middle %s left the selection at %d, want %d",
					tc.key, wf.selected, want)
			}
		})
	}
}

// Scenario: Un popup MARCADO pero VACIO no se come las teclas.
//
// Every key arm guards on `wf.showPopup && len(wf.suggestions) > 0`, and the second
// conjunct looks redundant: showPopup is set to `len(suggestions) > 0` by
// updateSuggestions and cleared to false by the two paths that hide the popup, so no
// line ever sets it true over an empty list. That is why this state cannot be built
// through the widget — and why the conjunct looks like dead code to a reader.
//
// It is still the only thing standing between a marked-but-empty popup and a key that
// vanishes into the filter: with the conjunct dropped, "up" on an empty list would
// report itself handled and move nothing. This test builds the state directly, which is
// the only way to reach it, and pins what the guard decides.
//
// The dismissed-popup case is the mirror image and is asserted too, because it is the
// one a user can actually hit: escape closes the popup but keeps the list, so the keys
// must fall through to the grid rather than act on a list nobody is looking at.
func TestWhereFilter_APopupWithNothingInItDoesNotSwallowTheKeys(t *testing.T) {
	// emptyPopup returns a filter marked as showing a popup that has nothing in
	// it: the state the conjunct exists to decline.
	emptyPopup := func(t *testing.T) *WhereFilter {
		t.Helper()
		wf := newWFBig(t)
		if len(wf.suggestions) == 0 {
			t.Fatal("the fixture has no suggestions to empty")
		}
		wf.suggestions = nil
		wf.showPopup = true
		return wf
	}

	t.Run("tab and the arrows reach the grid instead", func(t *testing.T) {
		for _, key := range []strKey{"tab", "up", "k", "down", "j"} {
			t.Run(string(key), func(t *testing.T) {
				wf := emptyPopup(t)
				sel := wf.selected
				handled, changed := wf.HandleKey(key)
				if handled || changed {
					t.Errorf("%s over an empty popup reported handled=%v changed=%v, want both false so the key reaches the grid",
						key, handled, changed)
				}
				if wf.ShouldApply() {
					t.Errorf("%s over an empty popup applied the clause", key)
				}
				if wf.selected != sel {
					t.Errorf("%s over an empty popup moved the selection to %d", key, wf.selected)
				}
			})
		}
	})

	t.Run("enter applies the clause instead of accepting nothing", func(t *testing.T) {
		wf := emptyPopup(t)
		wf.input = "id = 1"

		if handled, _ := wf.HandleKey(strKey("enter")); !handled {
			t.Fatal("enter over an empty popup was not handled")
		}
		if !wf.ShouldApply() {
			t.Error("enter over an empty popup did not arm the apply")
		}
		if wf.Input() != "id = 1" {
			t.Errorf("enter applied %q, want what was typed", wf.Input())
		}
	})

	// The mirror: popup dismissed, list intact. Same outcome — the keys reach
	// the grid — from the opposite side of the guard.
	t.Run("a dismissed popup lets the keys through too", func(t *testing.T) {
		for _, key := range []strKey{"tab", "up", "k", "down", "j"} {
			t.Run(string(key), func(t *testing.T) {
				wf := newWFBig(t)
				wf.HandleKey(strKey("esc"))
				if wf.showPopup {
					t.Fatal("escape did not close the popup")
				}
				if len(wf.suggestions) == 0 {
					t.Fatal("escape emptied the suggestion list, so this half proves nothing")
				}
				if !wf.Visible() {
					t.Fatal("escape closed the whole filter, so the keys never reach the popup path")
				}
				line, sel := wf.input, wf.selected
				handled, changed := wf.HandleKey(key)
				if handled || changed {
					t.Errorf("%s with the popup dismissed reported handled=%v changed=%v, want both false",
						key, handled, changed)
				}
				if wf.input != line || wf.selected != sel {
					t.Errorf("%s with the popup dismissed changed the line to %q or the selection to %d",
						key, wf.input, wf.selected)
				}
			})
		}
	})
}

// Scenario: Aceptar con la seleccion JUSTO pasado el final no toca nada.
//
// The guard exists for a selection that is one past the list. Reaching that state
// through the keys is impossible — moveSelection wraps and filterSuggestions rebuilds
// the list — so the boundary has to be set directly, which is exactly what a guard
// against an out-of-range index is for. mustNotPanic is load-bearing here: the mutant
// that turns `>=` into `>` does not return a wrong answer, it indexes past the end.
func TestWhereFilter_AcceptingOutsideTheListIsDeclinedNotFatal(t *testing.T) {
	for _, tc := range []struct {
		name     string
		selected func(n int) int
	}{
		{"one past the last", func(n int) int { return n }},
		{"one before the first", func(n int) int { return -1 }},
		{"far past the end", func(n int) int { return 99 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wf := newWFBig(t)
			before := wf.input
			cursor := wf.cursor
			wf.selected = tc.selected(len(wf.suggestions))

			mustNotPanic(t, "accepting a suggestion from outside the list", func() {
				wf.acceptSuggestion()
			})
			if wf.input != before {
				t.Errorf("accepting from outside the list wrote %q, want the line untouched at %q", wf.input, before)
			}
			if wf.cursor != cursor {
				t.Errorf("accepting from outside the list moved the cursor to %d", wf.cursor)
			}
		})
	}
}

// Scenario: Una clausula que EMPIEZA por palabra clave tiene una clausula detras.
//
// extractLastClause looks for the last keyword and returns everything after it, so a
// leading keyword has to split too — the filter is fresh, the user has typed nothing,
// and a leading AND is a clause boundary, not part of a word. `pos == 0` is the
// boundary: `strings.LastIndex` reports it, and `pos >= 0` accepts it.
func TestWhereFilter_ALeadingKeywordAlsoStartsAFreshClause(t *testing.T) {
	wf := newWFBig(t)
	for _, tc := range []struct {
		name, input, want string
	}{
		{"a leading AND", " AND name = 2", "name = 2"},
		{"a leading OR", " OR name = 2", "name = 2"},
		{"a leading NOT", " NOT name = 2", "name = 2"},
		// And the three-step version, where the LAST keyword is still the one
		// that wins even though the first is at position zero.
		{"a leading keyword does not outrank a later one", " AND a = 1 OR b = 2", "b = 2"},
		// The boundary is zero, so one character in front of it must NOT split.
		{"a character in front keeps the keyword whole", "x AND b = 2", "b = 2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := wf.extractLastClause(tc.input); got != tc.want {
				t.Errorf("extractLastClause(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// Scenario: El token empieza en la primera posicion, no en la segunda.
//
// currentInputToken walks back from the cursor looking for a space, and the walk has to
// REACH index zero. The boundary is a cursor sitting at offset one with a space at
// offset zero — reachable, because a clause can start with a space (" AND x") and the
// cursor can be walked back onto it. Getting it wrong returns the space as if it were
// the word being typed, and every suggestion then filters against " ".
func TestWhereFilter_TheTokenUnderTheCursorStartsAtTheFirstCharacter(t *testing.T) {
	wf := newWF(t, "")
	// Type a space, then a word, then walk the cursor back onto the space.
	// The line is " x" with the cursor at 1.
	for _, key := range []strKey{"space", "x", "left"} {
		wf.HandleKey(key)
	}
	if wf.input != " x" || wf.cursor != 1 {
		t.Fatalf("the fixture is %q with the cursor at %d, want \" x\" at 1", wf.input, wf.cursor)
	}
	if got := wf.currentInputToken(); got != "" {
		t.Errorf("with the cursor just after the leading space the token is %q, want it empty: there is no word yet", got)
	}

	// One character further along the same line the token IS the word, which
	// is what makes the empty one above a boundary rather than a constant.
	same := newWF(t, "")
	for _, key := range []strKey{"space", "x"} {
		same.HandleKey(key)
	}
	if got := same.currentInputToken(); got != "x" {
		t.Errorf("with the cursor past the word the token is %q, want %q", got, "x")
	}
}

// Scenario: Una columna cuyo indice es JUSTO el final de una fila se salta.
//
// DistinctValues guards against indexing past a row, and a ragged projection — a query
// that returns fewer values than the result has columns — is the normal case it exists
// for. The boundary is colIndex == len(row): one past the last value the row HAS, not
// past the end of the slice the guard thinks about. mustNotPanic is load-bearing: the
// mutant that turns `>=` into `>` reaches row[colIndex] and panics.
func TestWhereFilter_AColumnIndexAtTheEndOfAShortRowIsSkipped(t *testing.T) {
	// Rows one value long: index 1 is exactly past the end of each row.
	ragged := [][]interface{}{{"a"}, {"b"}, {"c"}}

	mustNotPanic(t, "a column index exactly at the end of a short row", func() {
		if got := DistinctValues(1, ragged, 10); len(got) != 0 {
			t.Errorf("an index at the end of every row returned %q, want nothing", got)
		}
	})

	// The same index against rows that DO have it, so the assertion above is
	// about the boundary and not about the index being out of range always.
	mustNotPanic(t, "a column index at the end of rows long enough", func() {
		if got := DistinctValues(1, [][]interface{}{{"a", "x"}, {"b", "y"}}, 10); strings.Join(got, "|") != "x|y" {
			t.Errorf("the last column of a two-value row returned %q, want x|y", got)
		}
	})

	// A row that is long enough next to one that is not: the guard has to
	// decide PER ROW, not for the whole result.
	mixed := [][]interface{}{{"a", "x"}, {"b"}, {"c", "z"}}
	mustNotPanic(t, "a short row among long ones", func() {
		if got := DistinctValues(1, mixed, 10); strings.Join(got, "|") != "x|z" {
			t.Errorf("a short row among long ones returned %q, want x|z", got)
		}
	})
}
