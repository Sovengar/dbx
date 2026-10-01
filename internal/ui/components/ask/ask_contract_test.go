// This file holds the CONTRACTS of the ASK overlay: what the pane guarantees to a
// caller and to a person using it, asserted over every state and every size rather
// than one case at a time.
//
// It sits beside ask_test.go, which walks the same component a scenario at a time.
// The two overlap on purpose and neither supersedes the other: the scenario tests
// read as a tour, and the invariants here are what actually kill mutants, because a
// mutant that changes a clamp or a trim count is invisible to a tour that only looks
// at one terminal size.
package ask

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/buble/dbx/internal/theme"
	"github.com/buble/dbx/internal/ui/keydisplay"
	"github.com/charmbracelet/x/ansi"
)

func newAskForTest() *Ask {
	a := New(theme.Resolve("dark").Styles())
	a.SetWidth(120)
	a.SetHeight(40)
	a.Show()
	return a
}

// The named keys, as the terminal delivers them. A key with Text set is one the
// pane will append; a key without is one it must ignore.
var (
	keyEnter    = tea.KeyPressMsg{Code: tea.KeyEnter}
	keyBackspce = tea.KeyPressMsg{Code: tea.KeyBackspace}
	keySpace    = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	keyEscape   = tea.KeyPressMsg{Code: tea.KeyEscape}
	keyUp       = tea.KeyPressMsg{Code: tea.KeyUp}
	keyDown     = tea.KeyPressMsg{Code: tea.KeyDown}
	keyLeft     = tea.KeyPressMsg{Code: tea.KeyLeft}
	keyRight    = tea.KeyPressMsg{Code: tea.KeyRight}
	keyCtrlC    = tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
)

// letter is a printable keypress for one ASCII letter, which is how a person typing
// a question delivers it.
func letter(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

// press sends one keypress and returns what the pane answered: the message its
// command produced, and whether the key was consumed.
func press(a *Ask, key tea.KeyPressMsg) (tea.Msg, bool) {
	cmd, handled := a.Update(key)
	if cmd == nil {
		return nil, handled
	}
	return cmd(), handled
}

// typeInto sends each character of a string the way a person would, with a real
// SPACE key for a space rather than a text key that happens to contain one.
func typeInto(a *Ask, str string) {
	for _, r := range str {
		if r == ' ' {
			_, _ = press(a, keySpace)
			continue
		}
		_, _ = press(a, letter(r))
	}
}

// contentLines returns the pane's own lines, with the rounded border taken off and
// the styling stripped. The border is two of the three characters on every line and
// the styling is invisible, so a test that does not remove them is asserting on the
// frame rather than on the content.
func contentLines(t *testing.T, view string) []string {
	t.Helper()
	raw := strings.Split(ansi.Strip(view), "\n")
	if len(raw) < 2 {
		t.Fatalf("the view has %d lines, too few to have a border:\n%q", len(raw), view)
	}
	out := raw[1 : len(raw)-1] // drop the top and bottom border
	for i, line := range out {
		trimmed := strings.TrimPrefix(line, "│")
		trimmed = strings.TrimSuffix(trimmed, "│")
		out[i] = trimmed
	}
	return out
}

// nonEmpty drops the padding lines, so a content assertion is about what is written
// rather than about how tall the box is.
func nonEmpty(lines []string) []string {
	var out []string
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			out = append(out, strings.TrimRight(l, " "))
		}
	}
	return out
}

// --- lifecycle --------------------------------------------------------------

// Scenario: El panel empieza oculto, y Update no toca nada mientras lo esté.
//
// The overlay is mounted for the whole session and shown on demand, so every message
// in the app arrives here whether or not the user asked for it. Returning
// "not consumed" while hidden is what lets a key reach the grid underneath instead
// of being swallowed by an invisible pane.
func TestUpdate_WhileHiddenConsumesNothing(t *testing.T) {
	a := New(theme.Resolve("dark").Styles())
	a.SetWidth(120)
	a.SetHeight(40)

	if a.IsVisible() {
		t.Error("a fresh Ask is visible")
	}
	if v := a.View(); v != "" {
		t.Errorf("a hidden Ask renders %q, want the empty string", v)
	}

	// Keys that WOULD be consumed if the pane were showing.
	for _, key := range []tea.KeyPressMsg{letter('a'), keyEnter, keyBackspce, keySpace, keyEscape, keyUp, keyDown} {
		if _, handled := press(a, key); handled {
			t.Errorf("the key %q was consumed by a hidden pane", key.String())
		}
	}
	// And nothing was recorded.
	if len(a.Turns()) != 0 {
		t.Errorf("a hidden pane recorded %d turns", len(a.Turns()))
	}
	if a.Input() != "" {
		t.Errorf("a hidden pane's input is %q, want empty", a.Input())
	}
}

// Scenario: Show limpia el input y vuelve a escribir, pero NO borra la conversación.
//
// Reopening the pane is the recovery path for a stuck one, and the state it has to
// put back is "ready for a question": typing, with an empty line. The transcript is
// the session's history and clearing it would throw away the answer the user is
// still reading, so it is explicitly kept.
func TestShow_ClearsTheInputAndKeepsTheTranscript(t *testing.T) {
	a := newAskForTest()
	typeInto(a, "half typed")
	a.AddQuestion("an earlier question")
	a.SetGeneratedSQL("SELECT 1")

	a.Hide()
	if a.IsVisible() {
		t.Fatal("Hide did not hide the pane")
	}
	a.Show()

	if !a.IsVisible() {
		t.Error("Show did not show the pane")
	}
	if got := a.Input(); got != "" {
		t.Errorf("after Show the input is %q, want it cleared", got)
	}
	if a.state != stateTyping {
		t.Errorf("after Show the state is %v, want stateTyping", a.state)
	}
	turns := a.Turns()
	if len(turns) != 1 {
		t.Fatalf("after Show the transcript holds %d turns, want the 1 from before", len(turns))
	}
	if turns[0].Question != "an earlier question" || turns[0].SQL != "SELECT 1" {
		t.Errorf("the surviving turn is %+v, want it untouched", turns[0])
	}
}

