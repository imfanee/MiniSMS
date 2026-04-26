# MiniSMS v1.3 Release Notes

## Delivery Receipt (DLR) Webhook Forwarding

This release adds end-to-end Delivery Receipt support to MiniSMS, including:

- Client-level DLR webhook configuration (URL + optional signing secret)
- Per-message DLR controls on `POST /api/v1/sms/send`
- Carrier DLR settings and callback mapping
- Inbound DLR callback processing (`/api/v1/dlr`)
- Outbound forwarding to client webhooks with optional HMAC signature
- SMPP TON/NPI resolution (static or dynamic) and propagation in DLR payloads

## Database Migration

New migration files:

- `migrations/003_dlr_support.up.sql`
- `migrations/003_dlr_support.down.sql`

Schema additions include:

- `clients`: `dlr_webhook_url`, `dlr_webhook_secret`
- `carriers`: DLR callback/field mapping settings, inbound secret, status map, SMPP config fields
- `sms_logs`: DLR request/forward lifecycle fields and resolved TON/NPI fields
- Indexes on `sms_logs` DLR fields for query efficiency

Migration up/down was validated successfully.

## API Changes

### 1) Send SMS API

Endpoint: `POST /api/v1/sms/send`

New optional request fields:

- `dlr`: `YES` or `NO` (case-insensitive; default `NO`)
- `dlr_url`: per-message webhook override (must be `https://` when provided)

New response fields:

- `dlr_requested` (boolean)
- `dlr_webhook_url` (resolved URL or `null`)

Resolution priority:

1. request `dlr_url` (when `dlr=YES`)
2. client default `dlr_webhook_url`
3. no forwarding URL (DLR still processed/stored)

### 2) Inbound Carrier DLR API

Supported endpoints:

- `GET /api/v1/dlr/{message_id}`
- `POST /api/v1/dlr/{message_id}`
- `GET /api/v1/dlr`
- `POST /api/v1/dlr`

Message ID lookup sources:

- path param `{message_id}`
- query params (`ref`, `msgid`, `reference`)
- callback payload fallbacks (`ref`, `msgid`, `reference`, `message_id`, `messageid`, `id`)

Behavior:

- Unknown message ID: returns `200` (`{"status":"ok"}`)
- Invalid/missing message ID: returns `400`
- Inbound secret mismatch (if carrier secret configured): returns `403`
- Client webhook delivery failure does **not** fail carrier callback; carrier still receives `200`

## Security

- Client and carrier DLR secrets are encrypted at rest (AES-256-GCM via existing crypto helpers).
- Optional outbound webhook signature:
  - Header: `X-MiniSMS-Signature`
  - Format: `sha256=<hex>`
  - Signature base: raw JSON body, HMAC-SHA256 with client DLR secret
- Inbound carrier secret verification supports:
  - query param `secret`
  - headers `X-DLR-Secret` / `X-Callback-Secret`
  - constant-time comparison

## Admin UI Updates

### Client Detail -> Info tab

- Added:
  - **DLR Webhook URL**
  - **DLR Webhook Secret**
- Secret is masked after save and has show/hide toggle.

### Carrier Detail -> DLR Settings tab

- Added DLR callback/template/mapping fields:
  - callback URL template
  - DLR field name (request side)
  - message-id/status field names (callback side)
  - status map JSON
  - inbound secret
- Added SMPP parameter section:
  - Source/Dest TON and NPI
  - each supports static values or `dynamic`

### SMS Log Detail Modal

Added DLR section with:

- requested flag
- webhook URL
- DLR status and timestamps
- forward status and attempts

## DLR Forward Payload

MiniSMS sends JSON payload to client webhook with:

- event/message identity fields
- to/from/client_ref/carrier/failover
- delivery status + timestamps
- segments/charged
- resolved SMPP TON/NPI fields

## Tests and Build

Validated in repository:

- `go test ./...` passes
- `go build ./...` passes

Included test coverage for:

- DLR handler core logic and secrets/signature behavior
- DLR integration scenarios (DB-backed; skip without `TEST_DATABASE_URL`)
- SMPP TON/NPI static and dynamic resolution cases
