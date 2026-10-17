package app

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
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

// TestMain sandboxes the clipboard, then terminates the shared PostgreSQL container (if
// one was started) after the package's tests finish.
func TestMain(m *testing.M) {
	dir := sandboxClipboard()
	code := m.Run()
	_ = os.RemoveAll(dir)
	stopSharedPostgres()
	os.Exit(code)
}

// clipboardHelperNames are every clipboard helper copyToClipboard can pick on the POSIX
// platforms this suite runs on. The sandbox writes a stub for each and the pin test reads
// the same list, so a helper the sandbox forgets is a red test and not a silent gap.
var clipboardHelperNames = []string{"wl-copy", "xclip", "xsel", "pbcopy"}

// sandboxClipboard puts fake clipboard helpers at the FRONT of PATH for the whole package
// and returns the directory holding them.
//
// copyToClipboard resolves its helper through PATH, so one case that forgets to stub the
// seam does not fail — it silently overwrites the clipboard of whoever ran the suite with
// a fixture like "SELECT * FROM users WHERE id = 1". That is what happened here: the
// package's own copy-SQL cases ran the real helper, and the person running the tests found
// their clipboard holding a statement they never copied.
//
// The fakes swallow their stdin and exit 0, so a forgotten stub copies nothing anywhere.
// The cases that DO inspect the payload prepend their own stub to PATH, which still wins
// because it goes in front of this directory.
func sandboxClipboard() string {
	dir, err := os.MkdirTemp("", "dbx-clipboard-sandbox")
	if err != nil {
		log.Fatalf("clipboard sandbox: %v", err)
	}
	for _, name := range clipboardHelperNames {
		stub := filepath.Join(dir, name)
		if err := os.WriteFile(stub, []byte("#!/bin/sh\ncat > /dev/null\n"), 0o700); err != nil {
			log.Fatalf("clipboard sandbox: %v", err)
		}
	}
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH")); err != nil {
		log.Fatalf("clipboard sandbox: %v", err)
	}
	return dir
}

// TestTheClipboardSandboxIsInstalled pins the net, because a net nobody checks is removed
// by the next person who finds the extra PATH entry surprising.
//
// It reads the files instead of calling copyToClipboard on purpose: probing the seam would
// reintroduce the leak if the sandbox were ever the thing that was removed.
func TestTheClipboardSandboxIsInstalled(t *testing.T) {
	first := strings.SplitN(os.Getenv("PATH"), string(os.PathListSeparator), 2)[0]
	for _, name := range clipboardHelperNames {
		data, err := os.ReadFile(filepath.Join(first, name))
		if err != nil {
			t.Fatalf("%s is not sandboxed, so a case that forgets to stub copyToClipboard reaches the real clipboard: %v", name, err)
		}
		if !strings.Contains(string(data), "/dev/null") {
			t.Errorf("%s does not swallow its stdin: %q", name, data)
		}
	}
}

// testDSN resolves the PostgreSQL DSN for integration tests:
//
//  1. DBX_TEST_DSN, when set, is used as-is (fast path; Docker is never
//     touched).
//  2. Otherwise a single shared testcontainers PostgreSQL is started for the
//     whole package run, so the authoritative READ ONLY tests run automatically
//     wherever Docker is available (including CI).
//  3. Docker is REQUIRED on that default path: if the testcontainers provider
//     is unhealthy the test fails loudly (t.Fatalf) with the provider error and
//     the explicit opt-out, instead of silently skipping and leaving CI green
//     but empty.
//  4. DBX_SKIP_DOCKER=1 is the only way to opt out: the test is then skipped
//     with a message naming the variable. This opt-out is deliberate and
//     visible; it is never inferred.
func testDSN(t *testing.T) string {
	t.Helper()
	if dsn := os.Getenv("DBX_TEST_DSN"); dsn != "" {
		return dsn
	}

	// Skip before probing Docker: an explicit opt-out must not depend on a
	// (possibly slow or hanging) provider health check.
	skipEnv := os.Getenv("DBX_SKIP_DOCKER")
	var healthErr error
	if skipEnv != "1" {
		healthErr = dockerProviderHealth()
	}

	switch fail, msg := dockerGate(skipEnv, healthErr == nil); {
	case fail:
		t.Fatalf("%s\nprovider error: %v", msg, healthErr)
	case msg != "":
		t.Skip(msg)
	}

	dsn, err := sharedPostgresDSN()
	if err != nil {
		t.Fatalf("start testcontainers PostgreSQL (%s): %v", testDBImage, err)
	}
	return dsn
}

// dockerGate is the pure fail-vs-skip decision for an unavailable Docker
// provider, kept separate from the testcontainers probing so it can be tested
// without a Docker daemon.
//
// skipEnv is the raw value of DBX_SKIP_DOCKER and healthy reports whether the
// testcontainers provider responded. It returns fail=true when the caller must
// abort (Docker is required and no opt-out was requested) and a non-empty msg
// whenever the caller must not proceed silently: the fatal guidance, or the
// skip message. A healthy provider without an opt-out yields (false, "").
func dockerGate(skipEnv string, healthy bool) (fail bool, msg string) {
	if skipEnv == "1" {
		return false, "DBX_SKIP_DOCKER=1: skipping real-PostgreSQL integration tests (explicit opt-out; Docker is not required)"
	}
	if healthy {
		return false, ""
	}
	return true, "real-PostgreSQL integration tests need a working Docker daemon (testcontainers provider is unhealthy).\nIf Docker is intentionally unavailable, opt out explicitly with:\n\n\tDBX_SKIP_DOCKER=1 go test ./internal/app/..."
}

// dockerProviderHealth reports whether the testcontainers Docker provider is
// usable, returning the underlying error instead of skipping. It mirrors
// testcontainers.SkipIfProviderIsNotHealthy but leaves the fail-vs-skip
// decision to dockerGate, so an unhealthy provider can no longer silence the
// suite.
func dockerProviderHealth() (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("recovered from panic while probing Docker: %v", r)
		}
	}()

	provider, err := testcontainers.ProviderDocker.GetProvider()
	if err != nil {
		return err
	}
	return provider.Health(context.Background())
}

func TestDockerGate(t *testing.T) {
	tests := []struct {
		name     string
		skipEnv  string
		healthy  bool
		wantFail bool
		wantSkip bool
	}{
		{name: "healthy provider proceeds silently", skipEnv: "", healthy: true},
		{name: "unhealthy provider fails", skipEnv: "", healthy: false, wantFail: true},
		{name: "explicit opt-out skips when unhealthy", skipEnv: "1", healthy: false, wantSkip: true},
		{name: "explicit opt-out skips when healthy too", skipEnv: "1", healthy: true, wantSkip: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fail, msg := dockerGate(tt.skipEnv, tt.healthy)
			if fail != tt.wantFail {
				t.Fatalf("dockerGate(%q, %v) fail = %v, want %v", tt.skipEnv, tt.healthy, fail, tt.wantFail)
			}
			if !tt.wantSkip && !tt.wantFail {
				if msg != "" {
					t.Fatalf("healthy provider without opt-out must stay silent, got %q", msg)
				}
				return
			}
			if !strings.Contains(msg, "DBX_SKIP_DOCKER") {
				t.Fatalf("message %q must name DBX_SKIP_DOCKER", msg)
			}
		})
	}
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
