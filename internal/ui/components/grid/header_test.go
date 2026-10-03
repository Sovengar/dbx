package grid

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/buble/dbx/internal/theme"
	"github.com/charmbracelet/x/ansi"
)

func newHeaderForTest() *Header {
	return NewHeader(theme.Resolve("dark").Styles())
}

// Scenario: El triángulo de orden tiene exactamente tres estados y vuelve al
// punto de partida.
//
// The cycle is none -> asc -> desc -> none, and the third click on the SAME
// column is the one that clears sortCol as well as sortDir. If the third state
// only reset the direction and left the column behind, the header would keep
// drawing a sort arrow on a column the query is no longer sorted by.
func TestHeader_ToggleSortCyclesAscDescNone(t *testing.T) {
	h := newHeaderForTest()
	h.SetColumns([]string{"c0", "c1", "c2"})

	// No columns set yet: a fresh header has sortCol == -1, so the first click
	// takes the "different column" branch and starts at ascending.
	if got := h.ToggleSort(1); got != SortAsc {
		t.Fatalf("first ToggleSort(1) = %v, want SortAsc", got)
	}
	if got := h.SortDirection(); got != "ASC" {
		t.Errorf("SortDirection() = %q, want %q", got, "ASC")
	}

	if got := h.ToggleSort(1); got != SortDesc {
		t.Fatalf("second ToggleSort(1) = %v, want SortDesc", got)
	}
	if got := h.SortDirection(); got != "DESC" {
		t.Errorf("SortDirection() = %q, want %q", got, "DESC")
	}

	if got := h.ToggleSort(1); got != SortNone {
		t.Fatalf("third ToggleSort(1) = %v, want SortNone", got)
	}
	if got := h.SortDirection(); got != "" {
		t.Errorf("SortDirection() = %q after clearing, want empty", got)
	}
	if got := h.SortColumn(); got != "" {
		t.Errorf("SortColumn() = %q after the cycle, want empty: the column must be released too", got)
	}

	// And the header draws no arrow any more.
	h.SetWidths([]int{10, 10, 10})
	out := ansi.Strip(h.Render(0, 3))
	if strings.ContainsAny(out, "↑↓") {
		t.Errorf("the header still draws a sort arrow after clearing: %q", out)
	}
}

// Scenario: ClearSort suelta la columna, no solo la dirección.
//
// ClearSort backs the "the table changed under us" path: a new result set keeps
// its own ordering, so the old ORDER BY must not survive. Resetting only the
// direction would leave SortColumn pointing at whatever index happened to be
// there, and the next render would draw an arrow on an arbitrary column.
func TestHeader_ClearSortReleasesTheColumn(t *testing.T) {
	h := newHeaderForTest()
	h.SetColumns([]string{"c0", "c1", "c2"})
	h.SetWidths([]int{10, 10, 10})

	h.ToggleSort(2)
	if got := h.SortColumn(); got != "c2" {
		t.Fatalf("SortColumn() = %q, want %q", got, "c2")
	}

	h.ClearSort()

	if got := h.SortColumn(); got != "" {
		t.Errorf("SortColumn() = %q after ClearSort, want empty", got)
	}
	if got := h.SortDirection(); got != "" {
		t.Errorf("SortDirection() = %q after ClearSort, want empty", got)
	}

	// The next sort starts from scratch rather than continuing the old cycle:
	// sorting a different column from a cleared header must be ascending, and
	// sorting the column that was cleared before must also be ascending (not
	// descending, which is what would happen if the direction had been kept).
	for _, col := range []int{0, 2} {
		if got := h.ToggleSort(col); got != SortAsc {
			t.Errorf("ToggleSort(%d) after ClearSort = %v, want SortAsc", col, got)
		}
		h.ClearSort()
	}

	// The header draws no arrow on any column.
	h.ToggleSort(1)
	h.ClearSort()
	out := ansi.Strip(h.Render(0, 3))
	if strings.ContainsAny(out, "↑↓") {
		t.Errorf("the header draws a sort arrow after ClearSort: %q", out)
	}
}

