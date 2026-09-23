package explorerpreview

import (
	"fmt"
	"strings"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/theme"
)

type Tab struct {
	ID    string
	Label string
	Key   string
}

type TabBar struct {
	tabs   []Tab
	active int
	styles *theme.Styles
	width  int
}

func NewTabBar(styles *theme.Styles, keybinds config.Resolver) *TabBar {
	keyOf := func(id config.ActionID) string {
		if keybinds == nil {
			return ""
		}
		return keybinds.PrimaryKey(id)
	}
	tabs := []Tab{
		{ID: "overview", Label: "Overview", Key: keyOf("overview_tab")},
		{ID: "columns", Label: "Columns", Key: keyOf("columns_tab")},
		{ID: "constraints", Label: "Constraints", Key: keyOf("constraints_tab")},
		{ID: "foreign_keys", Label: "Foreign Keys", Key: keyOf("foreign_keys_tab")},
		{ID: "indexes", Label: "Indexes", Key: keyOf("indexes_tab")},
		{ID: "ere", Label: "ERE", Key: keyOf("ere_tab")},
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
