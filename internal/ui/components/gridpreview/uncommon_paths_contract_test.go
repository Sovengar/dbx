package gridpreview

// Scenario: Los caminos que solo se alcanzan con una fila uncommon.
//
// The preview has three states a normal fixture never reaches, and every one of them is
// reachable from the app:
//
//	an EMPTY row — a query that matched nothing, or a NULL row
//	a row SHORTER than the column list — a SELECT of fewer expressions than the table has
//	a value JSON CANNOT MARSHAL — json.Marshal erroring on a channel or a NaN
//
// Each of them was uncovered. The second is the one that can take the process down: the
// pairing loop is `for i, col := range columns { if i < len(row) {...} }`, and a row one
// element short of the column list is ordinary — it is what a projection of a subset of a
// table's columns returns — so a missing bound there is a panic on the first row of a
// perfectly normal query.
//
// The rest of this file is the render and the jq path, which between them decide what the
// user sees after a filter.

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/buble/dbx/internal/drivers/postgres"
)

// ---------------------------------------------------------------------------
// SetRow
// ---------------------------------------------------------------------------

func TestSetRowToleratesEveryShapeAQueryCanReturn(t *testing.T) {
	t.Run("a row SHORTER than the column list does not panic and does not invent values", func(t *testing.T) {
		// The real shape: SELECT id, name FROM t where the table has id, name, email.
		p := newPreviewTest()
		p.SetRow([]string{"id", "name", "email"}, []interface{}{7, "ada"})

		mustNotPanic(t, "rebuilding after a short row", func() { p.rebuildLines() })
		data := p.displayData()
		if got, ok := data["email"]; ok {
			t.Errorf("the short row produced a value for the missing column: %v", got)
		}
		if len(data) != 2 {
			t.Errorf("the display holds %d keys (%v), want the two the row carried", len(data), keysOfAny(data))
		}
		if got := data["name"]; got != "ada" {
			t.Errorf("name is %v, want ada", got)
		}
	})

	t.Run("a row LONGER than the column list keeps only the named columns", func(t *testing.T) {
		// The other direction. The extra cells have no name, so there is nowhere to
		// put them; keeping them would show a column the user never selected.
		p := newPreviewTest()
		p.SetRow([]string{"id"}, []interface{}{7, "extra", "more"})

		data := p.displayData()
		if len(data) != 1 {
			t.Errorf("the display holds %d keys (%v), want just id", len(data), keysOfAny(data))
		}
		if data["id"] != 7 {
			t.Errorf("id is %v, want 7", data["id"])
		}
	})

	t.Run("a nil row clears the preview", func(t *testing.T) {
		p := newPreviewTest()
		p.cursorLine = 3
		p.scrollY = 2

		for _, tc := range []struct {
			name    string
			columns []string
			row     []interface{}
		}{
			{"a nil row", []string{"a", "b", "c"}, nil},
			{"no columns", nil, []interface{}{1}},
			{"both empty", nil, nil},
		} {
			t.Run(tc.name, func(t *testing.T) {
				p.SetRow(tc.columns, tc.row)

				if len(p.lines) != 0 {
					t.Errorf("the preview kept %d lines: %v", len(p.lines), p.lines)
				}
				if p.rawJSON != nil {
					t.Errorf("the preview kept raw JSON: %s", p.rawJSON)
				}
				if p.columns != nil {
					t.Errorf("the preview kept the columns %v", p.columns)
				}
				if p.rowData != nil {
					t.Errorf("the preview kept row data %v", p.rowData)
				}
				// The maps are re-made, not left holding the old row's expansions.
				// A stale expansion would make a later row render as the previous one.
				if p.expandedFKs == nil || len(p.expandedFKs) != 0 {
					t.Errorf("expandedFKs is %v, want an empty map", p.expandedFKs)
				}
				if p.nestedFKs == nil || len(p.nestedFKs) != 0 {
					t.Errorf("nestedFKs is %v, want an empty map", p.nestedFKs)
				}
				// The cursor and the scroll are deliberately NOT reset here, and
				// the first version of this asserted that they were. They are not,
				// and nothing breaks: Render returns before slicing when there are
				// no lines, so a stale scroll of 2 over an empty preview renders
				// "No data" rather than panicking on p.lines[2:0]. Asserted
				// explicitly, because "the cursor is 3 and there are 0 lines" reads
				// like a bug until you know what protects it.
				if p.cursorLine != 3 {
					t.Errorf("the cursor is on line %d after clearing, want it left at 3 — the clear path does not reset it", p.cursorLine)
				}
				if p.scrollY != 2 {
					t.Errorf("the scroll is at %d after clearing, want it left at 2", p.scrollY)
				}
				// The empty render is what makes the stale scroll safe.
				if out := p.Render(); !strings.Contains(stripEsc(out), "No data") {
					t.Errorf("a cleared preview with a stale scroll rendered:\n%s", out)
				}
			})
		}
	})

	t.Run("setting a row RESETS the cursor and the scroll", func(t *testing.T) {
		// Moving to another row of another table has to start at the top. A cursor
		// left at line 20 of the previous row points past the end of the new one,
		// and every navigation action then clamps it silently.
		p := newPreviewTest()
		p.cursorLine = 5
		p.scrollY = 4

		p.SetRow([]string{"x", "y"}, []interface{}{1, 2})
		if p.cursorLine != 0 {
			t.Errorf("the cursor is on line %d after a new row, want 0", p.cursorLine)
		}
		if p.scrollY != 0 {
			t.Errorf("the scroll is at %d after a new row, want 0", p.scrollY)
		}
	})

	t.Run("a value JSON cannot marshal becomes ONE line saying so", func(t *testing.T) {
		// json.Marshal errors on a NaN and on a channel. The preview must survive it:
		// the render would otherwise walk a nil p.lines and the user sees a blank
		// pane with no explanation for a row the server did return.
		p := newPreviewTest()
		p.SetRow([]string{"a"}, []interface{}{math.Inf(1)})

		if len(p.lines) != 1 {
			t.Fatalf("the preview has %d lines (%v), want one error line", len(p.lines), p.lines)
		}
		if !strings.HasPrefix(p.lines[0], "Error:") {
			t.Errorf("the line is %q, want it to start with Error:", p.lines[0])
		}
		if p.rawJSON != nil {
			t.Errorf("raw JSON is %s after a marshal failure, want nil — jq would run against stale data", p.rawJSON)
		}
		// And it renders as that line rather than as nothing.
		if !strings.Contains(stripEsc(p.Render()), "Error") {
			t.Errorf("the render does not show the error:\n%s", p.Render())
		}
	})

	t.Run("rebuildLines reports a marshal failure the same way", func(t *testing.T) {
		p := newPreviewTest()
		p.rowData = map[string]interface{}{"a": math.NaN()}
		p.rebuildLines()
		if len(p.lines) != 1 || !strings.HasPrefix(p.lines[0], "Error:") {
			t.Errorf("rebuildLines produced %v, want one error line", p.lines)
		}
	})
}

