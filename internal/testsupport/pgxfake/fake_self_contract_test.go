package pgxfake

// Scenario: El contrato del propio fake, que es codigo de PRODUCCION.
//
// This package is not a test helper in the ordinary sense: internal/drivers/postgres's
// tests and internal/app's tests both decide what is true by believing it. A fake that
// answers a query the caller did not script, or that swallows a scan error, makes the test
// that depends on it pass for the wrong reason — and the failure surfaces in production,
// as a wrong answer about a database, with the test still green.
//
// So it has its own tests, in its own package, and that is why the cross-package uses do
// not count toward coverage here: internal/app exercising pgxfake proves something about
// the app, not about whether the fake lies.
//
// The parts pinned below are the ones a caller can notice:
//
//	Exec — the app's write path, which had no way to be told a write was refused
//	Asked/AskedCount — the only way to assert that error handling CHANGED the queries
//	Scan — the arity check, and the destinations a fake is expected to fill

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// Exec answers, and records. The tag is not inspected by any caller, so what matters is
// that a refused write is REFUSABLE — the app's arms for that were unreachable before.
func TestExecAnswersAndCanRefuse(t *testing.T) {
	t.Run("a write succeeds and is recorded", func(t *testing.T) {
		c := New()
		tag, err := c.Exec(context.Background(), "DELETE FROM orders WHERE id = $1", 7)
		if err != nil {
			t.Fatalf("Exec: %v", err)
		}
		if tag.RowsAffected() != 1 {
			t.Errorf("the tag reports %d rows, want 1", tag.RowsAffected())
		}
		if len(c.Execs) != 1 || c.Execs[0] != "DELETE FROM orders WHERE id = $1" {
			t.Errorf("Exec recorded %q", c.Execs)
		}
		// The bound argument is recorded too, and this is not bookkeeping for its own sake:
		// the app's write path loops over arguments and the question "one statement per
		// argument or one statement with all of them" is only answerable from here.
		if len(c.Args) != 1 || len(c.Args[0]) != 1 || c.Args[0][0] != 7 {
			t.Errorf("Exec recorded the arguments %v", c.Args)
		}
	})

	t.Run("a refused write returns the error and still records it", func(t *testing.T) {
		c := New()
		c.ExecErr = errors.New("permission denied")

		tag, err := c.Exec(context.Background(), "DROP TABLE orders")
		if err == nil {
			t.Fatal("a refused write reported success")
		}
		if !errors.Is(err, c.ExecErr) {
			t.Errorf("Exec returned %v, want the configured error", err)
		}
		if tag.RowsAffected() != 0 {
			t.Errorf("a refused write reported %d rows affected", tag.RowsAffected())
		}
		// Recorded even though it failed: a test that asserts "the app tried exactly once"
		// must not be satisfied by an app that never tried.
		if len(c.Execs) != 1 {
			t.Errorf("a refused write recorded %d statements", len(c.Execs))
		}
	})
}

// Asked and AskedCount exist because the query log is the only evidence of what error
// handling DID, as opposed to what it returned. Both are substring queries over the log.
func TestTheQueryLogCanBeSearchedBySubstring(t *testing.T) {
	c := New()
	ctx := context.Background()
	// A step per query, because an unmatched query is an ERROR — which is the fake's most
	// important property and the reason a fixture has to declare everything it expects to
	// be asked. Answering an undeclared query would let a test pass against a call the
	// production code never makes.
	c.Steps = []Step{
		{Match: "pg_indexes"},
		{Match: "information_schema.columns"},
		{Match: "SELECT 1"},
	}

	for _, sql := range []string{
		"SELECT * FROM pg_indexes WHERE schemaname = $1",
		"SELECT * FROM information_schema.columns",
		"SELECT 1",
	} {
		if _, err := c.Query(ctx, sql); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}

	t.Run("Asked finds what ran", func(t *testing.T) {
		if !c.Asked("pg_indexes") {
			t.Error("Asked did not find a query that ran")
		}
		if !c.Asked("information_schema.columns") {
			t.Error("Asked did not find the second query")
		}
	})

	t.Run("Asked reports what did not", func(t *testing.T) {
		if c.Asked("pg_constraint") {
			t.Error("Asked found a query that never ran")
		}
		// An empty substring matches everything, which is a trap worth naming rather
		// than defending: a fixture that passes "" would be asking "did anything run at
		// all", which is what Unmatched is for.
		if !c.Asked("") {
			t.Error("an empty substring found nothing")
		}
	})

	t.Run("AskedCount counts, for the retry question", func(t *testing.T) {
		if got := c.AskedCount("pg_indexes"); got != 1 {
			t.Errorf("AskedCount found %d occurrences, want 1", got)
		}
		if got := c.AskedCount("pg_constraint"); got != 0 {
			t.Errorf("AskedCount found %d occurrences of a query that never ran", got)
		}
		// Every query, counted once each, plus the two the Steps matched.
		if got, want := c.AskedCount("SELECT"), 3; got != want {
			t.Errorf("AskedCount(SELECT) is %d, want %d", got, want)
		}
	})

	t.Run("an unasked connection answers false and zero", func(t *testing.T) {
		empty := New()
		if empty.Asked("anything") {
			t.Error("a connection that ran nothing claims it ran a query")
		}
		if got := empty.AskedCount("anything"); got != 0 {
			t.Errorf("a connection that ran nothing reports %d queries", got)
		}
	})
}

