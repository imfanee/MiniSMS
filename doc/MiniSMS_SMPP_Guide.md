<!-- Architected and Developed by :- Faisal Hanif | imfanee@gmail.com. -->

# MiniSMS SMPP Guide (v1)

Operator-facing summary for SMPP v3.4 interconnect. Deploy and runtime context: [doc/agent/OPERATIONS.md](agent/OPERATIONS.md).

## Listeners

| Role | Default | Env |
|------|---------|-----|
| Client SMSC (ESME → MiniSMS) | `:2775` | `SMPP_SERVER_ENABLED=true`, `SMPP_LISTEN_ADDR` |
| Carrier egress (MiniSMS → SMSC) | per `carriers.smpp_host:port` | automatic when `egress_transport` ∈ `smpp`, `both` |

HTTP API and DLR HTTP callbacks are unchanged on port `8080` / nginx.

## Client bind (ingress)

1. Enable `clients.smpp_ingress_enabled`, set `smpp_system_id` + encrypted `smpp_password_enc`.
2. Optional: `smpp_allowed_cidrs`, `smpp_max_binds`, `smpp_throughput_per_s`.
3. Bind as **TX** or **TRX** to submit; **RX** or **TRX** to receive DLR `deliver_sm`.

## submit_sm → pipeline

- Destination normalized to E.164 where possible.
- `submit_sm_resp.message_id` = MiniSMS UUID on **ESME_ROK** (`0`).
- Billing/routing/failover identical to `POST /api/v1/sms/send`.

## command_status mapping (client ingress)

| Pipeline outcome | command_status |
|------------------|----------------|
| Accepted | `ESME_ROK` (0) |
| Insufficient balance | `ESME_RQUERYFAIL` |
| No rate / no route | `ESME_RINVDSTADR` |
| No eligible carrier / carrier failure / temporary error | `ESME_RSUBMITFAIL` |
| Bind/auth/CIDR/max binds | `ESME_RBINDFAIL` / `ESME_RTHROTTLED` |
| Invalid message / address | `ESME_RINVMGLEN` / `ESME_RINVSRCADR` |

## Client DLR (`dlr_delivery_mode`)

| Mode | Behavior |
|------|----------|
| `http` | Webhook only (default) |
| `smpp` | `deliver_sm` on RX/TRX bind; else `dlr_forward_status=smpp_no_bind` |
| `both` | SMPP if session exists; else single HTTP webhook attempt |

## Carrier egress

See Phase C: `egress_transport`, `smpp_status`, TRX `deliver_sm` receipts → same DLR core as HTTP.
