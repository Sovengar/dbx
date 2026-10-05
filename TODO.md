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
nc=  0 lived= 69 killed= 525  internal/ui/components/grid/table.go  <- DONE
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
- [x] `internal/ui/components/editor/highlight.go` (91 killed, 0 survivors, 9
      unkillable timeouts) — was Tier 3; 27 uncovered → 0, 30 lived → 0
- [x] `internal/ui/components/grid/cell.go` (39 killed, 2 survivors) — was Tier 3;
      41 uncovered → 0, 0 lived → 2
- [x] `internal/config` NOT COVERED: 21 → 0 uncovered, 12 mutants killed

### table.go: ONE FIXTURE BUG EXPLAINED 128 OF 414 UNCOVERED MUTANTS

Every pre-existing fixture in this package built its grid as `New(styles, 0, nil)`,
and both arguments are wrong in ways that hide code:

- **pageSize 0** makes `Pager.Limit()` return 0, so `renderRecordsView` computes
  `endRow == 0` and the row loop `for i := startRow; i < endRow` never runs. Not one row
  was ever rendered by the whole suite — which is why the four largest row-renderer lines
  were uncovered.
- **a nil `keybinds` Resolver** panics the moment `handleKey` resolves a key, so nothing
  that goes through the key path could be tested at all.

A third one, found later and worth its own note: `Update` drops everything for an
unfocused grid while `HandleAction` deliberately skips that guard. That is why the
pre-existing action tests could get away with an unfocused fixture and the contract
tests all call `Focus()`.

### table.go: TWO REAL BUGS, both found by the gate

**Backspace removed one BYTE, not one character.** `handleEditKey`'s `backspace` and
`delete` cases sliced `editValue` at `editCursor`, which is a byte offset — that is what
makes insertion a two-slice concatenation. On an accented letter or an emoji that leaves
invalid UTF-8 in the value and the cursor inside the broken sequence, and that value is
the cell content the user is about to commit. The same defect was found and fixed in the
ASK pane's editor earlier in this project; the shape is identical. Fixed here with
`runeAt`/`runeAtBefore` helpers and the whole editor made rune-aware, cursor movement
included.

**`expandEditCol` remembered the wrong width.** It saved the column's CURRENT width into
`editOrigWidth` every time it ran, and it runs once per typed character — so the
"original" tracked the previous expansion. Typing 20 characters into a 16-cell column left
it at 24 for the rest of the session. The trigger is a COUNT: one keystroke works, two do
not, so a test that types once passes against the broken code.

### table.go: TWO DEAD RULES DELETED, and one of them explains four mutants

- `syncScroll` clamped `scrollCol` to `len(widths)-1`, and the cursor clamp two lines above
  had already bounded `cursorCol` to `len-1` while the rule below pulls the window back to
  the cursor. Any value the clamp could write was overwritten.
- `jumpToColumn` computed a scroll start two columns left of the match, but
  `syncScroll`'s expand-left loop refills from the left for as long as the cursor still
  fits, so the final window is a function of the cursor and the widths alone. Five mutants
  gone, and one place to keep in step instead of two.

### table.go: FIVE FIXTURES THAT PROVED NOTHING, and what each was hiding

The recurring mistake is a fixture chosen for readability rather than for being at a
boundary or at a non-zero offset.

- **A page offset of zero makes three of four terms invisible.** `offset + scrollRow`,
  `offset - scrollRow` and `scrollRow` alone all agree when `offset == 0`, and so do
  `cursorCol - scrollCol` and `cursorCol + scrollCol` when `scrollCol == 0`. Four mutants
  on the yank's row index, the render's first row and the selected cell's column survived
  until one fixture set page offset, row scroll and column scroll to three DIFFERENT
  non-zero numbers at the same time.
- **A fixture that never restores a change proves nothing about restoring.** The discard
  and undo fixtures set a pending update but left the data holding the ORIGINAL value, so
  "restored to the original" passed for a restore that did nothing. That is why the guard
  for row ZERO survived: the only row whose index a `>= 0` test could mistake was the one
  the fixture never actually changed.
- **An assertion on the end state misses a boundary that fires earlier.** Typing a value
  that exactly fills a column does not widen it, and neither does a comparison that fires
  one keystroke early — the column ends up the same width either way. What the user sees is
  it nudging sideways as they type, so the width is now checked after EVERY character.
- **One insert cannot tell `+ n` from `- n`.** `startInsertRow`'s edit row is
  `len(rows) + pendingRow`, and with a single insert `pendingRow` is zero, so one minus zero
  is one. Two inserts can, and the test presses the key repeatedly.
- **A row block slice that runs to the end of the output measures the wrong thing.** It also
  holds the trailing blanks and the mode indicator, so a length check on it is not a check
  on the row budget.

### table.go: A WIDTH ASSERTION IS NOT A CONTENT ASSERTION, AGAIN

The same lesson as `ere.go` and `picker.go`, in a third place. There, a width matrix could
not see a mutation that replaced a character with another character. Here the row renderer
draws a highlight on ONE cell of a row, and the usual fixture shape — "the output changed"
— passes a renderer that lit the wrong column. The observation that kills it reads each
cell's colours off the escape sequences: `cells` tokenises the line and accumulates the
SGR parameters in force over each run, and `inStyle` picks out the cells painted in one
style's colours. Those colours are derived from the THEME at test time rather than written
as literals, so a theme change cannot quietly turn the assertions into ones that compare
the wrong thing and pass.

### table.go: A PANICKING MUTANT SCORES AS A SURVIVOR, so a bounds check is a permanent allowlist entry

Twenty-nine survivors are off-by-ones on a slice bound: `i < len(S)` to `i <= len(S)`,
`i >= len(S)` to `i > len(S)`. The only value that tells them apart is `i == len(S)`, and
there the original declines the index while the mutant performs it, so the mutant reads one
element past the end and panics. A panicking mutant is scored as LIVED, so these are real
robustness regressions the gate structurally cannot see. They are allowlisted with that
stated plainly rather than hidden, because an allowlist entry that does not say what it is
saying is worse than no entry.

### table.go: FOUR KEYS STILL OPEN

Four survivor keys have no explanation yet and are NOT allowlisted: the `isEditing`
expression in `renderRecordsView` and the `activeCol` in its multi-selected branch. Both
need a fixture where the page offset, the row scroll and the column scroll are all
non-zero AND the rendered row is one that takes that branch, and the obvious candidate
does not reach them. They are the honest remainder of this file.

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
- [ ] `internal/ui/components/explorerpreview/ere.go` — 32 uncovered, 76 lived
- [x] `internal/ui/components/palette/fuzzy.go` — done, 35 killed / 4 survivors
- [x] `internal/ui/components/grid/pager.go` — done, 43 killed / 5 survivors
- [x] `internal/ui/components/picker/picker.go` (37 killed, 2 survivors) — 39
      uncovered → 0, 0 lived → 2; one real rendering bug fixed
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
- [x] `internal/ui/components/grid/table.go` — 414 uncov → 0, 117 lived → 69; 525 killed.
      Two real bugs fixed, two dead-code deletions, four survivor keys still open.

## Round 2 — the three remaining giants

Ordered easiest first, same rule as round 1: measure, kill, admit, commit. Each entry
is updated in place with its measured numbers and whatever the file taught, so this
section is the running record rather than a to-do list.

- [ ] `internal/ui/components/grid/where_filter.go` — 171 uncovered. Smallest of the
      three and a self-contained widget: no I/O, no app wiring, everything reachable
      from the in-package test file. Start here.
- [ ] `internal/ui/components/gridpreview/preview.go` — 281 uncovered. One test file
      against 1253 lines, so almost all of it is untested; the pattern that killed
      table.go's rows applies here.
- [ ] `internal/app/app.go` — 350 uncovered, 63 lived. The largest gap in the repo and
      the hardest: it is the lifecycle and the router, so its fixtures are teatest
      programs rather than function calls. Last, deliberately.

Rules carried over from round 1, because every one of them cost time to learn:

- **A fixture with a zero value where a real one is needed is worse than no fixture.**
  `New(styles, 0, nil)` hid 128 of table.go's 414 mutants on its own.
- **A fixture that stays at zero offsets cannot see arithmetic.** `offset + n`,
  `offset - n` and `n` agree when `offset == 0`. Set every offset non-zero and
  different.
- **A differential assertion cannot say WHICH thing changed.** Read the change off the
  output — the cell's colours, the row's index — not off "it differs from before".
- **An assertion on the end state misses a boundary that fires earlier.** Check after
  every step, not once at the end.
- **A skip that tolerates the symptom of a defect is a hole.** A `t.Skip` for "this
  case has nothing to assert" hid a live mutant in ere.go.
- **Write the expectation from the contract.** Guessed expectations cost six cycles in
  cell.go, six in picker.go and three in ask.go.
- **Read the source before asserting.** More allowlist entries were settled by reading
  the function than by probing it.

## Round 3 — bajar los supervivientes, que es el UNICO camino a una cifra alta

Objetivo pedido: 98% de eficacia del gate. Medido y **reescrito a mitad de camino**,
porque la aritmética lo hace inalcanzable de otra forma.

### La aritmética, y por qué admitir ficheros no ayuda

    eficacia = K / (K+L)      98%  <=>  49*L <= K

Con K=1776 y L=180, los 180 supervivientes que ya existen exigen `K >= 8820`. Un
fichero nuevo aporta `x` muertos y `y` vivos: `49*(180+y) <= 1776 + x`, o sea
`49y <= x - 7044`. Como `y = x*(1-e)/e`, el margen es **negativo para todo `e < 98%`
y cero justo en `e = 98%`**. Añadir ficheros al gate no sube la eficacia; la deja
igual o peor. El único palanca es reducir L.

- [x] `where_filter.go` — 53 NOT COVERED -> 0. 136 muertos, 35 supervivientes. Los
      tests son `where_filter_contract_test.go`, en el paquete, con la lista de
      sugerencias y el popup leídos directamente. Las sugerencias son variables de
      PAQUETE compartidas y NO se copian al entregarse, asi que un test que las
      mutara contaminaria el resto del paquete: nada las toca.
- [x] `grid/table.go` — 20 de 21 guardas de slice matadas (8488e78).

### El patron que lo hizo posible: un panic no es un rojo

Un panic dentro de un test aborta el binario entero. La herramienta no tiene
resultado por test para ese mutante y lo puntua SUPERVIVIENTE — por eso 29 de estas
guardas estaban escritas como "no se pueden matar desde el test". Era cierto de los
tests que existian. `mustNotPanic` (en `table_contract_test.go`) recupera el panic y
lo reporta por el paquete `testing`, asi que el crash pasa a ser un test rojo normal.

Verificado a mano en 21 sitios: 20 muertos. El vivo (`navigateFK:933`) es equivalente
de verdad: tras la guardia hay un `g.keyIcons[g.cursorCol]` que es **map**, luego un
cursor pasado la ultima columna se rechaza igual en los dos.

### La ficha que faltaba: las ENTRADAS OBSOLETAS

`toast.go` tiene un test que asserta AMBOS extremos de los nueve rangos CJK con las
runas exactas. Al aplicar los 6 rangos a mano, 4 ya estaban muertos y solo 2
sobrevivian de verdad (`210` y `216`, cubiertos ambos por las tablas `unicode.Hangul`
y `unicode.Han`). Las otras 4 entradas del allowlist eran **obsoletas**: nadie ha
re-ejecutado el gate para limpiarlas tras escribir el test.

Una entrada obsoleta es peor que no tener entrada: declara equivalente un riesgo que
en realidad esta cubierto, y queda con respuesta aparente. Por eso el barrido
manual de las 152 `CONDITIONALS_BOUNDARY` es la accion de mayor rendimiento que queda:
cada entrada que muere es un muerto gratis.

Reglas del barrido (`/tmp/opencode/sweep_allowlist.py`):

- **Una linea puede generar dos mutantes** (`i >= 0 && i < len(s)`): uno por `<` y otro
  por `>=`. La entrada del allowlist es `fichero:linea`, asi que para borrar la entrada
  tienen que morir LOS DOS.
- **Una cobertura por tabla de unicode hace equivalente el mutante.** Si la runa del
  limite la cubre `unicode.Hangul`/`Han`/etc., el mutante del borde no es observable
  por ningun test. Eso es lo que pasa en `toast.go:210` y `:216`.
- **La ficha de tipo tabla de scripts ya existe** en `toast_test.go` con un flag
  `tailIsTableWide`; ese flag es exactamente la nocion de "otro disjunto ya cubre
  este punto". Merece la pena repetirlo donde haya disjunciones.
- La ficha por LINEA no distingue dos mutantes de la misma linea: al re-verificar hay
  que intentar AMBOS.

### Estado: allowlist 172 -> 133

- 34 **entradas obsoletas** borradas (los tests ya las mataban). Verificadas 3/3.
- 3 borradas por **codigo muerto** eliminado (`fuzzy.go:31`, el bonus camelCase).
- 4 matadas con tests nuevos: `table.go` (20 guardas), `ere.go:290`, `ere.go:306`,
  `modal.go:86`, `bordered.go:251`.
- 2 **restauradas** por falso positivo del barredor: `modal.go:71`, `modal.go:206`.

### Lo que la aritmética del gate NO permite

De nada sirve admitir mas ficheros para subir la cifra (ver arriba). Y de los 133 que
quedan, la mayoria son **equivalentes por construccion** y ningun test los mata:

- ~50 clamps `if x < N { x = N }` donde el cuerpo reescribe el valor que ya hay
- caps cuyo umbral es su propia constante: `len(x) > Max` con `len(x) == Max` hace
  `x[:Max]` (= todos) y `len - Max` (= 0) — **identico**. `ere.go:201/208` y
  `query_history.go:76` son de esta forma, y mi primera lectura de ellos estaba mal
- comparaciones con una guardia encima, o el segundo operando de un `&&` (cortocircuito)
- `toast.go:210`: la runa del limite la cubre `unicode.Hangul`

Techo real alrededor de **92-93%**, no 94.7%: la estimacion de "~80 matables" era
optimista. Los 34 obsoletas mas los ~10 nuevos dan ~44, no 80.

### Las tres trampas que costaron mas tiempo (y aparecen en este proyecto por 3a vez)

1. **Un timeout parece un kill.** Con la CPU competida por otro repo, un test no
   relacionado se paso de tiempo, el codigo de salida fue distinto de cero, y el probe
   lo conto como muerte. `header.go:101` fue dado por muerto dos veces. Ahora cada
   mutacion se aplica 3 veces y las 3 tienen que ser rojas, y ninguna puede ser timeout.

2. **Una linea genera hasta DOS mutantes** y la clave es `fichero:linea`. "Alguna
   variante de esta linea muere" NO es "esta linea no tiene supervivientes" — es lo que
   casi borro 4 entradas correctas y lo que devolvio 2 incorrectas.

3. **Una prueba construida desde el estado que NEUTRALIZA el mutante.** `modal.go:86`
   estaba "justificado" porque en `scroll == 0` el clamp esconde la diferencia — y 0 es
   justo el estado por defecto que produce un fixture nuevo. Es la misma trampa que un
   fixture con offset cero.

Y una cuarta, de agrupacion:

4. **Agrupar por FORMA en vez de por PRUEBA.** `ere.go:290` y `:306` estaban bajo el
   grupo de los clamps junto a diez pruebas sound; ninguno era clamp. Estar rodeado de
   pruebas buenas es exactamente por que nadie miro las dos que no lo eran.

### LA MEDICION FINAL (corrida `make mutate`, la autoridad)

    killed 1942   lived 185   eficacia 91.17%   cobertura de mutadores 98.52%
    NOT COVERED 21 (conjunto exacto)   TIMED OUT 14 (bajo techo)
    allowlist 185 entradas = exactamente el conjunto de supervivientes
    43 ficheros gateados, 29 excluidos, 0 sin clasificar

O sea: **90.80% -> 91.17%**, no al 94.7% que se estimo. Y el estimacion era
optimista por el motivo que sigue.

### EL ERROR DE FONDO: MI PROBE NO REPRODUCE LA ATRIBUCION DEL GATE

El gate ejecuta, **por mutante, los tests que el mismo atribuye a esa linea** — no el
paquete entero. Eso convierte "la suite se pone roja" en evidencia NECESARIA pero NO
SUFICIENTE. Un probe que solo mira el exit code esta adivinando, y se equivoco en las
dos direcciones:

- **No sabia que comparacion murio.** Una linea con `&&` genera un mutante por operando
  y la clave es `fichero:linea`, asi que UNA entrada cubre los dos. El probe se paraba en
  el primer operador que dejaba la suite roja y declaraba la linea muerta. Eso borro 13
  entradas correctas. `table.go:343` es el caso claro:
  `if available > 0 && len(g.widths) > 0` tiene dos operandos que parecen iguales y no lo
  son — uno protege una division, el otro un slice.
- **Un timeout parece un kill.** Con la CPU competida por otro repo, un test no
  relacionado se paso de tiempo y el exit code distinto de cero se conto como muerte.
  `modal.go:71` y `:206` se borraron y volvio a ponerlas.

Regla: un probe que no reproduzca la atribucion del gate no puede retirar una entrada.
Solo puede sugerir las que el gate ya marco como muertas.

(esa linea estaba corrupta en `main` — bytes Cruzados de una escritura interrumpida. No
la habia leido nadie porque el gate no la parsea: es prosa, no una entrada. Los ficheros
que SI parsea el gate hay que revisarlos con `go vet` despues de cada escritura
interrumpida, que es donde el truncamiento se detecta.)

### where_filter.go: los 35 supervivientes, agrupados por POR QUE

RESUELTO en la ronda 4: 10 entradas retiradas, 4 reescritas como equivalencia. El
agrupamiento de abajo estaba mal en 5 de sus 9 lineas, y el error no fue de criterio sino
de lectura: se aceptaron pruebas que describian un test que no existe, o un test que no
llega donde dice. Queda el agrupamiento ORIGINAL y su correccion, porque el modo de fallar
es lo reutilizable.

