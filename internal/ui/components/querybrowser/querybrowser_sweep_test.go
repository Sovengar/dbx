package querybrowser

// Scenario: Los supervivientes no-Boundary del sweep de querybrowser.go.
//
// Two of them are killed here: the tab marker's comparison at 277, and the selected
// row's own comparison at 344. The other in-scope pair (269's `modalH - 4` and 319's
// `maxVisible + 2`, which together decide the pad-to-fill target) is decided empirically
// against the border's own clipping, documented next to its verdict.

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Scenario: El marcador ► está en la pestaña activa.
//
// Kills CONDITIONALS_NEGATION 277 (`b.tab == 0` -> `!=`, which paints the History marker
// while the Favorites tab is active and vice versa). The existing tab test asserts that
// both tab NAMES are present, which both variants satisfy; the marker is what says which
// one is live, so the assertion has to be about the marker and its position.
func TestTheTabMarkerLivesOnTheActiveTab(t *testing.T) {
	b := newQB(t, "one", "two")

	view := ansi.Strip(b.View())
	if !strings.Contains(view, "► History") {
		t.Errorf("on the History tab the frame does not mark it: %q", view)
	}
	if strings.Contains(view, "► Favorites") {
		t.Errorf("on the History tab the Favorites tab is marked too: %q", view)
	}

	press(t, b, "tab")

	view = ansi.Strip(b.View())
	if !strings.Contains(view, "► Favorites") {
		t.Errorf("on the Favorites tab the frame does not mark it: %q", view)
	}
	if strings.Contains(view, "► History") {
		t.Errorf("on the Favorites tab the History tab is marked too: %q", view)
	}
}

// Scenario: Solo la fila del cursor lleva el estilo Selected.
//
// Kills CONDITIONALS_NEGATION 344 (`idx == b.cursor` -> `!=` in renderEntry, which styles
// every row BUT the cursor's). With the cursor on the second of three entries the dead
// code styles exactly one row, the mutant styles two, and the assertion about "exactly
// one, and it is the cursor's SQL" separates them.
func TestOnlyTheCursorRowIsStyled(t *testing.T) {
	b := newQB(t, "one", "two", "three")
	press(t, b, "down") // cursor on "two"

	sel := b.styles.Selected.Render("x")
	sig := sel[:strings.Index(sel, "x")]

	var marked []string
	for _, line := range strings.Split(b.View(), "\n") {
		if strings.Contains(line, sig) {
			marked = append(marked, ansi.Strip(line))
		}
	}

	if len(marked) != 1 {
		t.Fatalf("%d rows carry the Selected style, want exactly 1: %q", len(marked), marked)
	}
	if !strings.Contains(marked[0], "two") {
		t.Errorf("the highlighted row is %q, want the cursor's entry (two)", marked[0])
	}
}

// Scenario: El marco vacío no muestra el pie, y eso es un contrato.
//
// Kills ARITHMETIC_BASE 319 (`len(lines) < maxVisible+2` -> `maxVisible-2`, which stops
// the pad-to-fill loop four lines early: the footer then FITS inside the frame and the
// user sees it, while dead code overflows the inner height by exactly one line and the
// border clips the footer away). The empty store is the fixture where the loop runs at
// full height; a store with entries fills the window with content and clips the footer in
// both variants. The assertion pins the current geometry: an empty browser shows the
// title, the tabs and "No queries yet" — and no key hints.
func TestAnEmptyFrameClipsTheFooter(t *testing.T) {
	b := newQB(t)

	if strings.Contains(ansi.Strip(b.View()), "navigate") {
		t.Errorf("the empty frame shows the footer, want it clipped by the frame's own height")
	}
}
