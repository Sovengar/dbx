package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/buble/dbx/internal/ai/session"
)

// --- replayEntries -----------------------------------------------------------

func logEntry(sql string) session.LogEntry {
	return session.LogEntry{Level: session.LogQuery, SQL: sql}
}

// Scenario: Un replay ejecuta las consultas del log y cuenta las que fallan.
//
// A failing statement is recorded and the replay continues: the point of a replay
// is to see how far a session gets, and stopping at the first error would answer a
// different question. The summary line is what tells the user where it stopped
// mattering.
func TestReplayEntries_ContinuesPastAFailedStatement(t *testing.T) {
	// The outcome depends on WHICH statement runs, so the connection is scripted
	// per statement rather than per query shape.
	conn := &fakeConn{}
	conn.perStatement = []queryAnswer{
		{rows: [][]any{{1}}},
		{err: errors.New("relation \"orders\" does not exist")},
		{rows: [][]any{{2}, {3}}},
	}

	entries := []session.LogEntry{
		logEntry("SELECT 1"),
		logEntry("SELECT * FROM orders"),
		logEntry("SELECT 2"),
	}

	var stdout, stderr bytes.Buffer
	if err := replayEntries(&stdout, &stderr, conn, entries, "s.jsonl", false); err != nil {
		t.Fatalf("replayEntries: %v", err)
	}

	// All three ran: the failure did not stop the replay.
	if len(conn.queried) != 3 {
		t.Errorf("%d statements ran, want 3: a failure must not stop the replay", len(conn.queried))
	}
	if !strings.Contains(stderr.String(), "OK: SELECT 1 (1 rows,") {
		t.Errorf("stderr = %q, want an OK line for the first statement", stderr.String())
	}
	if !strings.Contains(stderr.String(), "FAIL: SELECT * FROM orders") {
		t.Errorf("stderr = %q, want a FAIL line for the failing statement", stderr.String())
	}
	if !strings.Contains(stderr.String(), `relation "orders" does not exist`) {
		t.Errorf("stderr = %q, want the driver's error text in the FAIL line", stderr.String())
	}
	if !strings.Contains(stderr.String(), "Replay complete: 3 queries, 1 errors") {
		t.Errorf("stderr = %q, want the summary to count 3 queries and 1 error", stderr.String())
	}
	// The text mode prints nothing on stdout: the report is a log, not data.
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want nothing in the text mode", stdout.String())
	}
}

// Scenario: El resumen cuenta solo las entradas que son consultas.
//
// A session log also holds notes, results and errors. Replaying a note would
// either fail or, worse, run something that was never meant to run, and the
// counts in the summary have to reflect only the statements.
func TestReplayEntries_SkipsEntriesThatAreNotQueries(t *testing.T) {
	conn := &fakeConn{}
	conn.perStatement = []queryAnswer{{rows: [][]any{{1}}}}

	entries := []session.LogEntry{
		{Level: session.LogConnect, SQL: ""},
		{Level: session.LogQuery, SQL: "SELECT 1"},
		{Level: session.LogQuery, SQL: ""},         // a query entry with no SQL
		{Level: session.LogError, SQL: "SELECT 2"}, // an error entry is not a query to replay
	}

	var stdout, stderr bytes.Buffer
	if err := replayEntries(&stdout, &stderr, conn, entries, "s.jsonl", false); err != nil {
		t.Fatalf("replayEntries: %v", err)
	}
	if len(conn.queried) != 1 {
		t.Errorf("%d statements ran, want 1: only real query entries are replayed", len(conn.queried))
	}
	if !strings.Contains(stderr.String(), "Replay complete: 1 queries, 0 errors") {
		t.Errorf("stderr = %q, want the summary to count exactly one query", stderr.String())
	}
}

