package explorer

import "github.com/buble/dbx/internal/config"

// HandledActions lists the registry actions the explorer dispatches itself.
func (e *Explorer) HandledActions() []config.ActionID {
	return []config.ActionID{
		"navigate_down", "navigate_up", "go_first", "go_last",
		"collapse_node", "toggle_columns", "expand_node",
		"drop_table", "view_ddl", "new_table", "filter_tables", "refresh_schema",
	}
}
