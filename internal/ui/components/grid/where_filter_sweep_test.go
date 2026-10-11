package grid

// Scenario: Los supervivientes no-Boundary del sweep de where_filter.go.
//
// Three of these kill outright. The other two are PROVEN EQUIVALENT here rather than
// killed, and the proof is the point: the detail column is the last column of the row, so
// any change to its padding only lengthens or shortens a run of trailing spaces, and the
// border (BorderActive.Width(popupWidth)) pads that back out to the popup's width. The
// stripped line is byte-identical either way, which is why no render assertion can ever
// separate them and why they belong in the allowlist.

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/buble/dbx/internal/theme"
	"github.com/charmbracelet/x/ansi"
)

// lipglossWidth is the visible width of a rendered line, ANSI and all.
func lipglossWidth(s string) int { return lipgloss.Width(s) }

// sweepPopup builds a shown filter at maxWidth 60 carrying exactly one suggestion, so a
// change to the detail column cannot be absorbed by a second row next to it.
func sweepPopup(t *testing.T, selected int, detail string) string {
	t.Helper()
	wf := NewWhereFilter(theme.Resolve("dark").Styles(), wfColumns, 60)
	wf.Show()
	wf.suggestions = []Suggestion{{Label: "id", InsertText: "id", Kind: "column", Detail: detail}}
	wf.selected = selected
	return wf.RenderPopup()
}

// Scenario: El token actual es la última palabra antes del cursor.
//
// Kills ARITHMETIC_BASE and INVERT_NEGATIVES 470 (`lastSpace == -1` -> `== 1`, which reads
// "the space is at index 1" as "there is no space" and hands back the WHOLE input instead
// of the word after it). "a b" is the fixture because its space is at index 1, the one
// index the mutant mistakes for the not-found sentinel.
func TestTheCurrentTokenIsTheWordAfterTheLastSpace(t *testing.T) {
	wf := NewWhereFilter(theme.Resolve("dark").Styles(), wfColumns, 80)
	wf.SetInput("a b")
	wf.cursor = 3

	if got := wf.currentInputToken(); got != "b" {
		t.Errorf("the current token is %q, want %q", got, "b")
	}
}

// Scenario: La barra del prompt no se rellena más allá del ancho del panel.
//
// Kills ARITHMETIC_BASE and INVERT_NEGATIVES 543 (`barWidth < wf.maxWidth-4` -> `+4`, which
// widens the window in which the padding runs). The padding on the next line still
// subtracts the bar's own width, so for a bar 1..7 columns over the limit the repeat count
// goes negative and strings.Repeat panics; on dead code the same bar is simply not padded.
// 27 is the length that lands inside that window at maxWidth 40 (the bar is 10 wider than
// its input: the " WHERE │ " prefix and the cursor cell).
func TestThePromptBarIsNeverPaddedPastThePanelWidth(t *testing.T) {
	wf := NewWhereFilter(theme.Resolve("dark").Styles(), wfColumns, 40)
	wf.Show()
	wf.SetInput(strings.Repeat("x", 27))

	// A bar wider than maxWidth-4 is left alone, not padded and not panicked on.
	if got := lipglossWidth(wf.RenderInput()); got != 37 {
		t.Errorf("the bar is %d columns, want 37 (its natural width, unpadded)", got)
	}
}

// Scenario: Un detail más largo que su columna se corta con "..." y cabe en el popup.
//
// Kills CONDITIONALS_NEGATION 590 (`detailWidth > 14` -> `<=`, which inverts both arms: a
// short detail is "truncated" to a no-op and a LONG one is left whole, pushing the row past
// the border). The long detail is the arm that differs observably; the short arm is the
// trailing-space equivalence described below and cannot separate them.
func TestALongDetailIsTruncatedIntoItsColumn(t *testing.T) {
	const long = "a-very-long-detail-that-exceeds"

	for _, selected := range []int{-1, 0} {
		got := ansi.Strip(sweepPopup(t, selected, long))

		if strings.Contains(got, long) {
			t.Errorf("selected=%d: the popup shows the detail whole, want it cut to its column:\n%s",
				selected, got)
		}
		if !strings.Contains(got, "a-very-long...") {
			t.Errorf("selected=%d: the popup does not show the truncated detail:\n%s", selected, got)
		}
		if lines := strings.Split(got, "\n"); len(lines) != 3 {
			t.Errorf("selected=%d: the popup is %d lines, want 3 (border, one row, border):\n%s",
				selected, len(lines), got)
		}
	}
}

// Scenario: El padding del detail es whitespace final, y el borde lo rellena igual.
//
// NOT a kill — the equivalence proof for the two mutants no render can separate.
//
// ARITHMETIC_BASE and INVERT_NEGATIVES 556 (`detailPad := 14 - detailWidth` -> `+`, which
// pads the detail to 14 PLUS its own width) and CONDITIONALS_NEGATION 595 (`detailPad > 0`
// -> `<=`, which skips that padding for every padded detail). Both only change how many
// SPACES follow the detail text, and the detail is the last column: a longer or shorter
// row is padded back to popupWidth by the border, so the stripped line is identical. This
// asserts that fact directly, on both a short detail (556 over-pads, 595 under-pads) and a
// long one, for a plain row and a selected one.
func TestTheDetailPaddingIsInvisibleTrailingWhitespace(t *testing.T) {
	for _, tc := range []struct{ name, detail string }{
		{"a short detail", "eq"},
		{"a long detail", "a-very-long-detail-that-exceeds"},
	} {
		for _, selected := range []int{-1, 0} {
			got := ansi.Strip(sweepPopup(t, selected, tc.detail))

			// The row fits the popup exactly: no wrapped blank line, the full content
			// width (maxWidth-6), and the detail still readable. Any mutant that made the
			// padding VISIBLE would break one of these, which is the whole argument for
			// the allowlist.
			if lines := strings.Split(got, "\n"); len(lines) != 3 {
				t.Errorf("%s selected=%d: the popup is %d lines, want 3:\n%s",
					tc.name, selected, len(lines), got)
			}
			for _, line := range strings.Split(got, "\n")[1:2] {
				if w := lipglossWidth(line); w != 60-6 {
					t.Errorf("%s selected=%d: the row is %d columns, want the popup's %d:\n%s",
						tc.name, selected, w, 60-6, got)
				}
			}
		}
	}
}
