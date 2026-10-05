package grid

// Scenario: Las aritmeticas puras del grid, que es donde una mutacion se cuela sin que
// nadie lo note.
//
// Todo lo que hay aqui son funciones sin entrada de usuario: cuantas filas le quedan a
// esta pagina, donde cae el cursor dentro de un texto multibyte, cuanto relleno hay que
// pegar antes del indicador de modo. Ninguna necesita una base de datos y ninguna necesita
// un reloj, asi que se pueden barrer enteras en vez de probarse con un caso.
//
// Lo que las une es que las tres son RESTAS con un clamp, y las tres existen porque un
// indice de slice negativo es un panic. La de las filas era ademas tres copias del mismo
// calculo que se habian separado: dos se desreferenciaban g.data sin guarda y la tercera
// si. Ver TestTheRowCountOfThePageIsOneDecision.

import (
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/buble/dbx/internal/drivers/postgres"
)

// gridOnPage is a grid with nRows rows sitting on the given 1-based page of a
// pageSize-row page.
func gridOnPage(t *testing.T, nRows, pageSize, page int) *Grid {
	t.Helper()
	g := newGrid(t, nRows, 3, 80, 20, pageSize)
	if page > 1 {
		g.pager.GoToPage(page)
	}
	return g
}

func TestTheRowCountOfThePageIsOneDecision(t *testing.T) {
	t.Run("rowsAfterOffset is what the rest of the page holds", func(t *testing.T) {
		for _, tc := range []struct {
			nRows, pageSize, page, want int
		}{
			{100, 10, 1, 100}, // page one sees everything loaded
			{100, 10, 2, 90},  // the offset eats the first ten
			{100, 10, 10, 10}, // the last page is the remainder, not a full page
			{100, 25, 4, 25},
			{7, 10, 1, 7},   // a short final page
			{1, 10, 1, 1},   // a single row
			{0, 10, 1, 0},   // nothing loaded
			{10, 10, 1, 10}, // exactly one full page
		} {
			g := gridOnPage(t, tc.nRows, tc.pageSize, tc.page)
			if got := g.rowsAfterOffset(); got != tc.want {
				t.Errorf("with %d rows on page %d of %d the page holds %d, want %d",
					tc.nRows, tc.page, tc.pageSize, got, tc.want)
			}
		}
	})

	t.Run("an offset PAST the data clamps to zero instead of going negative", func(t *testing.T) {
		// This is not a hypothetical. A delete or a filter that shrinks the result while
		// the user sits on page 3 leaves the pager's offset pointing past the new end. An
		// unclamped negative would make EVERY `absRow >= remaining` true at once, so
		// every row would read as a pending insert.
		g := newGrid(t, 10, 3, 80, 20, 5)
		g.pager.GoToPage(3)                               // offset 10, and there are exactly 10 rows
		g.SetData(&postgres.QueryResult{}, "public", "t") // and now there are none

		if got := g.rowsAfterOffset(); got != 0 {
			t.Errorf("an offset past an emptied result reads %d rows, want 0", got)
		}
		// And the two callers built on it must not panic and must not invent rows.
		if got := g.cursorRowType(); got != "insert" {
			t.Errorf("with no rows every row is a pending insert, got %q", got)
		}
		if got := g.pendingInsertIndex(); got != -1 {
			t.Errorf("pendingInsertIndex with no pending rows is %d, want -1", got)
		}
	})

	t.Run("no data at all is zero, not a panic", func(t *testing.T) {
		// The guard the two drifted copies were missing. cursorRowType() and
		// pendingInsertIndex() dereferenced g.data with no nil check while
		// visibleRows() had one — so the same question had two different answers
		// depending on which of the three you asked.
		g := newGrid(t, 0, 0, 80, 20, 10)
		g.data = nil

		for name, call := range map[string]func() int{
			"rowsAfterOffset": g.rowsAfterOffset,
			"visibleRows":     g.visibleRows,
		} {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("%s() on a grid with no data panicked: %v", name, r)
					}
				}()
				if got := call(); got != 0 {
					t.Errorf("%s() = %d, want 0", name, got)
				}
			}()
		}
		// pendingInsertIndex answers with a SENTINEL rather than a count, so its
		// contract is the sentinel and the absence of a panic.
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("pendingInsertIndex() on a grid with no data panicked: %v", r)
				}
			}()
			if got := g.pendingInsertIndex(); got != -1 {
				t.Errorf("pendingInsertIndex() = %d, want -1", got)
			}
		}()
		if got := g.cursorRowType(); got != "insert" {
			t.Errorf("cursorRowType with no data is %q, want %q", got, "insert")
		}
	})

	t.Run("visibleRows counts the pending rows too", func(t *testing.T) {
		// visibleRows is rowsAfterOffset PLUS the pending inserts, capped at the page
		// limit. Three things in one function, so all three are pinned: the sum, the
		// cap, and the early return when there is nothing at all.
		g := newGrid(t, 10, 3, 80, 20, 10)
		if got := g.visibleRows(); got != 10 {
			t.Errorf("a full page of ten is %d, want 10", got)
		}

		g = newGrid(t, 3, 3, 80, 20, 10)
		g.pendingRows = make([][]interface{}, 4)
		for i := range g.pendingRows {
			g.pendingRows[i] = make([]interface{}, 3)
		}
		if got := g.visibleRows(); got != 7 {
			t.Errorf("three real rows and four pending make %d, want 7", got)
		}

		// Pending alone, with no real rows: the early return needs BOTH to be empty, so
		// pending rows alone must still be counted.
		g = newGrid(t, 0, 3, 80, 20, 10)
		g.pendingRows = make([][]interface{}, 2)
		for i := range g.pendingRows {
			g.pendingRows[i] = make([]interface{}, 3)
		}
		if got := g.visibleRows(); got != 2 {
			t.Errorf("two pending rows and nothing real make %d, want 2", got)
		}

		// And the cap: ten pending rows on a page that holds five is five, not ten.
		g = newGrid(t, 0, 3, 80, 20, 5)
		g.pendingRows = make([][]interface{}, 10)
		for i := range g.pendingRows {
			g.pendingRows[i] = make([]interface{}, 3)
		}
		if got := g.visibleRows(); got != 5 {
			t.Errorf("ten pending rows on a five-row page make %d, want the page's 5", got)
		}
	})
}

