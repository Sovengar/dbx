// The render window's own size, at zero.
//
// `Render(startCol, count)` guards with `len(h.columns) == 0 || count <= 0` and returns
// an empty string. At count == 0 the two forms of that comparison differ: the original
// returns early, while the mutant walks a loop of zero iterations and then RENDERS a
// header with no columns in it — an empty row framed in borders, which is a visible
// artefact where there should be nothing at all.
//
// So the assertion is on the RETURNED STRING, not on the absence of a panic, and the
// control case matters: the same call with a real count has to produce a non-empty
// header, otherwise "returns empty" would be true for the wrong reason.
//
// The counting is done on the STRIPPED render, because a header with no columns is
// still all border and escape sequence — wide enough to draw, and empty of content.

package grid

import (
	"strings"
	"testing"

	"github.com/buble/dbx/internal/theme"
	"github.com/charmbracelet/x/ansi"
)

// Scenario: Una ventana de render de tamano CERO no dibuja una fila vacia.
//
// Two ways to reach the boundary and they are not the same state: count == 0 on a
// header that HAS columns, and count == 0 on a header with none. The first is the one
// that matters — it is what a horizontally scrolled grid past its last column asks for.
func TestHeaderRender_ZeroColumnsToRenderDrawsNothing(t *testing.T) {
	t.Run("a header with columns asked for none", func(t *testing.T) {
		h := NewHeader(theme.Resolve("dark").Styles())
		h.SetColumns([]string{"id", "name", "age"})
		if len(h.columns) != 3 {
			t.Fatalf("the fixture has %d columns, want 3", len(h.columns))
		}

		got := h.Render(0, 0)
		if got != "" {
			t.Errorf("Render(0, 0) on a header with 3 columns returned %q, want nothing at all",
				ansi.Strip(got))
		}
	})

	t.Run("a header with no columns asked for some", func(t *testing.T) {
		h := NewHeader(theme.Resolve("dark").Styles())
		if got := h.Render(0, 5); got != "" {
			t.Errorf("Render(0, 5) on an empty header returned %q, want nothing", ansi.Strip(got))
		}
	})

	// The controls. Without these a header that always returned "" would pass
	// everything above, and the mutants at the OTHER end of the same guard
	// (a start past the last column) would look covered when they are not.
	t.Run("a window of one column does draw", func(t *testing.T) {
		h := NewHeader(theme.Resolve("dark").Styles())
		h.SetColumns([]string{"id", "name", "age"})
		got := h.Render(0, 1)
		if got == "" {
			t.Fatal("Render(0, 1) returned nothing, so the zero-count cases prove nothing")
		}
		// The header renders column names UPPER-CASED, which was measured
		// rather than assumed: an assertion looking for "id" here passes
		// against a header that is broken in a completely different way.
		if !strings.Contains(ansi.Strip(got), "ID") {
			t.Errorf("Render(0, 1) rendered %q, want it to carry the first column's name", ansi.Strip(got))
		}
	})

	t.Run("a window of every column does draw them all", func(t *testing.T) {
		h := NewHeader(theme.Resolve("dark").Styles())
		h.SetColumns([]string{"id", "name", "age"})
		got := ansi.Strip(h.Render(0, 3))
		for _, want := range []string{"ID", "NAME", "AGE"} {
			if !strings.Contains(got, want) {
				t.Errorf("Render(0, 3) rendered %q, want it to carry %q", got, want)
			}
		}
	})

	// A NEGATIVE count is out of range on the other side and has to decline
	// the same way, which is the other mutant on this line's guard.
	t.Run("a negative count draws nothing", func(t *testing.T) {
		h := NewHeader(theme.Resolve("dark").Styles())
		h.SetColumns([]string{"id", "name", "age"})
		if got := h.Render(0, -1); got != "" {
			t.Errorf("Render(0, -1) returned %q, want nothing", ansi.Strip(got))
		}
	})

	// And the horizontally scrolled case that motivates count == 0 in
	// production: starting past the last column.
	t.Run("starting past the last column draws nothing", func(t *testing.T) {
		h := NewHeader(theme.Resolve("dark").Styles())
		h.SetColumns([]string{"id", "name", "age"})
		if got := h.Render(3, 2); got != "" {
			t.Errorf("Render(3, 2) on 3 columns returned %q, want nothing", ansi.Strip(got))
		}
		// The last column alone still draws, so the window is not simply
		// refused whenever startCol is at the end.
		if got := h.Render(2, 1); got == "" {
			t.Error("Render(2, 1) on 3 columns returned nothing, want the last column")
		}
	})
}
