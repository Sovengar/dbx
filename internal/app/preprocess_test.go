package app

import (
	"strings"
	"testing"
)

// hasPrefixFold reports whether s starts with prefix, ignoring ASCII case.
func hasPrefixFold(s, prefix string) bool {
	return strings.HasPrefix(strings.ToUpper(s), strings.ToUpper(prefix))
}

// contains reports whether s contains sub, ignoring ASCII case.
func contains(s, sub string) bool {
	return strings.Contains(strings.ToUpper(s), strings.ToUpper(sub))
}

// stringsLastIndex returns the byte index of the last occurrence of sub, or -1.
func stringsLastIndex(s, sub string) int { return strings.LastIndex(s, sub) }

// Scenario: Una sentencia que ya empieza por una palabra clave se devuelve sin
// tocar. This is the fast path: the whole list of leading keywords is what keeps
// a normal query from being rewritten.
func TestPreprocessSQL_LeadingKeywordIsUntouched(t *testing.T) {
	for _, kw := range []string{
		"SELECT", "WITH", "INSERT", "UPDATE", "DELETE", "EXPLAIN",
		"ALTER", "CREATE", "DROP", "GRANT", "REVOKE",
	} {
		in := kw + " * FROM t"
		if got := preprocessSQL(in); got != in {
			t.Errorf("preprocessSQL(%q) = %q, want it unchanged", in, got)
		}
	}
}

// Scenario: El reconocimiento de la palabra clave ignora mayúsculas y espacios
// alrededor, sin alterar el texto que se devuelve.
func TestPreprocessSQL_KeywordMatchIsCaseAndSpaceInsensitive(t *testing.T) {
	for _, in := range []string{
		"  select * from t  ", // leading spaces, lowercase
		"\n\tSELECT * FROM t", // leading newline and tab
		"select * from t",     // no trailing space at all
		"SeLeCt * FrOm t",     // mixed case
	} {
		if got := preprocessSQL(in); got != in {
			t.Errorf("preprocessSQL(%q) = %q, want it unchanged", in, got)
		}
	}
}

// Scenario: Una sentencia que empieza por FROM recibe un SELECT delante.
func TestPreprocessSQL_FromGetsASelect(t *testing.T) {
	got := preprocessSQL("FROM t")
	if want := "SELECT * FROM t"; got != want {
		t.Errorf("preprocessSQL(%q) = %q, want %q", "FROM t", got, want)
	}

	// The keyword is matched case-insensitively, but the payload keeps its own
	// case: only the leading and trailing whitespace is trimmed.
	if got := preprocessSQL("  from t  "); got != "SELECT * from t" {
		t.Errorf("preprocessSQL(%q) = %q, want %q", "  from t  ", got, "SELECT * from t")
	}
}

// Scenario: Una sentencia con varios SELECT de nivel superior pone el último
// primero y deja el resto detrás. The reordering is the whole point: the editor's
// result grid shows the first SELECT, so it has to be the one the user cares about.
func TestPreprocessSQL_LastTopLevelSelectComesFirst(t *testing.T) {
	got := preprocessSQL("a = 1; SELECT b; c = 2; SELECT d")
	if !hasPrefixFold(got, "SELECT d") {
		t.Errorf("preprocessSQL = %q, want it to start with the last top-level SELECT", got)
	}
	if !contains(got, "SELECT b") {
		t.Errorf("preprocessSQL = %q, dropped the earlier SELECT", got)
	}
	// The remainder keeps the non-SELECT parts.
	if !contains(got, "a = 1") || !contains(got, "c = 2") {
		t.Errorf("preprocessSQL = %q, dropped part of the statement", got)
	}
}

// Scenario: Un SELECT dentro de paréntesis no cuenta como de nivel superior, así
// que no se reordena. This is the subquery guard: hoisting an inner SELECT would
// change what the query means.
func TestPreprocessSQL_SubquerySelectIsNotHoisted(t *testing.T) {
	// A single subquery inside parens, with no top-level SELECT before it.
	for _, in := range []string{
		"WHERE id IN (SELECT id FROM t)",
		"x = (SELECT max(y) FROM t)",
	} {
		got := preprocessSQL(in)
		if hasPrefixFold(got, "SELECT") {
			t.Errorf("preprocessSQL(%q) = %q, want a subquery SELECT left in place", in, got)
		}
	}
}

// Scenario: Un SELECT de nivel superior después de un subquery sí se destaca, y
// el subquery no se confunde con él.
func TestPreprocessSQL_TopLevelSelectAfterSubqueryIsHoisted(t *testing.T) {
	got := preprocessSQL("WHERE id IN (SELECT id FROM a) SELECT id FROM b")
	if !hasPrefixFold(got, "SELECT id FROM b") {
		t.Errorf("preprocessSQL = %q, want the top-level SELECT hoisted to the front", got)
	}
	if !contains(got, "SELECT id FROM a") {
		t.Errorf("preprocessSQL = %q, dropped the subquery", got)
	}
}

// Scenario: La posición del SELECT se busca por palabra completa, no por
// subcadena. "SELECTED" and "MYSELECT" are identifiers, not keywords.
func TestFindLastTopLevelSELECT_RequiresAWholeWord(t *testing.T) {
	for _, in := range []string{
		"SELECTED FROM t",
		"MYSELECT FROM t",
		"xSELECT FROM t",
		"DESELECT FROM t",
	} {
		if got := findLastTopLevelSELECT(in); got != -1 {
			t.Errorf("findLastTopLevelSELECT(%q) = %d, want -1 (not a whole-word SELECT)", in, got)
		}
	}
}

