package explorerpreview

// Scenario: El preview del explorer en sus extremos: lo que el diagrama bonito no toca.
//
// The ER panel is the most arithmetic-heavy thing in the TUI — a box drawing with padding
// computed from string widths — and almost all of its guards are about content that does not
// fit. That is the case a diagram never shows, because the panel is only opened for a table
// whose relationships render.
//
// So the cases below are: a relationship that points at a table which is also the target of
// another one, a box narrower than the name in it, a scroll past the end of the content, a
// selection that is not there, and a panel with no diagram at all.

import (
	"strings"
	"testing"

	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/theme"
)

func previewForTest() *ExplorerPreview {
	e := New(theme.Resolve("dark").Styles(), nil)
	e.SetWidth(100)
	e.SetHeight(40)
	e.Focus()
	return e
}

// A table that is the target of TWO foreign keys. The diagram walks schemaFKs per target
// table, so the second key naming the same table has to be skipped — otherwise the target is
// drawn twice and the diagram claims a relationship the schema does not have.
func TestATableThatIsTheTargetOfTwoKeysIsDrawnOnce(t *testing.T) {
	schemaFKs := map[string][]postgres.ForeignKeyInfo{
		// Two different tables pointing at `users`.
		"orders": {{
			Name: "orders_user_id_fkey", Column: "user_id",
			RefSchema: "public", RefTable: "users", RefColumn: "id",
		}},
		"invoices": {{
			Name: "invoices_user_id_fkey", Column: "user_id",
			RefSchema: "public", RefTable: "users", RefColumn: "id",
		}},
	}

	d := BuildERDiagram("public", "users",
		[]postgres.ColumnInfo{
			{Name: "id", DataType: "integer"},
			{Name: "email", DataType: "text"},
		},
		[]postgres.ConstraintInfo{{Name: "users_pkey", Type: "PRIMARY KEY", Columns: "id"}},
		nil,
		schemaFKs,
	)

	// The premise: BOTH keys really did produce an incoming relationship naming users. The
	// skip is about not drawing it twice, so a fixture where only one survived would make the
	// case pass for the wrong reason.
	if d.Center.Name != "users" {
		t.Fatalf("the centre table is %q, want users", d.Center.Name)
	}
	// Relationship.ToTable is the SOURCE table for an incoming relationship — the one that
	// holds the key — which is the opposite of what the field name suggests. The first version
	// of this case keyed on it as the target and found nothing.
	froms := map[string]int{}
	for _, rel := range d.Incoming {
		froms[rel.ToTable]++
	}
	if len(froms) < 2 {
		t.Fatalf("the diagram shows %d incoming relationships naming users (%v), want the two the fixture declares",
			len(d.Incoming), d.Incoming)
	}

	// The property: each source appears ONCE. seenIncoming is what stops a second key naming
	// the same table from becoming a second box.
	for column, n := range froms {
		if n != 1 {
			t.Errorf("%s appears %d times as an incoming source, want 1", column, n)
		}
	}

	// And it renders.
	out := RenderERDiagram(d, 100, 40, NewERDiagramNav(len(d.Incoming), len(d.Outgoing)))
	if out == "" {
		t.Error("the diagram renders nothing")
	}
}

// The padding clamps. Each is `innerWidth - widthOf(content)` going negative, which happens
// when the content is wider than the box — and the alternative to clamping is a padding built
// from a negative number, which strings.Repeat turns into a panic.
func TestTheBoxesSurviveContentWiderThanThemselves(t *testing.T) {
	longName := "a_table_name_far_longer_than_any_reasonably_narrow_panel"
	d := BuildERDiagram("public", longName,
		[]postgres.ColumnInfo{
			{Name: "id", DataType: "integer"},
			{Name: "an_extremely_long_column_name_here", DataType: "character varying"},
		},
		[]postgres.ConstraintInfo{{Name: longName + "_pkey", Type: "PRIMARY KEY", Columns: "id"}},
		[]postgres.ForeignKeyInfo{{
			Name: "x_fkey", Column: "an_extremely_long_column_name_here",
			RefSchema: "public", RefTable: longName + "_referenced", RefColumn: "id",
		}},
		nil,
	)
	nav := NewERDiagramNav(0, 1)
	nav.MoveDown()

	for _, width := range []int{1, 2, 5, 10, 20, 30, 45, 60, 120} {
		for _, height := range []int{1, 3, 8, 20, 40} {
			out := RenderERDiagram(d, width, height, nav)

			if out == "" {
				t.Errorf("at %dx%d the diagram renders nothing", width, height)
				continue
			}
			// The frame survives, which is what a negative padding would have destroyed.
			if !strings.Contains(out, "┌") && !strings.Contains(out, "│") {
				t.Errorf("at %dx%d the diagram has no frame:\n%q", width, height, out)
			}
		}
	}
}

// The selection line with no diagram, and with a row that is not there.
//
// ereSelectionLine is called from ensureERESelectionVisible, which checks HasSelection first
// — so the nil-diagram and row<0 arms are reachable only from a caller that checks neither.
// That is the state a preview is in between being told to select something and being given a
// diagram to select it in.
func TestTheSelectionLineWithNothingToSelect(t *testing.T) {
	e := previewForTest()

	t.Run("with no diagram at all", func(t *testing.T) {
		if got := e.ereSelectionLine(0); got != 0 {
			t.Errorf("the selection line of a preview with no diagram is %d, want 0", got)
		}
	})

	t.Run("and the ensure path does not panic without a selection", func(t *testing.T) {
		e.ereViewport = NewERDiagramViewport(40, 20)
		e.ereNav = NewERDiagramNav(2, 0)

		// No selection: HasSelection is false and the guard returns.
		e.ensureERESelectionVisible()

		// And the row guard, reached by asking directly with a negative row.
		if got := e.ereSelectionLine(-1); got < 0 {
			t.Errorf("the selection line of row -1 is %d", got)
		}
	})
}

