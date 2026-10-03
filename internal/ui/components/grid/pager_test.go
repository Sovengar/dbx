package grid

import (
	"strings"
	"testing"

	"github.com/buble/dbx/internal/theme"
	"github.com/charmbracelet/x/ansi"
)

func newPagerForTest(pageSize int) *Pager {
	return NewPager(theme.Resolve("dark").Styles(), pageSize)
}

// Scenario: Un pager nuevo está en la primera página y la última es la única.
//
// Pages are 1-based, so page 1 is the initial state and a table with no rows
// still has one page. A 0-based pager would start at the bottom of an empty
// result, which is the classic way an "empty grid" bug starts.
func TestPager_StartsOnPageOne(t *testing.T) {
	p := newPagerForTest(10)

	if got := p.Page(); got != 1 {
		t.Errorf("Page() = %d, want 1", got)
	}
	if got := p.TotalPages(); got != 1 {
		t.Errorf("TotalPages() = %d, want 1 with no rows", got)
	}
	if got := p.Offset(); got != 0 {
		t.Errorf("Offset() = %d, want 0 on the first page", got)
	}
	if got := p.Limit(); got != 10 {
		t.Errorf("Limit() = %d, want the page size 10", got)
	}
}

// Scenario: El número de páginas redondea hacia arriba y nunca baja de una.
//
// The page count is what "Page 2 of 3" is drawn from, and the row range on the
// last page depends on it agreeing with the range. Rounding down would hide the
// tail of the table; rounding to 0 would produce "Page 1 of 0".
func TestPager_TotalPagesRoundsUpAndIsNeverBelowOne(t *testing.T) {
	for _, tc := range []struct {
		rows, pageSize, want int
		why                  string
	}{
		{0, 10, 1, "no rows still has one page"},
		{1, 10, 1, "one row"},
		{9, 10, 1, "one short of a full page"},
		{10, 10, 1, "exactly one page: the remainder is 0, so no extra page"},
		{11, 10, 2, "one over: the remainder is 1, so a second page exists"},
		{20, 10, 2, "exactly two pages"},
		{21, 10, 3, "one over two pages"},
		{100, 10, 10, "ten full pages"},
		{101, 10, 11, "one over ten full pages"},
		{1, 1, 1, "page size 1"},
		{7, 1, 7, "page size 1, seven rows"},
		{5, 100, 1, "everything fits in one page"},
	} {
		t.Run(tc.why, func(t *testing.T) {
			p := newPagerForTest(tc.pageSize)
			p.SetTotalRows(tc.rows)
			if got := p.TotalPages(); got != tc.want {
				t.Errorf("TotalPages() with %d rows at page size %d = %d, want %d", tc.rows, tc.pageSize, got, tc.want)
			}
		})
	}
}

// Scenario: Un tamaño de página no positivo se comporta como una sola página.
//
// pageSize comes from the caller's layout, and a collapsed grid can compute a
// size of 0. Dividing by it would panic, and negating the offset would send the
// query to a nonsense row range, so both are guarded.
func TestPager_NonPositivePageSizeDegradesToASinglePage(t *testing.T) {
	for _, pageSize := range []int{0, -1, -100} {
		p := newPagerForTest(pageSize)
		p.SetTotalRows(100)

		if got := p.TotalPages(); got != 1 {
			t.Errorf("page size %d: TotalPages() = %d, want 1", pageSize, got)
		}
		if got := p.Page(); got != 1 {
			t.Errorf("page size %d: Page() = %d, want 1", pageSize, got)
		}
		// Offset must not be negative: it is added to a row index before the
		// query is built, and a negative offset would read a row before the
		// first one.
		if got := p.Offset(); got < 0 {
			t.Errorf("page size %d: Offset() = %d, want a non-negative offset", pageSize, got)
		}
		// There is only one page, so NextPage must report that it did nothing
		// rather than walking off the end.
		if p.NextPage() {
			t.Errorf("page size %d: NextPage() reported movement with only one page", pageSize)
		}
		if got := p.Page(); got != 1 {
			t.Errorf("page size %d: after NextPage the page is %d, want it clamped at 1", pageSize, got)
		}
		p.GoToPage(9)
		if got := p.Page(); got != 1 {
			t.Errorf("page size %d: GoToPage(9) left the page at %d, want it clamped to 1", pageSize, got)
		}
		p.LastPage()
		if got := p.Page(); got != 1 {
			t.Errorf("page size %d: LastPage() left the page at %d, want 1", pageSize, got)
		}
	}
}

