package grid

import (
	"strings"
	"testing"

	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/theme"
)

func TestGrid_DraftSQL_Empty(t *testing.T) {
	styles := theme.Resolve("dark").Styles()
	g := New(styles, 0, nil)
	g.SetWidth(80)
	g.SetHeight(20)

	sql := g.DraftSQL()
	if sql != "" {
		t.Fatalf("DraftSQL() with no drafts = %q, want empty", sql)
	}
}

func TestGrid_DraftSQL_Insert(t *testing.T) {
	styles := theme.Resolve("dark").Styles()
	g := New(styles, 0, nil)
	g.SetWidth(80)
	g.SetHeight(20)

	g.columns = []string{"id", "name"}
	g.schema = "public"
	g.tableName = "users"
	g.data = &postgres.QueryResult{
		Columns: []postgres.ColumnInfo{{Name: "id"}, {Name: "name"}},
		Rows:    [][]interface{}{},
		Count:   0,
	}
	g.pendingRows = [][]interface{}{{1, "Alice"}}

	sql := g.DraftSQL()
	if !strings.Contains(sql, "INSERT INTO") {
		t.Fatalf("DraftSQL() for insert missing INSERT INTO: %q", sql)
	}
	if !strings.Contains(sql, "public") {
		t.Fatalf("DraftSQL() missing schema: %q", sql)
	}
	if !strings.Contains(sql, "users") {
		t.Fatalf("DraftSQL() missing table: %q", sql)
	}
}

func TestGrid_DraftSQL_Update(t *testing.T) {
	styles := theme.Resolve("dark").Styles()
	g := New(styles, 0, nil)
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
	g.pendingUpdates = []PendingUpdate{
		{RowIdx: 0, ColIdx: 1, OldValue: "Alice", NewValue: "Bob"},
	}

	sql := g.DraftSQL()
	if !strings.Contains(sql, "UPDATE") {
		t.Fatalf("DraftSQL() for update missing UPDATE: %q", sql)
	}
	if !strings.Contains(sql, "SET") {
		t.Fatalf("DraftSQL() for update missing SET: %q", sql)
	}
}

func TestGrid_DraftSQL_Delete(t *testing.T) {
	styles := theme.Resolve("dark").Styles()
	g := New(styles, 0, nil)
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
	g.pendingDeletes = []PendingDelete{
		{RowIdx: 0, Row: []interface{}{1, "Alice"}},
	}

	sql := g.DraftSQL()
	if !strings.Contains(sql, "DELETE") {
		t.Fatalf("DraftSQL() for delete missing DELETE: %q", sql)
	}
}

func TestGrid_DraftSQL_Multiple(t *testing.T) {
	styles := theme.Resolve("dark").Styles()
	g := New(styles, 0, nil)
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
	g.pendingRows = [][]interface{}{{2, "Bob"}}
	g.pendingUpdates = []PendingUpdate{
		{RowIdx: 0, ColIdx: 1, OldValue: "Alice", NewValue: "Charlie"},
	}

	sql := g.DraftSQL()
	if !strings.Contains(sql, "INSERT INTO") {
		t.Fatalf("DraftSQL() missing INSERT: %q", sql)
	}
	if !strings.Contains(sql, "UPDATE") {
		t.Fatalf("DraftSQL() missing UPDATE: %q", sql)
	}
}
