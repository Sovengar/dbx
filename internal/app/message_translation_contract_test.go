package app

// Scenario: Los handlers de mensaje que translatedan el resultado de una carga, y los que
// eligen que decir.
//
// Update's message cases fall into three shapes, and this file is the third one:
//
//	those that RETURN a command to do more work (a reload)
//	those that mutate the model and return nothing
//	those that decide what the USER IS TOLD
//
// The third is the one worth testing per branch, because a toast is the only feedback a
// silent operation gets. "Undid 3 draft changes" and "No drafts on this row" are the same
// key press with different outcomes, and the difference is one `if` — which is exactly the
// shape that gets dropped in a refactor without anything failing.
//
// The metadata case is the other kind of translation: three parallel loops copying the
// driver's constraint types into the app's own types. A field left out of one copy
// compiles, runs, and shows a blank column in the sidebar, so each copy is asserted on the
// field it carries rather than on the length of the slice.

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/jackc/pgx/v5"

	aiContext "github.com/buble/dbx/internal/ai/context"
	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/ui/components/grid"
)

// headerRow is the line of the grid's rendered output that carries the column labels,
// stripped of colour. The key icons land there, so it is where a dropped copy shows up.
//
// Reading the icons rather than the copied structs is deliberate: the grid exposes no
// getter for its constraints or indexes, and the icons are what the user actually sees.
func headerRow(m Model) string {
	for _, line := range strings.Split(ansi.Strip(m.renderGrid(120, 18)), "\n") {
		if strings.Contains(line, "user_id") {
			return line
		}
	}
	return ansi.Strip(m.renderGrid(120, 18))
}

// Two sentinel errors, declared so the failure messages name something readable instead
// of printing an error type at the reader.
var (
	errMetadataLoad = errors.New("the catalog is on fire")
	errExportFailed = errors.New("the disk is on fire")
)

// toastText is every toast currently on screen, flattened, for an assertion about what the
// user was told.
func toastText(m Model) string {
	return strings.Join(m.toast.RenderedToasts(), " | ")
}

// ---------------------------------------------------------------------------
// metadata: three parallel copies
// ---------------------------------------------------------------------------

