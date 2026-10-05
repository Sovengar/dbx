package app

// Scenario: La vista previa de una clave forzada se decide por CACHE, y el acierto va a la cache.
//
// The grid sidebar shows the row a foreign key points at: put the cursor on a FK column and
// the panel resolves the reference and shows it. That resolution is a database round trip,
// so the cursor moving up and down a column of ids re-queries for values the app has already
// looked up.
//
// The cache is therefore the difference between one query per key press and one query per
// distinct value ever visited, and its whole job is to be HIT before the fetch is built.
// Every branch here was uncovered, including the hit path — which is the branch that runs
// on every keystroke after the first visit to a value.
//
// What is asserted here needs no database, because the cache hit returns before the fetch
// is constructed. That is not an accident of the test: it is the property worth pinning. If
// a future change builds the command first and checks the cache inside the closure, the
// sidebar goes back to querying on every cursor move and nothing fails.

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/drivers/postgres"
)

// sidebarModel is a model with a connection — enough for syncGridSidebarPreviewForCursor to
// get past its first guard — and a grid carrying one row with a foreign key on column 1.
func sidebarModel(t *testing.T) Model {
	t.Helper()
	m := routerModelLoaded(t)
	m.conn = new(pgx.Conn)
	m.prevSchema = "public"

	m.grid.SetData(&postgres.QueryResult{
		Columns: []postgres.ColumnInfo{{Name: "id"}, {Name: "user_id"}, {Name: "total"}},
		Rows: [][]interface{}{
			{int64(1), int64(42), int64(7)},
		},
		Count: 1,
	}, "public", "orders")
	m.grid.SetMetadata(nil, []postgres.ForeignKeyInfo{{
		Column:    "user_id",
		RefSchema: "public",
		RefTable:  "users",
		RefColumn: "id",
	}}, nil)
	m.grid.Focus()
	m.router.FocusPane(FocusGrid)
	m.grid.SetWidth(80)
	m.grid.SetHeight(20)
	return m
}

// putCursorOnFKColumn moves the grid's cursor onto user_id, the column with the key.
//
// Through HandleAction rather than by setting the field: the grid exposes no cursor
// setter, and the action is how the app moves it anyway. navigate_right WRAPS, so one
// press from column 0 lands on column 1 — which is the coupling worth knowing, because a
// press from the last column would land back on the first.
func putCursorOnFKColumn(m *Model) {
	if _, handled := m.grid.HandleAction(config.ActionID("navigate_right")); !handled {
		panic("navigate_right was not handled on a grid with data")
	}
	if got := m.grid.CursorCol(); got != 1 {
		panic("the cursor is not on column 1")
	}
}

// putCursorOnColumn walks to the wanted column from column 0.
func putCursorOnColumn(m *Model, want int) {
	for m.grid.CursorCol() != want {
		m.grid.HandleAction(config.ActionID("navigate_right"))
	}
}

