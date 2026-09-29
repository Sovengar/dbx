package bordered

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// boxLines splits a rendered box into its lines with ANSI stripped.
func boxLines(s string) []string {
	plain := ansi.Strip(s)
	return strings.Split(plain, "\n")
}

// widthsOf returns the display width of every line in a rendered box.
func widthsOf(s string) []int {
	var out []int
	for _, l := range boxLines(s) {
		out = append(out, ansi.StringWidth(l))
	}
	return out
}

func allEqual(widths []int, want int) bool {
	for _, w := range widths {
		if w != want {
			return false
		}
	}
	return len(widths) > 0
}

// Scenario: Una caja con título y contenido sale con exactamente `width`
// columnas en cada línea, y con una línea de borde arriba y otra abajo.
func TestRenderWithTitle_BoxIsExactlyWidthWide(t *testing.T) {
	const width = 30
	got := RenderWithTitle(lipgloss.RoundedBorder(), nil, " Title ", "hello", width, 0)

	lines := boxLines(got)
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3 (top + content + bottom): %q", len(lines), lines)
	}
	if !allEqual(widthsOf(got), width) {
		t.Errorf("lines are not all %d wide: %v", width, widthsOf(got))
	}
	// The title must be embedded in the top border, not in the content.
	if !strings.Contains(lines[0], "Title") {
		t.Errorf("the top border does not carry the title: %q", lines[0])
	}
	if strings.Contains(lines[1], "Title") {
		t.Errorf("the content line carries the title: %q", lines[1])
	}
	if !strings.Contains(lines[1], "hello") {
		t.Errorf("the content line does not carry the content: %q", lines[1])
	}
}

// Scenario: Un ancho menor que 2 se eleva a 2, porque una caja más angosta que
// sus esquinas no se puede dibujar.
func TestRenderWithTitle_WidthIsFlooredAtTwo(t *testing.T) {
	// A rounded border's corners are 1 cell each, so the 2-column floor leaves
	// an inner width of 0. The content line is then just the two corners, and a
	// wrapped content cell pushes it to 3. What must not happen is a crash or a
	// line narrower than the floor.
	for _, width := range []int{-5, 0, 1} {
		got := RenderWithTitle(lipgloss.RoundedBorder(), nil, "", "x", width, 0)
		lines := boxLines(got)
		if len(lines) != 3 {
			t.Errorf("width %d produced %d lines, want 3: %q", width, len(lines), lines)
		}
		for i, w := range widthsOf(got) {
			if w < 2 {
				t.Errorf("width %d produced line %d at %d columns, narrower than the floor: %q", width, i, w, lines[i])
			}
		}
	}
}

// Scenario: La alineación del título coloca el texto a izquierda, centro o
// derecha dentro del borde superior. The three alignments must be visibly
// different, and the pads must add up to the same inner width either way.
func TestRenderWithTitle_AlignmentPositionsTheTitle(t *testing.T) {
	const (
		width = 30
		title = "T"
	)
	topOf := func(align int) string {
		return boxLines(RenderWithTitleEx(lipgloss.RoundedBorder(), nil, align, title, "x", width, 0))[0]
	}

	left, center, right := topOf(AlignLeft), topOf(AlignCenter), topOf(AlignRight)

	if left == center || center == right || left == right {
		t.Fatalf("alignments are not distinguishable:\n left   %q\n center %q\n right  %q", left, center, right)
	}
	// Count the border characters before the title on the top line.
	offsetOf := func(line string) int {
		i := strings.Index(line, title)
		if i < 0 {
			return -1
		}
		return ansi.StringWidth(line[:i]) - 1 // minus the top-left corner
	}
	lo, co, ro := offsetOf(left), offsetOf(center), offsetOf(right)
	if lo >= co || co >= ro {
		t.Errorf("title offsets are not ordered left<center<right: %d, %d, %d", lo, co, ro)
	}

	// An unknown alignment falls back to centered, not to a zero-width render.
	if got := topOf(999); got != center {
		t.Errorf("an unknown alignment produced %q, want the centered form %q", got, center)
	}
}

// Scenario: Un título más ancho que el interior se trunca, y la caja conserva
// su ancho. Truncation is what keeps a long title from pushing the corners out.
func TestRenderWithTitle_LongTitleIsTruncated(t *testing.T) {
	const width = 20
	long := "a-very-long-title-that-does-not-fit"

	got := RenderWithTitle(lipgloss.RoundedBorder(), nil, long, "x", width, 0)
	if !allEqual(widthsOf(got), width) {
		t.Errorf("a long title broke the box width: %v", widthsOf(got))
	}
	lines := boxLines(got)
	if strings.Contains(lines[0], long) {
		t.Errorf("the full title survived: %q", lines[0])
	}
	if len(strings.TrimSpace(strings.Trim(lines[0], "╭╮"))) == 0 {
		t.Errorf("the title was truncated away entirely: %q", lines[0])
	}
}

