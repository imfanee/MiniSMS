# MiniSMS Product Documentation

MiniSMS is a production Go SMS middleware gateway that sits between client applications and downstream SMS carriers. It exposes a northbound REST API for clients and a server-rendered Admin UI for operations teams. It handles routing, pricing, sender ID policy enforcement, balance control, delivery receipt (DLR) forwarding, and financial/audit traceability.

> This document describes the current implemented state (v1.2 + DLR) in present tense.

## 1. Overview

MiniSMS provides controlled SMS dispatch with operational and financial governance.

- **Primary users**
  - Platform operators (Admin UI)
  - Integrating client systems (REST API)
  - Finance/ops teams (ledgers, logs, reports, audits)
- **Architecture position**
  - Client app -> MiniSMS -> Carrier
  - Carrier DLR -> MiniSMS -> Client webhook
- **Core value**
  - Centralized carrier abstraction
  - Prefix-based rating and routing with failover
  - Strict sender ID policy and allowlists
  - Prepaid balance checks before send
  - Append-only ledgers and audit traceability
  - DLR webhook forwarding with optional HMAC signature

## 2. Architecture

MiniSMS runs as a single Go binary with a three-layer model:

1. **Transport/UI layer**
   - REST API under `/api/v1/`
   - Admin UI under `/admin/`
   - Static assets under `/static/`
   - Health endpoint under `/healthz`
2. **Domain/application layer**
   - Sender ID resolution
   - Rate lookup and segment calculation
   - Routing and carrier failover
   - DLR parsing, normalization, forwarding
3. **Data layer**
   - PostgreSQL 15+ (pgx)
   - Append-only client and carrier ledgers
   - SMS logs, system settings, audit log

Admin UI uses server-side `html/template` with Bootstrap 5.3. HTMX powers partial updates and Alpine.js powers local UI interactions.

```text
+------------------+        +----------------------+        +------------------+
| Client App       |        | MiniSMS             |        | Carrier          |
| (API consumer)   | -----> | /api/v1/sms/send    | -----> | HTTP SMS API     |
+------------------+        | Routing + Billing   |        +------------------+
        ^                   | + Sender ID Policy   |                 |
        |                   +----------------------+                 |
        |                          |                                 |
        |                          v                                 v
        |                   +----------------------+        +------------------+
        +-------------------| Client Webhook       |<-------| DLR Callback     |
                            | (forwarded DLR POST) |        | /api/v1/dlr/...  |
                            +----------------------+        +------------------+
```

## 3. Core Concepts

### 3.1 Currencies

MiniSMS has a currency registry and enforces currency selection from a predefined list in admin forms. The system seeds 20 currencies in migration data and uses currency on client balance and charging records.

### 3.2 Sender IDs

MiniSMS supports three sender ID types:

- `alpha`
- `numeric`
- `e164`

Resolution cascade (effective sender):

1. Client-provided `from`
2. Client default sender ID
3. Carrier default sender ID
4. System `default_sender_id`

Carrier policy enforcement supports:

- `any`
- `numeric`
- `e164`
- `list`
- `none`

Client-level allowlists are enforced before dispatch.

### 3.3 Carriers

Each carrier stores:

- Endpoint URL and method
- Auth headers
- Request template (body/query/content type)
- Sender ID policy/default sender
- Optional carrier rate group
- In-loss protection controls
- DLR callback settings and status map
- SMPP TON/NPI behavior (static or dynamic)

Carrier financial ledger is append-only (`payment`, `charge`, `adjustment`, `refund`) and carrier usage counters are updated on successful dispatch.

### 3.4 Request Templates and Variable Injection

Carrier templates support variable replacement at dispatch time.

Common variables:

- `{{to}}`
- `{{from}}`
- `{{message}}`
- `{{message_id}}`
- `{{timestamp}}`
- `{{client_id}}`
- `{{dlr_callback_url}}`
- `{{source_addr_ton}}`
- `{{source_addr_npi}}`
- `{{dest_addr_ton}}`
- `{{dest_addr_npi}}`

