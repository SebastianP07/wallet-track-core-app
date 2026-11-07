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

## Production-like local stack

`infra/docker-compose.prod.yml` runs the app the way Railway will (Phase 13): Postgres, a one-shot migration step, the API
and the web app, built from this repository's `Dockerfile` and `wallet-track-app`'s, behind Caddy on HTTPS. It needs the
sibling `wallet-track-app` checkout and `podman-compose`. Its own project name and volumes keep it apart from the
development database, so `make db-up` data is never touched.

```sh
bash scripts/keys.sh infra/.env.prod infra/.env.prod.example   # git-ignored; fills in the secrets, never prints them
podman-compose --env-file infra/.env.prod -f infra/docker-compose.prod.yml build
podman-compose --env-file infra/.env.prod -f infra/docker-compose.prod.yml up -d
podman-compose --env-file infra/.env.prod -f infra/docker-compose.prod.yml ps   # every service healthy, migrate exited 0
```

Then open <https://localhost:8443>. Stop it with `down`, which keeps the data. `down -v` also deletes the database and
Caddy's local CA, so the next start has an empty database and a new CA to trust.

| Piece | What it does |
|---|---|
| `caddy` | The only published port: `127.0.0.1:8443` by default. HTTPS from Caddy's local CA. Proxies everything to `web` and never reaches the API. Adds `X-Frame-Options`, `Permissions-Policy` and `Cross-Origin-Opener-Policy`, and fills HSTS, `X-Content-Type-Options` and `Referrer-Policy` only if the app did not. It never sets a CSP: the app's nonce-based policy passes through untouched |
| `web` | `server.mjs` with `TRUSTED_PROXY_HOPS=1`. Caddy trusts no proxy in front of it, so it replaces any client `X-Forwarded-For` with the peer's address, and that one entry is the client |
| `api` | Distroless, uid 65532, read-only root, no capabilities. `TRUSTED_PROXIES` is only the web app's fixed address on the internal network, `INSECURE_DEV_COOKIES=false`, so cookies are `__Host-` and `Secure` |
| `migrate` | The API image running `goose up`, Railway's pre-deploy command in Phase 13. The API starts only after it exits 0 |
| `postgres` | Postgres 18.6 on an internal network with no route out and no published port |

| Variable (`infra/.env.prod`) | Default | Purpose |
|---|---|---|
| `POSTGRES_PASSWORD`, `TOTP_ENC_KEY`, `PASSWORD_PEPPER` | none, required | Generated by `scripts/keys.sh`. The password is hex so the connection string needs no escaping |
| `WALLET_HOST` | `localhost` | The host name the browser uses. It sets `APP_ORIGIN`, `ALLOWED_ORIGINS`, `WEBAUTHN_RP_ID`, `WEBAUTHN_RP_ORIGINS` and Caddy's certificate, so they always agree. Passkeys stay bound to it |
| `WALLET_HTTPS_PORT` | `8443` | Host port Caddy publishes, part of the origin. Rootless Podman cannot bind 443 |
| `WALLET_BIND` | `127.0.0.1` | `0.0.0.0` opens the port to the LAN |

### Trust Caddy's local CA

Caddy creates its CA in the `caddy-data` volume on first start and cannot install it from inside the container. Until a
browser trusts it, it shows a certificate warning, and service workers refuse to register. Copy the root out:

```sh
podman cp wallet-track-prod_caddy_1:/data/caddy/pki/authorities/local/root.crt ./caddy-local-root.crt
```

| Where | How |
|---|---|
| Fedora system store (curl, most tools) | `sudo trust anchor --store caddy-local-root.crt`; undo with `sudo trust anchor --remove caddy-local-root.crt` |
| Chrome or Chromium on Linux | Settings, Privacy and security, Security, Manage certificates: import it as a trusted authority for websites |
| Firefox | Settings, Privacy & Security, Certificates, View Certificates, Authorities, Import |
| Android | Settings, Security, Encryption & credentials (names vary by vendor), Install a certificate, CA certificate |
| iOS | Open the file to install the profile, then turn it on under Settings, General, About, Certificate Trust Settings |

The root lives ten years, but it is a key on this machine: remove it from every device once you stop using the stack.
`down -v` destroys it and the next start makes a new one.

### Create the owner

