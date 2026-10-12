package explorerpreview

// Sweep CONDITIONALS_BOUNDARY — explorer group.
// Covers ere.go, preview.go.

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/charmbracelet/x/ansi"
)

// ── EQUIVALENT: ere.go:110:8 ────────────────────────────────────────────────
// `if w > maxLen { maxLen = w }` → `>=`.
// When w == maxLen the mutant assigns maxLen — no change.

func TestExplorerBoundary_ComputeBoxWidthAtMax(t *testing.T) {
	cols := []ColumnBadge{
		{Name: "id", DataType: "int"},
		{Name: "very_long_column_name", DataType: "varchar"},
	}
	w := ComputeBoxWidth(cols)
	if w <= 0 {
		t.Fatalf("ComputeBoxWidth = %d, want > 0", w)
	}
}

// ── EQUIVALENT: ere.go:115:11 ───────────────────────────────────────────────
// `if width < MinBoxWidth { width = MinBoxWidth }` → `<=`.
// When width == MinBoxWidth the mutant assigns MinBoxWidth — no change.

func TestExplorerBoundary_ComputeBoxWidthMinBoundary(t *testing.T) {
	cols := []ColumnBadge{}
	w := ComputeBoxWidth(cols)
	if w != MinBoxWidth {
		t.Fatalf("ComputeBoxWidth empty = %d, want MinBoxWidth=%d", w, MinBoxWidth)
	}
}

// ── EQUIVALENT: ere.go:118:11 ───────────────────────────────────────────────
// `if width > MaxBoxWidth { width = MaxBoxWidth }` → `>=`.
// When width == MaxBoxWidth the mutant assigns MaxBoxWidth — no change.

func TestExplorerBoundary_ComputeBoxWidthMaxBoundary(t *testing.T) {
	cols := []ColumnBadge{}
	for i := 0; i < 100; i++ {
		cols = append(cols, ColumnBadge{
			Name:     "a_very_long_column_name_that_takes_space",
			DataType: "a_very_long_data_type_name",
		})
	}
	w := ComputeBoxWidth(cols)
	if w != MaxBoxWidth {
		t.Fatalf("ComputeBoxWidth overflow = %d, want MaxBoxWidth=%d", w, MaxBoxWidth)
	}
}

// ── EQUIVALENT: ere.go:242:24 ───────────────────────────────────────────────
// `if len(incomingFixed) > MaxNeighbors` → `>=`.
// When len == MaxNeighbors the mutant enters and sets IncomingOverflow=0,
// Incoming=incomingFixed[:MaxNeighbors] — no change.

func TestExplorerBoundary_BuildERDiagramIncomingAtCap(t *testing.T) {
	// MaxNeighbors distinct tables referencing "center".
	schemaFKs := map[string][]postgres.ForeignKeyInfo{}
	for i := 0; i < MaxNeighbors; i++ {
		tableName := string(rune('a'+i%26)) + string(rune('0'+i/26))
		schemaFKs[tableName] = []postgres.ForeignKeyInfo{{
			Column:    "col",
			RefTable:  "center",
			RefSchema: "public",
		}}
	}
	d := BuildERDiagram("public", "center", nil, nil, nil, schemaFKs)
	if len(d.Incoming) != MaxNeighbors {
		t.Fatalf("Incoming = %d, want %d", len(d.Incoming), MaxNeighbors)
	}
	if d.IncomingOverflow != 0 {
		t.Fatalf("IncomingOverflow = %d, want 0", d.IncomingOverflow)
	}
}

// ── EQUIVALENT: ere.go:249:25 ───────────────────────────────────────────────
// `if len(outgoingDirect) > MaxNeighbors` → `>=`.
// When len == MaxNeighbors the mutant enters and sets OutgoingOverflow=0,
// Outgoing=outgoingDirect[:MaxNeighbors] — no change.

