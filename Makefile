.PHONY: build install test lint run clean check mutate mutate-diff coverage coverage-check

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X main.Version=$(VERSION)"
INSTALL_DIR := $(HOME)/.local/bin
GOLANGCI_LINT_VERSION := v2.13.2
MUTATE_BASE ?= main

# PostgreSQL compartido para la corrida de mutación. Vacío = la suite levanta
# testcontainers por corrida (correcto pero lento).
MUTATE_DSN ?=

# Alcance de la mutación. Solo código que la suite alcanza: sin tests no hay mutantes.
# Granularidad POR PAQUETE (RE2 no admite lookahead, así que "todo salvo estos dos
# ficheros" no es expresible) y, en ai/cli/drivers, POR FICHERO: la lista de exclusión
# hace que un fichero entre nombrando a sus hermanos fuera. TODO.md lleva el detalle.
#
# La alternancia va ENTRE comillas donde se expande: sin ellas sh lee cada '|' como un
# pipe y `make mutate` muere con "broken pipe".
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

# fmt-check: gofmt -l, y falla si algún fichero queda sin formatear.
fmt-check:
	@out="$$(gofmt -l .)"; \
	if [ -n "$$out" ]; then echo "gofmt pending in:"; echo "$$out"; exit 1; fi

# lint: fmt-check + golangci-lint (govet corre dentro; `go vet ./...` es manual).
lint: fmt-check
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run

# check: build + lint + test, el gate local de una sola tanda.
check: build lint test

# Mutación. scripts/mutate.sh lee MUTATE_EXCLUDE de este fichero y se lleva el presupuesto,
# el supervisor y el veredicto: local y CI miden por el mismo camino.
#
# El plazo por mutante se deriva de ceil(cap / pase de cobertura); el que expira queda
# fuera de los totales y su techo lo decide .mutation-timeouts dentro del script.
mutate: ## Whole-module mutation run, with the verdict (same wiring as CI)
	@MUTATE_DSN="$(MUTATE_DSN)" scripts/mutate.sh --run

mutate-diff: ## Mutation run over the diff vs MUTATE_BASE, with the verdict
	@MUTATE_DSN="$(MUTATE_DSN)" scripts/mutate.sh --diff

clean:
	rm -rf .local/bin/
