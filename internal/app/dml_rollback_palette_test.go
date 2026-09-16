package app

import (
	"testing"
)

// Scenario: Rollback command available in the palette.
func TestPalette_RollbackCommandRollsBackPendingTransaction(t *testing.T) {
	f := newFakeRunner()
	m := newRollbackTestModel()
	m.txRunner = f.runner

	runExecuteQuery(t, m, "UPDATE users SET name='test' WHERE id=1")

	updated, _ := m.handlePaletteCommand("global.rollback")
	m = updated.(Model)

	if f.txs[0].rollbacks != 1 {
		t.Fatalf("ROLLBACK sent %d times, want 1", f.txs[0].rollbacks)
	}
	if f.runner.pending() {
		t.Fatal("transaction still pending after the palette command")
	}
	if !lastToastContains(m, "Transaction rolled back") {
		t.Fatalf("toast does not say 'Transaction rolled back': %q", lastToastText(t, m))
	}
}
