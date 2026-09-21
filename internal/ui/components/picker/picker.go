package picker

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/theme"
)

type ConnectionSelectedMsg struct {
	Project config.FoundProject
}

type ProjectToggledMsg struct {
	Project config.FoundProject
	Active  bool
}

type Picker struct {
	projects []config.FoundProject
	cursor   int
	styles   *theme.Styles
	width    int
	height   int
	loading  bool
	err      error
}

func New(styles *theme.Styles) *Picker {
	return &Picker{
		styles:   styles,
		projects: make([]config.FoundProject, 0),
	}
}

func (p *Picker) SetProjects(projects []config.FoundProject) {
	p.projects = projects
	p.cursor = 0
}

func (p *Picker) SetWidth(w int) {
	p.width = w
}

func (p *Picker) SetHeight(h int) {
	p.height = h
}

func (p *Picker) SetLoading(loading bool) {
	p.loading = loading
}

func (p *Picker) SetError(err error) {
	p.err = err
}

func (p *Picker) Update(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return p.handleKey(msg)
	}
	return nil, false
}

func (p *Picker) handleKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	key := msg.String()
	switch key {
	case "j", "down":
		p.moveDown()
		return nil, true
	case "k", "up":
		p.moveUp()
		return nil, true
	case "space":
		if len(p.projects) > 0 {
			proj := p.projects[p.cursor]
			newActive := !proj.Active
			p.projects[p.cursor].Active = newActive
			return func() tea.Msg {
				return ProjectToggledMsg{Project: proj, Active: newActive}
			}, true
		}
	case "enter":
		if len(p.projects) > 0 {
			proj := p.projects[p.cursor]
			if proj.Active {
				return func() tea.Msg {
					return ConnectionSelectedMsg{Project: proj}
				}, true
			}
		}
	case "q", "ctrl+c":
		return tea.Quit, true
	}
	return nil, false
}

func (p *Picker) moveDown() {
	if p.cursor < len(p.projects)-1 {
		p.cursor++
	}
}

func (p *Picker) moveUp() {
	if p.cursor > 0 {
		p.cursor--
	}
}

func (p *Picker) View() string {
	if p.loading {
		return p.centerText(p.styles.Text.Render("Scanning for .dbx.toml files..."))
	}

	if p.err != nil {
		return p.centerText(p.styles.Error.Render(fmt.Sprintf("Error: %v", p.err)))
	}

	if len(p.projects) == 0 {
		return p.centerText(p.styles.TextMuted.Render("No .dbx.toml files found in ~/dev"))
	}

	var s strings.Builder

	// Title
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(p.styles.Text.GetForeground()).
		Render("dbx")
	s.WriteString(p.centerText(title))
	s.WriteString("\n\n")

	// Connection list
	for i, proj := range p.projects {
		arrow := "  "
		if i == p.cursor {
			arrow = lipgloss.NewStyle().
				Foreground(p.styles.Primary.GetForeground()).
				Bold(true).
				Render("▸")
		}

		// Determine styles based on active state
		nameFg := p.styles.Text.GetForeground()
		driverFg := p.styles.Primary.GetForeground()
		statusTag := ""
		if !proj.Active {
			nameFg = p.styles.TextMuted.GetForeground()
			driverFg = p.styles.TextMuted.GetForeground()
			statusTag = lipgloss.NewStyle().
				Foreground(p.styles.TextMuted.GetForeground()).
				Render(" [off]")
		}

		name := lipgloss.NewStyle().
			Bold(true).
			Foreground(nameFg).
			Render(proj.Name)

		port := p.getPort(proj.Connection.DSN)
		driverPort := proj.Connection.Driver
		if port != "" {
			driverPort = fmt.Sprintf("%s:%s", proj.Connection.Driver, port)
		}

		driver := lipgloss.NewStyle().
			Foreground(driverFg).
			Bold(true).
			Render(driverPort)

		path := lipgloss.NewStyle().
			Foreground(p.styles.TextMuted.GetForeground()).
			Render(proj.Path)

		line := fmt.Sprintf("  %s %s%s  %s  %s", arrow, name, statusTag, driver, path)

		s.WriteString(line)
		s.WriteString("\n")
	}

	// Footer
	s.WriteString("\n")
	footer := lipgloss.NewStyle().
		Foreground(p.styles.TextMuted.GetForeground()).
		Render("j/k ↑↓   space toggle   enter select   q quit")
	s.WriteString(p.centerText(footer))

	return s.String()
}

func (p *Picker) getPort(dsn string) string {
	// Find host:port pattern in DSN like postgres://user:pass@host:port/db
	// First, find the @ symbol (after credentials)
	atIdx := strings.Index(dsn, "@")
	if atIdx == -1 {
		return ""
	}
	
	// After @, find the next : for port
	afterAt := dsn[atIdx+1:]
	colonIdx := strings.Index(afterAt, ":")
	if colonIdx == -1 {
		return ""
	}
	
	// Extract port (digits after colon until / or ? or end)
	portStart := colonIdx + 1
	portEnd := portStart
	for portEnd < len(afterAt) && afterAt[portEnd] >= '0' && afterAt[portEnd] <= '9' {
		portEnd++
	}
	
	if portEnd > portStart {
		return afterAt[portStart:portEnd]
	}
	
	return ""
}

func (p *Picker) centerText(text string) string {
	textWidth := lipgloss.Width(text)
	if textWidth >= p.width {
		return text
	}
	padding := (p.width - textWidth) / 2
	return strings.Repeat(" ", padding) + text
}
