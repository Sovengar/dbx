package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/jackc/pgx/v5"
)

// txnSpaces is a readability helper for a run of blanked bytes.
func txnSpaces(n int) string { return strings.Repeat(" ", n) }

// txnMaskAssert checks the three invariants maskNonCode must hold for one input:
// the output has the same byte length, every newline of the input survives at the
// same index, and the output is exactly want.
//
// Length preservation is what lets callers keep using byte offsets computed on
// the original string, and newline preservation is what keeps a masked line
// breakable into the same number of output lines.
func txnMaskAssert(t *testing.T, in, want string) {
	t.Helper()
	got := maskNonCode(in)
	if len(got) != len(in) {
		t.Fatalf("maskNonCode(%q) returned %d bytes, want %d (byte length must be preserved)", in, len(got), len(in))
	}
	if got != want {
		t.Errorf("maskNonCode(%q)\n got %q\nwant %q", in, got, want)
	}
	for i := 0; i < len(in); i++ {
		if in[i] == '\n' && got[i] != '\n' {
			t.Errorf("maskNonCode(%q) lost the newline at byte %d: %q", in, i, got)
		}
	}
}

// Scenario: Una sentencia sin literales, identificadores citados ni comentarios
// sale intacta.
func TestMaskNonCode_PlainSQLIsUntouched(t *testing.T) {
	for _, in := range []string{
		"",
		"SELECT 1",
		"SELECT id, name FROM users WHERE age > 30 ORDER BY name",
		"a-b+c*d/e%f",
		"()",
	} {
		txnMaskAssert(t, in, in)
	}
}

// Scenario: Un literal de cadena se tapa entero, con el mismo ancho en bytes.
func TestMaskNonCode_SingleQuotedLiteral(t *testing.T) {
	txnMaskAssert(t, "'ab'", txnSpaces(4))
	txnMaskAssert(t, "SELECT 'DELETE FROM users'", "SELECT "+txnSpaces(len("'DELETE FROM users'")))
	// An empty literal is still a literal.
	txnMaskAssert(t, "''", txnSpaces(2))
}

// Scenario: Una comilla duplicada dentro del literal NO lo cierra: es el escape
// de PostgreSQL para una comilla simple. This is the rule that distinguishes a
// real string terminator from a doubled quote, and getting it wrong would cut
// the literal in half and expose the rest as code.
func TestMaskNonCode_DoubledQuoteDoesNotCloseTheLiteral(t *testing.T) {
	txnMaskAssert(t, "'a''b'", txnSpaces(len("'a''b'")))
	// Three quotes in a row: the first pair escapes, the third closes.
	txnMaskAssert(t, "'a'''", txnSpaces(len("'a'''")))
	// The doubled quote hides a keyword from the scanner.
	txnMaskAssert(t, "SELECT 'x''DELETE FROM users'", "SELECT "+txnSpaces(len("'x''DELETE FROM users'")))
}

// Scenario: Un literal se cierra en la PRIMERA comilla que no viene duplicada, y
// lo que sigue vuelve a ser código.
//
// The lexer is a lexer, not a parser: `'ab'c'x'` is the literal 'ab', the
// identifier c, and the literal 'x'. That is what PostgreSQL itself tokenises,
// and it is why a keyword sitting after the real terminator stays visible.
func TestMaskNonCode_LiteralEndsAtTheFirstUndoubledQuote(t *testing.T) {
	// 'ab'  ->  4 blanks, c stays, 'x'  ->  3 blanks.
	txnMaskAssert(t, "'ab'c'x'", txnSpaces(4)+"c"+txnSpaces(3))
	txnMaskAssert(t, "E'ab'c'x'", txnSpaces(5)+"c"+txnSpaces(3))
}

// Scenario: Un literal sin cerrar se tapa hasta el final de la entrada. An
// unterminated literal has no terminator to find, so the scan must stop at the
// end of input rather than run past it.
func TestMaskNonCode_UnterminatedSingleQuotedLiteral(t *testing.T) {
	txnMaskAssert(t, "'ab", txnSpaces(3))
	txnMaskAssert(t, "SELECT 'ab", "SELECT "+txnSpaces(3))
	// Only a quote, and nothing else.
	txnMaskAssert(t, "'", txnSpaces(1))
}

