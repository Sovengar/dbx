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
#   OUT: internal/app/app.go (350 uncovered), internal/ai/nl2sql/compatible.go,
#        and the rest of internal/ui/components.
# Optional. A shared PostgreSQL for the mutation run, so internal/app does not
# start a fresh testcontainers instance on every one of the hundreds of test
# runs. Point it at any reachable database; leaving it empty falls back to
# testcontainers, which is correct but slow. See the note above `mutate`.
MUTATE_DSN ?=

MUTATE_EXCLUDE ?= internal/testsupport/|internal/ui/keydisplay/|internal/ui/components/explorerpreview/(preview|tabbar|handled)\.go|internal/ui/components/(gridpreview|gridsidebarpreview|querybrowser)/|internal/ui/components/grid/(consts|handled|table|where_filter)\.go|internal/ui/components/editor/(autocomplete|handled|sql)\.go|internal/ui/components/(explorer/(explorer|handled|tree)|palette/palette)\.go|internal/app/(app|messages)\.go|internal/ai/nl2sql/(compatible|provider|prompt)\.go|cmd/|main\.go|internal/theme/

build:
	go build $(LDFLAGS) -o .local/bin/dbx .

install: build
	cp .local/bin/dbx $(INSTALL_DIR)/dbx

run:
	go run .

test:
	go test -race -count=1 -cover ./...

lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run

# One-shot local gate. `build` compiles into the repo-local .local/bin/dbx and
# never touches the system; `install` (which copies to ~/.local/bin) is
# deliberately excluded.
check: build lint test

# A timed-out mutant is ABSENT from report.json, so it is indistinguishable from
# a killed one. That makes timeouts dangerous rather than merely slow: a
# surviving mutant that times out is not caught, and an allowlisted survivor that
# times out is not re-verified either. Two independent causes, both fixed here.
#
# 1. --timeout-coefficient 20, not the default 3. At 3 this scope produced 136
#    timeouts on txn.go, which meant all 8 of its allowlisted survivors were
#    never evaluated at all.
# 2. DBX_TEST_DSN, so internal/app does not start a fresh testcontainers
#    PostgreSQL on every one of the hundreds of runs. Without it each run pays
#    ~4s of container startup and times out; with a shared container the same
#    package finishes in 0.5s. Set MUTATE_DSN, or leave it unset to let the
#    suite use testcontainers (correct but slow, and prone to timeouts).
#
# Check `jq '[.files[].mutations[]|select(.status=="TIMED OUT")]|length' report.json`
# after any change here. A jump in that number means results are no longer
# trustworthy, even when the gate still passes.
mutate:
	DBX_TEST_DSN="$(MUTATE_DSN)" go tool gremlins unleash --workers 4 --timeout-coefficient 20 --exclude-files '$(MUTATE_EXCLUDE)' --output report.json

# gremlins silently falls back to the whole module when the diff is empty (base == HEAD),
# so fail fast instead of running a full-module run that looks diff-scoped.
mutate-diff:
	@if git diff --name-only $(MUTATE_BASE)...HEAD | grep -q '\.go$$'; then \
		DBX_TEST_DSN="$(MUTATE_DSN)" go tool gremlins unleash --diff $(MUTATE_BASE) --workers 4 --timeout-coefficient 20 --exclude-files '$(MUTATE_EXCLUDE)' --output report.json; \
	else \
		echo "no .go changes vs $(MUTATE_BASE) - nothing to mutate"; \
	fi

clean:
	rm -rf .local/bin/
