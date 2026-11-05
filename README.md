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
make dev INSECURE_DEV_COOKIES=true TRUSTED_PROXIES=127.0.0.1/32,::1/128 ALLOWED_ORIGINS=http://localhost:3000 WEBAUTHN_RP_ID=localhost WEBAUTHN_RP_ORIGINS=http://localhost:3000
```

Other operator commands:

| Command | What it does |
|---|---|
| `make admin ARGS="reset-password --email you@example.com"` | New password, second-factor lockout cleared, every session revoked. If enrollment was never finished, also prints a new enrollment secret. MFA itself is not reset |
| `make admin ARGS="rotate-totp-key"` | Re-encrypts TOTP secrets under `TOTP_ENC_KEY`. Put the new key there and the old one in `TOTP_ENC_KEY_PREVIOUS`, run it, then remove the old key |

## Auth behavior worth knowing

| Topic | Behavior |
|---|---|
| Before the second factor | A password-only session lasts 10 minutes and reaches only `login/totp`, `login/recovery-code`, the two `login/passkey` operations and the two enrollment operations. Everything else answers 401 |
| Password lockout | 5 consecutive wrong passwords for one email from one client IP lock that pair for 1 minute, doubling up to 1 hour. Other addresses are unaffected, so nobody without the password can lock the owner out. A correct password clears the pair. Counters live in memory, so a restart forgets them |
| Second-factor lockout | 5 consecutive failures of TOTP, a recovery code, a passkey assertion or enrollment, all behind the password, lock the account from any IP for 1 minute, doubling up to 1 hour. The count persists in the database and clears only when a second factor succeeds |
| Rate limits (in memory) | Per client IP: burst of 20, one more every 6 s, on routes that check a credential. `GET /auth/csrf` has its own bucket: burst of 60, one more per second. Per account, only on steps behind the password: burst of 10, one more every 30 s |
| Unknown emails | Same code path as a real account: same dummy-hash cost, same 401, same lockout, same 429 |
| Client IP | The TCP peer by default, ignoring `X-Forwarded-For`. Behind the BFF that makes the per-IP limit one shared bucket, so set `TRUSTED_PROXIES` to the BFF's address, locally too. A listed peer's header is read from the right, and the first untrusted address is the client. Limits and password lockout key an IPv6 client by its /64 and an IPv4-mapped address as IPv4 |
| `Origin` | A mutating request carrying an `Origin` not in `ALLOWED_ORIGINS` gets a 403, so an empty list rejects them all. No `Origin` leaves the decision to the CSRF token |

## Passkeys and sessions

| Topic | Behavior |
|---|---|
| Role | A passkey is an alternative to TOTP in the second-factor step, after the password. `nextStep` answers `passkey` when the owner has one, and TOTP and recovery codes keep working. There is no passwordless login |
| Registering and removing | Both need a full session and a current `SecondFactorProof`, TOTP or recovery code, so a stolen session cannot silently add or remove a credential. The proof is spent in the transaction that does the work. Renaming and listing need the full session only |
| Challenges | Random, in `passkey_challenges`, one per session and purpose, single use and valid for 2 minutes. A registration challenge exists only once a proof was spent, so holding one is what shows the proof. Reading one deletes it, so a rejected response needs a new one |
| Checks | Origin, RP ID, challenge, signature and user verification, by `go-webauthn`, with no attestation requested. A rejected assertion is the generic 401 and counts toward the second-factor lockout. A success promotes and rotates the session as TOTP does |
| Sign count | One that does not grow is refused and recorded as `mfa_passkey_sign_count_regressed`, unless the authenticator keeps none and it stays at 0 |
| Stored | Credential ID, public key, transports, sign count, backup flags, name and last use. Credential IDs and public keys are never returned or logged |
| Library boundary | Only `internal/auth/adapters/passkey` imports `go-webauthn`. The rest sees the `PasskeyVerifier` port, and its options, responses and state as opaque bytes |
| Sessions list | `GET /auth/sessions` lists sessions neither revoked nor expired, with the user agent and address of the request that started them. A session awaiting its second factor appears with `mfaPending`, since it means someone passed the password step. Token hashes are never selected. Revoking the current one also clears its cookie |

## Period behavior worth knowing

- Months are independent. Any month from 2000-01 on can be opened or closed in any order, several can be open at
  once, and nothing opens or closes a month on its own.
- Opening clones the active templates only when it creates the month, so a template added later reaches the next month
  opened, not one already open. Opening a month that exists answers 200 and changes nothing, even if it is closed.
- Closing a month that has no period is a 404. Closing a closed month is a 200 that changes nothing. There is no reopen.

## Item behavior worth knowing

- Writes accept every kind. A card charge needs an existing `cardId` and has no due date. An edit can move it to another
  card, including one whose card was deleted, but `cardId: null` is a 400. Other kinds refuse a card.
- Card charges are informational (D3): they never count toward `totals.toPay` or `totals.toReceive`, and closing a month
  leaves them as they are. Done moves only `totals.cardCharge.pending`, and undo always returns one to `pending`.
- `GET /periods/{period}/card-totals` counts every card charge of the month, whatever its status. Every card has a row, with
  0 if it has no charges. Charges whose card was deleted form one last row with a null card, only when there are any, and
  count toward `total`.
- Income has no due date and no urgency: sending `dueDate` for it is a 400, on a create or an edit. `personId` cannot be
  set through these endpoints; opening a month generates those rows (D4).
- `totals.toReceive` is Por llegar and `totals.toPay` is Por pagar (D1). Neither moves the other, and received, paid and
  overdue items are out of both.
- In a closed month, adding an item is a 409. Toggling, editing and deleting are allowed. Undo returns an item to
  `overdue` there, and to `pending` in an open month.
- `dueDate` is any real date from 2000-01-01 on. It need not fall inside the item's month.
- Urgency is by calendar days left in `America/Bogota`. An overdue item still gets one; only a done item or one with no
  due date is always `none`.
- A month's totals stay within `kernel.MaxMoney`. Each item write locks the month's row first, so it needs no advisory
  lock, and `POST /periods` refuses templates that would add up past the bound.

## Overdue list

- `GET /overdue-items` lists every `overdue` fixed, occasional and income item of a closed month, whatever month is being
  looked at, oldest month first, then by due date. Pending items of open months and card charges never appear (D3, D7).
- `expenseTotal` and `incomeTotal` sum the whole list. They count toward no month's `toPay` or `toReceive`. Each month's sums
  stay within `kernel.MaxMoney`, but nothing bounds the months together, so these two stop at that bound rather than fail.
- The list has no writes of its own: the item's done endpoints settle it. It stays in its original month and leaves the
  list; undo returns it. A person-linked item records its payment as described below.

## Dashboard

- `GET /dashboard?period=` is one aggregated read: `kpis`, `toPayBreakdown`, `upcomingDue`, and every section in the shape it
  has at its own endpoint. `internal/dashboard` reaches other modules only through its application ports, wired in
  `cmd/server/dashboard.go`.
- `currentTotal` and `savings` are today's balances, so a past or closed month shows the same ones as any other (D6, D9).
- An overdue item of the selected month is in `incoming` and `outgoing`, which read every status (D2), and out of `toPay` and
  `toReceive`, which read only what is pending (D7). So a closed month has `toPay` and `toReceive` at 0, and its `surplus`
  equals today's `currentTotal`. Other months' items and card charges reach no KPI (D3, D7).
- `surplus` stops at `kernel.MaxMoney` in either direction. Writes bound balances and items apart, not this sum, and an error
  would take down the page where the user fixes it. Writes keep `incoming`, `outgoing` and `balance` within the bound already.
- A month not opened is a 404, as the contract says, so its balances come from `GET /accounts`.
- `upcomingDue` is the month's pending fixed and occasional items by due date, then name. Each item carries its urgency (D11).

## Platform behavior worth knowing

- A share is ⌈cost ÷ members⌉ in whole pesos (D10). An owner who is a member counts in the divisor, owes 0, gets no income
  item and cannot record a payment. A person's `monthlyOwed` is the sum of their rounded shares.
- Opening a month adds one income item per person who owes it, named `Platforms — <name>`, for one month's shares, in the
  transaction that clones the templates, and only when it creates the month (D4). A payment covering that month means no
  item. What counts is the month being opened, not today.
- Marking a person-linked item received records a payment: their `billingMonths`, `monthlyOwed × months`, and a date
  within the item's month, today when today falls in it, or else its nearest day, so settling a month late still covers
  that month. The item's own amount plays no part. Undo removes exactly that payment through `period_item_id`.
- `POST /people/{id}/payments` marks the item of the month of `paidAt` received and links the two, if one is waiting. An
  item already received keeps its own payment. `DELETE /platform-payments/{id}` returns a linked item to pending, or
  overdue in a closed month. Each of these is one transaction.
- Coverage counts from the payment's own month and never stacks. Payments may overlap: `coveredUntil` is the furthest any
  reaches, and a month is covered by any payment that includes it.
- A month's items are settled when it opens. Changing a platform, a membership or a frequency, or recording or undoing a
  payment that reaches other months, leaves the items of months already open as they are. So a later month's pending item
  stays after a payment comes to cover it, and undoing a payment does not create an item for a month opened meanwhile. The
  person's row still says what is covered and owed, and a payment can be recorded without an item.
- Editing a person-linked item changes only that month's row, never its payment. Deleting one keeps its payment and
  coverage, and opening the month again does not bring it back.
- Deleting a person with payments is a 409. Without payments, their memberships go and their items stay as plain income.

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
| `WEBAUTHN_RP_ID` | none, required | Server; the host name passkeys are bound to. Local: `localhost`. Changing it orphans every registered passkey, which leaves TOTP and recovery codes |
| `WEBAUTHN_RP_ORIGINS` | none, required | Server; comma-separated exact origins passkeys may be used from, each the RP ID or a subdomain of it, and `https` unless the host is `localhost`. Local: `http://localhost:3000`. The server refuses to start if they are inconsistent |
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
| `internal/shared/kernel` | Shared value objects: `Money`, bounded to ±(2^53-1), `YearMonth`, from 2000-01 on, with the due day clamp (D11), and `DateInBogota` |
| `internal/accounts` | Accounts and savings: `domain`, `application` (one struct per use case, ports in `ports.go`) and `adapters/{http,postgres}` |
| `internal/periods` | Periods and recurring templates: `domain`, `application` (one struct per use case, ports in `ports.go`) and `adapters/{http,postgres}` |
| `internal/expenses` | A month's items of every kind and their done toggle, with urgency (D11), Por pagar and Por llegar (D1): `domain`, `application` and `adapters/{http,postgres}` |
| `internal/tags` | Tags (D5): `domain`, `application` and `adapters/{http,postgres}` |
| `internal/cards` | Credit cards and a month's per-card totals (D3): `domain`, `application` and `adapters/{http,postgres}` |
| `internal/platforms` | People, platforms, memberships and payments (D4, D8, D10), reached by `periods` and `expenses` only through application ports wired in `cmd/server`: `domain`, `application` and `adapters/{http,postgres}` |
| `internal/dashboard` | The KPIs (D1, D2, D3, D7, D9) and the one read that gathers every section, in its own types, reading other modules only through application ports wired in `cmd/server`: `domain`, `application` and `adapters/http` |
| `internal/auth` | Auth module: `domain`, `application` (one struct per use case, ports in `ports.go`) and `adapters/{http,postgres,crypto,memory,passkey}` |
| `internal/shared/platform` | Config, logging, the system clock, HTTP server, database pool, middleware, cookies, CSRF, rate limiting, problem responses |
| `internal/decisions` | Pending tests for decisions D1-D11, each naming its phase |
| `infra` | Local Postgres compose file |

## HTTP contract

This repository owns `contracts/openapi.yaml`, OpenAPI 3.1. The frontend (`wallet-track-app`) keeps a
vendored copy, refreshed by a script that reads this repository as a sibling checkout. After changing the
contract, run `make contract-lint` here and `pnpm contract:refresh && pnpm contract:types` there.

## Schema notes

- `platform_payments.covered_until` is a generated column: the last day of the last covered month, counting
  the month of `paid_at` (D8).
- `platform_payments.period_item_id` is the income item a payment settled, unique, cleared if the item is deleted.
- Foreign keys: deleting a template, tag, card or person keeps past items and only clears the link. Deleting a
  card also clears it on templates and platforms. Deleting a person with payments, or a period with items, is
  rejected. Sessions and recovery codes go with their user; audit events stay, without the link.
