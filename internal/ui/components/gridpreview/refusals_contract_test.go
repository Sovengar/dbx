package gridpreview

// Scenario: Los brazos de rechazo de un panel que dibuja datos, uno por uno.
//
// Everything in this file is an arm the happy path never takes: a path that names
// nothing, an index that is not a number, a home directory that cannot be found, a
// document that is not JSON. They cluster because they share a cause — a panel that
// renders whatever it is given and must decide what to do when the input does not fit —
// and because each one is a two-line guard whose only observable is a line of text on
// screen, which is the kind of guard that gets deleted later as "defensive".
//
// One is NOT reachable and says so: strings.Split never returns an empty slice, so
// isExpandableFK's `len(parts) == 0` cannot fire. Pinned rather than removed, because it is
// the shape a reader expects and its absence would look like a missing case.

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/buble/dbx/internal/drivers/postgres"
)

// refusalsDoc is a document with a scalar, an object, an array and an empty array, because
// each one sends navigateJSON down a different arm.
const refusalsDoc = `{
  "name": "widget",
  "count": 3,
  "tags": ["a", "b"],
  "empty": [],
  "nested": {"id": 1, "deep": {"x": true}}
}`

// isExpandableFK's "no" answers are three different situations, and each needs its own
// case: a name that is not a foreign key, a name that WAS one and has been expanded, and
// a nested name whose parent has no nested foreign keys.
func TestAnUnknownPathIsNotAForeignKey(t *testing.T) {
	const fkCol = "user_id"

	withFK := func() *GridPreview {
		p := newPreviewTest()
		p.SetForeignKeys([]postgres.ForeignKeyInfo{{
			Name: "orders_" + fkCol + "_fkey", Column: fkCol,
			RefSchema: "public", RefTable: "users", RefColumn: "id",
		}})
		return p
	}

	t.Run("a name that is not a foreign key", func(t *testing.T) {
		if got := withFK().isExpandableFK("total"); got != nil {
			t.Errorf("a column that is not a foreign key resolved to %+v", got)
		}
	})

	t.Run("an expanded foreign key is no longer expandable", func(t *testing.T) {
		// The distinction the whole function exists for. A path that WAS expandable stops
		// being so, and the only thing that tells the two apart is the expanded set — so a
		// version of this function that forgot it would offer to expand the same key twice
		// and append a duplicate column.
		p := withFK()
		if got := p.isExpandableFK(fkCol); got == nil {
			t.Fatal("a foreign key is not expandable; the fixture is wrong")
		}
		p.expandedFKs[fkCol] = true
		if got := p.isExpandableFK(fkCol); got != nil {
			t.Errorf("an expanded foreign key is still expandable: %+v", got)
		}
	})

	t.Run("a nested path whose parent HAS nested keys but not that one", func(t *testing.T) {
		// The distinction the final `return nil` exists for. The parent resolved, the list was
		// walked, and no entry named the child — which is a different answer from "the parent
		// has no nested keys at all", and only one of them is a lookup that FOUND something.
		//
		// It is also the arm nothing reached: the case below covers the missing parent, and
		// without this one the walk-inside-the-parent loop was never observed running.
		p := withFK()
		p.nestedFKs["orders"] = []postgres.ForeignKeyInfo{{
			Name: "orders_other_fkey", Column: "other",
			RefSchema: "public", RefTable: "others", RefColumn: "id",
		}}

		if got := p.isExpandableFK("orders." + fkCol); got != nil {
			t.Errorf("a child the parent's list does not name resolved to %+v", got)
		}

		// And the one it DOES name, from the same list: the counterweight for the whole
		// nested branch, without which "always nil" satisfies the case above.
		if got := p.isExpandableFK("orders.other"); got == nil {
			t.Error("a child the parent's list does name was not found")
		} else if got.RefTable != "others" {
			t.Errorf("it resolved to %+v, want the nested foreign key", got)
		}
	})

	t.Run("a nested path whose parent has no nested keys", func(t *testing.T) {
		// One level deeper, and the parent lookup fails. The nested map is not nil here —
		// it is EMPTY, which is the state a table with no nested relationships reaches, and
		// it is a different path from a parent that was never looked up at all.
		p := withFK()
		if got := p.isExpandableFK("orders." + fkCol); got != nil {
			t.Errorf("a nested path with no nested keys resolved to %+v", got)
		}
	})

	t.Run("and the paths that are foreign keys", func(t *testing.T) {
		// The counterweight, so "everything returns nil" cannot satisfy the three above.
		p := withFK()
		got := p.isExpandableFK(fkCol)
		if got == nil {
			t.Fatal("a declared foreign key is not expandable")
		}
		if got.Column != fkCol || got.RefTable != "users" {
			t.Errorf("it resolved to %+v", got)
		}
	})

	t.Run("the empty-path guard that cannot fire", func(t *testing.T) {
		// DEAD, provably: strings.Split(s, sep) always returns at least one element, for
		// every s and every sep. So `len(parts) == 0` is unreachable and the nil it returns
		// is never produced.
		//
		// Pinned, not removed, because the behaviour is still right — the empty path DOES
		// arrive here, and the `len(parts) == 1` arm answers it by finding no matching
		// foreign key. The guard is just not what makes it right, and the comment above the
		// function reads as though it were.
		p := withFK()
		for _, path := range []string{"", ".", "..", fkCol + ".", "." + fkCol, "a.b.c.d"} {
			if got := p.isExpandableFK(path); got != nil {
				t.Errorf("the path %q resolved to %+v", path, got)
			}
		}
	})
}

