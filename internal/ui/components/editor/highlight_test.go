package editor

import (
	"strings"
	"testing"
	"time"

	"github.com/buble/dbx/internal/theme"
	"github.com/charmbracelet/x/ansi"
)

// --- the two invariants everything else rests on ----------------------------

// tokenizeWithin runs tokenizeSQL with a time budget and FAILS on a hang instead
// of hanging with it.
//
// This is not defensive style, it is the only way these tests can kill a class of
// mutant they otherwise cannot touch. Several of tokenizeSQL's loop guards are the
// only thing advancing the index: negate one, or turn an `i++` into an `i--`, and
// the loop never terminates. A test that HANGS kills nothing — the mutation
// harness gives up and reports the mutant as TIMED OUT, which is absent from
// report.json and therefore indistinguishable from a kill. The gate's efficacy
// number goes up and nobody learns that sixteen mutants were never evaluated.
//
// A goroutine plus a deadline turns the hang into an ordinary test failure, which
// the harness can see. The leaked goroutine does not matter: the binary exits when
// the test fails, and Go does not wait for it.
//
// The budget is small ON PURPOSE, and the number is not arbitrary.
//
// It has to fire BEFORE the mutation harness gives up, not merely eventually. The
// harness allows `--timeout-coefficient` times the unmutated suite time; this
// package runs in about 14ms and the coefficient is 20, so the harness abandons a
// hung mutant at roughly 280ms and reports it as TIMED OUT — which is absent from
// report.json and therefore indistinguishable from a kill. A 5s watchdog sounds
// generous and is useless: it never gets to run.
//
// 50ms is about a thousand times the real cost of tokenizing these fixtures, so it
// cannot fire on a slow machine, and it is comfortably inside the harness budget.
// If the coefficient or the suite time changes such that 50ms stops being safe,
// the symptom is timeouts reappearing in the report, which is visible. That is the
// property to keep: the coupling is real, and it fails loudly rather than silently.
const tokenizeBudget = 50 * time.Millisecond

func tokenizeWithin(t *testing.T, input string) []sqlToken {
	t.Helper()
	type result struct {
		toks []sqlToken
	}
	done := make(chan result, 1)
	go func() {
		defer func() {
			// A panic inside the tokenizer is the mutant's doing too, and it would
			// otherwise take the whole binary down instead of failing one test.
			if r := recover(); r != nil {
				done <- result{toks: nil}
			}
		}()
		done <- result{toks: tokenizeSQL(input)}
	}()

	select {
	case r := <-done:
		return r.toks
	case <-time.After(tokenizeBudget):
		t.Fatalf("tokenizeSQL(%q) did not finish within %s: the loop is not advancing the index", oneline(input), tokenizeBudget)
		return nil
	}
}

// tokensText concatenates the token texts, which is what the renderer writes out.
func tokensText(toks []sqlToken) string {
	var b strings.Builder
	for _, tok := range toks {
		b.WriteString(tok.text)
	}
	return b.String()
}

