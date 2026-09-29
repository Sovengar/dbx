package ui

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/buble/dbx/internal/theme"
	"github.com/charmbracelet/x/ansi"
)

func toastMgr() *ToastManager {
	return NewToastManager(theme.Resolve("dark").Styles())
}

// Scenario: Un toast se dibuja con su icono y su mensaje, y desaparece cuando
// expira. Expiry is time-based, so the test drives Update() explicitly rather
// than sleeping.
func TestToastManager_ShowRendersIconAndMessage(t *testing.T) {
	for _, tc := range []struct {
		show func(*ToastManager, string)
		icon string
	}{
		{(*ToastManager).ShowSuccess, "✓"},
		{(*ToastManager).ShowError, "✗"},
		{(*ToastManager).ShowInfo, "ℹ"},
		{(*ToastManager).ShowWarning, "⚠"},
	} {
		m := toastMgr()
		tc.show(m, "hello")
		got := ansi.Strip(m.View())
		if !strings.Contains(got, tc.icon+" hello") {
			t.Errorf("toast is missing the %q icon prefix: %q", tc.icon, got)
		}
	}
}

// Scenario: Un nivel desconocido cae al icono y estilo neutros, no a un panic.
func TestToastManager_UnknownLevelFallsBackToNeutral(t *testing.T) {
	m := toastMgr()
	m.Show("hi", ToastLevel(99))
	got := ansi.Strip(m.View())
	if !strings.Contains(got, "• hi") {
		t.Fatalf("unknown level did not use the neutral bullet icon: %q", got)
	}
}

// Scenario: Sin toasts no se dibuja nada. An empty manager must not emit a blank
// line, or the overlay would occupy space over the grid.
func TestToastManager_EmptyRendersNothing(t *testing.T) {
	m := toastMgr()
	if got := m.View(); got != "" {
		t.Errorf("View() with no toasts = %q, want empty", got)
	}
	if got := m.ViewLines(); len(got) != 0 {
		t.Errorf("ViewLines() with no toasts = %v, want empty", got)
	}
	if got := m.RenderedToasts(); len(got) != 0 {
		t.Errorf("RenderedToasts() with no toasts = %v, want empty", got)
	}
}

// Scenario: Cada toast se dibuja en su propia línea, y varios toasts se apilan.
func TestToastManager_MultipleToastsStackInOrder(t *testing.T) {
	m := toastMgr()
	m.Show("first", ToastInfo)
	m.Show("second", ToastError)
	m.Show("third", ToastSuccess)

	lines := m.ViewLines()
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3: %v", len(lines), lines)
	}
	plain := make([]string, len(lines))
	for i, l := range lines {
		plain[i] = ansi.Strip(l)
	}
	for i, want := range []string{"first", "second", "third"} {
		if !strings.Contains(plain[i], want) {
			t.Errorf("line %d = %q, want it to contain %q", i, plain[i], want)
		}
	}
	// View() is the joined form of the same content.
	if n := len(strings.Split(ansi.Strip(m.View()), "\n")); n != 3 {
		t.Errorf("View() has %d lines, want 3", n)
	}
}

// Scenario: Un toast con duración cero expira en la siguiente actualización,
// mientras que uno con duración un segundo sobrevive. The comparison is
// `<` against the duration, so a toast whose lifetime is exactly spent is
// already gone; the test pins both sides of that boundary.
func TestToastManager_ExpiryBoundaryIsStrict(t *testing.T) {
	m := toastMgr()
	m.Show("exactlySpent", ToastInfo)
	m.Show("notYetSpent", ToastInfo)
	// The second toast is given a full second; the first gets a duration that
	// has already elapsed by the time Update runs.
	m.toasts[0].Duration = 0
	m.toasts[0].Created = time.Now().Add(-time.Second)
	m.toasts[1].Duration = time.Minute
	m.toasts[1].Created = time.Now()

	m.Update()
	if len(m.toasts) != 1 {
		t.Fatalf("got %d toasts, want 1: %v", len(m.toasts), m.toasts)
	}
	if m.toasts[0].Message != "notYetSpent" {
		t.Errorf("survivor = %q, want %q", m.toasts[0].Message, "notYetSpent")
	}
}

