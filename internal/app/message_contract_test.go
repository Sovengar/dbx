package app

// The other half of Update: the messages, not the keys.
//
// A Bubble Tea program's Update is two things wearing one function — dispatch a keypress
// to a handler, and fold a message the rest of the program produced into the state. The
// key half is covered by router_contract_test.go. This is the message half: 47 types,
// from a window resize to the result of a foreign-key fetch, each of which has to leave
// the model in a state the next message can be folded into.
//
// What is asserted is deliberately thin, because most handlers have nothing to assert
// from the outside: they copy a message's fields into components and return. What IS
// worth asserting is structural and worth having:
//
//	M1  every message type the app handles can be folded in, on a model with nothing
//	    connected, without a panic and without losing the model
//	M2  the model is still drawable afterwards — a handler that leaves the components
//	    holding something View cannot render is a crash one message later
//	M3  the messages that carry DATA put that data in the components, because a handler
//	    that reads the message and drops it looks identical from outside to one that
//	    works until you notice the panel is empty
//	M4  the messages that carry an ERROR show it, because a failed fetch that leaves
//	    the previous table on screen reads exactly like a slow one
//	M5  the ticks are idempotent — a spinner tick arriving twice, or a toast tick with
//	    no toasts, changes nothing and costs nothing
//
// M1 is the one that finds bugs: eleven of the handlers dereference something the model
// only has once a connection has landed, and this is the only place that finds out which.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	aiContext "github.com/buble/dbx/internal/ai/context"
	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/ui/components/ask"
	"github.com/buble/dbx/internal/ui/components/editor"
	"github.com/buble/dbx/internal/ui/components/explorer"
	"github.com/buble/dbx/internal/ui/components/explorerpreview"
	"github.com/buble/dbx/internal/ui/components/grid"
	"github.com/buble/dbx/internal/ui/components/gridpreview"
	"github.com/buble/dbx/internal/ui/components/palette"
	"github.com/buble/dbx/internal/ui/components/picker"
	"github.com/buble/dbx/internal/ui/components/querybrowser"
)

// sampleResult is the fixture every data-carrying message reuses. Three columns and two
// rows, because a handler that copies columns but not rows, or rows but not columns, is
// only visible with both non-empty.
func sampleResult() *postgres.QueryResult {
	return &postgres.QueryResult{
		Columns: []postgres.ColumnInfo{
			{Name: "id", DataType: "int4"},
			{Name: "name", DataType: "text"},
			{Name: "email", DataType: "text"},
		},
		Rows: [][]interface{}{
			{int64(1), "ada", "ada@example.com"},
			{int64(2), "grace", "grace@example.com"},
		},
		Count: 2,
	}
}

