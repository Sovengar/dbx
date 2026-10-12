package editor

// CONDITIONALS_BOUNDARY sweep de autocomplete.go (grupo editor).
//
// Cada test abarca un operador de comparación whose solo punto observable es
// la igualdad de los dos operandos (x < n -> x <= n y familia). Hay dos
// familias:
//
//   - KILLABLE: el mutante cambia un resultado observable (se ofrece o se
//     oculta un item, el popup muestra el contador de scroll, la selección
//     se sale de rango). El test es rojo bajo el mutante.
//   - EQUIVALENTE: en la igualdad ambas ramas asignan el mismo valor o son
//     no-ops (strings.Repeat(" ", 0), ansi.Truncate a la anchura exacta,
//     rebanar un slice ya cortado, un ";" que ningún lector distingue).
//     El test construye la igualdad y afirma el comportamiento original:
//     seguirá verde bajo el mutante, que es exactamente la prueba.

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/buble/dbx/internal/theme"
)

// ---------------------------------------------------------------------------
// KILLABLE
// ---------------------------------------------------------------------------

// Scenario: Una selección igual al largo de la lista no sale de rango.
//
// Kills CONDITIONALS_BOUNDARY autocomplete.go:201 (`a.selected >=
// len(a.filtered)` -> `>`): iguales, el mutante no devuelve nil y
// `a.filtered[a.selected]` hace pánico. Visible() es true (hay items), así que
// la guarda es lo único que separa la selección final del pánico.
func TestEditorBoundarySelectedItemAtTheFilteredEndReturnsNil(t *testing.T) {
	a := NewAutocompleteState()
	a.visible = true
	a.filtered = []CompletionItem{
		{Name: "alpha", Kind: CompletionKeyword},
		{Name: "beta", Kind: CompletionKeyword},
	}
	a.selected = 2 // equality: selected == len(filtered)

	if item := a.SelectedItem(); item != nil {
		t.Errorf("SelectedItem() = %v, want nil at selected == len(filtered)", item.Name)
	}
}

// Scenario: Un calificador de cuatro tokens sigue resolviendo por esquema aunque
// el catálogo no tenga la tabla.
//
// Kills CONDITIONALS_BOUNDARY autocomplete.go:569 (`len(passed) >= 4` -> `>`):
// con exactamente cuatro tokens el mutante salta la rama `schema.table.` y
// trata la tabla como no calificada, así que cols de OTRO esquema (otro
// catálogo) aparecen en el popup. El control del mismo fixture con la tabla
// real demuestra que el fixture sí construye el estado.
func TestEditorBoundaryAnUnknownQualifierAtFourPassedTokensIsSchemaScoped(t *testing.T) {
	a := newTestAutocomplete()
	// A column of a table "b" that lives in a schema OTHER than the qualifier
	// names: offered only if the schema is part of the resolution.
	a.all = append(a.all, CompletionItem{
		Name: "bcol", Kind: CompletionColumn, Schema: "other", Table: "b", Detail: "text",
	})

	line := "a . b ." // tokens: a . b .  -> exactly 4 passed, pattern [w . w .]
	a.UpdateContext(a.detectContext(line, len(line)))

	if names := suggestionNames(a); hasName(names, "bcol") {
		t.Fatalf("a column of another schema leaked into the qualified context: %v", names)
	}

	// Control: the same shape with a real schema.table qualifier offers its
	// columns, so the fixture really builds the 4-token state.
	ctrl := newTestAutocomplete()
	ctrl.UpdateContext(ctrl.detectContext("public . users .", len("public . users .")))
	if names := suggestionNames(ctrl); !hasName(names, "id") || !hasName(names, "name") {
		t.Fatalf("control: public.users. does not offer its columns: %v", names)
	}
}

// Scenario: El cursor sobre el primer carácter de un string sin cerrar no está
// dentro del literal.
//
// Kills CONDITIONALS_BOUNDARY autocomplete.go:825 (`token.start >= col` ->
// `>`): en la igualdad el mutante sigue evaluando el token y el brazo de
// tokenString responde true (`col < token.end`), así que el contexto pasa a
// CompletionEmpty y el popup desaparece.
func TestEditorBoundaryCursorAtTheStartOfAnUnterminatedStringIsOutside(t *testing.T) {
	a := newTestAutocomplete()
	line := "'abc"
	a.UpdateContext(a.detectContext(line, 0)) // token.start(0) == col(0)

	if !a.Visible() {
		t.Fatal("the cursor on the first byte of an unterminated string must not be inside it: no popup")
	}
}

