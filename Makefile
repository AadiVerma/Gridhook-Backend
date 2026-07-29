LINT_VERSION := v2.12.2
GOOS := $(shell go env GOOS)
GOARCH := $(shell go env GOARCH)
LINT_PKG := golangci-lint-$(LINT_VERSION:v%=%)-$(GOOS)-$(GOARCH)
LINT_BASE_URL := https://github.com/golangci/golangci-lint/releases/download/$(LINT_VERSION)
BIN_DIR := $(CURDIR)/bin
LINT := $(BIN_DIR)/golangci-lint
COVERAGE := coverage.out
AIR_VERSION := v1.67.3

.DEFAULT_GOAL := help

.PHONY: help tools air lint lint-fix fmt build test test-race cover bench vet check clean migrate migrate-baseline migrate-status run-server run-worker dev dev-worker

help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

tools: $(LINT) ## Install pinned developer tooling

$(LINT):
	@mkdir -p $(BIN_DIR)
	@tmp=$$(mktemp -d); \
	curl -sSfL -o $$tmp/$(LINT_PKG).tar.gz $(LINT_BASE_URL)/$(LINT_PKG).tar.gz; \
	curl -sSfL -o $$tmp/checksums.txt $(LINT_BASE_URL)/golangci-lint-$(LINT_VERSION:v%=%)-checksums.txt; \
	cd $$tmp && grep "$(LINT_PKG).tar.gz$$" checksums.txt | shasum -a 256 -c - && \
	tar -xzf $(LINT_PKG).tar.gz -C $$tmp --strip-components=1 $(LINT_PKG)/golangci-lint && \
	mv $$tmp/golangci-lint $(LINT) && \
	rm -rf $$tmp

build: ## Compile both binaries into bin/
	go build -o $(BIN_DIR)/server ./cmd/server
	go build -o $(BIN_DIR)/worker ./cmd/worker

vet: ## Run go vet
	go vet ./...

test: ## Run the test suite
	go test ./...

test-race: ## Run the test suite under the race detector
	go test -race ./...

cover: ## Run tests with coverage and print a per-package summary
	go test -coverprofile=$(COVERAGE) -covermode=atomic ./...
	go tool cover -func=$(COVERAGE) | tail -1
	@echo "full report: go tool cover -html=$(COVERAGE)"

bench: ## Run benchmarks
	go test -run '^$$' -bench=. -benchmem ./...

lint: tools ## Run golangci-lint
	$(LINT) run ./...

lint-fix: tools ## Run golangci-lint with autofixes
	$(LINT) run --fix ./...

fmt: tools ## Format the tree
	$(LINT) fmt ./...

check: vet lint test-race ## Run vet, lint and race tests

migrate: ## Apply any migrations this database has not seen (requires DATABASE_URL)
	@test -n "$$DATABASE_URL" || { echo "DATABASE_URL is not set"; exit 1; }
	@psql "$$DATABASE_URL" -v ON_ERROR_STOP=1 -q -c \
		"CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now());"
	@for f in internal/db/migrations/*.up.sql; do \
		v=$$(basename "$$f" .up.sql); \
		if [ "$$(psql "$$DATABASE_URL" -tAc "SELECT 1 FROM schema_migrations WHERE version='$$v'")" = "1" ]; then \
			echo "  skip   $$v"; continue; \
		fi; \
		echo "  apply  $$v"; \
		psql "$$DATABASE_URL" -v ON_ERROR_STOP=1 -q --single-transaction \
			-f "$$f" -c "INSERT INTO schema_migrations (version) VALUES ('$$v');" || exit 1; \
	done
	@echo "migrations up to date"

migrate-baseline: ## Mark migrations up to UPTO=NNNN as applied without running them
	@test -n "$$DATABASE_URL" || { echo "DATABASE_URL is not set"; exit 1; }
	@test -n "$(UPTO)" || { echo "UPTO is required, e.g. make migrate-baseline UPTO=0003"; exit 1; }
	@psql "$$DATABASE_URL" -v ON_ERROR_STOP=1 -q -c \
		"CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now());"
	@for f in internal/db/migrations/*.up.sql; do \
		v=$$(basename "$$f" .up.sql); \
		if [ "$${v%%_*}" \> "$(UPTO)" ]; then echo "  leave    $$v (will be applied by 'make migrate')"; continue; fi; \
		psql "$$DATABASE_URL" -v ON_ERROR_STOP=1 -q -c \
			"INSERT INTO schema_migrations (version) VALUES ('$$v') ON CONFLICT DO NOTHING;" || exit 1; \
		echo "  baseline $$v"; \
	done

migrate-status: ## Show which migrations have been applied
	@test -n "$$DATABASE_URL" || { echo "DATABASE_URL is not set"; exit 1; }
	@psql "$$DATABASE_URL" -c "SELECT version, applied_at FROM schema_migrations ORDER BY version;" 2>/dev/null \
		|| echo "no schema_migrations table — run 'make migrate' or 'make migrate-baseline'"

run-server: ## Run the API + MCP runtime
	go run ./cmd/server

run-worker: ## Run background jobs
	go run ./cmd/worker

AIR := $(shell go env GOPATH)/bin/air

$(AIR):
	go install github.com/air-verse/air@$(AIR_VERSION)

air: $(AIR) ## Install the live-reload watcher

dev: $(AIR) ## Run the server with live reload
	$(AIR) -c .air.toml

dev-worker: $(AIR) ## Run the worker with live reload
	$(AIR) -c .air.toml \
		--build.cmd "go build -o ./tmp/worker ./cmd/worker" \
		--build.entrypoint "./tmp/worker"

clean: ## Remove build and coverage artifacts
	rm -rf $(BIN_DIR) $(COVERAGE) tmp build-errors.log