Example (JSON):

```json
{
  "to": "{{to}}",
  "from": "{{from}}",
  "text": "{{message}}",
  "client_ref": "{{client_id}}",
  "callback_url": "{{dlr_callback_url}}",
  "source_addr_ton": "{{source_addr_ton}}",
  "source_addr_npi": "{{source_addr_npi}}"
}
```

Example (form):

```text
to={{to}}&from={{from}}&message={{message}}&dlr={{dlr_callback_url}}
```

Example (XML):

```xml
<sms>
  <to>{{to}}</to>
  <from>{{from}}</from>
  <text>{{message}}</text>
  <callback>{{dlr_callback_url}}</callback>
</sms>
```

### 3.5 Rate Groups

Rate groups define destination-prefix prices with optional date windows. Matching uses longest-prefix-wins, with `*` as catch-all fallback.

### 3.6 Routing Groups

Routing groups define route entries by prefix. Each route can include:

- primary carrier
- failover 1 carrier
- failover 2 carrier

Selected carrier index is stored as `failover_sequence` (`0`, `1`, `2`).

### 3.7 Clients

Clients include:

- API key authentication
- Status (`active` required for API access)
- Prepaid balance model
- Assigned rate/routing groups
- Sender ID allowlist and defaults
- `allow_in_loss_delivery` behavior
- Default DLR webhook URL + encrypted secret

### 3.8 DLR Webhook Forwarding

DLR behavior includes:

- `dlr` in send request (`YES`/`NO`)
- `dlr_url` per-message override (HTTPS only)
- Client default webhook as fallback when `dlr=YES`
- Carrier callback URL template with `{{message_id}}`
- Public inbound DLR endpoints:
  - `GET /api/v1/dlr/{message_id}`
  - `POST /api/v1/dlr/{message_id}`
  - `GET /api/v1/dlr`
  - `POST /api/v1/dlr`
- Carrier secret verification:
  - query `secret`
  - header `X-DLR-Secret`
  - header `X-Callback-Secret`
- Status extraction:
  - default field `status`
  - carrier override via `dlr_status_field`
  - mapping via JSON `dlr_status_map`
- Standard normalized DLR statuses:
  - `delivered`
  - `undelivered`
  - `rejected`
  - `unknown`

SMPP parameters in forwarded payload:

- `source_addr_ton`
- `source_addr_npi`
- `dest_addr_ton`
- `dest_addr_npi`

These are resolved per message from per-carrier config:

- static integer values, or
- `dynamic` (libphonenumber-based resolution)

HMAC forwarding:

- If client DLR secret exists, MiniSMS signs outbound JSON with HMAC-SHA256.
- Header format: `X-MiniSMS-Signature: sha256=<hex>`.

### 3.9 Carrier Financial Ledger

Carrier ledger is append-only and tracks balance-affecting events:

- payment
- charge
- adjustment
- refund

Writes occur in DB transactions to keep carrier balance and ledger entries consistent.

### 3.10 Client Balance Ledger

Client balance is prepaid. MiniSMS checks and locks balance before send and returns `402` on insufficient funds. Debits and refunds are persisted in append-only balance ledger entries.

## 4. SMS Send Pipeline

1. Authenticate client API key (`X-API-Key` or `Authorization: Bearer`).
2. Validate request (`to`, `message`, `dlr`, optional `dlr_url`).
3. Resolve sender ID (4-step cascade + policy checks).
4. Resolve DLR webhook target:
   - if `dlr=NO` -> no webhook
   - if `dlr=YES` + `dlr_url` -> use request URL
   - else use client default URL
   - persist `dlr_requested` and `dlr_webhook_url` in pending SMS log
