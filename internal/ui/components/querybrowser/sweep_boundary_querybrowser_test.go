package querybrowser

// Los 18 supervivientes CONDITIONALS_BOUNDARY de querybrowser.go, uno por línea.
//
// El mutador cambia un operador de comparación en un límite: < por <=, > por >=.
// Solo es observable cuando un dato cae EXACTAMENTE en el límite. Cada test de
// aquí coloca el dato en el límite y afirma el comportamiento original:
//
//   - KILLED (192, 231, 372): el dato en el límite sigue el brazo que el
//     mutante cierra, y la afirmación separa ambos mundos.
//   - EQUIVALENTE: la asignación del otro brazo es un no-op (el valor ya es
//     el que se asigna), o la frontera exacta es inalcanzable con las
//     herramientas del widget (time.Since nunca devuelve el instante exacto;
//     el ancho de fila nunca llega a la frontera porque el prefijo fijo se lo
//     come), o el recorte del borde se traga la línea extra. La afirmación
//     sigue verde bajo el mutante: eso es exactamente lo que documenta la
//     equivalencia.

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/buble/dbx/internal/store"
	"github.com/buble/dbx/internal/theme"
	"github.com/charmbracelet/x/ansi"
)

// Scenario: enter sobre la PRIMERA fila la carga.
//
// KILLS 192 (`b.cursor >= 0` -> `b.cursor > 0`): con el cursor en 0 el mutante
// cierra el brazo de la selección — no carga nada y no emite QuerySelectedMsg,
// mientras que el original carga la consulta bajo el cursor y cierra el modal.
func TestQueryBrowserBoundaryEnterOnTheFirstEntrySelectsIt(t *testing.T) {
	b := newQB(t, "SELECT first", "SELECT second")
	if b.cursor != 0 {
		t.Fatalf("the fixture opened with the cursor at %d, want 0", b.cursor)
	}

	cmd, handled := press(t, b, "enter")
	if !handled {
		t.Fatal("enter was not reported as handled")
	}
	if cmd == nil {
		t.Fatal("enter on the first row produced no command: the cursor is at index 0 and the guard `cursor >= 0` has to let it through")
	}
	msg, ok := cmd().(QuerySelectedMsg)
	if !ok {
		t.Fatalf("enter produced a %T, want a QuerySelectedMsg", cmd())
	}
	if want := b.entries[0].SQL; msg.SQL != want {
		t.Errorf("enter loaded %q, want the first row's %q", msg.SQL, want)
	}
	if b.IsVisible() {
		t.Error("the browser stayed open after loading the first row's query")
	}
}

// Scenario: el cursor en el borde EXACTO de la ventana, hacia abajo.
//
// KILLS 231 (`b.cursor >= b.scroll+maxVisible` -> `b.cursor > b.scroll+maxVisible`):
// con el cursor exactamente en scroll+maxVisible el original abre un hueco de una
// fila (scroll = cursor - maxVisible + 1) y el mutante no mueve la ventana: la
// fila del cursor queda una posición por debajo del último hueco visible.
func TestQueryBrowserBoundaryEnsureVisibleCursorAtWindowEndScrolls(t *testing.T) {
	b := newQB(t, "SELECT 1") // 30 de alto: maxVisible = 20
	if got, want := b.maxVisibleEntries(), 20; got != want {
		t.Fatalf("the window shows %d entries, want %d — the fixture's size assumption is wrong", got, want)
	}
	b.cursor = 20
	b.scroll = 0

	b.ensureVisible()

	if b.scroll != 1 {
		t.Errorf("with the cursor at the window's bottom edge (20 = 0+20) the scroll is %d, want 1", b.scroll)
	}
}

