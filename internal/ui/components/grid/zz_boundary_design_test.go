package grid

// Boundary proofs for where_filter.go: the exact-equality fixture for each of the
// 13 LIVED CONDITIONALS_BOUNDARY mutants (report.json, where_filter.go lines 268,
// 363, 369, 394, 543, 557, 564, 572, 590, 595, 627, 632, 654).
//
// A boundary mutant (`<` -> `<=`, `>` -> `>=`, `>=` -> `>`) differs from the
// original on EXACTLY ONE input: equality of the two operands. Each test below
// builds that equality (and guards the fixture so the equality really holds), then
// asserts the original's observable behaviour. Running the suite under each mutant
// leaves every assertion green: at equality the mutated branch is a no-op (an
// assignment of the value already held, strings.Repeat(" ", 0), or
// ansi.Truncate at a string's exact width), or the equality is unreachable. That is
// a proof of equivalence, not a test gap — these belong in .mutation-allowlist.
//
// Verified by applying each mutant one at a time and re-running this file.

import (
	"strings"
	"testing"

	"github.com/buble/dbx/internal/theme"
	"github.com/charmbracelet/x/ansi"
)

func bndStyles() *theme.Styles { return theme.Resolve("dark").Styles() }

// LINE 268: `wf.selected < wf.scrollOffset` -> `<=`
// Boundary: selected == scrollOffset.
func TestBnd268SelectionAtTheTopOfTheWindow(t *testing.T) {
	wf := NewWhereFilter(bndStyles(), wfColumns, 80)
	wf.Show()
	wf.suggestions = make([]Suggestion, 20)
	wf.selected, wf.scrollOffset = 5, 5 // equality
	wf.ensureVisible()
	if wf.scrollOffset != 5 {
		t.Errorf("scrollOffset = %d, want 5 (the selection already IS the top of the window)", wf.scrollOffset)
	}
	if wf.selected < wf.scrollOffset || wf.selected >= wf.scrollOffset+maxPopupHeight {
		t.Errorf("after ensureVisible selected=%d scrollOffset=%d leaves the selection outside the window", wf.selected, wf.scrollOffset)
	}
}

