package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/theme"
)

type HelpModal struct {
	styles   *theme.Styles
	keybinds config.Resolver
	visible  bool
	scroll   int
	width    int
	height   int
}

func NewHelpModal(styles *theme.Styles, keybinds config.Resolver) *HelpModal {
	return &HelpModal{
		styles:   styles,
		keybinds: keybinds,
	}
}

func (m *HelpModal) Show() {
	m.visible = true
	m.scroll = 0
}

func (m *HelpModal) Hide() {
	m.visible = false
}

func (m *HelpModal) IsVisible() bool {
	return m.visible
}

func (m *HelpModal) SetWidth(w int)  { m.width = w }
func (m *HelpModal) SetHeight(h int) { m.height = h }

func (m *HelpModal) Update(msg tea.Msg) (tea.Cmd, bool) {
	if !m.visible {
		return nil, false
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		key := msg.String()
		switch key {
		// Overlay exception (ADR 0002 §2): the help modal's close and scroll
		// keys stay raw on purpose. The modal is an overlay, not a registry
		// context, so these are documented here instead of promoted into the
		// keybind inventory.
		case "esc", "?", "q":
			m.Hide()
			return nil, true
		case "up", "k":
			if m.scroll > 0 {
				m.scroll--
			}
			return nil, true
		case "down", "j":
			m.scroll++
			return nil, true
		case "ctrl+u":
			m.scroll -= 10
			if m.scroll < 0 {
				m.scroll = 0
			}
			return nil, true
		case "ctrl+d":
			m.scroll += 10
			return nil, true
		case "g":
			m.scroll = 0
			return nil, true
		case "G":
			m.scroll = 1000
			return nil, true
		}
	case tea.MouseWheelMsg:
		if msg.Y < 0 {
			m.scroll--
			if m.scroll < 0 {
				m.scroll = 0
			}
		} else {
			m.scroll++
		}
		return nil, true
	}

	return nil, false
}

// modalSections are the registry-backed sections, in display order, each with
// the human header and the context it renders.
var modalSections = []struct {
	Header  string
	Context string
}{
	{"Explorer", config.ContextExplorer},
	{"Grid", config.ContextGrid},
	{"Grid Preview", config.ContextGridPreview},
	{"Explorer Preview", config.ContextExplorerPreview},
	{"Editor", config.ContextEditor},
}

func (m *HelpModal) View() string {
	if !m.visible {
		return ""
	}

	modalW := m.width * 7 / 10
	if modalW < 50 {
		modalW = 50
	}
	if modalW > 90 {
		modalW = 90
	}

	contentW := modalW - 4

	var lines []string

	for _, sec := range modalSections {
		lines = append(lines, m.styles.Header.Render(sec.Header))
		for _, g := range config.GroupActions(m.keybinds.ActionsFor(sec.Context), m.keybinds.PrimaryKey) {
			if g.Grouped {
				lines = append(lines, m.groupLabelLine(g.Label))
				for _, a := range g.Members {
					lines = append(lines, m.renderAction(a))
				}
				continue
			}
			lines = append(lines, m.renderAction(g.Members[0]))
		}
		lines = append(lines, "")
	}

	lines = append(lines, m.styles.Header.Render("Query Browser"))
	lines = append(lines, m.staticLine("j/k", "Navigate up/down"))
	lines = append(lines, m.staticLine("g/G", "First/last entry"))
	lines = append(lines, m.staticLine("Enter", "Load query into editor"))
	lines = append(lines, m.staticLine("f", "Toggle favorite"))
	lines = append(lines, m.staticLine("d", "Delete entry"))
	lines = append(lines, m.staticLine("/", "Filter queries"))
	lines = append(lines, m.staticLine("Tab", "Switch History/Favorites"))
	lines = append(lines, m.staticLine("Esc", "Close browser"))
	lines = append(lines, "")

	lines = append(lines, m.styles.Header.Render("Connection Picker"))
	lines = append(lines, m.staticLine("j/k", "Navigate up/down"))
	lines = append(lines, m.staticLine("Space", "Toggle active/inactive"))
	lines = append(lines, m.staticLine("Enter", "Connect to selected project"))
	lines = append(lines, m.staticLine("q", "Quit"))
	lines = append(lines, "")

	lines = append(lines, m.styles.Header.Render("EDIT Mode (grid cell)"))
	lines = append(lines, m.staticLine("Esc", "Cancel edit (revert value)"))
	lines = append(lines, m.staticLine("Enter/Tab", "Commit cell, move to next column"))
	lines = append(lines, m.staticLine("Up/Down", "Move to prev/next row"))
	lines = append(lines, m.staticLine("←/→", "Move cursor within the cell"))
	lines = append(lines, m.staticLine("Home/End", "Jump to start/end of value"))
	lines = append(lines, m.staticLine("Backspace/Delete", "Delete character"))
	lines = append(lines, m.staticLine("Any char", "Type into cell"))
	lines = append(lines, "")

	lines = append(lines, m.styles.Header.Render("FILTER Mode (column find)"))
	lines = append(lines, m.staticLine("Esc", "Cancel filter"))
	lines = append(lines, m.staticLine("Enter", "Apply filter, jump to first match"))
	lines = append(lines, m.staticLine("Any char", "Append to filter text"))
	lines = append(lines, "")

	lines = append(lines, m.styles.Header.Render("WHERE FILTER Mode"))
	lines = append(lines, m.staticLine("Esc", "Cancel WHERE filter"))
	lines = append(lines, m.staticLine("Enter", "Apply WHERE clause (re-query DB)"))
	lines = append(lines, m.staticLine("Any char", "Type the WHERE clause"))
	lines = append(lines, "")

	lines = append(lines, m.styles.Header.Render("Mouse"))
	lines = append(lines, m.staticLine("Click", "Select node/cell, move cursor"))
	lines = append(lines, m.staticLine("Double click", "Expand/collapse, view full cell"))
	lines = append(lines, m.staticLine("Wheel", "Scroll the focused pane"))
	lines = append(lines, "")

	lines = append(lines, m.styles.Help.Render("  j/k scroll · Esc close"))

	maxVisible := m.height - 10
	if maxVisible < 5 {
		maxVisible = 5
	}

	if m.scroll > len(lines)-maxVisible {
		m.scroll = len(lines) - maxVisible
	}
	if m.scroll < 0 {
		m.scroll = 0
	}

	end := m.scroll + maxVisible
	if end > len(lines) {
		end = len(lines)
	}

	visibleLines := lines[m.scroll:end]
	content := strings.Join(visibleLines, "\n")

	modal := m.styles.BorderActive.
		Width(contentW).
		Render(m.styles.Header.Render("Help") + "\n" + content)

	return modal
}

func (m *HelpModal) renderAction(a config.Action) string {
	key := strings.Join(a.Keys, ", ")
	if key == "" {
		key = "?"
	}
	return m.staticLine(key, a.Description)
}

// groupLabelLine renders the shared label a collapsed group's members sit under.
func (m *HelpModal) groupLabelLine(label string) string {
	return "  " + m.styles.TextMuted.Render(label)
}

func (m *HelpModal) staticLine(key, description string) string {
	keyStyled := m.styles.Primary.Render(fmt.Sprintf("%-14s", key))
	descStyled := m.styles.Text.Render(description)
	return "  " + keyStyled + descStyled
}
