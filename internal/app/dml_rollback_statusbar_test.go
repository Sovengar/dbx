package app

import (
	"strings"
	"testing"
)

// Scenario: UPDATE opens a transaction that stays open (statusbar indicator).
func TestPendingTransaction_ShowsInStatusbar(t *testing.T) {
	f := newFakeRunner()
	m := newModelWithGrid(t)
	m.statusbar.SetWidth(200)
	m.runner = f.runner

	msg := runExecuteQuery(t, m, "UPDATE users SET name='test' WHERE id=1")
	updated, _ := m.Update(msg)
	m = updated.(Model)

	view := m.statusbar.View()
	if !strings.Contains(view, "tx pending") {
		t.Fatalf("statusbar does not show 'tx pending': %q", view)
	}
	if !strings.Contains(view, "U rollback") {
		t.Fatalf("statusbar does not show 'U rollback': %q", view)
	}
}

// Scenario: U rolls back the pending transaction (statusbar indicator cleared).
func TestRollbackKey_ClearsStatusbarIndicator(t *testing.T) {
	f := newFakeRunner()
	m := newModelWithGrid(t)
	m.statusbar.SetWidth(200)
	m.runner = f.runner

	msg := runExecuteQuery(t, m, "UPDATE users SET name='test' WHERE id=1")
	updated, _ := m.Update(msg)
	m = updated.(Model)
	if !strings.Contains(m.statusbar.View(), "tx pending") {
		t.Fatalf("precondition failed: statusbar should show the pending transaction")
	}

	m = pressRollback(t, m)

	if strings.Contains(m.statusbar.View(), "tx pending") {
		t.Fatalf("statusbar still shows 'tx pending' after rollback: %q", m.statusbar.View())
	}
}

// Scenario: SELECT does not open a transaction (statusbar stays clean).
func TestNoPendingTransaction_StatusbarHasNoIndicator(t *testing.T) {
	f := newFakeRunner()
	m := newModelWithGrid(t)
	m.statusbar.SetWidth(200)
	m.runner = f.runner

	msg := runExecuteQuery(t, m, "SELECT * FROM users LIMIT 10")
	updated, _ := m.Update(msg)
	m = updated.(Model)

	if strings.Contains(m.statusbar.View(), "tx pending") {
		t.Fatalf("statusbar shows 'tx pending' after a SELECT: %q", m.statusbar.View())
	}
}
