package app

// Scenario: El masker de literales y el arranque de una transaccion.
//
// Two things with the same shape: a guard that is only reachable through a dependency, and a
// guard that is only reachable through a state the happy path never visits.
//
//   - blankRange's two clamps. It used to be a closure over `out`, and a closure's arms cannot
//     be tested at all — you cannot hand-write a call to a local variable. Extracting it to
//     package level made them reachable and the point of extracting was to keep them, because
//     an out-of-range index from a caller would otherwise take the whole masker down and take
//     the user's SQL with it.
//   - the transaction begin failing inside a multi-statement run. The runner's `begin` is a
//     FIELD, which is the seam that makes it reachable: a runner whose begin always fails, with
//     a batch whose SECOND statement is DML.
//
// Both are refusals, so both have to be checked for two things: that they refuse, and that what
// they refuse leaves nothing half-done.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/buble/dbx/internal/testsupport/pgxfake"
	"github.com/jackc/pgx/v5"
)

// The clamps, with the bounds a caller could plausibly get wrong.
func TestTheMaskerClampsBoundsThatAreOutOfRange(t *testing.T) {
	for _, tc := range []struct {
		name     string
		size     int
		from, to int
		wantFrom int
		wantTo   int
	}{
		// A negative start: what a "find the quote before this index" caller computes when
		// the index is -1, which is what a not-found returns.
		{"a start before the beginning", 10, -5, 4, 0, 4},
		{"a start far before the beginning", 10, -1000, 4, 0, 4},
		// An end past the end: what a caller computes when it took len() of a shorter string.
		{"an end past the end", 10, 2, 99, 2, 10},
		{"an end far past the end", 10, 2, 1000, 2, 10},
		// Both, and the combination that matters most: a completely inverted range.
		{"both ends out of range", 10, -3, 400, 0, 10},
		{"a fully inverted range", 10, 8, 2, 8, 2},
		// The legal cases, as the counterweight: a clamp that fired on valid input would
		// silently truncate a literal and change what the statement means.
		{"a range inside the string", 10, 2, 7, 2, 7},
		{"the whole string", 10, 0, 10, 0, 10},
		{"an empty range", 10, 4, 4, 4, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := []byte(strings.Repeat("x", tc.size))

			blankRange(out, tc.from, tc.to)

			// The clamped range, read back out of the buffer: what is NOT a space is what was
			// left alone, so the untouched spans are the assertion.
			for i := range out {
				blanked := i >= tc.wantFrom && i < tc.wantTo
				if blanked && out[i] != ' ' {
					t.Errorf("index %d is inside the clamped range but holds %q", i, out[i])
				}
				if !blanked && out[i] != 'x' {
					t.Errorf("index %d is outside the clamped range but holds %q", i, out[i])
				}
			}
			// And no byte outside the buffer was touched, which is the whole reason for the
			// `to` clamp: without it the loop runs past the slice and panics.
			if len(out) != tc.size {
				t.Errorf("the buffer changed length")
			}
		})
	}
}

// Newlines are never blanked, so line numbers and column positions still line up with the
// original statement. This is the property that makes clamping safe rather than merely
// non-panicking: a masked literal that lost its newlines would renumber every line after it.
func TestTheMaskerKeepsEveryNewline(t *testing.T) {
	const sql = "SELECT 'a\nb'\nFROM t\nWHERE x = 'c\nd\ne'"
	masked := maskNonCode(sql)

	if got, want := strings.Count(masked, "\n"), strings.Count(sql, "\n"); got != want {
		t.Errorf("the masked statement has %d newlines, want the original's %d:\n%q", got, want, masked)
	}
	if len(masked) != len(sql) {
		t.Errorf("the masked statement is %d bytes, want the original's %d", len(masked), len(sql))
	}
	// And the literals are gone, which is what the masking is for.
	if strings.Contains(masked, "'a") {
		t.Errorf("a literal survived the masking:\n%s", masked)
	}
}

// nl joins the statements of a batch. A real batch is written across lines, and the runner's
// splitter is what has to handle it — so the fixture is written across lines too, rather than
// with a semicolon in a string literal.
const nl = "\n"

