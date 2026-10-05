package gridpreview

// Scenario: Lo que queda del preview: los rechazos que el camino feliz no toca.
//
// Eight arms, and they are four different kinds of thing:
//
//	a guard on state the code maintains elsewhere — the bottom scroll clamp, the span
//	  clamps in navigateJSON
//	a guard on state that arrives from outside — a document that is not JSON, a key the
//	  registry does not bind
//	an action the popup offers that nothing ever triggered
//	a place where a map lookup failed to produce an index
//
// The first kind is the interesting one, because it is the kind that silently rots: the
// invariant lives in a different function from the check, so a change to the writer can
// break the reader with nothing failing.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/drivers/postgres"
)

// The bottom scroll clamp. ensureCursorVisible follows the cursor, so walking it never needs
// the clamp — the clamp exists for the OTHER direction, where the content has shrunk under a
// scroll that was legal a moment ago.
func TestTheScrollClampsWhenTheContentShrinks(t *testing.T) {
	p := previewOf(t, `{"a":1}`)
	p.SetWidth(80)
	p.SetHeight(20)
	p.lines = make([]string, 60)

	// A scroll that was legal when the document was long, on a document that is now short.
	p.scrollY = 50
	p.cursorLine = 55

	p.ensureCursorVisible()

	maxScroll := len(p.lines) - (p.height - 6)
	if maxScroll < 0 {
		maxScroll = 0
	}
	if p.scrollY > maxScroll {
		t.Errorf("scrollY is %d, past the maximum %d for %d lines in a %d-high window",
			p.scrollY, maxScroll, len(p.lines), p.height)
	}
	if p.scrollY < 0 {
		t.Errorf("scrollY is %d", p.scrollY)
	}
}

// The two span clamps in navigateJSON, on the paths that address nothing. Both were the
// subject of the jq-mode contract test; this is the same property asserted through the
// navigator directly, including the bracket form the suggestions never offer.
func TestTheSpanClampsForPathsThatAddressNothing(t *testing.T) {
	doc := previewOf(t, `{"name":"widget","tags":["a","b"],"empty":[],"nested":{"name":"inner"}}`)

	for _, tc := range []struct {
		name string
		path string
	}{
		{"a bracket on a scalar", "name[0]"},
		{"a bracket on a scalar, by name", "name[x]"},
		{"an index past the end", "tags[9]"},
		{"an index into an empty array", "empty[0]"},
		{"a negative index", "tags[-1]"},
		{"a bracket with no number", "tags[]x"},
		{"a bracket never closed", "tags[0"},
		{"below a scalar", "name.deeper"},
		{"a missing name below a missing name", "missing.deeper"},
		// A bracket whose key is looked up on something that is not a map. This is a
		// DIFFERENT refusal from the ones above: the first bracket descends fine and the
		// failure is on the index, whereas here the key lookup itself is on a scalar.
		{"a bracket key on a scalar", "name.x[0]"},
		{"a bracket key on a scalar, deeper", "nested.name[0]"},

		// The BARE-WORD index form — `.tags.1` rather than `.tags[1]` — which is a different
		// block in the navigator with its own refusals, and which the bracket cases above
		// never reach.
		{"a bare index past the end", "tags.9"},
		{"a bare negative index", "tags.-1"},
		{"a bare index into an empty array", "empty.0"},
		{"a bare index that is not a number", "tags.abc"},
		{"a bare index on a scalar", "name.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := doc.navigateJSON(doc.rowData, tc.path); got != nil {
				t.Errorf("%q resolved to %#v, want nothing", tc.path, got)
			}
		})
	}
}

// The bare-word index form resolving to something, which is the counterweight for the five
// bare-word refusals above.
//
// `.tags.0` rather than `.tags[0]`: the two spellings are different blocks in the navigator,
// each with its own index parse, and the bracket cases above never reach the bare one.
func TestTheBareWordIndexFormResolves(t *testing.T) {
	doc := previewOf(t, `{"tags":["a","b"],"empty":[]}`)

	for _, tc := range []struct {
		path string
		want interface{}
	}{
		{".tags.0", "a"},
		{".tags.1", "b"},
		{"tags.0", "a"},
		{"tags.1", "b"},
	} {
		if got := doc.navigateJSON(doc.rowData, tc.path); got != tc.want {
			t.Errorf("%q resolved to %#v, want %#v", tc.path, got, tc.want)
		}
	}
}