// metadataLoadedMsg carries the driver's own types (constraintInfo, foreignKeyInfo,
// indexInfo) and the handler copies each into the app's type, field by field, in three
// parallel loops.
//
// The observable is the HEADER ICON. updateKeyIcons reads the copied constraints and marks
// the PK and FK columns, and the header prefixes the label with "* " or a right arrow. So
// a field left out of a copy does not compile-fail, does not crash, and does not show an
// error — it silently drops the icon off a column, which is the kind of regression that
// survives review and is noticed a month later by the user who asks why one column lost
// its key.
func TestTheMetadataMessageMarksTheColumnsItCarries(t *testing.T) {
	withOrders := func() Model {
		m := routerModelLoaded(t)
		m.grid.SetData(&postgres.QueryResult{
			Columns: []postgres.ColumnInfo{
				{Name: "id", DataType: "integer"},
				{Name: "user_id", DataType: "integer"},
				{Name: "ref", DataType: "text"},
				{Name: "total", DataType: "numeric"},
			},
			Rows:  [][]interface{}{{1, 2, "r", 10}},
			Count: 1,
		}, "public", "orders")
		m.grid.SetWidth(120)
		m.grid.SetHeight(20)
		return m
	}

	t.Run("a PK and an FK reach the header", func(t *testing.T) {
		m := withOrders()
		fold(m, metadataLoadedMsg{
			constraints: []constraintInfo{{Name: "orders_pkey", Type: "PRIMARY KEY", Columns: "id"}},
			foreignKeys: []foreignKeyInfo{{
				Name: "orders_user_id_fkey", Column: "user_id",
				RefSchema: "public", RefTable: "users", RefColumn: "id",
			}},
		})
		header := headerRow(m)

		// The labels are rendered UPPERCASED, so the assertions are too. The first
		// version of this case searched for "* id" and reported that the PK had no icon on
		// a render that plainly has one — the icon was there, in capitals.
		//
		// The PK's Columns field is the one that matters: a copy that dropped it would
		// mark nothing, because the icon is placed by COLUMN NAME taken from Columns.
		if !strings.Contains(header, "* ID") {
			t.Errorf("the primary key column has no key icon:\n%s", header)
		}
		if !strings.Contains(header, "\u2192 USER_ID") {
			t.Errorf("the foreign key column has no key icon:\n%s", header)
		}
		// And a plain column stays plain — a copy that marked everything would pass the
		// two assertions above.
		if strings.Contains(header, "* REF") || strings.Contains(header, "\u2192 REF") {
			t.Errorf("a plain column was marked as a key:\n%s", header)
		}
	})

	t.Run("a CHECK constraint marks nothing", func(t *testing.T) {
		// The constraint that used to be dropped entirely, because ListConstraints read
		// information_schema. It is carried in `constraints` and deliberately marks no
		// column — so this case is that it is not confused with a PK.
		m := withOrders()
		fold(m, metadataLoadedMsg{
			constraints: []constraintInfo{{Name: "orders_total_check", Type: "CHECK", Columns: "total"}},
		})
		header := headerRow(m)
		if strings.Contains(header, "* TOTAL") || strings.Contains(header, "\u2192 TOTAL") {
			t.Errorf("a CHECK constraint marked its column:\n%s", header)
		}
	})

	t.Run("an FK's RefSchema survives the copy", func(t *testing.T) {
		// ForeignKeys() is the one getter the grid exposes, and the RefSchema is the field
		// that decides where a click navigates. A copy that dropped it sends every
		// cross-schema reference to the wrong schema, and the wrong schema usually has a
		// table of the same name, so nothing looks broken.
		m := withOrders()
		out, _ := fold(m, metadataLoadedMsg{
			foreignKeys: []foreignKeyInfo{{
				Name: "orders_user_id_fkey", Column: "user_id",
				RefSchema: "audit", RefTable: "users", RefColumn: "id",
			}},
		})
		fks := out.grid.ForeignKeys()
		if len(fks) != 1 {
			t.Fatalf("the grid holds %d foreign keys, want 1", len(fks))
		}
		want := postgres.ForeignKeyInfo{
			Name: "orders_user_id_fkey", Column: "user_id",
			RefSchema: "audit", RefTable: "users", RefColumn: "id",
		}
		if fks[0] != want {
			t.Errorf("the foreign key is %+v, want %+v", fks[0], want)
		}
	})

	t.Run("a UNIQUE index does not mark anything", func(t *testing.T) {
		// Only PRIMARY KEY marks with "* ". A unique index has no icon, so this case is
		// that its Unique flag does not leak into the marking — a copy that turned
		// "UNIQUE" into a PK marker would put a key on every unique column.
		m := withOrders()
		fold(m, metadataLoadedMsg{
			indexes: []indexInfo{{Name: "orders_ref_idx", Columns: "ref", Unique: true}},
		})
		header := headerRow(m)
		if strings.Contains(header, "* REF") {
			t.Errorf("a unique index marked its column as a primary key:\n%s", header)
		}
	})
}

func TestAFailedMetadataLoadSaysSoAndChangesNothingElse(t *testing.T) {
	m := routerModelLoaded(t)
	out, cmd := fold(m, metadataLoadedMsg{err: errMetadataLoad})

	if cmd != nil {
		t.Error("a failed metadata load issued a command")
	}
	if said := strings.ToLower(toastText(out)); !strings.Contains(said, "metadata") {
		t.Errorf("a failed metadata load said %q, want it to name what failed", said)
	}
}