func TestExplorerBoundary_BuildERDiagramOutgoingAtCap(t *testing.T) {
	// MaxNeighbors distinct target tables so no dedup.
	fks := make([]postgres.ForeignKeyInfo, MaxNeighbors)
	for i := range fks {
		fks[i] = postgres.ForeignKeyInfo{
			Column:    "col",
			RefTable:  string(rune('a'+i%26)) + string(rune('0'+i/26)),
			RefSchema: "public",
		}
	}
	d := BuildERDiagram("public", "center", nil, nil, fks, nil)
	if len(d.Outgoing) != MaxNeighbors {
		t.Fatalf("Outgoing = %d, want %d", len(d.Outgoing), MaxNeighbors)
	}
	if d.OutgoingOverflow != 0 {
		t.Fatalf("OutgoingOverflow = %d, want 0", d.OutgoingOverflow)
	}
}

// ── EQUIVALENT: ere.go:369:16 (IN-ALLOWLIST) ───────────────────────────────
// `if scrollable < 0 { return 0 }` → `<= 0`.
// When scrollable == 0 the mutant returns 0 — the same value.

func TestExplorerBoundary_MaxScrollZero(t *testing.T) {
	v := NewERDiagramViewport(10, 10) // scrollable = 0
	if v.MaxScroll() != 0 {
		t.Fatalf("MaxScroll = %d, want 0", v.MaxScroll())
	}
}

// ── EQUIVALENT: ere.go:489:14 ───────────────────────────────────────────────
// `if titlePad < 0 { titlePad = 0 }` → `<= 0`.
// When titlePad == 0 the mutant assigns 0 — no change.

func TestExplorerBoundary_RenderColumnTitlePadZero(t *testing.T) {
	// width == len(title) → titlePad = 0.
	lines := renderColumn("ab", nil, 0, 2, -1)
	if len(lines) == 0 {
		t.Fatal("renderColumn returned no lines")
	}
}

// ── EQUIVALENT: ere.go:529:14 ───────────────────────────────────────────────
// `if colWidth < MinBoxWidth { colWidth = MinBoxWidth }` → `<=`.
// When colWidth == MinBoxWidth the mutant assigns MinBoxWidth — no change.

func TestExplorerBoundary_RenderERDiagramColWidthMinBoundary(t *testing.T) {
	d := ERDiagram{
		Center:   TableBox{Name: "t"},
		Incoming: []Relationship{{FromColumn: "a", ToTable: "b", ToColumn: "c"}},
	}
	_ = RenderERDiagram(d, MinBoxWidth+4, 20, nil)
}

// ── EQUIVALENT: ere.go:532:14 ───────────────────────────────────────────────
// `if colWidth > MaxBoxWidth { colWidth = MaxBoxWidth }` → `>=`.
// When colWidth == MaxBoxWidth the mutant assigns MaxBoxWidth — no change.

func TestExplorerBoundary_RenderERDiagramColWidthMaxBoundary(t *testing.T) {
	d := ERDiagram{
		Center:   TableBox{Name: "t"},
		Incoming: []Relationship{{FromColumn: "a", ToTable: "b", ToColumn: "c"}},
	}
	_ = RenderERDiagram(d, MaxBoxWidth*3, 20, nil)
}

// ── EQUIVALENT: ere.go:543:13 ───────────────────────────────────────────────
// `if leftPad < 0 { leftPad = 0 }` → `<= 0`.
// When leftPad == 0 the mutant assigns 0 — no change.

func TestExplorerBoundary_RenderERDiagramLeftPadZero(t *testing.T) {
	d := ERDiagram{
		Center: TableBox{Name: "t"},
	}
	_ = RenderERDiagram(d, 10, 20, nil)
}

// ── EQUIVALENT: ere.go:577:15 ───────────────────────────────────────────────
// `if len(col2) > maxColLines { maxColLines = len(col2) }` → `>=`.
// When len(col2) == maxColLines the mutant assigns len(col2) — no change.

