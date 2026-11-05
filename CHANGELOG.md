# Changelog

## 0.14.0 — 2026-11-05 — Passkeys and active sessions

- Passkeys in `internal/auth`, with `github.com/go-webauthn/webauthn` v0.18.2 confined to `adapters/passkey` behind the
  `PasskeyVerifier` port. New operations: `POST /auth/login/passkey/options`, `POST /auth/login/passkey`, `GET /auth/passkeys`,
  `POST /auth/passkeys/registration/options`, `POST /auth/passkeys/registration`, `PATCH /auth/passkeys/{id}` and
  `POST /auth/passkeys/{id}/remove`. `LoginStep.nextStep` gains `passkey`, answered when the owner has one.
- A passkey is an alternative to TOTP in the second-factor step, never a way around the password. TOTP and recovery codes keep
  working with a passkey present and after the last one goes.
- Registering and removing need a full session and a fresh `SecondFactorProof`, TOTP or recovery code, spent in the transaction
  that stores the challenge or deletes the passkey. A rejected proof counts toward the second-factor lockout, and a 404 leaves
  the code unspent. Removing takes a `POST` because the proof travels in the body.
- Migration 00008 adds `passkey_challenges`, one per session and purpose, holding the library's state and an expiry of 2 minutes.
  `DELETE ... RETURNING` reads and consumes it in one statement, so it works once whatever happens next and a replay finds nothing.
- Migration 00008 adds `passkeys` with only what an assertion needs: credential ID, public key, transports, sign count and
  backup flags. No attestation statement or AAGUID, since the options ask for no attestation.
- Every ceremony requires user verification, and the library checks origin, RP ID, challenge and signature. A rejected assertion
  is the generic 401 and counts toward the lockout. A sign count that does not grow is refused with
  `mfa_passkey_sign_count_regressed`, and the update repeats that check, so two assertions racing with one count cannot both pass.
- New audit events: `login_succeeded_passkey`, `mfa_passkey_failed`, `mfa_passkey_sign_count_regressed`, `passkey_registered`,
  `passkey_registration_failed` and `passkey_removed`. Rejections log the library's fixed wording, never its debug detail or a
  credential ID.
- `GET /auth/sessions` and `DELETE /auth/sessions/{id}`. The list selects no token hash. Sessions awaiting their second factor
  appear with `mfaPending`. Revoking the current one also clears its cookie.
- `WEBAUTHN_RP_ID` and `WEBAUTHN_RP_ORIGINS` are required: the server refuses to start without them, with an IP address or scheme
  as RP ID, with an origin outside the RP ID, or with plain `http` anywhere but `localhost`.
- No new automated tests (T7).

## 0.13.0 — 2026-11-05 — Dashboard

- Added `internal/dashboard` and `GET /api/v1/dashboard?period=`, with a full session: KPIs, the to-pay breakdown, the upcoming
  due list and every section of the month in its own endpoint's shape. It reads other modules through application ports only,
  with adapters in `cmd/server/dashboard.go`. A month not opened is a 404, a bad `period` a 400.
- Total actual and Ahorro are today's balances, in any month (D6, D9). Por pagar and Por llegar read pending items only; Llega
  and Se va read every status (D2). So an overdue item of the selected month stays in Llega, Se va and Balance and leaves Por
  pagar, Por llegar and Sobrante, and a closed month's Sobrante equals today's Total actual. Card charges and other months'
  items reach none of them (D3, D7).
- `surplus` stops at `kernel.MaxMoney` in either direction, as the overdue totals do: writes bound balances and items apart,
  not this sum, and an error would take down the page where it gets fixed. The contract's description now says so; the schema
  is unchanged. The month's write checks already keep `incoming`, `outgoing` and `balance` within the bound.
- `upcomingDue` orders by due date, then name, then id, so a tie holds still between refetches.
- No migrations. No new automated tests (T7).

## 0.12.0 — 2026-11-05 — Overdue list

- `GET /api/v1/overdue-items`, with a full session: every `overdue` fixed, occasional and income item of a closed month,
  oldest month first, with backend `expenseTotal` and `incomeTotal`. Pending items of open months and card charges never
  appear (D3, D7), and no month's totals include these (D7).