// Scenario: Un toast expira pasado su tiempo y sobrevive antes. Update() is the
// only thing that reaps, so a manager left alone keeps everything.
func TestToastManager_UpdateReapsExpiredToasts(t *testing.T) {
	m := toastMgr()
	m.Show("keeper", ToastInfo)
	expiring := m.toasts
	expiring = append(expiring, Toast{
		Message:  "goner",
		Level:    ToastInfo,
		Created:  time.Now().Add(-2 * time.Hour),
		Duration: time.Second,
	})
	m.toasts = expiring

	m.Update()
	if len(m.toasts) != 1 {
		t.Fatalf("got %d toasts after Update, want 1: %v", len(m.toasts), m.toasts)
	}
	if m.toasts[0].Message != "keeper" {
		t.Errorf("surviving toast = %q, want %q", m.toasts[0].Message, "keeper")
	}
}

// Scenario: Un toast cuya duración aún no vence sobrevive a Update.
func TestToastManager_UnexpiredToastSurvives(t *testing.T) {
	m := toastMgr()
	m.Show("fresh", ToastInfo)
	m.Update()
	if len(m.toasts) != 1 {
		t.Fatalf("a fresh toast was reaped: %v", m.toasts)
	}
}

// Scenario: El ancho se acota a un mínimo y un máximo legible. The width is what
// the border and the icon budget are derived from, so both clamps are pinned.
func TestToastManager_CalculateWidthIsClamped(t *testing.T) {
	m := toastMgr()
	if got := m.calculateWidth("hi"); got != toastMinWidth {
		t.Errorf("short message width = %d, want the %d minimum", got, toastMinWidth)
	}
	long := strings.Repeat("x", 200)
	if got := m.calculateWidth(long); got != toastMaxWidth {
		t.Errorf("long message width = %d, want the %d maximum", got, toastMaxWidth)
	}
	// A mid-size message gets its own width plus the icon and border budget.
	mid := strings.Repeat("x", 30)
	if got, want := m.calculateWidth(mid), 30+4+4; got != want {
		t.Errorf("mid message width = %d, want %d (text + icon + border)", got, want)
	}
}

// Scenario: El ancho se mide en celdas, no en bytes: un CJK ocupa 2 y un tab 4.
func TestToastManager_DisplayWidthCountsCellsNotBytes(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want int
	}{
		{"ascii", "abc", 3},
		{"han is double width", "日本", 4},
		{"tab counts four", "\t", 4},
		{"mixed", "a日\tb", 1 + 2 + 4 + 1},
		{"empty", "", 0},
	} {
		if got := displayWidth(tc.in); got != tc.want {
			t.Errorf("%s: displayWidth(%q) = %d, want %d", tc.name, tc.in, got, tc.want)
		}
	}
}

// Scenario: Cada rango de caracteres anchos cuenta como 2 celdas, y solo él.
//
// isWideRune mixes the unicode script tables (Han, Hangul, Katakana, Hiragana)
// with nine hardcoded ranges. Most of each range is already answered by those
// tables, but the TAIL of every range is not, and the tails are the only reason
// the hardcoded checks exist. So the fixture for each range is its first code
// point the tables do NOT match: drop a range and the tables never answer for
// its tail, the rune measures 1 cell, and the test fails.
func TestToastManager_EachWideRuneRangeIsDoubleWidth(t *testing.T) {
	// Every rune below is unmatched by the four script tables, so each is
	// killed only by the hardcoded range it sits in.
	for _, tc := range []struct {
		name string
		r    rune
	}{
		{"cjk radicals supplement", 0x2E9A}, // tail of 0x2E80-0x303E
		{"hiragana/katakana marks", 0x3040}, // tail of 0x3040-0x9FFF
		{"hangul jamo ext-A", 0xD7A4},       // tail of 0xAC00-0xD7AF
		{"cjk compat ideographs", 0xFA6E},   // tail of 0xF900-0xFAFF
		{"cjk compat forms", 0xFE30},        // whole 0xFE30-0xFE6F
		{"fullwidth ascii", 0xFF01},         // whole 0xFF01-0xFF60
		{"fullwidth currency", 0xFFE0},      // whole 0xFFE0-0xFFE6
		{"cjk ext B", 0x20000},              // head of 0x20000-0x2FA1F
	} {
		if !isWideRune(tc.r) {
			t.Errorf("%s: U+%04X reported as narrow", tc.name, tc.r)
			continue
		}
		if got := displayWidth(string(tc.r)); got != 2 {
			t.Errorf("%s: U+%04X measures %d cells, want 2", tc.name, tc.r, got)
		}
	}
}

