package ui

import (
	"strings"
	"testing"
	"time"

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

// Scenario: El ancho de un mensaje ancho crece con los caracteres de doble celda,
// así que un texto CJK corto ocupa más que su equivalente ASCII.
func TestToastManager_WideRunesWidenTheToast(t *testing.T) {
	ascii := toastMgr().calculateWidth("abcd")
	cjk := toastMgr().calculateWidth("日本") // 4 cells
	if cjk != ascii {
		t.Errorf("CJK message width = %d, ASCII = %d; double-width runes must count as 2", cjk, ascii)
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
