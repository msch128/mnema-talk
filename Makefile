# Mnema Talk - developer tasks. Run `make` or `make help` for the list.
# Requires GNU make, Go, Node 26 + npm, Python 3 and Docker (compose plugin).
# Windows: use Git Bash or WSL.

.DEFAULT_GOAL := help
.PHONY: help dev web build run test test-integration test-web typecheck-web contracts-check test-scripts coverage coverage-go coverage-web lint complexity-check fmt vuln openapi openapi-check openapi-diff check docker up down logs install-hooks scorecard e2e load-test smoke backup-drill postgres-upgrade-drill upgrade-drill fresh-install docs-check

BIN        ?= bin/mnema-talk
S3_HOST_PORT ?= 8333
GO_PKGS    := ./...
# Package fixtures reset a supplied shared test database. Serialize package
# binaries when one is configured; Docker fixtures otherwise remain isolated.
GO_INTEGRATION_PKG_FLAGS := $(if $(filter undefined,$(origin TEST_DATABASE_URL)),,-p=1)
# Keep in sync with .github/workflows/ci.yml.
GOVULNCHECK_VERSION ?= v1.8.0
GOCOGNIT_VERSION ?= v1.2.0
# Pinned oasdiff for make openapi-diff (CI runs the same target).
OASDIFF_VERSION ?= v1.34.1

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

E2E_DEPS := e2e/node_modules/.package-lock.json

$(E2E_DEPS): e2e/package-lock.json
	cd e2e && npm ci --no-audit --no-fund

$(WEB_DEPS): web/package-lock.json
	cd web && npm ci --no-audit --no-fund

web: $(WEB_DEPS) ## Install frontend deps (npm ci) and build web/dist
	# `web` is phony, so this always rebuilds; env -u keeps a leftover MNEMA_E2E (test hook) out of a release binary.
	cd web && env -u MNEMA_E2E MNEMA_VERSION=$(VERSION) npm run build

build: web ## Build the web app, then the Go binary to bin/mnema-talk
	@mkdir -p $(dir $(BIN))
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o $(BIN) ./cmd/server

run: build ## Build and run the binary (reads .env)
	./$(BIN)

test: ## Go unit tests (race detector)
	go test -race -count=1 $(GO_PKGS)

test-integration: ## Go integration tests (Docker, or TEST_DATABASE_URL)
	go test $(GO_INTEGRATION_PKG_FLAGS) -tags=integration -race -count=1 -timeout=300s $(GO_PKGS)

test-web: $(WEB_DEPS) ## Frontend unit tests (vitest)
	cd web && npm run test

contracts-check: $(WEB_DEPS) ## Check frontend declarations against the committed OpenAPI document
	cd web && npm run contracts:check

typecheck-web: $(WEB_DEPS) $(E2E_DEPS) ## Strict TypeScript checks for app, tests, build config and media workers
	cd web && npm run typecheck
	cd e2e && npm run typecheck

test-scripts: ## Backup/restore syntax and fault-injected lifecycle tests (Python 3; no Docker)
	bash -n scripts/security-floor.sh scripts/backup.sh scripts/restore.sh scripts/backup-common.sh scripts/drill-common.sh scripts/upgrade-drill.sh scripts/upgrade-drill-data.sh scripts/backup-drill.sh scripts/backup-drill-downgrade.sh
	python3 scripts/tests/test_backup_restore.py
	python3 scripts/tests/test_upgrade_mount_guard.py
	python3 scripts/tests/test_drill_versions.py
	python3 scripts/tests/test_drill_downgrade.py
	python3 scripts/tests/test_coverage_summary.py
	python3 scripts/tests/test_openapi_diff.py
	python3 scripts/tests/test_security_floor.py

# Minimum total Go coverage (%, decimals allowed, compared on the exact
# statement counts) and the floor for every single Go file; the frontend
# enforces 90% in total and 80% per file on all four metrics through
# web/scripts/check-coverage.ts and its reviewed per-module policy.
COVERAGE_MIN ?= 90
COVERAGE_FILE_MIN ?= 80
# Packages counted in the Go coverage total: everything but test helpers and
# build-time tools.
COVER_PKGS = $(shell go list ./cmd/... ./internal/... ./web | grep -v -e /internal/testutil -e /internal/mailer/mailtest -e /internal/tools/ | paste -sd, -)

coverage: coverage-go coverage-web ## Go + web coverage reports (coverage.out, web/coverage/)

coverage-go: ## Go unit + integration tests with coverage -> coverage.out (Docker, or TEST_DATABASE_URL)
	go test $(GO_INTEGRATION_PKG_FLAGS) -tags=integration -race -count=1 -timeout=300s -covermode=atomic -coverpkg=$(COVER_PKGS) -coverprofile=coverage.out $(GO_PKGS)
	@node scripts/coverage-summary.mjs go coverage.out --min $(COVERAGE_MIN) --file-min $(COVERAGE_FILE_MIN)