// Scenario: Los tokens particionan la entrada: nada se pierde, nada se duplica.
//
// This is THE invariant of a tokenizer, and it is worth more than any per-rule
// test: if the tokens do not concatenate back to the input, the editor is
// displaying something other than what the user typed, and the cursor column —
// which is a byte offset into the ORIGINAL string — no longer lines up with what
// is on screen.
//
// Every lexeme below is here because it is a plausible thing to get wrong, and the
// assertion is the same for all of them: text in, same text out, same length.
func TestTokenizeSQL_TokensConcatenateBackToTheInput(t *testing.T) {
	for _, in := range []string{
		"",
		"SELECT 1",
		"SELECT * FROM users WHERE id = 1;",
		// Every rule, in one line.
		"SELECT 'a''b', 12.5, x_y, a.b(c), d <= e, f <> g, h != i, -- comment\n",
		"/* block */ SELECT 1 /* another */",
		"/* unterminated",
		"/*",
		"'unterminated",
		"''",
		"'it''s'",
		"-",
		"--",
		"/",
		"/*/",
		"1.",
		".5",
		"1.2.3",
		// Multi-byte runes must never be split: the offsets are byte offsets.
		"SELECT 'héllo' FROM café",
		"SELECT '日本語' AS 名前",
		"élan -- 中文\n/* ünïcode */",
		// A word STARTING with a high byte. The word rule has its own `>= 0x80`
		// clause on the START check, separate from the one on the continuation,
		// and starting with one is the only input that reaches it.
		"\x80abc",
		"\xff",
		"\x80",
		// Bytes that are neither ASCII nor a known punctuation mark.
		"a\x00b\x01c",
		"@\x7f",
		// Whitespace runs of every kind.
		"  \t\r\n  SELECT",
		"\n\n\n",
		// Long enough to exercise the two-char operator branch repeatedly.
		"a<=b>=c<>d!=e<f>g=h+i-j*k/l%m",
		// A word starting with an underscore, and a word that is only digits after
		// a letter.
		"_x9 abc123",
		// A number immediately followed by a word: two tokens, not one.
		"1abc",
		// Punctuation runs.
		"((())) ,,;; ..",
	} {
		t.Run(oneline(in), func(t *testing.T) {
			toks := tokenizeWithin(t, in)
			if got := tokensText(toks); got != in {
				t.Errorf("the tokens concatenate to %q, want the input %q", got, in)
			}
			var total int
			for _, tok := range toks {
				total += tok.end - tok.start
			}
			if total != len(in) {
				t.Errorf("the token spans cover %d bytes, want the input's %d", total, len(in))
			}
		})
	}
}

// Scenario: Los tokens son contiguos y sus offsets son los de la entrada.
//
// The renderer's cursor arithmetic indexes the ORIGINAL string with token.start and
// token.end, so an offset that is merely self-consistent is not enough: it has to
// be the offset the byte really sits at. Gaps, overlaps and end-before-start all
// break the cursor.
func TestTokenizeSQL_OffsetsPartitionTheInput(t *testing.T) {
	for _, in := range []string{
		"SELECT 1",
		"SELECT 'a''b' FROM t -- x\nWHERE a <= b",
		"/* c */ é日本 1.2.3",
		"   ",
		"'unterminated",
		"/* unterminated",
	} {
		t.Run(oneline(in), func(t *testing.T) {
			toks := tokenizeWithin(t, in)
			at := 0
			for i, tok := range toks {
				if tok.start != at {
					t.Fatalf("token %d starts at %d, want %d (the end of the previous one): %+v", i, tok.start, at, tok)
				}
				if tok.end <= tok.start {
					t.Fatalf("token %d is empty or inverted: %+v", i, tok)
				}
				if tok.end > len(in) {
					t.Fatalf("token %d ends at %d, past the input's %d bytes: %+v", i, tok.end, len(in), tok)
				}
				if got := in[tok.start:tok.end]; got != tok.text {
					t.Fatalf("token %d says %q for bytes [%d:%d], which are %q", i, tok.text, tok.start, tok.end, got)
				}
				at = tok.end
			}
			if at != len(in) {
				t.Errorf("the tokens stop at %d, want the input's %d bytes", at, len(in))
			}
		})
	}
}

// Scenario: Una entrada vacía no produce ningún token.
//
// The renderer has an early return for "", and the tokenizer has to agree, or the
// two paths produce different output for the same input.
func TestTokenizeSQL_EmptyInputHasNoTokens(t *testing.T) {
	if got := tokenizeWithin(t, ""); len(got) != 0 {
		t.Errorf("tokenize(\"\") = %+v, want no tokens", got)
	}
}

// --- one test per lexing rule ------------------------------------------------

// typeAt is the type of the first token of the given type, or tokenDefault when
// there is none, plus how many there were.
func typesOf(toks []sqlToken) map[sqlTokenType]int {
	out := map[sqlTokenType]int{}
	for _, tok := range toks {
		out[tok.typ]++
	}
	return out
}

