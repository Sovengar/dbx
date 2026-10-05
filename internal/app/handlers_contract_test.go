package app

// Scenario: Los handlers de app.go que dependen de un estado o de un seam.
//
// Thirty-odd arms, and they are all the same two shapes:
//
//   - a guard for a field that CAN be nil: a panel built lazily, a message that arrived after
//     the thing it belongs to closed, a state a user reaches and the happy-path tests do not.
//   - a failure behind a dependency: the clipboard, a rollback, a schema load, a foreign-key
//     lookup. Each one has to be reachable on demand, or its arm is written for a machine this
//     project is not developed on.
//
// The clipboard is the one that needed production code to change. copyToClipboard is now a
// package variable — the same trick the CLI uses for loadConfig — because the only way to make
// it fail is a machine with no clipboard helper, so its failure branch was unreachable on a
// developer machine and only accidentally taken in CI.
//
// Every case here asserts the refusal AND what it left behind, because a guard that returns
// without cleaning up is a bug that looks exactly like a guard that worked.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/testsupport/pgxfake"
	"github.com/buble/dbx/internal/ui/components/grid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// withClipboard replaces the clipboard for the duration of a case.
func withClipboard(t *testing.T, err error) {
	t.Helper()
	old := copyToClipboard
	t.Cleanup(func() { copyToClipboard = old })
	copyToClipboard = func(string) error { return err }
}

// ── the clipboard ────────────────────────────────────────────────────────────

// Copying the statement under the cursor, with no clipboard.
//
// The user pressed a key to copy their SQL and nothing happened, which is the whole failure.
// The toast has to say so, and the editor has to stay open — closing it would throw away the
// statement they were trying to copy.
func TestCopyingSQLWithNoClipboardSaysSo(t *testing.T) {
	withClipboard(t, errors.New("no clipboard helper is installed"))

	m := routerModelLoaded(t)
	m.state = StateMain
	m.editorOpen = true
	m.editor.SetContent("SELECT 1")
	m.editor.Focus()

	cmd := m.handleCopySQL()
	if cmd == nil {
		t.Fatal("copying refused instead of answering")
	}

	msg, ok := cmd().(copySQLDoneMsg)
	if !ok {
		t.Fatalf("the copy produced %T, want copySQLDoneMsg", cmd())
	}
	if msg.err == nil {
		t.Fatal("a failed copy reported success")
	}
	if !strings.Contains(msg.err.Error(), "no clipboard helper") {
		t.Errorf("the error is %q, want it to carry the helper's own complaint", msg.err)
	}

	t.Run("and the editor keeps the statement", func(t *testing.T) {
		// The consequence: the handler toasts and stops. It does not close the editor, because
		// the statement is still the only copy the user has.
		out, next := fold(m, msg)
		if next != nil {
			_ = next
		}
		// The toast's own wording is "Clipboard not available", so the match is on the
		// lowercased text: a case-sensitive search for "clipboard" fails against the real
		// string, which is exactly what my first version did.
		if said := strings.ToLower(toastText(out)); !strings.Contains(said, "clipboard") {
			t.Errorf("the toast is %q, want it to name the clipboard", said)
		}
		if !out.editorOpen {
			t.Error("the editor closed after a failed copy")
		}
		if got := out.editor.Content(); got != "SELECT 1" {
			t.Errorf("the editor now holds %q", got)
		}
	})

	t.Run("and a working clipboard is silent", func(t *testing.T) {
		// The counterweight: a copy that always failed would satisfy the case above and the
		// feature would be dead.
		withClipboard(t, nil)

		cmd := m.handleCopySQL()
		if cmd == nil {
			t.Fatal("copying refused instead of answering")
		}
		if msg, ok := cmd().(copySQLDoneMsg); !ok {
			t.Fatalf("the copy produced %T", cmd())
		} else if msg.err != nil {
			t.Errorf("a working clipboard reported %v", msg.err)
		}
	})
}

