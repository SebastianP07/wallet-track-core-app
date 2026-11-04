# Local settings; every value also has a default, so .env is optional.
-include .env

COMPOSE_FILE_PATH := infra/docker-compose.yml
# Evaluated on use, so targets that need no container runtime never probe for one.
COMPOSE = $(shell if docker compose version >/dev/null 2>&1; then echo docker compose; \
	elif podman compose version >/dev/null 2>&1; then echo podman compose; fi)

POSTGRES_DB ?= wallet_track
POSTGRES_USER ?= wallet_track
POSTGRES_PASSWORD ?= wallet_track_local
POSTGRES_PORT ?= 5433
DATABASE_URL ?= postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@127.0.0.1:$(POSTGRES_PORT)/$(POSTGRES_DB)?sslmode=disable

# Fedora's packaged Go defaults to GOTOOLCHAIN=local, which refuses the toolchain pinned in go.mod.
export GOTOOLCHAIN ?= auto

# Nothing is exported by default: each target below receives only what it needs, so secrets never
# reach third-party tools. unexport also drops values make would inherit from the calling shell.
# It must follow the ?= defaults, because it defines a missing variable as empty.
unexport TOTP_ENC_KEY TOTP_ENC_KEY_PREVIOUS PASSWORD_PEPPER POSTGRES_PASSWORD DATABASE_URL GOOSE_DBSTRING

dev: export HTTP_ADDR := $(HTTP_ADDR)
dev: export LOG_LEVEL := $(LOG_LEVEL)
dev: export SHUTDOWN_TIMEOUT := $(SHUTDOWN_TIMEOUT)
dev: export TRUSTED_PROXIES := $(TRUSTED_PROXIES)
dev: export ALLOWED_ORIGINS := $(ALLOWED_ORIGINS)
dev: export INSECURE_DEV_COOKIES := $(INSECURE_DEV_COOKIES)

# The only recipes that receive the application secrets.
dev admin: export DATABASE_URL := $(DATABASE_URL)
dev admin: export TOTP_ENC_KEY := $(TOTP_ENC_KEY)
dev admin: export TOTP_ENC_KEY_PREVIOUS := $(TOTP_ENC_KEY_PREVIOUS)
dev admin: export PASSWORD_PEPPER := $(PASSWORD_PEPPER)

# Passed through the environment so the connection string never appears in make's output.
migrate migrate-down migrate-status: export GOOSE_DRIVER := postgres
migrate migrate-down migrate-status: export GOOSE_MIGRATION_DIR := migrations
migrate migrate-down migrate-status: export GOOSE_DBSTRING := $(DATABASE_URL)
seed: export DATABASE_URL := $(DATABASE_URL)

db-up db-down: export POSTGRES_DB := $(POSTGRES_DB)
db-up db-down: export POSTGRES_USER := $(POSTGRES_USER)
db-up db-down: export POSTGRES_PASSWORD := $(POSTGRES_PASSWORD)
db-up db-down: export POSTGRES_PORT := $(POSTGRES_PORT)

.DEFAULT_GOAL := help

.PHONY: help dev admin test lint migrate migrate-down migrate-status sqlc seed contract-lint db-up db-down keys

help: ## List available targets
	@awk 'BEGIN {FS = ":.*## "} /^[a-z-]+:.*## / {printf "  %-15s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

dev: ## Build and run the API server
	go build -o bin/server ./cmd/server
	exec ./bin/server

admin: ## Run cmd/admin, e.g. make admin ARGS="create-owner --email you@example.com"
	@go run ./cmd/admin $(ARGS)

test: ## Run all tests
	go test ./...

lint: ## Run go vet and golangci-lint
	go vet ./...
	golangci-lint run

migrate: ## Apply all pending migrations
	go tool goose up

migrate-down: ## Roll back the latest migration
	go tool goose down

migrate-status: ## Show which migrations are applied
	go tool goose status

sqlc: ## Generate typed Go from queries/
	go tool sqlc generate

seed: ## Load the invented dev fixture into local Postgres; refuses non-local hosts
	@go run ./cmd/seed

contract-lint: ## Lint contracts/openapi.yaml with Redocly CLI (needs pnpm)
	pnpm --package=@redocly/cli@2.75.0 dlx redocly lint contracts/openapi.yaml

db-up: ## Start local Postgres
	@test -n "$(COMPOSE)" || { echo "db-up: install 'docker compose' or 'podman compose'" >&2; exit 1; }
	$(COMPOSE) -f $(COMPOSE_FILE_PATH) up -d

db-down: ## Stop local Postgres, keeping its data volume
	@test -n "$(COMPOSE)" || { echo "db-down: install 'docker compose' or 'podman compose'" >&2; exit 1; }
	$(COMPOSE) -f $(COMPOSE_FILE_PATH) down

keys: ## Generate local secrets into .env without overwriting existing values
	bash scripts/keys.sh
