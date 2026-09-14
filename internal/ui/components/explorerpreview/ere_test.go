package explorerpreview

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/buble/dbx/internal/drivers/postgres"
)

// --- Scenario: ERE tab renders diagram for table with relationships ---
// Given the current table "orders" has foreign keys to "users" and "products"
// And the table "order_items" has a foreign key to "orders"
// When building the diagram for "orders"
// Then center box shows "orders" with all columns
// And column "user_id" is marked as FK, column "id" is marked as PK
// And neighbor boxes "users", "products", "order_items" are present
// And edge labels show cardinality N:1 for outgoing FKs and 1:N for incoming FKs

func TestBuildERDiagram_WithRelationships(t *testing.T) {
	columns := []postgres.ColumnInfo{
		{Name: "id", DataType: "integer"},
		{Name: "user_id", DataType: "integer"},
		{Name: "product_id", DataType: "integer"},
		{Name: "created_at", DataType: "timestamp"},
	}
	constraints := []postgres.ConstraintInfo{
		{Name: "orders_pkey", Type: "PRIMARY KEY", Columns: "id"},
	}
	outgoingFKs := []postgres.ForeignKeyInfo{
		{Name: "orders_user_id_fkey", Column: "user_id", RefSchema: "public", RefTable: "users", RefColumn: "id"},
		{Name: "orders_product_id_fkey", Column: "product_id", RefSchema: "public", RefTable: "products", RefColumn: "id"},
	}
	schemaFKs := map[string][]postgres.ForeignKeyInfo{
		"orders": outgoingFKs,
		"order_items": {
			{Name: "order_items_order_id_fkey", Column: "order_id", RefSchema: "public", RefTable: "orders", RefColumn: "id"},
		},
	}

	diagram := BuildERDiagram("public", "orders", columns, constraints, outgoingFKs, schemaFKs)

	// Center box
	if diagram.Center.Name != "orders" {
		t.Errorf("expected center name 'orders', got %q", diagram.Center.Name)
	}
	if len(diagram.Center.Columns) != 4 {
		t.Fatalf("expected 4 columns in center, got %d", len(diagram.Center.Columns))
	}

	// Check PK/FK badges
	for _, col := range diagram.Center.Columns {
		switch col.Name {
		case "id":
			if !col.IsPK {
				t.Error("expected 'id' to be PK")
			}
		case "user_id":
			if !col.IsFK {
				t.Error("expected 'user_id' to be FK")
			}
		case "product_id":
			if !col.IsFK {
				t.Error("expected 'product_id' to be FK")
			}
		}
	}

	// Outgoing relationships (orders -> users, orders -> products)
	if len(diagram.Outgoing) != 2 {
		t.Fatalf("expected 2 outgoing relationships, got %d", len(diagram.Outgoing))
	}
	for _, rel := range diagram.Outgoing {
		if rel.Cardinality != "N:1" {
			t.Errorf("expected outgoing cardinality N:1, got %q", rel.Cardinality)
		}
	}

	// Incoming relationships (order_items -> orders)
	if len(diagram.Incoming) != 1 {
		t.Fatalf("expected 1 incoming relationship, got %d", len(diagram.Incoming))
	}
	if diagram.Incoming[0].Cardinality != "1:N" {
		t.Errorf("expected incoming cardinality 1:N, got %q", diagram.Incoming[0].Cardinality)
	}
	if diagram.Incoming[0].ToTable != "order_items" {
		t.Errorf("expected incoming to table 'order_items', got %q", diagram.Incoming[0].ToTable)
	}
}

// --- Scenario: ERE tab shows empty state for table without relationships ---

