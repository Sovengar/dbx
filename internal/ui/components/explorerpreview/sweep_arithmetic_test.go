package explorerpreview

// Scenario: Los supervivientes no-Boundary del sweep de explorerpreview.
//
// Every test here kills a mutant that survived a full run, and two of them are about
// ABSOLUTE assertions: the pre-existing tab-bar test derives its expected styles from
// the same render it asserts, so a mutant that inverts the comparison is invisible to
// it. The highlight has to be asserted against the THEME's own style bytes, not against
// another render of the same model.
//
// The fixtures were measured with the probe helpers ported below (dbgTall, dbgShape):
// their numbers are stated in each test so the next reader can re-measure instead of
// trusting.

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/debuglog"
	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/theme"
)

// dbgTall builds a FOCUSED panel whose ERE diagram is a centre table with nFKs foreign
// keys and no incoming relationships, at the given height. Measured with height 14:
// contentHeight 68, pane 14, MaxScroll 54 — the tall fixture the viewport tests need.
func dbgTall(t *testing.T, nFKs, height int) *ExplorerPreview {
	t.Helper()
	fks := make([]postgres.ForeignKeyInfo, nFKs)
	cols := make([]postgres.ColumnInfo, nFKs)
	for i := range fks {
		name := string(rune('a' + i))
		cols[i] = col("col_"+name, "integer")
		fks[i] = postgres.ForeignKeyInfo{
			Name: "orders_fk_" + name, Column: "col_" + name,
			RefSchema: "public", RefTable: "t_" + name, RefColumn: "id",
		}
	}
	e := New(theme.Resolve("dark").Styles(), config.NewKeybindRegistry(config.KeybindingsConfig{}))
	e.SetWidth(70)
	e.SetHeight(height)
	e.Focus()
	e.SetData("public", "orders", cols, nil, fks, nil, nil)
	e.setActiveTab(5)
	return e
}

// dbgShape builds the same panel with an independent centre column count and FK count,
// for fixtures where the content-height ESTIMATE has to sit on one side or the other of
// the pane. Measured with centerCols=3, outFKs=1, height=12: the diagram renders 13
// lines, the estimate is 14 (MaxScroll 2 dead), the mutated estimate 12 (MaxScroll 0).
func dbgShape(t *testing.T, centerCols, outFKs, height int) *ExplorerPreview {
	t.Helper()
	fks := make([]postgres.ForeignKeyInfo, outFKs)
	cols := make([]postgres.ColumnInfo, centerCols)
	for i := range fks {
		name := string(rune('a' + i))
		fks[i] = postgres.ForeignKeyInfo{
			Name: "orders_fk_" + name, Column: "col_" + name,
			RefSchema: "public", RefTable: "t_" + name, RefColumn: "id",
		}
	}
	for i := range cols {
		name := string(rune('a' + i%26))
		cols[i] = col("c"+string(rune('0'+i/26))+name, "integer")
	}
	e := New(theme.Resolve("dark").Styles(), config.NewKeybindRegistry(config.KeybindingsConfig{}))
	e.SetWidth(70)
	e.SetHeight(height)
	e.Focus()
	e.SetData("public", "orders", cols, nil, fks, nil, nil)
	e.setActiveTab(5)
	return e
}

// tabStyleSgr is the escape sequence a theme style puts in effect immediately before
// its own text — the same extraction tabSgr applies to a render, so the two are
// comparable byte for byte.
func tabStyleSgr(style lipgloss.Style) string {
	rendered := style.Render("x")
	at := strings.Index(rendered, "x")
	if at < 0 {
		return ""
	}
	before := rendered[:at]
	end := strings.LastIndex(before, "m")
	if end < 0 {
		return ""
	}
	start := strings.LastIndex(before[:end], "\x1b")
	if start < 0 {
		return ""
	}
	return before[start : end+1]
}

