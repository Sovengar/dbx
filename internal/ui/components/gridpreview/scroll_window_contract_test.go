package gridpreview

// Scenario: The preview's scroll arithmetic and its visible window, asserted on exact values.
//
// Every case here is about a number that appears once in a subtraction and then again in the
// clamp that follows it, which is the shape that has produced every dead guard and every
// off-by-one in this package. A test that only checks "the view is not empty" cannot see any
// of it: the view is non-empty whether the scroll is 0 or 3 or 300.
//
// So the assertions are on the numbers themselves -- scrollY after a known move, the exact set
// of lines in the window, which one of them is highlighted -- and on the boundaries, because
// each of these is a `>` that becomes `>=` under mutation and the two agree everywhere except
// at the value itself.
//
// The clamps that CANNOT be killed are not asserted as if they could. Six of them assign the
// value the variable already holds, and they are in .mutation-allowlist with the proof written
// out there. What is left here is what a test can actually distinguish.

import (
	"fmt"
	"strings"
	"testing"
)

// linesPreview is a preview over a document with n keys, 80 wide and 20 tall.
//
// The braces are lines too: a document of n keys renders as n+2 lines, and the expectations
// below are all in terms of LINES, so getting that wrong by two would have made every case
// quietly assert the wrong thing. The key a line carries is its index minus one, for the
// opening brace, and keyIndex reads it back out of the rendered text.
//
// TWO content heights exist for one panel, and the cases below use both:
//
//   - Render draws height-4 == 16 lines.
//   - ensureCursorVisible SCROLLS as if height-6 == 14 were visible.
//
// So a document of 42 lines has a maxScroll of 28 by the scroll rule and could render 16. That
// inconsistency is pinned in TestTheScrollArithmeticUsesTwoDifferentHeights; the expectations
// here use whichever of the two the function under test actually uses, because a test that
// guessed wrong would fail for the wrong reason and teach nobody anything.
func linesPreview(t *testing.T, keys int) *GridPreview {
	t.Helper()
	p := previewOf(t, wideDoc(keys))
	p.SetWidth(80)
	p.SetHeight(20)
	if got, want := len(p.lines), keys+2; got != want {
		t.Fatalf("precondition failed: %d lines for %d keys, want %d (the braces are lines)", got, keys, want)
	}
	return p
}

// keyOfLine is the key a rendered line shows, which is its document index minus the brace.
func keyOfLine(t *testing.T, rendered []string, i int) int {
	t.Helper()
	if i < 0 || i >= len(rendered) {
		t.Fatalf("asked for rendered line %d of %d", i, len(rendered))
	}
	return keyIndex(t, rendered[i])
}

// The visible window: which lines, and which one is highlighted.
//
// The window is `start := scrollY; end := start + contentHeight` with end clamped to the last
// line. Three things can be wrong and a non-empty view hides all of them: the start, the end,
// and the mapping from a position in the window back to a line index. The third is the
// interesting one -- `lineIdx := start + i` is the only place the window's offset is undone,
// and getting it wrong highlights the wrong row while showing exactly the right number of rows.
func TestTheVisibleWindowIsTheSliceTheScrollOffsetAsksFor(t *testing.T) {
	// Render's contentHeight is height-4 == 16 and the document is 42 lines.
	for _, tc := range []struct {
		name       string
		scrollY    int
		cursorLine int
	}{
		{"at the top", 0, 0},
		{"scrolled down", 10, 12},
		{"the cursor on the first visible line", 10, 10},
		{"the cursor on the last visible line", 10, 25},
		{"scrolled as far as the content goes", 24, 24},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := linesPreview(t, 40)
			p.scrollY = tc.scrollY
			p.cursorLine = tc.cursorLine

			shown := splitRendered(p.Render())

			// The window is [scrollY, scrollY+16) with the end clamped to the last line.
			wantFirst := tc.scrollY
			if wantFirst > len(p.lines) {
				wantFirst = len(p.lines)
			}
			wantEnd := tc.scrollY + 16
			if wantEnd > len(p.lines) {
				wantEnd = len(p.lines)
			}

			if got := keyOfLine(t, shown, 0); got != wantFirst-1 {
				t.Errorf("the first visible line is key%02d, want key%02d -- the window starts at scrollY=%d",
					got, wantFirst-1, tc.scrollY)
			}
			if got := keyOfLine(t, shown, len(shown)-1); got != wantEnd-2 {
				t.Errorf("the last visible line is key%02d, want key%02d (window [%d, %d) of %d lines)",
					got, wantEnd-2, tc.scrollY, wantEnd, len(p.lines))
			}
			// And the count, which is what an off-by-one in the end clamp moves.
			if len(shown) != wantEnd-wantFirst {
				t.Errorf("%d lines rendered, want %d", len(shown), wantEnd-wantFirst)
			}

			// Exactly one line is highlighted -- the cursor's. A window that highlighted
			// everything, or nothing, or the wrong row, still has the right number of lines.
			//
			// The index is into the RENDERED window, so it is offset by scrollY: turning that
			// offset back into a line index is what `lineIdx := start + i` does, and asserting
			// it here is what makes that line load-bearing rather than incidental.
			got := highlightedIndices(t, p, tc.cursorLine)
			if len(got) != 1 {
				t.Fatalf("%d rendered lines are highlighted, want exactly one", len(got))
			}
			if line := tc.scrollY + got[0]; line != tc.cursorLine {
				t.Errorf("the highlighted line is %d (scrollY %d + row %d), want the cursor at %d",
					line, tc.scrollY, got[0], tc.cursorLine)
			}
		})
	}
}

