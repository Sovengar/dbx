package grid

// The hovered row has its own two renderers (plain and draft-update) and the
// branch that picks between them was the one state a test never arranged:
// every fixture left the mouse outside the grid, so `isHovered` was always
// false and neither renderer was ever called.

import (
	"testing"
)

func TestGrid_TheHoveredRowRendersAsHovered(t *testing.T) {
	g := newGrid(t, 3, 2, 60, 16, 100)
	g.mouse.SetHover(1)

	visCols, visWidths := g.visibleColumns()
	visValues := make([]interface{}, len(visCols))
	for j := range visCols {
		visValues[j] = g.data.Rows[1][g.scrollCol+j]
	}
	want := g.cells.RenderHoveredRow(visValues, visWidths)

	block := resultOf(g.renderRecordsView())[g.recordsHeaderOffset()+1:]
	if got := block[1]; got != want {
		t.Errorf("the hovered row is %q, want the hover renderer's output %q", got, want)
	}

	// And the rows around it are NOT hovered: one hovered line, not a pane.
	if got := block[0]; got == want {
		t.Error("the row above the hovered one rendered as hovered too")
	}
}

func TestGrid_AHoveredRowWithDraftsRendersAsADraftUpdate(t *testing.T) {
	g := newGrid(t, 3, 2, 60, 16, 100)
	// A pending update on row 1 and the hover on that same row.
	g.pendingUpdates = append(g.pendingUpdates, PendingUpdate{
		RowIdx: 1, ColIdx: 0, OldValue: marker(1, 0), NewValue: "changed",
	})
	g.data.Rows[1][0] = "changed"
	g.mouse.SetHover(1)

	visCols, visWidths := g.visibleColumns()
	visValues := make([]interface{}, len(visCols))
	for j := range visCols {
		visValues[j] = g.data.Rows[1][g.scrollCol+j]
	}
	draftCols := make(map[int]bool)
	for j := range visCols {
		draftCols[j] = g.isCellModified(1, g.scrollCol+j)
	}
	want := g.cells.RenderDraftUpdateRow(visValues, visWidths, draftCols, -1)

	block := resultOf(g.renderRecordsView())[g.recordsHeaderOffset()+1:]
	if got := block[1]; got != want {
		t.Errorf("the hovered draft row is %q, want the draft renderer's output %q", got, want)
	}
}