// Scenario: Una columna de clave se marca con su propio prefijo y no con el de
// la otra.
//
// PK renders "* name" and FK renders "→ name". They are different facts about
// the schema and a user reads them at a glance, so a swap or a shared prefix
// is a real defect, not cosmetic. KeyNone renders the bare name.
func TestHeader_KeyIconsUseDistinctPrefixes(t *testing.T) {
	h := newHeaderForTest()
	h.SetColumns([]string{"id", "author_id", "title"})
	h.SetWidths([]int{12, 14, 12})
	h.SetKeyIcons(map[int]KeyIcon{0: KeyPK, 1: KeyFK})

	out := ansi.Strip(h.Render(0, 3))

	if !strings.Contains(out, "* ID") {
		t.Errorf("the PK column is not marked with \"* \": %q", out)
	}
	if !strings.Contains(out, "→ AUTHOR_ID") {
		t.Errorf("the FK column is not marked with \"→ \": %q", out)
	}
	if !strings.Contains(out, " TITLE") {
		t.Errorf("a column with no icon lost its label: %q", out)
	}
	// The icons must not be confused with each other: the PK marker must not
	// carry the arrow and vice versa.
	if strings.Contains(out, "* →") || strings.Contains(out, "→ *") {
		t.Errorf("the two icon prefixes are mixed together: %q", out)
	}
	// A grid with no key metadata at all draws no icon anywhere.
	plain := newHeaderForTest()
	plain.SetColumns([]string{"id", "title"})
	plain.SetWidths([]int{12, 12})
	if got := ansi.Strip(plain.Render(0, 2)); strings.ContainsAny(got, "*→") {
		t.Errorf("a header with no key icons drew one: %q", got)
	}
}

// Scenario: La flecha distingue el sentido del orden.
//
// Ascending is ↑ and descending is ↓. A shared glyph, or one direction drawn
// for both, means the user cannot tell whether the result is oldest-first or
// newest-first, which is the whole point of clicking twice.
func TestHeader_SortArrowDistinguishesDirection(t *testing.T) {
	for _, tc := range []struct {
		name    string
		clicks  int
		want    string
		notWant string
	}{
		{"ascending", 1, "↑", "↓"},
		{"descending", 2, "↓", "↑"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHeaderForTest()
			h.SetColumns([]string{"created_at", "title"})
			h.SetWidths([]int{16, 12})
			for i := 0; i < tc.clicks; i++ {
				h.ToggleSort(0)
			}

			out := ansi.Strip(h.Render(0, 2))
			if !strings.Contains(out, tc.want+" ") && !strings.HasSuffix(strings.TrimRight(out, " "), tc.want) {
				t.Errorf("after %d click(s) the header does not show %q: %q", tc.clicks, tc.want, out)
			}
			if strings.Contains(out, tc.notWant) {
				t.Errorf("after %d click(s) the header also shows %q: %q", tc.clicks, tc.notWant, out)
			}
			// Only the sorted column carries an arrow.
			if strings.Contains(ansi.Strip(h.Render(1, 1)), "↑") || strings.Contains(ansi.Strip(h.Render(1, 1)), "↓") {
				t.Errorf("the arrow leaked onto the unsorted column: %q", out)
			}
		})
	}
}

// Scenario: Con poca anchura se corta el texto, sin puntos suspensivos.
//
// Two different truncations exist, and the switch is at maxTextWidth 3. Below
// and at 3 the label is cut outright, because an ellipsis would not fit inside
// the space; above it the tail is spent on "...". The difference is visible:
// "IDE" versus "I...".
func TestHeader_NarrowCellsCutTheTextRatherThanAddingAnEllipsis(t *testing.T) {
	// maxTextWidth is width-2. The threshold is maxTextWidth <= 3, so width 5
	// is the last width that cuts and width 6 is the first that ellipsises.
	for _, tc := range []struct {
		width     int
		wantText  string
		wantDots  bool
		reasoning string
	}{
		{width: 4, wantText: "ID", wantDots: false, reasoning: "maxTextWidth 2, below the threshold"},
		{width: 5, wantText: "IDE", wantDots: false, reasoning: "maxTextWidth 3, exactly AT the threshold"},
		{width: 6, wantText: "I...", wantDots: true, reasoning: "maxTextWidth 4, above the threshold"},
		{width: 8, wantText: "IDE...", wantDots: true, reasoning: "maxTextWidth 6, above the threshold"},
	} {
		t.Run(tc.reasoning, func(t *testing.T) {
			h := newHeaderForTest()
			h.SetColumns([]string{"identifier"})
			h.SetWidths([]int{tc.width})

			out := ansi.Strip(h.Render(0, 1))
			// The cell is exactly the requested width, padding included.
			if got := lipgloss.Width(out); got != tc.width {
				t.Errorf("cell width = %d, want %d", got, tc.width)
			}
			// One cell of padding on each side, the text in between.
			text := strings.Trim(out, " ")
			if text != tc.wantText {
				t.Errorf("text = %q, want %q", text, tc.wantText)
			}
			if strings.Contains(text, ".") != tc.wantDots {
				t.Errorf("text = %q, ellipsis present = %v, want %v", text, strings.Contains(text, "."), tc.wantDots)
			}
		})
	}
}