// Scenario: Los rangos tienen sus límites exactos: 1 celda justo antes, 2
// dentro, 1 celda justo después. A range check that swallowed its neighbours
// would still pass the test above, so the edges are pinned on both sides.
func TestToastManager_WideRuneRangeEdges(t *testing.T) {
	for _, tc := range []struct {
		name             string
		below, in, above rune
	}{
		{"cjk radicals supplement", 0x2E7F, 0x2E80, 0x303F},
		// 0x3040-0x9FFF runs to the top of that block, so its upper edge is
		// U+A000, not a code point inside the range.
		{"hiragana through han", 0x303F, 0x3040, 0xA000},
		{"hangul syllables", 0xABFF, 0xAC00, 0xF8FF},
		// 0xAC00-0xD7AF has no narrow neighbour at its top: 0xD7A3 and 0xD7B0 are
		// both inside Hangul Jamo Extended-A, which the Hangul table already
		// reports as wide. Its head edge is the only bracketed one, so the tail
		// is pinned on its own below.
		{"cjk compat ideographs", 0xF8FF, 0xF900, 0xFB00},
		{"cjk compat forms", 0xFE2F, 0xFE30, 0xFE70},
		{"fullwidth ascii", 0xFF00, 0xFF01, 0xFF61},
		{"fullwidth currency", 0xFFDF, 0xFFE0, 0xFFE7},
	} {
		if isWideRune(tc.below) {
			t.Errorf("%s: U+%04X just below the range reported as wide", tc.name, tc.below)
		}
		if !isWideRune(tc.in) {
			t.Errorf("%s: U+%04X at the range head reported as narrow", tc.name, tc.in)
		}
		if isWideRune(tc.above) {
			t.Errorf("%s: U+%04X just past the range reported as wide", tc.name, tc.above)
		}
	}

	// The Hangul tail is what the hardcoded check exists for, so it is pinned
	// directly rather than through a bracketing neighbour.
	if !isWideRune(0xD7A4) {
		t.Error("U+D7A4, the Hangul Jamo tail only the hardcoded range covers, reported as narrow")
	}
	if !isWideRune(0xD7AF) {
		t.Error("U+D7AF, the last code point of the Hangul range, reported as narrow")
	}
}

// Scenario: Los rangos extendidos de CJK cuentan como 2 celdas hasta su final,
// y el carácter siguiente ya no. The Ext range spans 65k code points, so both
// ends are sampled rather than assumed.
func TestToastManager_CJKExtensionRangesAreDoubleWidth(t *testing.T) {
	for _, r := range []rune{0x20000, 0x2A6E0, 0x2A6DF, 0x2F800, 0x2FA1D} {
		if !isWideRune(r) {
			t.Errorf("U+%04X inside the CJK extension ranges reported as narrow", r)
		}
		if got := displayWidth(string(r)); got != 2 {
			t.Errorf("U+%04X measures %d cells, want 2", r, got)
		}
	}
	if isWideRune(0x2FA20) {
		t.Error("U+2FA20 is past the last CJK extension range but reported as wide")
	}
}

