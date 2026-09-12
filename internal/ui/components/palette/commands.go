package palette

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
	Action  string
	Section CommandSection
}

type scoredCommand struct {
	command command
	score   int
}

func DefaultCommands() []command {
	return []command{
		{Name: "Refresh Schema", Alias: "refresh", Action: "explorer.refresh", Section: SectionDatabase},
		{Name: "Refresh Data", Alias: "refresh-data", Action: "grid.refresh", Section: SectionDatabase},
		{Name: "Export Table", Alias: "export", Action: "global.export", Section: SectionDatabase},

		{Name: "Execute Query", Alias: "execute", Action: "editor.execute", Section: SectionQuery},
		{Name: "Clear Editor", Alias: "clear", Action: "editor.clear", Section: SectionQuery},

		{Name: "Show Help", Alias: "help", Action: "global.help", Section: SectionUI},

		{Name: "Focus Explorer", Alias: "explorer", Action: "global.focus_explorer", Section: SectionNav},
		{Name: "Focus Grid", Alias: "grid", Action: "global.focus_grid", Section: SectionNav},
		{Name: "Toggle Editor", Alias: "editor", Action: "global.focus_editor", Section: SectionNav},
		{Name: "Toggle Explorer", Alias: "toggle-explorer", Action: "global.cycle_focus", Section: SectionNav},
		{Name: "Focus Grid Preview", Alias: "preview", Action: "grid.focus_preview", Section: SectionNav},
		{Name: "Preview Scroll Up", Alias: "preview-up", Action: "grid-preview.scroll_up", Section: SectionNav},
		{Name: "Preview Scroll Down", Alias: "preview-down", Action: "grid-preview.scroll_down", Section: SectionNav},
		{Name: "Preview → Explorer", Alias: "preview-explorer", Action: "grid-preview.toggle_explorer", Section: SectionNav},
	}
}

func BuildCommands(keybindings map[string]string) []command {
	cmds := DefaultCommands()

	for i := range cmds {
		if kb, ok := keybindings[cmds[i].Action]; ok {
			cmds[i].Alias = cmds[i].Alias + " (" + kb + ")"
		}
	}

	return cmds
}
