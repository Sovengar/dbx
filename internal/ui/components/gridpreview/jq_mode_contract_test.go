package gridpreview

// Scenario: El modo jq del preview — que es donde el popup se convierte en algo que el
// usuario acepta con TAB.
//
// JQ mode turns the preview from a viewer into a filter: the user types an expression and
// the row is re-rendered through it. The suggestion bar exists because the expression
// syntax is dotted paths and nobody remembers them, and because enter and tab ACCEPT the
// selected entry — which makes the ordering load-bearing rather than cosmetic.
//
// Two properties carry that weight, and they are what this file is about:
//
//	SORTED. Go iterates a map in a random order, so an unsorted list picks a different
//	field each time the expression is recomputed. Pressing tab twice on the same document
// gave two different completions, and the list reordered under the cursor.
//
//	EVERY PATH RESOLVES. A suggestion that does not resolve to something makes the
// expression fail, and the user cannot tell whether the field they wanted is missing or
//	the suggestion was wrong. So each entry is checked by walking it with the same
//	navigator the filter uses.
//
// The array suggestions are the awkward half: `[]`, `[0]` and `length` are not keys, so
// they are hand-written, and a hand-written list is where a missing arm hides.

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/buble/dbx/internal/drivers/postgres"
)

// postgresForeignKey is the driver's own type, aliased so the fixture literals below read
// as fixtures rather than as a constructor call.
type postgresForeignKey = postgres.ForeignKeyInfo

// jsonUnmarshalString is a named wrapper so the test bodies read as "decode the fixture"
// rather than repeating the two-argument call.
func jsonUnmarshalString(s string, v any) error { return json.Unmarshal([]byte(s), v) }

// containsStr is membership over a plain string slice.
func containsStr(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

// fmtEqual compares two decoded JSON values by their rendered form, which is enough for the
// fixtures here and does not need a deep-equality walk.
func fmtEqual(a, b any) bool { return fmt.Sprintf("%#v", a) == fmt.Sprintf("%#v", b) }

// jqDoc is the fixture: an object, a nested object, and an array of objects. Chosen so the
// root, the child and the array arms are all reachable in one document.
const jqDoc = `{
  "id": 7,
  "name": "ada",
  "user": {"city": "London", "zip": "E1"},
  "items": [{"sku": "a"}, {"sku": "b"}],
  "tags": ["x", "y"]
}`

// inJQ is a preview with jq mode open over jqDoc.
func inJQ(t *testing.T) *GridPreview {
	t.Helper()
	p := previewOf(t, jqDoc)
	p.SetWidth(80)
	p.SetHeight(20)
	p.EnterJQMode()
	if !p.jqMode {
		t.Fatal("EnterJQMode did not open jq mode")
	}
	return p
}

func suggestionPaths(p *GridPreview) []string {
	out := make([]string, len(p.jqSugs))
	for i, s := range p.jqSugs {
		out[i] = s.Path
	}
	return out
}

// TestTheSuggestionListIsSorted pins the property that makes tab usable.
//
// The failure it prevents is not cosmetic: the selected entry is ACCEPTED, so an unsorted
// list means pressing tab on the same document twice inserts two different fields.
//
// The array cases below are DETERMINISTIC rather than sorted, and the distinction is not
// pedantry: the map branches iterate a Go map, which is randomised, so they must be sorted.
// The array branch is a fixed list of literals, so it cannot drift, and sorting it would
// only move the iterator off the default tab stop. A version of the production code added
// the sort there on the reasoning that every other branch has one, which is the drift
// pattern run backwards.
func TestTheSuggestionListIsSorted(t *testing.T) {
	// The failure this prevents is not cosmetic: the selected entry is ACCEPTED, so an
	// unsorted list means pressing tab on the same document twice inserts two different
	// fields.
	//
	// The array cases are DETERMINISTIC rather than sorted, and the distinction is not
	// pedantry. The map branches iterate a Go map, which is randomised, so they must be
	// sorted. The array branch is a fixed list of literals, so it cannot drift, and
	// sorting it would only move the iterator off the default tab stop — which is what a
	// version of the production code did, on the reasoning that every other branch had a
	// sort. That is the drift pattern run backwards: fixing a branch that could not drift
	// and breaking the one thing the order was for.
	for _, tc := range []struct {
		name  string
		input string
		// fromMap is whether the list comes from iterating a map, which is the only source
		// that needs sorting.
		fromMap bool
	}{
		{"at the root, with nothing typed", "", true},
		{"at the root, with a prefix", ".u", true},
		{"inside an object", ".user.", true},
		{"a prefix matching one array suggestion", ".items.[", false},
		{"inside an array", ".items.", false},
		{"a prefix that matches nothing inside an array", ".items.[0", false},
		{"inside an array of scalars", ".tags.", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := inJQ(t)
			p.jqInput = tc.input
			p.jqCursor = len([]rune(tc.input))
			p.updateJQSuggestions()

			paths := suggestionPaths(p)
			if tc.fromMap {
				if len(paths) == 0 {
					t.Fatalf("no suggestions for %q", tc.input)
				}
				for i := 1; i < len(paths); i++ {
					if paths[i-1] > paths[i] {
						t.Errorf("the list is not sorted at %d: %q comes after %q\n  full list: %v",
							i, paths[i], paths[i-1], paths)
						break
					}
				}
			} else if len(paths) == 0 {
				// Not every prefix matches, and saying so is the contract: the bar shows
				// nothing rather than everything.
				t.Logf("no suggestions for %q, which is a valid answer for a prefix that matches nothing", tc.input)
			}

			// And stable across recomputations, for both kinds: the same input gives the
			// same list, twice. This is the property that does not depend on the source.
			first := strings.Join(paths, ",")
			p.updateJQSuggestions()
			if second := strings.Join(suggestionPaths(p), ","); second != first {
				t.Errorf("recomputing the same input gave a different list:\n  %s\n  %s", first, second)
			}
		})
	}
}