- **Conjunto redundante** (145, 156, 163, 170): `wf.showPopup && len(wf.suggestions) > 0`.
  Hubo **dos** pruebas y las dos estaban mal, en direcciones opuestas, y ninguna leia mal
  el codigo: las dos razonaban sobre el estado equivocado. Hay tres estados, y solo uno
  separa las dos formas:

  | estado | original | mutante | distingue |
  |---|---|---|---|
  | `showPopup=true`, lista>0 | true | true | no |
  | `showPopup=false`, lista>0 | false | false | no ← **alcanzable** |
  | `showPopup=true`, lista vacia | false | true | **si** ← inalcanzable |

  - La **primera** prueba: "`showPopup` se asigna en un unico sitio como
    `len(suggestions) > 0`, luego el segundo operando no decide nada. Es codigo muerto."
    La conclusion era correcta pero la frase "se asigna en un unico sitio" es falsa (son
    tres: `:97` `Hide`, `:130` escape, `:283` `updateSuggestions`), y por ahi se coló la
    idea de borrarlo.
  - La **segunda** prueba (esta, al principio de la ronda 4): "`Hide()` lo limpia sin
    tocar las sugerencias" — osea el estado `showPopup == false` CON lista poblada — "y
    ahi el mutante acepta una sugerencia que el usuario ya habia descartado". **FALSA.**
    Ahi las dos formas dan `false`. El gate lo dijo sin ambiguedad: con esa fixture, los
    cuatro volvieron `LIVED`.

  El fallo de las dos es el mismo y hay que nombrarlo: la segunda
  chose el estado **alcanzable** porque es el que un usuario puede=live, y un guard que no
  hace nada donde se llega parece el guard que importa. Es justo al reves: el conjunct
  decide donde nada puede llegar. `updateSuggestions` pone `showPopup` desde
  `len(suggestions) > 0` **despues** de que `filterSuggestions` asigne la lista, y las
  otras dos asignaciones solo ponen `false` — luego `showPopup == true` implica lista no
  vacia, siempre, y el mutante si es equivalente sobre los estados alcanzables.

  Lo que si tiene arreglo: el conjunct es redundante en produccion y ** indefendido en la
  suite**, y eso se arregla. Los tests de este paquete ya tocan el struct y asignan
  `showPopup` directamente, asi que el tercer estado es construible desde un test. La
  fixture lo monta y fija lo que decide el guard: tab y las flechas caen al grid, enter
  aplica la clausula. 3/3 rojos en las cuatro lineas.

  Y el conjunct **se queda**. Borrarlo, como exigia la primera prueba, habria hecho que
  "arriba" sobre un popup vacio se declarara atendido y no moviera nada.

- **Familia clamp** (268, 355, 361, 535, 549, 556, 564, 582, 587): el cuerpo reescribe el
  valor que ya tiene. En 355 el empate es IMPOSIBLE (hace falta que dos palabras clave
  empiecen una runa de distancia, lo que su propio texto prohibe) y en 361 `bestPos`
  nunca es 0 porque es `pos + len >= 4`.
- **Signo no observable** (462, 586): en 462 las dos ramas son el MISMO slice cuando
  dispara (`lastSpace+1 == 0`). En 586 **no** es equivalente — la diferencia existe para
  todo ancho — y queda como hueco real: el detalle se renderiza dentro de un borde de
  ancho fijo y ningun test lee la celda de ahi.
- **Limite real sin fixture encima** (164, 242, 353, 386, 455, 498, 619, 624, 646): la
  etiqueta-era "NO son equivalencias, son huecos". **Seis de las nueve no eran ninguna de
  las dos cosas**: cuatro eran equivalencias (ver abajo) y dos tenian la prueba al reves.
  - `164` — la prueba citaba el test de la vuelta como el fixture que rechaza un delta
    `+1`. Ese test llama `wf.moveSelection(+-1)` **directamente** y nunca pasa por
    `HandleKey`: fija el ENVOLTORIO, no el signo del handler. El unico test que pulsa la
    flecha a traves de `HandleKey` afirmaba solo `wf.selected != before` — y desde la
    primera fila, "arriba" cae en la ultima y "abajo" en la 1, y **ambas** difieren de 0.
    Un guard que compara por cambio en vez de por valor no ve un signo. **Resuelto**:
    la fixture nueva camina desde la primera fila (donde la vuelta es visible) y desde
    la mitad (donde las dos direcciones se separan), porque ninguna posicion sola fija la
    direccion.
  - `386` — la prueba decia "un input de solo espacios, donde el mutante anade un token
    vacio". Falso dos veces: `>= 0` no anade nada, y un input de solo espacios vuelve en
    `where_filter.go:369` antes de que la linea que cierra el bucle se lea alguna vez.
  - `242, 353, 455, 498` — estas cuatro si eran huecos. `455` esta **CONFIRMADO muerto**
    por el gate; las otras tres estan con fixture escrita y pendientes de re-medir.

**Las cuatro que SI eran equivalencias** (386, 619, 624, 646), con la prueba:

- `386` `current.Len() > 0` -> `>= 0`. `strings.Builder.Len` devuelve `len(b.buf)`, no
  negativo por el lenguaje: `>= 0` es `true` incondicional, el mismo valor que `> 0` ya
  devuelve cuando se llega a la linea.
- `624` `pad > 0` -> `>= 0`. `pad` es `maxWidth - width` y solo se calcula en el `else` de
  `width > maxWidth`, asi que `pad >= 0` ya. El cuerpo que guarda,
  `strings.Repeat(" ", 0)`, es la cadena vacia de todos modos.
- `646` `w < width` -> `w <= width`. Con anchos iguales el mutante corre
  `strings.Repeat(" ", width-w)` con `width-w == 0`, o sea `""`. Salida identica.
- `619` `width > maxWidth` -> `>= maxWidth`. Esta necesita al llamado: `ansi.Truncate`
  devuelve su argumento **sin tocar** cuando `StringWidth(s) <= length` (charm.land/x/ansi
  `truncate.go`, las dos primeras lineas de `truncate`). O sea que en `width == maxWidth`
  el mutante devuelve la misma cadena que devolveria el `else`, porque el pad que se salta
  es `Repeat(" ", 0)`. Esto explica tambien por que el fixture de ancho exacto que la
  prueba vieja citaba **si existe y si corre** — y aun asi no puede distinguir las dos
  formas, que es exactamente como se ve un mutante equivalente desde fuera.

### Lo que el codigo de gremlins explica del porque de los supervivientes

`internal/engine/mappings.go`, que convierte esto en regla y no en busqueda:

- `INVERT_NEGATIVES` y `ARITHMETIC_BASE` **generan el MISMO codigo** para un `SUB`: los dos
  mapean `SUB -> ADD`. Por eso un `a - b` produce DOS mutantes identicos y hacen falta
  DOS entradas en el allowlist para una sola mutacion — y por eso los 14 `ARITHMETIC_BASE`
  y los 8 `INVERT_NEGATIVES` del allowlist se leen casi siempre en pareja sobre la misma
  linea (`462`, `535`, `548`, `586`, `table.go:1853`, `table.go:2124`, `ere.go:537/538`).
- `CONDITIONALS_BOUNDARY` es exactamente `GEQ<->GTR` y `LEQ<->LSS`: los 142 supervivientes
 Boundary son, todos, un intercambio de un operador. Por eso la barredor manual funciona:
  no hay que inventar el mutante, se lee el operador y se sabe cual es.
- `ARITHMETIC_BASE` sobre `ADD` es `ADD -> SUB`, o sea que `i+1 < len(sql)` se convierte en
  `i-1 < len(sql)` — que entra al cuerpo y **indexa fuera de rango**. Por eso
  `txn.go:338/354/374/396` son fallos de `mustNotPanic`, no de valores.

### Siguiente

- [x] Los 8 `CONDITIONALS_BOUNDARY` de `where_filter.go` que la cobertura no acreditaba:
      **la cobertura nunca estuvo stale**. `go test -covermode=atomic` sobre el paquete
      marca las 9 lineas con ambos lados tomados (`where_filter.go:242` -> bloque
      `241-242` count 11, bloque `242-244` count 2). La hipotesis de "regenerar la
      cobertura" era falsa; los mutantes vivian porque las fixtures no distinguian al
      mutante, no porque el gate no las viera.
- [x] El conjunct "muerto" de 145/156/163/170: **no** borrarlo, y no era tan muerto como
      decia. Ver la tabla de tres estados.
- [x] Barrer las `CONDITIONALS_BOUNDARY` a mano. Con el mapeo de `mappings.go` el mutante
      se deduce del operador, y la sonda multi-variante paga el trabajo de confirmarlo.
      **table.go (47) y ere.go (22) barredos por completo; 12 entradas retiradas en total.**
      El hallazgo importante no eran las entradas, sino una **bomba logica** en el
      allowlist que iba a borrar cuatro correctas:
- [ ] **UNA ENTRADA DEL ALLOWLIST POR (mutador, fichero, linea), SIN COLUMNA.** Una linea
      con dos comparaciones genera DOS mutantes y UNA entrada los cubre a los dos. El
      allowlist de `table.go` afirmaba que `400`, `2092` y `2152` se retiraban porque su
      mutante de PRIMER operando muriese — y el de segundo vive. Medido, 3/3 rojos y 0/3
      verdes en las cuatro lineas (`343`, `400`, `2092`, `2152`), SIEMPRE el segundo:
      - `343` el guard es **codigo muerto**: `calculateWidths` vuelve en `:309` cuando no
        hay columnas, y `:314` construye `g.widths` con `len(columns)` entradas, luego
        `len(g.widths) > 0` es siempre cierto. El test que se llama
        `...DoesNotDivideByZero` **vuelve antes de llegar**: su caso "sin columnas" pasa
        por el retorno temprano. Un test cuyo nombre promete un guard y cuyo fixture no
        llega a el es peor que no tener test, porque lee como cobertura.
      - `400` `:380` ya acota `cursorCol` a `len-1` y `:392` garantiza
        `scrollCol <= cursorCol`, luego el primer operando ya implica el segundo.
      - `2092`/`2152` una fila de mas, y el hueco de `:2250` se calcula del recuento de
        lineas del cuerpo que rellena: un bloque una linea mas alto y un newline menos.
        Es el mismo argumento que el `:2186` de al lado.
      Regla que sustituye a la anterior: una linea con dos comparaciones exige sondear
      AMBAS variantes, y solo un veredicto "todas muertas" retira la entrada.
- [x] **COBERTURA, no mutantes.** Medido el mapa real de cobertura y estan cuatro vistas al
      0% sin un solo test: `explorerpreview/preview.go` (226 stmt), `querybrowser.go` (203),
      `palette/palette.go` (116), `gridsidebarpreview/preview.go` (91), `nl2sql/provider.go`
      (86). Los ficheros ya gated estan al 93-99%, luego el margen esta ahi, no en el gate.
- [x] `palette/palette.go`: **0% -> 98%**, y **admitido al gate** (43 -> 44 gated, ver
      `MUTATE_SCOPE OK: 44 gated, 28 excluded`). Un widget puro sin I/O: `New/Show/Hide/
      IsVisible/SetWidth/SetHeight`, `handleKey` via `Update` y `View()`. 116 statements.
      - **Un bug real, la TERCERA vez del mismo defecto**: `handleKey` hacia backspace por
        BYTES (`p.query[:len(p.query)-1]`). Con una tilde o un CJK deja media runa, la linea
        renderiza un caracter de reemplazo y el filtro casa contra bytes que no deletrean
        nada. Ya estaba en el editor de celdas del grid y en el prompt de `ask`. Corregido
        a runas; el test que lo mata es `TestPalette_BackspaceRemovesOneWholeCharacter`.
      - **El desplazamiento lo calcula `View()`, no `Update()`**: `updateFiltered` pone el
        offset a 0 en cada tecla y `View` lo recalcula desde el cursor. O sea que el offset
        es un efecto secundario del render, y `View` tiene que ser idempotente — fijado.
      - **El suelo de 40 celdas se come la regla del 60%** para todo viewport <= 67, luego
        la caja es SIEMPRE de 38 celgas entre 10 y 67 columnas, y ** desborda** por debajo de
        38. Fijado como decision, no como descripcion.
      - Los alias llevan el atajo (`"copy (ctrl+y)"`), luego filtrar por "r" casa 17 de 20
        comandos. No es bug — es por que la busqueda por letra casi no filtra — pero es una
        decision de producto y ahora esta escrita.
      - Quedan 2 statements sin cubrir, `View:231` y `View:234`: los clamps de
        `scrollOffset` por encima y por debajo. **2400 estados** (40 semillas x 60 teclas)
        no los alcanzan. Son codigo muerto, y la property test
        (`TestPalette_AfterAnyKeySequenceThePaletteIsStillUsable`) es lo que lo demuestra.
- [x] `querybrowser/querybrowser.go`: **0% -> 99%**, admitido al gate (45 -> 45, luego 46 con
      `preview.go`). 201 statements, y el paquete no tenia ni un fichero de test.
      - **PERDIDA DE DATOS.** `handleKey` pasaba `b.cursor` a `store.ToggleFavorite` y
        `store.Delete`, que toman un indice de `All()` — la lista SIN filtrar, en orden
        cronologico inverso — mientras `b.cursor` indexa `b.entries`, que es la lista
        filtrada con un filtro activo y la solo-favoritos en esa pestana. Ambas son
        subsecuencias de `All()`, luego la fila `i` de una es la `j` de la otra con
        `j >= i`, y `j` deja de ser `i` en cuanto algo se filtra. Medido: filtrando a
        `SELECT alpha` y pulsando `d` borro `SELECT charlie`; en la pestana Favoritos, SIN
       ningun filtro, pulsando `d` sobre `favA` borro `plain`. Arreglado con
        `store.ToggleFavoriteEntry` / `DeleteEntry`, que emparejan por SQL+timestamp.
      - **CUARTA vez el backspace por bytes**: `b.filter[:len(b.filter)-1]`.
      - Una asercion puede **pasar con el bug presente** si su mensaje contradice su
        condicion: mi primera version decia "lo dejo en el store" detrás de una condicion
        que disparaba cuando NO estaba. Los dos subtests de P3 pasaban con la perdida de
        datos viva.
      - Una fixture que aterriza donde **los dos ordenes coinciden** no prueba nada. Los dos
        subtests empezaban en `t.Skipf` porque elegian el indice 0 — la misma fila siempre
        que la mas reciente sobreviva al filtro. Ahora eligen la fila donde DIVERGEN, y
        `Skipf` es `Fatalf`: un skip silencioso es como un test vacio sobrevive a la
        revision.
      - Con el filtro escribiendose, `d` es una LETRA. Correcto, y la fixture tiene que
        pulsar `enter` antes.
- [x] `explorerpreview/preview.go`: **0% -> 97%**, admitido al gate (**46 gated, 26
      excluded**). 226 statements, seis pestanas mas la navegacion del diagrama ERE.
      - **TERCER bug real en este fichero**: `ensureERESelectionVisible` estimaba la linea
        de la seleccion como `8 + row*6`, y era erronea por partida doble. La base ignoraba
        que la caja central crece con el numero de columnas (4 de marco + 1 por columna), y
        el paso decia 6 lineas cuando una relacion ocupa **4 lineas de caja + 1 en blanco**.
        Los dos errores la dejan POR DEBAJO de la posicion real y ambos crecen con la fila,
        luego en un diagrama mas alto que el panel la seleccion se sale por abajo mientras
        el scroll insiste en que ya se ve. Medido: con 10 relaciones y un panel de 14 lineas
        la seleccion ya no estaba en pantalla en la fila 0. Corregido con
        `ereSelectionLine`, que suma las MISMAS llamadas que hace el render.
      - El diagrama se construye con el `schemaForeignKeys` que hay **en ese momento**:
        `SetSchemaForeignKeys` DESPUES de `SetData` no tiene efecto y el diagrama se queda
        sin su mitad entrante, sin error y sin estado vacio. El app lo hace bien
        (`app.go:1260` antes de `:1261`), y por eso merece un test: un orden que solo un
        llamante acierta es un orden que se rompe al anadir el siguiente.
      - La navegacion arranca en la columna 1:N **sin seleccion**, `MoveDown` no hace nada en
        una columna vacia, y `clampRow` recorta pero **no crea** seleccion. Tres hechos que
        solo se aprenden leyendo `ERDiagramNav`, y un fixture que pulse abajo primero no
        prueba nada aunque parezca que si.
      - `MaxNeighbors` (10) corta las columnas: un fixture de 12 relaciones mide el cap, no
        el scroll.
      - Los 6 statements que quedan son inalcanzables y estan documentados en el allowlist
        del scope: los fallbacks de `CenterSchema`, el retorno tardio de `renderERE`, el
        `row < 0` de un guard que solo se llama con fila seleccionada, y el guard del log.
- [x] `gridsidebarpreview/preview.go`: **0% -> 96%**, admitido al gate (46 -> 47). 95 stmt.
      - **El scroll no llegaba al final del documento.** `ScrollDown` acotaba con
        `len(lines) - height + 2` mientras la ventana es `height - 4`, luego las **dos
        ultimas lineas eran inalcanzables** — y la ultima linea de un JSON indentado es su
        llave de cierre. Un usuario no podia ver que el documento terminaba. Corregido
        extrayendo `contentHeight()`, que es ahora el unico sitio que dice cuanto cabe, para
        que las dos mitades no puedan volver a separarse.
      - `ScrollDown` calculaba su limite con una aritmetica distinta de la de `Render`, y por
        eso se separaban; el bug ES la separacion.
- [x] `ai/nl2sql/provider.go`: **0% -> 96%**, admitido al gate (**48 gated, 24 excluded**).
      80 stmt. Los tests aisan el entorno (`t.Setenv` sobre cada variable + `HOME` temporal),
      asi que la clave real de quien lo ejecuta no puede cambiar un resultado.
      - **Configurar `ai.providers.anthropic.model` rompia anthropic.** La tabla de
        proveedores se miraba ANTES que los Arms integrados, asi que una entrada para un
        nombre integrado mandaba anthropic al camino OpenAI-compatible, donde la variable
        de la API key es la que diga la entrada — vacia si el usuario no la puso. O sea: la
        unica cosa que un usuario podia hacer para elegir modelo era la que le rompia el
        proveedor. Corregido: **un nombre integrado es integrado**, y la tabla nombra
        endpoints ADICIONALES.
      - **`ai.model` no hacia nada para qwen, ni para pi, hermes y jcode.** Tres arms
        consultaban el modelo global con fallback al del proveedor; el cuarto solo miraba la
        tabla. Y los cuatro detectores de ficheros: solo `resolveOpenCode` aceptaba el
        modelo configurado. Unificado en `modelFor` y `overrideModel`.
      - La precedencia entre `ai.model` (que tiene default `mimo-v2.5`) y
        `ai.providers.<n>.model` la dejo como estaba, salvo lo mecanico: es una decision de
        producto y merecia una pregunta, no un cambio mientras se barre cobertura.
