package explorerpreview

// Scenario: El constructor del diagrama ER, y los dos recortes de ancho que solo se ven con
// una caja de tamano cero.
//
// BuildERDiagram walks the foreign keys in both directions and builds the two neighbour
// lists. Three things about it are decisions rather than mechanics, and none of them is
// visible from the rendered box:
//
//	which neighbours make the list — every table that references this one, and every table
//	    this one references, each ONCE
//	what happens past ten — a counter, not a silent cut
//	what a junction table is — one whose every foreign key points somewhere else, so it is
//	    a bridge and not an entity
//
// The truncation helpers have a `width <= 0` guard that no rendering path can reach,
// because every caller derives its width from a window. It is there for the callers that do
// not, and a strings.Repeat with a negative count panics — so it is asserted directly
// rather than through a render.

import (
	"strings"
	"testing"

	"github.com/buble/dbx/internal/drivers/postgres"
)

// fk is a fixture shorthand: this table's column points at refTable.refColumn.
func fkTo(column, refSchema, refTable, refColumn string) postgres.ForeignKeyInfo {
	return postgres.ForeignKeyInfo{
		Name:      fkNameOf(column) + "_fkey",
		Column:    column,
		RefSchema: refSchema,
		RefTable:  refTable,
		RefColumn: refColumn,
	}
}

// tableAliasOf is a stable constraint name from a column, so the fixture's names do not have
// to be written out.
func fkNameOf(column string) string { return "orders_" + column }

// centreColumns is the table at the middle of the diagram.
func centreColumns() []postgres.ColumnInfo {
	return []postgres.ColumnInfo{
		{Name: "id", DataType: "integer"},
		{Name: "user_id", DataType: "integer"},
		{Name: "order_id", DataType: "integer"},
	}
}

func TestTheDiagramCollectsBothDirectionsExactlyOnce(t *testing.T) {
	// orders points at users and invoices; both of them point back at orders. So the centre
	// has two outgoing and two incoming, and each must appear ONCE — a table appearing
	// twice is two boxes in the render and a relationship drawn twice.
	outgoing := []postgres.ForeignKeyInfo{
		fkTo("user_id", "public", "users", "id"),
		fkTo("order_id", "public", "invoices", "id"),
	}
	incoming := map[string][]postgres.ForeignKeyInfo{
		"public.receipts":  {fkTo("order_id", "public", "orders", "id")},
		"public.shipments": {fkTo("order_id", "public", "orders", "id")},
		// A table that references something else entirely is not a neighbour of orders, so
		// it must not appear even though it is in the same map.
		"public.unrelated": {fkTo("product_id", "public", "products", "id")},
	}

	d := BuildERDiagram("public", "orders", centreColumns(), nil, outgoing, incoming)

	if len(d.Outgoing) != 2 {
		t.Errorf("the centre has %d outgoing relationships, want the two it declares", len(d.Outgoing))
	}
	if len(d.Incoming) != 2 {
		names := make([]string, len(d.Incoming))
		for i, r := range d.Incoming {
			names[i] = r.ToTable
		}
		t.Errorf("the centre has %d incoming relationships (%v), want the two tables that reference it", len(d.Incoming), names)
	}
	for _, r := range d.Incoming {
		if r.ToTable == "unrelated" {
			t.Error("a table that references something else was listed as a neighbour of orders")
		}
	}

	t.Run("each neighbour appears once even with several keys pointing the same way", func(t *testing.T) {
		// Two foreign keys from the SAME table into orders. The loop breaks after the first
		// match, so one table contributes one relationship — which is what "neighbour"
		// means here. Two boxes for one table would be wrong twice over.
		two := map[string][]postgres.ForeignKeyInfo{
			"public.receipts": {
				fkTo("order_id", "public", "orders", "id"),
				{Name: "orders_ref_fkey", Column: "ref", RefSchema: "public", RefTable: "orders", RefColumn: "ref"},
			},
		}
		d := BuildERDiagram("public", "orders", centreColumns(), nil, nil, two)
		if len(d.Incoming) != 1 {
			t.Errorf("one table with two keys into the centre contributed %d relationships, want 1", len(d.Incoming))
		}
	})
}

// The cap. Ten neighbours, and the count of what did not fit — which is what the render
// shows as "+N more". A silent cut would make a wide schema look complete.
func TestTheDiagramCountsWhatDidNotFit(t *testing.T) {
	t.Run("incoming", func(t *testing.T) {
		incoming := map[string][]postgres.ForeignKeyInfo{}
		for i := range MaxNeighbors + 4 {
			incoming["public.t"+string(rune('a'+i))] = []postgres.ForeignKeyInfo{
				fkTo("order_id", "public", "orders", "id"),
			}
		}

		d := BuildERDiagram("public", "orders", centreColumns(), nil, nil, incoming)

		if len(d.Incoming) != MaxNeighbors {
			t.Errorf("the diagram holds %d incoming relationships, want the cap of %d", len(d.Incoming), MaxNeighbors)
		}
		if d.IncomingOverflow != 4 {
			t.Errorf("the overflow counter says %d, want 4", d.IncomingOverflow)
		}
	})

	t.Run("outgoing", func(t *testing.T) {
		// A DISTINCT table per key. Several keys pointing at the same table are ONE
		// neighbour — the outgoing list is built per RefTable, not per key — so a fixture
		// with one target measures nothing. The first version of this case pointed all
		// thirteen keys at the same table and reported a cap that was never reached.
		outgoing := make([]postgres.ForeignKeyInfo, 0, MaxNeighbors+3)
		for i := range MaxNeighbors + 3 {
			outgoing = append(outgoing, fkTo(
				"f"+string(rune('a'+i)), "public", "t"+string(rune('a'+i)), "id"))
		}

		d := BuildERDiagram("public", "orders", centreColumns(), nil, outgoing, nil)

		if len(d.Outgoing) != MaxNeighbors {
			t.Errorf("the diagram holds %d outgoing relationships, want the cap of %d", len(d.Outgoing), MaxNeighbors)
		}
		if d.OutgoingOverflow != 3 {
			t.Errorf("the overflow counter says %d, want 3", d.OutgoingOverflow)
		}
	})

	t.Run("exactly at the cap has no overflow", func(t *testing.T) {
		incoming := map[string][]postgres.ForeignKeyInfo{}
		for i := range MaxNeighbors {
			incoming["public.t"+string(rune('a'+i))] = []postgres.ForeignKeyInfo{
				fkTo("order_id", "public", "orders", "id"),
			}
		}
		d := BuildERDiagram("public", "orders", centreColumns(), nil, nil, incoming)

		if len(d.Incoming) != MaxNeighbors {
			t.Errorf("the diagram holds %d relationships, want the full %d", len(d.Incoming), MaxNeighbors)
		}
		if d.IncomingOverflow != 0 {
			t.Errorf("a list exactly at the cap reports %d overflow, want 0", d.IncomingOverflow)
		}
	})
}

