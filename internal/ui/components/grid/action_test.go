package grid

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/theme"
)

func newActionTestGrid(pageSize, rows int, kb config.Resolver) *Grid {
	styles := theme.Resolve("dark").Styles()
	g := New(styles, pageSize, kb)
	g.columns = []string{"c0"}
	data := make([][]interface{}, rows)
	for i := range data {
		data[i] = []interface{}{i}
	}
	g.data = &postgres.QueryResult{
		Columns: []postgres.ColumnInfo{{Name: "c0"}},
		Rows:    data,
		Count:   rows,
	}
	g.width = 40
	g.height = 20
	g.widths = []int{10}
	g.focused = true
	g.pager.SetTotalRows(rows)
	return g
}

func defaultRegistry() *config.KeybindRegistry {
	return config.NewKeybindRegistry(config.KeybindingsConfig{})
}

// Scenario: Mouse wheel scrolls through resolved navigation actions; the pane
// under the cursor reacts even when it is not focused.
func TestGrid_HandleAction_NavigateDownWorksUnfocused(t *testing.T) {
	g := newActionTestGrid(100, 5, defaultRegistry())
	g.focused = false

	if _, handled := g.HandleAction("navigate_down"); !handled {
		t.Fatal("HandleAction(navigate_down) was not handled")
	}
	if g.CursorRow() != 1 {
		t.Fatalf("cursorRow = %d, want 1", g.CursorRow())
	}
}

// Scenario: dispatching a mouse action preserves the pending-cancel side effects.
func TestGrid_HandleAction_CancelsPendingConfirmations(t *testing.T) {
	g := newActionTestGrid(100, 5, defaultRegistry())
	g.discardPending = true
	g.commitPending = true
	g.refreshPending = true

	g.HandleAction("navigate_down")

	if g.discardPending || g.commitPending || g.refreshPending {
		t.Fatalf("pending flags not cleared: discard=%v commit=%v refresh=%v",
			g.discardPending, g.commitPending, g.refreshPending)
	}
}

// Scenario: Double-click dispatches the resolved edit action.
func TestGrid_HandleAction_EditCellStartsEditing(t *testing.T) {
	g := newActionTestGrid(100, 5, defaultRegistry())

	if _, handled := g.HandleAction("edit_cell"); !handled {
		t.Fatal("HandleAction(edit_cell) was not handled")
	}
	if !g.editing {
		t.Fatal("HandleAction(edit_cell) did not start editing")
	}
}

// Scenario: Rebinding a navigation action does not change what the wheel runs,
// because the wheel dispatches by action ID rather than injecting a raw key.
func TestGrid_HandleAction_IsRebindIndependent(t *testing.T) {
	kb := config.NewKeybindRegistry(config.KeybindingsConfig{Custom: map[string]string{"navigate_down": "X"}})
	g := newActionTestGrid(100, 5, kb)

	// The old key no longer resolves, so a raw 'j' press is a no-op.
	g.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if g.CursorRow() != 0 {
		t.Fatalf("raw 'j' moved the cursor to %d after navigate_down was rebound", g.CursorRow())
	}

	// The wheel still runs the action.
	g.HandleAction("navigate_down")
	if g.CursorRow() != 1 {
		t.Fatalf("HandleAction(navigate_down) cursorRow = %d, want 1", g.CursorRow())
	}
}

// Scenario: Digit page-jump is removed; only F-keys jump pages.
func TestGrid_DigitKeyIsNoOpButFKeyJumps(t *testing.T) {
	g := newActionTestGrid(2, 10, defaultRegistry()) // 10 rows / page size 2 -> 5 pages

	g.Update(tea.KeyPressMsg{Code: '5', Text: "5"})
	if got := g.pager.Page(); got != 1 {
		t.Fatalf("a digit key jumped to page %d, want 1 (no-op)", got)
	}

	g.Update(tea.KeyPressMsg{Code: tea.KeyF5})
	if got := g.pager.Page(); got != 5 {
		t.Fatalf("f5 jumped to page %d, want 5", got)
	}
}