// Scenario: un SQL de EXACTAMENTE el ancho disponible no se trunca.
//
// KILLS 372 (`len(runes) > sqlMaxW` -> `len(runes) >= sqlMaxW`): con exactamente
// sqlMaxW runas el original muestra la consulta completa y el mutante corta la
// última y pone una elipsis.
func TestQueryBrowserBoundaryTruncateOnlyPastTheSQLWidth(t *testing.T) {
	b := newQB(t, "SELECT 1")
	const maxW = 60
	sqlMaxW := maxW - 13                              // timestamp(10) + marca(2) + espacio(1)
	sql := "SELECT " + strings.Repeat("y", sqlMaxW-7) // exactamente sqlMaxW runas

	line := ansi.Strip(b.renderEntry(0, store.QueryEntry{SQL: sql, Timestamp: time.Now()}, maxW))
	if strings.Contains(line, "…") {
		t.Errorf("a query of exactly %d cells was truncated: %q", sqlMaxW, line)
	}
	if !strings.Contains(line, sql) {
		t.Errorf("the row does not carry the full %d-cell query: %q", sqlMaxW, line)
	}
}

// Scenario: refreshEntries con el cursor ya en cero.
//
// EQUIVALENTE 82 (`b.cursor < 0` -> `b.cursor <= 0`): con cursor == 0 el
// mutante entra en el brazo, pero la asignación `b.cursor = 0` es un no-op
// sobre un valor que ya es 0. Con cursor < 0 ambos brazos ponen 0; con cursor
// > 0 ninguno entra. No hay estado observable que los separe.
func TestQueryBrowserBoundaryRefreshEntriesCursorZeroStaysZero(t *testing.T) {
	b := newQB(t, "SELECT 1")
	b.cursor = 0 // el límite: aquí `cursor < 0` y `cursor <= 0` difieren

	b.refreshEntries()

	if b.cursor != 0 {
		t.Errorf("refreshEntries left the cursor at %d, want 0", b.cursor)
	}
}

// Scenario: una tecla con texto vacío dentro del modo filtro.
//
// EQUIVALENTE 139 (`len(msg.Text) > 0` -> `len(msg.Text) >= 0`): el mutante
// hace la condición siempre verdadera, pero el brazo que abre solo ejecuta
// `b.filter += ""` (un no-op sobre el filtro) y refreshEntries sobre un filtro
// que no ha cambiado; el retorno es (nil, true) en ambos caminos. El filtro,
// las entries y el mensaje son idénticos.
func TestQueryBrowserBoundaryFilterKeyWithEmptyTextTypesNothing(t *testing.T) {
	b := newQB(t, "SELECT 1")
	press(t, b, "/")

	cmd, handled := b.Update(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl}) // ctrl+a: Text vacío
	if !handled {
		t.Error("a key the filter mode does not name was not reported as handled")
	}
	if cmd != nil {
		t.Error("a key with empty text produced a command")
	}
	if b.filter != "" {
		t.Errorf("a key with empty text typed %q into the filter, want it untouched", b.filter)
	}
}

// Scenario: G sobre una lista de UNA entrada.
//
// EQUIVALENTE 185 (`b.cursor < 0` -> `b.cursor <= 0`): G deja el cursor en
// len(entries)-1 = 0; el mutante entra en el brazo y asigna 0 sobre un valor
// que ya es 0. Con cero entradas ambos brazos ponen 0 (cursor = -1); con más
// de una ninguno entra.
func TestQueryBrowserBoundaryGJumpOnASingleEntry(t *testing.T) {
	b := newQB(t, "SELECT 1")

	press(t, b, "G")

	if b.cursor != 0 {
		t.Errorf("G on a one-entry list left the cursor at %d, want 0", b.cursor)
	}
}

// Scenario: el cursor en el borde EXACTO de la ventana, hacia arriba.
//
// EQUIVALENTE 228 (`b.cursor < b.scroll` -> `b.cursor <= b.scroll`): con
// cursor == scroll el mutante entra en el brazo y asigna scroll = cursor sobre
// un valor que ya es ese. Con cursor < scroll ambos ponen scroll = cursor; con
// cursor > scroll ninguno entra.
func TestQueryBrowserBoundaryEnsureVisibleCursorAtScrollStays(t *testing.T) {
	b := newQB(t, "SELECT 1")
	b.cursor = 5
	b.scroll = 5 // el límite: cursor == scroll

	b.ensureVisible()

	if b.scroll != 5 {
		t.Errorf("ensureVisible moved the scroll to %d with the cursor at the window's top edge, want it to stay at 5", b.scroll)
	}
}

