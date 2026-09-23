package grid

import (
	"testing"

	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/theme"
)

// Regression: with drafts, the first refresh arms the confirmation and the
// second refresh actually emits the reload. The app delegates to Refresh(), so
// this two-step flow must live in the grid.
func TestGrid_Refresh_TwoStepWithDrafts(t *testing.T) {
	g := New(theme.Resolve("dark").Styles(), 0, nil)
	g.SetWidth(80)
	g.SetHeight(20)
	g.columns = []string{"id", "name"}
	g.schema = "public"
	g.tableName = "users"
	g.data = &postgres.QueryResult{
		Columns: []postgres.ColumnInfo{{Name: "id"}, {Name: "name"}},
		Rows:    [][]interface{}{{1, "Alice"}},
		Count:   1,
	}
	g.pendingUpdates = []PendingUpdate{{RowIdx: 0, ColIdx: 1, OldValue: "Alice", NewValue: "Bob"}}

	// First press: arm the pending confirmation, no reload yet.
	cmd, handled := g.Refresh()
	if !handled {
		t.Fatal("first Refresh() with drafts was not handled")
	}
	if cmd != nil {
		t.Fatal("first Refresh() with drafts must not reload yet")
	}
	if !g.IsRefreshPending() {
		t.Fatal("first Refresh() with drafts did not arm the pending confirmation")
	}

	// Second press: emit the reload message.
	cmd, handled = g.Refresh()
	if !handled || cmd == nil {
		t.Fatalf("second Refresh() = handled=%v cmd=%v, want a reload command", handled, cmd)
	}
	msg, ok := cmd().(GridRefreshConfirmMsg)
	if !ok {
		t.Fatalf("second Refresh() produced %T, want GridRefreshConfirmMsg", cmd())
	}
	if msg.Schema != "public" || msg.Table != "users" {
		t.Fatalf("reload message = %+v, want public.users", msg)
	}
	if g.IsRefreshPending() {
		t.Fatal("pending confirmation still armed after the second press")
	}
}

// Without drafts a single refresh reloads immediately.
func TestGrid_Refresh_NoDraftsReloadsImmediately(t *testing.T) {
	g := New(theme.Resolve("dark").Styles(), 0, nil)
	g.SetWidth(80)
	g.SetHeight(20)
	g.columns = []string{"id"}
	g.schema = "public"
	g.tableName = "users"
	g.data = &postgres.QueryResult{
		Columns: []postgres.ColumnInfo{{Name: "id"}},
		Rows:    [][]interface{}{{1}},
		Count:   1,
	}

	cmd, handled := g.Refresh()
	if !handled || cmd == nil {
		t.Fatalf("Refresh() without drafts = handled=%v cmd=%v, want a reload command", handled, cmd)
	}
	if _, ok := cmd().(GridRefreshConfirmMsg); !ok {
		t.Fatalf("Refresh() produced %T, want GridRefreshConfirmMsg", cmd())
	}
	if g.IsRefreshPending() {
		t.Fatal("Refresh() armed a pending confirmation without drafts")
	}
}