// Scenario: Un pager vacío vuelve a la primera página cuando los datos desaparecen.
//
// The table is reloaded, not appended to, and a query that matches nothing
// leaves the pager on whatever page the previous result was showing. Page 7 of
// an empty result is not a state anything can render, so the reset is what keeps
// the footer honest.
func TestPager_EmptyingTheResultResetsToTheFirstPage(t *testing.T) {
	p := newPagerForTest(10)
	p.SetTotalRows(100)
	p.LastPage()
	if got := p.Page(); got != 10 {
		t.Fatalf("the fixture is wrong: the page is %d, want 10", got)
	}

	p.SetTotalRows(0)

	if got := p.Page(); got != 1 {
		t.Errorf("Page() after the result emptied = %d, want 1", got)
	}
	if got := p.TotalPages(); got != 1 {
		t.Errorf("TotalPages() with no rows = %d, want 1", got)
	}
	if got := p.Offset(); got != 0 {
		t.Errorf("Offset() after the result emptied = %d, want 0", got)
	}
	if got := ansi.Strip(p.Render()); !strings.Contains(got, "No data") {
		t.Errorf("the footer says %q, want it to say there is no data", got)
	}
	if got := ansi.Strip(p.RenderFooter()); got != "" {
		t.Errorf("RenderFooter() with no rows = %q, want empty: a footer with no range on it is noise", got)
	}
}

// Scenario: Avanzar y retroceder se detienen en los extremos y lo dicen.
//
// NextPage and PrevPage return whether they moved, so the caller can tell a
// keypress that did nothing from one that did. Walking off the end would put the
// grid's cursor on a page that does not exist.
func TestPager_PageMovementStopsAtBothEndsAndReportsIt(t *testing.T) {
	p := newPagerForTest(10)
	p.SetTotalRows(25) // three pages

	// Forward from the first page.
	for want := 2; want <= 3; want++ {
		if !p.NextPage() {
			t.Fatalf("NextPage() to page %d reported no movement", want)
		}
		if got := p.Page(); got != want {
			t.Fatalf("after NextPage the page is %d, want %d", got, want)
		}
	}
	// Already on the last page: the call must report that it did nothing.
	if p.NextPage() {
		t.Error("NextPage() on the last page reported movement")
	}
	if got := p.Page(); got != 3 {
		t.Errorf("NextPage() on the last page moved to %d, want it to stay at 3", got)
	}
	// And again, to prove the state is stable rather than accidentally right.
	if p.NextPage() {
		t.Error("a second NextPage() on the last page reported movement")
	}
	if got := p.Page(); got != 3 {
		t.Errorf("the page is %d after two extra NextPage calls, want 3", got)
	}

	// Backward to the first.
	for want := 2; want >= 1; want-- {
		if !p.PrevPage() {
			t.Fatalf("PrevPage() to page %d reported no movement", want)
		}
		if got := p.Page(); got != want {
			t.Fatalf("after PrevPage the page is %d, want %d", got, want)
		}
	}
	if p.PrevPage() {
		t.Error("PrevPage() on the first page reported movement")
	}
	if got := p.Page(); got != 1 {
		t.Errorf("PrevPage() on the first page moved to %d, want it to stay at 1", got)
	}
	if p.PrevPage() {
		t.Error("a second PrevPage() on the first page reported movement")
	}
	if got := p.Page(); got != 1 {
		t.Errorf("the page is %d after two extra PrevPage calls, want 1", got)
	}
}

