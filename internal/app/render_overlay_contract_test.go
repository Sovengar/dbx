package app

// Scenario: El popup del autocompletado se dibuja SIN tapar lo que el usuario escribe.
//
// renderEditor has to divide the pane between the editor and the popup that hangs off it,
// and every one of those divisions is arithmetic with a clamp somewhere in it. A popup that
// is one row too tall pushes the text the user is typing down by a row on every keystroke;
// a popup that is capped at the wrong number shows fewer suggestions than the arrow keys
// can reach.
//
// The overlay helpers are the other half: they paste a box onto a frame, and both clamps —
// x < 0 and y < 0 — exist because a box bigger than the frame has a negative offset, and a
// negative offset in a string slice is a panic rather than a cosmetic problem.
//
// None of this needs a database. What it needs is a model whose editor has a popup open,
// which is a state the keyboard produces.

import (
	"fmt"
	"strings"
	"testing"

	aiContext "github.com/buble/dbx/internal/ai/context"
	"github.com/buble/dbx/internal/config"

	tea "charm.land/bubbletea/v2"
)

// wideSchema is a table with thirty columns, so the popup has more suggestions than the
// cap allows. Thirty is comfortably over fifteen; the cap's job is exactly this case.
func wideSchema() *aiContext.SchemaExport {
	cols := make([]aiContext.ColumnInfo, 30)
	for i := range cols {
		cols[i] = aiContext.ColumnInfo{Name: fmt.Sprintf("column_%02d", i), DataType: "text"}
	}
	return &aiContext.SchemaExport{Schemas: []aiContext.SchemaInfo{{
		Name:   "public",
		Tables: []aiContext.TableInfo{{Name: "t", Columns: cols}},
	}}}
}

// editorWithPopup is a model with the editor focused and its popup open, carrying
// suggestions from the given schema.
//
// The popup count is NOT controllable from this package: the editor owns its autocomplete
// state and exposes no setter and no selection methods, so the only way to have N
// suggestions is to build a schema with N columns and let the keyboard open the popup. The
// first version of this test tried to set the count and discovered there is no seam.
// editorWithAutocomplete is routerModelLoaded with the autocomplete turned ON.
//
// routerModel builds from &config.Config{}, and the editor reads `editor.autocomplete` from
// it — so the shared harness has the popup DISABLED, and the third version of this test
// typed a statement, moved the cursor, sent ctrl+space and reported that the popup would
// not open. It was right to: the key handler cancels the autocomplete and returns before
// looking at anything.
func editorWithAutocomplete(t *testing.T) Model {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	cfg := &config.Config{}
	cfg.Editor.Autocomplete = true
	cfg.Editor.AutocompleteTrigger = 2
	m := NewModel(cfg)
	m.state = StateMain
	return resize(m, 120, 40)
}

func editorWithPopup(t *testing.T, schema *aiContext.SchemaExport, typed string) Model {
	t.Helper()
	m := editorWithAutocomplete(t)
	m.editorOpen = true
	m.editor.Focus()
	m.editor.SetSchema(schema)
	m.editor.SetContent(typed)
	// The cursor to the END of what was typed. SetContent leaves it at zero, and the
	// context is detected at the CURSOR — so at position 0 the line reads as an empty
	// statement and there is nothing to suggest. The first version of this test typed a
	// statement and asked for suggestions without moving the cursor, and reported that
	// the popup would not open.
	m.editor.SetCursorPos(0, len([]rune(typed)))

	// ctrl+space is what the editor's own Update handles to trigger the popup. Tab does
	// NOT: it is bound to the `autocomplete` action in the registry, which ACCEPTS the
	// current selection rather than opening the list. The first version of this test sent
	// Tab and reported that the popup would not open — correctly, since it had asked for
	// the wrong thing.
	m.editor.Update(tea.KeyPressMsg{Code: ' ', Mod: tea.ModCtrl})
	return m
}

func TestThePopupOpensFromTheKeyboard(t *testing.T) {
	// The premise of every case below, asserted so a change to the popup's trigger shows
	// up here rather than as five skips.
	m := editorWithPopup(t, wideSchema(), "SELECT ")
	if !m.editor.AutocompleteVisible() {
		t.Fatal("ctrl+space on a half-typed statement did not open the popup")
	}
	// Not 30: the list also holds the keywords and the functions, so thirty columns
	// produce a list comfortably longer than the fifteen-row cap — which is what the cap
	// case below needs.
	if got := m.editor.AutocompleteItemCount(); got <= 15 {
		t.Errorf("the popup has %d items, want more than the cap of fifteen", got)
	}
}

