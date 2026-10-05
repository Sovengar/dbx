package app

// Scenario: Las guardas del app que la suite anterior dejo sin tocar.
//
// Update has forty-seven message handlers and Model has a dozen helpers, and both are mostly
// defensive: they check for a nil component, a missing connection, an absent provider. Each of
// those checks exists because the corresponding field CAN be nil — a model built by a test, a
// connection that failed, a message that arrived out of order — and none of them is exercised
// by the happy-path tests, because on the happy path nothing is nil.
//
// So the cases here are all the same shape: arrange for one specific thing to be absent, then
// assert the app answers rather than panicking. The absence is the input.

import (
	"errors"
	"strings"
	"testing"

	"github.com/buble/dbx/internal/ai/session"
	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/testsupport/pgxfake"
)

// modelWithoutAsk is a loaded model whose ASK panel does not exist. Every ask entry point
// checks for it, because the panel is built lazily and a message can arrive before it is.
func modelWithoutAsk(t *testing.T) Model {
	t.Helper()
	m := routerModelLoaded(t)
	m.ask = nil
	m.askOpen = false
	m.state = StateMain
	return m
}

// executeQuery with no runner. That is "not connected", and it has to say so rather than
// dereference: the runner is created when a connection completes, so a query action bound
// before that arrives finds it nil.
func TestAQueryWithNoRunnerSaysItIsNotConnected(t *testing.T) {
	m := routerModelLoaded(t)
	m.state = StateMain
	m.runner = nil
	// The editor is OPEN, which is the state a user is in when they press run: the assertion
	// below is that a statement which never ran does not close it. The first version of this
	// case never opened it, so the assertion was passing for a reason that had nothing to do
	// with the handler.
	m.editorOpen = true
	m.editor.SetContent("SELECT 1")

	cmd := m.executeQuery("SELECT 1")
	if cmd == nil {
		t.Fatal("executeQuery refused instead of answering")
	}

	msg, ok := cmd().(queryExecutedMsg)
	if !ok {
		t.Fatalf("executeQuery produced %T, want queryExecutedMsg", cmd())
	}
	if msg.err == nil {
		t.Fatal("a query with no runner reported success")
	}
	if !strings.Contains(msg.err.Error(), "not connected") {
		t.Errorf("the error is %q, want it to say the app is not connected", msg.err)
	}
	// And nothing is marked as having run.
	if msg.committedTx {
		t.Error("a query that never ran claims a transaction was committed")
	}

	t.Run("and the handler turns it into a toast", func(t *testing.T) {
		out, _ := fold(m, msg)
		if said := toastText(out); !strings.Contains(said, "not connected") {
			t.Errorf("the user is told %q, want the connection named", said)
		}
		// The editor stays open and keeps its text: the statement never ran, so closing would
		// throw away the SQL the user wrote — and they would have to type it again to find out
		// what went wrong.
		if !out.editorOpen {
			t.Error("the editor closed on a statement that never ran")
		}
		if got := out.editor.Content(); got != "SELECT 1" {
			t.Errorf("the editor now holds %q, want the statement untouched", got)
		}
	})
}

// The three ask entry points, with no panel. All of them check, all of them return, and none
// of them has a test — because a test that opens the panel first never reaches the check.
func TestTheAskEntryPointsRefuseWithoutAPanel(t *testing.T) {
	t.Run("opening", func(t *testing.T) {
		m := modelWithoutAsk(t)

		// handleAskOpen returns a tea.Model, so it is asserted through the same type assertion
		// every other case in this package uses for it.
		out, cmd := m.handleAskOpen()
		if cmd != nil {
			t.Errorf("opening the ASK panel with none issued %T", cmd())
		}
		typed, ok := out.(Model)
		if !ok {
			t.Fatalf("handleAskOpen returned a %T, want a Model", out)
		}
		if typed.askOpen {
			t.Error("the app opened an ASK panel that does not exist")
		}
	})

	t.Run("generating SQL", func(t *testing.T) {
		m := modelWithoutAsk(t)

		// The generator resolves a provider first, so a nil provider would be reported before
		// the nil panel — and the panel is what the caller wanted to know about.
		m.ask = nil
		cmd := m.generateAskSQL("a question", 1)
		if cmd == nil {
			t.Fatal("generateAskSQL refused instead of answering")
		}
		msg, ok := cmd().(askGeneratedMsg)
		if !ok {
			t.Fatalf("generateAskSQL produced %T, want askGeneratedMsg", cmd())
		}
		if msg.err == nil {
			t.Error("generating SQL with no provider reported success")
		}
		// The sequence number travels with the failure, or a caller waiting for answer 1 would
		// wait forever after answer 1's own failure.
		if msg.seq != 1 {
			t.Errorf("the failure carries seq %d, want 1", msg.seq)
		}
	})

	t.Run("executing generated SQL", func(t *testing.T) {
		m := modelWithoutAsk(t)

		if cmd := m.executeAskSQL("SELECT 1", 1); cmd != nil {
			t.Errorf("executing SQL with no panel issued %T", cmd())
		}
	})
}

