# TODO —mutation gate backlog

Objective: widen the gate in `MUTATE_EXCLUDE` file by file. Two rules, learned the hard way:

1. **Tests first, gate second.** A file is admitted only once its survivors are
   killed or proven equivalent. Admitting an uncovered file makes the check green
   by construction, which is the failure mode the gate exists to prevent.
2. **Write tests from the contract**, never from a guessed mutant replacement. On
   `bordered` and `txn.go` that produced 7 wrong "equivalent" claims.

## How to measure one file

```bash
go tool gremlins unleash ./path/to/pkg --workers 4 --timeout-coefficient 3 \
  --output /tmp/m.json
jq -r '.files[].mutations[] | select(.status=="LIVED") | "\(.type) \(.line):\(.column)"' /tmp/m.json
```

Per-file counts across the whole module (measured on `ad614e3`):

```
nc=  0 lived=  0 killed=  11  internal/ui/components/explorer/node.go
nc=  0 lived=  0 killed=  4  internal/ui/components/palette/commands.go
nc=  0 lived= 10 killed=  53  internal/ui/toast.go                      [GATED]
nc=  0 lived=  2 killed=  42  internal/config/keybindings_groups.go      [GATED]
nc=  0 lived=  9 killed=  32  internal/ui/modal.go                      [GATED]
nc=  2 lived=  0 killed=  13  internal/config/keybindings.go            [GATED]
nc=  2 lived=  0 killed=  24  internal/ui/keybindspane.go               [GATED]
nc=  2 lived= 11 killed=  90  internal/ui/bordered/bordered.go          [GATED]
nc=  2 lived=  9 killed=  34  internal/ui/components/grid/mouse.go
nc=  0 lived=  0 killed=   3  internal/cli/root.go
nc=  3 lived=  0 killed=   0  internal/config/config.go                 [GATED]
nc=  4 lived=  0 killed=   0  internal/app/router.go
nc=  0 lived=  0 killed=   6  internal/cli/context.go
nc=  6 lived= 16 killed=  15  internal/ui/components/grid/header.go
nc=  8 lived=  0 killed=   0  internal/ai/nl2sql/anthropic.go
nc=  9 lived=  0 killed=   0  internal/ai/nl2sql/{deepseek,openai,qwen}.go
nc=  9 lived=  0 killed=   0  internal/ui/components/grid/export_picker.go
nc= 10 lived=  2 killed=   9  internal/config/scanner.go                [GATED]
nc=  0 lived=  0 killed=  13  internal/cli/ask.go
nc=  0 lived=  0 killed=  13  internal/drivers/postgres/query.go
nc= 14 lived=  3 killed=  31  internal/store/query_history.go           [GATED]
nc=  0 lived=  0 killed=  14  internal/cli/pipe.go
nc= 18 lived=  0 killed=   0  internal/ai/context/schema.go
nc= 18 lived=  0 killed=   0  internal/ui/components/explorerpreview/tabbar.go
nc= 22 lived=  0 killed=   0  internal/ai/nl2sql/provider.go
nc= 25 lived=  8 killed= 125  internal/app/txn.go                       [GATED]
nc= 26 lived=  0 killed=   0  internal/ai/session/logger.go
nc= 27 lived= 30 killed=  34  internal/ui/components/editor/highlight.go
nc= 31 lived= 12 killed=   5  internal/ui/components/grid/pager.go
nc=  0 lived=  0 killed=  32  internal/cli/commands.go
nc= 32 lived= 76 killed= 103  internal/ui/components/explorerpreview/ere.go
nc=  0 lived=  1 killed=  33  internal/drivers/postgres/schema.go
nc= 39 lived=  0 killed=   0  internal/ui/components/palette/fuzzy.go
nc= 39 lived=  0 killed=   0  internal/ui/components/picker/picker.go
nc= 41 lived=  0 killed=   0  internal/ui/components/grid/cell.go
nc= 43 lived=  6 killed=  22  internal/ui/components/ask/ask.go
nc= 63 lived=  0 killed=   0  internal/ui/components/explorerpreview/preview.go
nc= 65 lived=  0 killed=   0  internal/ui/components/explorer/explorer.go
nc= 65 lived=  0 killed=   0  internal/ui/components/gridsidebarpreview/preview.go
nc= 70 lived= 17 killed=   4  internal/ui/components/explorer/tree.go
nc= 76 lived= 25 killed=  33  internal/ui/components/editor/sql.go
nc= 76 lived= 44 killed=  93  internal/ui/components/editor/autocomplete.go
nc= 81 lived=  0 killed=   0  internal/ui/components/palette/palette.go
nc= 83 lived= 11 killed=  27  internal/ai/nl2sql/compatible.go
nc=116 lived=  0 killed=   0  internal/ui/components/querybrowser/querybrowser.go
nc=171 lived=  0 killed=   0  internal/ui/components/grid/where_filter.go
nc=281 lived= 31 killed=  13  internal/ui/components/gridpreview/preview.go
nc=350 lived= 63 killed= 124  internal/app/app.go
nc=414 lived=117 killed=  85  internal/ui/components/grid/table.go
```

