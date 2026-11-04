# Changelog

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
