package explorer

// The window arithmetic: where the scroll starts, how many rows fit, and how wide a row
// is once it has been cut to the pane.
//
// Every number below is a bound a reader can check by hand, and every one of them was the
// subject of a mutant that survived until this file existed. They survive on assertions
// that only say "roughly fits" — a row two cells too wide still fits, a window that moved
// one row for no reason still shows the cursor, a pane that drew nothing at all still has
// no row wider than the pane — so each case asserts the EXACT value and says where the
// number comes from.
//
// The width one is worth spelling out, because four of the mutants live in the same
// expression. A row is
//
//	indent + expandIcon + " " + icon + " " + name
//
// and the name is cut to `availableWidth - prefixLen`, where prefixLen counts exactly
// those five pieces. The pane is 14 wide, the border takes a cell per side, so the tree
// has 12 and the inside of the border has 8. A leaf at depth 1 is two cells of indent and
// two of expand marker, its icon is blank, and the two single spaces are the separators —
// 2+2+1+0+1 = 6, leaving 2 for the name. Get any one of those five additions wrong and
// the row is no longer 8 cells wide, which is what the assertion measures.

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestTheWindowStopsAtTheEdgeOfTheList(t *testing.T) {
	f := newFixture(t)
	tree := f.explorer.tree
	tree.SetHeight(3)
	rows := len(tree.filtered)
	if rows <= 3 {
		t.Fatalf("the fixture has %d rows, too few to scroll", rows)
	}

	// Past the end: the window ends exactly one pane before the last row.
	tree.offset = rows
	tree.clampOffset()
	if tree.offset != rows-3 {
		t.Errorf("an offset past the end was clamped to %d, want %d", tree.offset, rows-3)
	}

	// Before the beginning: back to the top, not to a negative scroll.
	tree.offset = -5
	tree.clampOffset()
	if tree.offset != 0 {
		t.Errorf("a negative offset was clamped to %d, want 0", tree.offset)
	}

	// And an offset already inside the list is left alone.
	tree.offset = 1
	tree.clampOffset()
	if tree.offset != 1 {
		t.Errorf("an offset of 1 inside a list of %d became %d, want it untouched", rows, tree.offset)
	}
}

// The window follows the cursor only when the cursor has actually left it. Comparing the
// cursor against `offset - height` instead of `offset + height` is the shape of the
// mistake: every cursor is below a negative bound, so the window would jump on every key.
func TestAVisibleCursorDoesNotMoveTheWindow(t *testing.T) {
	f := newFixture(t)
	tree := f.explorer.tree
	tree.SetHeight(3)
	if len(tree.filtered) <= 4 {
		t.Fatalf("the fixture has %d rows, too few to put the cursor at 3", len(tree.filtered))
	}

	tree.offset = 2
	tree.cursor = 3 // the second row of the window
	tree.scrollToCursor()
	if tree.offset != 2 {
		t.Errorf("a cursor already on screen moved the window from 2 to %d", tree.offset)
	}
}

// An off-screen cursor pulls the window to exactly `cursor - height + 1`: the cursor on
// the LAST row of the window and no further. The list is padded well past the cursor on
// purpose — with a short list the clamp at the end of the document would put the right
// answer back and hide a wrong one.
func TestAnOffScreenCursorPullsTheWindowJustFarEnough(t *testing.T) {
	f := newFixture(t)
	for i := range 40 {
		f.public.AddChild(NewNode(fmt.Sprintf("x%d", i), NodeTable, fmt.Sprintf("tbl%02d", i)))
	}
	f.explorer.SetNodes([]*Node{f.db})

	tree := f.explorer.tree
	tree.SetHeight(3)
	if len(tree.filtered) < 20 {
		t.Fatalf("the fixture has %d rows, too few to hide the clamp", len(tree.filtered))
	}

	tree.offset = 0
	tree.cursor = 10
	tree.scrollToCursor()
	if tree.offset != 8 { // 10 - 3 + 1
		t.Errorf("the cursor at 10 with a window of 3 put the top at %d, want 8", tree.offset)
	}
}

