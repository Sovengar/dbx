package explorer_test

// The explorer lane and the grid lane share one contentHeight, computed once by
// the app, and are stacked with the keybinds pane below them. If the explorer
// renders even one line more than the grid for the same height, everything
// after it is pushed down and the keybinds pane is cut.
//
// Both are bordered boxes drawn by internal/ui/bordered, whose `height` argument
// is the TOTAL box height in lines, borders included. So the parity is entirely
// a property of the number each View passes as that height: Grid.View passes
// g.height, and Explorer.View must pass e.height too. This test pins that down
// from outside the package, by building both panes at the same height and
// comparing the rendered line counts.
//
// It lives in package explorer_test because internal/drivers/postgres imports
// the explorer package (schema extraction); an internal test importing grid
// would drag postgres in and cycle.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/theme"
	"github.com/buble/dbx/internal/ui/components/explorer"
	"github.com/buble/dbx/internal/ui/components/grid"
)

// renderedLines counts the lines a pane occupies on screen. Every line of a
// bordered box carries its left and right border characters, so a trimmed-empty
// line count is not what we want here — the split length is.
func renderedLines(s string) int {
	return len(strings.Split(ansi.Strip(s), "\n"))
}

func TestExplorerAndGridPanesOccupyTheSameVerticalSpace(t *testing.T) {
	styles := theme.Resolve("dark").Styles()
	registry := config.NewKeybindRegistry(config.KeybindingsConfig{})

	// Enough rows on both sides that the panes overflow a short height, so the
	// borders are what clip rather than the content falling short.
	newExplorer := func(h int) *explorer.Explorer {
		e := explorer.New(styles, nil, registry)
		e.SetWidth(34)
		e.SetHeight(h)
		schema := explorer.NewNode("sch", explorer.NodeSchema, "public")
		schema.Expanded = true
		for i := range 20 {
			schema.AddChild(explorer.NewNode("t", explorer.NodeTable, fmt.Sprintf("t%d", i)))
		}
		e.SetNodes([]*explorer.Node{schema})
		return e
	}

	newGrid := func(h int) *grid.Grid {
		g := grid.New(styles, 0, registry)
		g.SetWidth(80)
		g.SetHeight(h)
		g.Focus()
		cols := []postgres.ColumnInfo{{Name: "c0", DataType: "text"}, {Name: "c1", DataType: "text"}}
		rows := make([][]interface{}, 30)
		for r := range rows {
			rows[r] = []interface{}{fmt.Sprintf("r%d", r), "x"}
		}
		g.SetData(&postgres.QueryResult{Columns: cols, Rows: rows, Count: len(rows)}, "public", "t")
		return g
	}

	// The range crosses renderBox's floor (a box cannot be shorter than 3
	// lines) and normal heights, so a future offset on either side shows up.
	for _, h := range []int{3, 4, 5, 8, 12, 20, 30} {
		t.Run(fmt.Sprintf("height %d", h), func(t *testing.T) {
			explorerLines := renderedLines(newExplorer(h).View())
			gridLines := renderedLines(newGrid(h).View())
			if explorerLines != gridLines {
				t.Errorf("at height %d the explorer rendered %d lines and the grid %d; the panes must match",
					h, explorerLines, gridLines)
			}
			// And both are exactly the height they were given (never below the
			// 3-line floor), so parity is not two panes agreeing on a wrong
			// number.
			want := h
			if want < 3 {
				want = 3
			}
			if explorerLines != want {
				t.Errorf("at height %d the explorer rendered %d lines, want %d", h, explorerLines, want)
			}
		})
	}
}
