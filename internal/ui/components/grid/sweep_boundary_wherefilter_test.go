package grid

// Boundary proofs for the leftover CONDITIONALS_BOUNDARY targets of the
// grid-where_filter sweep (cell.go:130, header.go:101, header.go:138,
// mouse.go:28, mouse.go:61, mouse.go:66 cols 39 and 67, pager.go:46,
// pager.go:89, pager.go:93, pager.go:108, pager.go:131).
//
// Two kinds of proof live here:
//
// KILL tests (mouse.go:66 cols 39 and 67): the equality input makes the
// mutated loop walk one step past the end of a slice, and that step is
// observable — a phantom column that answers the click (col 39), or an
// index-out-of-range panic on widths (col 67).
//
// EQUIVALENCE tests: the boundary mutant differs from the original on
// exactly ONE input, and at that input the mutated branch is a no-op (a
// clamp assigning the value already held, a zero-iteration loop that still
// joins to "") or the input is unreachable (a guard above returns first).
// Each test builds the equality, guards the fixture so the equality really
// holds, then asserts the original's observable behaviour. The suite stays
// green under that one mutant: that green IS the equivalence proof, and it
// is why these belong in .mutation-allowlist.
//
// Verified by applying each mutant one at a time and re-running the package.

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// ---------------------------------------------------------------------------
// mouse.go:66 col 39 — `i < len(mh.grid.columns)` -> `i <= len(...)`.
// Boundary: i == len(columns) with len(widths) > len(columns). The original
// leaves the loop and answers -1; the mutant enters it, reads a width that
// has no column, and answers a visible-column index that does not exist.
// The click is answered with a sort command for a phantom column.
// ---------------------------------------------------------------------------

func TestWhereFilterBoundary_Mouse66_Col39_PhantomColumnPastColumns(t *testing.T) {
	g := newMouseTestGrid(40, []int{5}, 5)
	// One column, two widths: the second width belongs to no column.
	g.widths = []int{5, 5}
	if len(g.columns) != 1 || len(g.widths) != 2 {
		t.Fatalf("fixture: %d columns / %d widths, want 1 / 2", len(g.columns), len(g.widths))
	}

	// Header row, contentX=5: the original walks column 0 (5 <= 5 is not
	// < 5), then leaves the loop at i=1 == len(columns) -> -1 -> no sort.
	cmd, hit := g.HandleClick(6, 1)
	if cmd != nil {
		t.Errorf("a click past the last real column produced a sort command: %v", cmd)
	}
	if hit {
		t.Errorf("a click past the last real column reported a cell hit")
	}
}

// ---------------------------------------------------------------------------
// mouse.go:66 col 67 — `i < len(mh.grid.widths)` -> `i <= len(...)`.
// Boundary: i == len(widths) with len(columns) > len(widths). The original
// leaves the loop; the mutant enters it and indexes widths one past the end.
// ---------------------------------------------------------------------------

func TestWhereFilterBoundary_Mouse66_Col67_PastWidthsPanics(t *testing.T) {
	g := newMouseTestGrid(40, []int{5, 5}, 5)
	// Two columns, one width: column 1 exists, its width does not.
	g.widths = []int{5}
	if len(g.columns) != 2 || len(g.widths) != 1 {
		t.Fatalf("fixture: %d columns / %d widths, want 2 / 1", len(g.columns), len(g.widths))
	}

	// Header row, contentX=5: the original walks column 0, leaves the loop at
	// i=1 == len(widths) -> -1 -> no sort, no hit.
	cmd, hit := g.HandleClick(6, 1)
	if cmd != nil {
		t.Errorf("a click past the last width produced a sort command: %v", cmd)
	}
	if hit {
		t.Errorf("a click past the last width reported a cell hit")
	}
}

// ---------------------------------------------------------------------------
// EQUIVALENCE tests.
// ---------------------------------------------------------------------------

// cell.go:130 — `if cursorPos > len(runes)` -> `>=`.
// Boundary: cursorPos == len(runes) (cursor after the last rune). The clamp
// assigns the value already held; the cursor block lands after the text.
func TestWhereFilterBoundary_Cell130_CursorExactlyAtEnd(t *testing.T) {
	cr := newCellRendererForTest()
	if w := ansi.StringWidth(cr.RenderEditCell(nil, 10, "abc", 3)); w != 10 {
		t.Fatalf("fixture: the edit cell is %d cells, want the padded 10", w)
	}
	// cursorPos == len(runes) == 3: the `>` mutant's clamp is a no-op, and
	// the original puts the cursor block after "abc" either way.
	if got := ansi.Strip(cr.RenderEditCell(nil, 10, "abc", 3)); !strings.Contains(got, "abc█") {
		t.Errorf("RenderEditCell with cursorPos == len(runes) = %q, want the cursor after the text", got)
	}
	// Live neighbours: one under moves the block mid-text, one over is clamped.
	if got := ansi.Strip(cr.RenderEditCell(nil, 10, "abc", 1)); !strings.Contains(got, "a█bc") {
		t.Errorf("cursorPos 1 = %q, want the block after the first rune", got)
	}
	if got := ansi.Strip(cr.RenderEditCell(nil, 10, "abc", 9)); !strings.Contains(got, "abc█") {
		t.Errorf("cursorPos 9 = %q, want the clamped cursor after the text", got)
	}
}

