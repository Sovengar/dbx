// The contracts of the ERE renderer, asserted as properties over matrices of widths
// and fixtures rather than one case at a time.
//
// ere_test.go walks the same code a scenario at a time. What is here is what actually
// kills mutants: the invariants hold across a whole matrix of widths, so a change to a
// clamp, a pad or a trim count cannot pass. A tour that renders one diagram at one
// pane size is blind to every one of them.
//
// The load-bearing properties, in one place:
//
//	I1  every line a box renders is exactly `width` display cells wide
//	I2  content whose display width equals the inner width is emitted verbatim;
//	    only inner+1 or more loses a character
//	I3  the two neighbour columns plus their 2-space gutter consume the pane, and
//	    the centre box is horizontally centred within it
//
// Two facts about the code that shaped the assertions, both verified rather than
// assumed:
//
//	`width <= 2` is genuinely broken — width 1 panics on a negative Repeat count and
//	width 2 emits a 3-wide row. `ComputeBoxWidth` floors at MinBoxWidth (20) so it is
//	unreachable in production, and the matrices below start at 6 deliberately. It is
//	left alone rather than "fixed", because a fix would be an untested change to
//	unreachable code.
//
//	Not every line is `width` wide. `renderColumn`'s spacer between boxes is the empty
//	string and its overflow trailer is unpadded, and the merged rows shrink to a bare
//	gutter once the shorter column runs out. A global "all lines are width W" is
//	therefore a false invariant and the width assertions are scoped to the lines that
//	are supposed to be.
package explorerpreview

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/buble/dbx/internal/drivers/postgres"
)

// --- helpers ----------------------------------------------------------------

// rel is one neighbour relationship, named so a failure says which one.
func rel(from, to string) Relationship {
	return Relationship{FromColumn: from, ToTable: to, ToColumn: "id", Cardinality: "N:1"}
}

// checkWidth asserts the box-alignment property over one rendered box. A blank line is
// skipped: renderColumn's own spacer is deliberately the empty string.
func checkWidth(t *testing.T, label, box string, width int) {
	t.Helper()
	for i, line := range strings.Split(box, "\n") {
		if line == "" {
			continue
		}
		if got := ansi.StringWidth(line); got != width {
			t.Errorf("%s: line %d is %d cells wide, want %d:\n%s", label, i, got, width, box)
		}
	}
}

// named returns an ASCII string of n characters, so display width and byte length
// agree and a fixture cannot be defeated by a multi-byte character.
func named(n int) string { return strings.Repeat("x", n) }

// truncateForLog keeps a failure message short enough to read.
func truncateForLog(s string) string {
	if ansi.StringWidth(s) > 60 {
		return ansi.Truncate(s, 57, "…")
	}
	return s
}

// ---------------------------------------------------------------------------
// Truncation
// ---------------------------------------------------------------------------

// Scenario: El corte es EXCLUSIVO en el límite, y se mide por celdas.
//
// MaxTableName is 40 and the ellipsis is one cell, so the two sides of the boundary
// differ: 40 characters is returned untouched and 41 becomes 40 cells plus the
// ellipsis. The interesting case is 40, because a check written as "the result is at
// most 41 runes" is satisfied by the 41-character input too and therefore proves
// nothing about where the boundary is.
//
// `len` is bytes, so the fixtures are ASCII: the 41-character result is 43 bytes
// because the ellipsis is three, and a byte comparison would be asserting the wrong
// thing.
func TestTruncate_OnlyAnOversizeNameLosesACharacter(t *testing.T) {
	for _, tc := range []struct {
		name  string
		fn    func(string) string
		input int
		cap   int
	}{
		{"table name at the cap", TruncateTableName, 40, MaxTableName},
		{"table name one over", TruncateTableName, 41, MaxTableName},
		{"table name two over", TruncateTableName, 200, MaxTableName},
		{"column name at the cap", TruncateColumnName, 30, MaxColumnName},
		{"column name one over", TruncateColumnName, 31, MaxColumnName},
		{"column name two over", TruncateColumnName, 90, MaxColumnName},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.fn(named(tc.input))

			if tc.input <= tc.cap {
				// At or under the cap the name must come back exactly as it went
				// in. This is the assertion that kills the boundary mutant: an
				// inclusive comparison would elide one character here.
				if got != named(tc.input) {
					t.Errorf("a %d-character name came back as %d cells (%q), want it untouched", tc.input, ansi.StringWidth(got), truncateForLog(got))
				}
				if strings.Contains(got, "…") {
					t.Errorf("a %d-character name was truncated: %q", tc.input, truncateForLog(got))
				}
				return
			}

			// Over the cap: the name is cut to the cap and the ellipsis is
			// added, so the result is one cell WIDER than the cap.
			if !strings.HasSuffix(got, "…") {
				t.Errorf("a %d-character name came back without an ellipsis: %q", tc.input, truncateForLog(got))
			}
			if want := tc.cap + 1; ansi.StringWidth(got) != want {
				t.Errorf("a %d-character name came back as %d cells, want %d (the cap plus the ellipsis)", tc.input, ansi.StringWidth(got), want)
			}
			if want := named(tc.cap) + "…"; got != want {
				t.Errorf("the truncated name is %q, want %q", truncateForLog(got), want)
			}
		})
	}
}

// Scenario: Un nombre corto pasa intacto por las dos funciones.
//
// The functions are called on every table and column name, most of which are short.
// A name under the cap is returned as the same string, not a rebuilt one.
func TestTruncate_ShortNamesAreReturnedUnchanged(t *testing.T) {
	for _, name := range []string{"", "a", "id", "users", "order_items", "user_id"} {
		if got := TruncateTableName(name); got != name {
			t.Errorf("TruncateTableName(%q) = %q, want it unchanged", name, got)
		}
		if got := TruncateColumnName(name); got != name {
			t.Errorf("TruncateColumnName(%q) = %q, want it unchanged", name, got)
		}
	}
}

// ---------------------------------------------------------------------------
// ComputeBoxWidth
// ---------------------------------------------------------------------------

// Scenario: El ancho sale de la columna más larga, con sus cuatro de badge.
//
// Every column line is `badge + name + " " + datatype`, where the badge is "PK ",
// "FK " or three spaces — always three cells — plus a space before the type. So a
// column's own width is len(name)+len(type)+4, and the box has to fit the widest one.
// Getting that constant wrong shrinks or grows every box in the diagram.
func TestComputeBoxWidth_IsTheWidestColumnPlusFour(t *testing.T) {
	for _, tc := range []struct {
		name    string
		columns []ColumnBadge
		want    int
	}{
		{"no columns falls back to the minimum", nil, MinBoxWidth},
		{"one short column is floored", []ColumnBadge{{Name: "id", DataType: "int"}}, MinBoxWidth},
		{"the widest column wins", []ColumnBadge{
			{Name: "user_id", DataType: "integer"},
			{Name: "created_at", DataType: "timestamp"},
		}, len("created_at") + len("timestamp") + 4},
		// A short column AFTER a long one must not shrink the box: the loop takes
		// the maximum, and an ordering that took the last value would return the
		// short one.
		{"a short column after a long one does not shrink it", []ColumnBadge{
			{Name: "created_at", DataType: "timestamp"},
			{Name: "id", DataType: "int"},
		}, len("created_at") + len("timestamp") + 4},
		// The badge width is the same for PK, FK and neither — always three
		// cells — so a flagged column measures the same as an unflagged one and
		// only the badge's TEXT differs. The widest here is 3+4+4 == 11, which
		// is below the floor, so the floor is what comes back.
		{"the badge is three cells whichever flag is set", []ColumnBadge{
			{Name: "a", DataType: "b", IsPK: true},
			{Name: "cc", DataType: "dd", IsFK: true},
			{Name: "eee", DataType: "ffff"},
		}, MinBoxWidth},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ComputeBoxWidth(tc.columns); got != tc.want {
				t.Errorf("ComputeBoxWidth = %d, want %d", got, tc.want)
			}
		})
	}
}

// Scenario: El ancho se limita por arriba y por abajo.
//
// MinBoxWidth exists so a one-column box still shows a name rather than a sliver, and
// MaxBoxWidth so a pathological column does not take the whole pane. Both clamps are
// exercised with a value that lands exactly on the boundary, because that is the value
// a comparison written the other way round would treat differently.
func TestComputeBoxWidth_ClampsToTheAllowedRange(t *testing.T) {
	// Landing exactly on MaxBoxWidth: 46 + 4 == 50.
	onCap := []ColumnBadge{{Name: named(23), DataType: named(23)}}
	if got := ComputeBoxWidth(onCap); got != MaxBoxWidth {
		t.Errorf("a column measuring exactly %d produced width %d, want %d", MaxBoxWidth, got, MaxBoxWidth)
	}
	// One over, still capped.
	overCap := []ColumnBadge{{Name: named(30), DataType: named(30)}}
	if got := ComputeBoxWidth(overCap); got != MaxBoxWidth {
		t.Errorf("an oversized column produced width %d, want it capped at %d", got, MaxBoxWidth)
	}
	// A 19-wide computed width is the largest that still gets floored.
	underFloor := []ColumnBadge{{Name: named(6), DataType: named(9)}}
	if got := ComputeBoxWidth(underFloor); got != MinBoxWidth {
		t.Errorf("a 19-wide column produced width %d, want it floored at %d", got, MinBoxWidth)
	}
}

// ---------------------------------------------------------------------------
// renderCompactBox — the neighbour box
// ---------------------------------------------------------------------------

