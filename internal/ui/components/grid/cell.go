package grid

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/buble/dbx/internal/theme"
	"github.com/charmbracelet/x/ansi"
)

type CellRenderer struct {
	styles *theme.Styles
}

func NewCellRenderer(styles *theme.Styles) *CellRenderer {
	return &CellRenderer{styles: styles}
}

func (cr *CellRenderer) FormatValue(value interface{}) string {
	if value == nil {
		return cr.styles.TextMuted.Render("NULL")
	}

	switch v := value.(type) {
	case []byte:
		return string(v)
	case string:
		return v
	case int, int32, int64:
		return fmt.Sprintf("%d", v)
	case float32, float64:
		return fmt.Sprintf("%g", v)
	case bool:
		if v {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", v)
	}
}

func (cr *CellRenderer) Truncate(text string, maxWidth int) string {
	if maxWidth <= 0 {
		return text
	}

	if lipgloss.Width(text) <= maxWidth {
		return text
	}

	tail := "..."
	if maxWidth <= 3 {
		tail = ""
	}
	return ansi.Truncate(text, maxWidth, tail)
}

func (cr *CellRenderer) RenderCell(value interface{}, width int, selected bool) string {
	raw := cr.FormatValue(value)
	truncated := cr.Truncate(raw, width-2)

	if selected {
		return cr.styles.Selected.
			PaddingLeft(1).
			PaddingRight(1).
			Width(width).
			Render(truncated)
	}
	return cr.styles.Text.
		PaddingLeft(1).
		PaddingRight(1).
		Width(width).
		Render(truncated)
}

func (cr *CellRenderer) RenderRow(values []interface{}, widths []int, activeCol int) string {
	var cells []string
	for i, val := range values {
		cells = append(cells, cr.RenderCell(val, widths[i], i == activeCol))
	}
	return strings.Join(cells, "")
}

func (cr *CellRenderer) RenderEditCell(value interface{}, width int, editValue string, cursorPos int) string {
	truncated := cr.Truncate(editValue, width-2)

	runes := []rune(truncated)
	if cursorPos > len(runes) {
		cursorPos = len(runes)
	}

	left := string(runes[:cursorPos])
	right := string(runes[cursorPos:])

	display := left + "█" + right

	cell := cr.styles.Selected.
		PaddingLeft(1).
		PaddingRight(1).
		Width(width).
		Render(display)

	return cell
}

func (cr *CellRenderer) RenderEditRow(values []interface{}, widths []int, editCol int, editValue string, editCursor int) string {
	var cells []string
	for i, val := range values {
		if i == editCol {
			cells = append(cells, cr.RenderEditCell(val, widths[i], editValue, editCursor))
		} else {
			cells = append(cells, cr.RenderCell(val, widths[i], false))
		}
	}
	return strings.Join(cells, "")
}