// Scenario: Todos los límites de los rangos anchos, extremo por extremo.
//
// A range check survives mutation testing only when BOTH of its boundaries are
// covered: widening the head swallows the rune below it, narrowing the head
// drops the range's first code point, and the same holds for the tail. Testing
// only a representative in the middle of a range leaves all four mutations
// alive, because no interior rune distinguishes them.
//
// The rune below each head and above each tail must be NARROW. That is the
// discriminating case: a check that grew by one would report it wide, and the
// only way to get that is for the check to be exactly the range it claims.
func TestToastManager_EveryWideRuneBoundaryIsExact(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		below, head, tail, above rune
		// tailIsTableWide marks a range whose next code point is already
		// covered by a unicode script table, so no narrow rune exists above it
		// and the upper boundary is not independently observable.
		tailIsTableWide bool
	}{
		// Hangul Jamo and Hangul Syllables end where the Hangul table keeps
		// going, so their tails cannot be bracketed by a narrow rune.
		{"hangul jamo", 0x10FF, 0x1100, 0x115F, 0x1160, true},
		{"cjk radicals", 0x2E7F, 0x2E80, 0x303E, 0x303F, false},
		{"hiragana/han", 0x303F, 0x3040, 0x9FFF, 0xA000, false},
		{"hangul syllables", 0xABFF, 0xAC00, 0xD7AF, 0xD7B0, true},
		{"cjk compat ideographs", 0xF8FF, 0xF900, 0xFAFF, 0xFB00, false},
		{"cjk compat forms", 0xFE2F, 0xFE30, 0xFE6F, 0xFE70, false},
		{"fullwidth ascii", 0xFF00, 0xFF01, 0xFF60, 0xFF61, false},
		{"fullwidth currency", 0xFFDF, 0xFFE0, 0xFFE6, 0xFFE7, false},
		{"cjk extensions", 0x1FFFF, 0x20000, 0x2FA1F, 0x2FA20, false},
	} {
		// The rune just below the head is narrow: a check that grew downward
		// by one would claim it.
		if isWideRune(tc.below) {
			t.Errorf("%s: U+%04X sits just below the range but is reported wide", tc.name, tc.below)
		}
		if !tc.tailIsTableWide && isWideRune(tc.above) {
			t.Errorf("%s: U+%04X sits just above the range but is reported wide", tc.name, tc.above)
		}
		// Both ends of the range itself are wide: a check that shrank by one
		// would drop them.
		if !isWideRune(tc.head) {
			t.Errorf("%s: U+%04X is the first code point of the range but is reported narrow", tc.name, tc.head)
		}
		if !isWideRune(tc.tail) {
			t.Errorf("%s: U+%04X is the last code point of the range but is reported narrow", tc.name, tc.tail)
		}
	}

	// The head code point of each range is a boundary the tables cannot cover
	// on their own, so it gets its own explicit check. Testing the head through
	// the loop above only kills a boundary mutant when NO script table already
	// claims that exact code point; these five are the ranges whose head IS
	// table-covered, so a `>=` weakened to `>` would go unnoticed there.
	for _, tc := range []struct {
		name string
		r    rune
	}{
		// U+2E80 is CJK Radicals, matched by no script table.
		{"cjk radicals head", 0x2E80},
		// U+3040 is a Hiragana mark, matched by no script table.
		{"hiragana head", 0x3040},
		// U+2FA1F is the last CJK Extension F block, matched by no table.
		{"cjk extension tail", 0x2FA1F},
	} {
		if !isWideRune(tc.r) {
			t.Errorf("%s: U+%04X reported as narrow", tc.name, tc.r)
		}
	}
}

// Scenario: Un texto que cabe entero conserva su espaciado interno.
//
// wrapText has two paths: a fast path that returns the text untouched when it
// already fits, and a word-wrap fallback that rebuilds it from Fields(). The
// fallback collapses runs of whitespace, so a text that fits EXACTLY must take
// the fast path or its spacing is silently normalized. The budget is the
// inclusive end of the `<=` comparison, so "one space between words" would
// survive either path and prove nothing.
func TestToastManager_FittingTextKeepsItsInternalSpacing(t *testing.T) {
	// Four columns, which is exactly the budget.
	if got := wrapText("a  b", 4); len(got) != 1 {
		t.Fatalf("a 4-column text at a 4-column budget = %q, want one line", got)
	}
	if got := wrapText("a  b", 4); got[0] != "a  b" {
		t.Errorf("a fitting text was re-wrapped and its spacing collapsed: %q, want %q", got[0], "a  b")
	}

	// A tab is 4 columns, so "a\tb" is 6 and must survive whole at budget 6.
	if got := wrapText("a\tb", 6); len(got) != 1 || got[0] != "a\tb" {
		t.Errorf("a tabbed text that fits = %q, want it returned untouched", got)
	}
}

