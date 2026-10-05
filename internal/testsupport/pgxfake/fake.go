// Package pgxfake provides a scriptable stand-in for a pgx connection, so the
// query-building and row-scanning code above the driver can be tested without a
// PostgreSQL server.
//
// It is only ever imported from _test.go files, so it is not linked into the dbx
// binary. The alternative — a real server per test — costs seconds per case and
// still cannot reproduce a mid-iteration failure.
package pgxfake

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// Result is one scripted answer: a row set, or a failure.
type Result struct {
	// Columns and FieldOIDs describe the result set. A name with no OID maps to
	// the text type, which is what most fixtures want.
	Columns []Column
	Rows    [][]any
	// Err is returned from Query instead of a row set.
	Err error
	// IterErr is what Next stops with, which is how a mid-iteration failure is
	// reproduced. ExecuteQuery wraps it as a row iteration error.
	IterErr error
	// ScanErr is what Values and Scan return for a row that reads fine but cannot
	// be converted.
	ScanErr error
	// Values answers a QueryRow directly, for a read whose result is a single
	// row rather than a set. Rows takes precedence when both are given.
	Values []any
}

// Column is one field of a scripted result set.
type Column struct {
	Name string
	// OID is the pg type OID. Zero means text.
	OID uint32
	// Nullable makes the fake hand back nil for this column.
	Nullable bool
}

// Step answers a query when its SQL contains Match. An empty Match matches
// anything, so it works as a catch-all placed last.
type Step struct {
	Match string
	// ArgContains narrows a step to a query carrying that bound argument. Every
	// schema query passes the schema as $1, so this is what lets a fixture answer
	// one schema and fail another from the same connection.
	ArgContains string
	Result      Result
	// Err is shorthand for Result{Err: ...}, kept because it is the common case.
	Err error
}

// Conn is a fake connection. It satisfies postgres.Conn.
//
// Answers are resolved in this order, and the order matters:
//
//  1. the first Step whose Match is a substring of the SQL;
//  2. otherwise the Nth Sequential answer, counting only queries that no Step
//     matched, so a fixture does not have to count its own setup queries;
//  3. otherwise an error naming the query.
type Conn struct {
	Steps      []Step
	Sequential []Result

	// RowName is what QueryRow scans a single string into, which is how
	// current_database() is answered.
	RowName string
	// RowValues answers a QueryRow no Step matched.
	RowValues []any
	// RowErr is returned by QueryRow's Scan.
	RowErr error
	// PingErr is returned by Ping.
	PingErr error
	// ConnectErr is returned by Dial's connection.
	ConnectErr error

	Queries   []string
	Args      [][]any
	closed    int
	pinged    int
	unmatched int
	// CloseErr is what Close returns, for a caller that must not ignore it.
	CloseErr error
	// ExecErr is what every Exec returns, for the arms that handle a rejected write.
	// There is no per-statement seam for Exec and there does not need to be one: a
	// write either is refused or it is not, and a fixture that needs "this one refused"
	// installs a whole Conn per case the way it installs a per-case Steps list.
	ExecErr error

	// Execs records the statements Exec was given, in order, because the app's write
	// path is a loop over arguments and a test needs to see that it issued one
	// statement per argument rather than one statement with all of them.
	Execs []string
}

// New returns a connection that answers nothing, for tests that install their own
// steps.
func New() *Conn { return &Conn{} }

// NewRow returns a connection whose QueryRow scans into name, for the code paths
// that read one scalar (current_database(), COUNT(*)).
func NewRow(name string) *Conn { return &Conn{RowName: name} }

// Dial returns the connection or ConnectErr, so a caller can write
// `pgxConnect = func(...) (T, error) { return fake.Dial(ctx, dsn) }` for whatever
// interface T happens to be. The interface is left to the caller so this package
// does not have to import the driver, which its own tests import.
func (c *Conn) Dial() error { return c.ConnectErr }

func (c *Conn) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	c.Queries = append(c.Queries, sql)
	c.Args = append(c.Args, args)

	for _, s := range c.Steps {
		if s.Match != "" && !strings.Contains(sql, s.Match) {
			continue
		}
		if s.ArgContains != "" && !anyArgContains(args, s.ArgContains) {
			continue
		}
		if s.Err != nil {
			return nil, s.Err
		}
		return newRows(s.Result), nil
	}

	c.unmatched++
	if c.unmatched <= len(c.Sequential) {
		return newRows(c.Sequential[c.unmatched-1]), nil
	}
	return nil, fmt.Errorf("pgxfake: no step matches this query: %s", oneline(sql))
}