// Scenario: El resaltado de la pestaña activa ES el estilo del tema, no "distinto".
//
// Kills CONDITIONALS_NEGATION tabbar.go:89 (`i == t.active` -> `!=`): inverted, every
// tab BUT the active one carries TabActive. TestOnlyTheActiveTabIsHighlighted cannot
// see this — it reads both styles back off the same render, so under the mutant
// "activeStyle" is simply TabInactive and the crossed branches stay consistent with
// themselves. The theme's own bytes cannot be fooled that way.
func TestTheActiveTabCarriesTheThemeStylesOwnHighlight(t *testing.T) {
	tb := newTabBar(t)

	activeSig := tabStyleSgr(tb.styles.TabActive)
	inactiveSig := tabStyleSgr(tb.styles.TabInactive)
	if activeSig == "" || inactiveSig == "" {
		t.Fatalf("no styling found: active %q inactive %q", activeSig, inactiveSig)
	}
	if activeSig == inactiveSig {
		t.Fatalf("the theme styles an active and an inactive tab identically: %q", activeSig)
	}

	for _, active := range []int{0, 2, 5} {
		tb.SetActive(active)
		for i := range tb.TabCount() {
			want := inactiveSig
			if i == active {
				want = activeSig
			}
			if got := tabSgr(tb.Render(), i, tb); got != want {
				t.Errorf("with tab %d active, tab %d (%s) carries %q, want the theme's %q",
					active, i, tb.tabs[i].ID, got, want)
			}
		}
	}
}

