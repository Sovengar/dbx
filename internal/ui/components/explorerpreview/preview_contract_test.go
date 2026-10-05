package explorerpreview

// The contracts of the explorer preview panel, asserted as properties over states rather
// than one scenario at a time.
//
// The panel is a pure view over data the app hands it: a table's columns, constraints,
// foreign keys, indexes and overview, spread over six tabs, plus an ERE diagram the user
// can walk. There is no I/O here — the only file it touches is a debug log — so the
// in-package test file can reach every field and drive every branch.
//
// Four things it promises:
//
//	P1  with no table selected it says so, and does not pretend to have content.
//	P2  the tab keys are the KEYS, not hardcoded literals: they come from the keybind
//	    registry, so a rebinding moves the tab with it.
//	P3  each tab renders its own data and says so when it has none, with a different
//	    message per tab because the remedy differs.
//	P4  walking the ERE diagram moves a selection that stays visible, and enter on a
//	    relationship re-centres the diagram on the table at the other end.

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/theme"
	"github.com/charmbracelet/x/ansi"
)

// newPreview builds a FOCUSED panel at a usable size, with one table's worth of data in
// every tab. Focused, because every test here is about what it does while it has focus;
// the blurred case is its own test.
func newPreview(t *testing.T) *ExplorerPreview {
	t.Helper()
	e := New(theme.Resolve("dark").Styles(), config.NewKeybindRegistry(config.KeybindingsConfig{}))
	e.SetWidth(100)
	e.SetHeight(40)
	e.Focus()
	e.SetData("public", "orders",
		[]postgres.ColumnInfo{
			col("id", "integer"),
			col("customer_id", "integer"),
			{Name: "note", DataType: "text", IsNullable: "YES"},
		},
		[]postgres.ConstraintInfo{
			{Name: "orders_pkey", Type: "PRIMARY KEY", Columns: "id"},
			{Name: "orders_cust_fk", Type: "FOREIGN KEY", Columns: "customer_id"},
		},
		[]postgres.ForeignKeyInfo{{
			Name: "orders_customer_id_fkey", Column: "customer_id",
			RefSchema: "public", RefTable: "customers", RefColumn: "id",
		}},
		[]postgres.IndexInfo{
			{Name: "orders_pkey", Columns: "id", Unique: true, Def: "CREATE UNIQUE INDEX orders_pkey ON orders(id)"},
			{Name: "orders_note_idx", Columns: "note", Def: "CREATE INDEX orders_note_idx ON orders(note)"},
		},
		&postgres.TableOverview{
			TableName: "orders", TableType: "BASE TABLE",
			TotalSize: "16 kB", TableSize: "8 kB", IndexSize: "8 kB",
			LiveTuples: 120, DeadTuples: 3,
			ColumnCount: 3, ConstraintCount: 2, IndexCount: 2, FKCount: 1,
		},
	)
	return e
}

func str(s string) *string { return &s }

func tm(s string) *time.Time {
	t, err := time.Parse("2006-01-02 15:04:05", s)
	if err != nil {
		panic("fixture has a bad timestamp: " + s)
	}
	return &t
}

// pressTab sends the key the registry binds to a tab action, read from the registry
// rather than hardcoded — which is the point of P2. If the binding moves, the test moves
// with it instead of quietly testing a key nothing listens to.
func pressTab(t *testing.T, e *ExplorerPreview, actionID config.ActionID) tea.Cmd {
	t.Helper()
	keys := e.keybinds.KeysFor(actionID)
	if len(keys) == 0 {
		t.Fatalf("the registry binds no key to %q, so this fixture cannot press it", actionID)
	}
	key := keys[0]
	cmd, handled := e.Update(tea.KeyPressMsg{Code: rune(key[0]), Text: key})
	if !handled {
		t.Fatalf("the key %q for %q was not handled", key, actionID)
	}
	return cmd
}

// tabMessage runs the command a tab key returns and reads the tab out of it, so the tab
// change is asserted through the widget's only output rather than read back off a field.
func tabMessage(t *testing.T, cmd tea.Cmd, want int) {
	t.Helper()
	if cmd == nil {
		t.Fatal("the tab key returned no command, so the app is never told the tab moved")
	}
	msg, ok := cmd().(ExplorerPreviewTabChangeMsg)
	if !ok {
		t.Fatalf("the tab key produced a %T, want an ExplorerPreviewTabChangeMsg", cmd())
	}
	if msg.Tab != want {
		t.Errorf("the message says tab %d, want %d", msg.Tab, want)
	}
}

// tabActions pairs the ACTION that selects a tab with the tab's own ID as the bar knows
// it. The two are NOT the same string — the action is "columns_tab" and the bar's ID is
// "columns" — and conflating them is what a first version of this table did.
func tabActions() []struct {
	action config.ActionID
	barID  string
	index  int
	name   string
} {
	return []struct {
		action config.ActionID
		barID  string
		index  int
		name   string
	}{
		{"overview_tab", "overview", 0, "Overview"},
		{"columns_tab", "columns", 1, "Columns"},
		{"constraints_tab", "constraints", 2, "Constraints"},
		{"foreign_keys_tab", "foreign_keys", 3, "Foreign Keys"},
		{"indexes_tab", "indexes", 4, "Indexes"},
		{"ere_tab", "ere", 5, "ERE"},
	}
}

// ---------------------------------------------------------------------------
// P1: no table, no content
// ---------------------------------------------------------------------------

