#!/usr/bin/env python3
"""Check MUTATE_EXCLUDE against the intended in/out set of every Go file.

The regexp is an EXCLUSION list, so a file is gated only when nothing in the
list matches it. Getting that wrong in either direction is bad: a file that
should be gated but is not is a silent hole, and a file that should be out but
is in turns the gate into a check that rarely fails.

Run from the repo root:  python3 scripts/check_mutate_scope.py
"""
import re
import subprocess
import sys
from pathlib import Path

# Read the value out of the Makefile rather than duplicating it here, so this
# check cannot drift from what `make mutate` actually runs.
makefile = Path("Makefile").read_text(encoding="utf-8")
m = re.search(r"^MUTATE_EXCLUDE \?= (.*)$", makefile, re.MULTILINE)
if not m:
    sys.exit("could not find MUTATE_EXCLUDE in the Makefile")
raw = m.group(1)
value = re.search(r"'\$\(MUTATE_EXCLUDE\)'", raw)
pattern = raw.replace("'$(MUTATE_EXCLUDE)'", "").strip()
rx = re.compile(pattern)

# The intended gate scope. Each entry is (path, gated?) where gated=True means
# the file IS in the mutation gate, i.e. it must NOT match the exclusion regexp.
#
# Keep this list explicit rather than deriving it, so that admitting a new file
# is a deliberate act: the diff shows both sides.
EXPECTED = [
    # already gated
    # 4% -> above 90%. See the compatible.go note for the bugs; the ones here are
    # StateDir's discarded error, the dead DBX_* overrides, and the six config keys
    # that are declared with a default and read by NOTHING (statusbar_help,
    # history_size, query_history_path, password_env, retention_days, session.enabled)
    # — reported, not removed, because each is a product decision.
    ("internal/config/config.go", True),
    ("internal/config/keybindings.go", True),
    ("internal/config/keybindings_actions.go", True),
    ("internal/config/project.go", True),
    ("internal/config/project_state.go", True),
    ("internal/config/repoid.go", True),
    ("internal/config/scanner.go", True),
    ("internal/store/query_history.go", True),
    ("internal/ui/bordered/bordered.go", True),
    ("internal/ui/keybindspane.go", True),
    ("internal/ui/modal.go", True),
    ("internal/ui/toast.go", True),
    ("internal/ui/zones.go", True),
    ("internal/app/txn.go", True),
    ("internal/app/router.go", True),
    # newly admitted
    # internal/debuglog/debuglog.go was created by the round that collapsed nine
    # duplicate log functions into one. It does NOT match MUTATE_EXCLUDE, so by this
    # script's own rule it belongs to the gate -- and leaving it unclassified made
    # scope=bad, which made the Mutation job's `if:` false and the gate silently
    # SKIPPED on every PR. The mismatch is why the scope step now exits 1.
    ("internal/debuglog/debuglog.go", True),
    ("internal/ui/components/explorer/node.go", True),
    ("internal/ui/components/palette/commands.go", True),
    # deliberately out
    ("internal/ui/keydisplay/keydisplay.go", False),
    ("internal/app/app.go", False),
    # explorer.go and tree.go were at 7% and 18% — 163 and 135 statements uncovered
    # against an 85-line test file — and are now at 100% with
    # explorer_contract_test.go. Four real bugs came out of it, three of them the same
    # shape: "keep the cursor inside the list" and "scroll so the cursor is visible"
    # were each open-coded in two or three places, and each copy covered a different
    # subset of the cases. One is now a Selected() that crashes the app.
    ("internal/ui/components/explorer/explorer.go", True),
    ("internal/ui/components/explorer/handled.go", False),
    ("internal/ui/components/explorer/tree.go", True),
    # palette.go was at 0% — no test touched the widget at all — and reached 98%
    # with palette_contract_test.go. Admitted on the same terms as where_filter.go:
    # the two statements left uncovered are scroll clamps that 2400 walked states
    # never reach, so the gate will report them NOT COVERED rather than hide them.
    ("internal/ui/components/palette/palette.go", True),
    # querybrowser.go was at 0% — the package had no test file at all — and reached
    # 99% with querybrowser_contract_test.go. The two statements left uncovered are a
    # floor of three that modalH's own floor of ten makes unreachable, and the
    # debug-log open guard.
    ("internal/ui/components/querybrowser/querybrowser.go", True),
    # preview.go was at 0% — nothing in the package touched the panel — and reached
    # 97% with preview_contract_test.go. The six statements left uncovered are all
    # unreachable: SetData sets centreSchema and schema together so the former's
    # fallback cannot fire, renderERE's early return on a nil diagram precedes its
    # nil-viewport return, ensureERESelectionVisible is only called when a row is
    # already selected, and the sixth is the debug-log open guard.
    ("internal/ui/components/explorerpreview/preview.go", True),
    # gridsidebarpreview/preview.go was at 0% — a package with no test file — and
    # reached 96%. The four statements left are the json.MarshalIndent guards, which a
    # channel or a function value reaches and which the tests now drive.
    ("internal/ui/components/gridsidebarpreview/preview.go", True),
    # nl2sql/provider.go was at 0% and reached 96%. The remaining statements are the
    # jcode detector's own arms, which its fixture now drives through a temporary HOME.
    ("internal/ai/nl2sql/provider.go", True),
    # gridpreview/preview.go was at 13% — a 54-line test file against 1253 lines of
    # code — and reached 90% with preview_contract_test.go. It carries its own jq
    # engine, FK expansion and a history file, and the two real bugs it turned up were
    # an index that was not a number resolving to element zero, and a suggestion list
    # that came out in Go's random map order.
    ("internal/ui/components/gridpreview/preview.go", True),
    ("internal/ui/components/gridpreview/handled.go", False),
    ("internal/ui/components/grid/consts.go", False),
    ("internal/ui/components/grid/handled.go", False),
    # newly admitted
    ("internal/ui/components/grid/mouse.go", True),
    ("internal/ui/components/grid/header.go", True),
    ("internal/ui/components/grid/export_picker.go", True),
    ("internal/ui/components/palette/fuzzy.go", True),
    ("internal/ui/components/grid/pager.go", True),
    ("internal/cli/ask.go", True),
    ("internal/cli/commands.go", True),
    ("internal/cli/context.go", True),
    ("internal/cli/pipe.go", True),
    ("internal/cli/root.go", True),
    ("internal/ai/context/schema.go", True),
    ("internal/ui/components/editor/highlight.go", True),
    ("internal/ui/components/grid/cell.go", True),
    ("internal/ui/components/picker/picker.go", True),
    ("internal/ui/components/ask/ask.go", True),
    ("internal/ui/components/grid/table.go", True),
    ("internal/ui/components/explorerpreview/ere.go", True),
    ("internal/ai/session/logger.go", True),
    # Admitted despite scoring 79.5% on its own, which is BELOW the gate's
    # overall efficacy. Excluding a file because its number is unflattering is
    # the same move as hiding a real gap in the allowlist: it reports a
    # coverage choice as a coverage fact. 53 NOT COVERED -> 0 is worth more than
    # the three points of efficacy it costs.
    ("internal/ui/components/grid/where_filter.go", True),
    ("internal/drivers/postgres/query.go", True),
    ("internal/drivers/postgres/schema.go", True),
    # test-support fakes are never gated: they exist to be exercised by
    # other packages' tests, so almost all of their code is uncovered by
    # their own (absent) test file.
    ("internal/testsupport/pgxfake/fake.go", False),
    # sql.go was at 53% and autocomplete.go at 69% — 127 and 133 statements
    # uncovered — and are now at 95% and 85% with sql_contract_test.go. The editor
    # held the sixth byte-versus-rune backspace in this repo, and unlike the five
    # before it this one is REACHABLE from the keyboard and holds the query text:
    # backspacing an accent left invalid UTF-8 that was then sent to PostgreSQL,
    # and the render showed a COPYRIGHT SIGN where the user had typed an accent.
    # autocomplete.go came along for free because the sql.go contract test drives
    # the popup it owns.
    ("internal/ui/components/editor/sql.go", True),
    ("internal/ui/components/editor/autocomplete.go", True),
    ("internal/ui/components/editor/handled.go", False),
    ("internal/ui/components/explorerpreview/handled.go", False),
    # compatible.go was at 61% (74 of 190 statements uncovered) and config.go at 4%
    # (43 of 45). Both are now above 90%. Four real bugs, none of them a coverage
    # artefact:
    #
    # 1. cleanSQL compared the fence tag case-SENSITIVELY, so a model answering
    #    ```SQL kept the word glued to the statement. A previous round pinned that
    #    as a known defect; it is fixed and the pin is now an assertion of the fix.
    # 2. Both Generate functions returned EMPTY SQL WITH A NIL ERROR for a refused
    #    or truncated answer, which reached execute_query as an empty statement: the
    #    keypress would do nothing at all.
    # 3. DetectJCodeConfig ranged over a MAP to pick the provider, so Go's randomised
    #    iteration chose the endpoint the prompt is POSTed to: forty reads of one
    #    file with three keyed providers gave zzz 28 times, aaa 6, mmm 6.
    # 4. StateDir discarded the UserHomeDir error, so with no HOME the query
    #    history path came out RELATIVE and was written into whatever directory
    #    dbx was started from. SessionDir four lines up had always fallen back to
    #    TempDir; the two sisters disagreed.
    #
    # And one thing that was not a bug but looked like one: `DBX_*` environment
    # overrides never worked. AutomaticEnv does not feed Unmarshal, AND viper
    # builds the name as DBX_UI.PAGE_SIZE with the dot, so two things were missing
    # rather than one. Every setting here is nested, so it was all of them.
    ("internal/ai/nl2sql/compatible.go", True),
    ("internal/ai/nl2sql/prompt.go", False),
    # newly admitted
    ("internal/ai/nl2sql/anthropic.go", True),
    ("internal/ai/nl2sql/deepseek.go", True),
    ("internal/ai/nl2sql/openai.go", True),
    ("internal/ai/nl2sql/qwen.go", True),
    ("internal/app/messages.go", False),
    ("internal/config/keybindings_groups.go", True),
    # theme/ was at 0% — no test in the package at all, and Resolve is the first thing
    # every startup runs — and is now at 100% with theme_contract_test.go. The
    # interesting assertion is a property, not a scenario: every colour field of Theme
    # must be set in every theme Resolve can return, written over the struct by
    # reflection so a new field fails the test until every theme sets it. A nil colour
    # does not fail loudly — lipgloss substitutes NoColor and the text renders in the
    # terminal's default, which looks like a slightly wrong theme rather than a bug.
    ("internal/theme/builtin.go", True),
    ("internal/theme/styles.go", True),
    ("internal/theme/system.go", True),
    ("internal/theme/theme.go", True),
    # explorerpreview/tabbar.go was driven only through the panel that owns it, so its
    # two wraps were never at either end of the ring. Now 100% with
    # tabbar_contract_test.go, which walks the whole ring in both directions and counts
    # distinct visits — the only way to catch a wrap that wraps to the wrong place.
    ("internal/ui/components/explorerpreview/tabbar.go", True),
    ("main.go", False),
    ("cmd/dbx/main.go", False),
]

failures = []
for path, want_gated in EXPECTED:
    excluded = bool(rx.search(path))
    gated = not excluded
    if gated != want_gated:
        failures.append(
            f"  {path}: expected {'GATED' if want_gated else 'EXCLUDED'}, "
            f"regexp makes it {'EXCLUDED' if excluded else 'GATED'}"
        )

# Every real non-test Go file must be classified by one of the two, or be
# explicitly listed above. This catches a NEW file that nobody thought about,
# which is the failure that silently rots.
tracked = {p for p, _ in EXPECTED}
all_go = {
    str(p)
    for p in subprocess.run(
        ["git", "ls-files", "*.go"], capture_output=True, text=True, check=True
    ).stdout.split()
    if not p.endswith("_test.go")
}
unclassified = sorted(all_go - tracked)
if unclassified:
    failures.append("  unclassified new files (add them to EXPECTED):")
    failures.extend(f"    {p}" for p in unclassified)

if failures:
    print("MUTATE_SCOPE MISMATCH")
    for f in failures:
        print(f)
    sys.exit(1)

gated_count = sum(1 for _, g in EXPECTED if g)
print(f"MUTATE_SCOPE OK: {gated_count} gated, {len(EXPECTED) - gated_count} excluded, "
      f"0 unclassified")