## Queue — ordered easiest first

### Tier 1 — free admissions, no tests needed

Zero survivors, zero uncovered. The only work is narrowing `MUTATE_EXCLUDE`,
which is a regexp alternation, so a file is opted IN by naming its siblings OUT.
`scripts/check_mutate_scope.py` now enforces both directions and fails on any
new Go file nobody classified.

- [x] `internal/app/txn.go` — done in `ad614e3`
- [x] `internal/ui/components/explorer/node.go` (11 killed)
- [x] `internal/ui/components/palette/commands.go` (4 killed)
- [x] `internal/app/router.go` (4 killed)
- [x] `internal/ai/nl2sql/{anthropic,openai,deepseek,qwen}.go` (35 killed) — was
      Tier 2; all four went from 8-9 uncovered to zero in one table-driven file
- [x] `internal/ui/components/grid/mouse.go` (41 killed, 4 survivors) — was
      Tier 2; 2 uncovered → 0, 9 lived → 4
- [x] `internal/ui/components/grid/header.go` (34 killed, 3 survivors) — was
      Tier 2; 6 uncovered → 0, 13 lived → 3
- [x] `internal/ui/components/grid/export_picker.go` (9 killed, **0 survivors**) —
      was Tier 2; 9 uncovered → 0, no allowlist entry needed
- [x] `internal/ui/components/palette/fuzzy.go` (35 killed, 4 survivors) — was
      Tier 3; 39 uncovered → 0
- [x] `internal/ui/components/grid/pager.go` (43 killed, 5 survivors) — was
      Tier 3; 31 uncovered → 0, 12 lived → 5. The 5 are the densest cluster of
      no-op-at-equality clamps found so far; see Group A in the allowlist
- [x] `internal/cli/{ask,commands,context,pipe,root}.go` (68 killed, 0 survivors) —
      was Tier 2/3, and needed a production refactor to be testable at all
- [x] `internal/drivers/postgres/{query,schema}.go` (46 killed, 1 survivor) — no
      test file existed. Driven through `internal/testsupport/pgxfake`
- [x] `internal/ai/context/schema.go` (18 killed, **0 survivors**) — was Tier 3;
      free once the driver took an interface, so it came last and cost nothing
- [x] `internal/config` NOT COVERED: 21 → 0 uncovered, 12 mutants killed

### pager.go: the pattern to expect in the rest of the gate

Five of `pager.go`'s six survivors were the *same* shape: a clamp whose body
assigns the value already there, so the boundary mutant fires at equality and does
nothing. `if pages < 1 { pages = 1 }`, `if n < 1 { n = 1 }`, `if n > total`,
`if endRow > total` — twice, once per renderer. Expect roughly one of these per
clamp, and expect them to be genuinely unkillable rather than undertested. Group A
in the allowlist is the place they go.

**The sixth was a real miss and it is the lesson.** `if p.pendingCount > 0` in
`RenderFooter` had a `>= 0` variant, which at zero appends a literal `0 pending`.
I *had* written a test for that, but on `Render()` — the other renderer. Two
renderers of the same numbers sit next to each other, and I asserted the absence
of a segment in one of them. **When a value feeds two render paths, assert on
both.** Fixing that took the file from 5 survivors to 5 survivors, of which 5
became the no-op clamps: 42 → 43 killed.

Two assertions I wrote from a guess and the code corrected, both worth not
guessing next time:

- With a non-positive `pageSize` there is exactly one page, so `NextPage()`
  correctly returns **false**. I had asserted true, reasoning "it moved". The
  return value is "did it move", not "is there a next page".
- `Offset()` is `(page-1) * pageSize` with no clamping, so asking for page 11 of
  ten gives the offset of page 10, not a wrong answer I had to allow for. The
  clamp lives in `GoToPage`, and `GoToPage(99)` landing on the last page is the
  property that matters, because F1..F9 are wired to it unconditionally.