// A document that is not JSON, on the path that builds the SUGGESTIONS rather than the
// result. The suggestions read the document once at the top of updateJQSuggestions, so a
// document they cannot parse means no suggestions rather than a crash — and no error, which
// is the part worth pinning.
func TestSuggestionsOverSomethingThatIsNotJSON(t *testing.T) {
	p := previewOf(t, `{"a":1}`)
	p.rawJSON = []byte("not json at all")

	p.updateJQSuggestions()

	if len(p.jqSugs) != 0 {
		t.Errorf("%d suggestions were built from a document that does not parse", len(p.jqSugs))
	}
	if p.jqSugVisible {
		t.Error("the suggestion list is visible over a document that does not parse")
	}

	t.Run("and the popup still opens", func(t *testing.T) {
		// Opening jq mode on an unparseable document must not panic, and must leave the user
		// able to type a filter that will report the problem.
		p.EnterJQMode()
		if !p.jqMode {
			t.Fatal("EnterJQMode did not open jq mode")
		}
		if out := p.Render(); out == "" {
			t.Error("the panel renders nothing with jq mode open over a bad document")
		}
	})
}

// Update's fallthrough, and the action the panel offers that nothing triggered.
//
// `expand_fk` is the interesting one: it is in HandledActions(), so the app's coverage test
// asserts the panel claims it, and no test in the package ever sent it. An action the popup
// advertises and nothing exercises is an action whose behaviour is whatever it happens to do.
func TestTheActionThePanelClaimsAndNothingSent(t *testing.T) {
	t.Run("declines what it does not understand", func(t *testing.T) {
		p := previewOf(t, `{"a":1}`)
		p.Focus()

		for _, msg := range []tea.Msg{
			tea.WindowSizeMsg{Width: 100, Height: 30},
			tea.MouseClickMsg{X: 2, Y: 2},
			struct{ tea.Msg }{},
		} {
			if cmd, handled := p.Update(msg); handled {
				t.Errorf("the panel claimed %T", msg)
			} else if cmd != nil {
				t.Errorf("the panel produced %T for a message it does not understand", cmd())
			}
		}
	})

	t.Run("expand_fk on a path that is not a foreign key", func(t *testing.T) {
		// The refusal. handleExpand's guard is the same isExpandableFK the round-20 tests
		// pinned, reached here through the ACTION rather than through a direct call.
		p := previewOf(t, `{"a":1}`)
		p.SetForeignKeys([]postgres.ForeignKeyInfo{{
			Name: "orders_user_id_fkey", Column: "user_id",
			RefSchema: "public", RefTable: "users", RefColumn: "id",
		}})
		p.Focus()

		cmd, handled := p.dispatchAction("expand_fk")
		if handled {
			t.Errorf("expanding a path that is not a foreign key was handled, producing %T", cmdFuncOf(cmd))
		}
		if len(p.expandedFKs) != 0 {
			t.Errorf("the expansion set is %v after expanding nothing", p.expandedFKs)
		}
	})

	t.Run("expand_fk on a path that IS a foreign key", func(t *testing.T) {
		// The counterweight: without it, "the action always refuses" satisfies the case above
		// and the popup's advertise is a lie.
		//
		// The path comes from the CURSOR LINE, not from a field — handleExpand walks the
		// rendered lines up to the cursor to work out which key it is on. So the cursor has to
		// be ON the user_id line, which is why the first version of this case was refused: it
		// sat on line 0, the opening brace.
		p := previewOf(t, `{"user_id":1}`)
		p.SetForeignKeys([]postgres.ForeignKeyInfo{{
			Name: "orders_user_id_fkey", Column: "user_id",
			RefSchema: "public", RefTable: "users", RefColumn: "id",
		}})
		p.Focus()
		p.cursorLine = lineOf(t, p, "user_id")

		cmd, handled := p.dispatchAction("expand_fk")
		if !handled || cmd == nil {
			t.Fatalf("expanding user_id from line %d was refused", p.cursorLine)
		}

		// The expansion is a REQUEST, not a mutation: the panel emits the message and the app
		// answers with the joined row, which is what makes the expansion cancelable and
		// testable without a database. So the assertion is on the message — the first version
		// of this case checked expandedFKs, which stays empty until the app replies.
		msg, ok := cmd().(GridPreviewExpandFKMsg)
		if !ok {
			t.Fatalf("the expansion produced %T, want GridPreviewExpandFKMsg", cmd())
		}
		if msg.Column != "user_id" {
			t.Errorf("the expansion is for the column %q", msg.Column)
		}
		if msg.Path != "user_id" {
			t.Errorf("the expansion is for the path %q", msg.Path)
		}
		if msg.RefTable != "users" || msg.RefColumn != "id" {
			t.Errorf("the expansion points at %s.%s, want users.id", msg.RefTable, msg.RefColumn)
		}
		// And it carries the value, which is the whole point of the request.
		if msg.Value != float64(1) {
			t.Errorf("the expansion carries the value %#v, want the cell's own", msg.Value)
		}
	})

	t.Run("jq_filter opens the mode", func(t *testing.T) {
		p := previewOf(t, `{"a":1}`)
		p.Focus()

		if _, handled := p.dispatchAction("jq_filter"); !handled {
			t.Fatal("jq_filter was refused")
		}
		if !p.jqMode {
			t.Error("jq_filter did not open jq mode")
		}
	})
}

