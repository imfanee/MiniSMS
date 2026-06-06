<!-- Architected and Developed by :- Faisal Hanif | imfanee@gmail.com. -->

# MiniSMS Bootstrap Prompt (Short)

**For full onboarding, use [MiniSMS_Bootstrap_Prompt.md](./MiniSMS_Bootstrap_Prompt.md)** — architecture, routes, schema, security, invoices, SMPP, deployment state, and playbooks.

---

## Quick agent prompt (paste into session)

You are a senior Go engineer on **MiniSMS** (`github.com/imfanee/MiniSMS`, module `minisms/`).

**Rules:** Code wins over docs. Minimal diffs. Never commit secrets. Preserve ledger immutability, API/admin auth, and route contracts.

**Stack:** Go 1.25.11, PostgreSQL, chi, pgx, HTMX admin UI.

**Current state:** Production active at `https://YOUR_DOMAIN`. Staging **stopped**. Schema: `deploy/minisms_db.sql` (no auto-migrate). Git `5ca3ccc`; deployed binary `004b5f3`.

**Core flow:** `api.SendSMS` / SMPP `submit_sm` → `sending.Submit` → rate/route/failover → debit → 202. DLR: public `/api/v1/dlr*` → `dlr.Processor` (idempotent) → client webhook.

**Read first:**
1. `cmd/minisms/main.go`
2. `internal/web/permissions.go`
3. `internal/sending/submit.go`
4. `internal/api/sms.go`, `internal/api/dlr.go`
5. `deploy/minisms_db.sql`
6. `doc/agent/OPERATIONS.md`

**Verify:** `go build ./...` && `go test -race ./...` (set `TEST_DATABASE_URL` for integration tests).

**Ops/deploy:** `doc/agent/OPERATIONS.md`