// Scenario: Turns() devuelve una COPIA.
//
// The transcript is read by the app to decide what to record, and by tests. Handing
// out the live slice would let a caller rewrite history — reorder turns, edit a
// question, drop one — without going through any of the setters that are supposed to
// keep a turn's status and its error in step with each other.
func TestTurns_ReturnsACopy(t *testing.T) {
	a := newAskForTest()
	a.AddQuestion("original")
	a.AddQuestion("second")

	got := a.Turns()
	if len(got) != 2 {
		t.Fatalf("Turns returned %d turns, want 2", len(got))
	}
	got[0].Question = "MUTATED"
	got[0].Status = TurnError
	got[1].Question = "ALSO MUTATED"

	fresh := a.Turns()
	if fresh[0].Question != "original" {
		t.Errorf("editing the returned slice changed the pane's turn to %q", fresh[0].Question)
	}
	if fresh[0].Status != TurnGenerating {
		t.Errorf("editing the returned slice changed the status to %v", fresh[0].Status)
	}
	if fresh[1].Question != "second" {
		t.Errorf("editing the returned slice changed the second turn to %q", fresh[1].Question)
	}

	// And appending to the returned slice does not grow the transcript. This is the
	// case a `make`+`copy` gets right and a bare return does not: the returned slice
	// has no spare capacity, so an append reallocates instead of writing into the
	// pane's array.
	before := len(a.Turns())
	grown := append(a.Turns(), Turn{Question: "injected"})
	if len(grown) != before+1 {
		t.Fatalf("the append did not grow the copy: %d then %d", before, len(grown))
	}
	if len(a.Turns()) != before {
		t.Errorf("appending to the returned slice grew the pane's transcript to %d turns", len(a.Turns()))
	}
}

// Scenario: Una transcripción vacía se devuelve vacía, no nula.
//
// Turns() hands this to callers that marshal it, and a nil slice marshals to JSON
// `null` while an empty one marshals to `[]`. A consumer doing `jq '.[]'` gets an
// error on the first and an empty list on the second, so the difference is not
// cosmetic. The `make` in Turns() is what guarantees it; the assertion is what keeps
// it.
func TestTurns_AnEmptyTranscriptIsEmptyNotNil(t *testing.T) {
	a := newAskForTest()
	got := a.Turns()
	if got == nil {
		t.Fatal("Turns() on an empty transcript returned nil, which marshals to null instead of []")
	}
	if len(got) != 0 {
		t.Errorf("Turns() on an empty transcript returned %d turns", len(got))
	}
}

// --- typing -----------------------------------------------------------------

// Scenario: Se escribe con las teclas de sempre, y una tecla sin texto no escribe nada.
//
// The default branch appends `msg.Text` for anything printable. A key with no text —
// an arrow, a function key, a modifier combo — has nothing to append and must leave
// the line alone, while still counting as consumed, because the pane has already
// decided the key is its business.
func TestTyping_PrintableKeysAppendAndSilentKeysDoNot(t *testing.T) {
	a := newAskForTest()

	// Typed as a person would, spaces included, so the assertion is about the
	// interleaving and not about two separate passes.
	typeInto(a, "how many users")
	if got, want := a.Input(), "how many users"; got != want {
		t.Errorf("the input is %q, want %q", got, want)
	}
	// And each letter on its own is consumed.
	for _, r := range "abz" {
		if _, handled := press(a, letter(r)); !handled {
			t.Errorf("the letter %q was not consumed while typing", string(r))
		}
	}
	if got, want := a.Input(), "how many usersabz"; got != want {
		t.Errorf("the input is %q, want %q", got, want)
	}

	// A key with no Text appends nothing and is still consumed.
	for _, key := range []tea.KeyPressMsg{keyUp, keyDown, keyLeft, keyRight, keyCtrlC} {
		before := a.Input()
		_, handled := press(a, key)
		if !handled {
			t.Errorf("the key %q was not consumed while typing", key.String())
		}
		if got := a.Input(); got != before {
			t.Errorf("the silent key %q changed the input from %q to %q", key.String(), before, got)
		}
	}
}

// Scenario: Backspace borra un CARÁCTER, y no hace nada en una línea vacía.
//
// The character, not the byte, is the contract: the question is typed by a person
// and half of an accented letter or any CJK character is not a state a text field can
// be in. Cutting bytes left invalid UTF-8 in the input, which then went to the model
// as part of the question — visible to the user as mojibake and to the model as
// nonsense.
//
// The empty case matters separately: backspace on an empty line is a no-op rather
// than a panic, because a person holds keys down.
func TestBackspace_RemovesOneCharacterAndIsSafeWhenEmpty(t *testing.T) {
	for _, tc := range []struct {
		name  string
		typed string
		want  string
	}{
		{"one ASCII character", "a", ""},
		{"a word", "abc", "ab"},
		{"a two byte character", "é", ""},
		{"an accented character mid word", "café", "caf"},
		{"a CJK character", "日", ""},
		{"CJK mid word", "日本語", "日本"},
		{"an emoji", "🎉", ""},
		{"mixed", "aé日🎉z", "aé日🎉"},
		{"everything at once", "日本", "日"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newAskForTest()
			a.input = tc.typed
			if _, handled := press(a, keyBackspce); !handled {
				t.Error("backspace was not consumed")
			}
			if got := a.Input(); got != tc.want {
				t.Errorf("backspace on %q left %q, want %q", tc.typed, got, tc.want)
			}
			if !utf8.ValidString(a.Input()) {
				t.Errorf("backspace on %q left invalid UTF-8: %q", tc.typed, a.Input())
			}
		})
	}

	// And on an empty line, repeatedly, without a panic.
	a := newAskForTest()
	for range 5 {
		if _, handled := press(a, keyBackspce); !handled {
			t.Error("backspace on an empty line was not consumed")
		}
	}
	if got := a.Input(); got != "" {
		t.Errorf("backspace on an empty line produced %q", got)
	}
}

