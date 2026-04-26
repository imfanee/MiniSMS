# MiniSMS API Guide

This guide helps you integrate your application with MiniSMS to send SMS and receive delivery receipts (DLRs).

You use this guide for the **client-facing API only**. It does not cover the admin UI.

---

## 1. Introduction

MiniSMS is an SMS gateway API. Your application sends messages to MiniSMS, and MiniSMS routes them to configured carriers.  
If you request DLRs, MiniSMS forwards delivery updates to your webhook endpoint.

Base URL format:

```text
https://your-minisms-domain.com/api/v1/
```

Before you start, you need:

1. Your MiniSMS base URL
2. Your API key (provided by your MiniSMS operator)

> Use HTTPS for all API calls and webhook endpoints.

---

## 2. Authentication

You can authenticate using one of two methods.

### Method 1 (preferred): Bearer token

```bash
curl -X GET "https://your-minisms-domain.com/api/v1/account/balance" \
  -H "Authorization: Bearer YOUR_API_KEY"
```

### Method 2 (alternative): X-API-Key header

```bash
curl -X GET "https://your-minisms-domain.com/api/v1/account/balance" \
  -H "X-API-Key: YOUR_API_KEY"
```

### Auth failure response

On missing or invalid API key, you receive:

- HTTP `401`
- JSON body such as:

```json
{
  "error": "SMS_ERR_UNAUTHORIZED",
  "detail": "missing api key"
}
```

Security rules:

- treat API keys like passwords
- never put API keys in URL query strings
- rotate keys regularly

---

## 3. Request and Response Format

- Request bodies: `application/json`
- Responses: `application/json; charset=utf-8`
- Timestamps: ISO 8601 UTC (for example `2026-04-20T10:01:00Z`)
- Monetary values: decimal strings (typically 6dp, for example `"0.045000"`)
- Phone numbers: E.164 format (for example `+447700900123`)

---

## 4. Send SMS — `POST /api/v1/sms/send`

### 4.1 Request body fields

| Field | Type | Required | Default | Validation | Description |
|---|---|---:|---|---|---|
| `to` | string | Yes | — | E.164 format (`+` + 7-15 digits) | Destination MSISDN |
| `from` | string | No | account/system fallback | validated by sender policy | Sender ID |
| `message` | string | Yes | — | 1 to 1600 chars | SMS body |
| `client_ref` | string | No | — | trimmed string | Your own reference value |
| `dlr` | string | No | `NO` | `YES` or `NO` (case-insensitive) | Request delivery receipt |
| `dlr_url` | string | No | — | must be `https://` URL | Per-message DLR webhook override (used when `dlr=YES`) |

Notes:

- If `from` is omitted, MiniSMS resolves a fallback sender ID.
- If `dlr=YES` and `dlr_url` is omitted, MiniSMS uses your account default DLR webhook (if configured).

### 4.2 Success response — `202 Accepted`

| Field | Type | Description |
|---|---|---|
| `status` | string | Always `accepted` |
| `message_id` | string (UUID) | Unique message ID |
| `client_ref` | string | Echo of your `client_ref` (if provided) |
| `sender_id` | string | Effective sender ID used by MiniSMS |
| `sender_id_source` | string | Sender resolution source (`client_provided`, `client_default`, `carrier_default`, `system_default`) |
| `segments` | integer | Number of billable segments |
| `charged` | string | Amount charged for this message |
| `balance_remaining` | string | Remaining client balance |
| `carrier` | string | Carrier that accepted the message |
| `failover_sequence` | integer | `0` primary, `1` failover1, `2` failover2 |
| `source_addr_ton` | integer or null | Resolved SMPP source TON used for dispatch |
| `source_addr_npi` | integer or null | Resolved SMPP source NPI used for dispatch |
| `dest_addr_ton` | integer or null | Resolved SMPP destination TON used for dispatch |
| `dest_addr_npi` | integer or null | Resolved SMPP destination NPI used for dispatch |
| `dlr_requested` | boolean | Whether DLR was requested |
| `dlr_webhook_url` | string or null | Effective DLR webhook URL for this message |

Example:

