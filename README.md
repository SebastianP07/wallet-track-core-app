# Wallet Track Core

Go API for Wallet Track, a private personal finance app. The cross-repo plan and decisions live in
`PLAN.md`, next to this repository's checkout.

## Quick path

```sh
make keys     # create .env with local secrets (never overwrites existing values)
make db-up    # start local Postgres on 127.0.0.1:5433
make migrate  # apply the schema
make seed     # optional: load an invented fixture month
make dev      # run the API on :8080
curl localhost:8080/healthz
```

## Commands

| Command | What it does |
|---|---|
| `make dev` | Build and run the server |
| `make test` | Run all tests |
| `make lint` | Run `go vet` and `golangci-lint` |
| `make migrate` / `make migrate-down` | Apply all pending migrations / roll back the latest one |
| `make migrate-status` | Show which migrations are applied |
| `make sqlc` | Generate typed Go from `queries/`; the output is committed |
| `make seed` | Load the invented dev fixture; safe to re-run, refuses any non-local host, creates no user |
| `make contract-lint` | Lint `contracts/openapi.yaml` with Redocly CLI through `pnpm dlx` |
| `make db-up` / `make db-down` | Start / stop Postgres; `db-down` keeps the data volume |
| `make keys` | Generate `TOTP_ENC_KEY` and `PASSWORD_PEPPER` into the git-ignored `.env` |

## Requirements

| Tool | Version | Notes |
|---|---|---|
| Go | toolchain pinned in `go.mod` | `make` sets `GOTOOLCHAIN=auto`, so an older local Go downloads it. Plain `go` commands on Fedora need that variable too |
| `golangci-lint` | 2.15.0 | For `make lint` |
| Compose provider | `docker compose` or `podman compose` | The Makefile detects which is available |
| `openssl`, `bash` | any | For `make keys` |
| `pnpm` | any | For `make contract-lint` |

`goose` and `sqlc` need no global install: `go.mod` pins them with the `tool` directive and the Makefile
runs them with `go tool`. The first run compiles them, which takes about a minute.

## Configuration

Read from the environment; `make` loads `.env` automatically. `.env.example` lists every variable.

| Variable | Default | Used by |
|---|---|---|
| `HTTP_ADDR` | `:8080` | Server |
| `LOG_LEVEL` | `info` | Server |
| `SHUTDOWN_TIMEOUT` | `10s` | Server |
| `POSTGRES_DB`, `POSTGRES_USER`, `POSTGRES_PASSWORD` | see `.env.example` | Compose |
| `POSTGRES_PORT` | `5433` | Compose; host port, bound to localhost only |
| `DATABASE_URL` | built from `POSTGRES_*`, on `127.0.0.1` | `make migrate*`, `make seed` |
| `TOTP_ENC_KEY`, `PASSWORD_PEPPER` | none | Generated now, read from Phase 2 |

## Layout

| Path | Contents |
|---|---|
| `cmd/server` | Composition root |
| `cmd/seed` | Dev-only fixture loader; never deployed |
| `contracts` | `openapi.yaml`, the HTTP contract |
| `migrations` | goose SQL migrations |
| `queries/<module>` | sqlc SQL; each module gets a block in `sqlc.yaml` and generates into `internal/<module>/adapters/postgres/sqlcgen` |
| `internal/shared/kernel` | Shared value objects: `Money` |
| `internal/shared/platform` | Config, logging, HTTP server |
| `internal/decisions` | Pending tests for decisions D1-D11, each naming its phase |
| `infra` | Local Postgres compose file |

## HTTP contract

This repository owns `contracts/openapi.yaml`, OpenAPI 3.1. The frontend (`wallet-track-app`) keeps a
vendored copy, refreshed by a script that reads this repository as a sibling checkout. After changing the
contract, run `make contract-lint` here and `pnpm contract:refresh && pnpm contract:types` there.

## Schema notes

- `platform_payments.covered_until` is a generated column: the last day of the last covered month, counting
  the month of `paid_at` (D8).
- Foreign keys: deleting a template, tag, card or person keeps past items and only clears the link. Deleting a
  card also clears it on templates and platforms. Deleting a person with payments, or a period with items, is
  rejected. Sessions and recovery codes go with their user; audit events stay, without the link.
