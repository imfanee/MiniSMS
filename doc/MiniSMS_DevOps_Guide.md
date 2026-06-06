<!-- Architected and Developed by :- Faisal Hanif | imfanee@gmail.com. -->

# MiniSMS DevOps and Operations Guide

This guide is prescriptive and command-driven for operating MiniSMS (v1.3 + DLR + RBAC) on Ubuntu 24.04 LTS.

**See also:** [doc/README.md](./README.md) (documentation index).

> Runtime model: single Go binary (`minisms`) + PostgreSQL 15+.

## 1. Prerequisites

Run as a sudo-capable user on Ubuntu 24.04 LTS.

### 1.1 Install base packages

```bash
sudo apt update
sudo apt install -y git make curl ca-certificates tar gzip jq openssl nginx postgresql postgresql-contrib
```

### 1.2 Install Go 1.22+ (tarball)

```bash
GO_VERSION="1.22.5"
ARCH="linux-amd64"   # use linux-arm64 on ARM64 hosts
cd /tmp
curl -fLO "https://go.dev/dl/go${GO_VERSION}.${ARCH}.tar.gz"
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf "go${GO_VERSION}.${ARCH}.tar.gz"
echo 'export PATH=/usr/local/go/bin:$PATH' | sudo tee /etc/profile.d/go.sh >/dev/null
source /etc/profile.d/go.sh
go version
```

### 1.3 Optional: install Caddy (instead of nginx)

```bash
sudo apt install -y debian-keyring debian-archive-keyring apt-transport-https
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | sudo gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | sudo tee /etc/apt/sources.list.d/caddy-stable.list
sudo apt update
sudo apt install -y caddy
```

## 2. Getting the Source and Building

```bash
cd /opt
sudo git clone https://github.com/<your-org>/MiniSMS.git minisms
sudo chown -R "$USER":"$USER" /opt/minisms
cd /opt/minisms/minisms
go mod download
```

### 2.1 Development build

```bash
mkdir -p bin
go build -o bin/minisms ./cmd/minisms
```

### 2.2 Production build (stripped + version metadata)

```bash
VERSION="1.3.0"
COMMIT="$(git rev-parse --short HEAD)"
BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
mkdir -p bin
CGO_ENABLED=0 go build -trimpath \
  -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildTime=${BUILD_TIME}" \
  -o bin/minisms ./cmd/minisms
```

### 2.3 Verify binary type/static expectation

```bash
file bin/minisms
ldd bin/minisms || true
```

Expected: statically linked or minimal dynamic deps (depending on your build env/toolchain).