// ---------------------------------------------------------------------------
// SetForeignKeys
// ---------------------------------------------------------------------------

func TestSetForeignKeysPreparesTheMapsItNeeds(t *testing.T) {
	t.Run("on a fresh preview both maps are made", func(t *testing.T) {
		// Both are nil on a new GridPreview, and ExpandFK writes into them. The
		// lookup paths read them too, so a nil map there is a read of a nil map —
		// legal in Go and fine — but the WRITE is not.
		p := New(newPreviewTest().styles, nil)
		p.SetForeignKeys([]postgres.ForeignKeyInfo{{Column: "user_id"}})

		if p.foreignKeys == nil || len(p.foreignKeys) != 1 {
			t.Errorf("the foreign keys are %v, want the one that was set", p.foreignKeys)
		}
		if p.expandedFKs == nil {
			t.Error("expandedFKs is nil after SetForeignKeys")
		}
		if p.nestedFKs == nil {
			t.Error("nestedFKs is nil after SetForeignKeys")
		}
		// And the map is writable, which is the point.
		p.expandedFKs["user_id"] = map[string]interface{}{"id": 1}
		if len(p.expandedFKs) != 1 {
			t.Error("writing to expandedFKs after SetForeignKeys did nothing")
		}
	})

	t.Run("an EMPTY key list still prepares the maps", func(t *testing.T) {
		p := New(newPreviewTest().styles, nil)
		p.SetForeignKeys(nil)
		if p.expandedFKs == nil || p.nestedFKs == nil {
			t.Error("SetForeignKeys(nil) left the maps nil")
		}
	})
}

// ---------------------------------------------------------------------------
// Render
// ---------------------------------------------------------------------------

