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

## Create the owner and log in by hand

There is no sign-up. Create the single owner once; the password is read from a no-echo prompt, or from stdin
when piped. Save the enrollment secret it prints: it is shown only once and lasts 24 hours.

```sh
make admin ARGS="create-owner --email you@example.com"
```

The API sets `Secure`, `__Host-` cookies, which curl will not store over plain HTTP. For this walkthrough only,
run the server with `make dev INSECURE_DEV_COOKIES=true`, which names them `dev-session` and `dev-csrf`. In another
terminal, with `PASSWORD` and `ENROLLMENT_SECRET` set:

```sh
API=localhost:8080/api/v1
JSON='Content-Type: application/json'
# A CSRF token is required on every POST; it stays valid for later steps.
CSRF=$(curl -s -c jar -b jar $API/auth/csrf | jq -r .csrfToken)

# Step 1 answers {"nextStep":"enroll_mfa"} the first time, {"nextStep":"totp"} after that.
curl -s -c jar -b jar -H "X-CSRF-Token: $CSRF" -H "$JSON" $API/auth/login/password \
  -d "{\"email\":\"you@example.com\",\"password\":\"$PASSWORD\"}"

# First login only: returns the TOTP secret to add to an authenticator app.
curl -s -c jar -b jar -H "X-CSRF-Token: $CSRF" -H "$JSON" $API/auth/mfa/enrollment \
  -d "{\"enrollmentSecret\":\"$ENROLLMENT_SECRET\"}"

# First login only: returns the ten recovery codes, which are shown only here.
curl -s -c jar -b jar -H "X-CSRF-Token: $CSRF" -H "$JSON" $API/auth/mfa/enrollment/confirm \
  -d '{"code":"123456"}'

# Every later login, after step 1. A recovery code goes to /auth/login/recovery-code instead.
curl -s -c jar -b jar -H "X-CSRF-Token: $CSRF" -H "$JSON" $API/auth/login/totp -d '{"code":"123456"}'

curl -s -b jar $API/me
```

Delete `jar` when done: it holds a live session token.

To log in through the web app instead, run the API as below, then follow the "Run it against the API" section of
the `wallet-track-app` README. The first sign-in asks for the enrollment secret.

```sh
make dev INSECURE_DEV_COOKIES=true TRUSTED_PROXIES=127.0.0.1/32,::1/128 ALLOWED_ORIGINS=http://localhost:3000
```

Other operator commands:

| Command | What it does |
|---|---|
| `make admin ARGS="reset-password --email you@example.com"` | New password, second-factor lockout cleared, every session revoked. If enrollment was never finished, also prints a new enrollment secret. MFA itself is not reset |
| `make admin ARGS="rotate-totp-key"` | Re-encrypts TOTP secrets under `TOTP_ENC_KEY`. Put the new key there and the old one in `TOTP_ENC_KEY_PREVIOUS`, run it, then remove the old key |

## Auth behavior worth knowing

| Topic | Behavior |
|---|---|
| Before the second factor | A password-only session lasts 10 minutes and reaches only `login/totp`, `login/recovery-code` and the two enrollment operations. Everything else answers 401 |
| Password lockout | 5 consecutive wrong passwords for one email from one client IP lock that pair for 1 minute, doubling up to 1 hour. Other addresses are unaffected, so nobody without the password can lock the owner out. A correct password clears the pair. Counters live in memory, so a restart forgets them |
| Second-factor lockout | 5 consecutive failures of TOTP, a recovery code or enrollment, all behind the password, lock the account from any IP for 1 minute, doubling up to 1 hour. The count persists in the database and clears only when a second factor succeeds |
| Rate limits (in memory) | Per client IP: burst of 20, one more every 6 s, on routes that check a credential. `GET /auth/csrf` has its own bucket: burst of 60, one more per second. Per account, only on steps behind the password: burst of 10, one more every 30 s |
| Unknown emails | Same code path as a real account: same dummy-hash cost, same 401, same lockout, same 429 |
| Client IP | The TCP peer by default, ignoring `X-Forwarded-For`. Behind the BFF that makes the per-IP limit one shared bucket, so set `TRUSTED_PROXIES` to the BFF's address, locally too. A listed peer's header is read from the right, and the first untrusted address is the client. Limits and password lockout key an IPv6 client by its /64 and an IPv4-mapped address as IPv4 |
| `Origin` | A mutating request carrying an `Origin` not in `ALLOWED_ORIGINS` gets a 403, so an empty list rejects them all. No `Origin` leaves the decision to the CSRF token |

## Commands

| Command | What it does |
|---|---|
| `make dev` | Build and run the server |
| `make admin ARGS="..."` | Run `cmd/admin` with the secrets it needs |
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
| `curl`, `jq` | any | Only for the by-hand login above |

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
| `DATABASE_URL` | built from `POSTGRES_*`, on `127.0.0.1` | Server, `cmd/admin`, `make migrate*`, `make seed` |
| `TOTP_ENC_KEY`, `PASSWORD_PEPPER` | none, required | Server, `cmd/admin`. Generated by `make keys` |
| `TOTP_ENC_KEY_PREVIOUS` | empty | Server, `cmd/admin`; only while rotating |
| `TRUSTED_PROXIES` | empty, `X-Forwarded-For` ignored | Server; comma-separated CIDRs |
| `ALLOWED_ORIGINS` | empty, any `Origin` rejected | Server; comma-separated exact origins |
| `INSECURE_DEV_COOKIES` | `false` | Server; local development only. Drops `Secure` and the `__Host-` prefix, and logs a warning. The server refuses to start if `ALLOWED_ORIGINS` has an origin whose host is not `localhost`, `127.0.0.1` or `[::1]` |

## Layout

| Path | Contents |
|---|---|
| `cmd/server` | Composition root of the API |
| `cmd/admin` | Operator CLI: `create-owner`, `reset-password`, `rotate-totp-key` |
| `cmd/seed` | Dev-only fixture loader; never deployed |
| `contracts` | `openapi.yaml`, the HTTP contract |
| `migrations` | goose SQL migrations |
| `queries/<module>` | sqlc SQL; each module gets a block in `sqlc.yaml` and generates into `internal/<module>/adapters/postgres/sqlcgen` |
| `internal/shared/kernel` | Shared value objects: `Money` |
| `internal/auth` | Auth module: `domain`, `application` (one struct per use case, ports in `ports.go`) and `adapters/{http,postgres,crypto,memory}` |
| `internal/shared/platform` | Config, logging, HTTP server, database pool, middleware, cookies, CSRF, rate limiting, problem responses |
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