// Scenario: Enter con la línea en blanco no hace nada, y no la borra.
//
// Whitespace is not a question. Submitting one would send an empty prompt to the
// model, and clearing the line would destroy what the user typed — a stray space, or
// a question they were about to finish. So the key is consumed, nothing is emitted,
// and the line is left exactly as it was.
func TestEnter_OnABlankLineDoesNothingAndKeepsTheLine(t *testing.T) {
	for _, blank := range []string{"", " ", "   ", "\t", " \t "} {
		t.Run(fmt.Sprintf("%q", blank), func(t *testing.T) {
			a := newAskForTest()
			a.input = blank

			msg, handled := press(a, keyEnter)
			if !handled {
				t.Fatal("enter on a blank line was not consumed; it would fall through to whatever is under the overlay")
			}
			if msg != nil {
				t.Errorf("enter on a blank line emitted %T, want nothing", msg)
			}
			if got := a.Input(); got != blank {
				t.Errorf("enter on a blank line changed the input from %q to %q", blank, got)
			}
			if len(a.Turns()) != 0 {
				t.Errorf("enter on a blank line recorded %d turns", len(a.Turns()))
			}
		})
	}
}

// Scenario: Enter envía la pregunta SIN ESPACIOS around, y vacía la línea.
//
// The question is what the model sees, so it is trimmed on the way out — a trailing
// space in a prompt is noise. The turn stores the same trimmed text, so what the
// transcript shows and what was submitted cannot drift apart. And the line is
// cleared, because the question is now in the transcript and leaving it would submit
// it twice.
func TestEnter_SubmitsTheTrimmedQuestionAndClearsTheLine(t *testing.T) {
	for _, tc := range []struct {
		name  string
		typed string
		want  string
	}{
		{"already trimmed", "how many users", "how many users"},
		{"leading spaces", "   how many", "how many"},
		{"trailing spaces", "how many   ", "how many"},
		{"both", "  how many  ", "how many"},
		{"a tab inside", "how\tmany", "how\tmany"},
		{"one word", "users", "users"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newAskForTest()
			a.input = tc.typed

			msg, handled := press(a, keyEnter)
			if !handled {
				t.Fatal("enter was not consumed")
			}
			sub, ok := msg.(AskSubmittedMsg)
			if !ok {
				t.Fatalf("enter emitted %T, want AskSubmittedMsg", msg)
			}
			if sub.Question != tc.want {
				t.Errorf("the message carries %q, want the trimmed %q", sub.Question, tc.want)
			}
			if got := a.Input(); got != "" {
				t.Errorf("the input is %q after submitting, want it cleared", got)
			}

			turns := a.Turns()
			if len(turns) != 1 {
				t.Fatalf("the transcript holds %d turns, want 1", len(turns))
			}
			// The turn and the message carry the same string, so the transcript
			// cannot disagree with what was sent.
			if turns[0].Question != tc.want {
				t.Errorf("the turn records %q, want the same trimmed %q as the message", turns[0].Question, tc.want)
			}
			if turns[0].Status != TurnGenerating {
				t.Errorf("the new turn is %v, want TurnGenerating", turns[0].Status)
			}
			if a.state != stateGenerating {
				t.Errorf("the state is %v after submitting, want stateGenerating", a.state)
			}
		})
	}
}

// --- review and the async states ---------------------------------------------

// Scenario: En revisión, Enter manda el SQL de la ÚLTIMA vuelta.
//
// The confirmation is what runs, so it has to be the SQL the user was just shown as
// under review. With several turns in the transcript the last one is the live
// question, and confirming an earlier one would execute a stale statement.
func TestEnter_InReviewConfirmsTheLastTurnsSQL(t *testing.T) {
	a := newAskForTest()
	a.AddQuestion("first question")
	a.SetGeneratedSQL("SELECT 1")
	a.AddQuestion("second question")
	a.SetGeneratedSQL("SELECT 2")

	msg, handled := press(a, keyEnter)
	if !handled {
		t.Fatal("enter in review was not consumed")
	}
	conf, ok := msg.(AskConfirmMsg)
	if !ok {
		t.Fatalf("enter in review emitted %T, want AskConfirmMsg", msg)
	}
	if conf.SQL != "SELECT 2" {
		t.Errorf("the confirmation carries %q, want the LAST turn's %q", conf.SQL, "SELECT 2")
	}
	if a.state != stateExecuting {
		t.Errorf("the state is %v after confirming, want stateExecuting", a.state)
	}
}

// Scenario: En revisión no se puede confirmar sin SQL, y sale vacío en vez de reventar.
//
// The pane can reach review with no turns at all (SetGeneratedSQL does that on an
// empty transcript, pinned below), so the read of the last turn's SQL has to survive
// an empty transcript. Confirming then emits an empty SQL, which the app rejects
// downstream — the right outcome, because the alternative is a panic in a key
// handler.
func TestEnter_InReviewWithNoTurnsConfirmsEmpty(t *testing.T) {
	a := newAskForTest()
	a.state = stateReview // reachable: SetGeneratedSQL on an empty transcript does this

	msg, handled := press(a, keyEnter)
	if !handled {
		t.Fatal("enter in review with no turns was not consumed")
	}
	conf, ok := msg.(AskConfirmMsg)
	if !ok {
		t.Fatalf("enter emitted %T, want AskConfirmMsg", msg)
	}
	if conf.SQL != "" {
		t.Errorf("the confirmation carries %q, want empty: there is no turn to take SQL from", conf.SQL)
	}
	if a.state != stateExecuting {
		t.Errorf("the state is %v, want stateExecuting", a.state)
	}
}

