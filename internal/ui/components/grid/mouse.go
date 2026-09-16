package grid

import tea "charm.land/bubbletea/v2"

type MouseHandler struct {
	grid *Grid
}

func NewMouseHandler(grid *Grid) *MouseHandler {
	return &MouseHandler{grid: grid}
}

// HandleClick handles a click at coordinates relative to the grid's bordered
// box (0,0 = its top-left cell). It returns any command produced (a sort
// request when the header was clicked) and whether a data cell was hit.
func (mh *MouseHandler) HandleClick(x, y int) (tea.Cmd, bool) {
	if mh.grid.data == nil {
		return nil, false
	}

	headerY := mh.headerRowY()
	if y == headerY {
		if vj := mh.visibleColumnAtX(x); vj >= 0 {
			return mh.toggleSort(vj), false
		}
		return nil, false
	}
	if y < headerY {
		return nil, false
	}

	dataY := y - headerY - 1
	rows := mh.grid.visibleRows()
	if dataY < 0 || dataY >= rows {
		return nil, false
	}

	mh.grid.cursorRow = dataY
	if vj := mh.visibleColumnAtX(x); vj >= 0 {
		mh.grid.cursorCol = mh.grid.scrollCol + vj
	}
	mh.grid.syncScroll()
	return nil, true
}

// headerRowY is the box-relative line of the column header: the top border plus
// any prefix/filter lines rendered above it.
func (mh *MouseHandler) headerRowY() int {
	return 1 + mh.grid.recordsHeaderOffset()
}

// visibleColumnAtX maps a box-relative x to the index (within the visible
// columns) of the column under it, or -1 if it falls on the border or past the
// last visible column.
func (mh *MouseHandler) visibleColumnAtX(x int) int {
	contentX := x - 1 // skip the left border
	if contentX < 0 {
		return -1
	}
	available := mh.grid.width - 2
	if available <= 0 {
		return -1
	}

	acc := 0
	for vj, i := 0, mh.grid.scrollCol; i < len(mh.grid.columns) && i < len(mh.grid.widths); vj, i = vj+1, i+1 {
		w := mh.grid.widths[i]
		if acc+w > available {
			break
		}
		if contentX < acc+w {
			return vj
		}
		acc += w
	}
	return -1
}

func (mh *MouseHandler) toggleSort(visibleCol int) tea.Cmd {
	mh.grid.header.ToggleSort(mh.grid.scrollCol + visibleCol)
	sortCol := mh.grid.header.SortColumn()
	sortDir := mh.grid.header.SortDirection()

	return func() tea.Msg {
		return GridSortApplyMsg{
			Schema:   mh.grid.schema,
			Table:    mh.grid.tableName,
			OrderBy:  sortCol,
			OrderDir: sortDir,
			Where:    mh.grid.whereClause,
		}
	}
}

func (mh *MouseHandler) HandleScrollUp() bool {
	if !mh.grid.focused {
		return false
	}
	mh.grid.moveUp()
	return true
}

func (mh *MouseHandler) HandleScrollDown() bool {
	if !mh.grid.focused {
		return false
	}
	mh.grid.moveDown()
	return true
}