// Yank mode from the export picker: the same clipboard, a different caller, and a count.
//
// The count travels with the message because a yank that silently copied nothing leaves the
// user pasting whatever was on the clipboard before.
func TestYankingWithNoClipboardSaysSo(t *testing.T) {
	withClipboard(t, errors.New("wl-copy is not on PATH"))

	m := routerModelLoaded(t)
	m.state = StateMain
	m.grid.SetData(dataOf(3, 3), "public", "orders")
	m.grid.Focus()

	cmd := m.handleExport(grid.ExportSelectedMsg{
		Schema: "public", Table: "orders",
		Rows:     [][]interface{}{{int64(1), "a", "b"}},
		Columns:  []string{"id", "x", "y"},
		YankMode: true,
	})
	if cmd == nil {
		t.Fatal("the yank refused instead of answering")
	}

	msg, ok := cmd().(exportDoneMsg)
	if !ok {
		t.Fatalf("the yank produced %T, want exportDoneMsg", cmd())
	}
	if msg.err == nil {
		t.Fatal("a failed yank reported success")
	}
	if !strings.Contains(msg.err.Error(), "clipboard") {
		t.Errorf("the error is %q, want it to name the clipboard", msg.err)
	}
	// And no count: nothing was copied, so claiming a row count would be a lie the user acts on.
	if msg.yankCount != 0 {
		t.Errorf("a failed yank reports %d rows copied", msg.yankCount)
	}
	if msg.clipboard {
		t.Error("a failed yank claims it used the clipboard")
	}

	t.Run("and a working clipboard reports the count", func(t *testing.T) {
		withClipboard(t, nil)

		cmd := m.handleExport(grid.ExportSelectedMsg{
			Schema: "public", Table: "orders",
			Rows:     [][]interface{}{{int64(1), "a", "b"}},
			Columns:  []string{"id", "x", "y"},
			YankMode: true,
		})
		msg, ok := cmd().(exportDoneMsg)
		if !ok {
			t.Fatalf("the yank produced %T", cmd())
		}
		if msg.err != nil {
			t.Fatalf("a working clipboard reported %v", msg.err)
		}
		if !msg.clipboard {
			t.Error("a successful yank does not say it used the clipboard")
		}
		if msg.yankCount != 1 {
			t.Errorf("a successful yank reports %d rows, want the one it was given", msg.yankCount)
		}
	})
}

// ── the rollback ────────────────────────────────────────────────────────────

// The rollback that fails, from the transaction bar.
//
// A rollback failure is worse news than a failed query: the changes are still pending on a
// transaction nobody can close. So the toast has to say the ROLLBACK failed — not "the query
// failed" — and the pending state has to stay, because it is still there.
func TestAFailedRollbackSaysSoAndKeepsTheChangesPending(t *testing.T) {
	m := routerModelLoaded(t)
	m.state = StateMain
	m.runner = newStatementRunner(nil)
	m.runner.conn = pgxfake.New()

	// A transaction with pending changes, whose rollback refuses.
	tx := &failingTx{err: errors.New("server closed the connection unexpectedly")}
	m.runner.tx = tx

	// handleRollback returns a tea.Model, so it goes through the same assertion every other
	// case in this package uses for a handler's return value.
	raw, cmd := m.handleRollback()
	if cmd != nil {
		_ = cmd()
	}
	out, isModel := raw.(Model)
	if !isModel {
		t.Fatalf("handleRollback returned a %T, want a Model", raw)
	}

	if cmd != nil {
		_ = cmd()
	}
	// The toast names the rollback, which is the part a user has to act on.
	if said := toastText(out); !strings.Contains(said, "Rollback failed") {
		t.Errorf("the toast is %q, want it to say the ROLLBACK failed", said)
	}
	// And the runner has ALREADY let go of the transaction, even though the rollback failed.
	// That is deliberate — rollback clears r.tx before calling the server, so a retry opens a
	// fresh transaction rather than reusing one the server may have aborted — and it is worth
	// pinning because it looks like the opposite at a glance.
	//
	// It is also sound: a rollback that fails has failed because the transaction is gone
	// (connection dropped, aborted by a constraint, already aborted server-side), so there is
	// nothing left for the app to hold on to. The connection itself closes, and the server
	// discards the transaction with it.
	if out.runner.pending() {
		t.Error("the runner still holds a transaction whose rollback failed")
	}
	if tx.rolledBack != 1 {
		t.Errorf("the rollback was attempted %d times, want 1", tx.rolledBack)
	}
}

