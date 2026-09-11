package explorer

type NodeType int

const (
	NodeDatabase NodeType = iota
	NodeSchema
	NodeTable
	NodeColumn
	NodeIndex
	NodeConstraint
)

func (t NodeType) String() string {
	switch t {
	case NodeDatabase:
		return "database"
	case NodeSchema:
		return "schema"
	case NodeTable:
		return "table"
	case NodeColumn:
		return "column"
	case NodeIndex:
		return "index"
	case NodeConstraint:
		return "constraint"
	default:
		return "unknown"
	}
}

func (t NodeType) Icon() string {
	switch t {
	case NodeDatabase:
		return ""
	case NodeSchema:
		return ""
	case NodeTable:
		return ""
	case NodeColumn:
		return "  "
	case NodeIndex:
		return "  "
	case NodeConstraint:
		return "  "
	default:
		return "  "
	}
}

type Node struct {
	ID       string
	Type     NodeType
	Name     string
	Parent   *Node
	Children []*Node
	Expanded bool
	Selected bool
	Metadata map[string]interface{}
}

func NewNode(id string, nodeType NodeType, name string) *Node {
	return &Node{
		ID:       id,
		Type:     nodeType,
		Name:     name,
		Children: make([]*Node, 0),
		Metadata: make(map[string]interface{}),
	}
}

func (n *Node) AddChild(child *Node) {
	child.Parent = n
	n.Children = append(n.Children, child)
}

func (n *Node) ToggleExpand() {
	n.Expanded = !n.Expanded
}

func (n *Node) IsLeaf() bool {
	return len(n.Children) == 0
}

func (n *Node) RowCount() int {
	if count, ok := n.Metadata["row_count"]; ok {
		if c, ok := count.(int); ok {
			return c
		}
	}
	return 0
}
