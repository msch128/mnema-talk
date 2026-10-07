# Mnema Talk - developer tasks. Run `make` or `make help` for the list.
# Requires GNU make, Go, Node 24 + npm, Python 3, and Docker (compose plugin).
# Windows: use Git Bash or WSL.

.DEFAULT_GOAL := help
.PHONY: help dev web build run test test-integration test-web test-scripts coverage coverage-go coverage-web lint fmt vuln openapi openapi-check check docker up down logs install-hooks scorecard e2e

BIN        ?= bin/mnema-talk
S3_HOST_PORT ?= 8333
GO_PKGS    := ./...
# Keep in sync with .github/workflows/ci.yml.
GOVULNCHECK_VERSION ?= v1.8.0

OPENAPI_FILE := api/openapi.json

# Version and revision baked into the binary and the web app (they must
# match: the browser offers a reload when the server's version differs).
VERSION  ?= $(shell cat version.txt 2>/dev/null || echo dev)
REVISION ?= $(shell git rev-parse HEAD 2>/dev/null)
VERSION_PKG := github.com/msch128/mnema-talk/internal/version
LDFLAGS  := -w -s -X $(VERSION_PKG).Version=$(VERSION) -X $(VERSION_PKG).Revision=$(REVISION)

# openapi_gen,<target file>: swag v2 writes an OpenAPI 3.1 document from the
# handler annotations (internal/server/doc.go holds the general info); then
# internal/tools/openapifix applies the fixes swag cannot express itself.
define openapi_gen
	@tmp="$$(mktemp -d)" && trap 'rm -rf "$$tmp"' EXIT && \
	go tool swag init --v3.1 --quiet \
		-g doc.go -d internal/server,internal --parseInternal --requiredByDefault \
		--overridesFile api/swaggo.overrides --outputTypes json -o "$$tmp" && \
	go run ./internal/tools/openapifix "$$tmp/swagger.json" $(1)
endef

help: ## Show this help
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS=":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

dev: ## Start postgres + seaweedfs via compose, then run the server with go run (reads .env)
	docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d --wait postgres seaweedfs
	APP_ENV=development S3_ENDPOINT=http://localhost:$(S3_HOST_PORT) go run ./cmd/server

WEB_DEPS := web/node_modules/.package-lock.json

$(WEB_DEPS): web/package-lock.json
	cd web && npm ci --no-audit --no-fund

web: $(WEB_DEPS) ## Install frontend deps (npm ci) and build web/dist
	cd web && MNEMA_VERSION=$(VERSION) npm run build

build: web ## Build the web app, then the Go binary to bin/mnema-talk
	@mkdir -p $(dir $(BIN))
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o $(BIN) ./cmd/server

run: build ## Build and run the binary (reads .env)
	./$(BIN)

test: ## Go unit tests (race detector)
	go test -race -count=1 $(GO_PKGS)

test-integration: ## Go integration tests (Docker, or TEST_DATABASE_URL)
	go test -tags=integration -race -count=1 -timeout=300s $(GO_PKGS)

test-web: $(WEB_DEPS) ## Frontend unit tests (vitest)
	cd web && npm run test

test-scripts: ## Backup/restore syntax and fault-injected lifecycle tests (Python 3; no Docker)
	bash -n scripts/backup.sh scripts/restore.sh scripts/backup-common.sh
	python3 scripts/tests/test_backup_restore.py

# Packages counted in the Go coverage total: everything but test helpers and
# build-time tools.
# Minimum total coverage (%) for Go and web; keep in sync with .github/workflows/ci.yml.
COVERAGE_MIN ?= 80
COVER_PKGS = $(shell go list ./cmd/... ./internal/... ./web | grep -v -e /internal/testutil -e /internal/tools/ | paste -sd, -)

coverage: coverage-go coverage-web ## Go + web coverage reports (coverage.out, web/coverage/)

coverage-go: ## Go unit + integration tests with coverage -> coverage.out (Docker, or TEST_DATABASE_URL)
	go test -tags=integration -race -count=1 -timeout=300s -covermode=atomic -coverpkg=$(COVER_PKGS) -coverprofile=coverage.out $(GO_PKGS)
	@node scripts/coverage-summary.mjs go coverage.out --min $(COVERAGE_MIN)

coverage-web: $(WEB_DEPS) ## Frontend tests with coverage -> web/coverage/ (lcov + json summary)
	cd web && npm run test:coverage
	@node scripts/coverage-summary.mjs web web/coverage/coverage-summary.json --min $(COVERAGE_MIN)

fmt: ## Format Go code in place
	gofmt -w cmd internal api web/web.go

lint: $(WEB_DEPS) ## gofmt check, go vet, eslint
	@out="$$(gofmt -l cmd internal api web/web.go)"; \
	if [ -n "$$out" ]; then echo "gofmt needed on:"; echo "$$out"; exit 1; fi
	go vet $(GO_PKGS)
	go vet -tags=integration $(GO_PKGS)
	cd web && npm run lint

vuln: ## govulncheck (Go vulnerability scan)
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) $(GO_PKGS)

openapi: ## Generate api/openapi.json from the handler annotations (swag v2, OpenAPI 3.1)
	$(call openapi_gen,$(OPENAPI_FILE))

openapi-check: ## Fail when api/openapi.json is stale (regenerates into a temp file and diffs)
	@tmp="$$(mktemp -d)" && trap 'rm -rf "$$tmp"' EXIT && \
	$(MAKE) --no-print-directory openapi OPENAPI_FILE="$$tmp/openapi.json" && \
	diff -u api/openapi.json "$$tmp/openapi.json" || { echo "api/openapi.json is stale: run 'make openapi' and commit the result"; exit 1; }

check: lint openapi-check test test-scripts coverage-go vuln coverage-web web ## Everything CI runs: lint, tests, vuln scan, builds, npm audit, docker build
	cd web && npm audit --omit=dev --audit-level=high
	CGO_ENABLED=0 go build ./...
	docker build --build-arg VERSION=$(VERSION) --build-arg REVISION=$(REVISION) -t mnema-talk:ci .

e2e: ## Browser smoke test (Playwright + Chromium) against the real binary; needs Docker
	e2e/run.sh

docker: ## Build the app image via compose
	docker compose build --build-arg VERSION=$(VERSION) --build-arg REVISION=$(REVISION) app

up: ## Start the full stack (docker compose up -d)
	docker compose up -d

down: ## Stop the stack (data volumes are kept)
	docker compose down

logs: ## Follow the app logs
	docker compose logs -f app

install-hooks: ## Use scripts/git-hooks as this clone's git hooks
	git config core.hooksPath scripts/git-hooks
	@chmod +x scripts/git-hooks/* 2>/dev/null || true
	@echo "git hooks installed (core.hooksPath=scripts/git-hooks)"

scorecard: ## Print the weighted 1.0 score from docs/SCORECARD.md (local, untracked file; skipped when absent)
	@node scripts/scorecard.mjs