- Settling needed no change: the done endpoints already keep an overdue item in its month, undo returns it to `overdue`, and
  a person-linked item records its payment dated within that month, so a late settlement still covers it (Phase 9).
- `expenseTotal` and `incomeTotal` stop at `kernel.MaxMoney`. Writes bound each month, not the months together, and an error
  would hide the rows that bring the sum back down. The contract's two descriptions now say so; the schema is unchanged.
- No new automated tests (T7).

## 0.11.0 — 2026-11-05 — Shared platforms

- Added `internal/platforms`: people, platforms, memberships and payments under the contract's routes, with a full session
  and, on mutations, CSRF. It reaches `periods` and `expenses`, and they reach it, only through application ports wired in
  `cmd/server`, each built on the caller's transaction.
- Shares are ⌈cost ÷ members⌉ in integer arithmetic (D10). An owner who is a member counts in the divisor, owes 0, gets no
  income item and cannot record a payment (400). A write that would leave someone owing past the money bound is a 400.
- Opening a month adds one income item per owing person in the same transaction as cloning templates, idempotent through
  `period_items_period_person_income_key`, and none for a month a payment covers (D4). An advisory lock serializes writes to
  people, platforms and payments with the reads that decide items.
- Migration 00007 adds `platform_payments.period_item_id`. Marking a person-linked item received records its payment,
  undoing removes exactly that one, recording a payment marks the month's item, and deleting a payment returns its item to
  pending, or overdue in a closed month, each in one transaction (D4, D8).
- A payment made through an item is dated within the item's month, so settling an overdue month still covers that month.
- Decided: a month's items are settled when it opens. Platform, membership and payment changes never touch items of months
  already open, and a person-linked item can be edited or deleted on its own, keeping its payment. Payments may overlap,
  coverage never stacks, and `coveredUntil` is the furthest any payment reaches. Deleting a person with payments is a 409.
- Contract: `GET /people` takes an optional `period`, which adds `coveredInPeriod` and `periodPaymentId`, because the paid
  check needs them from the backend (D1). `Person.coveredUntil` now says the payment that reaches furthest. Payment, delete
  and done endpoints document the sync above.
- No new automated tests (T7).

## 0.10.0 — 2026-11-04 — Credit cards

- `internal/cards` now covers the contract: create, get, edit and delete under `/api/v1/credit-cards`, with a full session
  and, on mutations, CSRF. Names and issuers follow the same rules as other names; statement and due days are plain
  integers from 1 to 31.
- Deleting a card relies on what migration 00006 already does: a trigger deactivates its active `card_charge` templates and
  the foreign keys clear the card on templates, items and platforms, all in that delete.
- `GET /periods/{period}/card-totals` sums every card charge of the month, whatever its status, in one statement. Every
  card has a row, with total 0 if it has no charges. Charges whose card was deleted form one last row with null `cardId`
  and `cardName`, only when there are any, and count toward `total`. A month not opened is a 404.
- Item writes now accept `kind=card_charge`, by extending `internal/expenses`: `cardId` is required on create and must name
  an existing card, and `dueDate` is a 400. On an edit, `cardId` moves a charge to another card, including one whose card
  was deleted, and null is a 400. Other kinds still refuse a card. `RequireWritable` is gone.
- Done on a card charge moves no total but `totals.cardCharge.pending`. Undo always returns it to `pending`, in a closed
  month too. Closing a month still leaves it as it is (D3).
- D3 confirmed: `totals.toPay` and `totals.toReceive` read only fixed, occasional and income rows, and were checked unchanged
  by every card charge write. `Se va` and `Balance` do not exist until Phase 11.
- Contract: no schema change; `PeriodItemUpdate`, `card-totals` and the done endpoint document the above. No migrations. No
  new automated tests (T7).

## 0.9.0 — 2026-11-04 — Income

- Income items can be added, edited, deleted and toggled through the existing item endpoints, by extending
  `internal/expenses`: the contract has one set of routes for every kind, and income shares its table, month lock, total
  check and toggle. Card charge rows stay a 400 on `kind` until Phase 8.
- `totals.toReceive` is Por llegar (D1): pending income, without received or overdue items. Filters narrow `items` only,
  so tags never touch it (D5).
