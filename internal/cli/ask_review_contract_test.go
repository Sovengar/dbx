package cli

// Scenario: `dbx ask` y su paso de revision, que es lo que el CLI no tenia.
//
// The TUI ASK pane generates SQL and then WAITS: the query runs only after the
// user presses enter on the review. The CLI executed as soon as the model
// answered, so the two surfaces disagreed about whether a human ever sees the
// statement before it touches the database. `--dry-run` is the non-interactive
// review and `--confirm` the interactive one; the tests below pin both, and the
// property they share is the one that matters: a review that was not approved
// executes nothing.
//
// The assertion is deliberately about EXECUTION rather than about output. A
// dry-run that printed the SQL and then ran it anyway would pass an output-only
// test while being exactly the bug the flag exists to prevent.

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// executed reports whether the generated SQL reached the connection.
//
// The connection is searched rather than counted: the schema walk is three queries
// by itself, so "how many queries ran" is a fixture detail, while "did THIS
// statement run" is the contract.
func executed(conn *fakeConn, sql string) bool {
	for _, q := range conn.queried {
		if strings.Contains(q, sql) {
			return true
		}
	}
	return false
}

// TestAskDryRunPrintsTheSchemaAwareSQLWithoutExecuting is the whole point of the
// flag: the real schema reaches the model, the SQL reaches stdout, and the
// database sees it no further.
func TestAskDryRunPrintsTheSchemaAwareSQLWithoutExecuting(t *testing.T) {
	conn := askConn()
	provider := &stubProvider{name: "test-provider", sql: "SELECT id FROM users"}
	const question = "how many users?"

	var stdout, stderr bytes.Buffer
	if err := askAndReport(&stdout, &stderr, conn, provider, question, false, nil, nil, askOptions{dryRun: true}); err != nil {
		t.Fatalf("askAndReport: %v", err)
	}

	// It is a REVIEW, not `--sql-only`: the schema was read and given to the model,
	// which is what makes the printed SQL the one that would really run.
	if provider.asked != 1 {
		t.Errorf("the provider was asked %d times, want 1", provider.asked)
	}
	if !strings.Contains(provider.gotSchema, "users") {
		t.Errorf("the schema given to the model is %q, want the real one", provider.gotSchema)
	}

	// And the SQL is the RESULT here, so it goes to stdout — stderr is progress.
	// Printing it to stderr would make `dbx ask --dry-run > q.sql` write an empty file.
	if got := strings.TrimSpace(stdout.String()); got != "SELECT id FROM users" {
		t.Errorf("stdout = %q, want the generated SQL alone", got)
	}

	if executed(conn, "SELECT id FROM users") {
		t.Error("dry-run executed the generated SQL, which is the one thing it must not do")
	}
}

