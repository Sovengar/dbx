package explorerpreview

import (
	"fmt"
	"strings"

	"github.com/buble/dbx/internal/drivers/postgres"
)

const (
	MinBoxWidth    = 20
	MaxBoxWidth    = 50
	MaxTableName   = 40
	MaxColumnName  = 30
	MaxNeighbors   = 10
	boxHPad        = 2 // padding inside box borders
)

type ColumnBadge struct {
	Name     string
	DataType string
	IsPK     bool
	IsFK     bool
}

type TableBox struct {
	Schema  string
	Name    string
	Columns []ColumnBadge
}

type Relationship struct {
	FromColumn   string
	ToTable      string
	ToColumn     string
	Cardinality  string // "N:1" or "1:N"
}

type ERDiagram struct {
	Center            TableBox
	Outgoing          []Relationship
	Incoming          []Relationship
	OutgoingOverflow  int
	IncomingOverflow  int
}

func TruncateTableName(name string) string {
	if len(name) > MaxTableName {
		return name[:MaxTableName] + "…"
	}
	return name
}

func TruncateColumnName(name string) string {
	if len(name) > MaxColumnName {
		return name[:MaxColumnName] + "…"
	}
	return name
}

func ComputeBoxWidth(columns []ColumnBadge) int {
	maxLen := 0
	for _, col := range columns {
		w := len(col.Name) + len(col.DataType) + 4 // "PK " or "FK " or "   "
		if w > maxLen {
			maxLen = w
		}
	}
	width := maxLen
	if width < MinBoxWidth {
		width = MinBoxWidth
	}
	if width > MaxBoxWidth {
		width = MaxBoxWidth
	}
	return width
}

// buildPKSet extracts primary key column names from constraints.
func buildPKSet(constraints []postgres.ConstraintInfo) map[string]bool {
	pks := make(map[string]bool)
	for _, c := range constraints {
		if strings.ToUpper(c.Type) == "PRIMARY KEY" {
			for _, col := range strings.Split(c.Columns, ", ") {
				pks[strings.TrimSpace(col)] = true
			}
		}
	}
	return pks
}

// BuildERDiagram constructs the relationship graph for a table.
func BuildERDiagram(
	schema, table string,
	columns []postgres.ColumnInfo,
	constraints []postgres.ConstraintInfo,
	outgoingFKs []postgres.ForeignKeyInfo,
	schemaFKs map[string][]postgres.ForeignKeyInfo,
) ERDiagram {
	pks := buildPKSet(constraints)

	fkCols := make(map[string]bool)
	for _, fk := range outgoingFKs {
		fkCols[fk.Column] = true
	}

	centerCols := make([]ColumnBadge, len(columns))
	for i, c := range columns {
		centerCols[i] = ColumnBadge{
			Name:     c.Name,
			DataType: c.DataType,
			IsPK:     pks[c.Name],
			IsFK:     fkCols[c.Name],
		}
	}

	diagram := ERDiagram{
		Center: TableBox{
			Schema:  schema,
			Name:    table,
			Columns: centerCols,
		},
	}

	// Outgoing: current table's FKs → other tables
	// Deduplicate by target table — one box per table
	seenOutgoing := make(map[string]bool)
	for _, fk := range outgoingFKs {
		targetTable := fk.RefTable
		if fk.RefSchema != schema && fk.RefSchema != "" {
			targetTable = fk.RefSchema + "." + fk.RefTable
		}
		if seenOutgoing[targetTable] {
			continue
		}
		diagram.Outgoing = append(diagram.Outgoing, Relationship{
			FromColumn:  fk.Column,
			ToTable:     targetTable,
			ToColumn:    fk.RefColumn,
			Cardinality: "N:1",
		})
		seenOutgoing[targetTable] = true
	}

	// Incoming: other tables' FKs → current table
	// Deduplicate by source table — one box per table, even with multiple FKs
	seenIncoming := make(map[string]bool)
	incomingFixed := make([]Relationship, 0)
	for tableName, fks := range schemaFKs {
		if seenIncoming[tableName] {
			continue
		}
		for _, fk := range fks {
			if fk.RefTable == table && fk.RefSchema == schema {
				incomingFixed = append(incomingFixed, Relationship{
					FromColumn:  fk.Column,
					ToTable:     tableName,
					ToColumn:    fk.RefColumn,
					Cardinality: "1:N",
				})
				seenIncoming[tableName] = true
				break
			}
		}
	}

	// Apply hub cap
	if len(incomingFixed) > MaxNeighbors {
		diagram.IncomingOverflow = len(incomingFixed) - MaxNeighbors
		diagram.Incoming = incomingFixed[:MaxNeighbors]
	} else {
		diagram.Incoming = incomingFixed
	}

	if len(diagram.Outgoing) > MaxNeighbors {
		diagram.OutgoingOverflow = len(diagram.Outgoing) - MaxNeighbors
		diagram.Outgoing = diagram.Outgoing[:MaxNeighbors]
	}

	return diagram
}