// Scenario: Un E-string se tapa entero, y su barra invertida escapa la comilla
// siguiente. The backslash escape is the whole reason the E-prefix needs its own
// branch: a plain '...' would end at that quote.
func TestMaskNonCode_EStringHonoursBackslashEscapes(t *testing.T) {
	txnMaskAssert(t, `E'ab'`, txnSpaces(5))
	txnMaskAssert(t, `e'ab'`, txnSpaces(5))
	txnMaskAssert(t, `E'a\'b'`, txnSpaces(len(`E'a\'b'`)))
	// Two escapes in a row: the second backslash is consumed by the first, so
	// the quote after it closes the literal.
	txnMaskAssert(t, `E'a\\'`, txnSpaces(len(`E'a\\'`)))
	// A doubled quote inside an E-string. The E-string has its own copy of the
	// doubled-quote check, so the plain-string fixtures do not reach it.
	txnMaskAssert(t, `E'a''b'`, txnSpaces(len(`E'a''b'`)))
	// And a backslash escape immediately before a doubled quote. The escape
	// consumes the first quote, so the second one CLOSES the literal: what
	// follows is code again, and here it opens a second, unterminated literal
	// that runs to the end of input.
	txnMaskAssert(t, `E'a\''b'`, txnSpaces(6)+"b"+txnSpaces(1))
	// A doubled quote at the very end, with nothing after it.
	txnMaskAssert(t, `E'a''`, txnSpaces(len(`E'a''`)))
	// An E-string that closes, then real code follows.
	txnMaskAssert(t, `E'ab' DELETE`, txnSpaces(len(`E'ab'`))+" DELETE")
	// And one that runs to the end without closing.
	txnMaskAssert(t, `E'ab`, txnSpaces(len(`E'ab`)))
}

// Scenario: La E de un E-string solo cuenta si no forma parte de un
// identificador más largo.
//
// The rule is one identifier byte in front of the E. Without it, an identifier
// like foo$E'...' would be lexed as a string and its backslashes would stop
// being escapes, which is the wrong way round for a security check.
func TestMaskNonCode_EPrefixAfterAnIdentifierByteIsNotAnEString(t *testing.T) {
	// The E belongs to an identifier, so only the quoted part is a literal.
	txnMaskAssert(t, "xE'ab'", "xE"+txnSpaces(4))
	txnMaskAssert(t, "$E'ab'", "$E"+txnSpaces(4))
	txnMaskAssert(t, "\xc3\xa9E'ab'", "\xc3\xa9E"+txnSpaces(4))
	// At the very start there is no preceding byte, so it IS an E-string.
	txnMaskAssert(t, "E'ab'", txnSpaces(5))
	// And it is an E-string when it appears later in the statement, as long as
	// the byte in front of the E is not an identifier byte. That guard reads
	// sql[i-1], so a literal that does not start at index 0 exercises the other
	// half of it.
	txnMaskAssert(t, "a E'b'", "a "+txnSpaces(len("E'b'")))
	txnMaskAssert(t, "a E'b' c", "a "+txnSpaces(len("E'b'"))+" c")
	txnMaskAssert(t, "(E'b')", "("+txnSpaces(len("E'b'"))+")")
	// A '(' in front is not an identifier byte, so this is still an E-string.
	txnMaskAssert(t, "x(E'b')", "x("+txnSpaces(len("E'b'"))+")")
	// The E has to be followed by a quote; a bare E is just a letter.
	txnMaskAssert(t, "a Eb", "a Eb")
}

// Scenario: Un identificador citado se tapa entero, con las comillas duplicadas
// como escape. A quoted identifier is code for the purpose of column names, so
// it must not be allowed to hide a keyword either.
func TestMaskNonCode_QuotedIdentifier(t *testing.T) {
	txnMaskAssert(t, `"ab"`, txnSpaces(len(`"ab"`)))
	// A doubled quote inside a quoted identifier, which is the escape for a
	// literal double quote in the name.
	txnMaskAssert(t, `"a""b"x`, txnSpaces(len(`"a""b"`))+"x")
	txnMaskAssert(t, `"a""b"`, txnSpaces(len(`"a""b"`)))
	txnMaskAssert(t, `"drop table" x`, txnSpaces(len(`"drop table"`))+" x")
	// A quote that is never closed runs to the end.
	txnMaskAssert(t, `"ab`, txnSpaces(len(`"ab`)))
}

// Scenario: Un comentario de línea se tapa hasta el fin de línea, y el salto de
// línea sobrevive. The newline is load-bearing: dropping it would join the next
// line into the comment and blank real code.
func TestMaskNonCode_LineComment(t *testing.T) {
	txnMaskAssert(t, "-- a\nb", txnSpaces(len("-- a"))+"\nb")
	txnMaskAssert(t, "SELECT 1 -- note", "SELECT 1 "+txnSpaces(len("-- note")))
	// A comment that is the last thing in the input, with no trailing newline.
	txnMaskAssert(t, "SELECT 1 --", "SELECT 1 "+txnSpaces(len("--")))
	// Two dashes are not a comment on their own.
	txnMaskAssert(t, "a - b", "a - b")
	// A single dash followed by something else is not a comment either.
	txnMaskAssert(t, "a -b", "a -b")
}

