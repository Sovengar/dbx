# ADR 0001: ASK executes generic SELECTs under a READ ONLY transaction

- **Status:** Accepted
- **Date:** 2026-09-23
- **Feature:** `0003-feature-ask-nl2sql-chat`
- **Deciders:** feature owner (approved during planning and review)

## Context

dbx already documented `a` as "Ask (NL→SQL)" and shipped a CLI flow
(`dbx ask`) that converts a natural-language question into SQL. The TUI gained
an ASK pane that lets a user who knows the data but not SQL query the database
from the grid.

Two questions had to be answered before wiring the pane:

1. **What does the model produce?** A whole `SELECT` (joins, aggregates,
   arbitrary projections) or only a `WHERE` fragment appended to the
   currently-loaded table?
2. **How is "read-only" actually guaranteed?** A model can emit anything,
   including `DELETE`, `UPDATE`, `DROP`, or data-modifying CTEs.

Both have security and reversibility consequences, so they are recorded here.

## Decision

### 1. Execute the complete SELECT, not a WHERE filter

ASK generates and runs a **full `SELECT`**. The currently-loaded grid table and
its active `WHERE` clause are passed to the model as a **context hint**, never
as a hard restriction:

- The hint is descriptive ("the grid is currently showing table
  `public.users`; its current WHERE clause is `active = true`").
- ASK remains globally available regardless of what the grid shows.
- Query results are stored by the grid under the synthetic table name `query`
  and therefore never contribute a hint.

**Why not WHERE-only:** WHERE-only cannot answer aggregate or cross-table
questions ("how many orders per customer?"), which is exactly the class of
question a non-SQL user asks. Restricting to WHERE would make the feature
useless for its target user.

### 2. SELECT-only enforced in two layers

1. **Client-side validation (`selectOnlyViolation`)** — accepts only a single
   `SELECT` or `WITH … SELECT`. It tolerates leading comments, stray
   semicolons, dollar-quoted strings and `E''` escapes, and rejects
   data-modifying CTEs plus `INTO`, `FOR` (row locks) and `nextval`
   (sequence advancement) with a human-readable reason. This is feedback, not
   the guarantee.
2. **Server-enforced READ ONLY transaction (`statementRunner.executeReadOnly`)**
   — the statement runs inside `BeginTx(ctx, pgx.TxOptions{AccessMode:
   pgx.ReadOnly})` and is always rolled back (nothing is ever committed).
   PostgreSQL rejects any mutation that slips past validation.

**Why two layers:** the validator gives immediate, specific feedback and keeps
obvious mistakes from touching the database at all; the READ ONLY transaction
is the authoritative guard that holds even if the validator is wrong. A real
database is required to verify the guarantee (see below).

### 3. A pending DML transaction refuses ASK execution

If the user has an open, uncommitted DML transaction, ASK **refuses to
execute** and tells them to commit or roll back first. pgx cannot open a second
transaction on the same connection, and silently committing the user's draft
changes to run an AI query would be data loss. Refusing is the safe default.

### 4. Generated SQL is reviewed before it runs

The generated SQL is shown in the transcript and only runs when the user
confirms with `enter`; `esc` cancels without executing. This gives the user
control before anything touches the database.

## Consequences

**Positive**

- Non-SQL users can answer aggregate/join questions, not just filter a table.
- The SELECT-only guarantee does not depend on the client validator being
  perfect.
- No silent commits of user drafts.

**Negative / tradeoffs**

- The client validator over-rejects a few valid statements that contain
  mutation keywords (e.g. `SELECT … FOR UPDATE` is intentionally refused). This
  is acceptable: the authoritative guard is the transaction, and over-rejection
  is fail-safe.
- Correctness of the READ ONLY guarantee is only proven against a real
  PostgreSQL. The proving tests (`TestAsk_ReadOnlyTxRejectsDML` and
  `TestAsk_ReadOnlyTxRejectsSelectBasedMutation`) run **automatically**: they
  start a real PostgreSQL via testcontainers-go (`postgres:16-alpine`) when
  `DBX_TEST_DSN` is unset, and use `DBX_TEST_DSN` as an override when set. This
  is a **mandatory verification step**, not optional.
- **Accepted tradeoff:** testcontainers-go adds ~50 indirect Go modules for a
  test-only need, and the tests require Docker at run time; without Docker they
  skip with a clear message. A hand-rolled Docker invocation or a committed
  Postgres service was rejected as more brittle.
- A single connection is shared: ASK cannot run while a DML transaction is
  pending (mitigated by the refusal above), and other DB commands are refused
  while an ASK read-only transaction is in flight.

## Alternatives considered

- **WHERE-filter-only execution.** Rejected: too limited for the target user.
- **Client validator only.** Rejected: a model can bypass it; the server must
  be the authority.
- **Commit the pending DML transaction to free the connection.** Rejected:
  silently applying the user's drafts is data loss.
- **Execute in autocommit after validation.** Rejected: no server-side
  guarantee against mutations.

## References

- Plan: `docs/planning/0003-feature-ask-nl2sql-chat/plan.md`
- Behavior: `docs/planning/0003-feature-ask-nl2sql-chat/behavior.feature`
- Code: `internal/app/txn.go` (`selectOnlyViolation`, `executeReadOnly`),
  `internal/app/app.go` (`executeAskSQL`), `internal/ui/components/ask/`