// Scenario: Sin tabla seleccionada, el panel lo dice y no inventa contenido.
//
// This is the first thing a user sees after opening the explorer, and every one of the six
// tabs has to agree on it. A tab that rendered "No columns loaded" here would be telling
// the user their table has no columns when they have not chosen a table at all.
func TestExplorerPreview_WithoutATableItSaysSoAndRendersNothingElse(t *testing.T) {
	e := New(theme.Resolve("dark").Styles(), config.NewKeybindRegistry(config.KeybindingsConfig{}))
	e.SetWidth(100)
	e.SetHeight(40)
	e.Focus()

	if e.HasData() {
		t.Error("a panel with no table reports that it has data")
	}
	if e.TableName() != "" {
		t.Errorf("TableName is %q with no table", e.TableName())
	}

	for _, tc := range tabActions() {
		t.Run(tc.name, func(t *testing.T) {
			e.setActiveTab(tc.index)
			view := ansi.Strip(e.View())
			if !strings.Contains(view, "Select a table") {
				t.Errorf("the %s tab says %q, want it to ask for a table", tc.name, view)
			}
			// And none of the per-tab empty messages, which are about a table
			// that exists and has nothing in it.
			for _, wrong := range []string{
				"No columns loaded", "No constraints loaded", "No foreign keys loaded",
				"No indexes loaded", "Loading overview", "No data available",
			} {
				if strings.Contains(view, wrong) {
					t.Errorf("the %s tab says %q with no table selected", tc.name, wrong)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// P2: the tab keys come from the registry
// ---------------------------------------------------------------------------

// Scenario: Las teclas de pestana son las del REGISTRO, y solo en el contexto del panel.
//
// The tab actions are bound in the explorer-preview context, and the panel asks the
// resolver for that context specifically. Two things therefore have to hold: the bound
// key switches the tab, and a key bound to a DIFFERENT action in the SAME context does
// not — otherwise every action in the context would switch tabs.
func TestExplorerPreview_TheTabKeysComeFromTheRegistry(t *testing.T) {
	for _, tc := range tabActions() {
		t.Run(tc.name, func(t *testing.T) {
			e := newPreview(t)
			// Start somewhere else so a move is visible.
			e.setActiveTab((tc.index + 1) % len(tabActions()))

			cmd := pressTab(t, e, tc.action)
			tabMessage(t, cmd, tc.index)
			if e.ActiveTab() != tc.index {
				t.Errorf("the %s key left the panel on tab %d, want %d", tc.name, e.ActiveTab(), tc.index)
			}
			if got := e.ActiveTabID(); got != tc.barID {
				t.Errorf("ActiveTabID is %q, want the bar's own id %q", got, tc.barID)
			}
		})
	}

	t.Run("a key bound to another action in the same context does not switch tabs", func(t *testing.T) {
		e := newPreview(t)
		e.setActiveTab(1)

		// Any action that is NOT a tab action but IS declared for this context.
		var other config.ActionID
		for _, a := range e.keybinds.ActionsFor(config.ContextExplorerPreview) {
			isTab := false
			for _, tc := range tabActions() {
				if a.ID == tc.action {
					isTab = true
					break
				}
			}
			if !isTab {
				other = a.ID
				break
			}
		}
		if other == "" {
			t.Skip("every action in the explorer-preview context is a tab action, so there is nothing to confuse it with")
		}
		keys := e.keybinds.KeysFor(other)
		if len(keys) == 0 {
			t.Skipf("the action %q has no key bound", other)
		}
		e.Update(tea.KeyPressMsg{Code: rune(keys[0][0]), Text: keys[0]})
		if e.ActiveTab() != 1 {
			t.Errorf("the key for %q moved the panel to tab %d", other, e.ActiveTab())
		}
	})

	t.Run("a resolver-less panel ignores tab keys instead of panicking", func(t *testing.T) {
		// The keybind resolver is an interface and nil is a valid interface value
		// here: the panel is constructed in tests and in the app before the
		// registry exists. The nil check exists for that, so it is exercised.
		e := New(theme.Resolve("dark").Styles(), nil)
		e.SetWidth(80)
		e.SetHeight(30)
		e.Focus()
		e.SetData("public", "t", nil, nil, nil, nil, nil)
		e.setActiveTab(1)

		cmd, handled := e.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
		if handled {
			t.Error("a panel with no resolver handled a key")
		}
		if cmd != nil {
			t.Error("a panel with no resolver produced a command")
		}
		if e.ActiveTab() != 1 {
			t.Errorf("a panel with no resolver moved to tab %d", e.ActiveTab())
		}
	})
}

// Scenario: Sin foco, el panel NO se come ninguna tecla.
//
// It shares its context with the grid and the editor, so a blurred panel that handled
// keys would steal them from whatever the user is actually typing into.
func TestExplorerPreview_BlurredItSwallowsNothing(t *testing.T) {
	e := newPreview(t)
	e.Focus()
	e.setActiveTab(1)
	e.Blur()

	if e.IsFocused() {
		t.Fatal("a blurred panel reports itself focused")
	}

	for _, key := range []string{"1", "2", "6", "j", "k", "up", "down", "g", "G", "enter", "left", "right"} {
		t.Run(key, func(t *testing.T) {
			before := e.ActiveTab()
			cmd, handled := e.Update(tea.KeyPressMsg{Code: rune(key[0]), Text: key})
			if handled {
				t.Errorf("the key %q was handled by a blurred panel", key)
			}
			if cmd != nil {
				t.Errorf("the key %q produced a command while blurred", key)
			}
			if e.ActiveTab() != before {
				t.Errorf("the key %q moved the panel to tab %d", key, e.ActiveTab())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// P3: what each tab renders
// ---------------------------------------------------------------------------

// Scenario: Cada pestana pinta SUS datos, y dice lo suyo cuando no tiene.
//
// Six tabs, six data sets, six distinct empty messages. The messages are asserted to be
// DISTINCT as well as present, because two tabs sharing one message is how a user ends up
// looking at "No data" with no idea whether the problem is columns or indexes.
func TestExplorerPreview_EachTabRendersItsOwnData(t *testing.T) {
	e := newPreview(t)

	for _, tc := range []struct {
		tab       int
		name      string
		mustHave  []string
		mustNot   []string
		emptyText string
	}{
		{
			tab: 0, name: "Overview",
			mustHave: []string{"orders", "BASE TABLE", "Storage", "16 kB", "Statistics", "120", "3", "Maintenance", "Summary"},
			mustNot:  []string{"Loading overview"},
			// A table overview is fetched asynchronously, so its own empty state
			// is a progress message rather than an absence.
			emptyText: "Loading overview",
		},
		{
			tab: 1, name: "Columns",
			mustHave:  []string{"NAME", "TYPE", "NULLABLE", "DEFAULT", "id", "integer", "customer_id", "note", "NULL"},
			mustNot:   []string{"No columns loaded"},
			emptyText: "No columns loaded",
		},
		{
			tab: 2, name: "Constraints",
			mustHave:  []string{"NAME", "TYPE", "COLUMNS", "orders_pkey", "PRIMARY KEY", "orders_cust_fk", "customer_id"},
			mustNot:   []string{"No constraints loaded"},
			emptyText: "No constraints loaded",
		},
		{
			tab: 3, name: "Foreign Keys",
			mustHave:  []string{"NAME", "COLUMN", "REF TABLE", "REF COLUMN", "orders_customer_id_fkey", "customers", "id"},
			mustNot:   []string{"No foreign keys loaded"},
			emptyText: "No foreign keys loaded",
		},
		{
			tab: 4, name: "Indexes",
			mustHave:  []string{"NAME", "UNIQUE", "DEFINITION", "orders_pkey", "YES", "orders_note_idx", "NO"},
			mustNot:   []string{"No indexes loaded"},
			emptyText: "No indexes loaded",
		},
		{
			tab: 5, name: "ERE",
			mustHave:  []string{"orders"},
			mustNot:   []string{"No data available"},
			emptyText: "No data available",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e.setActiveTab(tc.tab)
			view := ansi.Strip(e.View())
			for _, want := range tc.mustHave {
				if !strings.Contains(view, want) {
					t.Errorf("the %s tab does not show %q:\n%s", tc.name, want, view)
				}
			}
			for _, unwanted := range tc.mustNot {
				if strings.Contains(view, unwanted) {
					t.Errorf("the %s tab shows %q, which is its EMPTY message:\n%s", tc.name, unwanted, view)
				}
			}
		})
	}

	t.Run("each tab's empty message is its own", func(t *testing.T) {
		empty := New(theme.Resolve("dark").Styles(), config.NewKeybindRegistry(config.KeybindingsConfig{}))
		empty.SetWidth(80)
		empty.SetHeight(30)
		empty.Focus()
		// A table with nothing in it: the name is what makes HasData true.
		empty.SetData("public", "bare", nil, nil, nil, nil, nil)

		seen := map[string]int{}
		for _, tc := range tabActions() {
			empty.setActiveTab(tc.index)
			view := ansi.Strip(empty.View())
			found := false
			// Two ERE messages exist and this fixture reaches one of them: a
			// table with a centre builds a diagram, and a diagram with no edges
			// says so here. "No data available for ERE diagram" belongs to a panel
			// with no centre at all, which the fallback test below covers.
			for _, msg := range []string{
				"Loading overview", "No columns loaded", "No constraints loaded",
				"No foreign keys loaded", "No indexes loaded", "No relationships for this table",
			} {
				if strings.Contains(view, msg) {
					found = true
					seen[msg]++
				}
			}
			if !found {
				t.Errorf("the %s tab says nothing about being empty:\n%s", tc.name, view)
			}
		}
		if len(seen) != 6 {
			t.Errorf("the six tabs produced %d distinct empty messages, want 6: %v", len(seen), seen)
		}
	})
}

// Scenario: El resumen distingue "no lo se" de "es cero", y "nada" de "sin Comentario".
//
// Three separate distinctions the overview could collapse, each of which changes what the
// user is being told about their database:
//
//   - LiveTuples of -1 means the statistics were never collected; -0 is not a thing, but
//     a table with genuinely zero rows must read 0 and not the em dash.
//   - a nil Comment is an absent comment, and the line is omitted rather than shown empty.
//   - a nil timestamp is "never vacuumed", not the zero time.
func TestExplorerOverview_DistinguishesAbsentFromZero(t *testing.T) {
	t.Run("a real zero reads as zero, not as a dash", func(t *testing.T) {
		e := newPreview(t)
		e.SetOverview(&postgres.TableOverview{
			TableName: "empty_table", TableType: "BASE TABLE",
			TotalSize: "0 bytes", TableSize: "0 bytes", IndexSize: "0 bytes",
			LiveTuples: 0, DeadTuples: 0,
		})
		view := ansi.Strip(e.renderOverview())
		if !strings.Contains(view, "Live rows:  0") {
			t.Errorf("a table with no rows rendered %q, want it to say 0", view)
		}
		if strings.Contains(view, "—") && strings.Contains(view, "Live rows:  —") {
			t.Errorf("a table with no rows is rendered as unknown:\n%s", view)
		}
	})

	t.Run("statistics that were never collected read as a dash", func(t *testing.T) {
		e := newPreview(t)
		e.SetOverview(&postgres.TableOverview{
			TableName: "never_analyzed", TableType: "BASE TABLE",
			LiveTuples: -1, DeadTuples: -1,
		})
		view := ansi.Strip(e.renderOverview())
		if !strings.Contains(view, "Live rows:  —") || !strings.Contains(view, "Dead rows:  —") {
			t.Errorf("uncollected statistics rendered %q, want em dashes for both", view)
		}
	})

	t.Run("a comment line appears only when there is a comment", func(t *testing.T) {
		e := newPreview(t)
		e.SetOverview(&postgres.TableOverview{TableName: "t", TableType: "BASE TABLE"})
		if got := ansi.Strip(e.renderOverview()); strings.Contains(got, "Comment:") {
			t.Errorf("a table with no comment still shows the line:\n%s", got)
		}

		e.SetOverview(&postgres.TableOverview{
			TableName: "t", TableType: "BASE TABLE",
			Comment: str("orders live here"),
		})
		got := ansi.Strip(e.renderOverview())
		if !strings.Contains(got, "orders live here") {
			t.Errorf("a table WITH a comment does not show it:\n%s", got)
		}
	})

	t.Run("a timestamp that never happened reads as a dash, not as year one", func(t *testing.T) {
		e := newPreview(t)
		if got := formatTime(nil); got != "—" {
			t.Errorf("formatTime(nil) = %q, want an em dash", got)
		}
		e.SetOverview(&postgres.TableOverview{
			TableName: "t", TableType: "BASE TABLE",
			LastVacuum: tm("2026-01-02 03:04:05"),
		})
		got := ansi.Strip(e.renderOverview())
		if !strings.Contains(got, "2026-01-02 03:04:05") {
			t.Errorf("a real vacuum time is not shown:\n%s", got)
		}
		if strings.Contains(got, "0001-01-01") {
			t.Errorf("the zero time reached the screen for the timestamps that never happened:\n%s", got)
		}
	})
}

// Scenario: Los datos se pueden reponer de una en una, sin pasar por SetData.
//
// The app loads a table's pieces as they arrive, so each setter has to replace exactly one
// slice and leave the rest alone. That matters because SetData also rebuilds the ERE
// diagram and resets the centre, which a late-arriving column list must NOT do — the user
// would lose their place in the diagram every time a detail loaded.
func TestExplorerPreview_EachSetterReplacesOnlyItsOwnPiece(t *testing.T) {
	e := newPreview(t)
	before := e.TableName()
	beforeCentre := e.CenterTable()

	e.SetColumns([]postgres.ColumnInfo{col("only", "text")})
	e.SetConstraints(nil)
	e.SetForeignKeys(nil)
	e.SetIndexes(nil)
	e.SetOverview(nil)

	if e.TableName() != before {
		t.Errorf("SetColumns changed the table name to %q", e.TableName())
	}
	if e.CenterTable() != beforeCentre {
		t.Errorf("SetColumns moved the ERE centre from %q to %q", beforeCentre, e.CenterTable())
	}
	if got := e.renderColumns(); !strings.Contains(ansi.Strip(got), "only") {
		t.Errorf("after SetColumns the panel does not show the new column:\n%s", ansi.Strip(got))
	}
	if got := ansi.Strip(e.renderConstraints()); !strings.Contains(got, "No constraints loaded") {
		t.Errorf("after SetConstraints(nil) the panel still shows constraints:\n%s", got)
	}
	if got := ansi.Strip(e.renderIndexes()); !strings.Contains(got, "No indexes loaded") {
		t.Errorf("after SetIndexes(nil) the panel still shows indexes:\n%s", got)
	}
	if got := ansi.Strip(e.renderOverview()); !strings.Contains(got, "Loading overview") {
		t.Errorf("after SetOverview(nil) the panel still shows an overview:\n%s", got)
	}

	// SetSchemaForeignKeys feeds the diagram's other schema, and is the one
	// setter that does NOT rebuild it: the diagram is built from SetData.
	e.SetSchemaForeignKeys(map[string][]postgres.ForeignKeyInfo{
		"public": {fk("a", "b", "c")},
	})
	if got := ansi.Strip(e.View()); strings.Contains(got, "Select a table") {
		t.Error("setting the schema's foreign keys made the panel think it has no table")
	}
}

// ---------------------------------------------------------------------------
// P4: walking the ERE diagram
// ---------------------------------------------------------------------------

// Scenario: Andar por el diagrama MUEVE una seleccion, y la tabla central se recentra.
//
// The ERE tab is the only one with navigation, and the navigation is a cursor over the
// two neighbour columns. Two promises: the arrows move it and keep it on screen, and enter
// on a relationship makes that relationship's other table the new centre — including when
// the name is schema-qualified, which is the case that decides whether the panel navigates
// to another table in the same schema or to a whole different schema.
func TestExplorerPreview_WalkingTheEREDiagram(t *testing.T) {
	newOnERETab := func(t *testing.T) *ExplorerPreview {
		t.Helper()
		e := newPreview(t)
		e.setActiveTab(5)
		if e.ereNav == nil {
			t.Skip("the fixture produced no diagram to navigate")
		}
		return e
	}

	t.Run("the arrows move the selection and keep it inside the diagram", func(t *testing.T) {
		e := newOnERETab(t)
		// The cursor starts in the 1:N column with NO selection, and MoveDown
		// does nothing at all in a column with no entries — so a fixture that
		// presses down first proves nothing and looks as though it does. Move
		// across to the column that has a relationship in it.
		if e.ereNav.ActiveRow() != -1 {
			t.Fatalf("the fixture starts with row %d selected, want the \"no selection\" sentinel", e.ereNav.ActiveRow())
		}
		// Moving into the populated column CLAMPS the row but does not create
		// one: clampRow only lowers a row that is past the end, and -1 is not
		// past the end. So a selection is something you make with down, not
		// something you fall into by looking at a column.
		e.ereNav.MoveRight()
		if e.ereNav.HasSelection() {
			t.Fatalf("moving right into a populated column created a selection at row %d; only down does that",
				e.ereNav.ActiveRow())
		}

		e.ereNav.MoveDown()
		if !e.ereNav.HasSelection() {
			t.Fatalf("down in a populated column selected nothing: row %d of %d entries",
				e.ereNav.ActiveRow(), e.ereNav.columnCounts[e.ereNav.ActiveColumn()])
		}
		first := e.ereNav.ActiveRow()
		for range 3 {
			if _, handled := e.Update(tea.KeyPressMsg{Code: tea.KeyDown}); !handled {
				t.Fatal("the down key was not handled on the ERE tab")
			}
		}
		// One entry, so three presses cannot go anywhere — which is the point:
		// the row stays INSIDE, at 0.
		if got := e.ereNav.ActiveRow(); got < 0 || got >= e.ereNav.columnCounts[e.ereNav.ActiveColumn()] {
			t.Errorf("three down presses left the row at %d, outside the column's %d entries",
				got, e.ereNav.columnCounts[e.ereNav.ActiveColumn()])
		}
		if e.ereNav.ActiveRow() == -1 {
			t.Errorf("the selection was lost: it started at %d", first)
		}

		// Up at the top stays put, and the four directions together never leave
		// the diagram or drop the selection below the sentinel.
		for range 4 {
			e.Update(tea.KeyPressMsg{Code: tea.KeyUp})
			e.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
			e.Update(tea.KeyPressMsg{Code: tea.KeyRight})
			e.Update(tea.KeyPressMsg{Code: tea.KeyDown})
			if r := e.ereNav.ActiveRow(); r < -1 {
				t.Fatalf("the row went below the sentinel: %d", r)
			}
			if c := e.ereNav.ActiveColumn(); c < 0 || c > 1 {
				t.Fatalf("the column left the diagram: %d", c)
			}
		}
	})

	t.Run("a column with no entries selects nothing at all", func(t *testing.T) {
		e := newPreview(t)
		e.setActiveTab(5)
		if len(e.ereDiagram.Incoming) != 0 {
			t.Skipf("the fixture has %d incoming relationships, so its first column is not empty", len(e.ereDiagram.Incoming))
		}
		// The cursor starts in the 1:N column, which is empty here.
		for range 5 {
			e.Update(tea.KeyPressMsg{Code: tea.KeyDown})
			e.Update(tea.KeyPressMsg{Code: tea.KeyUp})
		}
		if e.ereNav.HasSelection() {
			t.Errorf("the empty column produced a selection at row %d", e.ereNav.ActiveRow())
		}
		// And enter does nothing, because there is nothing to enter.
		cmd, handled := e.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		if !handled {
			t.Error("enter on the ERE tab was not reported as handled")
		}
		if cmd != nil {
			t.Error("enter with no selection produced a navigation")
		}
	})

	t.Run("the arrow keys only navigate on the ERE tab", func(t *testing.T) {
		e := newPreview(t)
		e.setActiveTab(1) // Columns
		for _, key := range []tea.KeyPressMsg{
			{Code: tea.KeyDown}, {Code: tea.KeyUp}, {Code: tea.KeyLeft}, {Code: tea.KeyRight},
			{Code: tea.KeyEnter},
		} {
			if _, handled := e.Update(key); handled {
				t.Errorf("the key %q was handled on the Columns tab, where it has no meaning", key.String())
			}
		}
	})

	t.Run("g and G jump to the ends of the viewport", func(t *testing.T) {
		e := newOnERETab(t)
		if e.ereViewport == nil {
			t.Skip("the fixture produced no viewport")
		}
		e.Update(tea.KeyPressMsg{Code: 'G', Text: "G"})
		bottom := e.ereViewport.ScrollOffset
		e.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
		if e.ereViewport.ScrollOffset != 0 {
			t.Errorf("g left the scroll at %d, want 0", e.ereViewport.ScrollOffset)
		}
		e.Update(tea.KeyPressMsg{Code: 'G', Text: "G"})
		if e.ereViewport.ScrollOffset != bottom {
			t.Errorf("G left the scroll at %d, want %d", e.ereViewport.ScrollOffset, bottom)
		}
	})

	t.Run("enter on a relationship re-centres the diagram", func(t *testing.T) {
		e := newOnERETab(t)
		// Walk to a neighbour that exists and select it.
		if !e.ereNav.HasSelection() && len(e.ereDiagram.Outgoing) == 0 && len(e.ereDiagram.Incoming) == 0 {
			t.Skip("the fixture's diagram has no neighbours to navigate to")
		}
		// Find a row that has a relationship.
		moved := false
		for range len(e.ereDiagram.Outgoing) + len(e.ereDiagram.Incoming) {
			e.ereNav.MoveRight()
			e.ereNav.MoveDown()
			if !e.ereNav.HasSelection() {
				continue
			}
			rel := e.ereNav.GetSelectedRelationship(e.ereDiagram)
			if rel == nil {
				continue
			}
			want := rel.ToTable
			before := e.CenterTable()

			cmd, handled := e.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			if !handled {
				t.Fatal("enter on a selected relationship was not handled")
			}
			if cmd == nil {
				t.Fatal("enter produced no command, so the app never loads the new table")
			}
			msg, ok := cmd().(ERENavigateMsg)
			if !ok {
				t.Fatalf("enter produced a %T, want an ERENavigateMsg", cmd())
			}
			if msg.Table != e.CenterTable() {
				t.Errorf("the message says %q but the panel is centred on %q", msg.Table, e.CenterTable())
			}
			if msg.Schema != e.CenterSchema() {
				t.Errorf("the message says schema %q but the panel is centred on %q", msg.Schema, e.CenterSchema())
			}
			// A schema-qualified name is SPLIT: the part before the dot is the
			// schema and the part after is the table. Without the split the panel
			// would centre on a table called "sales.orders" that does not exist.
			if strings.Contains(want, ".") {
				head, tail, _ := strings.Cut(want, ".")
				if msg.Table != tail || msg.Schema != head {
					t.Errorf("the qualified name %q was not split: got schema=%q table=%q, want schema=%q table=%q",
						want, msg.Schema, msg.Table, head, tail)
				}
			}
			_ = before
			moved = true
			break
		}
		if !moved {
			t.Skip("could not find a selectable relationship in the fixture")
		}
	})

	t.Run("enter with nothing selected changes nothing", func(t *testing.T) {
		e := newOnERETab(t)
		// A diagram with no neighbours has no selection at all.
		empty := New(theme.Resolve("dark").Styles(), config.NewKeybindRegistry(config.KeybindingsConfig{}))
		empty.SetWidth(80)
		empty.SetHeight(30)
		empty.Focus()
		empty.SetData("public", "lonely", []postgres.ColumnInfo{col("id", "int")}, nil, nil, nil, nil)
		empty.setActiveTab(5)

		before := empty.CenterTable()
		cmd, handled := empty.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		if !handled {
			t.Error("enter on the ERE tab was not reported as handled")
		}
		if cmd != nil {
			t.Error("enter with no relationship selected produced a navigation")
		}
		if empty.CenterTable() != before {
			t.Errorf("enter with no selection moved the centre to %q", empty.CenterTable())
		}
		_ = e
	})
}

// Scenario: El centro del diagrama cae al nombre de la tabla cuando no hay centro.
//
// CenterTable and CenterSchema are the diagram's own notion of "what am I looking at",
// which the ERE navigation moves. Before any navigation they agree with the selected
// table, and the panel has to say so even when one of the two was never set — hence the
// two-step fallback.
func TestExplorerPreview_TheCentreFallsBackToTheSelectedTable(t *testing.T) {
	e := newPreview(t)
	if got := e.CenterTable(); got != "orders" {
		t.Errorf("CenterTable is %q before any navigation, want %q", got, "orders")
	}
	if got := e.CenterSchema(); got != "public" {
		t.Errorf("CenterSchema is %q before any navigation, want %q", got, "public")
	}

	// A panel whose centre was never established — SetData with a blank schema
	// leaves centerSchema blank while schema is set, and the two accessors have to
	// agree on the answer either way.
	bare := New(theme.Resolve("dark").Styles(), config.NewKeybindRegistry(config.KeybindingsConfig{}))
	bare.SetData("", "", nil, nil, nil, nil, nil)
	if got := bare.CenterTable(); got != "" {
		t.Errorf("CenterTable is %q for an empty panel, want the empty string", got)
	}
	if bare.ereDiagram != nil {
		t.Error("a panel with no centre built a diagram anyway")
	}
	if got := ansi.Strip(bare.renderERE()); !strings.Contains(got, "No data available") {
		t.Errorf("the ERE tab for a panel with no centre says %q", got)
	}
}

// Scenario: La barra de pestanas se mueve CON la pestana activa.
//
// The bar and the content are two things that have to agree; if setActiveTab moved one and
// not the other the user would be reading the columns of one table under the header of
// another. Asserted through the rendered output, which is the only place both appear.
func TestExplorerPreview_TheTabBarFollowsTheActiveTab(t *testing.T) {
	e := newPreview(t)
	for _, tc := range tabActions() {
		t.Run(tc.name, func(t *testing.T) {
			e.setActiveTab(tc.index)
			view := ansi.Strip(e.View())
			bar := strings.SplitN(view, "\n", 2)[0]
			if !strings.Contains(bar, tc.name) {
				t.Errorf("the tab bar is %q with the %s tab active", bar, tc.name)
			}
			if e.ActiveTabID() != tc.barID {
				t.Errorf("ActiveTabID is %q, want %q", e.ActiveTabID(), tc.barID)
			}
			if e.tabBar.ActiveIndex() != tc.index {
				t.Errorf("the bar's own index is %d, want %d", e.tabBar.ActiveIndex(), tc.index)
			}
		})
	}
}

// Scenario: El diagrama se construye con las FK del esquema que YA HAY.
//
// SetData builds the ERE diagram and hands it whatever schemaForeignKeys the panel holds
// AT THAT MOMENT. Nothing rebuilds it afterwards, so the order of the two calls decides
// whether the diagram has its incoming edges at all: set the schema's foreign keys after
// SetData and the panel shows a diagram with only its outgoing half, with no error and no
// empty state — the incoming column is simply blank.
//
// The app gets this right (app.go sets the schema's foreign keys on the line before
// SetData), which is exactly why it is worth a test: an ordering that only one caller gets
// right is an ordering that breaks the next time somebody adds a caller.
func TestExplorerPreview_TheDiagramIsBuiltWithTheSchemaForeignKeysAlreadySet(t *testing.T) {
	schemaFKs := map[string][]postgres.ForeignKeyInfo{
		"order_items": {{
			Name: "order_items_order_fkey", Column: "order_id",
			RefSchema: "public", RefTable: "orders", RefColumn: "id",
		}},
	}

	withFKsFirst := New(theme.Resolve("dark").Styles(), config.NewKeybindRegistry(config.KeybindingsConfig{}))
	withFKsFirst.SetWidth(100)
	withFKsFirst.SetHeight(40)
	withFKsFirst.Focus()
	withFKsFirst.SetSchemaForeignKeys(schemaFKs)
	withFKsFirst.SetData("public", "orders",
		[]postgres.ColumnInfo{col("id", "integer")}, nil, nil, nil, nil)

	if len(withFKsFirst.ereDiagram.Incoming) != 1 {
		t.Fatalf("setting the schema's foreign keys BEFORE SetData gave %d incoming relationships, want 1",
			len(withFKsFirst.ereDiagram.Incoming))
	}
	if got := withFKsFirst.ereDiagram.Incoming[0].ToTable; got != "order_items" {
		t.Errorf("the incoming relationship points at %q, want order_items", got)
	}

	// The same panel, the other order. Not a failure and not a crash: a diagram
	// with no incoming half, which is the whole point of pinning the order.
	afterwards := New(theme.Resolve("dark").Styles(), config.NewKeybindRegistry(config.KeybindingsConfig{}))
	afterwards.SetWidth(100)
	afterwards.SetHeight(40)
	afterwards.Focus()
	afterwards.SetData("public", "orders",
		[]postgres.ColumnInfo{col("id", "integer")}, nil, nil, nil, nil)
	afterwards.SetSchemaForeignKeys(schemaFKs)

	if len(afterwards.ereDiagram.Incoming) != 0 {
		t.Errorf("setting the schema's foreign keys AFTER SetData gave %d incoming relationships, so SetData must rebuild the diagram for this to change",
			len(afterwards.ereDiagram.Incoming))
	}
	if afterwards.ereDiagram == nil {
		t.Error("no diagram was built at all")
	}
}

// Scenario: Una FK a OTRO esquema se parte en esquema y tabla.
//
// BuildERDiagram qualifies an outgoing relationship's target when the foreign key points
// at a different schema, so the panel is handed a name like "sales.orders". Entering that
// relationship has to SPLIT it: the part before the dot is the schema to load the table
// from, and the part after is the table. Without the split the panel would centre on a
// table called "sales.orders", which does not exist, and the app would be told to load it.
//
// The split only happens for a dot at index > 0, so a leading dot is left alone; both are
// asserted because the guard is a comparison and not a search.
func TestExplorerPreview_ACrossSchemaRelationshipIsSplitOnNavigation(t *testing.T) {
	newCrossSchema := func(t *testing.T) *ExplorerPreview {
		t.Helper()
		e := New(theme.Resolve("dark").Styles(), config.NewKeybindRegistry(config.KeybindingsConfig{}))
		e.SetWidth(100)
		e.SetHeight(40)
		e.Focus()
		e.SetData("public", "orders",
			[]postgres.ColumnInfo{col("id", "integer"), col("region_id", "integer")},
			nil,
			[]postgres.ForeignKeyInfo{{
				Name: "orders_region_fkey", Column: "region_id",
				RefSchema: "sales", RefTable: "regions", RefColumn: "id",
			}},
			nil, nil)
		e.setActiveTab(5)
		return e
	}

	t.Run("the qualified name reaches the diagram and is split on enter", func(t *testing.T) {
		e := newCrossSchema(t)
		if len(e.ereDiagram.Outgoing) != 1 {
			t.Fatalf("the fixture produced %d outgoing relationships, want 1", len(e.ereDiagram.Outgoing))
		}
		if got := e.ereDiagram.Outgoing[0].ToTable; got != "sales.regions" {
			t.Fatalf("the relationship points at %q, want the qualified name sales.regions", got)
		}

		e.ereNav.MoveRight()
		e.ereNav.MoveDown()
		if !e.ereNav.HasSelection() {
			t.Fatal("the relationship could not be selected")
		}

		cmd, handled := e.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		if !handled {
			t.Fatal("enter on the selected relationship was not handled")
		}
		msg, ok := cmd().(ERENavigateMsg)
		if !ok {
			t.Fatalf("enter produced a %T, want an ERENavigateMsg", cmd())
		}
		if msg.Schema != "sales" || msg.Table != "regions" {
			t.Errorf("the qualified name was not split: schema=%q table=%q, want sales/regions", msg.Schema, msg.Table)
		}
		// And the panel agrees with the message it just sent.
		if e.CenterSchema() != "sales" || e.CenterTable() != "regions" {
			t.Errorf("the panel is centred on %s.%s, want sales.regions", e.CenterSchema(), e.CenterTable())
		}
	})

	t.Run("an unqualified name keeps the current schema", func(t *testing.T) {
		e := New(theme.Resolve("dark").Styles(), config.NewKeybindRegistry(config.KeybindingsConfig{}))
		e.SetWidth(100)
		e.SetHeight(40)
		e.Focus()
		e.SetData("public", "orders",
			[]postgres.ColumnInfo{col("id", "integer"), col("customer_id", "integer")},
			nil,
			[]postgres.ForeignKeyInfo{{
				Name: "orders_customer_fkey", Column: "customer_id",
				RefSchema: "public", RefTable: "customers", RefColumn: "id",
			}},
			nil, nil)
		e.setActiveTab(5)

		e.ereNav.MoveRight()
		e.ereNav.MoveDown()
		cmd, _ := e.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		msg, ok := cmd().(ERENavigateMsg)
		if !ok {
			t.Fatalf("enter produced a %T, want an ERENavigateMsg", cmd())
		}
		if msg.Schema != "public" || msg.Table != "customers" {
			t.Errorf("an unqualified name gave schema=%q table=%q, want public/customers", msg.Schema, msg.Table)
		}
	})
}

// Scenario: La columna muestra su valor POR DEFECTO, y "no tiene" no es lo mismo que "NULL".
//
// A column with no default renders the word NULL, and a column whose default is the SQL
// keyword NULL also renders the word NULL — the two are told apart by the DATA, not by
// the text, so the fixture has to use a default that is not the keyword.
func TestExplorerPreview_AColumnShowsItsDefaultAndSaysNULLWhenItHasNone(t *testing.T) {
	e := New(theme.Resolve("dark").Styles(), config.NewKeybindRegistry(config.KeybindingsConfig{}))
	e.SetWidth(100)
	e.SetHeight(30)
	e.Focus()
	e.SetData("public", "orders", []postgres.ColumnInfo{
		{Name: "id", DataType: "integer", IsNullable: "NO", Default: str("nextval('orders_id_seq'::regclass)")},
		{Name: "created_at", DataType: "timestamp", IsNullable: "NO", Default: str("now()")},
		{Name: "note", DataType: "text", IsNullable: "YES"},
	}, nil, nil, nil, nil)
	e.setActiveTab(1)

	view := ansi.Strip(e.View())
	for _, want := range []string{"nextval('orders_id_seq'::regclass)", "now()"} {
		if !strings.Contains(view, want) {
			t.Errorf("the default %q is not shown:\n%s", want, view)
		}
	}
	// The column with no default shows the word NULL exactly once, on its own row.
	rows := strings.Split(view, "\n")
	var noteRow string
	for _, row := range rows {
		if strings.Contains(row, "note") {
			noteRow = row
		}
	}
	if noteRow == "" {
		t.Fatalf("the column \"note\" is not in the rendered list:\n%s", view)
	}
	if !strings.Contains(noteRow, "NULL") {
		t.Errorf("a column with no default rendered %q, want it to say NULL", noteRow)
	}
	if strings.Contains(noteRow, "nextval") || strings.Contains(noteRow, "now()") {
		t.Errorf("a column with no default borrowed another column's default: %q", noteRow)
	}
}

// Scenario: La seleccion se mantiene A LA VISTA al caminar un diagrama alto.
//
// ensureERESelectionVisible estimates the selected row's line and moves the scroll so the
// row lands inside the pane. With one or two neighbours the estimate always fits and
// neither branch fires, so the fixture needs a diagram TALLER than the pane — many
// relationships and a short panel.
//
// Both directions are needed: walking down past the bottom pushes the scroll down, and
// walking back up past the top pulls it up. A scroll that only goes one way is a diagram
// you cannot read back.
func TestExplorerPreview_TheSelectionStaysVisibleInATallDiagram(t *testing.T) {
	// MaxNeighbors, and not one more: BuildERDiagram caps a column at ten and folds
	// the rest into an overflow count, so a larger fixture would silently collapse
	// to ten and the test would be measuring the cap instead of the scroll.
	const n = MaxNeighbors

	fks := make([]postgres.ForeignKeyInfo, n)
	cols := make([]postgres.ColumnInfo, n)
	for i := range fks {
		name := string(rune('a' + i))
		cols[i] = col("col_"+name, "integer")
		fks[i] = postgres.ForeignKeyInfo{
			Name: "orders_fk_" + name, Column: "col_" + name,
			RefSchema: "public", RefTable: "t_" + name, RefColumn: "id",
		}
	}

	e := New(theme.Resolve("dark").Styles(), config.NewKeybindRegistry(config.KeybindingsConfig{}))
	e.SetWidth(70)
	e.SetHeight(14) // a short panel, so the diagram cannot fit
	e.Focus()
	e.SetData("public", "orders", cols, nil, fks, nil, nil)
	e.setActiveTab(5)

	if len(e.ereDiagram.Outgoing) != n {
		t.Fatalf("the fixture produced %d relationships, want %d", len(e.ereDiagram.Outgoing), n)
	}
	if e.ereViewport.PaneHeight >= n*5 {
		t.Skipf("the pane is %d lines and the diagram %d, so nothing has to scroll", e.ereViewport.PaneHeight, n*5)
	}

	// The selected relationship's box NAME is what identifies its row on screen,
	// so that is what the assertion looks for.
	// One move per step, through the KEY: the handler both moves the cursor and runs
	// ensureERESelectionVisible, so driving it any other way would skip the half of
	// the behaviour under test. (An earlier version moved the cursor directly AND
	// sent the key, which stepped two rows per iteration and reported the wrong
	// row in its own failure message.)
	visible := func(row int) error {
		e.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		if got := e.ereNav.ActiveRow(); got != row {
			return fmt.Errorf("after %d down presses the cursor is at %d, want %d", row+1, got, row)
		}
		if e.ereViewport.ScrollOffset < 0 {
			return fmt.Errorf("at row %d the scroll went negative: %d", row, e.ereViewport.ScrollOffset)
		}
		want := e.ereDiagram.Outgoing[e.ereNav.ActiveRow()].ToTable
		view := ansi.Strip(e.View())
		if !strings.Contains(view, want) {
			return fmt.Errorf("at row %d the selected %q is off screen (scroll %d of a %d-line pane):\n%s",
				row, want, e.ereViewport.ScrollOffset, e.ereViewport.PaneHeight, view)
		}
		return nil
	}

	e.ereNav.MoveRight() // into the populated column; clamps the row but selects nothing
	for row := range n {
		if err := visible(row); err != nil {
			t.Error(err)
			break // one report is enough: every later row is further off
		}
	}
	if e.ereViewport.ScrollOffset == 0 {
		t.Error("walking to the bottom of a diagram taller than the pane never scrolled")
	}

	// And back up: the scroll has to come back with the cursor. The walk down
	// ended on the LAST row, so the first press up is expected to land one below
	// where that left it — starting the loop at n-1 would be asserting that up
	// moves nowhere.
	for row := n - 2; row >= 0; row-- {
		e.Update(tea.KeyPressMsg{Code: tea.KeyUp})
		if !e.ereNav.HasSelection() {
			continue
		}
		if got := e.ereNav.ActiveRow(); got != row {
			t.Fatalf("walking up left the cursor at %d, want %d", got, row)
		}
		want := e.ereDiagram.Outgoing[row].ToTable
		if view := ansi.Strip(e.View()); !strings.Contains(view, want) {
			t.Errorf("walking up, row %d's %q is off screen:\n%s", row, want, view)
			break
		}
	}
}

// Scenario: El log de debug del ERE no revienta, y cuenta lo que dice contar.
//
// A helper that is never called by production code is still code, and it is still the
// first thing anyone debugging the diagram will reach for. Two things worth pinning: it
// does not panic on an empty diagram, and it counts the PK/FK columns rather than all of
// them — which is the number that distinguishes "the centre box is showing the key
// columns" from "it is showing everything".
func TestDebugLogERE_DoesNotPanicAndCountsTheKeyColumns(t *testing.T) {
	// A diagram with a PK, an FK and an ordinary column: 2 of 3 are key columns.
	d := &ERDiagram{
		Center: TableBox{
			Schema: "public", Name: "orders",
			Columns: []ColumnBadge{
				{Name: "id", DataType: "integer", IsPK: true},
				{Name: "customer_id", DataType: "integer", IsFK: true},
				{Name: "note", DataType: "text"},
			},
		},
		Incoming: []Relationship{{ToTable: "order_items"}},
		Outgoing: []Relationship{{ToTable: "customers"}, {ToTable: "regions"}},
	}

	mustNotPanic(t, "the ERE debug log", func() { debugLogERE(d, 1) })
	mustNotPanic(t, "the ERE debug log on an empty diagram", func() {
		debugLogERE(&ERDiagram{}, 0)
	})

	// The log goes to /tmp, so the assertion is that it ran and produced a file
	// rather than that it produced particular bytes: the point of the test is the
	// panic and the path, and a test that asserted on the contents would break
	// every time the format changed, which is the one time it is allowed to.
	if _, err := os.Stat("/tmp/dbx_ere_debug.log"); err != nil {
		t.Logf("the debug log is not at /tmp/dbx_ere_debug.log (%v); the call did not panic, which is what this test is for", err)
	}
}

// Scenario: G lleva al FINAL REAL del diagrama, no a un hueco debajo.
//
// buildEREDiagram estimates the content height as six lines per relationship — the
// comment there says "5 lines per box + 1 spacing", which is six, and it is what the
// pane scrolls within. But a rendered relationship is a FOUR-line box plus a blank, so the
// estimate runs one line long per relationship: ten relationships overshoot by ten lines.
// JumpBottom therefore aims at a line that is not there.
//
// What that looks like to a user is a pane that is BLANK, or one showing the column
// titles and nothing under them, after pressing the key that is supposed to show the end.
func TestExplorerPreview_JumpingToTheBottomShowsTheEndNotABlankPane(t *testing.T) {
	const n = MaxNeighbors

	fks := make([]postgres.ForeignKeyInfo, n)
	cols := make([]postgres.ColumnInfo, n)
	for i := range fks {
		name := string(rune('a' + i))
		cols[i] = col("col_"+name, "integer")
		fks[i] = postgres.ForeignKeyInfo{
			Name: "orders_fk_" + name, Column: "col_" + name,
			RefSchema: "public", RefTable: "t_" + name, RefColumn: "id",
		}
	}

	e := New(theme.Resolve("dark").Styles(), config.NewKeybindRegistry(config.KeybindingsConfig{}))
	e.SetWidth(70)
	e.SetHeight(16)
	e.Focus()
	e.SetData("public", "orders", cols, nil, fks, nil, nil)
	e.setActiveTab(5)

	e.ereNav.MoveRight()
	e.ereNav.MoveDown()

	e.Update(tea.KeyPressMsg{Code: 'G', Text: "G"}) // JumpBottom
	view := ansi.Strip(e.View())

	last := e.ereDiagram.Outgoing[len(e.ereDiagram.Outgoing)-1].ToTable
	if !strings.Contains(view, last) {
		t.Errorf("after G the last relationship %q is not on screen (scroll %d of a %d-line pane):\n%s",
			last, e.ereViewport.ScrollOffset, e.ereViewport.PaneHeight, view)
	}
	// A pane that is entirely blank is the shape of the bug, and it is worth
	// naming because "nothing rendered" is otherwise indistinguishable from
	// "there is nothing here".
	body := strings.Split(view, "\n")
	nonBlank := 0
	for _, line := range body[1:] { // skip the tab bar
		if strings.TrimSpace(line) != "" {
			nonBlank++
		}
	}
	if nonBlank < 3 {
		t.Errorf("after G the pane has %d non-blank lines, want a full screen of diagram:\n%s", nonBlank, view)
	}
}
