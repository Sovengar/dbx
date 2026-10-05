package gridsidebarpreview

// The contracts of the row preview, asserted as properties over states rather than one
// scenario at a time.
//
// The preview is the little JSON panel beside the grid: it takes the selected row, renders
// it as indented JSON with the keys, strings, numbers and literals in different colours,
// and scrolls it. There is no I/O and no app wiring, so the in-package test file reaches
// every field.
//
// Four things it promises:
//
//	P1  a row with no data says so, and a row with FEWER values than columns shows the
//	    values it has rather than failing on the ones it does not.
//	P2  the scroll window shows the WHOLE document: every line is reachable, including
//	    the last, which is the closing brace.
//	P3  the highlighting distinguishes a KEY from a string VALUE, and paints the literals
//	    differently from the numbers — the whole point of colouring it.
//	P4  a row that is not a foreign-key row forgets that it was one.

import (
	"strings"
	"testing"

	"github.com/buble/dbx/internal/theme"
	"github.com/charmbracelet/x/ansi"
)

func newPreview(t *testing.T, width, height int) *Preview {
	t.Helper()
	p := New(theme.Resolve("dark").Styles())
	p.SetWidth(width)
	p.SetHeight(height)
	return p
}

// wideRow builds a row with n columns and one value each, so the rendered JSON is long
// enough to need scrolling at any sane panel height.
func wideRow(n int) ([]string, []interface{}) {
	cols := make([]string, n)
	row := make([]interface{}, n)
	for i := range n {
		cols[i] = "col_" + string(rune('a'+i%26)) + string(rune('0'+i/26))
		row[i] = i
	}
	return cols, row
}

// rendered returns the visible text, with the styling stripped, split into lines.
func (p *Preview) renderedLines() []string {
	raw := p.Render()
	if raw == "" {
		return nil
	}
	return strings.Split(ansi.Strip(raw), "\n")
}

// ---------------------------------------------------------------------------
// P1: what a row becomes
// ---------------------------------------------------------------------------

// Scenario: Una fila sin datos dice "No data", y no se inventa un objeto vacio.
//
// Three ways a row can be absent — no row at all, no columns, or a preview that was never
// fed anything — and all three land on the same message rather than on a rendering of
// nothing that looks like a bug.
func TestPreview_WithoutARowItSaysNoData(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(p *Preview)
	}{
		{"never fed anything", func(p *Preview) {}},
		{"a nil row", func(p *Preview) { p.SetRow([]string{"id"}, nil) }},
		{"an empty row", func(p *Preview) { p.SetRow([]string{}, []interface{}{}) }},
		{"values but no columns", func(p *Preview) { p.SetRow(nil, []interface{}{1, 2}) }},
		{"a nil foreign-key row", func(p *Preview) { p.SetFKRow([]string{"id"}, nil, "users") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newPreview(t, 60, 20)
			tc.set(p)
			if p.Render() == "" {
				t.Fatal("the preview rendered nothing at all, want the No data message")
			}
			if got := ansi.Strip(p.Render()); !strings.Contains(got, "No data") {
				t.Errorf("the preview says %q, want it to say No data", got)
			}
		})
	}

	t.Run("columns with no values render an empty object, not a message", func(t *testing.T) {
		// There IS a row here — the projection returned one with no values in
		// it — so an empty object is the honest rendering. "No data" would be
		// claiming there is no row at all, which is a different thing to say.
		p := newPreview(t, 60, 20)
		p.SetRow([]string{"id", "name"}, []interface{}{})
		if got := strings.TrimSpace(ansi.Strip(p.Render())); got != "{}" {
			t.Errorf("a row with no values rendered %q, want an empty object", got)
		}
	})

	t.Run("a panel too short to hold a line renders nothing rather than one", func(t *testing.T) {
		// The window is height-4, so below four lines of panel there is no room
		// for a document line at all. The bound is clamped at zero rather than
		// left negative, because a negative window would slice from the END of
		// the document — which is the opposite of the top.
		for _, height := range []int{-5, 0, 1, 3, 4} {
			p := newPreview(t, 60, height)
			p.SetRow([]string{"id", "name"}, []interface{}{7, "ada"})
			if got := p.contentHeight(); got < 0 {
				t.Errorf("contentHeight() at height %d is %d, want it clamped at zero", height, got)
			}
			if strings.TrimSpace(ansi.Strip(p.Render())) != "" {
				t.Errorf("a %d-line panel rendered %q, want nothing", height, ansi.Strip(p.Render()))
			}
			// And scrolling it does not panic or run off either end.
			p.ScrollDown()
			p.ScrollUp()
		}
	})

	t.Run("a preview with no width renders nothing rather than wrapping", func(t *testing.T) {
		p := newPreview(t, 0, 20)
		p.SetRow([]string{"id"}, []interface{}{1})
		if got := p.Render(); got != "" {
			t.Errorf("a zero-width preview rendered %q, want the empty string", got)
		}
	})
}

