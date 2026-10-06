.PHONY: build install test lint run clean check mutate mutate-diff

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X main.Version=$(VERSION)"
INSTALL_DIR := $(HOME)/.local/bin
GOLANGCI_LINT_VERSION := v2.13.2
MUTATE_BASE ?= main

# Mutation gate scope. Only code the test suite actually reaches is gated:
# uncovered code emits no mutants at all, so including it would give a green
# check that cannot fail.
#
# Gated today: internal/config, internal/store, internal/ui/bordered, the
# internal/ui ROOT files (keybindspane, modal, toast, zones), and internal/app's
# txn.go. internal/ui's component subpackages stay excluded because they are 67%
# uncovered, and app.go for the same reason at a larger scale.
#
# Still excluded, with what it would take to admit them: internal/app (62%
# uncovered), internal/ai, internal/cli and internal/drivers (100%), cmd.
#
# Granularity is PER PACKAGE, not per file. --exclude-files takes an RE2 regexp
# and RE2 has no lookahead, so "everything except these two files" is not
# expressible; excluding the subpackage directories is the only way to admit the
# root package on its own.
#
# The value is a regexp alternation, so it MUST stay quoted where it is expanded:
# unquoted, sh reads each '|' as a pipe, `make mutate` dies with "broken pipe",
# no report.json is produced, and the CI Gate treats that as "no result, gate
# passes" - a permanently green gate.
# internal/ui/keydisplay is excluded on purpose: at 100% statement coverage it
# yields no mutants at all, so gating it would add a check that cannot fail.
#
# The ui/ai/cli/drivers groups are excluded A LA CARTE by filename, not as whole
# packages. The regexp is an exclusion list, so a file is opted IN by naming its
# siblings OUT. TODO.md tracks which files are in and which are still pending.
#
#   IN : internal/app/{txn,router}.go, internal/ai/nl2sql/{anthropic,openai,
#        deepseek,qwen}.go, internal/ui/components/explorer/node.go,
#        internal/cli/{ask,commands,context,pipe,root}.go,
#        internal/ai/context/schema.go,
#        internal/ui/components/editor/highlight.go,
#        internal/ui/components/grid/cell.go,
#        internal/ui/components/picker/picker.go,
#        internal/ui/components/ask/ask.go,
#        internal/ai/session/logger.go,
#        internal/ui/components/explorerpreview/ere.go,
#        internal/drivers/postgres/{query,schema}.go,
#        internal/ui/components/grid/{header,mouse,pager}.go,
#        internal/ui/components/palette/{commands,fuzzy}.go
#   OUT: internal/app/app.go (506 uncovered),
#        and the rest of internal/ui/components.
# Optional. A shared PostgreSQL for the mutation run, so internal/app does not
# start a fresh testcontainers instance on every one of the hundreds of test
# runs. Point it at any reachable database; leaving it empty falls back to
# testcontainers, which is correct but slow. See the note above `mutate`.
MUTATE_DSN ?=

MUTATE_EXCLUDE ?= internal/testsupport/|internal/ui/keydisplay/|internal/ui/components/explorerpreview/handled\.go|internal/ui/components/gridpreview/handled\.go|internal/ui/components/grid/(consts|handled)\.go|internal/ui/components/editor/handled\.go|internal/ui/components/explorer/handled\.go|internal/app/(app|messages)\.go|internal/ai/nl2sql/prompt\.go|cmd/|main\.go

build:
	go build $(LDFLAGS) -o .local/bin/dbx .

install: build
	cp .local/bin/dbx $(INSTALL_DIR)/dbx

run:
	go run .

test:
	go test -race -count=1 -cover ./...

# gofmt is not a linter and no linter here replaces it: golangci-lint's standard set is
# errcheck/govet/ineffassign/staticcheck/unused, and a deliberately mis-formatted
# `func F(  a int ) int {` passes `make lint` with 0 issues. Without this the repo has NO
# formatting gate at all, so a mis-formatted file lands on main in green CI and the next
# `gofmt -w` silently rewrites it inside somebody else's diff.
fmt-check:
	@out="$$(gofmt -l .)"; \
	if [ -n "$$out" ]; then echo "gofmt pending in:"; echo "$$out"; exit 1; fi

# `vet` is deliberately NOT a prerequisite here either. golangci-lint's `govet` runs the same
# analyzer passes ("roughly the same as 'go vet' and uses its passes"), and CI used to run a
# separate `go vet ./...` step on top of that -- the same check twice on every push. It runs
# ONCE, inside golangci-lint. The `make lint` recipe below is therefore gofmt + the linter and
# nothing else; a standalone vet run is `go vet ./...` by hand.
lint: fmt-check
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run

# One-shot local gate. `build` compiles into the repo-local .local/bin/dbx and
# never touches the system; `install` (which copies to ~/.local/bin) is
# deliberately excluded.
check: build lint test

# Mutation testing. scripts/mutate.sh owns everything the engine needs -- the exclusion (read from
# THIS file, which is exactly what scripts/check_mutate_scope.py validates against it), the budget,
# the supervisor and the verdict -- so the local loop and CI run the same code path. These two
# targets exist only so the local loop does not have to remember the flags.
#
# A timed-out mutant is ABSENT from the totals, so it is dangerous rather than merely slow: a
# surviving mutant that times out is not caught, and an allowlisted survivor that times out is not
# re-verified either. Two independent causes, and both are handled now:
#
# 1. The per-mutant deadline is DERIVED, not a fixed coefficient: ceil(cap / coverage-pass), so the
#    deadline is the cap whoever measures. The old --timeout-coefficient 3 produced 136 timeouts on
#    txn.go (all 8 of its allowlisted survivors never evaluated), and coefficient 20 only papered
#    over it by making every mutant wait 20 coverage passes -- a number nobody had agreed to.
# 2. DBX_TEST_DSN, so internal/app does not start a fresh testcontainers PostgreSQL on every one of
#    the hundreds of runs: ~4s of container startup each, against 0.5s with a shared container. Set
#    MUTATE_DSN, or leave it unset to let the suite use testcontainers (correct but slow, and the
#    reason the cap is 180s rather than the roomier figure a suite without a database needs).
#
# Check `jq '[.files[].mutations[]|select(.status=="TIMED OUT")]|length' report.json` after any
# change here: a jump in that number means results are no longer trustworthy, even when the gate
# still passes. Whether the jump is ALLOWED is .mutation-notcovered's TIMEOUT_MAX, enforced by
# scripts/check_mutate_nc.py -- this target does not get to decide it quietly.
mutate: ## Whole-module mutation run, with the verdict (same wiring as CI)
	@MUTATE_DSN="$(MUTATE_DSN)" scripts/mutate.sh --run

mutate-diff: ## Mutation run over the diff vs MUTATE_BASE, with the verdict
	@MUTATE_DSN="$(MUTATE_DSN)" scripts/mutate.sh --diff

clean:
	rm -rf .local/bin/