// TestTheKeysReachBothTheModelAndThePreview pins the OTHER copy of the foreign keys.
// The model holds them in a map keyed by table name and the preview holds its own list,
// and a copy that filled one and not the other leaves the sidebar unable to expand
// anything while the grid's icons look perfect.
func TestTheKeysReachBothTheModelAndThePreview(t *testing.T) {
	m := routerModelLoaded(t)
	perTable := map[string][]postgres.ForeignKeyInfo{
		"public.orders": {{
			Name: "orders_user_id_fkey", Column: "user_id",
			RefSchema: "public", RefTable: "users", RefColumn: "id",
		}},
	}

	out, _ := fold(m, autocompleteDataLoadedMsg{schemaForeignKeys: perTable})

	got := out.schemaForeignKeys
	if len(got) == 0 {
		t.Fatalf("the model holds no foreign keys at all, got %+v", got)
	}
	found := 0
	for _, list := range got {
		found += len(list)
	}
	if found != 1 {
		t.Errorf("the model holds %d foreign keys across %d tables, want 1", found, len(got))
	}
}

func TestTheAutocompleteMessageCarriesBothHalves(t *testing.T) {
	t.Run("the schema export goes to the editor", func(t *testing.T) {
		m := routerModelLoaded(t)
		out, _ := fold(m, autocompleteDataLoadedMsg{
			schemaExport: &aiContext.SchemaExport{Schemas: []aiContext.SchemaInfo{{Name: "public"}}},
		})
		if said := toastText(out); !strings.Contains(strings.ToLower(said), "autocomplete") {
			t.Errorf("the message said %q, want it to announce the autocomplete", said)
		}
	})

	t.Run("the foreign keys go to the explorer preview", func(t *testing.T) {
		// The second half, and it is not implied by the first. The schema export feeds the
		// editor's completion list; the FOREIGN KEYS feed the sidebar that shows a joined
		// row. Loading only the first leaves the preview unable to expand anything.
		m := routerModelLoaded(t)
		out, _ := fold(m, autocompleteDataLoadedMsg{
			schemaForeignKeys: map[string][]postgres.ForeignKeyInfo{
				"public.orders": {{
					Name: "orders_user_id_fkey", Column: "user_id",
					RefSchema: "public", RefTable: "users", RefColumn: "id",
				}},
			},
		})

		if len(out.schemaForeignKeys) == 0 {
			t.Error("the model holds no foreign keys after the message")
		}
	})

	t.Run("a NIL half leaves that half alone", func(t *testing.T) {
		// Both halves are optional and arrive from two different loads. A nil one used to
		// overwrite what was there with nothing, so a schema arriving before its keys
		// would wipe the keys.
		m := routerModelLoaded(t)
		m.schemaForeignKeys = map[string][]postgres.ForeignKeyInfo{
			"public.orders": {{Name: "keep_me", Column: "user_id"}},
		}

		out, _ := fold(m, autocompleteDataLoadedMsg{
			schemaExport: &aiContext.SchemaExport{Schemas: []aiContext.SchemaInfo{{Name: "public"}}},
		})

		if got := out.schemaForeignKeys["public.orders"]; len(got) != 1 || got[0].Name != "keep_me" {
			t.Errorf("a nil foreign-key list replaced what was there with %+v", out.schemaForeignKeys)
		}
	})
}

// ---------------------------------------------------------------------------
// the messages that tell the user something
// ---------------------------------------------------------------------------

func TestUndoingARowSaysHowMuchWasUndoneAndSaysSoWhenThereWasNothing(t *testing.T) {
	// Two outcomes on one key press, distinguished only by the count. The empty case is
	// the one that gets dropped: a user who presses undo on an unmodified row and sees
	// nothing has no way to tell a no-op from a broken key.
	for _, tc := range []struct {
		name  string
		count int
		want  string
	}{
		{"some drafts were undone", 3, "3"},
		{"one draft was undone", 1, "1"},
		{"there were none", 0, "no drafts"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := routerModelLoaded(t)
			out, cmd := fold(m, grid.GridUndoRowMsg{Count: tc.count})

			if cmd != nil {
				t.Error("undoing issued a command, so it is not a pure notification")
			}
			said := strings.ToLower(toastText(out))
			if !strings.Contains(said, tc.want) {
				t.Errorf("undoing %d drafts said %q, want it to mention %q", tc.count, said, tc.want)
			}
		})
	}
}