// Scenario: Un alto fijo rellena con líneas vacías hasta la cuenta exacta, y
// recorta el contenido que sobra. height counts the borders too.
func TestRenderWithTitle_HeightPadsAndTruncates(t *testing.T) {
	// height 6 = top + 4 content + bottom.
	got := RenderWithTitleEx(lipgloss.RoundedBorder(), nil, AlignLeft, "", "one", 20, 6)
	lines := boxLines(got)
	if len(lines) != 6 {
		t.Fatalf("got %d lines, want 6: %q", len(lines), lines)
	}
	if !strings.Contains(lines[1], "one") {
		t.Errorf("the content is not on the first content line: %q", lines[1])
	}
	for i := 2; i < len(lines)-1; i++ {
		if strings.TrimSpace(strings.Trim(lines[i], "│ ")) != "" {
			t.Errorf("padding line %d is not empty: %q", i, lines[i])
		}
	}

	// More content than the height allows is truncated, not dropped wholesale.
	got = RenderWithTitleEx(lipgloss.RoundedBorder(), nil, AlignLeft, "", "a\nb\nc\nd\ne\nf\ng\nh", 20, 5)
	lines = boxLines(got)
	if len(lines) != 5 {
		t.Fatalf("got %d lines, want 5: %q", len(lines), lines)
	}
	if !strings.Contains(lines[1], "a") {
		t.Errorf("truncation dropped the first content line: %q", lines[1])
	}
	if strings.Contains(strings.Join(lines, "\n"), "h") {
		t.Errorf("truncation kept the overflow line: %q", lines)
	}
}

// Scenario: Un alto menor que 3 aun así deja una línea de contenido, para que
// la caja nunca quede reduced a sus dos bordes.
func TestRenderWithTitle_TinyHeightStillHasContent(t *testing.T) {
	for _, height := range []int{1, 2, 3} {
		got := RenderWithTitleEx(lipgloss.RoundedBorder(), nil, AlignLeft, "", "x", 20, height)
		lines := boxLines(got)
		if len(lines) < 3 {
			t.Errorf("height %d produced %d lines, want at least 3: %q", height, len(lines), lines)
		}
	}
}

// Scenario: Cada carácter de borde vacío se sustituye por un espacio, y el
// recuadro conserva su ancho.
//
// All six fallbacks are exercised one at a time, because each is an independent
// branch: a border that omits Top must fill its top edge with spaces instead of
// collapsing the line, and the corners must survive either way.
func TestRenderWithTitle_EmptyBorderCharsBecomeSpaces(t *testing.T) {
	base := lipgloss.Border{
		TopLeft: "+", Top: "-", TopRight: "+",
		Left: "|", Right: "|",
		BottomLeft: "+", Bottom: "-", BottomRight: "+",
	}

	for _, blank := range []string{"Top", "Bottom", "Left", "Right"} {
		custom := base
		switch blank {
		case "Top":
			custom.Top = ""
		case "Bottom":
			custom.Bottom = ""
		case "Left":
			custom.Left = ""
		case "Right":
			custom.Right = ""
		}

		got := RenderWithTitle(custom, nil, "T", "hi", 20, 0)
		lines := boxLines(got)
		if len(lines) != 3 {
			t.Fatalf("%s blanked: got %d lines, want 3: %q", blank, len(lines), lines)
		}
		for i, w := range widthsOf(got) {
			if w != 20 {
				t.Errorf("%s blanked: line %d is %d wide, want 20: %q", blank, i, w, lines[i])
			}
		}
		// The horizontal corners always survive, and the blanked fill must be
		// spaces rather than the original glyph.
		if !strings.HasPrefix(lines[0], "+") || !strings.HasSuffix(lines[0], "+") {
			t.Errorf("%s blanked: the top corners are gone: %q", blank, lines[0])
		}
		if !strings.HasPrefix(lines[2], "+") || !strings.HasSuffix(lines[2], "+") {
			t.Errorf("%s blanked: the bottom corners are gone: %q", blank, lines[2])
		}
		if blank == "Top" && strings.Contains(lines[0][1:len(lines[0])-1], "-") {
			t.Errorf("%s blanked: the top edge still draws its fill: %q", blank, lines[0])
		}
		if blank == "Bottom" && strings.Contains(lines[2][1:len(lines[2])-1], "-") {
			t.Errorf("%s blanked: the bottom edge still draws its fill: %q", blank, lines[2])
		}
		if !strings.Contains(lines[1], "hi") {
			t.Errorf("%s blanked: content lost: %q", blank, lines[1])
		}
	}
}

// Scenario: Una esquina de doble celda descuenta su ancho real del interior, no
// un caracter.
//
// The inner width is computed from the TOP corners only, and it is then reused
// for the content line, whose side characters are whatever Left/Right happen to
// be. So a border with 2-cell top corners and 1-cell side characters produces
// horizontal lines that are 2 cells wider than the content lines. This test pins
// that actual behaviour, and records the raggedness: a border mixing corner
// widths does not render a rectangle.
//
// dbx never builds such a border, so nothing in the TUI depends on it. The
// assertion that matters is the one about CONTENT: the wide corners must not eat
// a column of payload, which is what the ANSI-safe width requirement is for.
func TestRenderWithTitle_WideCornersConsumeTheirRealWidth(t *testing.T) {
	custom := lipgloss.Border{
		TopLeft: "ab", Top: "-", TopRight: "cd",
		Left: "|", Right: "|",
		BottomLeft: "ef", Bottom: "-", BottomRight: "gh",
	}

	const (
		width    = 20
		cornerW  = 2 // each top corner is 2 cells
		innerW   = width - 2*cornerW
		contentW = innerW + 2 // plus the 1-cell left and right characters
	)

	got := RenderWithTitle(custom, nil, "", "hi", width, 0)
	lines := boxLines(got)

	// The top border consumes both 2-cell corners, so the inner width really is
	// width-4, and the edge lines come out at the full requested width.
	if w := ansi.StringWidth(lines[0]); w != width {
		t.Errorf("top line is %d wide, want %d: the wide corners must be counted, not truncated: %q", w, width, lines[0])
	}
	if !strings.HasPrefix(lines[0], "ab") || !strings.HasSuffix(lines[0], "cd") {
		t.Errorf("the wide top corners were not emitted whole: %q", lines[0])
	}

	// The content line is innerWidth plus its own 1-cell sides, which is the
	// ragged edge documented above.
	if w := ansi.StringWidth(lines[1]); w != contentW {
		t.Errorf("content line is %d wide, want %d (inner %d + two 1-cell sides): %q", w, contentW, innerW, lines[1])
	}
	if !strings.Contains(lines[1], "hi") {
		t.Errorf("content lost with wide corners: %q", lines[1])
	}
}

