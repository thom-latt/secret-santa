# Self-Hosted Secret Santa 🎅

Privacy-first, passwordless, bilingual (French / English) Secret Santa web application packaged as a single Go binary with an embedded Vue 3 SPA and persistent SQLite storage. Designed for effortless self-hosting on any NAS (Synology, QNAP, TrueNAS, Raspberry Pi) behind a reverse proxy such as Nginx.

---

## Quick Start (Docker Compose on NAS)

1. Copy the environment configuration template:
   ```bash
   cp .env.example .env
   ```
2. Edit `.env` (`APP_BASE_URL`, `SUPER_ADMIN_EMAIL`, `SUPER_ADMIN_PASSWORD`, `SESSION_SECRET`, and `SMTP_*` settings).
3. Start the container:
   ```bash
   docker compose up -d --build
   ```
4. Persistent SQLite data is stored in `./data/secret-santa.db` (WAL mode enabled).

---

## Local Development

- **Frontend (`web/`)**:
  ```bash
  cd web
  npm install
  npm run typecheck
  npm test
  npm run build
  ```
- **Backend (`Go`)**:
  ```bash
  go vet ./...
  go test -v ./...
  go run ./cmd/server
  ```
  In `APP_ENV=development` (default), **no SMTP network connection is ever opened**; all emails and magic links are printed to the console via `MockMailer`.

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
