# MiniSMS Admin User Guide

This guide helps you operate MiniSMS from the Admin UI. It is written for system operators who are comfortable with web admin tools.

You use this guide to:

- set up carriers, rates, routes, and clients
- manage balances and API keys
- monitor message delivery and DLR forwarding
- diagnose operational failures quickly

> **Warning (Financial impact):** Actions in Carrier Ledger and Client Ledger affect real balances used for routing decisions and charging. Always verify amount, currency, and reference before saving.

> **Warning (Delivery impact):** Misconfigured routing, sender ID policy, or DLR settings can stop messages or hide delivery outcomes. Test each change with a controlled sample message.

---

## 1. Introduction and Getting Started

### 1.1 What MiniSMS is

MiniSMS is an SMS middleware gateway. Your clients call MiniSMS API endpoints, MiniSMS routes messages to configured carriers, and MiniSMS records full operational and financial traces.

MiniSMS also receives delivery receipts (DLRs) from carriers and forwards them to your clients’ webhook URLs.

### 1.2 Logging in

1. Open your MiniSMS admin URL in a browser, for example: `https://your-domain/admin/login`.
2. Enter your admin username and password.
3. Click **Sign in**.

If login fails:

1. Check username/password carefully.
2. Confirm Caps Lock is off.
3. Try again from a private window.
4. If still blocked, ask your DevOps owner to validate `ADMIN_USERNAME` and `ADMIN_PASSWORD_HASH`.

Login abuse protection:

- repeated failed attempts from the same username and IP are temporarily blocked
- during this window, you receive a `Too many login attempts` message and must wait before retrying

Session behavior:

- Your session expires after the configured idle timeout.
- After timeout, you are redirected to login and must authenticate again.

### 1.3 The Admin interface

You work in a dark-themed interface with:

- left navigation menu (active item highlighted)
- flash messages (success/error notices)
- HTMX partial updates (many actions update only part of the page)
- LED footer branding at the bottom of the content area

Practical meaning of HTMX updates:

- you often do not see a full page reload
- updates appear quickly in place (tables, panels, forms)

### 1.4 Key concepts (plain English)

- **Carrier:** The downstream provider MiniSMS calls to send SMS.
- **Rate Group:** Price list by destination prefix for client charging.
- **Routing Group:** Carrier selection rules by destination prefix.
- **Client:** Your API consumer account (balance + API key + policy).
- **Balance:** Available prepaid amount used to authorize sends.
- **API Key:** Secret token clients use to call API endpoints.
- **Sender ID:** SMS originator (alpha/numeric/e164).
- **DLR:** Delivery receipt callback from carrier after submit.
- **Webhook:** HTTP endpoint receiving asynchronous events.
- **In-Loss Delivery:** Whether MiniSMS allows sending through a carrier that would lose money on that route.

---

## 2. Dashboard

The Dashboard gives live operations visibility.

### What you see

- top stat cards (message/financial snapshots)
- failover activity summary
- carrier health table with balance indicators
- date-range reports (7 charts)
- auto-refresh every ~30 seconds

### Date-range reports (how to read and act)

1. **Message Volume Trend**  
   Shows total messages over time.  
   Action if bad: sudden drop -> check client traffic, carrier outage, API errors.

2. **Success vs Failure Trend**  
   Shows accepted/failed distribution.  
   Action if bad: rising failures -> inspect SMS Logs status and carrier response codes.

3. **Failover Trend**  
   Shows how often F1/F2 carriers are used.  
   Action if bad: high failover -> primary carrier likely degraded.

4. **Client Charge Trend**  
   Shows billing charged to clients.  
   Action if bad: mismatch with expected volume -> verify rate group updates.

5. **Carrier Cost Trend**  
   Shows downstream spend.  
   Action if bad: unusual increase -> verify carrier cost/rate definitions.

6. **Margin/Spread View**  
   Shows charge vs cost behavior.  
   Action if bad: collapsing margin -> check in-loss controls and pricing.

7. **Carrier Health Mix**  
   Shows performance split by carrier.  
   Action if bad: skew toward failovers -> review primary route quality.

---

## 3. Currencies

