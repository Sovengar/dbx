package editor

// CONDITIONALS_BOUNDARY sweep de sql.go (grupo editor).
//
// Cada test abarca un operador de comparación whose solo punto observable es
// la igualdad de los dos operandos (x < n -> x <= n y familia). Hay dos
// familias:
//
//   - KILLABLE: el mutante cambia un resultado observable (un clamp que deja
//     el cursor fuera de rango, un pánico por índice, una línea de más en el
//     render). El test es rojo bajo el mutante.
//   - EQUIVALENTE: en la igualdad ambas ramas asignan el mismo valor o son
//     no-ops (clamp al valor ya tenido, DecodeRuneInString("") que devuelve
//     size 0, rebanar un slice ya cortado). El test construye la igualdad y
//     afirma el comportamiento original: seguirá verde bajo el mutante, que es
//     exactamente la prueba.

import (
	"fmt"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// KILLABLE
// ---------------------------------------------------------------------------

// Scenario: Una fila igual al número de líneas se clavija en la última.
//
// Kills CONDITIONALS_BOUNDARY sql.go:141 (`row >= len(e.lines)` -> `>`): en
// la igualdad el mutante no clava y `e.lines[row]` del clamp de columna hace
// pánico. El cursor debe quedar en la última línea.
func TestEditorBoundarySetCursorPosRowAtLineCountClampsToLastLine(t *testing.T) {
	ed := NewSQLEditor(testStyles())
	ed.SetContent("SELECT 1\nSELECT 2") // 2 lines
	ed.SetCursorPos(2, 0)               // equality: row == len(lines)

	if ed.cursorRow != 1 {
		t.Errorf("cursorRow = %d, want %d (the last line)", ed.cursorRow, 1)
	}
}

// Scenario: El retroceso de rune sobre una línea que empieza con un byte de
// continuidad no sale del rango.
//
// Kills CONDITIONALS_BOUNDARY sql.go:356 (`for i > 0` -> `>=`): en la
// igualdad (i == 0) el mutante evalúa !utf8.RuneStart(s[0]) — falso solo para
// un byte de CONTINUACIÓN (0x80-0xBF); un byte líder como 0xc3 ya es rune
// start y por eso el mutante sobrevive con ese fixture — y devuelve -1,
// dejando el cursor en -1. El helper directo y el camino de integración
// (moveLeft) lo cubren ambos.
func TestEditorBoundaryPrevRuneStartAtZeroOnAContinuationByte(t *testing.T) {
	if got := prevRuneStart("\x80", 1); got != 0 {
		t.Errorf("prevRuneStart(\"\\x80\", 1) = %d, want 0", got)
	}

	ed := NewSQLEditor(testStyles())
	ed.SetContent("\x80") // a line holding a lone continuation byte
	ed.SetCursorPos(0, 1)
	ed.moveLeft()

	if ed.cursorCol != 0 {
		t.Errorf("cursorCol = %d after moveLeft, want 0", ed.cursorCol)
	}
}

// Scenario: El largo de rune más allá de la cadena es cero.
//
// EQUIVALENTE sql.go:457 (`i >= len(s)` -> `>`): en la igualdad (i == len(s))
// el mutante decodifica el string vacío — utf8.DecodeRuneInString("") devuelve
// size 0 — y `return size` da 0, el mismo valor que el early-return
// reemplazado. Es el propio comentario de la función el que documenta que el
// size 0 solo ocurre al final de la cadena.
func TestEditorBoundaryRuneLenAtPastTheEndIsZero(t *testing.T) {
	if got := runeLenAt("abc", 3); got != 0 {
		t.Errorf("runeLenAt(\"abc\", 3) = %d, want 0", got)
	}
}

// Scenario: Volver al historial tras drenarlo restaura la última entrada.
//
// EQUIVALENTE sql.go:558 (`e.historyIdx < len(e.history)` -> `<=`): la
// igualdad del guard (idx == len) es inalcanzable — historyPrev siempre
// decrementa (idx > 0 → idx--) antes de evaluarlo, así que idx entra al
// guard como 0 o len-1 en ambos casos. El estado idx == len solo lo produce
// la rama else de historyNext, y desde ahí historyPrev decrementa antes de
// la guarda. El observable es idéntico en ambos casos.
func TestEditorBoundaryHistoryPrevAfterDrainingTheHistory(t *testing.T) {
	ed := NewSQLEditor(testStyles())
	ed.PushHistory("SELECT 1")
	ed.PushHistory("SELECT 2")

	ed.historyNext() // idx 2 -> 1, lines "SELECT 2"
	ed.historyNext() // else-branch: idx = len(history), the drained state
	ed.historyPrev() // decrements to len-1 BEFORE the boundary guard

	if got := ed.Content(); got != "SELECT 2" {
		t.Errorf("Content() = %q, want %q", got, "SELECT 2")
	}
}

// Scenario: Un editor de ancho cero no renderiza.
//
// Kills CONDITIONALS_BOUNDARY sql.go:594 col 13 (`e.width <= 0` -> `<`): en
// la igualdad (width == 0) el mutante continúa y renderiza el contenido.
func TestEditorBoundaryViewWithZeroWidthIsEmpty(t *testing.T) {
	ed := NewSQLEditor(testStyles())
	ed.SetContent("SELECT 1")
	ed.SetWidth(0) // equality: width == 0
	ed.SetHeight(10)

	if got := ed.View(); got != "" {
		t.Errorf("View() = %q, want the empty string at width 0", got)
	}
}

// Scenario: Un editor de alto cero no renderiza.
//
// EQUIVALENTE sql.go:594 col 30 (`e.height <= 0` -> `<`): en la igualdad
// (height == 0) el mutante continúa, pero el truncate de abajo
// (`lines = lines[:e.height]`) corta el contenido a cero líneas y View
// devuelve la cadena vacía igual que el guard original. El guard es
// redundante con el clamp.
func TestEditorBoundaryViewWithZeroHeightIsEmpty(t *testing.T) {
	ed := NewSQLEditor(testStyles())
	ed.SetContent("SELECT 1")
	ed.SetWidth(50)
	ed.SetHeight(0) // equality: height == 0

	if got := ed.View(); got != "" {
		t.Errorf("View() = %q, want the empty string at height 0", got)
	}
}

// Scenario: El relleno del editor llega exactamente hasta el alto.
//
// EQUIVALENTE sql.go:624 (`len(lines) < e.height` -> `<=`): en la igualdad
// (len(lines) == height) el mutante añade una línea de relleno extra, pero el
// truncate inmediatamente debajo (`lines = lines[:e.height]`) la corta: View
// queda en exactamente height líneas bajo ambos casos.
func TestEditorBoundaryViewPadsExactlyToHeight(t *testing.T) {
	ed := NewSQLEditor(testStyles())
	ed.SetContent("SELECT 1") // 1 line
	ed.SetWidth(50)
	ed.SetHeight(2)

	if got := strings.Split(ed.View(), "\n"); len(got) != 2 {
		t.Errorf("View() has %d lines, want 2 (the content plus one pad):\n%q", len(got), ed.View())
	}
}

// ---------------------------------------------------------------------------
// EQUIVALENTE — el test construye la igualdad y afirma el comportamiento
// original; seguirá verde bajo el mutante (esa es la prueba).
// ---------------------------------------------------------------------------

// Scenario: Un prefijo mínimo negativo se clavija en cero.
//
// EQUIVALENTE sql.go:60 (`minPrefix < 0` -> `<=`): en la igualdad
// (minPrefix == 0) ambos dejan minPrefix = 0, y con cero ningún token se
// oculta por longitud.
func TestEditorBoundaryNegativeMinPrefixClampsToZero(t *testing.T) {
	ed := NewSQLEditor(testStyles())
	ed.SetSchema(testExport())
	ed.Focus()
	ed.SetAutocompleteConfig(true, -3)
	ed.SetContent("SELECT * FROM u")
	ed.SetCursorPos(0, len("SELECT * FROM u"))
	ed.updateAutocompleteAfterEdit()

	if !ed.AutocompleteVisible() {
		t.Fatal("a minPrefix clamped to 0 must not hide the popup")
	}
}

// Scenario: Una fila negativa se clavija en cero.
//
// EQUIVALENTE sql.go:138 (`row < 0` -> `<=`): en la igualdad (row == 0)
// ambos dejan row = 0.
func TestEditorBoundaryNegativeRowClampsToZero(t *testing.T) {
	ed := NewSQLEditor(testStyles())
	ed.SetContent("SELECT 1")
	ed.SetCursorPos(-2, 0) // equality: row == 0 after the clamp

	if ed.cursorRow != 0 {
		t.Errorf("cursorRow = %d, want 0", ed.cursorRow)
	}
}

// Scenario: Una columna negativa se clavija en cero.
//
// EQUIVALENTE sql.go:144 (`col < 0` -> `<=`): en la igualdad (col == 0)
// ambos dejan col = 0.
func TestEditorBoundaryNegativeColClampsToZero(t *testing.T) {
	ed := NewSQLEditor(testStyles())
	ed.SetContent("SELECT 1")
	ed.SetCursorPos(0, -2) // equality: col == 0 after the clamp

	if ed.cursorCol != 0 {
		t.Errorf("cursorCol = %d, want 0", ed.cursorCol)
	}
}

// Scenario: Una columna más allá del final se clavija en el final de la línea.
//
// EQUIVALENTE sql.go:147 (`col > len(e.lines[row])` -> `>=`): en la igualdad
// (col == len) ambos dejan col = len(line).
func TestEditorBoundaryColPastLineEndClampsToLen(t *testing.T) {
	ed := NewSQLEditor(testStyles())
	ed.SetContent("abc")
	ed.SetCursorPos(0, 99) // equality: col == len(line) after the clamp

	if ed.cursorCol != 3 {
		t.Errorf("cursorCol = %d, want 3 (the line length)", ed.cursorCol)
	}
}

// Scenario: Una columna en cero no escanea el límite de rune.
//
// EQUIVALENTE sql.go:154 (`col > 0` -> `>=`): en la igualdad (col == 0) el
// mutante entra en el guard pero `utf8.RuneStart(line[0])` es true en cualquier
// string bien formado, así que el bucle no se ejecuta y el cursor no se mueve.
func TestEditorBoundaryZeroColSkipsRuneSnapping(t *testing.T) {
	ed := NewSQLEditor(testStyles())
	ed.SetContent("abc")
	ed.SetCursorPos(0, 0) // equality: col == 0

	if ed.cursorCol != 0 {
		t.Errorf("cursorCol = %d, want 0", ed.cursorCol)
	}

	empty := NewSQLEditor(testStyles())
	empty.SetContent("")
	empty.SetCursorPos(0, 0)
	if empty.cursorCol != 0 {
		t.Errorf("cursorCol = %d on the empty line, want 0", empty.cursorCol)
	}
}

// Scenario: El fin del rune más allá de la cadena es el fin de la cadena.
//
// EQUIVALENTE sql.go:364 (`start >= len(s)` -> `>`): en la igualdad
// (start == len(s)) el mutante decodifica el string vacío, obtiene size 0 y
// devuelve start + 0 — el mismo valor.
func TestEditorBoundaryNextRuneEndAtEndOfString(t *testing.T) {
	if got := nextRuneEnd("abc", 3); got != 3 {
		t.Errorf("nextRuneEnd(\"abc\", 3) = %d, want 3", got)
	}
	if got := nextRuneEnd("", 0); got != 0 {
		t.Errorf("nextRuneEnd(\"\", 0) = %d, want 0", got)
	}
}

// Scenario: El clamp de columna en el final de la línea es un no-op.
//
// EQUIVALENTE sql.go:449 (`e.cursorCol > lastCol` -> `>=`): en la igualdad
// (cursorCol == lastCol) ambos dejan cursorCol = lastCol; el cursor no cruza
// a la siguiente línea.
func TestEditorBoundaryClampColAtLineEndIsANoOp(t *testing.T) {
	ed := NewSQLEditor(testStyles())
	ed.SetContent("ab\ncd")
	ed.SetCursorPos(1, 2) // equality: cursorCol == len("cd")
	ed.moveRight()        // no next line: the cursor stays

	if ed.cursorRow != 1 || ed.cursorCol != 2 {
		t.Errorf("cursor = (%d, %d), want (1, 2)", ed.cursorRow, ed.cursorCol)
	}
}

// Scenario: Un prefijo mínimo de cero nunca oculta el popup.
//
// EQUIVALENTE sql.go:485 (`e.autocompleteMinPrefix > 0` -> `>=`): en la
// igualdad (minPrefix == 0) el mutante entra en el guard, pero la segunda
// condición `len(ctx.tokenText) < 0` es falsa para cualquier token, así que
// no cancela nada.
func TestEditorBoundaryZeroMinPrefixNeverHides(t *testing.T) {
	ed := NewSQLEditor(testStyles())
	ed.SetSchema(testExport())
	ed.Focus()
	ed.SetAutocompleteConfig(true, 0) // equality: minPrefix == 0
	ed.SetContent("SELECT * FROM u")
	ed.SetCursorPos(0, len("SELECT * FROM u"))
	ed.updateAutocompleteAfterEdit()

	if !ed.AutocompleteVisible() {
		t.Fatal("a minPrefix of 0 must not hide the popup")
	}
}

// Scenario: Cien entradas de historial se conservan enteras.
//
// EQUIVALENTE sql.go:587 (`len(e.history) > 100` -> `>=`): en la igualdad
// (len == 100) el mutante rebanaría e.history[0:] — el mismo slice, mismos
// elementos. El estado observable es idéntico.
func TestEditorBoundaryHundredEntriesAreRetained(t *testing.T) {
	ed := NewSQLEditor(testStyles())
	for i := 0; i < 100; i++ {
		ed.PushHistory(fmt.Sprintf("SELECT %d", i))
	}

	if len(ed.history) != 100 {
		t.Fatalf("len(history) = %d, want 100", len(ed.history))
	}
	ed.historyPrev()
	if got := ed.Content(); got != "SELECT 99" {
		t.Errorf("Content() = %q, want %q", got, "SELECT 99")
	}
}

// Scenario: Un contenido de exactamente el alto no se trunca.
//
// EQUIVALENTE sql.go:629 (`len(lines) > e.height` -> `>=`): en la igualdad
// (len == height) el mutante rebanaría lines[:height] — el mismo slice.
func TestEditorBoundaryViewTruncatesExactlyToHeight(t *testing.T) {
	ed := NewSQLEditor(testStyles())
	ed.SetContent("a\nb\nc") // 3 lines
	ed.SetWidth(50)
	ed.SetHeight(3) // equality: len(lines) == height

	if got := strings.Split(ed.View(), "\n"); len(got) != 3 {
		t.Errorf("View() has %d lines, want 3:\n%q", len(got), ed.View())
	}
}