// Scenario: Toda caja de vecino tiene EXACTAMENTE el ancho pedido.
//
// The neighbour column is a grid of boxes sharing a gutter, so a box one cell short
// or one cell long is visible as a ragged edge rather than as a wrong glyph. This is
// the property that pins every padding calculation at once, and it is why the matrix
// crosses width with content that fits, fits exactly and overflows: a clamp that
// zeroes a pad only shows up on the overflowing fixtures, and an off-by-one in the
// pad only shows up on the ones that fit.
//
// The matrix starts at width 6 on purpose. Widths 1 and 2 are genuinely broken — 1
// panics on a negative Repeat count and 2 emits a three-wide row — and are
// unreachable because the column width is floored at MinBoxWidth.
func TestRenderCompactBox_EveryLineIsExactlyTheRequestedWidth(t *testing.T) {
	// 3 and 4 are included on purpose: they are the only widths where the two branches of
	// the truncation differ. At innerWidth 1 both emit a bare ellipsis, but at innerWidth 2 the
	// taken branch emits one character plus the ellipsis while the else emits only the
	// ellipsis — so a matrix starting at 6 cannot tell the branches apart at all.
	//
	// Widths 1 and 2 are NOT included: 1 panics on a negative Repeat count and 2 emits a
	// three-wide row. That is genuinely broken rather than merely untested, and it is
	// unreachable because the column width is floored at MinBoxWidth.
	for _, width := range []int{3, 4, 5, 6, 8, 20, 33, 50} {
		inner := width - 2

		// Content chosen relative to the inner width, so "fits", "fits exactly"
		// and "overflows" are all exercised at every width.
		short := max(inner/2, 1)
		fitsName := named(short)
		exactName := named(inner)
		overName := named(inner + 12)

		// "FK " plus this equals the inner width exactly. It goes empty below
		// inner 3 rather than going negative: named() with a negative count
		// panics inside strings.Repeat, and a panic in a fixture is a fixture
		// bug that reads like a production crash.
		fkExact := ""
		if inner >= 3 {
			fkExact = named(inner - 3)
		}
		fkOver := named(inner + 9)

		for _, tc := range []struct {
			name     string
			boxName  string
			fk       string
			junction bool
			selected bool
		}{
			{"short name and short fk", fitsName, "x", false, false},
			{"name filling the inner width exactly", exactName, "x", false, false},
			{"name wider than the inner width", overName, "x", false, false},
			{"short name, junction", fitsName, "x", true, false},
			{"short name, selected", fitsName, "x", false, true},
			{"short name, junction and selected", fitsName, "x", true, true},
			{"fk filling the inner width exactly", fitsName, fkExact, false, false},
			{"fk wider than the inner width", fitsName, fkOver, false, false},
			{"both over the inner width", overName, fkOver, true, true},
		} {
			t.Run(fmt.Sprintf("width %d, %s", width, tc.name), func(t *testing.T) {
				box := renderCompactBox(tc.boxName, tc.fk, width, tc.junction, tc.selected)
				checkWidth(t, fmt.Sprintf("width %d", width), box, width)
				// Four lines: two borders, the name and the fk column. A box
				// that grew or lost a row would break the column grid's rhythm,
				// and the source comment saying "3 lines" is counting content
				// only.
				if n := len(strings.Split(box, "\n")); n != 4 {
					t.Errorf("the box has %d lines, want 4 (two borders, a name and an fk):\n%s", n, box)
				}
			})
		}
	}
}

// Scenario: La caja de vecino dice exactamente qué lleva.
//
// A neighbour box is four facts in three lines: which table, that it is a junction,
// that it is the one under the cursor, and which local column points at it. Each has
// an exact rendering, and the ellipsis rule decides what survives when the table name
// does not fit.
//
// The name row is LEFT-aligned (the centre box centres its name) and the junction star
// is appended to the name, not to the fk line — the star marks the neighbour table, not
// the column. A selector that lands in the wrong column of the row is the kind of
// mistake that still renders as plausible output.
func TestRenderCompactBox_SaysExactlyWhatItCarries(t *testing.T) {
	// The expected rows are spelled as content plus the padding the contract
	// requires, rather than as whole hand-counted strings. `padded` builds a row
	// from its content and the box width, so the expectation states the rule — the
	// content, then padding out to the inner width — instead of a literal that has
	// to be re-counted by hand. Reading the width back out of the box under test
	// would be tautological; this does not.
	padded := func(content string, width int) string {
		inner := width - 2
		pad := inner - ansi.StringWidth(content)
		if pad < 0 {
			pad = 0
		}
		return "│" + content + strings.Repeat(" ", pad) + "│"
	}
	rule := func(n int) string { return strings.Repeat("─", n) }

	for _, tc := range []struct {
		name     string
		boxName  string
		fk       string
		width    int
		junction bool
		selected bool
		// nameRow is what the name line must contain; fkRow the fk line.
		nameRow string
		fkRow   string
	}{
		{
			name: "a plain neighbour", boxName: "users", fk: "user_id", width: 20,
			nameRow: "users", fkRow: "FK user_id",
		},
		{
			// The star is part of the name, before the padding, so the row
			// stays the box width.
			name: "a junction marks its name with a star", boxName: "order_items", fk: "order_id",
			width: 20, junction: true,
			nameRow: "order_items*", fkRow: "FK order_id",
		},
		{
			// The selector wraps the name: "► " and " ◄", then the padding.
			name: "the selected neighbour is bracketed", boxName: "users", fk: "user_id",
			width: 20, selected: true,
			nameRow: "► users ◄", fkRow: "FK user_id",
		},
		{
			name: "a junction and a selection both mark the name", boxName: "users", fk: "user_id",
			width: 20, junction: true, selected: true,
			nameRow: "► users* ◄", fkRow: "FK user_id",
		},
		{
			// 18 characters is the inner width at width 20, so this one is NOT
			// truncated: the condition is strictly greater than.
			name:    "a name exactly as wide as the inner width is not truncated",
			boxName: named(18), fk: "x", width: 20,
			nameRow: named(18), fkRow: "FK x",
		},
		{
			// One over: the last cell becomes the ellipsis.
			name:    "a name one over the inner width loses its last character",
			boxName: named(19), fk: "x", width: 20,
			nameRow: named(17) + "…", fkRow: "FK x",
		},
		{
			// The fk line has no ellipsis: it is cut to fit, exactly.
			name: "an over-wide fk column is cut to fit", boxName: "users", fk: named(40),
			width:   20,
			nameRow: "users", fkRow: "FK " + named(15),
		},
		{
			name: "an empty fk column keeps the badge", boxName: "users", fk: "", width: 20,
			nameRow: "users", fkRow: "FK ",
		},
		{
			// The selector is added BEFORE the truncation, so a long name loses
			// its tail INSIDE the selector rather than dropping the closing
			// " ◄" and leaving a bracket open.
			name:    "a selected over-wide name is truncated inside the selector",
			boxName: named(30), fk: "x", width: 20, selected: true,
			nameRow: "► " + named(13) + "…", fkRow: "FK x",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := strings.Split(renderCompactBox(tc.boxName, tc.fk, tc.width, tc.junction, tc.selected), "\n")
			want := []string{
				"┌" + rule(tc.width-2) + "┐",
				padded(tc.nameRow, tc.width),
				padded(tc.fkRow, tc.width),
				"└" + rule(tc.width-2) + "┘",
			}
			if len(got) != len(want) {
				t.Fatalf("the box has %d lines, want %d:\n%s", len(got), len(want), strings.Join(got, "\n"))
			}
			for i := range want {
				if got[i] != want[i] {
					t.Errorf("line %d is\n  %q\nwant\n  %q", i, got[i], want[i])
				}
			}
		})
	}
}

// Scenario: Un nombre larguísimo se corta ANTES de la caja, con el nombre entero.
//
// TruncateTableName caps at 40 with an ellipsis, so the box then truncates THAT
// result. Two truncations in sequence produce one ellipsis, not two, and the name that
// reaches the box is already shortened — which is why a 200-character name renders the
// same as a 41-character one.
func TestRenderCompactBox_ALongNameIsTruncatedToTheNameCapFirst(t *testing.T) {
	long := named(200)
	overCap := named(MaxTableName + 1)

	for _, name := range []string{long, overCap} {
		got := strings.Split(renderCompactBox(name, "x", 20, false, false), "\n")[1]
		want := named(5) + "…" + "            " // 40 + ellipsis is wider than 18, so it is cut again
		if got != "│"+named(17)+"…│" {
			t.Errorf("a %d-character name rendered as %q, want the double-truncated form %q", len(name), got, want)
		}
		// The point of the assertion is that the two truncations agree: the
		// first produced 41 cells, and the box cut that to 18 with one ellipsis.
		if n := strings.Count(got, "…"); n != 1 {
			t.Errorf("a %d-character name rendered %d ellipses, want 1: %q", len(name), n, got)
		}
	}
}

// ---------------------------------------------------------------------------
// renderBox — the centre table
// ---------------------------------------------------------------------------

// Scenario: La caja central tiene EXACTAMENTE el ancho pedido, y nombra la tabla.
//
// The centre box sits alone on its own row above the two neighbour columns, so a wrong
// width is visible as a box that does not line up with the column header below it. The
// name is CENTRED here, unlike the neighbour boxes, and the split is `pad/2` on the
// left and the remainder on the right — so an odd amount of padding puts the extra
// cell on the right, and that asymmetry is worth pinning.
//
// The centre box is never selected, so there is no selector row: the cursor belongs to
// a neighbour. The two dead parameters that used to carry one are gone, which is why
// this has fewer fixtures than the neighbour box and not more.
func TestRenderBox_EveryLineIsExactlyTheRequestedWidthAndTheNameIsCentred(t *testing.T) {
	// See TestRenderCompactBox_EveryLineIsExactlyTheRequestedWidth for why 3, 4 and 5
	// are here and 1 and 2 are not.
	for _, width := range []int{3, 4, 5, 6, 8, 20, 33, 50} {
		inner := width - 2
		// A name that fits with room to spare, so the pad is positive and the
		// split between the two sides is what is being measured. max(.., 1)
		// because named() with a negative count panics inside strings.Repeat,
		// and a fixture that panics reads like a production crash.
		fits := named(max(inner/2-1, 1))

		for _, tc := range []struct {
			name     string
			boxName  string
			columns  []ColumnBadge
			junction bool
		}{
			{"a name that fits", fits, nil, false},
			{"a name that fits, with a junction marker", fits, nil, true},
			{"a name that fits, with columns", fits, []ColumnBadge{{Name: "id", DataType: "integer", IsPK: true}}, false},
			{"a name that fits, with columns and a junction marker", fits, []ColumnBadge{{Name: "id", DataType: "integer", IsPK: true}}, true},
			// The name is truncated to the inner width and then padded, so the
			// row is still exactly `width` wide.
			{"a name wider than the box", named(inner + 8), nil, false},
			{"a name wider than the box, with columns", named(inner + 8), []ColumnBadge{{Name: "id", DataType: "integer", IsPK: true}}, false},
		} {
			t.Run(fmt.Sprintf("width %d, %s", width, tc.name), func(t *testing.T) {
				box := renderBox(tc.boxName, tc.columns, width, tc.junction)
				checkWidth(t, fmt.Sprintf("width %d", width), box, width)

				lines := strings.Split(box, "\n")
				nameRow := lines[1]
				inner0 := strings.TrimSuffix(strings.TrimPrefix(nameRow, "│"), "│")
				leftPad := len(inner0) - len(strings.TrimLeft(inner0, " "))
				rightPad := len(inner0) - len(strings.TrimRight(inner0, " "))
				body := strings.TrimSpace(inner0)
				if leftPad != rightPad && leftPad != rightPad-1 {
					t.Errorf("the name %q has %d cells of left padding and %d on the right; want them equal or the left one short by one, so the name reads centred",
						body, leftPad, rightPad)
				}
			})
		}
	}
}