```json
{
  "status": "accepted",
  "message_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "client_ref": "ORDER-98765",
  "sender_id": "MyBrand",
  "sender_id_source": "client_provided",
  "segments": 1,
  "charged": "0.045000",
  "balance_remaining": "99.955000",
  "carrier": "Carrier-UK-Tier1",
  "failover_sequence": 0,
  "source_addr_ton": 5,
  "source_addr_npi": 0,
  "dest_addr_ton": 1,
  "dest_addr_npi": 1,
  "dlr_requested": true,
  "dlr_webhook_url": "https://myapp.example.com/webhooks/dlr"
}
```

### 4.3 Complete curl examples

Minimal request:

```bash
curl -X POST "https://your-minisms-domain.com/api/v1/sms/send" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"to":"+447700900123","message":"Hello from MiniSMS"}'
```

Full request:

```bash
curl -X POST "https://your-minisms-domain.com/api/v1/sms/send" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "to": "+447700900123",
    "from": "MyBrand",
    "message": "Your verification code is 123456. Valid for 10 minutes.",
    "client_ref": "ORDER-98765",
    "dlr": "YES",
    "dlr_url": "https://myapp.example.com/webhooks/dlr"
  }'
```

Use account default webhook for DLR:

```bash
curl -X POST "https://your-minisms-domain.com/api/v1/sms/send" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"to":"+447700900123","message":"Test message","dlr":"YES"}'
```

Using `X-API-Key` auth:

```bash
curl -X POST "https://your-minisms-domain.com/api/v1/sms/send" \
  -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"to":"+447700900123","message":"Hello"}'
```

### 4.4 SMS segmentation

MiniSMS detects encoding automatically:

- **GSM-7**: 160 chars/segment
- **UCS-2**: 70 chars/segment

If any character is outside GSM-7, UCS-2 is used.

Billing is per segment. Check `segments` in the `202` response.

| Encoding | Characters per segment | Typical use |
|---|---:|---|
| GSM-7 | 160 | Latin text, basic symbols |
| UCS-2 | 70 | Emoji, Arabic, Chinese, extended Unicode |

### 4.5 Error responses

Error format:

```json
{
  "error": "ERROR_CODE",
  "detail": "human readable message"
}
```

| HTTP | Error code | Cause | Resolution |
|---:|---|---|---|
| 400 | `SMS_ERR_INVALID_REQUEST` | Invalid JSON/fields/E.164/dlr/dlr_url | Fix request data |
| 401 | `SMS_ERR_UNAUTHORIZED` | Missing/invalid API key | Send valid key |
| 402 | `SMS_ERR_INSUFFICIENT_BALANCE` | Balance too low | Top up account |
| 403 | `SMS_ERR_FORBIDDEN` | Client inactive/forbidden resource | Activate account/check ownership |
| 422 | `SMS_ERR_NO_RATE` | No matching rate | Ask operator to configure rates |
| 422 | `SMS_ERR_NO_ROUTE` | No matching route | Ask operator to configure routes |
| 422 | `SMS_ERR_SENDER_NOT_ALLOWED` | Sender ID not allowed | Use allowed sender ID |
| 429 | `SMS_ERR_RATE_LIMITED` | Rate limit exceeded | Retry later with backoff |
| 502 | `SMS_ERR_CARRIER_FAILURE` | Carrier dispatch attempts failed | Retry with delay |
| 503 | `SMS_ERR_NO_ELIGIBLE_CARRIER` | All carriers skipped by policy | Fix sender/policy/in-loss conditions |
| 503 | `SMS_ERR_TEMPORARY_UNAVAILABLE` | Transient service/database issue | Retry with backoff |
| 404 | `SMS_ERR_NOT_FOUND` | Message not found (status endpoint) | Check message ID |

Error curl example:

```bash
curl -X POST "https://your-minisms-domain.com/api/v1/sms/send" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"to":"7700900123","message":"Invalid number format"}'
```

---

## 5. Check Account Balance — `GET /api/v1/account/balance`

### 5.1 Request

- No request body
- Auth header required

### 5.2 Response fields

| Field | Type | Description |
|---|---|---|
| `client_id` | string (UUID) | Your client account ID |
| `balance` | string | Current balance |
| `currency` | string | Account currency code |

### 5.3 curl example

```bash
curl -X GET "https://your-minisms-domain.com/api/v1/account/balance" \
  -H "Authorization: Bearer YOUR_API_KEY"
```

### 5.4 When to use

- before large batches
- periodic monitoring/alerts

---

## 6. Check Message Status — `GET /api/v1/sms/status/{message_id}`

