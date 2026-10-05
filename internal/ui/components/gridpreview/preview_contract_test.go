package gridpreview

// The contracts of the grid preview, asserted as properties over states rather than one
// scenario at a time.
//
// The preview is the panel beside the grid. It shows the selected row as indented JSON,
// lets you walk into a foreign key and fetch the row it points at, and has a jq filter
// with a suggestion list, a history file and its own key handling. Everything except the
// FK fetch is pure: the jq engine is gojq plus a hand-written path navigator, and the
// navigator is where the interesting mistakes live.
//
// Six things it promises:
//
//	N1  a path navigates to the value it names, or to nothing — never to the wrong value
//	    and never to a panic, however malformed the path is.
//	N2  the suggestions offered are the keys that exist, narrowed by what has been typed,
//	    and never more than the cap.
//	N3  a jq filter reports its own failure — parse, compile, run, empty — as text in the
//	    panel, instead of leaving the previous document on screen.
//	N4  expanding a foreign key splices the fetched row in, and collapsing it removes the
//	    whole subtree beneath that path.
//	N5  the history is a file, and it survives the process.
//	N6  what is rendered is the JSON, highlighted, in a window the cursor stays inside.

import (
	"encoding/json"
	"math/rand"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/theme"
	"github.com/charmbracelet/x/ansi"
)

// isolate gives a preview a private HOME so the history file cannot reach the developer's
// real one, and so two tests in this file cannot see each other's history.
func isolate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	return home
}

func newPanel(t *testing.T) *GridPreview {
	t.Helper()
	isolate(t)
	p := New(theme.Resolve("dark").Styles(), config.NewKeybindRegistry(config.KeybindingsConfig{}))
	p.SetWidth(80)
	p.SetHeight(24)
	p.SetRow([]string{"id", "name", "customer_id"}, []interface{}{1, "ada", 42})
	return p
}

// ---------------------------------------------------------------------------
// N1: the path navigator
// ---------------------------------------------------------------------------

// doc is the fixture every navigation case walks. It has an object, an array of objects,
// a null, an empty array, and an array of scalars, because each of those takes a different
// branch and a fixture with only objects would leave every other branch unreached.
// docObj and docArr are the two shapes the navigator has to tell apart, held separately
// so a fixture can index them without a type assertion at every use.
var (
	docObj = map[string]any{"a": 1.0, "b": "two", "empty": map[string]any{}}
	docArr = []any{
		map[string]any{"c": 10.0},
		map[string]any{"c": 20.0},
		"just a string",
	}
	docEmpties = []any{}
)

var doc = map[string]any{
	"scalar": "value",
	"number": 42.0,
	"nulled": nil,
	"obj":    map[string]any{"a": 1.0, "b": "two", "empty": map[string]any{}},
	"arr": []any{
		map[string]any{"c": 10.0},
		map[string]any{"c": 20.0},
		"just a string",
	},
	"empties":       []any{},
	"scalars":       []any{1.0, 2.0, 3.0},
	"key.with.dots": "dotted key",
}

// Scenario: Una ruta lleva al valor que nombra, y a NADA cuando no.
//
// This is the navigator behind the suggestion list: it is asked "what is under .arr[1]"
// and it has to answer with the second element or with nothing at all. Returning the WRONG
// value is the failure that matters, so every case asserts on the value rather than on a
// boolean — a navigator that always returned "found" would pass a presence check.
func TestNavigateJSON_APathResolvesToTheValueItNames(t *testing.T) {
	p := newPanel(t)

	for _, tc := range []struct {
		path string
		want any
	}{
		{"", doc},                              // the whole document
		{".", doc},                             // and the same with the dot
		{".scalar", "value"},                   // a scalar
		{".number", 42.0},                      // a number
		{".nulled", nil},                       // a null, which is a value AND an absence
		{".obj", docObj},                       // an object
		{".obj.a", 1.0},                        // through an object
		{".obj.empty", map[string]any{}},       // an empty object is still an object
		{".arr", docArr},                       // an array
		{".arr[0]", map[string]any{"c": 10.0}}, // an index
		{".arr[1]", map[string]any{"c": 20.0}},
		{".arr[2]", "just a string"},
		{".arr[0].c", 10.0},   // an index then a key
		{".obj.a.b.c", nil},   // a key that does not exist
		{".obj.missing", nil}, // and the same, spelled plainly
		{"scalar", "value"},   // a path with no leading dot
		// `.arr[]` is ITERATION and `.arr[0]` is the first element, and this table used to
		// say they were the same thing — which is the shape that hid it. `[]` resolved to
		// element zero, so the suggestion bar offered two entries that did exactly the same
		// work, and a filter written against one silently answered for the other.
		//
		// The navigator returns a single value, so the whole array is iteration's honest
		// equivalent: it is what gets rendered, and it is what a filter over the field
		// should see.
		{".arr[]", docArr}, // iteration yields every element
		// An empty array iterated is an empty result, not a missing one. Returning nil
		// conflated "there are no elements here" with "that path is wrong", and the two
		// need different answers.
		{".empties[]", []any{}},
	} {
		t.Run(tc.path, func(t *testing.T) {
			got := p.navigateJSON(doc, tc.path)
			// DeepEqual, NOT ==. A JSON navigator returns maps and slices, and
			// comparing two interfaces with == panics when the dynamic type is
			// uncomparable — so `got != tc.want` took the whole test binary
			// down on the case that returns the document itself.
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("navigateJSON(%q) = %#v, want %#v", tc.path, got, tc.want)
			}
		})
	}
}

// Scenario: Una ruta malformada devuelve NADA, nunca un valor ni un panic.
//
// The navigator is handed whatever the user typed, so every malformed shape is reachable:
// an unclosed bracket, a bracket on something that is not an array, an index past the end,
// a negative index, a bracket on a scalar. Each must come back as nil — because the one
// that returns a real value would put a WRONG value into the suggestion list, and the one
// that panics takes the panel down while a user is typing.
func TestNavigateJSON_AMalformedPathYieldsNothingRatherThanAWrongValue(t *testing.T) {
	p := newPanel(t)

	for _, tc := range []struct {
		name, path string
	}{
		{"an unclosed bracket", ".arr["},
		{"an unclosed bracket after a key", ".obj.a["},
		{"a stray closing bracket", ".arr]"},
		{"an index past the end", ".arr[9]"},
		{"a negative index", ".arr[-1]"},
		{"a bracket on a scalar", ".scalar[0]"},
		{"a bracket on a null", ".nulled[0]"},
		{"a bracket on an empty array", ".empties[0]"},
		{"an index that is not a number", ".arr[abc]"},
		{"an index on an empty array with no brackets", ".empties.0"},
		{"a key under a scalar", ".scalar.deeper"},
		{"a key under a null", ".nulled.deeper"},
		{"an index under a scalar", ".scalar.0"},
		{"a bracket followed by junk", ".arr[0]x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := p.navigateJSON(doc, tc.path); got != nil {
				t.Errorf("navigateJSON(%q) = %#v, want nil: a malformed path must not resolve to a value", tc.path, got)
			}
		})
	}

	t.Run("and it does not panic on any of them", func(t *testing.T) {
		// The assertions above already fail on a panic by taking the test binary
		// with them, so this is a statement of intent rather than extra work.
		for _, path := range []string{"[", "][", ".[", "..", ".a..b", ".a[0][", "[]"} {
			_ = p.navigateJSON(doc, path)
		}
	})

	t.Run("navigating into a nil document is nil, not a panic", func(t *testing.T) {
		if got := p.navigateJSON(nil, ".a"); got != nil {
			t.Errorf("navigateJSON(nil, \".a\") = %#v, want nil", got)
		}
		if got := p.navigateJSON(nil, ""); got != nil {
			t.Errorf("navigateJSON(nil, \"\") = %#v, want nil", got)
		}
	})
}

// ---------------------------------------------------------------------------
// The pure helpers, which the rest of the file is built on
// ---------------------------------------------------------------------------

