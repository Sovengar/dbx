package grid

import (
	"fmt"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/buble/dbx/internal/theme"
	"github.com/charmbracelet/x/ansi"
)

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*m`)

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

	var cell string
	if selected {
		cell = cr.styles.Selected.
			PaddingLeft(1).
			PaddingRight(1).
			Width(width).
			Render(truncated)
	} else {
		cell = cr.styles.Text.
			PaddingLeft(1).
			PaddingRight(1).
			Width(width).
			Render(truncated)
	}

	// DEBUG
	f, _ := os.OpenFile("/tmp/dbx_cell_debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if f != nil {
		fmt.Fprintf(f, "  RenderCell: width=%d rawLen=%d truncatedLen=%d cellLen=%d value=%v\n", width, lipgloss.Width(raw), lipgloss.Width(truncated), lipgloss.Width(cell), value)
		f.Close()
	}

	return cell
}

func (cr *CellRenderer) RenderRow(values []interface{}, widths []int, selected bool) string {
	if len(values) != len(widths) {
		// DEBUG
		f, _ := os.Create("/tmp/dbx_cell_debug.log")
		if f != nil {
			fmt.Fprintf(f, "RenderRow MISMATCH: values=%d widths=%d\n", len(values), len(widths))
			f.Close()
		}
		return ""
	}

	var cells []string
	for i, val := range values {
		cells = append(cells, cr.RenderCell(val, widths[i], selected))
	}

	result := strings.Join(cells, "")

	// DEBUG
	f, _ := os.OpenFile("/tmp/dbx_cell_debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if f != nil {
		fmt.Fprintf(f, "RenderRow OK: values=%d widths=%d resultWidth=%d selected=%v\n", len(values), len(widths), lipgloss.Width(result), selected)
		f.Close()
	}

	return result
}