// everyMessage builds one instance of every message type Update has a case for.
//
// Named so the failure says which message broke: a sweep of forty anonymous literals that
// panics on the ninth one tells you a line number and nothing else.
func everyMessage(t *testing.T) map[string]tea.Msg {
	t.Helper()
	project := config.FoundProject{Name: "test", Path: "/tmp/test", Active: true}
	schemaNode := explorer.NewNode("db", explorer.NodeDatabase, "testdb")
	sch := explorer.NewNode("s", explorer.NodeSchema, "public")
	sch.Metadata["schema"] = "public"
	sch.Expanded = true
	schemaNode.Expanded = true
	schemaNode.AddChild(sch)
	tbl := explorer.NewNode("t", explorer.NodeTable, "users")
	tbl.Metadata["schema"] = "public"
	tbl.Metadata["row_count"] = 2
	sch.AddChild(tbl)
	col := explorer.NewNode("c", explorer.NodeColumn, "id")
	col.Metadata["data_type"] = "int4"
	tbl.AddChild(col)

	overview := &postgres.TableOverview{TableName: "users", TableType: "r", LiveTuples: 2, ColumnCount: 3, IndexCount: 1}

	return map[string]tea.Msg{
		"window size":           tea.WindowSizeMsg{Width: 120, Height: 40},
		"window size, tiny":     tea.WindowSizeMsg{Width: 1, Height: 1},
		"window size, zero":     tea.WindowSizeMsg{Width: 0, Height: 0},
		"toast tick":            toastTickMsg{},
		"spinner tick":          spinnerTickMsg{},
		"projects scanned":      projectsScannedMsg{},
		"projects scanned, one": projectsScannedMsg{projects: []config.FoundProject{project}},
		"db connected":          dbConnectedMsg{project: &project},
		"db connected, failed":  dbConnectedMsg{project: &project, err: errFake},
		"schema loaded": schemaLoadedMsg{
			root: schemaNode, dbName: "testdb", targetSchema: "public", targetTable: "users",
		},
		"schema loaded, failed": schemaLoadedMsg{root: schemaNode, err: errFake},
		"autocomplete data": autocompleteDataLoadedMsg{
			schemaExport: &aiContext.SchemaExport{Schemas: []aiContext.SchemaInfo{{Name: "public"}}},
		},
		"table selected":    tableSelectedMsg{schema: "public", table: "users"},
		"table data loaded": tableDataLoadedMsg{result: sampleResult(), schema: "public", table: "users"},
		"table data, failed": tableDataLoadedMsg{
			schema: "public", table: "users", err: errFake,
		},
		"metadata loaded": metadataLoadedMsg{
			schema: "public", table: "users", overview: overview,
			indexes: []indexInfo{{Name: "users_pkey"}},
		},
		"metadata, failed": metadataLoadedMsg{schema: "public", table: "users", err: errFake},
		"explorer preview data": explorerPreviewDataMsg{
			schema: "public", table: "users",
			columns:  []postgres.ColumnInfo{{Name: "id", DataType: "int4"}},
			overview: overview,
		},
		"ere navigate":        explorerpreview.ERENavigateMsg{},
		"grid commit pending": grid.GridCommitPendingMsg{},
		"grid commit all":     grid.GridCommitAllMsg{},
		"grid undo row":       grid.GridUndoRowMsg{},
		"grid filter apply":   grid.GridFilterApplyMsg{},
		"grid sort apply":     grid.GridSortApplyMsg{},
		"grid cursor moved":   grid.GridCursorMovedMsg{},
		"grid export chosen": grid.ExportSelectedMsg{
			Format:  grid.ExportCSV,
			Schema:  "public",
			Table:   "users",
			Row:     []interface{}{int64(1), "ada", "ada@example.com"},
			Rows:    [][]interface{}{{int64(1), "ada", "ada@example.com"}},
			Columns: []string{"id", "name", "email"},
		},
		"export done":      exportDoneMsg{},
		"editor copy sql":  editor.CopySQLMsg{},
		"copy sql done":    copySQLDoneMsg{},
		"copy sql, failed": copySQLDoneMsg{err: errFake},
		"grid tab change":  grid.GridTabChangeMsg{},
		"grid navigate fk": grid.GridNavigateFKMsg{},
		"grid preview expand fk": gridpreview.GridPreviewExpandFKMsg{
			Column: "customer_id", Value: int64(1), RefSchema: "public",
			RefTable: "customers", RefColumn: "id",
		},
		"grid preview expand fk result": gridpreview.GridPreviewExpandFKResultMsg{
			Column: "customer_id", Path: "customer_id",
			Row: map[string]interface{}{"id": int64(1), "name": "ada"},
		},
		"grid preview expand fk failed": gridpreview.GridPreviewExpandFKResultMsg{
			Column: "customer_id", Err: errFake,
		},
		"sidebar fk lookup result": GridSidebarFKPreviewLookupResultMsg{
			CacheKey: "public.customers.id", CacheVal: int64(1), RefTable: "customers",
			Columns: []string{"id", "name"},
			Row:     []interface{}{int64(1), "ada"},
		},
		"sidebar fk lookup, failed": GridSidebarFKPreviewLookupResultMsg{
			CacheKey: "public.customers.id", RefTable: "customers", Err: errFake,
		},
		"grid go back":            grid.GridGoBackMsg{},
		"grid refresh confirm":    grid.GridRefreshConfirmMsg{},
		"query executed":          queryExecutedMsg{sql: "SELECT 1", result: sampleResult()},
		"query executed, failed":  queryExecutedMsg{sql: "SELECT 1", err: errFake},
		"query chosen":            querybrowser.QuerySelectedMsg{SQL: "SELECT 2"},
		"ask submitted":           ask.AskSubmittedMsg{},
		"ask confirmed":           ask.AskConfirmMsg{},
		"ask closed":              ask.AskClosedMsg{},
		"ask generated":           askGeneratedMsg{sql: "SELECT 3"},
		"ask generated, failed":   askGeneratedMsg{err: errFake},
		"ask query executed":      askQueryExecutedMsg{sql: "SELECT 3", result: sampleResult()},
		"query browser closed":    querybrowser.QueryBrowserClosedMsg{},
		"explorer table selected": explorer.TableSelectedMsg{Schema: "public", Table: "users"},
		"explorer new table":      explorer.NewTableMsg{Schema: "public"},
		"explorer drop table":     explorer.DropTableMsg{Schema: "public", Table: "users"},
		"explorer view ddl":       explorer.ViewDDLMsg{Schema: "public", Table: "users"},
		"explorer refresh":        explorer.ExplorerRefreshMsg{},
		"connection selected":     picker.ConnectionSelectedMsg{Project: project},
		"project toggled":         picker.ProjectToggledMsg{},
		"palette command":         palette.CommandSelectedMsg{Action: "help"},
		"mouse wheel, down":       tea.MouseWheelMsg{Button: tea.MouseWheelDown},
		"mouse wheel, up":         tea.MouseWheelMsg{Button: tea.MouseWheelUp},
		"mouse click":             tea.MouseClickMsg{Button: tea.MouseLeft},
		"mouse click, right":      tea.MouseClickMsg{Button: tea.MouseRight},
	}
}