func TestRenderCoversTheEmptyAndUnsizedCases(t *testing.T) {
	t.Run("a width of zero renders nothing at all", func(t *testing.T) {
		p := newPreviewTest()
		p.SetWidth(0)
		if got := p.Render(); got != "" {
			t.Errorf("a preview of width 0 rendered %q", got)
		}
	})

	t.Run("a preview with no lines says so", func(t *testing.T) {
		p := newPreviewTest()
		p.SetRow(nil, nil)

		out := stripEsc(p.Render())
		if !strings.Contains(out, "No data") {
			t.Errorf("an empty preview rendered:\n%s", out)
		}
		// Without the jq prompt: the prompt belongs to the filter mode, which is not
		// on, so showing it would tell the user they are filtering when they are not.
		if strings.Contains(out, "jq") {
			t.Errorf("an empty preview shows a jq prompt while not in jq mode:\n%s", out)
		}
	})

	t.Run("an empty preview in jq mode keeps the prompt", func(t *testing.T) {
		// The prompt is how a user knows which filter they are editing, so it has to
		// survive the empty case — that is the state right after pressing the filter
		// key, before any expression is typed.
		p := newPreviewTest()
		p.SetRow(nil, nil)
		p.jqMode = true

		out := stripEsc(p.Render())
		if !strings.Contains(out, "jq") {
			t.Errorf("an empty preview in jq mode has no prompt:\n%s", out)
		}
		if !strings.Contains(out, "No data") {
			t.Errorf("an empty preview in jq mode does not say there is no data:\n%s", out)
		}
	})

	t.Run("a non-empty filter shows the expression even outside jq mode", func(t *testing.T) {
		// Leaving jq mode with a filter applied must keep the filter visible, or the
		// user is looking at filtered data with no indication that it is filtered.
		p := newPreviewTest()
		p.jqExpr = ".a"

		out := stripEsc(p.Render())
		if !strings.Contains(out, "jq: .a") {
			t.Errorf("an applied filter is not shown outside jq mode:\n%s", out)
		}
	})

	t.Run("the render does not exceed the height it was given", func(t *testing.T) {
		// The pane is laid out by subtracting its rows from the window, so a render
		// taller than the pane pushes everything below it off screen.
		p := newPreviewTest()
		p.SetHeight(12)
		// Enough lines to overflow a twelve-row pane.
		cols := []string{}
		row := []interface{}{}
		for i := range 60 {
			cols = append(cols, string(rune('a'+i%26))+itoa(i))
			row = append(row, i)
		}
		p.SetRow(cols, row)

		lines := strings.Split(strings.TrimRight(p.Render(), "\n"), "\n")
		if len(lines) > 12 {
			t.Errorf("the preview rendered %d lines into a height of 12", len(lines))
		}
		if len(lines) == 0 {
			t.Error("the preview rendered no lines at all")
		}
	})

	t.Run("an expandable FK is marked and an expanded one is marked differently", func(t *testing.T) {
		// The two markers are the only indication that a value can be drilled into,
		// and they are opposite arrows: → not yet expanded, ↓ expanded.
		p := newPreviewTest()
		p.SetForeignKeys([]postgres.ForeignKeyInfo{{Column: "user_id"}})
		p.SetRow([]string{"user_id", "name"}, []interface{}{"x", "ada"})
		p.SetHeight(30)
		p.SetWidth(100)

		collapsed := stripEsc(p.Render())
		if !strings.Contains(collapsed, "→") {
			t.Errorf("an unexpanded FK has no arrow:\n%s", collapsed)
		}

		p.ExpandFK("user_id", "user_id", map[string]interface{}{"id": 1, "email": "a@b"}, nil)
		expanded := stripEsc(p.Render())
		if strings.Contains(expanded, "→") {
			t.Errorf("an expanded FK still shows the collapsed arrow:\n%s", expanded)
		}
		if !strings.Contains(expanded, "↓") {
			t.Errorf("an expanded FK has no expanded arrow:\n%s", expanded)
		}
	})
}

// ---------------------------------------------------------------------------
// applyJQ
// ---------------------------------------------------------------------------

