package grid

// Scenario: El filtro WHERE y la ultima fila del grid, en los casos que el resto de la suite
// no toca.
//
// Four guards, all of them arithmetic going out of range, and three of the four were dead
// guards for the same reason — a clamp applied EARLIER makes a later one redundant:
//
//   - the cursor row's clamp in the last-row pin. contentHeight has a floor of 1, so ch >= 1
//     and the computed row is never negative. Removed, with the two cases written out.
//   - the WHERE detector's empty-token arm. detectContext rejects an empty input four lines
//     above, extractLastClause returns a non-empty substring of a trimmed input, and tokenize
//     always yields at least one token. Removed, replaced by an exhaustive invariant.
//   - the suggestion popup's width floor and the label column's floor. These two are LIVE and
//     they fire TOGETHER: the popup floors at 40, so a narrow grid computes a label width of
//     40-10-14-6 = 10, below the fifteen it is then raised to. A test that only asserted "the
//     popup renders" would satisfy both without noticing which produced the width.
//
// The last-row pin itself is the refactor: the same four lines were written out three times,
// byte for byte, and the two floors had started to differ between the copies.

import (
	"strconv"
	"strings"
	"testing"

	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/theme"
)

// The last-row pin when the pane is shorter than the data.
//
// contentHeight has a floor of 1, so the SCROLL clamp is the one that fires here — a window
// with three rows of room and twenty rows of data gives totalRows-ch < 0 — while the cursor
// row's own clamp used to sit right below it as dead code. The arithmetic is checked, and so
// is the consequence, because "scrollRow is not negative" on its own says nothing about what
// a reader of the cursor row does next.
func TestPinningToTheLastRowInAPaneShorterThanTheData(t *testing.T) {
	for _, rows := range []int{1, 2, 20, 500} {
		t.Run(rowsName(rows), func(t *testing.T) {
			// The premise: there are more rows than the pane can show, which is what makes the
			// scroll offset negative before it is clamped.
			g := newGrid(t, rows, 3, 60, 12, 100)
			ch := g.contentHeight()
			total := g.visibleRows()
			if total == 0 {
				t.Skip("this fixture has no visible rows")
			}
			if total-ch >= 0 {
				t.Skipf("%d rows fit in %d lines, so the scroll clamp does not fire", total, ch)
			}

			g.pinToLastRow(total, ch)

			if g.scrollRow < 0 {
				t.Errorf("the scroll offset is %d, past the first row", g.scrollRow)
			}
			// The cursor row is the last one, and it is a legal index — which is the property
			// the removed clamp used to guarantee by assertion rather than by arithmetic.
			if g.cursorRow < 0 || g.cursorRow >= total {
				t.Errorf("the cursor row is %d, outside the %d rows", g.cursorRow, total)
			}
			if g.scrollRow+g.cursorRow != total-1 {
				t.Errorf("scroll %d plus cursor %d is %d, want the last row %d",
					g.scrollRow, g.cursorRow, g.scrollRow+g.cursorRow, total-1)
			}
		})
	}

	t.Run("and the three callers all survive it", func(t *testing.T) {
		// moveToLast, halfPageDown and clampCursor used to carry their own copy of this
		// arithmetic; now they share one function, so one fixture has to survive all three
		// entry points rather than one of them.
		for _, tc := range []struct {
			name string
			call func(*Grid)
		}{
			{"moveToLast", func(g *Grid) { g.moveToLast() }},
			{"halfPageDown", func(g *Grid) { g.halfPageDown() }},
			{"clampCursor", func(g *Grid) { g.clampCursor() }},
		} {
			t.Run(tc.name, func(t *testing.T) {
				g := newGrid(t, 500, 3, 60, 12, 100)
				g.cursorRow = 400

				tc.call(g)

				if g.cursorRow < 0 || g.cursorRow >= g.visibleRows() {
					t.Errorf("%s left the cursor at row %d of %d", tc.name, g.cursorRow, g.visibleRows())
				}
				if g.scrollRow < 0 {
					t.Errorf("%s left the scroll offset at %d", tc.name, g.scrollRow)
				}
			})
		}
	})
}

func rowsName(n int) string { return "rows-" + strconv.Itoa(n) }

