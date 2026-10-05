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
	offset   int
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
	t.cursor = 0
	t.offset = 0
	t.flattenNodes()
}

func (t *Tree) flattenNodes() {
	t.allNodes = make([]*Node, 0)
	if t.filter != "" {
		t.collectAllNodes(t.nodes)
	} else {
		t.flatten(t.nodes)
	}
	t.applyFilter()
	t.clampCursor()
	t.scrollToCursor()
}

// clampCursor pulls the cursor back inside the list.
//
// This is HERE, at the one point every list change goes through, because it used to
// be written in each of the three places that shrink the list — toggleExpand, collapse
// and the explorer's toggleColumns — and the fourth, SetFilter, did not have it. The
// result was a crash reachable by hand: arrow to the last table, press /, type a
// filter that matches fewer rows than the cursor's index, and the next Selected()
// indexed past the end and took the process with it. clampOffset only ever clamped
// the scroll, so nothing else caught it.
func (t *Tree) clampCursor() {
	if len(t.filtered) == 0 {
		t.cursor = 0
		return
	}
	if t.cursor < 0 {
		t.cursor = 0
	}
	if t.cursor >= len(t.filtered) {
		t.cursor = len(t.filtered) - 1
	}
}

// clampOffset keeps the window inside the list. It does NOT keep the CURSOR inside the
// window — that is scrollToCursor's other half, and the split is why this function is not
// enough on its own: with nothing holding the invariant, a filter change that left the
// list long enough could park the window on row 5 with the cursor on row 0, and every
// keypress would look like it did nothing because the cursor was above the top of the
// pane.
func (t *Tree) clampOffset() {
	if t.height <= 0 || len(t.filtered) <= t.height {
		t.offset = 0
		return
	}
	if t.offset > len(t.filtered)-t.height {
		t.offset = len(t.filtered) - t.height
	}
	if t.offset < 0 {
		t.offset = 0
	}
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
	// Key handling lives in Explorer, which resolves keys through the registry
	// and dispatches action IDs (handleAction). The tree keeps no raw-key
	// fallback: it is unreachable when a resolver is wired, which is always the
	// case in production.
	return nil, false
}

// scrollToCursor moves the window so the cursor is inside it.
//
// This is HERE because it was in moveDown and nowhere else, and the one place that
// needed it most was the one without it: go_last put the cursor on the last row and
// then called clampOffset, which only ever pulls the window BACK. In a list taller
// than the pane that left the cursor — and the row just selected — scrolled off the
// bottom, so pressing G in a long schema showed you the top of the schema instead of
// the table you asked for.
func (t *Tree) scrollToCursor() {
	if t.height > 0 {
		if t.cursor >= t.offset+t.height {
			t.offset = t.cursor - t.height + 1
		}
		if t.cursor < t.offset {
			t.offset = t.cursor
		}
	}
	// clampOffset runs even at no height, because its first arm is what puts
	// the offset back to zero when the list is shorter than the pane.
	t.clampOffset()
}

func (t *Tree) moveDown() {
	if t.cursor < len(t.filtered)-1 {
		t.cursor++
		t.scrollToCursor()
	}
}

func (t *Tree) moveUp() {
	if t.cursor > 0 {
		t.cursor--
		t.scrollToCursor()
	}
}

func (t *Tree) toggleExpand() tea.Cmd {
	if len(t.filtered) == 0 {
		return nil
	}
	node := t.filtered[t.cursor]
	if !node.IsLeaf() {
		node.ToggleExpand()
		t.flattenNodes() // clamps the cursor with the rest
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
		t.flattenNodes() // clamps the cursor with the rest
	} else if node.Parent != nil {
		// Moving to the parent does NOT change the list, so it does not go
		// through flattenNodes and has to find its own index. If the parent is
		// not in the list the cursor is left alone — there is nowhere better to
		// send it, and leaving it put keeps it in range.
		for i, n := range t.filtered {
			if n == node.Parent {
				t.cursor = i
				break
			}
		}
		// The cursor can land a long way from where the window is — collapsing a
		// table takes you to its schema, which may be several rows up — so the
		// window has to follow HERE too. It did not, and in a one-row window
		// pressing collapse moved the cursor off the top of the pane while the
		// pane kept showing the bottom: the row you were about to act on was not
		// the row you could see.
		t.scrollToCursor()
	}
	return nil
}

// Selected is the node under the cursor, or nil when the list is empty.
//
// The bounds check is not belt-and-braces for the clamp in flattenNodes: a public
// accessor that indexes a slice is a crash waiting for the next caller that sets the
// cursor directly. The clamp is what KEEPS the cursor honest, and a separate test
// asserts the cursor is in range after every kind of list change — so a broken clamp
// fails loudly there instead of being hidden by this nil.
func (t *Tree) Selected() *Node {
	if t.cursor < 0 || t.cursor >= len(t.filtered) {
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
	start := t.offset
	end := len(t.filtered)
	if t.height > 0 && end > start+t.height {
		end = start + t.height
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