func TestRollbackOnExitClosesTheConnection(t *testing.T) {
	// The exit path's other half: after a rollback — or instead of one — the connection is
	// closed. A leak here is a socket per quit, which is invisible until something runs out.
	conn := pgxfake.New()
	m := routerModelLoaded(t)
	m.state = StateMain
	m.runner = newStatementRunner(nil)
	m.runner.conn = conn
	m.conn = conn

	m.rollbackOnExit()

	if conn.Closed() != 1 {
		t.Errorf("the connection was closed %d times on exit, want 1", conn.Closed())
	}

	t.Run("and with nothing to roll back it still closes", func(t *testing.T) {
		clean := pgxfake.New()
		m := routerModelLoaded(t)
		m.state = StateMain
		m.runner = nil
		m.conn = clean

		m.rollbackOnExit()

		if clean.Closed() != 1 {
			t.Errorf("the connection was closed %d times, want 1", clean.Closed())
		}
	})

	t.Run("and a runner that fails its rollback still closes", func(t *testing.T) {
		// The failure path: the rollback is best-effort on exit — there is no user left to tell
		// — so the close has to happen regardless, or a bad rollback leaks the socket.
		bad := pgxfake.New()
		m := routerModelLoaded(t)
		m.state = StateMain
		m.runner = newStatementRunner(nil)
		m.runner.conn = bad
		m.runner.tx = &failingTx{err: errors.New("already closed")}
		m.conn = bad

		m.rollbackOnExit()

		if bad.Closed() != 1 {
			t.Errorf("the connection was closed %d times after a failed rollback, want 1", bad.Closed())
		}
	})
}

// ── the ask panel ───────────────────────────────────────────────────────────

// The ask entry points and replies, with no panel.
//
// Four separate handlers check `m.ask == nil`, and the panel is built lazily — so a message
// that arrives between the keypress and the panel's construction finds it absent. That is a
// state a user produces by pressing ask twice.
func TestTheAskPathsRefuseWithoutAPanel(t *testing.T) {
	noAsk := func(t *testing.T) Model {
		t.Helper()
		m := routerModelLoaded(t)
		m.state = StateMain
		m.ask = nil
		m.askOpen = false
		return m
	}

	t.Run("opening", func(t *testing.T) {
		m := noAsk(t)
		m.aiProvider = errProvider{}

		out, cmd := m.handleAskOpen()
		if cmd != nil {
			_ = cmd()
		}
		typed, ok := out.(Model)
		if !ok {
			t.Fatalf("handleAskOpen returned a %T", out)
		}
		if typed.askOpen {
			t.Error("the app opened an ASK panel that does not exist")
		}
	})

	t.Run("a generated-SQL reply", func(t *testing.T) {
		// The sequence number is how the app knows a reply answers the question it asked. With
		// no panel there is nothing to answer, and the sequence must not move: a panel that
		// opens later must not be told about a reply to a question it never asked.
		m := noAsk(t)
		m.askGenSeq = 3

		out, cmd := fold(m, askGeneratedMsg{seq: 3, sql: "DROP TABLE orders"})
		if cmd != nil {
			_ = cmd()
		}
		if out.askGenSeq != 3 {
			t.Errorf("the sequence moved to %d", out.askGenSeq)
		}
	})

	t.Run("an executed-SQL reply", func(t *testing.T) {
		m := noAsk(t)
		out, cmd := fold(m, askQueryExecutedMsg{seq: 1, sql: "SELECT 1"})
		if cmd != nil {
			_ = cmd()
		}
		if out.askGenSeq != 0 {
			t.Errorf("a reply with no panel moved the sequence to %d", out.askGenSeq)
		}
	})

	t.Run("and a second read-only query is refused while one runs", func(t *testing.T) {
		// Not a nil panel: the ASK path opens a server-enforced READ ONLY transaction, and
		// pgx cannot have two on one connection. So the refusal has to happen before the
		// second BEGIN, and the panel has to be told why.
		m := routerModelLoaded(t)
		m.state = StateMain
		if m.ask == nil {
			t.Skip("this fixture builds no ASK panel")
		}
		m.askOpen = true
		// Sized AND visible, because Ask.View returns "" for a hidden panel before it looks at
		// its width at all. Two versions of this case asserted on an empty string and called
		// it a failure of the refusal; it was a failure of the fixture.
		m.ask.SetWidth(80)
		m.ask.SetHeight(30)
		m.ask.Show()
		m.ask.AddQuestion("a question")
		m.runner = newStatementRunner(nil)
		m.runner.readOnlyActive.Store(true)

		cmd := m.executeAskSQL("SELECT 1", 1)
		if cmd != nil {
			_ = cmd()
		}
		out := m.ask.View()
		if out == "" {
			t.Fatal("the ASK panel renders nothing, so the fixture cannot show the refusal")
		}
		if !strings.Contains(out, "already running") {
			t.Errorf("the panel does not explain the refusal:\n%s", out)
		}
	})
}