// Scenario: Un `--` hasta el final de la línea es un comentario, y el salto de
// línea NO se lleva.
//
// The newline staying outside the comment is what lets the next line be tokenized.
// Swallowing it would colour the start of the next statement as a comment.
func TestTokenizeSQL_LineCommentStopsBeforeTheNewline(t *testing.T) {
	toks := tokenizeWithin(t, "SELECT 1 -- note\nFROM t")
	byType := typesOf(toks)
	if byType[tokenComment] != 1 {
		t.Fatalf("got %d comments, want 1: %+v", byType[tokenComment], toks)
	}
	for _, tok := range toks {
		if tok.typ == tokenComment {
			if tok.text != "-- note" {
				t.Errorf("the comment is %q, want %q with no newline", tok.text, "-- note")
			}
			// "SELECT 1 " is 9 bytes and "-- note" is 7, so the comment is
			// [9,16) and the newline sits at 16 — outside it.
			if tok.start != 9 || tok.end != 16 {
				t.Errorf("the comment spans [%d,%d), want [9,16) with the newline at 16", tok.start, tok.end)
			}
		}
	}
	// FROM is a keyword, so the line after the comment really was tokenized.
	if !hasToken(toks, tokenKeyword, "FROM") {
		t.Errorf("FROM was not tokenized as a keyword: %+v", toks)
	}
	// A single dash is an operator, not a comment.
	if tok := tokenizeWithin(t, "a - b"); typesOf(tok)[tokenComment] != 0 {
		t.Errorf("a lone dash produced a comment: %+v", tok)
	}
	// And a dash at the very end still opens a comment.
	if got := tokenizeWithin(t, "--"); typesOf(got)[tokenComment] != 1 {
		t.Errorf("a trailing -- produced no comment: %+v", got)
	}
}

// Scenario: Un `/*` se cierra en el primer `*/`, y el bloque puede contener
// cualquier cosa.
//
// The comment must swallow quotes, operators and newlines, or a `--` inside a
// block comment would end it early and the rest would be highlighted as code.
func TestTokenizeSQL_BlockCommentSwallowsEverythingToItsCloser(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"a simple block", "/* c */x", "/* c */"},
		{"with a quote inside", "/* don't */x", "/* don't */"},
		{"with an opener inside", "/* /* nested */x", "/* /* nested */"},
		{"spanning lines", "/* one\ntwo */x", "/* one\ntwo */"},
		{"unterminated runs to the end", "/* open", "/* open"},
		{"just the opener", "/*", "/*"},
		{"opener then slash-star", "/*/", "/*/"},
		// The closer needs the star: /*/ is not closed.
		{"a lone star does not close", "/* * / x", "/* * / x"},
		// An unterminated comment whose LAST byte is a star. This is the only
		// shape where the loop bound being off by one is observable at all: the
		// `input[i+1]` read is short-circuited by `input[i] == '*'`, so a
		// non-star last byte never reads past the end and the extra iteration
		// changes nothing.
		{"unterminated ending in a star", "/* x*", "/* x*"},
		{"just a star inside", "/*a*", "/*a*"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			toks := tokenizeWithin(t, tc.in)
			if typesOf(toks)[tokenComment] != 1 {
				t.Fatalf("got no comment token: %+v", toks)
			}
			if got := toks[0].text; got != tc.want {
				t.Errorf("the comment is %q, want %q", got, tc.want)
			}
		})
	}
}

// Scenario: Una cadena termina en la comilla que no está duplicada.
//
// The doubled quote is the one thing that makes a plain string scan wrong: without
// it, `'it”s'` closes at the first quote and the rest is highlighted as code.
func TestTokenizeSQL_StringLiteralEndsAtTheUndoubledQuote(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"simple", "'abc'", "'abc'"},
		{"a doubled quote inside", "'it''s'", "'it''s'"},
		{"three in a row", "'a''b''c'", "'a''b''c'"},
		{"the last quote is doubled", "'abc''", "'abc''"},
		{"a doubled quote at the start", "'''x'", "'''x'"},
		{"unterminated runs to the end", "'abc", "'abc"},
		{"a lone quote", "'", "'"},
		{"an empty string", "''", "''"},
		{"a newline inside is kept", "'a\nb'", "'a\nb'"},
		// A semicolon inside a string is data, not a statement end.
		{"a semicolon inside", "'a;b'", "'a;b'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			toks := tokenizeWithin(t, tc.in)
			if typesOf(toks)[tokenString] != 1 {
				t.Fatalf("got no string token: %+v", toks)
			}
			if got := toks[0].text; got != tc.want {
				t.Errorf("the string is %q, want %q", got, tc.want)
			}
			// And it is a string even when it contains an opener-looking thing.
			if tc.name == "a semicolon inside" && typesOf(toks)[tokenKeyword] != 0 {
				t.Errorf("something inside the string was lexed as a keyword: %+v", toks)
			}
		})
	}
}

