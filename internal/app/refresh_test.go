package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/ui/components/grid"
)

// Regression: the app must delegate the refresh key to the grid so the drafts
// confirmation flow stays intact (H1).
func TestApp_RefreshKeyDelegatesToGrid(t *testing.T) {
	m := newModelWithGrid(t)
	m.grid.SetData(&postgres.QueryResult{
		Columns: []postgres.ColumnInfo{{Name: "id"}},
		Rows:    [][]interface{}{{1}},
		Count:   1,
	}, "public", "users")
	m.router.FocusPane(FocusGrid)

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if cmd == nil {
		t.Fatal("pressing r in the grid did not return a reload command")
	}
	if _, ok := cmd().(grid.GridRefreshConfirmMsg); !ok {
		t.Fatalf("pressing r produced %T, want grid.GridRefreshConfirmMsg", cmd())
	}
}
