package pgxfake

// Scenario: El fake se testea a si mismo, porque un fake equivocado hace pasar a otro test
// por la razon equivocada.
//
// Every suite in internal/drivers/postgres runs its query-building and row-scanning tests
// against this. If assign hands back a zero value where pgx would have handed back a
// timestamp, or Steps resolves the wrong step, the driver's tests still pass — they are
// asserting against the fake, and the fake is what is wrong. That failure only shows up
// against a real server, which is the thing this package exists to avoid needing.
//
// So the contract is the same one pgx has, asserted directly:
//
//	a NULL leaves the destination at its ZERO value, and does not error
//	a value it cannot convert is an ERROR, never a silent zero
//	Steps resolve in order, and Sequential counts only what no Step matched
//
// The third is the subtle one. Sequential's counter skips queries a Step answered, so a
// fixture does not have to count its own setup queries — and if that arithmetic were
// wrong, every fixture after the first step would drift by one and read the wrong answer.

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

var errBoom = errors.New("errBoom")

func TestTheStepsResolveInOrderAndTheFirstMatchWins(t *testing.T) {
	c := &Conn{Steps: []Step{
		{Match: "FROM users", Result: Result{Rows: [][]any{{"users"}}}},
		{Match: "FROM", Result: Result{Rows: [][]any{{"wrong"}}}},
	}}

	r, err := c.Query(context.Background(), "SELECT * FROM users")
	if err != nil {
		t.Fatalf("the query failed: %v", err)
	}
	if !r.Next() {
		t.Fatal("no rows")
	}
	vals, _ := r.Values()
	if len(vals) != 1 || vals[0] != "users" {
		t.Errorf("the first matching step did not win: %v", vals)
	}
	if c.Unmatched() != 0 {
		t.Errorf("a matched query counted as unmatched (%d)", c.Unmatched())
	}
}

func TestAStepWithNoMatchTextIsACatchAll(t *testing.T) {
	// The shape every fixture ends with, and the one that makes a catch-all placed LAST
	// work: with a Match it must be skipped for every query that does not contain it.
	c := &Conn{Steps: []Step{
		{Match: "pg_index", Result: Result{Rows: [][]any{{"idx"}}}},
		{Result: Result{Rows: [][]any{{"fallback"}}}},
	}}

	r, err := c.Query(context.Background(), "SELECT something_else FROM t")
	if err != nil {
		t.Fatalf("the query failed: %v", err)
	}
	r.Next()
	if vals, _ := r.Values(); vals[0] != "fallback" {
		t.Errorf("the catch-all did not answer: %v", vals)
	}
}

func TestArgContainsNarrowsAStepToOneSchema(t *testing.T) {
	// Every schema query passes the schema as $1, and this is what lets one fixture
	// answer public and fail audit from the same connection. It is the reason the whole
	// argument is checked by SUBSTRING rather than equality — a fixture writes "public"
	// and the driver may pass "public" as part of something longer.
	c := &Conn{Steps: []Step{
		{Match: "schema", ArgContains: "audit", Err: errBoom},
		{Match: "schema", Result: Result{Rows: [][]any{{"ok"}}}},
	}}

	if _, err := c.Query(context.Background(), "SELECT schema", "public"); err != nil {
		t.Fatalf("the public query failed: %v", err)
	}
	if _, err := c.Query(context.Background(), "SELECT schema", "audit"); !errors.Is(err, errBoom) {
		t.Errorf("the audit query returned %v, want errBoom", err)
	}
}

func TestSequentialCountsOnlyWhatNoStepMatched(t *testing.T) {
	// The arithmetic, and it is the easiest thing in the file to get wrong. Sequential's
	// index counts queries that FELL THROUGH every Step, so a fixture with one step and
	// two sequential answers does not have to know how many times its own step fired.
	c := &Conn{
		Steps: []Step{
			{Match: "setup", Result: Result{Rows: [][]any{{"s"}}}},
		},
		Sequential: []Result{
			{Rows: [][]any{{"first"}}},
			{Rows: [][]any{{"second"}}},
		},
	}

	for range 3 {
		if _, err := c.Query(context.Background(), "SELECT setup"); err != nil {
			t.Fatalf("a matched query failed: %v", err)
		}
	}
	for _, want := range []string{"first", "second"} {
		r, err := c.Query(context.Background(), "SELECT something else")
		if err != nil {
			t.Fatalf("an unmatched query failed: %v", err)
		}
		r.Next()
		if vals, _ := r.Values(); vals[0] != want {
			t.Errorf("the sequential answer is %v, want %q — the counter drifted past the matched queries", vals, want)
		}
	}
}