### Two contracts worth keeping

**`router_test.go`** pins one thing the file does not state: only the explorer
and the grid are in the focus cycle. The editor and the two preview panes have a
`String()` but no `FocusByName` case, so the editor is not reachable by name at
all. `TestFocusByName_ReachableNamesRoundTrip` enumerates the panes and fails if
that set ever changes, so adding a name later is a deliberate edit.

**`provider_test.go`** covers the four NL providers with one table, because they
differ in three ways that each needed pinning separately: the endpoint, the auth
header (Anthropic uses `x-api-key`, the rest use `Authorization: Bearer`), and
the response shape (Anthropic nests the text at `content[].text`, the rest at
`choices[0].message.content`). It also pins that Anthropic sends the system
prompt in a dedicated `system` field rather than as a message, and that Qwen
sends no `max_tokens` at all.

The four call `http.DefaultClient` against hardcoded https URLs, so
`httptest.Server` cannot reach them. The seam is a custom `http.RoundTripper`
swapped onto `http.DefaultTransport` for the duration of the test, which
exercises the real request path, headers and body without the network.

### mouse.go: a surprising interaction worth knowing

`visibleColumnAtX` correctly refuses an x that lands on a column which does not
fit. But `HandleClick` then calls `syncScroll`, and `syncScroll` **clamps the
cursor into range and brings the window to it**. So the observable result is not
"nothing happened": clicking right of the visible area moves the cursor to a
valid column and `scrollCol` follows. `TestGrid_HandleClick_BeyondTheVisibleAreaScrollsTheCursor`
pins that. Skipping `syncScroll` when no column was hit would be the other
behaviour; which is right is a product decision, recorded rather than assumed.

One test trap worth repeating: a sentinel `cursorCol` of 99 does not work. It
has to be **in range**, because `syncScroll` clamps `cursorCol` to
`[0, len(widths)-1]` on every click. An out-of-range sentinel is silently
rewritten, so the test passes for the wrong reason. `sentinelCol(n)` returns
`n-1`.

### A FOURTH blind spot in the harness, and it was the biggest one

A NOT COVERED mutant is one gremlins chose not to run. The gate printed the count
in a CI comment and **failed on nothing**. So an untested branch could sit in the
gate indefinitely, and unlike a survivor — which shows up as a red build — this
was completely silent. Same family as the TIMED OUT hazard, one level worse,
because a timeout is at least absent from the report where you can count it.

`scripts/check_mutate_nc.py` and `.mutation-notcovered` now make it a hard
failure in both directions, and the Calibration job is not the only gate: the
Mutation job runs it too. A new uncovered mutant is a red build; an entry that
stops being uncovered is also a red build, so a suppressed gap cannot quietly
become a real one.

**And gremlins was wrong about a quarter of them.** `go tool cover` reported
exactly **3** uncovered blocks in `internal/app/txn.go`; gremlins reported **25**
uncovered mutants there, and all 25 sat on `case` lines of a `switch { ... }`.
Go's cover tool attributes a case expression to the case BODY, so the case line has
no block and gremlins' per-line lookup finds nothing to attribute to. Hand
applying `txn.go:384:26` and running the suite **KILLS it** — gremlins never ran
it. Twenty-two of the 58 were this artefact.

Closing them anyway found two real gaps the artefact had been hiding:
`E'` and `/*` as two-character inputs, where the bound that detects the opener is
one byte short. Both are now pinned.

**My earlier claim that txn.go was "22 survivors → 8" was measured with a broken
`jq` and was wrong about the uncovered half.** The killed count was right; 25
mutants were never being run and I did not notice. This is the third time a
measurement command in this project has failed silently, which is why the checks
are now scripts that exit non-zero rather than shell one-liners.

### A third dead branch, and a real bug in the project scanner

`internal/ui/bordered/bordered.go:232` has an "ensure at least one line" fallback
whose guard is `len(result) == 0`. The loop above it iterates
`strings.Split(content, "\n")`, and `strings.Split` **always** returns at least one
element — `[""]` for `""`. Inert. Pinned by a test that states both the premise
and the consequence.