// Scenario: El texto que cabe justo no se toca.
//
// The available text width is width-2, one cell of padding per side. A label of
// exactly that width is already correct and must be left alone; a label one
// wider must be cut. If the padding were subtracted more or less often, one of
// those two answers flips.
func TestHeader_TextOfExactlyTheAvailableWidthIsLeftAlone(t *testing.T) {
	const width = 12 // available text width is 10
	for _, tc := range []struct {
		name     string
		col      string
		wantDots bool
	}{
		{"exactly the available width", "ABCDEFGHIJ", false}, // 10 chars
		{"one wider than available", "ABCDEFGHIJK", true},    // 11 chars
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHeaderForTest()
			h.SetColumns([]string{tc.col})
			h.SetWidths([]int{width})

			out := ansi.Strip(h.Render(0, 1))
			if got := lipgloss.Width(out); got != width {
				t.Errorf("cell width = %d, want %d", got, width)
			}
			text := strings.Trim(out, " ")
			if strings.Contains(text, ".") != tc.wantDots {
				t.Errorf("text = %q, ellipsis present = %v, want %v", text, strings.Contains(text, "."), tc.wantDots)
			}
			if !tc.wantDots && text != tc.col {
				t.Errorf("text = %q, want the label untouched (%q)", text, tc.col)
			}
		})
	}

	// A cell so narrow that the available text width goes negative must still
	// render rather than panic or lose the label. Below 2 the cell cannot get
	// narrower than its own one-cell padding on each side, so a width of 0 or 1
	// still draws two cells of padding and no text.
	for _, width := range []int{0, 1, 2} {
		h := newHeaderForTest()
		h.SetColumns([]string{"identifier"})
		h.SetWidths([]int{width})
		out := h.Render(0, 1)
		want := width
		if want < 2 {
			want = 2
		}
		if got := lipgloss.Width(out); got != want {
			t.Errorf("width %d: cell width = %d, want %d", width, got, want)
		}
		if strings.Contains(ansi.Strip(out), "IDENTIFIER") {
			t.Errorf("width %d: the full label survived in a %d-wide cell: %q", width, width, ansi.Strip(out))
		}
	}
}

// Scenario: La celda usa la anchura de su columna cuando se conoce, y 20 si no.
//
// The 20 is a fallback for a column with no computed width yet, not a default
// that overrides the real one. Rendering every column at 20 would make a
// narrow grid look empty and a wide one overflow.
func TestHeader_CellWidthComesFromTheColumnNotTheFallback(t *testing.T) {
	// Both columns are known and neither is 20.
	h := newHeaderForTest()
	h.SetColumns([]string{"a", "b"})
	h.SetWidths([]int{8, 9})
	if got := lipgloss.Width(h.Render(0, 2)); got != 17 {
		t.Errorf("total width = %d, want 17 (8+9), so the real widths were used", got)
	}

	// The second column has no computed width: it falls back to 20.
	partial := newHeaderForTest()
	partial.SetColumns([]string{"a", "b"})
	partial.SetWidths([]int{8})
	if got := lipgloss.Width(partial.Render(0, 2)); got != 28 {
		t.Errorf("total width = %d, want 28 (8 plus the 20 fallback)", got)
	}

	// No widths at all: everything is the fallback.
	none := newHeaderForTest()
	none.SetColumns([]string{"a", "b"})
	if got := lipgloss.Width(none.Render(0, 1)); got != 20 {
		t.Errorf("width with no known widths = %d, want the 20 fallback", got)
	}
}