// Scan's arity check. A fake with too few values scripted must SAY SO rather than leave the
// destination at its zero value, because a zero value reads as a real answer: a caller
// checking "did the row have a name" would be told "no" and conclude the row was empty
// rather than concluding the fixture was.
func TestScanRefusesMoreDestinationsThanValues(t *testing.T) {
	r := row{values: []any{"only-one"}}

	var a, b string
	err := r.Scan(&a, &b)
	if err == nil {
		t.Fatal("scanning two destinations from one value reported success")
	}
	// The message names both counts, because "scan failed" would send a reader looking at
	// the caller instead of at the fixture.
	if !strings.Contains(err.Error(), "2 destinations") || !strings.Contains(err.Error(), "1 values") {
		t.Errorf("the error does not name both counts: %v", err)
	}
	// The destinations are untouched — the arity check happens BEFORE any assignment, so
	// a partial scan cannot leave a caller with half a row.
	if a != "" || b != "" {
		t.Errorf("the failed scan wrote %q and %q", a, b)
	}
}

// The nullable destinations the loaders actually use. Each of these has a nil branch that
// a real NULL takes, and each is a case where a fake that answered "" instead of nil would
// make the caller print an empty string where the database said nothing at all.
func TestTheNullableDestinationsTakeNilAsAbsence(t *testing.T) {
	t.Run("a nil *string is nil, an empty one is a pointer", func(t *testing.T) {
		var absent, present *string

		if err := assign(&absent, nil); err != nil {
			t.Fatalf("nil into **string: %v", err)
		}
		if absent != nil {
			t.Errorf("a NULL string became %q", *absent)
		}

		if err := assign(&present, "hello"); err != nil {
			t.Fatalf("text into **string: %v", err)
		}
		if present == nil || *present != "hello" {
			t.Errorf("a real string became %v", present)
		}

		// The empty string is the case that has to be told apart from NULL, because both
		// arrive as "nothing to show" and only one of them means "show nothing".
		var empty *string
		if err := assign(&empty, ""); err != nil {
			t.Fatalf("empty into **string: %v", err)
		}
		if empty != nil {
			t.Errorf("the empty string became %q, want nil", *empty)
		}
	})

	t.Run("a nil time is nil", func(t *testing.T) {
		var absent *time.Time
		if err := assign(&absent, nil); err != nil {
			t.Fatalf("nil into *time.Time: %v", err)
		}
		if absent != nil {
			t.Errorf("a NULL timestamp became %v", absent)
		}

		when := time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)
		var present, presentPtr *time.Time
		if err := assign(&present, when); err != nil {
			t.Fatalf("time into *time.Time: %v", err)
		}
		if present == nil || !present.Equal(when) {
			t.Errorf("a timestamp became %v", present)
		}
		if err := assign(&presentPtr, when); err != nil {
			t.Fatalf("time into **time.Time: %v", err)
		}
		if presentPtr == nil || !presentPtr.Equal(when) {
			t.Errorf("a timestamp became %v", presentPtr)
		}
	})

	t.Run("a nil list is nil, a comma list splits", func(t *testing.T) {
		var absent, split, whole []string

		if err := assign(&absent, nil); err != nil {
			t.Fatalf("nil into *[]string: %v", err)
		}
		if absent != nil {
			t.Errorf("a NULL list became %v", absent)
		}
		if err := assign(&split, "a,b,c"); err != nil {
			t.Fatalf("comma list: %v", err)
		}
		if len(split) != 3 || split[0] != "a" || split[2] != "c" {
			t.Errorf("a comma list became %v", split)
		}
		if err := assign(&whole, []string{"x", "y"}); err != nil {
			t.Fatalf("whole list: %v", err)
		}
		if len(whole) != 2 {
			t.Errorf("a list became %v", whole)
		}
	})

	t.Run("what it cannot do, it says", func(t *testing.T) {
		var target []string
		if err := assign(&target, 42); err == nil {
			t.Error("scanning a number into a string list reported success")
		}
		var anything any
		if err := assign(&anything, "x"); err == nil {
			t.Error("scanning into a destination of no known type reported success")
		}
		// A bool from its string form, because the loader reads these as text.
		var flag bool
		if err := assign(&flag, "true"); err != nil || !flag {
			t.Errorf("the string true became %t (%v)", flag, err)
		}
		if err := assign(&flag, "false"); err != nil || flag {
			t.Errorf("the string false became %t (%v)", flag, err)
		}
	})
}
