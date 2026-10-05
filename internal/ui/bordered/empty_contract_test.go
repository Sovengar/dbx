package bordered

// Scenario: Un panel sin contenido sigue siendo un panel.
//
// The "at least one line" branch exists because a box drawn with zero inner lines is not a
// box — lipgloss renders the top and bottom borders and nothing between them, and on a
// two-row modal that is a stripe rather than a dialog. So an empty body gets one blank line.
//
// Which means the branch is reachable only by asking for an empty body, and nothing in the
// product ever does: every caller passes content. That makes it exactly the kind of guard
// that gets deleted as dead code, and it is not dead — it is the difference between a modal
// that looks broken and one that looks empty.

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/buble/dbx/internal/theme"
)

func TestAnEmptyPanelStillHasAnInnerLine(t *testing.T) {
	styles := theme.Resolve("dark").Styles()
	border := lipgloss.RoundedBorder()

	for _, tc := range []struct {
		name    string
		content string
		width   int
		height  int
	}{
		{"no content at all", "", 30, 5},
		{"a single newline", "\n", 30, 5},
		{"content that is only spaces", "   ", 30, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := RenderWithTitle(border, styles.Border.GetBorderTopForeground(), "Title", tc.content, tc.width, tc.height)

			if out == "" {
				t.Fatal("the panel rendered nothing")
			}
			// At least one row between the borders: the rendered block has to be more than the
			// two border lines, or the box has no interior to be empty IN.
			rows := strings.Split(out, "\n")
			if len(rows) < 3 {
				t.Errorf("the panel is %d rows, want a top border, an interior and a bottom border:\n%q",
					len(rows), out)
			}
			// And an interior row that is actually blank rather than a stray border glyph —
			// which is what the branch exists to produce. A narrow width is deliberately not
			// in the table: lipgloss then wraps the content to fill every row and there is no
			// blank interior left to find, which is a different behaviour and a different test.
			interior := ""
			for _, r := range rows[1 : len(rows)-1] {
				interior += r
			}
			if !strings.Contains(interior, "  ") {
				t.Errorf("the interior is %q, with no blank row in it", interior)
			}
		})
	}

	t.Run("and a panel with content keeps it", func(t *testing.T) {
		// The counterweight: a version that always emitted one blank line and dropped the
		// content would satisfy every case above.
		out := RenderWithTitle(border, styles.Border.GetBorderTopForeground(), "Title", "real content", 40, 6)
		if !strings.Contains(out, "real content") {
			t.Errorf("the panel lost its content:\n%q", out)
		}
	})
}
