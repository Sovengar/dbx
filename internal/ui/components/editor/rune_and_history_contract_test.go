package editor

// Scenario: Las dos funciones de bytes, y el historial.
//
// runeLenAt and nextRuneEnd are the byte-level pair the cursor arithmetic is built on, and
// both have an arm for an INVALID BYTE — a byte sequence that is not UTF-8. It is reachable,
// and it is reachable the only way it could be: by putting a corrupt byte in a line, which is
// what a pasted binary blob or a truncated multi-byte character looks like.
//
// The comment on nextRuneEnd's arm says the point is progress: every loop makes at least one
// step. So the property under test is not "it returns a number" but "it terminates and keeps
// the cursor inside the line" — which is the difference between an editor that survives a
// corrupt cell and one that hangs.
//
// The history's `historyIdx < 0` arm is the other kind: a sentinel that means "not currently
// browsing", set by SetContent when the user edits. Pressing up from there goes to the NEWEST
// entry, which is the opposite end from the one you were on.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	ai "github.com/buble/dbx/internal/ai/context"
	"github.com/buble/dbx/internal/theme"
)

// Every one of these is an INVALID encoding: a lone continuation byte, and multi-byte
// characters truncated after one, two and three bytes. They are written as byte strings so
// the source file stays valid UTF-8.
var corruptEncodings = []struct {
	name string
	s    string
}{
	{"a lone continuation byte", "\x80"},
	{"a truncated two-byte character", "\xc3"},
	{"a truncated three-byte character", "\xe2\x82"},
	{"a truncated four-byte character", "\xf0\x9f"},
	{"two invalid bytes", "\xff\xfe"},
}

// The byte helpers' contract on invalid input, which is stronger than "returns a number":
// utf8.DecodeRuneInString returns (RuneError, 1) for EVERY one of them, so a helper built on
// it always advances. That is what stops a cursor loop wedging on a corrupt cell, and both
// helpers used to carry a `size <= 0` branch that claimed to guarantee it — a branch that
// could not run, guarding a hazard utf8 had already handled.
func TestTheByteHelpersWalkACorruptLine(t *testing.T) {
	t.Run("runeLenAt measures a corrupt byte as one", func(t *testing.T) {
		for _, tc := range corruptEncodings {
			t.Run(tc.name, func(t *testing.T) {
				// One, not zero. A zero here would be the wedge: every caller adds this answer
				// to an index and steps.
				if got := runeLenAt(tc.s, 0); got != 1 {
					t.Errorf("runeLenAt(%q, 0) = %d, want 1 — a zero would wedge every loop that adds it", tc.s, got)
				}
			})
		}

		// The two ends it does answer differently.
		if got := runeLenAt("abc", len("abc")); got != 0 {
			t.Errorf("runeLenAt at the end of the string gave %d, want 0 — the end IS zero", got)
		}
		if got := runeLenAt("", 0); got != 0 {
			t.Errorf("runeLenAt on an empty string gave %d, want 0", got)
		}
		if got := runeLenAt("é", 0); got != 2 {
			t.Errorf("runeLenAt at a two-byte character gave %d, want 2", got)
		}
		if got := runeLenAt("\U0001F389", 0); got != 4 {
			t.Errorf("runeLenAt at a four-byte character gave %d, want 4", got)
		}
	})

	t.Run("nextRuneEnd advances by exactly one", func(t *testing.T) {
		for _, tc := range corruptEncodings {
			t.Run(tc.name, func(t *testing.T) {
				if got := nextRuneEnd(tc.s, 0); got != 1 {
					t.Errorf("nextRuneEnd(%q, 0) = %d, want 1", tc.s, got)
				}
			})
		}
		if got := nextRuneEnd("abc", len("abc")); got != len("abc") {
			t.Errorf("nextRuneEnd at the end gave %d, want %d — it must not step past", got, len("abc"))
		}
	})

	t.Run("and a line of corrupt bytes is walked exactly once each", func(t *testing.T) {
		// The property, by exhaustion: walking a corrupt line terminates and lands on its
		// length. This is the assertion that would catch a wedge, and it is the one the
		// removed branch's comment claimed to provide.
		var line string
		for _, tc := range corruptEncodings {
			line += tc.s + "SELECT"
		}

		i := 0
		for range 500 {
			if i >= len(line) {
				break
			}
			before := i
			i = nextRuneEnd(line, i)
			if i <= before {
				t.Fatalf("nextRuneEnd(%q, %d) = %d, which does not advance", line, before, i)
			}
		}
		if i != len(line) {
			t.Errorf("walking a %d-byte corrupt line ended at %d", len(line), i)
		}
	})

	t.Run("and an editor with a corrupt byte in it still moves", func(t *testing.T) {
		// The end of the chain: the helpers exist for the cursor, so what matters is that the
		// cursor moves and stays inside the line.
		e := NewSQLEditor(theme.Resolve("dark").Styles())
		e.SetContent("\x80" + "SELECT 1")
		e.Focus()

		for _, key := range []tea.KeyPressMsg{
			{Code: tea.KeyLeft}, {Code: tea.KeyRight}, {Code: tea.KeyHome}, {Code: tea.KeyEnd},
		} {
			for range 30 {
				e.Update(key)
				line := e.lines[e.cursorRow]
				if e.cursorCol < 0 || e.cursorCol > len(line) {
					t.Fatalf("%v left the cursor at %d of a line of %d bytes", key, e.cursorCol, len(line))
				}
			}
		}
	})
}

