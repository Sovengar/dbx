# Specification: ERE Diagram Viewer

## Functional Requirements

### FR-1: Diagram Rendering
- The ERE tab renders a text-based Entity-Relationship diagram using Unicode box-drawing characters (`┌─┐`, `├─┤`, `└─┘`, `│`, `─`)
- The current table is rendered as the center box with ALL columns listed
- Columns are marked: `PK` for primary key columns, `FK` for foreign key columns
- Neighbor tables (referenced by FK or referencing the current table) are rendered as smaller boxes showing only the columns involved in the relationship
- Edges connect FK columns to their referenced counterparts with lines (`──`, `──▶`)
- Edge labels show cardinality: `N:1` (current FK → referenced PK), `1:N` (other FK → current PK)

### FR-2: Star Layout
- Center box is positioned at the left side of the diagram
- Outgoing FK tables (tables the current table references) are stacked vertically to the right-top
- Incoming FK tables (tables that reference the current table) are stacked vertically to the right-bottom
- Boxes are separated by vertical spacing (2 lines between boxes)
- Edge lines route from the center box's FK column row to the neighbor box's referenced column row

### FR-3: Navigation
- A cursor highlights the currently selected neighbor box
- `↑`/`↓` (or `j`/`k`) moves the cursor between neighbor boxes
- `Enter` on a selected neighbor re-centers the diagram on that table
- Re-centering updates the explorer tree selection to keep state in sync
- Scroll offset resets to 0 on table change

### FR-4: Viewport Scrolling
- When the diagram height exceeds the pane height, the content scrolls
- `↑`/`↓` scrolls when no neighbor is selected or when at the edges
- `g` jumps to top, `G` jumps to bottom (standard vim-style)
- Scroll state resets when the centered table changes

### FR-5: Dynamic Sizing
- Box width is computed from the longest column name/type in the table (min 20, max 50 chars)
- Column names longer than 30 chars are truncated with `…`
- Table names longer than 40 chars are truncated with `…`
- Cross-schema references show table name as `schema.table`

### FR-6: Empty State
- Tables with no FKs in either direction show: "No relationships for this table"
- Schema-wide FK loading failure shows: "Could not load relationships"

### FR-7: Hub Table Handling
- Maximum 10 neighbor boxes rendered per direction (outgoing/incoming)
- If more exist, a "+N more" indicator is shown at the bottom of the stack
- No crash or layout overflow on tables with 30+ FKs

## Acceptance Criteria

- [ ] AC-1: Pressing `6` on a table with relationships renders a text ERE diagram
- [ ] AC-2: Center box shows current table name + all columns with PK/FK indicators
- [ ] AC-3: Outgoing FKs render as edges to referenced tables
- [ ] AC-4: Incoming FKs render as edges from referencing tables
- [ ] AC-5: Edge cardinality labels shown (`1:N` / `N:1`)
- [ ] AC-6: Diagram styled via `theme.Styles` (no hardcoded colors)
- [ ] AC-7: Diagram scrolls when taller than pane
- [ ] AC-8: Tables with zero relationships show empty-state message
- [ ] AC-9: Hub tables render without crashing (capped + "+N more")
- [ ] AC-10: Enter on neighbor re-centers diagram on that table
- [ ] AC-11: Navigation updates explorer tree selection
- [ ] AC-12: `go build ./... && go vet ./... && go test ./...` pass
- [ ] AC-13: `make install` deployed successfully

## Data Contract

### Input (already available)
```go
// Current table data (from explorerPreviewDataMsg)
columns     []postgres.ColumnInfo      // all columns
constraints []postgres.ConstraintInfo  // for PK identification
foreignKeys []postgres.ForeignKeyInfo  // outgoing FKs
schema      string
table       string

// Schema-wide FKs (new - from existing ListForeignKeysBySchema)
schemaForeignKeys map[string][]postgres.ForeignKeyInfo  // table -> FKs
```

### Internal Model
```go
type ERDiagram struct {
    Center    TableBox
    Outgoing  []Relationship  // current FK → other table
    Incoming  []Relationship  // other FK → current table
}

type TableBox struct {
    Schema  string
    Name    string
    Columns []ColumnBadge
}

type ColumnBadge struct {
    Name     string
    DataType string
    IsPK     bool
    IsFK     bool
}

type Relationship struct {
    FromColumn string
    ToTable    string
    ToColumn   string
    Cardinality string  // "N:1" or "1:N"
}
```