- [ ] `ai/nl2sql/provider.go` (86 stmt, 0%).
- [ ] Repetir el barrido en `toast.go` (9), `bordered.go` (9), `modal.go` (8), `ask.go` (6),
      `pager.go` (5), `txn.go` (4), `mouse.go` (3), `header.go` (3),
      `query_history.go` (3), `picker.go` (2), `cell.go` (2), `scanner.go` (2),
      `keybindings_groups.go` (2), `fuzzy.go` (1), `schema.go` (1) — 53 en total.
      `bordered.go` esta practically resuelto por lectura: sus nueve son clamps, mas
      `147`/`189` que dependen del mismo retorno temprano de `ansi.Truncate` que hace
      equivalente a `where_filter.go:619`.
- [ ] Los otros 33 (`ARITHMETIC_BASE`, `CONDITIONALS_NEGATION`, `INVERT_NEGATIVES`,
      `INCREMENT_DECREMENT`) necesitan un barredor aparte: la sustitucion no es un
      intercambio de operador sino reescribir la operacion
- [ ] `go tool gremlins unleash` **panico** con `panic: send on closed channel` en
      `os/signal.process` al terminar un run por paquete, y **no escribe `report.json`**.
      Ocurre tras ~260 mutantes evaluados. Si pasa, el resultado no es "0
      supervivientes": es un run inexistente. Comprobar que el fichero existe antes de
      leer nada de el.
- [ ] **MEDIR CON LA CPU CONTENDIDA NO ES MEDIR.** La otra repo (`tsk`) corre su propio
      gate a 4 workers en paralelo; con las dos a la vez el load average llega a 64-70 y
      un mutante sano se pasa de tiempo. En el run de `where_filter` con load 64: 13 de
      128 resultados SALIERON `TIMED OUT`, incluidos `242:36`, `353` y `498` que no tienen
      entrada en el allowlist. Un timeout no es una muerte ni una supervivencia: es
      ausencia de informacion. Re-medir con el arbol quieto.

### Siguiente

- [x] Los 8 `CONDITIONALS_BOUNDARY` de `where_filter.go` que la cobertura no acreditaba:
      **la cobertura nunca estuvo stale**. `go test -covermode=atomic` sobre el paquete
      marca las 9 lineas con ambos lados tomados (`where_filter.go:242` -> bloque
      `241-242` count 11, bloque `242-244` count 2). La hipotesis de "regenerar la
      cobertura" era falsa; los mutantes vivian porque las fixtures no distinguian al
      mutante, no porque el gate no las viera.
- [x] El conjunct "muerto" de 145/156/163/170: **no** borrarlo. Ver arriba — decide todo.
- [ ] Barrer las 152 `CONDITIONALS_BOUNDARY` a mano y borrar las que mueren
- [ ] Los otros 20 (`ARITHMETIC_BASE`, `CONDITIONALS_NEGATION`, `INVERT_NEGATIVES`,
      `INCREMENT_DECREMENT`) necesitan un barredor aparte: la sustitucion no es un
      intercambio de operador sino reescribir la operacion
- [ ] `go tool gremlins unleash` **panico** con `panic: send on closed channel` en
      `os/signal.process` al terminar un run por paquete, y **no escribe `report.json`**.
      Ocurre tras ~260 mutantes evaluados. Si pasa, el resultado no es "0
      supervivientes": es un run inexistente. Comprobar que el fichero existe antes de
      leer nada de el.



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

- [x] `gridpreview/preview.go`: **13% -> 90%**, admitido al gate (**49 gated, 23
      excluded**). 644 statements, y el paquete tenia un test de 54 lineas contra 1253 de
      codigo. Lleva su propio motor jq, expansion de FK e historial en fichero.
      - **Un indice que no es un numero resolvia al elemento CERO.** `Sscanf` deja `idx`
        en 0 cuando no casa con nada, asi que `.arr[abc]` devenia `.arr[0]` en silencio. Un
        error de tecleo leyendo el primer elemento es peor que no leyendo: parece que
        funciona. Corregido comprobando cuantas conversiones hubo.
      - **Las sugerencias salian en ORDEN ALEATORIO**, porque Go itera un mapa asi, y la
        entrada resaltada es la que `enter` y `tab` aceptan. Dos `tab` sobre el mismo
        documento elegian campos distintos, y la lista se reordenaba bajo el cursor en cada
        recomputacion. Ordenadas.
      - **Caminar el historial reabria el popup de sugerencias.** Las flechas miran si la
        lista esta visible ANTES de decidir que manejan, y `updateJQSuggestions` la vuelve a
        mostrar; luego la SEGUNDA flecha arriba navegaba el popup en vez de seguir por el
        historial. Un usuario pulsando arriba tres veces avanzaba uno. Corregido: una
        expresion recordada no muestra sugerencias.
      - El popup **da la vuelta** en las dos direcciones; el historial **se detiene**. Las
        dos cosas fijadas, y el contraste vale: un popup que estabas leyendo debe volver a
        la fila que mirabas, un paseo por lo que ejecutaste debe parar en lo mas viejo.
      - `len(key) == 1` sobre la cadena hace que un caracter multibyte no se pueda escribir
        en el filtro. Es la misma limitacion documentada que en el WHERE filter, y queda
        fijada en los dos sitios: los identificadores de jq son ASCII, pero un literal de
        cadena no lo es, asi que `.name == "Cafe"` hoy no se puede escribir. **No lo
        cambie** — es una limitacion conocida del widget, no un descuido.
      - `halfPageUp`/`halfPageDown` mueven el SCROLL y no el cursor, y no pasan por
        `ensureCursorVisible`. Es un gesto distinto de flechear — leer mas abajo sin perder
        el sitio — y queda fijado como tal, con el invariante correcto (el scroll dentro
        del documento, no `scroll <= cursor`, que es falso por diseno tras media pagina).

- [x] `explorer/explorer.go` + `explorer/tree.go` (+ `node.go`, `handled.go`): **7%/18% ->
      100%**, los cuatro ficheros del paquete al 100%. Admitidos al gate (**51 gated, 21
      excluded**). 368 statements, y el paquete tenia un test de 85 lineas.
      **Cuatro bugs reales, y TRES de la misma forma**: "deja el cursor dentro de la
      lista" y "desplaza la ventana para que el cursor se vea" estaban escritos a mano en
      dos o tres sitios cada uno, y cada copia cubria un subconjunto DISTINTO de los
      casos.
      1. **La app se caia.** `Selected()` hacia `t.filtered[t.cursor]` sin comprobar nada,
         y `SetFilter` — el cuarto sitio que encoge la lista — no atenua el cursor. Ruta a
         mano: flechas hasta la ultima tabla, `/`, escribir un filtro que casa con menos
         filas que el indice del cursor, y el siguiente `Selected()` indexaba fuera de
         rango y se llevaba el proceso. `clampOffset` solo atenua el scroll, asi que nada
         mas lo cogia.
      2. **`G` dejaba la seleccion fuera de pantalla.** `go_last` ponia el cursor en la
         ultima fila y luego llamaba a `clampOffset`, que solo tira de la ventana hacia
         atras. En una lista mas alta que el panel, la tabla que acababas de elegir
         quedaba por debajo del borde inferior.
      3. **Colapsar un nodo ya colapsado movia el cursor sin mover la ventana.** El
         cursor salta al padre, que puede estar mucho mas arriba, y la ventana se queda
         mostrando el final: en un panel de una fila pulsas collapse y la fila sobre la
         que vas a actuar deja de ser la que ves.
      4. **Un clic en el borde o en la linea del filtro seleccionaba una fila fuera de
         pantalla.** `HandleClick` comprobaba `filteredIndex` pero no `listY`, asi que con
         el arbol desplazado un clic arriba daba `offset + (-1)`: una fila que ya habia
         salido por arriba. `Enter` actuaba sobre ella.
      - Corregido en el unico sitio por el que pasan todos: `flattenNodes` ahora llama a
        `clampCursor()` y `scrollToCursor()`, y las tres copias locales se fueron. El test
        de property (20 semillas x 120 pasos mezclando teclas, acciones y clics) es lo que
        encontró el 3 y el 4; el 1 y el 2 los encontró el test dirigido.
      - **La quinta vez que un byte se aplica a una cadena indexada por runas**:
        `filter[:len(filter)-1]`. El teclado no puede meter un caracter multibyte (el
        brazo por defecto prueba `len(key) == 1`), pero `SetFilter` es publico, asi que un
        filtro con un acento es alcanzable hoy. Las otras cuatro tambien eran
        "alcanzables solo con un acento".
      - **El carve-out de teclas de la app estaba escrito dos veces y solo una.** El modo
        jq tenia `help`/`palette`/`rollback`; el filtro del explorador no tenia NADA, y
        como `handleFilterKey` dice `handled` para cualquier tecla, `?` escribia un `?`
        literal en vez de abrir la ayuda — la unica tecla que pulsa quien no sabe que
        pulsar, que es justo cuando esta filtrando. Ahora los dos sitios leen
        `actionSurvivesTextInput`, una sola lista, y esa funcion tiene su test.
        `quit` sigue FUERA a proposito: esta en `q`, y `q` es un caracter legal en un
        filtro de tablas, y su otro binding `ctrl+c` no se separa de `q` a este nivel (el
        carve-out es por ACCION, no por tecla). Consecuencia, dicha de frente: **no hay
        tecla que salga de dentro de un filtro.** Fijado en el test para que "anadir quit"
        — la idea siguiente obvia — se vea equivocada.
      - **El filtro solo busca TABLAS.** Una columna (`email`) o un schema (`audit`) no
        encuentran nada. Es una limitacion, no un bug: la accion se llama `filter_tables`.
        Fijada, porque una busqueda que no encuentra la columna que estas viendo es
        sorprendente, y cedarla a "matches columnas" es una decision de producto.
      - **La ventana del arbol solo arranca con `height >= 1`**, y `Explorer.SetHeight(h)`
        le pasa `h-2`, asi que por debajo de 3 el arbol pinta su lista entera. No es un
        bug: `bordered.RenderWithTitleEx` recorta a `h+2`. Fijado sobre las dos mitades,
        para que un cambio futuro sepa que la otra lo sostiene.
      - **`expand_node` sobre una TABLA no expande**, pide la tabla. Expandir es
        `ToggleExpand` (el raton). Dos formas de expandir que hacen cosas distintas, y un
        test que las hubiera dado por una solo habria pasado mientras la diferencia
        pasaba inadvertida.
      - `toggle_columns` sobre una tabla solo puede colapsar, y deja el cursor en el
        schema que colapso; expandido de vuelta requiere un segundo pulso ya ahi.
      - `SelectTable` expande los ancestros y despues puede decir FALSE (si un filtro
        esconde la tabla). La mitad expandida es la util y el `false` es honesto; hacerlo
        atomico exigiria no expandir — que es justo lo que hace falta para ver la tabla.
        Fijado como comportamiento, no corregido.
      - Los iconos estan EN BLANCO y el marcador de expandir no cambia. Llevan asi desde
        el primer commit (`5d3983c`): no se perdio ningun glifo, nunca lo hubo. La rama
        `if node.Expanded` es codigo muerto cuyas dos ramas coinciden, asi que su unico
        mutante es PROVABLEMENTE equivalente y esta en la lista con la prueba (grupo 5).
        **No inventar glifos** es una decision de producto. El test los fija Y SE SALTARIA
        solo el dia que los marcadores difieran, para que la entrada no sobreviva al
        comportamiento que justifica.
      - Cuatro ramas defensivas se alcanzan escribiendo el campo a mano (cursor negativo,
        offset negativo, una fila `nil` en la lista, un arbol sin nada). Un guard que
        nadie ha corrido es un guard que nadie sabe que funciona.
      - `HandleAction` y `ToggleExpand` NO comprueban `e.tree == nil`; `SelectTable` y
        `HandleClick` si. No se anadio un cuarto guard: es inalcanzable, y lo que se fija
        es la asimetria.

- [x] `internal/app/app.go`: **25% -> 65%** (1446 statements). El ultimo grande.
      Tres ficheros nuevos: `ddl_navigation_test.go` (las funciones puras),
      `router_contract_test.go` (la tabla de acciones y el despacho de teclas) y
      `message_contract_test.go` (los 47 tipos de mensaje).
      **Dos bugs mas en `extractDDLTableName`, y los dos de la misma familia:**
      1. **El espacio alrededor no se toleraba, y contestaba con un nombre que NO
         EXISTE.** `upper` estaba recortado con `TrimSpace` y `sql` no; el corte
         posterior contaba los bytes de la palabra clave desde el texto SIN recortar.
         `"  DROP TABLE IF EXISTS public.users"` -&gt; `schema "public"`, `table "LE"`.
         El resultado alimenta `loadSchemaWithTarget`, asi que el explorador navegaba
         a `public.LE`. Y el espacio no es exotico: la sentencia llega del editor y del
         historial, y el `TrimSpace` de la primera linea de la funcion PROMETIA
         manejarlo. **Novena vez** que la misma cosa se derivaba en dos sitios.
      2. **El default de `public` no se aplicaba a un nombre entrecomillado**, porque
         ese brazo retorna antes. `"MyTable"` -&gt; schema vacio. Y entrecomillar es
         OBLIGATORIO en Postgres para cualquier nombre con mayuscula, asi que
         `CREATE TABLE "MyTable" (id int)` no navegaba a ningun sitio mientras el mismo
         `CREATE TABLE MyTable` navegaba bien. **Decima vez.**
      3. `CREATE TABLEX` y `CREATE TABLESPACE` casaban como `CREATE TABLE` y devolvian
         la tabla `"X"` / `"SPACE"`. Ahora `hasWordPrefix` pide una PALABRA.
      4. **El `;` cortaba el nombre detras de un esquema y no sin el.** Los dos lectores
         de token estaban escritos por separado y sus conjuntos de caracteres no
         coincidian: `CREATE TABLE users;` navegaba a `users;` y `CREATE TABLE s.users;`
         navegaba bien. Unificados en `takeName`.
      - **Limitaciones, no bugs, y quedan dichas.** El espacio ENTRE palabras clave no
        se tolera (`create\n  table users` no nombra nada) — a diferencia del de los
        extremos, este contesta NADA y no un nombre falso, y arreglarlo es tokenizar en
        vez de prefijar. `TRUNCATE users` es DDL pero no nombra tabla. `COMMENT ON` no es
        DDL. `formatSQLValue` entrecomilla un `int32`/`int`/`float32` que
        `formatFKValue` deja a pelo, y un `[]byte` sale como `'[120]'` en las dos: no hay
        respuesta obviamente correcta y el driver no devuelve esos tipos.
      - **`preview_cursor_up`, `preview_cursor_down`, `focus_explorer` y `focus_grid`
        no tienen tecla**: son de paleta (`Keys: nil` en el registro). El test lo
        convierte en una puerta de decision — el conjunto sin tecla debe ser EXACTAMENTE
        ese, en los dos sentidos — porque "esta en la paleta?" no se puede responder
        desde este paquete (el tipo de la lista de comandos no es exportado).
      - **`Model.Update` tiene receptor por VALOR y devuelve un modelo nuevo**, asi que
        `m.Update(msg)` como sentencia suelta tira el resultado. Mi harness lo hacia y
        el sintoma era desconcertante: la app renderizaba 10 lineas en un terminal de
        40 **y en uno de 100**, parecia un bug de layout y era un valor descartado.
        Ahora hay un helper `resize()` para que ningun fixture pueda repetirlo.
      - **Los componentes se dimensionan durante el render** (`renderGrid` llama a
        `SetWidth`/`SetHeight`), asi que inspeccionar `grid.View()` antes del primer
        render da nada y parece que los datos no llegaron. Se renderiza primero.
      - Los ticks (spinner y toast) son **idempotentes**: cincuenta seguidos no cambian
        el estado, ni el foco, ni la pantalla. Es el unico mensaje que se pliega millones
        de veces en la vida del programa.
      - Una conexion fallida va a `StateError`, no al picker: la app sabe QUE proyecto
        fallo y volver al picker lo perderia.
      - `help` y `palette` **abren**, no alternan; cerrarlos es de escape. Anotado porque
        la costumbre es alternar.

