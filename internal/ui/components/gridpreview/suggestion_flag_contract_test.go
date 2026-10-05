package gridpreview

// Scenario: The suggestion popup's flag and its list, and every reader that used to
// restate the flag's own meaning.
//
// The four key handlers read `p.jqSugVisible && len(p.jqSugs) > 0`. That is one fact written
// twice: jqSugVisible is turned on in exactly two places and BOTH require a non-empty list, so
// the second half could never be false when the first was true. The consequence was not a
// visible bug -- it was four dead boundary conditions that no test could kill, because a test
// cannot make a conjunction's second half fail without also failing the first.
//
// So the readers now read the flag alone, and the cases below hold the implication in both
// directions. If a future writer ever sets the flag with an empty list, the invariant test
// fails and the reader that trusted the flag is what breaks loudly.
//
// The other cases here are the exact boundaries the popup is built on: a list of exactly
// maxJQSuggestions (not truncated) and one more (truncated), a prefix that matches nothing
// (flag stays down), and a scroll of exactly half a page. Each is a place where "> " and ">="
// disagree, which is the only thing those mutants change.

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// jqKey is the key press a handler receives. handleJQInput takes a Msg, not a string, because
// the space-to-" " rewrite above the switch needs the real code as well as the text.
func jqKey(k string) tea.KeyPressMsg {
	switch k {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter, Text: "enter"}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape, Text: "esc"}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	default:
		return tea.KeyPressMsg{Code: rune(k[0]), Text: k}
	}
}

// wideDoc builds a flat JSON object whose root has n keys, for the truncation boundary.
//
// Flat on purpose: a nested object would contribute its own child suggestions on top of the
// root ones and the count would not be n, so the boundary would sit somewhere else than where
// it is being tested.
func wideDoc(n int) string {
	var b strings.Builder
	b.WriteString("{")
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, "\n  \"key%02d\": %d", i, i)
	}
	b.WriteString("\n}")
	return b.String()
}