// The transaction begin failing, inside a batch.
//
// The runner's `begin` is a FIELD precisely so this is reachable: a live database can be asked
// to open a transaction and can answer "no" — a statement timeout, a max_connections limit, a
// read-only server — and the batch has to stop there rather than run the statements without one.
func TestABatchStopsWhenTheTransactionCannotBegin(t *testing.T) {
	// newStatementRunner(nil) and then the two fields set by hand: the runner's constructor
	// wants a TxBeginner, and the fake is not one — it has no Begin. Assigning conn directly
	// is what the constructor does with its argument anyway, and it keeps the fixture honest
	// about which parts of the runner are under test (begin) and which are just plumbing (conn).
	r := newStatementRunner(nil)
	// The fake answers the read that comes FIRST in the batch, so the fixture reaches the DML
	// rather than stopping on an unmatched query: a fake with no step matches every query, and
	// a batch stops at the first one.
	conn := pgxfake.New()
	conn.Steps = []pgxfake.Step{{Match: "SELECT", Result: pgxfake.Result{
		Columns: []pgxfake.Column{{Name: "?column?"}},
		Rows:    [][]interface{}{{int64(1)}},
	}}}
	r.conn = conn
	// The seam: a begin that always fails, with a message a user would recognise.
	r.begin = func(context.Context) (pgx.Tx, error) {
		return nil, errors.New("sorry, too many clients already")
	}

	// The batch: a read, then a DML. The DML is what needs the transaction, and it is second on
	// purpose — the first statement has already run by then, so the refusal has to leave the
	// `committed` flag false rather than reporting the read as done.
	_, committed, err := r.execute(context.Background(), "SELECT 1;"+nl+"UPDATE orders SET total = 0;")

	if err == nil {
		t.Fatal("a batch whose transaction could not begin reported success")
	}
	if !strings.Contains(err.Error(), "too many clients already") {
		t.Errorf("the error is %q, want it to carry the server's own words", err)
	}
	if !strings.Contains(err.Error(), "begin") {
		t.Errorf("the error is %q, want it to say WHICH step failed", err)
	}
	// Nothing was committed: the transaction never opened, so there is nothing to commit and
	// reporting otherwise would make the app toast "Transaction committed".
	if committed {
		t.Error("a batch that never began a transaction reports a commit")
	}

	t.Run("and a read-only batch never needs a begin", func(t *testing.T) {
		// The counterweight: a runner that always called begin would fail every read-only
		// batch, and that is the query path — by far the most common thing this does.
		readsOnly := newStatementRunner(nil)
		reads := pgxfake.New()
		reads.Steps = []pgxfake.Step{{Match: "SELECT", Result: pgxfake.Result{
			Columns: []pgxfake.Column{{Name: "?column?"}},
			Rows:    [][]interface{}{{int64(1)}},
		}}}
		readsOnly.conn = reads
		readsOnly.begin = func(context.Context) (pgx.Tx, error) {
			return nil, errors.New("begin must not be called for a read-only batch")
		}

		if _, _, err := readsOnly.execute(context.Background(), "SELECT 1;"); err != nil {
			t.Errorf("a read-only batch: %v", err)
		}
	})

	t.Run("and a failed begin leaves no transaction behind", func(t *testing.T) {
		// The state the next query would find. A runner that stored a nil tx, or a stale one
		// from a previous batch, would make the second attempt behave differently from the
		// first — which is the kind of difference nobody reproduces twice.
		after := newStatementRunner(nil)
		after.conn = pgxfake.New()
		after.begin = func(context.Context) (pgx.Tx, error) {
			return nil, errors.New("still too many clients")
		}
		if _, _, err := after.execute(context.Background(), "UPDATE orders SET total = 0;"); err == nil {
			t.Fatal("the single-statement form reported success either")
		}
		if after.pending() {
			t.Error("a begin that failed left pending changes behind")
		}
	})
}

// FocusPane's name for a pane that is not one of the six.
//
// The default is not an unreachable branch: a FocusPane is an int, and any value that is not
// one of the six declared constants lands in it. My first version of this case assumed the
// ZERO value would — and it does not, because FocusExplorer is declared first and therefore
// IS zero, which is also why a fresh model starts focused on the explorer.
//
// The names themselves are load-bearing: they appear in the debug logs and they are what the
// mouse-routing tests identify a pane by, so they are asserted here rather than assumed. And
// "unknown" is the right answer for an undeclared value — a made-up name in a log sends a
// reader looking for a pane that does not exist.
func TestEveryFocusPaneHasAName(t *testing.T) {
	for _, tc := range []struct {
		pane FocusPane
		want string
	}{
		{FocusExplorer, "explorer"},
		{FocusGrid, "grid"},
		{FocusEditor, "editor"},
		{FocusGridPreview, "grid-preview"},
		{FocusExplorerPreview, "explorer-preview"},
		// The zero value is FocusExplorer, because it is declared first. That is not a detail:
		// it is why a Model that has never been focused starts on the explorer.
		{FocusPane(0), "explorer"},
		// Beyond every declared pane: the stringer must not invent a name for a value that
		// does not exist, and a NEGATIVE one is the shape a "cleared" field would take.
		{FocusPane(99), "unknown"},
		{FocusPane(-1), "unknown"},
	} {
		if got := tc.pane.String(); got != tc.want {
			t.Errorf("FocusPane(%d).String() = %q, want %q", int(tc.pane), got, tc.want)
		}
	}
}
