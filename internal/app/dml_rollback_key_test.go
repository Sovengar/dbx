package app

import (
	"testing"
)

// Scenario: U rolls back the pending transaction.
func TestRollbackKey_RollsBackPendingTransaction(t *testing.T) {
	f := newFakeRunner()
	m := newRollbackTestModel()
	m.runner = f.runner

	runExecuteQuery(t, m, "UPDATE users SET name='test' WHERE id=1")

	m = pressRollback(t, m)

	if f.txs[0].rollbacks != 1 {
		t.Fatalf("ROLLBACK sent %d times, want 1", f.txs[0].rollbacks)
	}
	if f.runner.pending() {
		t.Fatal("transaction still pending after U")
	}
	if !lastToastContains(m, "Transaction rolled back") {
		t.Fatalf("toast does not say 'Transaction rolled back': %q", lastToastText(t, m))
	}
}

// Scenario: U with no pending transaction shows info toast.
func TestRollbackKey_NoPendingTransactionShowsInfoToast(t *testing.T) {
	f := newFakeRunner()
	m := newRollbackTestModel()
	m.runner = f.runner

	m = pressRollback(t, m)

	if f.begins != 0 || len(f.txs) != 0 {
		t.Fatal("a transaction was opened for a rollback with nothing pending")
	}
	if !lastToastContains(m, "No pending transaction") {
		t.Fatalf("toast does not say 'No pending transaction': %q", lastToastText(t, m))
	}
}

// Scenario: pressing U rolls back ALL statements in a batch.
func TestRollbackKey_RollsBackWholeBatch(t *testing.T) {
	f := newFakeRunner()
	m := newRollbackTestModel()
	m.runner = f.runner

	runExecuteQuery(t, m, "UPDATE users SET name='a' WHERE id=1; UPDATE users SET name='b' WHERE id=2")

	m = pressRollback(t, m)

	if f.txs[0].rollbacks != 1 {
		t.Fatalf("ROLLBACK sent %d times, want 1", f.txs[0].rollbacks)
	}
	if f.begins != 1 {
		t.Fatalf("BEGIN sent %d times, want 1 — the batch must share one transaction", f.begins)
	}
	if f.txs[0].commits != 0 {
		t.Fatal("batch was committed before the rollback")
	}
	if f.runner.pending() {
		t.Fatal("transaction still pending after U")
	}
}