// QueryRow resolves through the same Steps as Query and uses the FIRST ROW of the
// matched result as the values, so one script can drive both APIs.
//
// That matters because a single logical read is often split across the two: the
// overview runs four scalar queries and a fixture that had to answer them through
// two different fields could not express "this one fails, that one succeeds".
func (c *Conn) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	c.Queries = append(c.Queries, sql)
	for _, s := range c.Steps {
		if s.Match != "" && !strings.Contains(sql, s.Match) {
			continue
		}
		if s.ArgContains != "" && !anyArgContains(args, s.ArgContains) {
			continue
		}
		if s.Err != nil {
			return row{err: s.Err}
		}
		if len(s.Result.Rows) > 0 {
			return row{values: s.Result.Rows[0]}
		}
		return row{values: s.Result.Values}
	}
	if len(c.RowValues) > 0 {
		return row{values: c.RowValues}
	}
	return row{values: []any{c.RowName}, err: c.RowErr}
}

func (c *Conn) Close(ctx context.Context) error {
	c.closed++
	return c.CloseErr
}

func (c *Conn) Ping(ctx context.Context) error {
	c.pinged++
	return c.PingErr
}

func (c *Conn) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	c.Queries = append(c.Queries, sql)
	c.Args = append(c.Args, args)
	c.Execs = append(c.Execs, sql)
	if c.ExecErr != nil {
		return pgconn.CommandTag{}, c.ExecErr
	}
	return pgconn.NewCommandTag("EXEC 1"), nil
}

// Closed reports how many times Close was called.
func (c *Conn) Closed() int { return c.closed }

// Pinged reports how many times Ping was called.
func (c *Conn) Pinged() int { return c.pinged }

// LastQuery is the most recent SQL sent, or "" if none.
func (c *Conn) LastQuery() string {
	if len(c.Queries) == 0 {
		return ""
	}
	return c.Queries[len(c.Queries)-1]
}

// Unmatched is how many queries fell through every Step, i.e. how many
// Sequential answers were consumed.
func (c *Conn) Unmatched() int { return c.unmatched }

// Asked reports whether any query so far contained substr. It exists for the
// assertions that a caller's error handling actually CHANGED what it asked for —
// a loader that stops at the first failure makes four fewer queries than one that
// carries on, and the difference is only visible from the query log.
//
// Without it the only way to say "it stopped early" is to count Queries, which
// counts the fixture's own setup queries too, so a fixture that grows a step
// silently changes the number and the assertion stops meaning anything.
func (c *Conn) Asked(substr string) bool {
	for _, q := range c.Queries {
		if strings.Contains(q, substr) {
			return true
		}
	}
	return false
}

// AskedCount is how many queries contained substr, for the case where one is not
// enough — a retry loop that must run once and not twice.
func (c *Conn) AskedCount(substr string) int {
	n := 0
	for _, q := range c.Queries {
		if strings.Contains(q, substr) {
			n++
		}
	}
	return n
}

// row is a single scripted result row, scanned into whatever destinations the
// caller passes.
type row struct {
	values []any
	err    error
}

// Scan fills the destinations from the scripted row, in order. A nil value leaves
// the destination at its zero value, which is what pgx does for a NULL and what
// the driver's callers expect.
func (r row) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) > len(r.values) {
		return fmt.Errorf("pgxfake: %d destinations but %d values scripted", len(dest), len(r.values))
	}
	for i, d := range dest {
		if err := assign(d, r.values[i]); err != nil {
			return err
		}
	}
	return nil
}

// asTime accepts what a fixture is likely to write for a timestamp: a time.Time, a
// layout PostgreSQL prints, or an RFC 3339 string.
func asTime(v any) (time.Time, error) {
	switch t := v.(type) {
	case time.Time:
		return t, nil
	case string:
		for _, layout := range []string{"2006-01-02 15:04:05.999999-07", "2006-01-02 15:04:05", time.RFC3339} {
			if parsed, err := time.Parse(layout, t); err == nil {
				return parsed, nil
			}
		}
		return time.Time{}, fmt.Errorf("pgxfake: %q is not a timestamp", t)
	default:
		return time.Time{}, fmt.Errorf("pgxfake: cannot read a timestamp out of %T", v)
	}
}

// asNumber accepts the numeric types a fixture is likely to write, so a row can be
// scripted as {4, 2, 3, 1} rather than {"4", "2", "3", "1"}.
func asNumber(v any) (int64, error) {
	switch n := v.(type) {
	case int:
		return int64(n), nil
	case int32:
		return int64(n), nil
	case int64:
		return n, nil
	case uint32:
		return int64(n), nil
	case uint64:
		return int64(n), nil
	case float64:
		return int64(n), nil
	case string:
		var out int64
		if _, err := fmt.Sscanf(n, "%d", &out); err != nil {
			return 0, fmt.Errorf("pgxfake: %q is not a number", n)
		}
		return out, nil
	default:
		return 0, fmt.Errorf("pgxfake: cannot read a number out of %T", v)
	}
}

type rows struct {
	res Result
	pos int
}

func newRows(res Result) *rows { return &rows{res: res} }