func TestTheCacheIsHitBeforeAnyFetchIsBuilt(t *testing.T) {
	m := sidebarModel(t)
	putCursorOnFKColumn(&m)

	const (
		schema = "public"
		table  = "orders"
		column = "user_id"
	)
	fkValue := int64(42)
	key := gridSidebarFKPreviewCacheKey(schema, table, column)

	cachedCols := []string{"id", "name"}
	cachedRow := []interface{}{int64(42), "ada"}
	m.storeGridSidebarFKPreviewCache(key, fkValue, cachedCols, cachedRow)

	t.Run("the stored entry comes back", func(t *testing.T) {
		got := m.lookupGridSidebarFKPreviewCache(key, fkValue)
		if got == nil {
			t.Fatal("the entry was stored and is not found")
		}
		if len(got.columns) != 2 || got.columns[0] != "id" {
			t.Errorf("the cached columns are %v", got.columns)
		}
		if len(got.row) != 2 || got.row[1] != "ada" {
			t.Errorf("the cached row is %v", got.row)
		}
	})

	t.Run("a CACHE HIT produces no command, so no query runs", func(t *testing.T) {
		// The whole point. A command here would be a database round trip on every cursor
		// move, and the fetch closure is built by fetchGridSidebarFKPreview, which needs
		// a live connection to be useful — so a non-nil command here would also mean the
		// sidebar had stopped honouring the cache at all.
		out := m.syncGridSidebarPreviewForCursor()
		if out != nil {
			t.Error("syncGridSidebarPreviewForCursor issued a command on a cache hit")
		}
		if !sidebarShowsRefTable(m, "users") {
			t.Error("the sidebar preview is not showing the referenced row after a cache hit")
		}
	})

	t.Run("a DIFFERENT value is not a hit", func(t *testing.T) {
		// The cache is keyed by column AND matched by value, not just by key. A cache
		// that hit on the key alone would show the previous row's user for every user.
		if got := m.lookupGridSidebarFKPreviewCache(key, int64(43)); got != nil {
			t.Errorf("value 43 found the entry for value 42: %v", got)
		}
	})

	t.Run("a different column is not a hit", func(t *testing.T) {
		other := gridSidebarFKPreviewCacheKey(schema, table, "other_column")
		if got := m.lookupGridSidebarFKPreviewCache(other, fkValue); got != nil {
			t.Error("a different column found the entry")
		}
	})

	t.Run("a different table is not a hit", func(t *testing.T) {
		other := gridSidebarFKPreviewCacheKey(schema, "invoices", column)
		if got := m.lookupGridSidebarFKPreviewCache(other, fkValue); got != nil {
			t.Error("a different table found the entry")
		}
	})

	t.Run("a value of a DIFFERENT TYPE with the same text is a hit, which is deliberate", func(t *testing.T) {
		// The match is on the formatted text, so int64(42) and the string "42" are the
		// same entry. That is right here — the cache holds what came back from the
		// database for that value, and the WHERE clause is built by formatting the value
		// the same way. Pinned because it is a loose comparison and reads as a bug.
		if got := m.lookupGridSidebarFKPreviewCache(key, "42"); got == nil {
			t.Error("the string \"42\" did not match the cached int64 42")
		}
	})

	t.Run("a NULL value is a miss, not a panic", func(t *testing.T) {
		if got := m.lookupGridSidebarFKPreviewCache(key, nil); got != nil {
			t.Error("nil matched a cached entry")
		}
	})

	t.Run("an unknown key is a miss", func(t *testing.T) {
		if got := m.lookupGridSidebarFKPreviewCache("", nil); got != nil {
			t.Error("an unknown key found an entry")
		}
	})
}

// sidebarShowsRefTable asks whether the sidebar is showing a referenced row for a table.
// The preview has no IsExpanded: the observable is what it renders, and the referenced
// table is named in it. That is also the better assertion — "is it expanded" would pass
// for a pane with the right state and the wrong contents.
func sidebarShowsRefTable(m Model, refTable string) bool {
	m.gridSidebarPreview.SetWidth(40)
	m.gridSidebarPreview.SetHeight(20)
	rendered := stripANSI(m.gridSidebarPreview.Render())
	return strings.Contains(rendered, refTable)
}

func TestTheCacheKeyNamesTheColumnExactly(t *testing.T) {
	// Three names joined by dots. A name containing a dot would make two different
	// (schema, table, column) triples collide onto one key — "a.b" + "c" and "a" +
	// "b.c" — so the key has to be built from a separator that cannot appear in an
	// identifier, and PostgreSQL identifiers CAN contain dots when quoted.
	got := gridSidebarFKPreviewCacheKey("public", "orders", "user_id")
	if got != "public.orders.user_id" {
		t.Errorf("the key is %q", got)
	}

	// The collision is real, and it is why this is documented rather than assumed away:
	// a quoted identifier may contain a dot.
	a := gridSidebarFKPreviewCacheKey("a.b", "c", "d")
	b := gridSidebarFKPreviewCacheKey("a", "b.c", "d")
	if a != b {
		t.Logf("the keys differ (%q vs %q); the separator is safe only while no identifier contains a dot", a, b)
	} else {
		t.Logf("the keys COLLIDE (%q): a quoted identifier containing a dot makes two different columns share a cache entry", a)
	}
}