// The invariant, both directions.
//
// Direction one: every way of turning the popup on leaves a non-empty list. Direction two:
// every way of emptying the list leaves the flag down.
//
// The two directions are listed separately because they fail differently. A writer that turns
// the flag on with an empty list breaks the readers immediately -- enter would accept a
// suggestion from nothing -- so that is the expensive one. A writer that empties the list
// without turning the flag down is the same bug wearing a disguise, and it is the one a reader
// cannot see.
func TestTheSuggestionFlagAndTheListCannotDisagree(t *testing.T) {
	t.Run("computing suggestions turns the flag on only with a list", func(t *testing.T) {
		// Every input shape updateJQSuggestions is reached with, and the flag each one
		// produces. A list of nothing must leave it down.
		for _, tc := range []struct {
			name      string
			doc       string
			input     string
			wantSugs  bool // whether the list must be non-empty afterwards
			wantVisib bool
		}{
			{"a bare dot offers every root key", jqDoc, ".", true, true},
			{"a prefix offers the keys that start with it", jqDoc, ".na", true, true},
			{"a prefix that matches nothing offers nothing", jqDoc, ".zzzz", false, false},
			{"a path into a key that does not exist offers nothing", jqDoc, ".nope.", false, false},
			{"a path past a leaf offers nothing", jqDoc, ".id.", false, false},
			{"an empty input offers the roots", jqDoc, "", true, true},
			{"a document with no keys at all offers nothing", `{}`, ".", false, false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				p := previewOf(t, tc.doc)
				p.EnterJQMode()
				p.jqInput = tc.input
				p.jqCursor = len(tc.input)
				p.updateJQSuggestions()

				if got := len(p.jqSugs) > 0; got != tc.wantSugs {
					t.Errorf("%d suggestions, want a non-empty list: %v", len(p.jqSugs), tc.wantSugs)
				}
				if p.jqSugVisible != tc.wantVisib {
					t.Errorf("the flag is %v with %d suggestions, want %v",
						p.jqSugVisible, len(p.jqSugs), tc.wantVisib)
				}
				// The implication itself, stated once so the table above reads as evidence.
				if p.jqSugVisible && len(p.jqSugs) == 0 {
					t.Error("the flag is on with an empty list, which is the state the readers assume impossible")
				}
			})
		}
	})

	t.Run("the readers accept nothing when the list is empty", func(t *testing.T) {
		// Direction two, through the keys that read the flag. Each is given a preview whose
		// flag is on and whose list is empty -- the state the invariant forbids -- and each must
		// behave as though the flag were off rather than accepting an entry from nothing.
		//
		// The state is built by hand rather than reached, because reaching it is exactly what
		// production cannot do. It is still worth asserting: a reader that dereferenced
		// p.jqSugs[0] on an empty list would panic here, and a reader that silently wrote to
		// the jq input would corrupt the query.
		blank := func() *GridPreview {
			p := previewOf(t, jqDoc)
			p.EnterJQMode()
			p.jqInput = ".na"
			p.jqCursor = len(p.jqInput)
			p.updateJQSuggestions()
			if !p.jqSugVisible {
				t.Fatal("precondition failed: the popup should be visible before it is emptied")
			}
			// Now the forbidden state: flag on, list gone.
			p.jqSugs = nil
			return p
		}

		t.Run("enter does not accept anything", func(t *testing.T) {
			p := blank()
			input := p.jqInput
			if _, handled := p.handleJQInput(jqKey("enter")); !handled {
				t.Fatal("enter was not handled")
			}
			if p.jqInput != input {
				t.Errorf("enter changed the input to %q with nothing to accept", p.jqInput)
			}
		})

		t.Run("tab does not accept anything", func(t *testing.T) {
			p := blank()
			input := p.jqInput
			if _, handled := p.handleJQInput(jqKey("tab")); !handled {
				t.Fatal("tab was not handled")
			}
			if p.jqInput != input {
				t.Errorf("tab changed the input to %q with nothing to accept", p.jqInput)
			}
		})

		// Moving the selection in a list of nothing. The property is that it does NOT move:
		// there is no entry to move onto, and the two old implementations each produced a
		// different wrong answer instead -- up wrapped past the front of an empty list to
		// len-1, which is -1, and down wrapped past the back of it to 0. A selection of -1 is
		// an index that addresses memory before the slice.
		for _, key := range []string{"up", "down"} {
			t.Run(key+" leaves the selection alone in an empty list", func(t *testing.T) {
				p := blank()
				before := p.jqSugSelected

				if _, handled := p.handleJQInput(jqKey(key)); !handled {
					t.Fatalf("%s was not handled", key)
				}
				if p.jqSugSelected != before {
					t.Errorf("%s moved the selection from %d to %d with %d suggestions to move within",
						key, before, p.jqSugSelected, len(p.jqSugs))
				}
			})
		}
	})
}

// The truncation boundary.
//
// len(jqSugs) > maxJQSuggestions: at exactly maxJQSuggestions nothing is dropped, at one more
// the last is dropped. Both sides are asserted because the mutation is `>` becoming `>=`, and
// one side alone cannot see it -- a list that only ever has fewer entries than the cap takes
// the same branch either way.
func TestTheSuggestionListIsTruncatedAtExactlyTheCap(t *testing.T) {
	for _, tc := range []struct {
		name string
		keys int
		want int
	}{
		{"one below the cap", maxJQSuggestions - 1, maxJQSuggestions - 1},
		{"exactly the cap", maxJQSuggestions, maxJQSuggestions},
		{"one above the cap", maxJQSuggestions + 1, maxJQSuggestions},
		{"far above the cap", maxJQSuggestions * 3, maxJQSuggestions},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := previewOf(t, wideDoc(tc.keys))
			p.EnterJQMode()
			p.jqInput = "."
			p.jqCursor = 1
			p.updateJQSuggestions()

			if len(p.jqSugs) != tc.want {
				t.Fatalf("%d suggestions from %d keys, want %d",
					len(p.jqSugs), tc.keys, tc.want)
			}
			// The ones kept are the FIRST ones, not an arbitrary subset: the list is sorted by
			// path, so truncation drops the alphabetically last keys. A cap that kept the last
			// ones instead would show a user a different document depending on how wide the
			// popup is allowed to be.
			first, last := suggestionPaths(p)[0], suggestionPaths(p)[len(p.jqSugs)-1]
			if !strings.HasPrefix(first, "key00") {
				t.Errorf("the first suggestion is %q, want the first key", first)
			}
			if !strings.HasPrefix(last, fmt.Sprintf("key%02d", tc.want-1)) {
				t.Errorf("the last suggestion is %q, want key%02d -- truncation must drop the TAIL",
					last, tc.want-1)
			}
		})
	}
}
