<!-- Architected and Developed by :- Faisal Hanif | imfanee@gmail.com. -->

# MiniSMS State Assessment

Point-in-time engineering assessment from a read-only onboarding pass. Every claim traces to a file path read during the pass. Where code and docs disagree, code wins.

## 1. Baseline status

- **Git HEAD:** `7c90afa` ("Add brand logo and favicon to the admin panel."). Working tree is NOT clean: uncommitted changes to `internal/api/dlr.go`, `internal/api/dlr_integration_test.go`, `internal/api/dlr_test.go`, `internal/dlr/processor.go`, `internal/smslog/timeline.go`, `internal/smslog/timeline_test.go`, `templates/admin/sms_logs/detail_modal.html`; untracked `internal/dlr/inbound.go`, `internal/dlr/inbound_test.go`, and `assets/` at repo root.
- **`go build ./...`** (from `minisms/`): success, no output.
- **`go vet ./...`**: clean, no findings.
- **`go test ./...`**: all packages pass. Postgres-backed integration tests (`internal/audit/*`, `internal/db/*_integration_test.go`) self-skip without `TEST_DATABASE_URL`, so the default green run does not exercise the money path against a real database.
- **Coverage (per package, `-cover`):** routing 100%, pathutil 75%, smslog 68%, adminauth 65%, config 56.5%, dlr 47%, carrier 42.6%, egress 33.8%, routecache 28%, billing 20.5%, api 16.1%, smpp/server 15.6%, invoice 7.1%, web 4.4%, sending 2.5%, db 0.5%, cmd 2.0%. The send/billing/persistence core has the thinnest safety net.

## 2. Architecture map

### Send path (POST /api/v1/sms/send)

```
api.SendSMS (sms.go:71)
  -> API-key auth middleware (api/middleware.go) + per-client rate limit
  -> validate E.164 dest, length 1..1600, DLR fields (sms.go:84-106)
  -> carrier.ResolveSenderID (senderid.go:21) [mode any/list/phone]
  -> sending.Service.Submit (submit.go:27)  [opens DB transaction]
       billing.LookupRate (rate.go) longest-prefix; SegmentInfo (GSM-7/UCS-2)
       lookupRouteEntry (route.go) longest-prefix -> primary + failover carriers
       lockAndCheckBalance (helpers.go:34) SELECT ... FOR UPDATE  <-- lock + check BEFORE dispatch
       db.CreateSMSLog status='pending'
       dispatchWithFailover (dispatch.go:23) -> per carrier:
            CheckCarrierEligibility (eligibility.go) sender-id policy + in-loss protection
            egressAttempts -> HTTP (carrier.DispatchToCarrier, SSRF-guarded) or SMPP (egress.Submit)
       on first accept: LookupCarrierCost -> DeductClientBalance + DeductCarrierBalance
                        + IncrementUsage + MarkSMSAccepted   <-- debit AFTER accept
       tx.Commit  <-- single transaction boundary
  -> 202 Accepted with submit outcome JSON (sms.go:143)
```

Balance is locked and checked before any carrier contact; both balances are debited only after a carrier accepts, all in one committed transaction. Optional refund-on-carrier-failure when all carriers fail.

### DLR path

Carrier posts to `GET|POST /api/v1/dlr` or `/api/v1/dlr/{message_id}` (unauthenticated by API key; authenticated by a per-carrier inbound secret via `dlr.VerifyInboundSecret`, checked from `?secret=`, `X-DLR-Secret`, or `X-Callback-Secret`). `api.HandleDLR` (dlr.go:19) parses query/JSON/form, resolves the MiniSMS message id, maps the carrier status to internal status (`dlr/status.go` + per-carrier map), updates `sms_logs`, then `dlr.Processor.HandleInbound` optionally forwards to the client `dlr_webhook_url` (template-built body, optional HMAC-SHA256 `X-MiniSMS-Signature`, redirects disabled).

### Admin/auth path

`/admin` routes: `UseForwardedHeaders` -> `CSRF` (gorilla/csrf, `X-CSRF-Token`, SameSite=Strict) -> protected group `SessionAuth` (cookie `minisms_session`, idle timeout) -> `LoadAdminUserMiddleware` -> `RequirePerm`/`RequireSuperAdmin`. Login is throttled per (username|IP); bootstrap super-admin is seeded from `ADMIN_USERNAME`/`ADMIN_PASSWORD_HASH` only when `admin_users` is empty (`db.EnsureBootstrapSuperAdmin`).

## 3. Invariant catalogue