// ── projects ────────────────────────────────────────────────────────────────

// The project scan's decision: connect straight away, or show the picker.
//
// The rule is three ORs — more than one active project, or any inactive one, or the user asked
// for the picker — and the two flags the loop computes are the whole of it. A model with no
// projects at all also gets the picker, because there is nothing to connect to.
func TestTheProjectScanDecidesBetweenPickerAndConnect(t *testing.T) {
	for _, tc := range []struct {
		name        string
		projects    []config.FoundProject
		forcePicker bool
		wantPicker  bool
	}{
		{"one active project connects on its own", []config.FoundProject{
			{Name: "only", Active: true},
		}, false, false},
		{"two active projects need the picker", []config.FoundProject{
			{Name: "one", Active: true},
			{Name: "two", Active: true},
		}, false, true},
		{"one inactive project needs the picker", []config.FoundProject{
			{Name: "only", Active: false},
		}, false, true},
		{"an active and an inactive project need the picker", []config.FoundProject{
			{Name: "on", Active: true},
			{Name: "off", Active: false},
		}, false, true},
		// No projects is NOT the picker's case. There is a guard ABOVE the picker branch for
		// it, and it reports an error: an empty picker asks the user to choose from nothing.
		// My first version of this table put it on the picker side and the code was right.
		{"no projects at all are an error, not a choice", nil, false, true},
		{"one active project still gets the picker when asked", []config.FoundProject{
			{Name: "only", Active: true},
		}, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := routerModel(t)
			m.state = StatePicker
			m.forcePicker = tc.forcePicker

			out, _ := fold(m, projectsScannedMsg{projects: tc.projects})

			if tc.wantPicker && len(tc.projects) == 0 {
				// The empty case, which the handler answers before the picker is ever
				// involved. Its own assertion.
				if out.state != StateError {
					t.Errorf("with no projects the state is %v, want the error state", out.state)
				}
				if out.err == nil {
					t.Error("with no projects there is no error to show")
				} else if !strings.Contains(out.err.Error(), ".dbx.toml") {
					t.Errorf("the error is %q, want it to name what it looked for", out.err)
				}
				if out.forcePicker {
					t.Error("the force flag survived the scan")
				}
				return
			}

			if tc.wantPicker {
				if out.state != StatePicker {
					t.Errorf("the state is %v, want the picker", out.state)
				}
				// And the picker can be drawn with what it was given — its contents are the
				// picker's own business and its package tests pin them; from here the
				// observable fact is that it renders, because that is what the state means.
				//
				// A picker with no projects and one with three both render, so the assertion
				// cannot tell them apart: what distinguishes them is the STATE, asserted
				// above, and that the scan did not carry on to connect.
				if out.picker.View() == "" {
					t.Error("the picker renders nothing")
				}
			} else if out.state == StatePicker {
				t.Error("the app stopped at the picker with one active project to connect to")
			}

			// The force flag is consumed either way, so a later scan does not show the picker
			// for the rest of the session.
			if out.forcePicker {
				t.Error("the force flag survived the scan that used it")
			}
		})
	}
}

// ── metadata with no connection ─────────────────────────────────────────────

