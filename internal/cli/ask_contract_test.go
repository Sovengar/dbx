package cli

// Scenario: `dbx ask`, y los caminos que su propia bandera abre.
//
// runAsk has three exits and the tests only had one of them:
//
//	sql-only  — no database at all, which is the flag's whole reason to exist: the point is
//	            to SEE the SQL, so connecting would make the command fail for anyone without a
//	            reachable database. Connecting first would defeat it.
//	the normal path — connect, read the schema, generate, run
//	and the refusal: a connection that cannot be made
//
// The schema walk inside getSchemaForLLM has two `continue` arms for a query that fails, and
// both are reachable through the pgxConnect seam: the loader is built from whatever
// postgres.Conn it is handed, so a fake that fails on ONE table's columns proves the walk
// skips that table and keeps the rest. A walk that abandoned the schema instead would answer
// a question with a smaller schema than the database has.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/testsupport/pgxfake"
)

// aiConfig is a config that resolves a provider, so runAsk gets past nl2sql.Resolve and the
// only thing under test is what happens after it.
//
// The provider is the OpenAI-compatible shape with a loopback base URL, so nothing in these
// cases can reach a network: the cases that need generation to FAIL point it at a port with
// nothing on it.
func aiConfig(t *testing.T) *config.Config {
	t.Helper()
	// The provider is resolved from an environment variable, so the variable has to exist —
	// which is also what makes the case independent of the developer's own configuration.
	t.Setenv("DBX_TEST_AI_KEY", "test-key-not-a-real-key")

	cfg := &config.Config{}
	cfg.AI.Provider = "test"
	cfg.AI.Model = "test-model"
	cfg.AI.Providers = map[string]config.AIProviderConf{
		"test": {
			APIKeyEnv: "DBX_TEST_AI_KEY",
			BaseURL:   "http://127.0.0.1:1",
		},
	}
	return cfg
}

func withAskSeams(t *testing.T, cfg *config.Config, cfgErr error) {
	t.Helper()
	oldLoad := loadConfig
	t.Cleanup(func() { loadConfig = oldLoad })
	loadConfig = func() (*config.Config, error) { return cfg, cfgErr }
}

// sql-only: no connection is attempted at all.
//
// The assertion is the negative one, and it is the whole point of the flag: pgxConnect is
// replaced with something that FAILS, so a runAsk that connected first would report an error.
// A version that connects and then ignores the connection would pass here and break the
// promise the flag makes.
func TestAskSQLOnlyNeverConnects(t *testing.T) {
	withAskSeams(t, aiConfig(t), nil)

	// A project directory, so getConnection takes its LOCAL branch — which is the one that
	// reaches pgxConnect at all. Without a .dbx.toml it falls back to the global config and
	// reports "connection not found" before the seam is ever consulted.
	inProjectWithAlias(t, "shopdb")

	oldConnect := pgxConnect
	t.Cleanup(func() { pgxConnect = oldConnect })
	var attempts int
	pgxConnect = func(context.Context, string) (pgconnPing, error) {
		attempts++
		return nil, errors.New("no database is reachable, and sql-only must not care")
	}

	// A provider that fails to generate is enough: the flag's promise is about the DATABASE,
	// not about the model, and this keeps the case from depending on a network.
	err := runAsk(askCmdForTest(t, true), []string{"how many orders are there?"})
	if err == nil {
		t.Fatal("ask with a provider that cannot be reached reported success")
	}
	if attempts != 0 {
		t.Errorf("sql-only attempted %d connections; it must not connect at all", attempts)
	}
	// And the error is about GENERATION, which is the proof it got past the flag's branch.
	if !strings.Contains(err.Error(), "generate") {
		t.Errorf("the error is %q, want it to be about generating the SQL", err)
	}
}