// Scenario: Mientras algo corre, las teclas se tragan.
//
// Generation and execution are asynchronous, and a key pressed in between must not
// reach the grid under the overlay — and must not start a second turn, which would
// interleave with the first. Esc is the exception, checked before the state switch
// so the user can always get out.
func TestBusyStates_SwallowKeysButStillClose(t *testing.T) {
	for _, state := range []struct {
		name  string
		state panelState
	}{
		{"generating", stateGenerating},
		{"executing", stateExecuting},
	} {
		t.Run(state.name, func(t *testing.T) {
			a := newAskForTest()
			a.AddQuestion("a question")
			a.state = state.state
			before := a.Turns()

			for _, key := range []tea.KeyPressMsg{letter('a'), keyEnter, keyBackspce, keySpace, keyUp, keyDown} {
				msg, handled := press(a, key)
				if !handled {
					t.Errorf("the key %q was not consumed while %s", key.String(), state.name)
				}
				if msg != nil {
					t.Errorf("the key %q emitted %T while %s", key.String(), msg, state.name)
				}
			}
			if got := a.Turns(); len(got) != len(before) {
				t.Errorf("typing while %s recorded a turn", state.name)
			}
			if got := a.Input(); got != "" {
				t.Errorf("typing while %s put %q in the input", state.name, got)
			}

			// Esc still gets out, and says so.
			msg, handled := press(a, keyEscape)
			if !handled {
				t.Fatal("esc was not consumed while busy")
			}
			if _, ok := msg.(AskClosedMsg); !ok {
				t.Errorf("esc emitted %T while %s, want AskClosedMsg", msg, state.name)
			}
			if a.IsVisible() {
				t.Errorf("esc did not hide the pane while %s", state.name)
			}
		})
	}
}

// Scenario: En revisión solo se acepta Enter; el resto se traga sin efecto.
//
// There is no editing a generated statement, so a keypress in review has nothing to
// do. It is still consumed, because letting it through would reach the grid under a
// modal that is asking a yes/no question. Esc is the way out.
func TestReview_OtherKeysAreConsumedAndDoNothing(t *testing.T) {
	a := newAskForTest()
	a.AddQuestion("q")
	a.SetGeneratedSQL("SELECT 1")

	for _, key := range []tea.KeyPressMsg{letter('a'), keyBackspce, keySpace, keyUp, keyDown, keyLeft, keyRight} {
		msg, handled := press(a, key)
		if !handled {
			t.Errorf("the key %q was not consumed in review", key.String())
		}
		if msg != nil {
			t.Errorf("the key %q emitted %T in review, want nothing", key.String(), msg)
		}
		if a.state != stateReview {
			t.Errorf("the key %q left the pane in %v, want it still reviewing", key.String(), a.state)
			break
		}
	}
	if turns := a.Turns(); len(turns) != 1 || turns[0].SQL != "SELECT 1" {
		t.Errorf("the turn changed while keys were pressed in review: %+v", turns)
	}
}

// Scenario: Esc cierra el panel desde CUALQUIER estado.
//
// Esc is checked before the state switch, which is what makes it a way out of a pane
// that is waiting on something. If it lived inside the per-state branches, a stuck
// generation would be unescapable.
func TestEsc_ClosesFromEveryState(t *testing.T) {
	for _, state := range []struct {
		name  string
		enter func(a *Ask)
		want  panelState
	}{
		{"typing", func(a *Ask) {}, stateTyping},
		{"generating", func(a *Ask) { a.AddQuestion("q") }, stateGenerating},
		{"review", func(a *Ask) { a.AddQuestion("q"); a.SetGeneratedSQL("SELECT 1") }, stateReview},
		{"executing", func(a *Ask) {
			a.AddQuestion("q")
			a.SetGeneratedSQL("SELECT 1")
			press(a, keyEnter)
		}, stateExecuting},
		{"error", func(a *Ask) { a.AddQuestion("q"); a.SetError(errors.New("boom")) }, stateError},
	} {
		t.Run(state.name, func(t *testing.T) {
			a := newAskForTest()
			state.enter(a)
			if a.state != state.want {
				t.Fatalf("the fixture is in %v, want %v", a.state, state.want)
			}

			msg, handled := press(a, keyEscape)
			if !handled {
				t.Fatal("esc was not consumed")
			}
			if _, ok := msg.(AskClosedMsg); !ok {
				t.Errorf("esc emitted %T, want AskClosedMsg", msg)
			}
			if a.IsVisible() {
				t.Error("esc did not hide the pane")
			}
			// And the transcript is a history, not scratch: closing keeps it.
			if len(a.Turns()) == 0 && state.name != "typing" {
				t.Error("esc cleared the transcript")
			}
		})
	}
}

// --- the setters ------------------------------------------------------------

// Scenario: El SQL se cuelga de la última vuelta, y borra el error anterior.
//
// A turn that failed and then gets SQL is a turn that recovered, and showing both the
// error and the statement would leave the user deciding which one is current. The
// error is cleared for exactly that reason.
func TestSetGeneratedSQL_AttachesToTheLastTurnAndClearsAPreviousError(t *testing.T) {
	a := newAskForTest()
	a.AddQuestion("how many users")
	a.SetError(errors.New("no such table"))
	if turns := a.Turns(); turns[0].Err == "" {
		t.Fatal("the fixture has no error to clear")
	}

	a.SetGeneratedSQL("SELECT count(*) FROM users")

	turns := a.Turns()
	if len(turns) != 1 {
		t.Fatalf("the transcript holds %d turns, want 1", len(turns))
	}
	if turns[0].SQL != "SELECT count(*) FROM users" {
		t.Errorf("the turn's SQL is %q, want the generated one", turns[0].SQL)
	}
	if turns[0].Err != "" {
		t.Errorf("the turn still carries the error %q after new SQL arrived", turns[0].Err)
	}
	if turns[0].Status != TurnReview {
		t.Errorf("the turn is %v, want TurnReview", turns[0].Status)
	}
	if a.state != stateReview {
		t.Errorf("the state is %v, want stateReview", a.state)
	}
	// And an earlier turn is not touched, so a transcript of several keeps its
	// own outcomes.
	a.AddQuestion("second")
	a.SetError(errors.New("second failed"))
	a.SetGeneratedSQL("SELECT 2")
	turns = a.Turns()
	if turns[0].SQL != "SELECT count(*) FROM users" {
		t.Errorf("the first turn's SQL became %q; only the last turn may be written to", turns[0].SQL)
	}
}