// TestEverySuggestionResolves is the property that makes the list worth offering.
func TestEverySuggestionResolves(t *testing.T) {
	for _, input := range []string{
		"",
		".u",
		".user.",
		".items.",
		".items.[0]",
		".tags.",
	} {
		t.Run("at "+input, func(t *testing.T) {
			p := inJQ(t)
			p.jqInput = input
			p.jqCursor = len([]rune(input))
			p.updateJQSuggestions()

			// A suggestion REPLACES the partial key it completes, it is not appended to
			// it. The first version of this case resolved `input + suggestion` and
			// reported that ".uuser" did not resolve — which was true, and was the test
			// modelling the wrong thing: typing ".u" and completing to "user" gives
			// ".user".
			//
			// So the completed expression is the input up to the last dot, plus the
			// suggestion. For a root suggestion that is the dot itself.
			for _, path := range suggestionPaths(p) {
				head := input
				if i := strings.LastIndex(head, "."); i >= 0 {
					head = head[:i+1]
				}
				full := head + strings.TrimPrefix(path, ".")
				var doc any
				if err := jsonUnmarshalString(jqDoc, &doc); err != nil {
					t.Fatalf("the fixture is not valid JSON: %v", err)
				}
				if p.navigateJSON(doc, full) == nil {
					t.Errorf("the suggestion %q completes to %q, which resolves to nothing", path, full)
				}
			}
		})
	}
}

// TestTheRootOffersEveryKeyAndTheChildOffersTheChildOnes separates the two levels, because
// the root uses a LEADING DOT and the child does not — and mixing that up produces an
// expression that does not resolve.
func TestTheRootOffersEveryKeyAndTheChildOffersTheChildOnes(t *testing.T) {
	t.Run("the root offers dotted keys", func(t *testing.T) {
		p := inJQ(t)
		got := suggestionPaths(p)
		for _, want := range []string{".id", ".name", ".user", ".items", ".tags"} {
			if !containsStr(got, want) {
				t.Errorf("the root does not offer %q: %v", want, got)
			}
		}
		// And no bare keys — the dot is what makes it a jq path.
		for _, path := range got {
			if !strings.HasPrefix(path, ".") {
				t.Errorf("the root offered %q with no leading dot, which is not a path", path)
			}
		}
	})

	t.Run("a child offers BARE keys", func(t *testing.T) {
		p := inJQ(t)
		p.jqInput = ".user."
		p.jqCursor = len(p.jqInput)
		p.updateJQSuggestions()

		got := suggestionPaths(p)
		for _, want := range []string{"city", "zip"} {
			if !containsStr(got, want) {
				t.Errorf("the child of .user does not offer %q: %v", want, got)
			}
		}
		for _, path := range got {
			if strings.HasPrefix(path, ".") {
				t.Errorf("a child offered %q with a leading dot, which would resolve to nothing", path)
			}
		}
	})

	t.Run("a prefix narrows the list", func(t *testing.T) {
		p := inJQ(t)
		p.jqInput = ".user.c"
		p.jqCursor = len(p.jqInput)
		p.updateJQSuggestions()

		got := suggestionPaths(p)
		if !containsStr(got, "city") {
			t.Errorf("the prefix .c did not narrow to city: %v", got)
		}
		if containsStr(got, "zip") {
			t.Errorf("the prefix .c still offers zip: %v", got)
		}
	})
}

