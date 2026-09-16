package grid

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// gridCursorVisible reports whether cursorCol falls inside the window of
// columns that visibleColumns would render.
func gridCursorVisible(g *Grid) bool {
	vis, _ := g.visibleColumns()
	if len(vis) == 0 {
		return false
	}
	return g.cursorCol >= g.scrollCol && g.cursorCol <= g.scrollCol+len(vis)-1
}

// TestGrid_SyncScroll_RegressionCursorOffRightEdge reproduces the reported bug:
// with four 10-wide columns fitting exactly in the viewport (width 42 ->
// available 40), advancing the cursor past the fourth column used to leave the
// cursor one column beyond the right edge.
func TestGrid_SyncScroll_RegressionCursorOffRightEdge(t *testing.T) {
	g := newMouseTestGrid(42, []int{10, 10, 10, 10, 10, 10, 10, 10}, 1)

	for i := 0; i <= 5; i++ {
		g.cursorCol = i
		g.syncScroll()
		if !gridCursorVisible(g) {
			t.Fatalf("cursorCol=%d not visible: scrollCol=%d visible=%v",
				g.cursorCol, g.scrollCol, visibleCols(g))
		}
	}

	if g.scrollCol != 2 {
		t.Fatalf("scrollCol = %d, want 2", g.scrollCol)
	}
}

func TestGrid_MoveRight_KeepsCursorVisible(t *testing.T) {
	widths := []int{18, 7, 25, 9, 12, 6, 30, 11, 8, 14}
	g := newMouseTestGrid(52, widths, 3)

	for step := 0; step < 3*len(widths); step++ {
		g.moveRight()
		if !gridCursorVisible(g) {
			t.Fatalf("after moveRight #%d: cursorCol=%d scrollCol=%d visible=%v",
				step, g.cursorCol, g.scrollCol, visibleCols(g))
		}
	}

	for step := 0; step < 3*len(widths); step++ {
		g.moveLeft()
		if !gridCursorVisible(g) {
			t.Fatalf("after moveLeft #%d: cursorCol=%d scrollCol=%d visible=%v",
				step, g.cursorCol, g.scrollCol, visibleCols(g))
		}
	}
}

func TestGrid_SyncScroll_ExpandsLeftOnlyWhenCursorFits(t *testing.T) {
	// Wide first column: pulling it back into view must not push the cursor out.
	g := newMouseTestGrid(30, []int{22, 8, 8, 8}, 1)
	g.cursorCol = 2
	g.scrollCol = 1
	g.syncScroll()

	if !gridCursorVisible(g) {
		t.Fatalf("cursorCol=%d hidden: scrollCol=%d visible=%v",
			g.cursorCol, g.scrollCol, visibleCols(g))
	}
	if g.scrollCol != 1 {
		t.Fatalf("scrollCol = %d, want 1 (column 0 is too wide to re-add)", g.scrollCol)
	}
}

// TestGrid_Header_ScrolledShowsIconsAndSortOnRealColumn verifies the header is
// indexed by absolute column even when the grid is scrolled horizontally.
func TestGrid_Header_ScrolledShowsIconsAndSortOnRealColumn(t *testing.T) {
	g := newMouseTestGrid(40, []int{12, 12, 12, 12, 12}, 3)
	g.header.SetColumns(g.columns)
	g.header.SetWidths(g.widths)
	g.keyIcons = map[int]KeyIcon{1: KeyFK, 3: KeyPK}
	g.header.SetKeyIcons(g.keyIcons)
	g.header.ToggleSort(3) // absolute column 3

	out := ansi.Strip(g.header.Render(2, 3)) // visible columns 2, 3, 4

	if !strings.Contains(out, "* C3") {
		t.Fatalf("PK icon missing from real column 3: %q", out)
	}
	if !strings.Contains(out, "C3 ↑") {
		t.Fatalf("sort indicator missing from real column 3: %q", out)
	}
	if strings.Contains(out, "* C2") {
		t.Fatalf("PK icon leaked onto column 2: %q", out)
	}
}

// TestGrid_ToggleSort_ScrolledUsesAbsoluteColumn verifies the keyboard sort
// targets the absolute column under the cursor, not the shifted header slot.
func TestGrid_ToggleSort_ScrolledUsesAbsoluteColumn(t *testing.T) {
	g := newMouseTestGrid(40, []int{12, 12, 12, 12, 12}, 3)
	g.header.SetColumns(g.columns)
	g.scrollCol = 2
	g.cursorCol = 3

	cmd := g.toggleSort()
	if cmd == nil {
		t.Fatal("toggleSort returned nil command")
	}
	if got := g.SortColumn(); got != "c3" {
		t.Fatalf("SortColumn() = %q, want %q", got, "c3")
	}

	msg, ok := cmd().(GridSortApplyMsg)
	if !ok {
		t.Fatalf("toggleSort command produced %T, want GridSortApplyMsg", cmd())
	}
	if msg.OrderBy != "c3" {
		t.Fatalf("OrderBy = %q, want %q", msg.OrderBy, "c3")
	}
}

func visibleCols(g *Grid) []string {
	cols, _ := g.visibleColumns()
	return cols
}