// Scenario: Un comentario de bloque se tapa entero, y uno sin cerrar se lleva
// todo lo que queda.
func TestMaskNonCode_BlockComment(t *testing.T) {
	txnMaskAssert(t, "/* a */ b", txnSpaces(len("/* a */"))+" b")
	txnMaskAssert(t, "/**/b", txnSpaces(len("/**/"))+"b")
	// Unterminated: everything to the end of input is comment.
	txnMaskAssert(t, "x /*ab", "x "+txnSpaces(len("/*ab")))
	// Unterminated and ending in a star, the hardest case for the scan bound.
	txnMaskAssert(t, "/*ab*", txnSpaces(len("/*ab*")))
	// The terminator is star-slash, so a lone slash does not close anything and
	// the rest of the input is swallowed.
	txnMaskAssert(t, "/*a/ b", txnSpaces(len("/*a/ b")))
	// A block comment may contain what looks like a line comment.
	txnMaskAssert(t, "/* -- */x", txnSpaces(len("/* -- */"))+"x")
	// A line comment may contain what looks like a block comment.
	txnMaskAssert(t, "-- /*\nx", txnSpaces(len("-- /*"))+"\nx")
}

// Scenario: Un literal con signo de dólar se tapa con su etiqueta completa.
//
// A dollar-quoted string is opaque: it can hold anything, including a
// semicolon or a full DROP TABLE, and the only thing that ends it is the same
// tag that started it.
func TestMaskNonCode_DollarQuotedLiteral(t *testing.T) {
	txnMaskAssert(t, "$$ab$$", txnSpaces(len("$$ab$$")))
	txnMaskAssert(t, "$tag$DELETE FROM users$tag$", txnSpaces(len("$tag$DELETE FROM users$tag$")))
	txnMaskAssert(t, "$1$a;b$1$", txnSpaces(len("$1$a;b$1$")))
	// The opening tag runs to the LAST matching one, so the whole input is one
	// literal and the "a" in the middle is body text, not code.
	txnMaskAssert(t, "$_x$ a $_x$", txnSpaces(len("$_x$ a $_x$")))
	txnMaskAssert(t, "$_x$ a $_x$ tail", txnSpaces(len("$_x$ a $_x$"))+" tail")
	// A tag made of a non-ASCII byte is legal.
	txnMaskAssert(t, "$\x80$a$\x80$", txnSpaces(len("$\x80$a$\x80$")))
	// Content after the literal is code again.
	txnMaskAssert(t, "$$x$$ DELETE", txnSpaces(len("$$x$$"))+" DELETE")

	// An EMPTY dollar-quoted string: the opening tag is immediately followed by
	// the closing tag, with no body. This is the only shape where the tag search
	// reports index 0, and it is still a valid match — the guard rejects only a
	// NEGATIVE index, which is what "tag not found" means.
	txnMaskAssert(t, "$$$$", txnSpaces(len("$$$$")))
	txnMaskAssert(t, "SELECT $$$$", "SELECT "+txnSpaces(len("$$$$")))

	// The same empty body with a tag made of letters. The tag is masked along
	// with the body, so a tag that spells a forbidden keyword does not reach the
	// keyword scan.
	txnMaskAssert(t, "SELECT $DROP$$DROP$", "SELECT "+txnSpaces(len("$DROP$$DROP$")))
	txnMaskAssert(t, "SELECT $A$$A$", "SELECT "+txnSpaces(len("$A$$A$")))
}

// Scenario: Un signo de dólar que no abre un literal con etiqueta se deja como
// está. A bare $ is legal in an identifier and is not a quote.
func TestMaskNonCode_BareDollarIsNotAQuote(t *testing.T) {
	for _, in := range []string{
		"a$b",
		"$$abc",    // never closed
		"$tag$abc", // never closed
		"a $",      // nothing after it
		"$ $",      // a space cannot be in a tag
		"$$",       // nothing after the opening tag
		"$$a$",     // the opening tag only appears once
		"$a$a$",    // neither tag appears twice
	} {
		txnMaskAssert(t, in, in)
	}
}

// Scenario: Un literal con signo de dólar vacío es un literal, y su etiqueta se
// tapa con él.
//
// The tag search returns 0 here, and 0 is a valid hit, not a failure: the guard
// rejects a NEGATIVE index. So `$$$$` and `$DROP$$DROP$` are both masked, and
// the letters of a forbidden keyword inside a tag never reach the keyword scan.
// Treating 0 as a miss would let those letters through as code, which is
// exactly the smuggled-DROP the mask exists to prevent.
func TestIsSelectOnly_EmptyDollarQuotedLiteralIsAllowed(t *testing.T) {
	for _, in := range []string{
		"SELECT $$$$",
		"SELECT $DROP$$DROP$",
		"SELECT $A$$A$",
	} {
		if got := selectOnlyViolation(in); got != "" {
			t.Errorf("selectOnlyViolation(%q) = %q, want it allowed (an empty dollar-quoted literal is still masked)", in, got)
		}
	}

	// A non-empty body is allowed too, so the empty body is not what makes these
	// work.
	for _, in := range []string{
		"SELECT $x$a$x$",
		"SELECT $tag$hello$tag$",
	} {
		if got := selectOnlyViolation(in); got != "" {
			t.Errorf("selectOnlyViolation(%q) = %q, want it allowed", in, got)
		}
	}
}

