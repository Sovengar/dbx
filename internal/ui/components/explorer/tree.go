package explorer

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/buble/dbx/internal/theme"
	ui "github.com/buble/dbx/internal/ui"
)

type Tree struct {
	nodes    []*Node
	cursor   int
	styles   *theme.Styles
	width    int
	height   int
	filter   string
	filtered []*Node
	allNodes []*Node
}

func NewTree(styles *theme.Styles) *Tree {
	return &Tree{
		nodes:    make([]*Node, 0),
		styles:   styles,
		filtered: make([]*Node, 0),
	}
}

func (t *Tree) SetNodes(nodes []*Node) {
	t.nodes = nodes
	t.flattenNodes()
	t.cursor = 0
}

func (t *Tree) flattenNodes() {
	t.allNodes = make([]*Node, 0)
	if t.filter != "" {
		t.collectAllNodes(t.nodes)
	} else {
		t.flatten(t.nodes)
	}
	t.applyFilter()
}

func (t *Tree) flatten(nodes []*Node) {
	for _, n := range nodes {
		t.allNodes = append(t.allNodes, n)
		if n.Expanded && !n.IsLeaf() {
			t.flatten(n.Children)
		}
	}
}

func (t *Tree) collectAllNodes(nodes []*Node) {
	for _, n := range nodes {
		t.allNodes = append(t.allNodes, n)
		if !n.IsLeaf() {
			t.collectAllNodes(n.Children)
		}
	}
}

func (t *Tree) applyFilter() {
	if t.filter == "" {
		t.filtered = t.allNodes
		return
	}

	matches := make(map[*Node]bool)
	for _, n := range t.allNodes {
		if n.Type == NodeTable && strings.Contains(strings.ToLower(n.Name), strings.ToLower(t.filter)) {
			matches[n] = true
		}
	}

	included := make(map[*Node]bool)
	for match := range matches {
		for node := match; node != nil; node = node.Parent {
			if !included[node] {
				included[node] = true
			}
		}
	}

	t.filtered = make([]*Node, 0)
	t.buildFilteredList(t.nodes, included)
}

func (t *Tree) buildFilteredList(nodes []*Node, included map[*Node]bool) {
	for _, n := range nodes {
		if included[n] {
			t.filtered = append(t.filtered, n)
			if !n.IsLeaf() {
				if n.Type == NodeTable && n.Expanded {
					t.addAllChildren(n)
				} else {
					t.buildFilteredList(n.Children, included)
				}
			}
		}
	}
}

func (t *Tree) addAllChildren(n *Node) {
	for _, child := range n.Children {
		t.filtered = append(t.filtered, child)
		if !child.IsLeaf() && child.Expanded {
			t.addAllChildren(child)
		}
	}
}

func (t *Tree) SetFilter(filter string) {
	t.filter = filter
	t.flattenNodes()
}

func (t *Tree) ClearFilter() {
	t.filter = ""
	t.flattenNodes()
}

func (t *Tree) Update(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return t.handleKey(msg)
	}
	return nil, false
}

func (t *Tree) handleKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	key := msg.String()
	switch key {
	case "j", "down":
		t.moveDown()
		return nil, true
	case "k", "up":
		t.moveUp()
		return nil, true
	case "enter", "l":
		return t.toggleExpand(), true
	case "backspace", "h":
		return t.collapse(), true
	case "g":
		t.cursor = 0
		return nil, true
	case "G":
		t.cursor = len(t.filtered) - 1
		if t.cursor < 0 {
			t.cursor = 0
		}
		return nil, true
	}
	return nil, false
}

func (t *Tree) moveDown() {
	if t.cursor < len(t.filtered)-1 {
		t.cursor++
	}
}

func (t *Tree) moveUp() {
	if t.cursor > 0 {
		t.cursor--
	}
}

func (t *Tree) toggleExpand() tea.Cmd {
	if len(t.filtered) == 0 {
		return nil
	}
	node := t.filtered[t.cursor]
	if !node.IsLeaf() {
		node.ToggleExpand()
		t.flattenNodes()
		if t.cursor >= len(t.filtered) {
			t.cursor = len(t.filtered) - 1
		}
	}
	return nil
}

