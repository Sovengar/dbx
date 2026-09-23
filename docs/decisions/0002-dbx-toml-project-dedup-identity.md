# ADR 0002: Project dedup identity for `.dbx.toml` scan

- **Status:** Accepted
- **Date:** 2026-09-23
- **Feature:** `0004-fix-dedup-dbx-toml`
- **Deciders:** feature owner (approved during planning)

## Context

`.dbx.toml` files can be reached more than once by the project scanner: a git
repository with linked worktrees, or a `.dbx.toml` in several subdirectories of
the same repo, produced duplicate picker entries for the same real connection.
The scanner also crashed on a `.dbx.toml` without a `[connections]` section
(`loadProject` returned `(nil, nil)`, and a caller dereferenced the nil), and
picked a non-deterministic connection when a file defined several (map
iteration order). Output order depended on `fd`'s traversal order.

Three questions had to be answered:

1. **How to derive repository identity without spawning `git`?** Running
   `git rev-parse --git-common-dir` would work but depends on `git` in `PATH`,
   is non-hermetic, and is slow per file.
2. **What is the dedup key, and which duplicate survives?**
3. **Where does dedup run — scanner or caller?**

## Decision

**Identity = canonical git common dir, parsed from `.git` without a subprocess.**
From the directory of each `.dbx.toml`, walk up to find `.git`, **bounded by the
scan root** (`Scanner.RootDir`, inclusive): the search never inspects ancestors
of the root, so a git repo that merely *contains* the root (e.g. a
yadm/dotfiles `$HOME`, or `~/dev`) does not absorb non-git projects and silently
break "non-git does not deduplicate".

- `.git` **directory** → the repo itself; the common dir is that directory, and
  the directory is a **main checkout**.
- `.git` **file** (`gitdir: <X>`) → read `<X>/commondir` (relative to `X`); if
  absent, use `X`. This is exactly how a linked worktree shares the main repo's
  common git dir, so main and worktree collapse together.
- **No `.git` up to the root** → identity is the project's own canonical path,
  which is unique, so non-git directories never deduplicate.

Canonicalization is absolute + clean + `EvalSymlinks`, falling back to the clean
path when resolution fails (conservative dedup over a panic).

The **dedup key** is
`(repo identity, connection name, driver, GetDSN(), ssh_tunnel)` — the DSN
compared **after `${env:...}` expansion**, so the same resolved DSN written in
different forms collapses, and connections differing only in driver or SSH tunnel
are not merged.

The **survivor** is the **main checkout** first (its resolved `.git` is a
directory; linked worktrees have a `.git` file), regardless of path length. On a
tie, or when no candidate is a main checkout, the shortest path wins, then the
lexicographic order. The choice is independent of session/active state, so
results are stable across sessions; active/inactive state is applied **after**
dedup, to the surviving path.

Dedup runs inside **`Scanner.Scan()`**, the single consumption point of the TUI
(the CLI does not use the scanner). Output is sorted by `(Path, Name)` for a
reproducible picker order. Discovery is behind an injectable seam
(`findFiles func(root string) []string`, nil → real `fd`/`WalkDir`), so the logic
is testable without `fd`; the `fd`→`WalkDir` fallback triggers on *loaded
projects*, not on raw paths.

## Alternatives considered

- **`git rev-parse` subprocess.** Rejected: adds a `git` dependency, a process
  per file, and nondeterminism under unusual environments; the `.git` file format
  is simple and stable enough to parse.
- **Worktree gitdir as identity (instead of the common dir).** Rejected: main and
  worktree would not collapse, which is the whole point.
- **Unbounded `.git` walk (up to `/`).** Rejected after review: an enclosing repo
  (dotfiles `$HOME`, `~/dev`) would give every non-git project the same identity
  and collapse them, violating "non-git does not deduplicate". The walk is bounded
  to the scan root.
- **Survivor by shortest path only.** Rejected after review: a linked worktree can
  have a shorter path than the main checkout; the main checkout must win. Path
  ordering remains only as the tie-breaker.
- **Dedup key without driver/ssh_tunnel.** Rejected after review: connections
  differing only in driver or SSH tunnel are distinct and must not collapse.
- **Dedup in the caller (`internal/app`).** Rejected: would duplicate the logic
  at every call site and leave the CLI/scanner contract ambiguous; the scanner is
  the natural owner.
- **Survivor by active state first.** Rejected: makes the result depend on the
  user's session state; main-checkout + path ordering is deterministic and
  reproducible.
- **Non-git identity based on name, or skipping dedup only for git.** Rejected:
  using the path keeps legitimate homonymous non-git projects separate.
- **New dependencies (e.g. a gitignore/git library).** Rejected: stdlib only.

## Consequences

- One picker entry per real `(repo, connection name, driver, DSN, ssh_tunnel)`;
  worktrees and duplicate subdirs collapse, with the main checkout surviving
  (or the shortest path when there is no single main checkout).
- A `.dbx.toml` without connections is skipped silently (sentinel
  `errNoConnections`); `Scan()` returns an empty slice instead of crashing.
- Connection selection per file is deterministic (lexicographically first name).
- Output order is stable regardless of `fd`'s ordering.
- The `.git` search is bounded by the scan root, so a repo enclosing the root
  (dotfiles `$HOME`, `~/dev`) no longer collapses unrelated non-git projects.
- The `fd`→`WalkDir` fallback keeps its previous semantics: it fires when no
  project loads, not merely when `fd` returns no paths.
- The active/inactive state of a discarded duplicate path is left orphaned in
  `project_state.json` but is inert (never shown, never applied). If the discarded
  path was the one toggled and the survivor differs, **the survivor's state
  governs**. Accepted, not migrated.
- A `.dbx.toml` in a repo subdirectory shares identity with the repo root; this is
  expected and documented.
- Known pre-existing divergence is unchanged: `fd --hidden` scans hidden dirs,
  the `WalkDir` fallback skips them (out of scope).