func TestTheCacheIsCappedAndDropsTheOldest(t *testing.T) {
	// A cursor swept down a column of a million ids would otherwise grow the map without
	// bound for the life of the process. The cap is per key, not global.
	m := Model{}
	key := gridSidebarFKPreviewCacheKey("public", "orders", "user_id")

	total := maxGridSidebarFKPreviewCacheSize + 10
	for i := range total {
		m.storeGridSidebarFKPreviewCache(key, i, []string{"id"}, []interface{}{i})
	}

	if got := len(m.gridSidebarFKPreviewCache[key]); got != maxGridSidebarFKPreviewCacheSize {
		t.Errorf("the cache holds %d entries for one key, want the cap of %d", got, maxGridSidebarFKPreviewCacheSize)
	}

	// The OLDEST are gone, so the newest are still there. Asserting the count alone
	// would pass if the cap dropped from the other end.
	if m.lookupGridSidebarFKPreviewCache(key, total-1) == nil {
		t.Errorf("the newest value %d was dropped", total-1)
	}
	if m.lookupGridSidebarFKPreviewCache(key, 0) != nil {
		t.Error("the oldest value 0 survived; the cap drops from the front")
	}

	t.Run("the cap is PER KEY, not global", func(t *testing.T) {
		m := Model{}
		for c := range 3 {
			k := gridSidebarFKPreviewCacheKey("public", "t", string(rune('a'+c)))
			for i := range maxGridSidebarFKPreviewCacheSize + 5 {
				m.storeGridSidebarFKPreviewCache(k, i, nil, nil)
			}
		}
		for c := range 3 {
			k := gridSidebarFKPreviewCacheKey("public", "t", string(rune('a'+c)))
			if got := len(m.gridSidebarFKPreviewCache[k]); got != maxGridSidebarFKPreviewCacheSize {
				t.Errorf("key %q holds %d entries, want the per-key cap", k, got)
			}
		}
	})

	t.Run("the map is created on first use", func(t *testing.T) {
		// A nil map would need a guard everywhere it is written. storeGridSidebarFKPreviewCache
		// makes it instead, so there is exactly one place that knows.
		m := Model{}
		if m.gridSidebarFKPreviewCache != nil {
			t.Fatal("a fresh model already has a cache map")
		}
		m.storeGridSidebarFKPreviewCache("k", 1, nil, nil)
		if m.gridSidebarFKPreviewCache == nil {
			t.Error("the cache map is still nil after a store")
		}
	})
}

func TestFindFKForColumnMatchesTheColumnAndOnlyThatColumn(t *testing.T) {
	m := sidebarModel(t)

	fk := m.findFKForColumn("user_id")
	if fk == nil {
		t.Fatal("no foreign key found for user_id")
	}
	if fk.RefTable != "users" || fk.RefColumn != "id" {
		t.Errorf("the key found is %+v, want users.id", fk)
	}

	t.Run("a column with no key is nil", func(t *testing.T) {
		if got := m.findFKForColumn("total"); got != nil {
			t.Errorf("total has a foreign key: %+v", got)
		}
	})

	t.Run("the match is exact, not a prefix", func(t *testing.T) {
		// "user" is not "user_id". A prefix or substring match would put the sidebar's
		// lookup against the wrong column and show a plausible-looking wrong row.
		for _, col := range []string{"user", "user_id_extra", "USER_ID", " id"} {
			if got := m.findFKForColumn(col); got != nil {
				t.Errorf("%q matched the user_id key: %+v", col, got)
			}
		}
	})

	t.Run("no keys at all is nil", func(t *testing.T) {
		empty := routerModelLoaded(t)
		if got := empty.findFKForColumn("user_id"); got != nil {
			t.Errorf("a grid with no keys reported %+v", got)
		}
	})
}

