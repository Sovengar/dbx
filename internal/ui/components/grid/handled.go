package grid

import "github.com/buble/dbx/internal/config"

// HandledActions lists the registry actions the grid dispatches itself through
// its own key switch. Actions the app intercepts before handing the key to the
// grid (export, refresh_data, undo_drafts, commit_drafts) are declared by the
// app instead — listing them here would claim coverage for the app's path.
func (g *Grid) HandledActions() []config.ActionID {
	return []config.ActionID{
		"navigate_down", "navigate_up", "navigate_left", "navigate_right",
		"go_first", "go_last", "half_page_up", "half_page_down",
		"next_page", "prev_page", "first_page", "last_page",
		"goto_page_1", "goto_page_2", "goto_page_3", "goto_page_4", "goto_page_5",
		"goto_page_6", "goto_page_7", "goto_page_8", "goto_page_9",
		"edit_cell", "delete_rows", "insert_row", "yank", "select_row",
		"sort_column", "filter_rows", "find_column",
		"discard_drafts", "navigate_fk", "go_back",
	}
}