// SetContent's one-line invariant. strings.Split returns at least one element for any input,
// so an editor set to "" has one blank line rather than none — which is what keeps
// e.lines[e.cursorRow] from panicking on the first keystroke in an empty editor.
func TestSetContentAlwaysHasALine(t *testing.T) {
	e := NewSQLEditor(theme.Resolve("dark").Styles())

	for _, tc := range []struct{ name, content string }{
		{"nothing at all", ""},
		{"one line", "SELECT 1"},
		{"a trailing newline", "SELECT 1\n"},
		{"a leading newline", "\nSELECT 1"},
		{"several lines", "SELECT 1\nFROM t\nWHERE x"},
		{"only newlines", "\n\n\n"},
		{"a corrupt line", "\x80"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e.SetContent(tc.content)

			if len(e.lines) == 0 {
				t.Fatalf("SetContent(%q) left the editor with no lines, so every cursor index panics", tc.content)
			}
			// And the first line is what an empty document should be: blank, not absent.
			if tc.content == "" && e.lines[0] != "" {
				t.Errorf("SetContent(\"\") gave %q as the first line", e.lines[0])
			}
			e.Focus()
			e.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
			if len(e.lines) == 0 {
				t.Fatal("typing into the editor left it with no lines")
			}
		})
	}
}

// The history's "not browsing" sentinel. PushHistory sets it when the user edits, and the
// first press of the up arrow from there jumps to the NEWEST entry rather than stepping back
// from wherever the cursor was.
func TestTheFirstPressUpFromTheNewestEntry(t *testing.T) {
	e := NewSQLEditor(theme.Resolve("dark").Styles())
	e.Focus()

	// No history at all: the guard, and the one that keeps the up arrow from indexing an
	// empty slice.
	if len(e.history) == 0 {
		e.historyPrev()
		if e.lines[0] != "" {
			t.Errorf("history navigation on an empty history produced %q", e.lines[0])
		}
	}

	e.PushHistory("SELECT 1")
	e.PushHistory("SELECT 2")
	e.PushHistory("SELECT 3")

	// PushHistory left the sentinel set, so this is the arm: the newest entry, not the one
	// after it.
	e.historyPrev()
	if got := strings.Join(e.lines, "\n"); got != "SELECT 3" {
		t.Errorf("the first press up gave %q, want the newest entry", got)
	}

	// And then it walks backwards one at a time.
	e.historyPrev()
	if got := strings.Join(e.lines, "\n"); got != "SELECT 2" {
		t.Errorf("the second press up gave %q", got)
	}

	// The other arm, at the top of the history: pressing up again stays put rather than
	// running off the end.
	e.historyPrev()
	e.historyPrev()
	e.historyPrev()
	if got := strings.Join(e.lines, "\n"); got != "SELECT 1" {
		t.Errorf("pressing up past the oldest entry gave %q", got)
	}

	// And back down, which is the other half of the sentinel's round trip.
	e.historyNext()
	if got := strings.Join(e.lines, "\n"); got != "SELECT 2" {
		t.Errorf("pressing down from the oldest entry gave %q", got)
	}
}

