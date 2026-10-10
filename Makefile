.PHONY: build install test lint run clean check mutate mutate-diff coverage coverage-check

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X main.Version=$(VERSION)"
INSTALL_DIR := $(HOME)/.local/bin
GOLANGCI_LINT_VERSION := v2.13.2
MUTATE_BASE ?= main

# Shared PostgreSQL for the mutation run. Empty = the suite starts testcontainers
# on every run (correct but slow).
MUTATE_DSN ?=

# Mutation scope. Only code the suite reaches: no tests, no mutants.
# Granularity is PER PACKAGE (RE2 has no lookahead, so "everything but these two
# files" is not expressible) and, in ai/cli/drivers, PER FILE: an exclusion list opts
# a file in by naming its siblings out. TODO.md carries the detail.
#
# The alternation MUST stay quoted where it expands: unquoted, sh reads each '|' as a
# pipe and `make mutate` dies with "broken pipe".
MUTATE_EXCLUDE ?= internal/testsupport/|internal/ui/keydisplay/|internal/ui/components/explorerpreview/handled\.go|internal/ui/components/gridpreview/handled\.go|internal/ui/components/grid/(consts|handled)\.go|internal/ui/components/editor/handled\.go|internal/ui/components/explorer/handled\.go|internal/app/(app|messages)\.go|internal/ai/nl2sql/prompt\.go|cmd/|main\.go

build:
	go build $(LDFLAGS) -o .local/bin/dbx .

install: build
	cp .local/bin/dbx $(INSTALL_DIR)/dbx

run:
	go run .

test:
	go test -race -count=1 -cover ./...

COVER_PROFILE ?= coverage.out

coverage: ## Deduplicated coverage profile of the whole suite
	@go test -count=1 -covermode=atomic -coverprofile=$(COVER_PROFILE) ./... > /dev/null
	@echo "profile: $(COVER_PROFILE)"

coverage-check: coverage ## Gate: this change's DIFF at 100%, and the total against scripts/coverage-floor
	@scripts/diff-coverage.sh "$(COVER_PROFILE)" "$(MUTATE_BASE)"

# fmt-check: gofmt -l, and fails if any file is left unformatted.
fmt-check:
	@out="$$(gofmt -l .)"; \
	if [ -n "$$out" ]; then echo "gofmt pending in:"; echo "$$out"; exit 1; fi

# lint: fmt-check + golangci-lint (govet runs inside; `go vet ./...` is manual).
lint: fmt-check
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run

# check: build + lint + test, the one-shot local gate.
check: build lint test

mutate:
	@MUTATE_DSN="$(MUTATE_DSN)" scripts/mutate.sh --run

mutate-diff:
	@MUTATE_DSN="$(MUTATE_DSN)" scripts/mutate.sh --diff

clean:
	rm -rf .local/bin/