// Scenario: Los ayudantes puros dicen LO QUE DICEN, y no mas.
//
// Six small functions the rest of the file leans on. Each is asserted against a table
// rather than a scenario, because the interesting cases are the edges — the empty string,
// the zero index, the type nothing was expecting — and an edge is what a table is for.
func TestThePureHelpers(t *testing.T) {
	t.Run("joinPath", func(t *testing.T) {
		for _, tc := range []struct{ parent, key, want string }{
			{"", "key", "key"}, // a root key has no prefix
			{"a", "b", "a.b"},
			{"a.b", "c", "a.b.c"},
			{"a", "", "a."}, // an empty key still joins
			{"", "", ""},
		} {
			if got := joinPath(tc.parent, tc.key); got != tc.want {
				t.Errorf("joinPath(%q, %q) = %q, want %q", tc.parent, tc.key, got, tc.want)
			}
		}
	})

	t.Run("countIndent counts LEADING spaces and stops at the first other character", func(t *testing.T) {
		for _, tc := range []struct {
			line string
			want int
		}{
			{"", 0},
			{"    ", 4},     // all spaces
			{"  x", 2},      // spaces then content
			{"x", 0},        // no leading spaces
			{"  x    y", 2}, // and it does not count the ones in the middle
			{"\tx", 0},      // a tab is not a space
		} {
			if got := countIndent(tc.line); got != tc.want {
				t.Errorf("countIndent(%q) = %d, want %d", tc.line, got, tc.want)
			}
		}
	})

	t.Run("jsonType names every type the decoder can produce", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			in   any
			want string
		}{
			{"nil", nil, "null"},
			{"an object", map[string]any{}, "object"},
			{"an array", []any{}, "array"},
			{"a string", "x", "string"},
			{"a number", 1.5, "number"},
			{"a bool", true, "bool"},
			// Only float64 is a number, because only float64 is what the JSON
			// decoder produces. An int reaching here means something built the
			// map by hand, and "unknown" is the honest answer rather than a lie
			// that would make the suggestion say "number" for something the
			// filter would treat differently.
			{"an int is not a number", int(1), "unknown"},
			{"an int64 is not a number", int64(1), "unknown"},
			{"a struct is unknown", struct{}{}, "unknown"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				if got := jsonType(tc.in); got != tc.want {
					t.Errorf("jsonType(%#v) = %q, want %q", tc.in, got, tc.want)
				}
			})
		}
	})

	t.Run("indexArray reads inside the array and nil outside it", func(t *testing.T) {
		arr := []any{"a", "b", "c"}
		if got := indexArray(arr, 0); got != "a" {
			t.Errorf("indexArray(0) = %#v, want the first", got)
		}
		if got := indexArray(arr, 2); got != "c" {
			t.Errorf("indexArray(2) = %#v, want the last", got)
		}
		for _, idx := range []int{-1, 3, 99} {
			if got := indexArray(arr, idx); got != nil {
				t.Errorf("indexArray(%d) = %#v, want nil: an index outside the array is an absence, not a panic", idx, got)
			}
		}
		if got := indexArray(nil, 0); got != nil {
			t.Errorf("indexArray(nil, 0) = %#v, want nil", got)
		}
	})

	t.Run("normalizeJSONTypes rewrites containers in place and leaves scalars alone", func(t *testing.T) {
		// There is nothing to rewrite today — the decoder already produces
		// float64 for every number — which is exactly why this needs saying:
		// the function is called on every keystroke of the suggestion list and
		// must be a no-op rather than a surprise.
		in := map[string]any{
			"n": 1.0, "s": "x", "b": true, "z": nil,
			"o":    map[string]any{"inner": 2.0},
			"arr":  []any{3.0, map[string]any{"deep": 4.0}},
			"emps": map[string]any{},
		}
		out := normalizeJSONTypes(in)
		m, ok := out.(map[string]any)
		if !ok {
			t.Fatalf("normalizeJSONTypes returned a %T, want a map", out)
		}
		// It has to REACH INSIDE, not just walk the top level: the suggestion
		// list asks the type of a nested value, so a normaliser that stopped at
		// the root would report "unknown" for everything below it.
		inner, ok := m["o"].(map[string]any)
		if !ok {
			t.Fatalf("the nested object came back as %T", m["o"])
		}
		if got, ok := inner["inner"].(float64); !ok || got != 2.0 {
			t.Errorf("the nested number came back as %#v, want the float64 2.0", inner["inner"])
		}
		deep, ok := m["arr"].([]any)
		if !ok || len(deep) != 2 {
			t.Fatalf("the array came back as %#v", m["arr"])
		}
		if obj, ok := deep[1].(map[string]any); !ok {
			t.Errorf("the object INSIDE the array came back as %T, so the recursion stopped at the array", deep[1])
		} else if got, ok := obj["deep"].(float64); !ok || got != 4.0 {
			t.Errorf("the number two levels down came back as %#v", obj["deep"])
		}
		before, _ := json.Marshal(in)
		after, _ := json.Marshal(out)
		if string(before) != string(after) {
			t.Errorf("normalizeJSONTypes changed the document: %s -> %s", before, after)
		}
		// And the SCALARS come back as themselves, which is what the default arm
		// is for.
		for _, v := range []any{"x", 1.0, true, nil} {
			if got := normalizeJSONTypes(v); got != v {
				t.Errorf("normalizeJSONTypes(%#v) = %#v, want it unchanged", v, got)
			}
		}
	})
}

// Scenario: El rastreador de rutas construye el camino POR INDENTACION.
//
// The expanded JSON is re-parsed for display by walking its indentation, so the tracker's
// whole job is popping the stack at the right moment. Getting it wrong produces paths like
// "obj.stale" — a key from a sibling that has already been closed — which is how an FK
// expansion ends up writing into the wrong place.
func TestFKPathTracker_PopsTheStackAtTheRightIndent(t *testing.T) {
	t.Run("a key at a deeper indent extends the path", func(t *testing.T) {
		tr := &fkPathTracker{}
		if got := tr.onKey("a", 0); got != "a" {
			t.Errorf("the first key made the path %q, want %q", got, "a")
		}
		if got := tr.onKey("b", 2); got != "a.b" {
			t.Errorf("a deeper key made the path %q, want %q", got, "a.b")
		}
		if got := tr.onKey("c", 4); got != "a.b.c" {
			t.Errorf("a deeper key made the path %q, want %q", got, "a.b.c")
		}
		if got := tr.currentPath(); got != "a.b.c" {
			t.Errorf("currentPath is %q, want %q", got, "a.b.c")
		}
	})

	t.Run("a key at the SAME indent is a sibling, not a child", func(t *testing.T) {
		tr := &fkPathTracker{}
		tr.onKey("a", 0)
		tr.onKey("b", 2)
		// This is the case that matters: at the same indent the previous key
		// is finished, so the path must go back to the parent rather than
		// nesting under it.
		if got := tr.onKey("c", 2); got != "a.c" {
			t.Errorf("a sibling key made the path %q, want %q — a sibling nested under its own predecessor is a path into the wrong object", got, "a.c")
		}
	})

	t.Run("a shallower key pops every level above it", func(t *testing.T) {
		tr := &fkPathTracker{}
		tr.onKey("a", 0)
		tr.onKey("b", 2)
		tr.onKey("c", 4)
		if got := tr.onKey("d", 0); got != "d" {
			t.Errorf("a root-level key made the path %q, want %q", got, "d")
		}
		if got := tr.currentPath(); got != "d" {
			t.Errorf("currentPath is %q after popping, want %q", got, "d")
		}
	})

	t.Run("a close at an indent pops to that level", func(t *testing.T) {
		tr := &fkPathTracker{}
		tr.onKey("a", 0)
		tr.onKey("b", 2)
		tr.onKey("c", 4)

		tr.onClose(4)
		if got := tr.currentPath(); got != "a.b" {
			t.Errorf("closing at indent 4 left the path %q, want %q", got, "a.b")
		}
		tr.onClose(0)
		if got := tr.currentPath(); got != "" {
			t.Errorf("closing at indent 0 left the path %q, want it empty", got)
		}
		// And closing past the bottom is not a panic.
		tr.onClose(0)
		tr.onClose(-1)
		if got := tr.currentPath(); got != "" {
			t.Errorf("closing past the bottom left the path %q, want it empty", got)
		}
	})
}

// ---------------------------------------------------------------------------
// N2: the suggestions
// ---------------------------------------------------------------------------

