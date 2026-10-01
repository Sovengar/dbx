package grid

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/buble/dbx/internal/theme"
	"github.com/charmbracelet/x/ansi"
)

func newCellRendererForTest() *CellRenderer {
	return NewCellRenderer(theme.Resolve("dark").Styles())
}

// rowRenderer is one of the ten Render*Row functions, behind a uniform signature
// so they can be tabled.
//
// They are the same function ten times: format each value, truncate it to the
// column's width minus its padding, style it, and join. Tableing them is not a
// shortcut, it is the observation that they share one contract, and it is why a
// change to any one of them has to keep the same geometry as the other nine.
type rowRenderer struct {
	name string
	// render takes the values, the widths, and the active column, and returns the
	// row. The last parameter is the draft-column set, which only one of them uses.
	render func(vals []interface{}, widths []int, activeCol int, draftCols map[int]bool) string
	// usesActiveCol is false for the two renderers that take the parameter and
	// ignore it. Recorded rather than hidden: see the pinned test below.
	usesActiveCol bool
}

func rowRenderers() []rowRenderer {
	return []rowRenderer{
		{"RenderRow", func(v []interface{}, w []int, a int, _ map[int]bool) string { return cellRenderer().RenderRow(v, w, a) }, true},
		{"RenderSelectedRow", func(v []interface{}, w []int, a int, _ map[int]bool) string {
			return cellRenderer().RenderSelectedRow(v, w, a)
		}, false},
		{"RenderCursorSelectedRow", func(v []interface{}, w []int, a int, _ map[int]bool) string {
			return cellRenderer().RenderCursorSelectedRow(v, w, a)
		}, true},
		{"RenderPendingRow", func(v []interface{}, w []int, a int, _ map[int]bool) string {
			return cellRenderer().RenderPendingRow(v, w, a)
		}, false},
		{"RenderDraftInsertRow", func(v []interface{}, w []int, a int, _ map[int]bool) string {
			return cellRenderer().RenderDraftInsertRow(v, w)
		}, false},
		{"RenderDraftInsertSelectedRow", func(v []interface{}, w []int, a int, _ map[int]bool) string {
			return cellRenderer().RenderDraftInsertSelectedRow(v, w)
		}, false},
		{"RenderDraftDeleteRow", func(v []interface{}, w []int, a int, _ map[int]bool) string {
			return cellRenderer().RenderDraftDeleteRow(v, w, a)
		}, true},
		{"RenderDraftUpdateRow", func(v []interface{}, w []int, a int, d map[int]bool) string {
			return cellRenderer().RenderDraftUpdateRow(v, w, d, a)
		}, true},
		// The three edit-row renderers take an edit column, an edit value and a
		// cursor, so they are tabled separately below rather than squeezed in here.
	}
}

// cellRenderer is the shared instance the table's closures use. Every renderer
// here is a pure function of its arguments and the shared theme, so sharing one is
// safe and keeps the table readable.
var sharedCellRenderer = newCellRendererForTest()

func cellRenderer() *CellRenderer { return sharedCellRenderer }

var (
	// Four columns of different widths, with values chosen to exercise every
	// FormatValue branch: an int, a string, a NULL and a bool.
	cellValues = []interface{}{1, "abc", nil, true}
	cellWidths = []int{6, 8, 5, 7}
)

func sumWidths(ws []int) int {
	total := 0
	for _, w := range ws {
		total += w
	}
	return total
}

// Scenario: Una fila mide exactamente la suma de las anchuras de sus columnas.
//
// The row IS the grid's geometry: the cells are concatenated and drawn inside a
// box whose width was computed from the same column widths. A row that is one cell
// narrower leaves a gap at the right edge; one cell wider overflows the border and
// wraps the whole box onto another line.
//
// This is the master invariant of the file and it is asserted for every renderer
// rather than one at a time, because all ten are the same function.
func TestEveryRowRenderer_IsExactlyTheSumOfItsColumnWidths(t *testing.T) {
	for _, r := range rowRenderers() {
		t.Run(r.name, func(t *testing.T) {
			// Every width is at least 3, which is the smallest that can hold the
			// one cell of padding on each side AND a character. Below that the
			// row is WIDER than the sum, and that is a separate pinned test.
			for _, widths := range [][]int{
				{6, 8, 5, 7},
				{3, 3, 3, 3}, // the minimum, exactly
				{4, 4, 4, 4},
				{20, 4, 12, 6},
				{40, 40, 40, 40},
			} {
				out := r.render(cellValues, widths, 1, map[int]bool{1: true, 2: true})
				if got, want := lipgloss.Width(out), sumWidths(widths); got != want {
					t.Errorf("widths %v: the row is %d cells, want %d", widths, got, want)
				}
			}
		})
	}
}

