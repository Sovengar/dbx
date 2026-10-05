package app

// Scenario: Los guardas de la app que se prueban por su EFFECTO, no por su codigo.
//
// NewModel's two knobs, the picker's quit keys, the explorer's default schema, the wheel's
// modal fallthrough: each of these is two or three lines whose behaviour is only visible in
// the state they leave behind. That is what makes them worth a case each — a guard with no
// assertion about its consequence is a guard that can be inverted without a test noticing.
//
// The four that share a shape are the MODAL OWNERSHIP guards. Update has six of them in a
// row — palette, query browser, ask, picker, cell editor, column filter, where filter — and
// each is the same three-line block:
//
//	an owner is open  ->  give it the message  ->  if it does not want it, STOP
//
// The "stop" is the half that is invisible. A fallthrough instead of a stop means a key the
// owner rejected gets a second chance from whatever is next, which is how a key that quits
// also moves a table. So every owner is asserted twice: once that it receives a key it wants,
// and once that a key it does NOT want stops there rather than travelling on.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/ui/components/explorer"
	"github.com/buble/dbx/internal/ui/components/picker"
	"github.com/buble/dbx/internal/ui/components/querybrowser"
)

// The two knobs on NewModel. Both have a default, and the default is what a model gets when
// the config leaves them at zero — which is the case a zero-valued config produces, and a
// zero-valued config is what a test builds by writing `&config.Config{}`.
func TestTheConfigKnobsOverrideTheDefaultsOnlyWhenSet(t *testing.T) {
	t.Run("a zero config takes the defaults", func(t *testing.T) {
		m := NewModel(&config.Config{})
		if got := m.grid.PagerLimit(); got != 100 {
			t.Errorf("a zero config gives a page size of %d, want the default 100", got)
		}
		if got := m.grid.YankMaxRows(); got != 10 {
			t.Errorf("a zero config gives a yank limit of %d, want the default 10", got)
		}
	})

	t.Run("a SET knob wins", func(t *testing.T) {
		// The knobs were declared, given defaults and read by nothing for a while — the
		// round-15 wiring. The line that reads them is one `if`, and this is the case that
		// makes it observable.
		m := NewModel(&config.Config{UI: config.UIConfig{PageSize: 25, YankMaxRows: 3}})
		if got := m.grid.PagerLimit(); got != 25 {
			t.Errorf("page_size 25 gives a page size of %d", got)
		}
		if got := m.grid.YankMaxRows(); got != 3 {
			t.Errorf("yank_max_rows 3 gives a limit of %d", got)
		}
	})

	t.Run("a NEGATIVE knob also takes the default", func(t *testing.T) {
		// The guard is `> 0`, not `!= 0`, and the difference is a config file that says
		// `page_size = -1`: as `!= 0` that would be a page size of -1, and every offset
		// arithmetic downstream would be negative.
		m := NewModel(&config.Config{UI: config.UIConfig{PageSize: -1, YankMaxRows: -5}})
		if got := m.grid.PagerLimit(); got != 100 {
			t.Errorf("a page size of -1 gives %d, want the default 100", got)
		}
		if got := m.grid.YankMaxRows(); got != 10 {
			t.Errorf("a yank limit of -5 gives %d, want the default 10", got)
		}
	})
}