// Scenario: isSQLIdentChar acepta justo lo que puede ir en una etiqueta de
// literal con signo de dólar o en un identificador sin comillas: letras,
// dígitos, el subrayado y cualquier byte no ASCII.
func TestIsSQLIdentChar(t *testing.T) {
	for _, b := range []byte{'_', 'a', 'z', 'A', 'Z', '0', '9', 0x80, 0xC3, 0xFF} {
		if !isSQLIdentChar(b) {
			t.Errorf("isSQLIdentChar(0x%02X) = false, want true", b)
		}
	}
	// Each range is pinned on both edges, because a check that was off by one
	// would accept a separator or reject the last valid byte.
	for _, b := range []byte{0x00, ' ', '(', '+', '-', '.', '/', ':', '@', '[', '`', '{', 0x7F, '$'} {
		if isSQLIdentChar(b) {
			t.Errorf("isSQLIdentChar(0x%02X) = true, want false", b)
		}
	}
	// The exact edge of the last range: 0x7F is not, 0x80 is.
	if isSQLIdentChar(0x7F) {
		t.Error("isSQLIdentChar(0x7F) = true, want false (0x7F is below the non-ASCII range)")
	}
	if !isSQLIdentChar(0x80) {
		t.Error("isSQLIdentChar(0x80) = false, want true (0x80 is the first non-ASCII byte)")
	}
	// The exact edges of the three ASCII ranges.
	for _, tc := range []struct{ lo, hi byte }{
		{'0', '9'},
		{'A', 'Z'},
		{'a', 'z'},
	} {
		if !isSQLIdentChar(tc.lo) || !isSQLIdentChar(tc.hi) {
			t.Errorf("isSQLIdentChar edges of %q-%q are not both accepted", tc.lo, tc.hi)
		}
		if isSQLIdentChar(tc.lo - 1) {
			t.Errorf("isSQLIdentChar(0x%02X) = true, want false (one below the %q range)", tc.lo-1, tc.lo)
		}
		if isSQLIdentChar(tc.hi + 1) {
			t.Errorf("isSQLIdentChar(0x%02X) = true, want false (one above the %q range)", tc.hi+1, tc.hi)
		}
	}
}

// Scenario: isIdentifierByte es isSQLIdentChar más el signo de dólar, porque un
// identificador de PostgreSQL sí puede contener $.
func TestIsIdentifierByte(t *testing.T) {
	for _, b := range []byte{'_', 'a', 'Z', '0', '$', 0x80} {
		if !isIdentifierByte(b) {
			t.Errorf("isIdentifierByte(0x%02X) = false, want true", b)
		}
	}
	for _, b := range []byte{' ', '(', '\'', '-', 0x7F} {
		if isIdentifierByte(b) {
			t.Errorf("isIdentifierByte(0x%02X) = true, want false", b)
		}
	}
	// The one difference between the two predicates, stated both ways.
	if isSQLIdentChar('$') {
		t.Error("isSQLIdentChar('$') = true, want false (a tag cannot contain a dollar)")
	}
	if !isIdentifierByte('$') {
		t.Error("isIdentifierByte('$') = false, want true (an identifier can contain a dollar)")
	}
}

// Scenario: sqlKeywordTokens se queda solo con las letras A-Z y el subrayado, y
// usa todo lo demás como separador.
//
// It is called on an already-uppercased string, so it never sees lowercase and
// never has to decide about digits: any rune outside A-Z and '_' ends the
// current token. 'Z' is a letter, not an edge, which is what makes "AZB" one
// token rather than two.
func TestSQLKeywordTokens(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"()", nil},
		{"SELECT", []string{"SELECT"}},
		{"SELECT 1", []string{"SELECT"}}, // a digit is a separator
		{"SELECT*FROM", []string{"SELECT", "FROM"}},
		{"A_Z", []string{"A_Z"}},           // the underscore joins a token
		{"A_B_C", []string{"A_B_C"}},       // and does not end one
		{"A_1", []string{"A_"}},            // but a digit after it does
		{"1ABC", []string{"ABC"}},          // a leading digit is dropped
		{"AZB", []string{"AZB"}},           // Z is a letter, not an edge
		{"ABZ", []string{"ABZ"}},           // at the end of a token too
		{"AZ", []string{"AZ"}},             // on its own
		{"_", []string{"_"}},               // the underscore alone
		{"A Z", []string{"A", "Z"}},        // a space separates
		{"A\nZ", []string{"A", "Z"}},       // a newline separates
		{"$", nil},                         // a dollar is a separator
		{"A1B2C", []string{"A", "B", "C"}}, // digits split
		{"$$DELETE$$", []string{"DELETE"}}, // only the letters survive
		{"DELETE-FROM", []string{"DELETE", "FROM"}},
	} {
		got := sqlKeywordTokens(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("sqlKeywordTokens(%q) = %q, want %q", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("sqlKeywordTokens(%q)[%d] = %q, want %q", tc.in, i, got[i], tc.want[i])
			}
		}
	}
}