// Scenario: La caja central dice qué columnas tiene y de qué clase son.
//
// A column line is `badge + name + " " + datatype`, and the badge is "PK ", "FK " or
// three spaces — always three cells, so every column line starts at the same offset
// and the datatype column lines up down the box. That fixed width is the whole reason
// the box is readable, and a badge that changed length would shift every line below it.
//
// PK wins over FK: a column can be both (it is part of a composite key and it
// references another table) and the badge has to be one thing.
func TestRenderBox_ColumnLinesCarryTheirBadgeNameAndType(t *testing.T) {
	box := strings.Split(renderBox("orders", []ColumnBadge{
		{Name: "id", DataType: "integer", IsPK: true},
		{Name: "user_id", DataType: "integer", IsFK: true},
		{Name: "created_at", DataType: "timestamp"},
		{Name: "total", DataType: "numeric", IsPK: true, IsFK: true},
	}, 20, false), "\n")

	// 3 border lines + 1 name line + 4 columns.
	if len(box) != 8 {
		t.Fatalf("the box has %d lines, want 8:\n%s", len(box), strings.Join(box, "\n"))
	}
	// The expected rows are content plus padding to the box's inner width, which
	// states the rule rather than re-counting spaces by hand. Deriving the width
	// from the box under test would be tautological; this does not.
	row := func(content string) string {
		const width = 20
		inner := width - 2
		pad := inner - ansi.StringWidth(content)
		if pad < 0 {
			pad = 0
		}
		return "│" + content + strings.Repeat(" ", pad) + "│"
	}
	want := []string{
		row("PK id integer"),
		row("FK user_id integer"),
		// The type is CUT rather than ellipsised, like the fk line of a
		// neighbour box: the 18 cells are "   created_at time", so
		// "timestamp" loses its final two characters with nothing to mark the
		// loss.
		row("   created_at time"),
		// PK wins over FK for a column that is both.
		row("PK total numeric"),
	}
	for i, w := range want {
		if got := box[3+i]; got != w {
			t.Errorf("column line %d is\n  %q\nwant\n  %q", i, got, w)
		}
	}

	// Every badge is exactly three cells, so the names all start at the same
	// offset — that is what lines the type column up. Only the column rows are
	// scanned, never the borders, which are shorter than the badge.
	for i, line := range box[3 : 3+len(want)] {
		inner := strings.TrimSuffix(strings.TrimPrefix(line, "│"), "│")
		if got := ansi.StringWidth(inner[:3]); got != 3 {
			t.Errorf("column line %d has a %d-cell badge prefix, want 3: %q", i, got, line)
		}
	}
}

// Scenario: Una caja central sin columnas conserva el divisor, aunque no haya nada debajo.
//
// The name is followed by the divider that separates it from the column list, whether or
// not there is a column list. With no columns the divider sits directly above the bottom
// border, which is a box with two horizontal rules and nothing between them — the reason
// this is worth asserting is that dropping the divider when the list is empty would look
// tidier and lose the visual "this is where the columns go".
func TestRenderBox_WithNoColumnsIsAFrameAroundTheName(t *testing.T) {
	box := strings.Split(renderBox("users", nil, 20, false), "\n")
	want := []string{
		"┌──────────────────┐",
		"│      users       │",
		"├──────────────────┤",
		"└──────────────────┘",
	}
	if len(box) != len(want) {
		t.Fatalf("the box has %d lines, want %d:\n%s", len(box), len(want), strings.Join(box, "\n"))
	}
	for i := range want {
		if box[i] != want[i] {
			t.Errorf("line %d is %q, want %q", i, box[i], want[i])
		}
	}
}

// Scenario: El asterisco de junction es su propia línea, debajo de las columnas.
//
// A junction table is drawn with a lone "  *" row after its columns, so the marker is
// below the data rather than next to the name. It is two spaces then the star, which
// puts it under the badge column rather than under the name.
func TestRenderBox_AJunctionMarkerIsItsOwnRowBelowTheColumns(t *testing.T) {
	withMarker := strings.Split(renderBox("order_items", []ColumnBadge{
		{Name: "order_id", DataType: "integer", IsFK: true},
		{Name: "product_id", DataType: "integer", IsFK: true},
	}, 20, true), "\n")
	without := strings.Split(renderBox("order_items", []ColumnBadge{
		{Name: "order_id", DataType: "integer", IsFK: true},
		{Name: "product_id", DataType: "integer", IsFK: true},
	}, 20, false), "\n")

	if len(withMarker) != len(without)+1 {
		t.Fatalf("the junction box has %d lines and the plain one %d, want exactly one more for the marker:\n%s\n---\n%s",
			len(withMarker), len(without), strings.Join(withMarker, "\n"), strings.Join(without, "\n"))
	}
	marker := withMarker[len(withMarker)-2]
	if want := "│  *               │"; marker != want {
		t.Errorf("the marker row is %q, want %q", marker, want)
	}
	// And the marker sits above the bottom border, not below it.
	if got := withMarker[len(withMarker)-1]; !strings.HasPrefix(got, "└") {
		t.Errorf("the last line is %q, want the bottom border", got)
	}
}

// ---------------------------------------------------------------------------
// renderColumn — title, boxes and the overflow trailer
// ---------------------------------------------------------------------------

// Scenario: Una columna es un título, cajas separadas por una línea vacía.
//
// The column is a title row followed by the boxes, with ONE empty line between
// consecutive boxes and none before the first. So the line count is a function of the
// relationship count: 1 for the title, 4 per box, 3 for the spacers between them.
//
// The spacers are what make a stack of boxes read as separate tables rather than one
// wall of borders, so their count is a contract and not an implementation detail.
func TestRenderColumn_LinesUpTitleBoxesAndSpacers(t *testing.T) {
	rels := []Relationship{
		rel("a_id", "alpha"), rel("b_id", "beta"), rel("c_id", "gamma"),
		rel("d_id", "delta"), rel("e_id", "epsilon"),
	}

	for _, n := range []int{1, 2, 3, 5} {
		t.Run(fmt.Sprintf("%d relationships", n), func(t *testing.T) {
			lines := renderColumn("1:N", rels[:n], 0, 24, -1)
			// title + n boxes of 4 lines + (n-1) spacers.
			want := 1 + n*4 + (n - 1)
			if len(lines) != want {
				t.Fatalf("the column has %d lines for %d relationships, want %d:\n%q",
					len(lines), n, want, lines)
			}
			// The title is padded to the full width, so it ends in spaces and
			// is the full column width.
			if got := ansi.StringWidth(lines[0]); got != 24 {
				t.Errorf("the title row is %d cells, want the full 24", got)
			}
			// The shape is: title, then box, spacer, box, spacer, box... So
			// the borders are at predictable offsets and there is NO spacer
			// before the first box.
			// The title, then box after box with exactly one blank line
			// BETWEEN them and none before the first. So a top border is at
			// line 1 for the first box and every 4 lines after that, and
			// anything else that is not a border or a spacer is a box's
			// interior.
			for i, line := range lines {
				switch {
				case i == 0:
					if strings.HasPrefix(line, "┌") {
						t.Errorf("line 0 is a box top, want the title:\n%q", lines)
					}
				case strings.HasPrefix(line, "┌"):
					// Only the FIRST box may sit directly under the title.
					// Every other one needs the spacer, and its absence is
					// what merges two boxes into one wall of borders.
					if i != 1 && lines[i-1] != "" {
						t.Errorf("the box at line %d has no spacer above it:\n%q", i, lines)
					}
				case strings.HasPrefix(line, "└"):
					if i < len(lines)-1 && lines[i+1] != "" && !strings.HasPrefix(lines[i+1], "┌") {
						t.Errorf("the box ending at line %d is followed by %q, want a spacer or another box:\n%q", i, lines[i+1], lines)
					}
				case line == "":
					// A spacer must sit between two boxes, never before the
					// first one.
					if i == 1 || !strings.HasPrefix(lines[i+1], "┌") {
						t.Errorf("line %d is a spacer that does not separate two boxes:\n%q", i, lines)
					}
				case strings.HasPrefix(line, "│"):
					// A box's interior. Nothing to check beyond the width,
					// which checkWidth covers.
				default:
					t.Errorf("line %d is neither a border, content nor a spacer: %q", i, line)
				}
			}
			// And every non-blank line is the column width.
			checkWidth(t, "column", strings.Join(lines, "\n"), 24)
		})
	}
}

// Scenario: Una columna sin relaciones es solo el título.
//
// Nothing to draw means a title row and nothing else. No box, no spacer, no trailer —
// so a column with no relationships is one line, and that is a different height from a
// column with one, which is what makes the merge below need a length check.
func TestRenderColumn_WithNoRelationshipsIsJustTheTitle(t *testing.T) {
	lines := renderColumn("1:N", nil, 0, 24, -1)
	if len(lines) != 1 {
		t.Fatalf("the column has %d lines, want only the title:\n%q", len(lines), lines)
	}
	if got := ansi.StringWidth(lines[0]); got != 24 {
		t.Errorf("the title row is %d cells, want the full 24", got)
	}
	if strings.Contains(lines[0], "┌") {
		t.Errorf("the title row contains a box border: %q", lines[0])
	}
}

// Scenario: El aviso de "+N more" aparece solo cuando hay relationships de más.
//
// A hub table has more neighbours than the diagram will draw, and the rest are
// reported rather than dropped: the user needs to know the diagram is not the whole
// truth, and "+N more" with the exact count is how they learn how much they are not
// seeing. Overflow of zero means no trailer at all — a "+0 more" would be a lie.
//
// The trailer is preceded by a blank line when there is something above it to separate
// it from, which is why an overflowing column with no relationships draws the trailer
// on its own without a stray blank first.
func TestRenderColumn_TheOverflowTrailerCountsWhatIsHidden(t *testing.T) {
	rels := []Relationship{rel("a_id", "alpha"), rel("b_id", "beta"), rel("c_id", "gamma")}

	for _, tc := range []struct {
		name      string
		rels      []Relationship
		overflow  int
		wantTrail string
	}{
		{"no overflow, no trailer", rels, 0, ""},
		{"one hidden", rels, 1, " +1 more"},
		{"several hidden", rels, 5, " +5 more"},
		// Overflow with nothing drawn is still reported: a column whose only
		// content is the trailer is a real state for a hub that had every
		// neighbour pushed out.
		{"overflow with no relationships drawn", nil, 10, " +10 more"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lines := renderColumn("1:N", tc.rels, tc.overflow, 24, -1)
			last := lines[len(lines)-1]

			if tc.overflow == 0 {
				for i, l := range lines {
					if strings.Contains(l, "more") {
						t.Errorf("line %d is a trailer with nothing hidden: %q", i, l)
					}
				}
				return
			}

			if last != tc.wantTrail {
				t.Errorf("the last line is %q, want %q", last, tc.wantTrail)
			}
			// The trailer is separated from the boxes above it by a blank
			// line, but only when there is a box above it.
			if len(tc.rels) > 0 && lines[len(lines)-2] != "" {
				t.Errorf("the line before the trailer is %q, want it blank so the trailer stands apart from the boxes", lines[len(lines)-2])
			}
			// And it is NOT padded to the column width — it is a count, not a
			// box, so it is short on purpose.
			if got := ansi.StringWidth(last); got == 24 {
				t.Errorf("the trailer is padded to the column width, so it reads as another box: %q", last)
			}
		})
	}
}

