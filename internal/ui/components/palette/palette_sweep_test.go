package palette

// Scenario: Los supervivientes no-Boundary del sweep de palette.go.
//
// Four groups: the item padding at 194-195 (a name wider than the modal, and the guard
// that keeps the pad count non-negative), the highlight's own counter and comparison at
// 199-205, and the upward-scroll branch at 222. What they share is that each mutant
// changes what the user SEES — a wider modal, a panic, a highlight on the wrong row, a
// window that scrolls when the cursor was already visible — so each is killed by
// asserting exactly that.

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// highlightedLine returns the index of the single rendered list line carrying the
// Selected style's escape prefix, or -1. The signature is the style's opening bytes up to
// the command name itself, the same technique the grid's sweep tests use.
func highlightedLine(p *Palette, lines []string) int {
	sel := p.styles.Selected.Width(p.width*6/10 - 4).Render("x")
	sig := sel[:strings.Index(sel, "x")]
	found := -1
	for i, line := range lines {
		if strings.Contains(line, sig) {
			if found != -1 {
				return -2 // more than one highlighted line: also a failure
			}
			found = i
		}
	}
	return found
}

// Scenario: Solo la fila del cursor está resaltada.
//
// Kills CONDITIONALS_NEGATION 199 (`cmdIdx == p.cursor` -> `!=`, which highlights every
// row BUT the cursor's) and INCREMENT_DECREMENT 205 (`cmdIdx++` -> `--`, which walks the
// counter away from every cursor position so nothing is highlighted at all). One fixture
// kills both: with the cursor on the second of three commands, the dead code highlights
// exactly that row, the flip highlights the other two, and the reversed counter highlights
// none.
func TestOnlyTheCursorRowIsHighlighted(t *testing.T) {
	p := withCommands(t, 3)
	p.cursor = 1

	lines := strings.Split(p.View(), "\n")

	switch got := highlightedLine(p, lines); {
	case got == -2:
		t.Errorf("more than one line is highlighted, want only the cursor's")
	case got < 0:
		t.Errorf("no line is highlighted, want the cursor's row")
	default:
		if !strings.Contains(ansi.Strip(lines[got]), "command-01") {
			t.Errorf("the highlighted row is %q, want the cursor's command-01", ansi.Strip(lines[got]))
		}
	}
}

// Scenario: Un nombre más ancho que el modal no rompe la lista.
//
// Kills CONDITIONALS_NEGATION 195 (the `padLen > 0` guard flipped to `<=`, which calls
// strings.Repeat with a NEGATIVE count for a name wider than the modal and panics the
// render). The two ARITHMETIC_BASE/INVERT_NEGATIVES pairs on 194 are proven equivalent
// in the allowlist — they only change the number of trailing spaces the border clips or
// pads back — so the assertions here pin the two observable invariants any future change
// to that line must keep: no panic for ANY name width, and no line past the modal.
func TestANameWiderThanTheModalIsPaddedSafely(t *testing.T) {
	p := withCommands(t, 1)
	p.commands = []command{{Name: strings.Repeat("n", 80), Section: SectionUI}}
	p.updateFiltered()

	view := p.View()

	modalW := p.width * 6 / 10
	if modalW > 80 {
		modalW = 80
	}
	for i, line := range strings.Split(view, "\n") {
		if w := lipgloss.Width(line); w > modalW+2 { // +2 for the border's own sides
			t.Errorf("line %d is %d cells wide, past the modal's %d: %q", i, w, modalW, ansi.Strip(line))
		}
	}
}

// Scenario: Subir el cursor devuelve la ventana a la fila del cursor.
//
// Kills ARITHMETIC_BASE 222 (the `p.scrollOffset+maxVisible` in the scroll-down branch
// mutated to `-`, which fires that branch for a cursor ABOVE the window too and, right
// after the scroll-up branch has set the offset to the cursor's line, drags it eleven
// lines further up). The walk down to command 30 gives the offset a value the upward move
// must undo; the dead code lands the cursor's line at the top of the window, the mutant
// scrolls past it.
func TestScrollingUpPutsTheCursorLineAtTheTopOfTheWindow(t *testing.T) {
	p := withCommands(t, 40)

	// Walk the cursor down the way the keys do, rendering at every stop.
	for cursor := 0; cursor <= 30; cursor++ {
		p.cursor = cursor
		_ = p.View()
	}

	// The offset now sits at the last page. One upward move must put the cursor's own
	// line at the top of the window.
	p.cursor = 5
	_ = p.View()

	// With a single section the header owns line 0 and command i owns line i+1.
	if want := 6; p.scrollOffset != want {
		t.Errorf("after scrolling up to command 5 the offset is %d, want the cursor's line %d at the top", p.scrollOffset, want)
	}
}
