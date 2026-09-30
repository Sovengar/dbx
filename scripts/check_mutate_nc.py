#!/usr/bin/env python3
"""Fail when a mutant is reported NOT COVERED without a written reason.

WHY THIS EXISTS
---------------
A NOT COVERED mutant is one gremlins chose not to run, because it believes the
code around it was never executed. Nothing in the gate failed on it: the summary
printed the count and the run passed. So a mutant could sit in an untested branch
indefinitely and the only symptom would be a number in a CI comment.

That is the same shape of hazard as a TIMED OUT mutant, which is absent from
report.json entirely and therefore indistinguishable from a killed one. Both are
"the tool did not evaluate this", and both used to be invisible.

WHAT IS ACTUALLY WRONG
----------------------
gremlins mis-attributes coverage for the `case` lines of a `switch { ... }`. Go's
cover tool attributes a case expression's coverage to the case BODY, so the case
line itself has no block of its own and a per-line coverage lookup finds nothing.
`go tool cover` reported exactly 3 uncovered blocks in internal/app/txn.go; gremlins
reported 25 uncovered MUTANTS, all 25 of them sitting on case lines.

Hand-applying them proved it: txn.go:384:26 (`i+1 < len(sql)` -> `i+2 < len(sql)`
in the line-comment case) is KILLED by the existing suite. gremlins never ran it.

So this script is not a formality. Each entry below is a NOT COVERED mutant that is
either genuinely uncovered (and needs a test) or provably equivalent (and needs a
proof). The file is a BASELINE, not a suppression list: a new entry is a red build,
and removing one is a deliberate edit with a reason.
"""

import json
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
REPORT = ROOT / "report.json"
BASELINE = ROOT / ".mutation-notcovered"

def load_baseline() -> set:
    out = set()
    for line in BASELINE.read_text(encoding="utf-8").splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        out.add(line)
    return out


def actual_not_covered(report_path: Path) -> set:
    data = json.loads(report_path.read_text(encoding="utf-8"))
    out = set()
    for f in data.get("files", []):
        name = f["file_name"]
        for m in f.get("mutations", []):
            if m.get("status") == "NOT COVERED":
                out.add(f"{name}:{m['line']}:{m['type']}")
    return out


def main() -> int:
    if not REPORT.exists():
        print(f"NOT_COVERED: {REPORT} not found; run `make mutate` first", file=sys.stderr)
        return 1

    expected = load_baseline()
    actual = actual_not_covered(REPORT)

    # A case-line mutant can be reported at the same line with several columns, so
    # collapse to file:line:type which is what the baseline is keyed on.
    new = sorted(actual - expected)
    gone = sorted(expected - actual)

    for entry in new:
        print(f"NEW NOT COVERED: {entry}")
        print("  a mutant here was never run. Either cover the branch with a test or")
        print("  add the entry to .mutation-notcovered with the reason it is unkillable.")

    if new:
        print(f"\nNOT_COVERED FAILED: {len(new)} new uncovered mutant(s), {len(actual)} total")
        return 1

    for entry in gone:
        print(f"now covered: {entry}")
    print(f"NOT_COVERED OK: {len(actual)} uncovered, all accounted for")
    return 0


if __name__ == "__main__":
    sys.exit(main())
