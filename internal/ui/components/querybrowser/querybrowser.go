package querybrowser

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/buble/dbx/internal/store"
	"github.com/buble/dbx/internal/theme"
	"github.com/buble/dbx/internal/ui/bordered"
)

type QuerySelectedMsg struct {
	SQL string
}

type QueryBrowser struct {
	styles      *theme.Styles
	store       *store.QueryStore
	entries     []store.QueryEntry
	cursor      int
	scroll      int
	tab         int // 0=History, 1=Favorites
	visible     bool
	width       int
	height      int
	filter      string
	filterActive bool
}

func New(styles *theme.Styles, qs *store.QueryStore) *QueryBrowser {
	return &QueryBrowser{
		styles: styles,
		store:  qs,
	}
}

func (b *QueryBrowser) Show() {
	b.visible = true
	b.tab = 0
	b.cursor = 0
	b.scroll = 0
	b.filter = ""
	b.filterActive = false
	b.refreshEntries()
}

func (b *QueryBrowser) Hide() {
	b.visible = false
}

func (b *QueryBrowser) IsVisible() bool {
	return b.visible
}

func (b *QueryBrowser) SetWidth(w int)  { b.width = w }
func (b *QueryBrowser) SetHeight(h int) { b.height = h }

func (b *QueryBrowser) refreshEntries() {
	if b.tab == 0 {
		b.entries = b.store.All()
	} else {
		b.entries = b.store.Favorites()
	}
	if b.filter != "" {
		b.entries = b.filterEntries(b.entries)
	}
	if b.cursor >= len(b.entries) {
		b.cursor = len(b.entries) - 1
	}
	if b.cursor < 0 {
		b.cursor = 0
	}
}

func (b *QueryBrowser) filterEntries(entries []store.QueryEntry) []store.QueryEntry {
	needle := strings.ToLower(b.filter)
	var result []store.QueryEntry
	for _, e := range entries {
		if strings.Contains(strings.ToLower(e.SQL), needle) ||
			strings.Contains(strings.ToLower(e.Name), needle) {
			result = append(result, e)
		}
	}
	return result
}

func (b *QueryBrowser) Update(msg tea.Msg) (tea.Cmd, bool) {
	if !b.visible {
		return nil, false
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return b.handleKey(msg)
	}

	return nil, false
}

func (b *QueryBrowser) handleKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	key := msg.String()

	if b.filterActive {
		switch key {
		case "esc":
			b.filterActive = false
			b.filter = ""
			b.refreshEntries()
			return nil, true
		case "enter":
			b.filterActive = false
			b.refreshEntries()
			return nil, true
		case "backspace":
			if len(b.filter) > 0 {
				b.filter = b.filter[:len(b.filter)-1]
				b.refreshEntries()
			}
			return nil, true
		default:
			if len(msg.Text) > 0 && msg.Text != " " || key == "space" {
				if key == "space" {
					b.filter += " "
				} else {
					b.filter += msg.Text
				}
				b.refreshEntries()
				return nil, true
			}
		}
		return nil, true
	}

	switch key {
	case "esc":
		b.Hide()
		return nil, true

	case "tab":
		b.tab = (b.tab + 1) % 2
		b.cursor = 0
		b.scroll = 0
		b.refreshEntries()
		return nil, true

	case "j", "down":
		if b.cursor < len(b.entries)-1 {
			b.cursor++
			b.ensureVisible()
		}
		return nil, true

	case "k", "up":
		if b.cursor > 0 {
			b.cursor--
			b.ensureVisible()
		}
		return nil, true

	case "g":
		b.cursor = 0
		b.scroll = 0
		return nil, true

	case "G":
		b.cursor = len(b.entries) - 1
		if b.cursor < 0 {
			b.cursor = 0
		}
		b.ensureVisible()
		return nil, true

	case "enter":
		if b.cursor >= 0 && b.cursor < len(b.entries) {
			sql := b.entries[b.cursor].SQL
			b.Hide()
			return func() tea.Msg { return QuerySelectedMsg{SQL: sql} }, true
		}
		return nil, true

	case "f":
		if b.cursor >= 0 && b.cursor < len(b.entries) {
			b.store.ToggleFavorite(b.cursor)
			b.refreshEntries()
		}
		return nil, true

	case "d":
		if b.cursor >= 0 && b.cursor < len(b.entries) {
			b.store.Delete(b.cursor)
			b.refreshEntries()
		}
		return nil, true

	case "/":
		b.filterActive = true
		b.filter = ""
		return nil, true
	}

	return nil, false
}