// Scenario: El candidato que llena el presupuesto exacto se queda en la línea.
//
// This is the accumulation step, not the fast path, so the fixture must be
// WIDER than the budget: otherwise the fast path returns first and the
// accumulation is never reached. "aaa bbb ccc ddd" is 15 columns; at a budget
// of 11 the running line reaches exactly 11 on the third word, which has to be
// kept, and the fourth word overflows and breaks.
func TestToastManager_WrapAccumulatesUpToTheBudgetExactly(t *testing.T) {
	const (
		text   = "aaa bbb ccc ddd" // 15 columns
		budget = 11
	)

	got := wrapText(text, budget)
	if len(got) != 2 {
		t.Fatalf("got %q, want 2 lines: the third word fills the %d budget exactly and must stay", got, budget)
	}
	if got[0] != "aaa bbb ccc" {
		t.Errorf("first line = %q, want %q (exactly the budget)", got[0], "aaa bbb ccc")
	}
	if got[1] != "ddd" {
		t.Errorf("second line = %q, want %q", got[1], "ddd")
	}

	// One column less and the third word no longer fits, so the break moves
	// earlier. Both sides of the `<=` are pinned by this pair.
	if got := wrapText(text, budget-1); len(got) != 2 {
		t.Errorf("at a %d budget, got %q, want 2 lines with the break one word earlier", budget-1, got)
	} else if got[0] != "aaa bbb" {
		t.Errorf("at a %d budget the first line is %q, want %q (the third word must no longer fit)",
			budget-1, got[0], "aaa bbb")
	}
}

// Scenario: Un texto de una sola palabra se devuelve tal cual, con su
// espaciado, cuando entra en el presupuesto.
func TestToastManager_WrapTextExactlyOnBudgetIsNotSplit(t *testing.T) {
	for _, n := range []int{1, 5, 11, 23} {
		text := strings.Repeat("x", n)
		if got := wrapText(text, n); len(got) != 1 || got[0] != text {
			t.Errorf("a %d-column text at a %d budget = %q, want it intact", n, n, got)
		}
	}
}

// Scenario: ASCII y los signos comunes siguen siendo de 1 celda. The wide
// checks must not creep into the Latin range the rest of the UI depends on.
func TestToastManager_NarrowRunesStayOneCell(t *testing.T) {
	for _, r := range []rune{'a', 'Z', '0', ' ', '!', 'ñ', 'é', '→', '✓', '•', '·'} {
		if isWideRune(r) {
			t.Errorf("%q (U+%04X) reported as wide", r, r)
		}
		if got := displayWidth(string(r)); got != 1 {
			t.Errorf("%q measures %d cells, want 1", r, got)
		}
	}
}

// Scenario: El ancho de un mensaje ancho crece con los caracteres de doble celda,
// así que un texto CJK corto ocupa más que su equivalente ASCII.
func TestToastManager_WideRunesWidenTheToast(t *testing.T) {
	ascii := toastMgr().calculateWidth("abcd")
	cjk := toastMgr().calculateWidth("日本") // 4 cells
	if cjk != ascii {
		t.Errorf("CJK message width = %d, ASCII = %d; double-width runes must count as 2", cjk, ascii)
	}
}

