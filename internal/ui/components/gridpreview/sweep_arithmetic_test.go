package gridpreview

// Scenario: Los supervivientes no-Boundary del sweep de preview.go.
//
// Every test here kills a specific mutant that survived a full run, and three of them are
// about ABSOLUTE assertions: the pre-existing popup tests diff the render against the same
// popup with jqSugSelected = -1, and a mutant that inverts `i == selected` inverts the
// sentinel too, so the relative diff still shows exactly one changed row. The highlight has
// to be asserted against the Selected style's own bytes, not against another render.

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// Scenario: La fila nueva aplica el filtro jq que el panel ya tenía.
//
// Kills CONDITIONALS_NEGATION 138 (`if p.jqExpr != ""` -> `==`): inverted, SetRow shows the
// RAW document when a filter is set and applies the (empty) filter when it is not. The
// empty direction is invisible — applyJQ with an empty expression falls back to the raw
// document itself — so the fixture asserts the direction that differs: a filter that would
// change what the panel shows must change it.
func TestSetRowAppliesTheHeldJQExpression(t *testing.T) {
	p := newPreviewTest()
	p.jqExpr = ".name"
	p.SetRow([]string{"name", "age"}, []interface{}{"ada", 36})

	got := strings.Join(p.lines, "\n")
	if got != `"ada"` {
		t.Errorf("the panel shows %q, want the filter's output %q", got, `"ada"`)
	}
}

// Scenario: Aceptar una sugerencia sin punto la añade donde está el cursor.
//
// Kills CONDITIONALS_NEGATION 889 col 12 (`dotPos == -1` -> `!=`, which sends the dot-less
// case down the dot branch and slices left[:0]) and the ARITHMETIC_BASE/INVERT_NEGATIVES
// pair on col 15 (`-1` -> `+1`, which makes LastIndex's "not found" read as "a dot at
// index 1" and takes the dot branch anyway). Both mutants agree on the observable: the
// input becomes "user" instead of "ususer".
func TestAcceptingADotlessSuggestionAppendsAtTheCursor(t *testing.T) {
	p := newPreviewTest()
	p.jqInput = "us"
	p.jqCursor = 2
	p.jqSugs = []JQSuggestion{{Path: "user", Type: "object"}}
	p.jqSugSelected = 0

	p.acceptJQSuggestion()

	if p.jqInput != "ususer" {
		t.Errorf("the input is %q, want %q", p.jqInput, "ususer")
	}
	if p.jqCursor != len([]rune("ususer")) {
		t.Errorf("the cursor is at %d, want %d", p.jqCursor, len([]rune("ususer")))
	}
}

// Scenario: La barra del prompt se rellena al ancho del panel, ni más ni menos.
//
// Kills the ARITHMETIC_BASE/INVERT_NEGATIVES pair on 1183 col 37 (`p.width-4` -> `+4`:
// the bar is padded eight columns too wide) and the pair on col 39 (`-barWidth` -> `+barWidth`:
// the repeat count includes the bar's own width, so it doubles it). The condition above the
// padding is Boundary scope; the two subtractions inside the count are this sweep's.
func TestTheJQPromptBarIsPaddedToThePanelWidth(t *testing.T) {
	p := previewOf(t, jqDoc)
	p.SetWidth(80)
	p.SetHeight(20)
	p.EnterJQMode()
	p.jqInput = "."
	p.jqCursor = 1
	p.jqSugVisible = false

	const want = 80 - 4
	if got := len([]rune(ansi_Strip(p.renderJQPrompt()))); got != want {
		t.Errorf("the prompt bar is %d columns, want the panel's %d", got, want)
	}
}

// Scenario: La línea con el cursor es la única rellenada al ancho del panel.
//
// Kills CONDITIONALS_NEGATION 1148 (`lineIdx == p.cursorLine` -> `!=`: every line BUT the
// cursor's is padded) and the ARITHMETIC_BASE/INVERT_NEGATIVES pair on 1149 (`p.width - 4`
// -> `+4`: the cursor's line is padded eight columns too wide). Counting the padded lines
// catches the first; requiring the padded line to be exactly width-4 catches the second.
func TestOnlyTheCursorLineIsPaddedToThePanelWidth(t *testing.T) {
	p := previewOf(t, jqDoc)
	p.SetWidth(80)
	p.SetHeight(20)
	p.cursorLine = 2

	const want = 80 - 4
	var padded []string
	for _, line := range strings.Split(ansi_Strip(p.Render()), "\n") {
		if len([]rune(line)) == want {
			padded = append(padded, line)
		}
	}
	if len(padded) != 1 {
		t.Fatalf("%d lines are padded to %d columns, want exactly the cursor's one:\n%s",
			len(padded), want, ansi_Strip(p.Render()))
	}
	if !strings.Contains(padded[0], strings.TrimSpace(p.lines[p.cursorLine])) {
		t.Errorf("the padded line is %q, want it to hold %q", padded[0], strings.TrimSpace(p.lines[p.cursorLine]))
	}
}