// Scenario: Solo la fila seleccionada lleva el cursor, y las demás no.
//
// selectedRow is an index into the relationships, and exactly that box is bracketed.
// The index is the position among the RELATIONSHIPS, not among the lines, which is the
// part a renderer can get wrong: line 4 is the first box's second line, and off-by-one
// there would highlight the wrong table or none at all.
//
// A selectedRow of -1 means no selection, which is what the merged render passes for
// the column the cursor is not in.
func TestRenderColumn_OnlyTheSelectedRowIsMarked(t *testing.T) {
	rels := []Relationship{rel("a_id", "alpha"), rel("b_id", "beta"), rel("c_id", "gamma")}

	for _, tc := range []struct {
		name        string
		selectedRow int
		wantMarked  string
	}{
		{"the first relationship", 0, "alpha"},
		{"the second relationship", 1, "beta"},
		{"the last relationship", 2, "gamma"},
		{"no selection highlights nothing", -1, ""},
		// An index past the end cannot highlight anything: the caller computes
		// it from a clamped cursor, and a stale index must not wrap around.
		{"an index past the end highlights nothing", 9, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lines := renderColumn("1:N", rels, 0, 24, tc.selectedRow)
			joined := strings.Join(lines, "\n")

			if tc.wantMarked == "" {
				if strings.Contains(joined, "►") || strings.Contains(joined, "◄") {
					t.Errorf("a row was highlighted that should not have been:\n%s", joined)
				}
				return
			}
			if !strings.Contains(joined, "► "+tc.wantMarked+" ◄") {
				t.Errorf("%s is not bracketed:\n%s", tc.wantMarked, joined)
			}
			// Exactly one box is bracketed, so the cursor cannot be on two.
			if n := strings.Count(joined, "►"); n != 1 {
				t.Errorf("%d boxes are marked, want exactly 1:\n%s", n, joined)
			}
			// And the OTHER relationships are untouched.
			for _, r := range rels {
				if r.ToTable == tc.wantMarked {
					continue
				}
				if strings.Contains(joined, "► "+r.ToTable+" ◄") {
					t.Errorf("%s is bracketed as well as %s", r.ToTable, tc.wantMarked)
				}
			}
		})
	}
}

// Scenario: El título se rellena al ancho, y uno más ancho que la columna no se corta.
//
// The title is padded to the column width so the header spans the grid. A title wider
// than the column is NOT truncated — the pad goes to zero and the title simply
// overflows, because a truncated "1:N" would render as "1" and stop meaning anything.
func TestRenderColumn_TheTitleIsPaddedToTheColumnWidth(t *testing.T) {
	for _, tc := range []struct {
		title    string
		width    int
		wantWide int
	}{
		{"1:N", 24, 24},
		{"N:1", 20, 20},
		{"a very long title indeed", 20, len("a very long title indeed")},
	} {
		lines := renderColumn(tc.title, nil, 0, tc.width, -1)
		if got := ansi.StringWidth(lines[0]); got != tc.wantWide {
			t.Errorf("the title %q in a %d-wide column is %d cells, want %d", tc.title, tc.width, got, tc.wantWide)
		}
		if !strings.HasPrefix(lines[0], tc.title) {
			t.Errorf("the title row is %q, want it to start with %q", lines[0], tc.title)
		}
	}
}

// ---------------------------------------------------------------------------
// ERDiagramNav — the two-column cursor
// ---------------------------------------------------------------------------

// Scenario: Mover abajo desde "sin selección" selecciona la fila 0, y no se pasa.
//
// The cursor starts with no selection (row -1), and the first MoveDown has to pick
// row 0 — which is a different rule from every subsequent one, because afterwards the
// cursor is already somewhere and MoveDown advances it by one. Getting the first
// press wrong is a cursor that starts one row in, or one that refuses to start.
//
// And it stops at the last row: a column with 2 neighbours has rows 0 and 1, and
// pressing down a third time must stay on 1 rather than running off the end.
func TestERDiagramNav_MoveDownSelectsThenStopsAtTheLastRow(t *testing.T) {
	for _, count := range []int{1, 2, 3, 8} {
		t.Run(fmt.Sprintf("a column of %d", count), func(t *testing.T) {
			nav := NewERDiagramNav(count, count)

			if nav.HasSelection() {
				t.Fatal("a fresh nav reports a selection")
			}
			if got := nav.ActiveRow(); got != -1 {
				t.Fatalf("a fresh nav is on row %d, want -1 for no selection", got)
			}

			nav.MoveDown()
			if !nav.HasSelection() {
				t.Fatal("the first MoveDown left the cursor unselected")
			}
			if got := nav.ActiveRow(); got != 0 {
				t.Errorf("the first MoveDown landed on row %d, want 0", got)
			}

			// Walk to the end and stay there.
			for range count * 2 {
				nav.MoveDown()
			}
			if got := nav.ActiveRow(); got != count-1 {
				t.Errorf("after overshooting, the cursor is on row %d, want it pinned at %d", got, count-1)
			}
		})
	}
}

// Scenario: Una columna vacía no se puede seleccionar, pero la otra sí.
//
// MoveDown on a column with no neighbours returns immediately: there is no row 0 to
// go to, and landing on one would leave the cursor pointing at nothing. The other
// column is unaffected, so a table with only incoming relationships can still be
// navigated.
func TestERDiagramNav_AnEmptyColumnCannotBeSelected(t *testing.T) {
	nav := NewERDiagramNav(0, 3) // no incoming, 3 outgoing

	nav.MoveDown() // in the empty column
	if nav.HasSelection() {
		t.Errorf("the cursor selected a row in an empty column: row %d", nav.ActiveRow())
	}
	if got := nav.ActiveRow(); got != -1 {
		t.Errorf("the cursor row is %d, want -1", got)
	}

	// Crossing to the populated column and pressing down works.
	nav.MoveRight()
	nav.MoveDown()
	if !nav.HasSelection() {
		t.Fatal("the cursor could not select in the populated column")
	}
	if got := nav.ActiveRow(); got != 0 {
		t.Errorf("the cursor is on row %d in the populated column, want 0", got)
	}
}

// Scenario: Moverse de columna ajusta la fila a la nueva columna.
//
// The two columns have different lengths in general, so a cursor on row 3 of a
// four-row column is not a valid position in a one-row column. Crossing over clamps it
// rather than leaving it out of range, which is what stops the highlight from appearing
// on a row that does not exist.
//
// KNOWN BEHAVIOUR inside this: crossing with NOTHING selected leaves it unselected
// rather than picking row 0. clampRow only clamps a row that is out of range, and -1 is
// not >= count, so it passes through. That is correct — moving across a diagram is not
// the same as pressing down on it, and a user who has not chosen a neighbour has not
// chosen one by looking at the other column.
func TestERDiagramNav_CrossingColumnsClampsTheRow(t *testing.T) {
	for _, tc := range []struct {
		name           string
		incoming       int
		outgoing       int
		rows           int
		wantAfterRight int
		wantAfterLeft  int
	}{
		// The cursor is placed in the INCOMING column and then crosses, so the
		// incoming count is the one being clamped FROM and the outgoing count
		// is what it has to fit into.
		{"a row valid in both columns is untouched", 5, 5, 3, 2, 2},
		{"the first row is untouched", 5, 5, 1, 0, 0},
		{"a row too far down becomes the LAST row", 5, 2, 4, 1, 1},
		{"one row too far becomes the last, in a one-row column", 5, 1, 4, 0, 0},
		{"crossing into an empty column unselects", 5, 0, 4, -1, -1},
		// Crossing with nothing selected stays unselected: -1 is not >= count,
		// so clampRow passes it through rather than inventing a selection.
		{"crossing with nothing selected stays unselected", 5, 2, 0, -1, -1},
		{"crossing an empty column stays unselected", 0, 0, 0, -1, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nav := NewERDiagramNav(tc.incoming, tc.outgoing)
			for range tc.rows {
				nav.MoveDown()
			}
			start := nav.ActiveRow()
			// The fixture must be standing on a real row of the column it is
			// in, or it is not testing the crossing.
			if tc.rows > 0 && start != tc.rows-1 {
				t.Fatalf("%d MoveDowns in a %d-row column left the cursor on row %d, want %d", tc.rows, tc.incoming, start, tc.rows-1)
			}

			nav.MoveRight()
			if got := nav.ActiveRow(); got != tc.wantAfterRight {
				t.Errorf("moving right from row %d into a %d-row column landed on row %d, want %d", start, tc.outgoing, got, tc.wantAfterRight)
			}
			// Whatever it became must be selectable, or correctly unselected.
			if got := nav.ActiveRow(); tc.outgoing > 0 && got >= tc.outgoing {
				t.Errorf("after moving right the cursor is on row %d of a %d-row column", got, tc.outgoing)
			}
			if tc.outgoing == 0 && nav.HasSelection() {
				t.Errorf("the cursor selected a row in an empty column: %d", nav.ActiveRow())
			}

			nav.MoveLeft()
			if got := nav.ActiveRow(); got != tc.wantAfterLeft {
				t.Errorf("moving back left landed on row %d, want %d", got, tc.wantAfterLeft)
			}
			if got := nav.ActiveRow(); tc.incoming > 0 && got >= tc.incoming {
				t.Errorf("after moving left the cursor is on row %d of a %d-row column", got, tc.incoming)
			}
		})
	}
}

// Scenario: El cursor no se sale de su columna en ninguna dirección.
//
// There are exactly two columns, 0 and 1. Moving left from 0 and right from 1 must
// both be no-ops — a cursor that wrapped around would put the highlight on the wrong
// side of the diagram, and one that ran past 1 would index out of range in
// GetSelectedRelationship.
func TestERDiagramNav_TheCursorCannotLeaveItsColumn(t *testing.T) {
	nav := NewERDiagramNav(3, 3)

	nav.MoveLeft()
	if got := nav.ActiveColumn(); got != 0 {
		t.Errorf("MoveLeft from the first column moved to column %d, want it pinned at 0", got)
	}
	nav.MoveRight()
	if got := nav.ActiveColumn(); got != 1 {
		t.Errorf("MoveRight did not reach column 1, got %d", got)
	}
	nav.MoveRight()
	if got := nav.ActiveColumn(); got != 1 {
		t.Errorf("MoveRight from the last column moved to column %d, want it pinned at 1", got)
	}
}

// Scenario: Subir nunca baja de la primera fila, ni la inventa.
//
// MoveUp stops at row 0 and does nothing at all when there is no selection, because
// -1 means "nothing selected" rather than "the row above the first".
func TestERDiagramNav_MoveUpStopsAtTheFirstRow(t *testing.T) {
	nav := NewERDiagramNav(4, 4)

	nav.MoveUp() // nothing selected yet
	if got := nav.ActiveRow(); got != -1 {
		t.Errorf("MoveUp with no selection landed on row %d, want it to stay unselected", got)
	}

	nav.MoveDown()
	nav.MoveDown()
	nav.MoveDown()
	if got := nav.ActiveRow(); got != 2 {
		t.Fatalf("the fixture is on row %d, want 2", got)
	}
	for range 5 {
		nav.MoveUp()
	}
	if got := nav.ActiveRow(); got != 0 {
		t.Errorf("after overshooting upward the cursor is on row %d, want it pinned at 0", got)
	}
	if !nav.HasSelection() {
		t.Error("the cursor lost its selection on the way up")
	}
}

