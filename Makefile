.PHONY: help install dev dev-backend dev-frontend lint test build set-password prod prod-down prod-logs prod-bin clean

help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

install: ## Install frontend and backend dependencies
	npm --prefix web ci
	go mod download

web/dist:
	@mkdir -p web/dist
	@touch web/dist/.keep

dev: web/dist ## Run Go backend (:8080) and Vite HMR server (:5173) concurrently
	@trap 'kill 0' INT TERM EXIT; \
	APP_ENV=development go run ./cmd/server & \
	npm --prefix web run dev & \
	wait

dev-backend: web/dist ## Run Go backend in development mode (:8080)
	APP_ENV=development go run ./cmd/server

dev-frontend: ## Run Vite frontend dev server (:5173, proxies /api to :8080)
	npm --prefix web run dev

lint: web/dist ## Run frontend typecheck and backend go vet
	npm --prefix web run typecheck
	go vet ./...

test: web/dist ## Run frontend (Vitest) and backend (Go race) tests
	npm --prefix web test
	go test -race ./...

build: ## Build embedded SPA + single static Go binary (./secret-santa)
	npm --prefix web run build
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o secret-santa ./cmd/server

set-password: ## Prompt for a new Super Admin password and write its PBKDF2 hash to .env
	@go run ./cmd/server set-password

prod: ## Build and start production container via Docker Compose
	docker compose up -d --build

prod-down: ## Stop production container
	docker compose down

prod-logs: ## Tail production container logs
	docker compose logs -f

prod-bin: build ## Build and run single binary directly in production mode
	APP_ENV=production ./secret-santa

clean: ## Remove build artifacts
	go clean
	rm -f secret-santa coverage.out