- [x] `editor/sql.go` **53% -> 95%** y `editor/autocomplete.go` **69% -> 85%**, ambos
      admitidos al gate (**53 gated, 19 excluded**). `sql_contract_test.go`.
      **La sexta vez que un byte se aplica a una cadena indexada por runas, y la
      que mas importa**: esta es el **texto de la consulta**. A diferencia de los cinco
      filtros, aqui SI se puede escribir un caracter multibyte — el brazo por defecto
      inserta `msg.Text` cuando no esta vacio, sin mirar `len` — y lo medido:
      - `é` + retroceso -> la linea guardaba `\xc3`, que no es UTF-8 valido
      - `表` + retroceso -> `\xe8\xa1`
      - `delete` en medio de `aéb` -> `a\xa9b`
      - `é` **renderizado** -> el byte de continuacion leido solo es U+00A9, asi que el
        editor mostraba un **signo de copyright** donde el usuario habia escrito un acento
      - mover el cursor con las flechas lo dejaba en mitad del caracter, y `end`/insercion
        partirian el texto ahi
      Es decir: texto invalido enviado a PostgreSQL, y un caracter visiblemente corrupto.
      - **El arreglo NO es "haz la columna un indice de runas"**: todo lo demas del
        fichero corta con ese numero, y cambiar la representacion tocaria todas las lineas
        a cambio de nada. Es mantener el indice en BYTES y no dejar que caiga nunca en
        mitad de un caracter — `prevRuneStart` / `nextRuneEnd` / `runeLenAt`, y
        `SetCursorPos` tambien ajusta hacia delante, porque recibe un offset del grid y un
        offset en mitad de un caracter es entrada legal.
      - **`tab tab` duplicaba la palabra**: `acceptCompletion` volvia a disparar el popup
        tras aceptar, y como una terminacion acaba en separador (espacio, o punto tras un
        schema) el contexto nuevo volvia a ofrecer el MISMO item sobre un token vacio
        detras. Medido: `"FROM u"` tab -> `"FROM users "`, tab -> `"FROM users users "`.
        El comentario de la propia funcion PROMETIA idempotencia ("accepting the keyword
        you already typed never duplicates it") y no la cumplia. Ahora cancela en vez de
        redisparar: el segundo tab indenta, que es lo que hace falta.
      - **El popup SIN esquema si se abre**, porque las palabras clave se cargan siempre y
        `UPDATE` casa con `u`. Lo que no puede aparecer es una TABLA ni una COLUMNA.
        Mi primer test afirmaba por visibilidad y reporto como fallo el comportamiento
        correcto.
      - **La ventana del editor es `lines[0:height]` y NO sigue al cursor.** Es una
        propiedad de un modal, no un descuido; fijada, con la correccion de que mi property
        walk la calculaba desde el cursor y dio por missing una linea que si estaba.
      - **Un grafo de varios code points se edita de uno en uno**: una bandera son dos
        Indicadores Regionales que se pintan como un glifo, y una `e` con acento combinante
        son dos code points. Editar por code points es lo correcto aqui — un cursor
        consciente de clusters seria un cambio mucho mayor por un caso que solo importa
        dentro de literales — y cada estado intermedio sigue siendo texto valido.
        Fijado porque mi tabla inicial esperaba que la bandera se borrara de una vez.
      - Un caracter de **N code points distintos** tiene **N columnas cursor**: para un
        solo `é` las unicas posiciones validas son 0 y 2, asi que "una flecha izquierda"
        desde el final va a 0. Mi primer test afirmaba `ancho-1`, que es una columna en
        medio del caracter.
      - `PushHistory` ignora solo la cadena EXACTAMENTE vacia: una sentencia de espacios
        se guarda, porque el editor las distingue en todas partes y recortarlas aqui
        haria que el historial discrepase de lo que hay en pantalla.
      - `home`/`end` SI los maneja el editor (`case "home", "0"`), asi que no estan entre
        las teclas "que no son texto y no son gesto".
      - `autocompleteMinPrefix` negativo se convierte en 0 a proposito: `len(t) < -1`
        nunca es cierto, asi que un negativo ya valia cero por accidente y no por decision.

- [x] `nl2sql/compatible.go` **61% -> 96%** y `config/config.go` **4% -> 100%**, ambos
      admitidos al gate (**54 gated, 18 excluded**).
      - **`cleanSQL` comparaba la etiqueta de la valla en MAYUSCULAS/S minusculas.** Un
        modelo que responde ` ```SQL ` dejaba la palabra pegada a la sentencia, que
        parsea como alias de columna — el usuario veia un error plausible de "relation no
        existe" en vez de "el modelo mando una valla". Una ronda anterior ya lo habia
        **fijado como defecto conocido** (`TestCleanSQL_UppercaseLanguageTagLeaksIntoTheSQL`);
        ahora esa fija pasa a afirmar el arreglo.
        - El arreglo son tres casos explicitos, no una regla: valla con salto de linea (quita
          la etiqueta en cualquier caja), valla pegada a la sentencia (` ```SELECT 1 `), y
          valla en linea sin salto. **El caso pegado es genuinamente ambiguo** —
          ` ```sqlSELECT 1 ` y ` ```SQL SELECT 1 ` son los mismos tres acentos graves mas
          unas letras — asi que decide el prefijo exacto en minusculas, que es la lectura
          que la suite ya fijaba. La consecuencia, dicha para que no se redescubra: una
          valla de una linea con etiqueta en mayusculas y sin salto final sigue soltando la
          etiqueta. Todo lo que pone la valla en su propia linea — que es lo que producen
          los renderizadores de markdown y lo que devuelven los modelos — queda limpio.
      - **Un SQL vacio se devolvia como EXITO.** Las dos `Generate` devolvian `("", nil)`
        cuando la respuesta venia vacia (una negativa, un filtro, una respuesta cortada), y
        eso llegaba a `execute_query` como sentencia vacia: la pulsacion no hacia nada.
        Ahora es un error en las dos.
      - **`DetectJCodeConfig` elegia proveedor recorriendo un MAP**, asi que la iteracion
        aleatoria de Go decidia a que endpoint se manda el prompt: 40 lecturas del mismo
        fichero con tres proveedores con clave dieron `zzz` 28 veces, `aaa` 6, `mmm` 6. Y
        como `model` es un valor global mientras `base_url` es por proveedor, el modelo
        global se mandaba a un host aleatorio. Ahora alfabetico: no hay respuesta
        "correcta" para cual de varios proveedores quiso el usuario, pero una reproducible y
        documentada le gana a una invisible, y es ademas asertable.
      - **`StateDir` descartaba el error de `UserHomeDir`**, asi que sin `HOME` la ruta del
        historial de consultas salia **RELATIVA** (`.local/state/dbx`) y se escribia en el
        directorio desde el que se lanzo dbx. `SessionDir` cuatro lineas mas arriba ya
        caia a `TempDir`: las dos hermanas discrepaban. Misma aritmetica escrita dos veces
        y abandonada al deriva.
      - **`DBX_*` nunca funciono, y faltaban DOS cosas, no una.** `AutomaticEnv` solo
        resuelve en `Get`, mientras `Unmarshal` construye desde `AllSettings` — asi que
        ninguna override llegaba a la struct; ademas viper pide `DBX_UI.PAGE_SIZE` con
        punto si no hay `SetEnvKeyReplacer`. Todas las claves son anidadas, asi que eran
        todas. Medido antes: fichero con `page_size = 7` y `DBX_UI_PAGE_SIZE=13` cargaba 7.
        El `BindEnv` se deriva de `AllKeys()` y no se reescribe a mano, porque una clave
        con default y sin binding es justamente el medio cableado que se deriva.
      - **`PiProvider.baseURL` es una CONSTANTE** y `NewPi` no la acepta, al contrario que
        `OpenAICompatible`: por eso su `Generate` era inalcanzable sin hablar con la API
        real de Inflection. El test usa el campo no exportado (mismo paquete) en vez de
        cambiar la firma, y la asimetria queda escrita.
      - **`stripJSONC` estaba bien** y lo verifique en vez de asumirlo: valla de bloque sin
        cerrar, comas finales antes de `}`/`]`, y `//` dentro de una cadena, todo correcto.

- [x] **Las seis llaves muertas, cableadas.** Una llave que no hace nada es PEOR que una
      que no existe, porque parece que funciono. Las seis estaban declaradas con default y
      no las leia nadie:
      - **`connections[].password_env`** — la forma documentada de no poner la contrasena en
        `config.toml` no hacia nada. Quien la usaba y quitaba la contrasena de la URL
        simplemente no conectaba, sin nada que dijera que la llave no se soporta. El
        mecanismo hermano **ya funcionaba**: los ficheros de proyecto `.dbx.toml` expanden
        `${env:NAME}`. Esto es el config global aprendiendo la convencion que los ficheros de
        proyecto ya tenian.
        - `dsnWithPassword` es una funcion pura y se prueba sola, porque "esta DSN tiene
          contrasena?" no es una pregunta: puede no tener userinfo, tener usuario pelado, o
          tener usuario:contrasena. Un chequeo que solo mira `:` antes de `@` falla en dos de
          los cuatro casos. Y una DSN que **ya** trae contrasena la conserva — el fichero es
          la declaracion mas explicita.
      - **`ui.statusbar_help`** — apagaba/encendia el panel de atajos. Y de paso: el layout
        reservaba un **`7` hardcodeado** mientras el pane dibuja un numero variable de lineas
        envueltas, asi que el contenido se quedaba corto por la diferencia en cualquier
        ventana cuyo vista necesitara mas o menos de cinco segmentos. **Cuarta vez** en este
        repositorio que la misma cantidad se escribe en dos sitios. Ahora el pane expone
        `Height()` y el layout lo pregunta.
      - **`ui.history_size`** — el store hardcodeaba `500` y esto valia `100`, y nunca se
        encontraron. Se recorta ** tambien al LEER**, no solo al anadir: si no, bajar el
        limite no hacia nada hasta que el usuario ejecutara suficientes queries, y un fichero
        escrito con un limite mayor se quedaba lleno.
      - **`ui.query_history_path` -> `ui.state_dir`** — el default del knob **no era ni la
        forma de la ruta real**: el historial es por proyecto, en
        `<raiz>/projects/<nombre>/query_history.json`, asi que una llave que nombra un
        FICHERO no podria funcionar nunca. Renombrada en vez de reinterpretada: la vieja no
        hacia nada, luego ninguna configuracion que funcionara depende de ella.
      - **`session.enabled` + `session.retention_days`** — `session.NewLogger(dir, retention)`
        existia entero y **nadie lo llamaba nunca**, asi que dbx no escribia ningun log de
        sesion. Por eso `session.dir` parecía funcionar: solo lo usaba el LECTOR de la CLI.
        Ahora se construye en `NewModel`, se registra cada sentencia con su duracion y sus
        filas, y `Cleanup()` corre una vez al arrancar en su propio comando (es un recorrido
        de directorio; en un portatil con un ano de logs son miles de `stat`).
        - **La conexion se registra por NOMBRE, nunca por DSN.** Una DSN lleva la contrasena
          — es justo lo que `password_env` mete en ella — asi que escribirla en un log
          desharia el punto entero de esa llave.
        - Un log que no se puede abrir **no detiene la TUI**: es la unica feature que puede
          estar ausente sin que el usuario se entere por las malas.
      - **`ai.providers.<n>.base_url`** — el campo faltaba en `AIProviderConf` mientras
        `nl2sql.ProviderConfig` ya lo tenia y `Resolve` ya lo leia. Dos lineas de enlace, y
        mapstructure descarta una clave desconocida **sin decir nada**, asi que escribir
        `base_url` no hacia absolutamente nada.
      - **En el camino, la cuarta copia de `~/.local/state/dbx`**: `NewQueryStore` reimplementaba
        la expresion de `config.StateDir()` **con el mismo error descartado**, asi que un
        directorio vacio resolvia a una ruta relativa. Tres copias de una ruta en un repo.
      - Un `m.config` nil en `initQueryStore` **panico** a la primera llamada con `stateDir`
        vacio. No es defensa de mas: el harness de test construye un modelo sin config.

- [x] **Ronda 11 — cobertura 87.7% -> 89.8%, gate 54 -> 59 ficheros.** Siete ficheros
      nuevos bajo test, de los cuales **cuatro tenian CERO** y tres estaban a medias.
      El hallazgo de la ronda: **cinco de los siete fallos eran mios**, no de la app.
      Ese es el dato, porque es el que se repite — ver "Learnings" abajo.

- [x] **`internal/theme` al 100% desde el 0%.** No habia ni un test en el paquete, y
      `theme.Resolve` es la PRIMERA linea de cada arranque: cada tema que el usuario
      puede elegir se estrenaba en su terminal.
      - La asercion que vale no es "devuelve algo" sino **"todo color que el tipo puede
        llevar esta puesto, en todo tema que Resolve puede devolver"**, escrita sobre la
        struct por reflexion. Un campo nuevo en `Theme` convierte esto en un test rojo
        hasta que todos los temas lo rellenen, que es justo el momento en que el bug se
        publicaria.
      - Un color sin puesto no falla ruidosamente: lipgloss pone `NoColor` y el texto se
        pinta con el color por defecto del terminal, que parece "un tema un poco raro" y
        no un bug. **Medido, no supuesto.**
      - `detectTerminalColors()` no recibe nada y devuelve dos hex fijos, asi que el tema
        "system" **no detecta nada**. Fijado como esta, con el comentario que lo dice,
        porque "cambiar el tema segun el terminal" es una decision de producto que
        alguien tiene que tomar a proposito.

- [x] **`keybindspane.View()` sin un solo test en su paquete.** Todos los tests
      existentes assertaban sobre `renderLines()`, el slice intermedio — la funcion que
      dibuja de verdad no se llamaba nunca. Y `Height()` (que anadi el la ronda anterior)
      se deriva de `renderLines()`, no se mide de `View()`: lo unico que los mantiene
      honestos es que un pane pintado ocupe `len(renderLines()) + 2` filas, y nada lo
      afirmaba. Un cambio de tema que añadiera una fila de borde habria movido la altura
      del contenido sin que nadie se enterara.
      - El color del borde se lee de la **linea de arriba**, que es borde puro. La primera
        version comparaba el render entero y afirmaba que el pane desenfocado no contenia
        el color activo: lo contenia, porque varios estilos del tema oscuro comparten
        `#89b4fa` y la secuencia aparece tambien en el texto de ayuda. La afirmacion
        hablaba del marco entero cuando la afirmacion era sobre el borde, y habria
        seguido pasando atraves de un cambio que intercambiara los dos colores.
      - Y el guard de "los dos colores tienen que ser distintos" **atrapo un bug mio**:
        `rgbHex` emitia ocho digitos con alpha, que lipgloss no parsea, asi que devolvia
        la cadena vacia para todos los colores y los dos comparaban iguales.

- [x] **`grid/table.go`: los brazos de movimiento y paginacion de `dispatchAction`**, que
      son el unico sitio donde `scrollRow` se pone a cero al paginar. Un brazo que
      paginara sin esa linea dejaria el cursor apuntando a otra fila de la pagina nueva,
      en silencio — y se editaria la fila equivocada.
      - **Bug real (tercera copia de la familia de la deriva):** `startEdit` **no
        protegia el `nil`** mientras las otras dos copias de la misma decision — las dos
        ramas de fila pendiente en el `switch` de `edit_cell` — si. Consecuencia:
        abrir el editor sobre una celda NULL y pulsar Enter **escribe los cuatro
        caracteres `<nil>`** en una columna que era NULL. Y la copia sin guarda era
        justamente la que alcanza un `SELECT` normal. Ahora las tres pasan por
        `cellText`.
      - Tres suposiciones mias mas, las tres porque el nombre de la funcion prometia algo
        que el codigo no hacia: `visibleColumns` devuelve **anchos**, no indices; `moveRight`
        **envuelve** deliberadamente; `go_last`Means "la ultima fila de la PAGINA", no de
        la pantalla. Y un fixture mio con 10 filas por pagina en una pantalla de 20 no
        puede detectar un `scrollRow` que no se reinicia — porque nunca se desplaza. Cuatro
        subtests DAMOS fallando sobre mi propio setup antes de dar con el fondo.

- [x] **`gridpreview`: los caminos que solo se alcanzan con una fila uncommon.** Una fila
      **mas corta que la lista de columnas** es lo que devuelve un `SELECT` de un
      subconjunto de columnas de una tabla, asi que el guard `i < len(row)` no es
      defensa de mas. Una fila **nula**, un valor que JSON **no puede marshallar**.
      - **Bug real:** `ctrl+space` **nunca abria la lista de sugerencias**. El case hacia
        `updateJQSuggestions()` — que **empieza poniendo** `jqSugVisible = false` y
        **acaba poniendola en true** si encontro algo — y luego invertia ese valor. Con
        sugerencias disponibles quedaba en `false` siempre. Las sugerencias si aparecian
        (`len(jqSugs)` pasaba de 0 a 3), que es por lo que pareceria vivo.
        Se parece a los seis knobs muertos: el efecto se notaba, la causa no.
      - Fijado como esta, sin "arreglar": `applyJQ` con `rawJSON` nil **no hace nada** y
        el preview sigue mostrando "No data", que es la verdad. Y un literal sin cerrar
        **no** se considera "dentro" si el cursor esta exactamente en su primer caracter.

- [x] **`editor/autocomplete.go`: `Render` entero**, que son 16 statements de decisiones
      de layout —popup mas ancho que la ventana, la seleccion fuera de la ventana, un
      nombre mas largo que la columna—. Ninguno es alcanzable desde un fixture con cuatro
      nombres cortos, que es lo que usaba todo test previo.
      - **No hay bug aqui, y por eso el fichero merece el test igual:** la seleccion
        **ENVUELVE** en las dos direcciones, como el cursor de columna del grid. Lo
        asumi mal al principio y mis tests dicen "se detiene en el final".
      - `isSQLKeywordPrefix` deja que cualquier clausula caiga a keywords, asi que
        `SELECT * FROM users ` (espacio detras de la tabla) **no ofrece columnas** y
        ofrece keywords. **Fijado con el comentario que lo dice**, porque es una decision
        de producto y no un bug que deba "arreglarse" en silencio.
      - Y una trampa real para el proximo que llame a `findTablesInStatement`: con los
        tokens CRUDOS devuelve `table: " "` y mete el nombre de la tabla en `alias`,
        porque `isWordToken` no rechaza un espacio. No es bug — el unico caller pasa
        `significantTokens` primero — pero esta fijado como trampa y no como nota al
        margen.

- [x] **`config/repoid.go` — BUG REAL y el importante de la ronda.** La identidad de repo
      se deriva del sistema de ficheros **sin ejecutar `git`**, y una derivacion
      sutilmente incorrecta es peor que no tener identidad: fusiona dos proyectos que
      deberian estar separados, o parte uno en seis.
      - `findDotGit` comprobaba el tope de `root` **un paso tarde**: probaba `root/.git` y
        solo despues preguntaba si habia llegado a root. Asi que un repositorio **en** root
        se encontraba desde cualquier directorio de debajo. El caso que importa es un
        repo dotfiles en `$HOME`, que es el que absorbe todos los proyectos del home:
        identidad compartida significa el flag activo, los borradores y el historial de
        consultas de uno aplicados a los demas, sin forma de saber a que proyecto
        pertenencia. **Su propio comentario decia que lo previene.** Arreglado: root se
        inspecciona solo cuando `dir` **es** root.
      - Lo demas de este fichero son las formas reales de un checkout: `.git` directorio,
        `.git` fichero con `gitdir:` relativo y absoluto, `commondir` presente y
        ausente, submodulo, directorio que no es repo. Un submodulo **no** comparte
        identidad con el superproyecto: si la compartiera, la identidad del
        superproyecto cambiaria con el submodulo.

- [x] **`config/project_state.go` y `store/query_history.go`.** El estado
      activo/inactivo y el historial son tres lineas cada metodo, y lo que estaba sin
      cover eran justo los casos donde el disco no coopera. Lo que importa no es que
      rompan: es que **un fichero de estado que decodifica a un mapa vacio hace que los
      tres proyectos que apagaste lean como encendidos, y no hay forma de saber que el
      fichero decia otra cosa**. Por eso `{"inactive_projects": null}` tiene su propio
      test: un mapa nil lee bien y **panica al escribir**, que es justo lo que haria el
      usuario al cambiar un proyecto por primera vez.
      - La migracion del historial global copia y luego **renombra** a `.bak`, no borra: un
        fallo entre los dos perderia el original. Y un proyecto que ya tiene fichero no
        se toca, porque el suyo es mas nuevo y el global es una sobra.

- [x] **`explorerpreview/tabbar.go` al 100%.** Solo se manejó a traves del panel que lo
      posee, asi que sus dos envueltes nunca se llegaron a probar en ninguno de los dos
      extremos. Ahora el anillo se recorre entero en las dos direcciones contando
      visitas distintas, que es la unica forma de cazar un envuelve que envuelve al sitio
      equivocado.

