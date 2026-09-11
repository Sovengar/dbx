package explorer

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/buble/dbx/internal/theme"
)

type Explorer struct {
	tree      *Tree
	mouse     *MouseHandler
	styles    *theme.Styles
	width     int
	height    int
	focused   bool
	filtering bool
}

func New(styles *theme.Styles, zones interface{}) *Explorer {
	var mh *MouseHandler
	if z, ok := zones.(*interface{ New() interface{} }); ok {
		_ = z
	}
	_ = mh
	return &Explorer{
		tree:   NewTree(styles),
		styles: styles,
	}
}

func (e *Explorer) SetNodes(nodes []*Node) {
	e.tree.SetNodes(nodes)
}

func (e *Explorer) SetWidth(w int) {
	e.width = w
	e.tree.SetWidth(w - 4)
}

func (e *Explorer) SetHeight(h int) {
	e.height = h
	e.tree.SetHeight(h - 4)
}

func (e *Explorer) Focus() {
	e.focused = true
}

func (e *Explorer) Blur() {
	e.focused = false
}

func (e *Explorer) Selected() *Node {
	return e.tree.Selected()
}

func (e *Explorer) Update(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if e.filtering {
			return e.handleFilterKey(msg)
		}
		if e.tree.filter != "" && msg.String() == "esc" {
			e.tree.ClearFilter()
			return nil, true
		}
		return e.tree.Update(msg)
	}
	return nil, false
}

func (e *Explorer) handleFilterKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	key := msg.String()
	switch key {
	case "esc":
		e.filtering = false
		e.tree.ClearFilter()
		return nil, true
	case "enter":
		e.filtering = false
		return nil, true
	case "backspace":
		filter := e.tree.filter
		if len(filter) > 0 {
			e.tree.SetFilter(filter[:len(filter)-1])
		}
		return nil, true
	default:
		if len(key) == 1 {
			e.tree.SetFilter(e.tree.filter + key)
		}
		return nil, true
	}
}

func (e *Explorer) View() string {
	var s strings.Builder

	title := e.styles.Header.Render("Explorer")
	s.WriteString(title)
	s.WriteString("\n")

	if e.filtering {
		filter := e.styles.Text.Render("Filter: " + e.tree.filter + "_")
		s.WriteString(filter)
		s.WriteString("\n")
	} else if e.tree.filter != "" {
		filter := e.styles.TextMuted.Render("Filter: " + e.tree.filter)
		s.WriteString(filter)
		s.WriteString("\n")
	}

	content := e.tree.View()
	s.WriteString(content)

	border := e.styles.Border
	if e.focused {
		border = e.styles.BorderActive
	}

	return border.
		Width(e.width - 2).
		Height(e.height - 2).
		Render(s.String())
}

func (e *Explorer) StartFilter() {
	e.filtering = true
	e.tree.SetFilter("")
}