- **Send success = 202**, JSON fields `status, message_id, client_ref, sender_id, sender_id_source, segments, charged, balance_remaining, carrier, failover_sequence, source/dest TON/NPI, dlr_requested, dlr_webhook_url` (`internal/api/sms.go:39-56, 143`).
- **Error contract:** 401 `SMS_ERR_UNAUTHORIZED`, 400 `SMS_ERR_INVALID_REQUEST`, 402 `SMS_ERR_INSUFFICIENT_BALANCE` (`{error, balance, required}`), 422 `SMS_ERR_NO_RATE`/`SMS_ERR_NO_ROUTE`/`SMS_ERR_SENDER_NOT_ALLOWED`, 503 `SMS_ERR_NO_ELIGIBLE_CARRIER`, 502 `SMS_ERR_CARRIER_FAILURE` (`internal/api/sms.go:160-184`, shape in `errors.go:9-12`).
- **Balance lock before dispatch:** `SELECT balance::text, (balance >= $2) AS enough FROM clients WHERE client_id=$1 FOR UPDATE` (`internal/sending/helpers.go:37-41`).
- **Debit after accept, one transaction:** `DeductClientBalance` + `DeductCarrierBalance` + `IncrementUsage` + `MarkSMSAccepted` then `tx.Commit` (`internal/sending/submit.go:158-179`).
- **SQL `deduct_client_balance`** uses `FOR UPDATE`, raises `INSUFFICIENT_BALANCE` (ERRCODE P0001), and inserts a ledger row with before/after balances (`deploy/minisms_db.sql:684-725`). `clients.balance` has `CHECK (balance >= 0)` (line 307); `ledger_entries.balance_after` has `CHECK (>= 0)` (line 353).
- **`deduct_carrier_balance`** uses `FOR UPDATE` and writes a ledger row but intentionally permits negative carrier balance; `carriers.balance` has no CHECK (`deploy/minisms_db.sql:613-650`, 117).
- **Append-only tables:** `ledger_entries`, `carrier_balance_entries`, `audit_log` each have no-UPDATE and no-DELETE triggers (`deploy/minisms_db.sql:498-528, 1395-1423`).
- **API-key auth:** `X-API-Key` or Bearer; 8-char prefix lookup then `subtle.ConstantTimeCompare(SHA256(salt||key))` (`internal/db/api_keys.go:107-142`).
- **Admin session:** cookie `minisms_session`, HttpOnly, SameSite=Strict, Secure in production, idle timeout from `admin_session_idle_minutes`; logout revokes (`internal/web/auth.go:178-244`, `middleware.go`).
- **CSRF:** gorilla/csrf, header `X-CSRF-Token`, Secure in production (`internal/web/middleware.go:28-48`).
- **RBAC:** 19 permission constants + super-admin bypass; `RequirePerm`/`RequireSuperAdmin` middleware; last-super-admin demotion blocked (`internal/adminauth/permissions.go:7-27`, `internal/db/admin_users.go:228-255`).
- **Password hashing:** bcrypt DefaultCost in app, cost 12 in `tools/hashpassword` (`internal/db/admin_users.go:257-267`).
- **Secrets at rest:** AES-256-GCM, 12-byte random nonce, key `SECRET_KEY` (32 bytes, validated each call); encrypts carrier auth headers, carrier/client SMPP passwords, DLR secrets (`internal/db/crypto.go`).
- **SSRF guard:** `ValidateEndpointURL` blocks non-http(s), loopback/RFC1918/link-local/unspecified, 169.254.169.254, and re-checks all DNS-resolved IPs; invoked on carrier HTTP dispatch (`internal/carrier/urlguard.go`, `dispatcher.go:67`).
- **Path safety:** `pathutil.ResolveUnder` / `ValidateRelativeDataPath` confine invoice/asset paths (`internal/pathutil/safe.go`).
- **Schema is a single hand-applied file** applied via `make schema`; no migration code in `internal/db/*`.

## 4. Risk register (do NOT fix in this session)