- [x] **Ronda 12 — cobertura 89.8% -> 91.4%, `internal/app` 65% -> 78%.** El objetivo
      era `app.go` (513 statements sin cubrir, el archivo mas grande del repo). Salieron
      **cinco bugs**, y dos de ellos por el mismo motivo que los de la ronda 11.

- [x] **`copyToClipboard` en X11 no daba el contenido al proceso.** El case hacia
      `xclip` **antes** de asignarle `Stdin`; con `Stdin` nil, exec le da al hijo el
      dispositivo nulo, `xclip` lee EOF, **no copia nada y sale con codigo 0** — y la
      funcion devolvia **exito sobre un portapapeles vacio**. Toast de "copiado" y el
      usuario pegaba cadena vacia. Ahora cada intento pasa por `clipVia`, que engancha el
      contenido **antes** de correr. Mismo patron de las seis llaves muertas: el efecto se
      notaba, la causa no. El test mete un stub en el PATH que escribe su stdin a un
      fichero, asi que lo que se comprueba es lo que **llego** al hijo, no lo que el padre
      pretendia darle.

- [x] **Cuatro exportadores, cuatro respuestas distintas a la misma suposicion.** "Una
      fila tiene un valor por columna" estaba escrito cuatro veces, y las cuatro estaban
      mal en direccion distinta:

      | exportador | que hacia con una fila corta |
      |---|---|
      | CSV | escribia menos campos que la cabecera — **rechazado por todo parser** |
      | JSON | indexaba la fila por indice de columna — **PANICA** |
      | SQL | escribia menos valores que columnas — **no parsea** |

      Ahora los cuatro preguntan `usableWidth` (las columnas que **todas** las filas
      tienen) antes de escribir. Truncar en vez de rellenar con NULL es la mitad honesta:
      una columna que la fila no tiene nunca se leyo de la base de datos, asi que un NULL
      inventaria un valor que la consulta no produjo.

- [x] **Un editor VACIO enviaba la consulta.** `preprocessSQL` devolvia el texto original
      **sin recortar** cuando no aplicaba ningun reescrito, asi que elWhitespace pasaba el
      `sql != ""` de `execute_query` y se mandaba a PostgreSQL. La respuesta era "empty
      query string", que es una forma confusa de aprender que pulsaste la tecla equivocada.
      - El test preexistente **afirmaba** que `preprocessSQL("   ")` volvia sin cambios, y
        ahora hay que distinguir vacio de "desconocido": un `SET` es una sentencia que esta
        funcion no reconoce y pasarla intacta es lo correcto; el whitespace no es una
        sentencia.

- [x] **El editor de celda **panicaba** al teclear.** `handleEditKey` indexaba
      `g.widths[g.editCol]` sin guarda, asi que escribir el primer caracter en una celda
      con el grid sin dimensionar loesia con "index out of range [0] with length 0".
      - Y la razon de fondo: **el grid solo se dimensionaba como efecto secundario del
        render** — `WindowSizeMsg` lo dimensionaba todo menos al grid. Un modelo redimensionado
        pero no dibujado tenia un grid sin dimensionar. Todos los componentes se dimensionan
        ahi; el grid era el unico que no.

- [x] **`fkRefTable` se escribia y no lo leia nadie.** El sidebar enseña la fila a la que
      apunta una clave foranea, guardaba de que tabla era… y no lo decia nunca. Con las dos
      tablas compartiendo nombres de columna — `id` y `name`, que es el caso **normal** en una
      clave foranea — el panel es indistinguible de la fila actual. La etiqueta se pone ahora,
      y **antes** de la comprobacion de "sin datos": una fila referenciada vacia es
      justo el caso donde mas hace falta saber que tabla volvio sin nada.

- [x] **La cache del sidebar de FKs se prueba a traves de la cache, no de la base de
      datos.** Todo el trabajo de la funcion es *acertar la cache antes de construir la
      consulta*, y esa rama se cubre porque **devuelve antes de construir el comando**. No
      es casualidad del test: es la propiedad que merece estar fijada. Si alguien construyera
      el comando primero y consultara la cache dentro del closure, el sidebar volveria a
      consultar en cada movimiento del cursor y **nada fallaria**.
      - La cache se compara por **texto formateado**, asi que `int64(42)` y `"42"` son la
        misma entrada. Correcto aqui —el WHERE se construye formateando el mismo valor— pero
        es una comparacion laxa y se fija porque parece un bug.

- [x] **Ronda 13 — cobertura 91.4% -> 92.5%, `internal/app` 78% -> 85%.** Con un Postgres
      real levantado (el contenedor `dbx-mut-pg` de la puerta de mutacion), los closures
      que hablan con la base de datos dejan de ser inalcanzables. Dos bugs mas, y los dos
      son **cosas que existen y no dicen lo que prometen**.

- [x] **El tab de Constraints omitia TODOS los CHECK.** La query unia
      `information_schema.table_constraints` con `key_column_usage` — y
      `key_column_usage` **solo lista constraints basadas en clave**, asi que un CHECK (que
      restringe una expresion, no una clave) no tiene fila con la que unirse y desaparece.
      El efecto: una tabla cuyos CHECK no existen para dbx. Y el CHECK es exactamente la
      clase de constraint que uno olvida y luego pisa. Ahora lee `pg_constraint`, que trae
      las cuatro clases con sus columnas en `conkey`, en una sola query.
      - Un test con la query anterior pasaba: la tabla de prueba no tenia CHECK en una
        tabla donde se buscaba. Ahora hay un CHECK explicito y la asercion lo exige.

- [x] **`TableDetail.Indexes` y `.FKs` estaban declarados y nadie los escribia ni leia.**
      Los indices y las claves llegan por `ListIndexesFullBySchema` y
      `ListForeignKeysBySchema`, en mapas por esquema y tabla. Los campos del struct
      Parecian el sitio del dato y estaban siempre vacios.
      - Un campo que **parece** donde vive el dato y no lo esta es peor que no tener
        campo: el proximo que lo rellene会发现 que el export — que lee los mapas — lo ignora
        en silencio. Mismo molde que `fkRefTable`, que el sidebar guardaba y no pintaba, y
        que las seis llaves de config. **Eliminados**, y hay un test por reflexion que falla
        si vuelven.

- [x] **`deriveDBName` estaba escrito DOS veces**, en `loadSchema` y en
      `loadSchemaWithTarget`, sin helper. Es la operacion de string que produce el nombre
      de la base que ve el usuario, asi que una divergencia entre las dos copias
      significaria que la app nombra la base de una manera en la primera carga y de otra
      tras recargar por un DDL. Ahora es una funcion pura.
      - **El orden de los dos pasos no es intercambiable**: se quita el `?` **despues** del
        `/`, porque el query string de un DSN puede contener una barra
        (`?options=-c%20search_path%3Dpublic`). Fijado con un caso que lo comprueba.

- [x] **`buildSchemaExportFull` (18 statements) sin un solo test.** Es una funcion ** pura**
      —y alimenta el prompt de NL→SQL, el autocompletado y el diagrama ERE. Si pierde una
      columna, la IA escribe contra una columna que no existe; si pierde una clave foranea,
      nunca menciona la integridad referencial y propone un DELETE que deja filas huerfanas.
      - Fijado tambien que un indice o una clave de **otra** tabla no se filtra a esta: los
        mapas estan indexados por tabla y la busqueda es por nombre, asi que un error que
        usara la primera entrada del esquema daria a cada tabla los indices de la primera.

- [x] **`handleExport` (32 statements) sin cobertura.** Decide entre portapapeles y fichero
      segun **cuantas filas** hay, y la decision esta escrita dos veces —una por rama de
      conteo— sin test. Un fichero en el directorio equivocado se descubre con `ls`; un
      yank que escribiera fichero en vez de copiar deja el portapapeles del siguiente copy
      del usuario en disco; y **un `switch` de formato al que le falta un brazo produce un
      fichero VACIO**, que parece un export correcto.
      - La rama de fichero se prueba con el PATH **sin ninguna herramienta de
        portapapeles**: que el PATH este vacio ES la asercion de que esa rama escribe
        fichero.
      - Y el numero de tuplas del SQL, no el de sentencias: este export **hoista la lista
        de columnas una vez**, asi que contar `");` da siempre cero, y la primera version
        de esa asercion pasaba con cualquier contenido.

- [x] **Fijado como esta, sin "arreglar": una tabla que ya no existe produce un pane VACIO.**
      Las tres consultas de catalogo devuelven cero filas para una tabla que no esta, y cero
      filas no es un error. El overview es el unico que falla, y `loadMetadata` se come ese
      error a proposito. El usuario llega aqui seleccionando una tabla que otra sesion
      solto; un pane vacio es la respuesta honesta, y la alternativa es un toast de error
      por algo que el usuario no hizo. La primera version afirmaba que llegaba un error y
      fallo; lo interesante es lo que **no** debe hacer: devolver datos de la tabla
      anterior.

- [x] **Ronda 14 — cobertura 92.5% -> 93.7%, `app.go` de 370 a 213 statements sin cubrir.**
      Los handlers de mensajes que **escriben en la base de datos**, probados contra el
      Postgres real. Ningun bug de app en esta ronda: los tres handlers hacen lo que dicen.
      Lo que si aparecio fueron **tres fixtures mias equivocadas**, y eso tambien se
      escribe.

- [x] **`GridCommitPendingMsg` / `GridCommitAllMsg`: los argumentos van LIGADOS, y un fallo
      para el LOTE.** Lo que se afirma no es "devolvio algo" sino:
      - Un valor **con comilla** llega como dato, no interpolado. Hay una fila con
        `it's here` y se lee de vuelta intacta. Ese es el motivo de que el mensaje lleve
        `Args` aparte de `Queries`.
      - De tres sentencias, la **anterior** al fallo se ejecuto y la **posterior no**. Esa
        asimetria es toda la afirmacion: un lote que corre tres y para en la segunda ha
        dejado la base en un estado que el usuario no ve y no puede deshacer desde la app.
      - Sin conexion no hace nada; con cero sentencias recarga y no es un error.

- [x] **`GridNavigateFKMsg`: seis campos en la pila de navegacion, y el filtro se COMBINA.**
      - Un `ScrollCol` que se cae trae al usuario a la tabla correcta, la fila correcta y el
        filtro correcto, pero horizontalmente a otro sitio — y eso se lee como que la app
        ha perdido el lugar.
      - El filtro existente se combina con `AND (...)` en vez de ser reemplazado: el usuario
        filtro `orders`, siguio una clave foranea, y al volver debe recuperar el filtro.
        Reemplazarlo ampliaria en silencio la tabla que estaba mirando. **Comprobado contra
        la base de datos**, porque la combinacion se construye en un WHERE que el loader
        ejecuta y nada mas lo revisa.

- [x] **`GridPreviewExpandFKMsg`**: la fila llega como **MAPA por nombre de columna**, porque
      el preview la mezcla en un objeto anidado y una slice posicional habria que
      emparejar por posicion. Y un valor sin fila referenciada es un **error**, no un pane
      vacio: el caso real es una clave foranea cuya fila destino fue borrada.

- [x] **Tres fixtures mias, y por que el handler tenia razon las tres veces:**
      - Los INSERT omitian `user_id`, que es NOT NULL. El handler **rechazo cada uno con el
        error de la propia base** — que es exactamente el comportamiento que se estaba
        probando. Dos rondas，这一次 para averiguar si la culpa era del handler o del test.
      - `filter_rows` se **rechaza en una grid sin datos**, porque no hay nada que filtrar.
      - Una clave foranea a una columna **entera** con un valor string falla por tipo
        ("invalid input syntax for type integer") **antes** de que el escapado de comillas
        importe. La primera version de ese test uso la columna equivocada y leyo un fallo de
        tipo como un WHERE mal formado. Ahora hay una FK a una columna de texto, que es
        donde el escapado importa de verdad.

- [x] **Ronda 15 — cobertura 93.7% -> 94.1%.** Dos clusters puros: la aritmetica del popup
      del autocompletado y el cableado a mano de las teclas del picker. **Ningun bug de app**,
      pero una limitacion latente encontrada y un bloque entero que se documenta como
      inalcanzable por el registry.

- [x] **`renderEditor` + los helpers de overlay.** `renderEditor` reparte el panel entre el
      editor y el popup, y cada reparto es aritmetica con un clamp. Un popup una fila mas
      alto empuja el texto que el usuario esta escribiendo una fila en cada pulsacion; un
      cap equivocado muestra menos sugerencias de las que las flechas pueden alcanzar.
      - El cap es de **quince filas** (`count > 15`), y `popupLines` es `count + 2`. Con
        treinta columnas el popup tiene 49 elementos y aun asi el panel no crece.
      - **`h - 4 - popupLines` puede dar NEGATIVO.** Un panel de 12 filas deja al editor en
        −9. Probado en 4/6/10/12/20/21/24/40 filas: no entra en panic y el render no
        excede la altura.

- [x] **Los clamps de `overlay` cubren el DESPLAZAMIENTO, no el TAMANO.** `x` e `y`
      negativos se corrigen a 0 — y sin eso un indice negativo en un slice es un panic, no un
      problema estetico. Pero una caja **mas ancha que el marco se desborda**: la linea pasa
      a tener las columnas de la caja. En una terminal una linea mas ancha que la ventana
      **envuelve**, asi que todas las columnas a su derecha bajan una fila.
      - **Latente, no vivo**: todos los callers (modal de ayuda, palette, export picker)
        dimensionan la caja desde la ventana. Fijado como limite con los callers nombrados,
        porque arreglarlo exige decidir si una caja sobredimensionada se trunca o se tira, y
        ninguna de las dos es obviamente correcta.

- [x] **`abs(math.MinInt)` no puede ser positivo.** En un `int` de 64 bits, `-MinInt`
      desborda a `MinInt`. Fijado como limite con la razon: solo hay un input mal y es el que
      aparece al comparar dos coordenadas en el borde de la direccion — inalcanzable desde
      una terminal. Arreglarlo exige ampliar el tipo de retorno y no hay caller que lo
      necesite.

- [x] **El picker, el error y la carga cablean teclas FUERA del registry.** `q`, `ctrl+c`,
      `r`, `enter` y `esc` son literales dentro de `Update`, antes de que la maquina de
      estados llegue a nada que despache por `defaultActions()`. Es deliberado —se ejecuta
      antes de que haya un pane enfocado— y tiene una consecuencia: **cambiar `q` en el
      registry no lo cambia aqui**, en las tres pantallas donde no hay otra tecla que pulsar.
      - Fijado como una lista de literales presentes, con la consecuencia escrita. Si alguien
        mueve el bloque al registry, la asercion deja de coincidir y hay que decidir si fue a
        proposito.
      - **En `StateLoading`, `q` NO cierra la app**: abandona la base de datos y vuelve al
        picker. La eleccion es sorprendente y por eso queda fijada — el siguiente que lea ese
        bloque va a preguntarse.
      - En `StateError`, `esc` pone **las dos** banderas: `connectCancelled` (para que la
        respuesta que llegue se descarte) y `forcePicker` (para que el siguiente escaneo no
        reconecte solo). `r` limpia `connectCancelled` al entrar: un retry que no la limpia
        seria cancelado por la misma bandera que el usuario uso para cancelar el intento
        anterior, y pareceria no hacer nada.

- [x] **Ronda 16 — cobertura 94.1% -> 94.9%.** Dos bigotes de drift mas (catorce guardas
      de "hay datos?" y dos caminantes de ruta JSON) y **cuatro bugs reales**, tres de
      ellos de la familia del drift.

- [x] **`nl2sql`: un rechazo en PROSA llegaba a `execute_query` como si fuera SQL.**
      `cleanSQL` quita vallas de codigo y recorta; no distingue una frase de una
      sentencia. El guardia existente solo comparaba con `""`.
      - Peor: **`cleanSQL` se llamaba desde seis sitios y solo DOS guardaban.** Anthropic,
        deepseek, openai y qwen pasaban cualquier contenido de mensaje tal cual — cuatro
        de los seis proveedores llegaban a execute_query con una frase en ingles.
      - Arreglo: **`requireSQL(provider, raw)` es ahora la única puerta** entre el mensaje
        del modelo y el editor. Seis llamadas, una invariante.
      - `looksLikeSQL` escanea **linea por linea** buscando una que empiece por palabra
        clave de sentencia. La primera version miraba solo la primera palabra y rechazaba
        `"Here you go:\nSELECT 1"` — la forma mas comun que devuelve un modelo real.
        Canjeaba un answer erroneo por otro peor: un rechazo nunca detectado.
      - Los casi-aciertos van fijados: `SELECTED`, `SETTINGS`, `MyRESET`, `CREATED`.

- [x] **`extractDDLTableName`: dos disagreementos entre las dos ramas de comillas.**
      - **`CREATE TABLE "MySchema".users` devolvia tabla vacia** — tras un schema entrecomillado
        nunca se leia una tabla sin comillas, asi que navegaba a ninguna parte.
      - **`sales."MyTable"` devolvia el nombre CON las comillas**, `"MyTable"`, que no es
        una tabla que exista, mientras el mismo statement sin schema devolvia `MyTable`.
      - **`DROP TABLE "users` (comilla sin cerrar) devolvia `"users`.** `takeName` ahora
        trata `"` como fin de nombre, asi que devuelve el prefijo vacio. Un test previo
        afirmaba que el fragmento era inocuo porque llevaba comillas — cierto, y tambien
        un argumento para no devolverlo: el siguiente paso del llamador es navegar con
        ese nombre.
      - `unquoteName` es la respuesta unica a "esto va entrecomillado", y rechaza la
        comilla sin cerrar en vez de devolver un fragmento.

- [x] **`resolveValue` y `navigateJSON` eran dos caminantes de la misma ruta.** El primero
      partia en "." y hacia busquedas de mapa a secas, asi que `items[0]` no resolvia
      ahi y si en todas partes demas. `resolveValue` ahora delega en `navigateJSON`.