func (r *rows) Close()                        {}
func (r *rows) Err() error                    { return r.res.IterErr }
func (r *rows) CommandTag() pgconn.CommandTag { return pgconn.CommandTag{} }
func (r *rows) RawValues() [][]byte           { return nil }
func (r *rows) Conn() *pgx.Conn               { return nil }
func (r *rows) TypeMap() *pgtype.Map          { return nil }

func (r *rows) FieldDescriptions() []pgconn.FieldDescription {
	out := make([]pgconn.FieldDescription, len(r.res.Columns))
	for i, c := range r.res.Columns {
		oid := c.OID
		if oid == 0 {
			oid = 25 // text
		}
		out[i] = pgconn.FieldDescription{Name: c.Name, DataTypeOID: oid}
	}
	return out
}

func (r *rows) Next() bool {
	if r.res.IterErr != nil {
		return false
	}
	if r.pos >= len(r.res.Rows) {
		return false
	}
	r.pos++
	return true
}

func (r *rows) Values() ([]any, error) {
	if r.res.ScanErr != nil {
		return nil, r.res.ScanErr
	}
	return r.res.Rows[r.pos-1], nil
}

func (r *rows) Scan(dest ...any) error {
	if r.res.ScanErr != nil {
		return r.res.ScanErr
	}
	row := r.res.Rows[r.pos-1]
	if len(dest) > len(row) {
		return fmt.Errorf("pgxfake: %d destinations for %d columns", len(dest), len(row))
	}
	for i, d := range dest {
		var v any
		if i < len(row) {
			v = row[i]
		}
		if err := assign(d, v); err != nil {
			return err
		}
	}
	return nil
}

// assign is the whole of pgx's dest conversion for the types the driver scans
// into. It is deliberately small: a test that needs a type this cannot handle
// should say so rather than get a zero value.
func assign(dest, v any) error {
	if v == nil {
		// pgx leaves the destination at its zero value for a NULL, and that is
		// what the driver's callers expect.
		return nil
	}
	switch p := dest.(type) {
	case *struct{}:
		// A discard destination. The driver uses `&struct{}{}` for columns it
		// selects but does not want, so this is not a test artefact: a fake that
		// refused it would make listIndexes untestable without changing the
		// query to stop selecting those columns.
		*p = struct{}{}
	case *string:
		*p = fmt.Sprint(v)
	case **string:
		if v == "" {
			*p = nil
			return nil
		}
		s := fmt.Sprint(v)
		*p = &s
	case *[]string:
		// NO `if v == nil` here, and there used to be one. assign returns at the top for a
		// nil value, so by the time any case runs v is not nil — the check was the second
		// copy of the guard four lines above it, written as if the first might not fire.
		//
		// A nil scanned into a *[]string still ends up nil: it is the top-level return that
		// does it, which TestAssignAnswersEveryDestinationTheDriversScanInto pins.
		switch list := v.(type) {
		case []string:
			*p = list
		case string:
			*p = strings.Split(list, ",")
		default:
			return fmt.Errorf("pgxfake: cannot scan %T into *[]string", v)
		}
	case *time.Time:
		// TableOverview scans a nullable timestamp, so pgx is handed a *time.Time
		// destination and the value arrives either as a time or as nil — and the nil case is
		// answered by assign's own first line, which is why there is no nil check here. There
		// used to be one, and it could not fire.
		t, err := asTime(v)
		if err != nil {
			return err
		}
		*p = t
	case **time.Time:
		// This is the shape GetTableOverview actually uses: the field is a
		// *time.Time and the scan gets a **time.Time. No nil check, for the same reason as
		// the case above: a nil value never reaches a case.
		t, err := asTime(v)
		if err != nil {
			return err
		}
		*p = &t
	case *int:
		n, err := asNumber(v)
		if err != nil {
			return err
		}
		*p = int(n)
	case *int64:
		n, err := asNumber(v)
		if err != nil {
			return err
		}
		*p = n
	case *bool:
		// "t" counts as true, and it used not to: the comparison was
		// `fmt.Sprint(v) == "true"`, which answers FALSE for "t" — and "t" is what Postgres
		// PRINTS for a true boolean, and what a fixture copied out of a psql session or a
		// `\d` listing contains. So a fixture written the way the database spells it was
		// silently false, and the test went on to assert that a row which was not there was
		// not there.
		//
		// Only Postgres's own OUTPUT spellings are accepted — "true" and "t", in either case.
		// Its input syntax is wider ("yes", "on", "1"), and those are still false, which the
		// existing contract test pins deliberately: a fixture that says "yes" and means true
		// gets a visible surprise, and one that says "yes" by accident gets a silent false.
		switch s := strings.ToLower(fmt.Sprint(v)); s {
		case "true", "t":
			*p = true
		default:
			*p = false
		}
	default:
		return fmt.Errorf("pgxfake: cannot scan %T into %T", v, dest)
	}
	return nil
}

func anyArgContains(args []any, want string) bool {
	for _, a := range args {
		if strings.Contains(fmt.Sprint(a), want) {
			return true
		}
	}
	return false
}

func oneline(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