// Scenario: El desplazamiento es el número de filas de las páginas anteriores.
//
// Offset is the first thing added to a row index when the next-page query is
// built, so being off by one here means the grid shows row 11 while highlighting
// row 10, and the user edits the wrong record.
func TestPager_OffsetCountsWholePages(t *testing.T) {
	for _, tc := range []struct {
		pageSize int
		rows     int
		page     int
		want     int
	}{
		{10, 100, 1, 0},
		{10, 100, 2, 10},
		{10, 100, 5, 40},
		{10, 100, 10, 90},
		{1, 10, 5, 4},
		{25, 100, 3, 50},
		{7, 77, 11, 70},
		{7, 70, 10, 63}, // the last page of ten, with a partial page of 0 left over
		{7, 70, 99, 63}, // asking past the end lands on the last page, not past it
	} {
		p := newPagerForTest(tc.pageSize)
		p.SetTotalRows(tc.rows)
		p.GoToPage(tc.page)
		if got := p.Offset(); got != tc.want {
			t.Errorf("page size %d, page %d of %d: Offset() = %d, want %d", tc.pageSize, tc.page, p.TotalPages(), got, tc.want)
		}
	}
}

// Scenario: Ir a una página la recorta al rango que existe.
//
// The grid binds F1 through F9 to GoToPage(1..9) unconditionally, so this clamp
// runs on every one of those keys whether or not the table has that many pages.
// Without it, pressing F5 on a two-page table would ask the driver for a page
// that does not exist and render nothing.
func TestPager_GoToPageClampsToThePagesThatExist(t *testing.T) {
	p := newPagerForTest(10)
	p.SetTotalRows(25) // three pages

	for _, tc := range []struct {
		name string
		ask  int
		want int
	}{
		{"the first page", 1, 1},
		{"a middle page", 2, 2},
		{"the last page", 3, 3},
		{"past the last page", 4, 3},
		{"far past the last page", 99, 3},
		{"zero", 0, 1},
		{"negative", -5, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p.GoToPage(tc.ask)
			if got := p.Page(); got != tc.want {
				t.Errorf("GoToPage(%d) left the page at %d, want %d", tc.ask, got, tc.want)
			}
			// The offset has to agree with the page, or the query skips rows.
			if got, want := p.Offset(), (tc.want-1)*10; got != want {
				t.Errorf("GoToPage(%d): Offset() = %d, want %d", tc.ask, got, want)
			}
		})
	}

	// Every one of the F1..F9 keys, on a table with only two pages.
	two := newPagerForTest(10)
	two.SetTotalRows(15)
	for key := 1; key <= 9; key++ {
		two.GoToPage(key)
		want := key
		if want > 2 {
			want = 2
		}
		if got := two.Page(); got != want {
			t.Errorf("pressing F%d on a two-page table left the page at %d, want %d", key, got, want)
		}
	}
}

// Scenario: FirstPage y LastPage van a los extremos sin importarle el estado.
//
// These are the keys a user reaches for when a query returns thousands of rows,
// so they must be absolute rather than relative.
func TestPager_FirstAndLastPageAreAbsolute(t *testing.T) {
	p := newPagerForTest(10)
	p.SetTotalRows(95) // ten pages

	for _, tc := range []struct {
		name string
		from int
		to   func()
		want int
	}{
		{"last from the first page", 1, p.LastPage, 10},
		{"first from the last page", 10, p.FirstPage, 1},
		{"first from the first page", 1, p.FirstPage, 1},
		{"last from the last page", 10, p.LastPage, 10},
		{"last from the middle", 5, p.LastPage, 10},
		{"first from the middle", 5, p.FirstPage, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p.GoToPage(tc.from)
			tc.to()
			if got := p.Page(); got != tc.want {
				t.Errorf("from page %d the pager is at %d, want %d", tc.from, got, tc.want)
			}
		})
	}
}