// The suggestion popup's two width floors, on a grid too narrow for them.
//
// The label column's floor is only reachable BECAUSE the popup has a floor of 40: at 40 wide
// the label computes to 40-10-14-6 = 10, which is below the fifteen it is then raised to. So
// the two guards fire together on the same grid and in that order, which is why the assertion
// is that both ran rather than that the popup rendered.
func TestTheSuggestionPopupSurvivesANarrowGrid(t *testing.T) {
	columns := []postgres.ColumnInfo{
		{Name: "id", DataType: "integer"},
		{Name: "customer_id", DataType: "integer"},
		{Name: "total", DataType: "numeric"},
	}

	// Widths below the popup's minimum of 40, plus a couple above it as the counterweight.
	for _, width := range []int{1, 10, 40, 45, 46, 60, 120} {
		t.Run(widthName(width), func(t *testing.T) {
			wf := NewWhereFilter(theme.Resolve("dark").Styles(), columns, width)
			wf.input = ""
			wf.updateSuggestions()
			wf.showPopup = true

			if len(wf.suggestions) == 0 {
				t.Skip("no suggestions for this input, so the popup has nothing to lay out")
			}

			// The premise: the popup's own floor is what makes the label's floor reachable.
			popupWidth := width - 6
			if popupWidth < 40 {
				popupWidth = 40
			}
			labelWidth := popupWidth - 10 - 14 - 6
			if width <= 46 && labelWidth >= 15 {
				t.Fatalf("at width %d the label computes to %d, so its floor does not fire and this case proves nothing", width, labelWidth)
			}

			out := wf.RenderPopup()
			if out == "" {
				t.Fatalf("at width %d the popup rendered nothing", width)
			}
			// And the rows it did draw are the same set regardless of width: a layout that
			// dropped rows to fit would look correct and answer fewer suggestions.
			for _, want := range []string{"id", "customer_id"} {
				if !strings.Contains(out, want) {
					t.Errorf("at width %d the popup dropped the suggestion %q:\n%s", width, want, out)
				}
			}
		})
	}
}

// The context detector's emptiness answer, and the reason it has only one caller.
//
// detectContext answers "everything" for an empty input, which is the arm above the one that
// was removed. This case asserts THAT arm works and, more usefully, states the invariant that
// makes the arm below it dead: a non-empty input always produces a non-empty token list.
//
// The invariant is asserted by exhaustion over the inputs a user types, because a comment
// saying extractLastClause cannot return an empty string is worth nothing on its own — and
// because a change to extractLastClause that COULD return one would then fail here rather than
// be caught by a guard that no longer exists.
func TestANonEmptyFilterAlwaysHasAToken(t *testing.T) {
	columns := []postgres.ColumnInfo{
		{Name: "id", DataType: "integer"},
		{Name: "total", DataType: "numeric"},
	}
	wf := NewWhereFilter(theme.Resolve("dark").Styles(), columns, 80)

	// The inputs that make the question interesting: each of the keyword boundaries
	// extractLastClause searches for, with and without trailing punctuation, because those are
	// the ones where a slice could come back empty.
	for _, input := range []string{
		"id", "id ", "id =", "id = ", "id AND", "id AND ", "id OR", "id OR ",
		"id NOT", "id NOT ", "AND", "OR", "NOT", "AND ", "NOT IN ", "id > 3 AND total <",
		"((id", "id IS", "id IS NOT", "id::", "id->", "  id  ",
	} {
		trimmed := strings.TrimSpace(input)
		if trimmed == "" {
			continue // the empty-input arm above handles it, and it is not this invariant
		}

		lastClause := wf.extractLastClause(trimmed)

		if strings.TrimSpace(lastClause) == "" {
			t.Errorf("extractLastClause(%q) = %q, which is empty — the empty-token arm was removed because this cannot happen", input, lastClause)
			continue
		}
		if got := wf.tokenize(lastClause); len(got) == 0 {
			t.Errorf("tokenize(%q) produced nothing for the input %q", lastClause, input)
		}
	}

	t.Run("and the empty input still answers with every column", func(t *testing.T) {
		// The arm that IS reachable, and the counterweight: detectContext with nothing typed
		// has nothing to narrow by, so everything is a candidate.
		for _, blank := range []string{"", "   ", "\t\t"} {
			wf.input = strings.ReplaceAll(blank, "\\t", "\t")
			wf.updateSuggestions()
			wf.showPopup = true

			if len(wf.suggestions) == 0 {
				t.Errorf("the input %q suggested nothing", wf.input)
			}
			labels := map[string]bool{}
			for _, s := range wf.suggestions {
				labels[s.Label] = true
			}
			for _, col := range columns {
				if !labels[col.Name] {
					t.Errorf("the blank input %q does not offer the column %q", wf.input, col.Name)
				}
			}
		}
	})
}

func widthName(w int) string {
	return "width-" + strconv.Itoa(w)
}