### 2.4 Cross-compilation example (ARM64)

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o bin/minisms-linux-arm64 ./cmd/minisms
file bin/minisms-linux-arm64
```

## 3. Database Setup

## 3.1 Create database and role

```bash
sudo -u postgres psql <<'SQL'
CREATE ROLE minisms WITH LOGIN PASSWORD 'change_me_now';
CREATE DATABASE minisms OWNER minisms;
GRANT ALL PRIVILEGES ON DATABASE minisms TO minisms;
SQL
```

Test:

```bash
psql "postgres://minisms:change_me_now@localhost:5432/minisms?sslmode=disable" -c "SELECT now();"
```

### 3.2 Apply database schema

Schema is a **single file:** `deploy/minisms_db.sql` (consolidated; includes invoices, SMPP, multi-admin, ledger immutability, client DLR templates).

```bash
cd /usr/src/MiniSMS/minisms
make schema DB_URL='postgres://minisms:change_me_now@localhost:5432/minisms?sslmode=disable'
```

The application **does not** auto-apply schema on startup. Apply explicitly when provisioning a database or after schema changes. Deploy runbook: [agent/OPERATIONS.md](./agent/OPERATIONS.md).

**Note:** `audit_log` is immutable; do not run UPDATE backfills against it. Historical audit rows may have NULL `admin_user_id`.

### 3.3 Schema reference

Current schema objects:

- **22+ tables** (includes `admin_users`, `invoices`, `smpp_bind_events`, interconnect columns; see `deploy/minisms_db.sql`)
- **9 SQL functions**
- **11 SQL views** (7 base + 4 from v1.2: `v_client_sender_ids`, `v_carrier_sender_ids`, `v_carrier_prefix_success`, `v_client_bill_vs_carrier_cost`)

Immutability-protected update prevention exists for:

- client balance ledger table (`ledger_entries`)
- carrier balance ledger table (`carrier_balance_entries`)
- audit log (`audit_log`)

### 3.4 DLR and SMPP columns

Key columns in `deploy/minisms_db.sql`:

- `clients`
  - `dlr_webhook_url TEXT`
  - `dlr_webhook_secret TEXT` (encrypted by app)
- `carriers`
  - `dlr_callback_url_template TEXT`
  - `dlr_field_name TEXT`
  - `dlr_inbound_secret TEXT` (encrypted by app)
  - `dlr_message_id_field TEXT`
  - `dlr_status_field TEXT`
  - `dlr_status_map JSONB`
  - `smpp_source_addr_ton TEXT NOT NULL DEFAULT 'dynamic'`
  - `smpp_source_addr_npi TEXT NOT NULL DEFAULT 'dynamic'`
  - `smpp_dest_addr_ton TEXT NOT NULL DEFAULT 'dynamic'`
  - `smpp_dest_addr_npi TEXT NOT NULL DEFAULT 'dynamic'`
- `sms_logs`
  - `dlr_requested BOOLEAN NOT NULL DEFAULT FALSE`
  - `dlr_webhook_url TEXT`
  - `dlr_status TEXT`
  - `dlr_received_at TIMESTAMPTZ`
  - `dlr_forwarded_at TIMESTAMPTZ`
  - `dlr_forward_status TEXT`
  - `dlr_forward_attempts INT NOT NULL DEFAULT 0`
  - `source_addr_ton SMALLINT`
  - `source_addr_npi SMALLINT`
  - `dest_addr_ton SMALLINT`
  - `dest_addr_npi SMALLINT`

Indexes:

- `idx_sms_logs_dlr_requested`
- `idx_sms_logs_dlr_status`

### 3.5 DLR/SMPP dependency

MiniSMS uses pure-Go libphonenumber:

```bash
go list -m github.com/nyaruka/phonenumbers
```

## 4. Configuration Reference

MiniSMS reads environment using `godotenv` (`.env` for local/dev).

> `dlr_webhook_secret` and `dlr_inbound_secret` are stored encrypted in DB using `SECRET_KEY`. No extra env var is required for those secrets.

### 4.1 Environment variables

| Variable | Required | Format | Example |
|---|---|---|---|
| `DATABASE_URL` | Yes | PostgreSQL DSN | `postgres://minisms:pass@localhost:5432/minisms?sslmode=disable` |
| `SECRET_KEY` | Yes | 64 hex chars (32 bytes) | `openssl rand -hex 32` |
| `ADMIN_USERNAME` | Yes | string | Bootstrap super admin username when `admin_users` is empty; required at every startup |
| `ADMIN_PASSWORD_HASH` | Yes | bcrypt hash | Bootstrap super admin password hash; wrap in single quotes in `.env` if `$` present |
| `CSRF_AUTH_KEY` | Yes | 64 hex chars (32 bytes) | `openssl rand -hex 32` |
| `PORT` | No | integer string | `8080` |
| `HTTP_LISTEN_ADDR` | No | host:port | `127.0.0.1:18081` (staging sidecar) |
| `HTTP_CARRIER_INSECURE_TLS` | No | bool | `false` (lab only) |
| `CSRF_TRUSTED_ORIGINS` | No | comma-separated URLs | Required if admin UI is served on a non-default origin (e.g. `:18080`) |
| `SMPP_SERVER_ENABLED` | No | bool | `false` |
| `SMPP_LISTEN_ADDR` | No | address | `:2775` |
| `TLS_ENABLED` | No | bool (`true/false`) | `false` |
| `TLS_CERT_FILE` | Cond. | path (required if TLS enabled) | `certs/dev-cert.pem` |
| `TLS_KEY_FILE` | Cond. | path (required if TLS enabled) | `certs/dev-key.pem` |
| `LOG_LEVEL` | No | `debug/info/warn/error` | `info` |
| `APP_ENV` | No | `development/production` | `production` |
| `SESSION_IDLE_MINUTES` | No | int (1..259200) | `240` |
| `CARRIER_DISPATCH_TIMEOUT_S` | No | int (1..3600) | `10` |

