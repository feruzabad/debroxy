# debroxy build tasks. Go tools pinned in go.mod run with `go tool`;
# sqlc and golangci-lint run from pinned Docker images, so nothing is installed on the host.

GO             ?= go
SQLC_IMAGE     := sqlc/sqlc:1.31.1
GOLANGCI_IMAGE := golangci/golangci-lint:v2.14.0
NODE_IMAGE     := node:26.12.0-alpine
PLAYWRIGHT_IMAGE := mcr.microsoft.com/playwright:v1.64.1-noble
# Node no longer bundles corepack (25+), so it is installed from npm.
COREPACK_VERSION := 0.36.0

# Generators write into the source tree, so they run as the host user.
DOCKER_GENERATE := docker run --rm --user $(shell id -u):$(shell id -g) -e HOME=/tmp -v $(CURDIR):/src -w /src
# The linter only reads the tree; named volumes keep its caches between runs.
DOCKER_LINT := docker run --rm -v $(CURDIR):/src -w /src \
	-v debroxy-lint-go-cache:/cache -e GOCACHE=/cache/go-build -e GOMODCACHE=/cache/mod \
	-e GOLANGCI_LINT_CACHE=/cache/golangci-lint
# Browser client commands run pnpm (through the pinned corepack, put on PATH so
# package scripts can call it) in the pinned Node image, as the host user.
PNPM_SH := mkdir -p /tmp/bin && npm install --global --silent --prefix /tmp/corepack-cli corepack@$(COREPACK_VERSION) \
	&& /tmp/corepack-cli/bin/corepack enable --install-directory /tmp/bin && PATH=/tmp/bin:$$PATH exec pnpm "$$@"
DOCKER_WEB := docker run --rm --user $(shell id -u):$(shell id -g) -e HOME=/tmp -e COREPACK_HOME=/tmp/corepack \
	-e npm_config_store_dir=/repo/web/node_modules/.pnpm-store -v $(CURDIR):/repo -w /repo/web \
	$(NODE_IMAGE) sh -c '$(PNPM_SH)' pnpm
# Visual regression runs in the pinned Playwright image (its browsers are at /ms-playwright).
DOCKER_VISUAL := docker run --rm --user $(shell id -u):$(shell id -g) -e HOME=/tmp -e COREPACK_HOME=/tmp/corepack \
	-e npm_config_store_dir=/repo/web/node_modules/.pnpm-store -v $(CURDIR):/repo -w /repo/web \
	$(PLAYWRIGHT_IMAGE) sh -c '$(PNPM_SH)' pnpm

.PHONY: check build vet test lint generate migrate-new docker web-install web web-gen web-test web-check web-visual web-visual-update

## check: everything a stage must pass (build, vet, race tests incl. archtest, lint)
check: build vet test lint

build:
	$(GO) build ./...

vet:
	$(GO) vet ./...

test:
	$(GO) test -race ./...

lint:
	$(DOCKER_LINT) $(GOLANGCI_IMAGE) golangci-lint run ./...

## generate: oapi-codegen through go:generate directives, then sqlc when configured
generate:
	$(GO) generate ./...
	@if [ -f sqlc.yaml ]; then $(DOCKER_GENERATE) $(SQLC_IMAGE) generate; fi

## migrate-new: create the next goose migration, e.g. make migrate-new CONTEXT=job CHANGE=create_remote_items
migrate-new:
	@test -n "$(CONTEXT)" -a -n "$(CHANGE)" || { echo "usage: make migrate-new CONTEXT=<context> CHANGE=<snake_case_change>"; exit 1; }
	@mkdir -p migrations
	@last=$$(ls migrations | sed -n 's/^\([0-9]\{4\}\)_.*\.sql$$/\1/p' | sort | tail -1); \
	next=$$(printf '%04d' $$((10#$${last:-0} + 1))); \
	file="migrations/$${next}_$(CONTEXT)_$(CHANGE).sql"; \
	printf -- '-- +goose Up\n\n-- +goose Down\n' > "$$file"; \
	echo "created $$file"

## docker: build the container image for the host platform
docker:
	docker build -t debroxy:dev .

## web-install: install the browser client's dependencies from the committed lockfile
web-install:
	$(DOCKER_WEB) install --frozen-lockfile

## web: build the browser client into web/dist
web: web-install
	$(DOCKER_WEB) run build

## web-gen: regenerate web/src/api/generated from api/openapi
web-gen: web-install
	$(DOCKER_WEB) run gen:api

## web-test: run the browser client's Vitest suite
web-test: web-install
	$(DOCKER_WEB) run test

## web-check: lint, type-check, test, build and size-check the browser client, then run the visual suite
web-check: web-install
	$(DOCKER_WEB) run check
	$(DOCKER_VISUAL) run test:visual

## web-visual: run the browser client's Playwright visual regression suite
web-visual: web
	$(DOCKER_VISUAL) run test:visual

## web-visual-update: regenerate the Playwright visual baselines
web-visual-update: web
	$(DOCKER_VISUAL) run test:visual:update