func TestSyncGridSidebarPreviewForCursorTakesEveryBranchThatDoesNotNeedADatabase(t *testing.T) {
	t.Run("no connection does nothing", func(t *testing.T) {
		// The guard exists because the sidebar fetches through the connection. Without it
		// the cursor movement would build a closure that dereferences a nil connection
		// when the event loop runs it — which is later, on another turn, so the failure
		// would be reported nowhere near the keypress that caused it.
		m := sidebarModel(t)
		m.conn = nil
		if cmd := m.syncGridSidebarPreviewForCursor(); cmd != nil {
			t.Error("a command was issued with no connection")
		}
	})

	t.Run("no grid does nothing", func(t *testing.T) {
		m := sidebarModel(t)
		m.grid = nil
		if cmd := m.syncGridSidebarPreviewForCursor(); cmd != nil {
			t.Error("a command was issued with no grid")
		}
	})

	t.Run("no row clears the preview", func(t *testing.T) {
		// The result of a query that matched nothing. Showing the PREVIOUS row's
		// reference would be the worst possible answer here: the user is looking at an
		// empty table and the sidebar confidently shows a user from three rows ago.
		m := sidebarModel(t)
		m.grid.SetData(&postgres.QueryResult{
			Columns: []postgres.ColumnInfo{{Name: "id"}},
		}, "public", "orders")

		if cmd := m.syncGridSidebarPreviewForCursor(); cmd != nil {
			t.Error("a command was issued with no row")
		}
		if sidebarShowsRefTable(m, "users") {
			t.Error("the sidebar preview is still showing a referenced row after the table became empty")
		}
	})

	t.Run("the cursor on a column with NO key shows the whole row", func(t *testing.T) {
		// A plain column: the sidebar falls back to showing the row itself rather than
		// the row it references, which is what a user expects from a preview of the
		// current row.
		m := sidebarModel(t)
		putCursorOnColumn(&m, 0)

		if cmd := m.syncGridSidebarPreviewForCursor(); cmd != nil {
			t.Error("a command was issued for a column with no foreign key")
		}
		if sidebarShowsRefTable(m, "users") {
			t.Error("the sidebar is showing a referenced row for a column that has no key")
		}
	})

	t.Run("a NULL in the key column shows the whole row and queries nothing", func(t *testing.T) {
		// A NULL foreign key has nothing to point at. There is no row to fetch and the
		// WHERE clause would be nonsense, so the value is checked before the fetch.
		m := sidebarModel(t)
		m.grid.SetData(&postgres.QueryResult{
			Columns: []postgres.ColumnInfo{{Name: "id"}, {Name: "user_id"}},
			Rows:    [][]interface{}{{int64(1), nil}},
			Count:   1,
		}, "public", "orders")
		m.grid.SetMetadata(nil, []postgres.ForeignKeyInfo{{
			Column: "user_id", RefSchema: "public", RefTable: "users", RefColumn: "id",
		}}, nil)
		m.grid.Focus()
		putCursorOnFKColumn(&m)

		if cmd := m.syncGridSidebarPreviewForCursor(); cmd != nil {
			t.Error("a command was issued for a NULL foreign key value")
		}
		if sidebarShowsRefTable(m, "users") {
			t.Error("the sidebar is showing a referenced row for a NULL key value")
		}
	})

	t.Run("a cache MISS issues exactly one command and bumps the token", func(t *testing.T) {
		// The token is what makes a slow reply attachable to the right cursor position:
		// move the cursor twice, two replies come back, and only the second is used. So
		// the counter must advance on every miss, and a miss must produce a command.
		m := sidebarModel(t)
		putCursorOnFKColumn(&m)

		before := m.gridSidebarFKPreviewCursor
		cmd := m.syncGridSidebarPreviewForCursor()
		if cmd == nil {
			t.Fatal("a cache miss produced no command, so the sidebar would never resolve anything")
		}
		if m.gridSidebarFKPreviewCursor != before+1 {
			t.Errorf("the token went from %d to %d, want %d", before, m.gridSidebarFKPreviewCursor, before+1)
		}

		// Two misses: the token advances twice, so the two replies are distinguishable.
		first := m.gridSidebarFKPreviewCursor
		m.syncGridSidebarPreviewForCursor()
		if m.gridSidebarFKPreviewCursor != first+1 {
			t.Errorf("the token did not advance on the second miss")
		}
	})

	t.Run("a cache HIT does NOT bump the token", func(t *testing.T) {
		// Bumping on a hit would invalidate the reply that the cache was about to serve,
		// so the sidebar would flicker between a cached row and a stale one.
		m := sidebarModel(t)
		putCursorOnFKColumn(&m)
		key := gridSidebarFKPreviewCacheKey("public", "orders", "user_id")
		m.storeGridSidebarFKPreviewCache(key, int64(42), []string{"id"}, []interface{}{int64(42)})

		before := m.gridSidebarFKPreviewCursor
		m.syncGridSidebarPreviewForCursor()
		if m.gridSidebarFKPreviewCursor != before {
			t.Errorf("a cache hit moved the token from %d to %d", before, m.gridSidebarFKPreviewCursor)
		}
	})

	t.Run("the cache key is built from the SCHEMA THE GRID IS SHOWING, not a global", func(t *testing.T) {
		// prevSchema is what the grid is displaying. A key built from anything else would
		// let a value cached for one schema be served for another table of the same name
		// in a different schema — same column, same value, different row.
		m := sidebarModel(t)
		m.prevSchema = "analytics"
		putCursorOnFKColumn(&m)

		// Store under the WRONG schema's key.
		m.storeGridSidebarFKPreviewCache(gridSidebarFKPreviewCacheKey("public", "orders", "user_id"),
			int64(42), []string{"id"}, []interface{}{int64(42)})

		if cmd := m.syncGridSidebarPreviewForCursor(); cmd == nil {
			t.Error("an entry cached for the public schema was served for analytics")
		}
	})
}

