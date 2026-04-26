# MiniSMS Quick Start (Ubuntu 24.04 LTS)

This runbook takes a fresh Ubuntu 24.04 server to a live MiniSMS service.

────────────────────────────────────────────────────
SECTION 0: Before You Begin
────────────────────────────────────────────────────

Prerequisites:

1. Ubuntu 24.04 LTS server
2. Non-root user with `sudo`
3. Domain pointing to server public IP
4. Reachable ports `22`, `80`, `443`
5. At least 1 GB RAM and 10 GB disk

If you start from root:

```bash
adduser deploy
usermod -aG sudo deploy
su - deploy
```

────────────────────────────────────────────────────
SECTION 1: System Update and Dependencies
────────────────────────────────────────────────────

Step 1.1 — Update the system and base tools:

```bash
sudo apt update && sudo apt upgrade -y
sudo apt install -y curl wget git build-essential ca-certificates software-properties-common apt-transport-https gnupg lsb-release
```

Step 1.2 — Install Go 1.22+:

```bash
wget https://go.dev/dl/go1.22.5.linux-amd64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.22.5.linux-amd64.tar.gz
rm go1.22.5.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.profile
export PATH=$PATH:/usr/local/go/bin
go version
```

Step 1.3 — Install PostgreSQL 15:

```bash
curl -fsSL https://www.postgresql.org/media/keys/ACCC4CF8.asc | sudo gpg --dearmor -o /usr/share/keyrings/postgresql-archive-keyring.gpg
echo "deb [signed-by=/usr/share/keyrings/postgresql-archive-keyring.gpg] https://apt.postgresql.org/pub/repos/apt $(lsb_release -cs)-pgdg main" | sudo tee /etc/apt/sources.list.d/pgdg.list
sudo apt update
sudo apt install -y postgresql-15 postgresql-client-15
sudo systemctl enable --now postgresql
```

Step 1.4 — Install nginx:

```bash
sudo apt install -y nginx
sudo systemctl enable --now nginx
```