// Scenario: El prefijo de palabra clave se acepta solo seguido de un separador
// real o de nada.
//
// Each accepted separator is checked on its own, because a check that only
// looked at the space would let "SELECT\t" through by accident and, worse, let a
// bare "SELECTED" be read as a DML statement.
func TestHasKeywordPrefix(t *testing.T) {
	for _, tc := range []struct {
		sql     string
		keyword string
		want    bool
	}{
		{"SELECT 1", "SELECT", true},   // the space
		{"SELECT", "SELECT", true},     // nothing after the keyword
		{"SELECT\t1", "SELECT", true},  // a tab
		{"SELECT\n1", "SELECT", true},  // a newline
		{"SELECT\r1", "SELECT", true},  // a carriage return
		{"SELECT(1)", "SELECT", true},  // an open paren, for CTE-ish shapes
		{"SELECTED", "SELECT", false},  // a letter continues the word
		{"SELECT_1", "SELECT", false},  // an underscore continues the word
		{"SELECT1", "SELECT", false},   // a digit continues the word
		{"SELECT,1", "SELECT", false},  // a comma is not a separator here
		{"SELECT.", "SELECT", false},   // nor is a dot
		{"SELECTED", "SELECTED", true}, // the exact word with nothing after
		{"SELEC 1", "SELECT", false},   // a prefix that is shorter
		{"SELECT", "SELECTS", false},   // a keyword longer than the string
		{"", "SELECT", false},          // nothing at all
	} {
		if got := hasKeywordPrefix(tc.sql, tc.keyword); got != tc.want {
			t.Errorf("hasKeywordPrefix(%q, %q) = %v, want %v", tc.sql, tc.keyword, got, tc.want)
		}
	}
}

// Scenario: Un SELECT seguido de otra cosa por delimitador de palabra es
// rechazado; el caso del espacio es el que más se repite.
func TestIsDML_WholeWordOnly(t *testing.T) {
	for _, tc := range []struct {
		sql  string
		want bool
	}{
		{"INSERT INTO t VALUES (1)", true},
		{"UPDATE t SET a=1", true},
		{"DELETE FROM t", true},
		{"WITH d AS (SELECT 1) SELECT 1", true},
		{"\n\tinsert into t values (1)", true},
		{"INSERTED INTO t", false},
		{"MYUPDATE t", false},
		{"SELECT 1", false},
		{"", false},
		{"   ", false},
		// SELECT itself is never DML, so it does not get a transaction.
		{"SELECT * FROM t", false},
		{"EXPLAIN SELECT 1", false},
	} {
		if got := isDML(tc.sql); got != tc.want {
			t.Errorf("isDML(%q) = %v, want %v", tc.sql, got, tc.want)
		}
	}
}

// Scenario: Una cadena que contiene la palabra prohibida sigue siendo un dato.
//
// This is the attack the lexer exists to stop. The literal holds a real DELETE,
// and PostgreSQL would store or return it as text. If the mask ended the literal
// at the doubled quote instead of treating it as an escape, the DELETE would
// become code and the statement would be rewritten into a mutation.
func TestIsSelectOnly_KeywordInsideALiteralStaysData(t *testing.T) {
	for _, in := range []string{
		`SELECT 'a''b DELETE FROM users'`,
		`SELECT 'x''; DROP TABLE users; --'`,
		`SELECT E'a\' DELETE FROM users'`,
		`SELECT 'it''s a DELETE FROM users'`,
	} {
		if got := selectOnlyViolation(in); got != "" {
			t.Errorf("selectOnlyViolation(%q) = %q, want it allowed (the keyword is inside a literal)", in, got)
		}
	}
}

// Scenario: El mismo texto fuera de un literal sí se rechaza, así que los casos
// anteriores no pasan por un escáner permisivo.
//
// The forbidden keyword sits right where the literal would have ended, so the
// only thing that makes these two halves differ is whether the lexer closed the
// literal at the right quote.
func TestIsSelectOnly_KeywordOutsideALiteralIsRejected(t *testing.T) {
	for _, tc := range []struct {
		sql  string
		want string
	}{
		{`SELECT 'a''b' x DROP TABLE users`, "DROP is not allowed"},
		{`SELECT E'a\'b' x DELETE FROM users`, "DELETE is not allowed"},
		{`SELECT 'ab'c'x' DELETE FROM users`, "DELETE is not allowed"},
		{`SELECT 'ab'c'x' nextval('s')`, "NEXTVAL is not allowed"},
		{`SELECT 'ab'c'x' UPDATE users SET a=1`, "UPDATE is not allowed"},
	} {
		got := selectOnlyViolation(tc.sql)
		if !strings.Contains(got, tc.want) {
			t.Errorf("selectOnlyViolation(%q) = %q, want it to mention %q", tc.sql, got, tc.want)
		}
	}
}