// Scenario: El modo JSON lleva cada consulta con su recuento o su error.
//
// The rows and the error are mutually exclusive per entry, and the error is a
// string rather than an object because it is for a human reading the report.
func TestReplayEntries_JSONModeReportsEachStatement(t *testing.T) {
	conn := &fakeConn{}
	conn.perStatement = []queryAnswer{
		{rows: [][]any{{1}, {2}}},
		{err: errors.New("boom")},
	}

	var stdout, stderr bytes.Buffer
	if err := replayEntries(&stdout, &stderr, conn, []session.LogEntry{
		logEntry("SELECT 1"),
		logEntry("SELECT bad"),
	}, "session.jsonl", true); err != nil {
		t.Fatalf("replayEntries: %v", err)
	}

	var got struct {
		Session string         `json:"session"`
		Total   int            `json:"total"`
		Errors  int            `json:"errors"`
		Queries []ReplayResult `json:"queries"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, stdout.String())
	}
	if got.Session != "session.jsonl" {
		t.Errorf("session = %q, want the file name", got.Session)
	}
	if got.Total != 2 || got.Errors != 1 {
		t.Errorf("total=%d errors=%d, want 2 and 1", got.Total, got.Errors)
	}
	if len(got.Queries) != 2 {
		t.Fatalf("got %d entries, want 2", len(got.Queries))
	}
	if got.Queries[0].SQL != "SELECT 1" || got.Queries[0].Rows != 2 || got.Queries[0].Error != "" {
		t.Errorf("first entry = %+v, want the SQL with two rows and no error", got.Queries[0])
	}
	if got.Queries[1].Error != "boom" || got.Queries[1].Rows != 0 {
		t.Errorf("second entry = %+v, want the error and no rows", got.Queries[1])
	}
}

// Scenario: Un log sin consultas reimprime un informe vacío.
//
// An empty session is an ordinary state, and the JSON has to be an empty list
// rather than `null`, for the same reason `dbx list tables -j` does.
func TestReplayEntries_AnEmptySessionIsAnEmptyReport(t *testing.T) {
	conn := &fakeConn{}

	var stdout, stderr bytes.Buffer
	if err := replayEntries(&stdout, &stderr, conn, nil, "empty.jsonl", true); err != nil {
		t.Fatalf("replayEntries: %v", err)
	}

	var got struct {
		Total   int            `json:"total"`
		Errors  int            `json:"errors"`
		Queries []ReplayResult `json:"queries"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, stdout.String())
	}
	if got.Total != 0 || got.Errors != 0 {
		t.Errorf("total=%d errors=%d, want zero", got.Total, got.Errors)
	}
	if len(got.Queries) != 0 {
		t.Errorf("queries = %v, want an empty list", got.Queries)
	}
	if strings.Contains(stdout.String(), "null") {
		t.Errorf("the report contains a null: %s, want an empty list", stdout.String())
	}
}

// --- askAndReport ------------------------------------------------------------

type stubProvider struct {
	name      string
	sql       string
	err       error
	asked     int
	gotSchema string
}

func (p *stubProvider) Name() string { return p.name }

func (p *stubProvider) Generate(ctx context.Context, prompt, schema string) (string, error) {
	p.asked++
	p.gotSchema = schema
	if p.err != nil {
		return "", p.err
	}
	return p.sql, nil
}

// askConn answers the schema list, the table list and the column list, which is
// what getSchemaForLLM asks for before the model is called.
func askConn() *fakeConn {
	return &fakeConn{steps: []step{
		{contains: "information_schema.schemata", spec: rowsSpec{rows: toAnyRows([]string{"public", "pg_catalog"})}},
		{contains: "information_schema.tables", spec: rowsSpec{rows: [][]any{{"users", "BASE TABLE", 3}}}},
		{contains: "information_schema.columns", spec: rowsSpec{rows: [][]any{{"id", "integer", "NO", nil}}}},
		// Catch-all last: the statement the model produced, whose text is not
		// known until the test writes the stub provider.
		{contains: "", spec: rowsSpec{rows: [][]any{{1}}}},
	}}
}