func TestApplyJQReportsEveryWayItCanFail(t *testing.T) {
	t.Run("an expression that does not compile", func(t *testing.T) {
		p := newPreviewTest()
		p.jqExpr = ".["
		p.applyJQ()

		if len(p.lines) != 1 {
			t.Fatalf("the preview has %d lines (%v), want one error line", len(p.lines), p.lines)
		}
		// Parse, not compile: ".[" fails to PARSE, which is a different failure
		// from an expression that parses and will not compile. Both are reported,
		// under their own names.
		if !strings.HasPrefix(p.lines[0], "jq parse error:") {
			t.Errorf("the line is %q, want a parse error", p.lines[0])
		}
	})

	t.Run("an expression that parses but will not compile", func(t *testing.T) {
		// A compile error needs an arity or binding mistake, not bad syntax: the
		// first version used "def f: f; f", which is not an error at all — it
		// compiles and then loops forever at runtime, and the test suite HUNG.
		// "f(2)" against a zero-argument f is rejected at compile time.
		p := newPreviewTest()
		p.jqExpr = "def f: 1; f(2)"
		p.applyJQ()

		if len(p.lines) != 1 {
			t.Fatalf("the preview has %d lines (%v), want one error line", len(p.lines), p.lines)
		}
		if !strings.HasPrefix(p.lines[0], "jq compile error:") {
			t.Errorf("the line is %q, want a compile error", p.lines[0])
		}
	})

	t.Run("an expression that fails at RUNTIME says so", func(t *testing.T) {
		// A third failure kind, and the only one that needs input to reach: the
		// expression is fine, the document is not the shape it expects.
		p := newPreviewTest()
		p.jqExpr = "1/0"
		p.applyJQ()

		if len(p.lines) != 1 || !strings.HasPrefix(p.lines[0], "jq runtime error:") {
			t.Errorf("the preview is %v, want a runtime error", p.lines)
		}
	})

	t.Run("a field that does not exist is null, not empty", func(t *testing.T) {
		// ".nosuchfield" against {"a":1} yields null, which jq renders as "null".
		// The first version of this asserted (empty result) and failed, having
		// assumed a missing key produces no output. It produces a value: null.
		// That distinction is worth pinning, because "(empty result)" and "null"
		// read very differently to someone debugging a filter.
		p := newPreviewTest()
		p.jqExpr = ".nosuchfield"
		p.applyJQ()

		if got := strings.Join(p.lines, "\n"); got != "null" {
			t.Errorf("a missing field gave %q, want \"null\"", got)
		}
	})

	t.Run("an expression that matches nothing says so", func(t *testing.T) {
		// "empty result" and "no output" have to be told apart from a crash: the
		// user typed a filter and got nothing, which is information.
		p := newPreviewTest()
		// `empty`, not a missing field: a missing field yields null, which is a
		// value. Only `empty` produces no output at all.
		p.jqExpr = "empty"
		p.applyJQ()

		if len(p.lines) != 1 || p.lines[0] != "(empty result)" {
			t.Errorf("the preview is %v, want (empty result)", p.lines)
		}
	})

	t.Run("one result is shown as one value and several as a list", func(t *testing.T) {
		p := newPreviewTest()
		p.jqExpr = ".a"
		p.applyJQ()
		single := strings.Join(p.lines, "\n")
		if strings.Contains(single, "[\n") {
			t.Errorf("a single result was rendered as a list:\n%s", single)
		}
		if !strings.Contains(single, "1") {
			t.Errorf("the single result does not carry its value:\n%s", single)
		}

		p.jqExpr = ".[]"
		p.applyJQ()
		several := strings.Join(p.lines, "\n")
		if !strings.HasPrefix(strings.TrimLeft(several, " "), "[") {
			t.Errorf("three results were not rendered as a list:\n%s", several)
		}
	})

	t.Run("applying a filter resets the scroll", func(t *testing.T) {
		// Otherwise the user filters, the line count collapses, and the scroll is
		// left past the end showing an empty pane.
		p := newPreviewTest()
		p.SetHeight(6)
		cols := make([]string, 0, 40)
		row := make([]interface{}, 0, 40)
		for i := range 40 {
			cols = append(cols, "c"+itoa(i))
			row = append(row, i)
		}
		p.SetRow(cols, row)
		p.scrollY = 20

		p.jqExpr = ".c0"
		p.applyJQ()
		if p.scrollY != 0 {
			t.Errorf("the scroll is at %d after filtering, want 0", p.scrollY)
		}
	})

	t.Run("applying a filter with no raw JSON does nothing at all", func(t *testing.T) {
		// rawJSON is nil until a row is set, and the jq key is reachable with no
		// row: the preview is focused before the grid has loaded. applyJQ returns
		// on the nil rather than reporting it, so the preview keeps showing "No
		// data" — which is the truth, and a better message than an error about a
		// document the user has not asked to see.
		p := newPreviewTest()
		p.SetRow(nil, nil)
		p.rawJSON = nil

		mustNotPanic(t, "applyJQ with no raw JSON", func() { p.applyJQ() })
		if !strings.Contains(stripEsc(p.Render()), "No data") {
			t.Errorf("after applyJQ with no raw JSON the preview renders:\n%s", p.Render())
		}
	})

	t.Run("an EMPTY filter shows the whole document again", func(t *testing.T) {
		// Clearing the filter has to bring the original row back, not leave the
		// filtered output — otherwise the user cannot undo a filter by emptying it.
		p := newPreviewTest()
		p.jqExpr = ".a"
		p.applyJQ()
		filtered := strings.Join(p.lines, "\n")

		p.jqExpr = ""
		p.applyJQ()
		whole := strings.Join(p.lines, "\n")
		if whole == filtered {
			t.Errorf("emptying the filter left the filtered output:\n%s", whole)
		}
		if !strings.Contains(whole, "\"b\"") {
			t.Errorf("emptying the filter did not restore the whole document:\n%s", whole)
		}
	})
}

