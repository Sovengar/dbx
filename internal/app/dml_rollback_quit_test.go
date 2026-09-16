package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// Plan: quitting with a pending transaction must roll it back before the
// connection is closed (graceful shutdown path).
func TestQuit_RollsBackPendingTransaction(t *testing.T) {
	f := newFakeRunner()
	m := newRollbackTestModel()
	m.runner = f.runner

	runExecuteQuery(t, m, "UPDATE users SET name='test' WHERE id=1")

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q'})
	if cmd == nil {
		t.Fatal("quit did not return a command")
	}
	if f.txs[0].rollbacks != 1 {
		t.Fatalf("ROLLBACK sent %d times on quit, want 1", f.txs[0].rollbacks)
	}
	if f.runner.pending() {
		t.Fatal("transaction still pending after quit")
	}
}