### 6.1 Path parameter

- `message_id`: UUID returned by send API

### 6.2 Response fields

| Field | Type | Description |
|---|---|---|
| `message_id` | string | Message UUID |
| `status` | string | Current message processing status |
| `to` | string | Destination number |
| `from` | string or null | Sender ID used |
| `client_ref` | string or null | Your reference from send request |
| `segments` | integer | Billable segments |
| `charged` | string | Charged amount |
| `carrier_id` | string or null | Carrier UUID if accepted |
| `carrier` | string or null | Carrier name if accepted |
| `failover_sequence` | integer | 0/1/2 depending on route leg |
| `carrier_response_code` | integer or null | Last carrier HTTP status |
| `received_at` | string | Message received timestamp |
| `dispatched_at` | string or null | Dispatch timestamp |
| `delivered_at` | string or null | Delivery timestamp if set |
| `failed_at` | string or null | Failure timestamp if set |
| `dlr_requested` | boolean | Whether DLR was requested |
| `dlr_webhook_url` | string or null | Effective DLR webhook URL |
| `dlr_status` | string or null | DLR status value |
| `dlr_received_at` | string or null | DLR callback processed timestamp |
| `dlr_forwarded_at` | string or null | Webhook forward attempt timestamp |
| `dlr_forward_status` | string or null | `success`, `failed`, or `no_url` |
| `dlr_forward_attempts` | integer | Number of forward attempts recorded |
| `source_addr_ton` | integer or null | SMPP source TON |
| `source_addr_npi` | integer or null | SMPP source NPI |
| `dest_addr_ton` | integer or null | SMPP destination TON |
| `dest_addr_npi` | integer or null | SMPP destination NPI |

### 6.3 Message status values (common)

| Status | Meaning |
|---|---|
| `pending` | Message logged, dispatch in progress |
| `accepted` | Carrier accepted message |
| `failed` | Message failed to dispatch |

### 6.4 DLR status values (via webhook context)

| Value | Meaning |
|---|---|
| `delivered` | Confirmed delivered |
| `undelivered` | Delivery failed |
| `rejected` | Rejected by carrier |
| `unknown` | Callback received but not mappable |

### 6.5 curl example

```bash
curl -X GET "https://your-minisms-domain.com/api/v1/sms/status/a1b2c3d4-e5f6-7890-abcd-ef1234567890" \
  -H "Authorization: Bearer YOUR_API_KEY"
```

### 6.6 Polling guidance

1. Poll every 30 seconds.
2. Stop when terminal status is reached (`accepted`/`failed`) per your workflow.
3. Prefer DLR webhooks for delivery outcomes.

---

## 7. Delivery Receipt (DLR) Webhooks

### 7.1 Overview

When you send with `dlr=YES`, MiniSMS forwards carrier DLR updates to your webhook URL.

### 7.2 Configuring webhook URL

Two options:

1. **Account default**: operator sets your client DLR webhook URL.
2. **Per-message**: include `dlr_url` in send request.

Per-message `dlr_url` overrides account default for that message.

### 7.3 Webhook request details

MiniSMS sends:

- Method: `POST`
- Content-Type: `application/json`
- User-Agent: `MiniSMS-DLR/1.0`
- `X-MiniSMS-Signature: sha256=<hex>` (if webhook secret is configured)

Your endpoint should return HTTP 2xx within 10 seconds.

MiniSMS does not automatically retry failed webhook deliveries.

### 7.4 Webhook payload example

```json
{
  "event": "dlr",
  "message_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "client_ref": "ORDER-98765",
  "to": "+447700900123",
  "from": "MyBrand",
  "dlr_status": "delivered",
  "carrier": "Carrier-UK-Tier1",
  "failover_sequence": 0,
  "received_at": "2026-04-20T10:01:00Z",
  "dlr_received_at": "2026-04-20T10:01:30Z",
  "segments": 1,
  "charged": "0.045000",
  "source_addr_ton": 5,
  "source_addr_npi": 0,
  "dest_addr_ton": 1,
  "dest_addr_npi": 1
}
```

### 7.5 Payload field reference