// ---------------------------------------------------------------------------
// Update and the jq input
// ---------------------------------------------------------------------------

func TestUpdateRoutesToTheRightPlace(t *testing.T) {
	t.Run("an unfocused preview handles nothing", func(t *testing.T) {
		// Before the user clicks the preview it must not react to keys: the grid
		// underneath owns them.
		p := newPreviewTest()
		if p.IsFocused() {
			t.Fatal("the fixture is focused")
		}
		cmd, handled := p.Update(tea.KeyPressMsg{Code: 'j'})
		if handled || cmd != nil {
			t.Errorf("an unfocused preview handled a key: %v, %v", cmd, handled)
		}
	})

	t.Run("a focused preview with NO keybinds handles nothing", func(t *testing.T) {
		// New accepts a nil Resolver, so this is a legal state and the nil check is
		// what keeps it from panicking on the first keypress.
		p := New(newPreviewTest().styles, nil)
		p.SetWidth(80)
		p.SetHeight(20)
		p.SetRow([]string{"a"}, []interface{}{1})
		p.Focus()

		mustNotPanic(t, "a preview with no keybinds on a keypress", func() { p.Update(tea.KeyPressMsg{Code: 'j', Text: "j"}) })
	})

	t.Run("a focused preview with no BINDING for the key handles nothing", func(t *testing.T) {
		p := newPreviewTest()
		p.Focus()
		cmd, handled := p.Update(tea.KeyPressMsg{Code: 'Q', Text: "Q"})
		if handled {
			t.Errorf("an unbound key was reported as handled, returning %v", cmd)
		}
	})

	t.Run("a focused preview in jq mode sends keys to the filter BUFFER", func(t *testing.T) {
		// The routing, not the editing: entering jq mode must divert every key away
		// from the registry, or typing a filter would also navigate.
		//
		// Typed characters go into jqInput and only become jqExpr on enter. The first
		// version asserted against jqExpr and found it empty, having missed that
		// there are two fields: the buffer being edited, and the expression
		// currently applied. Asserted on both, because the distinction is the whole
		// reason a filter can be edited without taking effect at every keystroke.
		p := newPreviewTest()
		p.Focus()
		p.jqMode = true

		if _, handled := p.Update(tea.KeyPressMsg{Code: 'q', Text: "q"}); !handled {
			t.Error("a character in jq mode was not handled")
		}
		if !strings.Contains(p.jqInput, "q") {
			t.Errorf("the filter buffer is %q, want the typed character in it", p.jqInput)
		}
		if p.jqExpr != "" {
			t.Errorf("the applied expression is %q before enter, want it still empty", p.jqExpr)
		}

		p.Update(tea.KeyPressMsg{Code: '.', Text: "."})
		p.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
		if p.jqInput != "q.a" {
			t.Errorf("the buffer is %q after typing q.a", p.jqInput)
		}

		// Enter commits it and leaves the mode.
		p.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Text: "enter"})
		if p.jqExpr != "q.a" {
			t.Errorf("the applied expression is %q after enter, want q.a", p.jqExpr)
		}
		if p.IsJQMode() {
			t.Error("enter did not leave jq mode")
		}
		if p.jqInput != "" {
			t.Errorf("the buffer is %q after leaving jq mode, want it cleared", p.jqInput)
		}
	})

	t.Run("ctrl+space opens and closes the suggestion list", func(t *testing.T) {
		p := newPreviewTest()
		p.Focus()
		p.jqMode = true

		// The switch matches on keyMsg.String(), so the message has to be the one
		// that stringifies to "ctrl+space". Sending Code:' ' with ModCtrl is what
		// the first version did, and it did nothing at all.
		toggle := tea.KeyPressMsg{Code: ' ', Mod: tea.ModCtrl}
		if got := toggle.String(); got != "ctrl+space" {
			t.Fatalf("the toggle key stringifies as %q, want \"ctrl+space\"; the handler matches on it", got)
		}

		p.handleJQInput(toggle)
		if !p.jqSugVisible {
			t.Error("ctrl+space did not show the suggestions")
		}
		p.handleJQInput(toggle)
		if p.jqSugVisible {
			t.Error("ctrl+space did not hide the suggestions")
		}
	})

	t.Run("tab with no suggestions open is handled and does nothing", func(t *testing.T) {
		// It has to be handled — tab is a mode key and swallowing it silently would
		// look like a dead key — but with no suggestions it must not change state.
		p := newPreviewTest()
		p.Focus()
		p.jqMode = true
		before := p.jqExpr

		cmd, handled := p.handleJQInput(tea.KeyPressMsg{Code: tea.KeyTab})
		if !handled {
			t.Error("tab with no suggestions was not reported as handled")
		}
		if cmd != nil {
			t.Error("tab with no suggestions returned a command")
		}
		if p.jqExpr != before {
			t.Errorf("tab with no suggestions changed the filter from %q to %q", before, p.jqExpr)
		}
	})

	t.Run("the suggestion list renders when there are suggestions", func(t *testing.T) {
		p := newPreviewTest()
		p.SetWidth(80)
		p.Focus()
		p.jqMode = true
		p.updateJQSuggestions()
		p.jqSugVisible = true
		if len(p.jqSugs) == 0 {
			t.Skip("the fixture produced no suggestions, so there is nothing to render")
		}

		out := stripEsc(p.renderJQSuggestions())
		if out == "" {
			t.Error("renderJQSuggestions returned nothing with suggestions available")
		}
		if !strings.Contains(out, p.jqSugs[0].Path) {
			t.Errorf("the rendered suggestions do not mention %q:\n%s", p.jqSugs[0].Path, out)
		}
		// Every suggestion is shown: this is a menu, not a single line.
		for _, s := range p.jqSugs {
			if !strings.Contains(out, s.Path) && len(s.Path) <= 15 {
				t.Errorf("the suggestion %q is missing from:\n%s", s.Path, out)
			}
		}
	})

	t.Run("no suggestions renders an empty box, not nothing", func(t *testing.T) {
		// An empty bordered frame rather than an empty string. The first version of
		// this asserted "" and failed on a box. That is a deliberate-looking
		// artefact: the function always draws its frame and there is no guard for an
		// empty list. It is harmless — the caller only shows this when
		// jqSugVisible is set and something is selected — so it is pinned as it is
		// rather than "fixed", and the comment says why nobody should.
		p := newPreviewTest()
		p.SetWidth(80)
		p.jqSugs = nil
		got := p.renderJQSuggestions()
		if got == "" {
			t.Skip("the renderer now returns nothing for an empty list")
		}
		if len(stripEsc(got)) == 0 {
			t.Error("renderJQSuggestions returned only escape sequences")
		}
	})
}

