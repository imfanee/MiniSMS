<!-- Architected and Developed by :- Faisal Hanif | imfanee@gmail.com. -->

# Example: Send SMS via HTTP API

**Audience:** Client integrators and operators testing HTTP sends.  
**Related:** [MiniSMS API Guide](../MiniSMS_API_Guide.md), [Admin Guide](../MiniSMS_Admin_Guide.md).

Replace `YOUR_DOMAIN`, `YOUR_CLIENT_API_KEY`, and MSISDNs with your deployment values.

---

## 1. Account context

| Field | Value |
|-------|--------|
| Client name | Your test client (Admin → Clients) |
| API base URL | `https://YOUR_DOMAIN/api/v1/` |
| Routing | Your routing group (prefix-matched) |
| Rate group | As assigned on the client |

Obtain or rotate the API key in **Admin → Clients → {client} → Generate API key**. Only the raw key shown once at generation is valid for API calls; store it like a password.

### Sender ID policy

Each client has a sender policy (allowlist, **Any (pattern)**, etc.). If using **Any (pattern)**, each `from` value must match `sender_id_any_allowed_pattern` in **System Settings**, typically:

```text
^[A-Za-z0-9 _.-]{1,15}$
```

If the API returns `SMS_ERR_SENDER_NOT_ALLOWED`, read the `detail` field. Do not use `"from": "any"` in JSON — that substitutes the system default sender.

> **Security:** Do not commit API keys to git. Use `YOUR_CLIENT_API_KEY` in scripts and load the real value from a secret store.

---

## 2. Prerequisites

1. Client status **active** and sufficient balance.
2. Destination number in E.164 format, e.g. `+447700900123`.
3. HTTPS access to `https://YOUR_DOMAIN`.
4. API key from Admin.

```bash
export MINISMS_BASE_URL='https://YOUR_DOMAIN'
export MINISMS_API_KEY='YOUR_CLIENT_API_KEY'
```

---

## 3. Send SMS — `POST /api/v1/sms/send`

### 3.1 Minimal example (Bearer token)

```bash
curl -sS -X POST "${MINISMS_BASE_URL}/api/v1/sms/send" \
  -H "Authorization: Bearer ${MINISMS_API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{
    "to": "+447700900123",
    "message": "Hello from MiniSMS HTTP API"
  }'
```

### 3.2 Full example (sender ID + DLR)

```bash
curl -sS -X POST "${MINISMS_BASE_URL}/api/v1/sms/send" \
  -H "Authorization: Bearer ${MINISMS_API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{
    "to": "+447700900123",
    "from": "MyBrand",
    "message": "Your test message here",
    "client_ref": "order-12345",
    "dlr": "YES"
  }'
```

### 3.3 Per-message DLR webhook override

```bash
curl -sS -X POST "${MINISMS_BASE_URL}/api/v1/sms/send" \
  -H "Authorization: Bearer ${MINISMS_API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{
    "to": "+447700900123",
    "from": "MyBrand",
    "message": "DLR webhook test",
    "dlr": "YES",
    "dlr_url": "https://your-app.example.com/webhooks/minisms-dlr"
  }'
```

`dlr_url` must be `https://`.

---

## 4. Expected success response (`202 Accepted`)

```json
{
  "status": "accepted",
  "message_id": "8fec4482-f965-45d8-8116-3c154d373176",
  "client_ref": "order-12345",
  "sender_id": "MyBrand",
  "sender_id_source": "client_provided",
  "segments": 1,
  "charged": "0.050000",
  "balance_remaining": "99.850000",
  "carrier": "Primary Carrier",
  "failover_sequence": 0,
  "dlr_requested": true,
  "dlr_webhook_url": null
}
```

---

## 5. Status and balance

```bash
curl -sS "${MINISMS_BASE_URL}/api/v1/sms/status/${MESSAGE_ID}" \
  -H "Authorization: Bearer ${MINISMS_API_KEY}"

curl -sS "${MINISMS_BASE_URL}/api/v1/account/balance" \
  -H "Authorization: Bearer ${MINISMS_API_KEY}"
```

---

## 6. DLR callback URL (carrier → MiniSMS)

Carriers call your MiniSMS instance at:

```text
https://YOUR_DOMAIN/api/v1/dlr/{message_id}?status=...&answer=...
```

Configure the carrier **DLR callback URL template** in Admin. See [Admin Guide §5.7](../MiniSMS_Admin_Guide.md).

---

## 7. Staging variant (optional)

```bash
export MINISMS_BASE_URL='https://YOUR_DOMAIN:18080'
```

Same paths and JSON; use a staging client API key and `minisms_test` database on the server.
