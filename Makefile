# Local settings; every value also has a default, so .env is optional.
-include .env
export

COMPOSE_FILE_PATH := infra/docker-compose.yml
# Evaluated on use, so targets that need no container runtime never probe for one.
COMPOSE = $(shell if docker compose version >/dev/null 2>&1; then echo docker compose; \
	elif podman compose version >/dev/null 2>&1; then echo podman compose; fi)

# Fedora's packaged Go defaults to GOTOOLCHAIN=local, which refuses the toolchain pinned in go.mod.
GOTOOLCHAIN ?= auto

.DEFAULT_GOAL := help

.PHONY: help dev test lint migrate db-up db-down keys

help: ## List available targets
	@awk 'BEGIN {FS = ":.*## "} /^[a-z-]+:.*## / {printf "  %-10s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

dev: ## Build and run the API server
	go build -o bin/server ./cmd/server
	exec ./bin/server

test: ## Run all tests
	go test ./...

lint: ## Run go vet and golangci-lint
	go vet ./...
	golangci-lint run

migrate: ## PLACEHOLDER until Phase 1 (goose migrations)
	@echo "migrate: placeholder - migrations arrive in Phase 1"

db-up: ## Start local Postgres
	@test -n "$(COMPOSE)" || { echo "db-up: install 'docker compose' or 'podman compose'" >&2; exit 1; }
	$(COMPOSE) -f $(COMPOSE_FILE_PATH) up -d

db-down: ## Stop local Postgres, keeping its data volume
	@test -n "$(COMPOSE)" || { echo "db-down: install 'docker compose' or 'podman compose'" >&2; exit 1; }
	$(COMPOSE) -f $(COMPOSE_FILE_PATH) down

keys: ## Generate local secrets into .env without overwriting existing values
	bash scripts/keys.sh