1. **DLR client-webhook forwarding bypasses the SSRF guard. (Security / SSRF, High) RESOLVED 2026-06-17.** Previously `internal/dlr/processor.go` issued the forward request to `fwd.URL` (client-controlled `dlr_webhook_url`) with no `ValidateEndpointURL` call. Fixed: `forwardEndpointValidator` (= `carrier.ValidateEndpointURL`) now runs on the built forward URL before `client.Do`; rejected forwards are recorded with `dlr_forward_status = "blocked"` and a timeline event, and no outbound call is made. Covered by `internal/dlr/processor_test.go` (private/loopback/link-local/metadata/non-http URLs rejected for both POST and GET forms, public host allowed). Residual: a DNS-rebinding TOCTOU window remains between validation and the request, the same posture as carrier dispatch.
2. **Carrier HTTP dispatch follows redirects without re-validating them. (Security / SSRF, Medium)** `dispatchHTTPClient` returns a default `http.Client` that follows redirects (`internal/carrier/dispatcher.go:31`); the SSRF guard runs only on the original `EndpointURL`, so a validated carrier endpoint can 302 to a private IP or metadata address. Direction: set a `CheckRedirect` that re-validates each hop, or disable redirects for carrier dispatch.
3. **Money and persistence paths are largely unverified by the default test run. (Concurrency / Money, High)** `sending` 2.5%, `db` 0.5%, `billing` 20.5%, `web` 4.4% coverage; the balance-concurrency and ledger-immutability tests in `internal/audit/` self-skip without `TEST_DATABASE_URL`. The strongest correctness guarantees (no oversell, append-only) are therefore not exercised in CI or local default runs. Direction: provision an ephemeral Postgres in CI and gate merges on these tests; add unit-level tests for `sending.Submit`.
4. **`HTTP_CARRIER_INSECURE_TLS` disables TLS verification globally for all carrier dispatch. (Security, Medium)** `internal/carrier/dispatcher.go:34` sets `InsecureSkipVerify: true` process-wide when enabled. Opt-in, but coarse. Direction: scope insecure TLS per carrier rather than globally, or document tightly.
5. **`record_carrier_payment` / `credit_client_balance` lack idempotency or duplicate guards. (Money, Medium)** `deploy/minisms_db.sql:550-602, 732-768` validate only `amount > 0`; a retried payment submission could double-credit. Direction: add an idempotency key or unique constraint on payment reference.
6. **DLR ingest endpoints are unauthenticated by API key and have no rate limit. (Abuse / DoS, Low-Medium)** `/api/v1/dlr*` rely solely on a per-carrier secret and are mounted without the API rate limiter (`cmd/minisms/main.go:440-444`). A leaked or guessable secret plus no throttle invites abuse. Direction: add per-source throttling and confirm secret entropy.
7. **In-flight uncommitted DLR-enrichment changes. (Maintainability, Low)** The new `internal/dlr/inbound.go` plus diffs to `dlr.go`/`processor.go`/`timeline.go` add inbound-callback metadata (with secret redaction) to the message timeline. They look complete and are accompanied by new tests, but are uncommitted. Direction: review, then commit or revert before further work to keep the tree clean.
8. **Session-token DB lookup is plain equality, not constant-time. (Security, Low)** `GetSessionByTokenHash` compares the stored SHA-256 token hash by SQL equality. Low risk given the token is a 32-byte random value, but worth noting.

## 5. Test-coverage gaps

- `internal/sending` (2.5%) and `internal/db` (0.5%): the core send orchestration and every table accessor are almost untested at unit level; correctness rests on the skipped integration tests.
- `internal/web` (4.4%): the large admin surface (RBAC enforcement, CSRF flows, form handlers) is barely covered.
- `internal/audit`: meaningful tests exist (balance concurrency, ledger immutability) but require `TEST_DATABASE_URL` and so do not run by default.
- `internal/invoice` (7.1%) and `internal/api` (16.1%): invoice PDF/line math and the public HTTP handlers have light coverage.
- Well covered and to be preserved as regression anchors: `internal/routing` (100%), `internal/pathutil` (75%), `internal/smslog` (68%), `internal/adminauth` (65%), and the SMPP decoder fuzz test (`internal/smpp/server/decode_fuzz_test.go`).

## 6. Candidate backlog (prioritized)

1. **(Small) DONE 2026-06-17.** Applied the SSRF guard to DLR client-webhook forwarding in `dlr/processor.go` before `client.Do`, with tests in `dlr/processor_test.go` that reject private/loopback/metadata webhooks. (Risk 1)
2. **(Medium)** Re-validate redirect targets in carrier HTTP dispatch via a `CheckRedirect` hook, or disable redirects; add a redirect-to-private-IP test. (Risk 2)
3. **(Medium)** Stand up an ephemeral Postgres in CI and run `internal/audit` integration tests (no-oversell, ledger immutability) so the money path is actually exercised on every change. (Risk 3)
4. **(Small/Medium)** Add unit tests for `sending.Submit`: happy-path 202, insufficient-balance 402, all-carriers-fail with and without refund, using a fake transport and DB seam. (Gap, Risk 3)
5. **(Small)** Review and commit (or revert) the in-flight DLR inbound-enrichment changes to clean the working tree. (Risk 7)
6. **(Small)** Add an optional port allowlist to `urlguard` (restrict to 80/443 or block known service ports). (Risk 2, defense in depth)
7. **(Medium)** Add idempotency/duplicate-payment protection to `record_carrier_payment` and credit functions in the schema. (Risk 5)
8. **(Small)** Add throttling to the unauthenticated DLR ingest endpoints. (Risk 6)

## 7. Open questions

- Is the negative carrier balance (overdraft) a deliberate business rule beyond the schema comment at `deploy/minisms_db.sql:609`? It reads intentional; confirm with the product owner before relying on it.
- Are there CI workflows or a `TEST_DATABASE_URL` provisioned anywhere outside the repo that already run the integration tests? None were found in the tree.
- The full set of HTMX fragment target IDs that admin templates depend on was not enumerated file by file; treat any template `id`/`hx-target` as a contract and confirm before renaming.
- Whether `dlr_webhook_url` is ever permitted as plain `http` in any code path (admin form requires https, but confirm no other writer bypasses that check).