// Scenario: El texto se parte contra el ancho del toast menos las 4 columnas
// del marco, no contra el mensaje.
//
// The toast is sized to fit its message, so an uncapped toast always has more
// budget than text and never wraps. Wrapping only happens once the toast hits
// its 60-column ceiling, where the budget is 60-4 = 56. So a 57-column message
// is the case that pins the reserve: a smaller reserve would wrap it, a larger
// one would not.
func TestToastManager_ContentBudgetIsToastWidthMinusBorder(t *testing.T) {
	// The message must be WIDER than the budget, or the fast path returns it
	// whole and the budget is never consulted.
	//
	// The running total has to land exactly ON the budget, because that is the
	// only place a one-column change in the reserve is visible. So the target
	// is 57, not 56: a 9-column word plus twelve " aaa" (4 each) is 9+48 = 57.
	// With the real 56 budget that word overflows and starts a new line; with
	// 55 it still overflows; with 57 or more it joins. A total of 61 columns
	// guarantees there is a word after the boundary to force the break.
	const (
		budget = toastMaxWidth - 4 // 56
		target = 57                // the running total lands here
		width  = 61                // "aaaaaaaaa" + 13 * " aaa"
	)

	msg := "aaaaaaaaa" + strings.Repeat(" aaa", 13) // 9 + 52
	if got := displayWidth(msg); got != width {
		t.Fatalf("fixture drifted: message is %d columns, want %d", got, width)
	}

	// The first line must be exactly the budget, which only holds if the
	// reserve is the 4 columns this test claims.
	if got := len(wrapText(msg, budget)); got != 2 {
		t.Fatalf("got %d lines, want 2: the %d-column prefix must overflow the %d budget", got, target, budget)
	}
	// The first line holds every word that still fits, which is the widest
	// prefix at or under the budget. With a 57 landing point and a 56 budget,
	// the previous word sits at 53.
	if got := wrapText(msg, budget)[0]; displayWidth(got) != 53 {
		t.Errorf("first line is %d columns (%q), want 53 (the widest prefix under the %d budget)",
			displayWidth(got), got, budget)
	}
	// One column of slack, and the word that lands on 57 would fit instead.
	if got := wrapText(msg, target); displayWidth(got[0]) != target {
		t.Errorf("at a %d budget the first line is %d columns (%q), want %d", target, displayWidth(got[0]), got[0], target)
	}

	m := toastMgr()
	if got := m.calculateWidth(msg); got != toastMaxWidth {
		t.Fatalf("fixture drifted: toast width = %d, want the %d ceiling", got, toastMaxWidth)
	}

	m.Show(msg, ToastInfo)
	got := m.ViewLines()
	if len(got) < 2 {
		t.Fatalf("a %d-column message stayed on one line; the budget is %d, so it must wrap",
			lipgloss.Width(msg), budget)
	}

	// The icon prefix sits outside the text budget, so the first rendered line
	// is the icon plus a full text line: budget + 2 for the icon and its space.
	for i, l := range got {
		plain := ansi.Strip(l)
		// Continuation lines are text only, so they must fit the budget outright.
		if !strings.HasPrefix(plain, "\u2139") {
			if w := lipgloss.Width(plain); w > budget {
				t.Errorf("line %d is %d columns, over the %d budget: %q", i, w, budget, plain)
			}
		}
	}
}

// Scenario: La expiración es estricta: un toast cuyo tiempo se ha cumplido
// exactamente ya no está. The comparison is `<` against the duration, so a
// toast whose age equals its duration is already gone. The two toasts differ
// only in that their elapsed time is just past, and just short of, the
// boundary, so a `<=` comparison would keep both.
func TestToastManager_ExpiryComparisonIsStrict(t *testing.T) {
	m := toastMgr()
	m.Show("long", ToastInfo)
	m.Show("short", ToastInfo)
	m.toasts[0].Duration = time.Nanosecond
	m.toasts[0].Created = time.Now().Add(-time.Hour)
	m.toasts[1].Duration = time.Hour
	m.toasts[1].Created = time.Now()

	m.Update()
	if len(m.toasts) != 1 {
		t.Fatalf("got %d toasts, want 1: %v", len(m.toasts), m.toasts)
	}
	if m.toasts[0].Message != "short" {
		t.Errorf("survivor = %q, want %q", m.toasts[0].Message, "short")
	}
}

