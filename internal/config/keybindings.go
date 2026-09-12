package config

type KeybindingsConfig struct {
	Custom map[string]string `mapstructure:"custom"`
}

type KeybindRegistry struct {
	bindings map[string][]string
}

func NewKeybindRegistry(cfg KeybindingsConfig) *KeybindRegistry {
	r := &KeybindRegistry{
		bindings: make(map[string][]string),
	}

	for k, v := range defaultBindings() {
		r.bindings[k] = v
	}

	for k, v := range cfg.Custom {
		r.bindings[k] = []string{v}
	}

	return r
}

func (r *KeybindRegistry) Match(key, context string) string {
	for action, keys := range r.bindings {
		if !inContext(action, context) {
			continue
		}
		for _, k := range keys {
			if k == key {
				return action
			}
		}
	}
	return ""
}

func (r *KeybindRegistry) Flatten() map[string]string {
	flat := make(map[string]string, len(r.bindings))
	for action, keys := range r.bindings {
		if len(keys) > 0 {
			flat[action] = keys[0]
		}
	}
	return flat
}

func inContext(action, context string) bool {
	if context == "global" {
		return len(action) > 7 && action[:7] == "global."
	}
	return len(action) > len(context)+1 && action[:len(context)+1] == context+"."
}

func DefaultKeybindings() map[string]string {
	return map[string]string{
		"global.quit":           "q",
		"global.help":           "?",
		"global.palette":        ":",
		"global.cycle_focus":    "e",
		"global.focus_explorer": "",
		"global.focus_grid":     "",
		"global.focus_editor":   "E",
		"global.ask":            "a",
		"global.export":         "x",
		"global.refresh":        "r",

		"explorer.down":          "j",
		"explorer.up":            "k",
		"explorer.expand":        "enter",
		"explorer.toggle_columns": "space",
		"explorer.collapse":      "backspace",
		"explorer.first":         "g",
		"explorer.last":          "G",
		"explorer.filter":        "/",
		"explorer.new":           "n",
		"explorer.drop":          "d",
		"explorer.view_ddl":      "v",

		"grid.down":       "j",
		"grid.up":         "k",
		"grid.left":       "h",
		"grid.right":      "l",
		"grid.first":      "g",
		"grid.last":       "G",
		"grid.half_up":    "ctrl+u",
		"grid.half_down":  "ctrl+d",
		"grid.next_page":  "n",
		"grid.prev_page":  "p",
		"grid.first_page": "P",
		"grid.last_page":  "N",
		"grid.goto_page_1":  "f1",
		"grid.goto_page_2":  "f2",
		"grid.goto_page_3":  "f3",
		"grid.goto_page_4":  "f4",
		"grid.goto_page_5":  "f5",
		"grid.goto_page_6":  "f6",
		"grid.goto_page_7":  "f7",
		"grid.goto_page_8":  "f8",
		"grid.goto_page_9":  "f9",
		"grid.tab_records":      "1",
		"grid.tab_columns":      "2",
		"grid.tab_constraints":  "3",
		"grid.tab_foreign_keys": "4",
		"grid.tab_indexes":      "5",
		"grid.edit_cell":  "enter",
		"grid.delete_row": "d",
		"grid.insert_row": "i",
		"grid.yank":       "y",
		"grid.select_row": "space",
		"grid.sort":       "s",
		"grid.filter":     "/",
		"grid.find_and_jump_to_column": "f",
		"grid.commit_pending":          "ctrl+s",
		"grid.navigate_fk":            "o",
		"grid.go_back":                "H",

		"editor.execute":      "ctrl+enter",
		"editor.clear":        "ctrl+u",
		"editor.autocomplete": "tab",
		"editor.history_prev": "ctrl+p",
		"editor.history_next": "ctrl+n",
	}
}

func defaultBindings() map[string][]string {
	return map[string][]string{
		"global.quit":           {"q", "ctrl+c"},
		"global.help":           {"?"},
		"global.palette":        {":"},
		"global.cycle_focus":    {"e"},
		"global.focus_explorer": {},
		"global.focus_grid":     {},
		"global.focus_editor":   {"E"},
		"global.ask":            {"a"},
		"global.export":         {"x"},
		"global.refresh":        {"r"},

		"explorer.down":          {"j", "down"},
		"explorer.up":            {"k", "up"},
		"explorer.expand":        {"enter", "l"},
		"explorer.toggle_columns": {"space"},
		"explorer.collapse":      {"backspace", "h"},
		"explorer.first":         {"g"},
		"explorer.last":          {"G"},
		"explorer.filter":        {"/"},
		"explorer.new":           {"n"},
		"explorer.drop":          {"d"},
		"explorer.view_ddl":      {"v"},

		"grid.down":       {"j", "down"},
		"grid.up":         {"k", "up"},
		"grid.left":       {"h", "left"},
		"grid.right":      {"l", "right"},
		"grid.first":      {"g"},
		"grid.last":       {"G"},
		"grid.half_up":    {"ctrl+u"},
		"grid.half_down":  {"ctrl+d"},
		"grid.next_page":  {"n", "]"},
		"grid.prev_page":  {"p", "["},
		"grid.first_page": {"P"},
		"grid.last_page":  {"N"},
		"grid.goto_page_1":  {"f1"},
		"grid.goto_page_2":  {"f2"},
		"grid.goto_page_3":  {"f3"},
		"grid.goto_page_4":  {"f4"},
		"grid.goto_page_5":  {"f5"},
		"grid.goto_page_6":  {"f6"},
		"grid.goto_page_7":  {"f7"},
		"grid.goto_page_8":  {"f8"},
		"grid.goto_page_9":  {"f9"},
		"grid.tab_records":      {"1"},
		"grid.tab_columns":      {"2"},
		"grid.tab_constraints":  {"3"},
		"grid.tab_foreign_keys": {"4"},
		"grid.tab_indexes":      {"5"},
		"grid.edit_cell":  {"enter"},
		"grid.delete_row": {"d"},
		"grid.insert_row": {"i"},
		"grid.yank":       {"y"},
		"grid.select_row": {"space"},
		"grid.sort":       {"s"},
		"grid.filter":     {"/"},
		"grid.find_and_jump_to_column": {"f"},
		"grid.commit_pending":          {"ctrl+s"},
		"grid.navigate_fk":            {"o"},
		"grid.go_back":                {"H"},

		"editor.execute":      {"ctrl+enter", "ctrl+r"},
		"editor.clear":        {"ctrl+u"},
		"editor.autocomplete": {"tab"},
		"editor.history_prev": {"ctrl+p"},
		"editor.history_next": {"ctrl+n"},
	}
}
