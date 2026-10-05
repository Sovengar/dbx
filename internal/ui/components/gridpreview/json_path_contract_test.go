package gridpreview

// Scenario: La aritmetica del preview de JSON — la resolucion de rutas con puntos, el
// parseo de una clave de una linea, y los clamps de ancho y de scroll.
//
// SetRow takes a []string of columns and a []interface{} row, and everything the preview
// draws comes from walking the row as a tree of maps and slices. The path syntax is the
// interesting part: `user.address.city` is three map lookups, `items[2]` is a slice index,
// and a path can mix them. Every one of those lookups can be given a value of the wrong
// SHAPE — a key where a list was expected, a list where a key was expected — and the
// question is whether that returns nil or reads something it should not.
//
// The clamps are the other half. The suggestion bar's widths are floored (30 and 15) and
// the path is truncated to fit; halfPageDown's maximum is floored at 0 because it is a
// subtraction that goes negative on a short document.
//
// None of this needs a database, and none needs a real jq — the preview stores its own
// JSON, so a literal is enough.

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/buble/dbx/internal/drivers/postgres"
)

// previewOf builds a preview over the document described by rawJSON.
//
// There is no setter for raw JSON: the preview derives its lines from rowData by
// marshalling it, so the fixture sets rowData and calls the same rebuild the FK path
// uses. Going through rowData rather than assigning the lines keeps the fixture honest —
// a preview built by assignment would have lines the code could never produce.
func previewOf(t *testing.T, rawJSON string) *GridPreview {
	t.Helper()
	p := newPreviewTest()
	var doc map[string]interface{}
	if err := json.Unmarshal([]byte(rawJSON), &doc); err != nil {
		t.Fatalf("the fixture is not valid JSON: %v", err)
	}
	p.rowData = doc
	p.rebuildLines()
	return p
}

// ---------------------------------------------------------------------------
// resolveValue: a dotted path through nested maps and slices
// ---------------------------------------------------------------------------

func TestADottedPathWalksMapsAndLists(t *testing.T) {
	const row = `{
		"id": 7,
		"name": "ada",
		"user": {"address": {"city": "London", "zip": "E1"}},
		"items": [
			{"sku": "a", "qty": 1},
			{"sku": "b", "qty": 2}
		],
		"matrix": [[1, 2], [3, 4]]
	}`

	for _, tc := range []struct {
		name string
		path string
		want interface{}
	}{
		{"a top-level key", "name", "ada"},
		{"a number", "id", float64(7)},
		{"one level down", "user.address.city", "London"},
		{"two levels down", "user.address.zip", "E1"},
		{"a list index", "items[0].sku", "a"},
		{"a deeper list index", "items[1].qty", float64(2)},
		{"a nested list", "matrix[1][0]", float64(3)},
		{"the deepest corner", "matrix[0][1]", float64(2)},
		// An object at the end of a path resolves to the map itself, not to a scalar.
		{"an object at the end of a path", "user.address", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := previewOf(t, row).resolveValue(tc.path)
			switch want := tc.want.(type) {
			case nil:
				// An object at the end of a path returns the map itself, so this case is
				// asserted as "not the scalar we asked for" rather than as nil.
				if got != nil {
					if m, ok := got.(map[string]interface{}); ok {
						if len(m) == 0 {
							t.Errorf("resolveValue(%q) is an empty map, want a value", tc.path)
						}
						return
					}
					t.Errorf("resolveValue(%q) = %v (%T), want the object or nil", tc.path, got, got)
				}
			default:
				if got != want {
					t.Errorf("resolveValue(%q) = %v (%T), want %v (%T)",
						tc.path, got, got, want, want)
				}
			}
		})
	}
}

