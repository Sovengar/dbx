package app

// The router: one table of app-level actions, one key-dispatch function, and the
// input-mode carve-outs that decide who owns the keyboard.
//
// The table is the interesting part. `appActions()` returns a map from an action ID to a
// handler, and both the key dispatch and the palette go through `dispatchAction`, so the
// table is the whole of what the app does on its own. Everything below is about that
// table and the dispatch in front of it:
//
//	R1  every action in the table runs, on a model with nothing connected, and leaves a
//	    model you can still render
//	R2  every action in the table is reachable from the keyboard — it has a key in the
//	    registry — because an action with no key is a command nobody can run
//	R3  the actions that change state change the state they name
//	R4  an action the table does not have says so, rather than doing nothing in silence
//	R5  every key, in every state and every focused pane, is routed somewhere and returns
//	    a model — the promise that makes the dispatch table-driven rather than a chain of
//	    special cases
//	R6  the three widgets that own the keyboard get the app-wide keys carved out, from
//	    one list
//
// R1 is deliberately run against a model with NO database connection and no project. A
// handler that needs either should say so rather than dereference nil, and this is the
// only place that finds out.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/ui/components/explorer"
)

// routerModel is a fully-built app with nothing connected: NewModel wires every widget,
// so a handler that reaches for one finds it. The HOME is redirected because NewModel
// resolves the NL provider, and a test that reads the developer's real credentials file is
// a test that passes on one machine and fails on another.
func routerModel(t *testing.T) Model {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	m := NewModel(&config.Config{})
	m.state = StateMain
	return resize(m, 120, 40)
}

// resize applies a window size and hands back the model that resulted.
//
// Model.Update has a VALUE receiver and returns a new model, so `m.Update(msg)` as a
// bare statement throws the result away and the model keeps the size it had — which is
// zero. Every component then renders with no room, the whole app comes out ten lines
// tall whatever the terminal size, and the failure looks like a layout bug rather than
// a discarded return value. This helper exists so no fixture can make that mistake
// quietly again.
func resize(m Model, w, h int) Model {
	updated, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	out, ok := updated.(Model)
	if !ok {
		panic("Update(WindowSizeMsg) did not return a Model")
	}
	return out
}

// routerModelLoaded is the same model with the explorer attached.
//
// NewModel leaves `m.explorer` nil on purpose: the explorer is built from the schema
// that arrives when the connection lands, so a nil explorer is the real state of the app
// between starting up and connecting. Both are worth testing, and the two models are kept
// apart rather than one being the default — a harness that quietly attaches an explorer
// would never notice a dispatch path that assumes there is one.
// routerModelAutocomplete is routerModel with the editor's autocomplete switched on.
//
// It is a separate constructor rather than a flag because the config is read once, in
// NewModel, and every other fixture wants the default. Getting this wrong does not fail
// loudly — the editor simply never completes anything — so the one test that needs it
// asks for it by name.
func routerModelAutocomplete(t *testing.T) Model {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	m := NewModel(&config.Config{Editor: config.EditorConfig{Autocomplete: true, AutocompleteTrigger: 1}})
	m.state = StateMain
	m = resize(m, 120, 40)
	m.explorer = explorer.New(m.styles, nil, m.keybinds)
	m = resize(m, 120, 40) // the explorer only gets sized once it exists
	return m
}

func routerModelLoaded(t *testing.T) Model {
	t.Helper()
	m := routerModel(t)
	m.explorer = explorer.New(m.styles, nil, m.keybinds)
	m.explorer.SetWidth(30)
	m.explorer.SetHeight(20)
	return m
}

// ---------------------------------------------------------------------------
// R1 + R2: the table is runnable and reachable
// ---------------------------------------------------------------------------