// Scenario: La relación seleccionada es la de la fila, de la columna correcta.
//
// GetSelectedRelationship is the bridge from the cursor to the data: the app acts on
// the relationship it returns. So it has to return the relationship at the cursor's
// row in the cursor's COLUMN — column 0 is incoming (1:N, the tables that point at
// this one) and column 1 is outgoing (N:1, the tables this one points at). Swapping
// them would navigate to the wrong table, and the highlight would still look right.
//
// It returns nil rather than panicking in three cases: no selection, an empty column,
// and a row past the end of the list. The third is reachable because the nav counts
// and the diagram's slices are set independently — the preview can be handed a diagram
// that has since been rebuilt shorter.
func TestERDiagramNav_GetSelectedRelationshipReadsTheRightCell(t *testing.T) {
	diagram := ERDiagram{
		Center: TableBox{Name: "orders", Columns: []ColumnBadge{{Name: "id", DataType: "integer", IsPK: true}}},
		Incoming: []Relationship{
			{FromColumn: "order_id", ToTable: "invoices", Cardinality: "1:N"},
			{FromColumn: "order_id", ToTable: "shipments", Cardinality: "1:N"},
		},
		Outgoing: []Relationship{
			{FromColumn: "user_id", ToTable: "users", Cardinality: "N:1"},
		},
	}

	t.Run("the incoming column yields an incoming relationship", func(t *testing.T) {
		nav := NewERDiagramNav(2, 1)
		nav.MoveDown() // row 0 of column 0
		got := nav.GetSelectedRelationship(&diagram)
		if got == nil {
			t.Fatal("nothing was selected")
		}
		if got.ToTable != "invoices" || got.Cardinality != "1:N" {
			t.Errorf("selected %+v, want the first INCOMING relationship (invoices, 1:N)", got)
		}
		nav.MoveDown() // row 1
		if got := nav.GetSelectedRelationship(&diagram); got == nil || got.ToTable != "shipments" {
			t.Errorf("selected %+v, want the second incoming relationship (shipments)", got)
		}
	})

	t.Run("the outgoing column yields an outgoing relationship", func(t *testing.T) {
		nav := NewERDiagramNav(2, 1)
		nav.MoveRight()
		nav.MoveDown()
		got := nav.GetSelectedRelationship(&diagram)
		if got == nil {
			t.Fatal("nothing was selected")
		}
		if got.ToTable != "users" || got.Cardinality != "N:1" {
			t.Errorf("selected %+v, want the OUTGOING relationship (users, N:1)", got)
		}
	})

	t.Run("the nil cases", func(t *testing.T) {
		if got := NewERDiagramNav(2, 1).GetSelectedRelationship(&diagram); got != nil {
			t.Errorf("an unselected cursor returned %+v, want nil", got)
		}
		empty := NewERDiagramNav(0, 1)
		if got := empty.GetSelectedRelationship(&diagram); got != nil {
			t.Errorf("an empty column returned %+v, want nil", got)
		}
		// Counts that exceed the diagram: the preview rebuilds the diagram and
		// the nav keeps the counts it was given.
		stale := NewERDiagramNav(9, 9)
		stale.MoveRight()
		for range 5 {
			stale.MoveDown()
		}
		if got := stale.GetSelectedRelationship(&diagram); got != nil {
			t.Errorf("a cursor past the end of a 1-row column returned %+v, want nil", got)
		}
	})
}

// ---------------------------------------------------------------------------
// ERDiagramViewport
// ---------------------------------------------------------------------------

// Scenario: El desplazamiento nunca pasa del contenido ni baja de cero.
//
// The viewport scrolls a rendered diagram inside a pane. It can scroll down only as
// far as the content exceeds the pane — a shorter diagram has nothing to scroll — and
// it can never go negative. Both edges are where a scroll bug shows up as a blank
// region or a panic.
func TestERDiagramViewport_ScrollingStaysInsideTheContent(t *testing.T) {
	for _, tc := range []struct {
		name          string
		pane, content int
		wantMax       int
	}{
		{"content taller than the pane", 10, 25, 15},
		{"content exactly as tall as the pane", 10, 10, 0},
		{"content shorter than the pane", 10, 4, 0},
		{"no content at all", 10, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := NewERDiagramViewport(tc.pane, tc.content)

			if got := v.MaxScroll(); got != tc.wantMax {
				t.Errorf("MaxScroll = %d, want %d", got, tc.wantMax)
			}
			// Down as far as it goes, and past it.
			for range tc.content * 2 {
				v.ScrollDown()
			}
			if got := v.ScrollOffset; got != tc.wantMax {
				t.Errorf("after scrolling down repeatedly the offset is %d, want it capped at %d", got, tc.wantMax)
			}
			// The real bound is MaxScroll, which is 0 when the content is
			// shorter than the pane: there is nothing to scroll, so an offset
			// of 0 is correct even though content-pane is negative.
			if v.ScrollOffset > v.MaxScroll() {
				t.Errorf("the offset %d exceeds the maximum scroll %d", v.ScrollOffset, v.MaxScroll())
			}
			if v.ScrollOffset < 0 {
				t.Errorf("the offset is negative: %d", v.ScrollOffset)
			}
			// And back up, and past the top.
			for range tc.content * 2 {
				v.ScrollUp()
			}
			if got := v.ScrollOffset; got != 0 {
				t.Errorf("after scrolling up repeatedly the offset is %d, want 0", got)
			}
		})
	}
}