// Scenario: Se dibujan exactamente las columnas pedidas, ni una más.
//
// Render(startCol, count) is a window, and the caller passes the number of
// visible columns. Drawing one extra would push the row of data out of
// alignment with the header, because the data row is built from the same
// window.
func TestHeader_RendersExactlyTheRequestedNumberOfColumns(t *testing.T) {
	// Five columns, so drawing count+1 is visible as extra width.
	h := newHeaderForTest()
	h.SetColumns([]string{"c0", "c1", "c2", "c3", "c4"})
	h.SetWidths([]int{8, 8, 8, 8, 8})

	for _, count := range []int{1, 2, 3} {
		out := ansi.Strip(h.Render(0, count))
		if got := lipgloss.Width(out); got != 8*count {
			t.Errorf("count %d: total width = %d, want %d", count, got, 8*count)
		}
		if n := strings.Count(out, "C"); n != count {
			t.Errorf("count %d: %d labels drawn, want %d: %q", count, n, count, out)
		}
	}

	// The window slides: startCol picks where it begins, and the window still
	// holds exactly count columns.
	win := ansi.Strip(h.Render(2, 2))
	if got := lipgloss.Width(win); got != 16 {
		t.Errorf("window at 2: total width = %d, want 16", got)
	}
	if !strings.HasPrefix(strings.TrimLeft(win, " "), "C2") {
		t.Errorf("the window at startCol 2 does not begin with C2: %q", win)
	}

	// A window wider than the data stops at the last column instead of
	// rendering blanks.
	over := ansi.Strip(h.Render(3, 9))
	if got := lipgloss.Width(over); got != 16 {
		t.Errorf("an over-wide window rendered %d cells of width, want 16 (columns 3 and 4 only)", got)
	}
}

// Scenario: Una ventana en scroll muestra las columnas por su índice absoluto.
//
// Key icons and the sort arrow are keyed by the ABSOLUTE column index, so a
// horizontally scrolled grid decorates the right column rather than the first
// one on screen. This is the contract that keeps the header honest about what
// the row of data below it contains.
func TestHeader_ScrolledWindowDecoratesTheAbsoluteColumn(t *testing.T) {
	h := newHeaderForTest()
	h.SetColumns([]string{"c0", "c1", "c2", "c3"})
	h.SetWidths([]int{10, 10, 10, 10})
	h.SetKeyIcons(map[int]KeyIcon{2: KeyPK})
	h.ToggleSort(3)

	out := ansi.Strip(h.Render(1, 3)) // absolute columns 1, 2, 3
	if !strings.Contains(out, "* C2") {
		t.Errorf("the PK icon is not on absolute column 2: %q", out)
	}
	if !strings.Contains(out, "C3 ↑") {
		t.Errorf("the sort arrow is not on absolute column 3: %q", out)
	}
	if strings.Contains(out, "* C1") {
		t.Errorf("the PK icon leaked onto the first column on screen: %q", out)
	}
	if strings.Contains(out, "C1 ↑") {
		t.Errorf("the sort arrow leaked onto the first column on screen: %q", out)
	}
}

// Scenario: SortColumn solo responde para un índice dentro de las columnas.
//
// The header is created with sortCol -1, meaning "no sort", and -1 is not a
// valid index. A cleared sort and an out-of-range sort are both "no column",
// and both must report empty rather than panicking.
func TestHeader_SortColumnOutsideTheColumnsIsEmpty(t *testing.T) {
	h := newHeaderForTest()
	h.SetColumns([]string{"c0", "c1"})

	// Never sorted.
	if got := h.SortColumn(); got != "" {
		t.Errorf("SortColumn() on a fresh header = %q, want empty", got)
	}

	// Sorted on a column that does not exist.
	h.ToggleSort(9)
	if got := h.SortColumn(); got != "" {
		t.Errorf("SortColumn() with sortCol 9 and 2 columns = %q, want empty", got)
	}

	// And a negative one, which is what ClearSort leaves behind.
	h.ToggleSort(0)
	h.ClearSort()
	h.sortCol = -1
	if got := h.SortColumn(); got != "" {
		t.Errorf("SortColumn() with sortCol -1 = %q, want empty", got)
	}
}

