package grid

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/buble/dbx/internal/theme"
	"github.com/charmbracelet/x/ansi"
)

func newExportPickerForTest() *ExportPicker {
	return NewExportPicker(theme.Resolve("dark").Styles())
}

// pickerKeys are the two aliases each direction accepts. They must behave
// identically: "j" is what most people press, the arrows are what a terminal
// user reaches for, and a picker that only honours one of them feels broken.
var pickerDown = []tea.KeyPressMsg{{Code: 'j', Text: "j"}, {Code: tea.KeyDown}}
var pickerUp = []tea.KeyPressMsg{{Code: 'k', Text: "k"}, {Code: tea.KeyUp}}

func press(t *testing.T, ep *ExportPicker, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	t.Helper()
	return ep.Update(msg)
}

// Scenario: El cursor recorre las tres opciones y se para en los dos extremos.
//
// The list is the whole picker, so the cursor has exactly one stop per option
// and nowhere else. If the last stop were exclusive the CSV option would be
// unreachable, which is the format users pick most; if it were one past the end
// the cursor would run off the list and the next render would index a missing
// option.
func TestExportPicker_CursorSweepsTheOptionsAndStopsAtBothEnds(t *testing.T) {
	for _, key := range pickerDown {
		t.Run("down "+key.String(), func(t *testing.T) {
			ep := newExportPickerForTest()
			ep.Show("public", "users", nil, nil, []string{"id"})
			const last = 2 // len(options) - 1

			// Walk down one option at a time and pin where the cursor lands.
			for want := 1; want <= last; want++ {
				if _, handled := press(t, ep, key); !handled {
					t.Fatalf("step %d: the key was not handled", want)
				}
				if ep.cursor != want {
					t.Fatalf("after %d press(es) the cursor is at %d, want %d", want, ep.cursor, want)
				}
			}

			// One more press must not move it off the end of the list.
			for i := 0; i < 3; i++ {
				if _, handled := press(t, ep, key); !handled {
					t.Fatal("a press past the end stopped being handled")
				}
				if ep.cursor != last {
					t.Fatalf("after %d press(es) past the end the cursor is at %d, want %d", i+1, ep.cursor, last)
				}
			}

			// And the last option is genuinely usable, not just reachable.
			cmd, _ := press(t, ep, tea.KeyPressMsg{Code: tea.KeyEnter})
			if cmd == nil {
				t.Fatal("enter on the last option produced no command")
			}
			msg, ok := cmd().(ExportSelectedMsg)
			if !ok {
				t.Fatalf("the command produced %T, want ExportSelectedMsg", cmd())
			}
			if msg.Format != ExportCSV {
				t.Errorf("Format = %v, want ExportCSV (the last option)", msg.Format)
			}
		})
	}

	for _, key := range pickerUp {
		t.Run("up "+key.String(), func(t *testing.T) {
			ep := newExportPickerForTest()
			ep.Show("public", "users", nil, nil, []string{"id"})
			ep.cursor = 2

			for want := 1; want >= 0; want-- {
				if _, handled := press(t, ep, key); !handled {
					t.Fatalf("step to %d: the key was not handled", want)
				}
				if ep.cursor != want {
					t.Fatalf("while walking up the cursor is at %d, want %d", ep.cursor, want)
				}
			}

			// The first option is the floor; it must not go below it.
			for i := 0; i < 3; i++ {
				if _, handled := press(t, ep, key); !handled {
					t.Fatal("a press above the first option stopped being handled")
				}
				if ep.cursor != 0 {
					t.Fatalf("after %d press(es) above the first option the cursor is at %d, want 0", i+1, ep.cursor)
				}
			}
		})
	}
}

// Scenario: El cursor nunca queda fuera de la lista.
//
// Every stop is asserted above, but the render is where an out-of-range cursor
// actually hurts: the options loop would highlight nothing, or the confirm would
// index past the end. A full down-then-up walk exercises the list from both
// sides.
func TestExportPicker_CursorStaysAValidOptionThroughAFullWalk(t *testing.T) {
	ep := newExportPickerForTest()
	ep.Show("public", "users", nil, nil, []string{"id"})

	n := len(ep.options)
	for i := 0; i < 2*n; i++ {
		key := pickerDown[i%len(pickerDown)]
		if _, _ = press(t, ep, key); ep.cursor < 0 || ep.cursor >= n {
			t.Fatalf("step %d: the cursor is at %d, outside [0,%d)", i, ep.cursor, n)
		}
	}
	for i := 0; i < 2*n; i++ {
		key := pickerUp[i%len(pickerUp)]
		if _, _ = press(t, ep, key); ep.cursor < 0 || ep.cursor >= n {
			t.Fatalf("step %d walking up: the cursor is at %d, outside [0,%d)", i, ep.cursor, n)
		}
	}
	if ep.cursor != 0 {
		t.Errorf("a walk up then all the way down ended at %d, want 0", ep.cursor)
	}
}