// TestAskDryRunJSONCarriesTheSQL is the agent-facing shape: the same review, in a
// document a script can read.
func TestAskDryRunJSONCarriesTheSQL(t *testing.T) {
	conn := askConn()
	provider := &stubProvider{name: "p", sql: "SELECT 1"}

	var stdout, stderr bytes.Buffer
	if err := askAndReport(&stdout, &stderr, conn, provider, "one?", true, nil, nil, askOptions{dryRun: true}); err != nil {
		t.Fatalf("askAndReport: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
	}
	if got["sql"] != "SELECT 1" {
		t.Errorf("the JSON sql field is %v, want the generated statement", got["sql"])
	}
	// A dry run has no rows, so the result keys have no business being here: a script
	// that reads `count` would see 0 and believe the query returned nothing.
	if _, ok := got["columns"]; ok {
		t.Errorf("the dry-run JSON carries result keys: %s", stdout.String())
	}
	if executed(conn, "SELECT 1") {
		t.Error("dry-run executed the generated SQL")
	}
}

// TestAskDryRunWinsOverConfirm pins the precedence of two "don't run it" flags
// that are combined by mistake: the dry run stops before the prompt, so a script
// that pipes no input is not left waiting on a question nobody will answer.
func TestAskDryRunWinsOverConfirm(t *testing.T) {
	conn := askConn()
	provider := &stubProvider{name: "p", sql: "SELECT 1"}

	var stdout, stderr bytes.Buffer
	if err := askAndReport(&stdout, &stderr, conn, provider, "one?", false, nil, strings.NewReader("y\n"), askOptions{dryRun: true, confirm: true}); err != nil {
		t.Fatalf("askAndReport: %v", err)
	}

	if strings.Contains(stderr.String(), "Execute this query?") {
		t.Errorf("a dry run prompted anyway:\n%s", stderr.String())
	}
	if executed(conn, "SELECT 1") {
		t.Error("dry-run executed the generated SQL, although the supplied answer was yes")
	}
}

// TestAskConfirmExecutesOnlyOnExplicitYes is the interactive review, and the
// default matters as much as the yes: the prompt says `[y/N]`, so everything that
// is not a deliberate affirmative has to leave the database untouched.
func TestAskConfirmExecutesOnlyOnExplicitYes(t *testing.T) {
	for _, tc := range []struct {
		name         string
		answer       string
		wantExecuted bool
	}{
		{name: "y", answer: "y\n", wantExecuted: true},
		{name: "yes", answer: "yes\n", wantExecuted: true},
		{name: "a capital Y", answer: "Y\n", wantExecuted: true},
		{name: "mixed case", answer: "YeS\n", wantExecuted: true},
		{name: "n", answer: "n\n", wantExecuted: false},
		{name: "an empty line", answer: "\n", wantExecuted: false},
		{name: "anything else", answer: "go ahead\n", wantExecuted: false},
		// EOF is the case a test normally cannot reach and the one a user hits by
		// pressing ctrl+D. It has to decline, not execute.
		{name: "an immediate EOF", answer: "", wantExecuted: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn := askConn()
			provider := &stubProvider{name: "p", sql: "SELECT id FROM users"}

			var stdout, stderr bytes.Buffer
			if err := askAndReport(&stdout, &stderr, conn, provider, "users?", false, nil, strings.NewReader(tc.answer), askOptions{confirm: true}); err != nil {
				t.Fatalf("askAndReport: %v", err)
			}

			if got := executed(conn, "SELECT id FROM users"); got != tc.wantExecuted {
				t.Errorf("executed = %v, want %v (answer %q)", got, tc.wantExecuted, tc.answer)
			}

			// The prompt and the verdict are both on stderr, so stdout stays the
			// result stream. A declined review says so rather than looking like a run
			// that produced no rows.
			if !strings.Contains(stderr.String(), "Execute this query? [y/N]") {
				t.Errorf("stderr does not carry the prompt:\n%s", stderr.String())
			}
			declined := !tc.wantExecuted
			if got := strings.Contains(stderr.String(), "Cancelled."); got != declined {
				t.Errorf("Cancelled. present = %v, want %v", got, declined)
			}
		})
	}
}

// TestAskDryRunPropagatesAWriteError is the "return err" arm of the new stdout
// write. A bytes.Buffer never fails, so without a writer that refuses there is
// nothing to tell "reported the failure" from "exited 0 having printed nothing",
// and the second is what a script piping the SQL would see.
func TestAskDryRunPropagatesAWriteError(t *testing.T) {
	conn := askConn()
	provider := &stubProvider{name: "p", sql: "SELECT 1"}

	err := askAndReport(errWriter{err: errWriteRefused}, &bytes.Buffer{}, conn, provider, "one?", false, nil, nil, askOptions{dryRun: true})
	if err == nil {
		t.Fatal("a refused stdout write reported success")
	}
	if !strings.Contains(err.Error(), "the terminal went away") {
		t.Errorf("the error is %q, want it to carry the write failure", err)
	}
}