// header.go:101 — `count <= 0` -> `<`.
// Boundary: count == 0. The original returns "" at once; the mutant falls
// through to a loop whose very first test (0 < 0) fails, and the join of no
// cells is the same "".
func TestWhereFilterBoundary_Header101_CountZero(t *testing.T) {
	h := newHeaderForTest()
	h.SetColumns([]string{"a", "b"})
	h.SetWidths([]int{10, 10})
	if got := h.Render(0, 0); got != "" {
		t.Errorf("Render(0, 0) = %q, want the empty header", got)
	}
	if got := h.Render(0, -1); got != "" {
		t.Errorf("Render(0, -1) = %q, want the empty header", got)
	}
	// Neighbour: one column over is rendered normally under both.
	if got := h.Render(0, 1); got == "" {
		t.Errorf("Render(0, 1) rendered nothing")
	}
}

// header.go:138 — `if maxTextWidth < 0` -> `<= 0`.
// Boundary: maxTextWidth == 0 (a 2-cell column: width-2 == 0). The clamp
// assigns 0 to a variable that already holds 0; the label is then cut to
// the empty string by the `<= 3` branch on both sides.
func TestWhereFilterBoundary_Header138_WidthTwoMakesZeroTextWidth(t *testing.T) {
	h := newHeaderForTest()
	h.SetColumns([]string{"ab"})
	h.SetWidths([]int{2}) // maxTextWidth = 0 exactly
	if got := ansi.Strip(h.Render(0, 1)); strings.Contains(got, "AB") {
		t.Errorf("a 2-cell header kept its label: %q", got)
	}
	// Neighbour: a 3-cell column gives maxTextWidth 1, and the same cut applies.
	h2 := newHeaderForTest()
	h2.SetColumns([]string{"ab"})
	h2.SetWidths([]int{3})
	if got := ansi.Strip(h2.Render(0, 1)); strings.Contains(got, "AB") {
		t.Errorf("a 3-cell header kept its 2-cell label: %q", got)
	}
}

// mouse.go:28 — `if y < headerY` -> `<=`.
// Boundary: y == headerY. Unreachable at this line: the `y == headerY` guard
// above returns on every path, so the `<` never evaluates equality — both
// operators answer identically on the only values that arrive (y < headerY
// and y > headerY). The fixture clicks exactly on the header line.
func TestWhereFilterBoundary_Mouse28_ClickExactlyOnHeaderLine(t *testing.T) {
	g := newMouseTestGrid(40, []int{10, 12, 14}, 5)
	headerY := g.mouse.headerRowY()
	if headerY != 1 {
		t.Fatalf("fixture: headerRowY() = %d, want 1", headerY)
	}
	// On the header line but on the left border: the header branch answers
	// (nil, false) — the same answer the `<` would give below the line.
	cmd, hit := g.HandleClick(0, headerY)
	if cmd != nil || hit {
		t.Errorf("HandleClick(0, %d) = (%v, %v), want (nil, false)", headerY, cmd, hit)
	}
	// A row above the header: the `<` at line 28 fires under both operators.
	cmd, hit = g.HandleClick(1, headerY-1)
	if cmd != nil || hit {
		t.Errorf("HandleClick(1, %d) = (%v, %v), want (nil, false)", headerY-1, cmd, hit)
	}
}

// mouse.go:61 — `if available <= 0` -> `<`.
// Boundary: available == 0 (a 2-cell grid). The original returns -1 at once;
// the mutant enters the column loop, which cannot answer a column there:
// returning vj needs contentX < acc+w with acc+w <= available == 0, but
// contentX >= 0 (the border guard), and acc+w > 0 breaks the loop. Both
// paths end at -1.
func TestWhereFilterBoundary_Mouse61_AvailableZero(t *testing.T) {
	// Case 1: the first column is wider than the (zero) available space —
	// the mutant breaks out of the loop immediately.
	g := newMouseTestGrid(2, []int{5}, 5)
	if avail := g.width - 2; avail != 0 {
		t.Fatalf("fixture: available = %d, want exactly 0", avail)
	}
	cmd, hit := g.HandleClick(1, 1) // header row, contentX=0
	if cmd != nil || hit {
		t.Errorf("HandleClick(1, 1) with available=0 = (%v, %v), want (nil, false)", cmd, hit)
	}

	// Case 2: a zero-width column does not break the loop, yet still cannot
	// be returned — contentX < 0 is impossible past the border guard.
	g2 := newMouseTestGrid(2, []int{0, 0}, 5)
	cmd, hit = g2.HandleClick(1, 1)
	if cmd != nil || hit {
		t.Errorf("HandleClick(1, 1) with zero-width columns = (%v, %v), want (nil, false)", cmd, hit)
	}
}