- [x] **`parseKeyFromLine` moria en la primera comilla, incluso si estaba escapada.** Una
      clave como `say \"hi\"` — que el propio `json.MarshalIndent` del preview produce —
      devolvia el fragmento `say \`. El preview ahora escapa saltando el caracter
      escapado.

- [x] **`renderJQSuggestions` dibujaba una caja vacia de tres lineas.** La guarda
      `len(jqSugs) > 0` estaba solo en el sitio de llamada; ahora esta tambien dentro, asi
      que la funcion es total.

- [x] **`clipboardAttempts(goos, wayland)`: la decision de plataforma es ahora dato.** Era
      un `switch runtime.GOOS` dentro de `copyToClipboard`, asi que las ramas de darwin y
      windows no eran alcanzables desde un test en Linux — y el ORDEN, que es todo el
      contenido de la funcion, no se podia comprobar. `clipViaAny` separado tambien deja
      alcanzable `lastErr == nil` con lista vacia, que es un error y no un exito.

- [x] **Catorce copias de dos guardas en `table.go` reducidas a `hasRows` / `hasColumns`.**
      Cuatro de `data == nil || len(Rows) == 0` y seis de `data == nil || len(columns) == 0`.
      Son DOS predicados a proposito: una tabla con columnas y sin filas es real, y
      insertarle la primera fila tiene que funcionar ahi.

- [x] **`rowsAfterOffset`: tres copias del `total - offset` con clamp.** Solo `visibleRows`
      comprobaba `g.data == nil`; `cursorRowType` y `pendingInsertIndex` lo desreferenciaban
      sin guarda. Una definicion.

- [x] **`refs` se calculaba ANTES del corte por `;` en el autocompletado.** La clausula ya
      se truncaba, las referencias de tablas no: `SELECT * FROM users; SELECT |` ofrecia
      las columnas de `users` dentro de una sentencia que no puede verla. El corte se hace
      una vez, sobre los tokens.

- [x] **`store.MigrateGlobalHistory`: el brazo de error de `WriteFile` es inalcanzable**
      poniendo un directorio en la ruta del proyecto — `os.Stat` lo encuentra y la PRIMERA
      guarda devuelve nil. Fijado, porque alguien escribira un test que espera ese error y
      no lo puede obtener.

- [x] **Errores mios, seis en total.** La de mas coste: **`g.grid.Focus()` no es el foco del
      router.** Una version de los guards del modal pasaba porque el cursor de la tabla no
      se movia — un cursor que no se mueve tanto si el modal esta abierto como si el teclado
      nunca llegara. `focusedGrid` ahora afirma su premisa con un test antes de devolver el
      modelo.

- [x] **Ronda 17 — cobertura 94.9% -> 95.2%.** Ciclo de vida de la conexion y traduccion de
      mensajes. **Dos bugs reales mas**, y una confirmacion de que el codigo era correcto
      donde yo suponia lo contrario.

- [x] **`dbConnectedMsg` no actualizaba `m.project`.** El handler leia `msg.project` — bien,
      porque `connectToDB` estampa en la respuesta el proyecto al que se conecto de verdad
      — pero **nunca lo escribia de vuelta en el modelo**. Un modelo cuyo `m.project` hubiera
      cambiado mientras la conexion estaba en vuelo cargaba el esquema de la base a la que
      llego mientras siguiera llamandose STALE: el panel de esquema, el titulo y toda
      busqueda posterior por proyecto usaban un proyecto al que nunca se conecto.
      - Fijado con dos proyectos distintos en el mensaje y en el modelo.

- [x] **Un error de conexion no paraba el spinner; el de carga de esquema si.** El brazo de
      `dbConnectedMsg` nunca tocaba `spinnerActive`, y veinte lineas mas abajo el de
      `schemaLoadedMsg` lo hacia. Misma decision escrita dos veces, y la copia que faltaba
      es la que dispara PRIMERO: la app se quedaba en la pantalla de error con un spinner
      ticking y reemitiendo su comando para siempre.

- [x] **`connectCancelled` se consume exactamente una vez.** Fijado como "el modelo queda
      utilizable": el flag se limpia, la siguiente respuesta NO se descarta, y la app llega
      a la vista principal. Un flag que sobrevive a la respuesta que lo consumio mataba la
      conexion siguiente — la que el usuario elige a proposito — y la app parecia no poder
      conectarse a nada.
      - Y el orden importa: el chequeo de cancelacion va ANTES del chequeo de error, asi que
        una conexion cancelada que fallo se descarta COMPLETA. La primera version de este
        caso afirmaba que el spinner se paraba ahi, lo que habria exigido mover el manejo de
        error por encima del cancel — y entonces una conexion cancelada que fallaba
        deixaba al usuario en la pantalla de error con el fallo que habia abandonado.

- [x] **`MigrateGlobalHistory` y `extractDDLTableName`: los defaults y las comillas ya
      cubiertos en la ronda 16.**

- [x] **Confirmaion: el handler de `dbConnectedMsg` SI usa `msg.project`, no `m.project`.**
      Yo asumi lo contrario por memoria de un grep y "encontre" un bug que no existia. El
      test habia construido `dbConnectedMsg{conn: conn}` sin proyecto — un mensaje que
      `connectToDB` no puede producir. Se cunto como ** septimo error mio en cinco rondas.

- [x] **Ronda 18 — cobertura 95.2% -> 95.3%.** Indices de bytes del editor, errores de
      escritura del CLI, y la busqueda del DSN local. **Tres bugs reales.**

- [x] **`findLocalDSN` devolvia una conexion ARBITRARIA de un mapa.** `ProjectConfig.
      Connections` es `map[string]ProjectConnection`, y `for _, c := range cfg.Connections
      { return c.GetDSN() }` devuelve un elemento arbitrario: Go randomiza la iteracion de
      mapas, asi que un proyecto con dos conexiones se conectaba a **una base de datos
      distinta en cada invocacion**.
      - Ahora ordena los nombres y toma el primero alfabeticamente. Fijado con veinte
        iteraciones, que es lo que hace visible una regresion: la primera version del test
        afirmaba "el primero de varios" escribiendo el orden, y pasaba o fallaba segun la
        corrida.
      - **El flag `--connection` se ignoraba dentro de un proyecto.** Se pasaba solo a la
        rama del config global, asi que `--connection zeta` desde un proyecto con
        `.dbx.toml` conectaba a lo que diera el mapa. El mismo comando se comportaba de
        otra forma segun existiera o no un archivo de proyecto.
      - Un nombre que no esta en el archivo devuelve vacio en vez de caer al default:
        caer conectaria a la base equivocada diciendo haber usado la nombrada.

- [x] **`ctrl+u` dejaba el editor sin marcar como modificado.** Usa `Clear()`, que pone
      `modified = false` — correcto para la accion `clear_editor`, que DESCARTA el buffer.
      `ctrl+u` es una edicion: el usuario borro una linea y espera que el editor este
      sucio. Sin el cambio, una app que tiene volcado el SQL de unos borradores sin
      confirmar trata el buffer limpiado como intacto.

- [x] **Los seis brazos de error de escritura del CLI.** Eran `if _, err := fmt.Fprintf(...);
      err != nil { return err }` — la forma que un cleanup "simplifica" a una llamada sin
      comprobar, sin que nada falle. Con un `io.Writer` que falla siempre (el mismo seam
      que permite leer los bytes exactos) los seis quedan alcanzables de una vez.
      - Lo que se traga un error de escritura aqui: `dbx query` imprime la cabecera, la
        terminal se va a mitad de escritura, y el comando sale con 0 habiendo reportado
        exito de una salida que nadie recibio. Peor que un fallo visible, porque un script
        que encadena la salida no puede notarlo.

- [x] **Ronda 19 — cobertura 95.3% -> 96.6%.** El fake de Postgres y las cajas del
      diagrama ER. **Un bug real, el septimo de la familia byte-vs-rune.**

- [x] **`internal/testsupport/pgxfake` estaba al 0% y es codigo de produccion.** Se usa
      desde `internal/drivers/postgres/query_test.go`, asi que el perfil — que es por
      paquete — no lo contaba. Era un tercio de lo que quedaba sin cubrir.
      - **Un fake equivocado hace pasar a otro test por la razon equivocada.** Si `assign`
        devuelve un valor cero donde pgx devolveria una fecha, los tests del driver siguen
        en verde: estan afirmando contra el fake, y el equivocado es el fake. Eso solo
        aparece contra un servidor real, que es justo lo que el paquete existe para evitar.
      - La propiedad de la que depende todo lo demas: **un valor que no se puede convertir
        es un ERROR, nunca un cero silencioso.** Un cero silencioso hace que un test del
        driver afirme algo cierto sobre el fake y falso sobre PostgreSQL.
      - Y la aritmetica sutil: **`Sequential` cuenta solo las consultas que NINGUN Step
        respondio.** Si esa cuenta estuviera mal, cada fixture despues del primer step
        derivaria en uno y leeria la respuesta equivocada.

- [x] **Septimo caso de la familia byte-vs-rune: el diagrama ER.** Siete slices por indice
      de byte en `ere.go` (`name[:MaxTableName]`, `entry[:innerWidth]`, `label[:
      innerWidth]`, `fkEntry[:innerWidth]`...) haciendo el mismo trabajo, y **un indice de
      bytes no es un ancho de pantalla**. Una columna llamada "日本" producia una caja cuyo
      propio marco era UTF-8 invalido. No entra en panic y no parece un error: parece un
      problema de fuente.
      - Dos funciones segun el contrato que cada sitio tenia de verdad: `truncateToWidth`
        (con elipsis DENTRO del presupuesto) y `cutToWidth` (corte duro). El corte duro
        estaba en el test suite y se respeta; el ancho, no.
      - Un test existente **fijaba el bug**: esperaba `named(13)` —16 columnas contra un
        ancho interior de 18— porque el selector "► " son cuatro bytes y dos columnas, asi
        que el slice por bytes llegaba dos columnas corto y la caja rellenaba con
        espacios. Los casos ASCII de alrededor no cambiaron, que es la senal: un slice por
        bytes y uno por columnas solo discrepan cuando hay un caracter multibyte.
      - Oncea instancia de la familia, siete arregladas.

- [x] **Ronda 20 — cobertura 96.7% -> 97.0%.** Vallas de codigo, modo jq del preview, y
      `dbx pipe` de punta a punta. **Cuatro bugs reales**, dos de ellos cambiando la
      sentencia que la app ejecuta.

- [x] **`nl2sql`: una valla de codigo se comia la primera palabra de la sentencia.**
      ```DELETE\nFROM t``` devolvia `"FROM t"`; ```SELECT\n1``` devolvia `"1"`.
      - **La app ejecutaba `FROM t`, reportaba exito, y no borraba nada.** El usuario pedia
        un borrado y la app decia que lo habia hecho.
      - La causa era una **discrepancia entre el lector y el llamador**: el lector decia
        "la primera linea es una etiqueta" y, cuando la respuesta era no, el llamador caia
        a "toma todo despues del primer salto" — que es lo correcto para una valla cuya
        siguiente linea es la sentencia, y exactamente lo que come una sentencia que
        empieza en la linea siguiente a las comillas invertidas.
      - Arreglo: **una sola funcion decide.** La primera linea se queda si parece una
        sentencia, leida con el MISMO predicado que decide si un mensaje entero es SQL.
        `sql` y `json` no son sentencias, se quitan. `DELETE` y `WITH` si, se quedan. Nada
        que mantener, y las dos decisiones no pueden discrepar porque son la misma.
      - Un test existente **fijaba el bug de al lado**: `named(13)` —16 columnas contra un
        ancho interior de 18— porque el glifo "► " son cuatro bytes y dos columnas.

- [x] **El prose alrededor de la valla no se quitaba, aunque el comentario promete que si.**
      El codigo exigia la valla en la posicion 0; la forma mas comun que devuelve un modelo
      es "Aqui tienes:\n```sql\n...". La prosa, las comillas invertidas y la etiqueta
      llegaban al panel de ASK. Y el cierre era un `TrimSuffix` condicional, asi que el
      prose posterior Filtraba el marcador.
      - Tomar el CONTENIDO es lo unico que funciona: no hay forma de quitar un prefijo de
        longitud desconocida sin saber donde acaba, y la valla es el unico marcador.

- [x] **`gridpreview`: `[]` no iteraba — devolvia el elemento 0, igual que `[0]`.**
      Dos entradas de la barra de sugerencias que hacen exactamente lo mismo, y
      `.tags[] == "y"` contestaba solo por el primer elemento. Ahora `[]` devuelve el array
      entero, que es el equivalente honesto de iterar en un caminante de valor unico.

- [x] **`gridpreview`: `length` se ofrecia y no resolvia NUNCA.** La barra lo sugiere y el
      caminante no lo implementa, asi que aceptar la sugerencia producia un filtro que no
      coincidia con nada y renderizaba un documento vacio — sin error en ningun sitio,
      porque "resolvio a nil" es la misma respuesta que "la ruta esta mal".

- [x] **`ask.SetContextHint` estaba muerto.** Tres campos escritos y nunca leidos, un setter
      que solo los escribe, y un sitio de llamada. La app guarda su propia
      `askContextHint`, que si se usa. Mismo caso que `TableDetail.Indexes`: eliminado.

- [x] **`CompletionSchema` y `CompletionOperator` son arms inalcanzables.** `detectContext`
      asigna Empty, Table, SelectList, Column, Keyword y Value — nunca esos dos. El arm de
      operadores alimenta `defaultOperators` — trece entradas con descripciones legibles
      como "equals" — a un llamador que no puede pedirlas.
      - **Eso no es codigo muerto: es una CARACTERISTICA que no funciona.** Los datos
        existen, el arm existe, y lo unico que falta es una clausula en `detectContext`.
      - Fijado con la nota y el punto de cableado exacto, sin cablearlo: anadirlo cambia lo
        que el popup muestra, que es una decision de producto y no un arreglo de cobertura.

- [x] **Ronda 21 — cobertura 97.0% -> 97.3%.** Guards del grid, constructor del diagrama ER,
      y el fake de Postgres otra vez. **Un bug real mas.**

- [x] **`scrollRow` negativo hacia panear el renderizador.** `startRow := offset +
      g.scrollRow` entraba directo al bucle de filas y `g.data.Rows[-1]` reventaba:
      **la pantalla entera se caia**, no una fila equivocada.
      - No es alcanzable hoy: las cinco asignaciones de `scrollRow` clampean. Pero "todas
        clampean" es un invariante sostenido en cinco sitios por cinco expresiones
        distintas, y el bucle de filas es el unico sitio que sabe que un indice negativo
        es fatal.
      - **Un clamp en la frontera** —donde se calcula `startRow`— en vez de auditar cinco
        llamadores cada vez que se anade un sexto. Y no se clampea en cada funcion de
        movimiento: eso seria el patron de drift.

- [x] **`dbx pipe` de punta a punta contra un Postgres real.** Tres guards, y el primero no
      lo puede alcanzar un archivo regular porque no es un dispositivo de caracteres —
      `/dev/null` si lo es.
      - El caso existente de "terminal vacio" **llega al chequeo y lo pasa de largo**: cubre
        el lado del pipe vacio y nada mas, que es lo que su propio comentario ya decia.
      - `readSQL` sobre un lector que entrega datos junto con `io.EOF` —la forma normal en
        que un pipe termina— y sobre uno que falla a mitad. Un error de lectura a mitad
        devuelve **nada**, no lo que.collection: ejecutar una sentencia que el usuario
        termino de escribir es peor que no ejecutar nada.

- [x] **Los errores de escritura de los comandos de listado.** Los seis brazos de
      `writeQueryResult` mas los de `listTables`/`listColumns`/`schema`, todos con el mismo
      `io.Writer` inyectable.
      - La propiedad sin contar lineas: **las posiciones que reportan el error son un
        PREFIJO** de la secuencia de escrituras. Un hueco en medio es un brazo que se traga
        su error, que es lo que se busca.
      - La primera version contaba las lineas a mano leyendo el formateador, y fallo por
        dos de cuatro comandos: el conteo depende de cuantos esquemas y tablas tenga el
        fixture.

- [x] **El filtro de columnas se traga toda tecla que no nombra — y eso esta bien.** Su
      switch tiene esc, enter, backspace y un default que anade caracteres imprimibles. Leido
      solo, parece que un filtro que come `ctrl+c` deja al usuario sin poder salir.
      - **No puede pasar, y el motivo esta una capa arriba**: la app resuelve TODA tecla
        ligada y la despacha antes de darsela al pane enfocado. `ctrl+c` esta ligada a
        `quit`, que la app reclama. El grid no necesita saber que teclasClaims la app ya.
      - Mi hipotesis era falsa; el codigo era correcto. La leccion de siempre: **un test que
        alcanza un estado que produccion no puede producir, sospeche del test primero**.

- [x] **`pgxfake`: `Next` ES el avance, no "hay fila".** No existe "antes de la primera
      fila": la primera llamada mueve `pos` a 1 y `Values` lee `Rows[0]`. Por eso `Values`
      indexa `Rows[pos-1]` y por eso llamarla antes de `Next` leeria fila -1.
      - **`Scan` es asimetrico a proposito**: menos destinos que columnas es leer un
        subconjunto y esta bien; mas destinos que columnas es una consulta que pidio una
        columna que no recibio, y rellenar con ceros dejaria pasar eso.
      - **`ArgContains` SI estrecha en la ruta escalar**, por el argumento variadico — y no
        lo **registra**. Mi primer test afirmaba lo contrario en ambas cosas.

- [x] **Ronda 22 — cobertura 97.4% -> 97.5%.** Guards de la app probados por su EFECTO, y
      el segundo proveedor de NL→SQL. **Dos cosas documentadas, un comentario corregido.**

- [x] **`ui.Zones` es un singleton de paquete con registro ASINCRONO.** `Scan` entrega los
      limites por un canal al bucle del manager, asi que un `Get` justo despues de `View()`
      es una CARRERA y suele devolver nil.
      - Mis tests de rueda se **saltaban a si mismos** — la zona del sidebar nunca se
        registraba — y el archivo los reportaba como cubiertos mientras dos ramas del
        routing quedaban sin probar.
      - Ahora el punto se **construye** desde las zonas: `pointInZone` busca una celda que
        pertenezca a esa zona y a ninguna otra, y el caso de "fuera de toda zona" busca una
        celda que no pertenezca a ninguna. Nada de coordenadas adivinadas.

- [x] **`NewPi` tenia la URL del endpoint HARDCODEADA en el constructor**, asi que los seis
      brazos de error de `PiProvider.Generate` — los mismos seis que tiene
      `OpenAICompatible` — eran inalcanzables desde un test.
      - `NewPiAt(apiKey, model, baseURL)` hace la decision dato, y `NewPi` delega. El mismo
        razonamiento que `clipboardAttempts`.
      - **La familia del drift en estado puro:** un escalera de errores escrito dos veces en
        el mismo fichero, con una copia probada y otra no. Ahora la MISMA tabla corre por los
        dos proveedores y la misma propiedad se afirma en cada uno, asi que una divergencia
        es un fallo de test y no una sorpresa en produccion.