// ask with a provider that resolved to nothing. generateAskSQL takes the provider from the
// model, and a model with no AI configured has none — which is what a user without any
// provider set up presses the ask key on.
func TestAskWithNoProviderSaysThereIsNone(t *testing.T) {
	m := routerModelLoaded(t)
	m.state = StateMain
	if m.ask == nil {
		t.Skip("this fixture builds no ASK panel, so the provider path is unreachable")
	}

	// The provider comes from the model, so the case is a model whose provider is absent.
	m.aiProvider = nil

	cmd := m.generateAskSQL("a question", 7)
	if cmd == nil {
		t.Fatal("generateAskSQL refused instead of answering")
	}
	msg, ok := cmd().(askGeneratedMsg)
	if !ok {
		t.Fatalf("generateAskSQL produced %T, want askGeneratedMsg", cmd())
	}
	if msg.err == nil {
		t.Fatal("ask with no provider reported success")
	}
	if !strings.Contains(msg.err.Error(), "provider") {
		t.Errorf("the error is %q, want it to name the missing provider", msg.err)
	}
	if msg.seq != 7 {
		t.Errorf("the failure carries seq %d, want 7", msg.seq)
	}
}

// The grid sidebar's foreign-key preview: the schema fallback and the lookup's two refusals.
//
// The preview is what makes hovering a foreign key useful, and all three arms are reachable
// with a fake — the loader is built from whatever connection the model holds.
func TestTheSidebarForeignKeyPreview(t *testing.T) {
	fk := &postgres.ForeignKeyInfo{
		Name: "orders_user_id_fkey", Column: "user_id",
		RefSchema: "", // the fallback's premise: the FK does not name a schema
		RefTable:  "users", RefColumn: "id",
	}

	t.Run("a foreign key with no schema falls back to the table's own", func(t *testing.T) {
		// The fallback matters because a FK loaded from some drivers carries no schema, and
		// without it the query is unqualified — which resolves against search_path and can
		// read a different users table from another schema.
		m := routerModelLoaded(t)
		m.prevSchema = "shop"
		fake := pgxfake.New()
		m.conn = fake

		cmd := m.fetchGridSidebarFKPreview(fk, 42, 1)
		if cmd == nil {
			t.Fatal("the lookup refused instead of answering")
		}
		if msg := cmd(); msg == nil {
			t.Error("the lookup produced no message")
		}

		// And the query it issued was qualified with the fallback.
		qualified := false
		for _, q := range fake.Queries {
			if strings.Contains(q, `"users"`) && strings.Contains(strings.ToLower(q), "shop") {
				qualified = true
			}
		}
		if !qualified {
			t.Errorf("no query was qualified with the fallback schema %q; the queries were %v",
				m.prevSchema, fake.Queries)
		}
	})

	t.Run("a lookup that fails", func(t *testing.T) {
		m := routerModelLoaded(t)
		m.prevSchema = "shop"
		failing := pgxfake.New()
		failing.Steps = []pgxfake.Step{
			{Match: "SELECT", Err: errors.New("permission denied for table users")},
		}
		m.conn = failing

		cmd := m.fetchGridSidebarFKPreview(fk, 42, 2)
		if cmd == nil {
			t.Fatal("the lookup refused instead of answering")
		}
		msg, ok := cmd().(GridSidebarFKPreviewLookupResultMsg)
		if !ok {
			t.Fatalf("the lookup produced %T", cmd())
		}
		if msg.Err == nil {
			t.Error("a failed lookup reported no error")
		}
		// And the token travels with the failure, or a stale-answer check would treat it as a
		// fresh answer and clear the preview.
		if msg.Token != 2 {
			t.Errorf("the failure carries token %d, want 2", msg.Token)
		}
	})

	t.Run("a lookup with no rows", func(t *testing.T) {
		m := routerModelLoaded(t)
		m.prevSchema = "shop"
		empty := pgxfake.New()
		empty.Steps = []pgxfake.Step{{Match: "SELECT", Result: pgxfake.Result{}}}
		m.conn = empty

		cmd := m.fetchGridSidebarFKPreview(fk, 42, 3)
		if cmd == nil {
			t.Fatal("the lookup refused instead of answering")
		}
		msg, ok := cmd().(GridSidebarFKPreviewLookupResultMsg)
		if !ok {
			t.Fatalf("the lookup produced %T", cmd())
		}
		// An empty answer IS reported, and as "referenced row not found" rather than as
		// nothing: the key points at a row that is gone, which is a fact the user needs and
		// an empty preview would not tell them. The first version of this case expected no
		// error here, and the code was right.
		if msg.Err == nil {
			t.Error("a key pointing at a deleted row reported no error")
		} else if !strings.Contains(msg.Err.Error(), "referenced row not found") {
			t.Errorf("the error is %q, want it to say the referenced row is gone", msg.Err)
		}
		if len(msg.Row) != 0 {
			t.Errorf("an empty result carries a row: %v", msg.Row)
		}
	})
}