The image carries `admin`. Run it in the running API container, which already has the secrets:

```sh
podman exec -it wallet-track-prod_api_1 /app/admin create-owner --email you@example.com
```

Then sign in at the origin, with the enrollment secret it printed.

### From a phone on the LAN

- A phone needs a host name, never an IP address: WebAuthn refuses an IP as the RP ID, and the certificate must name the
  host the phone asks for. Use a name every device resolves to this machine, such as an entry in your router's DNS or
  this machine's mDNS name (`<hostname>.local`; Android resolves `.local` names only on recent versions). Set it as
  `WALLET_HOST` and `WALLET_BIND=0.0.0.0`, then recreate the stack with `down` and `up -d`.
- Install the CA root on the phone as above, or the browser refuses the site and the service worker.
- The firewall must allow `WALLET_HTTPS_PORT/tcp` from the LAN. That is a change to this machine's network setup, so
  make it yourself, and close it again afterwards.
- `WALLET_HOST` is the passkeys' RP ID. A passkey registered under `localhost` does not work under another name, and the
  other way round. TOTP and recovery codes work under any name.

### Known local differences

- Rootless Podman forwards the published port from inside the network, so Caddy sees every client as its own address.
  Locally, every device therefore shares one per-IP rate limit and one password lockout bucket. A forged
  `X-Forwarded-For` still changes nothing, because Caddy discards it. Railway's edge passes the real address.
- Podman starts a container's dependencies whenever it starts that container, so `migrate` runs again when `api`, `web`
  or `caddy` is started or restarted. `goose up` with nothing pending changes nothing.
- The Caddyfile is a single-file bind mount. An editor that replaces the file leaves the container on the old copy, so
  recreate the stack with `down` and `up -d` after editing it. `podman restart` refuses, since `migrate` has exited.
- `podman-compose` turns a one-element exec healthcheck into a shell one, which distroless cannot run, so the API's
  healthcheck spells out its URL.

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
- Closing carries every unpaid fixed and occasional item, and every overdue expense the month owed, to the first later
  month that is not closed (D7). That month need not be opened: it lists them once it is. Months closed out of order are
  skipped. The rows themselves move nothing but `owed_year`/`owed_month`; `period` stays their month of origin.

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
- `totals.toReceive` is Por llegar and `totals.toPay` is Por pagar (D1). Neither moves the other. Received and paid items
  are out of both, and so are the month's own overdue items. `totals.overdue` is the overdue expenses this month owes
  (`owedIn`): its `pending` part is in `toPay` (D7). It stops at `kernel.MaxMoney` rather than fail.
- In a closed month, adding an item is a 409. Toggling, editing and deleting are allowed. Undo returns an item to
  `overdue` there, and to `pending` in an open month. An expense undone in a closed month is owed again by the month that
  owed it when it was paid, or the month after its own, moving on past closed months.
- `dueDate` is any real date from 2000-01-01 on. It need not fall inside the item's month.
- Urgency is by calendar days left in `America/Bogota`. An overdue item still gets one; only a done item or one with no
  due date is always `none`.
- A month's totals stay within `kernel.MaxMoney`. Each item write locks the month's row first, so it needs no advisory
  lock, and `POST /periods` refuses templates that would add up past the bound.

## Overdue list

- `GET /periods/{period}/overdue-items` lists the `overdue` fixed and occasional items the month owes (`owedIn`), oldest
  month of origin first, then every `overdue` income item of a closed month, whichever month is asked. A month not opened is
  a 404. Pending items and card charges never appear (D3, D7).
- `expenseTotal` equals the month's `totals.overdue.pending`, part of its `toPay`. `incomeTotal` counts toward no month's
  totals. Nothing bounds the months together, so both stop at `kernel.MaxMoney` rather than fail.
- The list has no writes of its own: the item's done endpoints settle it. A paid expense leaves the list, keeps its
  `owedIn` and stays in that month's `outgoing`; undo returns it. A person-linked item records its payment as described
  below.

## Dashboard

- `GET /dashboard?period=` is one aggregated read: `kpis`, `toPayBreakdown`, `upcomingDue`, and every section in the shape it
  has at its own endpoint. `internal/dashboard` reaches other modules only through its application ports, wired in
  `cmd/server/dashboard.go`.
