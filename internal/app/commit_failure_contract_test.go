package app

// Scenario: Un commit que falla no puede anunciarse como un commit que funcionó.
//
// This is a bug the tests found rather than one they were written for, and the shape of it
// is worth stating because it is the same shape the codebase has produced repeatedly: a
// boolean whose MEANING is "it worked", produced by a function that returns it true when it
// did not, read by a handler that turns it into a success toast BEFORE it looks at the
// error next to it.
//
// Three parts, all of which had to be true for the user to see the contradiction:
//
//	statementRunner.commitPending returned (true, err) — a transaction existed, and the
//	  flag was read as "committed"
//	executeQuery ORed that into the message's committedTx
//	the queryExecutedMsg handler toasted "Transaction committed" and only then checked
//	  msg.err, toasting "Query failed" after it
//
// The fix is one word in commitPending plus the order in the handler. The tests below pin
// both halves, because the ORDER is the defect: an ordering that can contradict itself is
// worth not having regardless of what feeds it.
//
// There is a consequence nobody can fix here, and it is written down rather than hidden:
// pgx marks a transaction closed before it checks the error (dbTx.Commit sets tx.closed
// unconditionally), and on a commit failure it closes the whole connection when the server
// is not idle. So after this the app's connection is gone, the model still holds it, and
// every later statement fails against a closed pipe. The app does not notice. Pinned, with
// the place to fix it named.

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/testsupport/pgxfake"
)

// A model that is mid-statement with a pending transaction, so queryExecutedMsg arrives
// with committedTx set.
func modelWithPendingCommit(t *testing.T) Model {
	t.Helper()
	m := routerModelLoaded(t)
	m.state = StateMain
	m.editorOpen = true
	m.runner = newStatementRunner(nil)
	m.runner.tx = &txnFailingTx{}
	return m
}

// The two halves of the same property, at the two levels it can be broken at.
func TestACommitThatFailedIsNeverReportedAsACommit(t *testing.T) {
	t.Run("the flag says no", func(t *testing.T) {
		f := newFakeRunner()
		f.runner.tx = &txnFailingTx{}
		committed, err := f.runner.commitPending(context.Background())
		if err == nil {
			t.Fatal("a failing commit reported success")
		}
		if committed {
			t.Error("the flag claims a commit that failed")
		}
	})

	t.Run("the handler does not announce it", func(t *testing.T) {
		m := modelWithPendingCommit(t)

		out, _ := fold(m, queryExecutedMsg{
			sql:         "INSERT INTO orders (id) VALUES (1)",
			committedTx: true, // what the bug produced
			err:         context.DeadlineExceeded,
		})

		said := toastText(out)
		if strings.Contains(said, "committed") {
			t.Errorf("the toast claims a commit: %q", said)
		}
		if !strings.Contains(said, "failed") {
			t.Errorf("the toast does not report the failure: %q", said)
		}
		// And the order, which is the defect: the error must be the whole answer. A toast
		// list containing both a success and a failure for one statement is the symptom,
		// whichever one is visible.
		if strings.Contains(strings.ToLower(said), "transaction committed") {
			t.Errorf("the toast list holds both outcomes for one statement: %q", said)
		}
	})

	t.Run("a commit that worked is still announced", func(t *testing.T) {
		// The counterweight. Without it, "never says committed" would satisfy the case
		// above, and so would a handler that deleted the toast entirely.
		m := routerModelLoaded(t)
		m.state = StateMain
		m.editorOpen = true

		out, _ := fold(m, queryExecutedMsg{
			sql:         "INSERT INTO orders (id) VALUES (1)",
			committedTx: true,
			result:      &postgres.QueryResult{},
		})

		if said := toastText(out); !strings.Contains(said, "Transaction committed") {
			t.Errorf("a commit that worked was not announced: %q", said)
		}
	})

	t.Run("a statement that was never a transaction is not announced as one", func(t *testing.T) {
		m := routerModelLoaded(t)
		m.state = StateMain
		m.editorOpen = true

		out, _ := fold(m, queryExecutedMsg{sql: "SELECT 1", result: &postgres.QueryResult{}})

		if said := toastText(out); strings.Contains(said, "Transaction") {
			t.Errorf("a SELECT announced a transaction: %q", said)
		}
	})

	t.Run("the run-on-commit path cannot produce the contradiction", func(t *testing.T) {
		// The end to end version: a statement whose transaction fails to commit, driven
		// through executeQuery rather than through a hand-built message. This is the path
		// that produced committedTx=true WITH an error in the first place.
		m := routerModelLoaded(t)
		m.state = StateMain
		m.editorOpen = true
		m.runner = newStatementRunner(nil)
		m.runner.tx = &txnFailingTx{}
		m.runner.exec = func(context.Context, postgres.Querier, string) (*postgres.QueryResult, error) {
			return &postgres.QueryResult{}, nil
		}

		cmd := m.executeQuery("INSERT INTO orders (id) VALUES (1)")
		if cmd == nil {
			t.Fatal("executeQuery refused, so this proves nothing")
		}

		msg, ok := cmd().(queryExecutedMsg)
		if !ok {
			t.Fatalf("executeQuery produced %T, want queryExecutedMsg", cmd())
		}
		if msg.err == nil {
			t.Error("a failed commit reached the message with no error")
		}
		if msg.committedTx {
			t.Error("queryExecutedMsg claims a commit that failed")
		}

		// And the statement is logged as a failure, not a success — the session log is the
		// other place this could have lied.
		out, _ := fold(m, msg)
		if said := toastText(out); strings.Contains(said, "committed") {
			t.Errorf("the toast claims a commit: %q", said)
		}
	})
}

