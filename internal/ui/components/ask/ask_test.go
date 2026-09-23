package ask

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/buble/dbx/internal/theme"
)

func testAsk() *Ask {
	a := New(theme.Resolve("dark").Styles())
	a.SetWidth(100)
	a.SetHeight(40)
	return a
}

func typeText(a *Ask, text string) {
	for _, r := range text {
		if r == ' ' {
			a.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
			continue
		}
		a.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// Scenario: `a` opens the ASK pane
func TestAsk_Show_EmptyTranscriptAndTyping(t *testing.T) {
	a := testAsk()
	a.Show()

	if !a.IsVisible() {
		t.Fatal("Show() did not make the pane visible")
	}
	if len(a.Turns()) != 0 {
		t.Fatalf("fresh pane transcript = %v, want empty", a.Turns())
	}
}

// Scenario: Asking a question generates SQL and shows it for review
func TestAsk_EnterSubmitsQuestion(t *testing.T) {
	a := testAsk()
	a.Show()
	typeText(a, "how many users")

	cmd, handled := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !handled {
		t.Fatal("enter was not handled while typing")
	}
	if cmd == nil {
		t.Fatal("enter did not produce a command")
	}
	msg, ok := cmd().(AskSubmittedMsg)
	if !ok {
		t.Fatalf("enter produced %T, want AskSubmittedMsg", cmd())
	}
	if msg.Question != "how many users" {
		t.Fatalf("submitted question = %q, want %q", msg.Question, "how many users")
	}
	if a.Input() != "" {
		t.Fatalf("input not cleared after submit: %q", a.Input())
	}
	turns := a.Turns()
	if len(turns) != 1 || turns[0].Question != "how many users" {
		t.Fatalf("transcript = %v, want one turn with the question", turns)
	}
	if turns[0].Status != TurnGenerating {
		t.Fatalf("turn status = %v, want TurnGenerating", turns[0].Status)
	}
}

func TestAsk_SetGeneratedSQL_WaitsForReview(t *testing.T) {
	a := testAsk()
	a.Show()
	typeText(a, "q")
	a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	a.SetGeneratedSQL("SELECT 1")

	turns := a.Turns()
	if len(turns) != 1 {
		t.Fatalf("transcript = %v, want one turn", turns)
	}
	if turns[0].SQL != "SELECT 1" || turns[0].Status != TurnReview {
		t.Fatalf("turn = %+v, want SQL under review", turns[0])
	}
}

// Scenario: Confirming executes the SQL and shows the result in the grid
func TestAsk_EnterInReviewEmitsConfirm(t *testing.T) {
	a := testAsk()
	a.Show()
	typeText(a, "q")
	a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	a.SetGeneratedSQL("SELECT 1")

	cmd, handled := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !handled || cmd == nil {
		t.Fatalf("enter in review handled=%v cmd=%v", handled, cmd)
	}
	msg, ok := cmd().(AskConfirmMsg)
	if !ok {
		t.Fatalf("enter produced %T, want AskConfirmMsg", cmd())
	}
	if msg.SQL != "SELECT 1" {
		t.Fatalf("confirmed SQL = %q, want %q", msg.SQL, "SELECT 1")
	}
}

// Scenario: `esc` closes the ASK pane without executing
func TestAsk_EscClosesWithoutExecuting(t *testing.T) {
	a := testAsk()
	a.Show()
	typeText(a, "q")
	a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	a.SetGeneratedSQL("SELECT 1")

	cmd, handled := a.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !handled || cmd == nil {
		t.Fatalf("esc handled=%v cmd=%v", handled, cmd)
	}
	if _, ok := cmd().(AskClosedMsg); !ok {
		t.Fatalf("esc produced %T, want AskClosedMsg", cmd())
	}
	if a.IsVisible() {
		t.Fatal("pane still visible after esc")
	}
}

// Scenario: The transcript keeps the question and the executed SQL
func TestAsk_TranscriptPersistsAcrossReopen(t *testing.T) {
	a := testAsk()
	a.Show()
	typeText(a, "how many users")
	a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	a.SetGeneratedSQL("SELECT count(*) FROM users")
	a.SetResultSummary("1 rows")
	a.Hide()

	a.Show()

	turns := a.Turns()
	if len(turns) != 1 {
		t.Fatalf("transcript after reopen = %v, want one turn", turns)
	}
	if turns[0].Question != "how many users" {
		t.Fatalf("question lost after reopen: %q", turns[0].Question)
	}
	if turns[0].SQL != "SELECT count(*) FROM users" {
		t.Fatalf("executed SQL lost after reopen: %q", turns[0].SQL)
	}
	if turns[0].Status != TurnExecuted {
		t.Fatalf("turn status = %v, want TurnExecuted", turns[0].Status)
	}
}

// Scenario: AI provider failure
func TestAsk_SetError_KeepsPaneOpen(t *testing.T) {
	a := testAsk()
	a.Show()
	typeText(a, "q")
	a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	a.SetError(errFake)

	turns := a.Turns()
	if len(turns) != 1 || turns[0].Status != TurnError || turns[0].Err == "" {
		t.Fatalf("transcript = %+v, want an error turn", turns)
	}
	if !a.IsVisible() {
		t.Fatal("pane should stay open after an error")
	}
}

func TestAsk_UpdateIgnoredWhenHidden(t *testing.T) {
	a := testAsk()
	if _, handled := a.Update(tea.KeyPressMsg{Code: 'x', Text: "x"}); handled {
		t.Fatal("hidden pane consumed a key")
	}
}

type fakeErr struct{}

func (fakeErr) Error() string { return "boom" }

var errFake error = fakeErr{}
