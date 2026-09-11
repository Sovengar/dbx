.PHONY: build install test lint run clean

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X main.Version=$(VERSION)"
INSTALL_DIR := $(HOME)/.local/bin

build:
	go build $(LDFLAGS) -o .local/bin/dbx .

install: build
	cp .local/bin/dbx $(INSTALL_DIR)/dbx

run:
	go run .

test:
	go test -race -cover ./...

lint:
	golangci-lint run

clean:
	rm -rf .local/bin/
