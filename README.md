# Self-Hosted Secret Santa 🎅

Privacy-first, passwordless, bilingual (French / English) Secret Santa web application packaged as a single Go binary with an embedded Vue 3 SPA and persistent SQLite storage. Designed for effortless self-hosting on any NAS (Synology, QNAP, TrueNAS, Raspberry Pi — `amd64` & `arm64`) behind a reverse proxy such as Nginx.

---

## Quick Start (Docker Compose on NAS)

1. Copy the environment configuration template:
   ```bash
   cp .env.example .env
   ```
2. Edit `.env` for production (`APP_ENV=production`, `APP_BASE_URL=https://santa.mydomain.com`, `SUPER_ADMIN_EMAIL`, `SESSION_SECRET`, and `SMTP_*` settings) and set `OPERATOR_NAME` / `OPERATOR_EMAIL` (required in production) and the Super Admin password hash (see step 3). Public event creation is off until you invite organizers or enable it (see below).
3. Set the Super Admin password. It prompts for a password (min 12 characters) and writes `SUPER_ADMIN_PASSWORD_HASH` into `.env`. From a repository checkout with `make`:
   ```bash
   make set-password
   ```
   Without `make` (for example on the NAS itself), run the image with its entrypoint overridden. The entrypoint wrapper ignores arguments, so a plain `docker compose run … set-password` would not work:
   ```bash
   docker compose run --rm --no-deps --user "$(id -u):$(id -g)" \
     --entrypoint /app/secret-santa -v "$PWD:/work" \
     secret-santa set-password --env /work/.env
   ```
4. Start the container:
   ```bash
   docker compose up -d
   ```
5. The SQLite schema is created automatically on startup, and all persistent data is stored in `./data/secret-santa.db` (WAL mode enabled).

---

## Environment Variables (`.env`)

| Variable | Default | Description |
| :--- | :--- | :--- |
| `APP_ENV` | `development` | `development` (strict console mock emails, zero SMTP network calls) or `production` (real SMTP) |
| `BIND_ADDR` | `127.0.0.1` | Host interface the Docker Compose port binds to (read by `docker-compose.yml`, not by the app) |
| `PORT` | `8080` | HTTP listen port inside the container |
| `APP_BASE_URL` | `http://localhost:8080` | Mandatory in production (e.g., `https://santa.mydomain.com`); used to build magic links |
| `DB_PATH` | `./data/secret-santa.db` | Path to the SQLite database file (`/data/secret-santa.db` in Docker) |
| `SUPER_ADMIN_EMAIL` | `admin@example.com` | Email for `/superadmin` login and SMTP test target |
| `SUPER_ADMIN_PASSWORD_HASH` | — | PBKDF2-HMAC-SHA256 password hash for `/superadmin` (set via `make set-password`, or the `docker compose run` command in Quick Start) |
| `SESSION_SECRET` | — | Secret key used to sign the `HttpOnly; SameSite=Strict` Super Admin cookie (required in production) |
| `SMTP_HOST` | — | SMTP server hostname (required in production) |
| `SMTP_PORT` | `587` | SMTP server port (`587`, `465`, or `25`) |
| `SMTP_USER` | — | SMTP username (optional if using an unauthenticated local relay) |
| `SMTP_PASS` | — | SMTP password |
| `OPERATOR_NAME` | — | Name of the person running the instance (data controller). Required in production, shown on `/privacy` |
| `OPERATOR_EMAIL` | — | Contact address shown on `/privacy`. Required in production, must be a valid email |
| `SMTP_FROM` | `Secret Santa <noreply@example.com>` | `From` header and envelope sender address |
| `SMTP_TLS_MODE` | `starttls` | `starttls` (port 587), `tls` (implicit TLS, port 465), or `none` (port 25 / local relay) |
| `TRUST_PROXY` | `false` | Set to `true` behind a reverse proxy that sets `X-Forwarded-For` (the Nginx config below does): rate limiting then uses the right-most entry instead of the proxy's address. Leave `false` if the app is directly exposed (the header is client-controlled) |

---

## Invite-only by default

A fresh database starts with **public event creation disabled**: the home page shows no creation form and `POST /api/events` answers `403`. To let someone organize an event:
- Log in at `/superadmin` and use **Invite an organizer**: you give an event title and the organizer's name and email; the event is created for them and they receive its administration link by email.
- Or turn on the **public event creation** toggle in the same page to open creation to everyone (rate limited to 20 creations per client per hour). You can turn it off again at any time.

## Data retention

Events are deleted automatically, with their participants, exclusions and wishlists: 30 days after the event date, or 12 months after creation when no date was set. The purge runs at startup and then every 24 hours. The rule and the operator identity are shown on the `/privacy` page.

## SQLite Persistence & Backups

The database uses **WAL** mode, so `./data` holds `secret-santa.db`, `secret-santa.db-wal` and `secret-santa.db-shm`. Do not copy the `.db` file alone while the app runs.

### Backup
`secret-santa backup <dst>` writes a consistent copy of the live database (`VACUUM INTO`, no `sqlite3` binary needed, safe while the app is running). It refuses to overwrite an existing file and creates the destination directory. `DB_PATH` is the only setting it reads.