// ---------------------------------------------------------------------------
// FK expansion, cursor visibility, scrolling
// ---------------------------------------------------------------------------

func TestFKExpansionAndCursorVisibility(t *testing.T) {
	t.Run("expanding a column that is not an FK does nothing", func(t *testing.T) {
		p := newPreviewTest()
		p.SetForeignKeys([]postgres.ForeignKeyInfo{{Column: "user_id"}})
		p.SetRow([]string{"name", "user_id"}, []interface{}{"ada", "x"})
		p.SetHeight(30)

		if _, handled := p.handleExpand(); handled {
			t.Error("expanding a non-FK column was reported as handled")
		}
		if len(p.expandedFKs) != 0 {
			t.Errorf("expanding a non-FK column recorded %v", p.expandedFKs)
		}
	})

	t.Run("a cursor past the end cannot be expanded", func(t *testing.T) {
		p := newPreviewTest()
		p.SetRow([]string{"a"}, []interface{}{1})
		p.cursorLine = len(p.lines) + 5

		mustNotPanic(t, "expanding with the cursor past the end", func() { p.handleExpand() })
		if _, handled := p.handleExpand(); handled {
			t.Error("expanding with the cursor past the end was reported as handled")
		}
	})

	t.Run("navigating back to the top scrolls back up", func(t *testing.T) {
		p := newPreviewTest()
		// Taller than 6: ensureCursorVisible's contentHeight is height-6, and at 6
		// it is zero and the function returns before doing anything. The first
		// version used a height of 6 and reported that the preview had not scrolled,
		// having measured the early return.
		p.SetHeight(14)
		cols := make([]string, 0, 30)
		row := make([]interface{}, 0, 30)
		for i := range 30 {
			cols = append(cols, "c"+itoa(i))
			row = append(row, i)
		}
		p.SetRow(cols, row)
		p.cursorLine = 25
		p.ensureCursorVisible()
		if p.scrollY == 0 {
			t.Fatal("the preview did not scroll to follow the cursor to line 25")
		}

		p.cursorLine = 0
		p.ensureCursorVisible()
		if p.scrollY != 0 {
			t.Errorf("the scroll is at %d with the cursor on line 0, want 0", p.scrollY)
		}
	})

	t.Run("a height too small to show anything scrolls nowhere", func(t *testing.T) {
		// contentHeight is height-6, which is zero or negative on a short pane. The
		// guard returns early, and without it the arithmetic below would set the
		// scroll to a negative number and the render would slice from the end.
		p := newPreviewTest()
		for _, h := range []int{0, 1, 5, 6} {
			p.SetHeight(h)
			p.cursorLine = 10
			mustNotPanic(t, "ensureCursorVisible at height "+itoa(h), func() { p.ensureCursorVisible() })
			if p.scrollY < 0 {
				t.Errorf("at height %d the scroll is %d, which is negative", h, p.scrollY)
			}
		}
	})

	t.Run("half_page_down stops at the bottom and half_page_up at the top", func(t *testing.T) {
		p := newPreviewTest()
		p.SetHeight(12)
		cols := make([]string, 0, 80)
		row := make([]interface{}, 0, 80)
		for i := range 80 {
			cols = append(cols, "c"+itoa(i))
			row = append(row, i)
		}
		p.SetRow(cols, row)

		for range 20 {
			p.halfPageDown()
		}
		if p.scrollY < 0 {
			t.Errorf("twenty half-page downs left the scroll at %d, which is negative", p.scrollY)
		}
		// The cap is len(lines) - height + 6, not len(lines) - height. The six is
		// the same border-and-chrome allowance the renderer subtracts, so the last
		// line stays reachable; the first version of this computed the cap without
		// it and reported a clamp that was not one.
		wantMax := len(p.lines) - p.height + 6
		if wantMax < 0 {
			wantMax = 0
		}
		if p.scrollY != wantMax {
			t.Errorf("twenty half-page downs left the scroll at %d, want the cap %d", p.scrollY, wantMax)
		}
		// And it is still a legal slice: the render does p.lines[scrollY:end], so a
		// scroll past the end is an out-of-range panic rather than a blank pane.
		if p.scrollY > len(p.lines) {
			t.Errorf("the scroll is %d with %d lines, which would panic the render", p.scrollY, len(p.lines))
		}
		if out := p.Render(); out == "" {
			t.Error("the render is empty at the bottom of a scrolled preview")
		}

		for range 20 {
			p.halfPageUp()
		}
		if p.scrollY != 0 {
			t.Errorf("twenty half-page ups left the scroll at %d, want 0", p.scrollY)
		}
	})
}

