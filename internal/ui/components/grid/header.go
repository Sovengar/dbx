package grid

import (
	"strings"

	"github.com/buble/dbx/internal/theme"
)

type SortDirection int

const (
	SortNone SortDirection = iota
	SortAsc
	SortDesc
)

type Header struct {
	styles    *theme.Styles
	columns   []string
	sortCol   int
	sortDir   SortDirection
	widths    []int
}

func NewHeader(styles *theme.Styles) *Header {
	return &Header{
		styles:  styles,
		sortCol: -1,
		sortDir: SortNone,
	}
}

func (h *Header) SetColumns(columns []string) {
	h.columns = columns
}

func (h *Header) SetWidths(widths []int) {
	h.widths = widths
}

func (h *Header) ToggleSort(col int) SortDirection {
	if h.sortCol == col {
		switch h.sortDir {
		case SortNone:
			h.sortDir = SortAsc
		case SortAsc:
			h.sortDir = SortDesc
		case SortDesc:
			h.sortDir = SortNone
			h.sortCol = -1
		}
	} else {
		h.sortCol = col
		h.sortDir = SortAsc
	}
	return h.sortDir
}

func (h *Header) SortColumn() string {
	if h.sortCol >= 0 && h.sortCol < len(h.columns) {
		return h.columns[h.sortCol]
	}
	return ""
}

func (h *Header) SortDirection() string {
	switch h.sortDir {
	case SortAsc:
		return "ASC"
	case SortDesc:
		return "DESC"
	default:
		return ""
	}
}

func (h *Header) ClearSort() {
	h.sortCol = -1
	h.sortDir = SortNone
}

func (h *Header) Render() string {
	if len(h.columns) == 0 {
		return ""
	}

	var cells []string
	for i, col := range h.columns {
		width := 20
		if i < len(h.widths) {
			width = h.widths[i]
		}

		label := col
		if i == h.sortCol {
			switch h.sortDir {
			case SortAsc:
				label = col + " ↑"
			case SortDesc:
				label = col + " ↓"
			}
		}

		cell := h.styles.Header.
			Width(width).
			PaddingLeft(1).
			PaddingRight(1).
			Render(strings.ToUpper(label))
		cells = append(cells, cell)
	}

	return strings.Join(cells, "")
}