// Scenario: El cursor justo tras el cierre de un comentario de bloque no está
// dentro del comentario.
//
// Kills CONDITIONALS_BOUNDARY autocomplete.go:842 (`col < token.end` ->
// `<=`): en la igualdad el mutante devuelve true incondicionalmente y el
// contexto pasa a CompletionEmpty. La línea ofrece el token que sigue al
// comentario, así que el popup abierto es el observable.
func TestEditorBoundaryCursorRightAfterAClosedBlockCommentIsOutside(t *testing.T) {
	a := newTestAutocomplete()
	line := "/* c */ u"
	a.UpdateContext(a.detectContext(line, len("/* c */"))) // token.end(7) == col(7)

	if !a.Visible() {
		t.Fatal("the cursor right after a closed block comment must not be inside it: no popup")
	}
}

// Scenario: Una lista de exactamente maxItems no muestra el contador de scroll.
//
// Kills CONDITIONALS_BOUNDARY autocomplete.go:967 (`len(a.filtered) >
// a.maxItems` -> `>=`): en la igualdad el mutante renderiza "  1/15" pese a
// que la lista cabe entera en la ventana. Los nombres no llevan "/", así que
// una "/" en el render sólo puede ser el contador.
func TestEditorBoundaryExactlyMaxItemsShowsNoScrollCounter(t *testing.T) {
	a := longSuggestions(15) // equality: len(filtered) == maxItems
	out := stripEscape(a.Render(theme.Resolve("dark").Styles(), 200))

	if strings.Contains(out, "/") {
		t.Errorf("a list of exactly maxItems shows scroll info:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// EQUIVALENTE — el test construye la igualdad y afirma el comportamiento
// original; seguirá verde bajo el mutante (esa es la prueba).
// ---------------------------------------------------------------------------

// Scenario: Un filtro vacío deja el popup invisible aunque el flag interno
// quede levantado.
//
// EQUIVALENTE autocomplete.go:249 (`len(a.filtered) > 0` -> `>=`): en la
// igualdad el flag interno es true, pero Visible() lo enmascara con su propia
// guarda (`a.visible && len(a.filtered) > 0`) y Render() también consulta
// Visible(), así que no hay ningún observador que distinta ambos estados.
func TestEditorBoundaryEmptyFilteredStaysInvisible(t *testing.T) {
	a := NewAutocompleteState()
	a.UpdateContext(completionContext{kind: CompletionEmpty})

	if a.Visible() {
		t.Fatal("an empty filtered list keeps the popup hidden")
	}
}

// Scenario: Una columna negativa se clavija en cero y no rompe el análisis.
//
// EQUIVALENTE autocomplete.go:458 (`col < 0` -> `<=`): en la igualdad (col 0)
// ambos dejan col = 0, así que el contexto es idéntico al del clamp normal.
func TestEditorBoundaryNegativeDetectColClampsToZero(t *testing.T) {
	a := newTestAutocomplete()
	line := "SELECT * FROM u"

	neg := a.detectContext(line, -1)
	zero := a.detectContext(line, 0)

	if neg.kind != zero.kind || neg.tokenStart != zero.tokenStart ||
		neg.tokenEnd != zero.tokenEnd || neg.prefix != zero.prefix {
		t.Errorf("detectContext(col=-1) = %+v, want the same as col=0: %+v", neg, zero)
	}
}

// Scenario: Una columna más allá del final se clavija en el final de la línea.
//
// EQUIVALENTE autocomplete.go:461 (`col > len(line)` -> `>=`): en la igualdad
// (col == len(line)) ambos dejan col = len(line), contexto idéntico.
func TestEditorBoundaryColPastTheEndClampsToLen(t *testing.T) {
	a := newTestAutocomplete()
	line := "SELECT * FROM u"

	over := a.detectContext(line, len(line)+50)
	end := a.detectContext(line, len(line))

	if over.kind != end.kind || over.tokenStart != end.tokenStart ||
		over.tokenEnd != end.tokenEnd || over.prefix != end.prefix {
		t.Errorf("detectContext(col=past-end) = %+v, want the same as col=len: %+v", over, end)
	}
}

// Scenario: Un punto y coma como primer token no cambia el análisis.
//
// EQUIVALENTE autocomplete.go:485 (`idx >= 0` -> `>`): en la igualdad (idx 0)
// el mutante no trunca, pero ";" nunca es palabra, nunca encaja en una frase de
// cláusula y nunca aparece en una referencia de tabla: ningún lector de
// passed/refs lo distingue. Ambos casos deben coincidir.
func TestEditorBoundaryASemicolonFirstTokenIsTransparent(t *testing.T) {
	a := newTestAutocomplete()

	// A finished statement followed by a live one: the leading ";" makes the
	// semicolon index 0, the boundary of the >= 0 guard.
	live := a.detectContext("; SELECT * FROM u", len("; SELECT * FROM u"))
	if live.kind != CompletionTable {
		t.Errorf("kind = %d, want CompletionTable (the semicolon must not truncate the live statement)", live.kind)
	}
	if a.UpdateContext(live); !a.Visible() {
		t.Error("no popup opened after the leading semicolon")
	}
	if names := suggestionNames(a); !hasName(names, "users") {
		t.Errorf("users is missing after the leading semicolon: %v", names)
	}

	// The lone semicolon: keyword context either way.
	a.UpdateContext(a.detectContext(";", 1))
	if names := suggestionNames(a); len(names) == 0 {
		t.Error("a lone semicolon offers the keyword vocabulary")
	}
}

// Scenario: Dos etiquetas de la misma longitud máxima calculan ese máximo.
//
// EQUIVALENTE autocomplete.go:871 (`l > maxNameLen` -> `>=`): en la igualdad
// (l == maxNameLen) ambos asignan el mismo valor al máximo, así que el ancho
// del popup es idéntico. Las etiquetas de 15 hacen popupW = 15 + 25 = 40 —
// justo el mínimo, sin clamp — y el máximo es 15 sea cual sea la variante.
func TestEditorBoundaryEqualLongestLabelsSetMaxNameWidth(t *testing.T) {
	a := suggestionsOf(15, 15) // both labels are the maximum, the equality of line 871
	styles := theme.Resolve("dark").Styles()
	out := a.Render(styles, 200)
	if got := len(strings.Split(out, "\n")); got != 4 {
		t.Errorf("the popup has %d lines, want 4 (two rows plus the border):\n%s", got, out)
	}
	for i, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w != 40 {
			t.Errorf("line %d is %d columns wide, want 40:\n%s", i, w, out)
		}
	}
}

// Scenario: Un popup de exactamente el mínimo (40) no se re-clava al mínimo.
//
// EQUIVALENTE autocomplete.go:877 (`popupW < 40` -> `<=`): en la igualdad
// (popupW == 40) ambos dejan popupW = 40.
func TestEditorBoundaryPopupAtFortyStaysAtForty(t *testing.T) {
	a := suggestionsOf(15) // popupW == 15 + 25 == 40, the equality
	styles := theme.Resolve("dark").Styles()
	out := a.Render(styles, 200)
	for i, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w != 40 {
			t.Errorf("line %d is %d columns wide, want 40:\n%s", i, w, out)
		}
	}
}

// Scenario: Un popup exactamente en el tope de ancho no se re-clava al tope.
//
// EQUIVALENTE autocomplete.go:880 (`popupW > width-4` -> `>=`): en la
// igualdad (popupW == width-4) ambos dejan popupW = width-4.
func TestEditorBoundaryPopupAtTheWidthCapIsUnclamped(t *testing.T) {
	a := suggestionsOf(40) // popupW == 65
	styles := theme.Resolve("dark").Styles()
	out := a.Render(styles, 69) // width-4 == 65, the equality
	for i, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w != 65 {
			t.Errorf("line %d is %d columns wide, want 65:\n%s", i, w, out)
		}
	}
}

