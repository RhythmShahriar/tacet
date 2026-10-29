# Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
# SPDX-License-Identifier: MIT

GOLANGCI := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0

# tacet is its own module; a go.work in a parent folder must not leak in.
export GOWORK := off

FUZZTIME ?= 30s

.PHONY: build test test-short lint fmt check hooks brand fuzz testbed bench test-scripts

# The release tag when HEAD is exactly on one (v1.0.0 gives 1.0.0), else dev.
VERSION ?= $(shell git describe --tags --exact-match --match 'v*' 2>/dev/null | sed 's/^v//')

build: ## build bin/tacet
	go build -tags gotreesitter_no_copyleft -ldflags "-X main.version=$(or $(VERSION),dev)" -o bin/tacet ./cmd/tacet

test: ## every Go test with the race detector
	go test -race ./...

test-short:
	go test -short ./...

lint:
	$(GOLANGCI) run ./...

fmt:
	gofmt -w .

brand: ## file headers, no AI attribution, writing style
	scripts/check-brand.sh

check: lint brand test ## everything CI runs

test-scripts: ## install.sh and verify-release.sh against local fixtures
	scripts/install-test.sh
	scripts/verify-release-test.sh

hooks: ## enable the Conventional Commits hook
	git config core.hooksPath .githooks

fuzz: ## run every Fuzz* target for FUZZTIME each
	@set -e; for pkg in $$(go list ./...); do \
		for f in $$(go test -list '^Fuzz' "$$pkg" | grep '^Fuzz' || true); do \
			go test -run '^$$' -fuzz "^$$f$$" -fuzztime $(FUZZTIME) "$$pkg"; \
		done; \
	done

testbed: build ## check the expected findings on every fixture project
	$(MAKE) -C testbed check

bench: build ## A/B runs through claude -p; writes docs/benchmark.md
	$(MAKE) -C testbed bench