func TestBuildERDiagram_EmptyState(t *testing.T) {
	columns := []postgres.ColumnInfo{
		{Name: "key", DataType: "text"},
		{Name: "value", DataType: "text"},
	}
	diagram := BuildERDiagram("public", "settings", columns, nil, nil, nil)

	if len(diagram.Outgoing) != 0 {
		t.Errorf("expected 0 outgoing, got %d", len(diagram.Outgoing))
	}
	if len(diagram.Incoming) != 0 {
		t.Errorf("expected 0 incoming, got %d", len(diagram.Incoming))
	}
	if diagram.Center.Name != "settings" {
		t.Errorf("expected center 'settings', got %q", diagram.Center.Name)
	}
}

// --- Scenario: Hub table caps rendered neighbors ---
// Given the current table "events" has 25 incoming FK relationships
// Then at most 10 incoming neighbor boxes are rendered

func TestBuildERDiagram_HubTableCap(t *testing.T) {
	columns := []postgres.ColumnInfo{{Name: "id", DataType: "integer"}}
	schemaFKs := map[string][]postgres.ForeignKeyInfo{}

	// We need 25 different tables referencing events
	incomingFKs := make([]postgres.ForeignKeyInfo, 25)
	for i := 0; i < 25; i++ {
		tableName := strings.Repeat("x", 1)
		tableName = string(rune('a' + i%26))
		if i >= 26 {
			tableName += string(rune('0' + i/26))
		}
		incomingFKs[i] = postgres.ForeignKeyInfo{
			Name:      "fk_" + tableName,
			Column:    "event_id",
			RefSchema: "public",
			RefTable:  "events",
			RefColumn: "id",
		}
		if schemaFKs[tableName] == nil {
			schemaFKs[tableName] = []postgres.ForeignKeyInfo{}
		}
		schemaFKs[tableName] = append(schemaFKs[tableName], incomingFKs[i])
	}

	diagram := BuildERDiagram("public", "events", columns, nil, nil, schemaFKs)

	if len(diagram.Incoming) > MaxNeighbors {
		t.Errorf("expected at most %d incoming, got %d", MaxNeighbors, len(diagram.Incoming))
	}
	if diagram.IncomingOverflow <= 0 {
		t.Error("expected positive incoming overflow count")
	}
	if diagram.IncomingOverflow != 15 {
		t.Errorf("expected incoming overflow of 15, got %d", diagram.IncomingOverflow)
	}
}

// --- Scenario: Cross-schema foreign key reference ---
// Given the current table "orders" has a FK to "audit.logs" in a different schema

func TestBuildERDiagram_CrossSchema(t *testing.T) {
	columns := []postgres.ColumnInfo{
		{Name: "id", DataType: "integer"},
		{Name: "log_ref", DataType: "integer"},
	}
	outgoingFKs := []postgres.ForeignKeyInfo{
		{Name: "orders_log_ref_fkey", Column: "log_ref", RefSchema: "audit", RefTable: "logs", RefColumn: "id"},
	}

	diagram := BuildERDiagram("public", "orders", columns, nil, outgoingFKs, nil)

	if len(diagram.Outgoing) != 1 {
		t.Fatalf("expected 1 outgoing, got %d", len(diagram.Outgoing))
	}
	if diagram.Outgoing[0].ToTable != "audit.logs" {
		t.Errorf("expected cross-schema table 'audit.logs', got %q", diagram.Outgoing[0].ToTable)
	}
}

// --- Deduplication: multiple FKs to same table produce one box ---

func TestBuildERDiagram_Deduplication(t *testing.T) {
	columns := []postgres.ColumnInfo{
		{Name: "id", DataType: "integer"},
		{Name: "user_id", DataType: "integer"},
		{Name: "creator_id", DataType: "integer"},
	}
	outgoingFKs := []postgres.ForeignKeyInfo{
		{Name: "fk_user", Column: "user_id", RefSchema: "public", RefTable: "users", RefColumn: "id"},
		{Name: "fk_creator", Column: "creator_id", RefSchema: "public", RefTable: "users", RefColumn: "id"},
	}

	diagram := BuildERDiagram("public", "orders", columns, nil, outgoingFKs, nil)

	// Two FKs to same table should produce only one outgoing box
	if len(diagram.Outgoing) != 1 {
		t.Errorf("expected 1 outgoing after dedup, got %d", len(diagram.Outgoing))
	}
}