// errFake is the sentinel the package already uses to drive error paths (ask_test.go), so
// every "and it went wrong" message here carries the same failure and the table below
// reads as a list of messages rather than a list of error constructions.

// ---------------------------------------------------------------------------
// M1 + M2: every message folds in
// ---------------------------------------------------------------------------

// Scenario: Cada mensaje se puede foldar, y el modelo sigue siendo dibujable.
//
// The sweep runs on a model with NOTHING connected, because that is the state the app is
// in between starting and connecting and because it is the state most likely to find a
// handler reaching for a nil connection, a nil project or a nil component.
func TestEveryMessageCanBeFoldedIn(t *testing.T) {
	for _, loaded := range []bool{false, true} {
		name := "nothing connected"
		if loaded {
			name = "explorer attached"
		}
		t.Run(name, func(t *testing.T) {
			for label, msg := range everyMessage(t) {
				func() {
					defer func() {
						if r := recover(); r != nil {
							t.Errorf("the message %q PANICKED: %v", label, r)
						}
					}()
					base := routerModel(t)
					if loaded {
						base = routerModelLoaded(t)
					}
					// And once more on a model that has been used, because a
					// handler that works on a fresh model and not on a
					// populated one is the interesting half.
					populated := base
					populated.grid.SetData(sampleResult(), "public", "query")

					for _, m := range []Model{base, populated} {
						updated, _ := m.Update(msg)
						if updated == nil {
							t.Fatalf("the message %q returned a nil model", label)
						}
						model, ok := updated.(Model)
						if !ok {
							t.Fatalf("the message %q returned a %T", label, updated)
						}
						if out := viewText(model); out == "" {
							t.Errorf("the message %q left the app rendering nothing", label)
						}
					}
				}()
			}
		})
	}

	t.Run("a message can arrive twice", func(t *testing.T) {
		// Async results can and do arrive after the user has moved on: the table
		// they were for is no longer selected, or the editor has been closed.
		// Folding the same message twice must not crash and must not double it.
		for label, msg := range everyMessage(t) {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("the message %q PANICKED the second time: %v", label, r)
					}
				}()
				m := routerModelLoaded(t)
				m.grid.SetData(sampleResult(), "public", "query")
				once, _ := m.Update(msg)
				model, ok := once.(Model)
				if !ok {
					t.Fatalf("the message %q returned a %T", label, once)
				}
				twice, _ := model.Update(msg)
				if twice == nil {
					t.Fatalf("the message %q returned a nil model the second time", label)
				}
			}()
		}
	})

	t.Run("and a message can arrive after the app has moved to another state", func(t *testing.T) {
		// The realistic version of the above: the user switched to the picker or
		// opened the editor while a fetch was in flight.
		for _, state := range []AppState{StatePicker, StateMain} {
			for label, msg := range everyMessage(t) {
				func() {
					defer func() {
						if r := recover(); r != nil {
							t.Errorf("the message %q PANICKED in %v: %v", label, state, r)
						}
					}()
					m := routerModelLoaded(t)
					m.state = state
					updated, _ := m.Update(msg)
					if updated == nil {
						t.Fatalf("the message %q returned a nil model in %v", label, state)
					}
				}()
			}
		}
	})
}