// The pane draws exactly as many rows as it has height for, and no more — the window is
// [offset, offset+height). A window computed with a subtraction ends before it starts and
// draws nothing at all, which no assertion about "no row wider than the pane" would notice.
func TestThePaneDrawsExactlyItsHeightInRows(t *testing.T) {
	f := newFixture(t)
	f.explorer.SetWidth(40)
	f.explorer.SetHeight(6) // the tree gets 6-2 = 4

	tree := f.explorer.tree
	if len(tree.filtered) <= tree.height {
		t.Fatalf("the fixture has %d rows in a window of %d, so nothing scrolls", len(tree.filtered), tree.height)
	}
	if got := nonBlankLines(tree.View()); got != tree.height {
		t.Errorf("the window rendered %d rows, want its height of %d", got, tree.height)
	}

	// The first row of the window is the one at the offset, so scrolling moves the rows
	// and not the count.
	tree.offset = 2
	if got, want := nonBlankLines(tree.View()), tree.height; got != want {
		t.Errorf("after a scroll the window rendered %d rows, want %d", got, want)
	}
}

// A name is cut so that the WHOLE row fits the inside of the border, which is the pane
// minus a cell per side minus 4 for the frame. The row is measured, not the name: the
// name is what has to give way, and a cut name costs exactly the cells it is cut to, so a
// row whose name was cut is always exactly as wide as the pane allows.
func TestACutNameLeavesTheRowExactlyAsWideAsThePane(t *testing.T) {
	f := newFixture(t)
	f.explorer.SetWidth(14)
	f.explorer.SetHeight(10)
	long := NewNode("long", NodeTable, "a_very_long_table_name_that_will_not_fit")
	f.db.AddChild(long)
	f.db.Expanded = true
	f.explorer.SetNodes([]*Node{f.db})

	// 14 of pane, 1 per side of border, 4 of frame: 8 for the row.
	const wantWidth = 8
	// The long table is the last row of the fixture, and the pane is tall enough to
	// draw all of them.
	rows := strings.Split(f.explorer.tree.View(), "\n")
	line := ansi.Strip(rows[len(f.explorer.tree.filtered)-1])
	if !strings.Contains(line, "…") {
		t.Fatalf("the long name was not cut at all: %q", line)
	}
	if got := lipgloss.Width(line); got != wantWidth {
		t.Errorf("the cut row is %d cells wide, want %d: %q", got, wantWidth, line)
	}
}

// The same measurement on a row whose icon is NOT blank, which is the only shape that can
// tell the two single spaces in the prefix apart. Every icon above a table is empty, so
// `1 - len(icon)` and `1 + len(icon)` are the same number there and a test over tables
// alone cannot see the difference; a column's icon is two cells and it can.
func TestACutColumnRowIsAlsoExactlyAsWideAsThePane(t *testing.T) {
	f := newFixture(t)
	f.explorer.SetWidth(30)
	f.explorer.SetHeight(14)
	col := NewNode("c2", NodeColumn, "a_very_long_column_name")
	col.Metadata["data_type"] = "text"
	f.items.AddChild(col)
	f.items.Expanded = true
	f.explorer.SetNodes([]*Node{f.db})

	// 30 of pane, 1 per side of border, 4 of frame: 24 for the row. The column sits at
	// depth 3 — six of indent, two of expand marker, two of icon and two separators —
	// so 12 of the 24 are prefix and 12 are name.
	const wantWidth = 24
	plain := lineWith(f.explorer.tree.View(), "a_very_long")
	if plain == "" {
		t.Fatal("the column row is not rendered")
	}
	if !strings.Contains(plain, "…") {
		t.Fatalf("the long column name was not cut at all: %q", plain)
	}
	if got := lipgloss.Width(plain); got != wantWidth {
		t.Errorf("the cut column row is %d cells wide, want %d: %q", got, wantWidth, plain)
	}
}

// Exactly one row is the selected one, and the pane can tell which: it is the only row
// drawn at the full width of the pane. Highlighting every row except the cursor's is a
// mutant no "the cursor is on a row that exists" assertion can see — every row still
// exists, and the styles are stripped before anything is compared.
func TestOnlyTheCursorRowIsDrawnAtFullWidth(t *testing.T) {
	f := newFixture(t)
	f.explorer.SetWidth(40)
	f.explorer.SetHeight(8)

	tree := f.explorer.tree
	tree.offset = 0
	tree.cursor = 2

	fullWidth := 0
	for _, l := range strings.Split(tree.View(), "\n") {
		if strings.TrimSpace(l) == "" {
			continue
		}
		if lipgloss.Width(l) == tree.width-4 {
			fullWidth++
		}
	}
	if fullWidth != 1 {
		t.Errorf("%d rows were drawn at the full width of the pane, want exactly 1 (the cursor's)", fullWidth)
	}
}