// Scenario: `dbx ask` da al modelo el esquema real de la base.
//
// The schema is read from the database and handed to the provider, which is the
// entire point of the command: a model that cannot see the schema cannot write the
// query.
func TestAskAndReport_PassesTheRealSchemaToTheProvider(t *testing.T) {
	conn := askConn()
	provider := &stubProvider{name: "test-provider", sql: "SELECT id FROM users"}

	var stdout, stderr bytes.Buffer
	if err := askAndReport(&stdout, &stderr, conn, provider, "how many users?", false); err != nil {
		t.Fatalf("askAndReport: %v", err)
	}

	if provider.asked != 1 {
		t.Fatalf("the provider was asked %d times, want once", provider.asked)
	}
	if !strings.Contains(provider.gotSchema, "users") {
		t.Errorf("the schema given to the model is %q, want it to mention the users table", provider.gotSchema)
	}
	if strings.Contains(provider.gotSchema, "pg_catalog") {
		t.Errorf("the schema given to the model includes pg_catalog: system schemas waste the context window")
	}
	if !strings.Contains(stderr.String(), "test-provider") {
		t.Errorf("stderr = %q, want it to name the provider in use", stderr.String())
	}
	if !strings.Contains(stderr.String(), "SELECT id FROM users") {
		t.Errorf("stderr = %q, want it to echo the generated SQL", stderr.String())
	}
}

// Scenario: El SQL que genera el modelo es el que se ejecuta.
//
// Round-tripping through the model is the whole contract: whatever it produced is
// what reaches the database, unchanged.
func TestAskAndReport_RunsExactlyTheGeneratedSQL(t *testing.T) {
	conn := askConn()
	provider := &stubProvider{name: "p", sql: "SELECT id FROM users WHERE id = 1"}

	var stdout, stderr bytes.Buffer
	if err := askAndReport(&stdout, &stderr, conn, provider, "the first user", false); err != nil {
		t.Fatalf("askAndReport: %v", err)
	}

	var ran string
	for _, q := range conn.queried {
		if strings.Contains(q, "WHERE id = 1") {
			ran = q
		}
	}
	if ran == "" {
		t.Errorf("the generated SQL was not among the statements run: %v", conn.queried)
	}
}

// Scenario: Si no se puede leer el esquema no se llama al modelo.
//
// Asking a model to write SQL for a schema it cannot see wastes an API call and
// produces a confident wrong answer. The schema is read first for that reason.
func TestAskAndReport_DoesNotCallTheModelWithoutASchema(t *testing.T) {
	conn := &fakeConn{steps: []step{{contains: "information_schema.schemata", err: errors.New("permission denied")}}}
	provider := &stubProvider{name: "p", sql: "SELECT 1"}

	var stdout, stderr bytes.Buffer
	err := askAndReport(&stdout, &stderr, conn, provider, "anything", false)
	if err == nil {
		t.Fatal("a schema read failure returned no error")
	}
	if !strings.Contains(err.Error(), "failed to get schema") {
		t.Errorf("error = %v, want it to say the schema could not be read", err)
	}
	if provider.asked != 0 {
		t.Error("the model was called even though the schema could not be read")
	}
}

// Scenario: Un modelo que falla es un error, no un resultado vacío.
//
// Silently printing nothing would look like "no rows", which is a completely
// different answer to the user's question.
func TestAskAndReport_AModelFailureIsReported(t *testing.T) {
	conn := askConn()
	boom := errors.New("429 rate limited")
	provider := &stubProvider{name: "p", err: boom}

	var stdout, stderr bytes.Buffer
	err := askAndReport(&stdout, &stderr, conn, provider, "how many users?", false)
	if err == nil {
		t.Fatal("a failing model returned no error")
	}
	if !strings.Contains(err.Error(), "failed to generate SQL") {
		t.Errorf("error = %v, want it to say the generation failed", err)
	}
	if !errors.Is(err, boom) {
		t.Errorf("error = %v, want it to wrap %v", err, boom)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want nothing when the model failed", stdout.String())
	}
}