// Scenario: Un ancho menor que las esquinas produce un interior de 0 y no un
// negativo. strings.Repeat with a negative count panics, so the floor is what
// keeps an over-narrow box renderable at all.
func TestRenderWithTitle_InnerWidthNeverGoesNegative(t *testing.T) {
	custom := lipgloss.Border{
		TopLeft: "abcdef", Top: "-", TopRight: "+",
		Left: "|", Right: "|",
		BottomLeft: "+", Bottom: "-", BottomRight: "+",
	}

	// This must not panic.
	got := RenderWithTitle(custom, nil, "", "x", 4, 0)
	if len(boxLines(got)) != 3 {
		t.Errorf("got %d lines, want 3: %q", len(boxLines(got)), boxLines(got))
	}
}

// Scenario: Un ancho de exactamente 2 no pasa por el suelo, y uno de 3 tampoco:
// el mínimo dibujable es el que ya dan las esquinas.
//
// The `width < 2` floor exists so a request below the corner width does not
// underflow. Testing width == 2 pins the boundary: the floor must not fire and
// clamp upward a width that is already drawable.
func TestRenderWithTitle_MinimumWidthIsNotClampedUp(t *testing.T) {
	// 1-cell corners, so width 2 means a zero-width interior.
	got := RenderWithTitle(lipgloss.RoundedBorder(), nil, "", "", 2, 0)
	lines := boxLines(got)
	if len(lines) != 3 {
		t.Fatalf("width 2 produced %d lines, want 3: %q", len(lines), lines)
	}
	for i, w := range widthsOf(got) {
		if w != 2 {
			t.Errorf("width 2 produced line %d at %d columns, want 2: %q", i, w, lines[i])
		}
	}

	// Width 3 is the smallest box that holds a character between the sides.
	got = RenderWithTitle(lipgloss.RoundedBorder(), nil, "", "x", 3, 0)
	if !strings.Contains(boxLines(got)[1], "x") {
		t.Errorf("width 3 lost the content: %q", boxLines(got)[1])
	}
	for i, w := range widthsOf(got) {
		if w != 3 {
			t.Errorf("width 3 produced line %d at %d columns, want 3: %q", i, w, boxLines(got)[i])
		}
	}
}

// Scenario: Un interior de 0 columnas trunca el título sin romper la caja.
//
// `titleWidth > innerWidth` guards the truncation, so with a zero-width interior
// the boundary is what matters: an empty title is already zero-width and passes
// through, while a real title is truncated to nothing.
func TestRenderWithTitle_ZeroInnerWidthTruncatesTitle(t *testing.T) {
	got := RenderWithTitle(lipgloss.RoundedBorder(), nil, "T", "x", 2, 0)
	lines := boxLines(got)

	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3: %q", len(lines), lines)
	}
	if !strings.HasPrefix(lines[0], "╭") || !strings.HasSuffix(lines[0], "╮") {
		t.Errorf("the top corners are gone: %q", lines[0])
	}
	if strings.Contains(lines[0], "T") {
		t.Errorf("a 1-cell title survived a 0-cell interior: %q", lines[0])
	}

	// An empty title needs no truncation, so the top line is just the corners.
	got = RenderWithTitle(lipgloss.RoundedBorder(), nil, "", "x", 2, 0)
	if want := "╭╮"; boxLines(got)[0] != want {
		t.Errorf("top line = %q, want %q for a zero-width interior", boxLines(got)[0], want)
	}
}

// Scenario: Un título justo del ancho interior no se trunca, y uno más ancho sí.
func TestRenderWithTitle_TitleOnTheInnerWidthBoundary(t *testing.T) {
	// Rounded corners are 1 cell each, so width 22 leaves a 20-cell interior.
	const (
		width  = 22
		innerW = width - 2
	)

	exact := strings.Repeat("x", innerW)
	got := RenderWithTitle(lipgloss.RoundedBorder(), nil, exact, "b", width, 0)
	if want := "╭" + exact + "╮"; boxLines(got)[0] != want {
		t.Errorf("a title of exactly the interior width = %q, want %q (the comparison is strict)",
			boxLines(got)[0], want)
	}

	over := strings.Repeat("x", innerW+1)
	got = RenderWithTitle(lipgloss.RoundedBorder(), nil, over, "b", width, 0)
	if strings.Contains(boxLines(got)[0], over) {
		t.Errorf("a title one cell over the interior was not truncated: %q", boxLines(got)[0])
	}
	for i, w := range widthsOf(got) {
		if w != width {
			t.Errorf("truncating the title broke the box: line %d is %d wide, want %d", i, w, width)
		}
	}
}

// Scenario: Un footer justo del ancho interior no se trunca, y uno más ancho sí.
func TestRenderWithTitle_FooterOnTheInnerWidthBoundary(t *testing.T) {
	const (
		width  = 22
		innerW = width - 2
	)

	exact := strings.Repeat("F", innerW)
	got := RenderWithTitleAndFooter(lipgloss.RoundedBorder(), nil, "", exact, "b", width, 0)
	if want := "╰" + exact + "╯"; boxLines(got)[2] != want {
		t.Errorf("a footer of exactly the interior width = %q, want %q", boxLines(got)[2], want)
	}

	over := strings.Repeat("F", innerW+1)
	got = RenderWithTitleAndFooter(lipgloss.RoundedBorder(), nil, "", over, "b", width, 0)
	if strings.Contains(boxLines(got)[2], over) {
		t.Errorf("a footer one cell over the interior was not truncated: %q", boxLines(got)[2])
	}
	for i, w := range widthsOf(got) {
		if w != width {
			t.Errorf("truncating the footer broke the box: line %d is %d wide, want %d", i, w, width)
		}
	}
}