// navigateJSON's refusals. The contract is the same for all of them: a path that does not
// address anything resolves to nil rather than to the zero value of whatever happens to be
// there — because "this field does not exist" and "this field is empty" are different
// answers, and a filter built on the second one silently matches the wrong rows.
func TestAPathThatAddressesNothingResolvesToNothing(t *testing.T) {
	doc := previewOf(t, refusalsDoc)

	for _, tc := range []struct {
		name string
		path string
	}{
		{"into a scalar", "name.first"},
		{"into an array by name", "tags.first"},
		{"a name that does not exist", "missing"},
		{"a name below a name that does not exist", "missing.deeper"},
		{"an index that is not a number", "tags[x]"},
		{"an index past the end", "tags[9]"},
		{"an index into the empty array", "empty[0]"},
		{"a negative index", "tags[-1]"},
		{"length of a scalar, bracketed", ".name[length]"},
		{"length of a scalar, bare word", ".name.length"},
		{"a bracket that is never closed", ".tags[0"},
		{"a bracket with no number in it", ".tags[]x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := doc.navigateJSON(doc.rowData, tc.path); got != nil {
				t.Errorf("%q resolved to %#v, want nothing", tc.path, got)
			}
		})
	}

	// The counterweight: the paths that DO address something keep working, because
	// "resolves to nothing" is also what a function that always returned nil would satisfy.
	t.Run("and the paths that do address something still do", func(t *testing.T) {
		for _, tc := range []struct {
			path string
			want interface{}
		}{
			{"name", "widget"},
			{"count", float64(3)},
			{"nested.id", float64(1)},
			{"nested.deep.x", true},
			{"tags[0]", "a"},
			{"tags[1]", "b"},
			// `length` means different things depending on what it lands on — an array's
			// element count, an object's entry count — and BOTH spellings resolve both,
			// because childSuggestions offers the bare word for either kind and the
			// bracketed form appears as `[length]` beside it.
			//
			// An `int`, not a float64, and the asymmetry with every value above is the point:
			// encoding/json decodes JSON numbers to float64 while len() counts in int. The
			// first version of this case wrote float64(2) and compared interface{} values,
			// which panics on slices and silently mismatches on int-vs-float — so the
			// comparison said the numbers differed when they did not. Recorded because the
			// same table would bite anyone adding a case next to these.
			{".tags.length", 2},
			{".tags[length]", 2},
			{".nested.length", 2},
			// `[]` is iteration in jq, and the navigator returns a single value, so the
			// honest equivalent is the whole array. This was element zero once, which made
			// it the same answer as `[0]`.
			{".tags[]", []interface{}{"a", "b"}},
			{".tags[:]", []interface{}{"a", "b"}},
			// And the identity: a filter that is just `.` shows the document. Blanking the
			// panel on a reset would be a bug, and the first version of this case expected
			// nil for the empty path — the code was right and the expectation was not.
			{".", doc.rowData},
			{"", doc.rowData},
		} {
			// reflect.DeepEqual, not ==, and the reason is written down because this file
			// walks straight into it otherwise: the table holds []interface{} rows, and
			// comparing two interface{} values that both hold a slice PANICS at runtime.
			// DeepEqual compares what they contain.
			if got := doc.navigateJSON(doc.rowData, tc.path); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("%q resolved to %#v, want %#v", tc.path, got, tc.want)
			}
		}
	})

	t.Run("length of a scalar is not offered, and does not resolve", func(t *testing.T) {
		// `length` of a string is a real jq expression, so this reads like a gap. It is not
		// reachable from the popup because childSuggestions does not offer `length` for a
		// string — a string has no children to suggest from. Asserted because the natural
		// next change is to make `length` work everywhere, and that change would have to
		// add the suggestion too or the feature would be unreachable for the same reason.
		for _, path := range []string{".name.length", ".name[length]"} {
			if got := doc.navigateJSON(doc.rowData, path); got != nil {
				t.Errorf("%s resolved to %#v", path, got)
			}
		}
	})
}