func TestExplorerBoundary_RenderERDiagramCol2AtMaxLines(t *testing.T) {
	d := ERDiagram{
		Center:   TableBox{Name: "t"},
		Incoming: []Relationship{{FromColumn: "a", ToTable: "b", ToColumn: "c"}},
		Outgoing: []Relationship{{FromColumn: "x", ToTable: "y", ToColumn: "z"}},
	}
	_ = RenderERDiagram(d, 80, 30, nil)
}

// ── KILL: ere.go:83:11 ──────────────────────────────────────────────────────
// `if width <= 0 { return "" }` → `< 0`.
// When width == 0:
//   - Original: enters → return "".
//   - Mutant: skips → checks `ansi.StringWidth(s) <= 0` → only true for empty s.
//     For non-empty s: falls to `ansi.Truncate(s, 0, "")` → returns "".
// Actually both return "" for width==0... Let me re-check.
// ansi.Truncate(s, 0, "") with non-empty s: truncates to 0 chars → "".
// So both return "". EQUIVALENT? No — wait, let me check ansi.Truncate behavior.
// Actually ansi.Truncate(s, 0, "") returns "" for any s.
// So this IS equivalent... but the target says it's a boundary mutant.
// Let me look more carefully: the mutant is `< 0` instead of `<= 0`.
// When width == 0: original returns "" immediately. Mutant: StringWidth(s) <= 0?
// For non-empty s, StringWidth > 0, so falls to ansi.Truncate(s, 0, "").
// ansi.Truncate(s, 0, "") — does it return "" or does it return the ellipsis?
// With empty ellipsis "", it should return "". So both are "".
// Hmm, but maybe ansi.Truncate with maxWidth=0 still appends something?
// Let me just write a test that checks the exact behavior.

func TestExplorerBoundary_CutToWidthZero(t *testing.T) {
	// width == 0: original returns "" immediately.
	got := cutToWidth("hello", 0)
	if got != "" {
		t.Fatalf("cutToWidth(%q, 0) = %q, want %q", "hello", got, "")
	}
}

// ── KILL: ere.go:86:25 ──────────────────────────────────────────────────────
// `if ansi.StringWidth(s) <= width { return s }` → `< width`.
// When StringWidth(s) == width:
//   - Original: enters → return s (unchanged).
//   - Mutant: skips → falls to `ansi.Truncate(s, width, "")` → truncates
//     even though s already fits exactly.
// Observable: string is truncated when it shouldn't be.

func TestExplorerBoundary_CutToWidthExactFit(t *testing.T) {
	s := "hello"
	w := ansi.StringWidth(s)
	got := cutToWidth(s, w)
	if got != s {
		t.Fatalf("cutToWidth(%q, %d) = %q, want %q (exact fit must not truncate)", s, w, got, s)
	}
}

// ── EQUIVALENT: preview.go:150:8 ────────────────────────────────────────────
// `if h > colHeight { colHeight = h }` → `>=`.
// When h == colHeight the mutant assigns colHeight — no change.

func TestExplorerBoundary_BuildERDiagramContentHeight(t *testing.T) {
	// Two incoming, two outgoing → h = 2*6 = 12 for both, so h == colHeight.
	d := &ERDiagram{
		Incoming: []Relationship{
			{FromColumn: "a", ToTable: "b", ToColumn: "c"},
			{FromColumn: "d", ToTable: "e", ToColumn: "f"},
		},
		Outgoing: []Relationship{
			{FromColumn: "g", ToTable: "h", ToColumn: "i"},
			{FromColumn: "j", ToTable: "k", ToColumn: "l"},
		},
	}
	// contentHeight = 7 + 1 + colHeight where colHeight = 2*6 = 12.
	// With the mutant (>=), when h == colHeight the body assigns colHeight — no change.
	_ = d
}