// ensureCursorVisible, through the render: a cursor below the window scrolls to it, and one
// already inside it does not move.
//
// The two directions are the whole function. A scroll that only ever followed the cursor down
// would pass every downward case and show a user who scrolls up with a cursor that stays put.
func TestMovingTheCursorScrollsOnlyWhenItLeavesTheWindow(t *testing.T) {
	// ensureCursorVisible's contentHeight is height-6 == 14, which is why the cursors below
	// start at 14. See TestTheScrollArithmeticUsesTwoDifferentHeights for why that is not 16.
	t.Run("a cursor below the window is scrolled to", func(t *testing.T) {
		for _, cursor := range []int{14, 15, 20, 41} {
			p := linesPreview(t, 40)
			p.scrollY = 0
			p.cursorLine = cursor
			p.ensureCursorVisible()

			want := cursor - 14 + 1
			if want < 0 {
				want = 0
			}
			if maxScroll := len(p.lines) - 14; want > maxScroll {
				want = maxScroll
			}
			if p.scrollY != want {
				t.Errorf("cursor %d from scrollY 0 scrolled to %d, want %d", cursor, p.scrollY, want)
			}
			if p.cursorLine < p.scrollY || p.cursorLine >= p.scrollY+14 {
				t.Errorf("the cursor at %d is outside the window [%d, %d)",
					p.cursorLine, p.scrollY, p.scrollY+14)
			}
		}
	})

	t.Run("a cursor inside the window leaves the scroll alone", func(t *testing.T) {
		// The counterweight, and the direction that is easy to lose: scrolled down with the
		// cursor near the top of the window, moving must NOT drag the view back up.
		p := linesPreview(t, 40)
		p.scrollY = 20
		p.cursorLine = 21
		p.ensureCursorVisible()
		if p.scrollY != 20 {
			t.Errorf("a cursor inside the window scrolled to %d, want it left at 20", p.scrollY)
		}

		// And exactly on the first visible line is INSIDE, not above: this is the boundary
		// between "scroll to the cursor" and "leave it alone", and getting it wrong makes
		// the view jump a line every time the cursor lands on the top row.
		p.scrollY = 20
		p.cursorLine = 20
		p.ensureCursorVisible()
		if p.scrollY != 20 {
			t.Errorf("a cursor on the first visible line scrolled to %d, want it left at 20", p.scrollY)
		}
	})

	t.Run("a cursor above the window scrolls up to it", func(t *testing.T) {
		p := linesPreview(t, 40)
		p.scrollY = 20
		p.cursorLine = 5
		p.ensureCursorVisible()
		if p.scrollY != 5 {
			t.Errorf("scrolled to %d, want 5 -- the cursor's own line", p.scrollY)
		}
	})
}