// Scenario: Un texto que no entra se parte por palabras, sin cortar ninguna.
func TestToastManager_WrapTextBreaksOnWords(t *testing.T) {
	text := "alpha bravo charlie delta"
	got := wrapText(text, 12)
	// "alpha bravo" is 11 wide; adding " charlie" would make 19, over 12.
	want := []string{"alpha bravo", "charlie", "delta"}
	if len(got) != len(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// Scenario: Todo texto que entra se queda en una sola línea, sin reordenarlo.
func TestToastManager_WrapTextKeepsShortTextIntact(t *testing.T) {
	if got := wrapText("alpha bravo", 40); len(got) != 1 || got[0] != "alpha bravo" {
		t.Errorf("short text = %q, want one unchanged line", got)
	}
}

// Scenario: Un ancho nulo o negativo no recorta: devuelve el texto entero.
func TestToastManager_WrapTextWithNoBudgetReturnsWholeText(t *testing.T) {
	for _, budget := range []int{0, -1} {
		if got := wrapText("alpha bravo", budget); len(got) != 1 || got[0] != "alpha bravo" {
			t.Errorf("budget %d = %q, want the whole text on one line", budget, got)
		}
	}
}

// Scenario: Texto solo con espacios no se pierde al partir. Fields() yields
// nothing, so the empty result must fall back to the original text.
func TestToastManager_WrapTextKeepsWhitespaceOnlyText(t *testing.T) {
	if got := wrapText("     ", 4); len(got) != 1 || got[0] != "     " {
		t.Errorf("whitespace-only text = %q, want it preserved on one line", got)
	}
}

// Scenario: Una palabra más larga que el ancho no se parte a la mitad: se deja
// entera en su línea, porque partirla require cortar caracteres.
func TestToastManager_WrapTextNeverSplitsAWord(t *testing.T) {
	got := wrapText("supercalifragilistic word", 5)
	for _, l := range got {
		if l == "supercali" || l == "fragilistic" {
			t.Errorf("word was split mid-token: %q", got)
		}
	}
	if !strings.Contains(strings.Join(got, " "), "supercalifragilistic") {
		t.Errorf("the long word is not intact: %q", got)
	}
}

// Scenario: Un toast largo produce varias líneas, y solo la primera lleva icono.
func TestToastManager_LongToastWrapsAndIndentsContinuations(t *testing.T) {
	m := toastMgr()
	m.Show(strings.Repeat("word ", 30), ToastInfo)

	lines := m.ViewLines()
	if len(lines) < 2 {
		t.Fatalf("a long toast did not wrap: %q", lines)
	}
	plain := make([]string, len(lines))
	for i, l := range lines {
		plain[i] = ansi.Strip(l)
	}
	if !strings.HasPrefix(plain[0], "ℹ ") {
		t.Errorf("first line = %q, want the icon prefix", plain[0])
	}
	for i, l := range plain[1:] {
		if !strings.HasPrefix(l, "  ") {
			t.Errorf("continuation line %d = %q, want a two-space indent under the icon", i, l)
		}
	}
}

// Scenario: El borde usa el ancho calculado para ese mensaje, no uno fijo.
func TestToastManager_RenderedToastsUseCalculatedWidth(t *testing.T) {
	m := toastMgr()
	m.Show("hi", ToastInfo) // clamps up to toastMinWidth

	rendered := m.RenderedToasts()
	if len(rendered) != 1 {
		t.Fatalf("got %d rendered toasts, want 1", len(rendered))
	}
	var widest int
	for _, l := range strings.Split(ansi.Strip(rendered[0]), "\n") {
		if n := len([]rune(l)); n > widest {
			widest = n
		}
	}
	// lipgloss Width() is the total block width, borders included.
	if widest != toastMinWidth {
		t.Errorf("rendered toast is %d wide, want the calculated %d", widest, toastMinWidth)
	}
}

// Scenario: El ancho configurado no altera el ancho del toast: el toast se
// dimensiona por su mensaje, no por el de la ventana.
func TestToastManager_SetWidthDoesNotResizeToasts(t *testing.T) {
	m := toastMgr()
	m.Show("hi", ToastInfo)
	before := m.calculateWidth("hi")

	m.SetWidth(500)
	if got := m.calculateWidth("hi"); got != before {
		t.Errorf("calculateWidth changed from %d to %d after SetWidth", before, got)
	}
}