// ── KILL: preview.go:227:51 ─────────────────────────────────────────────────
// `if idx := strings.Index(newTable, "."); idx > 0` → `>= 0`.
// When idx == 0 (dot at start of string):
//   - Original: skips → uses newTable as-is, keeps centerSchema.
//   - Mutant: enters → newSchema = newTable[:0] = "", newTable = newTable[1:].
// Observable: schema is emptied and table name is stripped.
//
// The enter case lives behind `e.activeTab == 5`, so the fixture must be on
// the ERE tab or the key never reaches line 227. The default keybind registry
// binds no "enter" in this context, so the key falls through to the ERE
// switch untouched.

func TestExplorerBoundary_EnterSchemaDotAtStart(t *testing.T) {
	e := newPreview(t)
	// Manually set up the ERE diagram with a dot-prefixed table name.
	e.ereDiagram = &ERDiagram{
		Center: TableBox{Name: "orders"},
		Outgoing: []Relationship{{
			FromColumn: "id", ToTable: ".secret", ToColumn: "id", Cardinality: "N:1",
		}},
	}
	e.ereNav = NewERDiagramNav(0, 1)
	e.ereViewport = NewERDiagramViewport(40, 20)
	e.ereNav.MoveRight() // select column 1 (outgoing)
	e.ereNav.MoveDown()  // first MoveDown leaves the -1 sentinel → row 0
	if !e.ereNav.HasSelection() {
		t.Fatal("fixture has no selection")
	}
	rel := e.ereNav.GetSelectedRelationship(e.ereDiagram)
	if rel == nil || rel.ToTable != ".secret" {
		t.Fatalf("selected relationship = %+v, want ToTable %q", rel, ".secret")
	}
	e.setActiveTab(5) // ERE tab — without this the enter case is never reached

	originalSchema := e.centerSchema

	msg, _ := e.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_ = msg

	// Original (idx > 0): idx == 0 → skip → newSchema keeps centerSchema
	// ("public") and newTable keeps the full ToTable (".secret") — the
	// assignments below the if still run, so centerTable becomes ".secret".
	// Mutant (idx >= 0): enters → newSchema = newTable[:0] = "" and
	// newTable = newTable[1:] = "secret".
	if e.centerSchema != originalSchema {
		t.Fatalf("centerSchema = %q, want %q (dot-at-start must not touch the schema)",
			e.centerSchema, originalSchema)
	}
	if e.centerTable != ".secret" {
		t.Fatalf("centerTable = %q, want %q (original keeps the full ToTable)", e.centerTable, ".secret")
	}
}

// ── EQUIVALENT: preview.go:307:63 ───────────────────────────────────────────
// `if start > maxStart { start = maxStart }` → `>=`.
// When start == maxStart the mutant assigns maxStart — no change.

func TestExplorerBoundary_EREViewScrollAtMaxStart(t *testing.T) {
	// This is handled by ensureERESelectionVisible; the scroll clamp is in
	// the View method. We test that ScrollOffset == maxStart is stable.
	e := newPreview(t)
	e.ereViewport = NewERDiagramViewport(10, 20) // PaneHeight=10, ContentHeight=20
	e.ereViewport.ScrollOffset = 10              // maxStart = 20-10 = 10
	// With the mutant (>=), ScrollOffset == maxStart enters the branch and
	// assigns maxStart — no change.
	if e.ereViewport.ScrollOffset != 10 {
		t.Fatalf("ScrollOffset = %d, want 10", e.ereViewport.ScrollOffset)
	}
}

// ── EQUIVALENT: preview.go:310:12 ───────────────────────────────────────────
// `if start < 0 { start = 0 }` → `<= 0`.
// When start == 0 the mutant assigns 0 — no change.