// Scenario: el suelo de la altura del modal, en el límite exacto.
//
// EQUIVALENTE 238 (`modalH < 10` -> `modalH <= 10`): con modalH == 10 el
// mutante entra en el brazo y asigna 10 sobre un valor que ya es 10. Con
// modalH < 10 ambos ponen 10; con modalH > 10 ninguno entra.
func TestQueryBrowserBoundaryMaxVisibleFloorAtTen(t *testing.T) {
	b := newQB(t, "SELECT 1")
	b.SetHeight(13) // 13*8/10 = 10: el límite del suelo

	if got, want := b.maxVisibleEntries(), 6; got != want {
		t.Errorf("maxVisibleEntries() = %d at modalH=10, want %d", got, want)
	}
}

// Scenario: el suelo del ancho del frame, en el límite exacto.
//
// EQUIVALENTE 256 (`modalW < 50` -> `modalW <= 50`): con modalW == 50 el
// mutante entra en el brazo y asigna 50 sobre un valor que ya es 50. Con
// modalW < 50 ambos ponen 50; con modalW > 50 ninguno entra.
func TestQueryBrowserBoundaryFrameWidthFloorAtFifty(t *testing.T) {
	b := newQB(t, "SELECT 1")
	b.SetWidth(67) // 67*3/4 = 50: el límite del suelo

	if got := ansi.StringWidth(strings.Split(b.View(), "\n")[0]); got != 50 {
		t.Errorf("the frame is %d cells wide at the floor, want 50", got)
	}
}

// Scenario: el techo del ancho del frame, en el límite exacto.
//
// EQUIVALENTE 259 (`modalW > 90` -> `modalW >= 90`): con modalW == 90 el
// mutante entra en el brazo y asigna 90 sobre un valor que ya es 90. Con
// modalW > 90 ambos ponen 90; con modalW < 90 ninguno entra.
func TestQueryBrowserBoundaryFrameWidthCeilingAtNinety(t *testing.T) {
	b := newQB(t, "SELECT 1")
	b.SetWidth(120) // 120*3/4 = 90: el límite del techo

	if got := ansi.StringWidth(strings.Split(b.View(), "\n")[0]); got != 90 {
		t.Errorf("the frame is %d cells wide at the ceiling, want 90", got)
	}
}

// Scenario: el suelo de la altura del frame, en el límite exacto.
//
// EQUIVALENTE 264 (`modalH < 10` -> `modalH <= 10`): con modalH == 10 el
// mutante entra en el brazo y asigna 10 sobre un valor que ya es 10. Con
// modalH < 10 ambos ponen 10; con modalH > 10 ninguno entra.
func TestQueryBrowserBoundaryFrameHeightFloorAtTen(t *testing.T) {
	b := newQB(t, "SELECT 1")
	b.SetHeight(13) // 13*8/10 = 10: el límite del suelo

	if got := len(strings.Split(b.View(), "\n")); got != 10 {
		t.Errorf("the frame is %d lines tall at the floor, want 10", got)
	}
}

