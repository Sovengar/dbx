---
name: dbx
description: "Trigger: dbx, PostgreSQL client, SQL query, schema export, session replay, database TUI, or .dbx.toml. Operates and configures dbx."
---

## Activation Contract

Load for dbx queries, connections, schema export, replay, TUI use, or configuration.

## Hard Rules

- Confirm the target database before a write operation.
- Use read-only queries first when investigating.
- Preserve dbx JSON output and error data.

## Execution Steps

1. Inspect connection and target context.
2. Run the smallest query or dbx operation that answers the request.
3. Verify changes with a read query when writes are authorized.
4. Report database, command, result, and warnings.

## Operation Guide

Use the TUI for interactive investigation. Use the CLI and JSON output for automation, schema export, or session replay. Keep a query read-only until the user authorizes a write.