func (t *Tree) collapse() tea.Cmd {
	if len(t.filtered) == 0 {
		return nil
	}
	node := t.filtered[t.cursor]
	if node.Expanded {
		node.Expanded = false
		t.flattenNodes()
		if t.cursor >= len(t.filtered) {
			t.cursor = len(t.filtered) - 1
		}
	} else if node.Parent != nil {
		for i, n := range t.filtered {
			if n == node.Parent {
				t.cursor = i
				break
			}
		}
	}
	return nil
}

func (t *Tree) Selected() *Node {
	if len(t.filtered) == 0 {
		return nil
	}
	return t.filtered[t.cursor]
}

func (t *Tree) FindTable(schema, table string) *Node {
	return t.findTableRecursive(t.nodes, schema, table)
}

func (t *Tree) findTableRecursive(nodes []*Node, schema, table string) *Node {
	for _, n := range nodes {
		if n.Type == NodeTable && n.Name == table {
			if s, ok := n.Metadata["schema"].(string); ok && s == schema {
				return n
			}
		}
		if len(n.Children) > 0 {
			if found := t.findTableRecursive(n.Children, schema, table); found != nil {
				return found
			}
		}
	}
	return nil
}

func (t *Tree) SetWidth(w int) {
	t.width = w
}

func (t *Tree) SetHeight(h int) {
	t.height = h
}

func (t *Tree) View() string {
	if len(t.filtered) == 0 {
		return t.styles.TextMuted.Render("  No items")
	}

	var s strings.Builder
	start := 0
	end := len(t.filtered)
	if t.height > 0 && end > t.height {
		end = t.height
	}

	availableWidth := t.width - 4

	for i := start; i < end; i++ {
		node := t.filtered[i]
		if node == nil {
			continue
		}

		indent := strings.Repeat("  ", t.getDepth(node))
		expandIcon := t.getExpandIcon(node)
		icon := node.Type.Icon()
		name := t.getNodeName(node)

		prefixLen := len(indent) + len(expandIcon) + 1 + len(icon) + 1
		nameMaxWidth := availableWidth - prefixLen
		if nameMaxWidth < 0 {
			nameMaxWidth = 0
		}
		name = truncateWithEllipsis(name, nameMaxWidth)

		line := fmt.Sprintf("%s%s %s %s", indent, expandIcon, icon, name)

		id := fmt.Sprintf("tree-%d", i)
		if i == t.cursor {
			selected := t.styles.Selected.
				Width(availableWidth).
				Render(line)
			s.WriteString(ui.Mark(id, selected))
		} else {
			s.WriteString(ui.Mark(id, t.styles.Text.Render(line)))
		}
		s.WriteString("\n")
	}

	return s.String()
}

func (t *Tree) getNodeName(node *Node) string {
	switch node.Type {
	case NodeTable:
		count := node.RowCount()
		if count > 0 {
			return fmt.Sprintf("%s (%d rows)", node.Name, count)
		}
		return node.Name
	case NodeColumn:
		return t.formatColumn(node)
	default:
		return node.Name
	}
}

func (t *Tree) getDepth(node *Node) int {
	depth := 0
	for node.Parent != nil {
		depth++
		node = node.Parent
	}
	return depth
}

func (t *Tree) getExpandIcon(node *Node) string {
	if node.IsLeaf() {
		return "  "
	}
	if node.Expanded {
		return " "
	}
	return " "
}

func (t *Tree) formatColumn(node *Node) string {
	name := node.Name
	dataType, _ := node.Metadata["data_type"].(string)
	isNullable, _ := node.Metadata["is_nullable"].(string)
	defaultVal, _ := node.Metadata["column_default"].(*string)

	if dataType == "" {
		return name
	}

	result := name + "  " + dataType

	if isNullable == "YES" {
		result += "  NULL"
	}

	if defaultVal != nil && *defaultVal != "" {
		result += "  = " + *defaultVal
	}

	return result
}

func truncateWithEllipsis(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= maxWidth {
		return s
	}
	return ansi.Truncate(s, maxWidth, "…")
}
