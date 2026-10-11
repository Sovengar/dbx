package app

import (
	"testing"
)

// Scenario: Los dos supervivientes ARITHMETIC_BASE de txn.go, uno matado y uno
// equivalente.
//
// ARITHMETIC_BASE internal/app/txn.go:443 (`sql[i+1] == '"'` -> `sql[i-1] == '"'`, the
// doubled-quote escape of a quoted identifier) is observable and killed below: the escape
// must be found by looking FORWARD from the current quote, and the mutant looks BACKWARD,
// sees the opening quote of an EMPTY identifier as its own escape and keeps masking past
// the real closer, so the code after `""` disappears from the masked statement.
//
// ARITHMETIC_BASE internal/app/txn.go:423 (`i+1 < len(sql)` -> `i-1 < len(sql)`, the
// guard of the backslash escape inside an E-string) has NO observer — proven equivalent,
// not merely untested:
//
//   - The mutant guard `i-1 < len(sql)` is true at EVERY position of the loop, because
//     `i < len(sql)` already implies `i-1 < len(sql)`. The original guard is false only
//     at `i == len(sql)-1`. So the two versions differ on exactly one input shape: a
//     backslash as the LAST byte of the statement.
//   - On that input the original skips the escape (`i++` → i == len), the mutant takes it
//     (`i += 2` → i == len+1), and then both do the same two things: blankRange clamps
//     `to > len(out)` to len(out) (txn.go:382 — the clamp's own test is
//     TestTheMaskerClampsBoundsThatAreOutOfRange), so blank(start, len) and
//     blank(start, len+1) write the identical bytes; and the outer loop terminates on
//     `i < len(sql)` for i == len and i == len+1 alike.
//   - Empirically: every string up to length 6 over {'"', ''', 'a', 'E', '\\', ' ', '-',
//     '\n', 'e'} masks byte-for-byte identically under the mutation (0 diffs), and the
//     full internal/app suite passes with the mutant applied.
//
// Proposed allowlist entry (do not add here): ARITHMETIC_BASE internal/app/txn.go:423

// Scenario: Un identificador citado vacío `""` no se traga el resto de la sentencia.
//
// Kills ARITHMETIC_BASE internal/app/txn.go:443. The lexer closes or escapes a quote by
// testing the NEXT byte (`i+1 < len(sql) && sql[i+1] == '"'`); the mutant tests the
// PREVIOUS one, so at the second quote of `""` it "escapes" as if the pair had started
// one byte earlier and swallows everything up to the next quote — of which there is none,
// so FROM and the table name are blanked out of the statement the keyword scanner reads.
func TestMaskNonCode_EmptyQuotedIdentifierDoesNotSwallowTheStatement(t *testing.T) {
	txnMaskAssert(t, `SELECT "" FROM users`, "SELECT"+txnSpaces(4)+"FROM users")
}
