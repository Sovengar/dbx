package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// Scenario: running a grid draft also commits the transaction it opens, so
// the reviewed changes become durable instead of being rolled back on quit.
func TestExecuteQuery_GridDraftCommitsTransaction(t *testing.T) {
	f := newFakeRunner()
	m := newRollbackTestModel()
	m.runner = f.runner

	const sql = "UPDATE users SET name='test' WHERE id=1"
	m.editor.SetContent(sql)
	m.editor.SetCommitOnRun(true)

	msg := runExecuteQuery(t, m, sql)
	if msg.err != nil {
		t.Fatalf("executeQuery() error = %v", msg.err)
	}
	if !msg.committedTx {
		t.Fatal("queryExecutedMsg did not report the transaction as committed")
	}
	if f.txs[0].commits != 1 {
		t.Fatalf("COMMIT sent %d times, want 1", f.txs[0].commits)
	}
	if f.runner.pending() {
		t.Fatal("transaction still pending after running a grid draft")
	}
}

// Scenario: manually typed SQL keeps the rollback window open (press U).
func TestExecuteQuery_ManualEditorKeepsTransactionPending(t *testing.T) {
	f := newFakeRunner()
	m := newRollbackTestModel()
	m.runner = f.runner

	const sql = "UPDATE users SET name='test' WHERE id=1"
	m.editor.SetContent(sql) // no commit-on-run intent

	msg := runExecuteQuery(t, m, sql)
	if msg.err != nil {
		t.Fatalf("executeQuery() error = %v", msg.err)
	}
	if msg.committedTx {
		t.Fatal("manual execution reported a commit")
	}
	if f.txs[0].commits != 0 {
		t.Fatal("manual execution committed the transaction")
	}
	if !f.runner.pending() {
		t.Fatal("manual execution left no pending transaction to roll back")
	}
}

// Scenario: cancelling the editor with Esc drops the commit intent, so a later
// execution cannot commit by surprise.
func TestEditorEscResetsCommitOnRun(t *testing.T) {
	m := newRollbackTestModel()
	m.editorOpen = true
	m.editor.SetContent("UPDATE users SET name='test' WHERE id=1")
	m.editor.SetCommitOnRun(true)

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	model, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update(esc) returned %T, want Model", updated)
	}
	if model.editorOpen {
		t.Fatal("Esc did not close the editor")
	}
	if model.editor.CommitOnRun() {
		t.Fatal("Esc left the commit-on-run intent set")
	}
}