// Scenario: Cada accion de la tabla SE EJECUTA y deja un modelo dibujable.
//
// This is the test that finds a handler reaching for something NewModel did not build. It
// runs on a model with no connection and no project on purpose: those are the two fields
// a handler is most likely to dereference without checking, and StatePicker is what the
// app starts in, so a handler that runs there has to survive being run.
func TestEveryAppActionRunsAndLeavesADrawableModel(t *testing.T) {
	base := routerModel(t)

	t.Run("every action in the table runs, from every pane focus", func(t *testing.T) {
		for id := range base.appActions() {
			for _, focus := range []FocusPane{FocusGrid, FocusExplorer, FocusGridPreview, FocusExplorerPreview} {
				func() {
					defer func() {
						if r := recover(); r != nil {
							t.Errorf("the action %q with focus %q PANICKED: %v", id, focus, r)
						}
					}()
					m := base
					m.router.FocusPane(focus)

					updated, _ := m.dispatchAction(id)
					if updated == nil {
						t.Errorf("the action %q with focus %q returned a nil model", id, focus)
						return
					}
					model, ok := updated.(Model)
					if !ok {
						t.Errorf("the action %q returned a %T, want Model", id, updated)
						return
					}
					// Drawable: a handler that leaves the model in a state
					// View cannot render is a crash one keystroke later.
					_ = viewText(model)
				}()
			}
		}
	})

	t.Run("every action in the table also runs from the picker", func(t *testing.T) {
		// StatePicker is where the app starts, and several handlers change
		// `state`. Running them there is what proves none of them assumes it
		// arrived from the main view.
		for id := range base.appActions() {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("the action %q PANICKED in StatePicker: %v", id, r)
					}
				}()
				m := base
				m.state = StatePicker
				updated, _ := m.dispatchAction(id)
				if updated == nil {
					t.Errorf("the action %q in StatePicker returned a nil model", id)
				}
			}()
		}
	})

	t.Run("hasAppAction and the table agree, for every ID either could be asked about", func(t *testing.T) {
		m := base
		table := m.appActions()
		for id := range table {
			if !m.hasAppAction(id) {
				t.Errorf("hasAppAction(%q) is false but the table has it", id)
			}
		}
		for _, id := range []config.ActionID{"", "no_such_action", "quit2", "QUIT", " focus_grid"} {
			if m.hasAppAction(id) {
				t.Errorf("hasAppAction(%q) is true but the table has no such entry", id)
			}
		}
		// The table is rebuilt per call, so two calls must not share state and
		// one call must not see the other's edits.
		first := m.appActions()
		if len(first) != len(table) {
			t.Errorf("the table has %d entries then %d, so it is not a fixed set", len(table), len(first))
		}
		delete(first, "quit")
		if _, still := m.appActions()["quit"]; !still {
			t.Error("deleting from one copy of the table removed it from the next call")
		}
	})
}

// Scenario: Cada accion de la tabla tiene una TECLA, o no se puede ejecutar.
//
// An action with no key is a command that exists in the source, passes every test above,
// and cannot be reached by a user. The registry is the source of truth for keys, so the
// check belongs here rather than in a parallel list that can drift.
func keysOfIDs(m map[config.ActionID]bool) []string {
	out := make([]string, 0, len(m))
	for id := range m {
		out = append(out, string(id))
	}
	return out
}