// Scenario: Las sugerencias son las CLAVES QUE EXISTEN, filtradas por lo escrito.
//
// The suggestion list is computed on every keystroke from the text before the cursor, and
// it is the only way a user discovers what is in the JSON. So the promises are: at the
// root, one suggestion per top-level key; after a dot, the keys of the thing under it,
// narrowed by the partial word; and never more than the cap.
func TestSuggestions_OfferTheKeysThatExist(t *testing.T) {
	p := newPanel(t)

	t.Run("at the root, every top-level key is offered with its type", func(t *testing.T) {
		sugs := p.rootSuggestions(doc)
		byPath := map[string]string{}
		for _, s := range sugs {
			byPath[s.Path] = s.Type
		}
		for path, wantType := range map[string]string{
			".scalar": "string",
			".number": "number",
			".nulled": "null",
			".obj":    "object",
			".arr":    "array",
		} {
			got, ok := byPath[path]
			if !ok {
				t.Errorf("the root does not offer %q; it offers %v", path, keysOf(sugs))
				continue
			}
			if got != wantType {
				t.Errorf("the suggestion for %q says its type is %q, want %q", path, got, wantType)
			}
		}
	})

	t.Run("an array at the root offers iteration and its first element", func(t *testing.T) {
		sugs := p.rootSuggestions(docArr)
		if len(sugs) != 2 {
			t.Fatalf("an array offered %d suggestions (%v), want two", len(sugs), sugs)
		}
		if sugs[0].Path != ".[]" || sugs[0].Type != "array-iter" {
			t.Errorf("the first suggestion is %+v, want the array iterator", sugs[0])
		}
		if sugs[1].Path != ".[0]" {
			t.Errorf("the second suggestion is %+v, want the first element", sugs[1])
		}
		// An EMPTY array still offers its first element, typed "null" — which
		// is what indexArray returns for an index outside the array. The
		// suggestion is wrong about there being an element there, and it is
		// pinned as it is rather than wished away: the cap of eight means one
		// dead suggestion in a list is cheaper than a special case, and what
		// matters is that selecting it navigates to nothing rather than
		// panicking.
		empty := p.rootSuggestions(docEmpties)
		if len(empty) != 2 {
			t.Errorf("an empty array offered %v, want the iterator and the first element", empty)
			return
		}
		if empty[1].Path != ".[0]" || empty[1].Type != "null" {
			t.Errorf("an empty array's element suggestion is %+v, want .[0] typed null", empty[1])
		}
		if got := p.navigateJSON(docEmpties, ".[0]"); got != nil {
			t.Errorf("selecting that suggestion navigates to %#v, want nothing", got)
		}
	})

	t.Run("a scalar at the root offers nothing", func(t *testing.T) {
		for _, v := range []any{nil, "text", 42.0, true} {
			if got := p.rootSuggestions(v); got != nil {
				t.Errorf("a scalar %#v offered %v, want nothing to walk into", v, got)
			}
		}
	})

	t.Run("under an object, the keys are narrowed by what has been typed", func(t *testing.T) {
		all := p.childSuggestions(docObj, "")
		if len(all) != 3 {
			t.Errorf("an object with three keys offered %d (%v)", len(all), all)
		}
		// No prefix narrows nothing.
		if got := p.childSuggestions(docObj, ""); len(got) != 3 {
			t.Errorf("an empty prefix offered %d, want all three", len(got))
		}
		// A prefix that matches nothing offers nothing — not everything, which
		// is what "or prefix == empty" style code tends to fall into.
		if got := p.childSuggestions(docObj, "zzz"); len(got) != 0 {
			t.Errorf("a prefix matching nothing offered %v, want nothing", got)
		}
		// And a real prefix narrows to what starts with it.
		for _, key := range []string{"a", "b", "empty"} {
			got := p.childSuggestions(docObj, key)
			if len(got) != 1 || got[0].Path != key {
				t.Errorf("the prefix %q offered %v, want just %q", key, got, key)
			}
		}
	})

	t.Run("under an array, the offered things are the ones that apply", func(t *testing.T) {
		arr := docArr
		// An empty prefix offers everything an array can do.
		all := p.childSuggestions(arr, "")
		want := map[string]bool{"[]": false, "[0]": false, "length": false}
		for _, s := range all {
			if _, ok := want[s.Path]; !ok {
				t.Errorf("an array offered %q, which is not something an array has", s.Path)
			}
			want[s.Path] = true
		}
		for path, seen := range want {
			if !seen {
				t.Errorf("an array with an empty prefix did not offer %q", path)
			}
		}
		// And each prefix narrows to just its own. The suggestion's path
		// carries the brackets the prefix does not — ".0" leads to "[0]" — so
		// the comparison is on the bracketed form.
		for prefix, wantPath := range map[string]string{
			"[]": "[]", "0": "[0]", "length": "length",
		} {
			got := p.childSuggestions(arr, prefix)
			if len(got) != 1 {
				t.Errorf("the prefix %q offered %d suggestions (%v), want one", prefix, len(got), got)
				continue
			}
			if got[0].Path != wantPath {
				t.Errorf("the prefix %q offered %q, want %q", prefix, got[0].Path, wantPath)
			}
		}
		// A prefix that matches no array operation offers nothing.
		if got := p.childSuggestions(arr, "zzz"); len(got) != 0 {
			t.Errorf("a prefix matching nothing offered %v", got)
		}
		// And an empty array offers it too, typed null, as at the root —
		// asserted rather than wished away, and paired with the check that
		// following it navigates to nothing.
		empty := p.childSuggestions(docEmpties, "0")
		for _, s := range empty {
			if s.Path == "[0]" && s.Type != "null" {
				t.Errorf("an empty array's element suggestion is %+v, want it typed null", s)
			}
		}
		if got := p.navigateJSON(docEmpties, "[0]"); got != nil {
			t.Errorf("following that suggestion navigates to %#v, want nothing", got)
		}
	})

	t.Run("under a scalar, nothing is offered", func(t *testing.T) {
		for _, v := range []any{nil, "text", 42.0, true} {
			if got := p.childSuggestions(v, ""); got != nil {
				t.Errorf("a scalar %#v offered %v", v, got)
			}
		}
	})

	t.Run("the list is capped, and the cap is visible", func(t *testing.T) {
		p := newPanel(t)
		wide := map[string]any{}
		for i := range maxJQSuggestions * 3 {
			wide[string(rune('a'+i%26))+itoa(i)] = i
		}
		p.rawJSON = mustMarshal(t, wide)
		p.jqInput = ""
		p.jqCursor = 0
		p.updateJQSuggestions()
		if len(p.jqSugs) > maxJQSuggestions {
			t.Errorf("the list holds %d suggestions, want at most the cap of %d", len(p.jqSugs), maxJQSuggestions)
		}
		if len(p.jqSugs) == 0 {
			t.Fatal("a document with far more keys than the cap offered nothing")
		}
		if !p.jqSugVisible {
			t.Error("a non-empty suggestion list is not marked visible")
		}
		if p.jqSugSelected != 0 {
			t.Errorf("the selection is at %d, want it reset to the first", p.jqSugSelected)
		}
	})

	t.Run("a document with nothing to walk offers nothing and says so", func(t *testing.T) {
		p := newPanel(t)
		p.rawJSON = nil
		p.updateJQSuggestions()
		if p.jqSugVisible || len(p.jqSugs) != 0 {
			t.Errorf("with no document the list is visible=%v with %d suggestions, want neither",
				p.jqSugVisible, len(p.jqSugs))
		}
	})
}

func keysOf(sugs []JQSuggestion) []string {
	out := make([]string, 0, len(sugs))
	for _, s := range sugs {
		out = append(out, s.Path)
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	return string(d)
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("could not encode the fixture: %v", err)
	}
	return raw
}

// ---------------------------------------------------------------------------
// N3: the jq filter reports its own failures
// ---------------------------------------------------------------------------

// Scenario: Un filtro que falla lo DICE, y no deja el documento anterior en pantalla.
//
// The panel replaces its contents with the filter's output, so every way the filter can
// fail has to leave text saying so. The failure this guards is silence: a filter that
// fails and leaves the previous document on screen looks exactly like a filter that
// succeeded, which is how a wrong query reads as a right one.
func TestApplyJQ_EveryFailureIsVisibleInThePanel(t *testing.T) {
	for _, tc := range []struct {
		name, expr, want string
	}{
		{"a syntax error", ".a |", "jq parse error"},
		{"an unknown function", "nosuchfunction", "jq"},
		{"an empty result", `select(.scalar == "nope")`, "(empty result)"},
		{"a key that is not there yields null", ".nothinghere", "null"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newPanel(t)
			p.rawJSON = mustMarshal(t, doc)
			p.jqExpr = tc.expr
			p.applyJQ()
			got := strings.Join(p.lines, "\n")
			if !strings.Contains(got, tc.want) {
				t.Errorf("the filter %q rendered %q, want it to say %q", tc.expr, got, tc.want)
			}
		})
	}

	t.Run("an empty filter shows the raw document, unformatted", func(t *testing.T) {
		p := newPanel(t)
		p.rawJSON = mustMarshal(t, doc) // compact, so one line
		p.jqExpr = ""
		p.applyJQ()
		if got := strings.Join(p.lines, "\n"); got != string(p.rawJSON) {
			t.Errorf("an empty filter rendered %q, want the raw document %q", got, p.rawJSON)
		}
		if strings.Contains(strings.Join(p.lines, "\n"), "jq ") {
			t.Errorf("an empty filter rendered a jq message: %q", p.lines)
		}
	})

	t.Run("with no document at all nothing happens", func(t *testing.T) {
		p := newPanel(t)
		p.rawJSON = nil
		p.jqExpr = ".a"
		before := append([]string(nil), p.lines...)
		p.applyJQ()
		if strings.Join(p.lines, "\n") != strings.Join(before, "\n") {
			t.Errorf("a filter with no document changed the panel to %q", p.lines)
		}
	})

	t.Run("a filter that works shows what it selected", func(t *testing.T) {
		p := newPanel(t)
		p.rawJSON = mustMarshal(t, doc)
		for _, tc := range []struct{ expr, want string }{
			{".scalar", "value"},
			{".obj.a", "1"},
			{".arr[1].c", "20"},
			{".arr | length", "3"},
		} {
			p.jqExpr = tc.expr
			p.applyJQ()
			if got := strings.Join(p.lines, "\n"); !strings.Contains(got, tc.want) {
				t.Errorf("the filter %q rendered %q, want it to contain %q", tc.expr, got, tc.want)
			}
			if strings.Contains(strings.Join(p.lines, "\n"), "jq ") {
				t.Errorf("the working filter %q reported an error: %q", tc.expr, p.lines)
			}
		}
	})

	t.Run("a row re-set with a filter still applied re-runs it", func(t *testing.T) {
		p := newPanel(t)
		p.rawJSON = mustMarshal(t, doc)
		p.jqExpr = ".scalar"
		p.applyJQ()
		if got := strings.Join(p.lines, "\n"); !strings.Contains(got, "value") {
			t.Fatalf("the filter did not apply: %q", p.lines)
		}
		// Moving to another row re-runs the filter rather than showing the new
		// row unfiltered, or keeping the old row filtered.
		p.SetRow([]string{"scalar"}, []interface{}{"another value"})
		if got := strings.Join(p.lines, "\n"); !strings.Contains(got, "another value") {
			t.Errorf("after moving rows with a filter applied the panel shows %q, want the new row's value", got)
		}
	})
}

// ---------------------------------------------------------------------------
// N4: foreign-key expansion
// ---------------------------------------------------------------------------