// A junction table is one whose every foreign key points somewhere else, so it is a bridge
// between two entities rather than an entity itself. The centre badge in the box says so,
// and mistaking a junction for an entity is what puts a join table in the middle of a
// diagram where it does not belong.
func TestAJunctionTableIsOneWhoseEveryKeyPointsElsewhere(t *testing.T) {
	// order_items points at BOTH orders and products, so it is a bridge.
	junction := []postgres.ForeignKeyInfo{
		fkTo("order_id", "public", "orders", "id"),
		fkTo("product_id", "public", "products", "id"),
	}
	if !isJunctionTable(junction) {
		t.Error("a table whose every key points elsewhere is not treated as a junction")
	}

	// An entity points somewhere but is pointed at as well.
	entity := []postgres.ForeignKeyInfo{
		fkTo("user_id", "public", "users", "id"),
	}
	if isJunctionTable(entity) {
		t.Error("a table with one outgoing key is treated as a junction, which makes every FK table a bridge")
	}

	// Nothing at all is not a junction either — it is a table with no relationships, and
	// the diagram says so in words rather than drawing a bridge.
	if isJunctionTable(nil) {
		t.Error("a table with no keys is treated as a junction")
	}

	t.Run("and the box says so", func(t *testing.T) {
		withMark := renderBox("order_items", []ColumnBadge{
			{Name: "id", DataType: "integer", IsPK: true},
		}, 30, true)
		if !strings.Contains(withMark, "*") {
			t.Errorf("a junction box carries no junction mark:\n%s", withMark)
		}
		without := renderBox("users", []ColumnBadge{
			{Name: "id", DataType: "integer", IsPK: true},
		}, 30, false)
		if strings.Contains(without, "*") {
			t.Errorf("a plain box carries the junction mark:\n%s", without)
		}
	})
}

// The two truncation helpers at a width no render can produce.
//
// Every rendering caller derives its width from a window, so `width <= 0` is unreachable
// through View. It is still the difference between an empty string and a panic:
// strings.Repeat("─", negative) panics, and a box that cannot be given a width is a case a
// caller gets wrong rather than a case the type prevents.
func TestTruncatingToNoWidthIsEmptyRatherThanAPanic(t *testing.T) {
	for _, width := range []int{0, -1, -100} {
		for _, s := range []string{"", "x", "orders", "año", "日本"} {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("truncateToWidth(%q, %d) panicked: %v", s, width, r)
					}
				}()
				if got := truncateToWidth(s, width); got != "" {
					t.Errorf("truncateToWidth(%q, %d) = %q, want the empty string", s, width, got)
				}
			}()
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("cutToWidth(%q, %d) panicked: %v", s, width, r)
					}
				}()
				if got := cutToWidth(s, width); got != "" {
					t.Errorf("cutToWidth(%q, %d) = %q, want the empty string", s, width, got)
				}
			}()
		}
	}
}

// The primary key and foreign key marks in the centre box. The PK set comes from the
// CONSTRAINTS and the FK set from the outgoing keys, and they are marked differently — a
// column that is both is a column the user asked about most often, so losing either mark
// is a visible loss rather than a cosmetic one.
func TestTheCentreBoxMarksKeysAndPlainColumnsDifferently(t *testing.T) {
	constraints := []postgres.ConstraintInfo{{Name: "orders_pkey", Type: "PRIMARY KEY", Columns: "id"}}
	outgoing := []postgres.ForeignKeyInfo{fkTo("user_id", "public", "users", "id")}

	d := BuildERDiagram("public", "orders", centreColumns(), constraints, outgoing, nil)

	byName := map[string]ColumnBadge{}
	for _, c := range d.Center.Columns {
		byName[c.Name] = c
	}
	for name, want := range map[string][2]bool{
		"id":       {true, false},  // PK
		"user_id":  {false, true},  // FK
		"order_id": {false, false}, // plain: its own FK is declared but this centre has none
	} {
		got := byName[name]
		if got.IsPK != want[0] {
			t.Errorf("column %q IsPK is %t, want %t", name, got.IsPK, want[0])
		}
		if got.IsFK != want[1] {
			t.Errorf("column %q IsFK is %t, want %t", name, got.IsFK, want[1])
		}
	}
}