### 4.2 Generate required secrets

```bash
openssl rand -hex 32
openssl rand -hex 32
```

### 4.3 Example `.env`

```dotenv
DATABASE_URL=postgres://minisms:change_me_now@localhost:5432/minisms?sslmode=disable
SECRET_KEY=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
ADMIN_USERNAME=admin
ADMIN_PASSWORD_HASH='$2a$12$XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX'
CSRF_AUTH_KEY=abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789
PORT=8080
TLS_ENABLED=false
TLS_CERT_FILE=certs/dev-cert.pem
TLS_KEY_FILE=certs/dev-key.pem
LOG_LEVEL=info
APP_ENV=production
SESSION_IDLE_MINUTES=240
CARRIER_DISPATCH_TIMEOUT_S=10
# CSRF_TRUSTED_ORIGINS=https://sms.example.com:18080
# HTTP_LISTEN_ADDR=127.0.0.1:18081
```

### 4.4 Admin bootstrap and RBAC

1. First process start with an empty `admin_users` table creates one **super admin** from `ADMIN_USERNAME` / `ADMIN_PASSWORD_HASH`.
2. Sign in at `/admin/login`, then use **Admin users** to create operators with granular permissions.
3. Env credentials remain required for config validation even after DB users exist; rotate bootstrap password in DB if env is rotated.
4. **Audit log** and **Settings** require super admin. See [MiniSMS_Admin_Guide.md](./MiniSMS_Admin_Guide.md) §1.5 and §10.

## 5. Running the Application

### 5.1 Development (local)

```bash
cd /opt/minisms/minisms
cp .env.example .env
# edit .env with real values
go run ./cmd/minisms
```

Health check:

```bash
curl -i http://127.0.0.1:8080/healthz
```

Expected JSON keys:

```json
{"status":"ok","version":"...","commit":"...","build_time":"..."}
```

### 5.2 Production with systemd (hardened)

Create service user:

```bash
sudo useradd --system --home /opt/minisms --shell /usr/sbin/nologin minisms || true
sudo chown -R minisms:minisms /opt/minisms
```

Install binary/env:

```bash
sudo install -m 0755 /opt/minisms/minisms/bin/minisms /usr/local/bin/minisms
sudo install -m 0640 /opt/minisms/minisms/.env /etc/minisms.env
sudo chown root:minisms /etc/minisms.env
```

Unit file `/etc/systemd/system/minisms.service`:

```ini
[Unit]
Description=MiniSMS Gateway
After=network-online.target postgresql.service
Wants=network-online.target

[Service]
Type=simple
User=minisms
Group=minisms
WorkingDirectory=/opt/minisms/minisms
EnvironmentFile=/etc/minisms.env
ExecStart=/usr/local/bin/minisms
Restart=always
RestartSec=3
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ProtectControlGroups=true
ProtectKernelModules=true
ProtectKernelTunables=true
LockPersonality=true
MemoryDenyWriteExecute=true
ReadWritePaths=/opt/minisms
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
```

