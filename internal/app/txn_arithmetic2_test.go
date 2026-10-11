package app

import (
	"testing"
)

// Scenario: Los dos ARITHMETIC_BASE de txn.go:407, el escape de comilla doble de un
// literal entre comillas simples.
//
// ARITHMETIC_BASE internal/app/txn.go:407 col 10 (`i+1 < len(sql)` -> `i-1 < len(sql)`)
// and col 32 (`sql[i+1] == '\''` -> `sql[i-1] == '\''`) are both observable and killed
// below. They guard different things in the SAME statement:
//
//   - Col 10 is the bounds guard, and `i < len(sql)` already implies `i-1 < len(sql)`,
//     so the mutant guard is true at EVERY position — including `i == len(sql)-1`, where
//     the original is false and where `sql[i+1]` is one past the end. The guard's ONLY
//     job is to make that index legal; negating it turns "the quote closes the string"
//     into an index out of range. A string whose closing quote is the last byte of the
//     input is the smallest trigger, and the mutant panics on it.
//   - Col 32 is the doubled-quote escape test itself (`''` inside a literal), and looking
//     BACKWARD makes the closing quote of an EMPTY literal read as its own escape — the
//     exact twin of the identifier case killed by TestMaskNonCode_
//     EmptyQuotedIdentifierDoesNotSwallowTheStatement (txn_arithmetic_test.go, line 443),
//     one branch over.
//
// Proposed allowlist entry: NONE — both mutants are killed by tests below.

// Scenario: Una sentencia que TERMINA con la comilla de cierre no lee más allá de la
// entrada.
//
// Kills ARITHMETIC_BASE internal/app/txn.go:407 col 10. The closing quote at the last
// byte is not followed by anything, so the escape check must not look at `sql[i+1]` —
// the original guard `i+1 < len(sql)` keeps the index legal and the string closes
// cleanly; the mutant guard `i-1 < len(sql)` is always true, evaluates `sql[i+1]` and
// panics. The expected mask states the closed-string contract the panic skips past.
func TestMaskNonCode_StringClosingAtTheInputEndDoesNotReadPastIt(t *testing.T) {
	txnMaskAssert(t, `SELECT 'a'`, "SELECT"+txnSpaces(4))
}

// Scenario: Un literal de cadena vacío `”` no se traga el resto de la sentencia.
//
// Kills ARITHMETIC_BASE internal/app/txn.go:407 col 32. The escape check looks at the
// NEXT byte (`sql[i+1] == '\”`); the mutant looks at the PREVIOUS one, so at the second
// quote of `”` it "escapes" as if the pair had started one byte earlier, skips past the
// real closer and masks everything up to the next quote — of which there is none, so
// FROM and the table name disappear from the statement the keyword scanner reads.
func TestMaskNonCode_EmptyStringLiteralDoesNotSwallowTheStatement(t *testing.T) {
	txnMaskAssert(t, `SELECT '' FROM users`, "SELECT"+txnSpaces(4)+"FROM users")
}