// Scenario: Un número se come sus puntos, y un punto suelto es puntuación.
//
// `1.2.3` is one number because the scan accepts dots freely, and `t.col` is a word
// then a dot then a word because the word scan starts at the letter. Getting the
// two confused is what makes `users.id` render as three colours.
func TestTokenizeSQL_NumbersAbsorbDotsButSurroundingDotsDoNot(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
		typ  sqlTokenType
	}{
		{"12", []string{"12"}, tokenNumber},
		{"12.5", []string{"12.5"}, tokenNumber},
		{"1.2.3", []string{"1.2.3"}, tokenNumber},
		{"1.", []string{"1."}, tokenNumber},
		// A leading dot is punctuation, so ".5" is a dot then a number.
		{".5", []string{".", "5"}, tokenDefault},
		// A digit after a letter continues the WORD, not a number.
		{"abc123", []string{"abc123"}, tokenDefault},
		// A digit after a NUMBER is still a number, so "1abc" is a number then a
		// word — the scan takes digits and dots and stops at the letter.
		{"1abc", []string{"1", "abc"}, tokenNumber},
	} {
		t.Run(tc.in, func(t *testing.T) {
			toks := tokenizeWithin(t, tc.in)
			if len(toks) != len(tc.want) {
				t.Fatalf("got %d tokens %+v, want %d", len(toks), toks, len(tc.want))
			}
			for i, want := range tc.want {
				if toks[i].text != want {
					t.Errorf("token %d is %q, want %q", i, toks[i].text, want)
				}
			}
			if toks[0].typ != tc.typ {
				t.Errorf("the first token is type %d, want %d", toks[0].typ, tc.typ)
			}
		})
	}
}

// Scenario: Una palabra admite letras, dígitos, guion bajo y bytes altos.
//
// The `>= 0x80` clauses are what keep a UTF-8 rune in one piece. Splitting one
// would put a token boundary in the middle of a character, and the renderer would
// emit half a rune followed by a style change.
func TestTokenizeSQL_WordsKeepRunesWhole(t *testing.T) {
	for _, in := range []string{
		"café", "日本語", "élan", "名前", "_x9", "abc123", "a_b_c", "Ωmega",
		// Starting with a high byte, which the START clause has to allow.
		"\x80abc", "éclair",
	} {
		t.Run(in, func(t *testing.T) {
			toks := tokenizeWithin(t, in)
			if len(toks) != 1 {
				t.Fatalf("got %d tokens %+v, want the whole word as one", len(toks), toks)
			}
			if toks[0].text != in {
				t.Errorf("the token is %q, want %q", toks[0].text, in)
			}
		})
	}
	// A keyword written with an accent in it is not a keyword, which is correct:
	// it is a different word.
	if typesOf(tokenizeWithin(t, "sélect"))[tokenKeyword] != 0 {
		t.Error("an accented word was lexed as a keyword")
	}
}

// Scenario: Un operador de dos caracteres se queda entero.
//
// Splitting `<=` into `<` and `=` would still concatenate back to the input, so the
// partition invariant does not catch it — but the rendered operator is then two
// style runs, and a user reading the line sees a differently coloured character
// mid-operator.
func TestTokenizeSQL_TwoCharacterOperatorsStayTogether(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
	}{
		{"<=", "<="},
		{">=", ">="},
		{"<>", "<>"},
		{"!=", "!="},
		// These pairs are NOT two-character operators, so they stay split.
		{"<<", "<"},
		{">>", ">"},
		{"==", "="},
		{"=>", "="},
		// A single operator followed by a non-pair character.
		{"<a", "<"},
		{"!a", "!"},
	} {
		t.Run(tc.in, func(t *testing.T) {
			toks := tokenizeWithin(t, tc.in)
			// Find the operator token and check its text.
			for _, tok := range toks {
				if tok.typ == tokenOperator {
					if tok.text != tc.want {
						t.Errorf("the operator token is %q, want %q (all tokens: %+v)", tok.text, tc.want, toks)
					}
					return
				}
			}
			t.Fatalf("no operator token in %+v", toks)
		})
	}
}