// The three array suggestions are hand-written because they are not keys. Each one has a
// different meaning and the pair below pins what that is, because offering `[]` where the
// user wanted `length` produces a filter that silently matches everything.
func TestAnArrayOffersIterationElementAndLength(t *testing.T) {
	p := inJQ(t)
	p.jqInput = ".tags."
	p.jqCursor = len(p.jqInput)
	p.updateJQSuggestions()

	got := suggestionPaths(p)
	for _, want := range []string{"[]", "[0]", "length"} {
		if !containsStr(got, want) {
			t.Errorf("an array does not offer %q: %v", want, got)
		}
	}

	t.Run("the three are different filters", func(t *testing.T) {
		var doc any
		if err := jsonUnmarshalString(jqDoc, &doc); err != nil {
			t.Fatalf("fixture: %v", err)
		}
		// .tags.[] yields the two elements; .tags.length yields the count. Asserted as
		// "they are not equal", because that is the property that matters: a suggestion
		// bar where two entries do the same thing is a bar where the user picks the wrong
		// one and gets a plausible empty result.
		all := p.navigateJSON(doc, ".tags.[]")
		first := p.navigateJSON(doc, ".tags.[0]")
		count := p.navigateJSON(doc, ".tags.length")

		allList, ok := all.([]interface{})
		if !ok || len(allList) != 2 {
			t.Errorf(".tags.[] gave %#v, want the two elements", all)
		}
		if first != "x" {
			t.Errorf(".tags.[0] gave %#v, want the first element", first)
		}
		if count == nil {
			t.Error(".tags.length resolved to nothing")
		}
		if fmtEqual(all, count) {
			t.Error(".tags.[] and .tags.length resolve to the same thing")
		}
	})

	t.Run("an EMPTY array still offers them", func(t *testing.T) {
		// The degenerate case, and it is the one a naive implementation gets wrong by
		// indexing element zero to decide the type. `[0]` on an empty array is the jq
		// idiom for "null when empty", and the suggestion has to be there for it.
		empty := previewOf(t, `{"tags": []}`)
		empty.SetWidth(80)
		empty.EnterJQMode()
		empty.jqInput = ".tags."
		empty.jqCursor = len(empty.jqInput)
		empty.updateJQSuggestions()

		for _, want := range []string{"[]", "[0]", "length"} {
			if !containsStr(suggestionPaths(empty), want) {
				t.Errorf("an empty array does not offer %q: %v", want, suggestionPaths(empty))
			}
		}
	})
}

// The FK expansion from the preview. Three refusals, and each is a different question:
//
//	the cursor is past the last line
//	the line under it is not part of a path
//	the path is not an expandable foreign key
//
// The last one is the interesting one, because the preview holds the catalogue of keys and
// a path that names an ordinary column must not produce a lookup.
func TestExpandingFromThePreviewRefusesWhatItCannotExpand(t *testing.T) {
	t.Run("a cursor past the last line is refused", func(t *testing.T) {
		p := previewOf(t, jqDoc)
		p.cursorLine = len(p.lines) + 5

		cmd, handled := p.handleExpand()
		if handled || cmd != nil {
			t.Errorf("a cursor past the end expanded: handled=%t cmd=%v", handled, cmd)
		}
	})

	t.Run("a line that is not a key is refused", func(t *testing.T) {
		// A scalar line has no path above it, so there is nothing to expand. Offering
		// anything here would navigate to the parent key of a value.
		p := previewOf(t, jqDoc)
		for i, line := range p.lines {
			if strings.Contains(line, `"name": "ada"`) {
				p.cursorLine = i
				break
			}
		}
		if _, handled := p.handleExpand(); handled {
			t.Error("a value line expanded")
		}
	})

	t.Run("a column that is NOT a foreign key is refused", func(t *testing.T) {
		p := previewOf(t, jqDoc)
		p.SetForeignKeys([]postgresForeignKey{{
			RefSchema: "public", RefTable: "users", Column: "user_id", RefColumn: "id",
		}})
		// "name" is in the preview and is not a key.
		for i, line := range p.lines {
			if strings.Contains(line, `"name"`) {
				p.cursorLine = i
				break
			}
		}
		if _, handled := p.handleExpand(); handled {
			t.Error("a non-key column expanded as a foreign key")
		}
	})

	t.Run("and a REAL key DOES expand, with the full reference", func(t *testing.T) {
		p := previewOf(t, `{"user_id": 42}`)
		p.SetForeignKeys([]postgresForeignKey{{
			Name: "orders_user_id_fkey", Column: "user_id",
			RefSchema: "public", RefTable: "users", RefColumn: "id",
		}})
		p.cursorLine = 1 // the "user_id" line

		cmd, handled := p.handleExpand()
		if !handled || cmd == nil {
			t.Fatalf("an expandable key was refused: handled=%t cmd=%v", handled, cmd)
		}
		msg, ok := cmd().(GridPreviewExpandFKMsg)
		if !ok {
			t.Fatalf("the command produced %T", cmd())
		}
		if msg.Path != "user_id" {
			t.Errorf("the path is %q, want user_id", msg.Path)
		}
		if msg.Column != "user_id" || msg.RefTable != "users" || msg.RefColumn != "id" {
			t.Errorf("the message carries %+v, want the full reference", msg)
		}
		if msg.RefSchema != "public" {
			t.Errorf("the reference schema is %q, want public", msg.RefSchema)
		}
		// The value comes out of json, so a number is a float64 — compared through its
		// rendering rather than as a literal 42, which is what the first version of this
		// case did and it failed on a correct expansion.
		if got := fmt.Sprintf("%v", msg.Value); got != "42" {
			t.Errorf("the value is %s, want the 42 in the row", got)
		}
	})
}
