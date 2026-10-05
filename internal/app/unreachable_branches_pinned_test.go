package app

// Scenario: Las CINCO ramas que quedan sin cubrir, y por que no se pueden cubrir.
//
// This file executes nothing on purpose. It is the last five statements of the repository that
// no test can reach, and each one is listed here with the call site, the reason, and what it
// would take. Written down rather than left as a coverage number, because an uncovered
// statement nobody has classified is indistinguishable from a bug, and this project has found
// about two dozen real bugs by reading exactly these places.
//
// The three questions a pin has to answer, and the three shapes of answer here:
//
//	what state would make it fire?         a state production cannot be RESTED into
//	why can't a test arrange that state?  because it needs a machine, a socket, or a race
//	what would it mean if it fired?        so the arm is known to be right or wrong
//
// The fourth kind — a branch that is provably dead — is NOT here. Those were removed with the
// measurement written next to the removal, and each is held by a test that asserts the
// invariant that killed it. A dead branch pinned as "unreachable" is just a dead branch with
// extra steps.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/buble/dbx/internal/config"
	"github.com/buble/dbx/internal/drivers/postgres"
	"github.com/buble/dbx/internal/testsupport/pgxfake"
)

// ── 1. app.go: the ping after a successful connect — RESOLVED ───────────────

// RESOLVED. This branch was the first one pinned and the first one made reachable, so it is
// the worked example for the other four: read what state would make it fire, find the one call
// a test cannot make for itself, and put a variable in front of it.
//
// Call site: connectToDB, the `conn.Ping(ctx)` immediately after the connect returns.
// What would make it fire: a server that ACCEPTS the connection and then does not answer.
// What it would mean: "failed to ping: ...", which tells a user the DSN is right and the
// server is not ready — different advice from a refused connection, which is the whole reason
// the two are separate arms.
//
// WHAT IT TOOK: a package variable `pgxConnect` beside connectToDB, exactly as the CLI has one
// — and TestConnectingToAServerThatCannotAnswer at the bottom of this file drives the branch
// through it. What the seam bought beyond coverage is a fixed leak: the old code returned
// after a failed Ping without closing the connection pgx.Connect had already opened.
//
// The entry above is left in place as the record of how the branch was classified before it
// was reachable, because "what would it take" is the question worth answering when the next
// unreachable branch turns up.

// ── 2 and 3. session/logger.go: the two DirEntry.Info() guards — RESOLVED ───

// RESOLVED, by the injection the entry below predicted. Kept as the second worked example.
//
// Call sites: the `if err != nil { continue }` after `entry.Info()` in Logger.Cleanup's
// retention sweep and in Reader.ListSessions.
//
// What would make them fire: the file disappearing between os.ReadDir returning the entry and
// Info() stat-ing it. Nothing else.
//
// Why a test could not arrange it before the seam: two reasons, and both are properties of the
// standard library.
//
//	os.ReadDir's DirEntry.Info() calls LSTAT, not stat. So a DANGLING SYMLINK — a symlink
//	whose target does not exist — stats successfully and never reaches the guard. That is the
//	case a fixture would naturally use, and the guard survives it.
//
//	Permissions do not help either: a file the process cannot stat fails for EVERY entry in
//	the directory at once, which the caller treats as "nothing to sweep", not as a per-entry
//	failure. The guard is specifically about ONE entry vanishing while its siblings are fine.
//
// So the only trigger was a race, and a test that creates one is a test that either does not
// reproduce or hangs. Which is the question the seam answered instead.
//
// What they would mean: a rotation that skips one vanished file instead of failing. Which is
// the right behaviour, and is why the arm is a `continue` rather than an error return.
//
// WHAT IT TOOK: exactly the injection named above. `readDir func(string) ([]os.DirEntry,
// error)` is now a field on both the Logger and the Reader, production passes os.ReadDir, and
// TestTheRetentionSweepSkipsAFileThatCannotBeStatEd and
// TestTheSessionListingSkipsAFileItCannotStat drive both guards with a three-line entry whose
// Info() fails. Both call sites go through a listDir() helper rather than calling os.ReadDir
// directly — a seam two of three call sites bypass is not a seam.