// Scenario: Los paréntesis, comas, punto y coma y puntos son tokens por sí solos.
//
// They are tokenDefault, not tokenOperator, so they render unstyled. That is the
// visible difference between the two and it is worth pinning.
func TestTokenizeSQL_PunctuationIsUnstyled(t *testing.T) {
	for _, in := range []string{"(", ")", ",", ";", "."} {
		t.Run(in, func(t *testing.T) {
			toks := tokenizeWithin(t, in)
			if len(toks) != 1 || toks[0].typ != tokenDefault {
				t.Errorf("tokenize(%q) = %+v, want one default-typed token", in, toks)
			}
		})
	}
	// And they are not operators, which is what makes them unstyled.
	for _, in := range []string{"(", ")", ",", ";", "."} {
		if typesOf(tokenizeWithin(t, in))[tokenOperator] != 0 {
			t.Errorf("%q was lexed as an operator", in)
		}
	}
}

// Scenario: Los espacios y tabuladores son un solo token, y los saltos de línea
// se cuentan aparte.
//
// A run of whitespace is one token so the renderer does not emit a style run per
// space. Newlines are INSIDE that run, which is why the line count of the rendered
// output has to match the input's.
func TestTokenizeSQL_WhitespaceRunsAreOneToken(t *testing.T) {
	for _, in := range []string{" ", "  ", "\t", "\n", " \t\n\r ", "\n\n\n"} {
		t.Run(oneline(in), func(t *testing.T) {
			toks := tokenizeWithin(t, in)
			if len(toks) != 1 {
				t.Fatalf("got %d tokens %+v, want the run as one", len(toks), toks)
			}
			if toks[0].typ != tokenDefault {
				t.Errorf("the whitespace token is type %d, want default (untyped)", toks[0].typ)
			}
			if toks[0].text != in {
				t.Errorf("the token is %q, want %q", toks[0].text, in)
			}
		})
	}
}

// Scenario: Un byte que no es nada conocido sigue siendo un token, y no se
// pierde.
//
// The unknown-character branch at the end is the safety net: a NUL, a control byte
// or a stray symbol must still be emitted, or the rendered line would be shorter
// than the input and every column after it would be off.
func TestTokenizeSQL_UnknownBytesAreStillEmitted(t *testing.T) {
	for _, in := range []string{"@", "#", "\x00", "\x01", "\x7f", "$", "\\", "|", "^", "~", "?", "&"} {
		t.Run(oneline(in), func(t *testing.T) {
			toks := tokenizeWithin(t, in)
			if len(toks) != 1 || toks[0].text != in {
				t.Fatalf("tokenize(%q) = %+v, want one token holding it", in, toks)
			}
			if toks[0].typ != tokenDefault {
				t.Errorf("the token is type %d, want default", toks[0].typ)
			}
		})
	}
}

// Scenario: Una palabra en MAYÚSCULAS es palabra clave y una en minúsculas
// también.
//
// SQL keywords are case-insensitive, so `select` and `SELECT` have to be the same
// token type. Getting that wrong colours half of every query the user writes.
func TestTokenizeSQL_KeywordsAreRecognisedInAnyCase(t *testing.T) {
	for _, in := range []string{"SELECT", "select", "Select", "sElEcT", "from", "WHERE", "where"} {
		t.Run(in, func(t *testing.T) {
			toks := tokenizeWithin(t, in)
			if len(toks) != 1 || toks[0].typ != tokenKeyword {
				t.Errorf("tokenize(%q) = %+v, want one keyword token", in, toks)
			}
		})
	}
	// A word that is not a keyword is not one, whatever its case.
	for _, in := range []string{"users", "USERS", "Users", "my_table", "id"} {
		t.Run(in, func(t *testing.T) {
			toks := tokenizeWithin(t, in)
			if toks[0].typ == tokenKeyword {
				t.Errorf("tokenize(%q) = %+v, want it NOT to be a keyword", in, toks)
			}
			if toks[0].typ != tokenDefault {
				t.Errorf("tokenize(%q) is type %d, want default", in, toks[0].typ)
			}
		})
	}
}