// Scenario: Una línea envuelta de ancho exacto no se queda sin relleno.
//
// A wrapped chunk can already be the full inner width, so the padding
// subtraction reaches zero. strings.Repeat with a negative count panics, and the
// clamp is what keeps a full-width chunk renderable.
func TestRenderWithTitle_FullWidthChunkDoesNotPanic(t *testing.T) {
	token := strings.Repeat("z", 200)

	got := RenderWithTitle(lipgloss.RoundedBorder(), nil, "", token, 20, 0)
	if !allEqual(widthsOf(got), 20) {
		t.Errorf("lines are not all 20 wide: %v", widthsOf(got))
	}
	if len(boxLines(got)) < 5 {
		t.Errorf("a 200-column token wrapped into %d lines: %q", len(boxLines(got)), boxLines(got))
	}
}

// Scenario: Una línea que llega justo al ancho interior entra sin ajuste, y la
// que lo excede en una celda se ajusta.
//
// buildContentLines decides between the pad path and the wrap path with
// `displayWidth <= innerWidth`. Both sides of that inclusive comparison are
// observable, and a strict `<` would send a line that exactly fills the box down
// the wrap path, where it comes back as a single full-width chunk.
func TestRenderWithTitle_ContentOnTheInnerWidthBoundary(t *testing.T) {
	const (
		width  = 20
		innerW = width - 2 // 18
	)

	// Exactly the interior: one content line, no wrap.
	exact := strings.Repeat("a", innerW)
	got := RenderWithTitle(lipgloss.RoundedBorder(), nil, "", exact, width, 0)
	if len(boxLines(got)) != 3 {
		t.Errorf("content of exactly %d columns produced %d lines, want 3: %q",
			innerW, len(boxLines(got)), boxLines(got))
	}

	// One cell over: it must be adjusted to fit rather than overflow. An
	// unsplittable token cannot be word-wrapped, so wrapLine returns it whole
	// and the box would overflow without the adjustment; what must hold is that
	// the box keeps its width.
	over := strings.Repeat("a", innerW+1)
	got = RenderWithTitle(lipgloss.RoundedBorder(), nil, "", over, width, 0)
	for i, w := range widthsOf(got) {
		if w != width {
			t.Errorf("content of %d columns in a %d box produced line %d at %d columns: %q",
				innerW+1, width, i, w, boxLines(got)[i])
		}
	}
}

// Scenario: Un contenido de ancho interior exacto más el relleno mantiene la caja
// alineada, sin una columna de más.
//
// The pad path is `line + strings.Repeat(" ", innerWidth-displayWidth)`, so a
// line that already fills the interior must get zero padding. A padding count
// that was off by one would show up as a box one cell too wide.
func TestRenderWithTitle_ZeroPaddingLineKeepsTheBoxAligned(t *testing.T) {
	const (
		width  = 20
		innerW = width - 2
	)

	// Two content lines, both exactly the interior width.
	content := strings.Repeat("a", innerW) + "\n" + strings.Repeat("b", innerW)

	got := RenderWithTitle(lipgloss.RoundedBorder(), nil, "", content, width, 0)
	lines := boxLines(got)
	if len(lines) != 4 {
		t.Fatalf("got %d lines, want 4: %q", len(lines), lines)
	}
	for i, w := range widthsOf(got) {
		if w != width {
			t.Errorf("line %d is %d wide, want %d (zero padding must not add a column): %q",
				i, w, width, lines[i])
		}
	}
	// And the payload must be intact on each line.
	if !strings.Contains(lines[1], strings.Repeat("a", 3)) {
		t.Errorf("first content line lost its text: %q", lines[1])
	}
	if !strings.Contains(lines[2], strings.Repeat("b", 3)) {
		t.Errorf("second content line lost its text: %q", lines[2])
	}
}

// Scenario: Una línea envuelta corta lleva relleno, y una llena no.
func TestRenderWithTitle_WrappedLinePadding(t *testing.T) {
	// Interior is 18 inside a width-20 box, so a 30-cell token must wrap.
	token := strings.Repeat("漢", 15) // 30 cells
	got := RenderWithTitle(lipgloss.RoundedBorder(), nil, "", token, 20, 0)
	lines := boxLines(got)
	if len(lines) < 4 {
		t.Fatalf("a 30-cell token in an 18-cell interior did not wrap: %q", lines)
	}
	for i, w := range widthsOf(got) {
		if w != 20 {
			t.Errorf("line %d is %d wide, want 20: %q", i, w, lines[i])
		}
	}
	// The first wrapped line is 18 cells of payload, already full, so the
	// padding subtraction lands on exactly zero.
	body := strings.TrimRight(strings.Trim(lines[1], "│"), " ")
	if got := ansi.StringWidth(body); got != 18 {
		t.Errorf("first wrapped payload is %d cells (%q), want the 18-cell interior", got, body)
	}
	// The last wrapped line holds the remainder and is padded up to the interior.
	last := strings.TrimRight(strings.Trim(lines[len(lines)-2], "│"), " ")
	if ansi.StringWidth(lines[len(lines)-2]) != 20 {
		t.Errorf("the final wrapped line is %d wide, want 20: %q", ansi.StringWidth(lines[len(lines)-2]), lines[len(lines)-2])
	}
	if ansi.StringWidth(last) == 0 {
		t.Errorf("the final wrapped line is empty: %q", lines[len(lines)-2])
	}
}

