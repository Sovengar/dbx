package editor

import "github.com/buble/dbx/internal/config"

// HandledActions lists the registry actions the editor dispatches itself while
// focused. execute_query, copy_sql and clear_editor are declared by the app,
// which intercepts them before the editor sees the key; only the keys the app
// leaves to the editor are listed here.
func (e *SQLEditor) HandledActions() []config.ActionID {
	return []config.ActionID{
		"autocomplete", "history_prev", "history_next",
	}
}
