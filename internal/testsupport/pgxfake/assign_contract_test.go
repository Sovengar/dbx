package pgxfake

// Scenario: El contrato de `assign`, que es lo que hace fiable el resto de la suite.
//
// pgxfake stands in for a live connection in tests across four other packages, so its behaviour
// when a scan goes wrong is not a detail — it is what tells a fixture author that their fixture
// is wrong. Two things have to hold:
//
//   - every destination type the drivers actually scan into is supported, and
//   - every one of them SAYS NO when the value does not fit, instead of storing a zero.
//
// The second is the important half. An assign that quietly wrote 0 into an int64 when the
// scripted value was a word would make a schema load "succeed" with wrong data, and the test
// would assert on the wrong thing and pass.
//
// The arity check is the third: pgx refuses a Scan whose destinations outnumber the scripted
// values, and a fake that did not would answer with a zero for the extra column — turning "you
// asked for three columns and the query returned two" into a plausible row.
//
// The cases are written out one by one rather than as a table, because each one's assertion is
// about a DIFFERENT pointer and a table would need a reflection helper to get at them. The
// repetition is the point: this is the list of types the drivers scan into, and it should read
// like one.

import (
	"strings"
	"testing"
	"time"
)

func TestAssignAnswersEveryDestinationTheDriversScanInto(t *testing.T) {
	now := time.Date(2024, 3, 14, 15, 9, 26, 0, time.UTC)

	t.Run("a string", func(t *testing.T) {
		var got string
		if err := assign(&got, "hello"); err != nil {
			t.Fatal(err)
		}
		if got != "hello" {
			t.Errorf("got %q, want %q", got, "hello")
		}
	})

	t.Run("a nullable string that is null", func(t *testing.T) {
		// pgx hands a **string for a nullable column and the value arrives as nil.
		var got *string
		if err := assign(&got, nil); err != nil {
			t.Fatal(err)
		}
		if got != nil {
			t.Errorf("got %q, want nil", *got)
		}
	})

	t.Run("a nullable string with a value", func(t *testing.T) {
		var got *string
		if err := assign(&got, "not null"); err != nil {
			t.Fatal(err)
		}
		if got == nil || *got != "not null" {
			t.Errorf("got %v, want a pointer to %q", got, "not null")
		}
	})

	t.Run("a string slice", func(t *testing.T) {
		var got []string
		if err := assign(&got, []string{"a", "b"}); err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || got[1] != "b" {
			t.Errorf("got %v, want two elements ending in b", got)
		}
	})

	t.Run("a string slice written as a comma-separated string", func(t *testing.T) {
		// A fixture convenience, and one worth pinning because it is invisible: a string in a
		// column the driver scans into a slice is SPLIT, not refused. That is what lets a
		// fixture write `"a,b,c"` where the driver expects three elements.
		var got []string
		if err := assign(&got, "a,b,c"); err != nil {
			t.Fatal(err)
		}
		if len(got) != 3 || got[0] != "a" || got[2] != "c" {
			t.Errorf("got %v, want three comma-separated elements", got)
		}
	})

	t.Run("a string slice that is null", func(t *testing.T) {
		var got []string
		if err := assign(&got, nil); err != nil {
			t.Fatal(err)
		}
		if got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})

	t.Run("a time", func(t *testing.T) {
		var got time.Time
		if err := assign(&got, now); err != nil {
			t.Fatal(err)
		}
		if !got.Equal(now) {
			t.Errorf("got %v, want %v", got, now)
		}
	})

	t.Run("a time that is null", func(t *testing.T) {
		// A nil into a NON-pointer time is left as the zero value rather than panicking: a
		// nullable timestamp scanned into a plain time.Time is how pgx reports NULL to a
		// caller that did not ask for the difference.
		var got time.Time
		if err := assign(&got, nil); err != nil {
			t.Fatal(err)
		}
		if !got.IsZero() {
			t.Errorf("got %v, want the zero time", got)
		}
	})

	t.Run("a nullable time with a value", func(t *testing.T) {
		// And this is the shape GetTableOverview actually uses: the field is a *time.Time and
		// the scan gets a **time.Time.
		var got *time.Time
		if err := assign(&got, now); err != nil {
			t.Fatal(err)
		}
		if got == nil || !got.Equal(now) {
			t.Errorf("got %v, want a pointer to %v", got, now)
		}
	})

	t.Run("a nullable time that is null", func(t *testing.T) {
		var got *time.Time
		if err := assign(&got, nil); err != nil {
			t.Fatal(err)
		}
		if got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})

	t.Run("an int from an int64", func(t *testing.T) {
		// Postgres has one integer type and the fake answers in int64, so this narrowing is
		// what every integer fixture goes through.
		var got int
		if err := assign(&got, int64(7)); err != nil {
			t.Fatal(err)
		}
		if got != 7 {
			t.Errorf("got %d, want 7", got)
		}
	})

	t.Run("an int64", func(t *testing.T) {
		var got int64
		if err := assign(&got, int64(9)); err != nil {
			t.Fatal(err)
		}
		if got != 9 {
			t.Errorf("got %d, want 9", got)
		}
	})

	t.Run("a bool, written three ways", func(t *testing.T) {
		// Postgres booleans arrive as "t" from some drivers and "true" from others, and a
		// fixture written with the other one would silently be false — which is the kind of
		// wrong that only shows up as a missing row three assertions later.
		// "t" and "T" are what Postgres PRINTS, and before the fix only "true" counted — so a
		// fixture copied out of psql was silently false. Postgres's wider INPUT spellings
		// ("yes", "on", "1") stay false on purpose, which the existing contract test pins.
		for _, value := range []any{true, "true", "TRUE", "t", "T"} {
			var got bool
			if err := assign(&got, value); err != nil {
				t.Fatalf("%#v: %v", value, err)
			}
			if !got {
				t.Errorf("%#v scanned as false", value)
			}
		}
		for _, value := range []any{false, "false", "FALSE", "f", "F", "yes", "on", "1"} {
			var got bool
			if err := assign(&got, value); err != nil {
				t.Fatalf("%#v: %v", value, err)
			}
			if got {
				t.Errorf("%#v scanned as true", value)
			}
		}
	})
}