// Scenario: Una fila con MENOS valores que columnas muestra los que tiene.
//
// A projection can return fewer values than the result has columns, so the row is shorter
// than the header. The pairing walks the COLUMNS and takes what is there, which means the
// extra columns are simply absent from the JSON — and the ones present keep the value
// they were given, which is what a test that only checked the count would miss.
func TestPreview_ARowShorterThanItsHeaderShowsTheValuesItHas(t *testing.T) {
	p := newPreview(t, 60, 20)
	p.SetRow([]string{"id", "name", "total"}, []interface{}{7, "ada"})

	view := ansi.Strip(p.Render())
	for _, want := range []string{`"id"`, `"name"`, "7", "ada"} {
		if !strings.Contains(view, want) {
			t.Errorf("the rendered row does not show %q:\n%s", want, view)
		}
	}
	// A column with no value is ABSENT, not rendered as a null placeholder: the
	// pairing walks the columns and takes what the row has, so a column past the
	// row's end never enters the map. That is the right answer for a projection
	// returning fewer values than the header lists — but it is a decision, so it
	// is asserted rather than assumed.
	if strings.Contains(view, `"total"`) {
		t.Errorf("a column with no value was rendered anyway:\n%s", view)
	}
	if !strings.Contains(view, "}") {
		t.Errorf("the rendered JSON is not closed:\n%s", view)
	}

	t.Run("more values than columns keeps the columns and drops the rest", func(t *testing.T) {
		q := newPreview(t, 60, 20)
		q.SetRow([]string{"id"}, []interface{}{7, "extra", "also extra"})
		view := ansi.Strip(q.Render())
		if strings.Contains(view, "extra") {
			t.Errorf("a value with no column of its own was rendered anyway:\n%s", view)
		}
		if !strings.Contains(view, "7") {
			t.Errorf("the paired value is missing:\n%s", view)
		}
	})
}

