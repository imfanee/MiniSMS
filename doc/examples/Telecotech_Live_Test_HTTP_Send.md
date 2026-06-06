<!-- Architected and Developed by :- Faisal Hanif | imfanee@gmail.com. -->

# Example: Send SMS via HTTP API (Telecotech Live Test)

**Audience:** Client integrators and operators testing production HTTP sends.  
**Related:** [MiniSMS API Guide](../MiniSMS_API_Guide.md), [Admin Guide](../MiniSMS_Admin_Guide.md).

This walkthrough uses the production **Telecotech Live Test** client account on `https://sms.telecotech.net`. Replace placeholders with your own API key and MSISDNs.

---

## 1. Account context

| Field | Value |
|-------|--------|
| Client name | Telecotech Live Test |
| Email | `live-test@telecotech.net` |
| Client ID | `960167db-80bd-4e9c-937b-c64bfd718a7d` |
| API base URL | `https://sms.telecotech.net/api/v1/` |
| Routing | **DRC Routes** (prefix `243`) |
| Carrier (primary) | **IZZI DRC Airtel** (Kamex HTTP interconnect) |
| Rate group | DRC Retail |

Obtain or rotate the API key in **Admin → Clients → Telecotech Live Test → Generate API key**. Only the raw key shown once at generation is valid for API calls; store it like a password.

### Allowed Sender IDs = **Any (pattern)**

This client uses **Any (pattern)**, not an unrestricted sender list. Each `from` value must match the global regex in **System Settings** (`sender_id_any_allowed_pattern`), typically:

```text
^[A-Za-z0-9 _.-]{1,15}$
```

Examples that work: `IZ tech`, `IZ_tech`, `MiniSMS`. Examples that fail: `IZ-tech!`, names longer than 15 characters, Unicode-only brands.

If the API returns `SMS_ERR_SENDER_NOT_ALLOWED`, read the `detail` field — it includes the pattern. Do not use `"from": "any"` in JSON; that literal value tells MiniSMS to substitute the system default sender, not to disable validation.

> **Security:** Do not commit API keys to git or paste them into tickets. Use `YOUR_CLIENT_API_KEY` in scripts and load the real value from a secret store or environment variable.

---

## 2. Prerequisites

1. Client status **active** and sufficient balance.
2. Destination number in E.164 format with DRC country code, for example `+243814190770`.
3. HTTPS access to `https://sms.telecotech.net`.
4. API key from Admin (see §1).

Optional environment helper:

```bash
export MINISMS_BASE_URL='https://sms.telecotech.net'
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
    "to": "+243814190770",
    "message": "Hello from MiniSMS HTTP API"
  }'
```

### 3.2 Full example (sender ID + DLR)

```bash
curl -sS -X POST "${MINISMS_BASE_URL}/api/v1/sms/send" \
  -H "Authorization: Bearer ${MINISMS_API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{
    "to": "+243814190770",
    "from": "IZ tech",
    "message": "Your test message here",
    "client_ref": "order-12345",
    "dlr": "YES"
  }'
```

### 3.3 Same request using `X-API-Key`

```bash
curl -sS -X POST "${MINISMS_BASE_URL}/api/v1/sms/send" \
  -H "X-API-Key: ${MINISMS_API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{"to":"+243814190770","from":"IZ tech","message":"Your test message here","dlr":"YES"}'
```

### 3.4 Per-message DLR webhook override

If the client account has no default DLR URL, you can set one per message:

```bash
curl -sS -X POST "${MINISMS_BASE_URL}/api/v1/sms/send" \
  -H "Authorization: Bearer ${MINISMS_API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{
    "to": "+243814190770",
    "from": "IZ tech",
    "message": "DLR webhook test",
    "dlr": "YES",
    "dlr_url": "https://your-app.example.com/webhooks/minisms-dlr"
  }'
```

`dlr_url` must be `https://`.

---

## 4. Expected success response (`202 Accepted`)

Example body (values will differ per send):

```json
{
  "status": "accepted",
  "message_id": "8fec4482-f965-45d8-8116-3c154d373176",
  "client_ref": "order-12345",
  "sender_id": "IZ tech",
  "sender_id_source": "client_provided",
  "segments": 1,
  "charged": "0.050000",
  "balance_remaining": "99.850000",
  "carrier": "IZZI DRC Airtel",
  "failover_sequence": 0,
  "source_addr_ton": 5,
  "source_addr_npi": 0,
  "dest_addr_ton": 1,
  "dest_addr_npi": 1,
  "dlr_requested": true,
  "dlr_webhook_url": null
}
```

Save `message_id` for status polling and support lookups.

When `dlr=YES` and no client or per-message webhook is configured, `dlr_webhook_url` is `null`. MiniSMS still records carrier DLRs internally; your app will not receive a client webhook until a URL is configured.

---

## 5. Check message status — `GET /api/v1/sms/status/{message_id}`

```bash
MESSAGE_ID='8fec4482-f965-45d8-8116-3c154d373176'

curl -sS "${MINISMS_BASE_URL}/api/v1/sms/status/${MESSAGE_ID}" \
  -H "Authorization: Bearer ${MINISMS_API_KEY}"
```

---

## 6. Check account balance — `GET /api/v1/account/balance`

```bash
curl -sS "${MINISMS_BASE_URL}/api/v1/account/balance" \
  -H "Authorization: Bearer ${MINISMS_API_KEY}"
```

---

## 7. Common errors

| HTTP | Error code | Typical cause |
|------|------------|----------------|
| 401 | `SMS_ERR_UNAUTHORIZED` | Missing, invalid, or revoked API key |
| 402 | `SMS_ERR_INSUFFICIENT_BALANCE` | Client balance too low |
| 400 | `SMS_ERR_VALIDATION` | Invalid `to`, `message`, `dlr`, or `dlr_url` |
| 404 | `SMS_ERR_NOT_FOUND` | Unknown `message_id` on status endpoint |

Regenerate the API key in Admin if authentication fails after a key rotation (generating a new key revokes the previous active key for that client).

---

## 8. DLR behavior (this deployment)

- Carrier **IZZI DRC Airtel** posts delivery updates to MiniSMS at  
  `https://sms.telecotech.net/api/v1/dlr/{message_id}?status=...&answer=...`  
  (Kamex template with `%d`, `%A`, etc.).
- MiniSMS maps Kamex status codes to internal states (`submitted`, `delivered`, `undelivered`, etc.).
- To forward DLRs to your application, set a **client default DLR webhook** in Admin or pass `dlr_url` on each send (§3.4).

See [Admin Guide §5.7](../MiniSMS_Admin_Guide.md) for Kamex DLR callback configuration.

---

## 9. Staging variant

For integration tests against staging (`minisms_test`, port **18080**):

```bash
export MINISMS_BASE_URL='https://sms.telecotech.net:18080'
# Use a staging client API key from Admin on the staging instance
```

Same paths and JSON bodies apply; only base URL and credentials differ.

---

## 10. Quick checklist

- [ ] API key loaded from Admin (not hard-coded in shared repos)
- [ ] `to` is E.164 with `+243…` for DRC routing
- [ ] `from` allowed by client sender policy (if enforced)
- [ ] `dlr=YES` only when you need delivery tracking
- [ ] `message_id` stored for status API and support
- [ ] Client webhook URL set if your app must receive DLR callbacks