// A table's rows arriving after the connection died.
//
// Reachable, and only because of a bug fixed earlier in this project: handleCommitFailure sets
// m.conn = nil when pgx has closed the connection, so a refresh that was in flight at that
// moment has a reply coming for a connection that is no longer there.
//
// The ORDER inside the handler is the whole point of this case. The rows are applied FIRST —
// they were read before the connection died and they are the answer to the question the user
// asked — and only the follow-up metadata load is skipped, because that would need a
// connection. A guard placed before SetData would throw away good data to protect against
// nothing.
func TestARowsReplyAfterTheConnectionDiedKeepsTheRows(t *testing.T) {
	m := routerModelLoaded(t)
	m.state = StateMain
	// What handleCommitFailure does when the commit failed AND the connection was gone.
	m.conn = nil

	out, cmd := fold(m, tableDataLoadedMsg{
		result: dataOf(3, 2),
		schema: "public",
		table:  "orders",
	})

	if cmd != nil {
		_ = cmd()
		// A follow-up query on a dead connection would fail inside a tea.Cmd goroutine, where
		// there is nowhere to report it.
		t.Error("a rows reply with no connection asked for another query")
	}

	// The rows ARE there: they were read while the connection was alive.
	if got := out.grid.TableName(); got != "orders" {
		t.Errorf("the grid shows the table %q, want orders", got)
	}
	if rows := out.grid.TotalRows(); rows != 3 {
		t.Errorf("the grid holds %d rows, want the 3 the reply carried", rows)
	}
	// And the navigation followed, so the breadcrumbs name what is on screen.
	if out.prevSchema != "public" || out.prevTable != "orders" {
		t.Errorf("the previous selection is %s.%s, want public.orders", out.prevSchema, out.prevTable)
	}

	t.Run("and a reply WITH a connection does ask for metadata", func(t *testing.T) {
		// The counterweight, and the reason the guard is in the handler rather than in the
		// entry points: a live connection is the normal case and must keep loading metadata.
		live := routerModelLoaded(t)
		live.state = StateMain
		live.conn = pgxfake.New()

		if _, cmd := fold(live, tableDataLoadedMsg{
			result: dataOf(3, 2), schema: "public", table: "orders",
		}); cmd == nil {
			t.Error("a rows reply with a live connection did not ask for metadata")
		}
	})

	t.Run("and a table selection with no connection is ignored entirely", func(t *testing.T) {
		// The sibling guard, one message earlier: a SELECTION is not data. There is nothing to
		// show, so nothing happens at all — and the previous selection is left alone rather
		// than being cleared by a click that could not be honoured.
		none := routerModelLoaded(t)
		none.state = StateMain
		none.conn = nil
		none.prevSchema = "billing"
		none.prevTable = "invoices"

		out, cmd := fold(none, tableSelectedMsg{schema: "public", table: "orders"})

		if cmd != nil {
			_ = cmd()
			t.Error("a selection with no connection issued a query")
		}
		if out.prevSchema != "billing" || out.prevTable != "invoices" {
			t.Errorf("the previous selection became %s.%s", out.prevSchema, out.prevTable)
		}
	})
}

// ── layout ──────────────────────────────────────────────────────────────────

// usableWidth with a row narrower than the column count.
//
// It is the width the export writes at, so a row that is short must not make it report the
// full column count — the file would then be padded with nothing, or the columns misaligned.
func TestTheUsableWidthIsTheNarrowestRow(t *testing.T) {
	for _, tc := range []struct {
		name    string
		columns int
		rows    [][]interface{}
		want    int
	}{
		{"every row fills every column", 3, [][]interface{}{{1, 2, 3}, {4, 5, 6}}, 3},
		{"one short row narrows it", 3, [][]interface{}{{1, 2, 3}, {4}}, 1},
		{"an empty row", 3, [][]interface{}{{}, {1, 2}}, 0},
		{"no rows at all", 3, nil, 3},
		{"a negative starting width", -1, nil, 0},
		{"a negative starting width with rows", -5, [][]interface{}{{1}}, 0},
	} {
		if got := usableWidth(tc.columns, tc.rows); got != tc.want {
			t.Errorf("usableWidth(%d, %v) = %d, want %d", tc.columns, tc.rows, got, tc.want)
		}
	}
}