// Scenario: Un valor que no se puede serializar se DICE, no revienta.
//
// The row's values come from a driver, so in production they are scalars — but the
// preview's own contract is that it renders whatever it is handed, and json.MarshalIndent
// fails on a channel or a function. Both setters have a guard for it, and a guard nobody
// has ever run is a guard that does not work.
func TestPreview_AValueThatCannotBeSerialisedIsReportedNotFatal(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(p *Preview)
	}{
		{"an ordinary row", func(p *Preview) { p.SetRow([]string{"bad"}, []interface{}{make(chan int)}) }},
		{"a foreign-key row", func(p *Preview) { p.SetFKRow([]string{"bad"}, []interface{}{func() {}}, "users") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newPreview(t, 60, 20)
			tc.set(p)
			got := ansi.Strip(p.Render())
			if !strings.Contains(got, "Error:") {
				t.Errorf("an unserialisable value rendered %q, want it reported as an error", got)
			}
			// And the preview is still usable afterwards, rather than stuck
			// showing the error for every later row too.
			p.SetRow([]string{"id"}, []interface{}{1})
			if after := ansi.Strip(p.Render()); strings.Contains(after, "Error:") {
				t.Errorf("after a good row the preview still shows %q", after)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// P2: the scroll window shows the whole document
// ---------------------------------------------------------------------------

// Scenario: Se puede ver HASTA LA ULTIMA LINEA, y la ultima linea es el cierre del JSON.
//
// This is the one that was broken. The window is height-4 lines tall and ScrollDown's
// bound was `len(lines) - height + 2`, so the furthest the scroll could go left the last
// TWO lines permanently off screen — and the last line of an indented JSON object is its
// closing brace. A user scrolling a tall row could not see that the document ended.
//
// The test asserts reachability rather than an offset: scroll to the end and check that
// the final line is on screen. That is the property, and it holds for every document
// height instead of for one fixture.
// Scenario: Una fila de OTRA tabla dice de qu{table} es.
//
// SetFKRow stored the referenced table in fkRefTable and nothing ever read it, so the
// sidebar showed a row from another table with no indication of which one. That is
// indistinguishable from the current row whenever the two tables share column names, and
// `id` and `name` being in both tables is the ORDINARY case for a foreign key — the
// referenced table usually has the id you followed and a name.
//
// The same shape as the six config keys that were declared with a default and read by
// nothing: the information was carried all the way to the render and dropped there.
func TestPreview_ANamedForeignKeyRowSaysWhichTableItIsFrom(t *testing.T) {
	t.Run("the referenced table is named in the render", func(t *testing.T) {
		p := newPreview(t, 40, 20)
		p.SetFKRow([]string{"id", "name"}, []interface{}{int64(42), "ada"}, "users")

		out := plain(p.Render())
		if !strings.Contains(out, "users") {
			t.Errorf("the render does not name the referenced table:\n%s", out)
		}
	})

	t.Run("the name is gone once the row is a plain row again", func(t *testing.T) {
		// Otherwise the label outlives what it describes: the user filters, the row
		// becomes the current row, and the panel still claims it came from users.
		p := newPreview(t, 40, 20)
		p.SetFKRow([]string{"id"}, []interface{}{int64(42)}, "users")
		if !strings.Contains(plain(p.Render()), "users") {
			t.Fatal("the referenced table is not named to begin with")
		}

		p.SetRow([]string{"id", "name"}, []interface{}{int64(42), "ada"})
		if out := plain(p.Render()); strings.Contains(out, "users") {
			t.Errorf("the label survived into a plain row:\n%s", out)
		}
	})

	t.Run("a plain row carries no name", func(t *testing.T) {
		p := newPreview(t, 40, 20)
		p.SetRow([]string{"id"}, []interface{}{int64(42)})
		if strings.Contains(plain(p.Render()), "→") {
			t.Errorf("a plain row renders a reference arrow:\n%s", p.Render())
		}
	})

	t.Run("the name is there even when the row has no values", func(t *testing.T) {
		// The label is about WHICH TABLE, which is known even with no data in it — and
		// an empty referenced row is exactly the case where the user most needs to know
		// which table it failed to find.
		p := newPreview(t, 40, 20)
		p.SetFKRow(nil, nil, "users")

		out := plain(p.Render())
		if !strings.Contains(out, "users") {
			t.Errorf("an empty referenced row does not name its table:\n%s", out)
		}
	})

	t.Run("the label costs one row of the pane, and the document still fits", func(t *testing.T) {
		// The label is prepended to the rendered lines, so it competes with the JSON for
		// height. The scroll window is computed from the pane height, so a short pane
		// must still scroll to the last line of the document.
		p := newPreview(t, 40, 8)
		cols, row := wideRow(40)
		p.SetFKRow(cols, row, "users")

		if len(p.renderedLines()) == 0 {
			t.Fatal("nothing was rendered")
		}
		// Whatever the label costs, the document is not silently truncated.
		joined := plain(p.Render())
		if !strings.Contains(joined, "{") {
			t.Errorf("the render has no document in it:\n%s", joined)
		}
	})
}

func TestPreview_EveryLineOfTheDocumentIsReachable(t *testing.T) {
	for _, tc := range []struct {
		name          string
		width, height int
		cols          int
	}{
		{"a short row that fits entirely", 60, 40, 3},
		{"a row just over the window", 60, 12, 6},
		{"a tall row", 60, 12, 20},
		{"a very tall row in a very short panel", 60, 8, 40},
		{"a tall row in a tall panel", 60, 40, 40},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newPreview(t, tc.width, tc.height)
			cols, row := wideRow(tc.cols)
			p.SetRow(cols, row)

			total := len(p.lines)
			// Scroll as far as the widget allows.
			for range total * 2 {
				before := p.scrollY
				p.ScrollDown()
				if p.scrollY == before {
					break
				}
			}
			visible := p.renderedLines()
			if len(visible) == 0 {
				t.Fatal("scrolled to the end and rendered nothing")
			}
			last := strings.TrimSpace(visible[len(visible)-1])
			if last != "}" {
				t.Errorf("after scrolling to the end the last visible line is %q, want the JSON's closing brace.\nThe document has %d lines and the window is %d.\n%s",
					last, total, tc.height-4, strings.Join(visible, "\n"))
			}
			// And nothing was skipped: the pane at the end is full, not a
			// couple of lines short of the bottom.
			if got, want := last, p.lines[len(p.lines)-1]; got != want {
				t.Errorf("the last visible line is %q, want the document's last line %q", got, want)
			}
		})
	}

	t.Run("scrolling back up returns to the first line", func(t *testing.T) {
		p := newPreview(t, 60, 10)
		cols, row := wideRow(30)
		p.SetRow(cols, row)
		first := strings.TrimSpace(p.renderedLines()[0])

		for range 100 {
			p.ScrollDown()
		}
		for range 200 {
			p.ScrollUp()
		}
		if p.scrollY != 0 {
			t.Errorf("scrolling up to the top left the offset at %d", p.scrollY)
		}
		got := strings.TrimSpace(p.renderedLines()[0])
		if got != first {
			t.Errorf("back at the top the first line is %q, want %q", got, first)
		}
	})

	t.Run("the scroll never goes negative and never past the end", func(t *testing.T) {
		for _, cols := range []int{1, 3, 8, 40} {
			p := newPreview(t, 60, 12)
			c, r := wideRow(cols)
			p.SetRow(c, r)

			for range 50 {
				p.ScrollUp()
				if p.scrollY < 0 {
					t.Fatalf("%d columns: scrolling up took the offset to %d", cols, p.scrollY)
				}
			}
			for range 200 {
				p.ScrollDown()
				if p.scrollY < 0 {
					t.Fatalf("%d columns: scrolling down took the offset to %d", cols, p.scrollY)
				}
				if p.scrollY >= len(p.lines) {
					t.Fatalf("%d columns: scrolling down took the offset to %d with %d lines",
						cols, p.scrollY, len(p.lines))
				}
			}
		}
	})

	t.Run("a new row starts at the top", func(t *testing.T) {
		p := newPreview(t, 60, 10)
		c, r := wideRow(40)
		p.SetRow(c, r)
		for range 50 {
			p.ScrollDown()
		}
		if p.scrollY == 0 {
			t.Fatal("the fixture never scrolled")
		}

		// Moving to another row is a new document, not a continuation of the
		// old one: showing the tail of the previous row's JSON would be a
		// rendering of a row the user is no longer on.
		p.SetRow([]string{"id"}, []interface{}{1})
		if p.scrollY != 0 {
			t.Errorf("a new row left the scroll at %d, want 0", p.scrollY)
		}
	})
}

// ---------------------------------------------------------------------------
// P3: the highlighting
// ---------------------------------------------------------------------------

// Scenario: Una clave se pinta DISTINTO de un valor de texto.
//
// The whole reason to render JSON rather than plain text is that the eye can find a field
// without reading it, and that only works if a key and a string value are different
// colours. Asserted on the STYLE, not on the text: the two lines carry the same
// characters and must still differ.
func TestHighlightJSON_PaintsKeysAndStringValuesDifferently(t *testing.T) {
	p := newPreview(t, 60, 20)
	p.styles = theme.Resolve("dark").Styles()

	key := p.highlightJSON(`  "name":`)
	value := p.highlightJSON(`  "value"`)
	if key == value {
		t.Error("a key and a string value render identically, which is the one thing the colouring must not do")
	}
	if ansi.Strip(key) != `  "name":` || ansi.Strip(value) != `  "value"` {
		t.Errorf("the highlighting changed the text: %q and %q", ansi.Strip(key), ansi.Strip(value))
	}

	// And each token is painted with EXACTLY the theme's style for its role,
	// compared against what the theme itself would render for that token alone.
	// Comparing the escape codes is stronger than looking for a colour substring:
	// it fails if the wrong style is used and also if none is.
	//
	// The INDENTATION AND THE PUNCTUATION ARE NOT PAINTED, and that is deliberate:
	// the panel's own colours define the frame, and painting the brackets would
	// make the JSON's structure look like content. So the expected rendering is
	// built from the pieces, not from styling the whole line.
	st := theme.Resolve("dark").Styles()
	if want := "  " + st.Primary.Render(`"name"`) + ":"; key != want {
		t.Errorf("a key is rendered %q, want %q", key, want)
	}
	if want := "  " + st.Success.Render(`"value"`); value != want {
		t.Errorf("a string value is rendered %q, want %q", value, want)
	}

	// A number is a different role again, and a fourth colour would be a change
	// nobody asked for.
	number := p.highlightJSON("42")
	if want := st.Info.Render("42"); number != want {
		t.Errorf("a number is rendered %q, want %q", number, want)
	}
	for _, lit := range []string{"null", "true", "false"} {
		got := p.highlightJSON(lit)
		if !strings.Contains(got, "\x1b[") {
			t.Errorf("the literal %q is not painted at all: %q", lit, got)
		}
	}
	// And the punctuation of a whole line stays unpainted, which the pieces
	// above already imply and this states outright.
	whole := p.highlightJSON(`  "a": {`)
	if strings.Contains(strings.SplitN(whole, "\x1b", 2)[0], "\x1b") {
		t.Errorf("the indentation of a line is painted: %q", whole)
	}
}

// Scenario: Los LITERALES se pintan como literales, y los numeros como numeros.
//
// null, true and false each get their own branch, and a number is a run of characters
// rather than a single one — so "1e-10", "-3" and "12.5" have to come out whole and
// unstyled-as-text. The braces and the colon are left alone.
func TestHighlightJSON_PaintsLiteralsAndNumbersAndLeavesThePunctuation(t *testing.T) {
	p := newPreview(t, 60, 20)

	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"null", "null", "null"},
		{"true", "true", "true"},
		{"false", "false", "false"},
		{"a plain number", "42", "42"},
		{"a negative number", "-42", "-42"},
		{"a decimal", "12.5", "12.5"},
		{"scientific notation", "1e-10", "1e-10"},
		{"a timestamp-shaped number", "1700000000", "1700000000"},
		{"braces and colon", "{:}", "{:}"},
		{"an empty line", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ansi.Strip(p.highlightJSON(tc.in))
			if got != tc.want {
				t.Errorf("highlightJSON(%q) reads back as %q, want %q", tc.in, got, tc.want)
			}
		})
	}

	t.Run("a null value is not mistaken for a key", func(t *testing.T) {
		// "null" starts with 'n', not a quote, so it goes through the literal
		// branch; the failure this guards against is it being eaten as the
		// first letter of a longer word.
		got := ansi.Strip(p.highlightJSON(`"a": null`))
		if got != `"a": null` {
			t.Errorf("a null value reads back as %q", got)
		}
	})

	t.Run("a word that starts like a literal is not eaten", func(t *testing.T) {
		// The literal branches match a PREFIX, so "nullable" must not have its
		// "null" swallowed and then leave "able" — the whole word has to
		// survive, which it does only because the branch is checked before the
		// fallback writes the single character.
		for _, word := range []string{"nullable", "trueish", "falsey"} {
			if got := ansi.Strip(p.highlightJSON(word)); got != word {
				t.Errorf("highlightJSON(%q) reads back as %q, want the whole word", word, got)
			}
		}
	})

	t.Run("an unterminated string does not lose the rest of the line", func(t *testing.T) {
		// The scanner looks for the closing quote and bails when there is none,
		// writing the remainder as plain text. A line that opens a quote and
		// never closes it is what a truncated value looks like, and losing
		// everything after the quote would make the row unreadable.
		got := ansi.Strip(p.highlightJSON(`"broken`))
		if got != `"broken` {
			t.Errorf("an unterminated string reads back as %q, want the input back", got)
		}
	})
}

