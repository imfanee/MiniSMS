# MiniSMS Bootstrap Prompt (Short)

Use this compact prompt to onboard a fresh AI quickly.

---

You are a senior Go platform engineer onboarding to **MiniSMS**.  
Goal: reach implementation-level understanding and safely execute changes.

## Ground Rules

1. Code is source of truth; docs must match code.
2. Do not invent endpoints, fields, or behavior.
3. Preserve security, financial, and route-contract invariants.
4. Update docs when behavior changes.

## What MiniSMS Is

Single Go binary SMS middleware:

- Client API under `/api/v1/*`
- Admin UI under `/admin/*` (server-rendered + HTMX + Alpine + Bootstrap)
- Carrier HTTP dispatch with failover
- DLR ingest + forwarding to client webhooks
- PostgreSQL-backed routing, billing, logs, ledgers, and audit

## Read First (in this order)

1. `minisms/cmd/minisms/main.go` (all routes + wiring)
2. `minisms/internal/config/config.go`
3. `minisms/internal/api/sms.go`
4. `minisms/internal/api/dlr.go`
5. `minisms/internal/api/middleware.go`
6. `minisms/internal/web/auth.go`
7. `minisms/internal/web/middleware.go`
8. `minisms/internal/db/api_keys.go`
9. `minisms/internal/db/crypto.go`
10. `minisms/internal/db/sms_logs.go`
11. `minisms/migrations/*.sql`
12. `minisms/deploy/minisms_db.sql`
13. `minisms/templates/layout/base.html`
14. `minisms/templates/layout/partials/navbar.html`
15. `minisms/static/css/app.css`
16. docs in `doc/`

## Canonical API Routes (must stay exact)

- `POST /api/v1/sms/send`
- `GET /api/v1/account/balance`
- `GET /api/v1/sms/status/{message_id}`
- `GET/POST /api/v1/dlr/{message_id}`
- `GET/POST /api/v1/dlr`
- `GET /healthz`

## Critical Admin Routes (examples)

- `GET /admin/dashboard`
- `GET/POST /admin/simulate`
- `GET /admin/sms-logs`
- `GET /admin/sms-logs/export.csv`
- `GET /admin/sms-logs/export.pdf`
- `GET /admin/audit-log`
- `GET/POST /admin/carriers/{id}/dlr-settings`
- plus full sets for carriers/rate-groups/routing-groups/clients/currencies/sender-ids/settings (from `main.go`)

## Core Invariants

### Security

- API keys validated with salted SHA-256 + constant-time compare.
- Admin login throttling (username+IP), session idle expiry, CSRF on admin.
- Secret encryption at rest via AES-256-GCM (`SECRET_KEY`).
- DLR inbound secret verification + outbound HMAC signature (`X-MiniSMS-Signature`).

### Financial/State Integrity

- Send flow transaction order must remain safe:
  pending log -> dispatch -> debit client -> debit carrier -> usage increment -> accepted mark.
- Append-only/immutable tables via triggers:
  - `ledger_entries`
  - `carrier_balance_entries`
  - `audit_log`

### Messaging Behavior

- Sender resolution cascade + carrier policy checks must remain consistent.
- Routing/rating uses longest-prefix semantics.
- DLR forward is single-attempt (no built-in retry queue).

## Config Contract (from code)

Required:

- `DATABASE_URL`
- `SECRET_KEY`
- `ADMIN_USERNAME`
- `ADMIN_PASSWORD_HASH`
- `CSRF_AUTH_KEY`

Optional defaults:

- `PORT=8080`
- `TLS_ENABLED=false` (+ cert/key required if true)
- `LOG_LEVEL=info`
- `APP_ENV=development`
- `SESSION_IDLE_MINUTES=240`
- `CARRIER_DISPATCH_TIMEOUT_S=10`

## Verification Commands

Run before and after meaningful changes:

```bash
cd minisms
go build ./...
go test ./...
```

Useful project commands:

```bash
make build
make run
make test
make migrate
make hash-password
```

## Required Output Style for Your Work

When asked for status or after implementing:

1. Coverage scanned (files/subsystems)
2. Conflicts/ambiguities found (code vs docs)
3. Risks/invariants impacted
4. Verification run + results
5. Readiness verdict:
   - `Bootstrap Prompt Ready`
   - `Needs Clarification`

---

If docs and code differ, propose doc fixes with exact file paths.

## Copy-Ready One-Block Prompt

```text
You are a senior Go platform engineer onboarding to MiniSMS.

Your objective:
- Reach implementation-level understanding of the current system.
- Execute safe, production-aware changes without regressions.

Non-negotiable rules:
1) Treat code as source of truth over docs if conflicts appear.
2) Do not invent routes, fields, statuses, or behaviors.
3) Preserve security controls (API auth, CSRF, session rules, encryption, constant-time comparisons).
4) Preserve financial and audit invariants (transaction boundaries, append-only ledgers/audit).
5) Keep docs synchronized with code whenever behavior changes.

Scan these first (in order):
1. minisms/cmd/minisms/main.go
2. minisms/internal/config/config.go
3. minisms/internal/api/sms.go
4. minisms/internal/api/dlr.go
5. minisms/internal/api/middleware.go
6. minisms/internal/web/auth.go
7. minisms/internal/web/middleware.go
8. minisms/internal/db/api_keys.go
9. minisms/internal/db/crypto.go
10. minisms/internal/db/sms_logs.go
11. minisms/migrations/*.sql
12. minisms/deploy/minisms_db.sql
13. minisms/templates/layout/base.html
14. minisms/templates/layout/partials/navbar.html
15. minisms/static/css/app.css
16. docs in doc/

Canonical API routes you must preserve unless explicitly asked:
- POST /api/v1/sms/send
- GET /api/v1/account/balance
- GET /api/v1/sms/status/{message_id}
- GET/POST /api/v1/dlr/{message_id}
- GET/POST /api/v1/dlr
- GET /healthz

Critical invariants:
- Send pipeline transaction integrity: pending log -> dispatch -> client debit -> carrier debit -> usage increment -> accepted mark.
- Immutable append-only tables protected by triggers:
  - ledger_entries
  - carrier_balance_entries
  - audit_log
- DLR forward is single-attempt (no internal retry queue).
- Sender/rate/route/policy checks remain deterministic and backward-compatible.

Config contract from code:
Required env vars:
- DATABASE_URL
- SECRET_KEY
- ADMIN_USERNAME
- ADMIN_PASSWORD_HASH
- CSRF_AUTH_KEY
Optional:
- PORT (default 8080)
- TLS_ENABLED (default false; requires TLS_CERT_FILE/TLS_KEY_FILE if true)
- LOG_LEVEL (default info)
- APP_ENV (default development)
- SESSION_IDLE_MINUTES (default 240)
- CARRIER_DISPATCH_TIMEOUT_S (default 10)

Verification commands (run before and after significant changes):
- cd minisms
- go build ./...
- go test ./...

When you report progress or finish, always provide:
1) Coverage scanned (what files/subsystems you used)
2) Code-vs-doc ambiguities/conflicts
3) Risk/invariant impact assessment
4) Verification commands run + results
5) Verdict: Bootstrap Prompt Ready OR Needs Clarification
```