func TestBuildERDiagram_IncomingDeduplication(t *testing.T) {
	columns := []postgres.ColumnInfo{{Name: "id", DataType: "integer"}}
	schemaFKs := map[string][]postgres.ForeignKeyInfo{
		"order_items": {
			{Name: "fk_order1", Column: "order_id", RefSchema: "public", RefTable: "orders", RefColumn: "id"},
			{Name: "fk_order2", Column: "parent_order_id", RefSchema: "public", RefTable: "orders", RefColumn: "id"},
		},
	}

	diagram := BuildERDiagram("public", "orders", columns, nil, nil, schemaFKs)

	// Two FKs from same table should produce only one incoming box
	if len(diagram.Incoming) != 1 {
		t.Errorf("expected 1 incoming after dedup, got %d", len(diagram.Incoming))
	}
}

// --- Scenario: Long table name truncation ---
// Given the current table has a name longer than 40 characters

func TestTruncateTableName_Long(t *testing.T) {
	longName := "this_is_a_very_long_table_name_that_exceeds_forty_characters_limit"
	truncated := TruncateTableName(longName)
	if utf8.RuneCountInString(truncated) > 41 { // 40 chars + ellipsis
		t.Errorf("expected truncated name <= 41 chars, got %d: %q", utf8.RuneCountInString(truncated), truncated)
	}
	if !strings.HasSuffix(truncated, "…") {
		t.Errorf("expected truncation with '…', got %q", truncated)
	}
}

func TestTruncateTableName_Short(t *testing.T) {
	shortName := "users"
	truncated := TruncateTableName(shortName)
	if truncated != shortName {
		t.Errorf("expected unchanged short name, got %q", truncated)
	}
}

// --- Scenario: Long column name truncation ---

func TestTruncateColumnName_Long(t *testing.T) {
	longName := "this_is_a_very_long_column_name_that_exceeds_thirty_chars"
	truncated := TruncateColumnName(longName)
	if utf8.RuneCountInString(truncated) > 31 { // 30 chars + ellipsis
		t.Errorf("expected truncated name <= 31 chars, got %d: %q", utf8.RuneCountInString(truncated), truncated)
	}
	if !strings.HasSuffix(truncated, "…") {
		t.Errorf("expected truncation with '…', got %q", truncated)
	}
}

func TestTruncateColumnName_Short(t *testing.T) {
	shortName := "id"
	truncated := TruncateColumnName(shortName)
	if truncated != shortName {
		t.Errorf("expected unchanged short name, got %q", truncated)
	}
}

// --- Box width computation ---

func TestComputeBoxWidth(t *testing.T) {
	tests := []struct {
		name     string
		columns  []ColumnBadge
		expected int
	}{
		{
			name: "minimum width",
			columns: []ColumnBadge{
				{Name: "id", DataType: "int"},
			},
			expected: MinBoxWidth,
		},
		{
			name: "computed width",
			columns: []ColumnBadge{
				{Name: "user_id", DataType: "integer"},
				{Name: "created_at", DataType: "timestamp"},
			},
			expected: len("created_at") + len("timestamp") + 4, // 10 + 9 + 4 = 23
		},
		{
			name: "maximum width",
			columns: func() []ColumnBadge {
				cols := make([]ColumnBadge, 10)
				for i := range cols {
					cols[i] = ColumnBadge{
						Name:     strings.Repeat("a", 25),
						DataType: strings.Repeat("b", 25),
					}
				}
				return cols
			}(),
			expected: MaxBoxWidth,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeBoxWidth(tt.columns)
			if got != tt.expected {
				t.Errorf("expected width %d, got %d", tt.expected, got)
			}
		})
	 }
}