You use Currencies to control allowed money codes across pricing and balances.

### What you can do

- view list
- add currency (inline)
- edit currency (inline)
- activate/deactivate

### Why delete is restricted

Currencies are referenced by clients, carriers, rates, and ledger data. Deletion would break historical integrity, so you deactivate instead.

### Default seeded currencies

MiniSMS seeds a standard set (20 currencies) at setup. Add a new currency only when you truly need to onboard operations in a code not already present.

### Add currency (steps)

1. Open **Currencies**.
2. Click **Add**.
3. Enter code (for example `KES`), name, symbol, decimal places.
4. Save.
5. Verify it appears as active in the list.

---

## 4. Sender IDs

A Sender ID is the origin shown on recipient devices (subject to carrier and handset behavior).

### Sender ID types

- **alpha**: text brand, typically up to 11 chars (example: `MiniSMS`)
- **numeric**: numeric short/long code (example: `447700900123`)
- **e164**: full international number with plus prefix (example: `+447700900123`)

### Typical tasks

1. Open **Sender IDs**.
2. Add sender ID with type.
3. Activate it.
4. Map it to clients and/or carrier acceptable lists as needed.

### Why “cannot delete” appears

If a Sender ID is referenced by clients/carriers/history, MiniSMS protects integrity and blocks delete. Deactivate instead.

### Relationship to clients and carriers

- client allowed list controls what a client may use
- carrier policy controls what carrier accepts
- effective routing uses both checks

---

## 5. Carriers

Carriers define how MiniSMS dispatches requests downstream.

### 5.1 Carrier list

Columns generally include:

- name/status
- endpoint/method summary
- policy and balance indicators

Actions:

1. Add carrier.
2. Edit carrier basics.
3. Activate/deactivate.

### 5.2 Carrier detail tabs

You manage a carrier through tabs:

- **Auth Headers**
- **Request Template**
- **Sender IDs**
- **Ledger**
- **Usage**
- **DLR Settings**

### 5.3 Auth Headers

Use Auth Headers for API authentication with carrier endpoints.

Add/edit/delete:

1. Open carrier -> **Auth Headers**.
2. Add header name/value (for example `Authorization`).
3. Save.
4. Use reveal/mask controls when validating values.

Security behavior:

- sensitive values are encrypted at rest
- UI shows masked values unless revealed

### 5.4 Request Template

Request Template defines the outbound carrier request structure.

Supported variables:

- `{{to}}` destination MSISDN
- `{{from}}` resolved sender ID
- `{{message}}` SMS text
- `{{message_id}}` MiniSMS UUID
- `{{timestamp}}` UTC ISO timestamp
- `{{client_id}}` internal client ID
- `{{dlr_callback_url}}` callback URL generated for this message
- (future variables may be added)

Content-Type options:

- JSON
- form-url-encoded
- XML
- GET query template

JSON example:

```json
{
  "to": "{{to}}",
  "from": "{{from}}",
  "message": "{{message}}",
  "message_id": "{{message_id}}",
  "client_id": "{{client_id}}",
  "timestamp": "{{timestamp}}",
  "callback_url": "{{dlr_callback_url}}"
}
```

Form example:

```text
to={{to}}&from={{from}}&text={{message}}&reference={{message_id}}&callback_url={{dlr_callback_url}}
```

XML example:

```xml
<sms>
  <to>{{to}}</to>
  <from>{{from}}</from>
  <text>{{message}}</text>
  <reference>{{message_id}}</reference>
  <callback_url>{{dlr_callback_url}}</callback_url>
</sms>
```

GET query example:

```text
to={{to}}&from={{from}}&msg={{message}}&ref={{message_id}}&cb={{dlr_callback_url}}
```

Common mistakes:

- wrong content type for payload format
- extra spaces in token names
- missing JSON quotes
- forgetting to include callback field when carrier expects it

How to test:

1. Save template.
2. Send one controlled test SMS.
3. Open **SMS Logs** detail for that message.
4. Verify carrier response and status.

### 5.5 Sender ID policy and acceptable list

Carrier policy options:

- `any` -> accept any valid sender
- `numeric` -> only numeric sender
- `e164` -> only international number format
- `list` -> only sender IDs listed in carrier acceptable list
- `none` -> no sender accepted (effectively skips)

When sender fails policy:

- carrier is skipped
- skip reason is written to SMS log
- failover may continue to next carrier

Set policy = list (steps):

1. Open carrier -> **Sender IDs**.
2. Set policy to `list`.
3. Add allowed sender IDs.
4. Save.
5. Test with one allowed and one non-allowed sender.

### 5.6 Carrier financial ledger and payments

Ledger tab shows:

- current balance
- entries with type/color cues:
  - payment
  - charge
  - adjustment
  - refund

Record payment:

1. Open carrier -> **Ledger**.
2. Click **Record Payment**.
3. Enter amount, currency, reference, notes.
4. Save.
5. Confirm balance card updates.

Why negative balance may appear:

- postpaid/invoice style operation
- delayed reconciliation

Low-balance alerts:

- configured in Settings
- used as operational warning thresholds

### 5.7 DLR Settings (new)

This tab controls callback interoperability with each carrier.

#### DLR Callback URL Template

- defines callback URL sent to carrier
- include `{{message_id}}`

Example:

```text
https://minisms.example.com/api/v1/dlr/{{message_id}}
```

#### DLR Field Name

- identifies which outgoing carrier request field carries callback URL
- ensure your request template contains `{{dlr_callback_url}}` in the right field

#### DLR Message ID Field

- name of callback field containing your MiniSMS message ID

#### DLR Status Field

- name of callback field containing delivery status

#### DLR Status Map

JSON map from carrier values to MiniSMS values.

Example:

```json
{
  "DELIVRD": "delivered",
  "UNDELIV": "undelivered",
  "REJECTD": "rejected"
}
```

#### DLR Inbound Secret

- shared secret to verify carrier callback authenticity
- MiniSMS checks query/header secret values

#### SMPP parameters section

You set four dropdowns:

- Source Addr TON
- Source Addr NPI
- Dest Addr TON
- Dest Addr NPI

Each supports:

- `dynamic — Resolve automatically` (default)
- static numeric choices

Plain meaning:

- **TON** = Type Of Number
- **NPI** = Numbering Plan Indicator

Common numeric references:

- `0` unknown
- `1` international (E.164)
- `5` alphanumeric sender type (TON)

When to use static:

- carrier requires fixed values
- testing/debugging exact protocol expectations

When to use dynamic (recommended):

- most carriers
- MiniSMS auto-detects e164/alphanumeric patterns

SMPP template variables:

- `{{source_addr_ton}}`
- `{{source_addr_npi}}`
- `{{dest_addr_ton}}`
- `{{dest_addr_npi}}`

Worked JSON example:

```json
{
  "to": "{{to}}",
  "from": "{{from}}",
  "text": "{{message}}",
  "callback_url": "{{dlr_callback_url}}",
  "source_addr_ton": "{{source_addr_ton}}",
  "source_addr_npi": "{{source_addr_npi}}",
  "dest_addr_ton": "{{dest_addr_ton}}",
  "dest_addr_npi": "{{dest_addr_npi}}"
}
```

Configure and test DLR (steps):

1. Set callback URL template with `{{message_id}}`.
2. Set message ID/status field names.
3. Add status map if carrier uses non-standard labels.
4. Set inbound secret if supported.
5. Save.
6. Send test SMS with DLR enabled.
7. Open SMS Logs detail and verify DLR fields.

---

## 6. Rate Groups

Rate Groups define client billing rates by destination prefix.

### Core behavior

- longest-prefix match wins
- `*` can be used as catch-all
- effective date windows support scheduled changes

### Create and maintain (steps)

1. Open **Rate Groups**.
2. Create group and choose currency from dropdown.
3. Add rate entries (`prefix`, `rate`, `effective from/to`).
4. Save.
5. Verify badges (active/expired) in list.

Use scheduling for future pricing:

1. Add new entry with future effective date.
2. Keep current entry active until cutover date.
3. Validate with simulation/test message before effective date.

Delete safety:

- system blocks delete when dependencies exist
- deactivate/replace entries instead

---