// Scenario: Saltar a los extremos va al primero y al último.
//
// JumpTop and JumpBottom are absolute, and Reset is JumpTop. Bottom is the maximum
// scroll rather than the content height: the last line still has to be visible, so
// scrolling to ContentHeight would put the pane past the end showing blanks.
func TestERDiagramViewport_JumpsGoToTheEnds(t *testing.T) {
	v := NewERDiagramViewport(8, 30)
	if got := v.MaxScroll(); got != 22 {
		t.Fatalf("MaxScroll = %d, want 22", got)
	}

	v.JumpBottom()
	if got := v.ScrollOffset; got != 22 {
		t.Errorf("JumpBottom left the offset at %d, want 22", got)
	}
	// The bottom of the content is what is showing.
	if lastShown := v.ScrollOffset + v.PaneHeight; lastShown != v.ContentHeight {
		t.Errorf("at the bottom the pane shows up to line %d of %d", lastShown, v.ContentHeight)
	}

	v.JumpTop()
	if got := v.ScrollOffset; got != 0 {
		t.Errorf("JumpTop left the offset at %d, want 0", got)
	}

	v.ScrollDown()
	v.ScrollDown()
	v.Reset()
	if got := v.ScrollOffset; got != 0 {
		t.Errorf("Reset left the offset at %d, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// BuildERDiagram
// ---------------------------------------------------------------------------

// col and fk build the inputs BuildERDiagram takes, so the fixtures below read as
// schema rather than as struct literals.
func col(name, dataType string) postgres.ColumnInfo {
	return postgres.ColumnInfo{Name: name, DataType: dataType}
}

func fk(column, refTable, refColumn string) postgres.ForeignKeyInfo {
	return postgres.ForeignKeyInfo{Column: column, RefSchema: "public", RefTable: refTable, RefColumn: refColumn}
}

func pk(columns ...string) postgres.ConstraintInfo {
	return postgres.ConstraintInfo{Type: "PRIMARY KEY", Columns: strings.Join(columns, ", ")}
}

// Scenario: La caja central enseña las columnas PK y FK, y solo esas.
//
// The centre box is the one that answers "how does this table key and join", so it
// shows the columns that take part in a key or a reference and leaves the rest out. A
// table with fifty columns and two keys draws a box a reader can actually scan.
//
// When a table has neither, showing nothing would be worse than showing everything, so
// the filter falls back to all of them.
func TestBuildERDiagram_TheCentreShowsKeyAndReferenceColumnsOnly(t *testing.T) {
	diagram := BuildERDiagram("public", "orders",
		[]postgres.ColumnInfo{
			col("id", "integer"), col("user_id", "integer"),
			col("total", "numeric"), col("created_at", "timestamptz"),
		},
		[]postgres.ConstraintInfo{pk("id")},
		[]postgres.ForeignKeyInfo{fk("user_id", "users", "id")},
		nil,
	)

	got := make([]string, 0, len(diagram.Center.Columns))
	for _, c := range diagram.Center.Columns {
		got = append(got, c.Name)
	}
	want := []string{"id", "user_id"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("the centre shows %v, want %v: only key and reference columns belong in the box", got, want)
	}

	// The badges are what make those two rows meaningful, and a column that is
	// both keeps PK.
	for _, c := range diagram.Center.Columns {
		switch c.Name {
		case "id":
			if !c.IsPK || c.IsFK {
				t.Errorf("id is PK=%v FK=%v, want PK and not FK", c.IsPK, c.IsFK)
			}
		case "user_id":
			if !c.IsFK || c.IsPK {
				t.Errorf("user_id is PK=%v FK=%v, want FK and not PK", c.IsFK, c.IsPK)
			}
		}
	}
	if diagram.Center.Name != "orders" || diagram.Center.Schema != "public" {
		t.Errorf("the centre box is %s.%s, want public.orders", diagram.Center.Schema, diagram.Center.Name)
	}
}

// Scenario: Una tabla sin claves enseña todas sus columnas.
//
// The fallback: a table with no primary key and no foreign keys would otherwise draw an
// empty frame, which tells the reader nothing about the table at all. Showing every
// column is the lesser surprise, and it is the only place the PK/FK filter is bypassed.
func TestBuildERDiagram_WithNoKeysEveryColumnIsShown(t *testing.T) {
	diagram := BuildERDiagram("public", "logs",
		[]postgres.ColumnInfo{col("a", "text"), col("b", "text"), col("c", "text")},
		[]postgres.ConstraintInfo{{Type: "UNIQUE", Columns: "a"}}, // not a primary key
		nil, nil,
	)

	if got := len(diagram.Center.Columns); got != 3 {
		t.Errorf("the centre shows %d columns, want all 3: a table with no keys falls back to every column", got)
	}
	for _, c := range diagram.Center.Columns {
		if c.IsPK || c.IsFK {
			t.Errorf("column %q claims PK=%v FK=%v on a table with neither", c.Name, c.IsPK, c.IsFK)
		}
	}
}

// Scenario: Una tabla de union tiene exactamente DOS referencias a tablas distintas.
//
// A junction table resolves a many-to-many into two many-to-ones, and that is exactly
// what it is used for in the diagram: it is drawn with a marker so the user can see
// the many-to-many rather than inferring it from two arrows.
//
// The test is exact: one reference is a plain foreign key, two to the SAME table is a
// self-reference, and three is some other kind of table. Only "exactly two, and they
// differ" is a junction.
func TestIsJunctionTable_ExactlyTwoReferencesToDifferentTables(t *testing.T) {
	for _, tc := range []struct {
		name string
		fks  []postgres.ForeignKeyInfo
		want bool
	}{
		{"no references", nil, false},
		{"one reference", []postgres.ForeignKeyInfo{fk("a_id", "users", "id")}, false},
		{"two references to different tables", []postgres.ForeignKeyInfo{
			fk("order_id", "orders", "id"), fk("product_id", "products", "id"),
		}, true},
		{"two references to the same table", []postgres.ForeignKeyInfo{
			fk("parent_id", "nodes", "id"), fk("owner_id", "nodes", "id"),
		}, false},
		{"three references", []postgres.ForeignKeyInfo{
			fk("a_id", "a", "id"), fk("b_id", "b", "id"), fk("c_id", "c", "id"),
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isJunctionTable(tc.fks); got != tc.want {
				t.Errorf("isJunctionTable = %v, want %v", got, tc.want)
			}
		})
	}
}

// Scenario: Una tabla central de union lleva el marcador.
//
// The junction flag on the diagram is computed from the table's OWN outgoing
// references, which is what makes the centre box carry the marker.
func TestBuildERDiagram_AJunctionCentreTableIsMarked(t *testing.T) {
	junction := BuildERDiagram("public", "order_items",
		[]postgres.ColumnInfo{col("order_id", "integer"), col("product_id", "integer")},
		[]postgres.ConstraintInfo{pk("order_id", "product_id")},
		[]postgres.ForeignKeyInfo{
			fk("order_id", "orders", "id"),
			fk("product_id", "products", "id"),
		},
		nil,
	)
	if !junction.Center.IsJunction {
		t.Error("a centre table with two references to different tables is not marked as a junction")
	}

	plain := BuildERDiagram("public", "orders",
		[]postgres.ColumnInfo{col("id", "integer"), col("user_id", "integer")},
		[]postgres.ConstraintInfo{pk("id")},
		[]postgres.ForeignKeyInfo{fk("user_id", "users", "id")},
		nil,
	)
	if plain.Center.IsJunction {
		t.Error("a centre table with one reference is marked as a junction")
	}
}

// Scenario: Una clave compuesta cuenta TODAS sus columnas.
//
// A composite primary key is written as one constraint with a comma-separated column
// list, and every column in it is a key column. Reading only the first would badge one
// column and leave the other looking like an ordinary field.
func TestBuildPKSet_EveryColumnOfAKeyIsAKeyColumn(t *testing.T) {
	pks := buildPKSet([]postgres.ConstraintInfo{
		pk("order_id", "product_id"),
		{Type: "UNIQUE", Columns: "sku"},             // not a primary key
		{Type: "primary key", Columns: "lower_case"}, // the type is compared case-insensitively
	})

	for _, col := range []string{"order_id", "product_id", "lower_case"} {
		if !pks[col] {
			t.Errorf("%q is not marked as a key column, want it to be", col)
		}
	}
	if pks["sku"] {
		t.Error("a UNIQUE constraint marked a column as a primary key")
	}
	// And the separator's whitespace is handled, since it comes from
	// information_schema's output formatting.
	if !pks["product_id"] || !pks["order_id"] {
		t.Errorf("the composite key was not split correctly: %v", pks)
	}
}

// Scenario: Una tabla enseña como mucho MAX NEIGHBORS vecinos, y cuenta los que esconde.
//
// A hub table has hundreds of neighbours and the diagram is a pane, so it draws at
// most MaxNeighbors and reports the rest. The overflow count is the user's only clue
// that the diagram is not the whole truth, so it has to be exact — and both sides of
// the cap are tested, because only an over-cap fixture exercises the subtraction.
func TestBuildERDiagram_TheHubCapIsSymmetricAndExact(t *testing.T) {
	for _, tc := range []struct {
		name         string
		n            int
		wantShown    int
		wantOverflow int
	}{
		{"far under the cap", 3, 3, 0},
		{"one under the cap", MaxNeighbors - 1, MaxNeighbors - 1, 0},
		{"exactly at the cap", MaxNeighbors, MaxNeighbors, 0},
		{"one over the cap", MaxNeighbors + 1, MaxNeighbors, 1},
		{"far over the cap", 25, MaxNeighbors, 25 - MaxNeighbors},
	} {
		t.Run(fmt.Sprintf("%s, %d incoming", tc.name, tc.n), func(t *testing.T) {
			// Distinct table names, so the per-table dedup does not collapse
			// them into one.
			schemaFKs := make(map[string][]postgres.ForeignKeyInfo, tc.n)
			for i := range tc.n {
				schemaFKs[fmt.Sprintf("t%02d", i)] = []postgres.ForeignKeyInfo{fk("x_id", "hub", "id")}
			}

			d := BuildERDiagram("public", "hub",
				[]postgres.ColumnInfo{col("id", "integer")},
				[]postgres.ConstraintInfo{pk("id")},
				nil, schemaFKs,
			)

			if got := len(d.Incoming); got != tc.wantShown {
				t.Errorf("Incoming holds %d relationships, want %d", got, tc.wantShown)
			}
			if got := d.IncomingOverflow; got != tc.wantOverflow {
				t.Errorf("IncomingOverflow = %d, want %d: it is the only clue that the diagram is partial", got, tc.wantOverflow)
			}
			// And the two agree: what is shown plus what is hidden is what
			// exists.
			if got := len(d.Incoming) + d.IncomingOverflow; got != tc.n {
				t.Errorf("shown %d plus hidden %d is %d, want the %d that exist", len(d.Incoming), d.IncomingOverflow, got, tc.n)
			}
		})
	}

	t.Run("the outgoing cap is the same shape", func(t *testing.T) {
		// Distinct referenced tables, which is what the outgoing dedup keys on.
		fks := make([]postgres.ForeignKeyInfo, 0, 25)
		for i := range 25 {
			fks = append(fks, fk(fmt.Sprintf("t%02d_id", i), fmt.Sprintf("t%02d", i), "id"))
		}

		d := BuildERDiagram("public", "hub", []postgres.ColumnInfo{col("id", "integer")},
			[]postgres.ConstraintInfo{pk("id")}, fks, nil)

		if got := len(d.Outgoing); got != MaxNeighbors {
			t.Errorf("Outgoing holds %d relationships, want the cap of %d", got, MaxNeighbors)
		}
		if got := d.OutgoingOverflow; got != 25-MaxNeighbors {
			t.Errorf("OutgoingOverflow = %d, want %d", got, 25-MaxNeighbors)
		}
	})
}

// Scenario: Una tabla aparece UNA vez, aunque la referencie con varias columnas.
//
// Two foreign keys from the same table are two columns of the same relationship, and
// the diagram draws relationships between TABLES. Listing the table twice would show
// one neighbour as two boxes, which reads as a mistake in the data.
func TestBuildERDiagram_EachNeighbourTableAppearsOnce(t *testing.T) {
	t.Run("incoming: one table with two columns pointing at the centre", func(t *testing.T) {
		d := BuildERDiagram("public", "users",
			[]postgres.ColumnInfo{col("id", "integer")},
			[]postgres.ConstraintInfo{pk("id")},
			nil,
			map[string][]postgres.ForeignKeyInfo{
				"invoices": {fk("user_id", "users", "id"), fk("approved_by", "users", "id")},
			},
		)
		if got := len(d.Incoming); got != 1 {
			t.Fatalf("Incoming holds %d relationships, want 1: a table appears once however many columns reference it", got)
		}
		if got := d.Incoming[0].ToTable; got != "invoices" {
			t.Errorf("the incoming relationship points at %q, want invoices", got)
		}
		if got := d.Incoming[0].Cardinality; got != "1:N" {
			t.Errorf("an incoming relationship has cardinality %q, want 1:N", got)
		}
	})

	t.Run("outgoing: two columns pointing at the same table", func(t *testing.T) {
		d := BuildERDiagram("public", "orders",
			[]postgres.ColumnInfo{col("id", "integer"), col("ship_to", "integer"), col("bill_to", "integer")},
			[]postgres.ConstraintInfo{pk("id")},
			[]postgres.ForeignKeyInfo{
				fk("ship_to", "addresses", "id"),
				fk("bill_to", "addresses", "id"),
			},
			nil,
		)
		if got := len(d.Outgoing); got != 1 {
			t.Fatalf("Outgoing holds %d relationships, want 1", got)
		}
		if got := d.Outgoing[0].ToTable; got != "addresses" {
			t.Errorf("the outgoing relationship points at %q, want addresses", got)
		}
		if got := d.Outgoing[0].Cardinality; got != "N:1" {
			t.Errorf("an outgoing relationship has cardinality %q, want N:1", got)
		}
	})
}

// Scenario: Una referencia a OTRO esquema se nombra con su esquema.
//
// A cross-schema reference is written as `schema.table`, because the diagram is the
// user's only view of it and an unqualified name would be ambiguous with a same-named
// table in the current schema. A reference with no schema at all is assumed to be
// local and stays unqualified.
func TestBuildERDiagram_CrossSchemaReferencesAreQualified(t *testing.T) {
	for _, tc := range []struct {
		name string
		fk   postgres.ForeignKeyInfo
		want string
	}{
		{"the same schema stays bare", fk("user_id", "users", "id"), "users"},
		{"no schema at all stays bare", postgres.ForeignKeyInfo{Column: "user_id", RefTable: "users", RefColumn: "id"}, "users"},
		{"another schema is qualified", postgres.ForeignKeyInfo{Column: "user_id", RefSchema: "auth", RefTable: "users", RefColumn: "id"}, "auth.users"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := BuildERDiagram("public", "orders",
				[]postgres.ColumnInfo{col("id", "integer"), col("user_id", "integer")},
				[]postgres.ConstraintInfo{pk("id")},
				[]postgres.ForeignKeyInfo{tc.fk},
				nil,
			)
			if len(d.Outgoing) != 1 {
				t.Fatalf("Outgoing holds %d relationships, want 1", len(d.Outgoing))
			}
			if got := d.Outgoing[0].ToTable; got != tc.want {
				t.Errorf("the reference is named %q, want %q", got, tc.want)
			}
		})
	}
}

// Scenario: Una referencia INCOMING de otro esquema no cuenta.
//
// Incoming is built by walking every table in the schema map and asking which ones
// point at THIS table in THIS schema. A same-named table in another schema is a
// different table, so it must not appear as a neighbour.
func TestBuildERDiagram_AnIncomingReferenceFromAnotherSchemaIsNotANeighbour(t *testing.T) {
	d := BuildERDiagram("public", "users",
		[]postgres.ColumnInfo{col("id", "integer")},
		[]postgres.ConstraintInfo{pk("id")},
		nil,
		map[string][]postgres.ForeignKeyInfo{
			"invoices": {fk("user_id", "users", "id")}, // local: a neighbour
			"audit":    {postgres.ForeignKeyInfo{Column: "user_id", RefSchema: "legacy", RefTable: "users", RefColumn: "id"}},
		},
	)

	if got := len(d.Incoming); got != 1 {
		t.Fatalf("Incoming holds %d relationships, want only the local one: %+v", got, d.Incoming)
	}
	if got := d.Incoming[0].ToTable; got != "invoices" {
		t.Errorf("the neighbour is %q, want invoices", got)
	}
}

// ---------------------------------------------------------------------------
// RenderERDiagram — the whole thing
// ---------------------------------------------------------------------------

// diagramWith builds a diagram whose centre box is sized by `colWidths` DISTINCT
// columns, so the centring arithmetic is observable rather than hidden behind the clamp:
// one wide column already saturates at MaxBoxWidth, and a narrow one leaves the box at
// the floor, so only the column COUNT changes the result.
func diagramWith(colWidths int, incoming, outgoing int) ERDiagram {
	cols := make([]ColumnBadge, 0, colWidths)
	for i := range colWidths {
		// A name and type chosen so the widest column measures near the middle
		// of the allowed range, where the clamp is not hiding the arithmetic.
		name := fmt.Sprintf("column_%02d", i)
		cols = append(cols, ColumnBadge{Name: name, DataType: "character varying", IsPK: i == 0})
	}
	d := ERDiagram{
		Center: TableBox{Schema: "public", Name: "orders", Columns: cols},
	}
	for i := range incoming {
		d.Incoming = append(d.Incoming, Relationship{FromColumn: "f_id", ToTable: fmt.Sprintf("in%02d", i), Cardinality: "1:N"})
	}
	for i := range outgoing {
		d.Outgoing = append(d.Outgoing, Relationship{FromColumn: "g_id", ToTable: fmt.Sprintf("out%02d", i), Cardinality: "N:1"})
	}
	return d
}

// lines splits a render into lines.
func lines(s string) []string { return strings.Split(s, "\n") }

// Scenario: Una tabla sin vecinos dice exactamente eso.
//
// The empty case gets its own sentence rather than an empty frame, because "no
// relationships" is information and an empty box would look like a rendering failure.
// A table with only incoming neighbours still draws, since a 1:N relationship is a
// relationship.
func TestRenderERDiagram_OnlyATotallyIsolatedTableGetsTheEmptySentence(t *testing.T) {
	isolated := ERDiagram{Center: TableBox{Name: "lonely", Columns: []ColumnBadge{{Name: "id", DataType: "integer"}}}}
	if got := RenderERDiagram(isolated, 80, 20, nil); got != "  No relationships for this table" {
		t.Errorf("an isolated table rendered %q, want the empty sentence", got)
	}
	// A centre box and no neighbours is still the empty case: the sentence
	// covers the whole diagram, not just the columns.
	empty := ERDiagram{Center: TableBox{Name: "lonely", Columns: []ColumnBadge{{Name: "id", DataType: "integer"}}}}
	if got := RenderERDiagram(empty, 80, 20, nil); !strings.Contains(got, "No relationships") {
		t.Errorf("a table with no neighbours rendered %q, want the empty sentence", got)
	}
	// And one neighbour is enough to draw.
	one := diagramWith(2, 1, 0)
	if got := RenderERDiagram(one, 80, 20, nil); strings.Contains(got, "No relationships") {
		t.Errorf("a table with one neighbour rendered the empty sentence: %q", got)
	}
}

// Scenario: Las dos columnas usan la mitad del ancho, MENOS un gutter de dos.
//
// The neighbour columns split the pane between them with a two-space gutter down the
// middle. So each column is (paneWidth-4)/2: the four cells are the outer margin and
// the gutter, and the integer division means an ODD pane leaves one cell unused rather
// than one column being a cell wider than the other — two unequal columns would make
// the centre line of the diagram ragged.
//
// The sweep stays inside [44, 104] so neither clamp fires: outside that range the clamp
// masks any change to the division, which is exactly what makes a test there prove
// nothing.
func TestRenderERDiagram_TheTwoColumnsSplitThePaneWithAGutter(t *testing.T) {
	for _, paneWidth := range []int{44, 45, 60, 80, 104} {
		t.Run(fmt.Sprintf("pane width %d", paneWidth), func(t *testing.T) {
			d := diagramWith(2, 1, 1)
			out := lines(RenderERDiagram(d, paneWidth, 20, nil))

			wantColWidth := (paneWidth - 4) / 2
			if wantColWidth < MinBoxWidth {
				t.Fatalf("the fixture's pane width %d clamps the columns, so it cannot test the division", paneWidth)
			}

			// The header row is the widest merged row: title + gutter + title.
			header := 0
			for _, l := range out {
				if w := ansi.StringWidth(l); w > header {
					header = w
				}
			}
			// 2*colWidth + 2 for the gutter. With an odd pane the last cell is
			// unused, so the block is one narrower than the pane minus margins.
			wantHeader := 2*wantColWidth + 2
			if header != wantHeader {
				t.Errorf("the widest merged row is %d cells, want %d (two %d columns plus a two-cell gutter)", header, wantHeader, wantColWidth)
			}
			// Nothing may exceed the pane.
			for i, l := range out {
				if w := ansi.StringWidth(l); w > paneWidth {
					t.Errorf("line %d is %d cells wide in a %d-cell pane: %q", i, w, paneWidth, l)
				}
			}
		})
	}
}

// Scenario: La caja central va CENTRADA, con un desfase conocido de dos celdas.
//
// The centre table sits alone above the two neighbour columns, so it is centred rather
// than left-aligned: it belongs to no column, and hanging it off the left edge would
// read as "this table is the one on the left".
//
// The box width is measured from the output rather than recomputed, so the assertion
// cannot be satisfied by a change to ComputeBoxWidth that happens to cancel out.
func TestRenderERDiagram_TheCentreBoxIsCentredInThePane(t *testing.T) {
	// The centring arithmetic adds TWO to the box width before halving the
	// remainder:
	//
	//	centerTotalWidth := centerWidth + 2
	//	leftPad := (paneWidth - centerTotalWidth) / 2
	//
	// renderBox already counts its own borders inside `width` — innerWidth is
	// width-2 and every row is │ + inner + │ — so that +2 counts the two border
	// characters a second time. Because the inflated number is then HALVED, the
	// error lands as TWO cells of extra left margin, not one: with a leftover of
	// 2k the pad becomes k instead of k+1, leaving the right margin k+2.
	//
	// KNOWN BEHAVIOUR, pinned rather than fixed. Two cells of asymmetry in a
	// decorative box, and the fix — removing the +2 — would move every rendered
	// diagram by one cell for no functional gain, so the cost is not obviously
	// worth it. What is worth having on record is that it is a KNOWN asymmetry
	// rather than a drifting one: the test states the exact formula, so if
	// somebody removes the +2 this fails and they have to decide deliberately.
	for _, paneWidth := range []int{50, 80, 104, 120, 140} {
		for _, colWidths := range []int{1, 2, 3, 4, 6} {
			t.Run(fmt.Sprintf("pane %d, %d columns", paneWidth, colWidths), func(t *testing.T) {
				d := diagramWith(colWidths, 1, 1)
				out := lines(RenderERDiagram(d, paneWidth, 20, nil))
				if len(out) == 0 {
					t.Fatal("the render produced no lines")
				}

				top := out[0]
				leading := len(top) - len(strings.TrimLeft(top, " "))
				boxWidth := ansi.StringWidth(strings.TrimLeft(top, " "))

				// Centring is only observable when the box is narrower than the
				// pane: at equality the pad is zero under either formula.
				//
				// There is deliberately NO skip for leading == 0. An earlier
				// version of this test had one, on the reasoning that a box
				// flush against the left edge has nothing to assert — and it
				// silently hid a mutant that zeroes leftPad, because zeroing it
				// produces exactly leading == 0. A skip that tolerates the
				// symptom of a defect is a hole, so the assertion below is
				// absolute instead.
				if boxWidth >= paneWidth {
					t.Skipf("the centre box is %d cells in a %d-cell pane, so there is nothing to centre", boxWidth, paneWidth)
				}
				if leading == 0 {
					t.Errorf("the centre box starts in column 0 of a %d-cell pane with a width of %d; a box narrower than the pane must be padded, so the centring clamp or the arithmetic above is zeroing the pad:\n%q",
						paneWidth, boxWidth, top)
				}

				// The exact formula the code applies, with the box width
				// measured from the output rather than recomputed — a change to
				// ComputeBoxWidth that happened to cancel out would not satisfy
				// this.
				want := (paneWidth - (boxWidth + 2)) / 2
				if leading != want {
					t.Errorf("the centre box starts at column %d with a measured width of %d in a %d pane, want %d:\n%q",
						leading, boxWidth, paneWidth, want, top)
				}

				// And the consequence, stated so a reader is not surprised by
				// it: the box sits left of centre, and by exactly the two or
				// three cells the halved +2 explains. Never by more, and never
				// to the right.
				right := paneWidth - leading - boxWidth
				if diff := right - leading; diff < 2 || diff > 3 {
					t.Errorf("the left margin is %d and the right is %d, a difference of %d; want 2 or 3, which is what the halved +2 produces",
						leading, right, diff)
				}
				// If the +2 is ever removed the box becomes truly centred and
				// this fails, which is the point: the change would be deliberate.
				if diff := right - leading; diff == 0 {
					t.Errorf("the box is exactly centred, so the double-counted +2 appears to have been removed — that is a behaviour change and belongs in a commit of its own")
				}
			})
		}
	}
}

// Scenario: Solo la columna del cursor lleva la marca, en la fila del cursor.
//
// The cursor belongs to one neighbour in one column, and exactly that box is bracketed.
// The existing suite only ever selects the OUTGOING column, so a renderer that lit up
// the wrong column — or that ignored the column and lit the first neighbour in either —
// would still pass it. Both columns are exercised here, and so are two different rows
// in one column, because "which box" depends on the row as well.
//
// The check is on the LINE that carries the marker rather than on a substring of the
// whole render: both column headers sit above their boxes, so a whole-render substring
// search cannot tell which side a name is on.
func TestRenderERDiagram_OnlyTheCursorNeighbourIsMarked(t *testing.T) {
	// Two neighbours on each side, so a flipped column has a box to land on.
	d := ERDiagram{
		Center: TableBox{Name: "orders", Columns: []ColumnBadge{{Name: "id", DataType: "integer", IsPK: true}}},
		Incoming: []Relationship{
			{FromColumn: "order_id", ToTable: "invoices", Cardinality: "1:N"},
			{FromColumn: "order_id", ToTable: "shipments", Cardinality: "1:N"},
		},
		Outgoing: []Relationship{
			{FromColumn: "user_id", ToTable: "users", Cardinality: "N:1"},
			{FromColumn: "product_id", ToTable: "products", Cardinality: "N:1"},
		},
	}

	for _, tc := range []struct {
		name       string
		in, out    int // MoveDown counts, applied after crossing
		crossRight bool
		want       string
		wantAbsent []string
	}{
		{"the first incoming neighbour", 1, 0, false, "invoices", []string{"shipments", "users", "products"}},
		{"the second incoming neighbour", 2, 0, false, "shipments", []string{"invoices", "users", "products"}},
		{"the first outgoing neighbour", 0, 1, true, "users", []string{"invoices", "shipments", "products"}},
		{"the second outgoing neighbour", 0, 2, true, "products", []string{"invoices", "shipments", "users"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nav := NewERDiagramNav(2, 2)
			if tc.crossRight {
				nav.MoveRight()
			}
			for range max(tc.in, tc.out) {
				nav.MoveDown()
			}

			out := RenderERDiagram(d, 80, 20, nav)

			// The marked neighbour, on a line of its own.
			if !strings.Contains(out, "► "+tc.want+" ◄") {
				t.Errorf("%s is not bracketed:\n%s", tc.want, out)
			}
			// Exactly one box is bracketed.
			if n := strings.Count(out, "►"); n != 1 {
				t.Errorf("%d boxes are bracketed, want exactly 1:\n%s", n, out)
			}
			// The others are not.
			for _, absent := range tc.wantAbsent {
				if strings.Contains(out, "► "+absent+" ◄") {
					t.Errorf("%s is bracketed as well as %s:\n%s", absent, tc.want, out)
				}
			}
		})
	}

	t.Run("no cursor marks nothing", func(t *testing.T) {
		// A nav that exists but has not selected anything.
		nav := NewERDiagramNav(2, 2)
		for _, out := range []string{
			RenderERDiagram(d, 80, 20, nil),
			RenderERDiagram(d, 80, 20, nav),
		} {
			if strings.Contains(out, "►") || strings.Contains(out, "◄") {
				t.Errorf("an unselected diagram marked a neighbour:\n%s", out)
			}
		}
	})
}

// Scenario: Una columna más corta no inventa filas de la otra.
//
// The two columns are merged line by line and each row is `left + two spaces + right`.
// Where one column has run out its side is empty, so those rows are just the gutter and
// whatever the other column drew — they are NOT padded to the column width, because
// padding them would draw a box-sized blank that reads as a neighbour with no name.
//
// The fixture is deliberately lopsided: three on one side and one on the other, which
// is the case where an off-by-one in the loop either drops the last line or reads past
// the end of the shorter column.
func TestRenderERDiagram_AShorterColumnIsNotPaddedIntoFiction(t *testing.T) {
	// A column of n neighbours is 5n lines: the title, n boxes of four lines, and
	// n-1 spacers between them. The merged block is the taller of the two columns,
	// so it is 5*max(in, out). Derived rather than tabulated, so a change to the
	// compact box's line count shows up here as a wrong formula instead of as a
	// dozen rows that each have to be edited.
	const (
		titleLines  = 1
		boxLines    = 4
		spacersEach = 1
	)
	columnHeight := func(n int) int { return titleLines + n*boxLines + max(n-1, 0)*spacersEach }

	for _, tc := range []struct {
		name     string
		incoming int
		outgoing int
	}{
		{"three incoming against one outgoing", 3, 1},
		{"one incoming against three outgoing", 1, 3},
		{"two against two", 2, 2},
		{"one against one", 1, 1},
		{"five against one", 5, 1},
		{"one against five", 1, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantMergedRows := max(columnHeight(tc.incoming), columnHeight(tc.outgoing))
			d := diagramWith(2, tc.incoming, tc.outgoing)
			out := lines(RenderERDiagram(d, 80, 20, nil))

			// The merged block starts at the column header, the one line that
			// carries both titles, and runs to the end of the render.
			merged := 0
			seenHeader := false
			for i, l := range out {
				if strings.HasPrefix(l, "1:N") && strings.Contains(l, "N:1") {
					merged = len(out) - i
					seenHeader = true
					break
				}
			}
			if !seenHeader {
				t.Fatalf("the merged block was not found:\n%s", strings.Join(out, "\n"))
			}
			if merged != wantMergedRows {
				t.Errorf("the merged block has %d rows, want %d: %d against %d neighbours\n%s",
					merged, wantMergedRows, tc.incoming, tc.outgoing, strings.Join(out, "\n"))
			}

			// The LAST merged row must not be the bare gutter. That is what a
			// phantom extra iteration would append.
			last := out[len(out)-1]
			if strings.TrimSpace(last) == "" {
				t.Errorf("the last row is blank, so the merge ran one iteration past the end:\n%s", strings.Join(out, "\n"))
			}
			// And no row may be wider than the pane.
			for i, l := range out {
				if w := ansi.StringWidth(l); w > 80 {
					t.Errorf("row %d is %d cells in an 80-cell pane: %q", i, w, l)
				}
			}
		})
	}
}

// Scenario: El aviso de "+N more" aparece en el render completo, en su columna.
//
// The overflow trailer has to survive the merge: it is placed inside its own column,
// before the two columns are joined, so it lands under the neighbour it refers to and
// not at the far right of the diagram.
func TestRenderERDiagram_TheOverflowTrailerStaysInItsColumn(t *testing.T) {
	d := ERDiagram{
		Center: TableBox{Name: "hub", Columns: []ColumnBadge{{Name: "id", DataType: "integer", IsPK: true}}},
	}
	for i := range 14 {
		d.Incoming = append(d.Incoming, Relationship{FromColumn: "f_id", ToTable: fmt.Sprintf("in%02d", i), Cardinality: "1:N"})
	}
	d.IncomingOverflow = 4

	out := RenderERDiagram(d, 80, 20, nil)

	if !strings.Contains(out, " +4 more") {
		t.Errorf("the render does not report the 4 hidden neighbours:\n%s", out)
	}
	// Once, not once per neighbour.
	if n := strings.Count(out, "more"); n != 1 {
		t.Errorf("the trailer appears %d times, want 1:\n%s", n, out)
	}
	// And it is on the incoming side, which is the left half.
	for _, l := range lines(out) {
		if !strings.Contains(l, "+4 more") {
			continue
		}
		col := strings.Index(l, "+4 more")
		if col > 40 {
			t.Errorf("the trailer is at column %d, want it in the left half of an 80-cell pane:\n%s", col, l)
		}
		return
	}
	t.Errorf("the trailer line was not found:\n%s", out)
}

// ---------------------------------------------------------------------------
// Narrow boxes: where the two truncation branches actually differ
// ---------------------------------------------------------------------------

// Scenario: En una caja de 3 celdas el nombre es SIEMPRE una elipsis.
//
// At an inner width of 1 there is no room for a character and the ellipsis, so the
// renderer gives up on the name entirely. Both branches of the truncation agree here —
// which is exactly why a test that only uses wide boxes cannot tell them apart.
//
// Width 3 is reachable in neither renderer: ComputeBoxWidth floors at 20 and the column
// width is floored too. It is asserted anyway, because it is the case where a change to
// the threshold would be invisible everywhere else.
func TestRenderCompactBox_AtInnerWidthOneTheNameIsAlwaysAnEllipsis(t *testing.T) {
	for _, tc := range []struct {
		name     string
		boxName  string
		junction bool
		selected bool
	}{
		{"a name that does not fit", named(40), false, false},
		{"a name that fits", "users", false, false},
		{"a junction", "users", true, false},
		{"a selection", "users", false, true},
		{"a junction and a selection", "users", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lines := strings.Split(renderCompactBox(tc.boxName, "x", 3, tc.junction, tc.selected), "\n")
			if lines[1] != "│…│" {
				t.Errorf("the name row is %q, want %q: there is no room for a character and an ellipsis together", lines[1], "│…│")
			}
			// The fk line is cut, not ellipsised, so it keeps its one cell.
			if lines[2] != "│F│" {
				t.Errorf("the fk row is %q, want %q", lines[2], "│F│")
			}
		})
	}
}