// The bottom clamp's counterpart: a panel narrower than the suggestion list. The path column
// had a floor of 15 that the width floor above already guaranteed, so removing it changed
// nothing — and this case says so from the render side rather than from the arithmetic.
func TestTheSuggestionsRenderAtEveryWidth(t *testing.T) {
	// From 1, not 0: a zero-width panel is not a window a user can have, and the panel
	// legitimately renders nothing for it. The floor that used to be in pathWidth was about
	// a NARROW window, which starts at 1 and is covered below.
	for _, width := range []int{1, 2, 10, 29, 30, 38, 39, 80, 200} {
		p := previewOf(t, `{"user":{"city":"London"},"items":[{"sku":"a"}]}`)
		p.SetWidth(width)
		p.SetHeight(20)
		p.EnterJQMode()
		p.jqInput = "."
		p.updateJQSuggestions()

		out := p.Render()
		if out == "" {
			t.Errorf("at width %d the panel renders nothing", width)
			continue
		}
		// The border has to survive: a suggestion list that overflows its own panel is the
		// failure the removed floor was there to prevent.
		if !strings.Contains(out, "│") {
			t.Errorf("at width %d the panel has no side border", width)
		}
	}
}

// HandledActions is what the app's coverage test checks the panel against, and nothing in
// this package called it — so a name that stopped being dispatched would still be claimed.
func TestTheHandledActionsAreTheOnesDispatched(t *testing.T) {
	p := newPreviewTest()

	claimed := map[string]bool{}
	for _, id := range p.HandledActions() {
		claimed[string(id)] = true
	}

	// Every action the panel's own switch names is claimed, so the app routes keys to the
	// panel rather than swallowing them.
	for _, id := range []config.ActionID{
		"navigate_up", "navigate_down", "go_first", "go_last",
		"half_page_up", "half_page_down", "jq_filter", "expand_fk",
	} {
		if !claimed[string(id)] {
			t.Errorf("the panel dispatches %q but does not claim it", id)
		}
	}

	// And nothing is claimed that is not dispatched.
	if len(p.HandledActions()) != 8 {
		t.Errorf("the panel claims %d actions, want 8: %v", len(p.HandledActions()), p.HandledActions())
	}
}

// lineOf is the index of the first rendered line containing substr, for the cases where the
// cursor's position is what decides.
func lineOf(t *testing.T, p *GridPreview, substr string) int {
	t.Helper()
	for i, l := range p.lines {
		if strings.Contains(l, substr) {
			return i
		}
	}
	t.Fatalf("no rendered line contains %q; the panel has %d lines: %q", substr, len(p.lines), p.lines)
	return -1
}

func cmdFuncOf(cmd func() tea.Msg) string {
	if cmd == nil {
		return "<nil>"
	}
	msg := cmd()
	if msg == nil {
		return "<nil message>"
	}
	return "a message"
}