// Scenario: G en un diagrama cuyo ESTIMATE queda a un lado del pane.
//
// Kills ARITHMETIC_BASE preview.go:154 (`7 + 1 + colHeight` -> the estimate minus 2,
// MaxScroll 0 instead of 2). Measured fixture: centerCols=3, outFKs=1, height=12 — the
// diagram renders 13 lines against a 12-line pane. Dead: G lands on MaxScroll 2, which
// the renderer clamps to the last screenful, so the pane shows the diagram's LAST line.
// Mutant: MaxScroll is 0, G is a no-op and the pane shows the FIRST screenful.
func TestGoingToTheBottomShowsTheLastScreenful(t *testing.T) {
	e := dbgShape(t, 3, 1, 12)
	if got := e.ereViewport.MaxScroll(); got != 2 {
		t.Fatalf("the fixture's MaxScroll is %d, want 2 — the estimate drifted, re-measure", got)
	}

	e.Update(tea.KeyPressMsg{Code: 'G', Text: "G"})

	if got := e.ereViewport.ScrollOffset; got != 2 {
		t.Fatalf("G left the scroll at %d, want the MaxScroll of 2", got)
	}
	full := strings.Split(RenderERDiagram(*e.ereDiagram, e.width, e.height, e.ereNav), "\n")
	want := strings.Join(full[len(full)-e.ereViewport.PaneHeight:], "\n")
	if got := e.renderERE(); got != want {
		t.Errorf("after G the pane is not the last screenful:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// Scenario: G y g en un diagrama MÁS ALTO que el pane.
//
// Kills CONDITIONALS_NEGATION preview.go:213 (the `ereViewport != nil` guard of the
// "g" case -> `== nil`, which skips JumpTop). The pre-existing "g and G jump" test
// cannot see it: its fixture has MaxScroll 0, so skipping the jump still leaves the
// scroll at 0. dbgTall(10, 14) has MaxScroll 54 — G puts the scroll at the bottom, and
// "g" has somewhere to come BACK from.
func TestJumpingBackToTheTopFromTheBottomOfATallDiagram(t *testing.T) {
	e := dbgTall(t, 10, 14)
	if got := e.ereViewport.MaxScroll(); got != 54 {
		t.Fatalf("the fixture's MaxScroll is %d, want 54 — re-measure", got)
	}

	e.Update(tea.KeyPressMsg{Code: 'G', Text: "G"})
	if got := e.ereViewport.ScrollOffset; got != e.ereViewport.MaxScroll() {
		t.Fatalf("G left the scroll at %d, want the bottom %d", got, e.ereViewport.MaxScroll())
	}

	e.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	if got := e.ereViewport.ScrollOffset; got != 0 {
		t.Errorf("g left the scroll at %d, want 0", got)
	}
}

// Scenario: Enter devuelve el scroll al principio aunque el rebuild no llegue a pasar.
//
// Kills CONDITIONALS_NEGATION preview.go:234 (the `ereViewport != nil` guard around
// the Reset of the enter case -> `== nil`). On the normal path the Reset is invisible —
// buildEREDiagram has just built a FRESH viewport, already at 0 — so the fixture has to
// take the path where it is not: a relationship whose target resolves to no table, so
// buildEREDiagram returns early and the OLD viewport, scroll and all, is the one Enter
// hands back. Dead: the Reset parks it at 0. Mutant: the stale scroll survives.
func TestEnterResetsTheScrollEvenWhenTheRebuildDoesNotRun(t *testing.T) {
	e := dbgTall(t, 10, 14)
	e.Update(tea.KeyPressMsg{Code: 'G', Text: "G"})
	if got := e.ereViewport.ScrollOffset; got != e.ereViewport.MaxScroll() || got == 0 {
		t.Fatalf("fixture precondition failed: the scroll is %d, want the bottom (> 0)", got)
	}

	// A relationship to "no table at all": the navigation still fires, but the rebuild
	// has nothing to centre on and leaves the old diagram — and the old viewport.
	e.ereDiagram.Outgoing[0].ToTable = ""
	e.ereNav.MoveRight()
	e.ereNav.MoveDown()
	if !e.ereNav.HasSelection() {
		t.Fatal("fixture precondition failed: no selection to enter")
	}

	if _, handled := e.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); !handled {
		t.Fatal("enter on a selected relationship was not handled")
	}
	if got := e.ereViewport.ScrollOffset; got != 0 {
		t.Errorf("enter left the stale scroll at %d, want 0", got)
	}
}

// Scenario: subir la selección DENTRO de la ventana no mueve el scroll.
//
// Kills ARITHMETIC_BASE preview.go:336 (`selLine >= ScrollOffset+PaneHeight` ->
// `ScrollOffset-PaneHeight`: the below-window test fires while the selection is still
// inside the window and the scroll jumps to follow it). Measured on dbgTall(10, 14):
// moving right and five downs puts the selection on line 37 with offset 24, window
// [24,38). One up moves it to line 32 — still visible. Dead: `32 >= 38` is false and
// the offset stays 24. Mutant: `32 >= 10` is true and the offset becomes 32-14+1=19.
func TestMovingUpInsideTheWindowLeavesTheScrollAlone(t *testing.T) {
	e := dbgTall(t, 10, 14)
	e.ereNav.MoveRight()
	for range 5 {
		e.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if got := e.ereViewport.ScrollOffset; got != 24 {
		t.Fatalf("the fixture's scroll after five downs is %d, want 24 — re-measure", got)
	}
	if got := e.ereSelectionLine(e.ereNav.ActiveRow()); got != 37 {
		t.Fatalf("the fixture's selection is on line %d, want 37 — re-measure", got)
	}

	e.Update(tea.KeyPressMsg{Code: tea.KeyUp})

	if got := e.ereViewport.ScrollOffset; got != 24 {
		t.Errorf("the up-arrow inside the visible window moved the scroll to %d, want it left at 24", got)
	}
}

// Scenario: el log de debug cuenta las columns PK/FK del centro, hacia arriba.
//
// Kills INCREMENT_DECREMENT preview.go:509 (`centerColsPKFK++` -> `--`: the count goes
// negative). The counter is only reachable from tests, and the debug log it feeds is
// the observable: DefaultDir is redirected into the test's own temp dir and the
// reported pkfkCols is read back from the file.
func TestTheDebugLogCountsTheCentrePKAndFKColumns(t *testing.T) {
	dir := t.TempDir()
	old := debuglog.DefaultDir
	debuglog.DefaultDir = dir
	t.Cleanup(func() { debuglog.DefaultDir = old })

	d := ERDiagram{
		Center: TableBox{Name: "orders", Columns: []ColumnBadge{
			{Name: "id", DataType: "integer", IsPK: true},
			{Name: "note", DataType: "text"},
			{Name: "customer_id", DataType: "integer", IsFK: true},
			{Name: "region_id", DataType: "integer", IsPK: true, IsFK: true},
			{Name: "created_at", DataType: "timestamptz"},
		}},
		Outgoing: []Relationship{{FromColumn: "customer_id", ToTable: "customers"}},
	}
	debugLogERE(&d, 1)

	data, err := os.ReadFile(debuglog.Path("ere"))
	if err != nil {
		t.Fatalf("the log was not written: %v", err)
	}
	if !strings.Contains(string(data), "pkfkCols=3 outgoing=1") {
		t.Errorf("the log does not report the three PK/FK columns: %q", data)
	}
}
