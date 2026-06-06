<!-- Architected and Developed by :- Faisal Hanif | imfanee@gmail.com. -->

# MiniSMS

MiniSMS is a Go SMS middleware gateway that provides client-facing SMS APIs, carrier routing/failover, billing controls, and an operator Admin UI in a single binary.

![Go](https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go)
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
- Carrier **HTTP** or **SMPP** interconnect with request templates (JSON, form, XML, GET query)
- Carrier sender policy (`any`/`numeric`/`e164`/`list`/`none`)
- Prefix-based rate groups with effective date windows
- Prefix-based routing groups with failover sequence
- Carrier in-loss control and skip reason tracing
- Client prepaid balance ledger (append-only)
- Carrier financial ledger (payment/charge/adjustment/refund)
- DLR forwarding with optional HMAC-SHA256 signature
- Optional **SMPP server** for client ESME binds (migration `004+`)
- SMPP TON/NPI support (static or dynamic resolution)
- **Multi-admin RBAC** with permission keys and super-admin-only settings/audit/users
- **Audit log** (append-only) with admin user attribution
- Dashboard reports and HTMX-driven Admin UI
- Client/carrier **invoices** (PDF generation, payment allocation, summary stats)

## API Overview

| Method | Path | Description |
|---|---|---|
| POST | `/api/v1/sms/send` | Submit SMS for dispatch |
| GET | `/api/v1/account/balance` | Get client balance |
| GET | `/api/v1/sms/status/{message_id}` | Get message status |
| GET/POST | `/api/v1/dlr/{message_id}` | Carrier DLR callback |
| GET/POST | `/api/v1/dlr` | Carrier DLR callback (ID in query/body) |

## Admin UI

Admin UI is server-rendered (`html/template`) with HTMX/Alpine. Navigation is permission-aware: operators only see menu items their account allows. Super admins additionally manage **Settings**, **Audit log**, and **Admin users**. Sign out is at the bottom of the sidebar.

Operator guide: [doc/MiniSMS_Admin_Guide.md](../doc/MiniSMS_Admin_Guide.md).

## Project Structure

```text
cmd/minisms/      # application entrypoint
internal/         # business logic, API, DB access, web handlers
templates/        # embedded HTML templates
static/           # embedded CSS/JS assets
deploy/minisms_db.sql  # complete PostgreSQL schema (single file)
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
| `HTTP_LISTEN_ADDR` | No | — | Override bind address (e.g. `127.0.0.1:18081` for staging) |
| `HTTP_CARRIER_INSECURE_TLS` | No | `false` | Skip TLS verify on carrier HTTP (lab only) |
| `CSRF_TRUSTED_ORIGINS` | No | — | Comma-separated origins when admin is on non-default host/port |
| `TLS_ENABLED` | No | `false` | Enable built-in TLS listener |
| `TLS_CERT_FILE` | Cond. | — | TLS cert path if TLS enabled |
| `TLS_KEY_FILE` | Cond. | — | TLS key path if TLS enabled |
| `LOG_LEVEL` | No | `info` | Log level |
| `APP_ENV` | No | `development` | Runtime environment |
| `SESSION_IDLE_MINUTES` | No | `240` | Admin session idle timeout (env fallback) |
| `CARRIER_DISPATCH_TIMEOUT_S` | No | `10` | Per-carrier dispatch timeout (overridable in Settings) |
| `SMPP_SERVER_ENABLED` | No | `false` | Listen for client SMPP binds |
| `SMPP_LISTEN_ADDR` | No | `:2775` | SMPP bind address |

`ADMIN_USERNAME` / `ADMIN_PASSWORD_HASH` are required for startup and used to **bootstrap** the first super admin when `admin_users` is empty. See `deploy/minisms.env.example`.

Schema: `deploy/minisms_db.sql`. Apply with `make schema DB_URL=...` before first start. The app does not auto-migrate.

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
| `schema` | Import `deploy/minisms_db.sql` to database |
| `test` | Run tests |
| `dev` | Run with `.env` auto-load |
| `hash-password` | Run admin password hasher tool |
| `build-tools` | Build standalone tools |
| `vet` | Run `go vet` |
| `clean` | Remove `bin/` |
| `docker-build` | Build and tag Docker image |

## Deployment

- Full step-by-step: [QUICK_START.md](QUICK_START.md)
- Operations reference: [doc/MiniSMS_DevOps_Guide.md](doc/MiniSMS_DevOps_Guide.md)

## Documentation

| File | Description |
|---|---|
| `../doc/README.md` | Documentation index |
| `QUICK_START.md` | End-to-end deployment guide |
| `../doc/MiniSMS_Product_Documentation.md` | Product reference |
| `../doc/MiniSMS_DevOps_Guide.md` | DevOps/operations guide |
| `../doc/MiniSMS_Admin_Guide.md` | Admin user guide |
| `../doc/MiniSMS_API_Guide.md` | Client API reference |
| `../doc/MiniSMS_SMPP_Guide.md` | SMPP operations |

## License

MIT License © Faisal Hanif
