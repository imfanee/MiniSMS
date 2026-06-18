<!-- Architected and Developed by :- Faisal Hanif | imfanee@gmail.com. -->

# CLAUDE.md - MiniSMS Working Contract

This file is auto-loaded by Claude Code on every session. It supersedes `doc/MiniSMS_Bootstrap_Prompt.md` as the standing contract for working in this repository. The point-in-time engineering assessment lives in `doc/agent/STATE_ASSESSMENT.md`.

## System summary

MiniSMS is a production Go SMS middleware gateway shipped as a single binary (`minisms/cmd/minisms/main.go`). Northbound it exposes a client REST API under `/api/v1` (send SMS, check balance, query status) plus carrier-facing DLR ingest, and a server-rendered admin UI under `/admin` (HTMX + Bootstrap 5 + Alpine, session + CSRF + RBAC). Southbound it dispatches to carriers over HTTP templates or SMPP, rates and bills each message against client and carrier balances, and forwards delivery receipts back to clients. Persistence is PostgreSQL via pgx; secrets are encrypted at rest with AES-256-GCM. Optional SMPP ingress (ESME server) is gated by `SMPP_SERVER_ENABLED`.

## Build / run / test (run from `minisms/`, not the repo root)

The Go module root is `/usr/src/MiniSMS/minisms` (the repo root above it holds `minisms/`, `doc/`, `assets/`, `certs/`). All `go` and `make` commands must run from `minisms/`.

- `go build ./...` - verified clean at HEAD.
- `go vet ./...` - verified clean at HEAD.
- `go test ./...` - verified all packages pass. Integration tests that need Postgres (`internal/audit/*`, `internal/db/*_integration_test.go`) self-skip unless `TEST_DATABASE_URL` is set, so a green run does NOT prove the money path.
- `go test ./... -cover` - coverage is uneven; see STATE_ASSESSMENT.md.
- `make run` / `make dev` (dev loads `.env`) - needs a non-production `DATABASE_URL`.
- `make hash-password` - bcrypt hasher (cost 12) for `ADMIN_PASSWORD_HASH` (`tools/hashpassword`).
- `make schema DB_URL=postgres://...` - applies `deploy/minisms_db.sql`. Schema is NOT auto-applied at startup.

## Repository map

- `minisms/cmd/minisms/main.go` - entry point: load config, open pgx pool, `EnsureBootstrapSuperAdmin`, parse embedded templates, `runtime.Start`, mount chi routes, listen.
- `minisms/assets.go` - `//go:embed templates` and `//go:embed static`. Editing templates/static requires a rebuild.
- `minisms/deploy/minisms_db.sql` - single consolidated schema (tables, views, functions, triggers, seeds). Hand-applied via `make schema`.
- `internal/api/` - public REST edge: `sms.go` (send), `account.go` (balance, status), `dlr.go` (carrier DLR ingest), `middleware.go` (API-key auth + per-client rate limit), `errors.go` (JSON error shape).
- `internal/web/` - admin UI surface: `auth.go`/`middleware.go` (session + CSRF), `permissions.go`/`page_admin.go` (RBAC), plus carriers, clients, rate_groups, routing_groups, invoices, sms_logs, settings, dashboard, simulate, smpp_admin, currencies, sender_ids, audit_log, diagnoses_send.
- `internal/sending/` - core send pipeline: `submit.go` (transaction orchestration), `dispatch.go`/`failover.go` (carrier attempts), `route.go`, `transport.go`/`http_transport.go`, `helpers.go` (balance lock), `outcome.go`.
- `internal/routing/` (longest-prefix matcher) and `internal/routecache/` (RWMutex-guarded in-memory route + carrier cache; reload via `web/cache_reload.go`).
- `internal/billing/` - `balance.go`, `rate.go`, `carrier_charge.go`: rate lookup, segment math, balance deduction wrappers over SQL functions.
- `internal/carrier/` - `dispatcher.go` (HTTP dispatch), `urlguard.go` (SSRF guard), `eligibility.go`, `senderid.go`/`client_sender.go`, `template.go`, `smpp.go` (TON/NPI).
- `internal/smpp/server/` - inbound ESME SMPP listener: bind-rate limiting, CIDR allowlist, fuzz-tested decoder (`decode_fuzz_test.go`).
- `internal/smpp/egress/` - outbound carrier SMPP pool: per-carrier `sessionGroup` of N parallel ESME binds (`carriers.smpp_bind_count`, 1..16, editable in the admin carrier SMPP panel), round-robin `submit_sm`, decrypts SMPP password at connect. A `deliver_sm` DLR may land on any bind; correlation is carrier-wide by `carrier_message_id` (`dlr.HandleCarrierSMPP` -> `db.ResolveSMSLogMessageID`). Changing the bind count rebinds within ~60s (manager refresh tick). Live example: carrier `Airtel DRC Direct (SMPP)` runs 8 binds to the Airtel DRC SMSC (see project memory `minisms-airtel-smpp`). Session events stream to a per-carrier in-memory ring buffer (`internal/smpp/egresslog`, no secrets, no disk); the admin carrier SMPP panel has an `Open SMPP logs` button that live-tails them over SSE (`GET /admin/carriers/{id}/smpp-logs` + `/stream`, gated by `PermCarriersView`, GET-only so CSRF-exempt, `X-Accel-Buffering: no` for nginx, strict per-response CSP nonce on the popup).
- `internal/dlr/` - `processor.go` (ingest + client forwarding), `status.go` (carrier status -> internal mapping), `template.go`, `inbound.go` (callback metadata for timeline).
- `internal/billing/` + `internal/audit/` - money + append-only audit; the audit package holds the balance-concurrency and ledger-immutability integration tests.
- `internal/db/` - data-access layer, one file per table, pgx-based. `crypto.go` (AES-256-GCM), `api_keys.go`, `admin_users.go`, `*_ledger.go`, `audit_log.go`, `queries.go`. No migration/auto-apply logic.
- `internal/pathutil/` (path-traversal guard), `internal/adminauth/` (permission constants/checks), `internal/config/` (env surface), `internal/runtime/` (app wiring), `internal/models/`, `internal/invoice/` (gofpdf invoices), `internal/smslog/` (per-message timeline).