// --- Navigation ---

type ERDiagramNav struct {
	totalNeighbors int
	selectedIndex  int // -1 = no selection
}

func NewERDiagramNav(totalNeighbors int) *ERDiagramNav {
	return &ERDiagramNav{
		totalNeighbors: totalNeighbors,
		selectedIndex:  -1,
	}
}

func (n *ERDiagramNav) HasSelection() bool {
	return n.selectedIndex >= 0
}

func (n *ERDiagramNav) SelectedIndex() int {
	return n.selectedIndex
}

func (n *ERDiagramNav) MoveDown() {
	if n.totalNeighbors == 0 {
		return
	}
	if n.selectedIndex < n.totalNeighbors-1 {
		n.selectedIndex++
	}
}

func (n *ERDiagramNav) MoveUp() {
	if n.selectedIndex > 0 {
		n.selectedIndex--
	} else if n.selectedIndex == 0 {
		n.selectedIndex = -1
	}
}

func (n *ERDiagramNav) GetSelectedNeighbor(neighbors []Relationship) *Relationship {
	if !n.HasSelection() || n.selectedIndex >= len(neighbors) {
		return nil
	}
	return &neighbors[n.selectedIndex]
}

// --- Viewport ---

type ERDiagramViewport struct {
	PaneHeight   int
	ContentHeight int
	ScrollOffset  int
}

func NewERDiagramViewport(paneHeight, contentHeight int) *ERDiagramViewport {
	return &ERDiagramViewport{
		PaneHeight:   paneHeight,
		ContentHeight: contentHeight,
	}
}

func (v *ERDiagramViewport) MaxScroll() int {
	max := v.ContentHeight - v.PaneHeight
	if max < 0 {
		return 0
	}
	return max
}

func (v *ERDiagramViewport) ScrollDown() {
	if v.ScrollOffset < v.MaxScroll() {
		v.ScrollOffset++
	}
}

func (v *ERDiagramViewport) ScrollUp() {
	if v.ScrollOffset > 0 {
		v.ScrollOffset--
	}
}

func (v *ERDiagramViewport) JumpTop() {
	v.ScrollOffset = 0
}

func (v *ERDiagramViewport) JumpBottom() {
	v.ScrollOffset = v.MaxScroll()
}

func (v *ERDiagramViewport) Reset() {
	v.ScrollOffset = 0
}

// --- Renderer ---

func renderBox(name string, columns []ColumnBadge, width int, isActive bool, styles ...interface{}) string {
	var lines []string

	// Truncate name if needed
	displayName := TruncateTableName(name)

	// Ensure width fits the name
	nameWidth := len(displayName) + 4 // " " + name + " "
	if nameWidth+2 > width {
		width = nameWidth + 2
	}

	// Top border
	lines = append(lines, "┌"+strings.Repeat("─", width-2)+"┐")

	// Name line
	namePad := width - 2 - len(displayName)
	leftPad := namePad / 2
	rightPad := namePad - leftPad
	lines = append(lines, "│"+strings.Repeat(" ", leftPad)+displayName+strings.Repeat(" ", rightPad)+"│")

	// Separator
	lines = append(lines, "├"+strings.Repeat("─", width-2)+"┤")

	// Columns
	for _, col := range columns {
		badge := "   "
		if col.IsPK {
			badge = "PK "
		} else if col.IsFK {
			badge = "FK "
		}
		colName := TruncateColumnName(col.Name)
		entry := badge + colName + " " + col.DataType
		if len(entry) > width-2 {
			entry = entry[:width-2]
		}
		pad := width - 2 - len(entry)
		lines = append(lines, "│"+entry+strings.Repeat(" ", pad)+"│")
	}

	// Bottom border
	lines = append(lines, "└"+strings.Repeat("─", width-2)+"┘")

	return strings.Join(lines, "\n")
}