`internal/config/scanner.go` had a real one. The hidden-directory guard in
`scanWithWalkDir` tested `d.Name()`, and WalkDir visits the ROOT first — so any
root whose own last element starts with a dot was skipped entirely and the walk
found nothing. `NewScanner("/home/x/.config/dev").Scan()` returns no projects. The
guard now tests `path != root`. Found by the fixture `TestScanWithWalkDir_AHiddenRootIsStillWalked`.

`internal/config/scanner.go:49`, the sort's `Name < Name` tiebreak, is dead for a
structural reason: dedup runs first, and two survivors can only share a `Path` if
they came from the same directory, whose single `.dbx.toml` gives them the same
`Name`. Pinned as an invariant rather than a fixture, so a change that broke it
would fail the test instead of silently depending on a comparator that can never
return true.

### The probe corrupted my working tree, and how I caught it

A probe script whose arguments were in the wrong order never applied its
mutations, and one of its runs ended with `git checkout -- <file>` on a file with
uncommitted work. That reverted a bug fix, and a later run left `if err == nil`
sitting in `scanWithFD` as if it were production code. Two symptoms: `fd` was
installed and worked from a shell, but `scanWithFD` returned nothing, and the test
I had just written to prove it worked was failing for no visible reason.

`/tmp/opencode/verify_clean.sh` now prints every non-test `.go` diff after a probe
run. **Never let a probe script touch a file with uncommitted work**, and take the
backup before the FIRST probe of a session, not per-run.

### A second defect found, not fixed: the camelCase bonus in the palette is dead

`fuzzyMatch` (`internal/ui/components/palette/fuzzy.go:31`) scores a match on an
upper-case letter +3, so typing `cc` should rank `CamelCase` above `concat`. The
check is `if t[ti] >= 'A' && t[ti] <= 'Z'`, but line 11 is
`t := strings.ToLower(target)` and that is the only thing `t` is ever bound to.
**The lower-case bytes `0x41`-`0x5A` cannot occur in a lower-cased string, so the
`score += 3` is unreachable.** All three mutants on that line are provably
equivalent, and the allowlist says so.

The score it would have changed is load-bearing for the ranking, so the inert
branch is pinned rather than left implicit:
`TestFuzzyMatch_CamelCaseBonusIsUnreachable` asserts `c`/`camelCase` = 28
(10 + 8 + 10, no bonus) and that `XMLHttp` and `xmlhttp` score identically. The
fix would be to score against the un-lowercased target and keep `q`/`t` lower-cased
for the scan, which is a production change and was left alone.

Also pinned, because it surprised me while writing the test: **the search is not
accent-insensitive.** There is no Unicode normalisation anywhere in the path, so
`cafe` does not find `café`.

### A defect found, not fixed