// ── 4. cli/root.go: the default program runner ──────────────────────────────

// Call site: the body of runProgram's default value, `tea.NewProgram(m).Run()`.
//
// What would make it fire: it is not a branch. It is the default of a seam variable, and it
// runs whenever a test calls runProgram without replacing it.
//
// Why a test cannot reach it: a real bubbletea program opens /dev/tty. A test process usually
// has no controlling terminal even when its own stdin is a character device, so Run fails with
// "could not open TTY". The sibling default — buildModel — IS covered, because constructing
// the model needs no terminal; and that asymmetry is why one of the two lines sat uncovered
// while its neighbour was exercised every round.
//
// What it would mean: nothing observable, which is the point: it is a two-word call into a
// library whose own tests cover it.
//
// What it would take: a terminal, which CI does not have. The case that would assert it
// exists and skips with the reason printed.

// ── 5. postgres/query.go: a row whose Values() fails ────────────────────────

// Call site: the `if err != nil { return nil, fmt.Errorf("failed to scan row: %w", err) }`
// inside ExecuteQuery's iteration loop.
//
// What would make it fire: pgx failing to decode a field into the generic `any` it hands back.
// That happens for an OID pgx cannot map, or a wire-level corruption mid-row.
//
// Why a test cannot arrange it: the row is a pgx.Rows, produced by the connection. The seam
// that exists — postgres.Conn — hands back whatever the implementation's Query returns, and
// pgxfake's rows are its own type. To make Values() fail, the fake would need a value pgx's
// decoder rejects, which means implementing a second decoder, which is a much larger thing to
// own than the branch is worth.
//
// The neighbouring arm IS covered and is the one that happens: a failure from Next() itself,
// which pgxfake's IterErr reproduces exactly.
//
// What it would mean: the query fails with "failed to scan row: ...", naming the row rather
// than the query — which is why it is a distinct message and a distinct arm.
//
// What it would take: a pgx.Rows implementation in pgxfake with a configurable Values()
// failure. Doable, and it is a real candidate for the next round if the number is ever worth
// six figures to a project that already covers the other 99.9%.

// This is a _test.go file on purpose. A comment about unreachable branches is a note to the
// next person reading the tests; shipping it inside the binary would put a page of prose about
// test fixtures into a program whose whole job is to draw a table.

// ── 1. app.go: the ping after a successful connect — NOW REACHABLE ──────────