// ---------------------------------------------------------------------------
// the cell editor indexes by BYTE
// ---------------------------------------------------------------------------

// runeAt and runeAtBefore exist because the cell editor indexes by byte. That is what
// makes inserting a rune a two-slice concatenation, and it is also a trap: every step that
// moves the cursor has to move by a WHOLE rune or it lands in the middle of a
// multi-byte character, and decoding from a byte offset in the middle of one yields
// U+FFFD instead of the character.
func TestTheCellEditorReadsWholeRunesAndNeverTheMiddleOfOne(t *testing.T) {
	// "añó" is 5 bytes and 3 runes: a is one, ñ (U+00F1) and ó (U+00F3) are two each.
	// "🎉" (U+1F389) is 4 bytes and 1 rune. Nine bytes in total. Chosen because a
	// byte-stepping implementation fails on the two-byte and the four-byte rune alike,
	// and because the two-byte ones are the ones people get wrong: both accented letters
	// here are TWO bytes, and an expectation that calls ñ three has the arithmetic wrong
	// rather than the code.
	const text = "añó🎉"

	t.Run("runeAt reads the rune that STARTS at each byte offset", func(t *testing.T) {
		for i := 0; i < len(text); {
			want, size := utf8.DecodeRuneInString(text[i:])
			got := runeAt(text, i)
			if got != want {
				t.Errorf("runeAt(%q, %d) = %q, want %q", text, i, got, want)
			}
			i += size
		}
	})

	t.Run("runeAt returns the replacement char at or past the end", func(t *testing.T) {
		for _, i := range []int{-1, -100, len(text), len(text) + 5} {
			if got := runeAt(text, i); got != utf8.RuneError {
				t.Errorf("runeAt(%q, %d) = %q, want U+FFFD", text, i, got)
			}
		}
		// And at zero it must NOT be the replacement char — that is what distinguishes
		// "out of range" from "the text genuinely starts with a replacement char".
		if got := runeAt(text, 0); got == utf8.RuneError {
			t.Error("runeAt(text, 0) is U+FFFD for text that does not start with one")
		}
	})

	t.Run("runeAtBefore reads the rune that ENDS at each byte offset", func(t *testing.T) {
		for i := len(text); i > 0; {
			want, size := utf8.DecodeLastRuneInString(text[:i])
			got := runeAtBefore(text, i)
			if got != want {
				t.Errorf("runeAtBefore(%q, %d) = %q, want %q", text, i, got, want)
			}
			i -= size
		}
	})

	t.Run("runeAtBefore returns the replacement char at or before the start", func(t *testing.T) {
		for _, i := range []int{0, -1, -100} {
			if got := runeAtBefore(text, i); got != utf8.RuneError {
				t.Errorf("runeAtBefore(%q, %d) = %q, want U+FFFD", text, i, got)
			}
		}
		if got := runeAtBefore(text, 1); got == utf8.RuneError {
			t.Error("runeAtBefore(text, 1) is U+FFFD for text whose first byte is not one")
		}
	})

	t.Run("backspace and delete move by whole runes, never by bytes", func(t *testing.T) {
		// The failure this pair exists to prevent: backspacing one byte at a time through
		// an emoji leaves three stray bytes, which then render as three replacement
		// chars — in the cell content the user is about to commit.
		const name = "edit_cell"

		start := func() *Grid {
			g := newGrid(t, 1, 1, 40, 10, 10)
			g.SetData(&postgres.QueryResult{
				Columns: []postgres.ColumnInfo{{Name: "c0", DataType: "text"}},
				Rows:    [][]interface{}{{text}},
				Count:   1,
			}, "public", "t")
			if _, handled := g.HandleAction(name); !handled {
				t.Fatalf("%s was not handled", name)
			}
			if !g.editing {
				t.Fatalf("%s did not start an edit", name)
			}
			if g.editValue != text {
				t.Fatalf("the edit opened on %q, want %q", g.editValue, text)
			}
			return g
		}

		for _, tc := range []struct {
			name   string
			keys   []tea.KeyPressMsg
			want   string
			cursor int
		}{
			{
				name:   "backspace over the four-byte emoji",
				keys:   []tea.KeyPressMsg{{Code: tea.KeyBackspace}},
				want:   "añó",
				cursor: len("añó"),
			},
			{
				// ó and ñ are both TWO bytes (U+00F3, U+00F1); the emoji is four.
				// The first version of this case said "backspace twice leaves a" and
				// reported a mismatch on a correct implementation — it had ñ down as a
				// three-byte rune, which it is not.
				name:   "backspace twice, over the emoji and then the accented o",
				keys:   []tea.KeyPressMsg{{Code: tea.KeyBackspace}, {Code: tea.KeyBackspace}},
				want:   "añ",
				cursor: len("añ"),
			},
			{
				name:   "delete forward from the start removes the plain a",
				keys:   []tea.KeyPressMsg{{Code: tea.KeyHome}, {Code: tea.KeyDelete}},
				want:   "ñó🎉",
				cursor: 0,
			},
			{
				name:   "left arrow steps back over a WHOLE rune",
				keys:   []tea.KeyPressMsg{{Code: tea.KeyLeft}},
				want:   text,
				cursor: len("añó🎉") - 4,
			},
			{
				name:   "right arrow steps forward over a WHOLE rune",
				keys:   []tea.KeyPressMsg{{Code: tea.KeyHome}, {Code: tea.KeyRight}},
				want:   text,
				cursor: len("a"),
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				g := start()
				for _, k := range tc.keys {
					g.handleEditKey(k)
				}
				if g.editValue != tc.want {
					t.Errorf("the value is %q, want %q", g.editValue, tc.want)
				}
				if strings.ContainsRune(g.editValue, utf8.RuneError) {
					t.Errorf("the value holds a replacement character: %q", g.editValue)
				}
				if g.editCursor != tc.cursor {
					t.Errorf("the cursor is at byte %d, want %d", g.editCursor, tc.cursor)
				}
				// The cursor has to stay on a rune boundary or every subsequent
				// splice cuts a character in half — which is how a stray U+FFFD gets
				// typed. len(g.editValue) is itself a boundary (the end of the string),
				// so only the strictly-inside case needs checking.
				if c := g.editCursor; c > 0 && c < len(g.editValue) && !utf8.RuneStart(g.editValue[c]) {
					t.Errorf("the cursor at byte %d is inside a rune of %q", c, g.editValue)
				}
			})
		}

		t.Run("backspace at the start and delete at the end do nothing", func(t *testing.T) {
			// Both guards are `editCursor > 0` and `editCursor < len(...)`, and a naive
			// -w on a zero cursor would slice at a negative index.
			g := start()
			g.editCursor = 0
			g.handleEditKey(tea.KeyPressMsg{Code: tea.KeyBackspace})
			if g.editValue != text {
				t.Errorf("backspace at the start changed the value to %q", g.editValue)
			}

			g.editCursor = len(text)
			g.handleEditKey(tea.KeyPressMsg{Code: tea.KeyDelete})
			if g.editValue != text {
				t.Errorf("delete at the end changed the value to %q", g.editValue)
			}
		})
	})
}

