package editor

import "github.com/buble/dbx/internal/config"

// HandledActions lists the registry actions the editor dispatches itself while
// focused. Editor keys are widget-local: they keep their raw key handling, but
// exposing the set lets the app-wide coverage test verify action↔handler.
func (e *SQLEditor) HandledActions() []config.ActionID {
	return []config.ActionID{
		"clear_editor", "copy_sql", "autocomplete", "history_prev", "history_next",
	}
}