// The picker quits on q and ctrl+c — and BOTH answers live in the picker itself, which is
// the finding here.
//
// The app's picker block has the shape "give it the message; if it does not want it, check
// for q or ctrl+c and quit". The second half cannot fire: the picker's own key switch names
// q and ctrl+c and returns `tea.Quit, true`, so the app's fallthrough is only reached for
// keys the picker does NOT name — and there are none of those that are also q or ctrl+c.
//
// So the quit has one live implementation and one unreachable copy. Two places answer the
// same question, which is the shape that lets one of them rot: if someone removed q from the
// picker's switch the app's copy would quietly take over and nobody would know the picker
// had changed.
//
// Pinned rather than removed. It is three lines of a key path, the answer it would give is
// the right one, and the picker's key set is a UI decision that will gain and lose keys —
// the fallback is cheap insurance against a future set that omits one. What is NOT acceptable
// is not knowing which one is live, so that is what this says.
//
// ctrl+c has a THIRD answer, one layer up: it is bound to quit app-wide, so the app claims it
// before the picker is asked. Same shape as the grid's column filter, which also looks like it
// eats ctrl+c and does not.
func TestThePickerQuitsOnBothKeysAndTheLiveImplementationIsThePickers(t *testing.T) {
	t.Run("q quits", func(t *testing.T) {
		m := routerModel(t)
		m.state = StatePicker

		_, cmd := fold(m, tea.KeyPressMsg{Code: 'q', Text: "q"})
		if cmd == nil {
			t.Fatal("q at the picker produced no command, so nothing quit")
		}
	})

	t.Run("ctrl+c quits, claimed by the app before the picker is asked", func(t *testing.T) {
		m := routerModel(t)
		m.state = StatePicker

		// Asserted structurally as well as by effect: the point is that the app resolves it
		// as a bound action, so the picker's own q/ctrl+c case is not what answers.
		action, ok := m.keybinds.Resolve("ctrl+c", m.router.Context())
		if !ok || !m.hasAppAction(action) {
			t.Fatalf("ctrl+c resolves to %q (bound=%t), want an app-level action", action, ok)
		}

		// WITHOUT Text. A modified key that carries Text stringifies as the bare
		// character — `{Code:'c', Text:"c", Mod:ModCtrl}` is "c", not "ctrl+c" — so the
		// first version of this case sent "c" to the picker, which does not name it, and
		// reported that ctrl+c did not quit. The key was wrong, not the app.
		_, cmd := fold(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
		if cmd == nil {
			t.Fatal("ctrl+c at the picker produced no command, so nothing quit")
		}
	})

	t.Run("a key the picker WANTS is answered by the picker, not by the quit branch", func(t *testing.T) {
		// The counterweight. Without it, "every key produces a command" would satisfy the
		// cases above, and so would a branch that quits on everything.
		m := routerModel(t)
		m.state = StatePicker
		// A project to select. With none, the picker's enter has nothing to act on and
		// returns unhandled — which is correct, and is why the first version of this case
		// reported that enter did nothing: it had no project either.
		m.picker.SetProjects([]config.FoundProject{
			{Name: "shopdb", Path: t.TempDir(), Active: true},
		})

		out, cmd := fold(m, tea.KeyPressMsg{Code: tea.KeyEnter})
		if cmd == nil {
			t.Fatal("enter at the picker produced no command")
		}
		msg := cmd()
		if _, ok := msg.(picker.ConnectionSelectedMsg); !ok {
			t.Errorf("enter produced %T, want the picker's own ConnectionSelectedMsg", msg)
		}
		if out.state == StatePicker && msg == nil {
			t.Error("the picker neither acted nor quit")
		}
	})

	t.Run("and a key nobody wants changes nothing", func(t *testing.T) {
		m := routerModel(t)
		m.state = StatePicker

		out, cmd := fold(m, tea.KeyPressMsg{Code: tea.KeyF12})
		if cmd != nil {
			t.Errorf("an unbound key at the picker produced %T", cmd())
		}
		if out.state != StatePicker {
			t.Errorf("an unbound key moved the app out of the picker into %v", out.state)
		}
	})
}

// The explorer's "new table" template. An empty schema means "the one I was looking at",
// and the default is the only thing standing between that and a statement that creates a
// table in a schema named "".
func TestANewTableDefaultsToPublicWhenTheExplorerNamesNoSchema(t *testing.T) {
	t.Run("no schema at all becomes public", func(t *testing.T) {
		m := routerModelLoaded(t)

		out, cmd := fold(m, explorer.NewTableMsg{})
		if cmd != nil {
			t.Error("the new-table template issued a command")
		}
		if !strings.Contains(out.editor.Content(), "public") {
			t.Errorf("the template does not name a schema: %q", out.editor.Content())
		}
		if !out.editorOpen {
			t.Error("the editor did not open")
		}
	})

	t.Run("a named schema is kept", func(t *testing.T) {
		m := routerModelLoaded(t)

		out, _ := fold(m, explorer.NewTableMsg{Schema: "audit"})
		if !strings.Contains(out.editor.Content(), "audit.") {
			t.Errorf("the template replaced the schema the explorer named: %q", out.editor.Content())
		}
	})
}

// The wheel's help-modal fallthrough. The modal gets the wheel FIRST and scrolls itself; if
// it declines the message the app must still not scroll the pane behind it, because the
// pane is behind a dialog the user is reading.
func TestTheWheelStopsAtAnOpenHelpModalEvenWhenTheModalDeclinesIt(t *testing.T) {
	m := routerModelLoaded(t)
	m.grid.SetData(dataOf(60, 3), "public", "t")
	m.grid.SetWidth(80)
	m.grid.SetHeight(20)
	m.grid.Focus()
	m.router.FocusPane(FocusGrid)
	m.state = StateMain
	m.helpModal.Show()

	before := m.grid.CursorRow()
	// A wheel over no zone: the modal does not scroll for it, and neither does the grid.
	out, _ := fold(m, tea.MouseWheelMsg{Button: tea.MouseWheelDown})

	if out.grid.CursorRow() != before {
		t.Errorf("the wheel moved the grid cursor from %d to %d while the help modal was open",
			before, out.grid.CursorRow())
	}
	if !out.helpModal.IsVisible() {
		t.Error("the wheel closed the help modal")
	}
}

// The query browser's ownership. It is a modal list, so while it is open the grid must not
// scroll underneath it — and a key the browser declines must not travel on to the grid
// either.
func TestTheQueryBrowserOwnsTheKeyboardWhileItIsOpen(t *testing.T) {
	m := routerModelLoaded(t)
	m.grid.SetData(dataOf(40, 3), "public", "t")
	m.grid.SetWidth(80)
	m.grid.SetHeight(20)
	m.grid.Focus()
	m.router.FocusPane(FocusGrid)
	m.queryBrowserOpen = true
	if m.queryBrowser == nil {
		m.queryBrowser = querybrowser.New(m.styles, nil)
	}
	// The app's queryBrowserOpen flag and the browser's OWN visible flag are two
	// different things, and Update reads the second one. Without Show() the browser
	// declines every message and the app's block falls straight through — which is the
	// same shape as "the browser does not handle this key", and the two are told apart
	// only by whether Show() was called.
	m.queryBrowser.Show()
	if !m.queryBrowser.IsVisible() {
		t.Fatal("the query browser did not become visible")
	}

	before := m.grid.CursorRow()

	t.Run("a navigation key goes to the browser", func(t *testing.T) {
		// j is the browser's own "move down" AND the grid's. While the browser is open it
		// belongs to the browser — that is what "owns the keyboard" means.
		out, _ := fold(m, tea.KeyPressMsg{Code: 'j', Text: "j"})
		if out.grid.CursorRow() != before {
			t.Errorf("j moved the grid cursor from %d to %d with the query browser open",
				before, out.grid.CursorRow())
		}
		if !out.queryBrowser.IsVisible() {
			t.Error("a navigation key closed the query browser")
		}
	})

	t.Run("a key the browser DECLINES stops there", func(t *testing.T) {
		// The stop, not a fallthrough. Without it a key the browser does not want would be
		// offered to the grid behind it, and two meanings would answer one press.
		out, _ := fold(m, tea.KeyPressMsg{Code: 'F', Text: "F", Mod: tea.ModAlt})
		if out.grid.CursorRow() != before {
			t.Errorf("a key the query browser declined moved the grid cursor from %d to %d",
				before, out.grid.CursorRow())
		}
	})
}

// The schema-refresh path. It needs a project AND a connection, and it is the only place
// both are checked together — the guard that stops a refresh attempted before connecting.
func TestRefreshingTheSchemaNeedsBothAProjectAndAConnection(t *testing.T) {
	t.Run("both present: the schema reloads", func(t *testing.T) {
		m := routerModelLoaded(t)
		m.conn = newTestConn(t)
		m.project = &config.FoundProject{Name: "shopdb", Path: t.TempDir(), Active: true}

		out, cmd := fold(m, explorer.ExplorerRefreshMsg{})
		if cmd == nil {
			t.Fatal("a refresh with a project and a connection issued no reload")
		}
		if !strings.Contains(toastText(out), "refreshed") {
			t.Errorf("the refresh did not say it happened: %q", toastText(out))
		}
	})

	for _, tc := range []struct {
		name   string
		mutate func(m *Model)
	}{
		{"no connection", func(m *Model) { m.conn = nil }},
		{"no project", func(m *Model) { m.project = nil }},
		{"neither", func(m *Model) { m.conn = nil; m.project = nil }},
	} {
		t.Run(tc.name+" is refused", func(t *testing.T) {
			m := routerModelLoaded(t)
			m.conn = newTestConn(t)
			m.project = &config.FoundProject{Name: "shopdb", Path: t.TempDir(), Active: true}
			tc.mutate(&m)

			out, cmd := fold(m, explorer.ExplorerRefreshMsg{})
			if cmd != nil {
				t.Errorf("a refresh with %s issued a reload", tc.name)
			}
			if said := toastText(out); said != "" {
				t.Errorf("a refresh with %s said %q, want nothing", tc.name, said)
			}
		})
	}
}

// The project toggle. It persists the active/inactive state to disk AND says so, and the
// two toasts are the only feedback a user gets that their toggle took — so "Enabled" for a
// project that is already enabled is a wrong answer the test has to catch.
func TestTogglingAProjectSaysWhatChanged(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	for _, tc := range []struct {
		name   string
		active bool
		want   string
	}{
		{"enabling", true, "Enabled"},
		{"disabling", false, "Disabled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := routerModelLoaded(t)
			project := config.FoundProject{Name: "shopdb", Path: t.TempDir(), Active: !tc.active}

			out, cmd := fold(m, picker.ProjectToggledMsg{Project: project, Active: tc.active})

			if cmd != nil {
				t.Errorf("%s issued a command", tc.name)
			}
			said := toastText(out)
			if !strings.Contains(said, tc.want) {
				t.Errorf("%s said %q, want it to contain %q", tc.name, said, tc.want)
			}
			if !strings.Contains(said, project.Name) {
				t.Errorf("%s did not name the project: %q", tc.name, said)
			}
		})
	}
}