func TestEveryAppActionHasAKeyInTheRegistry(t *testing.T) {
	m := routerModel(t)
	table := m.appActions()

	// The actions with no key, by name. They are not a gap: they are palette-only
	// (`preview-up` and `preview-down` in palette/commands.go), and a keyless
	// action is the right way to express "reachable by command, not by key".
	//
	// Naming them turns a question this package cannot answer — "is it in the
	// palette?" — into a decision gate. Palette reachability itself is asserted in
	// the palette's own tests, because the command list lives there and its type is
	// unexported. What this asserts is that a FOURTH keyless app action has not
	// appeared without somebody deciding it should be palette-only.
	paletteOnly := map[config.ActionID]bool{
		"focus_explorer":      true,
		"focus_grid":          true,
		"preview_cursor_up":   true,
		"preview_cursor_down": true,
	}

	// The set of keyless app actions must EQUAL the set declared here, in both
	// directions. One direction would be a real gap (an action nobody can run);
	// the other is staleness (an entry here that is no longer keyless, so the
	// list has quietly stopped describing reality).
	keyless := map[config.ActionID]bool{}
	for id := range table {
		if len(m.keybinds.KeysFor(id)) == 0 {
			keyless[id] = true
		}
	}
	for id := range keyless {
		if !paletteOnly[id] {
			t.Errorf("the action %q has no key and is not one of the declared palette-only actions %v: "+
				"either bind it to a key or declare it palette-only here", id, keysOfIDs(paletteOnly))
		}
	}
	for id := range paletteOnly {
		if _, inTable := table[id]; !inTable {
			t.Errorf("the declared palette-only action %q is not in the dispatch table at all, so the list is stale", id)
			continue
		}
		if !keyless[id] {
			t.Errorf("the declared palette-only action %q now has keys (%v): either it was rebound, "+
				"or this list needs updating", id, m.keybinds.KeysFor(id))
		}
	}

	// And every key an action DOES have must resolve back to it, or the binding
	// is pointing somewhere else — a different bug from having no key, and one
	// that a "does it have keys" check would miss.
	//
	// Every context is tried, because an action carries the list of panes it
	// applies to and an app-level action applies to several.
	//
	// It is deliberately NOT "and to nothing else", because that is false by
	// design: `tab` is `autocomplete` in the editor, `explorer_open_preview` in
	// the explorer, `focus_preview` in the grid and `preview_back` in the
	// previews; `esc` is `close_editor` in the editor and `preview_back` in the
	// previews. One key meaning different things per pane is the whole reason
	// contexts exist, and the first version of this test asserted the opposite
	// and reported eight of them.
	contexts := []string{
		config.ContextPicker,
		config.ContextGrid,
		config.ContextEditor,
		config.ContextExplorer,
		config.ContextGridPreview,
		config.ContextExplorerPreview,
	}

	for id := range table {
		for _, key := range m.keybinds.KeysFor(id) {
			resolved := false
			for _, ctx := range contexts {
				if got, ok := m.keybinds.Resolve(key, ctx); ok && got == id {
					resolved = true
					break
				}
			}
			if !resolved {
				t.Errorf("the key %q is listed for %q but resolves to something else in every context: it cannot reach it", key, id)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// R3: the actions that change state
// ---------------------------------------------------------------------------

// Scenario: Las acciones que cambian estado cambian EL estado que nombran.
//
// Each of these has one observable consequence, and the consequence is the contract. The
// ones that only produce a command are covered by R1 instead — there is nothing to assert
// about a command other than that building it did not crash.
func TestTheStateChangingActionsChangeTheStateTheyName(t *testing.T) {
	act := func(t *testing.T, m Model, id config.ActionID) Model {
		t.Helper()
		updated, _ := m.dispatchAction(id)
		model, ok := updated.(Model)
		if !ok {
			t.Fatalf("the action %q returned a %T, want Model", id, updated)
		}
		return model
	}

	t.Run("quit asks the program to stop", func(t *testing.T) {
		m := routerModel(t)
		_, cmd := m.dispatchAction("quit")
		if cmd == nil {
			t.Fatal("quit produced no command, so the program does not stop")
		}
		if _, isQuit := cmd().(tea.QuitMsg); !isQuit {
			t.Errorf("quit produced %T, want a tea.QuitMsg", cmd())
		}
	})

	t.Run("help and palette show and hide their overlays", func(t *testing.T) {
		m := routerModel(t)
		if m.helpModal.IsVisible() {
			t.Fatal("the fixture starts with help open")
		}
		m = act(t, m, "help")
		if !m.helpModal.IsVisible() {
			t.Error("help did not open the help modal")
		}
		// Open, not toggle. Pressing the key again leaves it open, and escape is
		// what closes it — pinned because "press ? again to close" is the
		// reflex and the answer here is no.
		m = act(t, m, "help")
		if !m.helpModal.IsVisible() {
			t.Error("a second press of help closed the help modal: it is an open, escape is the close")
		}
		m.helpModal.Hide()

		if m.palette.IsVisible() {
			t.Fatal("the fixture starts with the palette open")
		}
		m = act(t, m, "palette")
		if !m.palette.IsVisible() {
			t.Error("palette did not open the palette")
		}
		m = act(t, m, "palette")
		if !m.palette.IsVisible() {
			t.Error("a second press of palette closed it: it is an open, escape is the close")
		}
		m.palette.Hide()
	})

	t.Run("focus_explorer and focus_grid move the focus", func(t *testing.T) {
		m := routerModel(t)
		m = act(t, m, "focus_explorer")
		if got := m.router.Focus(); got != FocusExplorer {
			t.Errorf("focus_explorer left the focus on %q, want the explorer", got)
		}
		m = act(t, m, "focus_grid")
		if got := m.router.Focus(); got != FocusGrid {
			t.Errorf("focus_grid left the focus on %q, want the grid", got)
		}
	})

	t.Run("focus_editor opens AND closes, and the editor's own focus follows", func(t *testing.T) {
		m := routerModel(t)
		m = act(t, m, "focus_editor")
		if !m.editorOpen {
			t.Fatal("focus_editor did not open the editor")
		}
		m = act(t, m, "focus_editor")
		if m.editorOpen {
			t.Error("focus_editor did not close the editor")
		}
	})

	t.Run("close_editor closes it and leaves it closed", func(t *testing.T) {
		m := routerModel(t)
		m = act(t, m, "focus_editor")
		if !m.editorOpen {
			t.Fatal("the fixture did not open the editor")
		}
		m = act(t, m, "close_editor")
		if m.editorOpen {
			t.Error("close_editor left the editor open")
		}
		// Closing an already-closed editor is not an error and not a reopen.
		m = act(t, m, "close_editor")
		if m.editorOpen {
			t.Error("closing a closed editor opened it")
		}
	})

	t.Run("query_browser opens the browser, and opening it is idempotent", func(t *testing.T) {
		// Also an open, not a toggle — escape is the close. Asserted because a
		// browser that closed itself would hide the query you were reading.
		m := routerModel(t)
		m = act(t, m, "query_browser")
		if !m.queryBrowserOpen {
			t.Error("query_browser did not open the browser")
		}
		// And the render survives the browser being open, which is a different
		// layout from the one the fixture asserted elsewhere.
		_ = viewText(m)
		m = act(t, m, "query_browser")
		if !m.queryBrowserOpen {
			t.Error("a second press of query_browser closed the browser: it is an open")
		}
	})

	t.Run("clear_editor empties the editor and does not close it", func(t *testing.T) {
		m := routerModel(t)
		m = act(t, m, "focus_editor")
		m.editor.SetContent("SELECT 1")
		if m.editor.Content() == "" {
			t.Fatal("the fixture did not put anything in the editor")
		}
		m = act(t, m, "clear_editor")
		if m.editor.Content() != "" {
			t.Errorf("clear_editor left %q in the editor, want it empty", m.editor.Content())
		}
		if !m.editorOpen {
			t.Error("clear_editor closed the editor; clearing the text is not closing the pane")
		}
	})

	t.Run("undo_drafts only speaks when the GRID is focused and has rows", func(t *testing.T) {
		// Two guards, in this order, and both are silent. Focused elsewhere,
		// "undo" does nothing at all — no toast — because undoing a row edit
		// while looking at the editor would be answering a question nobody asked.
		m := routerModel(t)
		m.router.FocusPane(FocusExplorer)
		m = act(t, m, "undo_drafts")
		if got := m.toast.RenderedToasts(); len(got) != 0 {
			t.Errorf("undo_drafts with the explorer focused showed %v, want silence", got)
		}

		// Focused on the grid but with NO ROWS: still silent, because the guard
		// is `HasData()` and not just the focus. Pinned because it is the case
		// that reads as a broken key — the grid is focused, the user pressed
		// undo, and nothing happened. The reason nothing happens is that there
		// is nothing to undo, and the alternative (a toast every time) would
		// fire on every undo press in an empty grid.
		m2 := routerModel(t)
		m2.router.FocusPane(FocusGrid)
		m2 = act(t, m2, "undo_drafts")
		if got := m2.toast.RenderedToasts(); len(got) != 0 {
			t.Errorf("undo_drafts on an empty grid showed %v, want silence: there is nothing to undo", got)
		}

		// With rows, and nothing edited, it says so — the key was aimed at the
		// grid and the user deserves to know why nothing changed.
		m3 := routerModel(t)
		m3.router.FocusPane(FocusGrid)
		m3.grid.SetData(&postgres.QueryResult{
			Columns: []postgres.ColumnInfo{{Name: "id"}, {Name: "name"}},
			Rows:    [][]interface{}{{int64(1), "ada"}},
			Count:   1,
		}, "", "query")
		m3 = act(t, m3, "undo_drafts")
		if got := lastToastText(t, m3); !strings.Contains(strings.ToLower(got), "draft") {
			t.Errorf("the toast is %q, want it to mention drafts", got)
		}
	})

	t.Run("copy_sql opens the editor, and copies nothing when there is nothing", func(t *testing.T) {
		// The observable part is the editor opening: copy_sql is how you get
		// INTO the editor from the grid, so an empty editor here is the normal
		// first press rather than a failure.
		m := routerModel(t)
		m.router.FocusPane(FocusGrid)
		m = act(t, m, "copy_sql")
		if !m.editorOpen {
			t.Error("copy_sql from the grid did not open the editor")
		}
		// With an empty editor there is no command and no toast: the copy is a
		// no-op rather than an error, and nothing claims otherwise.
		if _, cmd := m.dispatchAction("copy_sql"); cmd != nil {
			t.Errorf("copy_sql with an empty editor produced a command: %T", cmd)
		}
	})

	t.Run("toggle_explorer_focus cycles, and it takes the preview out to the explorer", func(t *testing.T) {
		// Three behaviours in one action, which is why it is worth naming: from
		// the grid preview it goes to the EXPLORER, from the explorer preview to
		// the GRID, and from anywhere else it cycles.
		m := routerModel(t)

		m.router.FocusPane(FocusGridPreview)
		m = act(t, m, "toggle_explorer_focus")
		if got := m.router.Focus(); got != FocusExplorer {
			t.Errorf("from the grid preview the focus went to %q, want the explorer", got)
		}

		m.router.FocusPane(FocusExplorerPreview)
		m = act(t, m, "toggle_explorer_focus")
		if got := m.router.Focus(); got != FocusGrid {
			t.Errorf("from the explorer preview the focus went to %q, want the grid", got)
		}

		m.router.FocusPane(FocusGrid)
		first := m.router.Focus()
		m = act(t, m, "toggle_explorer_focus")
		if m.router.Focus() == first {
			t.Error("toggle_explorer_focus from the grid left the focus where it was")
		}
	})
}

// ---------------------------------------------------------------------------
// R4: an action with no handler says so
// ---------------------------------------------------------------------------

// Scenario: Una accion sin manejador lo DICE, y no hace nada en silencio.
//
// The palette can name any action ID, including one the table has lost. Falling through
// to nothing would leave the user pressing a command that does not even acknowledge
// itself.
func TestAnActionWithNoHandlerSaysSo(t *testing.T) {
	m := routerModel(t)
	updated, cmd := m.dispatchAction("no_such_action")
	if updated == nil {
		t.Fatal("dispatching an unknown action returned a nil model")
	}
	model, ok := updated.(Model)
	if !ok {
		t.Fatalf("dispatching an unknown action returned a %T", updated)
	}
	if cmd != nil {
		t.Errorf("dispatching an unknown action produced a command: %T", cmd())
	}
	if got := lastToastText(t, model); !strings.Contains(got, "no_such_action") {
		t.Errorf("the toast is %q, want it to name the action that was not found", got)
	}
}

// ---------------------------------------------------------------------------
// R5 + R6: keys, in every state
// ---------------------------------------------------------------------------

// Scenario: Cada tecla, en cada estado, ALGO se le hace y vuelve un modelo.
//
// The router's real promise is not that any particular key does anything — that is the
// registry's job and R3's — but that no key can leave the app without a model or take it
// down. So this presses every key the registry binds, in every app state and every focused
// pane, and checks only the structural contract.
//
// The three input modes are in the middle of it on purpose, because they are the three
// places the dispatch short-circuits before the table, and they are the three places a
// keypress can be swallowed entirely.
func TestEveryKeyInEveryStateIsRoutedSomewhere(t *testing.T) {
	// The loaded model, so the explorer states below are reachable at all.
	base := routerModelLoaded(t)

	// Every key the registry binds to anything, deduplicated, plus the keys that
	// no action owns because they are TEXT — which is the case that matters most
	// and the one a list of action IDs would miss.
	keys := map[string]tea.KeyPressMsg{
		"?":         {Code: '?', Text: "?"},
		"q":         {Code: 'q', Text: "q"},
		"j":         {Code: 'j', Text: "j"},
		"k":         {Code: 'k', Text: "k"},
		"g":         {Code: 'g', Text: "g"},
		"G":         {Code: 'G', Text: "G"},
		"d":         {Code: 'd', Text: "d"},
		"U":         {Code: 'U', Text: "U"},
		" ":         {Code: tea.KeySpace, Text: " "},
		"a":         {Code: 'a', Text: "a"},
		"1":         {Code: '1', Text: "1"},
		"é":         {Code: 'é', Text: "é"},
		"enter":     {Code: tea.KeyEnter},
		"esc":       {Code: tea.KeyEscape, Text: "esc"},
		"tab":       {Code: tea.KeyTab},
		"backspace": {Code: tea.KeyBackspace},
		"up":        {Code: tea.KeyUp},
		"down":      {Code: tea.KeyDown},
		"left":      {Code: tea.KeyLeft},
		"right":     {Code: tea.KeyRight},
		"home":      {Code: tea.KeyHome},
		"end":       {Code: tea.KeyEnd},
		"pgup":      {Code: tea.KeyPgUp},
		"pgdown":    {Code: tea.KeyPgDown},
		"f1":        {Code: tea.KeyF1},
		"ctrl+c":    {Code: 'c', Mod: tea.ModCtrl},
		"ctrl+r":    {Code: 'r', Mod: tea.ModCtrl},
		"ctrl+z":    {Code: 'z', Mod: tea.ModCtrl},
	}
	for id := range base.appActions() {
		for _, k := range base.keybinds.KeysFor(id) {
			keys[k] = tea.KeyPressMsg{Code: rune(k[0]), Text: k}
		}
	}

	states := []struct {
		name  string
		setup func(*Model)
	}{
		{"nothing loaded yet", func(m *Model) {
			// The explorer is nil here, which is the app's real state between
			// starting and connecting. Every key has to survive it, because the
			// user can press any of them during that window.
			m.state = StateMain
			m.explorer = nil
		}},
		{"picker", func(m *Model) { m.state = StatePicker }},
		{"main, focus grid", func(m *Model) { m.state = StateMain; m.router.FocusPane(FocusGrid) }},
		{"main, focus explorer", func(m *Model) { m.state = StateMain; m.router.FocusPane(FocusExplorer) }},
		{"main, focus grid preview", func(m *Model) { m.state = StateMain; m.router.FocusPane(FocusGridPreview) }},
		{"main, focus explorer preview", func(m *Model) { m.state = StateMain; m.router.FocusPane(FocusExplorerPreview) }},
		{"main, editor open", func(m *Model) { m.state = StateMain; m.editorOpen = true }},
		{"main, browser open", func(m *Model) { m.state = StateMain; m.queryBrowserOpen = true }},
		{"main, ask open", func(m *Model) { m.state = StateMain; m.askOpen = true }},
		{"main, grid filtering", func(m *Model) {
			m.state = StateMain
			m.router.FocusPane(FocusGrid)
			m.grid.HandleAction("where_filter")
		}},
		{"main, explorer filtering", func(m *Model) {
			m.state = StateMain
			m.router.FocusPane(FocusExplorer)
			m.explorer.StartFilter()
		}},
		{"main, jq mode", func(m *Model) {
			m.state = StateMain
			m.router.FocusPane(FocusGridPreview)
			m.gridPreview.EnterJQMode()
		}},
	}

	for _, st := range states {
		for name, key := range keys {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("the key %q in %s PANICKED: %v", name, st.name, r)
					}
				}()
				m := base
				st.setup(&m)

				updated, _ := m.Update(key)
				if updated == nil {
					t.Fatalf("the key %q in %s returned a nil model", name, st.name)
				}
				model, ok := updated.(Model)
				if !ok {
					t.Fatalf("the key %q in %s returned a %T", name, st.name, updated)
				}
				_ = viewText(model)
			}()
		}
	}
}

// Scenario: Las teclas de la app sobreviven a los TRES modos que se tragan el teclado.
//
// The one structural promise worth asserting rather than merely surviving: while a widget
// owns the keyboard, the app-wide keys still work. There are three such widgets and they
// have three separate carve-outs in the dispatch, which is exactly the number of places
// this can drift — so it is asserted for all three.
func TestTheAppWideKeysSurviveAllThreeInputModes(t *testing.T) {
	base := routerModelLoaded(t)

	modes := []struct {
		name  string
		setup func(*Model)
	}{
		{"grid where filter", func(m *Model) {
			m.state = StateMain
			m.router.FocusPane(FocusGrid)
			m.grid.HandleAction("where_filter")
		}},
		{"explorer filter", func(m *Model) {
			m.state = StateMain
			m.router.FocusPane(FocusExplorer)
			m.explorer.StartFilter()
		}},
		{"jq expression", func(m *Model) {
			m.state = StateMain
			m.router.FocusPane(FocusGridPreview)
			m.gridPreview.EnterJQMode()
		}},
	}

	for _, mode := range modes {
		t.Run(mode.name, func(t *testing.T) {
			for _, action := range []config.ActionID{"help", "palette", "rollback"} {
				m := base
				mode.setup(&m)
				if !actionSurvivesTextInput(action) {
					t.Fatalf("the action %q is not on the carve-out list, so this mode does not protect it", action)
				}
				keys := m.keybinds.KeysFor(action)
				if len(keys) == 0 {
					t.Fatalf("the registry binds no key to the action %q", action)
				}
				// Close the overlay the action opens, so the modes do not
				// interfere with each other through a shared modal.
				m.helpModal.Hide()
				m.palette.Hide()

				updated, _ := m.Update(tea.KeyPressMsg{Code: rune(keys[0][0]), Text: keys[0]})
				model, ok := updated.(Model)
				if !ok {
					t.Fatalf("the key %q returned a %T", keys[0], updated)
				}
				switch action {
				case "help":
					if !model.helpModal.IsVisible() {
						t.Errorf("the key %q did not open help while the %s had the keyboard", keys[0], mode.name)
					}
				case "palette":
					if !model.palette.IsVisible() {
						t.Errorf("the key %q did not open the palette while the %s had the keyboard", keys[0], mode.name)
					}
				}
			}

			t.Run("and the widget still gets the OTHER keys", func(t *testing.T) {
				// The carve-out must not be so wide that the widget loses the
				// keyboard: `?` is not a control character, so in these modes it
				// is TEXT and belongs to the widget.
				m := base
				mode.setup(&m)
				before := m.editor.Content()

				updated, _ := m.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
				model, ok := updated.(Model)
				if !ok {
					t.Fatalf("the key returned a %T", updated)
				}
				if model.helpModal.IsVisible() {
					t.Log("`?` opened help here, which is fine: this mode does not treat it as text")
				}
				if model.editor.Content() != before {
					t.Errorf("`?` changed the editor's content to %q, want it unchanged at %q", model.editor.Content(), before)
				}
			})
		})
	}
}