Nightly cron on the host (14-day rotation; the container path `/data` is the `./data` folder):
```cron
15 3 * * * cd /path/to/secret-santa && docker compose exec -T secret-santa /app/secret-santa backup /data/backups/santa-$(date +\%F).db && find ./data/backups -name 'santa-*.db' -mtime +14 -delete
```
Then sync `./data/backups/` off-site (rsync, rclone, NAS snapshot replication...). Also back up `.env`: it holds `SESSION_SECRET`, the Super Admin hash and the SMTP credentials, and is not part of the database. Backups contain every magic link: store them with the same care as the live database.

### Restore
```bash
docker compose stop
rm -f ./data/secret-santa.db-wal ./data/secret-santa.db-shm   # stale WAL would corrupt the restored file
cp ./data/backups/santa-YYYY-MM-DD.db ./data/secret-santa.db
sudo chown 10001 ./data/secret-santa.db                        # the container user
docker compose start
```

### Health check
`GET /api/health` returns `200 {"status":"ok"}` when the database answers and `503` otherwise. Point your uptime monitor at it.

---

## Email operator checklist

Draw emails are only useful if they reach the inbox. Before going live:
- **SPF**: a TXT record on the sending domain that authorizes your SMTP provider.
- **DKIM**: enable signing at your provider and publish the key it gives you.
- **DMARC**: publish at least `v=DMARC1; p=none; rua=mailto:you@example.com`, then tighten.
- **PTR**: if you send from your own IP, its reverse DNS must match the sending hostname.
- **Ports**: many ISPs block outbound 25; use 587 (`starttls`) or 465 (`tls`).
- **App passwords**: with Gmail and similar providers, use an app password as `SMTP_PASS`, not the account password.
- **Test**: use *Send test email* in `/superadmin`, then score a message with [mail-tester.com](https://www.mail-tester.com) (use that address as an invitation target).

Mail the app sends is rate limited: each participant can be sent a link at most 3 times per hour, and the "resend failed" action is limited to 5 calls per event per hour.

---

## Local Development

Prerequisites: **`make` and Podman** (or Docker) — nothing else (no Go, no Node). Every task runs in a container:

```bash
make dev                      # backend :8080 + Vite HMR :5173
make install                  # install dependencies
make lint [SCOPE=frontend|backend]   # typecheck / go vet (default: both)
make test [SCOPE=frontend|backend]   # Vitest / go test -race (default: both)
make build                    # build the production image
make set-password             # write the Super Admin hash to .env
make clean
```
`make test-backend`, `make lint-frontend`, etc. also work. Set `RT=docker` to force a runtime.

### Dev Container (editor with Go + Vue tooling)
Open the folder in VS Code and choose **Reopen in Container** (`.devcontainer/`). It provides Go, Node, gopls and Volar; the same `make dev`, `make test`, `make lint` commands run natively inside it, and ports 8080/5173 are forwarded. With Podman, set `"dev.containers.dockerPath": "podman"` in your VS Code settings. With Docker, remove `"--userns=keep-id"` from `runArgs` in `devcontainer.json`.

  In `APP_ENV=development` (default), **no SMTP network connection is ever opened** (even if `SMTP_*` variables are set); all emails and clickable magic links are handled by `ConsoleMockMailer` (`MockMailer`) and printed to the terminal logs.

---

## Recommended Nginx Reverse Proxy Configuration

Set `TRUST_PROXY=true` (the `.env.example` default) so rate limiting sees real client addresses. The app trusts the right-most `X-Forwarded-For` entry, which is the one Nginx appends.

```nginx
limit_req_zone $binary_remote_addr zone=santa_api:10m rate=10r/s;

# Magic-link tokens are secrets: keep them out of the access log.
map $request_uri $santa_logged_uri {
    ~^(?<pfx>/(?:p|admin/events|api/p|api/admin/events|api/superadmin/events)/)[^/?]+(?<rest>.*)$ "${pfx}[redacted]${rest}";
    default $request_uri;
}
log_format santa '$remote_addr - [$time_local] "$request_method $santa_logged_uri" '
                 '$status $body_bytes_sent "$http_user_agent"';

server {
    listen 80;
    server_name santa.mydomain.com;
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl;
    http2 on;
    server_name santa.mydomain.com;

    ssl_certificate     /etc/letsencrypt/live/santa.mydomain.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/santa.mydomain.com/privkey.pem;

    access_log /var/log/nginx/santa.access.log santa;
    add_header Referrer-Policy "no-referrer" always;

    # Optional: restrict the back-office to your LAN / VPN (put before the other locations).
    # location ~ ^/(api/)?superadmin {
    #     allow 192.168.0.0/16;
    #     allow 10.0.0.0/8;
    #     deny  all;
    #     proxy_pass http://127.0.0.1:8080;
    #     proxy_set_header Host $host;
    #     proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    #     proxy_set_header X-Forwarded-Proto $scheme;
    # }

    location /api/ {
        limit_req zone=santa_api burst=20 nodelay;
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_read_timeout 120s;   # a draw sends its emails synchronously, up to 90s
    }

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```
