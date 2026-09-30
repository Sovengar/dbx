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
    # newly admitted
    ("internal/ui/components/explorer/node.go", True),
    ("internal/ui/components/palette/commands.go", True),
    # deliberately out
    ("internal/ui/keydisplay/keydisplay.go", False),
    ("internal/app/app.go", False),
    ("internal/app/router.go", False),
    ("internal/ui/components/explorer/explorer.go", False),
    ("internal/ui/components/explorer/handled.go", False),
    ("internal/ui/components/explorer/tree.go", False),
    ("internal/ui/components/palette/fuzzy.go", False),
    ("internal/ui/components/palette/palette.go", False),
    ("internal/ui/components/grid/table.go", False),
    ("internal/ui/components/grid/mouse.go", False),
    ("internal/ui/components/grid/header.go", False),
    ("internal/ui/components/grid/cell.go", False),
    ("internal/ui/components/grid/pager.go", False),
    ("internal/ui/components/grid/consts.go", False),
    ("internal/ui/components/grid/handled.go", False),
    ("internal/ui/components/grid/export_picker.go", False),
    ("internal/ui/components/grid/where_filter.go", False),
    ("internal/ui/components/gridpreview/preview.go", False),
    ("internal/ui/components/gridpreview/handled.go", False),
    ("internal/ui/components/gridsidebarpreview/preview.go", False),
    ("internal/ui/components/editor/sql.go", False),
    ("internal/ui/components/editor/highlight.go", False),
    ("internal/ui/components/editor/autocomplete.go", False),
    ("internal/ui/components/editor/handled.go", False),
    ("internal/ui/components/ask/ask.go", False),
    ("internal/ui/components/picker/picker.go", False),
    ("internal/ui/components/querybrowser/querybrowser.go", False),
    ("internal/ui/components/explorerpreview/preview.go", False),
    ("internal/ui/components/explorerpreview/ere.go", False),
    ("internal/ui/components/explorerpreview/tabbar.go", False),
    ("internal/ui/components/explorerpreview/handled.go", False),
    ("internal/cli/root.go", False),
    ("internal/cli/ask.go", False),
    ("internal/cli/commands.go", False),
    ("internal/cli/context.go", False),
    ("internal/cli/pipe.go", False),
    ("internal/ai/nl2sql/provider.go", False),
    ("internal/ai/nl2sql/compatible.go", False),
    ("internal/ai/nl2sql/anthropic.go", False),
    ("internal/ai/nl2sql/deepseek.go", False),
    ("internal/ai/nl2sql/openai.go", False),
    ("internal/ai/nl2sql/qwen.go", False),
    ("internal/ai/nl2sql/prompt.go", False),
    ("internal/app/messages.go", False),
    ("internal/config/keybindings_groups.go", True),
    ("internal/theme/theme.go", False),
    ("internal/theme/styles.go", False),
    ("internal/theme/builtin.go", False),
    ("internal/theme/system.go", False),
    ("main.go", False),
    ("internal/ai/context/schema.go", False),
    ("internal/ai/session/logger.go", False),
    ("internal/drivers/postgres/query.go", False),
    ("internal/drivers/postgres/schema.go", False),
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