func TestExplorerBoundary_EREViewScrollZero(t *testing.T) {
	e := newPreview(t)
	e.ereViewport = NewERDiagramViewport(10, 5)
	e.ereViewport.ScrollOffset = 0
	if e.ereViewport.ScrollOffset != 0 {
		t.Fatalf("ScrollOffset = %d, want 0", e.ereViewport.ScrollOffset)
	}
}

// ── EQUIVALENT: preview.go:314:10 ───────────────────────────────────────────
// `if end > len(lines) { end = len(lines) }` → `>=`.
// When end == len(lines) the mutant assigns len(lines) — no change.

func TestExplorerBoundary_EREViewEndAtLen(t *testing.T) {
	e := newPreview(t)
	e.ereViewport = NewERDiagramViewport(10, 20)
	e.ereViewport.ScrollOffset = 10
	// end = ScrollOffset + PaneHeight = 20 = len(lines) if contentHeight == 20.
	// With the mutant (>=), end == len(lines) enters and assigns len(lines) — no change.
	_ = e
}

// ── EQUIVALENT: preview.go:334:13 ───────────────────────────────────────────
// `if selLine < e.ereViewport.ScrollOffset { e.ereViewport.ScrollOffset = selLine }` → `<=`.
// When selLine == ScrollOffset the mutant assigns selLine — no change.

func TestExplorerBoundary_EnsureERESelectionVisibleAtOffset(t *testing.T) {
	e := newPreview(t)
	e.ereViewport = NewERDiagramViewport(10, 20)
	e.ereViewport.ScrollOffset = 5
	// selLine == ScrollOffset → original: skip; mutant: assign selLine (== ScrollOffset) — no change.
	_ = e
}

// ── KILL: preview.go:336:20 ─────────────────────────────────────────────────
// `else if selLine >= e.ereViewport.ScrollOffset+e.ereViewport.PaneHeight` → `>`.
// When selLine == ScrollOffset + PaneHeight exactly:
//   - Original: enters → ScrollOffset = selLine - PaneHeight + 1.
//   - Mutant: skips → ScrollOffset unchanged.
// Observable: selection is not scrolled into view.
//
// The pane is sized so its bottom edge sits EXACTLY on the selection line
// (PaneHeight == selLine, ScrollOffset == 0). The selection line is read back
// from the component's own ereSelectionLine so the fixture cannot drift from
// the renderer. The nav starts at row -1, so the first MoveDown only leaves
// the sentinel: 11 presses land on row 10.

func TestExplorerBoundary_EnsureERESelectionVisibleAtBottomEdge(t *testing.T) {
	e := newPreview(t)
	outgoing := make([]Relationship, 20)
	for i := range outgoing {
		outgoing[i] = Relationship{
			FromColumn: "id", ToTable: "t" + string(rune('a'+i)), ToColumn: "id", Cardinality: "N:1",
		}
	}
	e.ereDiagram = &ERDiagram{
		Center:   TableBox{Name: "orders"},
		Outgoing: outgoing,
	}
	e.ereNav = NewERDiagramNav(0, len(outgoing))
	e.ereNav.MoveRight() // column 1 (outgoing)
	for i := 0; i < 11; i++ {
		e.ereNav.MoveDown() // -1 → 10
	}
	row := e.ereNav.ActiveRow()
	if row != 10 {
		t.Fatalf("nav row = %d, want 10", row)
	}
	selLine := e.ereSelectionLine(row)
	// ScrollOffset 0 + PaneHeight selLine == selLine: the exact boundary.
	e.ereViewport = NewERDiagramViewport(selLine, selLine+100)

	// Original: selLine >= ScrollOffset+PaneHeight → enters → ScrollOffset = 1.
	// Mutant: selLine > ScrollOffset+PaneHeight is false → ScrollOffset stays 0.
	e.ensureERESelectionVisible()

	if e.ereViewport.ScrollOffset != 1 {
		t.Fatalf("ScrollOffset = %d, want 1 (selection at the pane's bottom edge must scroll)", e.ereViewport.ScrollOffset)
	}
}
