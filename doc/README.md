<!-- Architected and Developed by :- Faisal Hanif | imfanee@gmail.com. -->

# MiniSMS documentation index

**Last updated:** 2026-06-05  
**Application source:** `/usr/src/MiniSMS/minisms` (Go module)  
**Database schema:** `minisms/deploy/minisms_db.sql` (single consolidated file)

Use this page as the entry point for operators, integrators, and developers.

---

## Operator and product guides

| Document | Audience | Contents |
|----------|----------|----------|
| [MiniSMS_Admin_Guide.md](./MiniSMS_Admin_Guide.md) | Admin UI users | Login, RBAC, carriers, clients, invoices, SMS logs, settings |
| [MiniSMS_API_Guide.md](./MiniSMS_API_Guide.md) | Client integrators | REST API: send, balance, status, DLR webhooks |
| [examples/Telecotech_Live_Test_HTTP_Send.md](./examples/Telecotech_Live_Test_HTTP_Send.md) | Integrators / QA | Worked HTTP curl example |
| [MiniSMS_Product_Documentation.md](./MiniSMS_Product_Documentation.md) | Product / architecture | System overview, concepts, data model |
| [MiniSMS_SMPP_Guide.md](./MiniSMS_SMPP_Guide.md) | SMPP operators | ESME binds, carrier/client SMPP tabs |
| [MiniSMS_DevOps_Guide.md](./MiniSMS_DevOps_Guide.md) | DevOps | Install, build, schema, systemd, nginx, env vars |

---

## Quick start (repository root)

| Document | Contents |
|----------|----------|
| [../README.md](../README.md) | Repository layout and doc links |
| [../minisms/README.md](../minisms/README.md) | Application README, features, config |
| [../minisms/QUICK_START.md](../minisms/QUICK_START.md) | Full Ubuntu 24.04 deployment runbook |

---

## Operations (this deployment host)

| Document | Contents |
|----------|----------|
| [agent/OPERATIONS.md](./agent/OPERATIONS.md) | **Single reference:** deploy, topology, architecture, dev/test, security audit, SMPP ops |

Replaces former `doc/agent/` runbooks, phase notes, and audit reports.

---

## Database schema

Apply once per database (fresh install or manual upgrade):

```bash
cd minisms
make schema DB_URL='postgres://minisms:<password>@127.0.0.1:5432/minisms?sslmode=disable'
```

File: `minisms/deploy/minisms_db.sql` — complete schema (extensions, tables, views, functions, triggers, seeds).

The application **does not** auto-apply schema on startup.

---

## Admin authentication

1. **Bootstrap** — `ADMIN_USERNAME` + `ADMIN_PASSWORD_HASH` create super admin when `admin_users` is empty.
2. **Database admins** — **Admin → Admin users** (super admin only).
3. **Sessions** — Cookie-based; idle timeout from `SESSION_IDLE_MINUTES` / Settings.
4. **Logout** — `POST /admin/logout` with CSRF (sidebar **Sign out**).

---

## External URLs (this deployment)

| Environment | Admin / API base | Notes |
|-------------|------------------|-------|
| Production | `https://sms.telecotech.net` | nginx → `127.0.0.1:8080` |
| Staging | `https://sms.telecotech.net:18080` | `minisms_test`, app `127.0.0.1:18081` |

---

## Deployment status

| Item | Value |
|------|--------|
| Last deploy | 2026-06-05T23:09:41Z (`20260605T230941Z`) |
| Build | `004b5f3` |
| Services | `minisms` + `minisms-staging` active |

Details: [agent/OPERATIONS.md](./agent/OPERATIONS.md).

---

## Bootstrap prompts (greenfield builds)

- [MiniSMS_Bootstrap_Prompt.md](./MiniSMS_Bootstrap_Prompt.md)
- [MiniSMS_Bootstrap_Prompt_Short.md](./MiniSMS_Bootstrap_Prompt_Short.md)