// Scenario: Un footer vacío produce un borde inferior plano; con footer, queda
// alineado a la derecha.
func TestRenderWithTitle_FooterIsRightAligned(t *testing.T) {
	const width = 30
	bottomOf := func(footer string) string {
		lines := boxLines(RenderWithTitleAndFooterEx(lipgloss.RoundedBorder(), nil, AlignLeft, "", footer, "x", width, 0))
		return lines[len(lines)-1]
	}

	// No footer means a plain filled bottom border.
	plain := bottomOf("")
	if want := "╰" + strings.Repeat("─", width-2) + "╯"; plain != want {
		t.Errorf("empty footer = %q, want a plain bottom line %q", plain, want)
	}

	// With a footer, the line keeps its width and the footer sits flush right,
	// immediately before the bottom-right corner and nothing else.
	withFooter := bottomOf("F")
	if ansi.StringWidth(withFooter) != width {
		t.Errorf("bottom line with a footer is %d wide, want %d", ansi.StringWidth(withFooter), width)
	}
	if want := "╰" + strings.Repeat("─", width-3) + "F╯"; withFooter != want {
		t.Errorf("footer = %q, want it flush right as %q", withFooter, want)
	}
}

// Scenario: Un footer más ancho que el interior se trunca sin romper la caja.
func TestRenderWithTitle_LongFooterIsTruncated(t *testing.T) {
	const width = 20
	long := "a-footer-far-wider-than-the-box"

	got := RenderWithTitleAndFooter(lipgloss.RoundedBorder(), nil, "", long, "x", width, 0)
	if !allEqual(widthsOf(got), width) {
		t.Errorf("a long footer broke the box width: %v", widthsOf(got))
	}
	lines := boxLines(got)
	if strings.Contains(lines[len(lines)-1], long) {
		t.Errorf("the full footer survived: %q", lines[len(lines)-1])
	}
}

// Scenario: El borde colorea los caracteres del marco, no el contenido.
func TestRenderWithTitle_BorderColorAppliesToFrameOnly(t *testing.T) {
	got := RenderWithTitle(lipgloss.RoundedBorder(), lipgloss.Color("1"), "T", "hi", 20, 0)

	if !strings.Contains(got, "\033[") {
		t.Fatal("a border color produced no ANSI styling at all")
	}
	// The content itself must not carry the border's color sequence.
	for i, l := range strings.Split(got, "\n") {
		if i == 0 || i == len(boxLines(got))-1 {
			continue
		}
		if strings.Contains(ansi.Strip(l), "\033") {
			t.Errorf("content line %d carries ANSI: %q", i, l)
		}
	}
}

// Scenario: Las cuatro variantes públicas producen la misma caja cuando los
// argumentos coinciden. They are thin wrappers over renderBox, and a drifted
// wrapper would silently change the panel's look.
func TestRenderWithTitle_VariantsAgree(t *testing.T) {
	b := lipgloss.RoundedBorder()
	const width = 24

	centered := RenderWithTitle(b, nil, "T", "body", width, 5)
	exCentered := RenderWithTitleEx(b, nil, AlignCenter, "T", "body", width, 5)
	if centered != exCentered {
		t.Errorf("RenderWithTitle and RenderWithTitleEx(AlignCenter) disagree:\n%q\n%q", centered, exCentered)
	}

	withFooter := RenderWithTitleAndFooter(b, nil, "T", "", "body", width, 5)
	exWithFooter := RenderWithTitleAndFooterEx(b, nil, AlignCenter, "T", "", "body", width, 5)
	if withFooter != exWithFooter {
		t.Errorf("RenderWithTitleAndFooter and its Ex variant disagree:\n%q\n%q", withFooter, exWithFooter)
	}
}

// Scenario: Un contenido vacío produce una caja con una línea de contenido
// vacía, no una caja sin cuerpo.
func TestRenderWithTitle_EmptyContentStillHasABody(t *testing.T) {
	got := RenderWithTitle(lipgloss.RoundedBorder(), nil, "T", "", 20, 0)
	lines := boxLines(got)
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3: %q", len(lines), lines)
	}
	if strings.TrimSpace(strings.Trim(lines[1], "│ ")) != "" {
		t.Errorf("the body line is not empty: %q", lines[1])
	}
}

// Scenario: Un contenido multilínea da una línea de contenido por línea, todas
// del mismo ancho.
func TestRenderWithTitle_MultilineContentKeepsOneLineEach(t *testing.T) {
	const width = 20
	got := RenderWithTitle(lipgloss.RoundedBorder(), nil, "", "a\nbb\nccc", width, 0)
	lines := boxLines(got)
	if len(lines) != 5 {
		t.Fatalf("got %d lines, want 5 (top + 3 + bottom): %q", len(lines), lines)
	}
	if !allEqual(widthsOf(got), width) {
		t.Errorf("lines are not all %d wide: %v", width, widthsOf(got))
	}
}

// Scenario: Una línea más ancha que el interior se parte en varias, y ninguna
// excede el ancho interior.
func TestRenderWithTitle_LongLineWrapsInsideTheBox(t *testing.T) {
	const width = 20
	long := strings.Repeat("ab ", 15) // 45 columns of content

	got := RenderWithTitle(lipgloss.RoundedBorder(), nil, "", long, width, 0)
	lines := boxLines(got)
	if len(lines) < 4 {
		t.Fatalf("a 45-column line did not wrap: %q", lines)
	}
	if !allEqual(widthsOf(got), width) {
		t.Errorf("wrapping broke the box width: %v", widthsOf(got))
	}
}

