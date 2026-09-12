package grid

import (
	"fmt"
	"strings"

	"github.com/buble/dbx/internal/theme"
)

type Tab struct {
	ID    string // "records", "columns", "constraints", "foreign_keys", "indexes"
	Label string // "Records", "Columns", ...
	Key   string // "1", "2", ... (from keybindings)
}

type TabBar struct {
	tabs   []Tab
	active int
	styles *theme.Styles
	width  int
}

func NewTabBar(styles *theme.Styles, keybinds map[string]string) *TabBar {
	tabs := []Tab{
		{ID: "records", Label: "Records", Key: keybinds["grid.tab_records"]},
		{ID: "columns", Label: "Columns", Key: keybinds["grid.tab_columns"]},
		{ID: "constraints", Label: "Constraints", Key: keybinds["grid.tab_constraints"]},
		{ID: "foreign_keys", Label: "Foreign Keys", Key: keybinds["grid.tab_foreign_keys"]},
		{ID: "indexes", Label: "Indexes", Key: keybinds["grid.tab_indexes"]},
	}
	return &TabBar{
		tabs:   tabs,
		active: 0,
		styles: styles,
	}
}

func (t *TabBar) SetWidth(w int) {
	t.width = w
}

func (t *TabBar) SetActive(i int) {
	if i >= 0 && i < len(t.tabs) {
		t.active = i
	}
}

func (t *TabBar) ActiveID() string {
	if t.active >= 0 && t.active < len(t.tabs) {
		return t.tabs[t.active].ID
	}
	return ""
}

func (t *TabBar) ActiveIndex() int {
	return t.active
}

func (t *TabBar) NextTab() {
	t.active++
	if t.active >= len(t.tabs) {
		t.active = 0
	}
}

func (t *TabBar) PrevTab() {
	t.active--
	if t.active < 0 {
		t.active = len(t.tabs) - 1
	}
}

func (t *TabBar) TabCount() int {
	return len(t.tabs)
}

func (t *TabBar) Render() string {
	var parts []string
	for i, tab := range t.tabs {
		label := fmt.Sprintf("%s [%s]", tab.Label, tab.Key)
		if i == t.active {
			parts = append(parts, t.styles.TabActive.Render(label))
		} else {
			parts = append(parts, t.styles.TabInactive.Render(label))
		}
	}
	return strings.Join(parts, t.styles.Sep.Render(" │ "))
}
