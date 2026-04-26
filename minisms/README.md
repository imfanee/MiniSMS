# MiniSMS

MiniSMS is a Go SMS middleware gateway that provides client-facing SMS APIs, carrier routing/failover, billing controls, and an operator Admin UI in a single binary.

![Go](https://img.shields.io/badge/Go-1.22%2B-00ADD8?logo=go)
![PostgreSQL](https://img.shields.io/badge/PostgreSQL-15%2B-336791?logo=postgresql)
![License](https://img.shields.io/badge/License-MIT-green)

## What it does

- Exposes a northbound REST API for SMS send, balance, and status.
- Routes messages by destination prefix with primary + two failover carriers.
- Enforces sender ID policies and client sender allowlists.
- Performs prepaid charging with append-only client/carrier ledger records.
- Handles carrier DLR callbacks and forwards to client webhooks with optional HMAC.

## Architecture

MiniSMS runs as a single Go binary with embedded templates/static assets, backed by PostgreSQL.

```text
Client App
   |
   | POST /api/v1/sms/send
   v
MiniSMS (API + Routing + Billing + Admin UI)
   |
   | Carrier HTTP dispatch
   v
Carrier

Carrier ---- POST /api/v1/dlr/{message_id} ----> MiniSMS ----> Client Webhook
```

## Quick Start

See [QUICK_START.md](QUICK_START.md) for full deployment instructions.

Short version:

```bash
git clone https://github.com/YOUR_ORG/minisms.git && cd minisms/minisms
go mod download && make build
cp deploy/minisms.env.example .env
psql "$DATABASE_URL" -f deploy/minisms_db.sql
make run
```

## Features

- Currency registry (20 seeded currencies)
- Sender ID library (`alpha`/`numeric`/`e164`)
- Sender ID resolution cascade (`client_provided`/`client_default`/`carrier_default`/`system_default`)
- Carrier sender policy (`any`/`numeric`/`e164`/`list`/`none`)
- Prefix-based rate groups with effective date windows
- Prefix-based routing groups with failover sequence
- Carrier in-loss control and skip reason tracing
- Client prepaid balance ledger (append-only)
- Carrier financial ledger (payment/charge/adjustment/refund)
- DLR forwarding with optional HMAC-SHA256 signature
- SMPP TON/NPI support (static or dynamic resolution)
- Dashboard reports and HTMX-driven Admin UI

## API Overview

| Method | Path | Description |
|---|---|---|
| POST | `/api/v1/sms/send` | Submit SMS for dispatch |
| GET | `/api/v1/account/balance` | Get client balance |
| GET | `/api/v1/sms/status/{message_id}` | Get message status |
| GET/POST | `/api/v1/dlr/{message_id}` | Carrier DLR callback |
| GET/POST | `/api/v1/dlr` | Carrier DLR callback (ID in query/body) |

## Admin UI

Admin UI is server-rendered (`html/template`) and progressively enhanced with HTMX/Alpine. It uses a collapsible sidebar and partial updates for operational screens (carriers, rates, routing, clients, logs, settings, reports, simulation).

## Project Structure

```text
cmd/minisms/      # application entrypoint
internal/         # business logic, API, DB access, web handlers
templates/        # embedded HTML templates
static/           # embedded CSS/JS assets
migrations/       # incremental golang-migrate SQL files
deploy/           # deployment artifacts (schema/unit/env template)
tools/            # standalone utilities (password hasher)
doc/              # generated product/devops/admin/api docs
```

## Configuration

Primary runtime config is read from environment variables.

| Variable | Required | Default | Purpose |
|---|---|---|---|
| `DATABASE_URL` | Yes | — | PostgreSQL DSN |
| `SECRET_KEY` | Yes | — | AES-GCM app encryption key (hex 32-byte) |
| `ADMIN_USERNAME` | Yes | — | Admin login username |
| `ADMIN_PASSWORD_HASH` | Yes | — | bcrypt hash for admin login |
| `CSRF_AUTH_KEY` | Yes | — | CSRF signing key (hex 32-byte) |
| `PORT` | No | `8080` | Listen port |
| `TLS_ENABLED` | No | `false` | Enable built-in TLS listener |
| `TLS_CERT_FILE` | Cond. | — | TLS cert path if TLS enabled |
| `TLS_KEY_FILE` | Cond. | — | TLS key path if TLS enabled |
| `LOG_LEVEL` | No | `info` | Log level |
| `APP_ENV` | No | `development` | Runtime environment |
| `SESSION_IDLE_MINUTES` | No | `240` | Admin session idle timeout |
| `CARRIER_DISPATCH_TIMEOUT_S` | No | `10` | Per-carrier dispatch timeout |

See `deploy/minisms.env.example` for the full template.

## Development

```bash
git clone https://github.com/YOUR_ORG/minisms.git
cd minisms/minisms
go mod download
make build
createdb minisms
psql -d minisms -f deploy/minisms_db.sql
cp deploy/minisms.env.example .env
make hash-password
make run
make test
```

## Makefile Targets

| Target | Description |
|---|---|
| `build` | Build app binary to `bin/minisms` |
| `run` | Run app from source |
| `migrate` | Apply incremental migrations |
| `test` | Run tests |
| `dev` | Run with `.env` auto-load |
| `hash-password` | Run admin password hasher tool |
| `build-tools` | Build standalone tools |
| `schema` | Import consolidated schema file |
| `vet` | Run `go vet` |
| `clean` | Remove `bin/` |
| `docker-build` | Build and tag Docker image |

## Deployment

- Full step-by-step: [QUICK_START.md](QUICK_START.md)
- Operations reference: [doc/MiniSMS_DevOps_Guide.md](doc/MiniSMS_DevOps_Guide.md)

## Documentation

| File | Description |
|---|---|
| `QUICK_START.md` | End-to-end deployment guide |
| `doc/MiniSMS_Product_Documentation.md` | Product reference |
| `doc/MiniSMS_DevOps_Guide.md` | DevOps/operations guide |
| `doc/MiniSMS_Admin_Guide.md` | Admin user guide |
| `doc/MiniSMS_API_Guide.md` | Client API reference |

## License

MIT License © Faisal Hanif