// ---------------------------------------------------------------------------
// the bottom padding in front of the mode indicator
// ---------------------------------------------------------------------------

// The mode indicator has to be glued to the BOTTOM border, and the bordered box pads its
// content to height-2 lines — so any padding it added would land BELOW the indicator. The
// padding is therefore done by hand, above the indicator, as `gap+1` newlines where gap is
// how many blank lines are missing. Two clamps guard it, and both exist because the
// subtraction can go negative.
func TestTheModeIndicatorIsPaddedToTheBottomOfTheBox(t *testing.T) {
	rows := func(render string) []string { return strings.Split(render, "\n") }

	t.Run("a short body is padded so the indicator lands on the last content line", func(t *testing.T) {
		// Grid 80x14: contentHeight is 14-2-something, and with two rows the body is far
		// shorter. The indicator must end up immediately above the bottom border, i.e. at
		// index len(rows)-3 of a render whose last line is the border's bottom edge.
		g := newGrid(t, 2, 3, 80, 14, 10)
		out := rows(g.View())
		if len(out) <= 2 {
			t.Fatalf("the render has %d lines, which is too few to hold an indicator", len(out))
		}
		indicator := strings.TrimRight(out[len(out)-3], "│ ")
		if indicator == "" {
			t.Errorf("the line above the bottom border is blank:\n%q", out[len(out)-3])
		}
	})

	t.Run("a body TALLER than the box does not pad at all", func(t *testing.T) {
		// gap = target - bodyLines - 1 goes negative here, and the clamp is what stops a
		// negative repeat count. strings.Repeat with a negative count PANICS, so this is
		// the case the clamp is for.
		g := newGrid(t, 60, 6, 80, 10, 100)
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("rendering an overfull grid panicked: %v", r)
				}
			}()
			out := g.View()
			if len(rows(out)) == 0 {
				t.Error("the overfull grid rendered nothing")
			}
		}()
	})

	t.Run("a box one row tall still renders an indicator", func(t *testing.T) {
		// target = height-2, and the clamp floors it at 1. Below that the repeat would be
		// negative.
		for _, h := range []int{1, 2, 3, 4, 5} {
			g := newGrid(t, 3, 2, 40, h, 10)
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("a %d-row grid panicked on render: %v", h, r)
					}
				}()
				_ = g.View()
			}()
		}
	})
}