// Scenario: el corte del rango de render, en el límite exacto.
//
// EQUIVALENTE 305 (`end > len(b.entries)` -> `end >= len(b.entries)`): con
// end == len(entries) el mutante entra en el brazo y asigna end = len sobre
// un valor que ya es len. Con end > len ambos lo clamplean; con end < len
// ninguno entra. El rango renderizado es idéntico.
func TestQueryBrowserBoundaryViewEndClampAtListLength(t *testing.T) {
	sqls := make([]string, 12)
	for i := range sqls {
		sqls[i] = fmt.Sprintf("SELECT b%02d", i)
	}
	b := New(theme.Resolve("dark").Styles(), newStore(t, sqls...))
	b.SetWidth(100)
	b.SetHeight(20) // modalH 16, maxVisible 12: end = 0+12 == len(entries), el límite
	b.Show()

	if got := strings.Count(b.View(), "SELECT b"); got != 12 {
		t.Errorf("the frame shows %d of the 12 rows, want all 12", got)
	}
}

// Scenario: el relleno del content, en el límite exacto.
//
// EQUIVALENTE 319 (`len(lines) < maxVisible+2` -> `len(lines) <= maxVisible+2`):
// el mutante añade UNA línea de relleno más, pero el content ya lleva el footer
// justo detrás, y bordered recorta el content a targetContent = modalH-2 =
// maxVisible+2 (bordered.go:96-102): la línea extra del mutante cae siempre en
// la región recortada, así que el frame es idéntico. La igualdad en el límite:
// con 20 entradas el content llega exactamente a maxVisible+2 y el footer
// queda fuera — con y sin la línea extra. Fail-fast: dos asserts secos.
func TestQueryBrowserBoundaryPadToFillStopsAtTheClipPoint(t *testing.T) {
	sqls := make([]string, 20)
	for i := range sqls {
		sqls[i] = fmt.Sprintf("SELECT a%02d", i)
	}
	b := newQB(t, sqls...) // 30 de alto: maxVisible = 20, content objetivo = 22

	lines := strings.Split(b.View(), "\n")

	if got, want := len(lines), 24; got != want {
		t.Errorf("the frame is %d lines tall, want %d (the border clips the content, footer included)", got, want)
	}
	if got := strings.Count(b.View(), "SELECT a"); got != 20 {
		t.Errorf("the frame shows %d of the 20 rows", got)
	}
	if strings.Contains(b.View(), "navigate") {
		t.Error("the frame shows the footer: the border has to clip it")
	}
}

// Scenario: el bucket de edad en el minuto, en ambos lados de la frontera.
//
// EQUIVALENTE 349 (`age < time.Minute` -> `age <= time.Minute`): la frontera
// exacta age == time.Minute es inalcanzable — time.Since(ts) con ts fijado en
// now-60s siempre devuelve 60s+δ (δ>0, el reloj avanza entre fijar ts y
// evaluar renderEntry), y con ts en el futuro age < 0 y ambos operadores toman
// el mismo brazo. Los dos lados medibles afirman el comportamiento original.
func TestQueryBrowserBoundaryAgeBucketsAtTheMinute(t *testing.T) {
	b := newQB(t, "SELECT 1")
	render := func(age time.Duration) string {
		return ansi.Strip(b.renderEntry(0, store.QueryEntry{SQL: "x", Timestamp: time.Now().Add(-age)}, 60))
	}
	if line := render(59 * time.Second); !strings.Contains(line, "just now") {
		t.Errorf("a 59-second-old query rendered %q, want 'just now'", line)
	}
	if line := render(61 * time.Second); !strings.Contains(line, "1m ago") {
		t.Errorf("a 61-second-old query rendered %q, want '1m ago'", line)
	}
}

// Scenario: el bucket de edad en la hora, en ambos lados de la frontera.
//
// EQUIVALENTE 351 (`age < time.Hour` -> `age <= time.Hour`): como en 349, la
// frontera exacta age == time.Hour es inalcanzable con time.Since (siempre
// 60m+δ, δ>0), y los dos lados medibles afirman el comportamiento original.
func TestQueryBrowserBoundaryAgeBucketsAtTheHour(t *testing.T) {
	b := newQB(t, "SELECT 1")
	render := func(age time.Duration) string {
		return ansi.Strip(b.renderEntry(0, store.QueryEntry{SQL: "x", Timestamp: time.Now().Add(-age)}, 60))
	}
	if line := render(59*time.Minute + 59*time.Second); !strings.Contains(line, "59m ago") {
		t.Errorf("a 59m59s-old query rendered %q, want '59m ago'", line)
	}
	if line := render(61 * time.Minute); !strings.Contains(line, "1h ago") {
		t.Errorf("a 61m-old query rendered %q, want '1h ago'", line)
	}
}

