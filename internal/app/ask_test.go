package app

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/buble/dbx/internal/ai/nl2sql"
	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/ui/components/ask"
	"github.com/jackc/pgx/v5"
)

// errFake is a sentinel error used to drive the error paths.
var errFake = errFakeType{}

type errFakeType struct{}

func (errFakeType) Error() string { return "boom" }

// fakeProvider is a deterministic nl2sql.Provider that records the prompt and
// schema it was called with.
type fakeProvider struct {
	name      string
	sql       string
	err       error
	gotPrompt string
	gotSchema string
	calls     int
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) Generate(_ context.Context, prompt, schema string) (string, error) {
	f.calls++
	f.gotPrompt = prompt
	f.gotSchema = schema
	if f.err != nil {
		return "", f.err
	}
	return f.sql, nil
}

func queryResult(rows ...[]interface{}) *postgres.QueryResult {
	return &postgres.QueryResult{
		Columns: []postgres.ColumnInfo{{Name: "n"}},
		Rows:    rows,
		Count:   len(rows),
	}
}

// newAskRunner builds a statementRunner wired to deterministic fakes.
func newAskRunner(result *postgres.QueryResult, execErr error) (*statementRunner, *[]string) {
	execs := &[]string{}
	r := &statementRunner{
		conn:          &fakeQuerier{label: "conn"},
		begin:         func(context.Context) (pgx.Tx, error) { return &fakeTx{}, nil },
		beginReadOnly: func(context.Context) (pgx.Tx, error) { return &fakeTx{}, nil },
		exec: func(_ context.Context, _ postgres.Querier, sql string) (*postgres.QueryResult, error) {
			*execs = append(*execs, sql)
			return result, execErr
		},
	}
	return r, execs
}

func newAskTestModel(t *testing.T, provider nl2sql.Provider) Model {
	t.Helper()
	m := newModelWithGrid(t)
	m.ask = ask.New(m.styles)
	m.ask.SetWidth(100)
	m.ask.SetHeight(40)
	m.aiProvider = provider
	m.grid.SetWidth(100)
	m.grid.SetHeight(30)
	return m
}

func press(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(msg)
	model, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update(%T) returned %T, want Model", msg, updated)
	}
	return model, cmd
}