## 7. Routing Groups

Routing Groups decide which carriers are tried for each destination prefix.

### Route entry fields

- prefix
- priority/order
- primary carrier
- failover 1
- failover 2

Carrier choices must be distinct in one route path.

### Failover behavior

1. MiniSMS tries primary carrier.
2. If skipped/failed, it tries failover 1.
3. If needed, it tries failover 2.
4. Selected attempt index is stored as failover sequence.

Skipped vs failed:

- **skipped**: policy/in-loss prevented attempt (with explicit reason)
- **failed**: attempted but downstream/carrier call failed

Best practices:

- use different vendors across failover path
- avoid same underlying provider in all three slots
- keep prefixes specific where commercial/quality differences exist

---

## 8. Clients

Clients are your API tenant accounts.

### 8.1 Client list

You see columns for identity, status, currency, and balance context. Use this page to add/edit and monitor account state quickly.

### 8.2 Client detail tabs

- **Info**
- **Sender IDs**
- **Ledger**
- **API Key**
- **DLR Webhook**

### 8.3 Info tab (field explanations)

- **Allow Any Sender ID**: bypasses strict sender allowlist behavior.  
  Security impact: broader sender usage risk.
- **Allow In-Loss Delivery**: allows send even if carrier cost exceeds charge.  
  Use cautiously.
- **Default Sender ID**: fallback sender when API request omits `from`.
- Other profile fields: name/status/contact/routing-rate assignments as configured.

> **Warning (Margin risk):** If you enable in-loss delivery broadly, high-volume traffic can produce sustained losses.

### 8.4 Sender IDs tab

1. Add allowed sender IDs for client.
2. Set default sender ID from allowed values.
3. Remove unused sender IDs when policy tightness is needed.

### 8.5 Ledger tab

Use this tab for client balance operations.

Add credit:

1. Open client -> **Ledger**.
2. Click **Add Credit**.
3. Enter amount/reference/notes.
4. Save.
5. Confirm updated balance and ledger row.

Ledger records include debit and credit events for traceability.

### 8.6 API Key tab

Generate key:

1. Open **API Key** tab.
2. Click **Generate**.
3. Copy key immediately (one-time display).
4. Share securely with client operator.

Rotate key:

1. Generate new key.
2. Ask client to switch traffic.
3. Revoke old key after cutover.

### 8.7 DLR Webhook tab (new)

#### DLR Webhook URL

- default destination for forwarded DLRs when messages are sent with `dlr=YES`
- must be `https://`

#### DLR Webhook Secret

- optional shared secret used by MiniSMS to sign webhook payload
- signature sent in `X-MiniSMS-Signature`
- value is masked in UI and encrypted at rest

Testing workflow:

1. Set webhook URL to a test receiver (for example webhook.site).
2. Send test SMS with `dlr=YES`.
3. Wait for carrier callback.
4. Check SMS log detail DLR section.
5. Verify receiver got expected payload fields.

---

## 9. SMS Logs

SMS Logs are your primary operational truth source.

### 9.1 Table columns (including DLR)

Common columns include:

- message ID
- client/carrier
- destination/sender
- status
- segments/charged
- failover sequence
- `dlr_requested`
- `dlr_status`
- `dlr_forward_status`

### 9.2 Status values

Message lifecycle status (send pipeline) is separate from DLR status.

DLR status values:

- delivered
- undelivered
- rejected
- unknown

### 9.3 Failover badges

- Primary
- F1
- F2

These show which route leg succeeded (or how far flow progressed).

### 9.4 Filters

Use filters for client/carrier/status/date and DLR-focused investigations such as:

- DLR requested only
- delivered vs failed DLR forwards
- recent callbacks

### 9.5 Message detail modal (DLR section)

You can inspect:

- DLR Requested
- DLR Webhook URL
- DLR Status
- DLR Received At
- DLR Forwarded At
- DLR Forward Status
- DLR Forward Attempts

### 9.6 Diagnosing failures quickly

- `dlr_forward_status=failed` -> client webhook endpoint issue
- `dlr_forward_status=no_url` -> DLR requested but no webhook URL resolved
- `dlr_status` empty/null -> carrier callback has not arrived yet