// Scenario: Una sentencia sin un SELECT completo devuelve -1.
func TestFindLastTopLevelSELECT_NoSelectIsMinusOne(t *testing.T) {
	for _, in := range []string{"", "FROM t", "UPDATE t SET a = 1"} {
		if got := findLastTopLevelSELECT(in); got != -1 {
			t.Errorf("findLastTopLevelSELECT(%q) = %d, want -1", in, got)
		}
	}
}

// Scenario: Un SELECT de 6 caracteres al final de la cadena se encuentra, y el
// límite del bucle lo alcanza.
//
// The loop bound is `i < len(s)-5`, so the last position the scan can start at
// is len(s)-6, which is exactly where a trailing 6-character keyword begins.
// A trailing "select" with nothing after it is found at index 0.
func TestFindLastTopLevelSELECT_TrailingSelectAtTheLoopBound(t *testing.T) {
	// Exactly at the bound: 6 characters, nothing after.
	if got := findLastTopLevelSELECT("select"); got != 0 {
		t.Errorf("findLastTopLevelSELECT(%q) = %d, want 0 (the bound reaches the last start)", "select", got)
	}
	// A trailing SELECT with text before it, so idx > 0 and preprocessSQL would
	// actually reorder on it.
	if got := findLastTopLevelSELECT("x select"); got != 2 {
		t.Errorf("findLastTopLevelSELECT(%q) = %d, want 2", "x select", got)
	}
	// A trailing identifier byte after the keyword disqualifies it, and that is
	// the whole-word rule: "select1" is an identifier, not the keyword.
	if got := findLastTopLevelSELECT("select1"); got != -1 {
		t.Errorf("findLastTopLevelSELECT(%q) = %d, want -1 (a trailing identifier byte disqualifies the word)", "select1", got)
	}
	// Dropping that byte is what makes it match, which proves the disqualifier is
	// the trailing byte and not the loop bound.
	if got := findLastTopLevelSELECT("select 1"); got != 0 {
		t.Errorf("findLastTopLevelSELECT(%q) = %d, want 0 (a space does not disqualify the word)", "select 1", got)
	}
}

// Scenario: La búsqueda devuelve la ÚLTIMA aparición, no la primera.
func TestFindLastTopLevelSELECT_ReturnsTheLastOccurrence(t *testing.T) {
	in := "SELECT a FROM t; SELECT b FROM t"
	got := findLastTopLevelSELECT(in)
	if got != stringsLastIndex(in, "SELECT b") {
		t.Errorf("findLastTopLevelSELECT(%q) = %d, want the position of the last SELECT (%d)",
			in, got, stringsLastIndex(in, "SELECT b"))
	}
}

// Scenario: isASCIILetter cubre letras, dígitos y el subrayado, que es lo que
// separa un identificador de una palabra clave.
func TestIsASCIILetter(t *testing.T) {
	for _, b := range []byte{'a', 'z', 'A', 'Z', '0', '9', '_'} {
		if !isASCIILetter(b) {
			t.Errorf("isASCIILetter(%q) = false, want true (identifier byte)", b)
		}
	}
	for _, b := range []byte{' ', '(', ')', ',', ';', '\'', '-', '.', '\n'} {
		if isASCIILetter(b) {
			t.Errorf("isASCIILetter(%q) = true, want false (separator byte)", b)
		}
	}
}

// Scenario: Una sentencia sin palabra clave conocida y sin SELECT vuelve
// intacta, sin inventarle un SELECT.
func TestPreprocessSQL_UnknownStatementIsUntouched(t *testing.T) {
	for _, in := range []string{
		"SET search_path TO public",
		"VACUUM ANALYZE",
		"COPY t FROM stdin",
	} {
		if got := preprocessSQL(in); got != in {
			t.Errorf("preprocessSQL(%q) = %q, want it unchanged", in, got)
		}
	}
}

// Scenario: Un editor VACIO no produce una sentencia.
//
// "" and "   " used to be in the list above, asserting they came back unchanged. They came
// back UNCHANGED, which is not the same as coming back as something to run: preprocessSQL
// returned the raw whitespace, execute_query's `sql != ""` check passed, and pressing run
// on an empty editor sent whitespace to PostgreSQL. The answer came back as "empty query
// string", which is a confusing way to learn you pressed the wrong key.
//
// Empty is a different answer from "unknown": a SET statement is a statement this function
// does not recognise, and passing it through untouched is the correct answer. Whitespace is
// not a statement at all.
func TestPreprocessSQL_AnEmptyEditorHasNoStatement(t *testing.T) {
	for _, in := range []string{"", "   ", "\n", "\t \n  ", " \r\n "} {
		if got := preprocessSQL(in); got != "" {
			t.Errorf("preprocessSQL(%q) = %q, want no statement at all", in, got)
		}
	}

	t.Run("a statement that is only a semicolon is not empty", func(t *testing.T) {
		// It is not a runnable statement either, but it is SOMETHING the user typed and
		// the database is the right place to complain about it. Distinguishing this from
		// whitespace would need a parser; the whitespace case is the one that matters
		// because it is what an untouched empty editor holds.
		if got := preprocessSQL(";"); got == "" {
			t.Error("preprocessSQL(\";\") is empty; a semicolon is something the user typed")
		}
	})
}