// Scenario: En una caja de 4 celdas el nombre conserva UN carácter y la elipsis.
//
// This is the case that makes the truncation branch observable at all. At an inner
// width of 2 the taken branch emits `name[:1] + "…"` while the other branch would emit
// a bare "…". They differ, and a fixture that only ever used width 20 would never see
// it — which is the same class of blind spot as a truncation room that fits at every
// size.
//
// It is also where the SELECTOR becomes visible: "► " plus a name is four cells before
// the ellipsis even starts, so a selected box at this width is almost entirely arrow.
// That looks wrong and is not — the branch is applied after the selector, so the box
// still measures exactly its width and never overflows the column.
func TestRenderCompactBox_AtInnerWidthTwoOneCharacterSurvives(t *testing.T) {
	// Not selected: the name's own first character survives.
	plain := strings.Split(renderCompactBox(named(40), "x", 4, false, false), "\n")
	if want := "│x…│"; plain[1] != want {
		t.Errorf("the name row is %q, want %q: one character fits alongside the ellipsis", plain[1], want)
	}
	if want := "│FK│"; plain[2] != want {
		t.Errorf("the fk row is %q, want %q", plain[2], want)
	}
	// A short name at the same width is truncated the same way: the branch is on
	// width, not on how far over the name is.
	short := strings.Split(renderCompactBox("users", "x", 4, false, false), "\n")
	if want := "│u…│"; short[1] != want {
		t.Errorf("a 5-character name at width 4 is %q, want %q", short[1], want)
	}
	// Selected: the arrow is 1 of the 2 inner cells, so the ellipsis is what is
	// left. The row still measures exactly 4.
	sel := strings.Split(renderCompactBox("users", "x", 4, false, true), "\n")
	if got := ansi.StringWidth(sel[1]); got != 4 {
		t.Errorf("the selected name row is %d cells, want 4: %q", got, sel[1])
	}
	if !strings.Contains(sel[1], "…") {
		t.Errorf("the selected name row is %q, want it truncated: the arrow left no room", sel[1])
	}
	// And every width is respected, which is the property that matters most here.
	for _, w := range []int{3, 4, 5, 6, 8, 20, 50} {
		for _, name := range []string{named(40), "users", ""} {
			for _, j := range []bool{false, true} {
				for _, s := range []bool{false, true} {
					checkWidth(t, "narrow box", renderCompactBox(name, named(30), w, j, s), w)
				}
			}
		}
	}
}