// Scenario: Una función es una palabra más, y se distingue de una palabra clave.
//
// The two maps overlap: NOW and COUNT are in both, REPLACE and EXISTS too. The
// keyword branch is tested first, so a word in both maps renders as a keyword.
// That is the order the code has, and it is pinned here because swapping the two
// `else if` arms would recolour every aggregate in a query.
func TestTokenizeSQL_FunctionsAreDistinctFromKeywordsAndWords(t *testing.T) {
	// In sqlFunctions but not sqlKeywords.
	for _, in := range []string{"upper", "lower", "concat_ws", "date_trunc", "dense_rank", "strpos", "ceiling"} {
		// ceiling is not in either map; skip it below.
		if !sqlFunctions[strings.ToUpper(in)] || sqlKeywords[strings.ToUpper(in)] {
			continue
		}
		toks := tokenizeWithin(t, in)
		if toks[0].typ != tokenFunction {
			t.Errorf("tokenize(%q) is type %d, want a function", in, toks[0].typ)
		}
	}
	// In BOTH maps: the keyword branch wins.
	for word := range sqlFunctions {
		if !sqlKeywords[word] {
			continue
		}
		toks := tokenizeWithin(t, strings.ToLower(word))
		if toks[0].typ != tokenKeyword {
			t.Errorf("%q is in both maps and came out as type %d, want a keyword: the keyword branch has to be tested first", word, toks[0].typ)
		}
	}
	// In neither: default.
	for _, in := range []string{"users", "my_col", "x"} {
		if sqlKeywords[strings.ToUpper(in)] || sqlFunctions[strings.ToUpper(in)] {
			continue
		}
		if tok := tokenizeWithin(t, in)[0]; tok.typ != tokenDefault {
			t.Errorf("tokenize(%q) is type %d, want default", in, tok.typ)
		}
	}
}

// --- the character predicates -----------------------------------------------

// Scenario: Las tres tablas de caracteres son exactamente lo que dicen.
//
// These are the leaves every rule above is built on, and each has two boundaries
// worth pinning: the byte just inside and the byte just outside.
func TestTheCharacterPredicates(t *testing.T) {
	for _, tc := range []struct {
		name string
		fn   func(byte) bool
		yes  []byte
		no   []byte
	}{
		{"isASCIILetter", isASCIILetter, []byte{'a', 'z', 'A', 'Z', 'e', 'Q'}, []byte{'0', '9', '_', ' ', '@', 0x7f, 0x80}},
		{"isASCIIDigit", isASCIIDigit, []byte{'0', '5', '9'}, []byte{'a', 'Z', '_', ' ', '/', 0x7f, 0x80}},
		{"isOperatorByte", isOperatorByte, []byte{'=', '<', '>', '!', '+', '-', '*', '/', '%'}, []byte{'(', ')', ',', ';', '.', ' ', 'a', '0', '_', '@', 0x80}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, b := range tc.yes {
				if !tc.fn(b) {
					t.Errorf("%s(%q) = false, want true", tc.name, string(rune(b)))
				}
			}
			for _, b := range tc.no {
				if tc.fn(b) {
					t.Errorf("%s(%q) = true, want false", tc.name, string(rune(b)))
				}
			}
		})
	}
}

// Scenario: Los bytes altos NO son letras ASCII ni dígitos, pero sí son parte de
// una palabra.
//
// This is the pair of facts the word rule depends on, and it is easy to get half
// right: treating 0x80 as a letter would make `isASCIILetter` lie about its name
// and would let a word start on a continuation byte.
func TestTheCharacterPredicates_HighBytesAreNeitherLetterNorDigit(t *testing.T) {
	for _, b := range []byte{0x80, 0xC3, 0xA9, 0xE6, 0xFF} {
		if isASCIILetter(b) {
			t.Errorf("isASCIILetter(0x%02x) = true, want false", b)
		}
		if isASCIIDigit(b) {
			t.Errorf("isASCIIDigit(0x%02x) = true, want false", b)
		}
	}
	// But a word CONTAINING one is a single token, which the word rule handles
	// with its own `>= 0x80` clause rather than through the predicates.
	if got := tokenizeWithin(t, "a\x80b"); len(got) != 1 {
		t.Errorf("a word containing a high byte split into %d tokens: %+v", len(got), got)
	}
}