// The jq history file. All three of load, save and the path itself ask for a path first
// and give up when there is none, which is the case of a home directory that cannot be
// found — an unusual but real state for a container or a service account, and one where the
// app must not crash because a user typed a filter.
func TestTheJQHistorySurvivesHavingNoHomeDirectory(t *testing.T) {
	// t.Setenv restores whatever was there, and the lookup os.UserHomeDir performs is $HOME
	// on every platform this runs on.
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")

	p := newPreviewTest()

	if got := p.historyFilePath(); got != "" {
		t.Errorf("with no home the path is %q, want the empty string", got)
	}
	// Both must be no-ops rather than errors, and must not touch the history.
	p.loadJQHistory()
	if len(p.jqHistory) != 0 {
		t.Errorf("loading with no home produced %d entries", len(p.jqHistory))
	}
	p.jqHistory = append(p.jqHistory, "a.b")
	p.saveJQHistory()
	if len(p.jqHistory) != 1 || p.jqHistory[0] != "a.b" {
		t.Errorf("saving with no home changed the history to %v", p.jqHistory)
	}
}

// And the saver's other refusal, reachable with a home directory: a parent path that is a
// FILE rather than a directory. The history lives at $HOME/.config/dbx/jq_history or
// wherever it lives, so making one level of that a file makes the mkdir impossible.
func TestTheJQHistoryRefusesToWriteWhereItCannot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	p := newPreviewTest()
	// Whatever path the preview wants, make its parent a FILE.
	path := p.historyFilePath()
	if path == "" {
		t.Fatal("there is no history path to block; the fixture is wrong")
	}
	blocked := filepath.Dir(filepath.Dir(path))
	if err := os.MkdirAll(filepath.Dir(blocked), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blocked, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}

	p.jqHistory = []string{"a.b", ".c[0]"}
	p.saveJQHistory()

	// The property that matters: the history survives IN MEMORY, so the user still has
	// their filters for this session. A saver that cleared the history on failure would be
	// worse than one that does not write.
	if len(p.jqHistory) != 2 || p.jqHistory[0] != "a.b" {
		t.Errorf("the history became %v", p.jqHistory)
	}

	t.Run("and it does write when it can", func(t *testing.T) {
		// The counterweight, because "the saver always fails" satisfies the case above. Its
		// own HOME: t.Setenv in the parent applies to subtests too, so without this the
		// counterweight would run against the very directory the parent just turned into a
		// file and fail for the reason it is meant to disprove.
		t.Run("with a home of its own it writes", func(t *testing.T) {
			own := t.TempDir()
			t.Setenv("HOME", own)
			t.Setenv("USERPROFILE", own)

			writable := newPreviewTest()
			writable.jqHistory = []string{"a.b", ".c[0]"}
			writable.saveJQHistory()

			data, err := os.ReadFile(writable.historyFilePath())
			if err != nil {
				t.Fatalf("the history was not written: %v", err)
			}
			if !strings.Contains(string(data), "a.b") || !strings.Contains(string(data), ".c[0]") {
				t.Errorf("the file holds %q", data)
			}

			// And a second preview reads it back, which is the round trip the file exists for.
			reloaded := newPreviewTest()
			reloaded.loadJQHistory()
			if len(reloaded.jqHistory) != 2 {
				t.Fatalf("the history read back as %v", reloaded.jqHistory)
			}
			if reloaded.jqHistory[0] != "a.b" {
				t.Errorf("the first entry read back as %q", reloaded.jqHistory[0])
			}
		})
	})
}

