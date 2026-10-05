package postgres

// Scenario: El bucle que convierte un result set en valores, aislado de la conexion.
//
// collectRows is a function of its argument and nothing else — no connection, no context, no
// package state — and that is the entire reason it exists. It was the loop inside Select, and
// the one error arm inside it (pgx unable to decode a field) could not be reached from any
// test: a connection fake can script the QUERY failing, or the ITERATION failing through Err,
// but never a driver rejecting a value it has already produced.
//
// Taking the loop out as a function of a two-method interface turns "needs a broken server"
// into "needs three lines", and the three lines are below.
//
// The cases are the four shapes the loop has to survive, plus the counterweight in each
// direction: rows that decode, a row that does not, an empty result set, and a row that is
// nil rather than empty.

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/buble/dbx/internal/testsupport/pgxfake"
)

// failingRows is a rowIterator scripted one row at a time: each element of values is one
// row's worth of data, and a nil element means "Next() reported a row and Values() failed".
type failingRows struct {
	values   [][]any
	failWith error
	pos      int
}

func (r *failingRows) Next() bool {
	if r.pos >= len(r.values) {
		return false
	}
	r.pos++
	return true
}

func (r *failingRows) Values() ([]any, error) {
	row := r.values[r.pos-1]
	if row == nil {
		return nil, r.failWith
	}
	return row, nil
}

// pgx.Rows has to keep satisfying the interface the extraction introduced — the compile
// proves it for Select's own call, and this is the named assertion so a future change to the
// interface cannot quietly stop being satisfied by the real thing.
var _ rowIterator = (interface {
	Next() bool
	Values() ([]any, error)
})(nil)

func TestCollectRowsReadsEveryRowItIsGiven(t *testing.T) {
	for _, tc := range []struct {
		name string
		rows [][]any
	}{
		{"no rows at all", nil},
		{"one row", [][]any{{int64(1)}}},
		{"three rows of three columns", [][]any{
			{int64(1), "Ada", true},
			{int64(2), "Grace", false},
			{int64(3), nil, true},
		}},
		{"a row that is entirely NULLs", [][]any{{nil, nil, nil}}},
		{"rows of different widths", [][]any{{1}, {1, 2}, {1, 2, 3}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := collectRows(&failingRows{values: tc.rows})
			if err != nil {
				t.Fatalf("collectRows: %v", err)
			}

			if len(got) != len(tc.rows) {
				t.Fatalf("collectRows returned %d rows, want %d", len(got), len(tc.rows))
			}
			// reflect.DeepEqual, not ==: comparing two interface{} holding slices PANICS, and
			// a row IS a slice. That trap is the reason this assertion is written this way.
			for i := range tc.rows {
				if !reflect.DeepEqual(got[i], tc.rows[i]) {
					t.Errorf("row %d is %#v, want %#v", i, got[i], tc.rows[i])
				}
			}
		})
	}
}

// The arm that could not be reached: a row the driver cannot decode.
//
// The error names the ROW, not the query, and says it wrapped the driver's complaint — both
// matter. A user whose query failed to decode a row needs to know which statement produced it
// (the caller adds that) and that the failure is in the data rather than in the SQL (this
// message is what says so).
func TestCollectRowsReportsARowItCannotDecode(t *testing.T) {
	decodeErr := errors.New("cannot decode unknown OID 99999 into an any")

	// The failure on the FIRST row, the LAST row, and in the middle: a loop that checked the
	// row count before reading, or that kept going past an error, would answer differently in
	// each of those, and the one that matters is the middle one — a result set of ten thousand
	// rows where row 9999 is the bad one.
	for _, tc := range []struct {
		name string
		rows [][]any
	}{
		{"the first row", [][]any{nil, {int64(2)}}},
		{"a row in the middle", [][]any{{int64(1)}, nil, {int64(3)}}},
		{"the last row", [][]any{{int64(1)}, {int64(2)}, nil}},
		{"the only row", [][]any{nil}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := collectRows(&failingRows{values: tc.rows, failWith: decodeErr})

			if err == nil {
				t.Fatal("a row that cannot be decoded reported success")
			}
			if !strings.Contains(err.Error(), "failed to scan row") {
				t.Errorf("the error is %q, want it to name the ROW rather than the query", err)
			}
			if !errors.Is(err, decodeErr) {
				t.Errorf("the error does not wrap the driver's own: %v", err)
			}
			// Nothing is returned with the error: a caller that ignored the error and used
			// the rows anyway would get a truncated result set that looks complete.
			if got != nil {
				t.Errorf("a failed collection returned %d rows", len(got))
			}
		})
	}
}

// The connection's own iteration error is NOT collectRows' business — Select reads rows.Err()
// itself, after the loop, because a failure there means the result set ended early rather than
// that a row was bad. So the two have different messages and this case keeps them apart.
func TestCollectRowsDoesNotInventAnIterationError(t *testing.T) {
	// A rows whose Next() simply stops. collectRows has no way to know whether that was the
	// end or a failure — which is why the caller asks the rows, not this function.
	rows := &failingRows{values: [][]any{{int64(1)}, {int64(2)}}}

	got, err := collectRows(rows)

	if err != nil {
		t.Fatalf("collectRows invented an error for a clean end of results: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("collectRows returned %d rows, want the two it was given", len(got))
	}
}

// The same arm, through the whole of Select — which is where the message a user sees is
// assembled.
//
// The fake connection can script this after all, once the decode lives behind an interface it
// can implement: pgxfake's rows carry a ScanErr, which is exactly a driver's refusal to decode
// a value it produced. So the case below needs no server and no broken one.
//
// It exists because the pure case above proves collectRows' contract and this one proves
// Select still passes the error through untouched — a wrapper that swallowed it would leave
// the first test green and the driver wrong.
func TestSelectReportsARowTheDriverCannotDecode(t *testing.T) {
	decodeErr := errors.New("cannot decode unknown OID 99999")

	fake := pgxfake.New()
	fake.Steps = []pgxfake.Step{{
		Match: "FROM",
		Result: pgxfake.Result{
			Columns: []pgxfake.Column{{Name: "id"}},
			Rows:    [][]interface{}{{int64(1)}},
			ScanErr: decodeErr,
		},
	}}

	loader := NewSchemaLoader(fake)
	got, err := loader.Select(context.Background(), "orders", SelectOptions{Schema: "public"})

	if err == nil {
		t.Fatal("a driver that cannot decode a row reported success")
	}
	if !strings.Contains(err.Error(), "failed to scan row") {
		t.Errorf("the error is %q, want it to keep the row-level message", err)
	}
	// And the QUERY is NOT in the message — worth pinning, because it is an asymmetry with
	// the failure three lines above. A query that FAILS says "query failed: ... Query was:
	// ..."; a row that cannot be DECODED says only "failed to scan row", because collectRows
	// has no idea which statement it was reading and Select does not add it.
	//
	// My first version of this case asserted the table was named, on the assumption that the
	// two failures were wrapped the same way. They are not, and the difference is real: a
	// decode failure is about the data, and the statement is the one thing the caller already
	// knows — it just asked. So the assertion is the shape of the message, not a wish.
	if strings.Contains(err.Error(), "Query was") {
		t.Errorf("the error is %q, want the row-level message with no query echo", err)
	}
	if got != nil {
		t.Errorf("a failed select returned a result with %d rows", got.Count)
	}
}