// --- HighlightSQL ------------------------------------------------------------

// highlightOf runs the renderer and strips the styling, under the same watchdog
// as tokenizeWithin.
//
// HighlightSQL calls tokenizeSQL, so it inherits every hang: wrapping only the
// tokenizer leaves the renderer tests to spin for the whole harness budget. That
// is not a cosmetic difference — the leaked spinning goroutine saturates a core,
// which is how one hung mutant turned a 15ms package into a 93 second run.
func highlightRawWithin(t *testing.T, in string) string {
	t.Helper()
	type result struct{ out string }
	done := make(chan result, 1)
	styles := theme.Resolve("dark").Styles()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- result{out: ""}
			}
		}()
		done <- result{out: HighlightSQL(in, styles)}
	}()

	select {
	case r := <-done:
		return r.out
	case <-time.After(tokenizeBudget):
		t.Fatalf("HighlightSQL(%q) did not finish within %s: the tokenizer is not advancing the index", oneline(in), tokenizeBudget)
		return ""
	}
}

// highlightWithin is the same thing with the styling stripped, which is the form
// every text-comparison test wants.
func highlightWithin(t *testing.T, in string) string {
	t.Helper()
	return ansi.Strip(highlightRawWithin(t, in))
}

// Scenario: Fuera de las palabras clave, lo resaltado es el mismo texto.
//
// The renderer's whole contract is that it changes appearance and nothing else.
// The ONE exception is the keyword uppercase, which the next test covers, so this
// one uses input with no keywords in it. The user is typing into this, and a
// dropped or duplicated byte is data loss on screen even though the model is
// untouched.
func TestHighlightSQL_ChangesOnlyTheStyling(t *testing.T) {
	for _, in := range []string{
		"",
		"id",
		"'a''b', 1.5, x -- c\n/* b */ é日本",
		"users(id, name)\n",
		"a<=b, c<>d, e!=f, g%h",
		"café(日本).x",
		"\\",
	} {
		t.Run(oneline(in), func(t *testing.T) {
			if got := highlightWithin(t, in); got != in {
				t.Errorf("the rendered text is %q, want the input %q", got, in)
			}
		})
	}
}

// Scenario: Una palabra clave se pinta EN MAYÚSCULAS, y el resto no se toca.
//
// Uppercasing the keyword is a deliberate choice — it makes the reserved words
// pop out of a lowercase query — but it means the rendered text is NOT the input
// for keywords. Everything else has to come through byte for byte, so the two are
// asserted separately.
func TestHighlightSQL_KeywordsAreUppercasedAndNothingElseIs(t *testing.T) {
	got := highlightWithin(t, "select id from users")

	if !strings.Contains(got, "SELECT") {
		t.Errorf("the keyword was not uppercased: %q", got)
	}
	if !strings.Contains(got, "FROM") {
		t.Errorf("the keyword FROM was not uppercased: %q", got)
	}
	// The identifiers keep their case, which is the half that must not change.
	if !strings.Contains(got, "users") {
		t.Errorf("the identifier was altered: %q", got)
	}
	if !strings.Contains(got, "id") {
		t.Errorf("the column was altered: %q", got)
	}
	if strings.Contains(got, "select") {
		t.Errorf("the lowercase keyword survived: %q", got)
	}

	// Already-uppercase input is unchanged, so the operation is idempotent.
	if got := highlightWithin(t, "SELECT id"); got != "SELECT id" {
		t.Errorf("an already-uppercase keyword changed: %q", got)
	}
}

// Scenario: Una cadena no se reescribe nunca.
//
// A string literal is data, and uppercasing it would change what the query means.
// The uppercase rule is on the keyword branch only, and this is the test that
// keeps it there.
func TestHighlightSQL_StringLiteralsAreNeverRewritten(t *testing.T) {
	for _, tc := range []struct{ in, lit string }{
		{"select 'from where'", "'from where'"},
		{"'SELECT'", "'SELECT'"},
		{"'select'", "'select'"},
		{"'MixedCase'", "'MixedCase'"},
		{"'it''s'", "'it''s'"},
		{"'日本'", "'日本'"},
		{"x = 'Insert Into'", "'Insert Into'"},
	} {
		t.Run(oneline(tc.in), func(t *testing.T) {
			// The keyword outside the literal IS uppercased; the literal is not.
			// Asserting on the quoted form sidesteps the two rules colliding.
			if got := highlightWithin(t, tc.in); !strings.Contains(got, tc.lit) {
				t.Errorf("the literal %q was rewritten, or the whole input was: %q", tc.lit, got)
			}
			if got := highlightWithin(t, tc.in); strings.Contains(got, strings.ToUpper(tc.lit)) && tc.lit != strings.ToUpper(tc.lit) {
				t.Errorf("the literal %q was uppercased: %q", tc.lit, got)
			}
		})
	}
}