`cleanSQL` strips the literal ` ```sql ` prefix and the bare ` ``` ` prefix, both
lowercase, then the trailing fence. A model that emits ` ```SQL ` in caps gets
the bare-fence strip, which removes the backticks and leaves the word `SQL`
glued to the front of the statement. The result parses as a column alias, so the
app reports a plausible "missing relation" error instead of "the model sent a
fence". `TestCleanSQL_UppercaseLanguageTagLeaksIntoTheSQL` pins the actual
behaviour with a note; the fix is a case-insensitive tag compare in
`internal/ai/nl2sql/anthropic.go`, which is a production change and was left
alone.

### Tier 2 — under 10 uncovered

DONE. Tier 2 is empty.

- [x] `internal/ui/components/grid/header.go` — done, 34 killed / 3 survivors
- [x] `internal/ui/components/grid/export_picker.go` — done, 9 killed / 0 survivors
- [x] `internal/cli/{ask,commands,context,pipe,root}.go` — done, 68 killed /
      **0 survivors**. Needed a production refactor, not just tests. See above.

### Tier 3 — 10-45 uncovered

- [ ] `internal/ai/nl2sql/provider.go` — 22
- [ ] `internal/ai/session/logger.go` — 26
- [ ] `internal/ui/components/explorerpreview/tabbar.go` — 18
- [ ] `internal/ui/components/editor/highlight.go` — 27 uncovered, 30 lived
- [ ] `internal/ui/components/explorerpreview/ere.go` — 32 uncovered, 76 lived
- [x] `internal/ui/components/palette/fuzzy.go` — done, 35 killed / 4 survivors
- [x] `internal/ui/components/grid/pager.go` — done, 43 killed / 5 survivors
- [ ] `internal/ui/components/{picker/picker,grid/cell}.go` — 39/41
- [ ] `internal/ui/components/ask/ask.go` — 43 uncovered, 6 lived

### Tier 4 — 60+ uncovered, high value, high cost

- [ ] `internal/ui/components/explorerpreview/preview.go` — 63
- [ ] `internal/ui/components/explorer/explorer.go` — 65
- [ ] `internal/ui/components/gridsidebarpreview/preview.go` — 65
- [ ] `internal/ui/components/explorer/tree.go` — 70 uncovered, 17 lived
- [ ] `internal/ui/components/editor/sql.go` — 76 uncovered, 25 lived
- [ ] `internal/ui/components/editor/autocomplete.go` — 76 uncovered, 44 lived
- [ ] `internal/ui/components/palette/palette.go` — 81
- [ ] `internal/ai/nl2sql/compatible.go` — 83 uncovered, 11 lived
- [ ] `internal/ui/components/querybrowser/querybrowser.go` — 116
- [ ] `internal/ui/components/grid/where_filter.go` — 171
- [ ] `internal/ui/components/gridpreview/preview.go` — 281 uncovered, 31 lived
- [ ] `internal/app/app.go` — 350 uncovered, 63 lived. The single largest gap.
- [ ] `internal/ui/components/grid/table.go` — 414 uncovered, 117 lived. Largest.

## Before trusting any measurement here

**A TIMED OUT mutant is absent from `report.json`, so it is indistinguishable
from a killed one.** This is the most dangerous property of the tool: it makes
the gate pass for the wrong reason, and it makes an allowlist entry look
re-verified when it was never evaluated.

Two independent causes, both fixed in the `mutate` target:

- `--timeout-coefficient 20`, not the default 3. At 3 this scope produced 136
  timeouts on `txn.go` alone, which meant all 8 of its allowlisted survivors were
  skipped entirely.
- `MUTATE_DSN`, so `internal/app` reuses one PostgreSQL instead of starting a
  testcontainer per run. Without it each of the hundreds of runs pays ~4s of
  container startup. The package takes 4.3s without a DSN and 0.5s with one.

Current state after both: **9 timeouts out of 563 mutants**, down from 142. Check
with `jq '[.files[].mutations[]|select(.status=="TIMED OUT")]|length' report.json`
after any change to the harness. A jump means results are untrustworthy even when
the gate still passes.

```bash
docker run -d --name dbx-mut-pg -e POSTGRES_PASSWORD=dbx -e POSTGRES_USER=dbx \
  -e POSTGRES_DB=dbx_test -p 55432:5432 postgres:16-alpine
make mutate MUTATE_DSN='postgres://dbx:dbx@127.0.0.1:55432/dbx_test?sslmode=disable'
```

## Notes for whoever picks this up

- **gremlins does not report the applied replacement.** `report.json` carries
  only `{type,status,line,column}`. Learn a mutant by hand-applying it.
- **A probe is only trustworthy once the suite is green.** A probe run while
  `go test` fails for any reason reports false KILLs. Check the suite first.
- **Locate the span by searching the line for the literal operator**, never by
  the reported column. gremlins columns have been off by one, and a 1-char span
  on a 2-char operator silently applies a no-op that reads as SURVIVED.
- **A probe that disagrees with `make mutate` is the probe that is wrong.** Twice
  now: once because the span was off by a column and produced invalid Go, once
  because I probed line 71 while the surviving mutant was on line 68, and
  concluded from it that a killable mutant was equivalent. **Never write an
  allowlist entry from a probe alone** — re-run the real thing first.
- **gremlins is nondeterministic run to run.** Timed-out mutants are absent from
  `report.json`, so survivors appear and disappear. Re-run before concluding —
  but note that a deterministic 136 timeouts in a row is NOT variance, it is a
  harness problem, and it was hiding eight allowlist entries from verification.
- **NOT COVERED is attributed by line, Go's cover tool by block.** A multi-line
  `if`/`case` condition is attributed to the PRECEDING block, so a line can be
  reported NOT COVERED while the block containing its code has count > 0.
  `txn.go:110` was exactly this. Confirm by hand-applying before writing a test.
- **`ARITHMETIC_BASE` on a relational subexpression retargets the addend**, not
  the operator, and the reported column points at the addend. Replace the whole
  bound (`i+1 < len` -> `i+2 < len`), not a bare `2`.
- **`CONDITIONALS_BOUNDARY` relaxes toward "true on equality"**: `<`->`<=`,
  `<=`->`<`, `>`->`>=`, `>=`->`>`. Guessing the opposite gives an already-killed mutant.