// Scenario: Una lista de exactamente maxItems cabe entera en la ventana.
//
// EQUIVALENTE autocomplete.go:887 (`end > a.maxItems` -> `>=`): en la
// igualdad (end == maxItems) ambos dejan end = maxItems, así que la ventana
// sigue mostrando maxItems filas.
func TestEditorBoundaryWindowOfExactlyMaxItemsFits(t *testing.T) {
	a := longSuggestions(15) // end == 15 == maxItems, the equality
	styles := theme.Resolve("dark").Styles()
	lines := strings.Split(a.Render(styles, 200), "\n")
	if want := a.maxItems + 2; len(lines) != want {
		t.Errorf("the popup has %d lines, want %d (maxItems rows plus the border):\n%s", len(lines), want, lines)
	}
}

// Scenario: Una selección más allá del final de una lista corta muestra la
// lista entera.
//
// EQUIVALENTE autocomplete.go:897 (`end > len(a.filtered)` -> `>=`) y :900
// (`start < 0` -> `<=`): en la igualdad (end == len) ambos re-clavan end =
// len y start = len - maxItems, que el clamp de start deja en 0 — la misma
// ventana que el original.
func TestEditorBoundarySelectionPastTheEndClampsTheWindow(t *testing.T) {
	a := suggestionsOf(2, 4, 6)
	a.selected = 20 // as if the selection was reset on a shorter list
	styles := theme.Resolve("dark").Styles()
	out := stripEscape(a.Render(styles, 200))
	if !strings.Contains(out, "nn") {
		t.Errorf("the rows vanished with the selection past the end:\n%s", out)
	}
}

