package explorer

import (
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/buble/dbx/internal/theme"
)

type TableSelectedMsg struct {
	Schema string
	Table  string
}

type NewTableMsg struct {
	Schema string
}

type DropTableMsg struct {
	Schema string
	Table  string
}

type ViewDDLMsg struct {
	Schema string
	Table  string
}

type ExplorerRefreshMsg struct{}

type Explorer struct {
	tree        *Tree
	styles      *theme.Styles
	keybindings map[string]string
	width       int
	height      int
	focused     bool
	filtering   bool
}

func New(styles *theme.Styles, zones interface{}, keybindings map[string]string) *Explorer {
	return &Explorer{
		tree:        NewTree(styles),
		styles:      styles,
		keybindings: keybindings,
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

		key := msg.String()

		if node := e.tree.Selected(); node != nil && node.Type == NodeTable {
			schema := ""
			if s, ok := node.Metadata["schema"].(string); ok {
				schema = s
			}

			if key == e.keybindings["explorer.toggle_columns"] {
				if node.Parent != nil && node.Parent.Type == NodeSchema {
					schemaNode := node.Parent
					schemaNode.Expanded = false
					e.tree.flattenNodes()
					for i, n := range e.tree.filtered {
						if n == schemaNode {
							e.tree.cursor = i
							break
						}
					}
					e.tree.clampOffset()
				}
				return nil, true
			}

			if key == e.keybindings["explorer.expand"] {
				return func() tea.Msg {
					return TableSelectedMsg{
						Schema: schema,
						Table:  node.Name,
					}
				}, true
			}

			if key == e.keybindings["explorer.drop"] {
				return func() tea.Msg {
					return DropTableMsg{
						Schema: schema,
						Table:  node.Name,
					}
				}, true
			}

			if key == e.keybindings["explorer.view_ddl"] {
				return func() tea.Msg {
					return ViewDDLMsg{
						Schema: schema,
						Table:  node.Name,
					}
				}, true
			}
		}

		if key == e.keybindings["explorer.new"] {
			schema := ""
			if node := e.tree.Selected(); node != nil {
				if s, ok := node.Metadata["schema"].(string); ok {
					schema = s
				}
			}
			return func() tea.Msg {
				return NewTableMsg{
					Schema: schema,
				}
			}, true
		}

		if key == e.keybindings["explorer.filter"] {
			e.StartFilter()
			return nil, true
		}

		if key == e.keybindings["explorer.refresh"] {
			return func() tea.Msg {
				return ExplorerRefreshMsg{}
			}, true
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

	output := s.String()

	border := e.styles.Border
	if e.focused {
		border = e.styles.BorderActive
	}

	return border.
		Width(e.width - 2).
		Height(e.height - 2).
		MaxHeight(e.height - 2).
		Render(output)
}

func (e *Explorer) StartFilter() {
	e.filtering = true
	e.tree.SetFilter("")
}

func (e *Explorer) IsFiltering() bool {
	return e.filtering
}

func (e *Explorer) HandleClick(y int) bool {
	if e.tree == nil || len(e.tree.filtered) == 0 {
		return false
	}
	filteredIndex := e.tree.offset + y
	if filteredIndex < 0 || filteredIndex >= len(e.tree.filtered) {
		return false
	}
	e.tree.cursor = filteredIndex
	return true
}

func (e *Explorer) SelectTable(schema, table string) bool {
	if e.tree == nil {
		return false
	}
	target := e.tree.FindTable(schema, table)
	if target == nil {
		return false
	}
	// Expand all ancestors so the table is visible
	for node := target.Parent; node != nil; node = node.Parent {
		if !node.Expanded {
			node.Expanded = true
		}
	}
	e.tree.flattenNodes()
	for i, n := range e.tree.filtered {
		if n == target {
			e.tree.cursor = i
			e.tree.clampOffset()
			return true
		}
	}
	return false
}

func (e *Explorer) ToggleExpand() tea.Cmd {
	node := e.tree.Selected()
	if node == nil {
		return nil
	}

	if node.Type == NodeTable {
		schema := ""
		if s, ok := node.Metadata["schema"].(string); ok {
			schema = s
		}

		node.ToggleExpand()
		e.tree.flattenNodes()
		if e.tree.cursor >= len(e.tree.filtered) {
			e.tree.cursor = len(e.tree.filtered) - 1
		}

		// DEBUG
		f, _ := os.Create("/tmp/dbx_explorer_debug.log")
		if f != nil {
			fmt.Fprintf(f, "ToggleExpand Table: name=%q schema=%q\n", node.Name, schema)
			fmt.Fprintf(f, "  node.Expanded=%v\n", node.Expanded)
			fmt.Fprintf(f, "  children count: %d\n", len(node.Children))
			f.Close()
		}

		return func() tea.Msg {
			return TableSelectedMsg{
				Schema: schema,
				Table:  node.Name,
			}
		}
	}

	node.ToggleExpand()
	e.tree.flattenNodes()
	if e.tree.cursor >= len(e.tree.filtered) {
		e.tree.cursor = len(e.tree.filtered) - 1
	}
	return nil
}