// The refusals. Every one of them is a fixture that is wrong, and the whole value of a fake is
// that it says so instead of inventing an answer.
//
// This is the half that stops a broken fixture from passing: an assign that stored a zero
// instead of refusing would make every case below answer successfully with wrong data, and the
// fixture author would never learn about it.
func TestAssignRefusesAValueThatDoesNotFit(t *testing.T) {
	t.Run("a string slice from something that is not one", func(t *testing.T) {
		// A NUMBER, because a STRING is accepted and split — which surprised the first version
		// of this case, and is why the case above exists.
		var got []string
		err := assign(&got, int64(3))

		requireNames(t, err, "[]string")
		if got != nil {
			t.Errorf("a refused scan still wrote %v", got)
		}
	})

	t.Run("a time from a string that is not a time", func(t *testing.T) {
		var got time.Time
		err := assign(&got, "the day before yesterday")

		requireNames(t, err, "time")
		if !got.IsZero() {
			t.Errorf("a refused scan still wrote %v", got)
		}
	})

	t.Run("a nullable time from a string that is not a time", func(t *testing.T) {
		// The pointer form, which is the one a nullable timestamp actually uses — and the
		// shape where a "write zero and carry on" would be invisible, because nil and the
		// zero time are both plausible answers to "the value was not a time".
		var got *time.Time
		err := assign(&got, "not a timestamp either")

		requireNames(t, err, "time")
		if got != nil {
			t.Errorf("a refused scan still wrote %v", got)
		}
	})

	t.Run("an int from a word", func(t *testing.T) {
		var got int
		err := assign(&got, "seven")

		// "number", not "int": the message names the SHAPE of the problem rather than the
		// destination, which is the right way round — the reader has the value in front of
		// them and wants to know it is not one.
		requireNames(t, err, "number")
		if got != 0 {
			t.Errorf("a refused scan still wrote %d", got)
		}
	})

	t.Run("an int64 from a word", func(t *testing.T) {
		var got int64
		err := assign(&got, "nine")

		requireNames(t, err, "number")
		if got != 0 {
			t.Errorf("a refused scan still wrote %d", got)
		}
	})

	t.Run("a destination type the fake does not handle", func(t *testing.T) {
		// The catch-all. A fake that accepted it would silently do nothing and the caller
		// would read a zero value — the worst outcome, because there is nothing to notice.
		got := 1.5
		err := assign(&got, 2.5)

		requireNames(t, err, "float64")
		if got != 1.5 {
			t.Errorf("a refused scan still wrote %v", got)
		}
	})
}

// The arity check, from the outside: a Scan with more destinations than the scripted row.
//
// pgx refuses this, and so must the fake — otherwise "the query returned two columns and the
// code scans three" reads as a row with a zero in the third field, which is a plausible answer
// and a completely wrong one.
func TestScanRefusesMoreDestinationsThanTheRowHas(t *testing.T) {
	r := row{values: []any{"one", int64(2)}}

	var a, b, c string
	err := r.Scan(&a, &b, &c)
	if err == nil {
		t.Fatal("scanning three destinations into a two-value row reported success")
	}
	if !strings.Contains(err.Error(), "3 destinations") || !strings.Contains(err.Error(), "2 values") {
		t.Errorf("the error is %q, want it to give both counts", err)
	}
	// And nothing was written, so the caller cannot read a half-scanned row.
	if a != "" || b != "" || c != "" {
		t.Errorf("a refused scan still wrote %q %q %q", a, b, c)
	}

	t.Run("and fewer destinations than the row has is fine", func(t *testing.T) {
		// The other direction, and it is legal: a caller that only wants the first two columns
		// of a three-column result is asking a real question of the database, and the fake has
		// to answer it the same way.
		var first, second string
		if err := r.Scan(&first, &second); err != nil {
			t.Fatalf("scanning two of three: %v", err)
		}
		if first != "one" || second != "2" {
			t.Errorf("the scan gave %q %q, want \"one\" and \"2\"", first, second)
		}
	})

	t.Run("and a failed assignment stops the scan", func(t *testing.T) {
		// Half a scan is worse than none: the first destination is written, the second fails,
		// and the caller sees an error plus a value it should not trust. pgx behaves the same
		// way, which is why the fake reports the failure rather than swallowing it.
		// A value that cannot be assigned at all — a word where a number is expected. The row
		// above had int64(2), which assign handles, so my first version of this case passed
		// because the scan SUCCEEDED.
		broken := row{values: []any{"one", "seven"}}

		var first string
		var second int
		if err := broken.Scan(&first, &second); err == nil {
			t.Fatal("a scan that cannot assign reported success")
		}
		// The first destination WAS written before the failure, which is pgx's behaviour too:
		// a partial scan plus an error, rather than an error and an untouched row.
		if first != "one" {
			t.Errorf("the first destination is %q, want the value that was assigned before the failure", first)
		}
	})
}

// requireNames asserts that a refusal happened AND that its message names what it could not
// handle — because the reader is a fixture author looking at a test failure, and "cannot scan"
// alone does not tell them which column or which type.
func requireNames(t *testing.T, err error, names ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("a wrong fixture was accepted; a refusal was expected naming %v", names)
	}
	for _, name := range names {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("the error %q does not name %q", err, name)
		}
	}
}