// Scenario: SetGeneratedSQL sin vueltas igual entra en revisión.
//
// KNOWN BEHAVIOUR, pinned because it is a state the pane can be in with nothing to
// show, and because the state is set OUTSIDE the length check — the turn is
// conditional and the state is not.
//
// The consequence is a pane that says "review the SQL above" with no SQL above it,
// and whose Enter confirms an empty string. In the app this is unreachable:
// AddQuestion always runs before generation, and the generated-SQL message is
// discarded when the sequence is stale. So it is a hazard for a future caller rather
// than a bug a user can hit, and the fix would be a one-line move of the assignment
// inside the guard.
func TestSetGeneratedSQL_WithNoTurnsStillEntersReview(t *testing.T) {
	a := newAskForTest()
	a.SetGeneratedSQL("SELECT 1")

	if len(a.Turns()) != 0 {
		t.Fatalf("the transcript holds %d turns, want none", len(a.Turns()))
	}
	if a.state != stateReview {
		t.Errorf("the state is %v, want stateReview: the assignment sits outside the length guard", a.state)
	}
	// And what the user would see.
	lines := nonEmpty(contentLines(t, a.View()))
	found := false
	for _, l := range lines {
		if strings.Contains(l, "review the SQL") {
			found = true
		}
	}
	if !found {
		t.Errorf("the pane does not show the review prompt, so this state is not observable:\n%q", lines)
	}
}

// Scenario: Un error nil no cambia nada, ni el turno ni el estado.
//
// SetError is called from the app's error paths, and a nil there would mean a bug
// upstream. Returning early keeps the pane exactly as it was instead of blanking the
// turn's error and moving to an error screen with nothing to show. The guard is what
// makes `SetError(nil)` safe to call, so it is asserted rather than assumed.
func TestSetError_NilIsANoOp(t *testing.T) {
	a := newAskForTest()
	a.AddQuestion("q")
	a.SetGeneratedSQL("SELECT 1")

	a.SetError(nil)

	turns := a.Turns()
	if turns[0].Status != TurnReview {
		t.Errorf("a nil error changed the turn's status to %v", turns[0].Status)
	}
	if turns[0].Err != "" {
		t.Errorf("a nil error put %q on the turn", turns[0].Err)
	}
	if a.state != stateReview {
		t.Errorf("a nil error moved the pane to %v, want it still reviewing", a.state)
	}
}

// Scenario: Un error se anota en la última vuelta, y el panel queda listo para otra.
//
// StateError is in the same branch as stateTyping in the key handler, so a failed
// turn does not lock the pane: the user sees the error and can immediately ask
// again. That is the whole point of the state existing separately from the turn's
// status.
func TestSetError_RecordsOnTheLastTurnAndLeavesThePaneUsable(t *testing.T) {
	a := newAskForTest()
	a.AddQuestion("how many users")
	a.SetGeneratedSQL("SELECT 1")
	a.SetError(errors.New("relation \"users\" does not exist"))

	turns := a.Turns()
	if turns[0].Status != TurnError {
		t.Errorf("the turn is %v, want TurnError", turns[0].Status)
	}
	if turns[0].Err != `relation "users" does not exist` {
		t.Errorf("the turn carries %q, want the error's message", turns[0].Err)
	}
	if a.state != stateError {
		t.Errorf("the state is %v, want stateError", a.state)
	}

	// Usable: a new question goes through exactly as after a fresh Show.
	typeInto(a, "try again")
	msg, handled := press(a, keyEnter)
	if !handled {
		t.Fatal("enter after an error was not consumed")
	}
	if sub, ok := msg.(AskSubmittedMsg); !ok || sub.Question != "try again" {
		t.Errorf("enter after an error emitted %#v, want AskSubmittedMsg{Question: \"try again\"}", msg)
	}
	if len(a.Turns()) != 2 {
		t.Errorf("the transcript holds %d turns, want the failed one plus the new one", len(a.Turns()))
	}
}

// Scenario: Un error sin vueltas cambia el estado igualmente.
//
// KNOWN BEHAVIOUR, same shape as SetGeneratedSQL with no turns: the turn update is
// conditional and the state is not. Unreachable from the app, which always has a
// turn by the time an error arrives.
func TestSetError_WithNoTurnsStillEntersTheErrorState(t *testing.T) {
	a := newAskForTest()
	a.SetError(errors.New("not connected to a database"))

	if len(a.Turns()) != 0 {
		t.Fatalf("the transcript holds %d turns, want none", len(a.Turns()))
	}
	if a.state != stateError {
		t.Errorf("the state is %v, want stateError: the assignment sits outside the length guard", a.state)
	}
}

// Scenario: MarkExecuted marca la vuelta y NO mueve el panel.
//
// KNOWN BEHAVIOUR, pinned because the state left behind looks like a bug and is not
// one — and because the test is what stops somebody "fixing" it.
//
// After a successful execution the pane stays in stateExecuting, so the status line
// keeps saying "Executing...". In the app that is never on screen: the handler calls
// MarkExecuted and then Hide on the next line, because a completed ASK is a
// one-shot. The pane becomes usable again through Show, which resets the state.
//
// So the fix is NOT to set the state here. Moving it would change nothing a user
// sees and would put a second source of truth for "the ASK is done" next to the
// app's own Hide.
func TestMarkExecuted_MarksTheTurnAndLeavesTheStateAlone(t *testing.T) {
	a := newAskForTest()
	a.AddQuestion("how many users")
	a.SetGeneratedSQL("SELECT 1")
	press(a, keyEnter) // -> stateExecuting
	if a.state != stateExecuting {
		t.Fatalf("the fixture is in %v, want stateExecuting", a.state)
	}

	a.MarkExecuted()

	turns := a.Turns()
	if turns[0].Status != TurnExecuted {
		t.Errorf("the turn is %v, want TurnExecuted", turns[0].Status)
	}
	if a.state != stateExecuting {
		t.Errorf("the state is %v, want it left at stateExecuting: the app hides the pane instead", a.state)
	}

	// And Show is the way back to a usable pane.
	a.Show()
	if a.state != stateTyping {
		t.Errorf("after Show the state is %v, want stateTyping", a.state)
	}
	if turns := a.Turns(); len(turns) != 1 || turns[0].Status != TurnExecuted {
		t.Errorf("Show disturbed the transcript: %+v", turns)
	}
}