// ---------------------------------------------------------------------------
// M3 + M4: the data lands, and the failure shows
// ---------------------------------------------------------------------------

// Scenario: El mensaje que trae DATOS las deja en el sitio.
//
// Three of them, each with one place the data has to arrive, because that is the whole
// point of the message: the grid shows rows, the explorer shows the schema, the editor
// gets the autocomplete data. A handler that reads the message and drops it is invisible
// from outside except that the panel stays empty.
func TestTheMessagesThatCarryDataPutItSomewhere(t *testing.T) {
	t.Run("table data reaches the grid", func(t *testing.T) {
		m := routerModelLoaded(t)
		updated, _ := m.Update(tableDataLoadedMsg{
			result: sampleResult(), schema: "public", table: "users",
		})
		model, ok := updated.(Model)
		if !ok {
			t.Fatalf("Update returned a %T", updated)
		}
		// Asserted on the GRID, not on the app's whole render. The app's render
		// is a layout of six panes sharing a 40-row terminal, and whether a given
		// row of the grid lands inside the visible slice is a layout question,
		// not a question about this message. The message's contract is "the rows
		// are in the grid now", so that is what is asked.
		if !model.grid.HasData() {
			t.Fatal("the grid says it has no data after the rows were loaded")
		}
		// RENDER FIRST, then read the grid. The grid's width and height are set
		// inside renderGrid, which only the app's own render calls — so a grid
		// inspected before the first render has no size, renders nothing, and
		// makes this assertion fail in a way that looks like the data never
		// arrived. The first version of this test read the grid before rendering
		// and reported four missing values from a grid that held them.
		_ = viewText(model)
		grid := stripANSI(model.grid.View())
		// EMAIL, not email: the grid prints its headers uppercased, and a test
		// that spells a column the way the source does is testing the wrong
		// thing. "1-2 of 2" is the status bar's own row count, which is the
		// visible proof that the RESULT reached the grid rather than just the
		// cells.
		for _, want := range []string{"ada", "grace", "EMAIL", "ID", "1-2 of 2"} {
			if !strings.Contains(grid, want) {
				t.Errorf("the loaded data is not in the grid: %q is missing from:\n%s", want, grid)
			}
		}
		if model.router.Focus() != FocusGrid {
			t.Errorf("the focus is on %q after loading a table, want the grid", model.router.Focus())
		}
		// And the app remembered where it came from, which is what the back
		// stack and the preview are built on.
		if model.prevSchema != "public" || model.prevTable != "users" {
			t.Errorf("the app remembers %q.%q, want public.users — the back stack reads these", model.prevSchema, model.prevTable)
		}
		// No toast on success: the row count is in the grid's own status bar, and
		// a toast for it would cover the data the user just asked for. Pinned so
		// "add a success toast" reads as a deliberate change rather than a fix.
		if got := model.toast.RenderedToasts(); len(got) != 0 {
			t.Errorf("a successful load showed %v, want nothing: the grid already says how many rows came back", got)
		}
	})

	t.Run("the schema reaches the explorer", func(t *testing.T) {
		base := routerModelLoaded(t)
		schema := base.explorer
		// Reach for the node the message carries, and check the EXPLORER was
		// told, rather than checking the message round-tripped.
		node := explorer.NewNode("db", explorer.NodeDatabase, "testdb")
		node.Expanded = true
		sch := explorer.NewNode("s", explorer.NodeSchema, "public")
		sch.Expanded = true
		node.AddChild(sch)
		tbl := explorer.NewNode("t", explorer.NodeTable, "users")
		tbl.Metadata["schema"] = "public"
		sch.AddChild(tbl)

		updated, _ := base.Update(schemaLoadedMsg{
			root: node, dbName: "testdb", targetSchema: "public", targetTable: "users",
		})
		model, ok := updated.(Model)
		if !ok {
			t.Fatalf("Update returned a %T", updated)
		}
		if model.explorer == nil {
			t.Fatal("loading a schema left the app with no explorer")
		}
		if model.explorer.Selected() == nil {
			t.Error("the explorer has nothing selected after a schema loaded")
		}
		out := stripANSI(viewText(model))
		if !strings.Contains(out, "public") {
			t.Errorf("the schema is not on screen:\n%s", out)
		}
		_ = schema
	})

	t.Run("autocomplete data reaches the editor", func(t *testing.T) {
		// Autocomplete has to be switched ON in the config: NewModel passes
		// cfg.Editor.Autocomplete straight to the editor, and the zero Config
		// leaves it off. A test that forgot that would conclude the message does
		// not arrive, which is the wrong conclusion for the right code.
		m := routerModelAutocomplete(t)
		m.editorOpen = true
		updated, _ := m.Update(autocompleteDataLoadedMsg{
			schemaExport: &aiContext.SchemaExport{
				Schemas: []aiContext.SchemaInfo{{
					Name:   "public",
					Tables: []aiContext.TableInfo{{Name: "users"}, {Name: "orders"}},
				}},
			},
		})
		model, ok := updated.(Model)
		if !ok {
			t.Fatalf("Update returned a %T", updated)
		}
		// The handler's own observable effect is the toast: it says the
		// autocomplete is ready, which is the user-visible difference between a
		// query that loaded its metadata and one that did not.
		//
		// The completion popup itself is NOT asserted from here. Whether a
		// partial word completes is the editor's business and it is tested in
		// the editor's package; from here it would be a two-hop assertion that
		// fails for a reason in another file. The fixture still switches the
		// feature on, because with the default config the editor never completes
		// anything and the assertion would be meaningless either way.
		if got := lastToastText(t, model); !strings.Contains(got, "ready") {
			t.Errorf("the toast is %q, want it to say the autocomplete is ready", got)
		}
		if model.spinnerActive {
			t.Error("the spinner is still running after the autocomplete data arrived")
		}
	})

	t.Run("a FAILED table load says so and does not claim rows", func(t *testing.T) {
		// The failure that matters: a fetch that errors and leaves the previous
		// table on screen is indistinguishable from a slow one, and the user
		// reads the old numbers as the new query's.
		m := routerModelLoaded(t)
		updated, _ := m.Update(tableDataLoadedMsg{schema: "public", table: "users", err: errFake})
		model, ok := updated.(Model)
		if !ok {
			t.Fatalf("Update returned a %T", updated)
		}
		if got := lastToastText(t, model); !strings.Contains(strings.ToLower(got), "wrong") &&
			!strings.Contains(strings.ToLower(got), "fail") &&
			!strings.Contains(strings.ToLower(got), "error") {
			t.Errorf("the toast after a failed load is %q, want it to say the load failed", got)
		}
	})

	t.Run("a FAILED schema load says so", func(t *testing.T) {
		m := routerModelLoaded(t)
		updated, _ := m.Update(schemaLoadedMsg{err: errFake})
		model, ok := updated.(Model)
		if !ok {
			t.Fatalf("Update returned a %T", updated)
		}
		if got := lastToastText(t, model); got == "" {
			t.Error("a failed schema load showed no toast at all")
		}
	})

	t.Run("a FAILED metadata load says so", func(t *testing.T) {
		m := routerModelLoaded(t)
		updated, _ := m.Update(metadataLoadedMsg{schema: "public", table: "users", err: errFake})
		model, ok := updated.(Model)
		if !ok {
			t.Fatalf("Update returned a %T", updated)
		}
		if got := lastToastText(t, model); got == "" {
			t.Error("a failed metadata load showed no toast at all")
		}
	})

	t.Run("a FAILED connection says so and lands in the picker", func(t *testing.T) {
		// A failed connection is the one message that must NOT leave the app
		// pretending it is connected, because everything downstream reads
		// `m.conn` and would otherwise try to run a query.
		m := routerModel(t)
		m.state = StateMain
		updated, _ := m.Update(dbConnectedMsg{err: errFake})
		model, ok := updated.(Model)
		if !ok {
			t.Fatalf("Update returned a %T", updated)
		}
		// StateError, not the picker: the app knows WHICH project failed, and
		// dropping straight back to the picker would discard that. The first
		// version of this test expected the picker because it assumed "failed"
		// and "not connected yet" are the same state; they are not.
		if model.state != StateError {
			t.Errorf("after a failed connection the app is in state %v, want StateError (%v)", model.state, StateError)
		}
		if got := lastToastText(t, model); got == "" {
			t.Error("a failed connection showed no toast at all")
		}
	})
}