- Income has no due date and no urgency. `dueDate` is a 400 on it, whether sent to create or to edit. `personId` cannot be
  set here; Phase 9 owns those rows (D4).
- `domain.Item.RequireExpense` became `RequireWritable`. A missing or malformed `dueDate` on an expense is still a 400 on
  that field, now reported by the domain, which is the one place that knows the rule for each kind.
- Contract: `PeriodItemTotals` gained a required `toReceive`, and `PeriodItemUpdate` documents the income rule. No
  migrations. No new automated tests (T7).

## 0.8.0 — 2026-11-04 — Expenses and tags

- Added `internal/expenses`: list a month's items, add, get, edit and delete, and the done toggle, under `/api/v1`, as the
  contract says. Every operation needs a full session; mutations also need CSRF. `GET /overdue-items` waits for Phase 10.
- Writes accept kinds `fixed` and `occasional` only. Income and card charge rows can be read and are totaled, but
  creating, editing, deleting or toggling one is a 400 on `kind` until Phases 7 and 8.
- `urgency` comes from a pure `domain.UrgencyOf` (D11), with today from a Clock port through `kernel.DateInBogota`.
- `totals.toPay` is Por pagar (D1): pending fixed + occasional, without done, overdue or card charge items. Filters narrow
  `items` only, so tags never touch totals (D5).
- Done is idempotent and keeps the first `doneAt`. Undo returns an item to `pending`, or to `overdue` in a closed month
  (D7). In a closed month, adding is a 409; toggling, editing and deleting are allowed, as the contract has no 409 for them.
- A create or edit that would take a month's total past `kernel.MaxMoney` is a 400 on `amount`. Every item write locks
  the month's row first, which serializes the check and any close without an advisory lock. If a total is still out of
  bound, from a direct database edit, the list answers 500 and logs it.
- `POST /periods` is a 400 when the active templates of one kind add up past the bound, because that month's list could
  otherwise never answer.
- Added `internal/tags`: list, create, edit and delete. A bad hex color is a 400 and a taken name is a 409.
- `dueDate` must be a real date from 2000-01-01 on, the same floor as `YearMonth`.
- Added `kernel.DateInBogota` and `platform.SystemClock`.
- Contract: `PeriodItemTotals` gained a required `toPay`. No migrations. No new automated tests (T7).

## 0.7.0 — 2026-11-04 — Periods and templates

- Added `internal/periods`: list, get, open and close periods, and list, get, create, edit and delete recurring
  templates under `/api/v1`, as the contract says. Every operation needs a full session; mutations also need CSRF.
- Opening clones the active templates only when it creates the month, in one transaction. A retry answers 200 and
  changes nothing; concurrent opens wait on the period's unique key, and `period_items_period_template_key` backs it.
- Closing marks pending fixed, occasional and income items `overdue`, leaves card charges pending (D3, D7), and
  changes nothing on a month already closed.
- Added `kernel.YearMonth`, whose `DateOn` clamps a due day to the month's last day (D11). Only fixed items get a due
  date, as the contract says.
- Refused template writes answer 400 on the field: the domain checks the kind's rules first, and the repository maps
  the `recurring_templates` CHECK and foreign key violations, so a card deleted meanwhile is a 400 too.
- Added `internal/cards` with only `GET /credit-cards`, ahead of Phase 8, because the web app's card selection needs it.
- Contract: `YearMonth` starts at 2000-01, which the `periods` table already required, and `updateRecurringTemplate`
  documents the 400 for a `dueDay` or `cardId` that does not fit the kind. No new automated tests (T7).

## 0.6.0 — 2026-11-04 — Accounts and savings

- Added `internal/accounts`: list, get, create, edit (name, kind, balance) and hard delete under `/api/v1/accounts`,
  as the contract says. Every operation needs a full session; mutations also need CSRF.
- The list returns `currentTotal` (non-savings balances) and `savingsTotal` (savings balances), computed in the domain.
- Added `kernel.MaxMoney` (2^53-1), `NewMoney` and an exact `SumMoney`; `ParseMoney` now enforces the bound. A balance
  beyond it is a 400 on the field. Decimals, exponents and quoted numbers are a 400 too.