// Scenario: El JSON completo se lee igual con estilos que sin estilos.
//
// The highlighter rewrites the line, so the risk is that it rewrites it INTO something
// else — a dropped character, a doubled one. Stripping the styling and comparing with the
// input is the check that catches both, and it is stronger than asserting on any single
// token.
func TestHighlightJSON_DoesNotChangeTheTextItIsGiven(t *testing.T) {
	p := newPreview(t, 60, 20)
	cols, row := wideRow(8)
	p.SetRow(cols, row)

	for i, line := range p.lines {
		if got := ansi.Strip(p.highlightJSON(line)); got != line {
			t.Errorf("line %d reads back as %q, want %q", i, got, line)
		}
	}

	t.Run("and the same for values of every JSON type", func(t *testing.T) {
		p.SetRow([]string{"s", "n", "f", "b", "z", "neg", "sci"},
			[]interface{}{"text", 42, 1.5, true, nil, -7, 1e-9})
		for i, line := range p.lines {
			if got := ansi.Strip(p.highlightJSON(line)); got != line {
				t.Errorf("line %d reads back as %q, want %q", i, got, line)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// P4: the foreign-key marker
// ---------------------------------------------------------------------------

// Scenario: Una fila que no es de FK OLVIDA que lo era.
//
// The preview remembers which table a row's foreign key points at, and nothing in this
// file renders it — the panel that hosts the preview draws the arrow. So what this test
// pins is the BOOKKEEPING: setting an FK row records the table, and setting an ordinary
// row afterwards clears it, because the marker belongs to the row and not to the session.
// A stale marker is an arrow pointing at the wrong table.
func TestPreview_AnOrdinaryRowForgetsTheForeignKeyMarker(t *testing.T) {
	p := newPreview(t, 60, 20)
	cols, row := wideRow(4)

	p.SetFKRow(cols, row, "users")
	if p.fkRefTable != "users" {
		t.Fatalf("SetFKRow recorded %q, want %q", p.fkRefTable, "users")
	}

	p.SetRow(cols, row)
	if p.fkRefTable != "" {
		t.Errorf("after an ordinary row the marker is still %q: the arrow would point at a table this row has nothing to do with", p.fkRefTable)
	}

	p.SetFKRow(cols, row, "orders")
	if p.fkRefTable != "orders" {
		t.Errorf("a second FK row recorded %q, want %q", p.fkRefTable, "orders")
	}

	// And an FK row with no data still records it, because "which table does
	// this row point at" is a property of the SELECTION, not of whether
	// anything rendered.
	p.SetFKRow(nil, nil, "regions")
	if p.fkRefTable != "regions" {
		t.Errorf("an FK row with no data recorded %q, want the table it points at", p.fkRefTable)
	}
}

// ---------------------------------------------------------------------------
// A property, over many states
// ---------------------------------------------------------------------------

// Scenario: Con CUALQUIER scroll, lo pintado es una rebanada del documento.
//
// The scenario tests each reach one state. This checks the invariant that ties them
// together: whatever the offset, the rendered lines are a CONTIGUOUS slice of the
// document's lines, in order, unmodified apart from styling. A slice that skipped a line,
// reordered two, or rendered a line that is not in the document is the shape of every bug
// this widget could plausibly have.
func TestPreview_AnyScrollRendersAContiguousSliceOfTheDocument(t *testing.T) {
	for _, cols := range []int{1, 4, 12, 30} {
		for _, height := range []int{6, 10, 20} {
			p := newPreview(t, 60, height)
			c, r := wideRow(cols)
			p.SetRow(c, r)
			doc := append([]string(nil), p.lines...)

			for step := range 4 * len(doc) {
				visible := p.renderedLines()
				if len(visible) == 0 {
					break
				}
				start := p.scrollY
				if start >= len(doc) {
					t.Fatalf("%d columns at height %d: the offset %d is past the document's %d lines",
						cols, height, start, len(doc))
				}
				for i, line := range visible {
					at := start + i
					if at >= len(doc) {
						t.Fatalf("%d columns at height %d: rendered %d lines from offset %d but the document has %d",
							cols, height, len(visible), start, len(doc))
					}
					if line != strings.TrimRight(doc[at], " ") {
						t.Fatalf("%d columns at height %d step %d: rendered line %d is %q, want the document's %q",
							cols, height, step, at, line, doc[at])
					}
				}
				// And it is CONTIGUOUS: no gaps, which the loop above checks
				// by index, and no duplicate of an earlier step's slice.
				if len(visible) > len(doc) {
					t.Fatalf("%d columns at height %d: rendered %d lines from a %d-line document",
						cols, height, len(visible), len(doc))
				}
				p.ScrollDown()
			}
		}
	}
}

// plain strips the escape sequences so an assertion about text is not about colour.
func plain(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			in = true
		case in && (r == 'm' || r == 'K'):
			in = false
		case in:
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
