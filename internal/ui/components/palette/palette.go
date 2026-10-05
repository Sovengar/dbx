package palette

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/theme"
)

type CommandSelectedMsg struct {
	Action config.ActionID
}

type Palette struct {
	styles       *theme.Styles
	commands     []command
	filtered     []scoredCommand
	query        string
	cursor       int
	scrollOffset int
	visible      bool
	width        int
	height       int
}

func New(styles *theme.Styles, keybindings config.Resolver) *Palette {
	cmds := BuildCommands(keybindings)
	return &Palette{
		styles:   styles,
		commands: cmds,
	}
}

func (p *Palette) Show() {
	p.visible = true
	p.query = ""
	p.cursor = 0
	p.scrollOffset = 0
	p.updateFiltered()
}

func (p *Palette) Hide() {
	p.visible = false
	p.query = ""
}

func (p *Palette) IsVisible() bool {
	return p.visible
}

func (p *Palette) SetWidth(w int) {
	p.width = w
}

func (p *Palette) SetHeight(h int) {
	p.height = h
}

func (p *Palette) updateFiltered() {
	p.filtered = fuzzySort(p.query, p.commands)
	p.scrollOffset = 0
	if p.cursor >= len(p.filtered) {
		p.cursor = len(p.filtered) - 1
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
}

func (p *Palette) Update(msg tea.Msg) (tea.Cmd, bool) {
	if !p.visible {
		return nil, false
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return p.handleKey(msg)
	}

	return nil, false
}

func (p *Palette) handleKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	key := msg.String()

	switch key {
	case "esc":
		p.Hide()
		return nil, true
	case "enter":
		if len(p.filtered) > 0 && p.cursor >= 0 && p.cursor < len(p.filtered) {
			action := p.filtered[p.cursor].command.Action
			p.Hide()
			return func() tea.Msg {
				return CommandSelectedMsg{Action: action}
			}, true
		}
		return nil, true
	case "up", "k":
		if p.cursor > 0 {
			p.cursor--
		}
		return nil, true
	case "down", "j":
		if p.cursor < len(p.filtered)-1 {
			p.cursor++
		}
		return nil, true
	case "ctrl+u":
		p.query = ""
		p.cursor = 0
		p.updateFiltered()
		return nil, true
	case "backspace":
		// The query is TEXT, not bytes: a terminal delivers an accented
		// character as one keystroke carrying several bytes, and slicing one
		// byte off leaves half a rune behind — which renders as a replacement
		// character and filters against bytes that spell nothing.
		if runes := []rune(p.query); len(runes) > 0 {
			p.query = string(runes[:len(runes)-1])
			p.updateFiltered()
		}
		return nil, true
	case "space":
		p.query += " "
		p.updateFiltered()
		return nil, true
	case "tab":
		return nil, true
	default:
		if len(msg.Text) > 0 && msg.Text != " " {
			p.query += msg.Text
			p.updateFiltered()
			return nil, true
		}
	}

	return nil, false
}

func (p *Palette) View() string {
	if !p.visible {
		return ""
	}

	modalW := p.width * 6 / 10
	if modalW < 40 {
		modalW = 40
	}
	if modalW > 80 {
		modalW = 80
	}

	inputW := modalW - 4

	input := p.styles.BorderActive.
		Width(inputW).
		Padding(0, 1).
		Render(p.styles.Text.Render(":" + p.query + "_"))

	var listLines []string
	var cmdLineMap []int
	sectionOrder := []CommandSection{SectionDatabase, SectionQuery, SectionUI, SectionNav}

	usedSections := make(map[CommandSection]bool)
	for _, sc := range p.filtered {
		usedSections[sc.command.Section] = true
	}

	cmdIdx := 0
	firstSection := true
	for _, section := range sectionOrder {
		if !usedSections[section] {
			continue
		}

		if !firstSection {
			listLines = append(listLines, "")
		}
		firstSection = false

		header := p.styles.Header.Render(string(section))
		listLines = append(listLines, "  "+header)

		for _, sc := range p.filtered {
			if sc.command.Section != section {
				continue
			}

			item := p.styles.Text.Render(sc.command.Name)

			padLen := inputW - lipgloss.Width(sc.command.Name) - 2
			if padLen > 0 {
				item += strings.Repeat(" ", padLen)
			}

			if cmdIdx == p.cursor {
				item = p.styles.Selected.Width(inputW).Render(sc.command.Name)
			}

			cmdLineMap = append(cmdLineMap, len(listLines))
			listLines = append(listLines, "  "+item)
			cmdIdx++
		}
	}

	if len(listLines) == 0 {
		listLines = append(listLines, "  "+p.styles.TextMuted.Render("No matching commands"))
	}

	maxVisible := 12

	// scroll to keep cursor visible
	if len(cmdLineMap) > 0 && p.cursor >= 0 && p.cursor < len(cmdLineMap) {
		cursorLine := cmdLineMap[p.cursor]

		if cursorLine < p.scrollOffset {
			p.scrollOffset = cursorLine
		}
		if cursorLine >= p.scrollOffset+maxVisible {
			p.scrollOffset = cursorLine - maxVisible + 1
		}
	}

	// Clamp scrollOffset — two branches, not three. The `else if p.scrollOffset >
	// totalLines-maxVisible` one used to be here and could never fire: the branch above
	// always runs (cmdLineMap has exactly one entry per filtered command, and updateFiltered
	// keeps the cursor inside that range, so `p.cursor >= 0 && p.cursor < len(cmdLineMap)`
	// holds for every state the palette can be in), and it assigns
	// `cursorLine - maxVisible + 1`, where cursorLine is at most totalLines-1. So the value
	// it writes is at most totalLines-maxVisible already.
	//
	// Which means the invariant the third branch was checking is maintained by the writer,
	// and TestTheScrollOffsetIsAlwaysLegal is what holds it now: it walks every cursor
	// position at every list length rather than trusting the arithmetic above.
	totalLines := len(listLines)
	if totalLines <= maxVisible {
		p.scrollOffset = 0
	}
	if p.scrollOffset < 0 {
		p.scrollOffset = 0
	}

	visible := listLines
	if totalLines > maxVisible {
		visible = listLines[p.scrollOffset : p.scrollOffset+maxVisible]
	}

	list := strings.Join(visible, "\n")

	content := input + "\n" + list

	modal := p.styles.BorderActive.
		Width(modalW - 2).
		Render(content)

	return modal
}