5. Lookup client rate (longest-prefix-wins).
6. Calculate encoding/segments and total charge.
7. Lookup route entry (longest-prefix-wins with failovers).
8. Begin transaction and lock/check client balance.
9. Create pending SMS log.
10. Dispatch to primary/failover carrier sequence, skipping ineligible carriers.
11. On success, debit client, debit carrier, update usage, mark log accepted, commit, return `202`.

Asynchronous return path after step 11:

- Carrier sends DLR callback later.
- MiniSMS resolves message, verifies secret, normalizes status.
- MiniSMS updates SMS log DLR fields.
- If DLR requested and webhook exists, MiniSMS forwards DLR payload to client webhook.

## 5. DLR Webhook Reference

### 5.1 End-to-end flow

1. Client sends SMS with optional `dlr=YES`.
2. Carrier accepts message.
3. Carrier later calls MiniSMS DLR endpoint.
4. MiniSMS validates and normalizes DLR.
5. MiniSMS forwards DLR to client webhook (single attempt).

### 5.2 Client webhook payload

MiniSMS forwards JSON like:

```json
{
  "event": "dlr",
  "message_id": "4ac6d6e2-03ad-4728-a271-59a8438c3192",
  "client_ref": "order-7781",
  "to": "+447700900123",
  "from": "MiniSMS",
  "dlr_status": "delivered",
  "carrier": "Carrier-A",
  "failover_sequence": 0,
  "received_at": "2026-04-26T12:00:05Z",
  "dlr_received_at": "2026-04-26T12:01:14Z",
  "segments": 1,
  "charged": "0.035000",
  "source_addr_ton": 5,
  "source_addr_npi": 0,
  "dest_addr_ton": 1,
  "dest_addr_npi": 1
}
```

### 5.3 HMAC signature verification

If configured, verify `X-MiniSMS-Signature`:

```text
header = "sha256=" + hex(HMAC_SHA256(secret, raw_body_bytes))
```

Client verification steps:

1. Read raw request body bytes (exact bytes, before JSON reformatting).
2. Compute HMAC-SHA256 with shared secret.
3. Compare with header value using constant-time comparison.

### 5.4 SMPP TON/NPI parameters

TON and NPI describe address type/numbering plan metadata used by many SMPP-integrated carriers. MiniSMS includes these values in template variables and client DLR payload.

Per carrier, each of the four fields can be:

- static numeric value
- `dynamic` (resolved per message)

Dynamic defaults aim to classify alphanumeric sender IDs and E.164 destinations consistently.

### 5.5 DLR status values

Normalized values:

- `delivered`
- `undelivered`
- `rejected`
- `unknown`

MiniSMS also maps common aliases (for example `delivrd`, `ok`, `success`, `failed`, `undeliv`).

### 5.6 Retry policy

MiniSMS performs one outbound webhook attempt per inbound DLR event. Failed forwarding is recorded (`dlr_forward_status`, attempts metadata) and not retried automatically.

> Operators should monitor failed DLR forwards and recover externally if needed.

## 6. Northbound REST API Reference

### Authentication

Send API and account APIs require client API key.

- `X-API-Key: <key>`
- `Authorization: Bearer <key>`

DLR inbound endpoints are public by path design and rely on carrier-side shared secret verification.

### 6.1 POST /api/v1/sms/send

Request fields:

| Field | Type | Required | Notes |
|---|---|---:|---|
| `to` | string | Yes | E.164, `+` and digits |
| `from` | string | No | Sender ID; policy-checked |
| `message` | string | Yes | 1..1600 chars |
| `client_ref` | string | No | Client correlation value |
| `dlr` | string | No | `YES` or `NO` (blank treated as `NO`) |
| `dlr_url` | string | No | HTTPS URL; per-message override |

Request example:

```json
{
  "to": "+447700900123",
  "from": "MiniSMS",
  "message": "Hello from MiniSMS",
  "client_ref": "invoice-938",
  "dlr": "YES",
  "dlr_url": "https://client.example.com/hooks/dlr"
}
```