// Scenario: Un FOR al final del texto no es un bloqueo de fila: no hay token
// detrás que pueda ser una de las palabras de bloqueo.
func TestSelectOnlyViolation_ForAtTheEndHasNoLockWord(t *testing.T) {
	for _, in := range []string{
		"SELECT 1 FOR",
		"SELECT 1 FOR;",
		"SELECT substring(x FROM 1 FOR 2)",
	} {
		if got := selectOnlyViolation(in); got != "" {
			t.Errorf("selectOnlyViolation(%q) = %q, want it allowed", in, got)
		}
	}
	// One word later and it is a row lock, which is refused.
	for _, in := range []string{
		"SELECT 1 FOR UPDATE",
		"SELECT 1 FOR NO KEY UPDATE",
		"SELECT 1 FOR KEY SHARE",
		"SELECT 1 FOR SHARE",
	} {
		if got := selectOnlyViolation(in); got == "" {
			t.Errorf("selectOnlyViolation(%q) allowed a row lock, want it refused", in)
		}
	}
}

// Scenario: Un WITH sin ningún SELECT se rechaza: la rama que valida el WITH
// exige encontrar un SELECT, y si no lo hay la consulta no es de lectura.
//
// This is the case that distinguishes "found a SELECT inside the WITH" from
// "the loop happened to not find one". A check that treated those as the same
// would let a data-modifying CTE through whenever it had no outer SELECT.
func TestSelectOnlyViolation_WithWithoutSelectIsRejected(t *testing.T) {
	// None of these contain the token SELECT anywhere. RECURSIVE does not help:
	// the scan is for the literal word, and it is not fooled by WITH RECURSIVE
	// being valid PostgreSQL on its own.
	for _, in := range []string{
		"WITH t AS (VALUES (1))",
		"WITH t AS (VALUES (1)) t",
		"WITH RECURSIVE t AS (VALUES (1))",
	} {
		got := selectOnlyViolation(in)
		if !strings.Contains(got, "must end in a SELECT") {
			t.Errorf("selectOnlyViolation(%q) = %q, want the WITH-without-SELECT refusal", in, got)
		}
	}
	// And with an outer SELECT it is allowed, which is what makes the refusal
	// above about the missing SELECT rather than about WITH itself.
	for _, in := range []string{
		"WITH t AS (VALUES (1)) SELECT * FROM t",
		"WITH t AS (SELECT 1) SELECT * FROM t",
		"WITH t AS (SELECT 1), u AS (SELECT 2) SELECT * FROM t",
		"WITH t AS (VALUES (1)), u AS (VALUES (2)) SELECT * FROM t",
	} {
		if got := selectOnlyViolation(in); got != "" {
			t.Errorf("selectOnlyViolation(%q) = %q, want it allowed", in, got)
		}
	}
}

// Scenario: Una entrada sin sentencias ejecutables devuelve un resultado vacío,
// no un nil.
//
// A caller reads Columns and Rows off the result without a nil check, so an
// all-whitespace input has to come back as a usable zero value.
func TestExecute_EmptyInputReturnsAnEmptyResult(t *testing.T) {
	f := newFakeRunner()
	for _, in := range []string{"", "   ", "\n\t", ";"} {
		result, committed, err := f.runner.execute(context.Background(), in)
		if err != nil {
			t.Errorf("execute(%q) = %v, want no error", in, err)
		}
		if result == nil {
			t.Errorf("execute(%q) returned a nil result, want an empty one", in)
		}
		if committed {
			t.Errorf("execute(%q) reported a commit, want false", in)
		}
		if len(f.execs) != 0 {
			t.Errorf("execute(%q) ran %d statements, want 0", in, len(f.execs))
		}
	}
}

// Scenario: Un fallo al elegir la querier aborta el lote.
//
// The statement never reaches exec, and the commit flag already accumulated from
// earlier statements is still reported, because earlier work in the batch really
// was committed.
func TestExecute_QuerierSelectionFailureAbortsTheBatch(t *testing.T) {
	f := newFakeRunner()
	// A pending transaction whose commit fails, so querierFor errors.
	f.runner.tx = &txnFailingTx{}

	result, committed, err := f.runner.execute(context.Background(), "SELECT 1")
	if err == nil {
		t.Fatal("execute returned no error for a failing querier selection")
	}
	if result != nil {
		t.Error("execute returned a result alongside the error")
	}
	if committed {
		t.Error("execute reported a successful commit alongside the error")
	}
	if len(f.execs) != 0 {
		t.Errorf("execute ran %d statements despite the selection failure, want 0", len(f.execs))
	}
}

// Scenario: Un fallo de exec aborta el lote y devuelve el error.
func TestExecute_ExecFailureAbortsTheBatch(t *testing.T) {
	f := newFakeRunner()
	f.runner.exec = func(_ context.Context, _ postgres.Querier, sql string) (*postgres.QueryResult, error) {
		f.execs = append(f.execs, recordedExec{sql: sql})
		return nil, errors.New("boom")
	}

	result, committed, err := f.runner.execute(context.Background(), "SELECT 1")
	if err == nil {
		t.Fatal("execute returned no error for a failing statement")
	}
	if result != nil {
		t.Error("execute returned a result alongside the error")
	}
	if committed {
		t.Error("execute reported a commit alongside the error")
	}
	// The error propagates rather than being swallowed.
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("error = %q, want it to carry the underlying failure", err)
	}
}