func typeAsk(t *testing.T, m Model, text string) Model {
	t.Helper()
	for _, r := range text {
		if r == ' ' {
			m, _ = press(t, m, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
			continue
		}
		m, _ = press(t, m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return m
}

func openAsk(t *testing.T, m Model) Model {
	t.Helper()
	m, _ = press(t, m, tea.KeyPressMsg{Code: 'a', Text: "a"})
	return m
}

// submitAsk types the question and drives the async generation to completion.
func submitAsk(t *testing.T, m Model, question string) Model {
	t.Helper()
	m = typeAsk(t, m, question)
	_, submitted := press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if submitted == nil {
		t.Fatal("enter produced no command")
	}
	m, genCmd := press(t, m, submitted())
	if genCmd == nil {
		t.Fatal("AskSubmittedMsg produced no generation command")
	}
	m, _ = press(t, m, genCmd())
	return m
}

// confirmAndExecute drives the confirm → read-only execution → result path.
func confirmAndExecute(t *testing.T, m Model) Model {
	t.Helper()
	_, confirmed := press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if confirmed == nil {
		t.Fatal("enter in review produced no command")
	}
	m, execCmd := press(t, m, confirmed())
	if execCmd == nil {
		return m // refused before execution (validation / connection / pending tx)
	}
	m, _ = press(t, m, execCmd())
	return m
}

func lastTurn(t *testing.T, m Model) ask.Turn {
	t.Helper()
	turns := m.ask.Turns()
	if len(turns) == 0 {
		t.Fatal("transcript is empty")
	}
	return turns[len(turns)-1]
}

// --- Opening the pane ---

// Scenario: `a` opens the ASK pane
func TestAsk_KeyOpensPane(t *testing.T) {
	m := newAskTestModel(t, &fakeProvider{name: "fake"})
	m = openAsk(t, m)

	if !m.askOpen || !m.ask.IsVisible() {
		t.Fatal("pressing 'a' did not open the ASK pane")
	}
	if len(m.ask.Turns()) != 0 {
		t.Fatalf("fresh transcript = %v, want empty", m.ask.Turns())
	}
}

// Scenario: ASK is globally available regardless of focus
func TestAsk_OpensRegardlessOfFocus(t *testing.T) {
	m := newAskTestModel(t, &fakeProvider{name: "fake"})
	m.router.FocusPane(FocusExplorer)
	m = openAsk(t, m)
	if !m.ask.IsVisible() {
		t.Fatal("ASK did not open while the explorer had focus")
	}
}

// Scenario: `a` does not open ASK while the grid is editing or filtering
func TestAsk_DoesNotOpenWhileGridEditing(t *testing.T) {
	m := newAskTestModel(t, &fakeProvider{name: "fake"})
	m.grid.SetData(queryResult([]interface{}{"1"}), "public", "users")
	m.router.FocusPane(FocusGrid)
	m.grid.Focus()
	if _, handled := m.grid.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); !handled {
		t.Fatal("grid did not enter edit mode")
	}
	if !m.grid.IsEditing() {
		t.Fatal("grid is not editing")
	}

	m, _ = press(t, m, tea.KeyPressMsg{Code: 'a', Text: "a"})

	if m.askOpen || m.ask.IsVisible() {
		t.Fatal("ASK opened while the grid was editing")
	}
	if !m.grid.IsEditing() {
		t.Fatal("grid left edit mode when 'a' was pressed")
	}
	if !strings.Contains(m.grid.EditValue(), "a") {
		t.Fatalf("grid edit value = %q, want it to contain 'a'", m.grid.EditValue())
	}
}

// Scenario: `esc` closes the ASK pane without executing
func TestAsk_EscClosesWithoutExecuting(t *testing.T) {
	provider := &fakeProvider{name: "fake", sql: "SELECT 1"}
	runner, execs := newAskRunner(queryResult(), nil)
	m := newAskTestModel(t, provider)
	m.runner = runner
	m = openAsk(t, m)
	m = submitAsk(t, m, "one row")

	_, cmd := press(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("esc produced no command")
	}
	m, _ = press(t, m, cmd())

	if m.askOpen || m.ask.IsVisible() {
		t.Fatal("ASK still open after esc")
	}
	if len(*execs) != 0 {
		t.Fatalf("esc executed %d statements, want 0", len(*execs))
	}
}

// --- Asking a question ---

// Scenario: Asking a question generates SQL and shows it for review
func TestAsk_QuestionGeneratesSQLForReview(t *testing.T) {
	provider := &fakeProvider{name: "fake", sql: "SELECT count(*) FROM public.users"}
	m := newAskTestModel(t, provider)
	m.askSchemaText = "Schema: public\n  Table: users (3 rows)\n    id integer\n"
	m = openAsk(t, m)
	m = submitAsk(t, m, "how many users are older than 18")

	if provider.calls != 1 {
		t.Fatalf("provider called %d times, want 1", provider.calls)
	}
	if !strings.Contains(provider.gotPrompt, "how many users are older than 18") {
		t.Fatalf("prompt = %q, want the question", provider.gotPrompt)
	}
	if !strings.Contains(provider.gotSchema, "users") {
		t.Fatalf("schema = %q, want the schema", provider.gotSchema)
	}
	turn := lastTurn(t, m)
	if turn.Question != "how many users are older than 18" {
		t.Fatalf("turn question = %q", turn.Question)
	}
	if turn.SQL != "SELECT count(*) FROM public.users" {
		t.Fatalf("turn SQL = %q", turn.SQL)
	}
	if turn.Status != ask.TurnReview {
		t.Fatalf("turn status = %v, want TurnReview", turn.Status)
	}
	if !m.ask.IsVisible() {
		t.Fatal("pane closed before confirmation")
	}
}

// Scenario: Confirming executes the SQL and shows the result in the grid
func TestAsk_ConfirmExecutesAndPopulatesGrid(t *testing.T) {
	provider := &fakeProvider{name: "fake", sql: "SELECT n FROM public.users"}
	result := queryResult([]interface{}{"a"}, []interface{}{"b"})
	runner, execs := newAskRunner(result, nil)
	m := newAskTestModel(t, provider)
	m.runner = runner
	m = openAsk(t, m)
	m = submitAsk(t, m, "list users")
	m = confirmAndExecute(t, m)

	if len(*execs) != 1 || (*execs)[0] != "SELECT n FROM public.users" {
		t.Fatalf("executed %v, want the generated SELECT", *execs)
	}
	if !m.grid.HasData() {
		t.Fatal("grid was not populated")
	}
	if m.grid.TableName() != "query" {
		t.Fatalf("grid table = %q, want %q for a query result", m.grid.TableName(), "query")
	}
	if m.askOpen || m.ask.IsVisible() {
		t.Fatal("ASK pane did not close on success")
	}
	if !lastToastContains(m, "2 rows") {
		t.Fatalf("toast = %q, want the row count", lastToastText(t, m))
	}
}

// Scenario: The transcript keeps the question and the executed SQL
func TestAsk_TranscriptKeepsExecutedQuery(t *testing.T) {
	provider := &fakeProvider{name: "fake", sql: "SELECT n FROM public.users"}
	runner, _ := newAskRunner(queryResult([]interface{}{"a"}), nil)
	m := newAskTestModel(t, provider)
	m.runner = runner
	m = openAsk(t, m)
	m = submitAsk(t, m, "list users")
	m = confirmAndExecute(t, m)

	m = openAsk(t, m)

	turns := m.ask.Turns()
	if len(turns) != 1 {
		t.Fatalf("transcript after reopen = %v, want one turn", turns)
	}
	if turns[0].Question != "list users" {
		t.Fatalf("question lost after reopen: %q", turns[0].Question)
	}
	if turns[0].SQL != "SELECT n FROM public.users" {
		t.Fatalf("executed SQL lost after reopen: %q", turns[0].SQL)
	}
}

// --- Table context hint ---

func setGridWhere(t *testing.T, m Model, where string) Model {
	t.Helper()
	m.router.FocusPane(FocusGrid)
	m.grid.Focus()
	m, _ = press(t, m, tea.KeyPressMsg{Code: '/'})
	m = typeAsk(t, m, where)
	m, _ = press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	return m
}

// Scenario: The loaded table and its WHERE clause are passed as a context hint
func TestAsk_PassesTableAndWhereAsContextHint(t *testing.T) {
	provider := &fakeProvider{name: "fake", sql: "SELECT count(*) FROM public.users"}
	m := newAskTestModel(t, provider)
	m.grid.SetData(queryResult([]interface{}{"1"}), "public", "users")
	m = setGridWhere(t, m, "active = true")
	if got := m.grid.WhereClause(); got != "active = true" {
		t.Fatalf("grid WHERE = %q, want %q", got, "active = true")
	}

	m = openAsk(t, m)
	m = submitAsk(t, m, "how many are admins")

	if !strings.Contains(provider.gotPrompt, "public.users") {
		t.Fatalf("prompt = %q, want it to mention public.users", provider.gotPrompt)
	}
	if !strings.Contains(provider.gotPrompt, "active = true") {
		t.Fatalf("prompt = %q, want it to mention the WHERE clause", provider.gotPrompt)
	}
}

// Scenario: ASK works with no table loaded
func TestAsk_NoTableLoadedUsesFullSchema(t *testing.T) {
	provider := &fakeProvider{name: "fake", sql: "SELECT 1"}
	m := newAskTestModel(t, provider)
	m.askSchemaText = "Schema: public\n  Table: orders (10 rows)\n    id integer\n"
	m = openAsk(t, m)
	m = submitAsk(t, m, "list the 10 most recent orders")

	if !strings.Contains(provider.gotSchema, "orders") {
		t.Fatalf("schema = %q, want the full schema", provider.gotSchema)
	}
	if strings.Contains(provider.gotPrompt, "grid is currently showing") {
		t.Fatalf("prompt = %q, want no table context hint", provider.gotPrompt)
	}
}

// --- SELECT-only enforcement ---

// Scenario: A non-SELECT statement is rejected before execution
func TestAsk_NonSelectRejected(t *testing.T) {
	provider := &fakeProvider{name: "fake", sql: "DELETE FROM users"}
	runner, execs := newAskRunner(queryResult(), nil)
	m := newAskTestModel(t, provider)
	m.runner = runner
	m = openAsk(t, m)
	m = submitAsk(t, m, "delete everyone")
	m = confirmAndExecute(t, m)

	turn := lastTurn(t, m)
	if turn.Status != ask.TurnError || turn.Err == "" {
		t.Fatalf("turn = %+v, want an error", turn)
	}
	if len(*execs) != 0 {
		t.Fatalf("executed %v, want nothing", *execs)
	}
	if m.grid.HasData() {
		t.Fatal("grid was populated by a rejected statement")
	}
	if !m.ask.IsVisible() {
		t.Fatal("pane should stay open after rejection")
	}
}

// --- Error paths ---

// Scenario: AI provider failure
func TestAsk_ProviderError(t *testing.T) {
	provider := &fakeProvider{name: "fake", err: errFake}
	runner, execs := newAskRunner(queryResult(), nil)
	m := newAskTestModel(t, provider)
	m.runner = runner
	m = openAsk(t, m)
	m = submitAsk(t, m, "anything")

	turn := lastTurn(t, m)
	if turn.Status != ask.TurnError || turn.Err == "" {
		t.Fatalf("turn = %+v, want an error", turn)
	}
	if !m.ask.IsVisible() {
		t.Fatal("pane should stay open after a provider error")
	}
	if len(*execs) != 0 {
		t.Fatalf("executed %v, want nothing", *execs)
	}
}

// Scenario: Invalid SQL from the AI
func TestAsk_InvalidSQL(t *testing.T) {
	provider := &fakeProvider{name: "fake", sql: "SELECT FROM WHERE"}
	runner, execs := newAskRunner(nil, errFake)
	m := newAskTestModel(t, provider)
	m.runner = runner
	m = openAsk(t, m)
	m = submitAsk(t, m, "broken")
	m = confirmAndExecute(t, m)

	if len(*execs) != 1 {
		t.Fatalf("executed %d statements, want 1 attempt", len(*execs))
	}
	turn := lastTurn(t, m)
	if turn.Status != ask.TurnError || turn.Err == "" {
		t.Fatalf("turn = %+v, want an error", turn)
	}
	if !m.ask.IsVisible() {
		t.Fatal("pane should stay open after an execution error")
	}
}

// Scenario: No database connection
func TestAsk_NoConnection(t *testing.T) {
	provider := &fakeProvider{name: "fake", sql: "SELECT 1"}
	m := newAskTestModel(t, provider) // runner is nil
	m = openAsk(t, m)
	m = submitAsk(t, m, "one row")
	m = confirmAndExecute(t, m)

	turn := lastTurn(t, m)
	if turn.Status != ask.TurnError || !strings.Contains(turn.Err, "not connected") {
		t.Fatalf("turn = %+v, want a not-connected error", turn)
	}
}

// Scenario: No AI provider configured
func TestAsk_NoProvider(t *testing.T) {
	m := newAskTestModel(t, nil)
	m = openAsk(t, m)

	if m.askOpen || m.ask.IsVisible() {
		t.Fatal("ASK opened without an AI provider")
	}
	if !lastToastContains(m, "No AI provider") {
		t.Fatalf("toast = %q, want an AI provider error", lastToastText(t, m))
	}
}

// Scenario: A pending DML transaction blocks ASK execution
func TestAsk_PendingTxBlocksExecution(t *testing.T) {
	provider := &fakeProvider{name: "fake", sql: "SELECT 1"}
	runner, execs := newAskRunner(queryResult(), nil)
	runner.tx = &fakeTx{} // pending DML transaction
	m := newAskTestModel(t, provider)
	m.runner = runner
	m = openAsk(t, m)
	m = submitAsk(t, m, "one row")
	m = confirmAndExecute(t, m)

	turn := lastTurn(t, m)
	if turn.Status != ask.TurnError {
		t.Fatalf("turn = %+v, want an error", turn)
	}
	if !strings.Contains(strings.ToLower(turn.Err), "commit or roll back") {
		t.Fatalf("turn error = %q, want commit/rollback guidance", turn.Err)
	}
	if len(*execs) != 0 {
		t.Fatalf("executed %v, want nothing", *execs)
	}
	if m.grid.HasData() {
		t.Fatal("grid was populated despite the pending transaction")
	}
}

// Scenario: The server rejects DML even if validation is bypassed
func TestAsk_ReadOnlyTxRejectsDML(t *testing.T) {
	dsn := testDSN(t)
	conn := connectTestDB(t, dsn)
	table := seedTxTestTable(t, conn, map[int]string{1: "a"})
	runner := newStatementRunner(conn)
	ctx := context.Background()

	// A SELECT runs and returns rows inside the read-only transaction.
	result, err := runner.executeReadOnly(ctx, fmt.Sprintf("SELECT name FROM %s WHERE id = 1", table))
	if err != nil {
		t.Fatalf("SELECT inside read-only tx: %v", err)
	}
	if result.Count != 1 {
		t.Fatalf("SELECT count = %d, want 1", result.Count)
	}

	// A mutation that bypassed client validation is rejected by the server.
	if _, err := runner.executeReadOnly(ctx, fmt.Sprintf("UPDATE %s SET name = 'b' WHERE id = 1", table)); err == nil {
		t.Fatal("UPDATE succeeded inside the read-only transaction")
	}
	if got := readName(t, conn, table, 1); got != "a" {
		t.Fatalf("name = %q, want it unchanged (%q)", got, "a")
	}
}
