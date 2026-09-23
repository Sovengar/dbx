package palette

import "github.com/buble/dbx/internal/config"

type CommandSection string

const (
	SectionDatabase CommandSection = "Database"
	SectionQuery    CommandSection = "Query"
	SectionUI       CommandSection = "UI"
	SectionNav      CommandSection = "Navigation"
)

type command struct {
	Name    string
	Alias   string
	Action  config.ActionID
	Section CommandSection
}

type scoredCommand struct {
	command command
	score   int
}

func DefaultCommands() []command {
	return []command{
		{Name: "Refresh Schema", Alias: "refresh", Action: "refresh_schema", Section: SectionDatabase},
		{Name: "Refresh Data", Alias: "refresh-data", Action: "refresh_data", Section: SectionDatabase},
		{Name: "Export Table", Alias: "export", Action: "export", Section: SectionDatabase},
		{Name: "Undo Row Drafts", Alias: "undo", Action: "undo_drafts", Section: SectionDatabase},
		{Name: "Rollback Last Transaction", Alias: "rollback", Action: "rollback", Section: SectionDatabase},

		{Name: "Switch Connection", Alias: "switch", Action: "switch_connection", Section: SectionDatabase},

		{Name: "Execute Query", Alias: "execute", Action: "execute_query", Section: SectionQuery},
		{Name: "Clear Editor", Alias: "clear", Action: "clear_editor", Section: SectionQuery},
		{Name: "Copy SQL", Alias: "copy", Action: "copy_sql", Section: SectionQuery},

		{Name: "Show Help", Alias: "help", Action: "help", Section: SectionUI},

		{Name: "Focus Explorer", Alias: "explorer", Action: "focus_explorer", Section: SectionNav},
		{Name: "Focus Grid", Alias: "grid", Action: "focus_grid", Section: SectionNav},
		{Name: "Toggle Editor", Alias: "editor", Action: "focus_editor", Section: SectionNav},
		{Name: "Query Browser", Alias: "queries", Action: "query_browser", Section: SectionNav},
		{Name: "Toggle Explorer", Alias: "toggle-explorer", Action: "toggle_explorer_focus", Section: SectionNav},
		{Name: "Focus Grid Preview", Alias: "preview", Action: "focus_preview", Section: SectionNav},
		{Name: "Preview Cursor Up", Alias: "preview-up", Action: "preview_cursor_up", Section: SectionNav},
		{Name: "Preview Cursor Down", Alias: "preview-down", Action: "preview_cursor_down", Section: SectionNav},
		{Name: "Preview → Explorer", Alias: "preview-explorer", Action: "toggle_explorer_focus", Section: SectionNav},
	}
}

func BuildCommands(keybindings config.Resolver) []command {
	cmds := DefaultCommands()

	for i := range cmds {
		if kb := keybindings.PrimaryKey(cmds[i].Action); kb != "" {
			cmds[i].Alias = cmds[i].Alias + " (" + kb + ")"
		}
	}

	return cmds
}