// pager.go:46 — `if pages < 1` -> `<= 1`.
// Boundary: pages == 1 (total == pageSize, no remainder). The clamp assigns
// 1 to a variable that already holds 1.
func TestWhereFilterBoundary_Pager46_ExactlyOnePage(t *testing.T) {
	p := newPagerForTest(10)
	p.SetTotalRows(10) // 10/10 = 1, remainder 0: pages == 1 exactly
	if got := p.TotalPages(); got != 1 {
		t.Errorf("TotalPages() with total == pageSize = %d, want 1", got)
	}
	// Neighbour: one row fewer still rounds up to the same single page.
	p2 := newPagerForTest(10)
	p2.SetTotalRows(9)
	if got := p2.TotalPages(); got != 1 {
		t.Errorf("TotalPages() with total = pageSize-1 = %d, want 1", got)
	}
	// Neighbour: one row over spills to a second page.
	p3 := newPagerForTest(10)
	p3.SetTotalRows(11)
	if got := p3.TotalPages(); got != 2 {
		t.Errorf("TotalPages() with total = pageSize+1 = %d, want 2", got)
	}
}

// pager.go:89 — `if n < 1` -> `<= 1`.
// Boundary: n == 1 in GoToPage. The clamp assigns 1 to n, which already
// holds 1.
func TestWhereFilterBoundary_Pager89_GoToExactlyPageOne(t *testing.T) {
	p := newPagerForTest(10)
	p.SetTotalRows(100)
	p.GoToPage(5)
	p.GoToPage(1) // n == 1 exactly
	if got := p.Page(); got != 1 {
		t.Errorf("GoToPage(1) left the pager on page %d, want 1", got)
	}
	// Neighbour: 0 still clamps up to 1 under both operators.
	p.GoToPage(0)
	if got := p.Page(); got != 1 {
		t.Errorf("GoToPage(0) left the pager on page %d, want 1", got)
	}
}

// pager.go:93 — `if n > total` -> `>= total`.
// Boundary: n == total in GoToPage. The clamp assigns total to n, which
// already holds total.
func TestWhereFilterBoundary_Pager93_GoToExactlyLastPage(t *testing.T) {
	p := newPagerForTest(10)
	p.SetTotalRows(25) // 3 pages
	p.GoToPage(3)      // n == total exactly
	if got := p.Page(); got != 3 {
		t.Errorf("GoToPage(3) with 3 total pages left the pager on page %d, want 3", got)
	}
	// Neighbour: 4 clamps down to 3 under both operators.
	p.GoToPage(4)
	if got := p.Page(); got != 3 {
		t.Errorf("GoToPage(4) with 3 total pages left the pager on page %d, want 3", got)
	}
}

// pager.go:108 — `if endRow > total` -> `>= total` (Render).
// Boundary: endRow == total (the last page ends exactly on the last row).
// The clamp assigns total to endRow, which already holds total.
func TestWhereFilterBoundary_Pager108_LastPageEndsExactlyOnTotal(t *testing.T) {
	p := newPagerForTest(5)
	p.SetTotalRows(10)
	p.GoToPage(2) // endRow = 10 == total exactly
	got := ansi.Strip(p.Render())
	if !strings.Contains(got, "6-10 of 10 · Page 2 of 2") {
		t.Errorf("Render() with endRow == total = %q, want %q", got, "6-10 of 10 · Page 2 of 2")
	}
	// Neighbour: a partial last page is clamped (or not) to the same total.
	p2 := newPagerForTest(5)
	p2.SetTotalRows(12)
	p2.GoToPage(3) // endRow = 15 > 12 -> clamped to 12 under both
	got2 := ansi.Strip(p2.Render())
	if !strings.Contains(got2, "11-12 of 12 · Page 3 of 3") {
		t.Errorf("Render() on a partial last page = %q, want %q", got2, "11-12 of 12 · Page 3 of 3")
	}
}

// pager.go:131 — `if endRow > total` -> `>= total` (RenderFooter).
// Boundary: endRow == total, same no-op clamp as Render, observed through the
// footer's right-aligned text.
func TestWhereFilterBoundary_Pager131_FooterLastPageEndsExactlyOnTotal(t *testing.T) {
	p := newPagerForTest(5)
	p.SetTotalRows(10)
	p.GoToPage(2) // endRow = 10 == total exactly
	got := ansi.Strip(p.RenderFooter())
	if !strings.Contains(got, "6-10 of 10") {
		t.Errorf("RenderFooter() with endRow == total = %q, want it to contain %q", got, "6-10 of 10")
	}
	if !strings.Contains(got, "Page 2/2") {
		t.Errorf("RenderFooter() with endRow == total = %q, want it to contain %q", got, "Page 2/2")
	}
	// Neighbour: a partial last page clamps to the same total under both.
	p2 := newPagerForTest(5)
	p2.SetTotalRows(12)
	p2.GoToPage(3)
	got2 := ansi.Strip(p2.RenderFooter())
	if !strings.Contains(got2, "11-12 of 12") {
		t.Errorf("RenderFooter() on a partial last page = %q, want it to contain %q", got2, "11-12 of 12")
	}
}