// LINE 363: `if end > bestPos` -> `>=`; LINE 369: `if bestPos >= 0` -> `>`.
// Boundary 363: end == bestPos. Boundary 369: bestPos == 0.
func TestBnd363369ExtractLastClauseBoundaries(t *testing.T) {
	wf := NewWhereFilter(bndStyles(), wfColumns, 80)
	for _, tc := range []struct{ name, input, want string }{
		{"last keyword wins (AND then OR)", "id = 1 AND name = 2 OR age = 3", "age = 3"},
		{"last keyword wins (OR then AND)", "id = 1 OR name = 2 AND age = 3", "age = 3"},
		{"leading keyword (pos 0 is the boundary of >= 0)", " AND name = 2", "name = 2"},
		{"no keyword at all (bestPos stays -1)", "id = 1", "id = 1"},
		{"trailing keyword (clause ends empty)", "id = 1 AND ", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := wf.extractLastClause(tc.input); got != tc.want {
				t.Errorf("extractLastClause(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// LINE 394: `current.Len() > 0` -> `>= 0`.
// Boundary: current.Len() == 0 when the loop ends.
func TestBnd394TheFinalTokenIsNeverEmpty(t *testing.T) {
	wf := NewWhereFilter(bndStyles(), wfColumns, 80)
	for _, in := range []string{"a", "a b", "  a b  ", "a\tb", "a ", " a", "\ta\t", " ", "", "a  b "} {
		got := wf.tokenize(in)
		for i, tk := range got {
			if tk == "" {
				t.Errorf("tokenize(%q) produced an empty token at %d: %q", in, i, got)
			}
		}
	}
	if got := wf.tokenize("a b "); strings.Join(got, "|") != "a|b" {
		t.Errorf("tokenize(%q) = %q, want a|b", "a b ", got)
	}
}

// LINE 543: `barWidth < wf.maxWidth-4` -> `<=`.
// Boundary: barWidth == maxWidth-4 (26-char input at maxWidth 40: 9 prefix + 26 + 1 cursor = 36 == 40-4).
func TestBnd543BarExactlyPanelWidthIsNotPadded(t *testing.T) {
	const maxWidth = 40
	wf := NewWhereFilter(bndStyles(), wfColumns, maxWidth)
	wf.Show()
	in := strings.Repeat("x", 26)
	wf.SetInput(in)

	natural := ansi.StringWidth(" WHERE │ ") + len(in) + 1
	if natural != maxWidth-4 {
		t.Fatalf("fixture: the bar's natural width is %d, want exactly %d (the boundary)", natural, maxWidth-4)
	}
	if got := ansi.StringWidth(wf.RenderInput()); got != maxWidth-4 {
		t.Errorf("the bar is %d cells, want exactly %d", got, maxWidth-4)
	}
}

// LINE 557: `popupWidth < 40` -> `<=`.
// Boundary: popupWidth == 40 → maxWidth 46 (46-6 == 40). 45 is the clamping side (39 -> 40).
func TestBnd557PopupWidthAtTheClampBoundary(t *testing.T) {
	for _, tc := range []struct {
		maxWidth      int
		wantUnclamped int
	}{
		{45, 39}, // clamped up to 40
		{46, 40}, // exactly 40, no clamp needed
	} {
		wf := NewWhereFilter(bndStyles(), wfColumns, tc.maxWidth)
		wf.Show()
		wf.suggestions = []Suggestion{{Label: "id", InsertText: "id", Kind: "column", Detail: "text"}}
		wf.selected = -1
		popup := wf.RenderPopup()
		if popup == "" {
			t.Fatalf("maxWidth %d: the popup rendered nothing", tc.maxWidth)
		}
		lines := strings.Split(ansi.Strip(popup), "\n")
		if len(lines) != 3 {
			t.Fatalf("maxWidth %d: the popup is %d lines, want 3:\n%s", tc.maxWidth, len(lines), popup)
		}
		// The content width of every row is popupWidth, 40 in both cases.
		if w := ansi.StringWidth(lines[1]); w != 40 {
			t.Errorf("maxWidth %d (unclamped %d): the row is %d cells, want 40", tc.maxWidth, tc.wantUnclamped, w)
		}
	}
}

// LINE 564: `end > len(wf.suggestions)` -> `>=`.
// Boundary: end == len(suggestions) → 8 suggestions, scrollOffset 0 (end = 0+8 = 8).
func TestBnd564WindowEndExactlyAtTheListEnd(t *testing.T) {
	wf := NewWhereFilter(bndStyles(), wfColumns, 80)
	wf.Show()
	wf.suggestions = make([]Suggestion, maxPopupHeight) // 8 == 0 + maxPopupHeight
	wf.scrollOffset, wf.selected = 0, 0
	wf.showPopup = true
	if len(wf.suggestions) != wf.scrollOffset+maxPopupHeight {
		t.Fatalf("fixture: %d suggestions, want the window to end exactly on the last one", len(wf.suggestions))
	}
	stripped := ansi.Strip(wf.RenderPopup())
	if stripped == "" {
		t.Fatal("the popup rendered nothing")
	}
	if strings.Contains(stripped, "↓") {
		t.Errorf("a down arrow with the window ending exactly on the last suggestion:\n%s", stripped)
	}
	if n := len(strings.Split(stripped, "\n")); n != maxPopupHeight+2 {
		t.Errorf("the popup has %d lines, want %d rows + 2 borders:\n%s", n, maxPopupHeight, stripped)
	}
}

// LINE 572: `labelWidth < 15` -> `<=`.
// Boundary: labelWidth == 15 → popupWidth 45 → maxWidth 51 (51-6-10-14-6 = 15).
func TestBnd572LabelWidthAtItsFloor(t *testing.T) {
	const maxWidth = 51
	wf := NewWhereFilter(bndStyles(), wfColumns, maxWidth)
	wf.Show()
	if got := (maxWidth - 6) - 10 - 14 - 6; got != 15 {
		t.Fatalf("fixture: the label column computes to %d, want exactly 15 (the boundary)", got)
	}
	wf.suggestions = []Suggestion{{Label: "id", InsertText: "id", Kind: "column", Detail: "text"}}
	wf.selected = -1
	stripped := ansi.Strip(wf.RenderPopup())
	// label padded to 15: "id" + 13 spaces, then the space that separates the columns.
	want := "  id" + strings.Repeat(" ", 13) + " column"
	if !strings.Contains(stripped, want) {
		t.Errorf("the popup does not show the label padded to its 15-cell column:\n%s", stripped)
	}
}

// LINE 590: `detailWidth > 14` -> `>=`; LINE 595: `detailPad > 0` -> `>=`.
// Boundary: detail visible width == 14 (pad == 0).
func TestBnd590595DetailExactlyItsColumn(t *testing.T) {
	detail := strings.Repeat("d", 14)
	styled := bndStyles().TextMuted.Render(detail)
	if got := ansi.StringWidth(styled); got != 14 {
		t.Fatalf("fixture: the rendered detail is %d cells, want exactly 14 (the boundary)", got)
	}
	// The property the 590 mutant rests on: truncating to a string's EXACT width
	// must be a byte-level no-op, escape sequences included.
	if got := ansi.Truncate(styled, 14, "..."); got != styled {
		t.Errorf("ansi.Truncate at the exact width changed the detail:\n before %q\n after  %q", styled, got)
	}
	raw := sweepPopup(t, -1, detail)
	if !strings.Contains(raw, styled) {
		t.Errorf("the popup does not carry the detail's own rendering whole:\n%s", raw)
	}
	for _, selected := range []int{-1, 0} {
		stripped := ansi.Strip(sweepPopup(t, selected, detail))
		if !strings.Contains(stripped, detail) {
			t.Errorf("selected=%d: a detail of exactly 14 cells was not kept whole:\n%s", selected, stripped)
		}
		if strings.Contains(stripped, "...") {
			t.Errorf("selected=%d: a detail of exactly 14 cells was truncated:\n%s", selected, stripped)
		}
		if n := len(strings.Split(stripped, "\n")); n != 3 {
			t.Errorf("selected=%d: the popup is %d lines, want 3:\n%s", selected, n, stripped)
		}
	}
}

// LINE 627: `width > maxWidth` -> `>=`; LINE 632: `pad > 0` -> `>=`.
// Boundary: rendered width == maxWidth (pad == 0).
func TestBnd627632LabelExactlyTheColumnWidth(t *testing.T) {
	wf := NewWhereFilter(bndStyles(), wfColumns, 80)
	label := strings.Repeat("q", 15)
	got := wf.renderLabel(label, 15)
	// Byte identity, not just visible width: renderLabel must not touch a label
	// that already fits, escape sequences included.
	if want := wf.styles.Text.Render(label); got != want {
		t.Errorf("renderLabel at the exact column width = %q, want it unchanged (%q)", got, want)
	}
	if ansi.Strip(got) != label {
		t.Errorf("renderLabel at the exact column width stripped to %q, want %q", ansi.Strip(got), label)
	}
	if w := ansi.StringWidth(got); w != 15 {
		t.Errorf("the exact-width label rendered %d cells, want 15", w)
	}
	// And the boundary has a live neighbour on both sides.
	if w := ansi.StringWidth(wf.renderLabel(strings.Repeat("q", 16), 15)); w != 15 {
		t.Errorf("a label one cell over rendered %d cells, want 15", w)
	}
	if w := ansi.StringWidth(wf.renderLabel(strings.Repeat("q", 14), 15)); w != 15 {
		t.Errorf("a label one cell under rendered %d cells, want 15", w)
	}
}

// LINE 654: `w < width` -> `<=`.
// Boundary: rendered kind width == width → renderKind("column", 6): "column" is 6 cells.
func TestBnd654KindExactlyItsColumn(t *testing.T) {
	wf := NewWhereFilter(bndStyles(), wfColumns, 80)
	if got := ansi.StringWidth(wf.styles.Success.Render("column")); got != 6 {
		t.Fatalf("fixture: the rendered kind is %d cells, want exactly 6 (the boundary)", got)
	}
	got := wf.renderKind("column", 6)
	if w := ansi.StringWidth(got); w != 6 {
		t.Errorf("renderKind at the exact column width rendered %d cells, want 6", w)
	}
	if !strings.Contains(ansi.Strip(got), "column") {
		t.Errorf("renderKind = %q, want it to contain its own name", ansi.Strip(got))
	}
	// Neighbours: one under pads, one over is left long.
	if w := ansi.StringWidth(wf.renderKind("value", 6)); w != 6 {
		t.Errorf("a kind one cell under rendered %d cells, want 6", w)
	}
	if w := ansi.StringWidth(wf.renderKind("keyword", 6)); w != 7 {
		t.Errorf("a kind one cell over rendered %d cells, want 7 (pads never truncate)", w)
	}
}