// Update's refusals, which are the difference between "this panel used the key" and "this
// panel did not touch the key and the app should keep looking".
func TestUpdateRefusesWhatItCannotOwn(t *testing.T) {
	t.Run("an unfocused preview claims nothing", func(t *testing.T) {
		p := newPreviewTest()
		if cmd, handled := p.Update(tea.KeyPressMsg{Code: 'j', Text: "j"}); handled || cmd != nil {
			t.Error("an unfocused preview claimed a key")
		}
	})

	t.Run("a key bound to nothing in this view is declined", func(t *testing.T) {
		// The distinction that matters to the app: a key Resolve does not know is not the
		// same as a key with no handler. Both return false and the app treats both as "not
		// mine", so the difference is invisible — pinned because a future change that made
		// them differ would need to know they currently do not.
		p := newPreviewTest()
		p.Focus()

		for _, key := range []tea.KeyPressMsg{
			{Code: tea.KeyF13},
			{Code: tea.KeyF5},
			{Code: tea.KeyPrintScreen},
		} {
			if _, handled := p.Update(key); handled {
				t.Errorf("the unbound key %q was claimed", key)
			}
		}

		// A key that IS bound, as the counterweight.
		if _, handled := p.Update(tea.KeyPressMsg{Code: 'j', Text: "j"}); !handled {
			t.Error("j was declined; the keybinds registry is not being consulted")
		}
	})

	t.Run("a preview with no keybinds at all claims nothing", func(t *testing.T) {
		// The nil resolver. A caller that has no config still builds a preview, and
		// dereferencing a nil interface would panic on the first keypress.
		p := New(nil, nil)
		p.Focus()
		if _, handled := p.Update(tea.KeyPressMsg{Code: 'j', Text: "j"}); handled {
			t.Error("a preview with no keybinds claimed a key")
		}
	})
}