func TestTheAutocompletePopupGetsItsOwnRows(t *testing.T) {
	t.Run("no popup means the editor gets the whole pane", func(t *testing.T) {
		m := editorWithAutocomplete(t)
		m.editorOpen = true
		m.editor.Focus()
		m.editor.SetContent("SELECT 1")
		if m.editor.AutocompleteVisible() {
			t.Skip("a complete statement still shows a popup; this case needs one that does not")
		}

		rendered := m.renderEditor(80, 24)
		lines := strings.Count(rendered, "\n") + 1
		if lines > 24 {
			t.Errorf("the editor rendered %d rows into a 24-row pane", lines)
		}
		if !strings.Contains(stripANSI(rendered), "SQL Editor") {
			t.Errorf("the pane does not carry its title:\n%s", rendered)
		}
	})

	t.Run("a popup never makes the pane taller than it was", func(t *testing.T) {
		// The whole point of subtracting the popup's rows: the editor's height is
		// h - 4 - popupLines, and if that arithmetic is wrong the pane grows and the layout
		// below it moves on every keystroke.
		m := editorWithPopup(t, wideSchema(), "SELECT ")
		if !m.editor.AutocompleteVisible() {
			t.Skip("no popup")
		}
		for _, size := range [][2]int{{80, 24}, {100, 40}, {60, 20}, {40, 12}} {
			rendered := m.renderEditor(size[0], size[1])
			lines := strings.Count(rendered, "\n") + 1
			if lines > size[1] {
				t.Errorf("at %dx%d the editor with a popup rendered %d rows", size[0], size[1], lines)
			}
		}
	})

	t.Run("the popup is CAPPED at fifteen rows however many suggestions there are", func(t *testing.T) {
		// Thirty suggestions and the cap is fifteen. The observable is the pane height:
		// a popup that used all thirty would take thirty-two rows off a twenty-four-row
		// pane and the editor would get a negative height.
		const width, height = 80, 24

		full := editorWithPopup(t, wideSchema(), "SELECT ")
		if !full.editor.AutocompleteVisible() {
			t.Skip("no popup")
		}
		count := full.editor.AutocompleteItemCount()
		if count <= 15 {
			t.Fatalf("the fixture produced %d suggestions, which does not exceed the cap", count)
		}

		rendered := strings.Count(stripANSI(full.renderEditor(width, height)), "\n") + 1
		if rendered > height {
			t.Errorf("%d suggestions rendered %d rows into a %d-row pane; the cap is not holding",
				count, rendered, height)
		}
	})

	t.Run("a pane too short for the popup does not produce a negative editor height", func(t *testing.T) {
		// The subtraction is h - 4 - popupLines, and popupLines is capped at 17, so a
		// twenty-row pane leaves the editor zero and a twelve-row pane leaves it
		// NEGATIVE — which is what makes a scroll computation wrap.
		m := editorWithPopup(t, wideSchema(), "SELECT ")
		if !m.editor.AutocompleteVisible() {
			t.Skip("no popup")
		}
		for _, height := range []int{4, 6, 10, 12, 20, 21, 24, 40} {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("renderEditor panicked at height %d: %v", height, r)
					}
				}()
				out := m.renderEditor(80, height)
				if lines := strings.Count(out, "\n") + 1; lines > height {
					t.Errorf("at height %d the render is %d lines", height, lines)
				}
			}()
		}
	})

	t.Run("the title says when running the query will commit", func(t *testing.T) {
		// Commit-on-run is the difference between running a SELECT and applying a
		// transaction, and it is invisible in the buffer. The title is the only place it
		// is stated, so it has to be there.
		m := editorWithPopup(t, nil, "SELECT 1")
		m.editor.CancelAutocomplete()
		if got := stripANSI(m.renderEditor(80, 24)); strings.Contains(got, "commit on run") {
			t.Error("the title mentions commit-on-run with nothing pending")
		}

		m.editor.SetCommitOnRun(true)
		if got := stripANSI(m.renderEditor(80, 24)); !strings.Contains(got, "commit on run") {
			t.Errorf("the title does not mention commit-on-run:\n%s", got)
		}
	})
}

// ---------------------------------------------------------------------------
// the overlay helpers
// ---------------------------------------------------------------------------

// overlayBox is a box of `rows` lines, each exactly `width` columns wide.
func overlayBox(width, rows int) string {
	var lines []string
	for range rows {
		lines = append(lines, strings.Repeat("x", width))
	}
	return strings.Join(lines, "\n")
}

