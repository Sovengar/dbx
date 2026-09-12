package gridpreview

import (
	"encoding/json"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/buble/dbx/internal/theme"
)

type GridPreview struct {
	styles   *theme.Styles
	keybinds map[string]string
	lines    []string
	scrollY  int
	width    int
	height   int
	focused  bool
}

func New(styles *theme.Styles, keybinds map[string]string) *GridPreview {
	return &GridPreview{
		styles:   styles,
		keybinds: keybinds,
	}
}

func (p *GridPreview) SetWidth(w int)  { p.width = w }
func (p *GridPreview) SetHeight(h int) { p.height = h }
func (p *GridPreview) Focus()          { p.focused = true }
func (p *GridPreview) Blur()           { p.focused = false }
func (p *GridPreview) IsFocused() bool { return p.focused }

func (p *GridPreview) SetRow(columns []string, row []interface{}) {
	if row == nil || len(columns) == 0 {
		p.lines = nil
		return
	}

	data := make(map[string]interface{})
	for i, col := range columns {
		if i < len(row) {
			data[col] = row[i]
		}
	}

	jsonBytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		p.lines = []string{fmt.Sprintf("Error: %v", err)}
		return
	}

	raw := string(jsonBytes)
	p.lines = strings.Split(raw, "\n")
	p.scrollY = 0
}

func (p *GridPreview) scrollUp() {
	if p.scrollY > 0 {
		p.scrollY--
	}
}

func (p *GridPreview) scrollDown() {
	maxScroll := len(p.lines) - p.height + 6
	if maxScroll < 0 {
		maxScroll = 0
	}
	if p.scrollY < maxScroll {
		p.scrollY++
	}
}

func (p *GridPreview) halfPageUp() {
	p.scrollY -= p.height / 2
	if p.scrollY < 0 {
		p.scrollY = 0
	}
}

func (p *GridPreview) halfPageDown() {
	p.scrollY += p.height / 2
	maxScroll := len(p.lines) - p.height + 6
	if maxScroll < 0 {
		maxScroll = 0
	}
	if p.scrollY > maxScroll {
		p.scrollY = maxScroll
	}
}

func (p *GridPreview) firstLine() {
	p.scrollY = 0
}

func (p *GridPreview) lastLine() {
	maxScroll := len(p.lines) - p.height + 6
	if maxScroll < 0 {
		maxScroll = 0
	}
	p.scrollY = maxScroll
}

func (p *GridPreview) Update(msg tea.Msg) (tea.Cmd, bool) {
	if !p.focused {
		return nil, false
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		key := msg.String()

		switch key {
		case p.keybinds["grid-preview.scroll_up"]:
			p.scrollUp()
			return nil, true
		case p.keybinds["grid-preview.scroll_down"]:
			p.scrollDown()
			return nil, true
		case p.keybinds["grid-preview.first"]:
			p.firstLine()
			return nil, true
		case p.keybinds["grid-preview.last"]:
			p.lastLine()
			return nil, true
		case p.keybinds["grid-preview.half_up"]:
			p.halfPageUp()
			return nil, true
		case p.keybinds["grid-preview.half_down"]:
			p.halfPageDown()
			return nil, true
		}
	}

	return nil, false
}

func (p *GridPreview) Render() string {
	if p.width <= 0 {
		return ""
	}

	title := p.styles.Header.Render("Preview")

	contentHeight := p.height - 6

	if len(p.lines) == 0 {
		return title + "\n" + p.styles.Text.Render("  No data")
	}

	start := p.scrollY
	end := start + contentHeight
	if end > len(p.lines) {
		end = len(p.lines)
	}

	var rendered []string
	for _, line := range p.lines[start:end] {
		rendered = append(rendered, p.highlightJSON(line))
	}

	content := strings.Join(rendered, "\n")
	return title + "\n" + content
}

func (p *GridPreview) highlightJSON(line string) string {
	var result strings.Builder
	i := 0

	for i < len(line) {
		ch := line[i]

		if ch == '"' {
			end := strings.IndexByte(line[i+1:], '"')
			if end == -1 {
				result.WriteString(p.styles.Text.Render(line[i:]))
				break
			}
			end += i + 1

			rest := strings.TrimSpace(line[end+1:])
			if len(rest) > 0 && rest[0] == ':' {
				result.WriteString(p.styles.Primary.Render(line[i : end+1]))
			} else {
				result.WriteString(p.styles.Success.Render(line[i : end+1]))
			}
			i = end + 1
			continue
		}

		if ch >= '0' && ch <= '9' || ch == '-' {
			j := i + 1
			for j < len(line) && (line[j] >= '0' && line[j] <= '9' || line[j] == '.' || line[j] == '-' || line[j] == 'e' || line[j] == 'E' || line[j] == '+' || line[j] == 't' || line[j] == 'Z') {
				j++
			}
			result.WriteString(p.styles.Info.Render(line[i:j]))
			i = j
			continue
		}

		if strings.HasPrefix(line[i:], "null") {
			result.WriteString(p.styles.TextMuted.Render("null"))
			i += 4
			continue
		}

		if strings.HasPrefix(line[i:], "true") {
			result.WriteString(p.styles.Info.Render("true"))
			i += 4
			continue
		}

		if strings.HasPrefix(line[i:], "false") {
			result.WriteString(p.styles.Info.Render("false"))
			i += 5
			continue
		}

		result.WriteByte(ch)
		i++
	}

	return result.String()
}
