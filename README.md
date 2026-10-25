# Self-Hosted Secret Santa 🎅

Privacy-first, passwordless, bilingual (French / English) Secret Santa web application packaged as a single Go binary with an embedded Vue 3 SPA and persistent SQLite storage. Designed for effortless self-hosting on any NAS (Synology, QNAP, TrueNAS, Raspberry Pi — `amd64` & `arm64`) behind a reverse proxy such as Nginx.

---

## Quick Start (Docker Compose on NAS)

1. Copy the environment configuration template:
   ```bash
   cp .env.example .env
   ```
2. Edit `.env` for production (`APP_ENV=production`, `APP_BASE_URL=https://santa.mydomain.com`, `SUPER_ADMIN_EMAIL`, `SUPER_ADMIN_PASSWORD`, `SESSION_SECRET`, and `SMTP_*` settings).
3. Start the container:
   ```bash
   docker compose up -d
   ```
4. Idempotent SQLite migrations run automatically on startup, and all persistent data is stored in `./data/secret-santa.db` (WAL mode enabled).

---

## Environment Variables (`.env`)

| Variable | Default | Description |
| :--- | :--- | :--- |
| `APP_ENV` | `development` | `development` (strict console mock emails, zero SMTP network calls) or `production` (real SMTP) |
| `PORT` | `8080` | HTTP listen port inside the container |
| `APP_BASE_URL` | `http://localhost:8080` | Mandatory in production (e.g., `https://santa.mydomain.com`); used to build magic links |
| `DB_PATH` | `./data/secret-santa.db` | Path to the SQLite database file (`/data/secret-santa.db` in Docker) |
| `SUPER_ADMIN_EMAIL` | `admin@example.com` | Email for `/superadmin` login and SMTP test target |
| `SUPER_ADMIN_PASSWORD` | — | Password for `/superadmin` (must be changed in production) |
| `SESSION_SECRET` | — | Secret key used to sign the `HttpOnly; SameSite=Strict` Super Admin cookie (required in production) |
| `SMTP_HOST` | — | SMTP server hostname (required in production) |
| `SMTP_PORT` | `587` | SMTP server port (`587`, `465`, or `25`) |
| `SMTP_USER` | — | SMTP username (optional if using an unauthenticated local relay) |
| `SMTP_PASS` | — | SMTP password |
| `SMTP_FROM` | `Secret Santa <noreply@example.com>` | `From` header and envelope sender address |
| `SMTP_TLS_MODE` | `starttls` | `starttls` (port 587), `tls` (implicit TLS, port 465), or `none` (port 25 / local relay) |

---

## SQLite Persistence & Backups on NAS

The SQLite database uses **WAL (Write-Ahead Logging)** mode (`journal_mode=WAL`, `busy_timeout=5000`, `foreign_keys=1`), creating three files in `./data`:
- `secret-santa.db`
- `secret-santa.db-wal`
- `secret-santa.db-shm`

### Safe Online Backup
To take a consistent hot backup while the container is running:
```bash
sqlite3 ./data/secret-santa.db ".backup './data/secret-santa-backup.db'"
```
Or stop the container briefly (which checkpoints and closes the WAL cleanly on `SIGTERM`) and copy `./data/secret-santa.db`:
```bash
docker compose stop
cp ./data/secret-santa.db ./secret-santa-backup-$(date +%F).db
docker compose start
```

---

## Local Development

- **Frontend (`web/`)**:
  ```bash
  cd web
  npm ci
  npm run typecheck
  npm test
  npm run build
  ```
- **Backend (`Go`)**:
  ```bash
  go vet ./...
  go test -race -v ./...
  go run ./cmd/server
  ```
  In `APP_ENV=development` (default), **no SMTP network connection is ever opened** (even if `SMTP_*` variables are set); all emails and clickable magic links are handled by `ConsoleMockMailer` (`MockMailer`) and printed to the terminal logs.

---

## Recommended Nginx Reverse Proxy Configuration

```nginx
limit_req_zone $binary_remote_addr zone=santa_api:10m rate=10r/s;

server {
    listen 443 ssl http2;
    server_name santa.mydomain.com;

    add_header Referrer-Policy "no-referrer" always;

    location /api/ {
        limit_req zone=santa_api burst=20 nodelay;
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_read_timeout 30s;
    }

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
```