// Scenario: Un fallo de begin para una sentencia DML devuelve el error y no
// deja transacción a medias.
//
// The mutex is released before returning, so a failed begin must not wedge the
// runner: the next statement has to be able to try again.
func TestQuerierFor_BeginFailurePropagates(t *testing.T) {
	f := newFakeRunner()
	f.runner.begin = func(context.Context) (pgx.Tx, error) {
		return nil, errors.New("no transaction available")
	}

	querier, committed, err := f.runner.querierFor(context.Background(), "INSERT INTO t VALUES (1)")
	if err == nil {
		t.Fatal("querierFor returned no error for a failing begin")
	}
	if !strings.Contains(err.Error(), "failed to begin transaction") {
		t.Errorf("error = %q, want it to name the failing step", err)
	}
	if querier != nil || committed {
		t.Errorf("querierFor = (%v, %v), want (nil, false) alongside the error", querier, committed)
	}
	if f.runner.pending() {
		t.Error("a failed begin left a transaction pending")
	}

	// The runner is still usable: a second attempt goes through once begin works.
	f.runner.begin = func(context.Context) (pgx.Tx, error) { return &fakeTx{}, nil }
	if _, _, err := f.runner.querierFor(context.Background(), "INSERT INTO t VALUES (1)"); err != nil {
		t.Errorf("second querierFor = %v, want it to succeed after the first failed begin", err)
	}
}

// Scenario: Una sentencia no DML con una transacción pendiente corre en
// autocommit y reporta que hubo que confirmar algo.
//
// This is the other branch of querierFor: the DML path reuses the transaction,
// and everything else has to commit it first so the statement is not part of it.
func TestQuerierFor_NonDMLCommitsThePendingTransaction(t *testing.T) {
	f := newFakeRunner()
	tx := &fakeTx{}
	f.runner.tx = tx

	querier, committed, err := f.runner.querierFor(context.Background(), "SELECT 1")
	if err != nil {
		t.Fatalf("querierFor: %v", err)
	}
	if !committed {
		t.Error("querierFor reported no commit, want true: a pending DML transaction was closed")
	}
	if querier != postgres.Querier(f.conn) {
		t.Error("the statement did not run on the connection itself")
	}
	if tx.commits != 1 {
		t.Errorf("commits = %d, want 1", tx.commits)
	}
	if f.runner.pending() {
		t.Error("the transaction is still pending after a non-DML statement")
	}

	// With nothing pending, the same statement reports no commit.
	querier, committed, err = f.runner.querierFor(context.Background(), "SELECT 1")
	if err != nil || committed {
		t.Errorf("querierFor with nothing pending = (%v, %v), want (conn, false)", committed, err)
	}
	if querier != postgres.Querier(f.conn) {
		t.Error("the statement did not run on the connection itself")
	}
}

// Scenario: Sin beginReadOnly configurado, la ruta de solo lectura se niega en
// vez de ejecutar sin protección.
//
// The nil check is what keeps a runner wired without the ASK hook from running
// an unvalidated statement: there is no server-enforced READ ONLY to fall back
// on, so it must refuse.
func TestExecuteReadOnly_RefusesWithoutAHook(t *testing.T) {
	f := newFakeRunner()
	f.runner.beginReadOnly = nil

	if _, err := f.runner.executeReadOnly(context.Background(), "SELECT 1"); err == nil {
		t.Fatal("executeReadOnly ran with no beginReadOnly hook")
	}
	if len(f.execs) != 0 {
		t.Errorf("executeReadOnly executed %d statements without a hook, want 0", len(f.execs))
	}
}

// Scenario: Un fallo al abrir la transacción de solo lectura se propaga y no
// activa el flag de ocupación.
func TestExecuteReadOnly_BeginFailurePropagates(t *testing.T) {
	f := newFakeRunner()
	f.runner.beginReadOnly = func(context.Context) (pgx.Tx, error) {
		return nil, errors.New("cannot begin")
	}

	if _, err := f.runner.executeReadOnly(context.Background(), "SELECT 1"); err == nil {
		t.Fatal("executeReadOnly returned no error for a failing begin")
	}
	// The flag is only set after a successful begin, so it must be clear now.
	if f.runner.readOnlyBusy() {
		t.Error("readOnlyActive stayed set after a failed begin")
	}
	if len(f.execs) != 0 {
		t.Errorf("executeReadOnly executed %d statements, want 0", len(f.execs))
	}
}

// Scenario: Una sentencia que solo tiene literales y comentarios no deja
// tokens, y se rechaza por estar vacía.
//
// The mask blanks every byte of it, so the token scan finds nothing at all. That
// is a different refusal from a forbidden keyword, and the message says so.
func TestSelectOnlyViolation_NoTokensAfterMasking(t *testing.T) {
	for _, in := range []string{
		"'just a literal'",
		"-- only a comment",
		"/* only a comment */",
		"'literal' -- comment",
	} {
		if got := selectOnlyViolation(in); got != "empty statement" {
			t.Errorf("selectOnlyViolation(%q) = %q, want %q", in, got, "empty statement")
		}
	}
	// A statement with a semicolon but nothing executable on either side is
	// still empty, not "two statements".
	if got := selectOnlyViolation("; ; ;"); got != "empty statement" {
		t.Errorf("selectOnlyViolation(%q) = %q, want %q", "; ; ;", got, "empty statement")
	}
}