// baseFrame is a frame of `height` lines, each exactly `width` columns of "." so a replaced
// region is visible.
func baseFrame(width, height int) string {
	var lines []string
	for range height {
		lines = append(lines, strings.Repeat(".", width))
	}
	return strings.Join(lines, "\n")
}

func TestOverlayCentresTheBoxAndClampsIt(t *testing.T) {
	t.Run("a box smaller than the frame is centred", func(t *testing.T) {
		out := overlay(baseFrame(21, 11), overlayBox(5, 3), 21, 11)
		lines := strings.Split(out, "\n")

		if len(lines) != 11 {
			t.Fatalf("the frame has %d lines, want 11", len(lines))
		}
		// x = (21-5)/2 = 8, y = (11-3)/2 = 4.
		if idx := strings.Index(lines[4], "x"); idx != 8 {
			t.Errorf("the box starts at column %d on line 4, want 8", idx)
		}
		for _, i := range []int{4, 5, 6} {
			if !strings.Contains(lines[i], "xxxxx") {
				t.Errorf("line %d has no box in it: %q", i, lines[i])
			}
		}
		// And the rest of the frame survives.
		if lines[0] != strings.Repeat(".", 21) {
			t.Errorf("the first line was damaged: %q", lines[0])
		}
	})

	t.Run("a box TALLER than the frame is clipped, not centred off it", func(t *testing.T) {
		// bh is clamped to height. Without the clamp, y = (11-20)/2 = -4 and writing at
		// y+j starts at -4, which is a negative index into the slice — a panic, not a
		// cosmetic problem.
		out := overlay(baseFrame(21, 11), overlayBox(5, 20), 21, 11)
		lines := strings.Split(out, "\n")

		if len(lines) != 11 {
			t.Fatalf("the frame has %d lines, want 11 — the box leaked past it", len(lines))
		}
		for i, line := range lines {
			if len(line) != 21 {
				t.Errorf("line %d is %d columns, want 21", i, len(line))
			}
		}
	})

	t.Run("a box WIDER than the frame OVERFLOWS it, which is the documented limit", func(t *testing.T) {
		// Recorded rather than fixed, and the reason is worth stating.
		//
		// The clamps cover the OFFSET, not the SIZE. x = (21-40)/2 = -9 clamps to 0, and
		// then the line becomes Truncate(line, 0) + a 40-column box + TruncateLeft(line, 40)
		// — which is 40 columns wide. In a terminal a line wider than the window WRAPS, so
		// every column to the right of the frame shifts down by one row and the layout
		// breaks in a way that looks like a rendering fault somewhere else entirely.
		//
		// Every caller passes a box that fits by construction (the help modal, the palette,
		// the export picker are all sized from the window), so this is latent rather than
		// live. Fixing it means deciding whether an oversized box is truncated or dropped,
		// and neither is obviously right — so the limit is asserted and the callers who
		// would trip it are named.
		out := overlay(baseFrame(21, 11), overlayBox(40, 3), 21, 11)
		lines := strings.Split(out, "\n")
		for i, line := range lines {
			if len(line) == 40 {
				continue // an overlaid row, and exactly the overflow being recorded
			}
			if len(line) != 21 {
				t.Errorf("line %d is %d columns, want the frame's 21", i, len(line))
			}
		}
		if len(lines[4]) != 40 {
			t.Errorf("line 4 is %d columns; if the box is now truncated this case is stale", len(lines[4]))
		}
	})

	t.Run("an EMPTY box leaves the frame alone", func(t *testing.T) {
		// bh = len([""]) = 1, and the block is "". So one line gets replaced by nothing
		// plus a truncation — which can shorten that line. Asserted so the shape is known
		// rather than assumed: a caller that overlays an empty string has a frame with a
		// hole in it, and that is a decision rather than a crash.
		out := overlay(baseFrame(21, 11), "", 21, 11)
		lines := strings.Split(out, "\n")
		if len(lines) != 11 {
			t.Fatalf("the frame has %d lines, want 11", len(lines))
		}
	})

	t.Run("a base with FEWER lines than the box grows to fit it", func(t *testing.T) {
		// The frame is padded with blanks first. Without that, writing at y+j would index
		// past the end of the slice on a model whose render returned fewer lines than the
		// window claims — which is exactly what happens when a component renders nothing.
		out := overlay("short", overlayBox(5, 6), 21, 11)
		lines := strings.Split(out, "\n")
		if len(lines) < 6 {
			t.Errorf("the result has %d lines, want room for the six of the box", len(lines))
		}
	})
}

