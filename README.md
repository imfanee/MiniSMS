<!-- Architected and Developed by :- Faisal Hanif | imfanee@gmail.com. -->

# MiniSMS

MiniSMS is a Go-based SMS middleware gateway: client REST API, carrier HTTP/SMPP interconnect, routing with failover, prepaid billing, DLR forwarding, multi-admin RBAC, and a server-rendered Admin UI — all in one binary.

## Repository layout

| Path | Description |
|------|-------------|
| `minisms/` | Go application (entrypoint, `internal/`, templates, migrations, `deploy/`) |
| `doc/` | Product, API, admin, DevOps guides and agent runbooks — **[documentation index](doc/README.md)** |
| `certs/` | Local development TLS certificates |
| `bin/` | Locally built helper binaries |

## Quick commands

```bash
cd minisms
go build ./...
go test ./...
make run    # development only — use a non-production DATABASE_URL
```

Full deployment: [minisms/QUICK_START.md](minisms/QUICK_START.md).

## Documentation

| Guide | Path |
|-------|------|
| **Index (start here)** | [doc/README.md](doc/README.md) |
| Admin UI (operators) | [doc/MiniSMS_Admin_Guide.md](doc/MiniSMS_Admin_Guide.md) |
| Client API | [doc/MiniSMS_API_Guide.md](doc/MiniSMS_API_Guide.md) |
| Product reference | [doc/MiniSMS_Product_Documentation.md](doc/MiniSMS_Product_Documentation.md) |
| DevOps / operations | [doc/MiniSMS_DevOps_Guide.md](doc/MiniSMS_DevOps_Guide.md) |
| Deployment host ops | [doc/agent/OPERATIONS.md](doc/agent/OPERATIONS.md) |
| SMPP | [doc/MiniSMS_SMPP_Guide.md](doc/MiniSMS_SMPP_Guide.md) |
| Application README | [minisms/README.md](minisms/README.md) |

## Current capabilities (summary)

- Prefix-based **rate groups** and **routing groups** with primary + failover carriers
- **HTTP** and **SMPP** carrier interconnect (per-carrier), request templates (JSON/form/XML/GET), auth headers
- **DLR** ingest from carriers and forward to client webhooks (optional HMAC)
- **Multi-admin** users with granular permissions; super admin for settings, audit log, user management
- **Audit log** for login/logout and selected admin mutations (append-only)
- Client **API keys**, prepaid balance, sender ID policies, SMS logs, dashboard, simulate
- **Invoices** (client/carrier PDF generation, payment allocation, summary stats)

## License

MIT License © Faisal Hanif
