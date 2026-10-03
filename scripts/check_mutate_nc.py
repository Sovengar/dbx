#!/usr/bin/env python3
"""Account for every mutant the tool did not evaluate.

Two statuses mean the same thing — the tool never ran the mutant, so nothing is
known about it — and neither is a kill or a survivor, so a check that only looks at
survivors passes either way:

  NOT COVERED  gremlins believes the surrounding code was never executed.
  TIMED OUT    the mutant makes the code loop forever and the harness gave up.

`make mutate` used to print both counts and pass anyway, so an untested branch could
sit here indefinitely with nothing but a number in a CI comment. A TIMED OUT mutant
is worse still: it is absent from the counted totals, so it makes the gate's
efficacy figure go UP.

THE TWO ARE CHECKED DIFFERENTLY, AND THE REASON IS NOT COSMETIC.

NOT COVERED is DETERMINISTIC, so it is checked as an exact set. A new entry is a
red build, and so is an entry that stops appearing — which is how a suppressed gap
is stopped from quietly becoming a real one.

TIMED OUT is NOT deterministic. Whether a hang is caught before the harness's own
budget expires depends on how many tests reach the mutated path and on CPU
contention with the other workers. internal/ui/components/editor/highlight.go
reported 9 timeouts on one run and 7 on the next with the identical suite. So the
exact set cannot be a gate without producing random CI failures, and what is checked
instead is a CEILING per file: more hangs than recorded is a regression, fewer is
fine, and the set is printed for information.

The underlying problem is a property of Go, not of any suite: an infinite loop
cannot be interrupted, so a test can only detect the hang and fail, and detecting it
costs the watchdog's whole budget. See .mutation-notcovered group 4.
"""

import json
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
REPORT = ROOT / "report.json"
BASELINE = ROOT / ".mutation-notcovered"

# Keys:  <file>:<line>:<mutator>              for NOT COVERED, an exact set.
#        TIMEOUT_MAX <file> <n> <why>         for TIMED OUT, a ceiling per file.
PREFIX_TIMEOUT_MAX = "TIMEOUT_MAX"


def load_baseline():
    exact = set()
    ceilings = {}
    for raw in BASELINE.read_text(encoding="utf-8").splitlines():
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        if line.startswith(PREFIX_TIMEOUT_MAX):
            parts = line.split(None, 3)
            if len(parts) < 3:
                raise SystemExit(f"{BASELINE}: malformed timeout ceiling: {line}")
            ceilings[parts[1]] = (int(parts[2]), parts[3] if len(parts) > 3 else "")
            continue
        exact.add(line)
    return exact, ceilings


def scan(report_path: Path):
    data = json.loads(report_path.read_text(encoding="utf-8"))
    not_covered = set()
    timed_out = {}
    for f in data.get("files", []):
        name = f["file_name"]
        for m in f.get("mutations", []):
            status = m.get("status")
            if status == "NOT COVERED":
                not_covered.add(f"{name}:{m['line']}:{m['type']}")
            elif status == "TIMED OUT":
                timed_out.setdefault(name, []).append(f"{m['type']} at {m['line']}:{m['column']}")
    return not_covered, timed_out


def main() -> int:
    if not REPORT.exists():
        print(f"UNEVALUATED: {REPORT} not found; run `make mutate` first", file=sys.stderr)
        return 1

    expected_nc, ceilings = load_baseline()
    actual_nc, actual_to = scan(REPORT)

    failed = False

    new_nc = sorted(actual_nc - expected_nc)
    gone_nc = sorted(expected_nc - actual_nc)
    for entry in new_nc:
        print(f"NEW NOT COVERED: {entry}")
        print("  a mutant here was never run. Cover the branch with a test, or add the")
        print("  entry to .mutation-notcovered with the reason it cannot be evaluated.")
    for entry in gone_nc:
        print(f"now covered: {entry}")
    if new_nc:
        print(f"\n{len(new_nc)} new NOT COVERED mutant(s)")
        failed = True

    # Every file with a timeout must have a recorded ceiling, and must not exceed
    # it. An unrecorded file with timeouts is the interesting case: it means a new
    # hang appeared and nobody wrote down why.
    for name in sorted(actual_to):
        got = len(actual_to[name])
        if name not in ceilings:
            print(f"NEW TIMED OUT FILE: {name} has {got} unevaluated mutant(s)")
            for entry in actual_to[name]:
                print(f"    {entry}")
            print("  add a TIMEOUT_MAX line to .mutation-notcovered saying why they cannot")
            print("  be killed, or make the loop advance so a test can.")
            failed = True
            continue
        want, why = ceilings[name]
        if got > want:
            print(f"TIMED OUT REGRESSION: {name} has {got}, recorded ceiling is {want} ({why})")
            for entry in actual_to[name]:
                print(f"    {entry}")
            failed = True
        else:
            print(f"timeouts: {name} {got} (ceiling {want}) {why}")

    if failed:
        print("\nUNEVALUATED FAILED")
        return 1

    print(
        f"UNEVALUATED OK: {len(actual_nc)} NOT COVERED (exact set) and "
        f"{sum(len(v) for v in actual_to.values())} TIMED OUT (under ceiling), all accounted for"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