func TestAMalformedPathResolvesToNothingRatherThanToSomethingWrong(t *testing.T) {
	const row = `{
		"id": 7,
		"name": "ada",
		"user": {"address": {"city": "London"}},
		"items": [{"sku": "a"}, {"sku": "b"}],
		"nothing": null
	}`

	// Every one of these walks off the end of the data. The contract is that they all
	// answer nil: an index out of range, a key on a scalar, a negative index, an index
	// that is not a number. The tempting failure is a Go panic from an unchecked slice
	// index or a type assertion — so the recover below is doing real work.
	for _, path := range []string{
		"missing",                  // a key that is not there
		"user.missing",             // a key missing one level down
		"user.address.city.deeper", // descending past a scalar
		"name.length",              // a key applied to a string
		"items[9]",                 // an index past the end
		"items[-1]",                // a negative index
		"items[abc]",               // an index that is not a number
		"id[0]",                    // an index applied to a number
		"nothing.deeper",           // descending past a null
	} {
		t.Run(path, func(t *testing.T) {
			p := previewOf(t, row)
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("resolveValue(%q) panicked: %v", path, r)
					}
				}()
				if got := p.resolveValue(path); got != nil {
					t.Errorf("resolveValue(%q) = %v (%T), want nil", path, got, got)
				}
			}()
		})
	}
}

// ---------------------------------------------------------------------------
// parseKeyFromLine
// ---------------------------------------------------------------------------