// The three forms contentHeight takes, told apart by how many lines come out.
//
// Render computes `contentHeight := p.height - 4`, then subtracts two more in jq mode, or one
// more when a jq expression is active -- and then ADDS a line back for the jq bar, which ends
// in a newline. The three arithmetic shapes therefore cancel out differently, and that is the
// point: a jq expression costs a content line and gives a bar line, so the view does not
// change height, while jq mode's two-row allowance is only partly given back by its one
// prompt line.
//
// A test that only checked "the view is not empty" would see nothing here. So the counts are
// asserted exactly, with the arithmetic written out beside each one.
func TestTheRenderedHeightDependsOnWhichJQLayerIsShowing(t *testing.T) {
	p := linesPreview(t, 40)

	// No jq at all: height-4 content lines and no bar.
	p.jqMode, p.jqExpr = false, ""
	if got, want := countRenderedLines(p.Render()), 20-4; got != want {
		t.Errorf("with no jq layer the view is %d lines, want %d (height-4, no bar)", got, want)
	}

	// A jq expression that is not currently running: one content line less, one bar line more.
	p.jqExpr = `.user`
	if got, want := countRenderedLines(p.Render()), (20-4-1)+1; got != want {
		t.Errorf("with a jq expression the view is %d lines, want %d (height-4-1 plus the bar)", got, want)
	}
	// Which is the same as with no expression at all. Asserted rather than assumed, because
	// "the bar replaced a content line" is the reason and not an accident -- and if it ever
	// stopped being true, the bar would push the content off the panel for no reason.
	noExpr := (20 - 4)
	if got := countRenderedLines(func() string { p.jqExpr = ""; return p.Render() }()); got != noExpr {
		t.Errorf("clearing the expression gave %d lines, want %d -- the bar must cost what it adds", got, noExpr)
	}
	p.jqExpr = `.user`

	// jq mode: two rows given up for the input, one of which the prompt line gives back.
	p.EnterJQMode()
	if got, want := countRenderedLines(p.Render()), (20-4-2)+1; got != want {
		t.Errorf("in jq mode the view is %d lines, want %d (height-4-2 plus the prompt)", got, want)
	}

	// And in jq mode an expression does NOT subtract a third time: the second arm is an
	// `else if`, and the only way to tell that from two independent `if`s is to be in the
	// state where they differ -- jq mode with an expression already set.
	p.jqInput = `.user`
	if got, want := countRenderedLines(p.Render()), (20-4-2)+1; got != want {
		t.Errorf("jq mode with an expression set is %d lines, want %d -- the expression bar must not "+
			"stack on top of the prompt", got, want)
	}
}

// Half a page, exactly.
//
// `p.scrollY -= p.height / 2` is the one place a division happens in the package, and the
// divisor is the only thing between a half-page move and a whole-page one -- which for a
// key bound to "half page" is the difference between the advertised behaviour and a lie.
func TestHalfAPageIsHalfOfTheHeight(t *testing.T) {
	for _, tc := range []struct {
		name     string
		height   int
		from     int
		wantDown int
		wantUp   int
	}{
		// wantDown and wantUp are written as from ± half rather than as constants, because
		// writing them as constants is what made the first version of this table pass by
		// accident: `10 + 10` for an odd height is the same as for an even one, so the row
		// looked like it was testing the division and was not.
		{"an even height", 20, 10, 10 + 20/2, 10 - 20/2},
		{"an odd height rounds down", 21, 10, 10 + 21/2, 10 - 21/2},
		{"a short panel", 9, 20, 20 + 4, 20 - 4},
		{"a one-line panel does not divide by zero", 2, 10, 11, 9},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := linesPreview(t, 40)
			p.SetHeight(tc.height)
			half := tc.height / 2

			p.scrollY = tc.from
			p.halfPageDown()
			if p.scrollY != tc.wantDown {
				t.Errorf("half_page_down from %d at height %d scrolled to %d, want %d (a half is %d)",
					tc.from, tc.height, p.scrollY, tc.wantDown, half)
			}

			p.scrollY = tc.from
			p.halfPageUp()
			if p.scrollY != tc.wantUp {
				t.Errorf("half_page_up from %d at height %d scrolled to %d, want %d (a half is %d)",
					tc.from, tc.height, p.scrollY, tc.wantUp, half)
			}
		})
	}
}