// Scenario: El suelo de ancho de nombre (10) se aplica también en la igualdad.
//
// EQUIVALENTE autocomplete.go:911 (`nameMaxW < 10` -> `<=`): en la igualdad
// (nameMaxW == 10) ambos dejan nameMaxW = 10, y el nombre largo se trunca.
func TestEditorBoundaryNameFloorOfTenApplies(t *testing.T) {
	a := suggestionsOf(20)
	styles := theme.Resolve("dark").Styles()
	out := stripEscape(a.Render(styles, 29)) // popupW == 25, nameMaxW == 10, the equality
	if !strings.Contains(out, "..") {
		t.Errorf("the long name is not truncated to the floor of ten:\n%s", out)
	}
	if strings.Contains(out, strings.Repeat("n", 20)) {
		t.Errorf("the untruncated name survived:\n%s", out)
	}
}

// Scenario: Un nombre de exactamente el ancho máximo no se trunca ni se
// re-rellena.
//
// EQUIVALENTE autocomplete.go:915 (`nameW > nameMaxW` -> `>=`) y :920
// (`namePad > 0` -> `>=`): en la igualdad ambos son no-ops — ansi.Truncate a
// la anchura exacta devuelve el nombre y strings.Repeat(" ", 0) añade nada.
// La igualdad exacta: popupW se clava al tope de ancho (width 60 -> popupW
// 56), nameMaxW = 56 - 10 - 5 = 41 y la etiqueta mide 41.
func TestEditorBoundaryNameAtMaxWidthIsNotTruncatedNorPadded(t *testing.T) {
	a := suggestionsOf(41) // nameW == 41 == nameMaxW at width 60, the equality
	styles := theme.Resolve("dark").Styles()
	out := stripEscape(a.Render(styles, 60))
	name := strings.Repeat("n", 41)
	if strings.Contains(out, "..") {
		t.Errorf("a name at exactly nameMaxW was truncated:\n%s", out)
	}
	if !strings.Contains(out, name+"text") {
		t.Errorf("the name is not rendered flush against its kind label:\n%s", out)
	}
}

// Scenario: Un kind de exactamente el ancho de clase no se trunca ni se
// re-rellena.
//
// EQUIVALENTE autocomplete.go:925 (`kindWDisplay > kindW` -> `>=`) y :930
// (`kindPad > 0` -> `>=`): en la igualdad ambos son no-ops por el mismo
// motivo que el nombre.
func TestEditorBoundaryKindAtKindWidthIsNotTruncatedNorPadded(t *testing.T) {
	a := NewAutocompleteState()
	a.visible = true
	a.filtered = []CompletionItem{
		{Name: "c1", Kind: CompletionColumn, Detail: "1234567890"}, // Detail is 10 == kindW, the equality
	}
	out := stripEscape(a.Render(theme.Resolve("dark").Styles(), 200))
	if strings.Contains(out, "..") {
		t.Errorf("a kind at exactly kindW was truncated:\n%s", out)
	}
	if !strings.Contains(out, "1234567890") {
		t.Errorf("the full kind label is missing:\n%s", out)
	}
}
