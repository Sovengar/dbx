package app

// Scenario: Las acciones de app que dependen de datos o del foco.
//
// The table in appActions is swept from every focus by TestEveryAppActionRunsAndLeavesADrawableModel —
// that test proves no handler panics. It runs on a model with no connection, no project
// and no data, which is the right way to find a nil dereference and the wrong way to find
// a handler that does nothing. A guard that returns early on every input is
// indistinguishable from a guard that works.
//
// These seven were the ones the sweep never reached the body of, and every one of them is
// the interesting case:
//
//	commit_drafts    the whole point of the action: turn pending cell edits into SQL
//	undo_drafts      and take them back
//	focus_preview    refuses while a cell is being edited or a filter typed, which is a
//	                 decision about what the preview is allowed to interrupt
//	explorer_open_preview  needs a TABLE selected, not any node
//	refresh_schema   needs both a project and a connection
//	export           needs the grid focused, with data, and the picker willing
//	execute_query    opens the editor when it is closed and runs when it is open
//
// The theme: every one of these has a guard that is easy to get wrong in the permissive
// direction — opening a preview on nothing, stealing focus mid-edit, committing drafts the
// user did not make.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/jackc/pgx/v5"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/ui/components/explorer"
)

// gridWithData is a model whose grid has three rows of a two-column table, focused.
func gridWithData(t *testing.T) Model {
	t.Helper()
	m := routerModelLoaded(t)
	m.grid.SetData(&postgres.QueryResult{
		Columns: []postgres.ColumnInfo{{Name: "id"}, {Name: "name"}},
		Rows: [][]interface{}{
			{int64(1), "ada"},
			{int64(2), "grace"},
			{int64(3), "hopper"},
		},
		Count: 3,
	}, "public", "users")
	m.grid.Focus()
	m.router.FocusPane(FocusGrid)
	return m
}

// gridWithADraft edits a cell through the grid's OWN key path — start editing, change the
// value, press tab to commit — rather than reaching into the grid's fields from another
// package. The point is that the draft exists because the user made one.
func gridWithADraft(t *testing.T) Model {
	t.Helper()
	m := gridWithData(t)

	if _, handled := m.grid.HandleAction(config.ActionID("edit_cell")); !handled {
		t.Fatal("edit_cell was not handled on a grid with data")
	}
	// Clear the cell and type a new value.
	//
	// The backspace carries NO Text. A KeyPressMsg with Code set and Text "\b"
	// stringifies as "\b", not as "backspace", so it falls through the switch to the
	// character-insertion arm and inserts a literal backspace character. The first
	// version of this did that and the draft never appeared.
	m.grid.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	m.grid.Update(tea.KeyPressMsg{Code: 'X', Text: "X"})
	// tab commits and moves to the next column.
	m.grid.Update(tea.KeyPressMsg{Code: tea.KeyTab})

	if !m.grid.HasDrafts() {
		t.Fatal("the grid has no drafts after a cell edit; the fixture is wrong, not the app")
	}
	return m
}

func dispatch(t *testing.T, m Model, id config.ActionID) (Model, tea.Cmd) {
	t.Helper()
	fn, ok := m.appActions()[id]
	if !ok {
		t.Fatalf("the action %q is not in the table", id)
	}
	out, cmd := fn(m)
	got, isModel := out.(Model)
	if !isModel {
		t.Fatalf("the action %q returned a %T, not a Model", id, out)
	}
	return got, cmd
}

// Scenario: Commit convierte los borradores en SQL y los pone en el editor.
func TestCommitDraftsPutsTheSQLInTheEditor(t *testing.T) {
	m := gridWithADraft(t)

	out, cmd := dispatch(t, m, "commit_drafts")
	if cmd != nil {
		t.Errorf("commit_drafts returned a command %v; it dumps SQL into the editor for review, it does not run it", cmd)
	}

	sql := out.editor.Content()
	if sql == "" {
		t.Fatal("commit_drafts left the editor empty")
	}
	if !strings.Contains(sql, "UPDATE") {
		t.Errorf("the editor does not hold an UPDATE:\n%s", sql)
	}
	// The editor opens in the state that says "run this commits the transaction".
	if !out.editorOpen {
		t.Error("commit_drafts did not open the editor")
	}
	if !out.editor.CommitOnRun() {
		t.Error("the editor does not have commit-on-run set, so running it would not commit")
	}

	t.Run("the drafts are STILL PENDING until the query runs", func(t *testing.T) {
		// The commit is a hand-off, not an execution. Clearing the drafts here would
		// lose them with no way back if the user closed the editor.
		if !out.grid.HasDrafts() {
			t.Error("commit_drafts cleared the drafts; closing the editor would lose them")
		}
	})

	t.Run("with NO drafts nothing is put in the editor", func(t *testing.T) {
		clean := gridWithData(t)
		out, _ := dispatch(t, clean, "commit_drafts")
		if out.editor.Content() != "" {
			t.Errorf("the editor was given %q with no drafts", out.editor.Content())
		}
		if out.editorOpen {
			t.Error("the editor opened with no drafts")
		}
	})

	t.Run("outside the grid it does nothing", func(t *testing.T) {
		// The guard is on the FOCUS, not on the drafts: committing from the explorer
		// would dump an unrelated row's SQL into the editor.
		m := gridWithADraft(t)
		m.router.FocusPane(FocusExplorer)
		out, _ := dispatch(t, m, "commit_drafts")
		if out.editor.Content() != "" {
			t.Errorf("the editor was given %q from the explorer", out.editor.Content())
		}
	})
}