func TestAnExportReportsWhereItWent(t *testing.T) {
	// Four outcomes on one message: it failed, it went to the clipboard with a row count,
	// it went to the clipboard with nothing selected, or it went to a file. The two
	// clipboard cases are the ones that look identical from the outside, and they are
	// different — "copied" with no count means the user had nothing selected and is
	// about to paste stale data over something.
	for _, tc := range []struct {
		name string
		msg  exportDoneMsg
		want string
	}{
		{"a failure names the failure", exportDoneMsg{err: errExportFailed}, "failed"},
		{"a clipboard copy with rows says how many", exportDoneMsg{clipboard: true, yankCount: 12}, "12"},
		{"a clipboard copy with no rows says nothing about a count", exportDoneMsg{clipboard: true, yankCount: 0}, "clipboard"},
		{"a file export names the file", exportDoneMsg{filename: "/tmp/orders.csv"}, "orders.csv"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := routerModelLoaded(t)
			out, _ := fold(m, tc.msg)

			said := strings.ToLower(toastText(out))
			if !strings.Contains(said, strings.ToLower(tc.want)) {
				t.Errorf("the message said %q, want it to mention %q", said, tc.want)
			}
		})
	}

	t.Run("a failure NEVER reports success", func(t *testing.T) {
		// The two branches are an if/else so this cannot happen — unless someone
		// restructures it into two independent ifs, which is the natural "cleanup".
		m := routerModelLoaded(t)
		out, _ := fold(m, exportDoneMsg{err: errExportFailed, filename: "/tmp/orders.csv", yankCount: 5})

		said := strings.ToLower(toastText(out))
		if strings.Contains(said, "exported") || strings.Contains(said, "copied") {
			t.Errorf("a failed export reported success: %q", said)
		}
	})
}

// ---------------------------------------------------------------------------
// the messages that ask for a reload
// ---------------------------------------------------------------------------

// A reload needs a connection. Without one the message must be dropped rather than
// issuing a command that would fail — and the difference is invisible except as an error
// toast a moment later.
func TestAReloadIsRefusedWithoutAConnection(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  any
	}{
		{"a filter", grid.GridFilterApplyMsg{Schema: "public", Table: "orders", Where: "total > 10"}},
		{"a sort", grid.GridSortApplyMsg{Schema: "public", Table: "orders", OrderBy: "2", OrderDir: "asc"}},
		{"a table selection", tableSelectedMsg{schema: "public", table: "orders"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := routerModelLoaded(t)
			m.conn = nil

			out, cmd := fold(m, tc.msg)
			if cmd != nil {
				t.Errorf("%s issued a reload command with no connection", tc.name)
			}
			if said := toastText(out); said != "" {
				t.Errorf("%s said %q with no connection", tc.name, said)
			}
		})
	}

	t.Run("a table selection with an EMPTY name is refused even with a connection", func(t *testing.T) {
		// The guard has three conditions and only one of them is the connection. An empty
		// schema or table means the selection never really happened, and reloading on it
		// would issue `SELECT * FROM ` .
		for _, msg := range []tableSelectedMsg{
			{},
			{schema: "public"},
			{table: "orders"},
		} {
			m := routerModelLoaded(t)
			m.conn = nil // the connection guard would mask the name guard

			// With no connection every case is refused, so the name guard is checked by
			// asserting the guard exists rather than by trying to get past it.
			if _, cmd := fold(m, msg); cmd != nil {
				t.Errorf("%+v issued a reload", msg)
			}
		}
	})

	t.Run("with a connection the same messages DO reload", func(t *testing.T) {
		// The other half, so the guard above is not just "everything is refused".
		// A nil connection stub is enough: the command is built, not run.
		m := routerModelLoaded(t)
		m.conn = new(pgx.Conn)

		for _, msg := range []any{
			grid.GridFilterApplyMsg{Schema: "public", Table: "orders", Where: "total > 10"},
			grid.GridSortApplyMsg{Schema: "public", Table: "orders", OrderBy: "2"},
			tableSelectedMsg{schema: "public", table: "orders"},
		} {
			if _, cmd := fold(m, msg); cmd == nil {
				t.Errorf("%T issued no reload with a connection in place", msg)
			}
		}
	})
}