| Field | Type | Description |
|---|---|---|
| `event` | string | Always `dlr` |
| `message_id` | string | MiniSMS message UUID |
| `client_ref` | string or null | Echo of your reference |
| `to` | string | Destination |
| `from` | string or null | Sender ID used |
| `dlr_status` | string | Normalized DLR status |
| `carrier` | string or null | Carrier name |
| `failover_sequence` | integer | Route leg index |
| `received_at` | string | Original message received timestamp |
| `dlr_received_at` | string | DLR callback processed timestamp |
| `segments` | integer | Segment count |
| `charged` | string | Charged amount |
| `source_addr_ton` | integer or null | Sender TON |
| `source_addr_npi` | integer or null | Sender NPI |
| `dest_addr_ton` | integer or null | Destination TON |
| `dest_addr_npi` | integer or null | Destination NPI |

SMPP TON/NPI notes:

- Values are resolved at send time using carrier configuration.
- Resolution can be static numeric or dynamic.
- Read payload values directly instead of assuming fixed constants.

### 7.6 DLR status values

| Value | Meaning |
|---|---|
| `delivered` | Handset/network confirms delivery |
| `undelivered` | Delivery failed |
| `rejected` | Rejected by carrier |
| `unknown` | Status not determinable |

### 7.7 Verify HMAC signature

Pseudo logic:

```text
expected = "sha256=" + HEX(HMAC_SHA256(secret, raw_body_bytes))
compare expected with X-MiniSMS-Signature using constant-time compare
```

Python:

```python
import hmac
import hashlib

def verify_signature(body_bytes: bytes, signature_header: str, secret: str) -> bool:
    expected = "sha256=" + hmac.new(secret.encode(), body_bytes, hashlib.sha256).hexdigest()
    return hmac.compare_digest(expected, signature_header or "")
```

Node.js:

```javascript
const crypto = require("crypto");

function verifySignature(bodyBuffer, signatureHeader, secret) {
  const expected =
    "sha256=" + crypto.createHmac("sha256", secret).update(bodyBuffer).digest("hex");
  const a = Buffer.from(expected);
  const b = Buffer.from(signatureHeader || "");
  if (a.length !== b.length) return false;
  return crypto.timingSafeEqual(a, b);
}
```

PHP:

```php
<?php
function verifySignature(string $body, string $signatureHeader, string $secret): bool {
    $expected = 'sha256=' . hash_hmac('sha256', $body, $secret);
    return hash_equals($expected, $signatureHeader);
}
```

### 7.8 Webhook endpoint implementation checklist

1. Accept POST JSON requests.
2. Read raw body bytes.
3. Verify signature if secret configured.
4. Parse JSON.
5. Update your internal message state using `message_id` or `client_ref`.
6. Return 2xx quickly.
7. Make handler idempotent for repeated events.

Minimal Flask example:

```python
from flask import Flask, request, jsonify
import hmac
import hashlib

app = Flask(__name__)
DLR_SECRET = "replace-with-your-secret"

def verify_signature(body: bytes, sig: str) -> bool:
    expected = "sha256=" + hmac.new(DLR_SECRET.encode(), body, hashlib.sha256).hexdigest()
    return hmac.compare_digest(expected, sig or "")

@app.post("/webhooks/dlr")
def dlr_webhook():
    body = request.get_data()
    sig = request.headers.get("X-MiniSMS-Signature", "")
    if DLR_SECRET and not verify_signature(body, sig):
        return "", 403
    data = request.get_json(force=True, silent=False)
    # update your message record here
    return "", 200
```

### 7.9 Test your webhook

