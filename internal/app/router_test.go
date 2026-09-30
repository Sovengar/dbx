package app

import (
	"testing"

	"github.com/buble/dbx/internal/config"
)

// routerFakeResolver is a config.Resolver that answers PrimaryKey from a table.
// Only the method the router actually calls is meaningful; the rest satisfy the
// interface and panic if something starts depending on them.
type routerFakeResolver struct {
	keys map[config.ActionID]string
}

func (f routerFakeResolver) PrimaryKey(id config.ActionID) string { return f.keys[id] }

func (f routerFakeResolver) Resolve(string, string) (config.ActionID, bool) {
	panic("router does not call Resolve")
}

func (f routerFakeResolver) KeysFor(config.ActionID) []string {
	panic("router does not call KeysFor")
}

func (f routerFakeResolver) ActionsFor(string) []config.Action {
	panic("router does not call ActionsFor")
}

func (f routerFakeResolver) All() []config.Action {
	panic("router does not call All")
}

// Scenario: Un router nuevo arranca con el foco en el explorer.
func TestNewRouter_StartsOnTheExplorer(t *testing.T) {
	r := NewRouter(routerFakeResolver{})

	if got := r.Focus(); got != FocusExplorer {
		t.Errorf("Focus() = %v (%q), want FocusExplorer", got, got)
	}
	if got := r.Context(); got != "explorer" {
		t.Errorf("Context() = %q, want %q", got, "explorer")
	}
}

// Scenario: Fijar el foco cambia lo que Focus y Context devuelven.
func TestRouter_FocusPaneSetsTheFocus(t *testing.T) {
	for _, pane := range []FocusPane{
		FocusExplorer,
		FocusGrid,
		FocusEditor,
		FocusGridPreview,
		FocusExplorerPreview,
	} {
		r := NewRouter(routerFakeResolver{})
		r.FocusPane(pane)

		if got := r.Focus(); got != pane {
			t.Errorf("Focus() = %v after FocusPane(%v)", got, pane)
		}
		// Context is derived from the focus, so it must track it exactly.
		if got, want := r.Context(), pane.String(); got != want {
			t.Errorf("Context() = %q after FocusPane(%v), want %q", got, pane, want)
		}
	}
}

// Scenario: CycleFocus recorre los dos paneles cyclables y vuelve al punto de
// partida.
//
// Only the explorer and the grid are in the cycle: the editor and the two
// preview panes are cycled out of deliberately, so reaching them by cycling
// would strand the user somewhere they cannot tab back from.
func TestRouter_CycleFocusWalksTheCycleAndReturns(t *testing.T) {
	r := NewRouter(routerFakeResolver{})

	// Start is known, so the whole walk is checkable.
	if got := r.Focus(); got != FocusExplorer {
		t.Fatalf("Focus() = %v before cycling, want FocusExplorer", got)
	}
	if got := r.CycleFocus(); got != FocusGrid {
		t.Errorf("first CycleFocus = %v, want FocusGrid", got)
	}
	if got := r.CycleFocus(); got != FocusExplorer {
		t.Errorf("second CycleFocus = %v, want FocusExplorer (back to the start)", got)
	}
	// A third lap must be identical, which is what pins the modulo.
	if got := r.CycleFocus(); got != FocusGrid {
		t.Errorf("third CycleFocus = %v, want FocusGrid", got)
	}
	if got := r.CycleFocus(); got != FocusExplorer {
		t.Errorf("fourth CycleFocus = %v, want FocusExplorer", got)
	}
}

// Scenario: Cycler desde un panel que no está en el ciclo no lo mueve.
//
// The loop looks for the current pane and only then advances. A focus outside
// the cycle is therefore left alone rather than jumping somewhere arbitrary,
// which is what makes a stray FocusPane(editor) recoverable.
func TestRouter_CycleFocusFromOutsideTheCycleStaysPut(t *testing.T) {
	for _, outside := range []FocusPane{FocusEditor, FocusGridPreview, FocusExplorerPreview} {
		r := NewRouter(routerFakeResolver{})
		r.FocusPane(outside)

		if got := r.CycleFocus(); got != outside {
			t.Errorf("CycleFocus from %v = %v, want it unchanged", outside, got)
		}
	}
}

