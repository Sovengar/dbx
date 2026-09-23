package explorer

import (
	"fmt"
	"image/color"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/theme"
	"github.com/buble/dbx/internal/ui/bordered"
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
	keybindings config.Resolver
	width       int
	height      int
	focused     bool
	filtering   bool
}

func New(styles *theme.Styles, zones interface{}, keybindings config.Resolver) *Explorer {
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
	e.tree.SetWidth(w - 2) // border takes 1 col per side
}

func (e *Explorer) SetHeight(h int) {
	e.height = h
	e.tree.SetHeight(h - 2) // border takes 1 row top + 1 row bottom
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

		if e.keybindings != nil {
			if action, ok := e.keybindings.Resolve(msg.String(), config.ContextExplorer); ok {
				if cmd, handled := e.handleAction(action); handled {
					return cmd, handled
				}
			}
		}

		return e.tree.Update(msg)
	}
	return nil, false
}

// HandleAction dispatches an action ID without synthesizing a key press. Mouse
// handlers use it, and it deliberately works regardless of focus so the pane
// under the cursor reacts.
func (e *Explorer) HandleAction(id config.ActionID) (tea.Cmd, bool) {
	return e.handleAction(id)
}

// handleAction dispatches a resolved explorer action. Returns handled=false so
// unknown keys fall through to the tree.
func (e *Explorer) handleAction(action config.ActionID) (tea.Cmd, bool) {
	switch action {
	case "navigate_down":
		e.tree.moveDown()
		return nil, true
	case "navigate_up":
		e.tree.moveUp()
		return nil, true
	case "go_first":
		e.tree.cursor = 0
		e.tree.offset = 0
		return nil, true
	case "go_last":
		e.tree.cursor = len(e.tree.filtered) - 1
		if e.tree.cursor < 0 {
			e.tree.cursor = 0
		}
		e.tree.clampOffset()
		return nil, true
	case "collapse_node":
		return e.tree.collapse(), true
	case "toggle_columns":
		return e.toggleColumns(), true
	case "expand_node":
		if node := e.tree.Selected(); node != nil && node.Type == NodeTable {
			schema := ""
			if s, ok := node.Metadata["schema"].(string); ok {
				schema = s
			}
			return func() tea.Msg {
				return TableSelectedMsg{Schema: schema, Table: node.Name}
			}, true
		}
		return e.tree.toggleExpand(), true
	case "drop_table":
		if node := e.tree.Selected(); node != nil && node.Type == NodeTable {
			schema := ""
			if s, ok := node.Metadata["schema"].(string); ok {
				schema = s
			}
			return func() tea.Msg {
				return DropTableMsg{Schema: schema, Table: node.Name}
			}, true
		}
		return nil, true
	case "view_ddl":
		if node := e.tree.Selected(); node != nil && node.Type == NodeTable {
			schema := ""
			if s, ok := node.Metadata["schema"].(string); ok {
				schema = s
			}
			return func() tea.Msg {
				return ViewDDLMsg{Schema: schema, Table: node.Name}
			}, true
		}
		return nil, true
	case "new_table":
		schema := ""
		if node := e.tree.Selected(); node != nil {
			if s, ok := node.Metadata["schema"].(string); ok {
				schema = s
			}
		}
		return func() tea.Msg {
			return NewTableMsg{Schema: schema}
		}, true
	case "filter_tables":
		e.StartFilter()
		return nil, true
	case "refresh_schema":
		return func() tea.Msg {
			return ExplorerRefreshMsg{}
		}, true
	}
	return nil, false
}

func (e *Explorer) toggleColumns() tea.Cmd {
	node := e.tree.Selected()
	if node == nil {
		return nil
	}
	if node.Type == NodeTable {
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
		return nil
	}
	if node.Type == NodeSchema {
		node.ToggleExpand()
		e.tree.flattenNodes()
		if e.tree.cursor >= len(e.tree.filtered) {
			e.tree.cursor = len(e.tree.filtered) - 1
		}
		e.tree.clampOffset()
	}
	return nil
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

	if e.filtering {
		filter := e.styles.Text.Render("Filter: " + e.tree.filter + "_")
		s.WriteString(filter)
		s.WriteString("\n")
	} else if e.tree.filter != "" {
		filter := e.styles.TextMuted.Render("Filter: " + e.tree.filter)
		s.WriteString(filter)
		s.WriteString("\n")
	}

	s.WriteString(e.tree.View())

	output := s.String()

	// Choose border style and color based on focus
	border := lipgloss.ThickBorder()
	var borderFg color.Color
	if e.focused {
		borderFg = e.styles.BorderActive.GetBorderTopForeground()
	} else {
		borderFg = e.styles.Border.GetBorderTopForeground()
	}

	return bordered.RenderWithTitleEx(border, borderFg, bordered.AlignLeft, " Explorer ", output, e.width, e.height+2)
}

func (e *Explorer) StartFilter() {
	e.filtering = true
	e.tree.SetFilter("")
}

func (e *Explorer) IsFiltering() bool {
	return e.filtering
}

// HandleClick selects the tree node under a click at pane-relative coordinates
// (0,0 = top-left of the explorer's bordered box).
func (e *Explorer) HandleClick(y int) bool {
	if e.tree == nil || len(e.tree.filtered) == 0 {
		return false
	}

	listY := y - 1 // skip the top border
	if e.filtering || e.tree.filter != "" {
		listY-- // skip the filter line
	}

	filteredIndex := e.tree.offset + listY
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
			_, _ = fmt.Fprintf(f, "ToggleExpand Table: name=%q schema=%q\n", node.Name, schema)
			_, _ = fmt.Fprintf(f, "  node.Expanded=%v\n", node.Expanded)
			_, _ = fmt.Fprintf(f, "  children count: %d\n", len(node.Children))
			_ = f.Close()
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