// Scenario: La caja central corta el nombre igual que la de vecino, en anchos estrechos.
//
// The centre box has its own copy of the truncation branch rather than sharing the
// neighbour box's, so the narrow cases have to be asserted against BOTH. A width-only
// matrix is not enough here: at width 4 the two branches both produce a 4-cell row, so
// only the CONTENT tells them apart.
//
// The centre box is the one that centres its name, so a bare ellipsis is centred rather
// than left-aligned — that is the visible difference from a neighbour box at the same
// width, and it is what makes the two functions distinguishable in a golden.
func TestRenderBox_NarrowWidthsTruncateTheNameToOneCharacter(t *testing.T) {
	for _, tc := range []struct {
		name    string
		width   int
		boxName string
		want    string
	}{
		{"an over-long name at width 3 is a bare centred ellipsis", 3, named(40), "│…│"},
		{"an over-long name at width 4 keeps one character", 4, named(40), "│x…│"},
		{"a fitting name at width 3 is still a bare ellipsis", 3, "users", "│…│"},
		{"a fitting name at width 4 keeps its first character", 4, "users", "│u…│"},
		{"at width 5 two characters survive", 5, named(40), "│xx…│"},
		{"at width 5 a fitting name loses its tail", 5, "users", "│us…│"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lines := strings.Split(renderBox(tc.boxName, nil, tc.width, false), "\n")
			if got := lines[1]; got != tc.want {
				t.Errorf("the name row is %q, want %q", got, tc.want)
			}
			// And the frame around it is the right width.
			if got := ansi.StringWidth(lines[0]); got != tc.width {
				t.Errorf("the top border is %d cells, want %d", got, tc.width)
			}
			if got := ansi.StringWidth(lines[len(lines)-1]); got != tc.width {
				t.Errorf("the bottom border is %d cells, want %d", got, tc.width)
			}
		})
	}
}