1. Get test URL from [webhook.site](https://webhook.site).
2. Send SMS with `dlr=YES` and `dlr_url` set to test URL.
3. Wait for callback.
4. Confirm incoming JSON in webhook.site.
5. Validate fields/signature format.

Test send:

```bash
curl -X POST "https://your-minisms-domain.com/api/v1/sms/send" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "to": "+447700900123",
    "message": "DLR webhook test",
    "dlr": "YES",
    "dlr_url": "https://webhook.site/your-uuid"
  }'
```

---

## 8. Rate Limiting

- Default: 60 requests/minute per client (operator configurable)
- On limit breach: HTTP `429` with `SMS_ERR_RATE_LIMITED`
- Response headers such as `X-RateLimit-*` are not guaranteed

Retry guidance:

1. Wait at least 60 seconds.
2. Use exponential backoff for batch senders.
3. Add client-side queue smoothing to avoid bursts.

---

## 9. Best Practices

### 9.1 Phone number formatting

- always send E.164
- remove spaces, dashes, parentheses
- include country code

### 9.2 Sender ID usage

- use branded alphanumeric sender for one-way messaging
- use e164 sender when reply behavior matters (carrier dependent)

### 9.3 Message content

- keep message short to reduce segments
- test Unicode separately (UCS-2 segment cost differs)

### 9.4 DLR strategy

- use `dlr=YES` for critical traffic
- prefer account-level webhook for simplicity
- use per-message `dlr_url` only for routing exceptions

### 9.5 Balance management

- check balance before batch sends
- create low-balance alarms in your system

### 9.6 Error handling

- retry `502` and `503` with backoff
- do not blindly retry `402` (top up first)
- do not blindly retry `422` (fix configuration/payload)

### 9.7 Idempotency/correlation

- always send `client_ref` for your own correlation
- store `message_id` from `202` response
- deduplicate on your side before resubmitting

---

## 10. Code Examples

### 10.1 JavaScript (Node.js fetch)

```javascript
// node >=18
async function sendSMS(baseUrl, apiKey, to, message, options = {}) {
  const payload = {
    to,
    message,
    ...(options.from ? { from: options.from } : {}),
    ...(options.client_ref ? { client_ref: options.client_ref } : {}),
    ...(options.dlr ? { dlr: options.dlr } : {}),
    ...(options.dlr_url ? { dlr_url: options.dlr_url } : {}),
  };

  const res = await fetch(`${baseUrl}/api/v1/sms/send`, {
    method: "POST",
    headers: {
      "Authorization": `Bearer ${apiKey}`,
      "Content-Type": "application/json",
    },
    body: JSON.stringify(payload),
  });

  const data = await res.json().catch(() => ({}));
  if (res.status === 202) return data;

  const err = new Error(data.detail || "MiniSMS API error");
  err.httpStatus = res.status;
  err.code = data.error || "UNKNOWN";
  throw err;
}
```

### 10.2 Python (requests)

```python
import requests

class MiniSMSError(Exception):
    def __init__(self, status, code, detail):
        super().__init__(f"{status} {code}: {detail}")
        self.status = status
        self.code = code
        self.detail = detail

def send_sms(base_url, api_key, to, message, **kwargs):
    payload = {"to": to, "message": message}
    for k in ("from", "client_ref", "dlr", "dlr_url"):
        if k in kwargs and kwargs[k] is not None:
            payload[k] = kwargs[k]

    r = requests.post(
        f"{base_url}/api/v1/sms/send",
        headers={"Authorization": f"Bearer {api_key}", "Content-Type": "application/json"},
        json=payload,
        timeout=15,
    )
    data = r.json() if r.content else {}
    if r.status_code == 202:
        return data
    raise MiniSMSError(r.status_code, data.get("error"), data.get("detail"))
```

### 10.3 PHP (cURL)

```php
<?php
function send_sms(string $baseUrl, string $apiKey, string $to, string $message, array $options = []): array {
    $payload = array_merge([
        "to" => $to,
        "message" => $message
    ], $options);

    $ch = curl_init($baseUrl . "/api/v1/sms/send");
    curl_setopt_array($ch, [
        CURLOPT_POST => true,
        CURLOPT_HTTPHEADER => [
            "Authorization: Bearer " . $apiKey,
            "Content-Type: application/json"
        ],
        CURLOPT_POSTFIELDS => json_encode($payload),
        CURLOPT_RETURNTRANSFER => true,
        CURLOPT_TIMEOUT => 15
    ]);

    $body = curl_exec($ch);
    $code = curl_getinfo($ch, CURLINFO_HTTP_CODE);
    if ($body === false) {
        throw new RuntimeException("cURL error: " . curl_error($ch));
    }
    curl_close($ch);

    $data = json_decode($body, true) ?? [];
    if ($code === 202) {
        return $data;
    }
    throw new RuntimeException("MiniSMS API error {$code}: " . ($data["error"] ?? "UNKNOWN") . " " . ($data["detail"] ?? ""));
}
```

### 10.4 Go (net/http)

```go
package minisms

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type SMSRequest struct {
	To       string `json:"to"`
	From     string `json:"from,omitempty"`
	Message  string `json:"message"`
	ClientRef string `json:"client_ref,omitempty"`
	DLR      string `json:"dlr,omitempty"`
	DLRURL   string `json:"dlr_url,omitempty"`
}

type SMSResponse struct {
	Status           string  `json:"status"`
	MessageID        string  `json:"message_id"`
	ClientRef        string  `json:"client_ref"`
	Segments         int     `json:"segments"`
	Charged          string  `json:"charged"`
	BalanceRemaining string  `json:"balance_remaining"`
	Carrier          string  `json:"carrier"`
	FailoverSequence int     `json:"failover_sequence"`
	DLRRequested     bool    `json:"dlr_requested"`
	DLRWebhookURL    *string `json:"dlr_webhook_url"`
}

type APIError struct {
	ErrorCode string `json:"error"`
	Detail    string `json:"detail"`
}

func SendSMS(ctx context.Context, client *http.Client, baseURL, apiKey string, req SMSRequest) (*SMSResponse, error) {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	b, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/v1/sms/send", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusAccepted {
		var out SMSResponse
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return nil, err
		}
		return &out, nil
	}

	var apiErr APIError
	_ = json.NewDecoder(resp.Body).Decode(&apiErr)
	return nil, fmt.Errorf("minisms error %d %s: %s", resp.StatusCode, apiErr.ErrorCode, apiErr.Detail)
}
```

---

## 11. Quick Reference

### 11.1 Endpoints

| Method | Path | Description | Auth required |
|---|---|---|---|
| POST | `/api/v1/sms/send` | Submit SMS for dispatch | Yes |
| GET | `/api/v1/account/balance` | Read account balance | Yes |
| GET | `/api/v1/sms/status/{message_id}` | Check message status | Yes |
| GET/POST | `/api/v1/dlr/{message_id}` | Carrier callback endpoint (ops/internal) | No (secret-verified) |
| GET/POST | `/api/v1/dlr` | Carrier callback endpoint with message ID in query/body | No (secret-verified) |

### 11.2 Error codes

| Code | Typical HTTP |
|---|---:|
| `SMS_ERR_INVALID_REQUEST` | 400 |
| `SMS_ERR_UNAUTHORIZED` | 401 |
| `SMS_ERR_INSUFFICIENT_BALANCE` | 402 |
| `SMS_ERR_FORBIDDEN` | 403 |
| `SMS_ERR_NOT_FOUND` | 404 |
| `SMS_ERR_NO_RATE` | 422 |
| `SMS_ERR_NO_ROUTE` | 422 |
| `SMS_ERR_SENDER_NOT_ALLOWED` | 422 |
| `SMS_ERR_RATE_LIMITED` | 429 |
| `SMS_ERR_CARRIER_FAILURE` | 502 |
| `SMS_ERR_NO_ELIGIBLE_CARRIER` | 503 |
| `SMS_ERR_TEMPORARY_UNAVAILABLE` | 503 |

### 11.3 Webhook payload fields

See section **7.5** for the full field reference and types.

### 11.4 SMPP TON/NPI quick reference

| Field | Meaning | Typical values |
|---|---|---|
| `source_addr_ton` | Sender Type Of Number | `5` alphanumeric, `1` international |
| `source_addr_npi` | Sender Numbering Plan Indicator | `0` unknown, `1` ISDN |
| `dest_addr_ton` | Destination TON | `1` international |
| `dest_addr_npi` | Destination NPI | `1` ISDN |

Resolution mode depends on carrier config:

- static numeric
- dynamic per message

### 11.5 DLR status values

| Value | Meaning |
|---|---|
| delivered | Delivered |
| undelivered | Not delivered |
| rejected | Rejected |
| unknown | Callback received but indeterminate |

### 11.6 Glossary

- **API key:** Secret token for authentication.
- **client_ref:** Your custom reference stored with a message.
- **DLR:** Delivery receipt callback event.
- **E.164:** International phone number format (`+` plus country code and number).
- **failover_sequence:** Which route leg accepted message (`0`, `1`, `2`).
- **HMAC:** Signature method for webhook authenticity/integrity.
- **message_id:** UUID assigned by MiniSMS per message.
- **NPI:** Numbering Plan Indicator (SMPP field).
- **segment:** Billable message unit after encoding split.
- **Sender ID:** Message originator (`from`).
- **TON:** Type Of Number (SMPP field).
- **UCS-2:** Unicode encoding used when GSM-7 is insufficient.
- **webhook:** HTTP endpoint receiving asynchronous callbacks.