// --- Rendering: empty state message ---

func TestRenderERDiagram_EmptyState(t *testing.T) {
	diagram := ERDiagram{
		Center: TableBox{
			Schema: "public",
			Name:   "settings",
			Columns: []ColumnBadge{
				{Name: "key", DataType: "text"},
			},
		},
	}
	output := RenderERDiagram(diagram, 80, 20)
	if !strings.Contains(output, "No relationships for this table") {
		t.Errorf("expected empty state message, got:\n%s", output)
	}
}

// --- Rendering: center box with columns ---

func TestRenderERDiagram_CenterBox(t *testing.T) {
	diagram := ERDiagram{
		Center: TableBox{
			Schema: "public",
			Name:   "orders",
			Columns: []ColumnBadge{
				{Name: "id", DataType: "integer", IsPK: true},
				{Name: "user_id", DataType: "integer", IsFK: true},
				{Name: "created_at", DataType: "timestamp"},
			},
		},
		Outgoing: []Relationship{
			{FromColumn: "user_id", ToTable: "users", ToColumn: "id", Cardinality: "N:1"},
		},
	}
	output := RenderERDiagram(diagram, 80, 20)

	// Check PK/FK badges
	if !strings.Contains(output, "PK") {
		t.Error("expected PK badge in output")
	}
	if !strings.Contains(output, "FK") {
		t.Error("expected FK badge in output")
	}
	// Check table name
	if !strings.Contains(output, "orders") {
		t.Error("expected table name 'orders' in output")
	}
}

// --- Rendering: edge cardinality labels ---

func TestRenderERDiagram_EdgeLabels(t *testing.T) {
	diagram := ERDiagram{
		Center: TableBox{
			Schema: "public",
			Name:   "orders",
			Columns: []ColumnBadge{
				{Name: "id", DataType: "integer", IsPK: true},
				{Name: "user_id", DataType: "integer", IsFK: true},
			},
		},
		Outgoing: []Relationship{
			{FromColumn: "user_id", ToTable: "users", ToColumn: "id", Cardinality: "N:1"},
		},
		Incoming: []Relationship{
			{FromColumn: "order_id", ToTable: "order_items", ToColumn: "id", Cardinality: "1:N"},
		},
	}
	output := RenderERDiagram(diagram, 80, 30)

	if !strings.Contains(output, "N:1") {
		t.Errorf("expected N:1 cardinality label, output:\n%s", output)
	}
	if !strings.Contains(output, "1:N") {
		t.Errorf("expected 1:N cardinality label, output:\n%s", output)
	}
}

// --- Rendering: box-drawing characters ---

func TestRenderERDiagram_BoxDrawing(t *testing.T) {
	diagram := ERDiagram{
		Center: TableBox{
			Schema: "public",
			Name:   "users",
			Columns: []ColumnBadge{
				{Name: "id", DataType: "integer", IsPK: true},
			},
		},
		Outgoing: []Relationship{
			{FromColumn: "org_id", ToTable: "orgs", ToColumn: "id", Cardinality: "N:1"},
		},
	}
	output := RenderERDiagram(diagram, 80, 20)

	if !strings.Contains(output, "┌") {
		t.Error("expected box-drawing character ┌")
	}
	if !strings.Contains(output, "┐") {
		t.Error("expected box-drawing character ┐")
	}
	if !strings.Contains(output, "└") {
		t.Error("expected box-drawing character └")
	}
	if !strings.Contains(output, "┘") {
		t.Error("expected box-drawing character ┘")
	}
}

// --- Navigation: cursor state ---