coverage-web: $(WEB_DEPS) ## Frontend tests with coverage -> web/coverage/ (lcov + json summary)
	cd web && npm run test:coverage

fmt: ## Format Go code in place
	gofmt -w cmd internal api web/web.go

lint: $(WEB_DEPS) ## gofmt check, go vet, cognitive complexity, eslint
	@out="$$(gofmt -l cmd internal api web/web.go)"; \
	if [ -n "$$out" ]; then echo "gofmt needed on:"; echo "$$out"; exit 1; fi
	go vet $(GO_PKGS)
	go vet -tags=integration $(GO_PKGS)
	$(MAKE) --no-print-directory complexity-check
	cd web && npm run lint

complexity-check: ## Fail on Go functions above cognitive complexity 15 (scripts/complexity-allowlist.txt only shrinks); file and function length are checked by internal/repo
	scripts/complexity-check.sh $(GOCOGNIT_VERSION)

vuln: ## govulncheck (Go vulnerability scan)
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) $(GO_PKGS)

openapi: ## Generate api/openapi.json from the handler annotations (swag v2, OpenAPI 3.1)
	$(call openapi_gen,$(OPENAPI_FILE))

openapi-diff: ## Fail on breaking changes of api/openapi.json against the latest v* tag (oasdiff; waived by a BREAKING CHANGE: commit footer)
	OASDIFF_VERSION=$(OASDIFF_VERSION) python3 scripts/openapi_diff.py

openapi-check: ## Fail when api/openapi.json is stale (regenerates into a temp file and diffs)
	@tmp="$$(mktemp -d)" && trap 'rm -rf "$$tmp"' EXIT && \
	$(MAKE) --no-print-directory openapi OPENAPI_FILE="$$tmp/openapi.json" && \
	diff -u api/openapi.json "$$tmp/openapi.json" || { echo "api/openapi.json is stale: run 'make openapi' and commit the result"; exit 1; }

web-bundle-check: web ## Sign web/dist with a throwaway key and verify the desktop bundle (scripts/web-bundle-check.sh)
	scripts/web-bundle-check.sh

check: docs-check lint typecheck-web contracts-check openapi-check openapi-diff test test-scripts coverage-go vuln coverage-web web web-bundle-check ## Everything CI runs: backend and web checks, Docker build, smoke, backup, PostgreSQL upgrade and release upgrade drills
	cd web && npm audit --omit=dev --audit-level=high
	CGO_ENABLED=0 go build ./...
	docker build --build-arg VERSION=$(VERSION) --build-arg REVISION=$(REVISION) -t mnema-talk:ci .
	scripts/smoke-image.sh mnema-talk:ci
	scripts/fresh-install-check.sh mnema-talk:ci
	scripts/backup-drill.sh mnema-talk:ci
	scripts/postgres-upgrade-drill.sh mnema-talk:ci
	scripts/upgrade-drill.sh mnema-talk:ci

e2e: ## Browser smoke test (Playwright + Chromium) against the real binary; needs Docker
	e2e/run.sh

load-test: ## Load test (PROFILE=smoke|full|join|reload|long) against a throwaway stack or BASE_URL; needs Docker, see loadtest/README.md
	loadtest/run.sh

smoke: docker ## Start the built image with the production compose file and check health + admin login
	scripts/smoke-image.sh mnema-talk:local

fresh-install: docker ## Follow doc/deployment.md "Fresh install" in an empty directory with this build, then sign in as the Owner
	scripts/fresh-install-check.sh mnema-talk:local

docs-check: ## Relative links and anchors in README.md, SECURITY.md and doc/*.md, and no merge-conflict markers in tracked files (no network)
	node scripts/check-doc-links.mjs
	node scripts/check-conflict-markers.mjs

backup-drill: docker ## Back up, change, verify and restore a throwaway compose stack, then check the data
	scripts/backup-drill.sh mnema-talk:local

# docker build, not compose: compose would need a .env with the required
# secrets just to build. Same tag as the compose default (MNEMA_IMAGE).
postgres-upgrade-drill: docker ## Install like 0.4 (PostgreSQL 17), then check the guard and scripts/upgrade-postgres.sh
	scripts/postgres-upgrade-drill.sh mnema-talk:local

upgrade-drill: docker ## Install each supported released image (FROM_VERSIONS, default 0.8.16 and 0.9.0), seed data via the API, upgrade to the local build and verify (needs Docker and network access to GHCR)
	scripts/upgrade-drill.sh mnema-talk:local

docker: ## Build the app image (mnema-talk:local, what compose runs by default)
	docker build --build-arg VERSION=$(VERSION) --build-arg REVISION=$(REVISION) -t mnema-talk:local .

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