func TestOverlayBottomRightPinsTheBoxToTheCornerAndClampsIt(t *testing.T) {
	t.Run("the box sits at the bottom right, above a margin", func(t *testing.T) {
		// y = height - bh - 7 - stackOffset*(bh+1). The 7 is the margin for the status bar
		// and the keybind pane; stackOffset is what makes a SECOND toast stack above the
		// first rather than exactly on top of it.
		const w, h, bw, bh = 60, 30, 20, 5
		out := overlayBottomRight(baseFrame(w, h), overlayBox(bw, bh), w, h, 0)
		lines := strings.Split(out, "\n")

		if len(lines) != h {
			t.Fatalf("the frame has %d lines, want %d", len(lines), h)
		}
		x := w - bw - 1
		y := h - bh - 7
		if idx := strings.Index(lines[y], "x"); idx != x {
			t.Errorf("the box starts at column %d, want %d", idx, x)
		}
		if !strings.Contains(lines[y+bh-1], strings.Repeat("x", bw)) {
			t.Errorf("the last row of the box is not there: %q", lines[y+bh-1])
		}
		// The margin is real: the last seven rows are untouched.
		for i := y + bh; i < h; i++ {
			if lines[i] != strings.Repeat(".", w) {
				t.Errorf("line %d is inside the margin: %q", i, lines[i])
			}
		}
	})

	t.Run("a stack offset moves the box UP", func(t *testing.T) {
		const w, h, bw, bh = 60, 30, 20, 5
		single := strings.Split(overlayBottomRight(baseFrame(w, h), overlayBox(bw, bh), w, h, 0), "\n")
		double := strings.Split(overlayBottomRight(baseFrame(w, h), overlayBox(bw, bh), w, h, 1), "\n")

		at := func(lines []string) int {
			for i, l := range lines {
				if strings.Contains(l, strings.Repeat("x", bw)) {
					return i
				}
			}
			return -1
		}
		first, second := at(single), at(double)
		if first < 0 || second < 0 {
			t.Fatalf("the box was not found (single %d, double %d)", first, second)
		}
		if second >= first {
			t.Errorf("with a stack offset the box is on line %d, want it ABOVE the %d of the first one", second, first)
		}
		if second != first-(bh+1) {
			t.Errorf("the box moved from line %d to %d, want exactly %d rows", first, second, bh+1)
		}
	})

	t.Run("a box too tall for the margin is pushed to the top, not off it", func(t *testing.T) {
		// y = 12 - 5 - 7 = 0 exactly; 12 - 8 - 7 = -3, which must clamp to 0. A negative y
		// writes at a negative index.
		const w, h, bw, bh = 60, 12, 20, 8
		out := overlayBottomRight(baseFrame(w, h), overlayBox(bw, bh), w, h, 0)
		lines := strings.Split(out, "\n")
		for i, line := range lines {
			if len(line) != w {
				t.Errorf("line %d is %d columns, want %d", i, len(line), w)
			}
		}
		if !strings.Contains(lines[0], strings.Repeat("x", bw)) {
			t.Errorf("the box is not on the first line:\n%q", lines[0])
		}
	})

	t.Run("a box wider than the frame overflows it, the same limit as overlay", func(t *testing.T) {
		// Same shape as the centred case and the same decision: the clamp is on the
		// offset, not the size. Asserted in both so the two cannot diverge.
		const w, h = 20, 20
		lines := strings.Split(overlayBottomRight(baseFrame(w, h), overlayBox(40, 3), w, h, 0), "\n")
		overlaid := 0
		for i, line := range lines {
			if len(line) == 40 {
				overlaid++
				continue
			}
			if len(line) != w {
				t.Errorf("line %d is %d columns, want %d", i, len(line), w)
			}
		}
		if overlaid != 3 {
			t.Errorf("%d rows overflowed, want the three of the box", overlaid)
		}
	})
}

func TestAbs(t *testing.T) {
	for _, tc := range []struct{ in, want int }{
		{0, 0}, {5, 5}, {-5, 5}, {-2147483648, 2147483648},
	} {
		if got := abs(tc.in); got != tc.want {
			t.Errorf("abs(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
	// The MinInt case is not a curiosity. On a 64-bit int, abs(math.MinInt) cannot return a
	// positive number — -MinInt overflows back to MinInt — so this function is wrong for
	// exactly one input, and it is the input that appears when two coordinates are compared
	// and one of them is at the far edge of the address space. That is not reachable from a
	// terminal, so it is pinned as a limit rather than fixed: the fix would be to widen the
	// return type, and there is no caller that needs it.
	if got := abs(-1 << 63); got >= 0 {
		t.Logf("abs(math.MinInt) = %d on this platform, so the negation did not overflow", got)
	}
}