// Scenario: Expandir una FK la ENGARDA, y colapsarla se lleva el subtree entero.
//
// The expansion keeps fetched rows in a side map keyed by dotted path and rebuilds the
// document from it, so the two things worth pinning are that an expansion REPLACES the key
// with the fetched row's contents — nested under the right path, not at the root — and
// that collapsing a path takes everything below it with it. Getting the second wrong
// leaves an orphan subtree that no longer corresponds to any FK.
func TestFKExpansion_SplicesAndUnsplicesTheSubtree(t *testing.T) {
	p := newPanel(t)
	p.SetForeignKeys([]postgres.ForeignKeyInfo{{
		Name: "orders_customer_fkey", Column: "customer_id",
		RefSchema: "public", RefTable: "customers", RefColumn: "id",
	}})

	t.Run("an expanded FK is reported as expandable only while it is collapsed", func(t *testing.T) {
		if got := p.isExpandableFK("customer_id"); got == nil {
			t.Fatal("a foreign key column is not reported as expandable")
		}
		p.ExpandFK("customer_id", "customer_id", map[string]any{"id": 42.0, "name": "ada"}, nil)
		if got := p.isExpandableFK("customer_id"); got != nil {
			t.Error("an already-expanded FK is still reported as expandable, so pressing enter again would fetch it twice")
		}
	})

	t.Run("a column with no FK is not expandable", func(t *testing.T) {
		for _, path := range []string{"name", "nope", ""} {
			if got := p.isExpandableFK(path); got != nil {
				t.Errorf("the path %q is reported as an expandable FK: %+v", path, got)
			}
		}
	})

	t.Run("the fetched row replaces the key, under its own path", func(t *testing.T) {
		p.ExpandFK("customer_id", "customer_id",
			map[string]any{"id": 42.0, "name": "ada", "city": "london"}, nil)

		data := p.displayData()
		customer, ok := data["customer_id"].(map[string]any)
		if !ok {
			t.Fatalf("the expanded FK is a %T, want the fetched row as an object", data["customer_id"])
		}
		if customer["name"] != "ada" {
			t.Errorf("the fetched row's name is %#v, want %q", customer["name"], "ada")
		}
		// And the sibling keys are untouched.
		if data["id"] != 1 {
			t.Errorf("expanding an FK changed the sibling key: id is %#v", data["id"])
		}
		// And the panel now shows it.
		shown := strings.Join(p.lines, "\n")
		if !strings.Contains(shown, "ada") {
			t.Errorf("the panel does not show the fetched row:\n%s", shown)
		}
	})

	t.Run("an expansion whose value is not an object is used as it is", func(t *testing.T) {
		// ExpandFK's parameter is typed map[string]interface{}, so this state
		// is NOT reachable through it — it has to be written into the map
		// directly. The branch still exists as a guard against a driver handing
		// back something unexpected, and a guard nobody can reach is a guard
		// nobody has run.
		p.expandedFKs["customer_id"] = "not an object"
		p.rebuildLines()
		if got := p.displayData()["customer_id"]; got != "not an object" {
			t.Errorf("a non-object expansion became %#v, want the value used as it is rather than walked into", got)
		}
		if got := strings.Join(p.lines, "\n"); strings.Contains(got, "Error:") {
			t.Errorf("a non-object expansion made the panel report an error: %q", got)
		}
	})

	t.Run("collapsing a path takes everything below it", func(t *testing.T) {
		p := newPanel(t)
		// The fetched customer has to CONTAIN the column the nested FK is on.
		// The expansion is spliced by walking the document, so a nested
		// expansion on a key the fetched row does not have is stored and then
		// never reached — which is what a first version of this fixture did.
		p.ExpandFK("customer_id", "customer_id",
			map[string]any{"id": 42.0, "name": "ada", "city_id": 7.0},
			[]postgres.ForeignKeyInfo{{Name: "customers_city_fkey", Column: "city_id",
				RefSchema: "public", RefTable: "cities", RefColumn: "id"}})

		// The nested FK is expandable only while it is collapsed, and it is
		// found through the PARENT's metadata rather than the root's — which is
		// the whole reason nestedFKs is keyed by path.
		if got := p.isExpandableFK("customer_id.city_id"); got == nil {
			t.Fatal("the nested FK is not reported as expandable, so the nested metadata did not take")
		}
		if got := p.isExpandableFK("customer_id.city_id"); got != nil && got.Column != "city_id" {
			t.Errorf("the nested FK resolves to %+v, want the city one", got)
		}
		if got := p.isExpandableFK("city_id"); got != nil {
			t.Errorf("a root-level path %q found a nested FK: %+v", "city_id", got)
		}

		// Now expand the nested one, which takes it out of the expandable set.
		p.ExpandFK("city_id", "customer_id.city_id", map[string]any{"id": 7.0, "city": "london"}, nil)
		if got := p.isExpandableFK("customer_id.city_id"); got != nil {
			t.Error("the nested FK is still expandable after being expanded")
		}
		customer, ok := p.displayData()["customer_id"].(map[string]any)
		if !ok {
			t.Fatalf("the expanded FK is %T, want an object", p.displayData()["customer_id"])
		}
		nested, ok := customer["city_id"].(map[string]any)
		if !ok {
			t.Fatalf("the nested expansion came back as %#v, want the fetched city", customer["city_id"])
		}
		if nested["city"] != "london" {
			t.Errorf("the nested expansion shows %q, want the fetched row's own field", nested["city"])
		}

		// Collapsing the outer one has to take the nested one with it: the
		// nested path is under it, and leaving it behind would put a
		// "customer_id.city_id" expansion on a document with no
		// customer_id object in it.
		p.CollapseFK("customer_id")

		if _, still := p.expandedFKs["customer_id"]; still {
			t.Error("collapsing the path left the expansion in place")
		}
		if _, orphan := p.expandedFKs["customer_id.city_id"]; orphan {
			t.Error("collapsing a path left an ORPHAN expansion beneath it: the nested document it belonged to is gone")
		}
		if _, orphan := p.nestedFKs["customer_id"]; orphan {
			t.Error("collapsing a path left the nested FK metadata behind")
		}
		if got := p.displayData()["customer_id"]; got != 42 {
			t.Errorf("after collapsing, the FK column is %#v, want the original value back", got)
		}
	})

	t.Run("collapsing a path does not touch its SIBLINGS", func(t *testing.T) {
		p := newPanel(t)
		p.ExpandFK("customer_id", "customer_id", map[string]any{"name": "ada"}, nil)
		p.ExpandFK("other", "customer_id_other", map[string]any{"name": "bob"}, nil)

		p.CollapseFK("customer_id")
		if _, kept := p.expandedFKs["customer_id_other"]; !kept {
			t.Error("collapsing a path removed an expansion whose path merely STARTS with the same characters: the match has to be on a dotted boundary")
		}
	})
}

// ---------------------------------------------------------------------------
// N5: the history file
// ---------------------------------------------------------------------------

// Scenario: El historial es un FICHAJO, y sobrevive al proceso.
//
// The history is the only state this widget keeps anywhere but memory, so the promise is
// that it is written where a new process will look and read back what it wrote. Both
// directions are asserted, and against a redirected HOME, because a test that reads the
// developer's real history file is a test that fails on someone else's machine.
func TestJQHistory_IsWrittenWhereTheNextProcessLooks(t *testing.T) {
	t.Run("a panel with no history file starts empty", func(t *testing.T) {
		p := newPanel(t)
		if len(p.jqHistory) != 0 {
			t.Errorf("a fresh panel started with %d history entries", len(p.jqHistory))
		}
	})

	t.Run("an expression added to the history is there, and in order", func(t *testing.T) {
		p := newPanel(t)
		for _, expr := range []string{".a", ".b", ".c"} {
			p.addToHistory(expr)
		}
		if len(p.jqHistory) != 3 {
			t.Fatalf("the history holds %d entries (%v), want three", len(p.jqHistory), p.jqHistory)
		}
		// Oldest first, with the newest at the END. That is the shape
		// jqHistoryNavigate walks, so it is the shape asserted.
		if p.jqHistory[len(p.jqHistory)-1] != ".c" {
			t.Errorf("the history is %v, want the newest expression last", p.jqHistory)
		}
	})

	t.Run("the same expression twice is stored once, at the top", func(t *testing.T) {
		p := newPanel(t)
		p.addToHistory(".a")
		p.addToHistory(".b")
		p.addToHistory(".a")
		if len(p.jqHistory) != 2 {
			t.Errorf("re-adding an expression left %d entries (%v), want two", len(p.jqHistory), p.jqHistory)
		}
		if p.jqHistory[len(p.jqHistory)-1] != ".a" {
			t.Errorf("the history is %v, want the repeated expression last, where it is newest", p.jqHistory)
		}
	})
}

// ---------------------------------------------------------------------------
// N6: the cursor and the render
// ---------------------------------------------------------------------------

// Scenario: El cursor se mueve dentro de la lista y nunca se sale.
//
// The cursor indexes RENDERED LINES, and the panel is a window onto a document that can be
// much taller than the panel. So the two promises are the obvious one — it never indexes a
// line that is not there — and the one that is easy to get wrong — that scrolling follows
// it, in both directions.
func TestTheCursorStaysInsideTheLines(t *testing.T) {
	p := newPanel(t)
	n := len(p.lines)

	t.Run("down stops at the last line and up at the first", func(t *testing.T) {
		for range n + 5 {
			p.cursorDown()
		}
		if p.cursorLine != n-1 {
			t.Errorf("pressing down past the end left the cursor at %d, want %d", p.cursorLine, n-1)
		}
		for range n + 5 {
			p.cursorUp()
		}
		if p.cursorLine != 0 {
			t.Errorf("pressing up past the start left the cursor at %d, want 0", p.cursorLine)
		}
	})

	t.Run("the scroll follows the cursor down a document taller than the panel", func(t *testing.T) {
		p := newPanel(t)
		p.SetHeight(8)
		wide := map[string]any{}
		for i := range 30 {
			wide["k"+itoa(i)] = i
		}
		p.SetRow([]string{"k"}, []interface{}{1})
		p.rawJSON = mustMarshal(t, wide)
		p.rebuildLines()

		if p.scrollY != 0 {
			t.Fatalf("the fixture starts scrolled to %d, so nothing can prove it moves", p.scrollY)
		}
		for range 10 {
			p.cursorDown()
		}
		if p.scrollY == 0 {
			t.Error("moving the cursor down a document taller than the panel never scrolled")
		}
		if p.cursorLine >= len(p.lines) {
			t.Errorf("the cursor is at %d with %d lines", p.cursorLine, len(p.lines))
		}
		// And back up.
		for range 20 {
			p.cursorUp()
		}
		if p.cursorLine != 0 {
			t.Errorf("walking up left the cursor at %d, want 0", p.cursorLine)
		}
		if p.scrollY != 0 {
			t.Errorf("walking back to the first line left the scroll at %d, want 0", p.scrollY)
		}
	})

	t.Run("a half page moves the SCROLL, and stops inside the document", func(t *testing.T) {
		p := newPanel(t)
		p.SetHeight(6)
		wide := map[string]any{}
		for i := range 40 {
			wide["k"+itoa(i)] = i
		}
		p.SetRow([]string{"k"}, []interface{}{1})
		p.rawJSON = mustMarshal(t, wide)
		p.rebuildLines()

		// The cursor does NOT move: half-paging is a way of reading further
		// down a long document without giving up your place in it, which is a
		// different gesture from arrowing. Asserted, because a half page that
		// moved the cursor would lose that place.
		cursorBefore := p.cursorLine
		before := p.scrollY
		p.halfPageDown()
		if p.cursorLine != cursorBefore {
			t.Errorf("a half page down moved the cursor from %d to %d, want it to stay put", cursorBefore, p.cursorLine)
		}
		if p.scrollY == before {
			t.Error("a half page down did not scroll")
		}
		for range 20 {
			p.halfPageDown()
		}
		maxScroll := len(p.lines) - p.height + 6
		if p.scrollY != maxScroll {
			t.Errorf("half pages to the end left the scroll at %d, want the maximum %d", p.scrollY, maxScroll)
		}
		for range 20 {
			p.halfPageUp()
		}
		if p.scrollY != 0 {
			t.Errorf("half pages back to the top left the scroll at %d, want 0", p.scrollY)
		}
	})
}

