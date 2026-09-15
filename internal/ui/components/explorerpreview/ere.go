package explorerpreview

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/x/ansi"

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
	Schema     string
	Name       string
	Columns    []ColumnBadge
	IsJunction bool
}

type Relationship struct {
	FromColumn   string
	ToTable      string
	ToColumn     string
	Cardinality  string // "N:1" or "1:N"
	IsJunction   bool
}

type ERDiagram struct {
	Center            TableBox
	Outgoing          []Relationship // N:1 (direct FKs, no junction)
	Incoming          []Relationship // 1:N (direct FKs, no junction)
	Junction          []Relationship // N:N (junction tables)
	OutgoingOverflow  int
	IncomingOverflow  int
	JunctionOverflow  int
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

// isJunctionTable checks if a table is a junction (N:N) table.
// A junction table has ≥2 foreign keys referencing different tables.
func isJunctionTable(fks []postgres.ForeignKeyInfo) bool {
	targets := make(map[string]bool)
	for _, fk := range fks {
		targets[fk.RefTable] = true
	}
	return len(targets) >= 2
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

	// Build all columns with badges, then filter to PK/FK only
	allCols := make([]ColumnBadge, 0, len(columns))
	for _, c := range columns {
		allCols = append(allCols, ColumnBadge{
			Name:     c.Name,
			DataType: c.DataType,
			IsPK:     pks[c.Name],
			IsFK:     fkCols[c.Name],
		})
	}

	centerCols := make([]ColumnBadge, 0, len(allCols))
	for _, col := range allCols {
		if col.IsPK || col.IsFK {
			centerCols = append(centerCols, col)
		}
	}
	if len(centerCols) == 0 {
		centerCols = allCols
	}

	diagram := ERDiagram{
		Center: TableBox{
			Schema:     schema,
			Name:       table,
			Columns:    centerCols,
			IsJunction: isJunctionTable(outgoingFKs),
		},
	}

	// Collect all junction candidates (tables that reference center AND are junction)
	junctionFixed := make([]Relationship, 0)
	incomingFixed := make([]Relationship, 0)
	seenIncoming := make(map[string]bool)

	for tableName, fks := range schemaFKs {
		if seenIncoming[tableName] {
			continue
		}
		for _, fk := range fks {
			if fk.RefTable == table && fk.RefSchema == schema {
				sourceIsJunction := isJunctionTable(fks)
				rel := Relationship{
					FromColumn:  fk.Column,
					ToTable:     tableName,
					ToColumn:    fk.RefColumn,
					Cardinality: "1:N",
					IsJunction:  sourceIsJunction,
				}
				if sourceIsJunction {
					junctionFixed = append(junctionFixed, rel)
				} else {
					incomingFixed = append(incomingFixed, rel)
				}
				seenIncoming[tableName] = true
				break
			}
		}
	}

	// Outgoing: current table's FKs → other tables
	// Deduplicate by target table, separate junction from direct
	seenOutgoing := make(map[string]bool)
	outgoingDirect := make([]Relationship, 0)
	for _, fk := range outgoingFKs {
		targetTable := fk.RefTable
		if fk.RefSchema != schema && fk.RefSchema != "" {
			targetTable = fk.RefSchema + "." + fk.RefTable
		}
		if seenOutgoing[targetTable] {
			continue
		}
		targetIsJunction := false
		if targetFKs, ok := schemaFKs[fk.RefTable]; ok {
			targetIsJunction = isJunctionTable(targetFKs)
		}
		rel := Relationship{
			FromColumn:  fk.Column,
			ToTable:     targetTable,
			ToColumn:    fk.RefColumn,
			Cardinality: "N:1",
			IsJunction:  targetIsJunction,
		}
		if targetIsJunction {
			// Only add to junction if not already there
			alreadyJunction := false
			for _, j := range junctionFixed {
				if j.ToTable == targetTable {
					alreadyJunction = true
					break
				}
			}
			if !alreadyJunction {
				junctionFixed = append(junctionFixed, rel)
			}
		} else {
			outgoingDirect = append(outgoingDirect, rel)
		}
		seenOutgoing[targetTable] = true
	}

	// Apply hub cap to each category
	if len(incomingFixed) > MaxNeighbors {
		diagram.IncomingOverflow = len(incomingFixed) - MaxNeighbors
		diagram.Incoming = incomingFixed[:MaxNeighbors]
	} else {
		diagram.Incoming = incomingFixed
	}

	if len(outgoingDirect) > MaxNeighbors {
		diagram.OutgoingOverflow = len(outgoingDirect) - MaxNeighbors
		diagram.Outgoing = outgoingDirect[:MaxNeighbors]
	} else {
		diagram.Outgoing = outgoingDirect
	}

	if len(junctionFixed) > MaxNeighbors {
		diagram.JunctionOverflow = len(junctionFixed) - MaxNeighbors
		diagram.Junction = junctionFixed[:MaxNeighbors]
	} else {
		diagram.Junction = junctionFixed
	}

	// Debug log
	if f, err := os.OpenFile("/tmp/dbx_ere_debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
		fmt.Fprintf(f, "BuildERE: table=%s totalCols=%d centerCols=%d(out of %d) outgoing=%d incoming=%d junction=%d\n",
			table, len(columns), len(diagram.Center.Columns), len(allCols),
			len(diagram.Outgoing), len(diagram.Incoming), len(diagram.Junction))
		f.Close()
	}

	return diagram
}

// --- Navigation ---

type ERDiagramNav struct {
	activeColumn int  // 0=1:N, 1=N:1, 2=N:N
	activeRow    int  // row within column, -1 = no selection
	columnCounts [3]int
}

func NewERDiagramNav(incoming, outgoing, junction int) *ERDiagramNav {
	return &ERDiagramNav{
		activeColumn: 0,
		activeRow:    -1,
		columnCounts: [3]int{incoming, outgoing, junction},
	}
}

func (n *ERDiagramNav) HasSelection() bool {
	return n.activeRow >= 0
}

func (n *ERDiagramNav) ActiveColumn() int {
	return n.activeColumn
}

func (n *ERDiagramNav) ActiveRow() int {
	return n.activeRow
}

func (n *ERDiagramNav) MoveLeft() {
	if n.activeColumn > 0 {
		n.activeColumn--
		n.clampRow()
	}
}

func (n *ERDiagramNav) MoveRight() {
	if n.activeColumn < 2 {
		n.activeColumn++
		n.clampRow()
	}
}

func (n *ERDiagramNav) MoveDown() {
	count := n.columnCounts[n.activeColumn]
	if count == 0 {
		return
	}
	if n.activeRow < count-1 {
		n.activeRow++
	} else if n.activeRow == -1 {
		n.activeRow = 0
	}
}

func (n *ERDiagramNav) MoveUp() {
	if n.activeRow > 0 {
		n.activeRow--
	} else if n.activeRow == 0 {
		n.activeRow = -1
	}
}

func (n *ERDiagramNav) clampRow() {
	count := n.columnCounts[n.activeColumn]
	if count == 0 {
		n.activeRow = -1
	} else if n.activeRow >= count {
		n.activeRow = count - 1
	}
}

func (n *ERDiagramNav) GetSelectedRelationship(diagram *ERDiagram) *Relationship {
	if !n.HasSelection() {
		return nil
	}
	var list []Relationship
	switch n.activeColumn {
	case 0:
		list = diagram.Incoming
	case 1:
		list = diagram.Outgoing
	case 2:
		list = diagram.Junction
	}
	if n.activeRow >= len(list) {
		return nil
	}
	return &list[n.activeRow]
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

// renderBox renders a box for the center table (full detail).
func renderBox(name string, columns []ColumnBadge, width int, cardinality string, isJunction bool, selected bool) string {
	var lines []string
	innerWidth := width - 2
	displayName := TruncateTableName(name)

	if selected {
		displayName = "► " + displayName + " ◄"
	}
	if ansi.StringWidth(displayName) > innerWidth {
		if innerWidth > 1 {
			displayName = displayName[:innerWidth-1] + "…"
		} else {
			displayName = "…"
		}
	}

	lines = append(lines, "┌"+strings.Repeat("─", innerWidth)+"┐")

	namePad := innerWidth - ansi.StringWidth(displayName)
	if namePad < 0 {
		namePad = 0
	}
	leftPad := namePad / 2
	rightPad := namePad - leftPad
	lines = append(lines, "│"+strings.Repeat(" ", leftPad)+displayName+strings.Repeat(" ", rightPad)+"│")

	lines = append(lines, "├"+strings.Repeat("─", innerWidth)+"┤")

	for _, col := range columns {
		badge := "   "
		if col.IsPK {
			badge = "PK "
		} else if col.IsFK {
			badge = "FK "
		}
		colName := TruncateColumnName(col.Name)
		entry := badge + colName + " " + col.DataType
		if ansi.StringWidth(entry) > innerWidth {
			entry = entry[:innerWidth]
		}
		pad := innerWidth - ansi.StringWidth(entry)
		if pad < 0 {
			pad = 0
		}
		lines = append(lines, "│"+entry+strings.Repeat(" ", pad)+"│")
	}

	if isJunction {
		label := "  N:N"
		if ansi.StringWidth(label) > innerWidth {
			label = label[:innerWidth]
		}
		pad := innerWidth - ansi.StringWidth(label)
		lines = append(lines, "│"+label+strings.Repeat(" ", pad)+"│")
	}

	if cardinality != "" {
		label := "  " + cardinality
		if ansi.StringWidth(label) > innerWidth {
			label = label[:innerWidth]
		}
		pad := innerWidth - ansi.StringWidth(label)
		lines = append(lines, "│"+label+strings.Repeat(" ", pad)+"│")
	}

	lines = append(lines, "└"+strings.Repeat("─", innerWidth)+"┘")
	return strings.Join(lines, "\n")
}

// renderCompactBox renders a compact box for neighbor tables (3-4 lines).
func renderCompactBox(name string, fkColumn string, width int, cardinality string, isJunction bool, selected bool) string {
	var lines []string
	innerWidth := width - 2
	displayName := TruncateTableName(name)

	if selected {
		displayName = "► " + displayName + " ◄"
	}
	if ansi.StringWidth(displayName) > innerWidth {
		if innerWidth > 1 {
			displayName = displayName[:innerWidth-1] + "…"
		} else {
			displayName = "…"
		}
	}

	lines = append(lines, "┌"+strings.Repeat("─", innerWidth)+"┐")

	// Name line — left-aligned (more compact)
	namePad := innerWidth - ansi.StringWidth(displayName)
	if namePad < 0 {
		namePad = 0
	}
	lines = append(lines, "│"+displayName+strings.Repeat(" ", namePad)+"│")

	// FK column line
	fkEntry := "FK " + fkColumn
	if ansi.StringWidth(fkEntry) > innerWidth {
		fkEntry = fkEntry[:innerWidth]
	}
	fkPad := innerWidth - ansi.StringWidth(fkEntry)
	if fkPad < 0 {
		fkPad = 0
	}
	lines = append(lines, "│"+fkEntry+strings.Repeat(" ", fkPad)+"│")

	// Cardinality or N:N badge
	if isJunction {
		label := "N:N"
		pad := innerWidth - ansi.StringWidth(label)
		if pad < 0 {
			pad = 0
		}
		lines = append(lines, "│"+label+strings.Repeat(" ", pad)+"│")
	} else if cardinality != "" {
		pad := innerWidth - ansi.StringWidth(cardinality)
		if pad < 0 {
			pad = 0
		}
		lines = append(lines, "│"+cardinality+strings.Repeat(" ", pad)+"│")
	}

	lines = append(lines, "└"+strings.Repeat("─", innerWidth)+"┘")
	return strings.Join(lines, "\n")
}

// renderColumn renders a single column with title and boxes.
func renderColumn(title string, rels []Relationship, overflow int, width int, selectedRow int) []string {
	var lines []string

	// Title line — padded to column width
	titlePad := width - ansi.StringWidth(title)
	if titlePad < 0 {
		titlePad = 0
	}
	lines = append(lines, title+strings.Repeat(" ", titlePad))

	for i, rel := range rels {
		if i > 0 {
			lines = append(lines, "") // spacing between boxes
		}
		isSelected := selectedRow == i
		box := renderCompactBox(rel.ToTable, rel.FromColumn, width, rel.Cardinality, rel.IsJunction, isSelected)
		boxLines := strings.Split(box, "\n")
		lines = append(lines, boxLines...)
	}
	if overflow > 0 {
		if len(lines) > 1 {
			lines = append(lines, "")
		}
		lines = append(lines, fmt.Sprintf(" +%d more", overflow))
	}

	return lines
}

// RenderERDiagram renders a complete ERE diagram as text with3-column layout.
func RenderERDiagram(diagram ERDiagram, paneWidth, paneHeight int, nav *ERDiagramNav) string {
	if len(diagram.Outgoing) == 0 && len(diagram.Incoming) == 0 && len(diagram.Junction) == 0 {
		return "  No relationships for this table"
	}

	// Center box width
	centerWidth := ComputeBoxWidth(diagram.Center.Columns)

	// Column widths: divide available width by 3 (with minimum)
	colWidth := (paneWidth - 4) / 3 // 4 = margins
	if colWidth < MinBoxWidth {
		colWidth = MinBoxWidth
	}
	if colWidth > MaxBoxWidth {
		colWidth = MaxBoxWidth
	}

	// Render center box
	centerBox := renderBox(diagram.Center.Name, diagram.Center.Columns, centerWidth, "", diagram.Center.IsJunction, false)
	centerLines := strings.Split(centerBox, "\n")

	// Center the center box horizontally
	centerTotalWidth := centerWidth + 2 // box width + border chars
	leftPad := (paneWidth - centerTotalWidth) / 2
	if leftPad < 0 {
		leftPad = 0
	}
	paddedCenter := make([]string, len(centerLines))
	for i, line := range centerLines {
		paddedCenter[i] = strings.Repeat(" ", leftPad) + line
	}

	// Selection state per column
	selCol := -1
	selRow := -1
	if nav != nil && nav.HasSelection() {
		selCol = nav.ActiveColumn()
		selRow = nav.ActiveRow()
	}

	// Render3 columns
	col1 := renderColumn("1:N", diagram.Incoming, diagram.IncomingOverflow, colWidth, -1)
	col2 := renderColumn("N:1", diagram.Outgoing, diagram.OutgoingOverflow, colWidth, -1)
	col3 := renderColumn("N:N", diagram.Junction, diagram.JunctionOverflow, colWidth, -1)

	if selCol == 0 {
		col1 = renderColumn("1:N", diagram.Incoming, diagram.IncomingOverflow, colWidth, selRow)
	} else if selCol == 1 {
		col2 = renderColumn("N:1", diagram.Outgoing, diagram.OutgoingOverflow, colWidth, selRow)
	} else if selCol == 2 {
		col3 = renderColumn("N:N", diagram.Junction, diagram.JunctionOverflow, colWidth, selRow)
	}

	// Build final output: center on top, then3 columns below
	var result []string
	result = append(result, paddedCenter...)
	result = append(result, "") // spacing

	// Merge3 columns line by line
	maxColLines := len(col1)
	if len(col2) > maxColLines {
		maxColLines = len(col2)
	}
	if len(col3) > maxColLines {
		maxColLines = len(col3)
	}

	for i := 0; i < maxColLines; i++ {
		left := ""
		if i < len(col1) {
			left = col1[i]
		}
		mid := ""
		if i < len(col2) {
			mid = col2[i]
		}
		right := ""
		if i < len(col3) {
			right = col3[i]
		}
		result = append(result, left+"  "+mid+"  "+right)
	}

	return strings.Join(result, "\n")
}