// Scenario: FocusByName reconoce los nombres de los paneles y deja el foco donde
// estaba cuando el nombre no existe.
//
// An unknown name must NOT clear the focus. Clearing it would leave the router
// on whatever the zero value is, which happens to be the explorer, so the user
// would be teleported rather than told the name was wrong.
func TestRouter_FocusByName(t *testing.T) {
	for _, tc := range []struct {
		name string
		want FocusPane
	}{
		{"explorer", FocusExplorer},
		{"grid", FocusGrid},
		{"grid-preview", FocusGridPreview},
		{"explorer-preview", FocusExplorerPreview},
	} {
		r := NewRouter(routerFakeResolver{})
		r.FocusByName(tc.name)

		if got := r.Focus(); got != tc.want {
			t.Errorf("FocusByName(%q) -> %v, want %v", tc.name, got, tc.want)
		}
		// The name and the pane's own String() must agree, or the config would
		// accept a name the TUI can never produce.
		if got := tc.want.String(); got != tc.name {
			t.Errorf("FocusPane(%v).String() = %q, want %q to match the config name", tc.want, got, tc.name)
		}
	}

	// Unknown names, including ones that only look right, leave the focus alone.
	for _, name := range []string{"", "editor", "Explorer", "gridpreview", "grid_preview", "preview", "unknown"} {
		r := NewRouter(routerFakeResolver{})
		r.FocusPane(FocusGrid)

		if got := r.FocusByName(name); got != FocusGrid {
			t.Errorf("FocusByName(%q) = %v, want the focus unchanged at FocusGrid", name, got)
		}
	}
}

// Scenario: FocusByName es el inverso de String para todos los paneles con
// nombre, y el nombre del editor no está expose a propósito.
//
// editor is a real pane with a String() but deliberately no config name, so a
// round trip through FocusByName must not be able to reach it. If a name is ever
// added for it, this test is the one that has to be updated on purpose.
func TestFocusByName_ReachableNamesRoundTrip(t *testing.T) {
	reachable := map[FocusPane]bool{
		FocusExplorer:        true,
		FocusGrid:            true,
		FocusGridPreview:     true,
		FocusExplorerPreview: true,
	}

	for pane := FocusExplorer; pane <= FocusExplorerPreview; pane++ {
		name := pane.String()
		r := NewRouter(routerFakeResolver{})
		r.FocusByName(name)
		got := r.Focus()

		if reachable[pane] {
			if got != pane {
				t.Errorf("pane %v round-tripped through %q to %v", pane, name, got)
			}
			continue
		}
		// Unreachable by name: FocusByName must leave the focus at the start.
		if got != FocusExplorer {
			t.Errorf("pane %v has name %q and became reachable; update the test deliberately", pane, name)
		}
	}
}

// Scenario: KeyFor delega en el resolver, y un resolver nil devuelve "" en vez
// de reventar.
//
// The nil check is what lets a router be built before the keybindings are
// loaded, which is the normal startup order.
func TestRouter_KeyFor(t *testing.T) {
	r := NewRouter(routerFakeResolver{keys: map[config.ActionID]string{
		"focus_grid": "g",
		"ask":        "?",
	}})

	if got := r.KeyFor("focus_grid"); got != "g" {
		t.Errorf("KeyFor(focus_grid) = %q, want %q", got, "g")
	}
	if got := r.KeyFor("ask"); got != "?" {
		t.Errorf("KeyFor(ask) = %q, want %q", got, "?")
	}
	// An action with no binding is the empty string, not an error.
	if got := r.KeyFor("unbound_action"); got != "" {
		t.Errorf("KeyFor(unbound) = %q, want the empty string", got)
	}
	// An empty binding in the table is the same as no binding.
	if got := r.KeyFor(""); got != "" {
		t.Errorf("KeyFor(empty id) = %q, want the empty string", got)
	}
}

// Scenario: Un router sin resolver no rompe al pedir una tecla.
func TestRouter_KeyForWithNoResolver(t *testing.T) {
	r := NewRouter(nil)

	if got := r.KeyFor("focus_grid"); got != "" {
		t.Errorf("KeyFor with a nil resolver = %q, want the empty string", got)
	}
	// The rest of the router still works without a resolver.
	if got := r.Focus(); got != FocusExplorer {
		t.Errorf("Focus() = %v, want FocusExplorer", got)
	}
	if got := r.CycleFocus(); got != FocusGrid {
		t.Errorf("CycleFocus() = %v, want FocusGrid", got)
	}
}