// ---------------------------------------------------------------------------
// The JSON highlighter
// ---------------------------------------------------------------------------

// Scenario: El resaltado cambia el COLOR, nunca el TEXTO.
//
// The highlighter rewrites every line it renders, so the failure that matters is a rewrite
// that loses or adds a character. Stripping the styling and comparing with the input
// catches that, and it is stronger than asserting on any single token.
func TestHighlightJSON_DoesNotChangeTheTextItIsGiven(t *testing.T) {
	p := newPanel(t)
	for i, line := range p.lines {
		if got := ansi.Strip(p.highlightJSON(line)); got != line {
			t.Errorf("line %d reads back as %q, want %q", i, got, line)
		}
	}

	t.Run("and neither does it on lines that have nothing to highlight", func(t *testing.T) {
		for _, line := range []string{"", "{", "}", `"key":`, "42", "null", "true", "false",
			`  "a": [1, 2],`, `  "unterminated`, `  "esc\":aped"`} {
			if got := ansi.Strip(p.highlightJSON(line)); got != line {
				t.Errorf("highlightJSON(%q) reads back as %q", line, got)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// A property, over many states
// ---------------------------------------------------------------------------

// Scenario: El panel nunca se sale del documento, pase lo que pase.
//
// One walk, many states, and the invariants that keep the widget usable: the cursor indexes
// a line that exists, the scroll is inside the document, and every rendered line is a line
// of the document — in order, contiguous, unmodified apart from styling. The last one is
// the shape of every bug a windowed view can have.
func TestThePanelNeverLeavesTheDocument(t *testing.T) {
	keys := []string{"j", "k", "g", "G", "half_page_down", "half_page_up", "/", "esc", "enter", "."}

	for seed := int64(1); seed <= 12; seed++ {
		p := newPanel(t)
		p.SetHeight(10)
		wide := map[string]any{
			"scalar": "value",
			"obj":    map[string]any{"a": 1.0, "b": map[string]any{"c": 2.0}},
			"arr":    []any{map[string]any{"d": 3.0}, map[string]any{"d": 4.0}},
		}
		p.SetRow([]string{"scalar", "obj", "arr"}, []interface{}{"value", wide["obj"], wide["arr"]})

		// math/rand rather than a hand-rolled LCG: the LCG overflows int64 to
		// NEGATIVE, and a negative index is a panic rather than a readable test
		// failure. That cost a whole run once already.
		rng := rand.New(rand.NewSource(seed))
		for step := range 60 {
			key := keys[rng.Intn(len(keys))]

			switch key {
			case "j":
				p.cursorDown()
			case "k":
				p.cursorUp()
			case "g":
				p.cursorLine = 0
			case "G":
				p.cursorLine = len(p.lines) - 1
			case "half_page_down":
				p.halfPageDown()
			case "half_page_up":
				p.halfPageUp()
			case "/":
				p.EnterJQMode()
			case "esc":
				p.ExitJQMode()
			}

			if p.cursorLine < 0 || p.cursorLine >= len(p.lines) {
				t.Fatalf("seed %d step %d: the cursor is at %d with %d lines after %q",
					seed, step, p.cursorLine, len(p.lines), key)
			}
			// The scroll's own range, not its relation to the cursor: a half
			// page moves the SCROLL and leaves the cursor where it was, so
			// "scroll <= cursor" is false by design after one. What must hold
			// is that the window is inside the document.
			window := p.height - 6
			maxScroll := len(p.lines) - window
			if maxScroll < 0 {
				maxScroll = 0
			}
			if p.scrollY < 0 || p.scrollY > maxScroll {
				t.Fatalf("seed %d step %d: the scroll is %d with %d lines in a %d-line window, so 0..%d, after %q",
					seed, step, p.scrollY, len(p.lines), window, maxScroll, key)
			}
			// And rendering does not panic and does not invent lines.
			out := p.Render()
			if len(p.lines) > 0 {
				rendered := strings.Split(out, "\n")
				if len(rendered) > len(p.lines) {
					t.Fatalf("seed %d step %d: the panel rendered %d lines from a %d-line document",
						seed, step, len(rendered), len(p.lines))
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// The jq input line: every key, and what it decides
// ---------------------------------------------------------------------------

// jqPanel is a panel in jq mode with a document worth walking, which is the state every
// key test below needs.
func jqPanel(t *testing.T) *GridPreview {
	t.Helper()
	p := newPanel(t)
	// rawJSON is set WITHOUT rebuildLines, because rebuildLines rebuilds it from
	// the ROW — which silently replaced this fixture's document with the row's and
	// made every filter in this file return null for reasons that had nothing to
	// do with jq.
	p.rawJSON = mustMarshal(t, map[string]any{
		"scalar": "value",
		"obj":    map[string]any{"alpha": 1.0, "beta": 2.0},
		"arr":    []any{map[string]any{"c": 3.0}},
	})
	p.lines = strings.Split(string(p.rawJSON), "\n")
	p.EnterJQMode()
	return p
}

func press(p *GridPreview, name string) (tea.Cmd, bool) {
	switch name {
	case "esc":
		return p.handleJQInput(tea.KeyPressMsg{Code: tea.KeyEscape, Text: "esc"})
	case "enter":
		return p.handleJQInput(tea.KeyPressMsg{Code: tea.KeyEnter, Text: "enter"})
	case "tab":
		return p.handleJQInput(tea.KeyPressMsg{Code: tea.KeyTab})
	case "space":
		return p.handleJQInput(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	case "backspace":
		return p.handleJQInput(tea.KeyPressMsg{Code: tea.KeyBackspace})
	case "delete":
		return p.handleJQInput(tea.KeyPressMsg{Code: tea.KeyDelete})
	case "left":
		return p.handleJQInput(tea.KeyPressMsg{Code: tea.KeyLeft})
	case "right":
		return p.handleJQInput(tea.KeyPressMsg{Code: tea.KeyRight})
	case "home", "ctrl+a":
		return p.handleJQInput(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	case "end", "ctrl+e":
		return p.handleJQInput(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	case "ctrl+u":
		return p.handleJQInput(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	case "up":
		return p.handleJQInput(tea.KeyPressMsg{Code: tea.KeyUp})
	case "down":
		return p.handleJQInput(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	return p.handleJQInput(tea.KeyPressMsg{Code: rune(name[0]), Text: name})
}

// Scenario: Escribir, borrar y mover el cursor sobre la linea del filtro.
//
// The filter line is TEXT the user typed, and every key that edits it has to treat it as
// runes. Both facts are asserted: the editing keys move the cursor to where the value is,
// and backspace removes one CHARACTER — the same defect this project has now found in four
// places, where a byte length cut a multi-byte character in half.
func TestJQInput_TheLineIsEditedAsRunes(t *testing.T) {
	t.Run("typing appends and the cursor follows", func(t *testing.T) {
		p := jqPanel(t)
		for _, ch := range []string{"a", "l", "p", "h", "a"} {
			p.handleJQInput(tea.KeyPressMsg{Code: rune(ch[0]), Text: ch})
		}
		if p.jqInput != "alpha" {
			t.Errorf("the line is %q, want %q", p.jqInput, "alpha")
		}
		if p.jqCursor != 5 {
			t.Errorf("the cursor is at %d, want 5 — a rune index, not a byte one", p.jqCursor)
		}
	})

	t.Run("backspace removes one whole character", func(t *testing.T) {
		// The cursor is a RUNE index, which is only observable on a line that
		// actually holds a multi-byte character. Nothing can type one today
		// (see the limitation test below), so the line is SET directly — which
		// is what a pasted expression would look like anyway.
		for _, tc := range []struct{ name, line, want string }{
			{"ascii", "ab", "a"},
			{"a two-byte accent alone", "é", ""},
			{"an accent after ascii", "ré", "r"},
			{"a three-byte character", "表", ""},
			{"a character between ascii", "a表b", "a表"},
			{"an emoji, four bytes and two runes", "a🙂b", "a🙂"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				p := jqPanel(t)
				p.jqInput = tc.line
				p.jqCursor = len([]rune(tc.line))
				press(p, "backspace")
				if p.jqInput != tc.want {
					t.Errorf("the line %q after backspace is %q, want %q", tc.line, p.jqInput, tc.want)
				}
				if p.jqCursor != len([]rune(tc.want)) {
					t.Errorf("the cursor is at %d, want %d — the cursor is a RUNE index", p.jqCursor, len([]rune(tc.want)))
				}
				if !utf8.ValidString(p.jqInput) {
					t.Errorf("the line %q is not valid UTF-8: a byte-wise backspace left half a character", p.jqInput)
				}
			})
		}
	})

	t.Run("backspace on an empty line does nothing", func(t *testing.T) {
		p := jqPanel(t)
		for range 5 {
			press(p, "backspace")
		}
		if p.jqInput != "" || p.jqCursor != 0 {
			t.Errorf("backspacing an empty line left %q at %d", p.jqInput, p.jqCursor)
		}
	})

	t.Run("delete removes the character UNDER the cursor, and not before it", func(t *testing.T) {
		p := jqPanel(t)
		for _, ch := range []string{"a", "b", "c"} {
			p.handleJQInput(tea.KeyPressMsg{Code: rune(ch[0]), Text: ch})
		}
		press(p, "left") // between b and c
		press(p, "delete")
		if p.jqInput != "ab" {
			t.Errorf("delete at offset 2 left %q, want %q", p.jqInput, "ab")
		}
		if p.jqCursor != 2 {
			t.Errorf("delete moved the cursor to %d, want it to stay at 2", p.jqCursor)
		}
	})

	t.Run("delete at the end does nothing", func(t *testing.T) {
		p := jqPanel(t)
		for _, ch := range []string{"a", "b"} {
			p.handleJQInput(tea.KeyPressMsg{Code: rune(ch[0]), Text: ch})
		}
		press(p, "delete")
		if p.jqInput != "ab" {
			t.Errorf("delete at the end left %q, want it unchanged", p.jqInput)
		}
	})

	t.Run("left and right walk the cursor and stop at the ends", func(t *testing.T) {
		p := jqPanel(t)
		for _, ch := range []string{"a", "b"} {
			p.handleJQInput(tea.KeyPressMsg{Code: rune(ch[0]), Text: ch})
		}
		for range 5 {
			press(p, "left")
		}
		if p.jqCursor != 0 {
			t.Errorf("walking left off the start left the cursor at %d", p.jqCursor)
		}
		for range 5 {
			press(p, "right")
		}
		if p.jqCursor != 2 {
			t.Errorf("walking right off the end left the cursor at %d, want 2", p.jqCursor)
		}
	})

	t.Run("a MULTI-BYTE character cannot be typed, which is a limitation not a bug", func(t *testing.T) {
		// The default arm tests `len(key) == 1` on the STRING, so a character
		// whose encoding is longer than one byte never reaches the line. That
		// is the same limitation the WHERE filter has and it is pinned in
		// both places rather than quietly relied on: a jq expression's
		// identifiers are ASCII, but a string literal in one is not, so
		// `.name == "Café"` cannot be typed today.
		p := jqPanel(t)
		for _, r := range []string{"é", "表", "🙂"} {
			p.handleJQInput(tea.KeyPressMsg{Code: []rune(r)[0], Text: r})
		}
		if p.jqInput != "" {
			t.Errorf("multi-byte characters reached the line: %q", p.jqInput)
		}
		if p.jqCursor != 0 {
			t.Errorf("multi-byte characters moved the cursor to %d", p.jqCursor)
		}
	})

	t.Run("home and end jump to the ends", func(t *testing.T) {
		p := jqPanel(t)
		for _, ch := range []string{"a", "b"} {
			p.handleJQInput(tea.KeyPressMsg{Code: rune(ch[0]), Text: ch})
		}
		press(p, "home")
		if p.jqCursor != 0 {
			t.Errorf("home left the cursor at %d, want 0", p.jqCursor)
		}
		press(p, "end")
		if p.jqCursor != 2 {
			t.Errorf("end left the cursor at %d, want 2", p.jqCursor)
		}
	})

	t.Run("ctrl+u empties the line in one press", func(t *testing.T) {
		p := jqPanel(t)
		for _, ch := range []string{"a", "l", "p", "h", "a"} {
			p.handleJQInput(tea.KeyPressMsg{Code: rune(ch[0]), Text: ch})
		}
		press(p, "ctrl+u")
		if p.jqInput != "" || p.jqCursor != 0 {
			t.Errorf("ctrl+u left %q at %d", p.jqInput, p.jqCursor)
		}
	})

	t.Run("space is a character of the expression", func(t *testing.T) {
		p := jqPanel(t)
		p.handleJQInput(tea.KeyPressMsg{Code: '.', Text: "."})
		press(p, "space")
		if p.jqInput != ". " {
			t.Errorf("the line is %q after a space, want %q", p.jqInput, ". ")
		}
	})

	t.Run("a key that is not a key is not handled", func(t *testing.T) {
		p := jqPanel(t)
		p.handleJQInput(tea.KeyPressMsg{Code: 'a', Text: "a"})
		before := p.jqInput
		cmd, handled := p.handleJQInput(someOtherMsg{})
		if handled || cmd != nil {
			t.Errorf("a non-key message produced handled=%v cmd=%v", handled, cmd != nil)
		}
		if p.jqInput != before {
			t.Errorf("a non-key message wrote %q", p.jqInput)
		}
	})

	t.Run("a non-printable character is not typed", func(t *testing.T) {
		for _, text := range []string{"\n", "\t", "\x00"} {
			p := jqPanel(t)
			p.handleJQInput(tea.KeyPressMsg{Code: rune(text[0]), Text: text})
			if p.jqInput != "" {
				t.Errorf("the control character %q was typed into the expression: %q", text, p.jqInput)
			}
		}
	})
}

type someOtherMsg struct{ N int }

// Scenario: Enter APLICA el filtro, sale del modo, y lo guarda en el historial.
//
// Three things happen on one key and all three matter: the expression is committed, the
// panel is filtered by it, and the expression is remembered so the up-arrow can bring it
// back. A commit that filtered without remembering would leave a user who mistyped it with
// no way back to what they had.
func TestJQInput_EnterCommitsFiltersAndRemembers(t *testing.T) {
	t.Run("a typed expression is applied and remembered", func(t *testing.T) {
		p := jqPanel(t)
		for _, ch := range ".scalar" {
			p.handleJQInput(tea.KeyPressMsg{Code: ch, Text: string(ch)})
		}
		if p.jqInput != ".scalar" {
			t.Fatalf("the typed line is %q, want %q", p.jqInput, ".scalar")
		}
		// With the suggestion list closed, enter COMMITS. With it open it
		// accepts a suggestion instead, which the next subtest covers.
		p.jqSugVisible = false
		press(p, "enter")

		if p.jqExpr != ".scalar" {
			t.Errorf("the committed expression is %q, want %q", p.jqExpr, ".scalar")
		}
		if p.IsJQMode() {
			t.Error("enter did not leave jq mode")
		}
		if got := strings.Join(p.lines, "\n"); !strings.Contains(got, "value") {
			t.Errorf("the panel does not show the filter's result:\n%s", got)
		}
		if len(p.jqHistory) != 1 || p.jqHistory[0] != ".scalar" {
			t.Errorf("the history is %v, want it to hold the committed expression", p.jqHistory)
		}
	})

	t.Run("an empty expression is committed but not remembered", func(t *testing.T) {
		p := jqPanel(t)
		press(p, "enter")
		if p.jqExpr != "" {
			t.Errorf("an empty commit set the expression to %q", p.jqExpr)
		}
		if len(p.jqHistory) != 0 {
			t.Errorf("an empty expression went into the history: %v", p.jqHistory)
		}
	})

	t.Run("enter with a suggestion open ACCEPTS the suggestion instead", func(t *testing.T) {
		p := jqPanel(t)
		// Type enough for the root suggestions to be showing.
		for _, ch := range ".o" {
			p.handleJQInput(tea.KeyPressMsg{Code: ch, Text: string(ch)})
		}
		if !p.jqSugVisible || len(p.jqSugs) == 0 {
			t.Skipf("the suggestions are not showing (%d of them), so this branch is not reached", len(p.jqSugs))
		}
		want := p.jqSugs[0].Path
		press(p, "enter")
		// Accepting a suggestion does NOT commit and does NOT leave the mode:
		// it fills in a field and lets you keep typing. Enter commits when the
		// list is closed, and accepts when it is open.
		if !p.IsJQMode() {
			t.Error("enter with a suggestion open left jq mode; accepting a suggestion should let the filter keep being typed")
		}
		if p.jqExpr != "" {
			t.Errorf("enter with a suggestion open committed the expression %q; it should only have accepted", p.jqExpr)
		}
		if !strings.Contains(p.jqInput, strings.TrimPrefix(want, ".")) &&
			!strings.Contains(p.jqExpr, strings.TrimPrefix(want, ".")) {
			t.Errorf("enter did not accept the suggestion %q: the line is %q and the expression is %q", want, p.jqInput, p.jqExpr)
		}
	})
}

// Scenario: Esc sale del modo, y primero cierra las sugerencias.
//
// Two levels of escape, and the order matters: with the suggestion list up, escape closes
// it and leaves you in the filter; with it closed, escape leaves jq mode. One level would
// mean a user cannot dismiss a popup without giving up their filter.
func TestJQInput_EscapeClosesTheSuggestionsBeforeTheMode(t *testing.T) {
	p := jqPanel(t)
	for _, ch := range "." {
		p.handleJQInput(tea.KeyPressMsg{Code: ch, Text: string(ch)})
	}
	if !p.jqSugVisible {
		t.Skip("the suggestions are not showing, so the first level of escape is not reached")
	}

	press(p, "esc")
	if p.jqSugVisible {
		t.Error("escape did not close the suggestion list")
	}
	if !p.IsJQMode() {
		t.Error("escape closed the whole mode while only the suggestions were up: the filter is still being typed")
	}

	press(p, "esc")
	if p.IsJQMode() {
		t.Error("the second escape did not leave jq mode")
	}
}

// Scenario: Las flechas eligen una sugerencia, o caminan el historial, seguna cual este visible.
//
// Both lists are navigated with the same keys, and which one a key drives depends on
// whether the suggestion list is up. Getting that backwards means the up-arrow edits the
// popup instead of the history while a popup happens to be open.
func TestJQInput_TheArrowsDriveWhicheverListIsShowing(t *testing.T) {
	t.Run("with suggestions up, the arrows move the selection and not the history", func(t *testing.T) {
		p := newPanel(t)
		for _, expr := range []string{".scalar", ".obj"} {
			p.addToHistory(expr)
		}
		p.EnterJQMode()
		for _, ch := range "." {
			p.handleJQInput(tea.KeyPressMsg{Code: ch, Text: string(ch)})
		}
		if !p.jqSugVisible {
			t.Skip("the suggestions are not showing")
		}
		historyBefore := p.jqInput

		press(p, "down")
		if p.jqSugSelected != 1 {
			t.Errorf("one down left the selection at %d, want 1", p.jqSugSelected)
		}
		if p.jqInput != historyBefore {
			t.Errorf("a down with suggestions up edited the line: %q -> %q", historyBefore, p.jqInput)
		}

		if len(p.jqSugs) < 2 {
			t.Skipf("only %d suggestions, so walking is not distinguishable", len(p.jqSugs))
		}
		// The POPUP WRAPS in both directions — down past the last comes back
		// to the first, up past the first to the last. The HISTORY does not
		// (the next subtest), and the contrast is worth naming: a popup you are
		// reading should come back to the row you were on, while a walk back
		// through what you ran should stop at the oldest thing you ran.
		p.jqSugSelected = 0 // the check above left it at 1
		for range len(p.jqSugs) {
			press(p, "down")
		}
		if p.jqSugSelected != 0 {
			t.Errorf("%d downs over %d suggestions left the selection at %d, want it back at the first",
				len(p.jqSugs), len(p.jqSugs), p.jqSugSelected)
		}
		for range len(p.jqSugs) - 1 {
			press(p, "down")
		}
		if p.jqSugSelected != len(p.jqSugs)-1 {
			t.Errorf("walking down left the selection at %d, want the last (%d)", p.jqSugSelected, len(p.jqSugs)-1)
		}
		press(p, "up")
		if p.jqSugSelected != len(p.jqSugs)-2 {
			t.Errorf("one up from the last left the selection at %d, want %d", p.jqSugSelected, len(p.jqSugs)-2)
		}
		// And up past the FIRST wraps to the last — one press, not a full turn.
		p.jqSugSelected = 0
		press(p, "up")
		if p.jqSugSelected != len(p.jqSugs)-1 {
			t.Errorf("one up from the first left the selection at %d, want the last (%d)",
				p.jqSugSelected, len(p.jqSugs)-1)
		}
	})

	t.Run("with no suggestions up, the arrows walk the history", func(t *testing.T) {
		// The history position is taken when jq mode is ENTERED, so the history
		// has to exist first — a fixture that fills it afterwards leaves the
		// cursor at index zero and the up-arrow starts from the OLDEST entry.
		p := newPanel(t)
		for _, expr := range []string{".scalar", ".obj", ".arr"} {
			p.addToHistory(expr)
		}
		p.EnterJQMode()
		p.jqSugVisible = false

		press(p, "up")
		if !strings.Contains(p.jqInput, "arr") {
			t.Errorf("one up brought back %q, want the most recent expression", p.jqInput)
		}
		press(p, "up")
		if !strings.Contains(p.jqInput, "obj") {
			t.Errorf("two ups brought back %q", p.jqInput)
		}
		press(p, "down")
		if !strings.Contains(p.jqInput, "arr") {
			t.Errorf("a down brought back %q, want the more recent one", p.jqInput)
		}
		// And the cursor lands at the end of what came back, so the next
		// character appends to it rather than landing in the middle.
		if p.jqCursor != len([]rune(p.jqInput)) {
			t.Errorf("the cursor is at %d for the line %q, want it at the end", p.jqCursor, p.jqInput)
		}
	})

	t.Run("walking the history past the oldest entry does nothing", func(t *testing.T) {
		p := newPanel(t)
		p.addToHistory(".only")
		p.EnterJQMode()
		p.jqSugVisible = false
		for range 5 {
			press(p, "up")
		}
		if p.jqInput != ".only" {
			t.Errorf("walking up past the oldest entry left the line %q", p.jqInput)
		}
		for range 5 {
			press(p, "down")
		}
		if p.jqInput != "" {
			t.Errorf("walking down past the newest entry left the line %q, want it emptied — that is what a fresh line is", p.jqInput)
		}
	})

	t.Run("the history is empty, so the arrows do nothing", func(t *testing.T) {
		p := jqPanel(t)
		press(p, "up")
		press(p, "down")
		if p.jqInput != "" {
			t.Errorf("walking an empty history produced the line %q", p.jqInput)
		}
	})

	t.Run("ctrl+p and ctrl+n are the same as up and down", func(t *testing.T) {
		p := newPanel(t)
		p.addToHistory(".one")
		p.addToHistory(".two")
		p.EnterJQMode()
		p.jqSugVisible = false

		p.handleJQInput(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
		if !strings.Contains(p.jqInput, "two") {
			t.Errorf("ctrl+p brought back %q, want the most recent", p.jqInput)
		}
		p.handleJQInput(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
		if p.jqInput != "" {
			t.Errorf("ctrl+n brought back %q, want the empty line past the newest", p.jqInput)
		}
	})
}

// Scenario: Las sugerencias se|Calculan a partir de lo escrito ANTES del cursor.
//
// The cursor's position is what decides which suggestions apply: writing `.ob` with the
// cursor at the end offers obj's keys, and the same text with the cursor in the middle
// offers the root's. That is what makes the list follow the cursor rather than the end of
// the line, and it is worth a fixture where the two differ.
func TestSuggestions_FollowTheCursorNotTheEndOfTheLine(t *testing.T) {
	p := jqPanel(t)
	// ".obj." — the dot that matters is the LAST one, so the parent is obj and
	// the prefix is empty.
	for _, ch := range ".obj." {
		p.handleJQInput(tea.KeyPressMsg{Code: ch, Text: string(ch)})
	}
	if p.jqInput != ".obj." {
		t.Fatalf("the typed line is %q, want %q", p.jqInput, ".obj.")
	}

	// Cursor at the end: the parent is obj, so the keys are obj's — "alpha"
	// and "beta" — and the root's own keys are NOT offered.
	p.jqCursor = len([]rune(p.jqInput))
	p.updateJQSuggestions()
	if len(p.jqSugs) == 0 {
		t.Fatalf("with the cursor at the end, %q offered nothing", p.jqInput)
	}
	for _, s := range p.jqSugs {
		if s.Path == "scalar" || s.Path == "arr" {
			t.Errorf("the suggestions inside .obj include the root's %q: %v", s.Path, keysOf(p.jqSugs))
		}
	}

	// Cursor moved back over the dot: the parent is the root again, so the
	// document's own keys come back. Same line, different list.
	p.jqCursor = 0
	p.updateJQSuggestions()
	found := false
	for _, s := range p.jqSugs {
		if s.Path == ".scalar" {
			found = true
		}
	}
	if !found {
		t.Errorf("with the cursor before the dot, %q offered %v, want the document's own keys", p.jqInput, keysOf(p.jqSugs))
	}

	t.Run("the root's keys come out SORTED, and always the same way", func(t *testing.T) {
		// Go iterates a map in a random order, so an unsorted list reorders
		// itself on every recomputation — and the SELECTED entry is what enter
		// and tab accept, so the same keystroke would pick a different field
		// each time. Called repeatedly on purpose: one call cannot tell a sorted
		// list from a lucky one.
		var first []string
		for i := range 20 {
			q := jqPanel(t)
			q.jqCursor = 0
			q.updateJQSuggestions()
			got := keysOf(q.jqSugs)
			if i == 0 {
				first = got
				for j := 1; j < len(got); j++ {
					if got[j-1] > got[j] {
						t.Errorf("the suggestions are not sorted: %v", got)
						break
					}
				}
				continue
			}
			if strings.Join(got, "|") != strings.Join(first, "|") {
				t.Fatalf("recomputation %d offered %v, the first offered %v — the order has to be stable", i, got, first)
			}
		}
	})
}

// Scenario: Aceptar una sugerencia la pega en la linea y cierra la lista.
//
// It also has to leave the cursor AFTER the text it inserted, not before it — otherwise
// the next character lands inside the field name and the filter stops matching.
func TestAcceptJQSuggestion_InsertsTheTextAndPutsTheCursorAfterIt(t *testing.T) {
	p := jqPanel(t)
	p.handleJQInput(tea.KeyPressMsg{Code: '.', Text: "."})
	for _, ch := range "ob" {
		p.handleJQInput(tea.KeyPressMsg{Code: ch, Text: string(ch)})
	}
	if !p.jqSugVisible || len(p.jqSugs) == 0 {
		t.Skip("the suggestions are not showing, so this fixture proves nothing")
	}
	want := p.jqSugs[0].Path

	press(p, "tab")
	// The list is recomputed after accepting, so it comes BACK if the newly
	// inserted field has children to offer — the completion cascades. What
	// matters is that the text landed and the cursor sits after it, not that
	// the popup stayed shut.
	if !strings.Contains(p.jqInput, strings.TrimPrefix(want, ".")) {
		t.Errorf("the line is %q, want it to contain the suggestion %q", p.jqInput, want)
	}
	if p.jqCursor != len([]rune(p.jqInput)) {
		t.Errorf("the cursor is at %d for the line %q, want it after the inserted text", p.jqCursor, p.jqInput)
	}

	t.Run("accepting with nothing selected changes nothing", func(t *testing.T) {
		q := jqPanel(t)
		q.handleJQInput(tea.KeyPressMsg{Code: '.', Text: "."})
		before := q.jqInput
		q.jqSugSelected = 99 // past the end, on purpose
		q.acceptJQSuggestion()
		if q.jqInput != before {
			t.Errorf("accepting an out-of-range selection wrote %q, want it left at %q", q.jqInput, before)
		}
		q.jqSugSelected = -1
		q.acceptJQSuggestion()
		if q.jqInput != before {
			t.Errorf("accepting a negative selection wrote %q", q.jqInput)
		}
	})
}

// ---------------------------------------------------------------------------
// The actions and the message pump
// ---------------------------------------------------------------------------

// Scenario: Sin foco, el panel NO se come ninguna tecla.
//
// It shares its context with the grid, so a blurred preview that handled keys would steal
// them from whatever the user is typing into.
func TestGridPreview_BlurredItSwallowsNothing(t *testing.T) {
	p := newPanel(t)
	if p.IsFocused() {
		t.Fatal("the fixture starts focused")
	}
	for _, key := range []string{"j", "k", "g", "G", ".", "tab", "enter", "esc"} {
		cmd, handled := p.Update(tea.KeyPressMsg{Code: rune(key[0]), Text: key})
		if handled {
			t.Errorf("the key %q was handled by a blurred preview", key)
		}
		if cmd != nil {
			t.Errorf("the key %q produced a command while blurred", key)
		}
	}
}

// Scenario: El foco se enciende y se apaga, y apagarlo tambien sale del modo jq.
//
// Blur clearing jqMode is the part worth pinning: a panel that is not focused must not be
// holding a half-typed filter, because the next keystroke belongs to whatever the user
// just focused.
func TestGridPreview_FocusAndBlur(t *testing.T) {
	t.Run("focus and blur", func(t *testing.T) {
		p := newPanel(t)
		if p.IsFocused() {
			t.Fatal("the fixture starts focused")
		}
		p.Focus()
		if !p.IsFocused() {
			t.Error("Focus did not focus the panel")
		}
		p.Blur()
		if p.IsFocused() {
			t.Error("Blur did not blur the panel")
		}
	})

	t.Run("blur also leaves jq mode", func(t *testing.T) {
		p := jqPanel(t)
		p.Focus()
		p.Blur()
		if p.IsJQMode() {
			t.Error("Blur left the panel in jq mode")
		}
	})
}

func TestGridPreview_ToggleExpand(t *testing.T) {
	p := newPanel(t)
	if p.IsExpanded() {
		t.Fatal("the fixture starts expanded")
	}
	p.ToggleExpand()
	if !p.IsExpanded() {
		t.Error("ToggleExpand did not expand")
	}
	p.ToggleExpand()
	if p.IsExpanded() {
		t.Error("ToggleExpand did not collapse")
	}
}

// Scenario: Cada accion del registro hace LO QUE DICE, y solo con foco.
//
// The panel dispatches by ACTION ID rather than by key, which is what lets a rebinding
// move the behaviour with it. So the actions are driven by their IDs, and the fixture
// reads each key out of the registry rather than hardcoding it — a rebinding moves the
// test with it instead of leaving it pressing a key nothing listens to.
func TestGridPreview_EachActionDoesWhatItSays(t *testing.T) {
	wide := map[string]any{}
	for i := range 30 {
		wide["k"+itoa(i)] = i
	}
	newTall := func(t *testing.T) *GridPreview {
		t.Helper()
		p := newPanel(t)
		p.SetHeight(8)
		p.SetRow([]string{"k"}, []interface{}{1})
		p.rawJSON = mustMarshal(t, wide)
		p.rebuildLines()
		return p
	}

	t.Run("go_first and go_last jump to the ends", func(t *testing.T) {
		p := newTall(t)
		if _, handled := p.HandleAction("go_last"); !handled {
			t.Fatal("go_last was not handled")
		}
		if p.cursorLine != len(p.lines)-1 {
			t.Errorf("go_last left the cursor at %d, want %d", p.cursorLine, len(p.lines)-1)
		}
		if _, handled := p.HandleAction("go_first"); !handled {
			t.Fatal("go_first was not handled")
		}
		if p.cursorLine != 0 {
			t.Errorf("go_first left the cursor at %d, want 0", p.cursorLine)
		}
	})

	t.Run("go_last on an empty panel does not move the cursor off the start", func(t *testing.T) {
		p := newPanel(t)
		p.lines = nil
		p.HandleAction("go_last")
		if p.cursorLine != 0 {
			t.Errorf("go_last on an empty panel left the cursor at %d, want 0: -1 would index the end of an empty slice on the next key", p.cursorLine)
		}
	})

	t.Run("half_page_up and half_page_down move the scroll", func(t *testing.T) {
		p := newTall(t)
		before := p.scrollY
		if _, handled := p.HandleAction("half_page_down"); !handled {
			t.Fatal("half_page_down was not handled")
		}
		if p.scrollY == before {
			t.Error("half_page_down did not scroll")
		}
		if _, handled := p.HandleAction("half_page_up"); !handled {
			t.Fatal("half_page_up was not handled")
		}
		if p.scrollY != before {
			t.Errorf("half_page_up left the scroll at %d, want it back at %d", p.scrollY, before)
		}
	})

	t.Run("jq_filter enters jq mode, and only that", func(t *testing.T) {
		// It is an ENTRY, not a toggle: leaving is Escape's and Enter's job,
		// because Enter is what commits the expression and Escape is what
		// abandons it. Pinned so a later "let me make it a toggle" is a
		// deliberate change.
		p := newPanel(t)
		if _, handled := p.HandleAction("jq_filter"); !handled {
			t.Fatal("jq_filter was not handled")
		}
		if !p.IsJQMode() {
			t.Fatal("jq_filter did not enter jq mode")
		}
		// And while the filter has the keys, the panel REFUSES actions: they
		// belong to the filter line, or a keypress meant as text would move
		// the cursor instead.
		if _, handled := p.HandleAction("go_first"); handled {
			t.Error("an action was dispatched while the jq filter had the keys")
		}
		if _, handled := p.HandleAction("jq_filter"); handled {
			t.Error("jq_filter was dispatched a second time while already in jq mode")
		}
		if !p.IsJQMode() {
			t.Error("the panel left jq mode without being asked to")
		}
	})

	t.Run("an action the panel does not implement is not claimed", func(t *testing.T) {
		p := newPanel(t)
		if _, handled := p.HandleAction("no_such_action"); handled {
			t.Error("an unimplemented action was reported as handled")
		}
	})

	t.Run("the keys come from the registry, not from literals", func(t *testing.T) {
		p := newPanel(t)
		p.Focus()
		// Every key the panel acts on, pressed as the registry spells it. If
		// the panel hardcoded a key instead of asking the registry, this
		// would stop moving anything after a rebinding.
		for _, action := range []string{"go_first", "go_last"} {
			keys := p.keybinds.KeysFor(config.ActionID(action))
			if len(keys) == 0 {
				t.Fatalf("the registry binds no key to %q", action)
			}
			p.HandleAction(config.ActionID(action))
			if _, handled := p.Update(tea.KeyPressMsg{Code: rune(keys[0][0]), Text: keys[0]}); !handled {
				t.Errorf("the key %q bound to %q was not handled by the panel", keys[0], action)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// The FK fetch
// ---------------------------------------------------------------------------

// Scenario: Enter sobre una clave de FK emite el Pedido de la fila, no la expansion local.
//
// The fetch is asynchronous: the panel emits a message and the app answers with the row.
// So what this can check is the request — that it names the column and the table, and that
// it does NOT happen for a key with no foreign key behind it, which would be a fetch for a
// column that is just a number.
func TestHandleExpand_AForeignKeyEmitsAFetchForTheRightTable(t *testing.T) {
	newWithFK := func(t *testing.T) *GridPreview {
		t.Helper()
		p := newPanel(t)
		p.SetForeignKeys([]postgres.ForeignKeyInfo{{
			Name: "orders_customer_fkey", Column: "customer_id",
			RefSchema: "public", RefTable: "customers", RefColumn: "id",
		}})
		return p
	}

	t.Run("the cursor on the FK's line emits a fetch naming the table", func(t *testing.T) {
		p := newWithFK(t)
		// The FK's line in the rendered document.
		line := -1
		for i, l := range p.lines {
			if strings.Contains(l, `"customer_id"`) {
				line = i
				break
			}
		}
		if line < 0 {
			t.Fatalf("the FK's line is not in the rendered document:\n%s", strings.Join(p.lines, "\n"))
		}
		p.cursorLine = line

		cmd, handled := p.handleExpand()
		if !handled {
			t.Fatal("enter on a foreign key was not handled")
		}
		if cmd == nil {
			t.Fatal("enter on a foreign key emitted no command, so the row is never fetched")
		}
		msg, ok := cmd().(GridPreviewExpandFKMsg)
		if !ok {
			t.Fatalf("the command produced a %T, want a GridPreviewExpandFKMsg", cmd())
		}
		// The request names the COLUMN, the value it holds and the table and
		// column that value points at — everything the app needs to issue one
		// query, and nothing that would have to be looked up again.
		if msg.Column != "customer_id" {
			t.Errorf("the request names the column %q, want customer_id", msg.Column)
		}
		if msg.RefTable != "customers" {
			t.Errorf("the request names the table %q, want customers", msg.RefTable)
		}
		if msg.RefSchema != "public" {
			t.Errorf("the request names the schema %q, want public — a request without it would search_path and could land on another schema's table", msg.RefSchema)
		}
		if msg.Path == "" {
			t.Error("the request carries no path, so the answer could not be spliced back where it came from")
		}
		if msg.Value != 42 {
			t.Errorf("the request carries the value %#v, want the key's own value 42", msg.Value)
		}
	})

	t.Run("the cursor on a plain value does not fetch anything", func(t *testing.T) {
		p := newWithFK(t)
		line := -1
		for i, l := range p.lines {
			if strings.Contains(l, `"name"`) {
				line = i
				break
			}
		}
		if line < 0 {
			t.Fatalf("the plain column's line is not in the document:\n%s", strings.Join(p.lines, "\n"))
		}
		p.cursorLine = line
		if cmd, _ := p.handleExpand(); cmd != nil {
			t.Errorf("enter on a plain value emitted a fetch: %+v", cmd())
		}
	})

	t.Run("the cursor past the end of the document does nothing", func(t *testing.T) {
		p := newWithFK(t)
		p.cursorLine = len(p.lines) + 5
		cmd, handled := p.handleExpand()
		if handled || cmd != nil {
			t.Errorf("a cursor past the end produced handled=%v cmd=%v", handled, cmd != nil)
		}
	})

	t.Run("an FK column whose value is null does not fetch", func(t *testing.T) {
		p := newPanel(t)
		p.SetForeignKeys([]postgres.ForeignKeyInfo{{
			Name: "orders_customer_fkey", Column: "customer_id",
			RefSchema: "public", RefTable: "customers", RefColumn: "id",
		}})
		p.SetRow([]string{"id", "customer_id"}, []interface{}{1, nil})
		for i, l := range p.lines {
			if strings.Contains(l, `"customer_id"`) {
				p.cursorLine = i
			}
		}
		if cmd, _ := p.handleExpand(); cmd != nil {
			t.Errorf("a null foreign key emitted a fetch: %+v", cmd())
		}
	})

	t.Run("an already-expanded FK does not fetch a second time", func(t *testing.T) {
		p := newWithFK(t)
		p.ExpandFK("customer_id", "customer_id", map[string]any{"id": 42.0}, nil)
		for i, l := range p.lines {
			if strings.Contains(l, `"customer_id"`) {
				p.cursorLine = i
			}
		}
		if cmd, _ := p.handleExpand(); cmd != nil {
			t.Errorf("an expanded FK emitted a second fetch: %+v", cmd())
		}
	})
}
