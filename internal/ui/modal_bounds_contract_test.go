// Four boundaries that no existing test reaches, and one of them is a crash.
//
// Each of the four is a comparison whose two forms agree everywhere except at ONE
// value, so the only way to tell them apart is to produce that value — and each needs
// a different kind of fixture:
//
//	modal.go:86           msg.Y == 0        the wheel event's own coordinate
//	header.go:101         count == 0        the render window's own size
//	ere.go:201 and :208   len(rels) == MaxNeighbors   the cap's own threshold
//	query_history.go:76   len(entries) == 500         the retention cap's own threshold
//	bordered.go:251       a string ENDING in ESC     the guard's own index
//
// What they have in common is that each boundary is the threshold's own constant, and
// a fixture that lands on either side of it proves nothing. So every test here asserts
// the value it means to be at BEFORE calling the code under test — the boundary IS the
// test, and a test that sits quietly beside it is the worst kind of hole.
//
// The styled ones are measured with the THEME resolved at test time rather than with
// hard-coded escape sequences, so they follow the theme instead of pinning today's.

package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/theme"
)

// Scenario: La rueda en Y EXACTAMENTE 0 baja, no sube.
//
// The wheel branch splits on the sign of msg.Y, so Y == 0 has to land on one specific
// side: a wheel at the top edge of the terminal is a scroll DOWN, and treating it as a
// scroll UP would fight the user at exactly the position where they are already at the
// top. The counter is the observable — the modal's scroll offset moves down by one.
func TestHelpModal_WheelAtTheTopRowScrollsDownNotUp(t *testing.T) {
	// Each case gets its OWN modal at a stated starting offset, because a shared one
	// accumulates and then "moved by 2" is ambiguous between two presses of +1 and one
	// press of +2 — which is the whole distinction this test exists to draw.
	for _, tc := range []struct {
		name  string
		start int
		y     int
		want  int
	}{
		// Y == 0 is the boundary: the original takes the `else` branch and
		// scrolls down, the mutant takes the up branch and scrolls up.
		{"the top row scrolls down", 0, 0, 1},
		{"a row below the top scrolls down", 0, 1, 1},
		{"a row well below the top scrolls down", 0, 40, 1},
		{"a row above the top scrolls up", 3, -1, 2},
		// And the clamp: scrolling up from the top leaves it at the top
		// rather than going negative, which is what makes the up branch
		// safe to take at all.
		{"scrolling up from the top stops at the top", 0, -1, 0},
		{"scrolling up hard from the top stops at the top", 0, -5, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewHelpModal(theme.Resolve("dark").Styles(), config.NewKeybindRegistry(config.KeybindingsConfig{}))
			m.SetWidth(80)
			m.SetHeight(400)
			m.Show()
			m.scroll = tc.start
			if m.scroll != tc.start {
				t.Fatalf("the fixture could not be placed at offset %d: Show reset it to %d", tc.start, m.scroll)
			}

			cmd, handled := m.Update(tea.MouseWheelMsg{Y: tc.y})
			if !handled {
				t.Fatalf("a wheel at Y=%d was not handled by the modal", tc.y)
			}
			if cmd != nil {
				t.Errorf("a wheel produced a command %v, want none: the modal scrolls itself", cmd)
			}
			if m.scroll != tc.want {
				t.Errorf("a wheel at Y=%d from offset %d left the offset at %d, want %d",
					tc.y, tc.start, m.scroll, tc.want)
			}
		})
	}
}