// Scenario: el bucket de edad en el día, en ambos lados de la frontera.
//
// EQUIVALENTE 353 (`age < 24*time.Hour` -> `age <= 24*time.Hour`): como en 349,
// la frontera exacta age == 24h es inalcanzable con time.Since (siempre 24h+δ,
// δ>0), y los dos lados medibles afirman el comportamiento original.
func TestQueryBrowserBoundaryAgeBucketsAtTheDay(t *testing.T) {
	b := newQB(t, "SELECT 1")
	render := func(age time.Duration) string {
		return ansi.Strip(b.renderEntry(0, store.QueryEntry{SQL: "x", Timestamp: time.Now().Add(-age)}, 60))
	}
	if line := render(23 * time.Hour); !strings.Contains(line, "23h ago") {
		t.Errorf("a 23h-old query rendered %q, want '23h ago'", line)
	}
	if line := render(25 * time.Hour); !strings.Contains(line, "1d ago") {
		t.Errorf("a 25h-old query rendered %q, want '1d ago'", line)
	}
}

// Scenario: el suelo del ancho del SQL, en el límite exacto.
//
// EQUIVALENTE 366 (`sqlMaxW < 10` -> `sqlMaxW <= 10`): con sqlMaxW == 10 el
// mutante entra en el brazo y asigna 10 sobre un valor que ya es 10. Con
// sqlMaxW < 10 ambos ponen 10; con sqlMaxW > 10 ninguno entra.
func TestQueryBrowserBoundarySQLWidthFloorAtTen(t *testing.T) {
	b := newQB(t, "SELECT 1")
	sql := "SELECT 1"                                                               // corta: cabe en el suelo de 10
	line := b.renderEntry(0, store.QueryEntry{SQL: sql, Timestamp: time.Now()}, 23) // sqlMaxW = 23-13 = 10, el límite
	if got := ansi.StringWidth(line); got != 23 {
		t.Errorf("the row is %d cells wide, want 23", got)
	}
	if !strings.Contains(ansi.Strip(line), sql) {
		t.Errorf("the row is %q, want it to carry %q", ansi.Strip(line), sql)
	}
}

// Scenario: la fila se rellena hasta el ancho de fila, sin truncarse.
//
// EQUIVALENTE 393 (`displayWidth > maxW` -> `displayWidth >= maxW`): la frontera
// displayWidth == maxW es inalcanzable — el ancho de la fila es 12 (ts+marca)
// + ancho del SQL ≤ 12 + sqlMaxW = maxW-1, así que displayWidth nunca pasa de
// maxW-1. En el máximo alcanzable (SQL de exactamente sqlMaxW runas) el
// original rellena con un espacio y el mutante toma el mismo brazo. La prueba
// además afirma que no hay elisión en la frontera alcanzable.
func TestQueryBrowserBoundaryRowPaddedToTheRowWidth(t *testing.T) {
	b := newQB(t, "SELECT 1")
	const maxW = 60
	sqlMaxW := maxW - 13
	sql := "SELECT " + strings.Repeat("y", sqlMaxW-7) // exactamente sqlMaxW runas: displayWidth = 59 = maxW-1

	line := b.renderEntry(0, store.QueryEntry{SQL: sql, Timestamp: time.Now()}, maxW)

	if got := ansi.StringWidth(line); got != maxW {
		t.Errorf("the row is %d cells wide, want %d", got, maxW)
	}
	if strings.Contains(ansi.Strip(line), "…") {
		t.Errorf("a row at the reachable boundary was truncated: %q", ansi.Strip(line))
	}
}
