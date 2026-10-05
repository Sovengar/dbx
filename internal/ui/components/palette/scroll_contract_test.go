package palette

// Scenario: El scroll del palette, que tiene tres pisos y solo uno puede dispararse.
//
// View clamps scrollOffset three times, in this order:
//
//	totalLines <= maxVisible        -> 0        (everything fits)
//	totalLines >  maxVisible        -> the last page
//	scrollOffset < 0                -> 0        (the cursor moved above the first line)
//
// Only the middle one is interesting, because it is the one that fires when the list is
// longer than a window AND the cursor is already at the bottom — the state a user reaches by
// holding the down arrow. A test that never scrolls there leaves it uncovered, which is what
// happened: the palette's list is twelve lines tall and every test in the package fit inside
// it.

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/theme"
)

// withCommands builds a visible palette showing the given number of commands.
func withCommands(t *testing.T, n int) *Palette {
	t.Helper()
	p := New(theme.Resolve("dark").Styles(), config.NewKeybindRegistry(config.KeybindingsConfig{}))
	p.commands = make([]command, n)
	for i := range p.commands {
		p.commands[i] = command{Name: fmt.Sprintf("command-%02d", i), Section: SectionUI}
	}
	p.updateFiltered()
	p.Show()
	p.SetWidth(60)
	p.SetHeight(30)
	return p
}

// Fewer commands than a window: the offset must be zero whatever it was.
//
// The offset is set to something absurd first, because a fresh palette has zero and zero
// would pass this without the clamp running.
func TestAPaletteThatFitsResetsItsScroll(t *testing.T) {
	// Up to eight, and the number is not arbitrary: the rendered list is the query box, a
	// blank, a section header, then one line per command — four lines of chrome. So eight
	// commands is twelve lines and fits, and twelve commands is sixteen and does not. The
	// first version of this case used twelve, reported the clamp broken, and the code was
	// right; guessing the boundary from the constant alone is what made it wrong.
	for _, n := range []int{0, 1, 2, 5, 8} {
		p := withCommands(t, n)
		p.scrollOffset = 999

		_ = p.View()

		if p.scrollOffset != 0 {
			t.Errorf("with %d commands the offset stayed at %d, want 0", n, p.scrollOffset)
		}
	}
}

// More commands than a window: the offset must never point past the last page.
//
// The clamp is `totalLines - maxVisible`, and the failure it prevents is a window scrolled
// past its content — which renders as blank rows under the list, so the palette looks empty
// while holding twenty commands.
func TestAPaletteThatOverflowsNeverScrollsPastItsContent(t *testing.T) {
	const commands = 30

	for _, offset := range []int{0, 5, 17, 18, 19, 500} {
		p := withCommands(t, commands)
		p.scrollOffset = offset

		out := p.View()

		if p.scrollOffset < 0 {
			t.Errorf("offset %d became %d", offset, p.scrollOffset)
		}
		if p.scrollOffset > commands-12 {
			t.Errorf("offset %d stayed at %d, past the last page of %d lines", offset, p.scrollOffset, commands)
		}
		if out == "" {
			t.Fatalf("at offset %d the palette rendered nothing", offset)
		}
	}
}

// The negative clamp. Only reachable through the cursor: View sets the offset from
// cmdLineMap[cursor], and that is never negative — so the guard protects against a cursor
// moving up past the first line while the offset had already been pushed negative by the
// cursor-above-window branch.
func TestANegativeScrollOffsetIsBroughtBack(t *testing.T) {
	p := withCommands(t, 30)
	p.scrollOffset = -5

	_ = p.View()

	if p.scrollOffset < 0 {
		t.Errorf("the offset is still %d after rendering", p.scrollOffset)
	}
}

// The whole scroll, driven by keys rather than by poking the field: down past the end, then
// back up past the start. This is the user-visible version of the three clamps.
func TestScrollingThePaletteWithKeysStaysInBounds(t *testing.T) {
	p := withCommands(t, 30)

	press := func(key string, n int) {
		t.Helper()
		for range n {
			if cmd, handled := p.Update(tea.KeyPressMsg{Code: rune(key[0]), Text: key}); handled && cmd != nil {
				_ = cmd()
			}
		}
		_ = p.View()
	}

	press("j", 100)
	if p.scrollOffset <= 0 {
		t.Errorf("scrolling down 100 times left the offset at %d, so the window never followed the cursor", p.scrollOffset)
	}
	if !strings.Contains(p.View(), "command-29") {
		t.Error("scrolling to the end does not show the last command")
	}

	press("k", 200)
	// The offset is NOT expected to be zero here, and the reason is worth writing down: the
	// first command sits on line 1 because line 0 is the query box, so "the cursor is on the
	// first command" is offset 1, not offset 0. Asserting 0 was the first version of this case
	// and it failed against correct code.
	out := p.View()
	if !strings.Contains(out, "command-00") {
		t.Errorf("scrolling back to the top does not show the first command: offset=%d", p.scrollOffset)
	}
	if p.scrollOffset > 30-12 {
		t.Errorf("the offset is %d, past the last page", p.scrollOffset)
	}
}

// The invariant the removed clamp used to check, asserted by exhaustion instead of by
// arithmetic.
//
// The clamp existed because nothing proved the writer kept the offset legal. Now the proof
// IS the test: for every list length from empty to well past a window, and for every cursor
// position in it, the rendered offset is inside the legal range. A change to the cursor
// branch that broke the invariant would fail here rather than show a palette scrolled into
// empty space.
func TestTheScrollOffsetIsAlwaysLegal(t *testing.T) {
	const maxVisible = 12

	for n := range 40 {
		p := withCommands(t, n)

		// Walk the cursor the way the keys do, rendering at every stop so the cursor branch
		// runs once per position — which is where the offset is written.
		for cursor := 0; cursor < n; cursor++ {
			p.cursor = cursor
			_ = p.View()

			if p.scrollOffset < 0 {
				t.Fatalf("n=%d cursor=%d: the offset is %d", n, cursor, p.scrollOffset)
			}
			total := n + 1 // the section header
			if maxLegal := max(0, total-maxVisible); p.scrollOffset > maxLegal {
				t.Fatalf("n=%d cursor=%d: the offset is %d, past the last page of %d lines",
					n, cursor, p.scrollOffset, total)
			}
		}
	}
}

// max is here because the test file targets a Go version whose predeclared max would shadow
// nothing but is worth being explicit about: this is a two-number comparison, not a
// refactor.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