## Non-negotiable invariants (confirm in source before changing)

- **Send returns HTTP 202** with JSON `{status, message_id, sender_id, segments, charged, balance_remaining, carrier, failover_sequence, ...}` (`internal/api/sms.go:143`). Error codes are contracts: 401 unauthorized, 400 invalid request, 402 `SMS_ERR_INSUFFICIENT_BALANCE` (`{error, balance, required}`), 422 no-rate / no-route / sender-not-allowed, 503 no-eligible-carrier, 502 carrier-failure. Do not change codes or JSON field names unless that is the task.
- **Billing order and locking:** balance is locked and checked with `SELECT ... FOR UPDATE` BEFORE any carrier contact (`internal/sending/helpers.go:34-43`, called at `submit.go:67`); the client and carrier balances are debited only AFTER a carrier accepts, inside the same transaction committed at the end (`submit.go:158-179`). Preserve this order and the single-transaction boundary. Optional refund-on-carrier-failure exists; keep it.
- **Append-only ledgers and audit:** `ledger_entries`, `carrier_balance_entries`, and `audit_log` block UPDATE and DELETE via triggers in `deploy/minisms_db.sql`. Never add code that mutates or deletes these rows. `clients.balance` has a DB `CHECK (balance >= 0)`; the SQL `deduct_client_balance` raises `INSUFFICIENT_BALANCE` under `FOR UPDATE`. Carrier balance is intentionally allowed to go negative (overdraft).
- **API-key auth:** key comes from `X-API-Key` or `Authorization: Bearer`; lookup is by 8-char prefix then `crypto/subtle.ConstantTimeCompare` of `SHA256(salt || key)` (`internal/db/api_keys.go:107-142`). Keep the constant-time compare and never log raw keys.
- **Admin auth:** session cookie `minisms_session` (HttpOnly, SameSite=Strict, Secure in production), idle timeout from `admin_session_idle_minutes`, gorilla/csrf with header `X-CSRF-Token` on `/admin` (`internal/web/middleware.go`). RBAC is 19 string permissions plus super-admin bypass (`internal/adminauth/permissions.go`, enforced via `RequirePerm`/`RequireSuperAdmin`). Do not weaken these or remove the last-super-admin demotion guard.
- **Secrets at rest:** AES-256-GCM with a 12-byte random nonce, key = `SECRET_KEY` (64 hex chars / 32 bytes), validated on every call (`internal/db/crypto.go`). It encrypts carrier auth headers, carrier/client SMPP passwords, and DLR secrets. Never store these in plaintext.
- **Outbound SSRF guard:** `carrier.ValidateEndpointURL` (`internal/carrier/urlguard.go`) blocks non-http(s) schemes, loopback/RFC1918/link-local/unspecified IPs, cloud-metadata (169.254.169.254), and resolves DNS to re-check every resolved IP. It is invoked on carrier HTTP dispatch (`dispatcher.go:67`) and on DLR client-webhook forwarding (`dlr/processor.go`, via `forwardEndpointValidator`, blocked forwards are recorded with status `blocked`). Keep both call sites. Known gap (see risk register): carrier dispatch follows redirects without re-validating them.
- **Carrier query templating:** values substituted into a carrier URL **query** template must go through `carrier.InjectQueryVariables` (URL-encodes each value, so a literal `+` in from/to becomes `%2B`, not a space, at the gateway); `carrier.InjectVariables` (verbatim) is for **body** templates only. The dispatch query path is `internal/sending/dispatch.go` -> `carrier.DispatchToCarrier`.
- **DLR ingest (multi-bit dlr-mask):** `db.UpdateDLRReceived` applies only while no final status (delivered/undelivered/rejected) is stored and returns whether it applied; an intermediate receipt (Kamex SMSC ACK / queued from `dlr-mask=31`) must never block or overwrite the final. Keep `dlr.IsFinalStatus` / `shouldForwardDLR` semantics (forward on first receipt, then again only on reaching a final state).
- **Path safety:** `pathutil.ResolveUnder` / `ValidateRelativeDataPath` confine invoice/asset file paths under a base dir; use them for any new filesystem path derived from user/admin input.
- **Schema coupling:** the schema is one hand-applied file. Any column change is a coordinated `make schema` operation, not a startup side effect. Keep `deploy/minisms_db.sql` and the `internal/db/*` accessors in sync.