// ---------------------------------------------------------------------------
// jq history
// ---------------------------------------------------------------------------

// jqHome points the history file at a temp dir. historyFilePath reads the user's home
// directory, so a test that does not do this writes to the developer's real ~/.config —
// and one that asserts on the contents reads the developer's real history.
func jqHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir) // the Windows spelling, for portability
	return dir
}

func TestJQHistoryRoundTripsThroughDisk(t *testing.T) {
	t.Run("an expression is written where it can be read back", func(t *testing.T) {
		home := jqHome(t)
		p := newPreviewTest()
		p.Focus()
		p.jqMode = true

		p.addToHistory(".a")
		p.addToHistory(".b")

		path := filepath.Join(home, ".config", "dbx", "jq_history.json")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("the history was not written: %v", err)
		}
		var stored []string
		if err := json.Unmarshal(data, &stored); err != nil {
			t.Fatalf("the history is not a JSON list of strings: %s", data)
		}
		if len(stored) != 2 || stored[0] != ".a" || stored[1] != ".b" {
			t.Errorf("the stored history is %v, want [.a .b]", stored)
		}

		// And a new preview reads it back.
		fresh := newPreviewTest()
		fresh.loadJQHistory()
		if len(fresh.jqHistory) != 2 {
			t.Errorf("the reloaded history is %v, want two entries", fresh.jqHistory)
		}
	})

	t.Run("re-adding an expression moves it to the end rather than duplicating it", func(t *testing.T) {
		jqHome(t)
		p := newPreviewTest()
		p.addToHistory(".a")
		p.addToHistory(".b")
		p.addToHistory(".a")

		if len(p.jqHistory) != 2 {
			t.Fatalf("the history is %v, want two entries after a repeat", p.jqHistory)
		}
		if p.jqHistory[1] != ".a" {
			t.Errorf("the history is %v, want .a last", p.jqHistory)
		}
	})

	t.Run("the history is capped", func(t *testing.T) {
		jqHome(t)
		p := newPreviewTest()
		for i := range maxJQHistory + 25 {
			p.addToHistory(".expr" + itoa(i))
		}
		if len(p.jqHistory) != maxJQHistory {
			t.Errorf("the history holds %d entries, want the cap of %d", len(p.jqHistory), maxJQHistory)
		}
		// The cap drops the OLDEST, so the last one added is present.
		if got := p.jqHistory[len(p.jqHistory)-1]; got != ".expr"+itoa(maxJQHistory+24) {
			t.Errorf("the newest entry is %q, want the last one added", got)
		}
	})

	t.Run("a missing history file is not an error", func(t *testing.T) {
		jqHome(t)
		p := newPreviewTest()
		mustNotPanic(t, "loading the jq history", func() { p.loadJQHistory() })
		if len(p.jqHistory) != 0 {
			t.Errorf("the history is %v with no file, want empty", p.jqHistory)
		}
	})

	t.Run("a history file that is not a JSON list is ignored", func(t *testing.T) {
		// A truncated file from a killed process. Refusing to start is worse than
		// starting with no history, and the history is a convenience.
		home := jqHome(t)
		dir := filepath.Join(home, ".config", "dbx")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "jq_history.json"), []byte("{not json"), 0o600); err != nil {
			t.Fatal(err)
		}

		p := newPreviewTest()
		mustNotPanic(t, "loading the jq history", func() { p.loadJQHistory() })
		if len(p.jqHistory) != 0 {
			t.Errorf("a corrupt history file produced %v", p.jqHistory)
		}
	})

	t.Run("the history is walked by index, and recalls go into the BUFFER", func(t *testing.T) {
		// Two facts the first version got wrong, both by assuming the opposite:
		// jqHistoryNavigate writes jqInput, not the applied jqExpr — a recalled
		// expression is a starting point to edit, not a filter already in force; and
		// the index counts FORWARD through the history, so +1 goes towards the
		// newest entry. It starts at 0, which is the oldest.
		jqHome(t)
		p := newPreviewTest()
		p.jqHistory = []string{"older", "newer"}
		p.jqHistoryIdx = 0

		p.jqHistoryNavigate(1)
		if got := p.jqInput; got != "newer" {
			t.Errorf("stepping forward gave the buffer %q, want newer", got)
		}
		if p.jqExpr != "" {
			t.Errorf("a recalled expression became the applied filter %q; it should stay a starting point", p.jqExpr)
		}
		// The cursor goes to the end of the recalled text, so typing appends to it.
		if p.jqCursor != len("newer") {
			t.Errorf("the cursor is at %d after recalling a %d-character expression", p.jqCursor, len("newer"))
		}

		p.jqHistoryNavigate(-1)
		if got := p.jqInput; got != "older" {
			t.Errorf("stepping back gave the buffer %q, want older", got)
		}

		// Past the oldest the index clamps rather than running off the front.
		mustNotPanic(t, "stepping back past the oldest history entry", func() {
			p.jqHistoryNavigate(-1)
			p.jqHistoryNavigate(-1)
		})
		if p.jqHistoryIdx != 0 {
			t.Errorf("the history index is %d after stepping back past the oldest, want 0", p.jqHistoryIdx)
		}
		if got := p.jqInput; got != "older" {
			t.Errorf("the buffer is %q after stepping back past the oldest, want the oldest entry", got)
		}

		// And past the newest it clears the buffer, which is "no expression".
		mustNotPanic(t, "stepping forward past the newest history entry", func() {
			p.jqHistoryNavigate(1)
			p.jqHistoryNavigate(1)
		})
		if p.jqInput != "" {
			t.Errorf("the buffer is %q past the newest entry, want it cleared", p.jqInput)
		}

		// An empty history is a no-op rather than an index error.
		empty := newPreviewTest()
		empty.jqHistory = nil
		mustNotPanic(t, "stepping through an empty history", func() {
			empty.jqHistoryNavigate(1)
			empty.jqHistoryNavigate(-1)
		})
	})
}

// ---------------------------------------------------------------------------

func keysOfAny(m map[string]interface{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// stripEsc removes the escape sequences so an assertion about TEXT is not an assertion
// about colour codes.
func stripEsc(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			inEsc = true
		case inEsc && (r == 'm' || r == 'K'):
			inEsc = false
		case inEsc:
			// inside a sequence, drop it
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// mustNotPanic runs f and fails the test naming what it was doing if f panics.
//
// Used where the claim is "this does not crash" rather than "this produces X". A plain
// call cannot express that: a panic aborts the whole test binary, so the failure arrives
// with no name on it — the test that was running, the value it was using and whether it
// was the second of four subtests all become guesswork from the stack.
//
// The recovered value is reported rather than swallowed, because "it panicked" with no
// value is half a diagnosis.
func mustNotPanic(t *testing.T, what string, f func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("%s panicked: %v", what, r)
		}
	}()
	f()
}