// Scenario: Solo la fila del cursor lleva la flecha.
//
// The arrow is the only indication of which option is selected, so it must be
// on exactly one row: on all of them the list reads as "everything is
// selected", and on none of them the user has to guess.
func TestExportPicker_ArrowMarksExactlyTheCursorRow(t *testing.T) {
	for cursor := 0; cursor < 3; cursor++ {
		t.Run(string(rune('0'+cursor)), func(t *testing.T) {
			ep := newExportPickerForTest()
			ep.Show("public", "users", nil, nil, []string{"id"})
			ep.cursor = cursor

			lines := strings.Split(ansi.Strip(ep.View()), "\n")
			var marked []int
			for i, line := range lines {
				if strings.Contains(line, "▸") {
					marked = append(marked, i)
				}
			}
			if len(marked) != 1 {
				t.Fatalf("cursor %d: %d rows carry the arrow, want exactly 1: %q", cursor, len(marked), lines)
			}
			// The marked row must be the one holding the option's own label, and
			// the label must be the option the cursor is on.
			labels := []string{"SQL", "JSON", "CSV"}
			if !strings.Contains(lines[marked[0]], labels[cursor]) {
				t.Errorf("cursor %d: the arrow is on %q, want the %q row", cursor, strings.TrimSpace(lines[marked[0]]), labels[cursor])
			}
		})
	}
}

// Scenario: Enter confirma la opción del cursor y cierra el picker.
//
// The message is what the rest of the app acts on, so it has to carry the chosen
// format and the context it was opened with. The picker closing on confirm is
// what stops the user from exporting twice into the same dialog.
func TestExportPicker_EnterConfirmsTheOptionUnderTheCursor(t *testing.T) {
	wantFormats := map[int]ExportFormat{0: ExportSQL, 1: ExportJSON, 2: ExportCSV}

	for cursor, wantFormat := range wantFormats {
		row := []interface{}{7, "ada"}
		rows := [][]interface{}{{1, "a"}, {2, "b"}}
		cols := []string{"id", "name"}

		for _, show := range []struct {
			name string
			call func(*ExportPicker)
		}{
			{"clipboard", func(p *ExportPicker) { p.Show("sales", "orders", row, rows, cols) }},
			{"file", func(p *ExportPicker) { p.ShowFileMode("sales", "orders", row, rows, cols) }},
			{"yank", func(p *ExportPicker) { p.ShowYankMode("sales", "orders", row, rows, cols) }},
		} {
			t.Run(show.name+" cursor "+string(rune('0'+cursor)), func(t *testing.T) {
				ep := newExportPickerForTest()
				show.call(ep)
				ep.cursor = cursor

				cmd, handled := press(t, ep, tea.KeyPressMsg{Code: tea.KeyEnter})
				if !handled {
					t.Fatal("enter was not handled")
				}
				if cmd == nil {
					t.Fatal("enter produced no command")
				}
				if ep.IsVisible() {
					t.Error("the picker is still visible after confirming")
				}

				msg, ok := cmd().(ExportSelectedMsg)
				if !ok {
					t.Fatalf("the command produced %T, want ExportSelectedMsg", cmd())
				}
				if msg.Format != wantFormat {
					t.Errorf("Format = %v, want %v", msg.Format, wantFormat)
				}
				if msg.Schema != "sales" || msg.Table != "orders" {
					t.Errorf("the message carries %s.%s, want sales.orders", msg.Schema, msg.Table)
				}
				if len(msg.Row) != len(row) {
					t.Errorf("Row = %v, want %v", msg.Row, row)
				}
				if len(msg.Rows) != len(rows) {
					t.Errorf("Rows has %d entries, want %d", len(msg.Rows), len(rows))
				}
				if len(msg.Columns) != len(cols) {
					t.Errorf("Columns = %v, want %v", msg.Columns, cols)
				}
				// YankMode is the one flag the caller cannot infer from the rest
				// of the message, so it has to be carried.
				if msg.YankMode != (show.name == "yank") {
					t.Errorf("YankMode = %v for the %s picker, want %v", msg.YankMode, show.name, show.name == "yank")
				}
			})
		}
	}
}

