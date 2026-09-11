package config

type KeybindingsConfig struct {
	Mode   string            `mapstructure:"mode"`
	Custom map[string]string `mapstructure:"custom"`
}

type KeybindRegistry struct {
	bindings map[string][]string
	mode     string
}

func NewKeybindRegistry(cfg KeybindingsConfig) *KeybindRegistry {
	r := &KeybindRegistry{
		bindings: make(map[string][]string),
		mode:     cfg.Mode,
	}

	defaults := getDefaults(cfg.Mode)
	for k, v := range defaults {
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

func getDefaults(mode string) map[string][]string {
	switch mode {
	case "modern":
		return modernDefaults()
	case "emacs":
		return emacsDefaults()
	default:
		return vimDefaults()
	}
}

func DefaultKeybindings() map[string]string {
	return map[string]string{
		"global.quit":           "q",
		"global.help":           "?",
		"global.palette":        ":",
		"global.cycle_focus":    "tab",
		"global.focus_explorer": "1",
		"global.focus_grid":     "2",
		"global.focus_editor":   "E",
		"global.ask":            "a",
		"global.export":         "e",
		"global.refresh":        "r",

		"explorer.down":     "j",
		"explorer.up":       "k",
		"explorer.expand":   "enter",
		"explorer.collapse": "backspace",
		"explorer.first":    "g",
		"explorer.last":     "G",
		"explorer.filter":   "/",
		"explorer.new":      "n",
		"explorer.drop":     "d",
		"explorer.view_ddl": "v",

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
		"grid.edit_cell":  "enter",
		"grid.delete_row": "d",
		"grid.insert_row": "o",
		"grid.yank":       "y",
		"grid.sort":       "s",
		"grid.filter":     "/",

		"editor.execute":      "ctrl+enter",
		"editor.clear":        "ctrl+u",
		"editor.autocomplete": "tab",
		"editor.history_prev": "ctrl+p",
		"editor.history_next": "ctrl+n",
	}
}

func vimDefaults() map[string][]string {
	return map[string][]string{
		"global.quit":           {"q", "ctrl+c"},
		"global.help":           {"?"},
		"global.palette":        {":"},
		"global.cycle_focus":    {"tab"},
		"global.focus_explorer": {"1"},
		"global.focus_grid":     {"2"},
		"global.focus_editor":   {"E"},
		"global.ask":            {"a"},
		"global.export":         {"e"},
		"global.refresh":        {"r"},

		"explorer.down":     {"j", "down"},
		"explorer.up":       {"k", "up"},
		"explorer.expand":   {"enter", "l"},
		"explorer.collapse": {"backspace", "h"},
		"explorer.first":    {"g"},
		"explorer.last":     {"G"},
		"explorer.filter":   {"/"},
		"explorer.new":      {"n"},
		"explorer.drop":     {"d"},
		"explorer.view_ddl": {"v"},

		"grid.down":       {"j", "down"},
		"grid.up":         {"k", "up"},
		"grid.left":       {"h", "left"},
		"grid.right":      {"l", "right"},
		"grid.first":      {"g"},
		"grid.last":       {"G"},
		"grid.half_up":    {"ctrl+u"},
		"grid.half_down":  {"ctrl+d"},
		"grid.next_page":  {"n"},
		"grid.prev_page":  {"p"},
		"grid.edit_cell":  {"enter", "i"},
		"grid.delete_row": {"d"},
		"grid.insert_row": {"o"},
		"grid.yank":       {"y"},
		"grid.sort":       {"s"},
		"grid.filter":     {"/"},

		"editor.execute":      {"ctrl+enter", "ctrl+r"},
		"editor.clear":        {"ctrl+u"},
		"editor.autocomplete": {"tab"},
		"editor.history_prev": {"ctrl+p"},
		"editor.history_next": {"ctrl+n"},
	}
}

func modernDefaults() map[string][]string {
	return map[string][]string{
		"global.quit":           {"q", "ctrl+c"},
		"global.help":           {"?"},
		"global.palette":        {":"},
		"global.cycle_focus":    {"tab"},
		"global.focus_explorer": {"1"},
		"global.focus_grid":     {"2"},
		"global.focus_editor":   {"E"},
		"global.ask":            {"a"},
		"global.export":         {"e"},
		"global.refresh":        {"r"},

		"explorer.down":     {"down"},
		"explorer.up":       {"up"},
		"explorer.expand":   {"enter", "right"},
		"explorer.collapse": {"backspace", "left"},
		"explorer.first":    {"home"},
		"explorer.last":     {"end"},
		"explorer.filter":   {"ctrl+f"},
		"explorer.new":      {"ctrl+n"},
		"explorer.drop":     {"delete"},
		"explorer.view_ddl": {"v"},

		"grid.down":       {"down"},
		"grid.up":         {"up"},
		"grid.left":       {"left"},
		"grid.right":      {"right"},
		"grid.first":      {"home"},
		"grid.last":       {"end"},
		"grid.half_up":    {"pageup"},
		"grid.half_down":  {"pagedown"},
		"grid.next_page":  {"ctrl+right"},
		"grid.prev_page":  {"ctrl+left"},
		"grid.edit_cell":  {"enter"},
		"grid.delete_row": {"delete"},
		"grid.insert_row": {"ctrl+n"},
		"grid.yank":       {"ctrl+c"},
		"grid.sort":       {"s"},
		"grid.filter":     {"ctrl+f"},

		"editor.execute":      {"ctrl+enter"},
		"editor.clear":        {"ctrl+u"},
		"editor.autocomplete": {"tab"},
		"editor.history_prev": {"ctrl+p"},
		"editor.history_next": {"ctrl+n"},
	}
}

func emacsDefaults() map[string][]string {
	return map[string][]string{
		"global.quit":           {"ctrl+x", "ctrl+c"},
		"global.help":           {"ctrl+h"},
		"global.palette":        {":"},
		"global.cycle_focus":    {"ctrl+o"},
		"global.focus_explorer": {"ctrl+1"},
		"global.focus_grid":     {"ctrl+2"},
		"global.focus_editor":   {"E"},
		"global.ask":            {"alt+a"},
		"global.export":         {"alt+e"},
		"global.refresh":        {"alt+r"},

		"explorer.down":     {"ctrl+n"},
		"explorer.up":       {"ctrl+p"},
		"explorer.expand":   {"enter"},
		"explorer.collapse": {"backspace"},
		"explorer.first":    {"alt+<"},
		"explorer.last":     {"alt+>"},
		"explorer.filter":   {"ctrl+s"},
		"explorer.new":      {"ctrl+x", "ctrl+n"},
		"explorer.drop":     {"ctrl+d"},
		"explorer.view_ddl": {"ctrl+v"},

		"grid.down":       {"ctrl+n"},
		"grid.up":         {"ctrl+p"},
		"grid.left":       {"ctrl+b"},
		"grid.right":      {"ctrl+f"},
		"grid.first":      {"alt+<"},
		"grid.last":       {"alt+>"},
		"grid.half_up":    {"alt+v"},
		"grid.half_down":  {"ctrl+v"},
		"grid.next_page":  {"ctrl+x", "ctrl+f"},
		"grid.prev_page":  {"ctrl+x", "ctrl+b"},
		"grid.edit_cell":  {"enter"},
		"grid.delete_row": {"ctrl+d"},
		"grid.insert_row": {"ctrl+x", "ctrl/o"},
		"grid.yank":       {"alt+w"},
		"grid.sort":       {"alt+s"},
		"grid.filter":     {"ctrl+s"},

		"editor.execute":      {"ctrl+enter"},
		"editor.clear":        {"ctrl+u"},
		"editor.autocomplete": {"tab"},
		"editor.history_prev": {"alt+p"},
		"editor.history_next": {"alt+n"},
	}
}