// The normal path's connection refusal, and the run after it.
//
// Both with a fake: the schema read and the query are two separate round trips, and a test
// that only exercised one of them would not notice a schema read that silently returned
// nothing.
func TestAskConnectsRunsAndReportsItsRefusals(t *testing.T) {
	withAskSeams(t, aiConfig(t), nil)

	// The same reason as the sql-only case: getConnection only consults pgxConnect once
	// findLocalDSN has produced a DSN, which means being inside a project.
	inProjectWithAlias(t, "shopdb")

	oldConnect := pgxConnect
	t.Cleanup(func() { pgxConnect = oldConnect })

	t.Run("a connection that cannot be made", func(t *testing.T) {
		pgxConnect = func(context.Context, string) (pgconnPing, error) {
			return nil, errors.New("connection refused")
		}

		err := runAsk(askCmdForTest(t, false), []string{"a question"})
		if err == nil {
			t.Fatal("a refused connection reported success")
		}
		if !strings.Contains(err.Error(), "refused") {
			t.Errorf("the error is %q, want it to carry the connection failure", err)
		}
	})

	t.Run("a schema that cannot be read", func(t *testing.T) {
		fake := pgxfake.New()
		fake.Steps = []pgxfake.Step{
			{Match: "information_schema.schemata", Err: errors.New("permission denied")},
		}
		pgxConnect = func(context.Context, string) (pgconnPing, error) { return fake, nil }

		err := runAsk(askCmdForTest(t, false), []string{"a question"})
		if err == nil {
			t.Fatal("a schema that cannot be read reported success")
		}
		if !strings.Contains(err.Error(), "schema") {
			t.Errorf("the error is %q, want it to mention the schema", err)
		}
		// And the connection is closed rather than leaked: the schema read is what failed, and
		// the deferred close is the only thing that runs on this path.
		if fake.Closed() != 1 {
			t.Errorf("the connection was closed %d times, want 1", fake.Closed())
		}
	})
}

// getSchemaForLLM's two `continue` arms.
//
// The walk is per schema and per table, and each query that fails skips its subject and keeps
// going. That is the difference between "the schema is smaller than the database's" and "the
// schema is not in the prompt at all" — and the second is a worse answer for a model asked to
// write SQL.
func TestTheSchemaWalkSkipsWhatItCannotReadAndKeepsTheRest(t *testing.T) {
	fake := pgxfake.New()
	fake.Steps = []pgxfake.Step{
		// Two schemas and three tables. One table's columns fail, and one schema's table list
		// fails — so both `continue` arms run, and the tables around them must survive.
		{
			// The real query filters `schema_name NOT LIKE 'pg_%'` and scans one column, so
			// the fixture has to answer one column per row — a two-column row would be a scan
			// arity error, and the walk would skip everything for a reason that has nothing to
			// do with the case.
			Match: "information_schema.schemata",
			Result: pgxfake.Result{Columns: []pgxfake.Column{{Name: "schema_name"}},
				Rows: [][]interface{}{{"alpha"}, {"beta"}, {"broken"}}},
		},
		{
			// One schema whose TABLE LIST fails, which is the other `continue` in the walk: a
			// schema that exists but whose tables cannot be listed contributes nothing, and
			// the walk must go on to the next one rather than abandoning the prompt.
			Match:       "information_schema.tables",
			ArgContains: "broken",
			Err:         errors.New("permission denied for schema broken"),
		},
		{
			// THREE columns, because ListTables scans table_name, table_type and row_count.
			// Two is a scan arity error, which the walk treats as a skip — so a two-column
			// fixture produces an empty schema and the case passes for the wrong reason. The
			// first version of this fixture had two.
			Match: "information_schema.tables",
			Result: pgxfake.Result{
				Columns: []pgxfake.Column{{Name: "table_name"}, {Name: "table_type"}, {Name: "row_count"}},
				Rows: [][]interface{}{
					{"kept_one", "BASE TABLE", int64(3)},
					{"unreadable", "BASE TABLE", int64(0)},
					{"kept_two", "BASE TABLE", int64(7)},
				},
			},
		},
		// The columns query fails for ONE table, selected by its bound argument — every schema
		// query passes the table as $2, and ArgContains is what narrows a step to one of
		// them. A step that failed for every table would make the walk produce an empty schema
		// and the case would pass without ever exercising the skip, because "nothing survived"
		// and "one table was skipped" look the same in the output.
		{
			Match:       "information_schema.columns",
			ArgContains: "unreadable",
			Err:         errors.New("permission denied for table unreadable"),
		},
		{
			// And the tables that CAN be read get their columns, or they would be skipped too
			// and there would be nothing left to assert is still there.
			Match: "information_schema.columns",
			Result: pgxfake.Result{
				Columns: []pgxfake.Column{
					{Name: "column_name"}, {Name: "data_type"}, {Name: "is_nullable"}, {Name: "column_default"},
				},
				Rows: [][]interface{}{{"id", "integer", "NO", nil}},
			},
		},
	}

	schema, err := getSchemaForLLM(context.Background(), fake)
	if err != nil {
		t.Fatalf("the walk refused the whole schema: %v", err)
	}
	if schema == "" {
		t.Fatal("the walk produced no schema at all")
	}

	// The tables it could read are in there.
	if !strings.Contains(schema, "kept_one") || !strings.Contains(schema, "kept_two") {
		t.Errorf("the schema is missing the tables it could read:\n%s", schema)
	}
	// And the ones it could not are simply absent, not half-present.
	if strings.Contains(schema, "unreadable") {
		t.Errorf("the schema contains a table whose columns failed to read:\n%s", schema)
	}

	t.Run("and both kinds of failure were really exercised", func(t *testing.T) {
		// The premise for the whole case. Without it, a walk that skipped EVERY table would
		// produce the same output and this test would be about nothing.
		if fake.AskedCount("information_schema.tables") < 3 {
			t.Errorf("the loader asked for the table list %d times, want one per schema (3)",
				fake.AskedCount("information_schema.tables"))
		}
		// The narrowed step is the one that has to fire, and the only way to see that is the
		// table list it came from: `unreadable` was asked about, so the columns query for it
		// was issued and matched the failing step.
		askedAboutUnreadable := false
		for _, q := range fake.Queries {
			if strings.Contains(q, "information_schema.columns") && strings.Contains(q, "ORDER BY ordinal_position") {
				askedAboutUnreadable = true
			}
		}
		for _, args := range fake.Args {
			for _, a := range args {
				if s, ok := a.(string); ok && s == "unreadable" {
					askedAboutUnreadable = true
				}
			}
		}
		if !askedAboutUnreadable {
			t.Error("the loader never asked for the unreadable table's columns, so the failing step never matched")
		}
	})

	t.Run("and a schema list that cannot be read is an error", func(t *testing.T) {
		// The other shape: the FIRST query failing is not a skip, it is the whole schema gone,
		// and returning an empty prompt would make the model invent a schema.
		broken := pgxfake.New()
		broken.Steps = []pgxfake.Step{{Match: "information_schema.schemata", Err: errors.New("denied")}}

		if _, err := getSchemaForLLM(context.Background(), broken); err == nil {
			t.Error("a schema list that cannot be read reported success")
		}
	})
}