// The clamps at the ends, told apart by which value they leave behind.
//
// Four ends, four different values, and the cases below are the boundaries between them: a
// half page up from exactly one half page down lands on exactly zero; a half page down into a
// document with no slack left lands on exactly the last window; and going further than either
// end stops there rather than going negative or past the content.
func TestScrollingStopsAtBothEnds(t *testing.T) {
	p := linesPreview(t, 40) // 42 lines, 20 tall -> contentHeight 14, maxScroll 28

	t.Run("up from exactly one half page lands on zero, not below", func(t *testing.T) {
		p.scrollY = 10
		p.halfPageUp()
		if p.scrollY != 0 {
			t.Errorf("scrolled to %d, want 0", p.scrollY)
		}
	})

	t.Run("up from less than a half page lands on zero", func(t *testing.T) {
		p.scrollY = 3
		p.halfPageUp()
		if p.scrollY != 0 {
			t.Errorf("scrolled to %d, want 0 -- not a negative offset", p.scrollY)
		}
	})

	t.Run("up from zero stays at zero", func(t *testing.T) {
		p.scrollY = 0
		p.halfPageUp()
		if p.scrollY != 0 {
			t.Errorf("scrolled to %d, want 0", p.scrollY)
		}
	})

	t.Run("down past the end lands on the last window", func(t *testing.T) {
		// maxScroll is len(lines) - contentHeight = 42 - 14 = 28.
		p.scrollY = 26
		p.halfPageDown()
		if p.scrollY != 28 {
			t.Errorf("scrolled to %d, want 28 -- the last full window", p.scrollY)
		}
	})

	t.Run("down from exactly the end stays there", func(t *testing.T) {
		p.scrollY = 28
		p.halfPageDown()
		if p.scrollY != 28 {
			t.Errorf("scrolled to %d, want 28", p.scrollY)
		}
	})

	t.Run("a document shorter than the view cannot scroll at all", func(t *testing.T) {
		// The maxScroll < 0 arm: with fewer lines than the view there is no slack, and the
		// offset that arithmetic produces is negative. Asserting the RESULT is zero is the
		// only way to see it -- the arithmetic itself is the same either way.
		short := linesPreview(t, 3)
		short.scrollY = 0
		short.halfPageDown()
		if short.scrollY != 0 {
			t.Errorf("scrolled to %d in a five-line view, want 0", short.scrollY)
		}
		// And the first line is still the first line, which is what "cannot scroll" means to
		// somebody reading it.
		if key := keyOfLine(t, splitRendered(short.Render()), 0); key != -1 {
			t.Errorf("the first visible line is key%02d, want the opening brace", key)
		}
	})
}

// ensureCursorVisible: the panel is tall enough to show nothing.
//
// `contentHeight := p.height - 6; if contentHeight <= 0 { return }`. At exactly the boundary
// the panel has room for zero lines, and returning early is the difference between a cursor
// that is left alone and a scroll offset computed from a height of zero.
func TestAPanelTooShortToShowAnythingDoesNotScroll(t *testing.T) {
	for _, h := range []int{1, 2, 5, 6, 7} {
		t.Run(fmt.Sprint("height ", h), func(t *testing.T) {
			p := linesPreview(t, 40)
			p.SetHeight(h)
			p.cursorLine = 30
			p.scrollY = 5

			p.ensureCursorVisible()

			if h-6 <= 0 {
				if p.scrollY != 5 {
					t.Errorf("the scroll moved to %d with room for %d lines, want it left at 5",
						p.scrollY, h-6)
				}
				return
			}
			// One taller and the scroll DOES follow the cursor, which is the counterweight
			// for the case above: without it, a function that never scrolled would pass.
			if p.scrollY == 5 {
				t.Errorf("the scroll stayed at 5 with room for %d lines", h-6)
			}
			if p.cursorLine < p.scrollY || p.cursorLine >= p.scrollY+(h-6) {
				t.Errorf("the cursor at %d is outside the window [%d, %d)",
					p.cursorLine, p.scrollY, p.scrollY+(h-6))
			}
		})
	}
}

// ensureCursorVisible: the scroll that follows a cursor to the bottom of the view.
//
// `p.scrollY = p.cursorLine - contentHeight + 1`. Three constants and a subtraction, and the
// result is the only thing that shows whether it is right: the cursor must land on the LAST
// visible line, not one past it and not two before it.
func TestScrollingToTheCursorPutsItOnTheLastVisibleLine(t *testing.T) {
	const height = 20 // ensureCursorVisible's contentHeight is height-6 == 14
	p := linesPreview(t, 40)

	for _, cursor := range []int{0, 13, 14, 15, 26, 39} {
		t.Run(fmt.Sprint("cursor at ", cursor), func(t *testing.T) {
			p.scrollY = 0
			p.cursorLine = cursor
			p.ensureCursorVisible()

			want := cursor - (height - 6) + 1
			if want < 0 {
				want = 0
			}
			// The last line of the document is the floor: you cannot scroll past the end.
			if maxScroll := 40 - (height - 6); p.scrollY > maxScroll {
				want = maxScroll
			}
			if p.scrollY != want {
				t.Errorf("cursor %d scrolled to %d, want %d", cursor, p.scrollY, want)
			}
			// And the invariant the constant is there to hold: the cursor is inside.
			if p.cursorLine < p.scrollY || p.cursorLine >= p.scrollY+(height-6) {
				t.Errorf("the cursor at %d is outside the window [%d, %d)",
					p.cursorLine, p.scrollY, p.scrollY+(height-6))
			}
		})
	}
}

// ── Helpers ──────────────────────────────────────────────────────────────

// splitRendered is the rendered view as its lines, with the colours stripped.
func splitRendered(out string) []string {
	return strings.Split(ansi_Strip(out), "\n")
}