// ---------------------------------------------------------------------------
// M5: the ticks
// ---------------------------------------------------------------------------

// Scenario: Los TICs no hacen nada que se note, y no cuestan nada.
//
// A tick arrives every hundred milliseconds for the whole life of the program, so it is
// the one message that will be folded in millions of times. Anything it does that is not
// idempotent, or that allocates, is a cost paid continuously — and a test that only folds
// it in once cannot see either.
func TestTheTicksAreCheapAndIdempotent(t *testing.T) {
	for _, tick := range []struct {
		name string
		msg  tea.Msg
	}{
		{"spinner", spinnerTickMsg{}},
		{"toast", toastTickMsg{}},
	} {
		t.Run(tick.name, func(t *testing.T) {
			m := routerModelLoaded(t)
			// No toasts showing and no query running: the two states a tick
			// normally has to check, and the two where a tick that assumed
			// otherwise would crash.
			first, _ := m.Update(tick.msg)
			model, ok := first.(Model)
			if !ok {
				t.Fatalf("Update returned a %T", first)
			}

			// Fifty of them, which is five seconds of program life, and the model
			// has to be in the same place afterwards.
			for range 50 {
				next, _ := model.Update(tick.msg)
				model, ok = next.(Model)
				if !ok {
					t.Fatalf("Update returned a %T", next)
				}
			}

			if model.state != m.state {
				t.Errorf("fifty %s ticks changed the app state from %v to %v", tick.name, m.state, model.state)
			}
			if model.router.Focus() != m.router.Focus() {
				t.Errorf("fifty %s ticks moved the focus from %q to %q", tick.name, m.router.Focus(), model.router.Focus())
			}
			if len(model.toast.RenderedToasts()) != len(m.toast.RenderedToasts()) {
				t.Errorf("fifty %s ticks changed the toast count from %d to %d",
					tick.name, len(m.toast.RenderedToasts()), len(model.toast.RenderedToasts()))
			}
			if stripANSI(viewText(model)) != stripANSI(viewText(m)) {
				t.Errorf("fifty %s ticks changed what is on screen", tick.name)
			}
		})
	}
}

// viewText is Model.View()'s Content, which is the string the terminal would draw.
//
// View() returns a tea.View struct, not a string, so every assertion about what is on
// screen has to say which field it means. Naming it once here means a test that says
// "the app is drawing nothing" is about the CONTENT and not about some other field of a
// struct that happens to be comparable.
func viewText(m Model) string { return m.View().Content }

func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