// Scrolling is clamped at both ends. The clamp at the bottom is the one that can be
// forgotten: a document shorter than the window has no scroll range at all, and a cursor
// that walks past the end leaves the view showing nothing.
func TestScrollingNeverGoesPastTheContent(t *testing.T) {
	// Tall enough that there IS a scroll range, and the cursor is then walked far past the
	// end so the bottom clamp is exercised rather than assumed.
	doc := previewOf(t, refusalsDoc)
	doc.SetWidth(80)
	doc.SetHeight(12)
	doc.lines = make([]string, 60)

	contentHeight := doc.height - 6
	if contentHeight <= 0 {
		t.Fatalf("a height of %d leaves no content height; the fixture is wrong", doc.height)
	}

	// The walk stops at the LAST LINE rather than running past it. ensureCursorVisible's job
	// is to keep the window on the content, and it does that by capping scrollY — so a
	// cursor walked beyond the final row leaves it one row below the window, which is the
	// clamp working and not a bug. Production bounds the cursor elsewhere, so asserting
	// "the cursor is always inside the window" would be asserting a property this function
	// does not have and does not need.
	lastLine := len(doc.lines) - 1
	for range lastLine {
		doc.cursorLine++
		doc.ensureCursorVisible()

		maxScroll := len(doc.lines) - contentHeight
		if maxScroll < 0 {
			maxScroll = 0
		}
		if doc.scrollY > maxScroll {
			t.Fatalf("scrollY is %d, past the maximum %d", doc.scrollY, maxScroll)
		}
		if doc.scrollY < 0 {
			t.Fatalf("scrollY went negative: %d", doc.scrollY)
		}
		if doc.cursorLine < doc.scrollY || doc.cursorLine >= doc.scrollY+contentHeight {
			t.Fatalf("the cursor at %d is outside the window [%d, %d)",
				doc.cursorLine, doc.scrollY, doc.scrollY+contentHeight)
		}
	}

	if doc.cursorLine != lastLine {
		t.Fatalf("the walk stopped at %d, want the last line %d", doc.cursorLine, lastLine)
	}

	t.Run("and back to the top", func(t *testing.T) {
		for range lastLine {
			doc.cursorLine--
			doc.ensureCursorVisible()
			if doc.scrollY < 0 {
				t.Fatalf("scrollY went negative: %d", doc.scrollY)
			}
			if doc.cursorLine < doc.scrollY {
				t.Fatalf("the cursor at %d is above the window starting at %d", doc.cursorLine, doc.scrollY)
			}
		}
		if doc.cursorLine != 0 || doc.scrollY != 0 {
			t.Errorf("the cursor ended at %d with scrollY %d, want both 0", doc.cursorLine, doc.scrollY)
		}
	})
}

// A document that is not JSON. The jq path has three failure messages and this is the
// middle one: the parse error and the runtime error are reachable with valid JSON, so the
// unmarshal arm is the one a test can forget.
func TestJQOverSomethingThatIsNotJSON(t *testing.T) {
	p := previewOf(t, refusalsDoc)
	// The preview builds rawJSON by marshalling its own row, so bad bytes have to be put
	// there directly — which is exactly the situation this covers: the row came from
	// somewhere that produced bytes this preview cannot parse.
	p.rawJSON = []byte("this is not json")
	p.jqExpr = "."

	p.applyJQ()

	joined := strings.Join(p.lines, "\n")
	if !strings.Contains(joined, "unmarshal error") {
		t.Errorf("the panel says %q, want an unmarshal error", joined)
	}
	// Shown rather than swallowed: a panel that went blank on bad input would leave the user
	// with no filter and no explanation of why.
	if len(p.lines) == 0 {
		t.Error("the panel is empty")
	}

	t.Run("and no rawJSON leaves the panel alone", func(t *testing.T) {
		// The other side of the same guard, and the one a user hits by closing the preview
		// before typing. It leaves the lines EXACTLY as they were rather than blanking
		// them — the first version of this case expected an empty panel, and the code was
		// right: there is nothing new to show, and clearing what is on screen would be a
		// worse answer than showing what was already there.
		empty := previewOf(t, refusalsDoc)
		before := strings.Join(empty.lines, "\n")
		empty.rawJSON = nil
		empty.jqExpr = "."
		empty.applyJQ()
		if got := strings.Join(empty.lines, "\n"); got != before {
			t.Errorf("the panel changed with nothing to filter:\n%q\nthen\n%q", before, got)
		}
	})

	t.Run("and no filter shows the document", func(t *testing.T) {
		// The third arm of the same function: an empty expression is not "no output", it is
		// "show me the document". Confusing the two would blank the panel on every reset.
		shown := previewOf(t, refusalsDoc)
		before := strings.Join(shown.lines, "\n")
		shown.jqExpr = ""
		shown.applyJQ()
		if got := strings.Join(shown.lines, "\n"); got != before {
			t.Errorf("clearing the filter changed the panel:\n%q\nthen\n%q", before, got)
		}
	})
}