// Scenario: La columna activa cambia el ESTILO de esa columna y nada más.
//
// Two assertions, and both are needed. The raw strings must DIFFER, or the
// highlight is not being drawn at all. The stripped strings must be EQUAL, or the
// active column is changing the TEXT — which would mean the grid edits or
// reformats a value just because the cursor moved onto it.
//
// This is also what pins WHICH column: a renderer that highlighted the wrong index
// would produce a difference at the wrong place and the per-index check below
// catches it.
func TestRowRenderers_TheActiveColumnChangesOnlyStyling(t *testing.T) {
	for _, r := range rowRenderers() {
		if !r.usesActiveCol {
			continue
		}
		t.Run(r.name, func(t *testing.T) {
			draft := map[int]bool{1: true, 2: true}
			for active := range cellValues {
				base := r.render(cellValues, cellWidths, active, draft)

				// Every other column must give a different raw string, or two
				// positions are indistinguishable and one of them is being ignored.
				for other := range cellValues {
					if other == active {
						continue
					}
					alt := r.render(cellValues, cellWidths, other, draft)
					if alt == base {
						t.Errorf("activeCol %d and %d render identically, so one of them is not being highlighted", active, other)
					}
					if got, want := ansi.Strip(alt), ansi.Strip(base); got != want {
						t.Errorf("activeCol %d changes the TEXT, not just the style:\n %q\n %q", other, got, want)
					}
					if lipgloss.Width(alt) != lipgloss.Width(base) {
						t.Errorf("activeCol %d changes the width: %d then %d", other, lipgloss.Width(base), lipgloss.Width(alt))
					}
				}
			}
		})
	}
}

// Scenario: DOS renderers aceptan la columna activa y la ignoran.
//
// KNOWN BEHAVIOUR, pinned because it is surprising and because the parameter being
// there is what makes it look like it works.
//
// RenderSelectedRow, RenderPendingRow, RenderDraftInsertRow and
// RenderDraftInsertSelectedRow all give every cell the same style, so there is
// nothing for a per-column highlight to do and the parameter is dead. The
// observable consequence is that pressing left or right in those modes does not
// move a visible highlight — the row looks the same whichever column the cursor is
// on.
//
// RenderPendingRow is the surprising one: it is named like the plain row, takes an
// active column, and then styles every cell the same way. It was recorded as
// ignoring the parameter only because a test compared two renderings and found them
// identical.
//
// Whether that is right is a product question, not a testing one. What matters here
// is that it is a decision on record: if a later change makes these honour
// activeCol, this test is what will notice.
func TestRowRenderers_TwoOfThemIgnoreTheActiveColumn(t *testing.T) {
	ignoring := map[string]bool{}
	for _, r := range rowRenderers() {
		if !r.usesActiveCol {
			ignoring[r.name] = true
		}
	}
	for _, want := range []string{"RenderSelectedRow", "RenderPendingRow", "RenderDraftInsertRow", "RenderDraftInsertSelectedRow"} {
		if !ignoring[want] {
			t.Errorf("%s is expected to ignore activeCol but is recorded as using it", want)
		}
	}

	// And the behaviour matches the record: the output really is identical.
	for _, r := range rowRenderers() {
		if r.usesActiveCol {
			continue
		}
		t.Run(r.name, func(t *testing.T) {
			for active := range cellValues {
				a := r.render(cellValues, cellWidths, active, map[int]bool{1: true})
				b := r.render(cellValues, cellWidths, 0, map[int]bool{1: true})
				if a != b {
					t.Errorf("activeCol %d changed the output, but this renderer is recorded as ignoring it", active)
				}
			}
		})
	}
}

// Scenario: La fila enseña el valor formateado, recortado a la columna.
//
// The value goes through FormatValue and then Truncate at width-2, which is the
// room left by the one cell of padding on each side. Getting the -2 wrong makes a
// value one cell too long for its column, which pushes the rest of the row right.
func TestEveryRowRenderer_ShowsTheFormattedValue(t *testing.T) {
	// Wide enough that nothing is truncated, so the assertion is about the VALUE.
	// The expectation is built from the same rule the code uses — one cell of
	// padding each side, the value, then padding out to width — rather than by
	// counting spaces by hand, which is how this assertion was wrong the first
	// time it was written.
	wide := []int{20, 20, 20, 20}
	texts := []string{"1", "abc", "NULL", "true"}
	var wantText strings.Builder
	for i, text := range texts {
		wantText.WriteString(" " + text + strings.Repeat(" ", wide[i]-2-len(text)) + " ")
	}
	want := wantText.String()

	for _, r := range rowRenderers() {
		t.Run(r.name, func(t *testing.T) {
			got := ansi.Strip(r.render(cellValues, wide, 1, map[int]bool{1: true, 2: true}))
			if got != want {
				t.Errorf("the row is\n%q\nwant\n%q", got, want)
			}
		})
	}
}