// The schema load's refusal, through the connection interface. loadSchemaWithTarget is what
// every connect path ends in, and its error is what a user sees when the database is reachable
// but will not describe itself.
func TestTheSchemaLoadRefusesAConnectionThatCannotDescribeItself(t *testing.T) {
	fake := pgxfake.New()
	// No steps at all: every query is unmatched, which pgxfake answers with an error naming
	// the query. That is a server reachable and unhelpful, which is a real state — a role with
	// no SELECT on the catalogs.
	m := routerModelLoaded(t)
	m.conn = fake

	cmd := m.loadSchemaWithTarget(fake, config.FoundProject{Name: "shopdb", Path: t.TempDir()}, "public", "orders")
	if cmd == nil {
		t.Fatal("the schema load refused instead of answering")
	}

	msg, ok := cmd().(schemaLoadedMsg)
	if !ok {
		t.Fatalf("the schema load produced %T, want schemaLoadedMsg", cmd())
	}
	if msg.err == nil {
		t.Fatal("a schema load against an unhelpful server reported success")
	}
	if !strings.Contains(msg.err.Error(), "schema") {
		t.Errorf("the error is %q, want it to mention the schema", msg.err)
	}

	t.Run("and the handler shows it and stops the spinner", func(t *testing.T) {
		out, next := fold(m, msg)
		if said := toastText(out); !strings.Contains(said, "Schema load failed") {
			t.Errorf("the toast is %q", said)
		}
		if out.spinnerActive {
			t.Error("the spinner is still running after a failed schema load")
		}
		if out.state != StateError {
			t.Errorf("the state is %v, want the error state", out.state)
		}
		if next != nil {
			// A retry would be a decision; the error path makes none.
			t.Log("the error path issued a command")
		}
	})
}

// The session logger's error line. It is best-effort — a failure to log is not a failure to
// query — so the case is that a session error is recorded at all.
func TestASessionErrorIsRecorded(t *testing.T) {
	dir := t.TempDir()

	logger, err := session.NewLogger(dir, 0)
	if err != nil {
		t.Fatalf("the logger: %v", err)
	}

	m := routerModelLoaded(t)
	m.sessionLogger = logger

	m.logSessionError("SELECT 1", errors.New("syntax error at or near \"SELCT\""))

	// The file it writes has grown and mentions the failure, which is the whole point of the
	// call: the next session can see what went wrong.
	entries := sessionEntries(t, dir)
	found := false
	for _, e := range entries {
		if strings.Contains(e.Error, "syntax error") && strings.Contains(e.SQL, "SELECT 1") {
			found = true
		}
	}
	if !found {
		t.Errorf("the session log holds %d entries, none of them the failed statement", len(entries))
	}

	t.Run("and a query that worked is not logged as an error", func(t *testing.T) {
		before := len(sessionEntries(t, dir))
		m.logSessionQuery("SELECT 2", 0, 1)
		after := sessionEntries(t, dir)
		if len(after) <= before {
			t.Error("a successful query was not recorded at all")
		}
		for _, e := range after[before:] {
			if e.Level == session.LogError {
				t.Errorf("a successful query was logged at error level: %+v", e)
			}
		}
	})
}

// connectToDB's ping refusal is PINNED, with the call site named: m.connectToDB, the
// `conn.Ping(ctx)` a few lines after `pgx.Connect`.
//
// The connect is not behind a seam — it calls pgx.Connect for real — so the ping arm needs a
// TCP endpoint that accepts and then does not answer, which is a socket this test does not
// have. What IS reachable, and is the case a user actually hits, is a DSN nothing is
// listening on: pgx.Connect fails first and the message says so, which is the assertion
// below.
func TestConnectingToAnUnreachableDatabaseSaysSo(t *testing.T) {
	cmd := m0(t).connectToDB(config.FoundProject{
		Name: "shopdb", Path: t.TempDir(),
		Connection: config.ProjectConnection{Driver: "postgres", DSN: "postgres://dbx@127.0.0.1:1/nodb"},
	})
	if cmd == nil {
		t.Fatal("the connect refused instead of answering")
	}

	msg, ok := cmd().(dbConnectedMsg)
	if !ok {
		t.Fatalf("the connect produced %T, want dbConnectedMsg", cmd())
	}
	if msg.err == nil {
		t.Fatal("an unreachable database reported success")
	}
	if !strings.Contains(msg.err.Error(), "failed to connect") {
		t.Errorf("the error is %q, want it to say the CONNECTION failed", msg.err)
	}
	// And no connection is handed over, or the model would hold a nil connection and report
	// itself connected.
	if msg.conn != nil {
		t.Error("a failed connect also returned a connection")
	}
}

// sessionEntries is what the logger wrote, read back through the reader the app itself uses
// for `dbx pipe`.
func sessionEntries(t *testing.T, dir string) []session.LogEntry {
	t.Helper()
	entries, err := session.NewReader(dir).ReadAll()
	if err != nil {
		t.Fatalf("reading the session log: %v", err)
	}
	return entries
}

// m0 is a model with no connection, for the paths that must not need one.
func m0(t *testing.T) Model {
	t.Helper()
	return routerModel(t)
}