// Scenario: Las filas pendientes cuentan como filas para la paginación.
//
// An uncommitted insert has to be paginated like a real row, or the footer
// claims fewer rows than the grid shows and the last page is unreachable.
func TestPager_PendingRowsCountTowardsTheTotal(t *testing.T) {
	// 9 real rows at a page size of 10 is one page; 2 pending makes 11, which is
	// two.
	p := newPagerForTest(10)
	p.SetTotalRows(9)
	if got := p.TotalPages(); got != 1 {
		t.Fatalf("the fixture is wrong: 9 rows is %d page(s), want 1", got)
	}
	p.SetPendingCount(2)
	if got := p.TotalPages(); got != 2 {
		t.Errorf("TotalPages() with 9 rows and 2 pending = %d, want 2", got)
	}
	if got := ansi.Strip(p.Render()); !strings.Contains(got, "of 11") {
		t.Errorf("the footer says %q, want it to count the 2 pending rows in the total", got)
	}
	if got := ansi.Strip(p.Render()); !strings.Contains(got, "2 pending") {
		t.Errorf("the footer says %q, want it to mention the pending count", got)
	}

	// And the count is removed again when the change is committed or discarded.
	p.SetPendingCount(0)
	if got := p.TotalPages(); got != 1 {
		t.Errorf("TotalPages() after the pending count went back to 0 = %d, want 1", got)
	}
	if got := ansi.Strip(p.Render()); strings.Contains(got, "pending") {
		t.Errorf("the long footer still says %q, want no mention of pending rows", got)
	}
	// Both renderers, not just the one the assertion above happened to check: a
	// "0 pending" segment would tell the user there is an uncommitted change when
	// there is none, and the two footers sit next to each other so only one of
	// them showing it would be worse than both.
	if got := ansi.Strip(p.RenderFooter()); strings.Contains(got, "pending") {
		t.Errorf("the compact footer says %q, want no mention of pending rows", got)
	}
	if got := ansi.Strip(p.RenderFooter()); strings.Contains(got, "0 pending") {
		t.Errorf("the compact footer says %q, want no zero pending count", got)
	}
	if got := ansi.Strip(p.Render()); strings.Contains(got, "0 pending") {
		t.Errorf("the long footer says %q, want no zero pending count", got)
	}
}

// Scenario: El pie describe el rango de filas de ESTA página.
//
// The range is 1-based and inclusive, and the last page stops at the total rather
// than running to a full page. "21-30 of 25" would be the bug: it tells the user
// five rows that do not exist are on screen.
func TestPager_RenderDescribesThisPagesRowRange(t *testing.T) {
	for _, tc := range []struct {
		rows, pageSize, page int
		want                 string
	}{
		{25, 10, 1, "1-10 of 25"},
		{25, 10, 2, "11-20 of 25"},
		{25, 10, 3, "21-25 of 25"}, // last page stops at the total
		{20, 10, 2, "11-20 of 20"},
		{5, 10, 1, "1-5 of 5"}, // a short single page
		{1, 10, 1, "1-1 of 1"},
		{95, 10, 10, "91-95 of 95"},
	} {
		p := newPagerForTest(tc.pageSize)
		p.SetTotalRows(tc.rows)
		p.GoToPage(tc.page)
		got := ansi.Strip(p.Render())
		if !strings.Contains(got, tc.want) {
			t.Errorf("%d rows at page size %d, page %d: the footer says %q, want it to contain %q", tc.rows, tc.pageSize, tc.page, got, tc.want)
		}
		// The page counter has to agree with the range.
		wantPage := strings.Replace(tc.want, " of ", " of ", 1)
		_ = wantPage
		if !strings.Contains(got, "Page "+itoa(tc.page)+" of "+itoa(p.TotalPages())) {
			t.Errorf("the footer says %q, want it to contain %q", got, "Page "+itoa(tc.page)+" of "+itoa(p.TotalPages()))
		}
	}
}