// Scenario: MarkExecuted borra el error anterior de la vuelta.
//
// A turn that failed and then succeeded is a turn that worked, and showing a stale
// error next to an executed marker would tell the user it failed. The error is the
// only thing cleared, not the SQL: the statement that ran is the thing the user
// wants to see afterwards.
func TestMarkExecuted_ClearsAPreviousErrorButKeepsTheSQL(t *testing.T) {
	a := newAskForTest()
	a.AddQuestion("how many users")
	a.SetGeneratedSQL("SELECT count(*) FROM users")
	a.SetError(errors.New("deadlock detected"))
	a.MarkExecuted()

	turns := a.Turns()
	if turns[0].Err != "" {
		t.Errorf("the turn still carries the error %q after executing", turns[0].Err)
	}
	if turns[0].SQL != "SELECT count(*) FROM users" {
		t.Errorf("the turn's SQL is %q, want it kept: the statement that ran is what the user wants to see", turns[0].SQL)
	}
	if turns[0].Status != TurnExecuted {
		t.Errorf("the turn is %v, want TurnExecuted", turns[0].Status)
	}
}

// --- the view ---------------------------------------------------------------

// Scenario: El panel tiene el tamaño que le corresponde, y nunca menos de 50 por 10.
//
// The box is three quarters of the terminal wide and four fifths tall, floored at 50
// by 10 so a question is readable on a small terminal, and capped at 90 wide because
// a SQL statement that wraps into a column that wide is unreadable. There is no cap
// on the height, which is why a tall terminal gets a tall box rather than one sized
// to its content.
//
// Total rendered height is the box height exactly, borders included, at every size.
func TestView_TheBoxIsSizedFromTheTerminal(t *testing.T) {
	for _, tc := range []struct {
		name          string
		width, height int
		wantW         int
		wantH         int
	}{
		// Three quarters of the width, floored at 50.
		{"a normal terminal", 120, 40, 90, 32},
		{"three quarters lands under the floor", 40, 20, 50, 16},
		{"three quarters lands on the floor", 66, 20, 50, 16},
		{"just over the floor", 68, 20, 51, 16},
		{"three quarters lands above the cap", 200, 60, 90, 48},
		{"a wide short terminal", 100, 30, 75, 24},
		// Four fifths of the height, floored at 10.
		{"a short terminal", 120, 12, 90, 10},
		{"four fifths under the floor", 120, 11, 90, 10},
		{"four fifths on the floor", 120, 13, 90, 10},
		{"a tall terminal", 120, 100, 90, 80},
		// Degenerate sizes: the floors are what keep the box drawable at all.
		{"nothing at all", 0, 0, 50, 10},
		{"one cell", 1, 1, 50, 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := New(theme.Resolve("dark").Styles())
			a.SetWidth(tc.width)
			a.SetHeight(tc.height)
			a.Show()

			view := a.View()
			lines := strings.Split(ansi.Strip(view), "\n")
			if len(lines) != tc.wantH {
				t.Errorf("%dx%d rendered %d lines, want %d (four fifths of the height, floored at 10)", tc.width, tc.height, len(lines), tc.wantH)
			}
			for i, line := range lines {
				if got := lipgloss.Width(line); got != tc.wantW {
					t.Errorf("%dx%d: line %d is %d cells wide, want %d", tc.width, tc.height, i, got, tc.wantW)
				}
			}
		})
	}
}

// Scenario: Cada vuelta escribe su pregunta, y su SQL, su error y su marca si los tiene.
//
// The transcript is four optional lines per turn plus a blank separator, and each
// line's presence is decided by the turn's own state. A turn with no SQL yet shows
// no SQL line — showing an empty "SQL:" would look like a statement that came back
// blank, which is a different and much worse thing.
func TestView_EachTurnWritesOnlyTheLinesItHas(t *testing.T) {
	styles := theme.Resolve("dark").Styles()

	// A turn in each of the four statuses, in one transcript.
	full := New(styles)
	full.SetWidth(120)
	full.SetHeight(60)
	full.Show()
	full.turns = []Turn{
		{Question: "just asked", Status: TurnGenerating},
		{Question: "under review", SQL: "SELECT 1", Status: TurnReview},
		{Question: "it failed", SQL: "SELECT 2", Status: TurnError, Err: "relation does not exist"},
		{Question: "it ran", SQL: "SELECT 3", Status: TurnExecuted},
	}

	lines := nonEmpty(contentLines(t, full.View()))
	// The last line is the footer, and the input line sits above it.
	body := lines[:len(lines)-2]

	for _, want := range []string{
		"You: just asked",
		"You: under review",
		"SQL: SELECT 1",
		"You: it failed",
		"SQL: SELECT 2",
		"Error: relation does not exist",
		"You: it ran",
		"SQL: SELECT 3",
		"(executed)",
	} {
		if !containsLine(body, want) {
			t.Errorf("the transcript has no line %q:\n%q", want, body)
		}
	}

	// A turn with no SQL writes no SQL line, and a turn that did not run writes no
	// executed marker. Counting is how this is checked: a rendered "SQL:" for the
	// generating turn would be invisible to a substring search for "SELECT 1".
	if got := countLinesWith(body, "SQL:"); got != 3 {
		t.Errorf("the transcript has %d SQL lines, want 3 (the generating turn has none)", got)
	}
	if got := countLinesWith(body, "Error:"); got != 1 {
		t.Errorf("the transcript has %d error lines, want 1", got)
	}
	if got := countLinesWith(body, "(executed)"); got != 1 {
		t.Errorf("the transcript has %d executed markers, want 1", got)
	}
	if got := countLinesWith(body, "You:"); got != 4 {
		t.Errorf("the transcript has %d question lines, want 4", got)
	}
}