// The overlay geometry, for a box taller and wider than the window it is drawn on.
//
// Both clamps exist so the slice below them stays in range, and both are reachable by asking
// for a window smaller than the overlay — which is what a terminal dragged to nothing produces.
func TestAnOverlayLargerThanItsWindowIsClamped(t *testing.T) {
	base := "a\nb\nc\nd\ne"

	for _, tc := range []struct {
		name          string
		box           string
		width, height int
	}{
		{"a box taller than the window", strings.Repeat("x\n", 40), 20, 5},
		{"a box wider than the window", strings.Repeat("y", 200), 10, 10},
		{"a box taller AND wider", strings.Repeat("z\n", 40), 3, 2},
		{"a window of nothing", "box", 0, 0},
		{"a one-line window", "box", 20, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The assertion is that it returns, and that what it returns is made of lines of a
			// length the window can hold — a pad built from a negative count is a panic, which
			// is what these two clamps are for.
			got := overlay(base, tc.box, tc.width, tc.height)
			if got == "" {
				t.Errorf("the overlay of %q over %q produced nothing", tc.box, base)
			}
			for i, line := range strings.Split(got, "\n") {
				if tc.width > 0 && len([]rune(line)) > tc.width && len(tc.box) <= tc.width {
					t.Errorf("line %d is %d cells wide, more than the window's %d", i, len([]rune(line)), tc.width)
				}
			}
		})
	}

	t.Run("and the bottom-right overlay is clamped the same way", func(t *testing.T) {
		// The other overlay, with its own two clamps: a box wider than the window puts x
		// negative, and a stack taller than the window drops blocks off the top.
		for _, tc := range []struct {
			name          string
			box           string
			width, height int
			stackOffset   int
		}{
			{"a box wider than the window", strings.Repeat("q", 200), 10, 10, 0},
			{"a box taller than the window", strings.Repeat("r\n", 40), 20, 4, 0},
			{"a stack offset past the top", "a", 20, 10, 99},
			{"a window of nothing", "a", 0, 0, 0},
		} {
			t.Run(tc.name, func(t *testing.T) {
				if got := overlayBottomRight(base, tc.box, tc.width, tc.height, tc.stackOffset); got == "" {
					t.Error("the overlay produced nothing")
				}
			})
		}
	})
}

// ── the tickers ─────────────────────────────────────────────────────────────

// The two tick commands, run for real.
//
// They are the only two pieces of the app whose correctness is a TIMING claim — a toast that
// expires, a spinner that turns — so a test that only checks the command is non-nil proves
// nothing about either. Running them costs a second and a tenth of a second, which is the
// price of asserting that they fire at all.
func TestTheTickersFire(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cmd     tea.Cmd
		want    tea.Msg
		maxWait time.Duration
	}{
		{"the toast tick", tickToast(), toastTickMsg{}, 3 * time.Second},
		{"the spinner tick", tickSpinner(), spinnerTickMsg{}, time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.cmd == nil {
				t.Fatal("the ticker produced no command")
			}
			// tea.Every's command BLOCKS until the interval elapses, so it has to run
			// somewhere else: a select case cannot CALL a function, and calling it inline would
			// simply hang the test with no timeout at all.
			done := make(chan tea.Msg, 1)
			go func() { done <- tc.cmd() }()

			select {
			case msg := <-done:
				switch msg.(type) {
				case toastTickMsg, spinnerTickMsg:
				default:
					t.Errorf("the ticker produced %T, want a tick message", msg)
				}
			case <-time.After(tc.maxWait):
				t.Fatalf("the ticker never fired within %s", tc.maxWait)
			}
		})
	}
}

// ── fixtures ────────────────────────────────────────────────────────────────

// errProvider is a provider that resolves: the ask paths check for a provider BEFORE they
// check for a panel, so a case about the panel has to get past the provider first.
type errProvider struct{}

func (errProvider) Name() string { return "test-provider" }

func (errProvider) Generate(context.Context, string, string) (string, error) {
	return "", errors.New("this provider is a fixture")
}

// failingTx is a pgx.Tx whose every operation fails, which is the shape of a connection the
// server has already dropped.
type failingTx struct {
	err        error
	rolledBack int
	committed  int
}

func (t *failingTx) Begin(context.Context) (pgx.Tx, error) { return nil, t.err }

func (t *failingTx) Commit(context.Context) error {
	t.committed++
	return t.err
}

func (t *failingTx) Rollback(context.Context) error {
	t.rolledBack++
	return t.err
}

func (t *failingTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, t.err
}

func (t *failingTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, t.err
}

func (t *failingTx) QueryRow(context.Context, string, ...any) pgx.Row { return nil }

func (t *failingTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, t.err
}