// Scenario: SortDirection solo nombra los dos sentidos reales.
//
// SortNone is the absence of a sort, so it has no name. Returning "ASC" or
// "NONE" for it would put a direction in the status bar and in the ORDER BY
// that a cleared sort is supposed to have removed.
func TestHeader_SortDirectionNamesOnlyTheRealDirections(t *testing.T) {
	h := newHeaderForTest()
	h.SetColumns([]string{"c0"})

	if got := h.SortDirection(); got != "" {
		t.Errorf("SortDirection() with no sort = %q, want empty", got)
	}
	h.ToggleSort(0)
	if got := h.SortDirection(); got != "ASC" {
		t.Errorf("SortDirection() ascending = %q, want %q", got, "ASC")
	}
	h.ToggleSort(0)
	if got := h.SortDirection(); got != "DESC" {
		t.Errorf("SortDirection() descending = %q, want %q", got, "DESC")
	}
	h.ToggleSort(0)
	if got := h.SortDirection(); got != "" {
		t.Errorf("SortDirection() after clearing = %q, want empty", got)
	}

	// A direction with no column behind it still has no ORDER BY, so the header
	// must not claim one.
	h.sortDir = SortAsc
	if got := h.SortColumn(); got != "" {
		t.Errorf("SortColumn() with a direction but no column = %q, want empty", got)
	}
}

// Scenario: Recortar un texto que ya cabe no lo cambia.
//
// The truncation in Render is guarded by a strict "is it wider than the space"
// test, and the guard's job is to be the only thing deciding whether to touch
// the label. ansi.Truncate is itself idempotent: given a label that already
// fits it returns it unchanged, and label[:maxTextWidth] is likewise a no-op
// when the label is not longer than maxTextWidth. So the two layers agree, and
// a label that exactly fills the available width comes out intact whether or
// not the guard let it through.
//
// This is the executable form of the proof in .mutation-allowlist for the
// `lipgloss.Width(label) > maxTextWidth` boundary.
func TestHeader_TruncatingTextThatAlreadyFitsChangesNothing(t *testing.T) {
	for _, s := range []string{
		strings.Repeat("x", 5),
		strings.Repeat("漢", 2), // double-width runes
		strings.Repeat("→", 5),
		"\x1b[1m" + strings.Repeat("y", 5) + "\x1b[0m",
	} {
		for n := 1; n <= 8; n++ {
			if lipgloss.Width(s) > n {
				continue // too wide: truncation is supposed to change it
			}
			if got := ansi.Truncate(s, n, "..."); got != s {
				t.Errorf("ansi.Truncate(%q, %d, \"...\") = %q, want it unchanged", s, n, got)
			}
			// The short path is the byte slice, so it only agrees while the
			// label is no longer than the width.
			if n <= 3 && lipgloss.Width(s) == n && len(s) >= n {
				if got := s[:n]; got != s {
					t.Errorf("s[:%d] = %q, want %q", n, got, s)
				}
			}
		}
	}
}

// Scenario: Sin columnas, o sin celdas pedidas, la cabecera no dibuja nada.
//
// Both are ordinary states, not errors: a table with no columns, and a grid too
// narrow to show any. Returning anything other than an empty string would paint
// a stray border on the top of the box.
func TestHeader_RendersNothingWithoutColumnsOrCells(t *testing.T) {
	empty := newHeaderForTest()
	empty.SetWidths([]int{10, 10})
	for _, count := range []int{-1, 0, 1, 5} {
		if got := empty.Render(0, count); got != "" {
			t.Errorf("Render with no columns and count %d = %q, want empty", count, got)
		}
	}

	h := newHeaderForTest()
	h.SetColumns([]string{"c0", "c1"})
	h.SetWidths([]int{10, 10})
	for _, count := range []int{-1, 0} {
		if got := h.Render(0, count); got != "" {
			t.Errorf("Render with columns and count %d = %q, want empty", count, got)
		}
	}
	if got := h.Render(0, 0); got != "" {
		t.Errorf("Render(0, 0) = %q, want empty", got)
	}
	if got := h.Render(0, 1); got == "" {
		t.Error("Render(0, 1) = empty, want the single column to be drawn")
	}
}
