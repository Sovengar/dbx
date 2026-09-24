package ask

import (
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/buble/dbx/internal/theme"
	"github.com/buble/dbx/internal/ui/bordered"
	"github.com/buble/dbx/internal/ui/keydisplay"
)

// AskSubmittedMsg is emitted when the user submits a question while typing.
type AskSubmittedMsg struct {
	Question string
}

// AskConfirmMsg is emitted when the user confirms the generated SQL for
// execution while the pane is under review.
type AskConfirmMsg struct {
	SQL string
}

// AskClosedMsg is emitted when the user dismisses the pane with esc.
type AskClosedMsg struct{}

// TurnStatus describes where a transcript turn is in its lifecycle.
type TurnStatus int

const (
	// TurnGenerating means the question was submitted and SQL is being generated.
	TurnGenerating TurnStatus = iota
	// TurnReview means the generated SQL is waiting for confirmation.
	TurnReview
	// TurnExecuted means the SQL ran successfully.
	TurnExecuted
	// TurnError means generation or execution failed.
	TurnError
)

// Turn is one question in the transcript with its generated SQL and outcome.
type Turn struct {
	Question string
	SQL      string
	Status   TurnStatus
	Err      string
}

type panelState int

const (
	stateTyping panelState = iota
	stateGenerating
	stateReview
	stateExecuting
	stateError
)

// Ask is the NL→SQL chat overlay. It owns the transcript, the current input
// and the panel state; the app drives generation and execution by feeding it
// results through the Set* setters.
type Ask struct {
	styles  *theme.Styles
	visible bool
	width   int
	height  int

	state panelState
	input string

	turns []Turn

	contextSchema string
	contextTable  string
	contextWhere  string
}

// New creates an empty ASK overlay.
func New(styles *theme.Styles) *Ask {
	return &Ask{styles: styles}
}

// Show makes the pane visible and readies the input, preserving the transcript.
func (a *Ask) Show() {
	a.visible = true
	a.state = stateTyping
	a.input = ""
	askDebugLog("Show: visible=%v turns=%d", a.visible, len(a.turns))
}

// Hide makes the pane invisible without clearing the transcript.
func (a *Ask) Hide() {
	a.visible = false
	askDebugLog("Hide: visible=%v turns=%d", a.visible, len(a.turns))
}

// IsVisible reports whether the pane is currently shown.
func (a *Ask) IsVisible() bool { return a.visible }

// SetWidth sets the terminal width used to size the overlay.
func (a *Ask) SetWidth(w int) { a.width = w }

// SetHeight sets the terminal height used to size the overlay.
func (a *Ask) SetHeight(h int) { a.height = h }

// Turns returns a copy of the transcript.
func (a *Ask) Turns() []Turn {
	out := make([]Turn, len(a.turns))
	copy(out, a.turns)
	return out
}

// Input returns the current input line.
func (a *Ask) Input() string { return a.input }

// SetContextHint records the grid context used as a prompt hint (display only;
// the app builds the actual prompt text).
func (a *Ask) SetContextHint(schema, table, where string) {
	a.contextSchema = schema
	a.contextTable = table
	a.contextWhere = where
}

// AddQuestion appends a turn for the submitted question and enters generating.
func (a *Ask) AddQuestion(q string) {
	a.turns = append(a.turns, Turn{Question: q, Status: TurnGenerating})
	a.state = stateGenerating
	askDebugLog("AddQuestion: question=%q turns=%d", q, len(a.turns))
}

// SetGeneratedSQL attaches the generated SQL to the current turn and waits for
// confirmation.
func (a *Ask) SetGeneratedSQL(sql string) {
	if len(a.turns) > 0 {
		last := &a.turns[len(a.turns)-1]
		last.SQL = sql
		last.Status = TurnReview
		last.Err = ""
	}
	a.state = stateReview
	askDebugLog("SetGeneratedSQL: sql=%q turns=%d", sql, len(a.turns))
}

// SetError records an error on the current turn and leaves the pane ready for
// another question.
func (a *Ask) SetError(err error) {
	if err == nil {
		return
	}
	if len(a.turns) > 0 {
		last := &a.turns[len(a.turns)-1]
		last.Status = TurnError
		last.Err = err.Error()
	}
	a.state = stateError
	askDebugLog("SetError: err=%q turns=%d", err.Error(), len(a.turns))
}