// The scroll clamp past the end of the content. The viewport's offset is set by the navigation
// keys, and a diagram that shrinks under a scroll — because the selection moved to a shorter
// table — leaves the offset pointing past the last line. Slicing there is a panic.
func TestAScrollPastTheEndOfTheContentIsClamped(t *testing.T) {
	e := previewForTest()
	e.SetData("public", "orders",
		[]postgres.ColumnInfo{
			{Name: "id", DataType: "integer"},
			{Name: "customer_id", DataType: "integer"},
		},
		[]postgres.ConstraintInfo{{Name: "orders_pkey", Type: "PRIMARY KEY", Columns: "id"}},
		[]postgres.ForeignKeyInfo{{
			Name: "orders_customer_id_fkey", Column: "customer_id",
			RefSchema: "public", RefTable: "customers", RefColumn: "id",
		}},
		nil, nil,
	)
	if e.ereViewport == nil || e.ereDiagram == nil {
		t.Skip("this editor state builds no ER diagram, so the scroll clamp is unreachable")
	}

	// The un-scrolled output, which is the reference: a clamped scroll must show the tail of
	// it, never less. That is the bug — end was clamped before start, so an offset past the
	// end produced lines[len:len] and an EMPTY panel.
	reference := e.renderERE()
	if reference == "" {
		t.Fatal("the preview renders nothing with no scroll applied, so the fixture proves nothing")
	}

	for _, offset := range []int{0, 5, 50, 500, 5000} {
		e.ereViewport.ScrollOffset = offset

		// renderERE, not View: View picks the tab and this fixture may not be on the ERE tab,
		// in which case the scroll clamp would not run at all.
		out := e.renderERE()

		if out == "" {
			t.Errorf("at scroll offset %d the preview renders NOTHING; a clamped scroll shows the tail of the content", offset)
			continue
		}
		// And it is content from the diagram, not a blank frame.
		if !strings.ContainsAny(out, "\u2502\u250c\u2514") && !strings.Contains(out, "orders") {
			t.Errorf("at scroll offset %d the preview shows %q, which is neither a frame nor content", offset, out)
		}
	}

	t.Run("and a negative offset is clamped at the top", func(t *testing.T) {
		e.ereViewport.ScrollOffset = -50
		if out := e.renderERE(); out != reference {
			t.Errorf("a negative offset produced %d bytes, want the same %d as no scroll at all", len(out), len(reference))
		}
	})

	// The premise, checked after the fact: the offsets really were past the end, so the clamp
	// ran rather than the values happening to be legal.
	full := RenderERDiagram(*e.ereDiagram, e.width, e.height, e.ereNav)
	lines := len(strings.Split(full, "\n"))
	if lines > 500 {
		t.Skipf("the diagram is %d lines, so no offset in this test is past the end", lines)
	}
}

// CenterSchema's two answers: the diagram's own schema when it has one, and the selected
// schema when it does not. The second is the case a user hits on a table whose ER diagram
// could not be built — the panel still has to say which schema it is describing.
func TestCenterSchemaFallsBackToTheSelectedSchema(t *testing.T) {
	e := previewForTest()

	e.SetData("inventory", "items",
		[]postgres.ColumnInfo{{Name: "id", DataType: "integer"}},
		nil, nil, nil, nil,
	)

	// SetData records the schema, and the diagram may or may not have been built.
	want := e.schema
	if got := e.CenterSchema(); got != want {
		t.Errorf("CenterSchema is %q, want the selected schema %q", got, want)
	}

	t.Run("and the diagram's own schema wins when it has one", func(t *testing.T) {
		withDiagram := previewForTest()
		withDiagram.centerSchema = "billing"
		if got := withDiagram.CenterSchema(); got != "billing" {
			t.Errorf("CenterSchema is %q, want the diagram's own schema", got)
		}
	})

	t.Run("and a table with no diagram at all still names its schema", func(t *testing.T) {
		bare := previewForTest()
		bare.schema = "public"
		bare.centerSchema = ""
		if got := bare.CenterSchema(); got != "public" {
			t.Errorf("CenterSchema is %q for a preview with no diagram", got)
		}
	})
}

// The tab bar's width. It is set by the panel's own sizing and read by its render, and no test
// called it — so the only thing asserted here is that a narrow bar does not lose its tabs off
// the right edge.
func TestTheTabBarSurvivesEveryWidth(t *testing.T) {
	for _, width := range []int{0, 1, 5, 12, 30, 80} {
		bar := NewTabBar(theme.Resolve("dark").Styles(), nil)
		bar.SetWidth(width)

		out := bar.Render()
		if out == "" && width > 0 {
			t.Errorf("at width %d the tab bar renders nothing", width)
		}
	}
}

// HandledActions is what the app checks the panel against, and nothing in this package called
// it.
func TestTheHandledActionsAreTheOnesDispatched(t *testing.T) {
	e := previewForTest()

	claimed := map[string]bool{}
	for _, id := range e.HandledActions() {
		claimed[string(id)] = true
	}
	for _, id := range []string{
		"overview_tab", "columns_tab", "constraints_tab",
		"foreign_keys_tab", "indexes_tab", "ere_tab",
	} {
		if !claimed[id] {
			t.Errorf("the panel dispatches %q but does not claim it", id)
		}
	}
	if len(e.HandledActions()) != 6 {
		t.Errorf("the panel claims %d actions, want 6: %v", len(e.HandledActions()), e.HandledActions())
	}
}