// Scenario: Un SQL que la base rechaza se informa como fallo de la consulta.
//
// Model-written SQL is wrong often enough that this is a normal outcome, and the
// message has to distinguish it from a model failure: the user may want to retry
// the question differently.
func TestAskAndReport_ABadQueryIsReportedAsAQueryFailure(t *testing.T) {
	// No catch-all step this time, so the model's statement is only answered by
	// perStatement and can be made to fail.
	conn := &fakeConn{steps: []step{
		{contains: "information_schema.schemata", spec: rowsSpec{rows: toAnyRows([]string{"public"})}},
		{contains: "information_schema.tables", spec: rowsSpec{rows: [][]any{{"users", "BASE TABLE", 3}}}},
		{contains: "information_schema.columns", spec: rowsSpec{rows: [][]any{{"id", "integer", "NO", nil}}}},
	}}
	conn.perStatement = []queryAnswer{{err: errors.New(`column "nmae" does not exist`)}}
	provider := &stubProvider{name: "p", sql: "SELECT nmae FROM users"}

	var stdout, stderr bytes.Buffer
	err := askAndReport(&stdout, &stderr, conn, provider, "names?", false)
	if err == nil {
		t.Fatal("a rejected statement returned no error")
	}
	if !strings.Contains(err.Error(), "query failed") {
		t.Errorf("error = %v, want it to say the query failed, not that the model failed", err)
	}
	if !strings.Contains(err.Error(), "nmae") {
		t.Errorf("error = %v, want the driver's message with the offending name", err)
	}
}

// Scenario: El modo JSON incluye el SQL junto al resultado.
//
// The generated SQL is the only record of how the answer was arrived at, and it is
// what a user re-runs by hand.
func TestAskAndReport_JSONModeCarriesTheSQL(t *testing.T) {
	conn := askConn()
	provider := &stubProvider{name: "p", sql: "SELECT id FROM users"}

	var stdout, stderr bytes.Buffer
	if err := askAndReport(&stdout, &stderr, conn, provider, "ids", true); err != nil {
		t.Fatalf("askAndReport: %v", err)
	}

	var got struct {
		SQL   string `json:"sql"`
		Count int    `json:"count"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, stdout.String())
	}
	if got.SQL != "SELECT id FROM users" {
		t.Errorf("sql = %q, want the generated statement", got.SQL)
	}
}

// --- runAsk, the sql-only path ------------------------------------------------

// Scenario: `--sql-only` imprime el SQL y no toca la base de datos.
//
// Connecting in this mode would make the flag fail for anyone without a reachable
// database, which is exactly when you want to see the SQL.
func TestGenerateSQLOnly_PrintsTheSQLAndPassesNoSchema(t *testing.T) {
	provider := &stubProvider{name: "p", sql: "SELECT count(*) FROM users"}

	var out bytes.Buffer
	if err := generateSQLOnly(&out, provider, "how many users?"); err != nil {
		t.Fatalf("generateSQLOnly: %v", err)
	}
	if got := out.String(); got != "SELECT count(*) FROM users\n" {
		t.Errorf("output = %q, want the SQL and a newline", got)
	}
	if provider.asked != 1 {
		t.Errorf("the provider was asked %d times, want once", provider.asked)
	}
	// No database was consulted, so passing a schema would be inventing one.
	if provider.gotSchema != "" {
		t.Errorf("the provider was given the schema %q, want an empty one", provider.gotSchema)
	}
}

// Scenario: Un modelo que falla en modo sql-only también es un error.
func TestGenerateSQLOnly_AModelFailureIsReported(t *testing.T) {
	boom := errors.New("no API key configured")
	provider := &stubProvider{name: "p", err: boom}

	var out bytes.Buffer
	err := generateSQLOnly(&out, provider, "anything")
	if err == nil {
		t.Fatal("a failing model returned no error")
	}
	if !strings.Contains(err.Error(), "failed to generate SQL") {
		t.Errorf("error = %v, want it to say the generation failed", err)
	}
	if !errors.Is(err, boom) {
		t.Errorf("error = %v, want it to wrap %v", err, boom)
	}
	if out.Len() != 0 {
		t.Errorf("output = %q, want nothing printed when the model failed", out.String())
	}
}