// ---------------------------------------------------------------------------
// the scroll window
// ---------------------------------------------------------------------------

// clampCursor's whole job: after a cursor movement, is the cursor still on a row that
// exists? The `totalRows - ch` line is the interesting one — it puts the cursor on the
// LAST row rather than wherever the arithmetic lands, and the two clamps after it exist
// because a window taller than the data gives a negative row.
//
// Its early return is the case with no observable otherwise: zero rows means the cursor
// has nowhere to be, and both fields have to be ZERO rather than keeping a stale position
// from before the data went away.
func TestClampingTheCursorNeverLeavesItOnARowThatDoesNotExist(t *testing.T) {
	t.Run("no rows puts both the cursor and the window at zero", func(t *testing.T) {
		// Without the early return, the arithmetic below runs with totalRows zero:
		// scrollRow = 0 - ch (clamped to 0) and cursorRow = 0 - 0 - 1 = -1, which then
		// needs the SECOND clamp. The early return is what makes that unreachable.
		for _, nRows := range []int{0, 1} {
			g := newGrid(t, nRows, 3, 80, 10, 10)
			if nRows == 0 {
				g.SetData(&postgres.QueryResult{
					Columns: []postgres.ColumnInfo{{Name: "c0", DataType: "text"}},
					Rows:    nil,
					Count:   0,
				}, "public", "t")
			}
			g.cursorRow = 7
			g.scrollRow = 3
			g.clampCursor()

			if g.visibleRows() == 0 {
				if g.cursorRow != 0 || g.scrollRow != 0 {
					t.Errorf("with %d rows the cursor is at %d in a window starting at %d, want both 0",
						nRows, g.cursorRow, g.scrollRow)
				}
			}
		}
	})

	t.Run("a cursor past the end lands on the LAST row, not on a clamp", func(t *testing.T) {
		// The interesting line. If scrollRow = totalRows - ch is right, the cursor ends up
		// on totalRows-1 with the window ending exactly at the data. If the clamp fires
		// instead, the cursor is on row 0 of a window at the end — a different, wrong
		// answer that looks fine on screen.
		g := newGrid(t, 100, 3, 80, 20, 100)
		g.cursorRow = 99
		g.scrollRow = 0
		g.clampCursor()

		total := g.visibleRows()
		if abs := g.scrollRow + g.cursorRow; abs != total-1 {
			t.Errorf("the cursor settled on absolute row %d of %d, want the last (%d)",
				abs, total, total-1)
		}
	})

	t.Run("a window TALLER than the data still leaves the cursor on a real row", func(t *testing.T) {
		// scrollRow = totalRows - ch is negative here, which is why the first clamp
		// exists. Without it, a negative window offset makes scrollRow + cursorRow
		// negative and the rendered row lookup indexes before the slice.
		for _, tc := range []struct{ nRows, height int }{
			{3, 30}, {5, 10}, {2, 8}, {1, 6}, {1, 3}, {4, 5},
		} {
			g := newGrid(t, tc.nRows, 3, 80, tc.height, 100)
			g.cursorRow = tc.nRows + 5
			g.scrollRow = 0

			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("%d rows in a %d-row window: panicked: %v",
							tc.nRows, tc.height, r)
					}
				}()
				g.clampCursor()

				if g.scrollRow < 0 || g.cursorRow < 0 {
					t.Errorf("%d rows in a %d-row window: scrollRow=%d cursorRow=%d",
						tc.nRows, tc.height, g.scrollRow, g.cursorRow)
				}
				total := g.visibleRows()
				if total == 0 {
					return
				}
				abs := g.scrollRow + g.cursorRow
				if abs < 0 || abs >= total {
					t.Errorf("%d rows in a %d-row window: the absolute row is %d, want 0..%d",
						tc.nRows, tc.height, abs, total-1)
				}
			}()
		}
	})

	t.Run("a cursor already inside the data is left alone", func(t *testing.T) {
		// The `if` is a no-op when it does not fire, so the observable is that the
		// position is EXACTLY what it was — not merely a valid one. A clamp that fired
		// anyway would be invisible in most of the matrix and would move the cursor on
		// every redraw.
		g := newGrid(t, 100, 3, 80, 20, 100)
		g.cursorRow = 7
		g.scrollRow = 0
		g.clampCursor()
		if g.cursorRow != 7 || g.scrollRow != 0 {
			t.Errorf("clampCursor moved a valid cursor to row %d in a window at %d", g.cursorRow, g.scrollRow)
		}
	})
}