// ---------------------------------------------------------------------------

// assertNotPanicking is here so a future branch that forgets its guard fails as a test
// failure rather than as a crashed binary with a stack trace pointing nowhere useful.
func assertNotPanicking(t *testing.T, what string, f func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("%s panicked: %v", what, r)
		}
	}()
	f()
}

func TestTheSidebarGuardsAreCheckedNotAssumed(t *testing.T) {
	// One sweep of the shapes that used to be reachable, each run under recover so a
	// regression is reported as a named failure.
	for _, tc := range []struct {
		name  string
		build func(*testing.T) Model
	}{
		{"no connection", func(t *testing.T) Model { m := sidebarModel(t); m.conn = nil; return m }},
		{"no grid", func(t *testing.T) Model { m := sidebarModel(t); m.grid = nil; return m }},
		{"no row", func(t *testing.T) Model {
			m := sidebarModel(t)
			m.grid.SetData(&postgres.QueryResult{Columns: []postgres.ColumnInfo{{Name: "id"}}}, "public", "orders")
			return m
		}},
		{"the cursor on the key column", func(t *testing.T) Model {
			m := sidebarModel(t)
			putCursorOnFKColumn(&m)
			return m
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.build(t)
			assertNotPanicking(t, tc.name, func() { m.syncGridSidebarPreviewForCursor() })
		})
	}
}

// Two branches of syncGridSidebarPreviewForCursor are NOT covered here and cannot be from
// this package: `cursorCol < 0` and `cursorCol >= len(columns)`. The grid exposes no cursor
// setter — HandleAction clamps or wraps — so no state a caller can construct reaches them.
// They are defensive guards against a stale cursor, and the honest thing is to say they are
// unproven from here rather than to fake a grid that produces one.