// What happens AFTER a commit failure, which is where the real damage was.
//
// pgx closes the whole connection when a COMMIT fails and the server is not idle — and it
// hands the result back to a model that still holds the connection. So the user saw "Query
// failed" and then every later statement failed the same way, for the rest of the session,
// with nothing anywhere saying the connection was gone.
//
// The fix cannot be "a commit failure means the connection is dead", because a commit whose
// acknowledgement was lost also fails and leaves a perfectly good connection with the DML
// stored. Both produce the same error text, so the app has to ask the connection.
//
// Both answers are here, and the second is the one that matters: a connection that works
// must be KEPT, because telling a user to reconnect when their transaction is committed and
// their connection is fine is a worse lie than the one it replaces.
func TestAFailedCommitAsksTheConnectionRatherThanAssuming(t *testing.T) {
	// A runner whose transaction fails to commit and whose connection answers Ping however
	// the case wants. The two hooks are separate on purpose: the commit's outcome and the
	// connection's survival are independent facts, and a fake that tied them together would
	// make every case above pass and every case here meaningless.
	runnerWith := func(t *testing.T, pingErr error) *statementRunner {
		t.Helper()
		r := newStatementRunner(nil)
		r.tx = &txnFailingTx{}
		r.ping = func(context.Context) error { return pingErr }
		return r
	}

	t.Run("a connection pgx closed is dropped and the user is told", func(t *testing.T) {
		m := routerModelLoaded(t)
		m.state = StateMain
		m.editorOpen = true
		m.conn = pgxfake.New()
		// The project has to be there for the assertion below to mean anything: "the way
		// back is one keypress" is a claim about the project surviving, and a fixture with
		// no project satisfies it vacuously.
		m.project = &config.FoundProject{Name: "shopdb", Path: t.TempDir(), Active: true}
		m.runner = runnerWith(t, errors.New("conn closed"))
		// The premise: there IS a transaction pending. Without this the case below would
		// pass on a runner with nothing to lose.
		if !m.runner.pending() {
			t.Fatal("the fixture has no pending transaction, so this proves nothing")
		}

		out, _ := fold(m, queryExecutedMsg{
			sql:          "INSERT INTO orders (id) VALUES (1)",
			commitFailed: true,
			err:          context.DeadlineExceeded,
		})

		if out.conn != nil {
			t.Error("the app still holds a connection pgx closed; every later statement will fail on it")
		}
		if out.runner != nil {
			t.Error("the app still holds a runner bound to that connection")
		}
		said := toastText(out)
		if !strings.Contains(strings.ToLower(said), "connection") {
			t.Errorf("the user is not told the connection went: %q", said)
		}
		if !strings.Contains(strings.ToLower(said), "reconnect") {
			t.Errorf("the user is not told how to get back: %q", said)
		}
		// The project survives, so the way back is one keypress rather than a restart.
		if out.project == nil {
			t.Error("the project was dropped, so reconnecting means choosing a database again")
		}
		// And the rollback offers to roll nothing back: the transaction lived inside the
		// connection that just died, so the user reading "tx pending" and pressing U would
		// get a refusal. That is the honest answer and it is a change in behaviour, so it is
		// asserted from the user's side rather than from the field.
		rolled, rollbackCmd := fold(out, tea.KeyPressMsg{Code: 'U', Text: "U"})
		if rollbackCmd != nil {
			t.Errorf("the app issued a rollback against a dead connection: %T", rollbackCmd())
		}
		if rolled.conn != nil {
			t.Error("the rollback reconnected behind the user's back")
		}
		if m.dbBusy() {
			t.Error("the model still reports a database operation in flight after the connection died")
		}
	})

	t.Run("a connection that survived is KEPT", func(t *testing.T) {
		// The counterweight, and the case that says the fix is a question rather than a
		// verdict. The commit landed and only the acknowledgement was lost: the DML is
		// stored, the connection works, and telling the user to reconnect would be false.
		m := routerModelLoaded(t)
		m.state = StateMain
		m.editorOpen = true
		m.conn = pgxfake.New()
		m.runner = runnerWith(t, nil)
		m.runner.tx = &txnFailingTx{}

		out, _ := fold(m, queryExecutedMsg{
			sql:          "INSERT INTO orders (id) VALUES (1)",
			commitFailed: true,
			err:          context.DeadlineExceeded,
		})

		if out.conn == nil {
			t.Error("a working connection was dropped")
		}
		if out.runner == nil {
			t.Error("a working runner was dropped")
		}
		said := toastText(out)
		if strings.Contains(strings.ToLower(said), "reconnect") {
			t.Errorf("the user was told to reconnect over a live connection: %q", said)
		}
		// And the message admits what it does not know, which is the whole point: "the
		// transaction may or may not have been committed" is true in exactly this case.
		if !strings.Contains(strings.ToLower(said), "may or may not") {
			t.Errorf("the message does not say the outcome is unknown: %q", said)
		}
	})

	t.Run("a runner that cannot be pinged is left alone", func(t *testing.T) {
		// No ping hook at all, which is what a connectionless runner has. Nothing was proved
		// lost, so nothing may be dropped — claiming otherwise would throw away a connection
		// on no evidence.
		m := routerModelLoaded(t)
		m.state = StateMain
		m.conn = pgxfake.New()
		m.runner = newStatementRunner(nil)
		m.runner.tx = &txnFailingTx{}

		out, _ := fold(m, queryExecutedMsg{
			sql:          "INSERT INTO orders (id) VALUES (1)",
			commitFailed: true,
			err:          context.DeadlineExceeded,
		})

		if out.conn == nil {
			t.Error("a connection was dropped because the runner could not be asked")
		}
		if said := toastText(out); strings.Contains(strings.ToLower(said), "connection was lost") {
			t.Errorf("the message claims the connection was lost with nothing to support it: %q", said)
		}
	})

	t.Run("and a statement that did not fail its commit is not treated as one", func(t *testing.T) {
		// The narrowness of the new branch. A plain statement failure must not ping and must
		// not drop the connection, or a constraint violation would cost a round trip and
		// could disconnect a user whose database is fine.
		m := routerModelLoaded(t)
		m.state = StateMain
		m.editorOpen = true
		m.conn = pgxfake.New()
		m.runner = runnerWith(t, errors.New("conn closed"))
		m.runner.tx = &txnFailingTx{}

		out, _ := fold(m, queryExecutedMsg{
			sql: "INSERT INTO orders (id) VALUES (1)",
			err: errors.New("duplicate key value violates unique constraint"),
		})

		if out.conn == nil {
			t.Error("a constraint violation dropped the connection")
		}
		if said := toastText(out); !strings.Contains(said, "Query failed") {
			t.Errorf("a statement failure stopped being reported as one: %q", said)
		}
	})

	t.Run("usable asks the connection and says so", func(t *testing.T) {
		// The predicate on its own, because the handler's correctness is entirely this one
		// answer and a reader should not have to read the handler to check it.
		if !newStatementRunner(nil).usable(context.Background()) {
			t.Error("a runner with no ping hook reports an unusable connection")
		}
		var nilRunner *statementRunner
		if !nilRunner.usable(context.Background()) {
			t.Error("a nil runner reports an unusable connection")
		}
	})
}
