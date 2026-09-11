package explorer

import (
	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone/v2"

	ui "github.com/buble/dbx/internal/ui"
)

type MouseHandler struct {
	zones *zone.Manager
}

func NewMouseHandler(zones *zone.Manager) *MouseHandler {
	return &MouseHandler{
		zones: zones,
	}
}

func (h *MouseHandler) HandleClick(msg tea.MouseClickMsg, tree *Tree) (tea.Cmd, bool) {
	for i := 0; i < len(tree.filtered); i++ {
		id := zone.NewPrefix() + "tree-" + string(rune(i))
		zi := h.zones.Get(id)
		if zi != nil && zi.InBounds(msg) {
			tree.cursor = i
			return tree.toggleExpand(), true
		}
	}
	return nil, false
}

func (h *MouseHandler) HandleWheel(msg tea.MouseWheelMsg, tree *Tree) (tea.Cmd, bool) {
	m := msg.Mouse()
	if m.Button == 64 {
		tree.moveUp()
		return nil, true
	} else if m.Button == 65 {
		tree.moveDown()
		return nil, true
	}
	return nil, false
}

func MarkTreeRow(id string, content string) string {
	return ui.Mark(id, content)
}