- A create, edit or delete that would take a total beyond the bound is a 400, checked under an advisory lock so
  concurrent writes cannot pass it together. If a total is still out of bound, from a direct database edit,
  `GET /accounts` answers 500 and logs it rather than send a number the contract cannot carry.
- Names are trimmed and refuse control characters, which also keeps NUL away from PostgreSQL.
- Moved the auth adapter's JSON body decoding to `platform.DecodeJSON`, with no change in behavior, so modules share it.
- `ListAccounts` now breaks name ties by id, so the order is stable. No contract change. No new automated tests (T7).

## 0.3.0 — 2026-11-04 — Auth backend

- Added `internal/auth`: password login, TOTP second factor, MFA enrollment gated by a one-time secret, recovery
  codes, logout, logout-all and `GET /me`, under `/api/v1` as the contract says.
- Added `cmd/admin` with `create-owner`, `reset-password` and `rotate-totp-key`, and `make admin`. Passwords come
  from a no-echo prompt or stdin, never a flag.
- Passwords use Argon2id with `PASSWORD_PEPPER`. TOTP secrets use AES-256-GCM with a `totp_key_id` derived from
  the key. Replayed TOTP codes are rejected through `users.last_totp_step`.
- Sessions: opaque token, only its SHA-256 stored, `__Host-session`, 7-day idle and 30-day absolute expiry, a
  10-minute limit before the second factor, token rotation after it.
- Added middleware: session auth, double-submit CSRF with an `Origin` check, per-IP token buckets (IPv6 by /64), a
  per-account bucket on the steps behind the password, security headers, request IDs and one log line per request.
- Added exponential lockout, from 1 minute after 5 failures up to 1 hour. Password failures count per email and
  client IP in memory, on one code path for known and unknown emails, and never touch the account's lockout
  columns, so nobody without the password can lock the owner out. TOTP and recovery-code failures lock the account
  in the database.
- The server refuses to start with `INSECURE_DEV_COOKIES=true` and a non-local origin in `ALLOWED_ORIGINS`.
- Added the `auth_events` audit log. Passwords, tokens, secrets and codes are never logged.
- Added `TOTP_ENC_KEY_PREVIOUS`, `TRUSTED_PROXIES`, `ALLOWED_ORIGINS` and `INSECURE_DEV_COOKIES`; `make dev` now
  also receives `DATABASE_URL`.
- Contract: added the missing 429 to `startMfaEnrollment`.
- HIBP password checking, optional in the plan, is not implemented. No new automated tests (T7).

## 0.2.0 — 2026-11-04 — Contract and schema

- Added `contracts/openapi.yaml`, OpenAPI 3.1 v1 for every module, linted with Redocly CLI.
- Added goose migrations for the whole data model, with `down` sections and database-level idempotency for
  opening a month and for member income items.
- Pinned goose and sqlc through the `go.mod` `tool` directive; added `sqlc.yaml` and the first generated query.
- Added `make migrate`, `migrate-down`, `migrate-status`, `sqlc`, `seed` and `contract-lint`.
- Added `make seed`, an invented dev fixture that is safe to re-run and refuses non-local hosts.
- Added `kernel.Money`: whole COP in `int64`, with parse and format.
- Required a one-time enrollment secret to start MFA enrollment, with its hash and expiry on `users`, so a leaked
  password cannot enroll an authenticator before the owner does.
- Added `users` columns for login lockout and TOTP replay protection.
- Excluded card charges from what closing a month marks `overdue` (D3, D7).
- Deleting a card now deactivates its `card_charge` templates through a trigger; an active one must have a card.
- Capped `billing_months` and `months_covered` at 120, in the schema and the contract.
- Replaced the Makefile's blanket `export`: `TOTP_ENC_KEY` and `PASSWORD_PEPPER` reach only `make dev`, and every
  other value only the targets that use it.

## 0.1.0 — 2026-11-04 — Foundations

- Added the server skeleton: environment config, JSON logging, `GET /healthz` and graceful shutdown.
- Added the Makefile, local Postgres compose file for Docker or Podman, and `make keys`.
- Added `golangci-lint` config and the CI workflow.
- Added pending test cases for business decisions D1–D11.
