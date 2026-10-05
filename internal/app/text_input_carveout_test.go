package app

import (
	"testing"

	"github.com/buble/dbx/internal/config"
)

// The app-wide keys that stay available while a widget owns the keyboard.
//
// This list was written twice — once for the jq expression line, once (not at all) for
// the explorer's table filter — and the two drifted until the filter had none. The two
// call sites now read this one function, and this test is what stops it drifting again.
//
// Scenario: Las teclas de la app siguen vivas mientras un widget tiene el teclado.
func TestActionSurvivesTextInput(t *testing.T) {
	t.Run("the keys a confused user reaches for are on the list", func(t *testing.T) {
		// `?` for help is the one that matters: a user who does not know what
		// else to press while filtering presses `?`, and it used to type a
		// literal `?` into the filter instead of opening the help sheet.
		for _, action := range []config.ActionID{"help", "palette", "rollback"} {
			if !actionSurvivesTextInput(action) {
				t.Errorf("the action %q does NOT survive a text input: a widget that claims every key would eat it", action)
			}
		}
	})

	t.Run("and almost nothing else is", func(t *testing.T) {
		// Anything else in this list is a key the user is typing INTO the input,
		// so letting it through would fire an action mid-word.
		for _, action := range []config.ActionID{
			"navigate_down", "navigate_up", "go_first", "go_last",
			"expand_node", "collapse_node", "toggle_columns",
			"filter_tables", "drop_table", "view_ddl", "new_table", "refresh_schema",
			"no_such_action", "",
		} {
			if actionSurvivesTextInput(action) {
				t.Errorf("the action %q survives a text input: it would fire while the user is typing", action)
			}
		}
	})

	t.Run("quit is off the list on purpose", func(t *testing.T) {
		// `quit` is bound to `q`, and `q` is a legal character in a table filter,
		// so carving it out would make the filter untypeable. Its other binding,
		// `ctrl+c`, is not separable from `q` at this layer — the carve-out is by
		// ACTION, not by key — so quitting from inside a text input stays the
		// business of the key that means quit everywhere.
		//
		// Pinned because "add quit to the carve-out" is the obvious next idea and
		// it is wrong. The consequence, stated plainly: there is no key that quits
		// from inside a filter.
		if actionSurvivesTextInput("quit") {
			t.Error("quit survives a text input: pressing q to type a q would quit the app")
		}
	})

	t.Run("every action on the list exists in the registry", func(t *testing.T) {
		// A name that is not an action ID never resolves, so it would sit in the
		// list looking like a carve-out and never fire — a list entry that does
		// nothing, which is the failure mode this list is most likely to develop.
		reg := config.NewKeybindRegistry(config.KeybindingsConfig{})
		for _, action := range []config.ActionID{"help", "palette", "rollback"} {
			if len(reg.KeysFor(action)) == 0 {
				t.Errorf("the action %q is on the carve-out list but the registry binds no key to it", action)
			}
		}
	})
}
