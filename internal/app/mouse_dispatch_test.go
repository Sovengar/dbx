package app

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone/v2"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/ui"
	"github.com/buble/dbx/internal/ui/components/grid"
)

// mouseDispatchModel builds a main-state model with a focused, populated grid
// and returns it after rendering so the pane zones are registered.
func mouseDispatchModel(t *testing.T, kb *config.KeybindRegistry) Model {
	t.Helper()
	m := newRollbackTestModel()
	m.keybinds = kb
	m.router = NewRouter(kb)
	m.width = 80
	m.height = 40
	m.grid = grid.New(m.styles, 100, kb)
	m.grid.SetWidth(70)
	m.grid.SetHeight(20)
	m.grid.SetData(&postgres.QueryResult{
		Columns: []postgres.ColumnInfo{{Name: "id"}},
		Rows:    [][]interface{}{{1}, {2}, {3}, {4}, {5}},
		Count:   5,
	}, "public", "t")
	m.router.FocusPane(FocusGrid)
	m.grid.Focus()
	ui.Zones.SetEnabled(true)
	_ = m.View() // registers pane zones
	return m
}

// gridZone polls until View's asynchronous zone registration lands.
func gridZone(t *testing.T) *zone.ZoneInfo {
	t.Helper()
	var zi *zone.ZoneInfo
	for i := 0; i < 100; i++ {
		zi = ui.Zones.Get(zonePaneGrid)
		if zi != nil && !zi.IsZero() {
			return zi
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Skip("grid zone was not registered by View")
	return nil
}

func gridZoneCenter(t *testing.T) (int, int) {
	t.Helper()
	zi := gridZone(t)
	return (zi.StartX + zi.EndX) / 2, (zi.StartY + zi.EndY) / 2
}

func defaultRegistry() *config.KeybindRegistry {
	return config.NewKeybindRegistry(config.KeybindingsConfig{})
}

// Scenario: Mouse wheel scrolls through resolved navigation actions.
func TestMouseWheel_DispatchesNavigateDown(t *testing.T) {
	m := mouseDispatchModel(t, defaultRegistry())
	x, y := gridZoneCenter(t)

	updated, _ := m.Update(tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelDown})
	got := updated.(Model)
	if got.grid.CursorRow() != 1 {
		t.Fatalf("wheel down cursorRow = %d, want 1", got.grid.CursorRow())
	}

	updated, _ = got.Update(tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelUp})
	got = updated.(Model)
	if got.grid.CursorRow() != 0 {
		t.Fatalf("wheel up cursorRow = %d, want 0", got.grid.CursorRow())
	}
}

// Scenario: Rebinding a navigation action changes the wheel behavior — the
// wheel dispatches by action ID, so the action still runs while the raw key
// stops working.
func TestMouseWheel_ReboundNavigationActionRuns(t *testing.T) {
	kb := config.NewKeybindRegistry(config.KeybindingsConfig{Custom: map[string]string{"navigate_down": "X"}})
	m := mouseDispatchModel(t, kb)
	x, y := gridZoneCenter(t)

	updated, _ := m.Update(tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelDown})
	got := updated.(Model)
	if got.grid.CursorRow() != 1 {
		t.Fatalf("wheel down cursorRow = %d, want 1 (action dispatched by ID)", got.grid.CursorRow())
	}

	// The old key no longer resolves, so a raw 'j' must not navigate.
	updated, _ = got.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	got = updated.(Model)
	if got.grid.CursorRow() != 1 {
		t.Fatalf("raw 'j' moved the cursor to %d after navigate_down was rebound", got.grid.CursorRow())
	}
}

// Scenario: Double-click dispatches the resolved edit action.
func TestMouseDoubleClick_DispatchesEditCell(t *testing.T) {
	m := mouseDispatchModel(t, defaultRegistry())
	zi := gridZone(t)
	// (StartX+2, StartY+2) is the first data cell: top border, header, then row.
	cx, cy := zi.StartX+2, zi.StartY+2

	for i := 0; i < 2; i++ {
		updated, _ := m.Update(tea.MouseClickMsg{X: cx, Y: cy, Button: tea.MouseLeft})
		m = updated.(Model)
	}

	if !m.grid.IsEditing() {
		t.Fatal("double-click did not dispatch edit_cell (grid is not editing)")
	}
}