// Scenario: Una entrada vacía se renderiza como vacía.
//
// The early return, and the tokenizer's empty result, have to agree — otherwise the
// two paths produce different output for the same input. This is the observable
// form of that; the two are provably equivalent (the builder would produce "" too),
// which is why .mutation-allowlist records the guard as a no-op.
func TestHighlightSQL_EmptyInputRendersEmpty(t *testing.T) {
	if got := highlightWithin(t, ""); got != "" {
		t.Errorf("HighlightSQL(\"\") = %q, want empty", got)
	}
}

// Scenario: Una consulta entera sobrevive al renderizado con el mismo número de
// líneas.
//
// The editor lays the rendered text out in a box, so a lost or gained newline moves
// every line below it. Line count is the cheapest way to catch that and it is what
// the user sees first.
func TestHighlightSQL_TheLineCountIsUnchanged(t *testing.T) {
	for _, in := range []string{
		"SELECT 1",
		"SELECT 1\nFROM t",
		"SELECT 1\nFROM t\nWHERE a = 1\nORDER BY b",
		"-- a comment\n\n/* another\n   spanning */\nSELECT 1",
		"'multi\nline string'",
		"SELECT 'a\nb' FROM t",
	} {
		t.Run(oneline(in), func(t *testing.T) {
			got := highlightWithin(t, in)
			if strings.Count(got, "\n") != strings.Count(in, "\n") {
				t.Errorf("the rendered text has %d newlines, want the input's %d: %q",
					strings.Count(got, "\n"), strings.Count(in, "\n"), got)
			}
		})
	}
}

// Scenario: Cada tipo de token se pinta de una manera distinta.
//
// Seven token types, six styled branches and a default. If two branches rendered
// identically the highlighting would be useless, and if a branch were missing the
// switch would fall through to the default and the token would look unstyled.
func TestHighlightSQL_EachTokenTypeGetsItsOwnStyling(t *testing.T) {
	styles := theme.Resolve("dark").Styles()

	// One input per token type, and the style each must produce.
	for _, tc := range []struct {
		name  string
		in    string
		style string
	}{
		{"keyword", "select", styles.Primary.Render("SELECT")},
		{"string", "'x'", styles.Success.Render("'x'")},
		{"number", "42", styles.Warning.Render("42")},
		{"comment", "-- c", styles.TextMuted.Render("-- c")},
		{"function", "upper", styles.Info.Render("upper")},
		{"operator", "<=", styles.TextBright.Render("<=")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := highlightRawWithin(t, tc.in)
			if got != tc.style {
				t.Errorf("HighlightSQL(%q) rendered %q, want %q (the type's own style)", tc.in, got, tc.style)
			}
			// And it really is styled: the plain text is a different string.
			if got == tc.in {
				t.Errorf("HighlightSQL(%q) returned the input unstyled", tc.in)
			}
		})
	}

	// The default branch is unstyled, which is what makes punctuation and
	// identifiers different from the six above.
	for _, in := range []string{"users", "(", ",", ";", ".", "  "} {
		if got := highlightWithin(t, in); got != in {
			t.Errorf("HighlightSQL(%q) = %q, want it unstyled and unchanged", in, got)
		}
	}
}

func hasToken(toks []sqlToken, typ sqlTokenType, text string) bool {
	for _, tok := range toks {
		if tok.typ == typ && tok.text == text {
			return true
		}
	}
	return false
}

func oneline(s string) string {
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	s = strings.ReplaceAll(s, "\t", "\\t")
	if len(s) > 40 {
		s = s[:40] + "..."
	}
	return s
}