// Scenario: Esc y q cancelan sin producir nada.
//
// A cancel must not emit an export: a message here would write a file the user
// never asked for.
func TestExportPicker_CancelClosesWithoutACommand(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{{Code: tea.KeyEscape}, {Code: 'q', Text: "q"}} {
		t.Run(key.String(), func(t *testing.T) {
			ep := newExportPickerForTest()
			ep.Show("public", "users", nil, nil, []string{"id"})

			cmd, handled := press(t, ep, key)
			if !handled {
				t.Errorf("%q was not handled", key.String())
			}
			if cmd != nil {
				t.Errorf("%q produced the command %v, want none", key.String(), cmd)
			}
			if ep.IsVisible() {
				t.Errorf("%q left the picker visible", key.String())
			}
		})
	}
}

// Scenario: Un picker oculto no dibuja nada ni reacciona a las teclas.
//
// The picker is part of the grid's key routing, so it sees every keystroke. If
// it acted while hidden, a "j" typed into a cell editor would move the hidden
// cursor and change the format the next export uses.
func TestExportPicker_HiddenPickerIsInert(t *testing.T) {
	ep := newExportPickerForTest()

	if got := ep.View(); got != "" {
		t.Errorf("View on a hidden picker = %q, want empty", got)
	}
	if ep.IsVisible() {
		t.Error("a fresh picker reports itself visible")
	}

	for _, msg := range []tea.KeyPressMsg{
		{Code: 'j', Text: "j"},
		{Code: 'k', Text: "k"},
		{Code: tea.KeyDown},
		{Code: tea.KeyEnter},
		{Code: tea.KeyEscape},
		{Code: 'q', Text: "q"},
	} {
		cmd, handled := ep.Update(msg)
		if handled {
			t.Errorf("%q was handled by a hidden picker", msg.String())
		}
		if cmd != nil {
			t.Errorf("%q produced a command on a hidden picker", msg.String())
		}
	}
	if ep.IsVisible() {
		t.Error("a keypress made the hidden picker visible")
	}
}

// Scenario: Una tecla que no es de navegación se ignora pero se cierra la mano.
//
// Unknown keys report "not handled" so the caller's routing can keep looking,
// and crucially they do not move the cursor or close the dialog.
func TestExportPicker_UnknownKeysLeaveThePickerAlone(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{{Code: 'x', Text: "x"}, {Code: 'z', Text: "z"}, {Code: tea.KeyTab}} {
		ep := newExportPickerForTest()
		ep.Show("public", "users", nil, nil, []string{"id"})
		ep.cursor = 1

		cmd, handled := press(t, ep, key)
		if handled {
			t.Errorf("%q was reported as handled", key.String())
		}
		if cmd != nil {
			t.Errorf("%q produced a command", key.String())
		}
		if !ep.IsVisible() {
			t.Errorf("%q closed the picker", key.String())
		}
		if ep.cursor != 1 {
			t.Errorf("%q moved the cursor to %d, want it unchanged at 1", key.String(), ep.cursor)
		}
	}
}

// Scenario: Cada modo se describe a sí mismo en el título y en las opciones.
//
// Three ways to open the same picker, and the user has to be able to tell them
// apart before choosing: copy to the clipboard, save to a file, or yank. The
// title names the action and the descriptions say Copy or Save to match.
func TestExportPicker_EachModeNamesItsOwnAction(t *testing.T) {
	for _, tc := range []struct {
		name       string
		show       func(*ExportPicker)
		wantTitle  string
		wantWords  []string
		unwantWord string
	}{
		{"clipboard", func(p *ExportPicker) { p.Show("sales", "orders", nil, nil, nil) },
			"Export sales.orders", []string{"Copy INSERT", "Copy JSON", "Copy CSV"}, "Save"},
		{"file", func(p *ExportPicker) { p.ShowFileMode("sales", "orders", nil, nil, nil) },
			"Export to file sales.orders", []string{"Save INSERT", "Save JSON", "Save CSV"}, "Copy"},
		{"yank", func(p *ExportPicker) { p.ShowYankMode("sales", "orders", nil, nil, nil) },
			"Yank sales.orders", []string{"Copy INSERT", "Copy JSON", "Copy CSV"}, "Save"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ep := newExportPickerForTest()
			tc.show(ep)
			view := ansi.Strip(ep.View())

			if !strings.Contains(view, tc.wantTitle) {
				t.Errorf("the title is not %q: %q", tc.wantTitle, view)
			}
			for _, w := range tc.wantWords {
				if !strings.Contains(view, w) {
					t.Errorf("the view is missing %q: %q", w, view)
				}
			}
			if strings.Contains(view, tc.unwantWord) {
				t.Errorf("the %s picker mentions %q, which belongs to another mode: %q", tc.name, tc.unwantWord, view)
			}
		})
	}
}