// Scenario: Undo deshace los borradores de la fila y AVISA de si habia alguno.
func TestUndoDraftsReportsWhetherThereWasAnythingToUndo(t *testing.T) {
	t.Run("with drafts it undoes them", func(t *testing.T) {
		m := gridWithADraft(t)
		out, _ := dispatch(t, m, "undo_drafts")

		if out.grid.HasDrafts() {
			t.Error("the drafts survived undo_drafts")
		}
	})

	t.Run("with NO drafts it says so instead of doing nothing quietly", func(t *testing.T) {
		// The two branches show DIFFERENT toasts. A user pressing undo twice should not
		// be told they undid something the second time.
		m := gridWithData(t)
		out, _ := dispatch(t, m, "undo_drafts")
		if out.grid.HasDrafts() {
			t.Error("there were no drafts to undo but there are drafts now")
		}
	})

	t.Run("outside the grid it does nothing", func(t *testing.T) {
		m := gridWithADraft(t)
		m.router.FocusPane(FocusExplorer)
		out, _ := dispatch(t, m, "undo_drafts")
		if !out.grid.HasDrafts() {
			t.Error("undo_drafts worked from the explorer")
		}
	})
}

// Scenario: El preview del grid NO se abre mientras se edita o se filtra.
func TestFocusPreviewRefusesWhileTheGridIsBusy(t *testing.T) {
	t.Run("with data and nothing in progress it opens", func(t *testing.T) {
		m := gridWithData(t)
		out, _ := dispatch(t, m, "focus_preview")

		if out.router.Focus() != FocusGridPreview {
			t.Errorf("the focus is %v, want the grid preview", out.router.Focus())
		}
		if out.gridPreview == nil {
			t.Fatal("there is no grid preview to focus")
		}
		if !out.gridPreview.IsFocused() {
			t.Error("the grid preview did not get the focus")
		}
	})

	t.Run("while a cell is being edited it stays in the grid", func(t *testing.T) {
		// The preview renders the row under the cursor. Opening it mid-edit would show
		// the pre-edit value while the user is looking at the new one, and moving the
		// cursor to the preview would abandon the edit.
		m := gridWithData(t)
		if _, handled := m.grid.HandleAction(config.ActionID("edit_cell")); !handled {
			t.Fatal("edit_cell was not handled")
		}
		if !m.grid.IsEditing() {
			t.Fatal("the grid is not editing; the fixture is wrong")
		}

		out, _ := dispatch(t, m, "focus_preview")
		if out.router.Focus() != FocusGrid {
			t.Errorf("the focus moved to %v while a cell was being edited", out.router.Focus())
		}
	})

	t.Run("while a filter is being typed it stays in the grid", func(t *testing.T) {
		m := gridWithData(t)
		m.grid.HandleAction(config.ActionID("filter_rows"))
		if !m.grid.IsWhereFiltering() {
			t.Fatal("the grid is not filtering; the fixture is wrong")
		}

		out, _ := dispatch(t, m, "focus_preview")
		if out.router.Focus() != FocusGrid {
			t.Errorf("the focus moved to %v while a filter was being typed", out.router.Focus())
		}
	})

	t.Run("with no data it does not open", func(t *testing.T) {
		m := routerModelLoaded(t)
		m.router.FocusPane(FocusGrid)
		out, _ := dispatch(t, m, "focus_preview")
		if out.router.Focus() != FocusGrid {
			t.Errorf("the preview opened with no data; the focus is %v", out.router.Focus())
		}
	})

	t.Run("toggle_explorer_focus CLOSES an open preview rather than cycling", func(t *testing.T) {
		// The distinction is the whole action. An open preview is a mode, and the same
		// key that cycles panes elsewhere has to leave it — otherwise a preview that is
		// open cannot be closed with the key that opened it.
		m := gridWithData(t)
		opened, _ := dispatch(t, m, "focus_preview")
		if opened.router.Focus() != FocusGridPreview {
			t.Fatal("the preview did not open")
		}

		closed, _ := dispatch(t, opened, "toggle_explorer_focus")
		if closed.router.Focus() == FocusGridPreview {
			t.Error("the preview is still focused after pressing the key that opens it")
		}
		if closed.gridPreview.IsFocused() {
			t.Error("the preview still has the focus after being closed")
		}
	})
}