func (b *QueryBrowser) ensureVisible() {
	maxVisible := b.maxVisibleEntries()
	if b.cursor < b.scroll {
		b.scroll = b.cursor
	}
	if b.cursor >= b.scroll+maxVisible {
		b.scroll = b.cursor - maxVisible + 1
	}
}

func (b *QueryBrowser) maxVisibleEntries() int {
	h := b.height - 10 // title + tabs + filter + footer
	if h < 3 {
		h = 3
	}
	return h
}

func (b *QueryBrowser) View() string {
	if !b.visible {
		return ""
	}

	modalW := b.width * 6 / 10
	if modalW < 50 {
		modalW = 50
	}
	if modalW > 90 {
		modalW = 90
	}

	contentW := modalW - 4
	maxVisible := b.maxVisibleEntries()

	var lines []string

	// Tabs
	historyTab := "History"
	favoritesTab := "Favorites"
	if b.tab == 0 {
		historyTab = b.styles.Primary.Render("► History")
		favoritesTab = b.styles.TextMuted.Render("  Favorites")
	} else {
		historyTab = b.styles.TextMuted.Render("  History")
		favoritesTab = b.styles.Primary.Render("► Favorites")
	}
	lines = append(lines, historyTab+"  "+favoritesTab)
	lines = append(lines, "")

	// Filter
	if b.filterActive {
		lines = append(lines, b.styles.Primary.Render("/ "+b.filter+"_"))
		lines = append(lines, "")
	}

	// Empty state
	if len(b.entries) == 0 {
		emptyMsg := "No queries yet"
		if b.tab == 1 {
			emptyMsg = "No favorites — press 'f' to add"
		}
		if b.filter != "" {
			emptyMsg = "No matches"
		}
		lines = append(lines, "  "+b.styles.TextMuted.Render(emptyMsg))
	} else {
		end := b.scroll + maxVisible
		if end > len(b.entries) {
			end = len(b.entries)
		}

		for i := b.scroll; i < end; i++ {
			e := b.entries[i]
			line := b.renderEntry(i, e, contentW)
			lines = append(lines, line)
		}
	}

	// Pad to fill
	for len(lines) < maxVisible+5 {
		lines = append(lines, "")
	}

	// Footer
	footer := " j/k navigate · Enter load · f favorite · d delete · / filter · Tab switch · Esc close"
	lines = append(lines, b.styles.Help.Render(footer))

	content := strings.Join(lines, "\n")

	title := b.styles.Header.Render("Query Browser")
	border := lipgloss.ThickBorder()
	borderFg := b.styles.BorderActive.GetBorderTopForeground()

	modal := bordered.RenderWithTitleEx(border, borderFg, bordered.AlignLeft, title, content, modalW)

	return lipgloss.Place(
		b.width, b.height,
		lipgloss.Center, lipgloss.Center,
		modal,
	)
}

func (b *QueryBrowser) renderEntry(idx int, e store.QueryEntry, maxW int) string {
	isSelected := idx == b.cursor

	// Timestamp
	ts := e.Timestamp.Format("Jan 02 15:04")
	age := time.Since(e.Timestamp)
	if age < time.Minute {
		ts = "just now"
	} else if age < time.Hour {
		ts = fmt.Sprintf("%dm ago", int(age.Minutes()))
	} else if age < 24*time.Hour {
		ts = fmt.Sprintf("%dh ago", int(age.Hours()))
	} else {
		ts = fmt.Sprintf("%dd ago", int(age.Hours()/24))
	}

	// SQL preview (first line, truncated)
	sql := strings.ReplaceAll(e.SQL, "\n", " ")
	sql = strings.TrimSpace(sql)
	if len(sql) > 50 {
		sql = sql[:50] + "…"
	}

	// Build line
	var parts []string
	parts = append(parts, b.styles.TextMuted.Render(fmt.Sprintf("%-10s", ts)))

	if e.Favorite {
		parts = append(parts, b.styles.Primary.Render("★ "))
	} else {
		parts = append(parts, "  ")
	}

	sqlStyled := b.styles.Text.Render(sql)
	parts = append(parts, sqlStyled)

	line := strings.Join(parts, "")

	// Truncate to content width
	displayWidth := ansi.StringWidth(ansi.Strip(line))
	if displayWidth > maxW {
		line = ansi.Truncate(line, maxW, "")
	} else {
		// Pad
		line += strings.Repeat(" ", maxW-displayWidth)
	}

	if isSelected {
		return b.styles.Selected.Render(line)
	}
	return line
}