// txnFailingTx is a pgx.Tx whose Commit and Rollback always fail, so the error
// branches of the transaction lifecycle can be reached without a live database.
type txnFailingTx struct {
	pgx.Tx
	commits   int
	rollbacks int
}

func (f *txnFailingTx) Commit(context.Context) error { f.commits++; return context.DeadlineExceeded }
func (f *txnFailingTx) Rollback(context.Context) error {
	f.rollbacks++
	return context.DeadlineExceeded
}

// Scenario: Un commit fallido se propaga y deja la transacción pendiente
// descartada.
//
// The runner clears r.tx BEFORE committing, so a failed commit must not leave a
// dangling handle that the next statement would try to reuse.
func TestCommitPending_CommitFailurePropagatesAndClears(t *testing.T) {
	f := newFakeRunner()
	tx := &txnFailingTx{}
	f.runner.tx = tx

	committed, err := f.runner.commitPending(context.Background())
	if err == nil {
		t.Fatal("commitPending returned no error for a failing commit")
	}
	if !committed {
		t.Error("commitPending reported no pending transaction, want true (there was one)")
	}
	if tx.commits != 1 {
		t.Errorf("commit called %d times, want 1", tx.commits)
	}
	if f.runner.pending() {
		t.Error("a failed commit left a transaction pending; the next statement would reuse it")
	}
	// A second call has nothing left to do.
	if committed, err := f.runner.commitPending(context.Background()); committed || err != nil {
		t.Errorf("second commitPending = (%v, %v), want (false, nil)", committed, err)
	}
}

// Scenario: Un rollback fallido se propaga y también descarta la referencia.
func TestRollback_RollbackFailurePropagatesAndClears(t *testing.T) {
	f := newFakeRunner()
	f.runner.tx = &txnFailingTx{}

	rolled, err := f.runner.rollback(context.Background())
	if err == nil {
		t.Fatal("rollback returned no error for a failing rollback")
	}
	if !rolled {
		t.Error("rollback reported no pending transaction, want true")
	}
	if f.runner.pending() {
		t.Error("a failed rollback left a transaction pending")
	}
}

// Scenario: Sin transacción pendiente, commit y rollback no hacen nada y no
// inventan un error.
func TestCommitPendingAndRollback_NothingPending(t *testing.T) {
	f := newFakeRunner()

	if committed, err := f.runner.commitPending(context.Background()); committed || err != nil {
		t.Errorf("commitPending with nothing pending = (%v, %v), want (false, nil)", committed, err)
	}
	if rolled, err := f.runner.rollback(context.Background()); rolled || err != nil {
		t.Errorf("rollback with nothing pending = (%v, %v), want (false, nil)", rolled, err)
	}
	if f.begins != 0 {
		t.Errorf("began %d transactions, want 0", f.begins)
	}
}

// Scenario: Una sentencia que no es DML, con una transacción pendiente cuyo
// commit falla, devuelve el error en vez de ejecutarse en autocommit.
//
// The order matters: the commit has to be attempted before the statement runs,
// and its failure has to stop the statement. Running it anyway would leave the
// connection in a state pgx cannot recover from.
func TestQuerierFor_NonDMLAfterAFailedCommitReturnsTheError(t *testing.T) {
	f := newFakeRunner()
	tx := &txnFailingTx{}
	f.runner.tx = tx

	querier, committed, err := f.runner.querierFor(context.Background(), "SELECT 1")
	if err == nil {
		t.Fatal("querierFor returned no error for a failing pending commit")
	}
	if querier != nil {
		t.Error("querierFor returned a querier alongside the error")
	}
	if committed {
		t.Error("querierFor reported a successful commit alongside the error")
	}
	if len(f.execs) != 0 {
		t.Errorf("the statement ran %d times despite the failed commit, want 0", len(f.execs))
	}
}

// Scenario: El mismo fallo visto desde execute: el error sube y no se ejecuta
// ninguna sentencia.
func TestExecute_NonDMLAfterAFailedCommitReturnsTheError(t *testing.T) {
	f := newFakeRunner()
	f.runner.tx = &txnFailingTx{}

	result, committed, err := f.runner.execute(context.Background(), "SELECT 1")
	if err == nil {
		t.Fatal("execute returned no error for a failing pending commit")
	}
	if result != nil {
		t.Error("execute returned a result alongside the error")
	}
	if committed {
		t.Error("execute reported a successful commit alongside the error")
	}
	if len(f.execs) != 0 {
		t.Errorf("execute ran %d statements despite the failed commit, want 0", len(f.execs))
	}
}