// expectedRow builds what a row of the given value across the given widths should
// look like, from the rule rather than from the implementation: each cell is one
// cell of padding, the value cut to width-2 — spending its last three cells on the
// tail when there are 4 or more, and cut outright below that — then padding out to
// width.
func expectedRow(value string, widths []int) string {
	var b strings.Builder
	for _, w := range widths {
		text := value
		switch room := w - 2; {
		case room <= 0:
			// No room at all: Truncate returns the value whole and the cell grows,
			// which is the degenerate case its own test covers.
			text = value
		case room <= 3:
			text = value[:room]
		case room < len(value):
			text = value[:room-3] + "..."
		}
		b.WriteString(" " + text)
		if pad := w - 1 - len(text); pad > 0 {
			b.WriteString(strings.Repeat(" ", pad))
		}
	}
	return b.String()
}

// Scenario: Un valor largo se recorta a la columna, y el texto recortado es el
// que se ve.
//
// The truncation happens at width-2, so a value wider than its column is cut and
// the row keeps its geometry. Asserting the visible text pins the -2: a -1 or -3
// would be off by a cell and the stripped text would not match.
func TestEveryRowRenderer_TruncatesToTheColumnWidth(t *testing.T) {
	long := "abcdefghijklmnop"
	values := []interface{}{long, long, long, long}
	widths := []int{8, 6, 5, 10}

	// The expectation is derived from the RULE, not from the implementation: a
	// column of w cells has w-2 of text room, and a text room of 4 or more spends
	// its last 3 cells on the tail while a smaller one is cut outright. Writing
	// the expected strings by hand got this wrong once already, by forgetting the
	// ellipsis on three of the four columns.
	want := expectedRow(long, widths)
	for _, r := range rowRenderers() {
		t.Run(r.name, func(t *testing.T) {
			got := ansi.Strip(r.render(values, widths, 1, map[int]bool{1: true, 2: true}))
			if got != want {
				t.Errorf("the row is\n%q\nwant\n%q", got, want)
			}
		})
	}
}

// Scenario: Las filas de edición ponen el cursor en la columna editada, y solo en
// ella.
//
// Three renderers edit a cell: the plain one, the pending one and the draft-insert
// one. All three take an edit column, and the edited cell has to be that one — a
// cursor drawn one column to the left of the edit would put the user's typing in
// the wrong field.
func TestEditRowRenderers_PutTheCursorInTheEditedColumn(t *testing.T) {
	widths := []int{10, 10, 10, 10}

	for _, tc := range []struct {
		name   string
		render func(vals []interface{}, widths []int, editCol int, editValue string, cursor int) string
	}{
		{"RenderEditRow", func(v []interface{}, w []int, c int, e string, p int) string {
			return cellRenderer().RenderEditRow(v, w, c, e, p)
		}},
		{"RenderPendingEditRow", func(v []interface{}, w []int, c int, e string, p int) string {
			return cellRenderer().RenderPendingEditRow(v, w, c, e, p)
		}},
		{"RenderDraftInsertEditRow", func(v []interface{}, w []int, c int, e string, p int) string {
			return cellRenderer().RenderDraftInsertEditRow(v, w, c, e, p)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := []interface{}{"a", "b", "c", "d"}
			// What the user has TYPED, and where the block goes inside it. The
			// display is the typed text with a block spliced in at the cursor, so
			// "XY" at position 1 renders as "X<block>Y" — four display cells for
			// three characters. Passing the expected DISPLAY as the typed value
			// instead is what made this assertion wrong the first time.
			const typed = "XY"
			const shown = "X█Y"
			for editCol := range values {
				out := ansi.Strip(tc.render(values, widths, editCol, typed, 1))

				// Built positionally, the way the code does: each cell is one cell
				// of padding, the text, then padding out to the width. Writing the
				// edited cell first and appending the rest is what made this
				// assertion wrong the first time.
				var wantText strings.Builder
				for i, v := range values {
					text := string(v.(string))
					if i == editCol {
						text = shown
					}
					wantText.WriteString(" " + text)
					if pad := widths[i] - 1 - lipgloss.Width(text); pad > 0 {
						wantText.WriteString(strings.Repeat(" ", pad))
					}
				}
				want := wantText.String()

				if out != want {
					t.Errorf("editCol %d: the row is\n%q\nwant\n%q", editCol, out, want)
				}
				if got := lipgloss.Width(tc.render(values, widths, editCol, typed, 1)); got != sumWidths(widths) {
					t.Errorf("editCol %d: the row is %d cells, want %d", editCol, got, sumWidths(widths))
				}
			}
		})
	}
}