Success (`202 Accepted`) example:

```json
{
  "status": "accepted",
  "message_id": "4ac6d6e2-03ad-4728-a271-59a8438c3192",
  "client_ref": "invoice-938",
  "segments": 1,
  "charged": "0.035000",
  "balance_remaining": "49.965000",
  "carrier": "Carrier-A",
  "failover_sequence": 0,
  "dlr_requested": true,
  "dlr_webhook_url": "https://client.example.com/hooks/dlr"
}
```

Primary send errors:

- `SMS_ERR_UNAUTHORIZED`
- `SMS_ERR_RATE_LIMITED`
- `SMS_ERR_INVALID_REQUEST`
- `SMS_ERR_SENDER_NOT_ALLOWED`
- `SMS_ERR_NO_RATE`
- `SMS_ERR_NO_ROUTE`
- `SMS_ERR_INSUFFICIENT_BALANCE`
- `SMS_ERR_NO_ELIGIBLE_CARRIER`
- `SMS_ERR_CARRIER_FAILURE`
- `SMS_ERR_TEMPORARY_UNAVAILABLE`

### 6.2 GET /api/v1/account/balance

Returns authenticated client balance:

```json
{
  "client_id": "....",
  "balance": "100.000000",
  "currency": "GBP"
}
```

### 6.3 GET /api/v1/sms/status/{message_id}

Returns message status data for owned message IDs.

### 6.4 GET/POST /api/v1/dlr/{message_id} (ops reference)

Inbound carrier callback endpoint. Also supports `/api/v1/dlr` when message ID is carried in query/body fields.

Rate limiting:

- Enforced per client using token-bucket limiter.
- Default from setting `api_rate_limit_per_minute` (default `60`).

## 7. Error Code Reference

| Code | HTTP | Meaning | Typical resolution |
|---|---:|---|---|
| `SMS_ERR_UNAUTHORIZED` | 401 | Missing/invalid API key or unauthenticated context | Provide valid API key |
| `SMS_ERR_FORBIDDEN` | 403 | Client inactive or message ownership mismatch | Activate client / query own message |
| `SMS_ERR_RATE_LIMITED` | 429 | Per-client API rate limit exceeded | Throttle and retry later |
| `SMS_ERR_INVALID_REQUEST` | 400 | Validation failed (`to`, `message`, `dlr`, `dlr_url`, JSON) | Fix request payload |
| `SMS_ERR_SENDER_NOT_ALLOWED` | 422 | Sender blocked by allowlist/policy | Use allowed sender ID |
| `SMS_ERR_NO_RATE` | 422 | No rate group or no matching rate prefix | Configure rate group/entries |
| `SMS_ERR_NO_ROUTE` | 422 | No matching route | Configure routing group/entries |
| `SMS_ERR_INSUFFICIENT_BALANCE` | 402 | Client balance too low | Credit client balance |
| `SMS_ERR_NO_ELIGIBLE_CARRIER` | 503 | All candidate carriers skipped by policy/in-loss checks | Fix carrier policy/funding |
| `SMS_ERR_CARRIER_FAILURE` | 502 | All dispatch attempts failed downstream | Check carrier availability/templates |
| `SMS_ERR_TEMPORARY_UNAVAILABLE` | 503 | DB/transaction/transient infra failure | Retry with backoff, inspect platform |
| `SMS_ERR_NOT_FOUND` | 404 | Message ID not found (status endpoint) | Verify message ID |
| `SMS_ERR_INTERNAL` | 503 | Internal computation failure | Investigate service and DB state |

DLR-specific errors:

- `DLR_ERR_INVALID_REQUEST` (400)
- `DLR_ERR_FORBIDDEN` (403)

## 8. SMS Segmentation

MiniSMS detects encoding and computes billable segment count:

- Uses `GSM7` if all runes are in GSM7 charset
- Falls back to `UCS2` otherwise
- Segment size:
  - GSM7: 160 chars
  - UCS2: 70 chars

Segments are `ceil(message_length / segment_size)` with minimum 1.

Billing uses:

```text
total_charge = rate_per_sms * segments
```

## 9. Security Model

| Area | Control |
|---|---|
| API auth | API key via `X-API-Key` or Bearer token |
| Client access | Client must be `active` |
| Admin login protection | Username+IP login throttling with temporary block after repeated failures |
| Rate limiting | Per-client token bucket with configurable limit |
| Admin CSRF | CSRF token in admin forms/requests |
| Secret at rest | Encrypted sensitive fields (for example DLR secrets, carrier header secrets) |
| Sender controls | Client allowlist + carrier sender policy checks |
| Financial integrity | Transactional updates for balances + ledgers |
| DLR endpoint exposure | Public endpoint by design; protected by shared secret verification |
| DLR forward integrity | HMAC-SHA256 signature in `X-MiniSMS-Signature` |
| Auditability | Append-only audit and financial ledgers; SMS skip reason traceability |

> `/api/v1/dlr/...` does not require API key. Security relies on carrier secret verification and message ID correlation.

Health response shape:

```json
{
  "status": "ok",
  "version": "1.0.0",
  "commit": "dev",
  "build_time": "unknown"
}
```

## 10. System Settings Reference

| Key | Type | Default | Description |
|---|---|---|---|
| `default_sender_id` | string | `MiniSMS` | Default sender when request omits `from` |
| `carrier_dispatch_timeout_s` | int | `10` | Per-carrier HTTP timeout (seconds) |
| `low_balance_alert_threshold` | decimal | `1.00` | Client low balance alert threshold |
| `refund_on_carrier_failure` | bool | `true` | Auto-refund client on carrier network failure |
| `max_sms_segments` | int | `4` | Max allowed concatenated segments |
| `admin_session_idle_minutes` | int | `240` | Admin session idle timeout |
| `api_rate_limit_per_minute` | int | `60` | Default API limit per client/minute |
| `failover_enabled` | bool | `true` | Enables carrier failover flow |
| `carrier_low_balance_alert` | decimal | `10.00` | Carrier low balance alert threshold |

## 11. Data Retention and Audit

MiniSMS currently has no automatic purge jobs.

- SMS logs are retained until manually purged.
- Client and carrier ledgers are append-only.
- Admin audit log is append-only.
- DLR forward outcomes remain in `sms_logs`.

This supports reconciliation and post-incident forensics.

## 12. Limitations (v1.2 + DLR)

- No inbound SMS (MO) processing
- No native SMPP/SS7 server interface
- No client self-service portal
- No automated payment gateway integration
- No multi-admin role hierarchy
- DLR forwarding has no built-in retry queue (single attempt)
- No real-time push channel to clients except webhook (status polling remains available)

## 13. Glossary

- **API key**: Client credential used for northbound API authentication.
- **Catch-all prefix (`*`)**: Fallback route/rate when no specific prefix matches.
- **Client ref**: Client-provided reference stored with SMS log.
- **DLR (Delivery Receipt)**: Carrier delivery outcome callback after message submission.
- **DLR status map (`dlr_status_map`)**: Carrier-specific raw status to normalized status mapping.
- **Failover sequence**: Index of selected carrier attempt (`0`, `1`, `2`).
- **HMAC**: Hash-based message authentication code used to verify payload integrity.
- **NPI**: Numbering Plan Indicator.
- **Prefix routing/rating**: Destination-based matching logic using longest-prefix-wins.
- **SMPP**: Short Message Peer-to-Peer protocol used by many carrier systems.
- **TON**: Type Of Number.
- **UCS2**: Unicode encoding used when GSM7 cannot represent message characters.
- **Webhook**: HTTP callback endpoint receiving asynchronous events (DLRs).