// The ping refusal, through the injected connect.
//
// This was the first of the five pinned branches, and the pin said what it would take: "a
// pgxConnect field on Model, exactly as the CLI has one". That is now what exists — a package
// variable beside connectToDB — so the branch is covered and the entry above is history. It is
// left in place rather than deleted because it explains WHY the seam is there, and someone
// removing the seam would want to read that first.
//
// The scenario is a server that accepted the connection and then could not answer a query: the
// DSN parsed, the socket opened, and the very first round trip fails. That is a real state —
// a server mid-restart, a connection pool exhausted, a proxy that accepted and gave up — and it
// is the one that distinguishes "failed to connect" from "failed to ping".
func TestConnectingToAServerThatCannotAnswer(t *testing.T) {
	oldConnect := pgxConnect
	t.Cleanup(func() { pgxConnect = oldConnect })

	t.Run("a connection that cannot be pinged is closed and refused", func(t *testing.T) {
		pingErr := errors.New("server is starting up")
		fake := pgxfake.New()
		fake.PingErr = pingErr
		pgxConnect = func(context.Context, string) (postgres.Conn, error) { return fake, nil }

		cmd := m0(t).connectToDB(config.FoundProject{
			Name:       "shopdb",
			Path:       t.TempDir(),
			Connection: config.ProjectConnection{Driver: "postgres", DSN: "postgres://dbx@127.0.0.1:1/db"},
		})
		if cmd == nil {
			t.Fatal("the connect refused instead of answering")
		}

		msg, ok := cmd().(dbConnectedMsg)
		if !ok {
			t.Fatalf("the connect produced %T, want dbConnectedMsg", cmd())
		}
		if msg.err == nil {
			t.Fatal("a connection that cannot answer reported success")
		}
		if !strings.Contains(msg.err.Error(), "failed to ping") {
			t.Errorf("the error is %q, want it to say the PING failed rather than the connection", msg.err)
		}
		if !errors.Is(msg.err, pingErr) {
			t.Errorf("the error does not wrap the server's own: %v", msg.err)
		}
		// And no connection is handed over: the app would otherwise report itself connected
		// and fail on the first query with a much less useful message.
		if msg.conn != nil {
			t.Error("a refused connection was also returned")
		}
		// The leak that this branch used to have. pgx.Connect had already opened the socket by
		// the time Ping failed, and the old code returned without closing it — so every
		// connection to a server that accepted and then failed left a socket and a backend
		// process behind. Closing it is the difference between one error message and a slow
		// exhaustion.
		if fake.Closed() != 1 {
			t.Errorf("the unusable connection was closed %d times, want 1", fake.Closed())
		}
	})

	t.Run("a DSN that cannot be connected says so instead", func(t *testing.T) {
		// The neighbouring arm, and the one a user actually hits: nothing is listening on the
		// port. It has to be a DIFFERENT message, because the advice is different — "failed to
		// connect" sends you to the DSN, "failed to ping" sends you to the server.
		refused := errors.New("connection refused")
		pgxConnect = func(context.Context, string) (postgres.Conn, error) { return nil, refused }

		cmd := m0(t).connectToDB(config.FoundProject{
			Name:       "shopdb",
			Path:       t.TempDir(),
			Connection: config.ProjectConnection{Driver: "postgres", DSN: "postgres://dbx@127.0.0.1:1/db"},
		})
		if cmd == nil {
			t.Fatal("the connect refused instead of answering")
		}
		msg, ok := cmd().(dbConnectedMsg)
		if !ok {
			t.Fatalf("the connect produced %T, want dbConnectedMsg", cmd())
		}
		if msg.err == nil {
			t.Fatal("a refused connection reported success")
		}
		if !strings.Contains(msg.err.Error(), "failed to connect") {
			t.Errorf("the error is %q, want it to say the CONNECTION failed", msg.err)
		}
		if strings.Contains(msg.err.Error(), "failed to ping") {
			t.Error("a connection that never opened was reported as a ping failure")
		}
	})

	t.Run("a working connection is handed over", func(t *testing.T) {
		// The counterweight: a connect that always failed would satisfy both cases above.
		fake := pgxfake.New()
		pgxConnect = func(context.Context, string) (postgres.Conn, error) { return fake, nil }

		cmd := m0(t).connectToDB(config.FoundProject{
			Name:       "shopdb",
			Path:       t.TempDir(),
			Connection: config.ProjectConnection{Driver: "postgres", DSN: "postgres://dbx@127.0.0.1:5432/db"},
		})
		msg, ok := cmd().(dbConnectedMsg)
		if !ok {
			t.Fatalf("the connect produced %T, want dbConnectedMsg", cmd())
		}
		if msg.err != nil {
			t.Fatalf("a working connection reported %v", msg.err)
		}
		if msg.conn == nil {
			t.Fatal("a working connection returned nothing")
		}
		if fake.Pinged() != 1 {
			t.Errorf("the connection was pinged %d times, want 1 — the ping is what says it works", fake.Pinged())
		}
		// And it is NOT closed: the app keeps it for the rest of the session.
		if fake.Closed() != 0 {
			t.Errorf("a working connection was closed %d times, want 0", fake.Closed())
		}
	})

	t.Run("and a project with no DSN never dials", func(t *testing.T) {
		// The check before the connect, which is why an empty DSN is not a network error.
		dialed := 0
		pgxConnect = func(context.Context, string) (postgres.Conn, error) {
			dialed++
			return pgxfake.New(), nil
		}

		cmd := m0(t).connectToDB(config.FoundProject{Name: "shopdb", Path: t.TempDir()})

		msg, ok := cmd().(dbConnectedMsg)
		if !ok {
			t.Fatalf("the connect produced %T", cmd())
		}
		if msg.err == nil || !strings.Contains(msg.err.Error(), "no DSN") {
			t.Errorf("the error is %v, want it to say there is no DSN", msg.err)
		}
		if dialed != 0 {
			t.Errorf("a project with no DSN dialed %d times", dialed)
		}
	})
}