// RenderERDiagram renders a complete ERE diagram as text.
func RenderERDiagram(diagram ERDiagram, paneWidth, paneHeight int) string {
	if len(diagram.Outgoing) == 0 && len(diagram.Incoming) == 0 {
		return "  No relationships for this table"
	}

	centerWidth := ComputeBoxWidth(diagram.Center.Columns)
	centerBox := renderBox(diagram.Center.Name, diagram.Center.Columns, centerWidth, false)
	centerLines := strings.Split(centerBox, "\n")
	centerHeight := len(centerLines)

	// Track neighbor boxes with their cardinality labels
	type neighborEntry struct {
		box   string
		label string
	}
	var neighbors []neighborEntry

	for _, rel := range diagram.Outgoing {
		cols := []ColumnBadge{
			{Name: rel.FromColumn, DataType: "", IsFK: true},
		}
		box := renderBox(rel.ToTable, cols, centerWidth, false)
		neighbors = append(neighbors, neighborEntry{box: box, label: rel.Cardinality})
	}

	for _, rel := range diagram.Incoming {
		cols := []ColumnBadge{
			{Name: rel.FromColumn, DataType: "", IsFK: true},
		}
		box := renderBox(rel.ToTable, cols, centerWidth, false)
		neighbors = append(neighbors, neighborEntry{box: box, label: rel.Cardinality})
	}

	// Build right-side lines from neighbors
	var rightLines []string
	var rightLabels []string // cardinality label per line (empty = no label)
	for _, n := range neighbors {
		boxLines := strings.Split(n.box, "\n")
		if len(rightLines) > 0 {
			rightLines = append(rightLines, "") // spacing
			rightLabels = append(rightLabels, "")
		}
		for j, bl := range boxLines {
			rightLines = append(rightLines, bl)
			// Put cardinality label on the separator line (line 2 = "├───┤")
			if j == 2 && n.label != "" {
				rightLabels = append(rightLabels, n.label)
			} else {
				rightLabels = append(rightLabels, "")
			}
		}
	}

	// Overflow indicators
	if diagram.OutgoingOverflow > 0 {
		if len(rightLines) > 0 {
			rightLines = append(rightLines, "")
			rightLabels = append(rightLabels, "")
		}
		rightLines = append(rightLines, fmt.Sprintf("  +%d more", diagram.OutgoingOverflow))
		rightLabels = append(rightLabels, "")
	}
	if diagram.IncomingOverflow > 0 {
		if len(rightLines) > 0 {
			rightLines = append(rightLines, "")
			rightLabels = append(rightLabels, "")
		}
		rightLines = append(rightLines, fmt.Sprintf("  +%d more", diagram.IncomingOverflow))
		rightLabels = append(rightLabels, "")
	}

	// Layout: center box on the left, neighbors on the right
	leftWidth := centerWidth + 4 // box + margin

	// Merge side by side
	maxLines := centerHeight
	if len(rightLines) > maxLines {
		maxLines = len(rightLines)
	}

	var result []string
	for i := 0; i < maxLines; i++ {
		left := ""
		if i < len(centerLines) {
			left = centerLines[i]
		}
		// Pad to fixed left width
		if len(left) < leftWidth {
			left += strings.Repeat(" ", leftWidth-len(left))
		}

		right := ""
		if i < len(rightLines) {
			right = rightLines[i]
		}

		// Edge connector with cardinality label
		connector := ""
		if right != "" && i < centerHeight {
			// Inside center box height: show connector
			if i > 0 && i < centerHeight-1 {
				label := rightLabels[i]
				if label == "" {
					connector = strings.Repeat("─", 3) + " "
				} else {
					connector = "─" + label + "─ "
				}
			}
		} else if right != "" && rightLabels[i] != "" {
			// Below center box: only show connector for labeled lines
			label := rightLabels[i]
			connector = "─" + label + "─ "
		}

		result = append(result, left+connector+right)
	}

	return strings.Join(result, "\n")
}