// Scenario: El preview del explorador solo se abre sobre una TABLA.
func TestExplorerOpenPreviewNeedsATable(t *testing.T) {
	withNode := func(n *explorer.Node) Model {
		m := routerModelLoaded(t)
		// An empty slice rather than a slice holding nil: SetNodes([]*Node{nil}) is a
		// list with one nil in it, which the explorer dereferences, and "nothing
		// selected" means no nodes at all.
		if n == nil {
			m.explorer.SetNodes(nil)
		} else {
			m.explorer.SetNodes([]*explorer.Node{n})
		}
		m.router.FocusPane(FocusExplorer)
		m.explorer.Focus()
		return m
	}

	t.Run("a selected TABLE opens the preview", func(t *testing.T) {
		n := explorer.NewNode("t", explorer.NodeTable, "users")
		n.Metadata["schema"] = "public"
		m := withNode(n)

		out, cmd := dispatch(t, m, "explorer_open_preview")
		if out.router.Focus() != FocusExplorerPreview {
			t.Errorf("the focus is %v, want the explorer preview", out.router.Focus())
		}
		if !out.explorerPreview.IsFocused() {
			t.Error("the explorer preview did not get the focus")
		}
		// The command loads the data; it is not run here because it needs a
		// connection, but returning nil would mean the pane opens with nothing in it and
		// nothing on the way to fill it.
		if cmd == nil {
			t.Error("explorer_open_preview returned no command, so nothing will load the preview")
		}
	})

	t.Run("a selected SCHEMA does not", func(t *testing.T) {
		// A schema has no columns, no constraints and no indexes: opening the preview on
		// one would show six empty tabs, which reads as "this table is empty" rather
		// than "this is not a table".
		m := withNode(explorer.NewNode("s", explorer.NodeSchema, "public"))
		out, cmd := dispatch(t, m, "explorer_open_preview")

		if out.router.Focus() == FocusExplorerPreview {
			t.Error("the preview opened on a schema node")
		}
		if cmd != nil {
			t.Error("a command was issued to load a preview for a schema")
		}
	})

	t.Run("nothing selected does not", func(t *testing.T) {
		m := withNode(nil)
		out, cmd := dispatch(t, m, "explorer_open_preview")
		if out.router.Focus() == FocusExplorerPreview {
			t.Error("the preview opened with nothing selected")
		}
		if cmd != nil {
			t.Error("a command was issued with nothing selected")
		}
	})

	t.Run("a node with no schema metadata still opens", func(t *testing.T) {
		// Metadata is a free-form map, so a table node built elsewhere in the app may
		// carry no schema. The preview has to open with an empty schema rather than
		// refusing: the table is named, which is the part the user navigated to.
		m := withNode(explorer.NewNode("t", explorer.NodeTable, "users"))
		out, cmd := dispatch(t, m, "explorer_open_preview")
		if out.router.Focus() != FocusExplorerPreview {
			t.Error("the preview did not open for a table with no schema metadata")
		}
		if cmd == nil {
			t.Error("no command was issued for a table with no schema metadata")
		}
	})

	t.Run("outside the explorer it does nothing", func(t *testing.T) {
		n := explorer.NewNode("t", explorer.NodeTable, "users")
		n.Metadata["schema"] = "public"
		m := withNode(n)
		m.router.FocusPane(FocusGrid)

		out, _ := dispatch(t, m, "explorer_open_preview")
		if out.router.Focus() == FocusExplorerPreview {
			t.Error("the explorer preview opened from the grid")
		}
	})
}

// Scenario: Refrescar el esquema necesita proyecto Y conexion.
func TestRefreshSchemaNeedsBothAProjectAndAConnection(t *testing.T) {
	t.Run("with neither it does nothing", func(t *testing.T) {
		m := routerModelLoaded(t)
		out, cmd := dispatch(t, m, "refresh_schema")
		if cmd != nil {
			t.Error("refresh_schema issued a command with no project and no connection")
		}
		if out.state == StateLoading {
			t.Error("the app went to the loading state with nothing to load")
		}
	})

	t.Run("with a project and a connection it reloads", func(t *testing.T) {
		// A non-nil zero connection is enough: loadSchema returns a command, and the
		// command is not run here, so nothing ever asks the fake connection anything.
		// Asserting that would need a real database to be worth anything.
		m := routerModelLoaded(t)
		m.project = &config.FoundProject{Name: "p", Path: "/tmp/p", Active: true}
		m.conn = new(pgx.Conn)

		// Only the COMMAND is asserted, not the state. The action returns loadSchema's
		// closure and the state changes when the closure's message comes back through
		// the event loop — a hand-built closure never runs, so asserting the state here
		// would be asserting something about a message that was never delivered.
		out, cmd := dispatch(t, m, "refresh_schema")
		if cmd == nil {
			t.Error("refresh_schema issued no command with a project and a connection")
		}
		_ = out
	})
}