## Change protocol for future sessions

1. Read the relevant code and confirm current behavior before changing it. Code wins over docs; note any drift.
2. Make the smallest correct diff; match existing patterns, naming, and test style.
3. For anything touching money, auth, routing, SSRF/path safety, or SMPP decoding: add or extend a test that would have caught the bug, and preserve the append-only and `FOR UPDATE` guarantees.
4. Never change a public HTTP status code, JSON field name, or HTMX fragment target unless that change is the explicit task.
5. After a behavior change, update the affected `doc/*.md`, and `deploy/minisms_db.sql` if columns changed (and note the manual `make schema` step).
6. Always run `go build ./...`, `go vet ./...`, and `go test ./...` from `minisms/` before declaring done; report what you ran. Where money/concurrency is involved, run the integration tests with a `TEST_DATABASE_URL`.
7. Keep the author header as the first line of every new file. Do not use the em-dash character in docs; use commas, colons, parentheses, or separate sentences.

## Conventions and environment facts

- Every Go/Markdown file begins with `// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.` (or the `<!-- ... -->` form). New files must keep it.
- No em-dash characters in any docs you write.
- Never commit secrets or real DSNs; use placeholders.
- Templates and static assets are embedded (`assets.go`), so rebuild after editing them.
- Schema is applied manually via `make schema DB_URL=...`; it is never auto-applied at startup. On the production host the **live DB schema is authoritative**, not `deploy/minisms_db.sql`; apply only additive deltas (rehearse on a restored copy first). See `doc/agent/OPERATIONS.md` and the project memory `minisms-prod-deployment`.
- **Production deployment:** single binary at `/usr/local/bin/minisms` (systemd unit `minisms`, env `/etc/minisms/minisms.env`, plain HTTP on `:8080` behind nginx); `/opt/minisms` is data-only (`assets/`, `invoices/`). Build from `/usr/src/MiniSMS/minisms`, back up, swap the binary, `systemctl restart`, health-check `http://127.0.0.1:8080/healthz`. Load the env file literally (its `ADMIN_PASSWORD_HASH` contains `$`; never `set -a; .` it).
- SMPP ingress is gated by `SMPP_SERVER_ENABLED` (default false). `HTTP_CARRIER_INSECURE_TLS` and `TLS_ENABLED`/`SMPP_TLS_ENABLED` gate TLS behavior.
- Required env: `DATABASE_URL`, `SECRET_KEY` (64 hex), `CSRF_AUTH_KEY` (64 hex), `ADMIN_USERNAME`, `ADMIN_PASSWORD_HASH`. See `internal/config/config.go` for the full surface and defaults.
