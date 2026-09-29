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
# Gated today: internal/config, internal/store, internal/ui/bordered, and the
# internal/ui ROOT files (keybindspane, modal, toast, zones). internal/ui's
# component subpackages stay excluded because they are 67% uncovered.
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
MUTATE_EXCLUDE ?= internal/ui/(components|keydisplay)/|internal/app/|internal/ai/|internal/cli/|internal/drivers/|cmd/

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

mutate:
	go tool gremlins unleash --workers 4 --timeout-coefficient 3 --exclude-files '$(MUTATE_EXCLUDE)' --output report.json

# gremlins silently falls back to the whole module when the diff is empty (base == HEAD),
# so fail fast instead of running a full-module run that looks diff-scoped.
mutate-diff:
	@if git diff --name-only $(MUTATE_BASE)...HEAD | grep -q '\.go$$'; then \
		go tool gremlins unleash --diff $(MUTATE_BASE) --workers 4 --timeout-coefficient 3 --exclude-files '$(MUTATE_EXCLUDE)' --output report.json; \
	else \
		echo "no .go changes vs $(MUTATE_BASE) - nothing to mutate"; \
	fi

clean:
	rm -rf .local/bin/
