package grid

import (
	"reflect"
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

// Regression: editing two columns of the same row must produce a single
// UPDATE. Emitting one statement per column reuses the original WHERE for
// both, so the first statement (SET id) invalidates the WHERE of the second
// (WHERE id = <original>) and the remaining columns silently never update.
func TestGrid_DraftSQL_MultipleColumnsSameRow_SingleUpdate(t *testing.T) {
	styles := theme.Resolve("dark").Styles()
	g := New(styles, 0, nil)
	g.SetWidth(80)
	g.SetHeight(20)

	g.columns = []string{"id", "name"}
	g.schema = "vsocial_integraciones"
	g.tableName = "pruebas_jonathan"
	oldRow := []interface{}{"11", "11"}
	g.data = &postgres.QueryResult{
		Columns: []postgres.ColumnInfo{{Name: "id"}, {Name: "name"}},
		Rows:    [][]interface{}{oldRow},
		Count:   1,
	}
	g.pendingUpdates = []PendingUpdate{
		{RowIdx: 0, ColIdx: 0, OldValue: "11", NewValue: "12", OldRow: oldRow},
		{RowIdx: 0, ColIdx: 1, OldValue: "11", NewValue: "12", OldRow: oldRow},
	}

	sql := g.DraftSQL()
	if got := strings.Count(sql, "UPDATE"); got != 1 {
		t.Fatalf("DraftSQL() produced %d UPDATE statements, want 1: %q", got, sql)
	}
	if !strings.Contains(sql, `SET "id" = '12', "name" = '12'`) {
		t.Fatalf("DraftSQL() must assign both columns in one SET: %q", sql)
	}
	if !strings.Contains(sql, `WHERE "id" = '11' AND "name" = '11'`) {
		t.Fatalf("DraftSQL() WHERE must keep original values: %q", sql)
	}
}

func TestGrid_DraftSQL_MultipleRows_SeparateUpdates(t *testing.T) {
	styles := theme.Resolve("dark").Styles()
	g := New(styles, 0, nil)
	g.SetWidth(80)
	g.SetHeight(20)

	g.columns = []string{"id", "name"}
	g.schema = "public"
	g.tableName = "users"
	g.data = &postgres.QueryResult{
		Columns: []postgres.ColumnInfo{{Name: "id"}, {Name: "name"}},
		Rows:    [][]interface{}{{"1", "Alice"}, {"2", "Bob"}},
		Count:   2,
	}
	g.pendingUpdates = []PendingUpdate{
		{RowIdx: 0, ColIdx: 1, OldValue: "Alice", NewValue: "Ann", OldRow: []interface{}{"1", "Alice"}},
		{RowIdx: 1, ColIdx: 1, OldValue: "Bob", NewValue: "Ben", OldRow: []interface{}{"2", "Bob"}},
	}

	sql := g.DraftSQL()
	if got := strings.Count(sql, "UPDATE"); got != 2 {
		t.Fatalf("DraftSQL() produced %d UPDATE statements, want 2 (one per row): %q", got, sql)
	}
}

// Regression: CommitAllDrafts must group a row's edits into one parameterized
// statement, with SET args first (in edit order) followed by the WHERE args.
func TestGrid_CommitAllDrafts_MultipleColumnsSameRow(t *testing.T) {
	styles := theme.Resolve("dark").Styles()
	g := New(styles, 0, nil)
	g.SetWidth(80)
	g.SetHeight(20)

	g.columns = []string{"id", "name"}
	g.schema = "public"
	g.tableName = "users"
	oldRow := []interface{}{"11", "Alice"}
	g.data = &postgres.QueryResult{
		Columns: []postgres.ColumnInfo{{Name: "id"}, {Name: "name"}},
		Rows:    [][]interface{}{oldRow},
		Count:   1,
	}
	g.pendingUpdates = []PendingUpdate{
		{RowIdx: 0, ColIdx: 0, OldValue: "11", NewValue: "22", OldRow: oldRow},
		{RowIdx: 0, ColIdx: 1, OldValue: "Alice", NewValue: "Bob", OldRow: oldRow},
	}

	cmd := g.CommitAllDrafts()
	if cmd == nil {
		t.Fatal("CommitAllDrafts() returned nil command")
	}
	msg, ok := cmd().(GridCommitAllMsg)
	if !ok {
		t.Fatalf("CommitAllDrafts() command produced %T, want GridCommitAllMsg", cmd())
	}
	if len(msg.Queries) != 1 {
		t.Fatalf("CommitAllDrafts() produced %d queries, want 1: %v", len(msg.Queries), msg.Queries)
	}
	wantQuery := `UPDATE "public"."users" SET "id" = $1, "name" = $2 WHERE "id" = $3 AND "name" = $4`
	if msg.Queries[0] != wantQuery {
		t.Fatalf("query = %q, want %q", msg.Queries[0], wantQuery)
	}
	wantArgs := []interface{}{"22", "Bob", "11", "Alice"}
	if len(msg.Args) != 1 || !reflect.DeepEqual(msg.Args[0], wantArgs) {
		t.Fatalf("args = %v, want %v", msg.Args, wantArgs)
	}
}