- [x] **`q`/`ctrl+c` en el bloque del picker de la app es INALCANZABLE.** El picker los
      maneja en su propio switch y devuelve `tea.Quit, true`, asi que el fallback del app
      solo se alcanza con teclas que el picker no nombra — y ninguna de esas es `q` ni
      `ctrl+c`.
      - Dos sitios responden a la misma pregunta, que es la forma que deja pudrir uno: si
        alguien quita `q` del switch del picker, la copia del app toma el relevo en
        silencio.
      - **Fijado, no eliminado**: son tres lineas de un camino de teclas, la respuesta seria
        correcta, y el set de teclas del picker es una decision de UI que ganara y perdera
        teclas — el fallback es un seguro barato. Lo que NO es aceptable es no saber cual de
        los dos vive.
      - Y `ctrl+c` tiene una TERCERA respuesta, una capa arriba: esta ligada a `quit` como
        accion de app, asi que la app la reclama antes de preguntar al picker. Igual que el
        filtro de columnas del grid, que tambien parece comer `ctrl+c` y no.

- [x] **Comentario que contradecia el codigo justo encima.** En `renderMainView`: "Previews /
      sidebar are intentionally left unmarked so clicks on them are ignored" — y la linea
      siguiente hace `ui.Mark(zonePaneGridSidebar, ...)`. El sidebar existe para ser
      hovereado, y el wheel rutea al pane bajo el cursor, que no puede funcionar sin zona.
      - Los previews (grid y explorer) si quedan sin marcar; el sidebar no.

- [x] **`ask.SetContextHint` sigue muerto** — ya fijado en la ronda 20.

- [x] **Ronda 23 — cobertura 97.5% -> 97.6%.** Un bug real de txn, un bug real de
      teclado, y el refactor que hace alcanzable el manejo de errores de la app.

- [x] **UN COMMIT FALLIDO SE ANUNCIABA COMO UN COMMIT EXITOSO.** Tres partes tenian que ser
      ciertas para que el usuario viera la contradiccion:
      - `commitPending` devolvia `(true, err)` — el flag significa "hubo commit y funciono",
        y con `true` en el fallo el handler lo traducía en un toast de exito.
      - `executeQuery` hacia `committed = committed || committedNow`.
      - El handler de `queryExecutedMsg` hacia el toast de "Transaction committed" y
        **despues** miraba `msg.err`.

      El sintoma, reproducido por el test al revertir el arreglo:

      ```
      ✓ Transaction committed   |   ✗ Query failed: context deadline exceeded
      ```

      **Arreglado en las tres partes**, porque las tres son el defecto: `false` en el
      fallo de commit, y el error se responde PRIMERO en el handler.

- [x] **Un test mio llevaba tres rondas afirmando la semantica equivocada.**
      `TestCommitPending_CommitFailurePropagatesAndClears` decia `if !committed { t.Error }`
      con el comentario "there was one" — que es cierto y no es lo que el flag es para.
      Un test escrito para coincidir con el codigo es justo lo que un test no debe ser.

- [x] **`Model.conn` paso de `*pgx.Conn` a la interfaz `postgres.Conn`.** Una base viva no
      puede fallar UNA sentencia y responder la siguiente bajo demanda, asi que los brazos de
      error de la app eran inalcanzables salvo rompiendo un contenedor. La interfaz ya existia
      con un comentario que prometia justo ese uso; la app era el ultimo que la usaba solo
      para el camino feliz.
      - `Exec` anadido a `postgres.Conn` (el app lo usa), `postgres.TxBeginner` nuevo para el
        runner, `pgxfake.Conn` gana `Exec`/`ExecErr`/`Execs`/`Asked`/`AskedCount`.
      - `newStatementRunner(nil)` ahora construye un runner sin conexion en vez de hacer
        panic al enlazar `conn.Begin` — un estado legitimo para mantener el flag readOnly
        sin base, y el panic estaba en la construccion, no en el uso.
      - El handler de `dbConnectedMsg` hace type assertion a `TxBeginner` y falla **visible**:
        un runner nil significa "cada sentencia se niega" mientras la UI dice "Connected".

- [x] **`internal/cli/cli_test.go` tiene un `fakeConn` DUPLICADO de `pgxfake.Conn`.** Los
      dos implementan la misma interfaz con fixtures que se solapan. Le aparece `Exec` a los
      dos esta ronda porque la interfaz crecio; los dosVan a crecer cada vez que la interfaz
      crezca.
      - **Para la ronda que toque**: borrar el de `internal/cli` y usar `pgxfake`. Las
        diferencias (perStatement, name, scanErr, pingErr, pinged) se expresan con `Steps`,
        `RowName`, `RowValues`, `RowErr` y `PingErr`.

- [x] **El filtro de columnas del grid se comia `?`.** `?` se escribia como `?` literal en
      el filtro, `:` como `:` y `U` como `U` — mientras el filtro de tabla del explorer y el
      input jq si los dejaban pasar. El propio comentario del codigo arguesa lo contrario
      ("`?` es la tecla a la que se recurre cuando uno esta confundido, y eso es justamente
      cuando se filtra"); el carve-out se aplico a dos de tres inputs con la misma forma.
      - `context := m.router.Context()` subido al principio del bloque `StateMain`; antes se
        declaraba tres bloques mas abajo, asi que ningun bloque de arriba podia preguntar.
      - Perder `U` no cuesta nada: `jumpToBestMatch` pasa a minusculas columna y query.
      - **El filtro WHERE NO debe recibirlo**, y por eso esta escrito: construye un
        fragmento SQL y `?` es el bind parameter de PostgreSQL. El "fix de consistencia"
        rompe la filtracion parametrizada. Escrito para que la proxima no lo "arregle".

- [x] **De los cuatro `if handled {return} / return m, nil` del camino de teclas, solo el del
      filtro WHERE puede dispararse.** `handleFilterKey`, `handleEditKey` y el del explorer
      terminan en un default que devuelve `true` para TODA tecla, asi que la mitad "stop" de
      esos tres bloques no puede correr. Solo el filtro WHERE devuelve `false` (j/k sin
      popup). Tres bloques de codigo muerto, fijados con su razon.
      - Y `ctrl+c` no hace nada en los CINCO inputs de texto — decision coherente, no bug:
        `quit` esta ligado a `pageViews`, que excluye el contexto del editor.

- [x] **Medido, no supuesto: `ui.Zones` es un singleton de paquete con registro ASINCRONO.**
      `Scan` entrega los limites por un canal al bucle del manager, asi que un `Get` justo
      despues de `View()` es una carrera. Los tests de rueda se saltaban a si mismos y el
      archivo los reportaba como cubiertos. Ahora el punto se CONSTRUYE desde las zonas
      (`pointInZone` busca una celda que pertenezca a esa zona y a ninguna otra).

- [x] **Ronda 24 — cobertura 97.6% -> 97.8%.** Un panic latente en el editor de celdas, y
      seis guardas del grid fijadas con su aritmética.

- [x] **`runeAtBefore` panicaba con un indice pasado el final.** Comprobaba solo `i <= 0`,
      asi que `s[:i]` con `i > len(s)` tumbo el proceso. La hermana `runeAt`, tres lineas
      arriba, comprueba LOS DOS extremos para el mismo proposito — dos funciones
      adyacentes con el mismo trabajo, una con el guard completo y otra con la mitad.
      Decima instancia de la familia byte-vs-rune.
      - **No es alcanzable desde una tecla hoy**: todo escritor de `editCursor` lo mantiene
        en `[0, len(editValue)]`. Fix preventivo, y el test lo dice.
      - El unico llamador es el backspace del editor de celda, cuyo trabajo es cortar ahi.
      - Para `"a🎉b"` el byte 2 esta DENTRO del emoji y RuneError es la respuesta correcta;
        los limites son 1 y 5. Mi primera version lo dio por vuelto.

- [x] **Seis guardas del grid NO pueden dispararse**, y dos de ellas cuelgan de UNA sola
      linea:
      - `dispatchAction` rechaza TODA accion cuando `len(g.data.Rows) == 0`, antes de llegar
        al caso de cada accion. Eso hace inalcanzables la guarda `row == nil` de
        `startDelete` y la `fkInfo == nil` de `navigateFK` — y explica por que `navigateFK`
        indexa `g.data.Rows` sin comprobar: es seguro exactamente por eso.
        **El refactor que hay que vigilar**: estrechar esa comprobacion a una lista de
        acciones que necesitan filas volveria vivas las dos guardas y dejaria el indice sin
        proteccion.
      - Las tres guardas `cursorRow < 0` (moveToLast, halfPageDown, clampCursor) tienen la
        misma forma `scrollRow = totalRows - contentHeight; cursorRow = totalRows - scrollRow - 1`,
        que da `contentHeight - 1` o `totalRows - 1`, ambos >= 0 porque los dos casos
        degenerados hacen return temprano.
      - `size == 0` en `runeAtBefore` se sigue del guard de arriba: `0 < i <= len(s)`.

- [ ] **Para la ronda que toque**: `internal/app/app.go` sigue con ~60 sentencias
      sin cubrir, casi todas brazos de error de mensajes que ahora SI son alcanzables con
      `pgxfake` inyectado en `m.conn`. El siguiente bloque grande es el panel `ask`
      (`m.ask == nil` aparece cuatro veces) y `rollbackOnExit` (el `conn.Close` sin
      comprobar que la conexion siga viva).

- [x] **ARREGLADO: tras un commit fallido la conexion puede estar muerta y la app no lo
      comprueba.** pgx v5 cierra TODA la conexion cuando un COMMIT falla y el servidor no
      esta IDLE, y devuelve el resultado a un modelo que sigue sosteniendola. El usuario
      veia "Query failed" y luego cada sentencia posterior fallaba igual, sin nada que
      dijera que la conexion se habia ido.
      - **La respuesta NO puede ser "un commit fallido significa conexion muerta"**: si el
        commit llego y solo se perdio el acuse, la conexion esta bien y el DML **esta
        guardado**. Los dos casos dan el mismo texto de error, asi que la app **pregunta**.
      - `queryExecutedMsg` lleva `commitFailed`; el handler delega en
        `handleCommitFailure`, que consulta `statementRunner.usable(ctx)`.
        - conexion muerta -> suelta `m.conn` y `m.runner`, dice como volver a conectar.
          El **proyecto se conserva**, asi que `switch_connection` esta a una tecla.
        - conexion viva -> la conserva y dice "the transaction may or may not have been
          committed", que es exactamente lo cierto en ese caso.
      - `Ping` anadido a `postgres.Conn`; `statementRunner` gana un campo `ping` y
        `usable()`, que responde **true** cuando no hay hook: nada queda probado perdido, y
        tirar una conexion sin pruebas es peor que quedarse con ella.
      - El `rollbackOnExit` se beneficia en cascada: con `m.conn == nil` ya no intenta
        cerrar una conexion muerta.

- [x] **Ronda 25 — hacia 100%. 97.8% -> 98.1%.** main.go, config e internal/ai al 100%.

- [x] **`main()` era "imposible" de probar y no lo era.** Mi primer intento fue un subproceso
      con `GOCOVERDIR` y no funciono (los contadores no se fusionan). La razon real por la que
      un test no puede llamar a `main()` es `os.Exit`, y `cli.Execute()` SOLO sale cuando el
      comando devuelve error — que es exactamente lo que dice el comentario del propio
      `execute()`. Con `--help` se llama en proceso. main.go al 100%.

- [x] **Cuatro proveedores de NL->SQL con el endpoint cocido en `Generate`.** Anthropic,
      Qwen, DeepSeek y OpenAI tenian el literal dentro del metodo, igual que Pi antes de
      `NewPiAt`. Los cinco tienen ahora su `At`. **9 sentencias** que eran inalcanzables.
      - Las constantes `*DefaultBaseURL` son la BASE y el constructor anade la version, como
        Pi y `OpenAICompatible`. La de Anthropic la puse como endpoint completo y daba
        `.../v1/v1/messages`.

- [x] **UN TEST MIO HIZO UNA LLAMADA DE RED REAL.** Anadi el seam a Qwen pero **olvide
      sustituir su `NewRequestWithContext`**; el test apunto a `dashscope.aliyuncs.com` con
      la key "key" y reporto el 401 que le devolvieron. `everyProvider` ahora hace **panic**
      si el baseURL no es `127.0.0.1` — un test que puede salir a produccion sale, y en
      silencio.

- [ ] **FIJADO: la guarda `entry.Info()` de los dos bucles del session logger es
      inalcanzable.** `os.ReadDir`'s `DirEntry.Info()` llama **LSTAT**, que NO sigue symlinks
      — un symlink colgante se stat'ea bien y nunca llega a la guarda. Hacerla fallar pide
      EACCES sobre el directorio, y eso falla para **todas** las entradas a la vez, asi que
      no demuestra "skip one, keep the rest". El disparador real es que el fichero desaparezca
      entre el ReadDir y el Info: una carrera que ningun test puede agendar sin un seam.
      Se queda porque quitarla convierte una carrera benigna en un crash de la pantalla de
      historial.

- [x] **Dos guardas muertas eliminadas**, cada una con su prueba escrita antes:
      - `project_state.go`: `json.MarshalIndent` sobre un `map[string]bool` no puede fallar.
      - `repoid.go`: el terminador del bucle de `findDotGit` — se llega a el solo si
        `pathWithin` es cierto, o sea un descendiente propio de root, y `filepath.Dir` solo
        devuelve su propio argumento para `/`. La terminacion viene de la profundidad finita
        del path, y `TestTheTwoGuardsThatCannotBeReached` afirma la premisa para que el
        argumento no se pudra.

- [x] **Ronda 26 — 98.3% -> 98.6%.** `main`, `config`, `internal/ai`, `internal/ui`,
      `bordered`, `ask`, `palette`, `editor`, `gridpreview` al 100%. Ocho guardas muertas
      eliminadas, cada una sustituida por algo que SI se prueba.

- [x] **Nueve copias del debug-log unificadas** en `internal/debuglog`. Seis ficheros, tres
      grafias de flags de OpenFile, dos estilos de error. Cuatro de las nueve tenian un brazo
      `if err != nil { return }` INALCANZABLE: solo se dispara si /tmp no es escribible.
      La ruta como ARGUMENTO hace el fallo testeable nombrando una ruta que no se abre.
      Los flags en tres ordenes eran EQUIVALENTES; lo que habia divergido era el estilo.

- [x] **`isWideRune` no cubria emoji.** Un 🎉 se media como una celda, la linea se desviaba
      una columna por cada uno y el borde del toast cruzaba el texto. Anadidos
      0x1F300-0x1F5FF, 0x1F600-0x1F9FF, 0x1FA70-0x1FAFF. **Los dingbats (0x2600-0x27BF) se
      dejan fuera a proposito**: son de ancho variable segun la fuente, y adivinar "ancho"
      romperia todos los toasts con un ✓.

- [x] **El rango U+1100-U+115F de `isWideRune` estaba ENTERO dentro de `unicode.Hangul`.**
      Medidos los 96 puntos de codigo: CERO fuera. La rama no podia responder nunca.
      `TestTheJamoRangeIsRedundant` vuelve a medirlo.

- [x] **`if size <= 0` tras `utf8.DecodeRuneInString` es INALCANZABLE en tres sitios.**
      Esa funcion devuelve `(RuneError, 1)` para CUALQUIER encoding invalido, asi que size
      nunca es 0 — y el comentario de `nextRuneEnd` describia un wedge que utf8 ya impedia.
      Medido: `"\x80"`, `"\xc3"`, `"\xe2\x82"`, `"\xf0\x9f"`, `"\xff\xfe"` → size 1.

- [x] **SEIS guardas mas, del mismo patron: "un piso aplicado antes hace innecesario el
      piso posterior".** Alguien escribe el segundo como si el primero no existiera.
      - `querybrowser.maxVisibleEntries`: modalH tiene piso 10, luego h >= 6.
      - `ask.View`: modalH tiene piso 10, luego innerH >= 8.
      - `gridpreview`: maxWidth tiene piso 30, luego pathWidth >= 16.
      - `palette.View`: el clamp `scrollOffset > totalLines-maxVisible` no puede firing,
        porque la rama del cursor SIEMPRE corre y escribe como maximo ese valor.
      - `gridpreview.isExpandableFK`: `strings.Split` nunca devuelve slice vacio.
      - `editor.acceptCompletion`: se llega solo con el popup visible, y toda mutacion de
        la linea refresca el span, asi que nunca puede estar stale.
      - `editor.applyFilter`: el skip de keyword exacto en la rama de prefijo vacio no puede
        firing (prefijo vacio implica token vacio, que es la primera mitad de la condicion).

- [x] **`CompletionSchema` NUNCA se asigna como kind de CONTEXTO.** Casi lo cambie para
      cubrirlo — un qualifier desconocido ofrecia COLUMNAS en vez de esquemas — y **los tests
      existentes tenian una razon escrita**: "un qualifier desconocido se trata como tabla sin
      cualificar". Revertido. La rama muerta se elimino y el comentario de `qualifiedContext`
      dice ahora que volveria si esa decision cambia.

- [ ] **Sin resolver, con las mediciones puestas:**
      - `session/logger.go`: los dos `entry.Info()` son guardas TOCTOU. `DirEntry.Info()`
        llama **LSTAT**, que NO sigue symlinks, asi que un symlink colgante se stat'ea bien;
        y los permisos fallan para TODAS las entradas a la vez. **La unica forma de
        dispararlo es que el fichero desaparezca entre el ReadDir y el Info**, que es una
        carrera. Refactor posible: inyectar el `os.ReadDir` como dependencia y un seam en el
        bucle; noworth it para dos ramas.
      - `config/repoid.go`: `canonicalPath` cae a `Clean(path)` si `filepath.Abs` falla, que
        solo pasa cuando `os.Getwd` falla (cwd borrado). Inyectar el `Getwd` lo haria
        testeable.

- [x] **Ronda 27 — 98.6% -> 98.9%.** `explorerpreview` y `cli` al 99%+. Seis guardas
      muertas mas, un bug real de scroll, y las seams de la CLI por fin ejecutadas.

- [x] **BUG REAL: el clamp del scroll del ERE dejaba el panel EN BLANCO.** `end` se calculaba
      antes de clampar `start`, y los dos se clampeaban a `len(lines)` — dejando
      `lines[len:len]`. Un offset de scroll que sobreviviera a un diagrama mas alto vaciaba el
      panel en vez de mostrar su ultima pantalla, justo mientras el usuario scrollea.
      Arreglado clampeando `start` a `len(lines) - paneHeight`, que es lo que significa
      "scrolleado al final".

- [x] **`seenIncoming` de `BuildERDiagram` era un map sobre las CLAVES de otro map.** Las
      claves de un `map[string][]FK` son unicas por definicion, asi que cada `tableName` se
      visitaba una vez y el guard no podia firing. Las dos claves que nombran el mismo destino
      son DOS claves distintas, y las dos se dibujan legitimamente.

- [x] **Cuatro guards de padding muertos en `ere.go`**: `cutToWidth` y `truncateToWidth` ya
      capan a `innerWidth`, asi que `innerWidth - width` nunca es negativo. En el cuarto si
      hacia el truncate — la diferencia que el par ocultaba.

- [x] **`ERDiagramNav.MoveDown` tenia un `else if activeRow == -1` inalcanzable**: `count == 0`
      hace return dos lineas antes, asi que llegar al else significa `activeRow >= count-1 >= 0`,
      que es justo lo que el arm comprobaba.

- [x] **Las seams por defecto de la CLI nunca se ejecutaban.** `buildModel` y `runProgram` se
      sustituyen en TODOS los tests, asi que los closures de produccion — dos lineas cuyo
      comentario afirma "nothing here changes behaviour" — nunca corrian.
      - Se capturan en el init del paquete (`productionBuildModel = buildModel`), que ocurre
        antes de que ningun test pueda sustituirlas. Llamar a la variable desde un test ejecuta
        lo que tenga, que tras el primer test es un stub; asignar una copia del default y
        llamar a esa copia no prueba nada del default.
      - `runProgram` necesita `/dev/tty`: se salta sin terminal controladora, que es lo
        honesto.

- [x] **`getConnection` ahora es alcanzable con un fake.** El seam `pgxConnect` devuelve
      `postgres.Conn + Ping`, que es exactamente `pgxfake.Conn`. Los tres casos (conexion
      rechazada, ping fallido,Conexion que se cierra)}y el walk de esquema de `dbx ask`
      (`ListTables` y `ListColumns` fallando uno cada uno)estan probados.
      - **Las fixtures del loader deben coincidir con el SQL REAL**: `ListTables` escanea TRES
        columnas, y una fixture de dos es un error de aridad que el walk trata como skip — asi
        que una fixture de dos produce un esquema vacio y el caso pasa por la razon equivocada.