func TestAQueryNoStepAnswersIsAnErrorNamingTheQuery(t *testing.T) {
	// Not an empty result. An empty result reads as "the table has no rows", which is a
	// completely different thing, and a test that mistook one for the other would pass.
	c := &Conn{}

	_, err := c.Query(context.Background(), "SELECT * FROM nowhere")
	if err == nil {
		t.Fatal("an unanswered query reported success")
	}
	if !strings.Contains(err.Error(), "nowhere") {
		t.Errorf("the error %q does not name the query", err)
	}
	// The SQL is collapsed to one line, so a multi-line statement does not put four
	// newlines into a %v that lands in a test failure message.
	if strings.Contains(err.Error(), "\n") {
		t.Errorf("the error spans several lines: %q", err)
	}
}

// Scanning. The whole of pgx's dest conversion, and the rule that matters is the last one:
// a value it cannot convert is an ERROR, never a silent zero. A silent zero is how a
// driver test ends up asserting that a timestamp is the zero time.
func TestScanningAssignsEveryTypeTheDriverUses(t *testing.T) {
	when := time.Date(2024, 3, 1, 12, 30, 0, 0, time.UTC)

	for _, tc := range []struct {
		name  string
		value any
		dest  func() any
		want  any
	}{
		{"a string", "hello", func() any { return new(string) }, "hello"},
		{"a number as a string", 42, func() any { return new(string) }, "42"},
		{"an int", 7, func() any { return new(int) }, 7},
		{"an int32", int32(7), func() any { return new(int) }, 7},
		{"an int64", int64(7), func() any { return new(int) }, 7},
		{"a uint32", uint32(7), func() any { return new(int) }, 7},
		{"a float64", 7.9, func() any { return new(int) }, 7},
		{"a numeric string", "8", func() any { return new(int) }, 8},
		{"an int into *int64", 9, func() any { return new(int64) }, int64(9)},
		{"a bool from true", "true", func() any { return new(bool) }, true},
		{"a bool from false", "false", func() any { return new(bool) }, false},
		{"a bool from anything else", "yes", func() any { return new(bool) }, false},
		{"a []string from a slice", []string{"a", "b"}, func() any { return new([]string) }, []string{"a", "b"}},
		{"a []string from a comma list", "a,b", func() any { return new([]string) }, []string{"a", "b"}},
		{"a time", when, func() any { return new(time.Time) }, when},
		{"a discard destination", "anything", func() any { return new(struct{}) }, struct{}{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dest := tc.dest()
			if err := assign(dest, tc.value); err != nil {
				t.Fatalf("assign(%T, %v) failed: %v", dest, tc.value, err)
			}
			// Compared through sameValueOf, not `!=`: comparing two interfaces holding
			// slices PANICS, and the first version of this table did exactly that on the
			// []string row. A panic in a table of conversions is also a confusing way to
			// learn that one row is not comparable.
			if got := reflectValueOf(dest); !sameValue(got, tc.want) {
				t.Errorf("assign gave %#v, want %#v", got, tc.want)
			}
		})
	}

	t.Run("a NULL leaves the destination at its zero value without an error", func(t *testing.T) {
		// pgx's behaviour, and the driver's callers depend on it: TableOverview scans a
		// nullable timestamp and checks the pointer, and a NULL that produced an error
		// would make every nullable column look like a broken query.
		s := "untouched"
		tm := when
		list := []string{"kept"}
		pp := new(string)
		*pp = "was set"

		for _, dest := range []any{&s, &tm, &list, pp} {
			if err := assign(dest, nil); err != nil {
				t.Errorf("assign(%T, nil) returned %v, want no error", dest, err)
			}
		}
		if s != "untouched" {
			t.Errorf("a NULL overwrote a string destination: %q", s)
		}
		if !tm.Equal(when) {
			t.Errorf("a NULL overwrote a time destination: %v", tm)
		}
		if pp != nil && *pp != "was set" {
			t.Errorf("a NULL cleared a **string that was set: %q", *pp)
		}
	})

	t.Run("an EMPTY string into **string becomes nil, because that is how NULL arrives", func(t *testing.T) {
		// The one asymmetry: a NULL leaves the destination alone, but an empty STRING
		// becomes nil. Postgres reports a NULL as an empty value in some text paths, and
		// the driver checks the pointer to decide whether a column exists.
		var p *string
		if err := assign(&p, ""); err != nil {
			t.Fatalf("assign returned %v", err)
		}
		if p != nil {
			t.Errorf("an empty string left the pointer at %q, want nil", *p)
		}
	})

	t.Run("an unconvertible value is an ERROR and never a zero", func(t *testing.T) {
		// The property the whole package rests on. A silent zero here would make a driver
		// test assert something true about the fake and false about PostgreSQL.
		var tm time.Time
		if err := assign(&tm, "not a timestamp at all"); err == nil {
			t.Errorf("a non-timestamp scanned into a time without an error, giving %v", tm)
		}
		var n int
		if err := assign(&n, "not a number"); err == nil {
			t.Errorf("a non-number scanned into an int without an error, giving %d", n)
		}
		var list []string
		if err := assign(&list, 42); err == nil {
			t.Errorf("a number scanned into *[]string without an error, giving %v", list)
		}
		var unsupported struct{ X int }
		if err := assign(&unsupported, "anything"); err == nil {
			t.Error("an unsupported destination type scanned without an error")
		}
		if err := assign(&tm, []int{1}); err == nil {
			t.Error("a slice scanned into a time without an error")
		}
	})
}

