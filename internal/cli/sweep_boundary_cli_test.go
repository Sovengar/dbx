package cli

import "testing"

// Scenario: a DSN that STARTS with an at sign — LastIndex returns 0, the exact
// boundary of `if at < 0`. The relaxed `at <= 0` takes the early return and
// hands the DSN back with no password spliced in, while the original treats
// position 0 as a real (empty) credentials section and inserts the password
// after the empty user.
func TestCliBoundaryDsnWithPasswordStartingWithAt(t *testing.T) {
	if got, want := dsnWithPassword("@localhost/db", "pw"), ":pw@localhost/db"; got != want {
		t.Errorf("dsnWithPassword(%q, %q) = %q, want %q", "@localhost/db", "pw", got, want)
	}
}

// Scenario: a DSN whose scheme starts at byte 0 — Index returns 0, the exact
// boundary of `if i >= 0` in the no-credentials branch. Relaxed to `i > 0`
// the splice is skipped and the password never enters the DSN.
func TestCliBoundaryDsnWithPasswordSchemeAtByteZero(t *testing.T) {
	if got, want := dsnWithPassword("://localhost/db", "pw"), "://:pw@localhost/db"; got != want {
		t.Errorf("dsnWithPassword(%q, %q) = %q, want %q", "://localhost/db", "pw", got, want)
	}
}

// Scenario: credentials that themselves start with "://" at byte 0 — the same
// boundary one level down, in the branch that inspects the part before the at
// sign. Relaxed to `i > 0` the scheme is not split off, the colon inside
// "://" makes the whole credentials look like user:password, and the function
// returns the DSN untouched instead of adding the password.
func TestCliBoundaryDsnWithPasswordCredsWithSchemeAtByteZero(t *testing.T) {
	if got, want := dsnWithPassword("://user@h/d", "x"), "://user:x@h/d"; got != want {
		t.Errorf("dsnWithPassword(%q, %q) = %q, want %q", "://user@h/d", "x", got, want)
	}
}
