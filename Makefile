# All dev tasks run in containers (prerequisites: make + podman or docker).
# Inside the dev container (DEVCONTAINER=true) the same targets run natively.
.DEFAULT_GOAL := help
.PHONY: help install dev lint lint-frontend lint-backend test test-frontend test-backend build set-password clean

SCOPE ?= all
ifeq ($(filter $(SCOPE),all frontend backend),)
$(error SCOPE must be one of: all, frontend, backend (got '$(SCOPE)'))
endif

ROOT       := $(CURDIR)
CACHE      := $(ROOT)/.cache
GO_IMAGE   ?= docker.io/library/golang:1.27-alpine
GOC_IMAGE  ?= docker.io/library/golang:1.27
NODE_IMAGE ?= docker.io/library/node:25-alpine
NET        := santa-dev
BACKEND    := santa-dev-backend
FRONTEND   := santa-dev-frontend

ifeq ($(DEVCONTAINER),true)
# ---- native mode: tools are installed in the dev container ----
RT           :=
go_run        =
gocgo_run     =
node_run      =
PROXY_TARGET := http://localhost:8080
else
# ---- host mode: wrap every tool in a container ----
RT ?= $(shell command -v podman 2>/dev/null || command -v docker 2>/dev/null)
ifeq ($(RT),)
$(error Install podman (or docker) to use this Makefile)
endif
USER_ARGS := --user $(shell id -u):$(shell id -g)$(if $(findstring podman,$(RT)), --userns=keep-id)
$(shell mkdir -p $(CACHE)/go-mod $(CACHE)/go-build $(CACHE)/npm $(ROOT)/web/node_modules)
# $(1): extra `run` flags. :z (shared label) lets several containers mount the repo at once.
go_run    = $(RT) run --rm $(USER_ARGS) $(1) -v $(ROOT):/app:z -w /app \
            -v $(CACHE)/go-mod:/cache/mod:z -v $(CACHE)/go-build:/cache/build:z \
            -e HOME=/tmp -e GOMODCACHE=/cache/mod -e GOCACHE=/cache/build -e CGO_ENABLED=0 $(GO_IMAGE)
# -race needs cgo + gcc: Debian-based Go image
gocgo_run = $(RT) run --rm $(USER_ARGS) $(1) -v $(ROOT):/app:z -w /app \
            -v $(CACHE)/go-mod:/cache/mod:z -v $(CACHE)/go-build:/cache/build:z \
            -e HOME=/tmp -e GOMODCACHE=/cache/mod -e GOCACHE=/cache/build -e CGO_ENABLED=1 $(GOC_IMAGE)
node_run  = $(RT) run --rm $(USER_ARGS) $(1) -v $(ROOT):/app:z -w /app \
            -v $(CACHE)/npm:/cache/npm:z -e HOME=/tmp -e npm_config_cache=/cache/npm $(NODE_IMAGE)
PROXY_TARGET := http://$(BACKEND):8080
endif

help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'
	@echo "  (lint/test accept SCOPE=frontend|backend)"

web/dist/.keep:
	@mkdir -p web/dist && touch web/dist/.keep

web/node_modules/.package-lock.json: web/package-lock.json
	$(call node_run,) npm --prefix web ci

install: ## Install frontend and backend dependencies
	$(call node_run,) npm --prefix web ci
	$(call go_run,) go mod download

dev: web/dist/.keep web/node_modules/.package-lock.json ## Run Go backend (:8080) and Vite HMR (:5173)
	@$(if $(RT),$(RT) network inspect $(NET) >/dev/null 2>&1 || $(RT) network create $(NET) >/dev/null;) \
	trap 'trap - INT TERM EXIT; $(if $(RT),$(RT) rm -f $(BACKEND) $(FRONTEND) >/dev/null 2>&1;) kill 0' INT TERM EXIT; \
	$(if $(RT),$(RT) rm -f $(BACKEND) $(FRONTEND) >/dev/null 2>&1;) \
	APP_ENV=development $(call go_run,--name $(BACKEND) --network $(NET) -p 127.0.0.1:8080:8080 -e APP_ENV) go run ./cmd/server & \
	API_PROXY_TARGET=$(PROXY_TARGET) $(call node_run,--name $(FRONTEND) --network $(NET) -p 127.0.0.1:5173:5173 -e API_PROXY_TARGET) npm --prefix web run dev -- --host & \
	echo "Backend: http://localhost:8080   Frontend (HMR): http://localhost:5173   (Ctrl-C to stop)"; \
	wait

lint: $(if $(filter-out backend,$(SCOPE)),lint-frontend) $(if $(filter-out frontend,$(SCOPE)),lint-backend) ## Typecheck + go vet
lint-frontend: web/node_modules/.package-lock.json
	$(call node_run,) npm --prefix web run typecheck
lint-backend: web/dist/.keep
	$(call go_run,) go vet ./...

test: $(if $(filter-out backend,$(SCOPE)),test-frontend) $(if $(filter-out frontend,$(SCOPE)),test-backend) ## Vitest + go test -race
test-frontend: web/node_modules/.package-lock.json
	$(call node_run,) npm --prefix web test
test-backend: web/dist/.keep
	$(call gocgo_run,) go test -race ./...

build: ## Build the production image (secret-santa)
	$(RT) build -t secret-santa .

set-password: ## Prompt for a new Super Admin password and write its hash to .env
	$(call go_run,-it) go run ./cmd/server set-password

clean: ## Remove dev containers, caches and build artifacts
	-@$(if $(RT),$(RT) rm -f $(BACKEND) $(FRONTEND) >/dev/null 2>&1; $(RT) network rm $(NET) >/dev/null 2>&1;) true
	rm -rf web/dist .cache secret-santa coverage.out