// asTime and asNumber are what make a fixture writable as {4, 2, 3, 1} instead of
// {"4", "2", "3", "1"}, so every accepted spelling is part of the interface.
func TestTheCoercionsAcceptEverySpellingAFixtureWouldWrite(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want time.Time
	}{
		{"2024-03-01 12:30:00", time.Date(2024, 3, 1, 12, 30, 0, 0, time.UTC)},
		{"2024-03-01T12:30:00Z", time.Date(2024, 3, 1, 12, 30, 0, 0, time.UTC)},
	} {
		t.Run(tc.in, func(t *testing.T) {
			got, err := asTime(tc.in)
			if err != nil {
				t.Fatalf("asTime(%q) failed: %v", tc.in, err)
			}
			if !got.Equal(tc.want) {
				t.Errorf("asTime(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}

	t.Run("a timestamp with fractional seconds parses", func(t *testing.T) {
		if _, err := asTime("2024-03-01 12:30:00.123456+00"); err != nil {
			t.Errorf("a fractional-second timestamp did not parse: %v", err)
		}
	})

	for _, tc := range []struct {
		in   any
		want int64
	}{
		{int(5), 5}, {int32(5), 5}, {int64(5), 5}, {uint32(5), 5},
		{uint64(5), 5}, {float64(5.9), 5}, {"5", 5},
	} {
		got, err := asNumber(tc.in)
		if err != nil {
			t.Errorf("asNumber(%T) failed: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("asNumber(%T(%v)) = %d, want %d", tc.in, tc.in, got, tc.want)
		}
	}

	for _, bad := range []any{"abc", []int{1}, nil, struct{}{}} {
		if _, err := asNumber(bad); err == nil {
			t.Errorf("asNumber(%T) returned no error", bad)
		}
	}
	for _, bad := range []any{"nope", 5, nil} {
		if _, err := asTime(bad); err == nil {
			t.Errorf("asTime(%T) returned no error", bad)
		}
	}
}

// The row iteration. Err is what ExecuteQuery wraps as a mid-iteration failure, which is
// the one thing a real server can reproduce and this package exists for.
func TestTheIterationStopsWithTheScriptedError(t *testing.T) {
	c := &Conn{Sequential: []Result{{
		Rows:    [][]any{{"a"}, {"b"}},
		IterErr: errBoom,
	}}}

	r, err := c.Query(context.Background(), "SELECT x")
	if err != nil {
		t.Fatalf("the query failed: %v", err)
	}

	got := 0
	for r.Next() {
		got++
	}
	if !errors.Is(r.Err(), errBoom) {
		t.Errorf("the iteration ended with %v, want errBoom", r.Err())
	}
	// And it stops at the scripted end, not at len(Rows) + 1.
	if got > 2 {
		t.Errorf("the iteration produced %d rows, want at most the two scripted", got)
	}
}

func TestTheRowsScanRefusesMoreDestinationsThanColumns(t *testing.T) {
	// A caller asking for more columns than the result has is a bug in the QUERY, and
	// saying so is the point: a fake that filled the extra destinations with zeros would
	// let a SELECT that forgot a column pass.
	c := &Conn{Sequential: []Result{{Rows: [][]any{{"only"}}}}}

	r, _ := c.Query(context.Background(), "SELECT x")
	r.Next()

	var a, b string
	if err := r.Scan(&a, &b); err == nil {
		t.Error("scanning two destinations into a one-column row reported success")
	}
	if err := r.Scan(&a); err != nil {
		t.Errorf("scanning one destination failed: %v", err)
	}
	if a != "only" {
		t.Errorf("the value is %q, want %q", a, "only")
	}
}

func TestTheScalarAccessors(t *testing.T) {
	ctx := context.Background()

	t.Run("QueryRow answers from the first row of a matched step", func(t *testing.T) {
		// One script drives both APIs, which is what lets a fixture say "this scalar read
		// fails, that one succeeds" without two separate fields.
		c := &Conn{Steps: []Step{
			{Match: "count", Result: Result{Rows: [][]any{{int64(12)}}}},
		}}

		var n int64
		if err := c.QueryRow(ctx, "SELECT count(*)").Scan(&n); err != nil {
			t.Fatalf("the scan failed: %v", err)
		}
		if n != 12 {
			t.Errorf("count = %d, want 12", n)
		}
	})

	t.Run("QueryRow falls back to Values, then to RowName", func(t *testing.T) {
		// The order, and each step is a different way a fixture answers a scalar.
		c := &Conn{Steps: []Step{
			{Match: "from-values", Result: Result{Values: []any{"v"}}},
		}, RowValues: []any{"fallback"}, RowName: "dbname"}

		var s string
		if err := c.QueryRow(ctx, "SELECT from-values").Scan(&s); err != nil || s != "v" {
			t.Errorf("Values did not answer: %q, %v", s, err)
		}
		if err := c.QueryRow(ctx, "SELECT other").Scan(&s); err != nil || s != "fallback" {
			t.Errorf("RowValues did not answer: %q, %v", s, err)
		}

		// With neither, RowName is the answer — that is how current_database() is
		// scripted. It needs a connection with NO RowValues, because RowValues has
		// precedence: the first version of this case set both fields and reported that
		// RowName did not answer, which was correct — RowValues answered first, exactly
		// as documented.
		rowNameOnly := &Conn{RowName: "dbname"}
		if err := rowNameOnly.QueryRow(ctx, "SELECT current_database()").Scan(&s); err != nil || s != "dbname" {
			t.Errorf("RowName did not answer: %q, %v", s, err)
		}

		// And RowErr scripts the FAILURE of that same read — so it too only applies when
		// RowValues is empty.
		rowErrOnly := &Conn{RowName: "dbname", RowErr: errBoom}
		if err := rowErrOnly.QueryRow(ctx, "SELECT current_database()").Scan(&s); !errors.Is(err, errBoom) {
			t.Errorf("RowErr did not answer: %v", err)
		}
	})

	t.Run("a step's Err is what QueryRow's Scan returns", func(t *testing.T) {
		c := &Conn{Steps: []Step{{Match: "x", Err: errBoom}}}
		if err := c.QueryRow(ctx, "SELECT x").Scan(new(string)); !errors.Is(err, errBoom) {
			t.Errorf("the scan returned %v, want errBoom", err)
		}
	})

	t.Run("Close and Ping are counted, because the driver branches on them", func(t *testing.T) {
		c := New()
		if c.Closed() != 0 || c.Pinged() != 0 {
			t.Fatal("a fresh connection already counted a close or a ping")
		}
		for range 2 {
			_ = c.Ping(ctx)
			_ = c.Close(ctx)
		}
		if c.Pinged() != 2 {
			t.Errorf("pinged %d times, want 2", c.Pinged())
		}
		if c.Closed() != 2 {
			t.Errorf("closed %d times, want 2", c.Closed())
		}
		if c.LastQuery() != "" {
			t.Errorf("LastQuery on a connection that sent nothing is %q", c.LastQuery())
		}

		c.PingErr = errBoom
		if err := c.Ping(ctx); !errors.Is(err, errBoom) {
			t.Errorf("Ping returned %v, want errBoom", err)
		}
		c.CloseErr = errBoom
		if err := c.Close(ctx); !errors.Is(err, errBoom) {
			t.Errorf("Close returned %v, want errBoom — the driver has a branch on it", err)
		}
	})

	t.Run("Dial reports the connect failure", func(t *testing.T) {
		c := New()
		if err := c.Dial(); err != nil {
			t.Errorf("Dial on a fresh connection returned %v", err)
		}
		c.ConnectErr = errBoom
		if err := c.Dial(); !errors.Is(err, errBoom) {
			t.Errorf("Dial returned %v, want errBoom", err)
		}
	})

	t.Run("every query is recorded with its arguments", func(t *testing.T) {
		// The assertion a query-building test leans on: "did the driver send the SQL I
		// expected, with which arguments". Without it every such test has to guess.
		c := &Conn{Sequential: []Result{{}}}
		_, _ = c.Query(ctx, "SELECT $1::int", 7)
		_, _ = c.Query(ctx, "SELECT $1::text", "x")

		if len(c.Queries) != 2 {
			t.Fatalf("recorded %d queries, want 2", len(c.Queries))
		}
		if c.LastQuery() != "SELECT $1::text" {
			t.Errorf("LastQuery is %q, want the most recent", c.LastQuery())
		}
		if len(c.Args) != 2 || c.Args[0][0] != 7 {
			t.Errorf("the arguments are %v, want the first query's 7", c.Args)
		}
		// QueryRow records the SQL too, and NOT the arguments — a gap worth pinning,
		// because a fixture using ArgContains on a QueryRow step would see no arguments.
		_ = c.QueryRow(ctx, "SELECT current_database()")
		if len(c.Queries) != 3 {
			t.Errorf("QueryRow did not record its SQL: %v", c.Queries)
		}
		if len(c.Args) != 2 {
			t.Errorf("QueryRow recorded arguments as well: %v", c.Args)
		}
	})
}

// reflectValueOf reads back whatever assign wrote, so the table above can compare against
// a value rather than against a pointer.
// sameValue compares two values without comparing slices, which panics through an
// interface comparison.
func sameValue(got, want any) bool {
	g, w := reflectValue(got), reflectValue(want)
	if g == nil || w == nil {
		return got == nil && want == nil
	}
	if reflect.DeepEqual(g, w) {
		return true
	}
	// A nil slice and an empty slice are the same thing to a caller checking for "no
	// columns", which is the only comparison this table makes.
	gs, gok := got.([]string)
	ws, wok := want.([]string)
	return gok && wok && len(gs) == 0 && len(ws) == 0
}

// reflectValue dereferences a pointer so a *[]string can be compared as a []string.
func reflectValue(v any) any {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer && !rv.IsNil() {
		return rv.Elem().Interface()
	}
	return v
}

func reflectValueOf(dest any) any {
	switch p := dest.(type) {
	case *string:
		return *p
	case *int:
		return *p
	case *int64:
		return *p
	case *bool:
		return *p
	case *[]string:
		return *p
	case *time.Time:
		return *p
	case *struct{}:
		return *p
	}
	return nil
}

// The pgx.Rows methods the driver does not call, and the two it calls in the states a test
// can put them in.
//
// Values and Scan index Rows[pos-1] with no bound of their own — they rely on the caller
// having checked Next(). That is the pgx contract too, so it is not a bug, but it means a
// Values() before the first Next() indexes row -1, and a test that reaches that state would
// be testing something the driver never does. Asserted rather than guarded: adding a guard
// would hide a caller that forgot Next(), which is exactly what a fake should surface.

// TestTheIterationBoundaryIsTheCallersJob pins the contract and its consequence.
func TestTheIterationBoundaryIsTheCallersJob(t *testing.T) {
	t.Run("Next reports the end twice, then stays there", func(t *testing.T) {
		c := &Conn{Sequential: []Result{{Rows: [][]any{{"a"}, {"b"}}}}}
		r, err := c.Query(context.Background(), "SELECT x")
		if err != nil {
			t.Fatalf("query: %v", err)
		}

		// Next IS the advance: there is no "before the first row" to ask about. The first
		// call moves pos to 1 and Values reads Rows[0] — which is why Values indexes
		// Rows[pos-1] and why calling it before Next would read row -1.
		//
		// The first version of this case asserted that the first Next said no, having read
		// it as "is there a row". It is "advance and report whether there is one", and the
		// distinction is the whole of the contract.
		row := 0
		for r.Next() {
			row++
			// Past the end is a no-op that STAYS false. A Next that returned true once more
			// would index past the slice on the next Values.
			if row > 2 {
				t.Fatalf("the iteration produced %d rows from a two-row result", row)
			}
		}
		if row != 2 {
			t.Errorf("the iteration yielded %d rows, want the two scripted", row)
		}
		for i := range 5 {
			if r.Next() {
				t.Errorf("Next said yes %d times past the end", i)
			}
		}
		if err := r.Err(); err != nil {
			t.Errorf("a finished iteration reported %v, want no error", err)
		}
	})

	t.Run("Values reads the row Next just advanced to", func(t *testing.T) {
		c := &Conn{Sequential: []Result{{Rows: [][]any{{"a", 1}, {"b", 2}}}}}
		r, _ := c.Query(context.Background(), "SELECT x")

		var want []string
		for _, step := range []string{"a", "b"} {
			if !r.Next() {
				t.Fatalf("Next said no at %q", step)
			}
			vals, err := r.Values()
			if err != nil {
				t.Fatalf("Values: %v", err)
			}
			if len(vals) != 2 {
				t.Fatalf("Values returned %d values, want 2", len(vals))
			}
			if vals[0] != step {
				t.Errorf("Values returned %v at step %q, want it to follow Next", vals, step)
			}
			want = append(want, step)
		}
		if len(want) != 2 {
			t.Errorf("walked %v", want)
		}
	})

	t.Run("ScanErr stops Values and Scan with the SAME error", func(t *testing.T) {
		// One knob for both, because they read the same row. A test that scripts ScanErr
		// and finds Values working is looking at a fixture that does not do what it says.
		c := &Conn{Sequential: []Result{{
			Rows:    [][]any{{"a"}},
			ScanErr: errBoom,
		}}}
		r, _ := c.Query(context.Background(), "SELECT x")
		r.Next()

		if _, err := r.Values(); !errors.Is(err, errBoom) {
			t.Errorf("Values returned %v, want the scripted scan error", err)
		}
		if err := r.Scan(new(string)); !errors.Is(err, errBoom) {
			t.Errorf("Scan returned %v, want the scripted scan error", err)
		}
		// And Next still works — ScanErr is about reading a row that reads fine but cannot
		// be converted, not about the iteration.
		if r.Next() {
			t.Error("Next reported another row on a one-row result")
		}
	})

	t.Run("Scan refuses more destinations than the row has columns", func(t *testing.T) {
		// The query's bug, reported rather than padded. A fake that filled the extra
		// destinations with zeros would let a SELECT that forgot a column pass.
		c := &Conn{Sequential: []Result{{Rows: [][]any{{"only", 1}}}}}
		r, _ := c.Query(context.Background(), "SELECT x")
		r.Next()

		var a, b, c3 string
		if err := r.Scan(&a, &b, &c3); err == nil {
			t.Error("scanning three destinations into a two-column row reported success")
		}
		// Fewer is fine — that is a caller reading a subset.
		if err := r.Scan(&a); err != nil {
			t.Errorf("scanning one destination failed: %v", err)
		}
		if a != "only" {
			t.Errorf("the value is %q, want %q", a, "only")
		}
	})

	t.Run("Scan reports a conversion failure rather than a zero", func(t *testing.T) {
		c := &Conn{Sequential: []Result{{Rows: [][]any{{"not a timestamp"}}}}}
		r, _ := c.Query(context.Background(), "SELECT x")
		r.Next()

		var when time.Time
		if err := r.Scan(&when); err == nil {
			t.Errorf("a non-timestamp scanned into a time without an error, giving %v", when)
		}
	})

	t.Run("a row SHORTER than the destination list is an error, not a partial fill", func(t *testing.T) {
		// Asymmetric on purpose, and the asymmetry is the point: fewer destinations than
		// columns is a caller reading a SUBSET and is fine; more destinations than columns
		// is a query that asked for a column it did not get, and padding the rest with
		// zeros would let that pass.
		//
		// The first version of this case asserted the opposite — that a short row left the
		// extra destinations at their zero values — because that is what a naive
		// implementation does and because "leaves it alone" is a kinder-sounding contract.
		// It is not what pgx does and not what a useful fake does.
		c := &Conn{Sequential: []Result{{Rows: [][]any{{"only"}}}}}
		r, _ := c.Query(context.Background(), "SELECT x")
		r.Next()

		var a, b string
		if err := r.Scan(&a, &b); err == nil {
			t.Errorf("scanning two destinations into a one-column row reported success: %q and %q", a, b)
		}

		// And the subset read is the other direction.
		if err := r.Scan(&a); err != nil {
			t.Errorf("scanning one destination into a one-column row failed: %v", err)
		}
		if a != "only" {
			t.Errorf("the value is %q, want %q", a, "only")
		}
	})
}

// FieldDescriptions is what the driver reads to learn the result's shape, and the OID
// default is the load-bearing part: a column with no OID maps to the text type, which is
// what every fixture in this repository writes.
func TestTheFieldDescriptionsReportEachColumnAndDefaultToText(t *testing.T) {
	c := &Conn{Sequential: []Result{{
		Columns: []Column{
			{Name: "id", OID: 23},      // int4
			{Name: "name"},             // no OID
			{Name: "total", OID: 1700}, // numeric
		},
		Rows: [][]any{},
	}}}
	r, _ := c.Query(context.Background(), "SELECT x")

	fields := r.FieldDescriptions()
	if len(fields) != 3 {
		t.Fatalf("the result reports %d fields, want one per column", len(fields))
	}
	for i, want := range []struct {
		name string
		oid  uint32
	}{
		{"id", 23},
		{"name", 25}, // the text OID, by default
		{"total", 1700},
	} {
		if fields[i].Name != want.name {
			t.Errorf("field %d is named %q, want %q", i, fields[i].Name, want.name)
		}
		if fields[i].DataTypeOID != want.oid {
			t.Errorf("field %q has OID %d, want %d — a column with no OID must read as text",
				want.name, fields[i].DataTypeOID, want.oid)
		}
	}

	t.Run("a result with no columns reports no fields", func(t *testing.T) {
		empty := &Conn{Sequential: []Result{{}}}
		r, _ := empty.Query(context.Background(), "SELECT x")
		if got := r.FieldDescriptions(); len(got) != 0 {
			t.Errorf("a result with no columns reports %d fields", len(got))
		}
	})
}

// The pgx.Rows methods the driver does not call. They exist to satisfy the interface, and
// asserting they answer is what keeps them answering: an empty stub that returns a zero
// value is correct until a caller starts using it, at which point the bug is somewhere
// else entirely.
func TestTheUnimplementedRowsMethodsAnswer(t *testing.T) {
	c := &Conn{Sequential: []Result{{Rows: [][]any{{"a"}}}}}
	r, _ := c.Query(context.Background(), "SELECT x")
	r.Next()

	// Each of these has a defined answer, and the point is that none of them panics — a
	// nil map or a nil pointer dereferenced here would surface as a panic in whatever
	// caller reaches for it first.
	if tag := r.CommandTag(); tag.String() != "" {
		t.Errorf("CommandTag is %q, want the zero tag", tag)
	}
	if raw := r.RawValues(); raw != nil {
		t.Errorf("RawValues is %v, want nil", raw)
	}
	if conn := r.Conn(); conn != nil {
		t.Error("Conn answered a connection out of a fake")
	}
	if tm := r.TypeMap(); tm != nil {
		t.Error("TypeMap answered a type map out of a fake")
	}
	// Close is a no-op and must stay callable twice.
	r.Close()
	r.Close()
}

// NewRow is the one-shot constructor for a single scalar read, and it exists so a fixture
// does not have to spell out the struct field.
func TestNewRowAnswersOneScalarRead(t *testing.T) {
	c := NewRow("my_database")
	if c.RowName != "my_database" {
		t.Errorf("NewRow(%q) stored %q", "my_database", c.RowName)
	}

	var got string
	if err := c.QueryRow(context.Background(), "SELECT current_database()").Scan(&got); err != nil {
		t.Fatalf("the scan failed: %v", err)
	}
	if got != "my_database" {
		t.Errorf("the scalar read gave %q, want the name the row was built with", got)
	}
}

// ArgContains on the QueryRow path DOES narrow, from the variadic argument — and does NOT
// record it, which is the difference between the two entry points.
//
// The first version of this test asserted the opposite on both counts, having read "QueryRow
// does not append to c.Args" as "QueryRow ignores its arguments". It uses them and simply
// does not keep them, which is why a fixture using ArgContains on a scalar step gets the
// right answer but a test asserting on c.Args sees nothing.
func TestTheScalarPathNarrowsByArgumentWithoutRecordingIt(t *testing.T) {
	t.Run("ArgContains narrows, so the matching step is the one that answers", func(t *testing.T) {
		c := &Conn{Steps: []Step{
			{Match: "count", ArgContains: "audit", Err: errBoom},
			{Match: "count", Result: Result{Rows: [][]any{{int64(12)}}}},
		}}

		var n int64
		if err := c.QueryRow(context.Background(), "SELECT count", "audit").Scan(&n); !errors.Is(err, errBoom) {
			t.Errorf("the audit read returned %v, want the narrowed step's error", err)
		}
		if err := c.QueryRow(context.Background(), "SELECT count", "public").Scan(&n); err != nil {
			t.Fatalf("the public read failed: %v", err)
		}
		if n != 12 {
			t.Errorf("the public read gave %d, want 12", n)
		}
	})

	t.Run("and the arguments are NOT recorded", func(t *testing.T) {
		// The asymmetry, which is worth knowing before a test asserts on c.Args: a query
		// made through QueryRow leaves no trace in Args, so a fixture that counts its own
		// arguments sees fewer than it sent.
		c := &Conn{Sequential: []Result{{}}}
		_ = c.QueryRow(context.Background(), "SELECT $1::int", 7)

		if len(c.Args) != 0 {
			t.Errorf("QueryRow recorded arguments as %v, want none", c.Args)
		}
		// The SQL IS recorded, so a test asserting "was this query issued" works.
		if len(c.Queries) != 1 {
			t.Errorf("QueryRow recorded %d queries, want one", len(c.Queries))
		}
	})
}

// The **time.Time destination, which is the shape GetTableOverview actually uses. It is
// separate from *time.Time in the switch and separate again inside, because pgx hands the
// scan a pointer to a nullable field.
func TestANullableTimestampScansThroughItsOwnDoublePointer(t *testing.T) {
	when := time.Date(2024, 6, 1, 9, 0, 0, 0, time.UTC)

	t.Run("a value arrives as a POINTER, not a copy", func(t *testing.T) {
		var p *time.Time
		if err := assign(&p, when); err != nil {
			t.Fatalf("assign failed: %v", err)
		}
		if p == nil {
			t.Fatal("a timestamp scanned into **time.Time gave nil")
		}
		if !p.Equal(when) {
			t.Errorf("the timestamp is %v, want %v", p, when)
		}
		// A COPY, so reassigning the field through the pointer does not change the original.
		*p = when.Add(time.Hour)
		if !when.Equal(time.Date(2024, 6, 1, 9, 0, 0, 0, time.UTC)) {
			t.Error("writing through the scanned pointer changed the fixture's value, so it is not a copy")
		}
	})

	t.Run("a NULL leaves it ALONE, which is pgx's rule for every destination", func(t *testing.T) {
		// Not cleared. assign returns early on nil for every type, because pgx leaves the
		// destination at its zero value for a NULL — and a *pointer* destination is already
		// the nullable shape, so there is nothing for the nil to say.
		//
		// The first version of this case expected nil here, having remembered that an empty
		// STRING into **string becomes nil. That is a different rule for a different reason:
		// a string has no null, so an empty one is how a null arrives, while a typed nil
		// needs no translation at all.
		set := when
		p := &set
		if err := assign(&p, nil); err != nil {
			t.Fatalf("assign(nil) failed: %v", err)
		}
		if p == nil || !p.Equal(when) {
			t.Errorf("a NULL changed the pointer to %v, want it left at %v", p, when)
		}
	})

	t.Run("a non-timestamp is an error", func(t *testing.T) {
		var p *time.Time
		if err := assign(&p, "the day before"); err == nil {
			t.Error("a phrase scanned into **time.Time without an error")
		}
	})

	t.Run("and a plain *time.Time takes the other arm", func(t *testing.T) {
		// The pair. Both are *time.Time-shaped but only one is nullable, and putting the
		// value in the wrong one produces a pointer where a value belongs — which is a
		// silent type change rather than an error.
		var when2 time.Time
		if err := assign(&when2, when); err != nil {
			t.Fatalf("assign into *time.Time failed: %v", err)
		}
		if !when2.Equal(when) {
			t.Errorf("the value is %v, want %v", when2, when)
		}
		var still time.Time
		if err := assign(&still, nil); err != nil {
			t.Errorf("a NULL into *time.Time returned %v, want no error", err)
		}
		if !still.Equal(time.Time{}) {
			t.Errorf("a NULL into *time.Time changed it to %v", still)
		}
	})
}