// The invariant that replaced acceptCompletion's two span clamps.
//
// The clamps were `start < 0 || start > len(line)` and `end < start || end > len(line)`, and
// neither could fire: acceptCompletion is reached only with the popup visible, and every
// mutation of the line refreshes the span from that line. So the span is always inside the
// line by the time it is used — a property of the CALLERS, not of the callee, and therefore
// worth holding here rather than inside the function the guards were removed from.
//
// The mutations below are every one handleKey has, crossed with four editor states that open a
// popup. A new edit path that skipped the refresh would fail this, instead of being caught by
// a guard that no longer exists.
func TestTheSpanIsNeverStaleWhenAccepting(t *testing.T) {
	typeText := func(e *SQLEditor, text string) {
		for _, r := range text {
			e.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		}
	}

	mutations := []struct {
		name string
		keys []tea.KeyPressMsg
	}{
		{"backspace", []tea.KeyPressMsg{{Code: tea.KeyBackspace}}},
		{"delete", []tea.KeyPressMsg{{Code: tea.KeyDelete}}},
		{"a space", []tea.KeyPressMsg{{Code: ' ', Text: " "}}},
		{"a typed character", []tea.KeyPressMsg{{Code: 'i', Text: "i"}}},
		{"a newline", []tea.KeyPressMsg{{Code: tea.KeyEnter}}},
		{"an indent", []tea.KeyPressMsg{{Code: tea.KeyTab}}},
		{"a cursor move", []tea.KeyPressMsg{{Code: tea.KeyLeft}, {Code: tea.KeyRight}}},
	}

	ran := 0
	for _, typed := range []string{"SELECT * FROM ", "SELECT * FROM or", "SELECT o", "SEL"} {
		for _, m := range mutations {
			ran++
			t.Run(typed+"/"+m.name, func(t *testing.T) {
				e := NewSQLEditor(theme.Resolve("dark").Styles())
				e.SetAutocompleteConfig(true, 1)
				e.SetWidth(80)
				e.SetHeight(20)
				e.SetSchema(&ai.SchemaExport{
					Database: "shopdb",
					Schemas: []ai.SchemaInfo{{
						Name: "public",
						Tables: []ai.TableInfo{{
							Name: "orders", Type: "BASE TABLE",
							Columns: []ai.ColumnInfo{{Name: "id", DataType: "integer"}},
						}},
					}},
				})
				e.Focus()
				e.SetContent("")
				typeText(e, typed)

				if !e.autocomplete.Visible() {
					t.Skipf("no popup after typing %q, so there is no span to check", typed)
				}

				for _, k := range m.keys {
					e.Update(k)
				}
				if !e.autocomplete.Visible() {
					return // the mutation closed the popup, so there is no span in use
				}

				start, end := e.autocomplete.CompletionSpan()
				line := e.lines[e.cursorRow]
				if start < 0 || start > len(line) || end < start || end > len(line) {
					t.Fatalf("the span %d..%d is outside a line of %d bytes (%q) after %s",
						start, end, len(line), line, m.name)
				}

				e.acceptSelectedCompletion()
				if e.cursorCol < 0 || e.cursorCol > len(e.lines[e.cursorRow]) {
					t.Errorf("the cursor is at %d of a line of %d bytes", e.cursorCol, len(e.lines[e.cursorRow]))
				}
			})
		}
	}
	if ran == 0 {
		t.Fatal("no case was built, so the invariant is untested")
	}
}