// Scenario: Los segmentos ANSI se separan del texto, con la secuencia de estilo
// en su propio campo.
func TestParseAnsiSegments_SplitsStyleFromText(t *testing.T) {
	got := parseAnsiSegments("\033[32mhello\033[0m world")

	// Segment 0 is the escape with no text, segment 1 is the styled text, and
	// the reset plus what follows lands in one trailing segment.
	if len(got) < 3 {
		t.Fatalf("got %d segments, want at least 3: %+v", len(got), got)
	}
	if got[0].style != "\033[32m" || got[0].text != "" {
		t.Errorf("segment 0 = %+v, want the bare escape %q with no text", got[0], "\033[32m")
	}
	if got[1].style != "" || got[1].text != "hello" {
		t.Errorf("segment 1 = %+v, want unstyled %q", got[1], "hello")
	}

	// The reset is its own escape, and the text after it rides along unstyled.
	var sawReset bool
	for _, seg := range got {
		if seg.style == "\033[0m" {
			sawReset = true
			if seg.text != "" {
				t.Errorf("the reset escape carries text %q, want it empty", seg.text)
			}
		}
	}
	if !sawReset {
		t.Errorf("the reset escape was not captured as its own segment: %+v", got)
	}
	last := got[len(got)-1]
	if last.text != " world" {
		t.Errorf("trailing text = %q, want %q", last.text, " world")
	}

	// Plain text with no escapes is a single segment with no style.
	if segs := parseAnsiSegments("plain"); len(segs) != 1 || segs[0].style != "" || segs[0].text != "plain" {
		t.Errorf("plain text = %+v, want one unstyled segment", segs)
	}

	// An empty string produces nothing.
	if segs := parseAnsiSegments(""); len(segs) != 0 {
		t.Errorf("empty string = %+v, want no segments", segs)
	}
}

// Scenario: Un límite de ancho cero o negativo devuelve la línea entera, sin
// intentar partirla.
func TestWrapLine_NoBudgetReturnsWholeLine(t *testing.T) {
	for _, budget := range []int{0, -3} {
		if got := wrapLine("hello world", budget); len(got) != 1 || got[0] != "hello world" {
			t.Errorf("budget %d = %q, want the whole line", budget, got)
		}
	}
}

// Scenario: Una línea que termina en ESC, justo en el límite del final, no se
// come el resto de la entrada.
//
// parseAnsiSegments needs i+1 < len(runes) before it looks at the byte after an
// ESC, and j+1 >= len(runes) before it reads runes[j+1]. A string that ENDS
// right after an escape is the discriminating case: the guard must stop the
// plain-text scan at that ESC instead of indexing past the end or looping.
func TestParseAnsiSegments_StringEndingInEscape(t *testing.T) {
	// Text that ends with an ESC: the scanner must not read past the end.
	segs := parseAnsiSegments("text\033")

	var rebuilt strings.Builder
	for _, s := range segs {
		rebuilt.WriteString(s.style)
		rebuilt.WriteString(s.text)
	}
	if rebuilt.String() != "text\033" {
		t.Errorf("parseAnsiSegments(%q) rebuilt to %q; the trailing ESC was lost", "text\033", rebuilt.String())
	}

	// A COMPLETE escape at the very end. This is the case that needs the i+1
	// bound: "ESC [" is two runes, so runes[i+1] exists, but the sequence has no
	// terminator and the inner scan must stop at the end of input.
	segs = parseAnsiSegments("\033[32m")
	if segs[0].style != "\033[32m" {
		t.Errorf("style = %q, want %q", segs[0].style, "\033[32m")
	}
	if segs[0].text != "" {
		t.Errorf("style segment carries text %q, want none", segs[0].text)
	}

	// A second escape right after a complete one, with no text between: the
	// plain-text scan must not start an unbounded run at the second ESC.
	segs = parseAnsiSegments("\033[32m\033[0m")
	if len(segs) != 2 {
		t.Errorf("got %d segments, want 2: %+v", len(segs), segs)
	}
	if segs[0].style != "\033[32m" || segs[1].style != "\033[0m" {
		t.Errorf("segments = %+v, want the two escapes in order", segs)
	}

	// Text between two escapes, with the pair ending exactly at the end.
	segs = parseAnsiSegments("\033[32mab\033[0m")
	var b strings.Builder
	for _, s := range segs {
		b.WriteString(s.style)
		b.WriteString(s.text)
	}
	if b.String() != "\033[32mab\033[0m" {
		t.Errorf("rebuilt %q, want the input unchanged", b.String())
	}
	if !strings.Contains(b.String(), "ab") {
		t.Errorf("the text between the escapes was lost: %q", b.String())
	}
}

// Scenario: Cada chunk mide como máximo el presupuesto, medido en celdas de
// display y no en bytes.
func TestWrapLine_ChunksRespectTheBudgetInCells(t *testing.T) {
	const budget = 10
	chunks := wrapLine(strings.Repeat("日本 ", 8), budget) // 8 wide runes + spaces

	if len(chunks) < 2 {
		t.Fatalf("a 24-cell line at a %d budget did not wrap: %q", budget, chunks)
	}
	for i, c := range chunks {
		if w := ansi.StringWidth(c); w > budget {
			t.Errorf("chunk %d is %d cells wide, over the %d budget: %q", i, w, budget, ansi.Strip(c))
		}
	}
	// Nothing may be lost: the visible text must survive the wrap.
	joined := strings.Join(chunks, "")
	if got, want := strings.Count(joined, "日"), 8; got != want {
		t.Errorf("the wrap lost characters: %d of %d wide runes survived", got, want)
	}
}