// Scenario: El cursor de edición se coloca donde le dicen, y se recorta al texto.
//
// The cursor is drawn as a block between the left and right halves, so its position
// in the output IS the cursor position. A position past the end of the text has to
// be clamped, or the slice panics on a user holding right-arrow at the end of a
// field.
func TestRenderEditCell_TheBlockIsWhereTheCursorIs(t *testing.T) {
	cr := newCellRendererForTest()

	for _, tc := range []struct {
		name, text string
		cursor     int
		want       string
	}{
		{"at the start", "abcd", 0, "█abcd"},
		{"in the middle", "abcd", 2, "ab█cd"},
		{"just before the end", "abcd", 3, "abc█d"},
		{"at the end", "abcd", 4, "abcd█"},
		{"past the end is clamped", "abcd", 99, "abcd█"},
		{"far past the end is clamped", "abcd", 1000, "abcd█"},
		{"an empty value", "", 0, "█"},
		{"an empty value with the cursor past it", "", 5, "█"},
		{"a multibyte value counts runes not bytes", "日本", 1, "日█本"},
		{"a multibyte value at the end", "日本", 2, "日本█"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ansi.Strip(cr.RenderEditCell(nil, 20, tc.text, tc.cursor))
			// One cell of padding, the display, then padding to the width.
			want := " " + tc.want
			if !strings.HasPrefix(got, want) {
				t.Errorf("RenderEditCell(%q, cursor %d) starts %q, want it to start %q", tc.text, tc.cursor, got, want)
			}
		})
	}
}

// Scenario: El valor original de la celda no se usa al editarla.
//
// RenderEditCell takes the cell's current value and ignores it, because the cell
// shows what the user is TYPING (editValue), not what the database holds. Pinned
// because a change to use the value would silently discard the user's keystrokes,
// and because the two arguments are easy to confuse at every call site.
func TestRenderEditCell_IgnoresTheStoredValue(t *testing.T) {
	cr := newCellRendererForTest()

	// The cursor is a block SPLICED into the typed text, so "typed" is never a
	// contiguous substring of the output. What is asserted is that the two halves
	// around the block are the typed text, which is what makes the two renderings
	// comparable at all.
	withValue := ansi.Strip(cr.RenderEditCell("the stored value", 20, "typed", 2))
	withoutValue := ansi.Strip(cr.RenderEditCell(nil, 20, "typed", 2))
	if withValue != withoutValue {
		t.Errorf("the stored value changed the output:\n %q\n %q", withValue, withoutValue)
	}
	if !strings.HasPrefix(withValue, " ty█ped") {
		t.Errorf("the output is not the typed text with the block at the cursor: %q", withValue)
	}
	if strings.Contains(withValue, "stored") {
		t.Errorf("the output shows the stored value instead of the typed one: %q", withValue)
	}
}

// --- FormatValue -------------------------------------------------------------