// Scenario: Abrir el picker lo reinicia por completo.
//
// Show, ShowFileMode and ShowYankMode all take the picker back to the top of the
// list, make it visible, and clear whichever of the two modes they do not set.
// Re-opening after a cancel that left the cursor on CSV must start at SQL again,
// and opening the file picker after the yank one must not still be in yank mode.
func TestExportPicker_ShowResetsTheCursorAndTheModes(t *testing.T) {
	ep := newExportPickerForTest()

	ep.Show("public", "users", nil, nil, nil)
	ep.cursor = 2
	ep.Hide()
	if ep.IsVisible() {
		t.Fatal("Hide did not hide the picker")
	}

	// Re-showing puts the cursor back at the top and makes it visible again.
	ep.Show("public", "users", nil, nil, nil)
	if !ep.IsVisible() {
		t.Error("Show did not make the picker visible")
	}
	if ep.cursor != 0 {
		t.Errorf("the cursor is at %d after re-showing, want 0", ep.cursor)
	}

	// The two modes are mutually exclusive, and each Show variant sets exactly
	// one of them.
	for _, tc := range []struct {
		name     string
		show     func(*ExportPicker)
		wantFile bool
		wantYank bool
	}{
		{"Show", func(p *ExportPicker) { p.Show("s", "t", nil, nil, nil) }, false, false},
		{"ShowFileMode", func(p *ExportPicker) { p.ShowFileMode("s", "t", nil, nil, nil) }, true, false},
		{"ShowYankMode", func(p *ExportPicker) { p.ShowYankMode("s", "t", nil, nil, nil) }, false, true},
	} {
		// Dirty both flags, then show, and check that only the intended one
		// survived.
		ep.fileMode, ep.yankMode = true, true
		tc.show(ep)
		if ep.fileMode != tc.wantFile || ep.yankMode != tc.wantYank {
			t.Errorf("%s left fileMode=%v yankMode=%v, want %v/%v",
				tc.name, ep.fileMode, ep.yankMode, tc.wantFile, tc.wantYank)
		}
		if ep.cursor != 0 {
			t.Errorf("%s left the cursor at %d, want 0", tc.name, ep.cursor)
		}
	}
}

// Scenario: El picker guarda el contexto con el que se abrió.
//
// The schema, table, row and columns are all passed in at open time and handed
// back untouched on confirm. Losing any of them would make the export land
// somewhere other than where the user was looking.
func TestExportPicker_KeepsTheContextItWasOpenedWith(t *testing.T) {
	row := []interface{}{42, "answer", nil}
	rows := [][]interface{}{{1, "a", nil}, {2, "b", nil}}
	cols := []string{"id", "body", "note"}

	ep := newExportPickerForTest()
	ep.Show("analytics", "events", row, rows, cols)

	if ep.schema != "analytics" || ep.table != "events" {
		t.Errorf("the picker kept %s.%s, want analytics.events", ep.schema, ep.table)
	}
	if len(ep.row) != 3 || ep.row[0] != 42 {
		t.Errorf("row = %v, want the row it was opened with", ep.row)
	}
	if len(ep.rows) != 2 {
		t.Errorf("rows has %d entries, want 2", len(ep.rows))
	}
	if len(ep.columns) != 3 {
		t.Errorf("columns = %v, want 3 entries", ep.columns)
	}

	// The title names the table it will export, so a stale name is visible.
	if !strings.Contains(ansi.Strip(ep.View()), "analytics.events") {
		t.Error("the view does not name the table it will export")
	}
}

// Scenario: Tamaño y visibilidad son ajustes, no decisiones del render.
//
// The picker keeps width and height as state, and neither changes what it draws:
// a modal is sized by its caller. Storing them without a single use is worth
// pinning, because it is what tells a future reader they are not load-bearing.
func TestExportPicker_WidthAndHeightAreStoredButNotDrawn(t *testing.T) {
	ep := newExportPickerForTest()
	ep.Show("public", "users", nil, nil, nil)

	plain := ansi.Strip(ep.View())
	for _, size := range []int{0, 1, 20, 200} {
		ep.SetWidth(size)
		ep.SetHeight(size)
		if got := ansi.Strip(ep.View()); got != plain {
			t.Errorf("the view changed at width/height %d:\n%s\nwant:\n%s", size, got, plain)
		}
	}
	if ep.width != 200 || ep.height != 200 {
		t.Errorf("width/height = %d/%d, want 200/200", ep.width, ep.height)
	}
}