// Scenario: El resaltado del popup es absoluto: una sola fila, la seleccionada.
//
// Kills CONDITIONALS_NEGATION 1217 (`i == p.jqSugSelected` -> `!=`). The pre-existing test
// diffs against a jqSugSelected = -1 render, and the mutant inverts that sentinel too —
// under it, -1 highlights EVERY row and the diff still shows exactly one changed line. The
// bytes of the Selected style itself cannot be fooled that way.
func TestThePopupHighlightIsAbsoluteNotRelative(t *testing.T) {
	p := previewOf(t, jqDoc)
	p.SetWidth(100)
	p.SetHeight(20)
	p.EnterJQMode()
	p.jqInput = "."
	p.jqCursor = 1
	p.updateJQSuggestions()
	if len(p.jqSugs) < 4 {
		t.Fatalf("precondition failed: %d suggestions, want at least four", len(p.jqSugs))
	}

	// The Selected style's own bytes: the highlight is a background, and comparing renders
	// against a jqSugSelected = -1 render is what lets an inverted comparison pass — under
	// the mutant, -1 highlights EVERY row and the selected row is the only one left plain.
	marker := p.styles.Selected.Render("x")
	sig := marker[:strings.Index(marker, "x")]

	p.jqSugSelected = 1
	lines := popupLines(p)
	var withSig []int
	for i, line := range lines {
		if strings.Contains(line, sig) {
			withSig = append(withSig, i)
		}
	}
	if len(withSig) != 1 {
		t.Fatalf("%d rows carry the selection highlight, want exactly one: %v", len(withSig), withSig)
	}
	// The bordered box puts row i of the list at line i+1 of the render.
	if withSig[0] != 2 {
		t.Errorf("the highlight is on render line %d, want line 2 (suggestion 1 of the list)", withSig[0])
	}
}

// Scenario: El tipo empieza donde acaba la columna del camino, con el camino corto.
//
// Kills CONDITIONALS_NEGATION 1224 (`pathPad > 0` -> `<=`: the padding of a SHORT path is
// skipped and the type column slides left to just after the path). A path of one column
// against a 16-wide column is the loudest case: the gap collapses from 17 to 2.
func TestTheTypeColumnStartsAfterThePathColumn(t *testing.T) {
	p := previewOf(t, jqDoc)
	p.SetWidth(38) // maxWidth floors to 30, so pathWidth is 16
	p.SetHeight(20)
	p.EnterJQMode()
	p.jqSugVisible = true
	p.jqSugSelected = -1
	p.jqSugs = []JQSuggestion{{Path: "k", Type: "string"}}

	const pathWidth = 16
	row := popupText(p)[1]
	// Relative, not absolute: the border and its padding are not this test's business.
	// The path column is padded out to pathWidth and one space separates it from the
	// type, so the gap between the path's first character and the type is pathWidth+1.
	gap := strings.Index(row, "string") - strings.Index(row, "k")
	if gap != pathWidth+1 {
		t.Errorf("the type starts %d columns after the path in %q, want %d", gap, row, pathWidth+1)
	}
}

// Scenario: Ninguna fila del popup crece más allá del borde.
//
// Kills the ARITHMETIC_BASE/INVERT_NEGATIVES pair on 1229 (`typePad := 14 - width` -> `+`:
// the type is padded to 14 PLUS its own width, the row outgrows the box and the border
// wraps it, turning the popup into a staircase). The per-line width assertions of the
// layout tests still pass — a wrapped line is padded back to the box's width — so the
// line COUNT is the observable.
func TestTheSuggestionRowsDoNotOutgrowTheBox(t *testing.T) {
	p := previewOf(t, jqDoc)
	p.SetWidth(100)
	p.SetHeight(20)
	p.EnterJQMode()
	p.jqSugVisible = true
	p.jqSugSelected = -1
	p.jqSugs = []JQSuggestion{
		{Path: "k", Type: "string"},
		{Path: "j", Type: "object"},
	}

	out := p.renderJQSuggestions()
	if want := len(p.jqSugs) + 2; len(strings.Split(out, "\n")) != want {
		t.Errorf("the popup has %d lines, want %d (one per row plus the border):\n%s",
			len(strings.Split(out, "\n")), want, ansi_Strip(out))
	}
	if w := lipgloss.Width(ansi_Strip(out)); w != 100-8 {
		t.Errorf("the popup is %d columns wide, want %d", w, 100-8)
	}
}