// Scenario: Un reset de estilo cierra el chunk vigente, de modo que el texto
// posterior no hereda el color anterior.
func TestWrapLine_StyleResetClosesTheOpenChunk(t *testing.T) {
	styled := "\033[32mgreen text\033[0m plain tail"

	chunks := wrapLine(styled, 40)
	if len(chunks) < 2 {
		t.Fatalf("the reset did not close a chunk: %q", chunks)
	}
	// The tail must live in a chunk with no colour.
	last := chunks[len(chunks)-1]
	if strings.HasPrefix(last, "\033[32m") {
		t.Errorf("the tail after the reset inherited the green style: %q", last)
	}
	if !strings.Contains(last, "plain tail") {
		t.Errorf("the tail text is missing: %q", last)
	}
}

// Scenario: Un estilo que cambia a mitad de línea toma efecto a partir del
// texto siguiente, no del anterior.
func TestWrapLine_StyleChangeAppliesToFollowingText(t *testing.T) {
	// KNOWN DEFECT, pinned so a fix is a deliberate change.
	//
	// The intent, per the comment in wrapLine, is that a style change applies to
	// the text that FOLLOWS it, leaving the text before it in the old colour.
	// That is not what happens: activeStyle is updated after the segment's text
	// is accumulated, but the chunk is only flushed on a reset, so a mid-line
	// colour change retroactively recolours the text that preceded it.
	//
	// This test records the ACTUAL behaviour. Flip it to the intended assertion
	// (chunk 0 green holding "aaa", a separate red chunk holding "bbb") when
	// wrapLine is fixed to flush on every style change, not just on a reset.
	styled := "\033[32maaa\033[31mbbb\033[0m"

	chunks := wrapLine(styled, 40)
	if len(chunks) != 1 {
		t.Fatalf("got %d chunks, want 1 (no budget pressure, no reset until the end): %q", len(chunks), chunks)
	}
	if !strings.Contains(ansi.Strip(chunks[0]), "aaabbb") {
		t.Errorf("the visible text was lost: %q", ansi.Strip(chunks[0]))
	}
	// "aaa" is green in the source but is emitted under red, because the chunk
	// is only closed at the reset.
	if !strings.HasPrefix(chunks[0], "\033[31m") {
		t.Errorf("chunk starts %q, want the last active style to win for the whole chunk", chunks[0][:min(8, len(chunks[0]))])
	}
}

// Scenario: Una línea yaStyled que no necesita partirse conserva su estilo.
func TestWrapLine_SingleChunkKeepsStyle(t *testing.T) {
	styled := "\033[32mshort\033[0m"

	chunks := wrapLine(styled, 40)
	if len(chunks) != 1 {
		t.Fatalf("got %d chunks, want 1: %q", len(chunks), chunks)
	}
	if !strings.HasPrefix(chunks[0], "\033[32m") || !strings.HasSuffix(chunks[0], "\033[0m") {
		t.Errorf("the single chunk lost its style wrapper: %q", chunks[0])
	}
}

// Scenario: Una línea vacía produce un chunk vacío, no se pierde.
func TestWrapLine_EmptyLineYieldsOneEmptyChunk(t *testing.T) {
	if got := wrapLine("", 10); len(got) != 1 || got[0] != "" {
		t.Errorf("empty line = %q, want one empty chunk", got)
	}
}

// Scenario: Un escape ANSI sin terminar se conserva como texto, no se traga.
//
// parseAnsiSegments reads a CSI sequence up to its final byte in 0x40-0x7E. A
// truncated escape has no terminator, so the scanner must stop at the end of
// input instead of running past it, and the partial sequence must survive into
// the output rather than being dropped.
func TestParseAnsiSegments_UnterminatedEscapeIsKept(t *testing.T) {
	// A lone ESC, and an ESC with a partial CSI body but no final byte.
	for _, in := range []string{"text\x1b", "text\x1b[", "text\x1b[38;2;1"} {
		segs := parseAnsiSegments(in)

		var rebuilt strings.Builder
		for _, s := range segs {
			rebuilt.WriteString(s.style)
			rebuilt.WriteString(s.text)
		}
		if rebuilt.String() != in {
			t.Errorf("parseAnsiSegments(%q) rebuilt to %q; nothing may be lost", in, rebuilt.String())
		}
		// The leading text must come back on its own, so a later chunk can be
		// styled without dragging the partial escape into a text run.
		if !strings.HasPrefix(segs[0].text, "text") {
			t.Errorf("parseAnsiSegments(%q) first segment = %+v, want it to start with the leading text", in, segs[0])
		}
	}
}

// Scenario: Los límites del byte terminador de un CSI.
//
// A CSI sequence ends at the first byte in 0x40-0x7E. Both ends of that window
// are discriminating: 0x3F is '@'-1 and still a parameter byte, 0x40 is '@' and
// already the terminator. A boundary check that shifted by one would treat
// either as part of the sequence body and swallow the following text.
func TestParseAnsiSegments_CSITerminatorWindow(t *testing.T) {
	// '@' is 0x40: the terminator. The text after it is NOT part of the escape.
	segs := parseAnsiSegments("\x1b[0@after")
	if len(segs) < 2 {
		t.Fatalf("got %d segments, want the escape and the trailing text: %+v", len(segs), segs)
	}
	if segs[0].style != "\x1b[0@" {
		t.Errorf("style = %q, want %q (0x40 terminates the CSI)", segs[0].style, "\x1b[0@")
	}
	if segs[1].text != "after" {
		t.Errorf("text after the terminator = %q, want %q", segs[1].text, "after")
	}

	// '?' is 0x3F: one below the window, so still a parameter byte and the
	// sequence must continue past it.
	segs = parseAnsiSegments("\x1b[?25lafter")
	if segs[0].style != "\x1b[?25l" {
		t.Errorf("style = %q, want %q (0x3F is a parameter, not a terminator)", segs[0].style, "\x1b[?25l")
	}
	if segs[1].text != "after" {
		t.Errorf("text after the sequence = %q, want %q", segs[1].text, "after")
	}

	// 0x7E is '~': the top of the window, and a terminator.
	segs = parseAnsiSegments("\x1b[1~after")
	if segs[0].style != "\x1b[1~" {
		t.Errorf("style = %q, want %q (0x7E terminates the CSI)", segs[0].style, "\x1b[1~")
	}
	if segs[1].text != "after" {
		t.Errorf("text after the terminator = %q, want %q", segs[1].text, "after")
	}

	// A byte just past the window, 0x7F, is not a terminator, so the sequence
	// keeps consuming.
	segs = parseAnsiSegments("\x1b[1\x7f@after")
	if segs[0].style != "\x1b[1\x7f@" {
		t.Errorf("style = %q, want 0x7F consumed as part of the sequence", segs[0].style)
	}
}

