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
	for _, fk := range outgoingFKs {
		targetTable := fk.RefTable
		if fk.RefSchema != schema && fk.RefSchema != "" {
			targetTable = fk.RefSchema + "." + fk.RefTable
		}
		diagram.Outgoing = append(diagram.Outgoing, Relationship{
			FromColumn:  fk.Column,
			ToTable:     targetTable,
			ToColumn:    fk.RefColumn,
			Cardinality: "N:1",
		})
	}

	// Incoming: other tables' FKs → current table
	for _, fks := range schemaFKs {
		for _, fk := range fks {
			if fk.RefTable == table && fk.RefSchema == schema {
				sourceTable := fk.RefTable // just for the relationship
				_ = sourceTable
				diagram.Incoming = append(diagram.Incoming, Relationship{
					FromColumn:  fk.Column,
					ToTable:     fk.Name, // placeholder, we'll fix below
					ToColumn:    fk.RefColumn,
					Cardinality: "1:N",
				})
			}
		}
	}

	// Fix incoming: we need the source table name, not the FK name.
	// Re-scan schemaFKs to find the table that owns each FK.
	incomingFixed := make([]Relationship, 0)
	for tableName, fks := range schemaFKs {
		for _, fk := range fks {
			if fk.RefTable == table && fk.RefSchema == schema {
				incomingFixed = append(incomingFixed, Relationship{
					FromColumn:  fk.Column,
					ToTable:     tableName,
					ToColumn:    fk.RefColumn,
					Cardinality: "1:N",
				})
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

	// Build neighbor boxes
	var outgoingBoxes []string
	for _, rel := range diagram.Outgoing {
		// We don't have full column info for neighbors, just render the table name + involved columns
		cols := []ColumnBadge{
			{Name: rel.ToTable, DataType: "", IsFK: true},
		}
		box := renderBox(rel.ToTable, cols, centerWidth, false)
		outgoingBoxes = append(outgoingBoxes, box)
	}

	var incomingBoxes []string
	for _, rel := range diagram.Incoming {
		cols := []ColumnBadge{
			{Name: rel.ToTable, DataType: "", IsFK: true},
		}
		box := renderBox(rel.ToTable, cols, centerWidth, false)
		incomingBoxes = append(incomingBoxes, box)
	}

	// Overflow indicators
	if diagram.OutgoingOverflow > 0 {
		outgoingBoxes = append(outgoingBoxes, fmt.Sprintf("  +%d more", diagram.OutgoingOverflow))
	}
	if diagram.IncomingOverflow > 0 {
		incomingBoxes = append(incomingBoxes, fmt.Sprintf("  +%d more", diagram.IncomingOverflow))
	}

	// Layout: center box on the left, neighbors on the right
	// Split pane: left half for center, right half for neighbors
	halfWidth := paneWidth / 2

	// Calculate total height needed
	centerLines := strings.Split(centerBox, "\n")
	centerHeight := len(centerLines)

	// Neighbor section: outgoing on top, incoming on bottom
	var neighborLines []string
	if len(outgoingBoxes) > 0 {
		neighborLines = append(neighborLines, "── Outgoing (N:1) ──")
		for _, box := range outgoingBoxes {
			neighborLines = append(neighborLines, strings.Split(box, "\n")...)
			neighborLines = append(neighborLines, "") // spacing
		}
	}
	if len(incomingBoxes) > 0 {
		neighborLines = append(neighborLines, "── Incoming (1:N) ──")
		for _, box := range incomingBoxes {
			neighborLines = append(neighborLines, strings.Split(box, "\n")...)
			neighborLines = append(neighborLines, "") // spacing
		}
	}

	// Build edge lines between center and neighbors
	// For simplicity, draw horizontal connectors
	edgePad := 3
	edgeLine := strings.Repeat("─", edgePad)

	// Merge left (center) and right (neighbors) side by side
	maxLines := centerHeight
	if len(neighborLines) > maxLines {
		maxLines = len(neighborLines)
	}

	var result []string
	for i := 0; i < maxLines; i++ {
		left := ""
		if i < len(centerLines) {
			left = centerLines[i]
		}
		// Pad left to halfWidth
		if len(left) < halfWidth {
			left += strings.Repeat(" ", halfWidth-len(left))
		}

		right := ""
		if i < len(neighborLines) {
			right = neighborLines[i]
		}

		// Add edge connector at the center box's right edge
		connector := ""
		if i > 0 && i < centerHeight-1 && i < len(neighborLines) {
			connector = edgeLine + " "
		}

		result = append(result, left+connector+right)
	}

	return strings.Join(result, "\n")
}
