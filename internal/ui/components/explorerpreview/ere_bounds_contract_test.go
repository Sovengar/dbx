// The nav's cursor bounds, reached AT the boundary rather than beside it.
//
// Two functions resolve the cursor row into a slice, and both are guarded by a
// comparison whose two forms agree everywhere except at one value:
//
//	clampRow                n.activeRow >= count   ->  >
//	GetSelectedRelationship n.activeRow >= len(list) -> >
//
// At `activeRow == len(list)` the original declines the index and the mutant performs
// it, which is one element past the end of the slice — and a panic inside a test
// aborts the whole binary, so the tool scores it a survivor rather than a kill.
//
// `mustNotPanic` recovers the panic and reports it through the testing package, so the
// crash becomes an ordinary red test and the mutant dies.
//
// WHY THE BOUNDARY IS ENTERED THROUGH THE FIELD AND NOT THROUGH MoveDown
//
// MoveDown pins the cursor at `count - 1`, so no key sequence lands on the boundary —
// that was MEASURED for this test rather than assumed, and it is why the state is set
// directly. The claim under test is not "the cursor can get past the end" (it cannot,
// and clampRow exists to prove it) but "given a row past the end, do these two
// functions decline it". A guard nobody can reach is the guard that rots.

package explorerpreview

import (
	"fmt"
	"testing"
)

// mustNotPanic turns "the guard declined to index past the end" into a test failure
// instead of a crashed binary. See the note above: the panic is what the tool scores as
// a survivor, so it has to be caught to be counted at all.
func mustNotPanic(t *testing.T, what string, f func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("%s panicked: %v — the guard must decline an index past the end, not perform it", what, r)
		}
	}()
	f()
}

// navDiagram builds a diagram whose two columns carry n relationships each.
// Named apart from the diagramWith the render contract tests already have, which
// builds one for RenderColumn and has a different shape.
func navDiagram(n int) *ERDiagram {
	rels := make([]Relationship, n)
	for i := range rels {
		rels[i] = Relationship{FromColumn: "col", ToTable: "t", ToColumn: "c", Cardinality: "N:1"}
	}
	return &ERDiagram{Incoming: rels, Outgoing: rels}
}

// Scenario: Un cursor en la fila JUSTO DESPUES no se lleva la relacion.
//
// The list the row indexes is chosen by the column, and the guard compares the cursor
// against THAT list's length. Both columns have to be covered, because the two branches
// pick different slices and a fixture that only fills one of them would leave the other
// guard untested.
func TestGetSelectedRelationship_ARowJustPastTheEndDeclinesTheIndex(t *testing.T) {
	const n = 3

	for _, col := range []int{0, 1} {
		t.Run(fmt.Sprintf("column %d", col), func(t *testing.T) {
			nav := NewERDiagramNav(n, n)
			nav.activeColumn = col
			d := navDiagram(n)

			// The fixture really is one past the end of the list the
			// column selects — otherwise the guard is never the thing
			// under test.
			list := len(d.Incoming)
			if col == 1 {
				list = len(d.Outgoing)
			}
			nav.activeRow = list
			if nav.ActiveRow() != list || !nav.HasSelection() {
				t.Fatalf("the fixture's row is %d over a list of %d, want it one past and still a selection",
					nav.ActiveRow(), list)
			}

			var got *Relationship
			mustNotPanic(t, "GetSelectedRelationship one row past the end", func() {
				got = nav.GetSelectedRelationship(d)
			})
			if got != nil {
				t.Errorf("GetSelectedRelationship returned %+v for a row past the end, want nil", got)
			}
		})
	}

	// And the control, because a guard that returned nil for everything would
	// pass the two above: the last real row still resolves, and it is the one
	// the cursor names.
	t.Run("the last real row still resolves", func(t *testing.T) {
		nav := NewERDiagramNav(n, n)
		nav.activeColumn = 0
		d := navDiagram(n)
		nav.activeRow = len(d.Incoming) - 1
		got := nav.GetSelectedRelationship(d)
		if got == nil {
			t.Fatal("GetSelectedRelationship returned nil for the last real row")
		}
		if got != &d.Incoming[len(d.Incoming)-1] {
			t.Error("GetSelectedRelationship returned a relationship that is not the one at the cursor")
		}
	})

	// The other end: a negative cursor is not a selection at all, so the guard
	// is never reached.
	t.Run("a negative cursor is not a selection", func(t *testing.T) {
		nav := NewERDiagramNav(n, n)
		nav.activeRow = -1
		if nav.HasSelection() {
			t.Fatal("a cursor of -1 reports a selection")
		}
		mustNotPanic(t, "GetSelectedRelationship with no selection", func() {
			if got := nav.GetSelectedRelationship(navDiagram(n)); got != nil {
				t.Errorf("GetSelectedRelationship returned %+v with no selection", got)
			}
		})
	})
}

// Scenario: clampRow corrige un cursor JUSTO DESPUES, y lo deja en la ultima fila.
//
// clampRow is the rule that keeps the cursor inside its column, so it is the one place
// where an out-of-range row is CORRECTED rather than declined. At `activeRow == count`
// the original pulls it back to `count - 1` and the mutant leaves it at `count` — which
// is not a crash but a cursor pointing one row past the last, drawn as an empty
// selection.
//
// So the assertion is on the corrected value, not on the absence of a panic: that is
// the whole difference between the two forms here.
func TestClampRow_ARowJustPastTheEndIsPulledBackToTheLastRow(t *testing.T) {
	for _, n := range []int{1, 2, 3, 8} {
		t.Run(fmt.Sprintf("a column of %d", n), func(t *testing.T) {
			nav := NewERDiagramNav(n, n)

			// One past the end.
			nav.activeRow = n
			nav.clampRow()
			if got := nav.ActiveRow(); got != n-1 {
				t.Errorf("clampRow left the cursor at %d for a column of %d, want %d", got, n, n-1)
			}

			// Far past the end, which is what a stale count after a
			// refresh looks like.
			nav.activeRow = n + 100
			nav.clampRow()
			if got := nav.ActiveRow(); got != n-1 {
				t.Errorf("clampRow left the cursor at %d for a wildly stale row, want %d", got, n-1)
			}

			// A row inside the column is left alone — otherwise the
			// clamp would fight the navigation.
			for r := 0; r < n; r++ {
				nav.activeRow = r
				nav.clampRow()
				if got := nav.ActiveRow(); got != r {
					t.Errorf("clampRow moved a row that was already inside (row %d became %d)", r, got)
				}
			}

			// An empty column has no row 0, so the cursor becomes
			// "no selection" rather than row 0 or row -1.
			empty := NewERDiagramNav(0, 0)
			empty.activeRow = 3
			empty.clampRow()
			if empty.ActiveRow() != -1 {
				t.Errorf("clampRow left the cursor at %d in an empty column, want -1 for no selection", empty.ActiveRow())
			}
			if empty.HasSelection() {
				t.Error("an empty column reports a selection after clamping")
			}
		})
	}
}