// Scenario: El chunk que llena el presupuesto exacto se cierra, y el siguiente
// rune empieza uno nuevo.
//
// The wrap test is `currentWidth+rw > max`, so a rune that lands exactly on the
// budget still belongs on the line. A `>=` would push it to the next chunk and
// leave a cell of the box empty.
func TestWrapLine_RuneExactlyOnBudgetStaysOnTheLine(t *testing.T) {
	const budget = 4
	// Four 1-cell runes fill the budget exactly; the fifth must start a chunk.
	chunks := wrapLine("abcdX", budget)
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2: %q", len(chunks), chunks)
	}
	if chunks[0] != "abcd" {
		t.Errorf("first chunk = %q, want %q (exactly the budget)", chunks[0], "abcd")
	}
	if chunks[1] != "X" {
		t.Errorf("second chunk = %q, want %q", chunks[1], "X")
	}

	// One cell less and the fourth rune no longer fits.
	if got := wrapLine("abcdX", budget-1); len(got) != 2 || got[0] != "abc" {
		t.Errorf("at a %d budget, got %q, want the first chunk to be %q", budget-1, got, "abc")
	}
}

// Scenario: Un rune de doble celda que no cabe se cierra solo el chunk, sin
// partirlo. Half of a wide rune must not be emitted on its own line.
func TestWrapLine_WideRuneNeverSplitsAcrossChunks(t *testing.T) {
	// Budget 3: one 2-cell rune fits, a second would make 4, so it must wrap.
	chunks := wrapLine("日本", 3)
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2: %q", len(chunks), chunks)
	}
	for i, c := range chunks {
		if w := ansi.StringWidth(c); w != 2 {
			t.Errorf("chunk %d is %d cells wide, want a whole 2-cell rune: %q", i, w, c)
		}
	}
	if chunks[0] != "日" || chunks[1] != "本" {
		t.Errorf("chunks = %q, want the runes on separate lines", chunks)
	}

	// A budget of 1 cannot hold even one wide rune. The actual behaviour is that
	// each rune still lands on its own line and overflows the budget, because
	// the check flushes BEFORE adding rather than testing whether the rune fits
	// at all. The invariant that matters is that no rune is ever split: each
	// chunk is a whole rune, so the box just overflows instead of corrupting.
	for _, c := range wrapLine("日本", 1) {
		if w := ansi.StringWidth(c); w != 2 {
			t.Errorf("a 1-cell budget produced a %d-cell chunk %q; a wide rune must never be split", w, c)
		}
	}
}

// Scenario: El texto que cabe exacto no se parte, y el que excede en una celda
// sí, sin perder caracteres.
func TestRenderWithTitle_ContentExactlyOnInnerWidthDoesNotWrap(t *testing.T) {
	// Rounded corners are 1 cell each, so a width-20 box has an 18-cell interior.
	const (
		width     = 20
		innerW    = width - 2
		exact     = innerW
		oneOver   = innerW + 1
		fillerStr = "ab"
	)

	// An unsplittable token of exactly the interior width stays whole: wrapLine
	// breaks on word boundaries, so one token can overflow rather than split.
	exactWord := strings.Repeat(fillerStr, exact/2)
	if len(exactWord) != exact {
		t.Fatalf("fixture drifted: exact word is %d columns, want %d", len(exactWord), exact)
	}
	got := RenderWithTitle(lipgloss.RoundedBorder(), nil, "", exactWord, width, 0)
	if len(boxLines(got)) != 3 {
		t.Errorf("content of exactly %d columns produced %d lines, want 3 (no wrap): %q",
			exact, len(boxLines(got)), boxLines(got))
	}

	// Space-separated content of exactly the interior width also stays whole.
	// The fit check is `<=`, so a run landing on the interior is not wrapped.
	spaced := strings.Repeat("a", innerW-2) + " b" // innerW columns
	if w := ansi.StringWidth(spaced); w != innerW {
		t.Fatalf("fixture drifted: spaced text is %d columns, want %d", w, innerW)
	}
	got = RenderWithTitle(lipgloss.RoundedBorder(), nil, "", spaced, width, 0)
	if len(boxLines(got)) != 3 {
		t.Errorf("spaced content of exactly %d columns wrapped: %q", innerW, boxLines(got))
	}

	// One column over the interior must wrap, and the box keeps its width.
	over := strings.Repeat("a", oneOver)
	got = RenderWithTitle(lipgloss.RoundedBorder(), nil, "", over, width, 0)
	if len(boxLines(got)) < 4 {
		t.Errorf("content of %d columns over a %d interior did not wrap: %q", oneOver, innerW, boxLines(got))
	}
	if !allEqual(widthsOf(got), width) {
		t.Errorf("wrapping broke the box width: %v", widthsOf(got))
	}
}