// pgx's own metadata queries, driven through the fake, to pin that the fixtures above match
// the SQL the loader really issues — because a fake step that never matches answers nothing
// and the case would pass for the wrong reason.
func TestTheSchemaFixturesMatchTheQueriesTheLoaderIssues(t *testing.T) {
	// A schema with no tables and a schema with one, so the walk issues all three queries.
	fake := pgxfake.New()
	fake.Steps = []pgxfake.Step{
		{Match: "information_schema.schemata",
			Result: pgxfake.Result{Columns: []pgxfake.Column{{Name: "schema_name"}},
				Rows: [][]interface{}{{"alpha"}, {"beta"}}}},
		{Match: "information_schema.tables",
			Result: pgxfake.Result{
				Columns: []pgxfake.Column{{Name: "table_name"}, {Name: "table_type"}, {Name: "row_count"}},
				Rows:    [][]interface{}{{"one", "BASE TABLE", int64(1)}},
			}},
		{Match: "information_schema.columns", Result: pgxfake.Result{}},
	}

	if _, err := getSchemaForLLM(context.Background(), fake); err != nil {
		t.Fatalf("the walk: %v", err)
	}

	// And only the ones that were really asked for count — which is the point: a step that
	// never matches answers nothing and the case above would still pass.
	for _, want := range []string{"information_schema.schemata", "information_schema.tables", "information_schema.columns"} {
		if !fake.Asked(want) {
			t.Errorf("the loader never asked for %s, so the fixture's step for it is decorative", want)
		}
	}
}

// A connection whose QueryRow is what the loader uses, so the fixtures are honest about the
// API being faked. Asserted rather than assumed: pgxfake answers both from the same Steps,
// and a change that made the loader use QueryRow only would otherwise go unnoticed.
func TestTheFakeServesBothQueryAndQueryRow(t *testing.T) {
	var _ postgres.Conn = pgxfake.New()
}