- [x] **`dbx ask --sql-only` no conecta, y ahora está probado.** La bandera existe para eso:
      conectar primero haria fallar el comando a quien no tenga base de datos. El test pone un
      `pgxConnect` que FALLA, asi que un runAsk que conectara primero reportaria error.

- [x] **Ronda 28 — 99.3% -> 99.6%.** `config`, `store`, `pgxfake`, `editor`, `gridpreview`,
      `explorerpreview`, `ask`, `palette`, `bordered`, `ui` al 100%. `cli` al 99.4%.

- [x] **BUG REAL en `pgxfake`: `case *bool` comparaba `== "true"`, asi que `"t"` — como lo
      ESCRIBE Postgres — se escaneaba como FALSE.** Un fixture copiado de psql quedaba
      silenciosamente false. Ahora acepta `true`/`TRUE`/`t`/`T`. Los spellings de ENTRADA
      (`yes`, `on`, `1`) se dejan en false a proposito: el test de contrato existente lo
      pinaba, y ahi un "yes" equivocado produce una sorpresa VISIBLE en vez de un false
      silencioso.

- [x] **`copyToClipboard` es ahora una VARIABLE de paquete.** Tres call sites (export, yank,
      copy-SQL) y ninguno testeable: la unica forma de que falle es una maquina sin helper de
      portapapeles, asi que el rama de fallo nunca se ejecutaba en ningun sitio — en una
      maquina de desarrollo nunca, y en CI por la razon equivocada. Mismo truco que
      `loadConfig` en la CLI.

- [x] **`pinToLastRow`: tres copias byte a byte del mismo bloque** en `moveToLast`,
      `halfPageDown` y `clampCursor`, y los dos pisos ya habian empezado a diferir entre las
      copias. Unificado. El clamp de `cursorRow` resulto MUERTO (`contentHeight()` tiene piso
      de 1); el de `scrollRow` vive.

- [x] **`Picker.height` se escribia y nunca se leia.** El popup se dimensiona por su
      contenido; solo `width` tiene opinion. Se fueron el campo, el setter, y la llamada del
      app en cada resize — un valor mantenido a mano en cada redimensionado para nada.

- [x] **Ocho guardas muertas mas**, cada una con su medicion o su invariante:
      - `assign` (pgxfake): tres `if v == nil` DENTRO de cases, cuando la primera linea ya
        devuelve para nil.
      - `where_filter.detectContext`: `len(tokens)==0` inalcanzable (input vacio rechazado
        arriba, `extractLastClause` no puede devolver vacio, `tokenize` siempre da >=1 token).
      - `runeAtBefore`: `size == 0` tras `DecodeLastRuneInString` — el guard de `i <= 0`
        hace que `s[:i]` nunca sea vacio.
      - `json.MarshalIndent` en `store/query_history.go` — la MISMA guarda copiada en tres
        ficheros, ninguno se pregunto nunca si el tipo podia fallar.

- [x] **`canonicalPath` con el directorio de trabajo borrado.** `filepath.Abs` falla solo si
      `os.Getwd` falla, y eso se consigue con `os.Chdir(tmp)` + `os.RemoveAll(tmp)`. Es el
      estado de una shell cuyo directorio borro alguien. Igual aplica a `findLocalDSN`.

- [x] **Ronda 29 — 99.6% -> 99.9%. Las ultimas 7 sentencias, TODAS clasificadas.**
      Este es el estado final: 7 de ~15000 sin cubrir, y ninguna es un agujero sin explicar.

- [x] **`app.go` a 1: el `Ping` tras un `pgx.Connect` exitoso.** No hay seam — el connect llama
      a pgx de verdad — y la unica forma de que `Ping` falle es un socket que ACEPTA y luego no
      contesta. El brazo vecino (puerto cerrado, que es lo que un usuario se encuentra) SI esta
      cubierto y dice "failed to connect". **FIJADO** en
      `internal/app/unreachable_branches_pinned_test.go` con el call site y lo que costaria.

- [x] **`logger.go:124` y `:169`, los dos `entry.Info()`.** Guardas TOCTOU: solo se disparan si
      el fichero desaparece ENTRE el ReadDir y el Info. Un symlink colgante NO las alcanza
      (`DirEntry.Info()` hace LSTAT, no stat) y los permisos fallan para TODAS las entradas a la
      vez — el llamante lo lee como "no hay nada que limpiar". Solo una carrera las dispara.

- [x] **`cli/root.go:34`, el `runProgram` por defecto.** No es una rama: es el valor por defecto
      de una seam. Un programa real de bubbletea abre `/dev/tty`, y un proceso de test no suele
      tener terminal controladora aunque su stdin sea un character device. Su hermano
      `buildModel` SI esta cubierto (construir el modelo no necesita terminal), y esa asimetria es
      la razon de que uno llevara rondando sin cubrir mientras el otro se ejercitaba cada ronda.

- [x] **`postgres/query.go:67`, un `rows.Values()` que falla.** Necesita que pgx no pueda
      decodificar un campo — un OID desconocido o corrupcion a mitad de fila. La fila es un
      `pgx.Rows` real, y `pgxfake` tiene las suyas propias; hacerlas fallar es implementar un
      segundo decoder. El brazo vecino (un fallo de `Next()`, que `pgxfake` reproduce con
      `IterErr`) SI esta cubierto.

- [x] **`grid/table.go:959` y `:1079`, MUERTAS y no inalcanzables.** `dispatchAction` rechaza
      TODA accion cuando `len(g.data.Rows) == 0`, asi que `startDelete`'s `row == nil` y
      `navigateFK`'s `fkInfo == nil` no se pueden alcanzar. Ya fijadas en
      `grid/refusals_contract_test.go`.

## Ronda 5 — el estado tras bajar los cinco ficheros al 0%

Cobertura global: **68.2%** (era 59% al empezar esta ronda). Gate: **48 gated, 24
excluded**. Cinco bugs reales encontrados y corregidos por el camino, todos de la misma
familia — una operacion hecha por bytes donde deberia ser por runas, o una aritmetica
duplicada en dos sitios que se separan.

Lo que queda, por tamaño de hueco:

| fichero | stmt sin cubrir | % |
|---|---|---|
| `internal/app/app.go` | 1088 | 24% |
| `internal/testsupport/pgxfake/fake.go` | 148 | 0% (fake, nunca gated por diseno) |
| `internal/ui/components/explorer/tree.go` | 135 | 18% |
| `internal/ui/components/editor/autocomplete.go` | 133 | 69% |
| `internal/ui/components/editor/sql.go` | 127 | 53% |

`app.go` es el ciclo de vida y el router, y a 24% de 1441 statements tiene la cobertura mas
baja en valor absoluto de todo el repo. Es el que queda.

Cobertura global: **87.8%** (era 59% al empezar). Gate: **54 gated, 18 excluded**.

Quedan, por statements sin cubrir: `app.go` 507 (ya al 65%), `pgxfake` 148 (un fake, nunca
al gate por diseno), `grid/table.go` 84 (ya 93%), `nl2sql/compatible.go` 74 (61%),
`gridpreview/preview.go` 64 (ya 90%), `editor/autocomplete.go` 63 (ya 85%),
`editor/sql.go` 15 (ya 95%), `config/config.go` 43 (4%).

Los `load*` de `app.go` (~90 statements) son la parte que queda de verdad alli: son el
cuerpo de un `tea.Cmd` que hace falta una base de datos viva para ejecutar. El harness de
testcontainers ya existe (`testdb_test.go`), asi que es alcanzable — es trabajo de
integracion, no logica.

## Ronda 30 — 100%, y los cinco ultimos huecos por refactor

Cobertura global: **100.0%**. Sin una sola declaracion sin cubrir en el repositorio. Los cinco
ultimos huecos no eran de pruebas: eran llamadas que ninguna prueba puede hacer por si misma, y
cada una se resolvio con una extraccion o una inyeccion. Ninguna cambio el comportamiento
observable; una (**el cierre de la conexion tras un `Ping` fallido**) arreglo una fuga real.

Lo que hizo cada uno, y por que era la unica forma:

| hueco | refactor | que compra |
|---|---|---|
| `postgres/query.go:67` | `collectRows(rowIterator)` — el bucle de filas extraido a una funcion pura | el fallo de DECODIFICACION pasa de "necesita un servidor roto" a "necesita un fake de tres lineas" |
| `logger.go:124`/`:169` | `readDir` inyectado en `Logger` y `Reader`, con `listDir()` en los tres call sites | las dos guardas TOCTOU pasan de "solo una carrera" a un `DirEntry` cuyo `Info()` falla |
| `app.go:360` | `var pgxConnect`, igual que el de la CLI | el `Ping` de una conexion que ya abrio se puede rechazar, y **la conexion se cierra** |
| `grid/table.go:959` | `fkAtColumn(idx)`: un lookup donde habia dos | el icono se DERIVA de `foreignKeysData`, asi que "esta columna es clave" estaba escrito dos veces y la segunda copia llevaba un `nil` que no podia dispararse |
| `grid/table.go:1079` | guardia eliminada, invariante probada | `SelectedRow()` solo devuelve nil sin filas, y `dispatchAction` rechaza todo sin filas |
| `cli/root.go:34` | `programOpts []tea.ProgramOption` nombrado | con `tea.WithInput(nil)` el cuerpo **de produccion** se ejecuta de verdad; solo se cambia la terminal por un fichero |

Tres cosas que estas refactorizaciones enseñan sobre las anteriores:

1. **Una seam no es una solo.** The one call a test cannot make for itself, put a variable in front
   of it. `pgxConnect` en la app, `programOpts` en la CLI, `readDir` en el logger, `rowIterator`
   en el driver: cuatro de los cinco ultimos huecos eran exactamente eso. La quinta
   (`grid/table.go`) era lo que queda cuando el hueco NO es una llamada sino una **redundancia**:
   el mismo hecho escrito dos veces y una de las dos copias con un guardia que no podia dispararse.
2. **Un seam que media tabla no usa no es un seam.** `readDir` esta en `Logger` y en `Reader`, y
   los TRES call sites pasan por `listDir()` en vez de llamar a `os.ReadDir` — si uno se hubiera
   quedado directo, la guardia de ese call site seguiria sin poder alcanzarse y habria parecido
   un hueco resuelto que no lo esta.
3. **Un guard eliminado vale lo que vale el test que lo sustituye.** El de `startDelete` no se
   borro por argumento: se borro y se escribio `TestDeletingEveryRowWorks`, que recorre el
   cursor hasta los dos extremos de una rejilla paginada y borra en cada parada. Un argumento sin
   test es un argumento que se pierde en el primer refactor.

### Lo que la puerta de mutacion dice ahora (y lo que NO dice)

`make mutate`: 3504 mutantes, 2993 killed, 442 lived, eficacia **87.13%**, cobertura de mutadores
**98.28%**. Cero `NOT_COVERED` — coherente con el 100% de statements.

Pero el gate de CI compara el **diff** contra `.mutation-allowlist`, y ahi hay dos hechos que
conviene no perder de vista:

- **356 supervivientes no listados** en `.mutation-allowlist`. No son deuda de esta ronda: son
  deuda del modulo entero, concentrada en `gridpreview/preview.go` (70), `grid/table.go` (46),
  `editor/autocomplete.go` (44), `explorer/tree.go` (24). El gate es diff-scoped y
  `mutation.yml` **no corre en push a `main`** (solo `pull_request` y `workflow_dispatch`, y
  deliberadamente no es required), asi que un push directo no lo dispara.
- **133 entradas del allowlist quedan obsoletas** (listan mutantes que las pruebas ya matan).
  Un allowlist que solo crece es un allowlist que deja de significar nada.

Ninguna de las dos cosas se ha tocado aqui a proposito: son la siguiente unidad de trabajo, no
esta. Registrar la cifra es lo que importa; arreglarla de a mezclas mientras secia lo que el
allowlist prohibe.

## Ronda 31 — el bucle de mutacion, y por que NO se borra el allowlist

Cobertura sigue en **100%**. Esta ronda no es de cobertura: es de la puerta de mutacion, que
llevaba **356 supervivientes no listados** y por tanto haria fallar cualquier PR.

### 1. Borrar el allowlist apaga la puerta. No la reinicia.

Medido, no supuesto. `mutation.yml:154-157` sin el fichero:

```
- gate NOT calibrated: .mutation-allowlist is missing, so this run reports only and cannot block
exit 0
```

Ademas `mutation.yml` corre solo en `pull_request` y `workflow_dispatch`, y **no** en push a
`main`. Borrarlo = 356 mutantes vivos sin mirar y sin avisar, y nadie se entera porque el
workflow tampoco corre al pushear. Lo que si funciona es **podarlo**: 174 entradas -> las 41
que realmente sobreviven. La puerta da el mismo resultado (356 nuevos bloquean igual) sin 133
mentiras, y una entrada que lista un mutante que las pruebas ya matan dice "esto se examino" de
algo que nadie examino dos veces.

### 2. El bucle es `commit -> mutate-diff -> arreglar -> commit`, NO `cambio -> mutate-diff`

Medido tambien. La regla es:

```make
git diff --name-only $(MUTATE_BASE)...HEAD | grep -q '\.go$'
```

Tres puntos, contra `HEAD`. Consecuencias:

| momento | ficheros que ve |
|---|---|
| cambio sin commitear | **0** |
| commiteado, sin pushear | 1 |
| pusheado (`HEAD == origin/main`) | **0** |

Asi que un cambio sin commitear no se muta, y tras pushear tampoco. El paso va **despues** del
commit y **antes** del push.

### 3. Y mutate-diff muta LINEAS cambiadas, no ficheros enteros

Esto es lo que hace el bucle barato. `gridpreview` tenia 84 mutantes vivos en el modulo
entero; el diff de esa rondarodujo **13 mutantes en total**, porque solo se mutan las lineas
tocadas. 12 muertos, 1 equivalente allowlisted. **6 segundos** de run.

Consecuencia practica: el bucle por commit es barato, y la deuda de 356 vive en lineas que
nadie ha tocado. Cada PR mueve su propio diff contra la puerta; reducing la deuda requiere
tocar deliberadamente cada fichero — que es justo lo que hace este bucle.

### 4. La ronda: gridpreview

84 supervivientes, **0 allowlisted** — el fichero era invisible para la puerta. Cuatro eran
condiciones muertas (`jqSugVisible && len(jqSugs) > 0`, un hecho escrito dos veces en cinco
sitios). Al quitarlas, el test del invariante encontro un agujero **de mi propio cambio**: `up`
pasaba por delante de una lista vacia a `len-1`, que es `-1`, y `acceptJQSuggestion` ya sabia
eso con una tercera redaccion de la misma regla. Los tres pasan ahora por
`moveJQSuggestionSelection`.

Tres hallazgos **fijados, no corregidos**, cada uno con su punto de cableado:

- El conjunto de caracteres del scanner de numeros tiene `t` y `Z` pero ni `T` ni `:`, asi que
  un timestamp ISO se pinta como cuatro numeros separados con una `T` suelta en medio.
  Completarlo significa anadir `:` al conjunto, que tambien se tragaria el colon de `{"a":1}`.
- **Un panel, dos alturas de contenido**: `Render` dibuja `height-4` y `ensureCursorVisible`
  desplaza como si fueran `height-6`. El error va en la direccion segura, que es probablemente
  por lo que sobrevive.
- Un tipo mas ancho que su columna **envuelve** la caja una fila mas alta, porque la columna de
  ruta recorta y la de tipo no. Inalcanzable: el vocabulario es cerrado y su miembro mas largo
  mide diez columnas.

### trampa nueva pagada

`addToHistory` **escribe** `~/.config/dbx/jq_history.json` y un panel lo **lee** al
construirse, asi que un test que.historia sin redirigir `HOME` lee el historial del
desarrollador y deja cien entradas `.k0042` detras. `previewOf` no aísla; `isolate(t)` si. Mis
tests de historiales no lo hacian y tocaron el fichero real (que contenia solo basura de
tests, nada del usuario).