// MarkExecuted records that the current turn's SQL ran successfully.
func (a *Ask) MarkExecuted() {
	if len(a.turns) > 0 {
		last := &a.turns[len(a.turns)-1]
		last.Status = TurnExecuted
		last.Err = ""
	}
	askDebugLog("MarkExecuted: turns=%d", len(a.turns))
}

// Update handles a message while the pane is visible. The bool reports whether
// the message was consumed.
func (a *Ask) Update(msg tea.Msg) (tea.Cmd, bool) {
	if !a.visible {
		return nil, false
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return a.handleKey(msg)
	}

	return nil, false
}

func (a *Ask) handleKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	key := msg.String()
	askDebugLog("handleKey: key=%q text=%q state=%v input=%q", key, msg.Text, a.state, a.input)

	if key == "esc" {
		a.Hide()
		return func() tea.Msg { return AskClosedMsg{} }, true
	}

	switch a.state {
	case stateTyping, stateError:
		switch key {
		case "enter":
			if strings.TrimSpace(a.input) == "" {
				return nil, true
			}
			question := strings.TrimSpace(a.input)
			a.AddQuestion(question)
			a.input = ""
			return func() tea.Msg { return AskSubmittedMsg{Question: question} }, true
		case "backspace":
			if len(a.input) > 0 {
				a.input = a.input[:len(a.input)-1]
			}
			return nil, true
		default:
			if len(msg.Text) > 0 && msg.Text != " " || key == "space" {
				if key == "space" {
					a.input += " "
				} else {
					a.input += msg.Text
				}
			}
			return nil, true
		}

	case stateReview:
		if key == "enter" {
			sql := ""
			if len(a.turns) > 0 {
				sql = a.turns[len(a.turns)-1].SQL
			}
			a.state = stateExecuting
			return func() tea.Msg { return AskConfirmMsg{SQL: sql} }, true
		}
		return nil, true
	}

	// generating / executing: swallow keys until the async work completes.
	return nil, true
}

// View renders the overlay. It returns an empty string when hidden.
func (a *Ask) View() string {
	if !a.visible {
		return ""
	}

	modalW := a.width * 3 / 4
	if modalW < 50 {
		modalW = 50
	}
	if modalW > 90 {
		modalW = 90
	}

	modalH := a.height * 8 / 10
	if modalH < 10 {
		modalH = 10
	}

	innerH := modalH - 2
	if innerH < 3 {
		innerH = 3
	}

	askDebugLog("View: width=%d height=%d modalW=%d modalH=%d turns=%d state=%v", a.width, a.height, modalW, modalH, len(a.turns), a.state)

	var lines []string
	for _, t := range a.turns {
		lines = append(lines, a.styles.TextMuted.Render("You: ")+a.styles.Text.Render(t.Question))
		if t.SQL != "" {
			lines = append(lines, a.styles.Primary.Render("SQL: ")+a.styles.Text.Render(t.SQL))
		}
		if t.Err != "" {
			lines = append(lines, a.styles.Error.Render("Error: "+t.Err))
		}
		if t.Status == TurnExecuted {
			lines = append(lines, a.styles.TextMuted.Render("  (executed)"))
		}
		lines = append(lines, "")
	}

	// Input / status line.
	switch a.state {
	case stateGenerating:
		lines = append(lines, a.styles.Info.Render("Generating SQL..."))
	case stateExecuting:
		lines = append(lines, a.styles.Info.Render("Executing..."))
	case stateReview:
		lines = append(lines, a.styles.Primary.Render("> ")+a.styles.Text.Render("review the SQL above, "+keydisplay.Key("Enter")+" to run, "+keydisplay.Key("Esc")+" to cancel"))
	default:
		lines = append(lines, a.styles.Primary.Render("> ")+a.styles.Text.Render(a.input)+"_")
	}

	// Keep the newest lines visible when the transcript overflows.
	if len(lines) > innerH-1 {
		lines = lines[len(lines)-(innerH-1):]
	}
	for len(lines) < innerH-1 {
		lines = append(lines, "")
	}

	footer := keydisplay.Key(" Enter send · Esc close")
	lines = append(lines, a.styles.Help.Render(footer))

	content := strings.Join(lines, "\n")

	border := lipgloss.RoundedBorder()
	borderFg := a.styles.BorderActive.GetBorderTopForeground()

	return bordered.RenderWithTitleEx(border, borderFg, bordered.AlignLeft, " ASK ", content, modalW, modalH)
}

func askDebugLog(format string, args ...interface{}) {
	f, err := os.OpenFile("/tmp/dbx_ask_debug.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = fmt.Fprintf(f, "Ask: "+format+"\n", args...)
}
