package app

import (
	"testing"
)

func TestExecuteQuery_AutoCommitIsReported(t *testing.T) {
	f := newFakeRunner()
	m := newRollbackTestModel()
	m.runner = f.runner

	first := runExecuteQuery(t, m, "UPDATE users SET name='a' WHERE id=1")
	if first.committedTx {
		t.Fatal("first execution reported a commit when nothing was pending")
	}

	second := runExecuteQuery(t, m, "UPDATE users SET name='b' WHERE id=2")
	if !second.committedTx {
		t.Fatal("auto-commit of the previous transaction was not reported")
	}
}

func TestQueryExecuted_AutoCommitShowsToast(t *testing.T) {
	f := newFakeRunner()
	m := newModelWithGrid(t)
	m.runner = f.runner

	runExecuteQuery(t, m, "UPDATE users SET name='a' WHERE id=1")
	msg := runExecuteQuery(t, m, "UPDATE users SET name='b' WHERE id=2")

	updated, _ := m.Update(msg)
	m = updated.(Model)

	if !lastToastContains(m, "Transaction committed") {
		t.Fatalf("toast does not say 'Transaction committed': %q", lastToastText(t, m))
	}
}

func TestQueryExecuted_NoAutoCommitNoToast(t *testing.T) {
	f := newFakeRunner()
	m := newModelWithGrid(t)
	m.runner = f.runner

	msg := runExecuteQuery(t, m, "SELECT * FROM users")

	updated, _ := m.Update(msg)
	m = updated.(Model)

	if lastToastContains(m, "Transaction committed") {
		t.Fatalf("unexpected 'Transaction committed' toast: %q", lastToastText(t, m))
	}
}