func countRenderedLines(out string) int { return len(splitRendered(out)) }

// highlightedIndices returns which RENDERED lines carry the cursor highlight.
//
// It is a DIFF, not a search for the highlight's escape codes: the view is rendered twice, once
// with the cursor on a line and once with it somewhere no line can be, and the lines that
// differ are the highlighted ones. That works whatever the theme's colours are and whatever
// lipgloss decides to emit for a background plus a width, which is the part a search for the
// escape prefix would get wrong.
//
// cursorLine = -1 is not a state production reaches -- the cursor is clamped -- and that is
// fine: this is a reference rendering, not a claim about what a user can do.
func highlightedIndices(t *testing.T, p *GridPreview, cursorLine int) []int {
	t.Helper()

	withCursor := func() string {
		saved := p.cursorLine
		p.cursorLine = cursorLine
		out := p.Render()
		p.cursorLine = saved
		return out
	}

	marked := splitRendered(withCursor())
	p.cursorLine = -1
	plain := splitRendered(p.Render())

	if len(marked) != len(plain) {
		t.Fatalf("the two renderings have %d and %d lines, so they cannot be compared",
			len(marked), len(plain))
	}

	var out []int
	for i := range marked {
		if marked[i] != plain[i] {
			out = append(out, i)
		}
	}
	return out
}

// keyIndex reads the keyNN out of a rendered line, or -1 when the line has none.
//
// The fixture numbers its keys, so the key in the output says which line of the document was
// rendered. Reading it out of the RENDER rather than out of p.lines is the point: a bug in the
// window's offset shows up as the wrong key even when the slice bounds are right.
func keyIndex(t *testing.T, line string) int {
	t.Helper()
	i := strings.Index(line, "\"key")
	if i < 0 {
		return -1
	}
	rest := line[i+4:]
	j := 0
	for j < len(rest) && rest[j] >= '0' && rest[j] <= '9' {
		j++
	}
	n := 0
	for _, c := range rest[:j] {
		n = n*10 + int(c-'0')
	}
	return n
}

// One panel, two content heights.
//
// Render draws height-4 == 16 lines. ensureCursorVisible scrolls as if height-6 == 14 were
// visible. Two names for the same quantity, two values, and neither mentions the other:
//
//	func (p *GridPreview) Render() string {
//	    contentHeight := p.height - 4          <- the renderer's answer
//	...
//	func (p *GridPreview) ensureCursorVisible() {
//	    contentHeight := p.height - 6          <- the scroller's answer
//
// The direction of the error is the safe one, which is presumably why it has survived: the
// scroller believes the panel is TALLER than it is, so it scrolls sooner than it needs to and
// leaves two blank lines at the bottom. A user scrolling down sees the content stop two rows
// early. Nothing breaks and nothing is unreachable, which is exactly the shape of a defect that
// reads as a style choice.
//
// It is pinned rather than fixed because changing either constant moves the scroll position for
// every panel, and which of the two is wrong depends on what the 4 rows are for -- a border and
// a status line, or a border and nothing else. Render does not draw a border, which is the
// evidence that the 6 is the stale one, but "does not draw a border" is not the same as "needs
// no rows", and that is a question about the panel's chrome rather than about this file.
//
// What is pinned is the direction and the size, so a change to either constant has to update
// this test and say why.
func TestTheScrollArithmeticUsesTwoDifferentHeights(t *testing.T) {
	const height = 20
	p := linesPreview(t, 40) // 42 lines

	rendered := countRenderedLines(p.Render())
	scrollsAs := height - 6

	if rendered != height-4 {
		t.Fatalf("precondition failed: the render is %d lines, want %d", rendered, height-4)
	}
	if rendered == scrollsAs {
		t.Skip("the two heights agree now, so this file's comment is stale rather than the code")
	}

	// The consequence, stated as behaviour rather than as arithmetic: a cursor on the second
	// to last visible row is INSIDE what the renderer shows and OUTSIDE what the scroller
	// thinks is there, so ensureCursorVisible scrolls for a cursor that is already on screen.
	p.scrollY = 0
	p.cursorLine = rendered - 2 // 14: row 14 of 16, so visible
	p.ensureCursorVisible()
	if p.scrollY == 0 {
		t.Errorf("a cursor on rendered row %d scrolled the view, so the scroller and the renderer "+
			"agree after all -- update this test", p.cursorLine)
	}
	if p.cursorLine < p.scrollY || p.cursorLine >= p.scrollY+scrollsAs {
		t.Errorf("the cursor at %d is outside the window the scroller chose [%d, %d)",
			p.cursorLine, p.scrollY, p.scrollY+scrollsAs)
	}
}