// Connecting from the picker. The one path where a project becomes the model's project, and
// it clears connectCancelled on the way — a flag left set would cancel the next attempt, the
// one the user made on purpose.
func TestChoosingAConnectionStartsItAndClearsAPreviousCancellation(t *testing.T) {
	m := routerModel(t)
	m.state = StatePicker
	m.connectCancelled = true

	out, cmd := fold(m, picker.ConnectionSelectedMsg{
		Project: config.FoundProject{Name: "shopdb", Path: t.TempDir(), Active: true},
	})

	if cmd == nil {
		t.Fatal("choosing a connection issued no connect command")
	}
	if out.state != StateLoading {
		t.Errorf("the state is %v, want loading", out.state)
	}
	if out.connectCancelled {
		t.Error("the previous cancellation is still set, so the next attempt would be cancelled by it")
	}
	if out.project == nil || out.project.Name != "shopdb" {
		t.Errorf("the model kept the project %+v, want the chosen one", out.project)
	}
}

// The app-wide carve-out, and the one input of the three that looks like the explorer filter
// that must NOT have it.
//
// Five inputs swallow the keyboard: the SQL editor, the grid column filter, the grid where
// filter, the cell editor, and jq input. Three of them share a shape — "the user is typing
// something narrow" — and the code answers that shape two different ways: the explorer's
// table filter and jq consult actionSurvivesTextInput, and the grid's COLUMN filter consulted
// nothing at all and handed every key straight to the filter.
//
// The observable is the difference between `?` opening help and `?` becoming part of what
// the user is searching for. Every case below sends the key that is both a character of the
// input AND an app action, so a "handled" answer that does nothing is distinguishable from
// one that does the right thing.
func TestTheCarveOutHasThreeCallersAndOnlyTwoOfThemWantIt(t *testing.T) {
	keys := []struct {
		key     tea.KeyPressMsg
		want    config.ActionID
		comment string
	}{
		{tea.KeyPressMsg{Code: '?', Text: "?"}, "help", "help — the key you reach for when confused"},
		{tea.KeyPressMsg{Code: ':', Text: ":"}, "palette", "the command palette"},
		{tea.KeyPressMsg{Code: 'U', Text: "U"}, "rollback", "rollback"},
	}

	// The premise of the whole thing: these three ARE app actions in the grid context, so
	// the carve-out has something to resolve to. If that stops being true the carve-out
	// silently does nothing and this file would pass for the wrong reason.
	t.Run("the premise: all three are app actions in the grid context", func(t *testing.T) {
		m := routerModelLoaded(t)
		for _, k := range keys {
			action, ok := m.keybinds.Resolve(k.key.String(), m.router.Context())
			if !ok {
				t.Errorf("%s resolves to nothing in the grid context", k.key)
				continue
			}
			if action != k.want {
				t.Errorf("%s resolves to %q, want %q", k.key, action, k.want)
			}
			if !m.hasAppAction(action) {
				t.Errorf("%s resolves to %q, which is not an app action", k.key, action)
			}
			if !actionSurvivesTextInput(action) {
				t.Errorf("%q is not in the list that survives text input, so the carve-out cannot claim it", action)
			}
		}
	})

	t.Run("the column filter lets them through", func(t *testing.T) {
		for _, k := range keys {
			t.Run(k.key.String(), func(t *testing.T) {
				m := routerModelLoaded(t)
				m.grid.SetData(dataOf(40, 3), "public", "t")
				m.grid.SetWidth(80)
				m.grid.SetHeight(20)
				m.grid.Focus()
				m.router.FocusPane(FocusGrid)
				m.state = StateMain
				m.grid.HandleAction("find_column")

				if !m.grid.IsFiltering() {
					t.Fatal("the column filter did not open, so this proves nothing")
				}

				out, _ := fold(m, k.key)

				if got := out.grid.FilterText(); got != "" {
					t.Errorf("%s went INTO the filter as %q; the filter matches column names, which contain no such character",
						k.key, got)
				}
				if !out.grid.IsFiltering() {
					t.Errorf("%s closed the filter as a side effect", k.key)
				}
				switch k.want {
				case "help":
					if !out.helpModal.IsVisible() {
						t.Error("? did not open help while the column filter was open")
					}
				case "palette":
					if !out.palette.IsVisible() {
						t.Error(": did not open the palette while the column filter was open")
					}
				}
			})
		}

		t.Run("and an ordinary character still filters", func(t *testing.T) {
			// The counterweight. Without it, "nothing reaches the filter" would satisfy
			// every case above, and so would a carve-out that swallowed the keyboard.
			m := routerModelLoaded(t)
			m.grid.SetData(dataOf(40, 3), "public", "t")
			m.grid.SetWidth(80)
			m.grid.SetHeight(20)
			m.grid.Focus()
			m.router.FocusPane(FocusGrid)
			m.state = StateMain
			m.grid.HandleAction("find_column")

			out, _ := fold(m, tea.KeyPressMsg{Code: 'i', Text: "i"})
			if out.grid.FilterText() != "i" {
				t.Errorf("typing i gave a filter of %q, want \"i\"", out.grid.FilterText())
			}
			if out.helpModal.IsVisible() {
				t.Error("typing i opened help")
			}
		})
	})

	// The negative case, and the reason it is written down. The where filter looks
	// identical to the column filter and must behave differently, because it is not
	// matching a name — it is composing a SQL fragment, and `?` is PostgreSQL's bind
	// parameter. Giving it the carve-out would make a parameterised filter untypable, and
	// the "consistency" fix would be a regression nobody would notice until a query failed
	// for a reason that has nothing to do with the query.
	t.Run("the where filter keeps its question mark, and that is correct", func(t *testing.T) {
		m := routerModelLoaded(t)
		m.grid.SetData(dataOf(40, 3), "public", "t")
		m.grid.SetWidth(80)
		m.grid.SetHeight(20)
		m.grid.Focus()
		m.router.FocusPane(FocusGrid)
		m.state = StateMain
		m.grid.HandleAction("filter_rows")

		if !m.grid.IsWhereFiltering() {
			t.Fatal("the where filter did not open, so this proves nothing")
		}

		out, _ := fold(m, tea.KeyPressMsg{Code: '?', Text: "?"})

		if out.helpModal.IsVisible() {
			t.Error("? opened help from the where filter; ? is PostgreSQL's bind parameter and the filter composes a WHERE clause")
		}
		// It stays open and does not crash, which is the whole of what is claimed: the
		// where filter DEclines keys it has no meaning for, and the app stops there
		// rather than answering on its behalf.
		if !out.grid.IsWhereFiltering() {
			t.Error("? closed the where filter")
		}
	})

	// The where filter is also the ONE owner of the four that can decline a key at all,
	// which is what makes the three `return m, nil` blocks above it live or dead.
	//
	// handleFilterKey, handleEditKey and the explorer's handleFilterKey each END in a
	// default arm that returns true for every key — the column filter appends any
	// single-character key and returns true, the cell editor appends any non-control key
	// and returns true. So for those three the `if handled { return }` / `return m, nil`
	// pair is a THREE-LINE BLOCK WHOSE SECOND HALF CANNOT RUN. The explorer filter is the
	// same shape, so that makes four.
	//
	// Only the where filter returns false: `j` and `k` with no suggestion popup open are
	// "not mine". That one arm is the live one, and it is why the wheel and the key path
	// disagree about what "declined" means in this codebase.
	t.Run("the where filter is the only owner that declines", func(t *testing.T) {
		m := routerModelLoaded(t)
		m.grid.SetData(dataOf(40, 3), "public", "t")
		m.grid.SetWidth(80)
		m.grid.SetHeight(20)
		m.grid.Focus()
		m.router.FocusPane(FocusGrid)
		m.state = StateMain
		m.grid.HandleAction("filter_rows")

		// j with no popup: HandleKey returns (false, false), so Grid.Update returns
		// (nil, false), and the app must STOP rather than offer the key to the grid
		// underneath. If the stop were a fallthrough the grid would move its cursor —
		// visible, wrong, and the kind of thing nobody reports because it looks like
		// the app is just noisy.
		before := m.grid.CursorRow()
		out, cmd := fold(m, tea.KeyPressMsg{Code: 'j', Text: "j"})

		if out.grid.CursorRow() != before {
			t.Errorf("j moved the grid cursor from %d to %d after the where filter declined it",
				before, out.grid.CursorRow())
		}
		if !out.grid.IsWhereFiltering() {
			t.Error("j closed the where filter it had declined")
		}
		if cmd != nil {
			t.Errorf("a declined j produced %T", cmd())
		}
	})
}