// Scenario: Cada tipo de Go se imprime de la forma que espera quien lo lee.
//
// The grid shows these strings to a user deciding how to filter or edit a value, so
// the shapes are a contract: an integer must not come out as 1.000000e+00, a bool
// must not come out as 0x1, and a byte slice must not come out as a list of
// numbers.
func TestFormatValue_EachTypeHasItsOwnShape(t *testing.T) {
	cr := newCellRendererForTest()

	for _, tc := range []struct {
		name  string
		value interface{}
		want  string
	}{
		{"nil is the word NULL", nil, "NULL"},
		{"a byte slice is its text", []byte("hello"), "hello"},
		{"an empty byte slice", []byte{}, ""},
		{"a string is itself", "abc", "abc"},
		{"an empty string", "", ""},
		{"an int", int(42), "42"},
		{"a negative int", int(-7), "-7"},
		{"an int32", int32(2147483647), "2147483647"},
		{"an int64", int64(9007199254740993), "9007199254740993"},
		{"a float32", float32(1.5), "1.5"},
		{"a float64", float64(2.25), "2.25"},
		{"a float64 that %v would render as an exponent", float64(1e21), "1e+21"},
		{"a whole float64", float64(3), "3"},
		{"true", true, "true"},
		{"false", false, "false"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ansi.Strip(cr.FormatValue(tc.value)); got != tc.want {
				t.Errorf("FormatValue(%#v) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}

	// The default branch is %v, which is what anything unrecognised gets. A struct
	// and a slice are the two a driver can actually hand back.
	for _, tc := range []struct {
		name  string
		value interface{}
		want  string
	}{
		{"a slice of ints", []int{1, 2}, "[1 2]"},
		{"a struct", struct{ A, B int }{1, 2}, "{1 2}"},
		{"a pointer to a struct", &struct{ A int }{7}, "&{7}"},
		{"a map", map[string]int{"a": 1}, "map[a:1]"},
		{"an untyped int (never nil, always int)", 0, "0"},
	} {
		t.Run("default branch/"+tc.name, func(t *testing.T) {
			if got := ansi.Strip(cr.FormatValue(tc.value)); got != tc.want {
				t.Errorf("FormatValue(%#v) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}
}

// Scenario: NULL se pinta atenuado, y es la palabra NULL.
//
// A NULL rendered as the empty string is indistinguishable from an empty string, and
// the user cannot tell a missing value from a blank one. The word is what makes them
// different, so it is pinned including the styling.
func TestFormatValue_NullIsTheStyledWordNull(t *testing.T) {
	cr := newCellRendererForTest()
	styles := theme.Resolve("dark").Styles()

	got := cr.FormatValue(nil)
	if want := styles.TextMuted.Render("NULL"); got != want {
		t.Errorf("FormatValue(nil) = %q, want the muted NULL %q", got, want)
	}
	if plain := ansi.Strip(got); plain != "NULL" {
		t.Errorf("FormatValue(nil) stripped to %q, want NULL", plain)
	}
	// And it is NOT the empty string, which is the whole point.
	if got == "" {
		t.Error("FormatValue(nil) returned the empty string")
	}
}

// --- Truncate ----------------------------------------------------------------

// Scenario: Recortar deja la celda con su texto entero, o la marca de corte.
//
// Two regimes, split at maxWidth 3: below it there is no room for a tail, so the
// text is cut hard; at or above it the last cells are spent on "...". The boundary
// is the interesting part, so it is pinned from both sides.
func TestTruncate_OnlySpendsCellsOnTheEllipsisWhenThereIsRoom(t *testing.T) {
	cr := newCellRendererForTest()

	for _, tc := range []struct {
		width int
		want  string
	}{
		{1, "a"},
		{2, "ab"},
		{3, "abc"},
		{4, "a..."},
		{5, "ab..."},
		{6, "abc..."},
		{8, "abcde..."},
		{10, "abcdefghij"}, // exactly the text: untouched
		{20, "abcdefghij"}, // wider than the text: untouched
	} {
		t.Run(fmt.Sprintf("width=%d", tc.width), func(t *testing.T) {
			if got := cr.Truncate("abcdefghij", tc.width); got != tc.want {
				t.Errorf("Truncate(%q, %d) = %q, want %q", "abcdefghij", tc.width, got, tc.want)
			}
		})
	}
}

// Scenario: Una anchura no positiva no recorta nada.
//
// A collapsed grid computes a width of 0 or a negative one. Returning the text
// whole is what keeps the cell from vanishing; returning "" would blank the column.
func TestTruncate_ANonPositiveWidthReturnsTheText(t *testing.T) {
	cr := newCellRendererForTest()
	for _, w := range []int{0, -1, -100} {
		if got := cr.Truncate("abcdefghij", w); got != "abcdefghij" {
			t.Errorf("Truncate(%q, %d) = %q, want the text untouched", "abcdefghij", w, got)
		}
	}
	// And an empty text stays empty at every width.
	for _, w := range []int{-1, 0, 1, 5} {
		if got := cr.Truncate("", w); got != "" {
			t.Errorf("Truncate(\"\", %d) = %q, want empty", w, got)
		}
	}
}

// Scenario: La anchura se mide en celdas de pantalla, no en bytes ni en runes.
//
// The grid is a grid of terminal cells, so a value containing CJK or an accented
// character has to be cut by DISPLAY width. Counting bytes would cut a two-byte
// rune in half and emit a broken character.
func TestTruncate_MeasuresDisplayWidth(t *testing.T) {
	cr := newCellRendererForTest()

	// Each case states the display width it assumes AND checks it, rather than
	// leaving the test to depend on arithmetic done in its head. Two drafts of
	// this test were wrong about what these strings measure: 日本語 is six cells
	// (three runes of two), not four, and 日本語だ is eight.
	//
	// What makes this a real test of display width rather than of a coincidence:
	// for every one of these, the BYTE count and the RUNE count differ from the
	// cell count, so cutting by either would give a different answer.
	for _, tc := range []struct {
		s     string
		cells int
	}{
		{"日本語", 6},
		{"éé", 2},
		{"🎉🎉", 4},
		{"ab日", 4},
		{"aé", 2},
	} {
		t.Run(fmt.Sprintf("%q=%dcells", tc.s, tc.cells), func(t *testing.T) {
			if got := lipgloss.Width(tc.s); got != tc.cells {
				t.Fatalf("lipgloss.Width(%q) = %d, but the test assumes %d: fix the case, not the code", tc.s, got, tc.cells)
			}
			// Exactly wide enough: untouched.
			if got := cr.Truncate(tc.s, tc.cells); got != tc.s {
				t.Errorf("Truncate(%q, %d) = %q, want it untouched: it fits exactly", tc.s, tc.cells, got)
			}
			// Wider: untouched.
			if got := cr.Truncate(tc.s, tc.cells+4); got != tc.s {
				t.Errorf("Truncate(%q, %d) = %q, want it untouched", tc.s, tc.cells+4, got)
			}
			// Narrower: cut, never past the width and never splitting a rune.
			if tc.cells >= 2 {
				got := cr.Truncate(tc.s, tc.cells-1)
				if w := lipgloss.Width(got); w > tc.cells-1 {
					t.Errorf("Truncate(%q, %d) = %q, which is %d cells wide", tc.s, tc.cells-1, got, w)
				}
				// Whatever came back has to be the input's own characters, and
				// only the tail may be something else. A prefix of a UTF-8
				// string is always valid UTF-8, so this is also the check that
				// no rune was cut in half. The tail is stripped first, because
				// "日..." is not a prefix of anything.
				kept := strings.TrimSuffix(got, "...")
				if !strings.HasPrefix(tc.s, kept) {
					t.Errorf("Truncate(%q, %d) = %q, whose text %q is not a prefix of the input", tc.s, tc.cells-1, got, kept)
				}
				if strings.Count(tc.s, "�") != 0 {
					t.Errorf("the input %q contains a replacement character, so this case proves nothing", tc.s)
				}
			}
		})
	}
}

// Scenario: Un NULL se recorta como cualquier otro texto, y con el mismo ancho.
//
// FormatValue returns a STYLED "NULL", so Truncate is being handed a string with
// escape codes in it. It has to measure the 4 visible cells and cut on that, or the
// cell's Width() and its text disagree by the length of the escapes.
func TestTruncate_HandlesAStyledValue(t *testing.T) {
	cr := newCellRendererForTest()
	styled := cr.FormatValue(nil)

	if got := cr.Truncate(styled, 6); ansi.Strip(got) != "NULL" {
		t.Errorf("Truncate(styled NULL, 6) stripped to %q, want NULL", ansi.Strip(got))
	}
	if got := cr.Truncate(styled, 4); ansi.Strip(got) != "NULL" {
		t.Errorf("Truncate(styled NULL, 4) stripped to %q, want NULL", ansi.Strip(got))
	}
	// Too narrow and the tail goes, but the styling must not break the count.
	narrow := cr.Truncate(styled, 2)
	if got := lipgloss.Width(narrow); got > 2 {
		t.Errorf("Truncate(styled NULL, 2) is %d cells wide, want at most 2", got)
	}
}

// --- RenderCell --------------------------------------------------------------

// Scenario: Una celda mide su anchura, con un espacio de acolchado a cada lado.
//
// The padding is the reason the truncation is at width-2. Both are pinned together
// because they are two halves of one decision: text that fits is centred between
// two spaces, and text that does not is cut to leave room for them.
func TestRenderCell_IsExactlyTheGivenWidth(t *testing.T) {
	cr := newCellRendererForTest()

	// And the degenerate side, stated here so the invariant above is not read as
	// "always": a cell narrower than 3 comes out WIDER than asked.
	for _, w := range []int{0, 1, 2} {
		if got := lipgloss.Width(cr.RenderCell("abc", w, false)); got <= w {
			t.Errorf("RenderCell(width %d) is %d cells, want it wider: there is no room for text", w, got)
		}
	}

	// Widths start at 3: a narrower cell has no room for text at all, so it grows
	// to fit its content. That is pinned separately, as a degenerate case.
	for _, selected := range []bool{false, true} {
		for _, w := range []int{3, 4, 5, 10, 40} {
			for _, v := range []interface{}{1, "abc", nil, "a very long value indeed", "日本"} {
				got := cr.RenderCell(v, w, selected)
				if width := lipgloss.Width(got); width != w {
					t.Errorf("RenderCell(%#v, %d, selected=%v) is %d cells, want %d", v, w, selected, width, w)
				}
			}
		}
	}
}

// Scenario: La celda activa se pinta distinto, y el texto es el mismo.
//
// The same two-part assertion as the rows: the raw strings differ, the stripped
// strings do not. A selected cell that rendered different text would be a bug the
// user sees as the value changing when the cursor moves.
func TestRenderCell_SelectedChangesOnlyTheStyling(t *testing.T) {
	cr := newCellRendererForTest()
	styles := theme.Resolve("dark").Styles()

	for _, v := range []interface{}{1, "abc", nil, true, "日本"} {
		normal := cr.RenderCell(v, 20, false)
		active := cr.RenderCell(v, 20, true)

		if normal == active {
			t.Errorf("RenderCell(%#v, selected) is identical to the unselected one", v)
		}
		if ansi.Strip(normal) != ansi.Strip(active) {
			t.Errorf("RenderCell(%#v) changes the text when selected:\n %q\n %q", v, ansi.Strip(normal), ansi.Strip(active))
		}
		if normal != styles.Text.PaddingLeft(1).PaddingRight(1).Width(20).Render(ansi.Strip(normal)) &&
			active == normal {
			t.Errorf("RenderCell(%#v) did not use the Selected style", v)
		}
	}
}

// Scenario: Una columna de menos de 3 celdas hace que la fila sea MÁS ANCHA que
// la suma.
//
// KNOWN BEHAVIOUR, pinned because it is a real coupling between two decisions and
// the reason is not visible at the call site.
//
// A cell is one cell of padding on each side plus the text, and the text is cut to
// width-2. So a column needs at least 3 cells to have any room for text. Below that
// `width-2` is 0 or negative, and Truncate's guard returns the value WHOLE rather
// than an empty string. The cell then has to grow to fit its own content, the row
// comes out wider than the sum, and everything to the right of it — and the box
// around it — shifts.
//
// Nothing in the grid produces such a width today; calculateWidths floors them
// higher. This is here so that if it ever did, the failure mode is on record rather
// than discovered as a mystery overflow.
func TestEveryRowRenderer_AWidthBelowThreeOverflowsTheRow(t *testing.T) {
	for _, r := range rowRenderers() {
		t.Run(r.name, func(t *testing.T) {
			for _, widths := range [][]int{{2, 2, 2, 2}, {1, 1, 1, 1}, {4, 2, 8, 2}} {
				out := r.render(cellValues, widths, 1, map[int]bool{1: true})
				got, want := lipgloss.Width(out), sumWidths(widths)
				if got <= want {
					t.Errorf("widths %v: the row is %d cells, want it WIDER than %d", widths, got, want)
				}
				// The cause: the value is shown in full, not cut.
				plain := ansi.Strip(out)
				for _, v := range []string{"1", "abc", "NULL", "true"} {
					if !strings.Contains(plain, v) {
						t.Errorf("widths %v: the value %q was cut, but this test expects it whole: %q", widths, v, plain)
					}
				}
			}
		})
	}
}

// --- the geometry the grid depends on ----------------------------------------

// Scenario: Las anchuras y el número de valores tienen que coincidir.
//
// Every renderer indexes widths[i] inside a loop over values, so a row with more
// values than widths panics. A driver that returns an extra column would take the
// whole grid down, and that is worth a test rather than a code comment.
func TestRowRenderers_MoreValuesThanWidthsPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("rendering more values than widths did not panic; the grid would corrupt instead")
		}
	}()
	cr := newCellRendererForTest()
	_ = cr.RenderRow([]interface{}{1, 2, 3}, []int{10, 10}, 0)
}

// Scenario: Cada columna recibe EXACTAMENTE el estilo que le toca.
//
// The differential test above proves the active column changes the styling and
// nothing else. It cannot prove WHICH column is the active one, because a renderer
// that highlighted the wrong index still produces a different string for every
// value of activeCol. So this builds the expected row cell by cell, naming the
// style each column must get, and compares.
//
// That is the difference between a survivor surviving and not: the five selection
// mutants left in this file all produce a string that varies with activeCol and has
// the same text, so only a direct expectation can see them.
func TestRowRenderers_EachColumnGetsExactlyItsOwnStyle(t *testing.T) {
	styles := theme.Resolve("dark").Styles()
	cr := NewCellRenderer(styles)

	// cell builds one cell the way every renderer in the file does.
	cell := func(style lipgloss.Style, text string, w int) string {
		return style.PaddingLeft(1).PaddingRight(1).Width(w).Render(text)
	}
	// text is the value as it appears in a 10-wide cell: 8 cells of room.
	text := func(v interface{}) string { return ansi.Strip(cr.Truncate(cr.FormatValue(v), 8)) }

	values := []interface{}{"a", "b", "c", "d"}
	widths := []int{10, 10, 10, 10}
	draft := map[int]bool{1: true, 2: true}
	const active = 2

	for _, tc := range []struct {
		name string
		got  string
		// want names the style of each column, in order.
		want []lipgloss.Style
	}{
		{
			"RenderRow",
			cr.RenderRow(values, widths, active),
			[]lipgloss.Style{styles.Text, styles.Text, styles.Selected, styles.Text},
		},
		{
			"RenderCursorSelectedRow",
			cr.RenderCursorSelectedRow(values, widths, active),
			[]lipgloss.Style{styles.Cursor, styles.Cursor, styles.Selected, styles.Cursor},
		},
		{
			"RenderDraftDeleteRow",
			cr.RenderDraftDeleteRow(values, widths, active),
			[]lipgloss.Style{styles.DraftDelete, styles.DraftDelete, styles.DraftDeleteSelected, styles.DraftDelete},
		},
		{
			"RenderSelectedRow",
			cr.RenderSelectedRow(values, widths, active),
			[]lipgloss.Style{styles.Cursor, styles.Cursor, styles.Cursor, styles.Cursor},
		},
		{
			"RenderPendingRow",
			cr.RenderPendingRow(values, widths, active),
			[]lipgloss.Style{styles.Pending, styles.Pending, styles.Pending, styles.Pending},
		},
		{
			"RenderDraftInsertRow",
			cr.RenderDraftInsertRow(values, widths),
			[]lipgloss.Style{styles.DraftInsert, styles.DraftInsert, styles.DraftInsert, styles.DraftInsert},
		},
		{
			// Its own style, NOT Cursor: the theme gives it Cursor's background
			// with DraftInsert's foreground, which is what distinguishes it from
			// RenderSelectedRow at a glance.
			"RenderDraftInsertSelectedRow",
			cr.RenderDraftInsertSelectedRow(values, widths),
			[]lipgloss.Style{styles.DraftInsertSelected, styles.DraftInsertSelected, styles.DraftInsertSelected, styles.DraftInsertSelected},
		},
		{
			// Four cases, and all four are reachable: a draft cell under the
			// cursor, a draft cell not under it, a clean cell under the cursor,
			// and a clean cell not under it.
			"RenderDraftUpdateRow",
			cr.RenderDraftUpdateRow(values, widths, draft, active),
			[]lipgloss.Style{styles.Text, styles.DraftUpdate, styles.DraftUpdateSelected, styles.Text},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var want strings.Builder
			for i, style := range tc.want {
				want.WriteString(cell(style, text(values[i]), widths[i]))
			}
			if tc.got != want.String() {
				t.Errorf("the row is\n%q\nwant\n%q", tc.got, want.String())
			}
		})
	}
}

// Scenario: La celda en edición se recorta a width-3, y su vecina a width-2.
//
// The editing cell reserves THREE cells, not two: the cursor block is drawn inside
// the value, so the text room is width-2 for the value and one more for the block.
// Rendering the editing cell at width-2 would push the block past the cell's
// right edge.
//
// The difference is only visible with a value too long to fit, so the fixture is
// one. A short value fits at every room size and cannot tell the two apart — and
// the first draft of this test used nil, which is why four of the six truncation
// mutants in this file survived the first run.
//
// The expected row is BUILT from the rule rather than sliced out of the output: a
// stripped row cannot be indexed by display cell, because the block character is
// three bytes wide. Two positions that look like a character apart are not.
func TestEditRowRenderers_TheEditedCellIsTruncatedOneCellShorter(t *testing.T) {
	cr := newCellRendererForTest()
	styles := theme.Resolve("dark").Styles()
	const width = 10

	// cell builds one cell the way every renderer in the file does.
	cell := func(style lipgloss.Style, text string, w int) string {
		return style.PaddingLeft(1).PaddingRight(1).Width(w).Render(text)
	}
	// room is the text room of a cell w cells wide: width-2 for an ordinary cell
	// and width-3 for the one being edited.
	room := func(w, reserved int) int { return w - reserved }
	// cut is the rule Truncate applies: spend three cells on the tail from four
	// up, cut outright below that, and leave it alone when it fits.
	cut := func(value string, cells int) string {
		switch {
		case cells <= 0:
			return value
		case cells <= 3:
			return value[:cells]
		case cells < len(value):
			return value[:cells-3] + "..."
		default:
			return value
		}
	}

	for _, tc := range []struct {
		name string
		// render is the row renderer.
		render func(vals []interface{}, widths []int, editCol int, editValue string, cursor int) string
		// otherStyle is the style a cell that is NOT being edited gets.
		otherStyle lipgloss.Style
	}{
		{"RenderEditRow", func(v []interface{}, w []int, c int, e string, p int) string {
			return cr.RenderEditRow(v, w, c, e, p)
		}, styles.Text},
		{"RenderPendingEditRow", func(v []interface{}, w []int, c int, e string, p int) string {
			return cr.RenderPendingEditRow(v, w, c, e, p)
		}, styles.Pending},
		{"RenderDraftInsertEditRow", func(v []interface{}, w []int, c int, e string, p int) string {
			return cr.RenderDraftInsertEditRow(v, w, c, e, p)
		}, styles.DraftInsert},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Two DIFFERENT long values: the edited cell and its neighbour are in
			// the same output and one value cannot tell which a match came from.
			editLong := strings.Repeat("a", 16)
			otherLong := strings.Repeat("b", 16)
			values := []interface{}{editLong, otherLong}
			widths := []int{width, width}

			// At width 10 the edited cell has 7 cells of room and the neighbour 8.
			wantEdit := cut(editLong, room(width, 3))   // "aaaa..."
			wantOther := cut(otherLong, room(width, 2)) // "bbbbb..."

			// The cursor at the end and at the start, so the block is on each side
			// of the truncated text in turn.
			for _, cursor := range []int{0, len(wantEdit)} {
				display := wantEdit[:cursor] + "█" + wantEdit[cursor:]
				want := cell(styles.Selected, display, width) +
					cell(tc.otherStyle, wantOther, width)
				if got := tc.render(values, widths, 0, editLong, cursor); got != want {
					t.Errorf("cursor %d: the row is\n%q\nwant\n%q", cursor, got, want)
				}
			}

			// A value that fits in the edited cell's room is untouched by either
			// room size, and the block lands where it was asked to.
			for _, v := range []string{"a", "ab", "abcd", "abcde", "abcdefg"} {
				for _, cursor := range []int{0, 1, len(v)} {
					display := v[:cursor] + "█" + v[cursor:]
					want := cell(styles.Selected, display, width) +
						cell(tc.otherStyle, v, width)
					if got := tc.render([]interface{}{v, v}, widths, 0, v, cursor); got != want {
						t.Errorf("value %q cursor %d: the row is\n%q\nwant\n%q", v, cursor, got, want)
					}
				}
			}
		})
	}
}