// Scenario: El pie de la caja lleva el rango y separadores propios.
//
// RenderFooter is the border-footer variant: the same numbers in a compact form,
// with a page counter written "3/3" rather than "of 3", and padded with a space
// on each side so it sits inside the border. The two renderers exist side by side,
// so both shapes are pinned: a change to one is not automatically a change to the
// other, and the tests are what keeps them from drifting into the same string.
func TestPager_RenderFooterIsTheCompactVariant(t *testing.T) {
	for _, tc := range []struct {
		rows, pageSize, page int
		wantRange            string
		wantPage             string
	}{
		{25, 10, 1, "1-10 of 25", "Page 1/3"},
		{25, 10, 2, "11-20 of 25", "Page 2/3"},
		{25, 10, 3, "21-25 of 25", "Page 3/3"},
		{5, 10, 1, "1-5 of 5", "Page 1/1"},
		{95, 10, 10, "91-95 of 95", "Page 10/10"},
	} {
		p := newPagerForTest(tc.pageSize)
		p.SetTotalRows(tc.rows)
		p.GoToPage(tc.page)

		got := ansi.Strip(p.RenderFooter())
		if !strings.Contains(got, tc.wantRange) {
			t.Errorf("RenderFooter() with %d rows at size %d, page %d = %q, want it to contain %q", tc.rows, tc.pageSize, tc.page, got, tc.wantRange)
		}
		if !strings.Contains(got, tc.wantPage) {
			t.Errorf("RenderFooter() = %q, want it to contain %q, written with a slash", got, tc.wantPage)
		}
		// The long form says "Page 3 of 3" and the compact one says "Page 3/3";
		// they must not be the same string.
		if strings.Contains(ansi.Strip(p.Render()), tc.wantPage) {
			t.Errorf("Render() picked up the compact %q: the two renderers have drifted together", tc.wantPage)
		}
	}
}

// Scenario: El pie de la caja y el pie largo cuentan lo mismo.
//
// They read the same three numbers, and they have to agree: the row range is what
// a user copies out of one to paste into the other to check it. Any difference is
// a bug in one of them.
func TestPager_BothFootersAgreeOnTheNumbers(t *testing.T) {
	for _, pending := range []int{0, 3} {
		for page := 1; page <= 3; page++ {
			p := newPagerForTest(10)
			p.SetTotalRows(25)
			p.SetPendingCount(pending)
			p.GoToPage(page)

			long := ansi.Strip(p.Render())
			compact := ansi.Strip(p.RenderFooter())
			for _, want := range []string{
				itoa(25 + pending),   // the total
				itoa(p.TotalPages()), // the number of pages
			} {
				if !strings.Contains(long, want) {
					t.Errorf("pending %d, page %d: the long footer %q is missing %q", pending, page, long, want)
				}
				if !strings.Contains(compact, want) {
					t.Errorf("pending %d, page %d: the compact footer %q is missing %q", pending, page, compact, want)
				}
			}
			if pending > 0 {
				for _, footer := range []string{long, compact} {
					if !strings.Contains(footer, itoa(pending)+" pending") {
						t.Errorf("pending %d, page %d: the footer %q does not mention the pending count", pending, page, footer)
					}
				}
			}
		}
	}
}

// Scenario: Un pager sin filas lo dice y nada más.
//
// Both renderers have a no-data state, and neither may print a range. A footer
// reading "0-0 of 0" is a lie the user has to interpret.
func TestPager_NoRowsSaysSoAndPrintsNoRange(t *testing.T) {
	for _, pending := range []int{0, 2} {
		p := newPagerForTest(10)
		p.SetPendingCount(pending)

		// pending rows alone DO make a range, so only the zero case is "no data".
		got := ansi.Strip(p.Render())
		if pending == 0 {
			if !strings.Contains(got, "No data") {
				t.Errorf("with no rows the long footer is %q, want it to say there is no data", got)
			}
			for _, unwanted := range []string{"0-0", "Page 1", "of 0"} {
				if strings.Contains(got, unwanted) {
					t.Errorf("the long footer %q contains %q, want no range at all", got, unwanted)
				}
			}
		} else {
			// 2 pending rows at a page size of 10: rows 1 to 2 of 2.
			if !strings.Contains(got, "1-2 of 2") {
				t.Errorf("with only pending rows the long footer is %q, want %q", got, "1-2 of 2")
			}
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}