// Scenario: La línea de estado dice en qué estado está el panel.
//
// One line, five states, and each says something the user needs: what is happening,
// or what to press next. The review line names Enter and Esc because that is the only
// place the two are written down, and the idle line is the input with a cursor on it.
func TestView_TheStatusLineNamesTheState(t *testing.T) {
	review := keydisplay.Key("Enter")
	esc := keydisplay.Key("Esc")

	for _, tc := range []struct {
		name  string
		setup func(a *Ask)
		want  string
	}{
		{"idle", func(a *Ask) { a.input = "how many" }, "> how many_"},
		{"idle with an empty line", func(a *Ask) {}, "> _"},
		{"generating", func(a *Ask) { a.AddQuestion("q") }, "Generating SQL..."},
		{"executing", func(a *Ask) {
			a.AddQuestion("q")
			a.SetGeneratedSQL("SELECT 1")
			press(a, keyEnter)
		}, "Executing..."},
		{"error is editable again", func(a *Ask) { a.AddQuestion("q"); a.SetError(errors.New("boom")) }, "> _"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newAskForTest()
			tc.setup(a)
			lines := nonEmpty(contentLines(t, a.View()))
			if len(lines) < 2 {
				t.Fatalf("the view has %d non-empty lines, too few:\n%q", len(lines), lines)
			}
			// The status line is the one before the footer.
			status := strings.TrimSpace(lines[len(lines)-2])
			if status != tc.want {
				t.Errorf("the status line is %q, want %q", status, tc.want)
			}
			if strings.HasSuffix(status, "_") && !strings.HasSuffix(tc.want, "_") {
				t.Errorf("the status line %q shows a cursor, which only the input line does", status)
			}
		})
	}

	t.Run("review", func(t *testing.T) {
		a := newAskForTest()
		a.AddQuestion("how many users")
		a.SetGeneratedSQL("SELECT count(*) FROM users")
		lines := nonEmpty(contentLines(t, a.View()))
		status := strings.TrimSpace(lines[len(lines)-2])
		if !strings.HasPrefix(status, "> ") {
			t.Errorf("the review line %q does not start with the prompt marker", status)
		}
		if !strings.Contains(status, "review the SQL above") {
			t.Errorf("the review line %q does not say what to do", status)
		}
		// The key names go through keydisplay, so they are glyphs on a Nerd Font
		// terminal and the words here.
		if !strings.Contains(status, review) {
			t.Errorf("the review line %q does not name Enter (as %q)", status, review)
		}
		if !strings.Contains(status, esc) {
			t.Errorf("the review line %q does not name Esc (as %q)", status, esc)
		}
	})
}

// Scenario: La línea de entrada enseña lo escrito, con el cursor al final.
//
// The pane is a single-line input, so the cursor is always at the end and the line is
// the input plus a block. No selection, no arrow keys — the text goes to the model
// exactly as typed, and a cursor the user could move would imply an editing model
// that is not there.
func TestView_TheInputLineIsTheInputWithACursorOnIt(t *testing.T) {
	for _, input := range []string{"", "h", "how many users", "a much longer question than fits on one line", "é 日本"} {
		a := newAskForTest()
		a.input = input
		lines := nonEmpty(contentLines(t, a.View()))
		if len(lines) < 2 {
			t.Fatalf("the view has %d non-empty lines:\n%q", len(lines), lines)
		}
		status := lines[len(lines)-2]
		if !strings.HasPrefix(status, "> ") {
			t.Errorf("the input line is %q, want it to start with the prompt marker", status)
		}
		shown := strings.TrimPrefix(status, "> ")
		if !strings.HasSuffix(shown, "_") {
			t.Errorf("the input line %q has no cursor on it", status)
		}
		if got := strings.TrimSuffix(shown, "_"); got != input {
			t.Errorf("the input line shows %q, want %q", got, input)
		}
	}
}

// Scenario: El panel se llena con líneas de relleno, y nunca se desborda.
//
// The content is padded to exactly the inner height minus the footer, so the box is
// the same height whether there is one question or none, and the input line does not
// jump around as the transcript grows. The footer is always last.
func TestView_TheContentIsPaddedToAFixedHeight(t *testing.T) {
	for _, turns := range []int{0, 1, 2, 5} {
		t.Run(fmt.Sprintf("%d turns", turns), func(t *testing.T) {
			a := New(theme.Resolve("dark").Styles())
			a.SetWidth(120)
			a.SetHeight(40) // -> box 32 tall, 30 inside, 29 of content
			a.Show()
			for i := range turns {
				a.AddQuestion(fmt.Sprintf("question %d", i))
			}

			lines := contentLines(t, a.View())
			// Borders excluded by contentLines, so the box height minus two.
			if len(lines) != 30 {
				t.Errorf("%d turns rendered %d content lines, want 30", turns, len(lines))
			}
			// The footer is the last line, and it is not padded away.
			if got, want := strings.TrimSpace(lines[len(lines)-1]), strings.TrimSpace(keydisplay.Key(" Enter send · Esc close")); got != want {
				t.Errorf("the last content line is %q, want the footer %q", got, want)
			}
		})
	}
}

