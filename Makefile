# Mnema Talk - developer tasks. Run `make` or `make help` for the list.
# Requires GNU make, Go, Node 22 + npm, and Docker (compose plugin).
# Windows: use Git Bash or WSL.

.DEFAULT_GOAL := help
.PHONY: help dev web build run test test-integration test-web lint fmt vuln openapi openapi-check check docker up down logs install-hooks scorecard e2e

BIN        ?= bin/mnema-talk
S3_HOST_PORT ?= 8333
GO_PKGS    := ./...
# Keep in sync with .github/workflows/ci.yml.
GOVULNCHECK_VERSION ?= v1.8.0

OPENAPI_FILE := api/openapi.json

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
	cd web && npm run build

build: web ## Build the web app, then the Go binary to bin/mnema-talk
	@mkdir -p $(dir $(BIN))
	CGO_ENABLED=0 go build -trimpath -ldflags="-w -s" -o $(BIN) ./cmd/server

run: build ## Build and run the binary (reads .env)
	./$(BIN)

test: ## Go unit tests (race detector)
	go test -race -count=1 $(GO_PKGS)

test-integration: ## Go integration tests (Docker, or TEST_DATABASE_URL)
	go test -tags=integration -race -count=1 -timeout=300s $(GO_PKGS)

test-web: $(WEB_DEPS) ## Frontend unit tests (vitest)
	cd web && npm run test

fmt: ## Format Go code in place
	gofmt -w cmd internal web/web.go

lint: $(WEB_DEPS) ## gofmt check, go vet, eslint
	@out="$$(gofmt -l cmd internal web/web.go)"; \
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

check: lint openapi-check test test-integration vuln test-web web ## Everything CI runs: lint, tests, vuln scan, builds, npm audit, docker build
	cd web && npm audit --omit=dev --audit-level=high
	CGO_ENABLED=0 go build ./...
	docker build -t mnema-talk:ci .

e2e: ## Browser smoke test (Playwright + Chromium) against the real binary; needs Docker
	e2e/run.sh

docker: ## Build the app image via compose
	docker compose build app

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

scorecard: ## Print the weighted 1.0 score from docs/SCORECARD.md
	@node scripts/scorecard.mjs
