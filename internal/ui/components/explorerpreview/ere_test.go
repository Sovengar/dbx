package explorerpreview

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/buble/dbx/internal/drivers/postgres"
)

// --- BuildERDiagram: basic relationships ---

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

	if diagram.Center.Name != "orders" {
		t.Errorf("expected center name 'orders', got %q", diagram.Center.Name)
	}
	if len(diagram.Center.Columns) != 3 {
		t.Fatalf("expected 3 columns in center (PK+FK only), got %d", len(diagram.Center.Columns))
	}

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

	if len(diagram.Outgoing) != 2 {
		t.Fatalf("expected 2 outgoing relationships, got %d", len(diagram.Outgoing))
	}
	for _, rel := range diagram.Outgoing {
		if rel.Cardinality != "N:1" {
			t.Errorf("expected outgoing cardinality N:1, got %q", rel.Cardinality)
		}
	}

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

// --- BuildERDiagram: empty state ---

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

// --- BuildERDiagram: hub table cap ---

func TestBuildERDiagram_HubTableCap(t *testing.T) {
	columns := []postgres.ColumnInfo{{Name: "id", DataType: "integer"}}
	schemaFKs := map[string][]postgres.ForeignKeyInfo{}

	for i := 0; i < 25; i++ {
		tableName := string(rune('a' + i%26))
		if i >= 26 {
			tableName += string(rune('0' + i/26))
		}
		fk := postgres.ForeignKeyInfo{
			Name:      "fk_" + tableName,
			Column:    "event_id",
			RefSchema: "public",
			RefTable:  "events",
			RefColumn: "id",
		}
		schemaFKs[tableName] = append(schemaFKs[tableName], fk)
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

// --- BuildERDiagram: cross-schema ---

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

// --- BuildERDiagram: deduplication ---

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

	if len(diagram.Incoming) != 1 {
		t.Errorf("expected 1 incoming after dedup, got %d", len(diagram.Incoming))
	}
}

// --- BuildERDiagram: junction table detection ---

func TestBuildERDiagram_JunctionDetection(t *testing.T) {
	columns := []postgres.ColumnInfo{{Name: "id", DataType: "integer"}}
	schemaFKs := map[string][]postgres.ForeignKeyInfo{
		"order_items": {
			// order_items is a junction candidate: FK to orders AND FK to products
			{Name: "fk_order", Column: "order_id", RefSchema: "public", RefTable: "orders", RefColumn: "id"},
			{Name: "fk_product", Column: "product_id", RefSchema: "public", RefTable: "products", RefColumn: "id"},
		},
		"payments": {
			// payments is NOT a junction: only FK to orders
			{Name: "fk_order", Column: "order_id", RefSchema: "public", RefTable: "orders", RefColumn: "id"},
		},
	}

	diagram := BuildERDiagram("public", "orders", columns, nil, nil, schemaFKs)

	// Both should be in Incoming (junction candidates stay in their column)
	if len(diagram.Incoming) != 2 {
		t.Fatalf("expected 2 incoming, got %d", len(diagram.Incoming))
	}

	// order_items should be marked as junction candidate
	for _, rel := range diagram.Incoming {
		if rel.ToTable == "order_items" && !rel.IsJunction {
			t.Error("expected 'order_items' to be marked as junction candidate")
		}
		if rel.ToTable == "payments" && rel.IsJunction {
			t.Error("expected 'payments' to NOT be marked as junction candidate")
		}
	}
}

func TestBuildERDiagram_OutgoingJunctionDetection(t *testing.T) {
	columns := []postgres.ColumnInfo{
		{Name: "id", DataType: "integer"},
		{Name: "order_id", DataType: "integer"},
	}
	outgoingFKs := []postgres.ForeignKeyInfo{
		{Name: "fk_order", Column: "order_id", RefSchema: "public", RefTable: "order_items", RefColumn: "id"},
	}
	schemaFKs := map[string][]postgres.ForeignKeyInfo{
		"order_items": {
			{Name: "fk_order", Column: "order_id", RefSchema: "public", RefTable: "orders", RefColumn: "id"},
			{Name: "fk_product", Column: "product_id", RefSchema: "public", RefTable: "products", RefColumn: "id"},
		},
	}

	diagram := BuildERDiagram("public", "orders", columns, nil, outgoingFKs, schemaFKs)

	// order_items should be in Outgoing (not separated) and marked as junction candidate
	if len(diagram.Outgoing) != 1 {
		t.Fatalf("expected 1 outgoing, got %d", len(diagram.Outgoing))
	}
	if !diagram.Outgoing[0].IsJunction {
		t.Error("expected 'order_items' to be marked as junction candidate")
	}
}

// --- BuildERDiagram: junction table center detection ---

func TestBuildERDiagram_CenterIsJunction(t *testing.T) {
	columns := []postgres.ColumnInfo{
		{Name: "id", DataType: "integer"},
		{Name: "order_id", DataType: "integer"},
		{Name: "product_id", DataType: "integer"},
	}
	outgoingFKs := []postgres.ForeignKeyInfo{
		{Name: "fk_order", Column: "order_id", RefSchema: "public", RefTable: "orders", RefColumn: "id"},
		{Name: "fk_product", Column: "product_id", RefSchema: "public", RefTable: "products", RefColumn: "id"},
	}

	diagram := BuildERDiagram("public", "order_items", columns, nil, outgoingFKs, nil)

	if !diagram.Center.IsJunction {
		t.Error("expected center 'order_items' to be detected as junction table")
	}
}

// --- BuildERDiagram: PK/FK filtering ---

func TestBuildERDiagram_OnlyPKFKColumns(t *testing.T) {
	columns := []postgres.ColumnInfo{
		{Name: "id", DataType: "integer"},
		{Name: "user_id", DataType: "integer"},
		{Name: "name", DataType: "text"},
		{Name: "created_at", DataType: "timestamp"},
		{Name: "updated_at", DataType: "timestamp"},
	}
	constraints := []postgres.ConstraintInfo{
		{Name: "pkey", Type: "PRIMARY KEY", Columns: "id"},
	}
	outgoingFKs := []postgres.ForeignKeyInfo{
		{Name: "fk_user", Column: "user_id", RefSchema: "public", RefTable: "users", RefColumn: "id"},
	}

	diagram := BuildERDiagram("public", "orders", columns, constraints, outgoingFKs, nil)

	if len(diagram.Center.Columns) != 2 {
		t.Fatalf("expected 2 columns (PK+FK only), got %d: %v", len(diagram.Center.Columns), diagram.Center.Columns)
	}
	for _, col := range diagram.Center.Columns {
		if !col.IsPK && !col.IsFK {
			t.Errorf("expected only PK or FK columns, got %q", col.Name)
		}
	}
}

func TestBuildERDiagram_NoPKFK_ShowsAll(t *testing.T) {
	columns := []postgres.ColumnInfo{
		{Name: "key", DataType: "text"},
		{Name: "value", DataType: "text"},
	}
	diagram := BuildERDiagram("public", "settings", columns, nil, nil, nil)
	if len(diagram.Center.Columns) != 2 {
		t.Fatalf("expected 2 columns (fallback to all), got %d", len(diagram.Center.Columns))
	}
}

// --- Truncation ---

func TestTruncateTableName_Long(t *testing.T) {
	longName := "this_is_a_very_long_table_name_that_exceeds_forty_characters_limit"
	truncated := TruncateTableName(longName)
	if utf8.RuneCountInString(truncated) > 41 {
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

func TestTruncateColumnName_Long(t *testing.T) {
	longName := "this_is_a_very_long_column_name_that_exceeds_thirty_chars"
	truncated := TruncateColumnName(longName)
	if utf8.RuneCountInString(truncated) > 31 {
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

// --- ComputeBoxWidth ---

func TestComputeBoxWidth(t *testing.T) {
	tests := []struct {
		name     string
		columns  []ColumnBadge
		expected int
	}{
		{
			name:     "minimum width",
			columns:  []ColumnBadge{{Name: "id", DataType: "int"}},
			expected: MinBoxWidth,
		},
		{
			name: "computed width",
			columns: []ColumnBadge{
				{Name: "user_id", DataType: "integer"},
				{Name: "created_at", DataType: "timestamp"},
			},
			expected: len("created_at") + len("timestamp") + 4,
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

// --- Rendering: empty state ---

func TestRenderERDiagram_EmptyState(t *testing.T) {
	diagram := ERDiagram{
		Center: TableBox{
			Schema:  "public",
			Name:    "settings",
			Columns: []ColumnBadge{{Name: "key", DataType: "text"}},
		},
	}
	output := RenderERDiagram(diagram, 80, 20, nil)
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
			},
		},
		Outgoing: []Relationship{
			{FromColumn: "user_id", ToTable: "users", ToColumn: "id", Cardinality: "N:1"},
		},
	}
	output := RenderERDiagram(diagram, 80, 20, nil)

	if !strings.Contains(output, "PK") {
		t.Error("expected PK badge in output")
	}
	if !strings.Contains(output, "FK") {
		t.Error("expected FK badge in output")
	}
	if !strings.Contains(output, "orders") {
		t.Error("expected table name 'orders' in output")
	}
}

// --- Rendering: 2-column layout headers ---

func TestRenderERDiagram_TwoColumnHeaders(t *testing.T) {
	diagram := ERDiagram{
		Center: TableBox{
			Schema: "public",
			Name:   "orders",
			Columns: []ColumnBadge{
				{Name: "id", DataType: "integer", IsPK: true},
			},
		},
		Incoming: []Relationship{
			{FromColumn: "order_id", ToTable: "order_items", ToColumn: "id", Cardinality: "1:N"},
		},
		Outgoing: []Relationship{
			{FromColumn: "user_id", ToTable: "users", ToColumn: "id", Cardinality: "N:1"},
		},
	}
	output := RenderERDiagram(diagram, 120, 40, nil)

	if !strings.Contains(output, "1:N") {
		t.Error("expected '1:N' column header")
	}
	if !strings.Contains(output, "N:1") {
		t.Error("expected 'N:1' column header")
	}
	if strings.Contains(output, "N:N") {
		t.Error("did not expect 'N:N' column header")
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
	output := RenderERDiagram(diagram, 80, 20, nil)

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

// --- Rendering: long neighbor name truncation ---

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
	output := RenderERDiagram(diagram, 80, 20, nil)

	if strings.Contains(output, "this_is_a_very_long_table_name_that_exceeds_forty_characters") {
		t.Error("expected long table name to be truncated")
	}
}

// --- Rendering: selection highlight ---

func TestRenderERDiagram_SelectedHighlight(t *testing.T) {
	diagram := ERDiagram{
		Center: TableBox{
			Schema: "public",
			Name:   "orders",
			Columns: []ColumnBadge{
				{Name: "id", DataType: "integer", IsPK: true},
			},
		},
		Outgoing: []Relationship{
			{FromColumn: "user_id", ToTable: "users", ToColumn: "id", Cardinality: "N:1"},
			{FromColumn: "product_id", ToTable: "products", ToColumn: "id", Cardinality: "N:1"},
		},
	}

	// No selection
	output := RenderERDiagram(diagram, 80, 20, nil)
	if strings.Contains(output, "►") || strings.Contains(output, "◄") {
		t.Error("expected no selection markers when nav=nil")
	}

	// Select first neighbor (column 1 = N:1, row 0)
	nav := NewERDiagramNav(0, 2)
	nav.MoveRight() // move to N:1 column
	nav.MoveDown()  // row 0
	output = RenderERDiagram(diagram, 80, 20, nav)
	if !strings.Contains(output, "► users ◄") {
		t.Errorf("expected '► users ◄' for selected neighbor, got:\n%s", output)
	}

	// Select second neighbor (column 1 = N:1, row 1)
	nav2 := NewERDiagramNav(0, 2)
	nav2.MoveRight() // move to N:1 column
	nav2.MoveDown()  // row 0
	nav2.MoveDown()  // row 1
	output = RenderERDiagram(diagram, 80, 20, nav2)
	if !strings.Contains(output, "► products ◄") {
		t.Errorf("expected '► products ◄' for selected neighbor, got:\n%s", output)
	}
}

// --- Navigation: 2D cursor ---

func TestERDiagramNav_2DCursorMovement(t *testing.T) {
	nav := NewERDiagramNav(2, 3) // 2 incoming, 3 outgoing

	// Initial state: no selection
	if nav.HasSelection() {
		t.Error("expected no selection initially")
	}

	// Move down — should select row 0 in column 0 (1:N)
	nav.MoveDown()
	if !nav.HasSelection() {
		t.Error("expected selection after MoveDown")
	}
	if nav.ActiveColumn() != 0 || nav.ActiveRow() != 0 {
		t.Errorf("expected (0,0), got (%d,%d)", nav.ActiveColumn(), nav.ActiveRow())
	}

	// Move down again — row 1 in column 0
	nav.MoveDown()
	if nav.ActiveColumn() != 0 || nav.ActiveRow() != 1 {
		t.Errorf("expected (0,1), got (%d,%d)", nav.ActiveColumn(), nav.ActiveRow())
	}

	// Move right — column 1 (N:1), row clamped to 2 (max for column 1)
	nav.MoveRight()
	if nav.ActiveColumn() != 1 || nav.ActiveRow() != 1 {
		t.Errorf("expected (1,1) after MoveRight, got (%d,%d)", nav.ActiveColumn(), nav.ActiveRow())
	}

	// Can't go past column 1 (only 2 columns now)
	nav.MoveRight()
	if nav.ActiveColumn() != 1 {
		t.Errorf("expected column stays at 1, got %d", nav.ActiveColumn())
	}

	// Move left — back to column 0
	nav.MoveLeft()
	if nav.ActiveColumn() != 0 {
		t.Errorf("expected column 0 after MoveLeft, got %d", nav.ActiveColumn())
	}

	// Move up — stays at row 0 (no deselect)
	nav.MoveUp()
	nav.MoveUp()
	if !nav.HasSelection() {
		t.Error("expected selection to remain after moving up past row 0")
	}
	if nav.ActiveRow() != 0 {
		t.Errorf("expected row to stay at 0, got %d", nav.ActiveRow())
	}
}

func TestERDiagramNav_EmptyColumns(t *testing.T) {
	nav := NewERDiagramNav(0, 0)

	nav.MoveDown()
	if nav.HasSelection() {
		t.Error("expected no selection with all empty columns")
	}

	nav.MoveRight()
	nav.MoveDown()
	if nav.HasSelection() {
		t.Error("expected no selection when all columns empty")
	}
}

func TestERDiagramNav_GetSelectedRelationship(t *testing.T) {
	diagram := &ERDiagram{
		Incoming: []Relationship{
			{FromColumn: "order_id", ToTable: "order_items", ToColumn: "id", Cardinality: "1:N"},
		},
		Outgoing: []Relationship{
			{FromColumn: "user_id", ToTable: "users", ToColumn: "id", Cardinality: "N:1"},
		},
	}
	nav := NewERDiagramNav(1, 1)

	// No selection
	if rel := nav.GetSelectedRelationship(diagram); rel != nil {
		t.Error("expected nil when no selection")
	}

	// Select incoming (column 0, row 0)
	nav.MoveDown()
	rel := nav.GetSelectedRelationship(diagram)
	if rel == nil {
		t.Fatal("expected non-nil relation")
	}
	if rel.ToTable != "order_items" {
		t.Errorf("expected 'order_items', got %q", rel.ToTable)
	}

	// Move to outgoing (column 1)
	nav.MoveRight()
	rel = nav.GetSelectedRelationship(diagram)
	if rel == nil {
		t.Fatal("expected non-nil relation")
	}
	if rel.ToTable != "users" {
		t.Errorf("expected 'users', got %q", rel.ToTable)
	}
}

// --- Viewport ---

func TestERDiagramViewport_ScrollDown(t *testing.T) {
	viewport := NewERDiagramViewport(10, 30)

	if viewport.ScrollOffset != 0 {
		t.Errorf("expected initial scroll 0, got %d", viewport.ScrollOffset)
	}
	viewport.ScrollDown()
	if viewport.ScrollOffset != 1 {
		t.Errorf("expected scroll 1 after ScrollDown, got %d", viewport.ScrollOffset)
	}
	for i := 0; i < 25; i++ {
		viewport.ScrollDown()
	}
	if viewport.ScrollOffset != 20 {
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
	output := RenderERDiagram(diagram, 80, 20, nil)

	if !strings.Contains(output, "orders") {
		t.Error("expected 'orders' in rendered output")
	}
	if !strings.Contains(output, "users") {
		t.Error("expected 'users' in rendered output")
	}
}

func TestBuildAndRender_OnlyPKFK(t *testing.T) {
	columns := []postgres.ColumnInfo{
		{Name: "id", DataType: "integer"},
		{Name: "user_id", DataType: "integer"},
		{Name: "description", DataType: "text"},
		{Name: "created_at", DataType: "timestamp"},
	}
	constraints := []postgres.ConstraintInfo{
		{Name: "pkey", Type: "PRIMARY KEY", Columns: "id"},
	}
	outgoing := []postgres.ForeignKeyInfo{
		{Name: "fk_user", Column: "user_id", RefSchema: "public", RefTable: "users", RefColumn: "id"},
	}

	diagram := BuildERDiagram("public", "orders", columns, constraints, outgoing, nil)
	output := RenderERDiagram(diagram, 80, 20, nil)

	if !strings.Contains(output, "PK") {
		t.Error("expected PK badge in output")
	}
	if !strings.Contains(output, "FK") {
		t.Error("expected FK badge in output")
	}
	if strings.Contains(output, "description") {
		t.Error("expected 'description' to be filtered out")
	}
	if strings.Contains(output, "created_at") {
		t.Error("expected 'created_at' to be filtered out")
	}
}