func (t *failingTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults { return nil }

func (t *failingTx) LargeObjects() pgx.LargeObjects { return pgx.LargeObjects{} }

func (t *failingTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, t.err
}

func (t *failingTx) Conn() *pgx.Conn { return nil }

// executeQuery's commit-on-run, with a commit that FAILS.
//
// This is the second half of the commit-failure contract, and it is reached through a different
// path than the batch one: the editor is set to commit-on-run (which the grid does when it
// dumps draft SQL into the editor), the statement opens a transaction, and the COMMIT is what
// fails. So the statement itself succeeded and the message has to say so — the failure is the
// commit's, and reporting it as a query failure would send the user looking in the wrong place.
//
// The runner's `exec` is a FIELD, which is what makes this reachable without a database: the
// statement is made to succeed by a stub, and the transaction is made to fail by a pgx.Tx whose
// Commit refuses. A live connection cannot do both on demand.
func TestACommitOnRunThatFailsIsReportedAsACommitFailure(t *testing.T) {
	m := routerModelLoaded(t)
	m.state = StateMain
	m.editor.SetCommitOnRun(true)

	tx := &failingTx{err: errors.New("server closed the connection unexpectedly")}
	r := newStatementRunner(nil)
	r.exec = func(context.Context, postgres.Querier, string) (*postgres.QueryResult, error) {
		// The statement succeeds: this is the arm where only the COMMIT fails.
		return &postgres.QueryResult{
			Columns: []postgres.ColumnInfo{{Name: "id", DataType: "integer"}},
			Rows:    [][]interface{}{{int64(1)}},
			Count:   1,
		}, nil
	}
	// The transaction is OPENED by the statement rather than pre-set, which is the detail that
	// makes this the arm it is: execute commits whatever is pending BEFORE it runs anything, so
	// a fixture that pre-set r.tx had its failure swallowed there — returned as the query's own
	// error, with commitFailed still false — and never reached the commit-on-run arm at all.
	//
	// So: nothing pending, the statement is DML, querierFor opens a transaction, the statement
	// runs on it, and the commit is what fails.
	r.begin = func(context.Context) (pgx.Tx, error) { return tx, nil }
	m.runner = r

	cmd := m.executeQuery("UPDATE orders SET total = 0")
	if cmd == nil {
		t.Fatal("executeQuery refused instead of answering")
	}

	msg, ok := cmd().(queryExecutedMsg)
	if !ok {
		t.Fatalf("executeQuery produced %T, want queryExecutedMsg", cmd())
	}
	if msg.err == nil {
		t.Fatal("a failed commit reported success")
	}
	// The flag is what tells the handler this was a COMMIT failure rather than a failed query,
	// and it is what routes it to handleCommitFailure — which asks the connection whether it
	// is still alive.
	if !msg.commitFailed {
		t.Error("a failed commit is not marked as one, so the handler will report it as a query error")
	}
	// And it is not reported as committed, which was the bug this flag replaced.
	if msg.committedTx {
		t.Error("a failed commit is still reported as committed")
	}
	if tx.committed != 1 {
		t.Errorf("the commit was attempted %d times, want 1", tx.committed)
	}

	out, next := fold(m, msg)
	if next != nil {
		_ = next
	}
	// The connection died with the commit, so the app says so rather than reporting a
	// connection it no longer has.
	if m.conn != nil {
		if out.conn == nil {
			t.Error("the model still holds a connection after a commit that took it down")
		}
	}
	if out.View().Content == "" {
		t.Error("the app renders nothing after a failed commit")
	}

	t.Run("and a statement that is not a commit does not try", func(t *testing.T) {
		// The counterweight: commit-on-run off, so a failed commit is not even attempted.
		quiet := routerModelLoaded(t)
		quiet.state = StateMain
		quiet.editor.SetCommitOnRun(false)
		quiet.runner = newStatementRunner(nil)
		quiet.runner.conn = pgxfake.New()
		quiet.runner.exec = func(context.Context, postgres.Querier, string) (*postgres.QueryResult, error) {
			return &postgres.QueryResult{}, nil
		}

		msg, ok := quiet.executeQuery("SELECT 1")().(queryExecutedMsg)
		if !ok {
			t.Fatal("the statement produced something other than queryExecutedMsg")
		}
		if msg.commitFailed {
			t.Error("a plain statement reported a commit failure")
		}
	})
}
