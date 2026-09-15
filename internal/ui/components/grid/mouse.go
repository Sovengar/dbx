package grid

import tea "charm.land/bubbletea/v2"

type MouseHandler struct {
	grid *Grid
}

func NewMouseHandler(grid *Grid) *MouseHandler {
	return &MouseHandler{grid: grid}
}

func (mh *MouseHandler) HandleClick(x, y int) bool {
	if !mh.grid.focused {
		return false
	}

	headerHeight := 2
	if y < headerHeight {
		return false
	}

	dataY := y - headerHeight
	ch := mh.grid.contentHeight()
	totalRows := mh.grid.visibleRows()
	if dataY >= ch && dataY >= totalRows {
		return false
	}

	mh.grid.scrollRow = 0
	mh.grid.cursorRow = dataY
	if mh.grid.cursorRow >= totalRows {
		mh.grid.cursorRow = totalRows - 1
	}
	return true
}

func (mh *MouseHandler) HandleHeaderClick(x int) tea.Cmd {
	if !mh.grid.focused {
		return nil
	}

	colX := 0
	for i, w := range mh.grid.widths {
		if x >= colX && x < colX+w {
			mh.grid.header.ToggleSort(i)

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
		colX += w
	}
	return nil
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