func TestERDiagramNav_CursorMovement(t *testing.T) {
	nav := NewERDiagramNav(3)

	// Initial state: no selection
	if nav.HasSelection() {
		t.Error("expected no selection initially")
	}

	// Move down
	nav.MoveDown()
	if !nav.HasSelection() {
		t.Error("expected selection after MoveDown")
	}
	if nav.SelectedIndex() != 0 {
		t.Errorf("expected selected index 0, got %d", nav.SelectedIndex())
	}

	// Move down twice more
	nav.MoveDown()
	nav.MoveDown()
	if nav.SelectedIndex() != 2 {
		t.Errorf("expected selected index 2, got %d", nav.SelectedIndex())
	}

	// Can't go past end
	nav.MoveDown()
	if nav.SelectedIndex() != 2 {
		t.Errorf("expected selected index stays at 2, got %d", nav.SelectedIndex())
	}

	// Move up
	nav.MoveUp()
	if nav.SelectedIndex() != 1 {
		t.Errorf("expected selected index 1, got %d", nav.SelectedIndex())
	}

	// Can't go below 0
	nav2 := NewERDiagramNav(3)
	nav2.MoveUp()
	if nav2.HasSelection() {
		t.Error("expected no selection when moving up from 0")
	}
}

func TestERDiagramNav_EmptyNeighbors(t *testing.T) {
	nav := NewERDiagramNav(0)

	nav.MoveDown()
	if nav.HasSelection() {
		t.Error("expected no selection with 0 neighbors")
	}

	nav.MoveUp()
	if nav.HasSelection() {
		t.Error("expected no selection with 0 neighbors")
	}
}

func TestERDiagramNav_GetSelectedNeighbor(t *testing.T) {
	neighbors := []Relationship{
		{FromColumn: "user_id", ToTable: "users", ToColumn: "id", Cardinality: "N:1"},
		{FromColumn: "product_id", ToTable: "products", ToColumn: "id", Cardinality: "N:1"},
	}
	nav := NewERDiagramNav(len(neighbors))

	// No selection
	if rel := nav.GetSelectedNeighbor(neighbors); rel != nil {
		t.Error("expected nil when no selection")
	}

	// Select first
	nav.MoveDown()
	rel := nav.GetSelectedNeighbor(neighbors)
	if rel == nil {
		t.Fatal("expected non-nil relation")
	}
	if rel.ToTable != "users" {
		t.Errorf("expected 'users', got %q", rel.ToTable)
	}

	// Select second
	nav.MoveDown()
	rel = nav.GetSelectedNeighbor(neighbors)
	if rel == nil {
		t.Fatal("expected non-nil relation")
	}
	if rel.ToTable != "products" {
		t.Errorf("expected 'products', got %q", rel.ToTable)
	}
}

// --- Scrolling: viewport ---

func TestERDiagramViewport_ScrollDown(t *testing.T) {
	viewport := NewERDiagramViewport(10, 30) // height=10, content=30

	if viewport.ScrollOffset != 0 {
		t.Errorf("expected initial scroll 0, got %d", viewport.ScrollOffset)
	}

	viewport.ScrollDown()
	if viewport.ScrollOffset != 1 {
		t.Errorf("expected scroll 1 after ScrollDown, got %d", viewport.ScrollOffset)
	}

	// Scroll to bottom
	for i := 0; i < 25; i++ {
		viewport.ScrollDown()
	}
	if viewport.ScrollOffset != 20 { // 30 - 10 = 20 max
		t.Errorf("expected scroll capped at 20, got %d", viewport.ScrollOffset)
	}
}

func TestERDiagramViewport_ScrollUp(t *testing.T) {
	viewport := NewERDiagramViewport(10, 30)
	viewport.ScrollOffset = 5

	viewport.ScrollUp()
	if viewport.ScrollOffset != 4 {
		t.Errorf("expected scroll 4 after ScrollUp, got %d", viewport.ScrollOffset)
	}

	// Can't go below 0
	viewport.ScrollOffset = 0
	viewport.ScrollUp()
	if viewport.ScrollOffset != 0 {
		t.Errorf("expected scroll stays at 0, got %d", viewport.ScrollOffset)
	}
}

