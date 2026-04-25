# Montly Deployment Guide

This guide covers deploying montly on a self-hosted server with HTTPS, either behind a reverse proxy or directly.

---

## Quick start (local / LAN, no HTTPS)

```bash
docker compose up -d
```

Open `http://localhost:8080` — on first access you'll be prompted to create the admin account through the UI.

> **Automated / headless deployment:** Pass `ADMIN_USERNAME` and `ADMIN_PASSWORD` as environment variables to skip the UI setup and create the admin account on startup instead.

---

## Environment variables

| Variable           | Default      | Description |
|--------------------|--------------|-------------|
| `PORT`             | `8080`       | TCP port to listen on |
| `DATA_DIR`         | `./data`     | Directory for SQLite file and receipt uploads |
| `ADMIN_USERNAME`   | *(optional)*  | Username for the initial admin account; omit to use the first-run UI setup instead |
| `ADMIN_PASSWORD`   | *(optional)*  | Password for the initial admin account — must be ≥ 8 characters |
| `SESSION_SECRET`   | *(random)*   | HMAC key for session cookies. Set a stable value so sessions survive restarts |
| `SECURE_COOKIES`   | `false`      | Set `true` when serving over HTTPS — adds `Secure` flag to cookies, HSTS header, and `upgrade-insecure-requests` to the CSP |
| `DB_TYPE`          | `sqlite`     | `sqlite` or `postgres` |
| `DATABASE_URL`     | *(required for postgres)* | Full Postgres connection string |
| `TRUST_PROXY_HEADERS` | `false`   | Set `true` when behind a trusted reverse proxy that sets `X-Forwarded-For`. Required for accurate IP-based rate limiting. **Do not enable** unless a proxy you control is stripping or overwriting this header. |

---

## Production compose file

Create `docker-compose.prod.yml`:

```yaml
services:
  montly:
    image: montly:latest   # or build: .
    ports:
      - "127.0.0.1:8080:8080"
    volumes:
      - montly_data:/data
    environment:
      DATA_DIR: /data
      PORT: "8080"
      SESSION_SECRET: "replace-with-a-long-random-string"
      SECURE_COOKIES: "true"
    restart: unless-stopped

volumes:
  montly_data:
```

> **Tip:** Generate a session secret with `openssl rand -base64 32`.

---

## Reverse proxy setup

montly does not terminate TLS itself. Put it behind Caddy or nginx.

### Caddy (recommended)

```Caddyfile
montly.example.com {
    reverse_proxy localhost:8080
}
```

Caddy provisions and auto-renews a Let's Encrypt certificate. No further TLS config needed.

### nginx

```nginx
server {
    listen 80;
    server_name montly.example.com;
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl http2;
    server_name montly.example.com;

    ssl_certificate     /etc/letsencrypt/live/montly.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/montly.example.com/privkey.pem;

    # Forward real client IP for rate limiting
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header Host $host;

    location / {
        proxy_pass http://127.0.0.1:8080;
    }
}
```

Obtain a certificate with Certbot: `certbot --nginx -d montly.example.com`.

> **Important:** nginx forwards `X-Forwarded-For`, but montly ignores it unless you also set `TRUST_PROXY_HEADERS: "true"` in the montly container's environment. Without it, the login rate limiter sees only the loopback IP and provides no per-user protection.

---

## PostgreSQL backend

Use `docker-compose.postgres.yml` as a starting point, or add a Postgres service to your compose file:

```yaml
services:
  db:
    image: postgres:16-alpine
    environment:
      POSTGRES_DB: montly
      POSTGRES_USER: montly
      POSTGRES_PASSWORD: dbpassword
    volumes:
      - pg_data:/var/lib/postgresql/data
    restart: unless-stopped

  montly:
    build: .
    ports:
      - "127.0.0.1:8080:8080"
    depends_on: [db]
    environment:
      DB_TYPE: postgres
      DATABASE_URL: postgres://montly:dbpassword@db:5432/montly?sslmode=disable
      SESSION_SECRET: "replace-with-a-long-random-string"
      SECURE_COOKIES: "true"
    restart: unless-stopped

volumes:
  pg_data:
```

---

## Backups

### SQLite

The entire database is a single file: `<DATA_DIR>/montly.db`.

```bash
# Back up to a timestamped copy
docker run --rm -v montly_data:/data alpine \
  cp /data/montly.db /data/montly.db.$(date +%Y%m%d_%H%M%S)
```

For automated daily backups, add a cron job on the host:

```bash
0 3 * * * docker run --rm -v montly_data:/data alpine \
  cp /data/montly.db /backup/montly.db.$(date +\%Y\%m\%d)
```

### PostgreSQL

```bash
docker compose exec db pg_dump -U montly montly | gzip > montly_$(date +%Y%m%d).sql.gz
```

### Receipt files

Receipt uploads live in `<DATA_DIR>/receipts/`. Include this directory in your backup alongside the database.

---

## Multi-user setup

1. On first access, create the admin account through the UI registration form (or via `ADMIN_USERNAME` / `ADMIN_PASSWORD` env vars for automated deployments).
2. Log in as the admin, open **Settings → Users**, and create additional accounts.
3. Each user has isolated tasks, completions, and settings.
4. Admins can create and delete users; regular users can only manage their own data.

---

## API tokens (headless / mobile clients)

1. Log in to the web UI, open **Settings → API Tokens**, and click **Create token**.
2. Copy the `mt_` prefixed token immediately — it is shown only once.
3. Use it in API requests:

```http
GET /api/tasks?month=2026-04
Authorization: Bearer mt_<your-token>
```

All API endpoints are available under both `/api` and `/api/v1`. The `X-API-Version: 1` response header identifies the current version.

---

## Upgrading

```bash
git pull
docker compose up -d --build
```

All schema migrations are applied automatically on startup and are idempotent — safe to run multiple times.

---

## Security checklist

- [ ] Set a strong, stable `SESSION_SECRET`
- [ ] Set `SECURE_COOKIES=true` when serving over HTTPS
- [ ] Use HTTPS (Caddy or nginx + certbot)
- [ ] Bind the container to `127.0.0.1:8080`, not `0.0.0.0:8080`
- [ ] Pass real client IP via `X-Forwarded-For` and set `TRUST_PROXY_HEADERS=true` so the login rate limiter works correctly
- [ ] Rotate admin password after first login
- [ ] Keep the `DATA_DIR` volume backed up regularly
