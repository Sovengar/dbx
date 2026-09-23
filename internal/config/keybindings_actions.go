package config

// Display sections. Actions are grouped by these in the panel and palette.
const (
	SectionNavigation = "Navigation"
	SectionData       = "Data"
	SectionActions    = "Actions"
	SectionTabs       = "Tabs"
	SectionPreview    = "Preview"
	SectionQuery      = "Query"
	SectionUI         = "UI"
)

// defaultActions is the single keybind inventory. Keys absorb both the primary
// binding and every alias so display and dispatch share one entry.
func defaultActions() []Action {
	navViews := []string{ContextExplorer, ContextGrid, ContextGridPreview}
	pageViews := []string{ContextExplorer, ContextGrid, ContextGridPreview, ContextExplorerPreview}

	actions := []Action{
		// ── App / UI ──────────────────────────────────────────────
		{ID: "quit", Keys: []string{"q", "ctrl+c"}, Section: SectionUI, Description: "Quit",
			Contexts: pageViews, Owner: "app"},
		{ID: "help", Keys: []string{"?"}, Section: SectionUI, Description: "Help",
			Contexts: pageViews, Owner: "app"},
		{ID: "palette", Keys: []string{":"}, Section: SectionUI, Description: "Command Palette",
			Contexts: pageViews, Owner: "app"},
		{ID: "toggle_explorer_focus", Keys: []string{"e"}, Section: SectionNavigation, Description: "Toggle Explorer",
			Contexts: pageViews, Owner: "app"},
		{ID: "focus_explorer", Keys: nil, Section: SectionNavigation, Description: "Focus Explorer",
			Contexts: []string{ContextExplorer, ContextGrid}, Owner: "app"},
		{ID: "focus_grid", Keys: nil, Section: SectionNavigation, Description: "Focus Grid",
			Contexts: []string{ContextGrid}, Owner: "app"},
		{ID: "focus_editor", Keys: []string{"E"}, Section: SectionNavigation, Description: "Toggle Editor",
			Contexts: pageViews, Owner: "app"},
		{ID: "query_browser", Keys: []string{"Q"}, Section: SectionUI, Description: "Query Browser",
			Contexts: pageViews, Owner: "app"},
		{ID: "rollback", Keys: []string{"U"}, Section: SectionUI, Description: "Rollback Last Transaction",
			Contexts: pageViews, Owner: "app"},
		{ID: "switch_connection", Keys: []string{"c"}, Section: SectionUI, Description: "Switch Connection",
			Contexts: pageViews, Owner: "app"},
		{ID: "ask", Keys: []string{"a"}, Section: SectionUI, Description: "Ask AI (NL→SQL)",
			Contexts: pageViews, Owner: "app"},

		// ── Navigation (shared) ───────────────────────────────────
		{ID: "navigate_down", Keys: []string{"j", "down"}, Section: SectionNavigation, Description: "Navigate Down",
			Contexts: navViews, Owner: "grid"},
		{ID: "navigate_up", Keys: []string{"k", "up"}, Section: SectionNavigation, Description: "Navigate Up",
			Contexts: navViews, Owner: "grid"},
		{ID: "navigate_left", Keys: []string{"h", "left"}, Section: SectionNavigation, Description: "Column Left",
			Contexts: []string{ContextGrid}, Owner: "grid"},
		{ID: "navigate_right", Keys: []string{"l", "right"}, Section: SectionNavigation, Description: "Column Right",
			Contexts: []string{ContextGrid}, Owner: "grid"},
		{ID: "go_first", Keys: []string{"g"}, Section: SectionNavigation, Description: "First",
			Contexts: navViews, Owner: "grid"},
		{ID: "go_last", Keys: []string{"G"}, Section: SectionNavigation, Description: "Last",
			Contexts: navViews, Owner: "grid"},
		{ID: "half_page_up", Keys: []string{"ctrl+u"}, Section: SectionNavigation, Description: "Half Page Up",
			Contexts: []string{ContextGrid, ContextGridPreview}, Owner: "grid"},
		{ID: "half_page_down", Keys: []string{"ctrl+d"}, Section: SectionNavigation, Description: "Half Page Down",
			Contexts: []string{ContextGrid, ContextGridPreview}, Owner: "grid"},
		{ID: "focus_preview", Keys: []string{"tab"}, Section: SectionNavigation, Description: "Focus Preview",
			Contexts: []string{ContextGrid}, Owner: "app"},
		{ID: "explorer_open_preview", Keys: []string{"tab"}, Section: SectionNavigation, Description: "Preview Table",
			Contexts: []string{ContextExplorer}, Owner: "app"},
		{ID: "preview_back", Keys: []string{"tab", "esc"}, Section: SectionNavigation, Description: "Back",
			Contexts: []string{ContextGridPreview, ContextExplorerPreview}, Owner: "app"},
		{ID: "go_back", Keys: []string{"H"}, Section: SectionNavigation, Description: "Go Back",
			Contexts: []string{ContextGrid}, Owner: "grid"},

		// ── Grid ──────────────────────────────────────────────────
		{ID: "next_page", Keys: []string{"n", "]", "ctrl+right"}, Section: SectionData, Description: "Next Page",
			Contexts: []string{ContextGrid}, Owner: "grid"},
		{ID: "prev_page", Keys: []string{"p", "[", "ctrl+left"}, Section: SectionData, Description: "Previous Page",
			Contexts: []string{ContextGrid}, Owner: "grid"},
		{ID: "first_page", Keys: []string{"P"}, Section: SectionData, Description: "First Page",
			Contexts: []string{ContextGrid}, Owner: "grid"},
		{ID: "last_page", Keys: []string{"N"}, Section: SectionData, Description: "Last Page",
			Contexts: []string{ContextGrid}, Owner: "grid"},
		{ID: "goto_page_1", Keys: []string{"f1"}, Section: SectionData, Description: "Go to Page 1",
			Contexts: []string{ContextGrid}, Owner: "grid"},
		{ID: "goto_page_2", Keys: []string{"f2"}, Section: SectionData, Description: "Go to Page 2",
			Contexts: []string{ContextGrid}, Owner: "grid"},
		{ID: "goto_page_3", Keys: []string{"f3"}, Section: SectionData, Description: "Go to Page 3",
			Contexts: []string{ContextGrid}, Owner: "grid"},
		{ID: "goto_page_4", Keys: []string{"f4"}, Section: SectionData, Description: "Go to Page 4",
			Contexts: []string{ContextGrid}, Owner: "grid"},
		{ID: "goto_page_5", Keys: []string{"f5"}, Section: SectionData, Description: "Go to Page 5",
			Contexts: []string{ContextGrid}, Owner: "grid"},
		{ID: "goto_page_6", Keys: []string{"f6"}, Section: SectionData, Description: "Go to Page 6",
			Contexts: []string{ContextGrid}, Owner: "grid"},
		{ID: "goto_page_7", Keys: []string{"f7"}, Section: SectionData, Description: "Go to Page 7",
			Contexts: []string{ContextGrid}, Owner: "grid"},
		{ID: "goto_page_8", Keys: []string{"f8"}, Section: SectionData, Description: "Go to Page 8",
			Contexts: []string{ContextGrid}, Owner: "grid"},
		{ID: "goto_page_9", Keys: []string{"f9"}, Section: SectionData, Description: "Go to Page 9",
			Contexts: []string{ContextGrid}, Owner: "grid"},
		{ID: "edit_cell", Keys: []string{"enter"}, Section: SectionData, Description: "Edit Cell",
			Contexts: []string{ContextGrid}, Owner: "grid"},
		{ID: "delete_rows", Keys: []string{"d"}, Section: SectionData, Description: "Delete Row(s)",
			Contexts: []string{ContextGrid}, Owner: "grid"},
		{ID: "insert_row", Keys: []string{"i"}, Section: SectionData, Description: "Insert Row",
			Contexts: []string{ContextGrid}, Owner: "grid"},
		{ID: "yank", Keys: []string{"y"}, Section: SectionData, Description: "Yank to Clipboard",
			Contexts: []string{ContextGrid}, Owner: "grid"},
		{ID: "select_row", Keys: []string{"space"}, Section: SectionData, Description: "Select Row",
			Contexts: []string{ContextGrid}, Owner: "grid"},
		{ID: "sort_column", Keys: []string{"s"}, Section: SectionData, Description: "Sort Column",
			Contexts: []string{ContextGrid}, Owner: "grid"},
		{ID: "filter_rows", Keys: []string{"/"}, Section: SectionData, Description: "Filter Rows",
			Contexts: []string{ContextGrid}, Owner: "grid"},
		{ID: "find_column", Keys: []string{"f"}, Section: SectionData, Description: "Find & Jump to Column",
			Contexts: []string{ContextGrid}, Owner: "grid"},
		{ID: "commit_drafts", Keys: []string{"ctrl+s"}, Section: SectionData, Description: "Commit Drafts",
			Contexts: []string{ContextGrid}, Owner: "app"},
		{ID: "discard_drafts", Keys: []string{"D"}, Section: SectionData, Description: "Discard Drafts",
			Contexts: []string{ContextGrid}, Owner: "grid"},
		{ID: "undo_drafts", Keys: []string{"u"}, Section: SectionData, Description: "Undo Row Drafts",
			Contexts: []string{ContextGrid}, Owner: "app"},
		{ID: "navigate_fk", Keys: []string{"o"}, Section: SectionData, Description: "Navigate Foreign Key",
			Contexts: []string{ContextGrid}, Owner: "grid"},
		{ID: "refresh_data", Keys: []string{"r"}, Section: SectionData, Description: "Refresh Data",
			Contexts: []string{ContextGrid}, Owner: "app"},
		{ID: "export", Keys: []string{"x"}, Section: SectionData, Description: "Export (SQL/JSON/CSV)",
			Contexts: []string{ContextGrid}, Owner: "app"},

		// ── Explorer ──────────────────────────────────────────────
		{ID: "expand_node", Keys: []string{"enter", "l"}, Section: SectionActions, Description: "Load Table",
			Contexts: []string{ContextExplorer}, Owner: "explorer"},
		{ID: "toggle_columns", Keys: []string{"space"}, Section: SectionActions, Description: "Collapse Schema",
			Contexts: []string{ContextExplorer}, Owner: "explorer"},
		{ID: "collapse_node", Keys: []string{"backspace", "h"}, Section: SectionActions, Description: "Collapse / Go to Parent",
			Contexts: []string{ContextExplorer}, Owner: "explorer"},
		{ID: "filter_tables", Keys: []string{"/"}, Section: SectionData, Description: "Filter Tables",
			Contexts: []string{ContextExplorer}, Owner: "explorer"},
		{ID: "new_table", Keys: []string{"n"}, Section: SectionData, Description: "New Table",
			Contexts: []string{ContextExplorer}, Owner: "explorer"},
		{ID: "drop_table", Keys: []string{"d"}, Section: SectionData, Description: "Drop Table",
			Contexts: []string{ContextExplorer}, Owner: "explorer"},
		{ID: "view_ddl", Keys: []string{"v"}, Section: SectionData, Description: "View DDL",
			Contexts: []string{ContextExplorer}, Owner: "explorer"},
		{ID: "refresh_schema", Keys: []string{"r"}, Section: SectionData, Description: "Refresh Schema",
			Contexts: []string{ContextExplorer}, Owner: "app"},

		// ── Explorer preview tabs ─────────────────────────────────
		{ID: "overview_tab", Keys: []string{"1"}, Section: SectionTabs, Description: "Overview Tab",
			Contexts: []string{ContextExplorerPreview}, Owner: "explorerpreview"},
		{ID: "columns_tab", Keys: []string{"2"}, Section: SectionTabs, Description: "Columns Tab",
			Contexts: []string{ContextExplorerPreview}, Owner: "explorerpreview"},
		{ID: "constraints_tab", Keys: []string{"3"}, Section: SectionTabs, Description: "Constraints Tab",
			Contexts: []string{ContextExplorerPreview}, Owner: "explorerpreview"},
		{ID: "foreign_keys_tab", Keys: []string{"4"}, Section: SectionTabs, Description: "Foreign Keys Tab",
			Contexts: []string{ContextExplorerPreview}, Owner: "explorerpreview"},
		{ID: "indexes_tab", Keys: []string{"5"}, Section: SectionTabs, Description: "Indexes Tab",
			Contexts: []string{ContextExplorerPreview}, Owner: "explorerpreview"},
		{ID: "ere_tab", Keys: []string{"6"}, Section: SectionTabs, Description: "ERE Diagram Tab",
			Contexts: []string{ContextExplorerPreview}, Owner: "explorerpreview"},

		// ── Grid preview ──────────────────────────────────────────
		{ID: "expand_fk", Keys: []string{"enter"}, Section: SectionPreview, Description: "Expand FK / Collapse",
			Contexts: []string{ContextGridPreview}, Owner: "gridpreview"},
		{ID: "jq_filter", Keys: []string{"/"}, Section: SectionPreview, Description: "JQ Filter",
			Contexts: []string{ContextGridPreview}, Owner: "gridpreview"},
		{ID: "preview_cursor_up", Keys: nil, Section: SectionPreview, Description: "Preview Cursor Up",
			Contexts: []string{ContextGridPreview}, Owner: "app"},
		{ID: "preview_cursor_down", Keys: nil, Section: SectionPreview, Description: "Preview Cursor Down",
			Contexts: []string{ContextGridPreview}, Owner: "app"},

		// ── Editor ────────────────────────────────────────────────
		{ID: "execute_query", Keys: []string{"ctrl+enter", "ctrl+r"}, Section: SectionQuery, Description: "Execute Query",
			Contexts: []string{ContextEditor}, Owner: "app"},
		{ID: "clear_editor", Keys: []string{"ctrl+u"}, Section: SectionQuery, Description: "Clear Editor",
			Contexts: []string{ContextEditor}, Owner: "app"},
		{ID: "copy_sql", Keys: []string{"ctrl+y"}, Section: SectionQuery, Description: "Copy SQL",
			Contexts: []string{ContextEditor}, Owner: "app"},
		{ID: "autocomplete", Keys: []string{"tab"}, Section: SectionQuery, Description: "Autocomplete",
			Contexts: []string{ContextEditor}, Owner: "editor"},
		{ID: "history_prev", Keys: []string{"ctrl+p"}, Section: SectionQuery, Description: "History Previous",
			Contexts: []string{ContextEditor}, Owner: "editor"},
		{ID: "history_next", Keys: []string{"ctrl+n"}, Section: SectionQuery, Description: "History Next",
			Contexts: []string{ContextEditor}, Owner: "editor"},
	}

	return actions
}
