package app

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// testDBImage is pinned explicitly; never use :latest.
const testDBImage = "postgres:16-alpine"

var (
	sharedDBOnce sync.Once
	sharedDBCtr  *tcpostgres.PostgresContainer
	sharedDBDSN  string
	sharedDBErr  error
)

// TestMain terminates the shared PostgreSQL container (if one was started)
// after the package's tests finish.
func TestMain(m *testing.M) {
	code := m.Run()
	stopSharedPostgres()
	os.Exit(code)
}

// testDSN resolves the PostgreSQL DSN for integration tests:
//
//  1. DBX_TEST_DSN, when set, is used as-is (fast path, no container).
//  2. Otherwise a single shared testcontainers PostgreSQL is started for the
//     whole package run, so the authoritative READ ONLY tests run automatically
//     wherever Docker is available (including CI).
//  3. If Docker/testcontainers is unavailable the test is skipped with a clear
//     message instead of failing.
func testDSN(t *testing.T) string {
	t.Helper()
	if dsn := os.Getenv("DBX_TEST_DSN"); dsn != "" {
		return dsn
	}
	testcontainers.SkipIfProviderIsNotHealthy(t)

	dsn, err := sharedPostgresDSN()
	if err != nil {
		t.Fatalf("start testcontainers PostgreSQL (%s): %v", testDBImage, err)
	}
	return dsn
}

// sharedPostgresDSN starts (once per package) the shared PostgreSQL container.
func sharedPostgresDSN() (string, error) {
	sharedDBOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		ctr, err := tcpostgres.Run(ctx, testDBImage,
			tcpostgres.WithDatabase("dbx_test"),
			tcpostgres.WithUsername("dbx"),
			tcpostgres.WithPassword("dbx"),
			tcpostgres.BasicWaitStrategies(),
		)
		if err != nil {
			sharedDBErr = err
			return
		}

		dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			sharedDBErr = err
			_ = ctr.Terminate(ctx)
			return
		}

		sharedDBCtr = ctr
		sharedDBDSN = dsn
	})
	return sharedDBDSN, sharedDBErr
}

func stopSharedPostgres() {
	if sharedDBCtr == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = sharedDBCtr.Terminate(ctx)
}