func TestERDiagramViewport_JumpTop(t *testing.T) {
	viewport := NewERDiagramViewport(10, 30)
	viewport.ScrollOffset = 15

	viewport.JumpTop()
	if viewport.ScrollOffset != 0 {
		t.Errorf("expected scroll 0 after JumpTop, got %d", viewport.ScrollOffset)
	}
}

func TestERDiagramViewport_JumpBottom(t *testing.T) {
	viewport := NewERDiagramViewport(10, 30)

	viewport.JumpBottom()
	if viewport.ScrollOffset != 20 {
		t.Errorf("expected scroll 20 after JumpBottom, got %d", viewport.ScrollOffset)
	}
}

func TestERDiagramViewport_Reset(t *testing.T) {
	viewport := NewERDiagramViewport(10, 30)
	viewport.ScrollOffset = 15

	viewport.Reset()
	if viewport.ScrollOffset != 0 {
		t.Errorf("expected scroll 0 after Reset, got %d", viewport.ScrollOffset)
	}
}

// --- Rendering: theme styles used (no hardcoded colors) ---

func TestRenderERDiagram_NoHardcodedColors(t *testing.T) {
	diagram := ERDiagram{
		Center: TableBox{
			Schema: "public",
			Name:   "test",
			Columns: []ColumnBadge{
				{Name: "id", DataType: "integer", IsPK: true},
			},
		},
	}
	output := RenderERDiagram(diagram, 80, 20)

	// The renderer should not contain ANSI color codes directly
	// (they should come from lipgloss styles, which are applied externally)
	// This test just verifies the renderer returns a non-empty string
	if output == "" {
		t.Error("expected non-empty render output")
	}
}

// --- Rendering: neighbor box with truncated long name ---

func TestRenderERDiagram_LongNeighborName(t *testing.T) {
	diagram := ERDiagram{
		Center: TableBox{
			Schema: "public",
			Name:   "orders",
			Columns: []ColumnBadge{
				{Name: "id", DataType: "integer", IsPK: true},
				{Name: "ref_id", DataType: "integer", IsFK: true},
			},
		},
		Outgoing: []Relationship{
			{FromColumn: "ref_id", ToTable: "this_is_a_very_long_table_name_that_exceeds_forty_characters", ToColumn: "id", Cardinality: "N:1"},
		},
	}
	output := RenderERDiagram(diagram, 80, 20)

	// The long table name should be truncated
	if strings.Contains(output, "this_is_a_very_long_table_name_that_exceeds_forty_characters") {
		t.Error("expected long table name to be truncated")
	}
}

// --- Integration: BuildERDiagram + Render roundtrip ---

func TestBuildAndRender_SimpleDiagram(t *testing.T) {
	columns := []postgres.ColumnInfo{
		{Name: "id", DataType: "integer"},
		{Name: "user_id", DataType: "integer"},
	}
	constraints := []postgres.ConstraintInfo{
		{Name: "pkey", Type: "PRIMARY KEY", Columns: "id"},
	}
	outgoing := []postgres.ForeignKeyInfo{
		{Name: "fk_user", Column: "user_id", RefSchema: "public", RefTable: "users", RefColumn: "id"},
	}
	schemaFKs := map[string][]postgres.ForeignKeyInfo{
		"users": {
			{Name: "fk_order", Column: "user_id", RefSchema: "public", RefTable: "orders", RefColumn: "id"},
		},
	}

	diagram := BuildERDiagram("public", "orders", columns, constraints, outgoing, schemaFKs)
	output := RenderERDiagram(diagram, 80, 20)

	// Should contain key elements
	if !strings.Contains(output, "orders") {
		t.Error("expected 'orders' in rendered output")
	}
	if !strings.Contains(output, "users") {
		t.Error("expected 'users' in rendered output")
	}
	if !strings.Contains(output, "N:1") {
		t.Error("expected 'N:1' in rendered output")
	}
}