---

## 10. Audit Log

Audit Log records administrative changes.

Use it to:

1. identify who changed what
2. track when config changed
3. support incident review and compliance evidence

Retention:

- append-only operational history (no routine auto-purge by default)

---

## 11. Settings

System Settings control global runtime behavior.

| Setting | Default | What it does | When you change it |
|---|---|---|---|
| `default_sender_id` | `MiniSMS` | fallback sender ID | branding or compliance changes |
| `carrier_dispatch_timeout_s` | `10` | outbound carrier timeout | slower/faster carrier APIs |
| `low_balance_alert_threshold` | `1.00` | client low balance warning | match your operating model |
| `refund_on_carrier_failure` | `true` | refund on downstream failure | billing policy decisions |
| `max_sms_segments` | `4` | max allowed segments per message | risk/cost control |
| `admin_session_idle_minutes` | `240` | admin session timeout | security policy |
| `api_rate_limit_per_minute` | `60` | per-client API throttle | abuse prevention/throughput |
| `failover_enabled` | `true` | enable multi-carrier failover | emergency control only |
| `carrier_low_balance_alert` | `10.00` | carrier low balance warning | finance threshold tuning |

> **Warning (Service risk):** Disabling failover or setting very low timeout values can sharply increase failed sends.

---

## 12. Recommended Workflows

### 12.1 First-time setup

1. Configure currencies if needed.
2. Create carrier(s).
3. Set auth headers and request templates.
4. Configure carrier sender policy.
5. Configure carrier DLR settings.
6. Create rate groups and entries.
7. Create routing groups and entries.
8. Create client accounts.
9. Assign sender IDs and default sender.
10. Configure client DLR webhook.
11. Generate API key.
12. Send controlled end-to-end test.

### 12.2 Onboarding a new client

1. Create client.
2. Set status active, currency, rate/routing groups.
3. Configure sender ID permissions.
4. Add initial credit.
5. Configure DLR webhook URL/secret.
6. Generate and share API key securely.
7. Validate with a test message and status check.

### 12.3 Adding a new carrier

1. Create carrier profile.
2. Add auth headers.
3. Build and save request template.
4. Set sender policy and list if required.
5. Configure DLR tab (callback template, field names, map, secret).
6. Set SMPP dropdowns (dynamic unless carrier says otherwise).
7. Add to routing groups as primary/failover.
8. Run test traffic and inspect SMS Logs.

### 12.4 Responding to carrier outage

1. Open Dashboard and confirm failure/failover spike.
2. In Routing Groups, move affected carrier to failover or deactivate route usage.
3. Optionally deactivate carrier temporarily.
4. Confirm traffic shifts to healthy carriers.
5. Monitor SMS Logs and recovery.

### 12.5 Topping up a client account

1. Open client -> Ledger.
2. Add credit entry.
3. Verify new balance.
4. Re-test blocked messages if any.

### 12.6 Rotating an API key

1. Generate new key.
2. Coordinate switchover window with client.
3. Verify client traffic on new key.
4. Revoke old key.

### 12.7 Diagnosing “DLR not arriving at client webhook”

1. Confirm message had `dlr_requested=true`.
2. Check `dlr_status` exists (carrier callback received).
3. Check `dlr_forward_status`.
4. If `no_url`, configure client webhook URL.
5. If `failed`, test client endpoint and signature handling.
6. Verify carrier inbound secret and mapping fields.

### 12.8 Scheduling a rate change

1. Add new rate entry with future effective date.
2. Keep current entry until cutover time.
3. Review overlap/order.
4. Validate expected match by prefix test.
5. Monitor first post-cutover traffic in logs.

---

## Appendix: Quick DLR Validation Checklist

1. Carrier DLR callback URL template contains `{{message_id}}`.
2. Carrier request template includes `{{dlr_callback_url}}` in expected field.
3. Carrier callback message ID/status field names are correct.
4. `dlr_status_map` covers carrier-specific status labels.
5. Inbound secret matches carrier configuration.
6. Client webhook URL is HTTPS and reachable.
7. Client verifies `X-MiniSMS-Signature` if secret is set.