- `currentTotal` is the sum of every account's balance today, so a past or closed month shows the same one as any other
  (D6). Savings is merged into Accounts for now: there is no savings KPI, and the kept `accounts.kind` column is never read (D9).
- An overdue item of the selected month is in `incoming` and `outgoing`, which read every status (D2), and out of `toPay` and
  `toReceive`, which read only what is pending (D7). The overdue expenses the month owes are in `toPay` while unpaid and in
  `outgoing` paid or not, and in `toPayBreakdown.overdue` (D7). A closed month owes nothing any more, so its `toPay` and
  `toReceive` are 0 and its `surplus` equals today's `currentTotal`. Card charges reach no KPI (D3).
- `surplus` stops at `kernel.MaxMoney` in either direction. Writes bound balances and items apart, not this sum, and an error
  would take down the page where the user fixes it. Writes keep `incoming`, `outgoing` and `balance` within the bound already.
- A month not opened is a 404, as the contract says, so its balances come from `GET /accounts`.
- `upcomingDue` is the month's pending fixed and occasional items by due date, then name. Each item carries its urgency (D11).
- `GET /dashboard/export?period=` downloads the month as CSV: `kind,name,amount,due_date,status,tag`, one row per item of every
  kind, then one per account with `kind` `account` and only a name and balance. Balances are today's in any month, so they
  are not to be summed with the items, and there are no totals (D1). UTF-8 with a BOM, CRLF, whole pesos as plain integers, and a
  month not opened is a 404.
- Every text cell of the export is quoted, and one that a spreadsheet would read as a formula gets a leading `'`, which it then
  shows as part of the text. So does what follows a `;`, a tab or a line break inside a cell, since Excel in a `;` locale splits
  there whatever the quotes say. Amounts never get one: a negative balance has to stay a number.

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
| `podman-compose` | 1.6 or later | Only for the production-like stack, whose `depends_on` conditions it honors |
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
| `cmd/healthcheck` | Container healthcheck: GETs `/healthz`, because the distroless image has no shell or curl |
| `contracts` | `openapi.yaml`, the HTTP contract |
| `migrations` | goose SQL migrations |
| `queries/<module>` | sqlc SQL; each module gets a block in `sqlc.yaml` and generates into `internal/<module>/adapters/postgres/sqlcgen` |
| `internal/shared/kernel` | Shared value objects: `Money`, bounded to ±(2^53-1), `YearMonth`, from 2000-01 on, with the due day clamp (D11), and `DateInBogota` |
| `internal/accounts` | Accounts, savings included for now (D9): `domain`, `application` (one struct per use case, ports in `ports.go`) and `adapters/{http,postgres}` |
| `internal/periods` | Periods and recurring templates: `domain`, `application` (one struct per use case, ports in `ports.go`) and `adapters/{http,postgres}` |
| `internal/expenses` | A month's items of every kind and their done toggle, with urgency (D11), Por pagar and Por llegar (D1): `domain`, `application` and `adapters/{http,postgres}` |
| `internal/tags` | Tags (D5): `domain`, `application` and `adapters/{http,postgres}` |
| `internal/cards` | Credit cards and a month's per-card totals (D3): `domain`, `application` and `adapters/{http,postgres}` |
| `internal/platforms` | People, platforms, memberships and payments (D4, D8, D10), reached by `periods` and `expenses` only through application ports wired in `cmd/server`: `domain`, `application` and `adapters/{http,postgres}` |
| `internal/dashboard` | The KPIs (D1, D2, D3, D7, D9) and the one read that gathers every section, in its own types, reading other modules only through application ports wired in `cmd/server`: `domain`, `application` and `adapters/http` |
| `internal/auth` | Auth module: `domain`, `application` (one struct per use case, ports in `ports.go`) and `adapters/{http,postgres,crypto,memory,passkey}` |
| `internal/shared/platform` | Config, logging, the system clock, HTTP server, database pool, middleware, cookies, CSRF, rate limiting, problem responses |
| `internal/decisions` | Pending tests for decisions D1-D11, each naming its phase |
| `infra` | `docker-compose.yml`, the development Postgres. `docker-compose.prod.yml`, `Caddyfile` and `.env.prod.example`, the production-like stack |
| `Dockerfile` | The API image: `server`, `admin`, `healthcheck`, goose and the migrations, on distroless as a non-root user |

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