Enable/start:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now minisms
sudo systemctl status minisms --no-pager
```

Logs:

```bash
journalctl -u minisms -f
```

### 5.3 Docker (single host)

`Dockerfile`:

```dockerfile
FROM golang:1.22-bookworm AS build
WORKDIR /src
COPY . .
RUN go mod download
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/minisms ./cmd/minisms

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/minisms /app/minisms
COPY --from=build /src/templates /app/templates
COPY --from=build /src/static /app/static
COPY --from=build /src/deploy/minisms_db.sql /app/deploy/minisms_db.sql
EXPOSE 8080
ENTRYPOINT ["/app/minisms"]
```

`docker-compose.yml`:

```yaml
services:
  db:
    image: postgres:15
    environment:
      POSTGRES_DB: minisms
      POSTGRES_USER: minisms
      POSTGRES_PASSWORD: change_me_now
    volumes:
      - pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U minisms -d minisms"]
      interval: 10s
      timeout: 5s
      retries: 10

  app:
    build: .
    depends_on:
      db:
        condition: service_healthy
    environment:
      DATABASE_URL: postgres://minisms:change_me_now@db:5432/minisms?sslmode=disable
      SECRET_KEY: 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
      ADMIN_USERNAME: admin
      ADMIN_PASSWORD_HASH: '$2a$12$XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX'
      CSRF_AUTH_KEY: abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789
      PORT: "8080"
      LOG_LEVEL: info
      APP_ENV: production
      SESSION_IDLE_MINUTES: "240"
      CARRIER_DISPATCH_TIMEOUT_S: "10"
      TLS_ENABLED: "false"
    ports:
      - "8080:8080"

volumes:
  pgdata:
```

Run:

```bash
docker compose up -d --build
docker compose logs -f app
```

## 6. Reverse Proxy Configuration

## 6.1 nginx

`/etc/nginx/sites-available/minisms.conf`:

```nginx
server {
    listen 80;
    server_name sms.example.com;
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl http2;
    server_name sms.example.com;

    ssl_certificate     /etc/letsencrypt/live/sms.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/sms.example.com/privkey.pem;

    client_max_body_size 2m;

    location / {
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Connection "";
        proxy_pass http://127.0.0.1:8080;
    }
}
```

Enable:

```bash
sudo ln -sf /etc/nginx/sites-available/minisms.conf /etc/nginx/sites-enabled/minisms.conf
sudo nginx -t
sudo systemctl reload nginx
```

> Do not block `/api/v1/dlr/` at proxy level. Carrier callbacks must reach MiniSMS.

### 6.2 Caddy

`/etc/caddy/Caddyfile`:

```caddy
sms.example.com {
  encode gzip
  reverse_proxy 127.0.0.1:8080
}
```

Apply:

```bash
sudo caddy validate --config /etc/caddy/Caddyfile
sudo systemctl reload caddy
```

## 7. DLR Callback Network Requirements

- `/api/v1/dlr/*` must be publicly reachable from carrier networks.
- If firewall exists, allow inbound 443 to reverse proxy.

UFW example:

```bash
sudo ufw allow 443/tcp
sudo ufw status
```

DLR endpoint test (unknown message is intentionally discarded with 200):

```bash
curl -i -X POST "https://sms.example.com/api/v1/dlr/11111111-1111-1111-1111-111111111111"
```

Expected body:

```json
{"status":"ok"}
```

Carrier callback URL template to configure in MiniSMS/carrier templates:

```text
https://sms.example.com/api/v1/dlr/{{message_id}}
```

`{{message_id}}` is substituted by MiniSMS when dispatching the outbound carrier request.

## 8. Upgrading

Zero/near-zero downtime sequence:

1. Build new binary.
2. Apply schema changes if any (`make schema` or manual `ALTER` from diff against `deploy/minisms_db.sql`).
3. Swap binary.
4. Restart service.
5. Verify health and DLR path.

Commands:

```bash
cd /usr/src/MiniSMS/minisms
git fetch --all
git checkout <release-tag-or-commit>
go mod download
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/minisms ./cmd/minisms
sudo install -m 0755 bin/minisms /usr/local/bin/minisms
sudo systemctl restart minisms
curl -f http://127.0.0.1:8080/healthz
```

Rollback (binary only): see [agent/OPERATIONS.md](./agent/OPERATIONS.md).

## 9. Database Operations

### 9.1 Backup

```bash
export DATABASE_URL='postgres://minisms:change_me_now@localhost:5432/minisms?sslmode=disable'
pg_dump "$DATABASE_URL" -Fc -f "/var/backups/minisms_$(date +%F_%H%M%S).dump"
```

### 9.2 Restore

```bash
createdb -U minisms minisms_restore
pg_restore -U minisms -d minisms_restore --clean --if-exists /var/backups/minisms_YYYY-MM-DD_HHMMSS.dump
```

### 9.3 Operational queries

Client balances:

```sql
SELECT client_id, name, balance, currency, status
FROM clients
ORDER BY balance ASC;
```

Carrier balances:

```sql
SELECT carrier_id, name, balance, currency, status
FROM carriers
ORDER BY balance ASC;
```

Recent DLR activity:

```sql
SELECT message_id, dlr_status, dlr_forward_status, dlr_forwarded_at
FROM sms_logs
WHERE dlr_requested = TRUE
ORDER BY received_at DESC
LIMIT 50;
```

DLR forward failures:

```sql
SELECT message_id, dlr_status, dlr_forward_status, dlr_webhook_url, dlr_forwarded_at
FROM sms_logs
WHERE dlr_forward_status = 'failed'
ORDER BY received_at DESC
LIMIT 100;
```

DLRs with no forward URL:

```sql
SELECT message_id, client_id, dlr_status, dlr_forward_status
FROM sms_logs
WHERE dlr_forward_status = 'no_url'
ORDER BY received_at DESC
LIMIT 100;
```

Table size and growth watch:

```sql
SELECT
  relname AS table_name,
  pg_size_pretty(pg_total_relation_size(relid)) AS total_size
FROM pg_catalog.pg_statio_user_tables
ORDER BY pg_total_relation_size(relid) DESC;
```

## 10. Monitoring and Alerting

### 10.1 Health

```bash
curl -f http://127.0.0.1:8080/healthz
```

### 10.2 Logs

MiniSMS uses structured logging (`slog`). Ship `journalctl` or container stdout to your log backend.

### 10.3 Alert conditions

- Service health check failures (`/healthz` down)
- High `SMS_ERR_TEMPORARY_UNAVAILABLE` rate
- High `SMS_ERR_CARRIER_FAILURE` rate
- DLR forward failures (`dlr_forward_status='failed'`) above threshold
- DLR no URL count (`dlr_forward_status='no_url'`) above threshold
- Carrier callback volume drops unexpectedly (possible carrier callback outage)
- Low client balances and low carrier balances crossing configured thresholds

## 11. Troubleshooting

Format: symptom -> cause -> diagnostic -> fix.

### 11.1 API returns 401 unauthorized

- Cause: missing/invalid API key
- Diagnostic: check request headers
- Fix: send `X-API-Key` or `Authorization: Bearer`

### 11.2 API returns 429 rate limited

- Cause: `api_rate_limit_per_minute` exceeded
- Diagnostic: inspect traffic burst patterns
- Fix: throttle callers or increase setting

### 11.3 API returns 422 no rate / no route

- Cause: missing/misconfigured rate or routing group entries
- Diagnostic: inspect client `rate_group_id`, `routing_group_id`
- Fix: add active prefix entries/catch-all entries

### 11.4 API returns 402 insufficient balance

- Cause: prepaid client balance below required charge
- Diagnostic: query `clients.balance` and segment charge
- Fix: top up client balance (ledger credit/payment path)

### 11.5 Carrier not sending DLR callbacks

- Cause: callback URL not configured or unreachable
- Diagnostic:
  - verify carrier `dlr_callback_url_template`
  - verify carrier-side callback config
  - test public reachability of `/api/v1/dlr/*`
- Fix: set proper callback URL template and open network/proxy path

### 11.6 DLR received but webhook forward fails

- Cause: client webhook returns non-2xx or times out
- Diagnostic: query `sms_logs.dlr_forward_status='failed'`
- Fix: repair client webhook endpoint; resend externally if needed (MiniSMS does not auto-retry)

### 11.7 DLR received but `dlr_forward_status='no_url'`

- Cause: `dlr=YES` but no effective webhook URL
- Diagnostic: inspect `sms_logs.dlr_requested` and `dlr_webhook_url`
- Fix: configure client `dlr_webhook_url` or provide per-message `dlr_url`

### 11.8 DLR endpoint returns 403

- Cause: carrier inbound secret mismatch
- Diagnostic: compare configured carrier secret with sent query/header secret
- Fix: update carrier `dlr_inbound_secret` and carrier callback credentials

### 11.9 Wrong DLR status is stored

- Cause: wrong `dlr_status_field` or `dlr_status_map`
- Diagnostic: capture raw carrier callback payload and compare mapping
- Fix: correct carrier status field and mapping JSON

### 11.10 Admin login fails

- Cause: invalid `ADMIN_PASSWORD_HASH` format
- Diagnostic: startup error indicates bcrypt parse failure
- Fix: regenerate bcrypt hash and restart

### 11.11 Admin login temporarily blocked (429)

- Cause: too many failed login attempts from same username+IP window
- Diagnostic: login returns HTTP 429 with temporary lock message
- Fix: wait for block window to expire, then retry with correct credentials

### 11.11 Schema apply failure on deploy

- Cause: `psql -f deploy/minisms_db.sql` error (object already exists, permission denied)
- Diagnostic: read `psql` stderr; compare live schema with `pg_dump --schema-only`
- Fix: apply only missing `ALTER`/`CREATE IF NOT EXISTS` sections, or restore from backup on maintenance window

## 12. Security Hardening Checklist

- [ ] Run MiniSMS as non-root user (`minisms`)
- [ ] Restrict `.env` file permissions (`640`, root:minisms)
- [ ] Use strong random `SECRET_KEY` and `CSRF_AUTH_KEY`
- [ ] Use TLS at edge proxy (nginx or Caddy)
- [ ] Restrict PostgreSQL to trusted hosts/networks
- [ ] Enable systemd hardening options (NoNewPrivileges, ProtectSystem, etc.)
- [ ] Centralize logs and set alerting
- [ ] Enforce regular backups and restore drills
- [ ] Rotate admin password hash and API keys periodically
- [ ] Monitor repeated admin login failures and investigate suspicious IP patterns
- [ ] Enforce client webhook HTTPS URLs
- [ ] `/api/v1/dlr/*` remains publicly accessible (required callbacks)
- [ ] Configure inbound DLR secret on carriers that support shared secret
- [ ] Implement HMAC verification in client webhook receivers
- [ ] Ensure `dlr_webhook_secret` and `dlr_inbound_secret` are encrypted at rest via `SECRET_KEY`

## 13. Canonical Route Contract

This section normalizes the documented paths to the exact implemented route patterns.

### 13.1 Public routes

| Method | Path |
|---|---|
| GET | `/healthz` |
| GET | `/admin/login` |
| POST | `/admin/login` |
| GET | `/admin/logout` |
| GET/POST | `/api/v1/dlr/{message_id}` |
| GET/POST | `/api/v1/dlr` |

### 13.2 Client API routes (API key auth)

| Method | Path |
|---|---|
| POST | `/api/v1/sms/send` |
| GET | `/api/v1/account/balance` |
| GET | `/api/v1/sms/status/{message_id}` |

### 13.3 Admin route naming notes (important)

The following parameter names are canonical in code and should be used in operational scripts/tests:

| Functional route | Canonical implemented path |
|---|---|
| Currency update | `PUT /admin/currencies/{code}` |
| Currency toggle | `POST /admin/currencies/{code}/toggle` |
| Carrier sender ID remove | `DELETE /admin/carriers/{id}/sender-ids/{cid}` |
| Client sender ID remove | `DELETE /admin/clients/{id}/sender-ids/{cid}` |

### 13.4 Additional operational admin routes

| Method | Path |
|---|---|
| GET/POST | `/admin/simulate` |
| GET | `/admin/audit-log` |
| GET | `/admin/sms-logs/export.csv` |
| GET | `/admin/sms-logs/export.pdf` |
| GET | `/admin/dashboard/reports` and `/admin/dashboard/reports/*` |

