package grid

import (
	"fmt"
	"testing"

	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/theme"
)

func newMouseTestGrid(width int, widths []int, rows int) *Grid {
	styles := theme.Resolve("dark").Styles()
	g := New(styles, 100, nil)

	g.columns = make([]string, len(widths))
	dataRows := make([][]interface{}, rows)
	for i := 0; i < rows; i++ {
		row := make([]interface{}, len(widths))
		for j := range widths {
			row[j] = i*10 + j
		}
		dataRows[i] = row
	}
	for j := range widths {
		g.columns[j] = fmt.Sprintf("c%d", j)
	}

	g.data = &postgres.QueryResult{
		Columns: nil,
		Rows:    dataRows,
		Count:   rows,
	}
	g.width = width
	g.widths = append([]int(nil), widths...)
	g.focused = true
	return g
}

func TestGrid_HandleClick_ColumnMapping(t *testing.T) {
	// width 40 -> inner width 38; columns are 10, 12, 14 (sum 36).
	// contentX = x-1, and each column spans its width.
	tests := []struct {
		name string
		x    int
		want int // expected cursorCol, -1 means column must stay unchanged
	}{
		{"left border", 0, -1},
		{"first column start", 1, 0},
		{"first column end", 10, 0},
		{"second column start", 11, 1},
		{"second column end", 22, 1},
		{"third column start", 23, 2},
		{"third column end", 36, 2},
		{"past last column", 37, -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := newMouseTestGrid(40, []int{10, 12, 14}, 5)
			g.cursorCol = 2 // sentinel to detect "unchanged"

			cmd, hit := g.HandleClick(tt.x, 2) // y=2 -> first data row
			if !hit {
				t.Fatalf("HandleClick(%d, 2) did not hit a cell", tt.x)
			}
			if cmd != nil {
				t.Fatalf("HandleClick(%d, 2) returned a command, want nil", tt.x)
			}
			if g.cursorRow != 0 {
				t.Fatalf("cursorRow = %d, want 0", g.cursorRow)
			}
			want := tt.want
			if want == -1 {
				want = 2 // sentinel preserved
			}
			if g.cursorCol != want {
				t.Fatalf("cursorCol = %d, want %d", g.cursorCol, want)
			}
		})
	}
}

func TestGrid_HandleClick_RowMappingKeepsScroll(t *testing.T) {
	g := newMouseTestGrid(40, []int{10, 12, 14}, 10)
	g.scrollRow = 3
	g.scrollCol = 0

	cmd, hit := g.HandleClick(1, 4) // header at y=1, rows start at y=2 -> dataY=2
	if !hit || cmd != nil {
		t.Fatalf("HandleClick returned (cmd=%v, hit=%v), want (nil, true)", cmd, hit)
	}
	if g.cursorRow != 2 {
		t.Fatalf("cursorRow = %d, want 2", g.cursorRow)
	}
	if g.scrollRow != 3 {
		t.Fatalf("scrollRow = %d, want 3 (click must not jump to page start)", g.scrollRow)
	}
}

func TestGrid_HandleClick_OutOfRange(t *testing.T) {
	g := newMouseTestGrid(40, []int{10, 12, 14}, 5)

	tests := []struct {
		name string
		x, y int
	}{
		{"top border", 1, 0},
		{"row past visible rows", 1, 2 + 5},
		{"far below", 1, 999},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g.cursorRow = 1
			g.cursorCol = 1
			cmd, hit := g.HandleClick(tt.x, tt.y)
			if hit || cmd != nil {
				t.Fatalf("HandleClick(%d, %d) = (cmd=%v, hit=%v), want (nil, false)", tt.x, tt.y, cmd, hit)
			}
			if g.cursorRow != 1 || g.cursorCol != 1 {
				t.Fatalf("cursor moved to (%d,%d), want unchanged (1,1)", g.cursorRow, g.cursorCol)
			}
		})
	}
}

func TestGrid_HandleClick_HeaderRow(t *testing.T) {
	g := newMouseTestGrid(40, []int{10, 12, 14}, 5)
	g.header.SetColumns(g.columns)
	g.header.SetWidths(g.widths)

	if cmd, hit := g.HandleClick(24, 1); cmd == nil || hit {
		t.Fatalf("header click = (cmd=%v, hit=%v), want (non-nil, false)", cmd, hit)
	}
	if got := g.header.SortColumn(); got != "c2" {
		t.Fatalf("SortColumn() = %q, want %q", got, "c2")
	}
}

func TestGrid_HandleClick_ScrolledHorizontally(t *testing.T) {
	// Make the first column too wide to fit back in so syncScroll can't expand
	// the window left; visible columns from scrollCol=1 are 12 and 14 (sum 26).
	g := newMouseTestGrid(28, []int{30, 12, 14}, 5)
	g.scrollCol = 1

	cmd, hit := g.HandleClick(1, 2) // first visible column (absolute index 1)
	if !hit || cmd != nil {
		t.Fatalf("HandleClick returned (cmd=%v, hit=%v), want (nil, true)", cmd, hit)
	}
	if g.cursorCol != 1 {
		t.Fatalf("cursorCol = %d, want 1", g.cursorCol)
	}
	if g.scrollCol != 1 {
		t.Fatalf("scrollCol = %d, want 1", g.scrollCol)
	}
}

func TestGrid_HandleClick_HeaderOffsetWithDrafts(t *testing.T) {
	g := newMouseTestGrid(40, []int{10, 12, 14}, 5)
	g.pendingUpdates = []PendingUpdate{{RowIdx: 0, ColIdx: 0, OldValue: 1, NewValue: 2}}

	// The draft prefix adds one line, so the header moves to y=2 and data to y=3.
	if _, hit := g.HandleClick(1, 2); hit {
		t.Fatal("y=2 with a draft prefix should be the header, not a data cell")
	}
	if cmd, hit := g.HandleClick(1, 3); !hit || cmd != nil {
		t.Fatalf("HandleClick(1, 3) = (cmd=%v, hit=%v), want (nil, true)", cmd, hit)
	}
	if g.cursorRow != 0 {
		t.Fatalf("cursorRow = %d, want 0", g.cursorRow)
	}
}