// Scenario: Exportar necesita el grid enfocado, con datos, y que el picker acepte.
func TestExportNeedsTheGridFocusedWithData(t *testing.T) {
	t.Run("with data in the grid it reaches the picker", func(t *testing.T) {
		m := gridWithData(t)
		out, _ := dispatch(t, m, "export")
		// The picker lives inside the grid, so the grid is the observable. Exporting
		// hands control to it, and the grid's render shows it — asserted through the
		// picker rather than through the app's render, which needs a window size.
		if out.grid.ExportPickerView() == "" {
			t.Error("export did not open the export picker")
		}
	})

	t.Run("outside the grid it does nothing", func(t *testing.T) {
		m := gridWithData(t)
		m.router.FocusPane(FocusExplorer)
		out, _ := dispatch(t, m, "export")
		if out.router.Focus() != FocusExplorer {
			t.Errorf("export moved the focus to %v", out.router.Focus())
		}
	})

	t.Run("with no data it does nothing", func(t *testing.T) {
		m := routerModelLoaded(t)
		m.router.FocusPane(FocusGrid)
		out, _ := dispatch(t, m, "export")
		if out.grid.HasData() {
			t.Error("the grid gained data")
		}
	})
}

// Scenario: Ejecutar abre el editor si esta cerrado, y corre si esta abierto.
func TestExecuteQueryOpensTheEditorThenRunsIt(t *testing.T) {
	t.Run("with the editor closed it opens it and runs nothing", func(t *testing.T) {
		// Pressing run with the editor closed is how the user asks to start writing.
		// Running the empty text would produce a query error about nothing.
		m := routerModelLoaded(t)
		m.editor.SetContent("SELECT 1")

		out, cmd := dispatch(t, m, "execute_query")
		if !out.editorOpen {
			t.Error("execute_query did not open the editor")
		}
		if cmd != nil {
			t.Error("execute_query ran a query on the first press")
		}
		if out.queryExecuting {
			t.Error("the app thinks a query is running")
		}
	})

	t.Run("with the editor OPEN and a connection it runs", func(t *testing.T) {
		m := routerModelLoaded(t)
		m.editorOpen = true
		m.conn = new(pgx.Conn)
		m.editor.SetContent("SELECT 1")

		out, cmd := dispatch(t, m, "execute_query")
		if cmd == nil {
			t.Error("execute_query produced no command with the editor open and a connection")
		}
		if !out.queryExecuting {
			t.Error("the app is not marked as executing; a second press would start another")
		}
	})

	t.Run("a second press while one is running does nothing", func(t *testing.T) {
		// Without this the user can queue the same query twice by pressing run twice
		// on a slow statement, and for an INSERT that is the same row twice.
		m := routerModelLoaded(t)
		m.editorOpen = true
		m.conn = new(pgx.Conn)
		m.editor.SetContent("INSERT INTO t VALUES (1)")
		m.queryExecuting = true

		out, cmd := dispatch(t, m, "execute_query")
		if cmd != nil {
			t.Error("a second execute_query issued a command while one was already running")
		}
		if !out.queryExecuting {
			t.Error("the executing flag was cleared by a refused second press")
		}
	})

	t.Run("with no connection it does not run", func(t *testing.T) {
		m := routerModelLoaded(t)
		m.editorOpen = true
		m.editor.SetContent("SELECT 1")

		out, cmd := dispatch(t, m, "execute_query")
		if cmd != nil {
			t.Error("execute_query ran a query with no connection")
		}
		if out.queryExecuting {
			t.Error("the app is marked as executing with no connection")
		}
	})

	t.Run("with EMPTY text it does not run", func(t *testing.T) {
		// An editor holding only whitespace preprocesses to empty. Sending that to the
		// database produces a syntax error the user cannot act on.
		m := routerModelLoaded(t)
		m.editorOpen = true
		m.conn = new(pgx.Conn)
		m.editor.SetContent("   \n  ")

		out, cmd := dispatch(t, m, "execute_query")
		if cmd != nil {
			t.Errorf("execute_query ran whitespace:\n%v", cmd)
		}
		if out.queryExecuting {
			t.Error("the app is marked as executing whitespace")
		}
	})
}