Step 1.5 — Install Certbot (Let's Encrypt TLS):

```bash
sudo apt install -y certbot python3-certbot-nginx
```

✅ Checkpoint:

```bash
go version && psql --version && nginx -v
```

────────────────────────────────────────────────────
SECTION 2: PostgreSQL Database Setup
────────────────────────────────────────────────────

Step 2.1 — Create the database role and database:

```bash
sudo -u postgres psql <<'SQL'
CREATE ROLE minisms LOGIN PASSWORD 'CHANGE_THIS_PASSWORD';
CREATE DATABASE minisms OWNER minisms;
\c minisms
CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS citext;
SQL
```

ℹ️ Note: Save this DB password. You will use it in `DATABASE_URL`.

✅ Checkpoint:

```bash
sudo -u postgres psql -d minisms -c "SELECT current_database(), current_user;"
```

────────────────────────────────────────────────────
SECTION 3: Clone the Repository and Build
────────────────────────────────────────────────────

Step 3.1 — Clone the repository:

```bash
cd ~
git clone https://github.com/imfanee/MiniSMS.git
cd minisms/minisms
```

Step 3.2 — Download dependencies:

```bash
go mod download
```

Step 3.3 — Run tests (optional but recommended):

```bash
go test ./...
```

Step 3.4 — Build the production binary:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s -X main.version=$(git describe --tags --always --dirty)" -o bin/minisms ./cmd/minisms
```

Step 3.5 — Build the password hash utility:

```bash
go build -o bin/hashpassword ./tools/hashpassword/
```

Step 3.6 — Install the binary:

```bash
sudo install -m 755 bin/minisms /usr/local/bin/minisms
```

✅ Checkpoint:

```bash
ls -lh /usr/local/bin/minisms
```

────────────────────────────────────────────────────
SECTION 4: Import the Database Schema
────────────────────────────────────────────────────

Step 4.1 — Import the complete schema:

```bash
psql "postgres://minisms:CHANGE_THIS_PASSWORD@localhost:5432/minisms" -f deploy/minisms_db.sql
```

Step 4.2 — Verify the schema was created:

```bash
psql "postgres://minisms:CHANGE_THIS_PASSWORD@localhost:5432/minisms" <<'SQL'
SELECT table_name
FROM information_schema.tables
WHERE table_schema = 'public'
ORDER BY table_name;
SQL
```

Step 4.3 — Verify seed data was inserted:

```bash
psql "postgres://minisms:CHANGE_THIS_PASSWORD@localhost:5432/minisms" -c "SELECT COUNT(*) FROM currencies;"
psql "postgres://minisms:CHANGE_THIS_PASSWORD@localhost:5432/minisms" -c "SELECT COUNT(*) FROM system_settings;"
```

✅ Checkpoint: currencies count must be `20`.

────────────────────────────────────────────────────
SECTION 5: Configuration
────────────────────────────────────────────────────

Step 5.1 — Create the configuration directory:

```bash
sudo mkdir -p /etc/minisms
```

Step 5.2 — Generate secret keys:

```bash
openssl rand -hex 32
openssl rand -hex 32
```

⚠️ Warning: If you lose `SECRET_KEY`, encrypted carrier auth headers and DLR secrets cannot be decrypted.

Step 5.3 — Generate the admin password hash:

```bash
~/minisms/minisms/bin/hashpassword
```

Step 5.4 — Create the environment file:

```bash
sudo cp ~/minisms/minisms/deploy/minisms.env.example /etc/minisms/minisms.env
sudo nano /etc/minisms/minisms.env
```

Set these values at minimum:

- `DATABASE_URL`
- `SECRET_KEY`
- `ADMIN_USERNAME`
- `ADMIN_PASSWORD_HASH`
- `CSRF_AUTH_KEY`
- `APP_ENV=production`

Step 5.5 — Lock down the environment file:

```bash
sudo chmod 600 /etc/minisms/minisms.env
```

✅ Checkpoint:

```bash
sudo ls -la /etc/minisms/minisms.env
```

────────────────────────────────────────────────────
SECTION 6: Create the Service User and Set Up 
PostgreSQL Access
────────────────────────────────────────────────────

Step 6.1 — Create the minisms system user:

```bash
sudo useradd --system --no-create-home --shell /usr/sbin/nologin --comment "MiniSMS service account" minisms
```

Step 6.2 — Set ownership of the environment file:

```bash
sudo chown minisms:minisms /etc/minisms
sudo chown minisms:minisms /etc/minisms/minisms.env
```

Step 6.3 — Verify PostgreSQL accepts connections from the service user:

```bash
sudo -u minisms psql "postgres://minisms:CHANGE_THIS_PASSWORD@localhost:5432/minisms" -c "SELECT 1 AS connection_test;"
```

────────────────────────────────────────────────────
SECTION 7: Install and Start the systemd Service
────────────────────────────────────────────────────

Step 7.1 — Install the unit file:

```bash
sudo cp ~/minisms/minisms/deploy/minisms.service /etc/systemd/system/minisms.service
```

Step 7.2 — Reload systemd and enable the service:

```bash
sudo systemctl daemon-reload
sudo systemctl enable minisms
```

Step 7.3 — Start the service:

```bash
sudo systemctl start minisms
```

Step 7.4 — Check service status:

```bash
sudo systemctl status minisms
```

Step 7.5 — View service logs:

```bash
sudo journalctl -u minisms -f
```

Step 7.6 — Verify the service is responding:

```bash
curl -s http://localhost:8080/healthz
```

✅ Checkpoint:

```bash
sudo systemctl is-active minisms
```

────────────────────────────────────────────────────
SECTION 8: Configure nginx Reverse Proxy with TLS
────────────────────────────────────────────────────

ℹ️ Replace `YOUR_DOMAIN` with your real domain in this section.

Step 8.1 — Create the nginx configuration:

```bash
sudo tee /etc/nginx/sites-available/minisms >/dev/null <<'NGINX'
server {
    listen 80;
    listen [::]:80;
    server_name YOUR_DOMAIN;
    location /.well-known/acme-challenge/ { root /var/www/html; }
    location / { return 301 https://$host$request_uri; }
}
server {
    listen 443 ssl;
    listen [::]:443 ssl;
    server_name YOUR_DOMAIN;
    ssl_certificate     /etc/letsencrypt/live/YOUR_DOMAIN/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/YOUR_DOMAIN/privkey.pem;
    ssl_protocols       TLSv1.2 TLSv1.3;
    location / {
        proxy_pass         http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header   Host              $host;
        proxy_set_header   X-Real-IP         $remote_addr;
        proxy_set_header   X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header   X-Forwarded-Proto $scheme;
    }
    location /api/v1/dlr/ {
        proxy_pass         http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header   Host              $host;
        proxy_set_header   X-Real-IP         $remote_addr;
        proxy_set_header   X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header   X-Forwarded-Proto $scheme;
    }
}
NGINX
```

Step 8.2 — Enable the site:

```bash
# Temporarily comment out ssl_certificate lines if needed before first cert issuance
sudo ln -sf /etc/nginx/sites-available/minisms /etc/nginx/sites-enabled/minisms
sudo nginx -t
sudo systemctl reload nginx
```

Step 8.3 — Obtain TLS certificate from Let's Encrypt:

```bash
sudo certbot --nginx -d YOUR_DOMAIN --non-interactive --agree-tos --email your-email@example.com --redirect
```

Step 8.4 — Verify TLS and auto-renewal:

```bash
sudo nginx -t && sudo systemctl reload nginx
curl -s https://YOUR_DOMAIN/healthz
sudo certbot renew --dry-run
```

✅ Checkpoint:

```bash
curl -sI https://YOUR_DOMAIN/healthz
```

────────────────────────────────────────────────────
SECTION 9: Final System Verification
────────────────────────────────────────────────────

Step 9.1 — Service health:

```bash
sudo systemctl is-active minisms
curl -s https://YOUR_DOMAIN/healthz
```

Step 9.2 — API health check:

```bash
curl -s -o /dev/null -w "%{http_code}" https://YOUR_DOMAIN/api/v1/account/balance
curl -s -X POST https://YOUR_DOMAIN/api/v1/dlr/00000000-0000-0000-0000-000000000000 -H "Content-Type: application/json" -d '{}'
```

Step 9.3 — PostgreSQL connection from service:

```bash
sudo journalctl -u minisms --since "5 minutes ago" | rg -i error || true
```

Step 9.4 — Check service restarts on failure (optional validation):

```bash
sudo kill -9 $(sudo systemctl show --property MainPID minisms | cut -d= -f2)
sleep 8
sudo systemctl is-active minisms
```

Step 9.5 — Firewall (recommended):

```bash
sudo apt install -y ufw
sudo ufw default deny incoming
sudo ufw default allow outgoing
sudo ufw allow ssh
sudo ufw allow http
sudo ufw allow https
sudo ufw --force enable
sudo ufw status
```

✅ Checkpoint 9: system live and verified.

────────────────────────────────────────────────────
SECTION 10: First Configuration Steps After Go-Live
────────────────────────────────────────────────────

1. Open `https://YOUR_DOMAIN/admin/login`.
2. Review `Settings`.
3. Add one `Carrier`.
4. Add one `Rate Group` and prefix rates.
5. Add one `Routing Group` and route entries.
6. Add one `Client`, assign groups, add balance, generate API key.

Test SMS:

```bash
curl -X POST https://YOUR_DOMAIN/api/v1/sms/send -H "Authorization: Bearer YOUR_CLIENT_API_KEY" -H "Content-Type: application/json" -d '{"to":"+447700900123","message":"MiniSMS is live. Test message.","dlr":"YES"}'
```

────────────────────────────────────────────────────
SECTION 11: Ongoing Operations Reference
────────────────────────────────────────────────────

Service management:

```bash
sudo systemctl start minisms
sudo systemctl stop minisms
sudo systemctl restart minisms
sudo systemctl reload minisms
sudo systemctl status minisms
sudo journalctl -u minisms -f
```

Update deployment:

```bash
cd ~/minisms/minisms
git pull
CGO_ENABLED=0 go build -ldflags="-w -s -X main.version=$(git describe --tags --always)" -o bin/minisms ./cmd/minisms
sudo install -m 755 bin/minisms /usr/local/bin/minisms
sudo systemctl restart minisms
```

Backup database:

```bash
pg_dump -h localhost -U minisms -d minisms -F c -f /backup/minisms_$(date +%Y%m%d_%H%M%S).dump
```

Changing admin password:

```bash
cd ~/minisms/minisms
./bin/hashpassword
sudo nano /etc/minisms/minisms.env
sudo systemctl restart minisms
```

Rotating secret keys:

⚠️ Rotating `SECRET_KEY` invalidates encrypted carrier auth headers and DLR webhook secrets. Re-enter them in the Admin UI after rotation.

```bash
openssl rand -hex 32
sudo nano /etc/minisms/minisms.env
sudo systemctl restart minisms
```

────────────────────────────────────────────────────
SECTION 12: Troubleshooting Quick Reference
────────────────────────────────────────────────────

| Symptom | Check | Fix |
|---|---|---|
| Service fails to start | `sudo journalctl -u minisms -n 50` | Fix env values in `/etc/minisms/minisms.env` |
| 502 from nginx | `curl -s http://localhost:8080/healthz` | Restart `minisms` service |
| Admin login fails | Verify `ADMIN_PASSWORD_HASH` | Regenerate hash with `bin/hashpassword` |
| DB auth errors | Verify `DATABASE_URL` password | Reset DB role password and env value |
| TLS issues | `sudo certbot renew --dry-run` | Re-issue certificate |
| Port conflict | `sudo ss -ltnp | rg ':8080'` | Free port or change `PORT` |

For full operations detail, use `doc/MiniSMS_DevOps_Guide.md`.

────────────────────────────────────────────────────
Appendix A: Makefile Reference
────────────────────────────────────────────────────

```bash
cd ~/minisms/minisms
make build
make run
make test
make migrate
make hash-password
make build-tools
make schema DB_URL=postgres://minisms:password@localhost/minisms
make vet
make clean
make docker-build
```

────────────────────────────────────────────────────
Appendix B: Repository Structure
────────────────────────────────────────────────────

```text
cmd/minisms/
internal/
templates/
static/
migrations/
deploy/
tools/
doc/
```