// Scenario: Cuando no cabe, se muestran las líneas MÁS NUEVAS.
//
// A long transcript is trimmed from the TOP, so what the user is looking at — the
// question just asked, the statement just generated — always survives, and the panel
// does not scroll the interesting part out of view. The number kept is the inner
// height minus the status line, which is what the input occupies.
//
// This is a LINE count, not a display height, and that is worth pinning: a line of
// CJK is two cells tall's worth of width but still one line, so a turn boundary can
// end up cut in half with its "SQL:" line on screen and its "You:" line gone.
func TestView_AnOverflowingTranscriptKeepsTheNewestLines(t *testing.T) {
	a := New(theme.Resolve("dark").Styles())
	a.SetWidth(120)
	a.SetHeight(40) // -> 29 content lines before the footer
	a.Show()

	// 40 turns of 2 lines each is 80, comfortably over 29.
	const n = 40
	for i := range n {
		a.AddQuestion(fmt.Sprintf("q%02d", i))
	}

	lines := contentLines(t, a.View())
	if len(lines) != 30 {
		t.Fatalf("rendered %d content lines, want 30 whatever the transcript length", len(lines))
	}
	body := lines[:28] // line 28 is the status line

	// The newest question is on screen and the oldest is not.
	if !containsLine(body, "You: q39") {
		t.Errorf("the newest question is not on screen:\n%q", body)
	}
	if containsLine(body, "You: q00") {
		t.Errorf("the oldest question is still on screen, so the trim kept the wrong end:\n%q", body)
	}
	// Exactly which lines. 40 turns write 2 lines each (the question and a blank
	// separator) and the status line brings it to 81, of which 29 are kept — so
	// 52 are dropped from the top. Line 52 is q26's question, since turn i owns
	// lines 2i and 2i+1. Worked out from the arithmetic rather than counted, so
	// a change to the transcript's shape shows up here as a wrong number.
	const totalBeforeTrim = 40*2 + 1
	const kept = 29
	if totalBeforeTrim != 81 || kept != 29 {
		t.Fatalf("the fixture is %d lines trimmed to %d: fix the fixture, not the code", totalBeforeTrim, kept)
	}
	firstKept := totalBeforeTrim - kept
	if wantTurn := fmt.Sprintf("q%02d", firstKept/2); !containsLine(body, "You: "+wantTurn) {
		t.Errorf("the oldest surviving question is not %s, so the trim count is off:\n%q", wantTurn, body)
	}
	if prev := fmt.Sprintf("q%02d", firstKept/2-1); containsLine(body, "You: "+prev) {
		t.Errorf("%s survived, so one turn too many was kept:\n%q", prev, body)
	}

	// And the status line is still the last of the kept lines, with the footer
	// after it: 29 kept means indices 0..28, of which 28 are transcript.
	status := lines[kept-1]
	if !strings.Contains(status, "Generating SQL...") {
		t.Errorf("the last kept line is %q, want the status line", status)
	}
	if got := strings.TrimSpace(lines[kept]); got != strings.TrimSpace(keydisplay.Key(" Enter send · Esc close")) {
		t.Errorf("the line after the kept block is %q, want the footer", got)
	}
}

// --- helpers ----------------------------------------------------------------

func containsLine(lines []string, want string) bool {
	for _, l := range lines {
		if strings.Contains(l, want) {
			return true
		}
	}
	return false
}

func countLinesWith(lines []string, want string) int {
	n := 0
	for _, l := range lines {
		if strings.Contains(l, want) {
			n++
		}
	}
	return n
}

// Scenario: MarkExecuted sin vueltas no revienta, y no hace nada.
//
// The same guard as SetGeneratedSQL and SetError, and the same reason it needs its
// own test: the panel is a zero value until the app shows it, and every one of these
// setters can be reached before a question exists. There is nothing to mark, so the
// call is a no-op — and the state is left alone, which is the recorded behaviour of
// MarkExecuted either way.
func TestMarkExecuted_WithNoTurnsIsANoOp(t *testing.T) {
	a := newAskForTest()

	a.MarkExecuted()

	if len(a.Turns()) != 0 {
		t.Errorf("the transcript holds %d turns, want none", len(a.Turns()))
	}
	if a.state != stateTyping {
		t.Errorf("the state is %v, want it untouched at stateTyping", a.state)
	}
}

// Scenario: Un tamaño exacto de transcripción no rompe nada.
//
// The trim fires on `len(lines) > innerH-1` and the padding loop then pads back up to
// the same count, so the rendered height is constant for every transcript length. The
// interesting lengths are the three AT the boundary rather than past it: exactly one
// short of the threshold, exactly at it, and exactly one over.
//
// At the threshold the trim is a no-op that keeps the whole slice, and one over it
// trims away a single line and pads a blank back. Both are asserted on the HEIGHT,
// because the height is the only thing that can tell them apart at these sizes — the
// content is the same either way.
func TestView_TheHeightIsConstantAcrossTheTranscriptBoundary(t *testing.T) {
	// A generating turn writes 2 lines (the question and a blank separator) and
	// the status line is the 3rd kind, so n turns means 2n+1 lines before the trim.
	// With height 40 the box is 32 tall, 30 inside, and 29 of content before the
	// footer.
	const contentH = 29

	for _, tc := range []struct {
		name  string
		turns int
	}{
		{"empty", 0},
		{"well under the threshold", 5},
		{"two under", (contentH - 3) / 2},                // 27 lines
		{"one under", (contentH - 1) / 2},                // 29 lines == the threshold exactly
		{"exactly at the threshold", (contentH + 1) / 2}, // 29 lines
		{"one over the threshold", (contentH + 3) / 2},   // 31 lines
		{"two over", (contentH + 5) / 2},                 // 33 lines
		{"far over", 40},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := New(theme.Resolve("dark").Styles())
			a.SetWidth(120)
			a.SetHeight(40)
			a.Show()
			for i := range tc.turns {
				a.AddQuestion(fmt.Sprintf("q%02d", i))
			}

			lines := contentLines(t, a.View())
			// contentLines drops the two border lines, so 32 becomes 30.
			if want := contentH + 1; len(lines) != want {
				t.Errorf("%d turns (%d lines before the trim) rendered %d content lines, want %d",
					tc.turns, 2*tc.turns+1, len(lines), want)
			}
			// The footer is always the last of them.
			if got, want := strings.TrimSpace(lines[len(lines)-1]), strings.TrimSpace(keydisplay.Key(" Enter send · Esc close")); got != want {
				t.Errorf("the last content line is %q, want the footer", got)
			}
		})
	}
}
