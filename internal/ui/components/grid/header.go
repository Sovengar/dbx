package grid

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/buble/dbx/internal/theme"
	"github.com/charmbracelet/x/ansi"
)

type SortDirection int

const (
	SortNone SortDirection = iota
	SortAsc
	SortDesc
)

type KeyIcon int

const (
	KeyNone KeyIcon = iota
	KeyPK
	KeyFK
)

type Header struct {
	styles    *theme.Styles
	columns   []string
	sortCol   int
	sortDir   SortDirection
	widths    []int
	keyIcons  map[int]KeyIcon
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

func (h *Header) SetKeyIcons(icons map[int]KeyIcon) {
	h.keyIcons = icons
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

// Render renders `count` columns starting at the absolute column index
// `startCol`. Key icons and the sort indicator are keyed by absolute column
// index, so a horizontally scrolled grid still shows them on the right column.
func (h *Header) Render(startCol, count int) string {
	if len(h.columns) == 0 || count <= 0 {
		return ""
	}

	var cells []string
	for j := 0; j < count; j++ {
		i := startCol + j
		if i < 0 || i >= len(h.columns) {
			break
		}
		col := h.columns[i]

		width := 20
		if i < len(h.widths) {
			width = h.widths[i]
		}

		label := col
		if icon, ok := h.keyIcons[i]; ok {
			switch icon {
			case KeyPK:
				label = "* " + col
			case KeyFK:
				label = "→ " + col
			}
		}

		if i == h.sortCol {
			switch h.sortDir {
			case SortAsc:
				label = label + " ↑"
			case SortDesc:
				label = label + " ↓"
			}
		}

		maxTextWidth := width - 2
		if maxTextWidth < 0 {
			maxTextWidth = 0
		}
		if lipgloss.Width(label) > maxTextWidth {
			if maxTextWidth <= 3 {
				label = label[:maxTextWidth]
			} else {
				label = ansi.Truncate(label, maxTextWidth, "...")
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