func TestTheKeyIsReadOutOfAQuotedLine(t *testing.T) {
	// The preview draws `"key": value` and a click has to work out which key a line is
	// about. The parse is a string index on the SECOND quote — off by one and every key
	// comes back truncated or with a trailing quote.
	for _, tc := range []struct {
		name string
		line string
		want string
	}{
		{"a plain key", `"id": 7`, "id"},
		{"a quoted key with a space", `"first name": "ada"`, "first name"},
		{"a key at the end of the line", `"id":`, "id"},
		{"leading whitespace", `   "nested": {`, "nested"},
		{"a key containing a dot", `"a.b": 1`, "a.b"},
		{"a key containing a quote escape", `"say \"hi\"": 1`, `say \"hi\"`},
		// The three shapes that are NOT a key. All of them answer the empty string, which
		// is what tells the rest of the code "this line is a value, not a key" and stops
		// the path tracker from inventing a level.
		{"a scalar line", `7`, ""},
		{"a line that merely contains a quote", `7, "seven"`, ""},
		{"an unterminated quote", `"id: 7`, ""},
		{"an empty line", ``, ""},
		{"only whitespace", `   `, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newPreviewTest()
			if got := p.parseKeyFromLine(tc.line); got != tc.want {
				t.Errorf("parseKeyFromLine(%q) = %q, want %q", tc.line, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ExpandFK / CollapseFK
// ---------------------------------------------------------------------------

// ExpandFK stores a fetched object under a dotted path and rebuilds. CollapseFK removes
// that path AND everything nested under it — a prefix test, where getting the prefix
// wrong either leaves the children behind or takes out a sibling whose name merely starts
// with the same characters.
func TestExpandingAndCollapsingAForeignKeyRemovesExactlyItsSubtree(t *testing.T) {
	// The row has to CONTAIN the key the FK replaces: buildDisplayData merges
	// expandedFKs by walking the row's own keys and looking each one's PATH up in the
	// expansion map. An expanded path whose first segment is not a row key is stored and
	// rendered into the map but never appears in the output — so a fixture that expands
	// "user" on a row of {a, b, c} tests nothing, which is what the first version of this
	// test did.
	newP := func() *GridPreview {
		p := newPreviewTest()
		p.SetRow([]string{"id", "user", "order"},
			[]interface{}{1, 42, 7})
		p.SetForeignKeys([]postgres.ForeignKeyInfo{
			{RefSchema: "public", RefTable: "orders", Column: "user_id"},
		})
		return p
	}

	t.Run("expanding twice on the same path REPLACES rather than duplicating", func(t *testing.T) {
		// The map assignment, not an append. Expanding the same FK again after a refresh
		// has to show the NEW row, and appending would show both.
		p := newP()
		p.ExpandFK("user", "user", map[string]interface{}{"name": "first"}, nil)
		first := len(p.expandedFKs)

		p.ExpandFK("user", "user", map[string]interface{}{"name": "second"}, nil)
		if len(p.expandedFKs) != first {
			t.Errorf("expanding the same path twice left %d entries, want %d", len(p.expandedFKs), first)
		}
		if got := p.resolveValue("user.name"); got != "second" {
			t.Errorf("the expanded value is %v, want the second one", got)
		}
	})

	t.Run("expanding with nested keys records them alongside the object", func(t *testing.T) {
		// Two maps, one for the objects and one for the key definitions. A preview that
		// only had the first would draw the rows but be unable to expand anything under
		// them, so the second map is not optional.
		p := newP()
		nested := []postgres.ForeignKeyInfo{{RefSchema: "public", RefTable: "addresses", Column: "address_id"}}
		p.ExpandFK("user", "user", map[string]interface{}{"name": "ada"}, nested)

		if len(p.nestedFKs["user"]) != 1 {
			t.Errorf("the nested key list holds %d entries, want 1", len(p.nestedFKs["user"]))
		}
		if p.nestedFKs["user"][0].Column != "address_id" {
			t.Errorf("the recorded nested key is %q, want address_id", p.nestedFKs["user"][0].Column)
		}
	})

	t.Run("collapsing a path removes ITS subtree and leaves its siblings", func(t *testing.T) {
		// The prefix test. `user.` must match `user.address` and must NOT match `users`
		// — and a prefix test written as `strings.HasPrefix(k, path)` would.
		p := newP()
		p.ExpandFK("user", "user", map[string]interface{}{"name": "ada"}, nil)
		p.ExpandFK("user.address", "user.address", map[string]interface{}{"city": "London"}, nil)
		p.ExpandFK("users", "users", map[string]interface{}{"name": "other"}, nil)
		p.ExpandFK("order", "order", map[string]interface{}{"total": 1}, nil)

		p.CollapseFK("user")

		for _, gone := range []string{"user", "user.address"} {
			if _, still := p.expandedFKs[gone]; still {
				t.Errorf("%q survived the collapse of its parent", gone)
			}
		}
		for _, kept := range []string{"users", "order"} {
			if _, ok := p.expandedFKs[kept]; !ok {
				t.Errorf("%q was removed by the collapse of an unrelated path", kept)
			}
		}
	})

	t.Run("collapsing a path with no children still removes the path", func(t *testing.T) {
		p := newP()
		p.ExpandFK("user", "user", map[string]interface{}{"name": "ada"}, nil)

		p.CollapseFK("user")
		if len(p.expandedFKs) != 0 {
			t.Errorf("collapsing left %d expansions, want none", len(p.expandedFKs))
		}
	})

	t.Run("expanding on a FRESH preview allocates both maps", func(t *testing.T) {
		// The two nil guards. Without them the assignment into a nil map is a panic, and
		// the first ExpandFK of a session is exactly the case that hits it.
		p := newPreviewTest()
		p.expandedFKs = nil
		p.nestedFKs = nil

		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("the first ExpandFK of a session panicked: %v", r)
				}
			}()
			p.ExpandFK("user", "user", map[string]interface{}{"name": "ada"}, nil)
		}()

		if p.expandedFKs == nil || p.nestedFKs == nil {
			t.Error("ExpandFK did not allocate its maps")
		}
	})
}

// ---------------------------------------------------------------------------
// scrolling
// ---------------------------------------------------------------------------

func TestHalfPagingNeverScrollsPastTheEndOrBeforeIt(t *testing.T) {
	// halfPageUp subtracts and floors at 0. halfPageDown adds and floors its MAXIMUM at 0
	// because the maximum is `len(lines) - height + 6`, which is negative on a document
	// shorter than the pane — and a negative clamp target would leave the view scrolled
	// off its own content.
	for _, nLines := range []int{0, 1, 3, 8, 20, 60, 200} {
		t.Run(plural(nLines), func(t *testing.T) {
			p := previewOf(t, longJSON(nLines))
			p.SetHeight(10)

			for range 5 {
				p.halfPageUp()
				if p.scrollY < 0 {
					t.Fatalf("%d lines: five half-page-ups left scrollY at %d", nLines, p.scrollY)
				}
			}
			if p.scrollY != 0 {
				t.Errorf("%d lines: five half-page-ups stopped at %d, want 0", nLines, p.scrollY)
			}

			for range 40 {
				p.halfPageDown()
				if p.scrollY < 0 {
					t.Fatalf("%d lines: scrolling down left scrollY at %d", nLines, p.scrollY)
				}
			}
			// And the far end: scrolling down forty times on a long document must stop
			// somewhere, not run off it.
			if p.scrollY > len(p.lines) {
				t.Errorf("%d lines: scrolling down settled at %d, past the %d lines there are",
					nLines, p.scrollY, len(p.lines))
			}
		})
	}

	t.Run("a document SHORTER than the pane does not scroll at all", func(t *testing.T) {
		// The negative maximum. Three lines in a ten-row pane: maxScroll is
		// 3-10+6 = -1, floored to 0, so scrollY stays 0.
		// `{"a":1}` marshalled with an indent is three lines, which in a ten-row pane
		// makes the maximum 3-10+6 = -1. Note that longJSON(3) would NOT do it: three
		// top-level keys come to eleven lines, so the maximum is positive and the clamp
		// never fires. The first version of this case used longJSON(3) and reported a
		// mismatch on correct scrolling.
		p := previewOf(t, `{"a":1}`)
		if len(p.lines) >= 10 {
			t.Fatalf("the fixture is %d lines, so it does not fit in a ten-row pane", len(p.lines))
		}
		p.SetHeight(10)

		p.halfPageDown()
		if p.scrollY != 0 {
			t.Errorf("a %d-line document in a ten-row pane scrolled to %d, want 0",
				len(p.lines), p.scrollY)
		}
	})
}

// ---------------------------------------------------------------------------
// the suggestion bar's widths
// ---------------------------------------------------------------------------

func TestTheSuggestionBarFloorsItsWidths(t *testing.T) {
	// Two floors and a truncation. maxWidth floors at 30 and pathWidth at 15, because
	// without them a narrow preview gets a negative repeat count — which panics — and a
	// wide one gets no room for the value next to the path.
	for _, width := range []int{0, 5, 20, 38, 44, 80, 200} {
		t.Run(plural(width), func(t *testing.T) {
			p := newPreviewTest()
			p.SetWidth(width)
			p.jqSugVisible = true
			p.jqSugs = []JQSuggestion{
				{Path: "a"},
				{Path: "an.extremely.long.path.that.cannot.fit.in.any.narrow.pane.at.all"},
			}
			p.jqSugSelected = 0

			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("the suggestion bar panicked at width %d: %v", width, r)
					}
				}()
				out := p.renderJQSuggestions()
				if strings.TrimSpace(ansi.Strip(out)) == "" {
					t.Errorf("at width %d the suggestion bar renders nothing:\n%q", width, out)
				}
			}()
		})
	}

	t.Run("an EMPTY suggestion list renders nothing rather than an empty box", func(t *testing.T) {
		// The `len(p.jqSugs) > 0` guard at the call site, and the same guard inside.
		p := newPreviewTest()
		p.jqSugVisible = true
		p.jqSugs = nil

		if out := p.renderJQSuggestions(); strings.TrimSpace(ansi.Strip(out)) != "" {
			t.Errorf("an empty suggestion list rendered %q, want nothing", out)
		}
	})
}

// ---------------------------------------------------------------------------
// the keybinds guard
// ---------------------------------------------------------------------------

func TestAPreviewWithoutKeybindsHandlesNothing(t *testing.T) {
	// The nil check before Resolve. Every other GridPreview fixture builds a registry, so
	// without this the resolver would be unreachable and the guard untested — which is
	// the same trap newGrid documents for the grid.
	p := newPreviewTest()
	p.keybinds = nil

	cmd, handled := p.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if handled {
		t.Error("a preview with no keybind registry handled a key")
	}
	if cmd != nil {
		t.Errorf("a preview with no keybind registry produced %T", cmd())
	}
	if p.cursorLine != 0 {
		t.Errorf("a preview with no keybind registry moved its cursor to line %d", p.cursorLine)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// longJSON is a document with roughly n top-level keys, so len(lines) tracks n.
func longJSON(n int) string {
	var b strings.Builder
	b.WriteString("{")
	for i := range n {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`"key_`)
		b.WriteString(strconv.Itoa(i))
		b.WriteString(`": "value_`)
		b.WriteString(strconv.Itoa(i))
		b.WriteByte('"')
	}
	b.WriteString("}")
	return b.String()
}

func plural(n int) string { return strconv.Itoa(n) }
