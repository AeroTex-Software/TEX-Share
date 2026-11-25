# Installing TEX Share

This guide covers a production install with Docker (recommended) and a manual
install from source. Both produce the same single-process server: one public
listener on port `3535` and one admin listener on port `3636`, backed by SQLite
and the local filesystem.

> **Keep the admin port private.** The admin UI is the control plane for your
> whole instance. Never publish it to the open internet — bind it to localhost,
> a VPN, or an authenticating reverse proxy.

---

## 1. Requirements

### Docker install

- Docker Engine 24+ with the Compose v2 plugin.
- ~2 vCPU, 1 GB RAM and enough disk for your share files. ClamAV wants roughly
  1–2 GB RAM when enabled; disable it on very small hosts.
- Two free TCP ports (`3535` public, `3636` admin) or a reverse proxy in front.

### Manual install

- Go **1.24 or newer** (the code targets Go 1.24+).
- Node **20 or newer** to build the frontend.
- A Linux host is recommended. Windows and macOS work for development.

---

## 2. Docker install

```bash
git clone https://github.com/AeroTex-Software/TEX-Share.git
cd TEX-Share
cp .env.example .env
```

Edit `.env` (see [Configuration](#configuration) below), then build and start:

```bash
docker compose up -d --build
```

Check that it is healthy:

```bash
docker compose ps
docker compose logs -f texshare
curl -fsS http://127.0.0.1:3535/healthz
```

Both listeners are published by default:

- Public UI: `http://<host>:3535`
- Admin UI: `http://<host>:3636`

All state lives in `./data` on the host (mounted at `/data` in the container).
Back this directory up; it holds the database, files and the `secret.key` and
`backup.key` material.

### Binding the admin port to localhost only

To keep the admin UI off the network entirely, change the published port in
`docker-compose.yml`:

```yaml
ports:
  - "3535:3535"
  - "127.0.0.1:3636:3636"   # reachable only from the host
```

Then tunnel in over SSH when you need it:

```bash
ssh -L 3636:127.0.0.1:3636 user@your-server
```

---

## 3. Manual install

```bash
git clone https://github.com/AeroTex-Software/TEX-Share.git
cd TEX-Share

# Build the frontend (output is embedded by the Go binary).
cd web
npm ci
npm run build
cd ..

# Build the server.
go build -trimpath -ldflags "-s -w" -o texshare .
```

Run it with a data directory:

```bash
TEXSHARE_DATA_DIR=/var/lib/texshare \
TEXSHARE_PUBLIC_ADDR=0.0.0.0:3535 \
TEXSHARE_ADMIN_ADDR=127.0.0.1:3636 \
./texshare
```

On Windows the same steps apply; the binary is `texshare.exe` and environment
variables are set with `$env:NAME = "value"` in PowerShell.

> The binary embeds `web/dist`. Whenever you change the frontend you must run
> `npm run build` and then rebuild the Go binary so the new assets are embedded.

### Running as a systemd service

```ini
# /etc/systemd/system/texshare.service
[Unit]
Description=TEX Share
After=network-online.target
Wants=network-online.target

[Service]
User=texshare
Group=texshare
WorkingDirectory=/opt/texshare
EnvironmentFile=/etc/texshare.env
ExecStart=/opt/texshare/texshare
Restart=on-failure
RestartSec=3
# Harden the process; adjust paths to match your data directory.
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/texshare

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now texshare
```

---

## 4. First-run setup

The setup wizard is served **only on the admin port** and only until the first
administrator exists. After that it is permanently disabled.

1. Open `http://<host>:3636`.
2. Fill in the wizard:
   - **Username / display name** for the first administrator.
   - **Password** — must satisfy the password policy (minimum length plus at
     least three character classes). There are no default credentials.
   - **Application name** shown across the UI.
   - **Primary domain** — defaults to `localhost`. In production enter the real
     hostname users will visit, e.g. `share.example.com`.
3. Submit. You are signed in to the admin UI.

There is **no public registration**. Create every other account from
**Admin → Users**. New users get a temporary password and are forced to change it
at first login.

---

## 5. Configuration

Bootstrap and secret values are read from environment variables. Everything
else is configured in the admin UI and stored in the database.

| Variable | Default | Purpose |
| --- | --- | --- |
| `TEXSHARE_DATA_DIR` | `./data` (or `/data` in the image) | State directory |
| `TEXSHARE_PUBLIC_ADDR` | `0.0.0.0:3535` | Public bind address |
| `TEXSHARE_ADMIN_ADDR` | `0.0.0.0:3636` | Admin bind address |
| `TEXSHARE_PUBLIC_URL` | – | Canonical public URL for links/CSRF |
| `TEXSHARE_ADMIN_URL` | – | Canonical admin URL |
| `TEXSHARE_ALLOWED_HOSTS` | – | Extra accepted public hostnames (comma separated) |
| `TEXSHARE_ADMIN_ALLOWED_HOSTS` | – | Extra accepted admin hostnames |
| `TEXSHARE_TRUSTED_PROXIES` | – | Proxy IPs/CIDRs whose forwarded headers are trusted |
| `TEXSHARE_TRUST_PROXY` | `false` | Legacy: trust forwarded headers from loopback only |
| `TEXSHARE_FORCE_SECURE_COOKIES` | `false` | Always mark cookies `Secure` |
| `TEXSHARE_LOG_LEVEL` | `info` | `debug` \| `info` \| `warn` \| `error` |
| `TEXSHARE_ACCESS_LOG` | `true` | Emit access logs |
| `TEXSHARE_CLAMAV_ENABLED` | `false` | Turn antivirus scanning on |
| `TEXSHARE_CLAMAV_MODE` | `managed` | `managed` or `external` |
| `TEXSHARE_CLOUDFLARE_ENABLED` | `false` | Enable Cloudflare Tunnel integration |
| `TEXSHARE_BACKUP_KEY_FILE` | `<data>/backup.key` | Key used for encrypted automatic backups |
| `TEXSHARE_DEV` | `false` | Development mode — never enable in production |
| `TEXSHARE_VERSION` / `TEXSHARE_COMMIT` | build values | Reported by the version endpoint |

The complete list with comments is in [`.env.example`](../.env.example).

The most important runtime settings live in **Admin → Settings** and include
storage limits and quotas, session lifetimes, login/API rate limits and lockout,
upload/download/ZIP concurrency, the antivirus toggle, the backup schedule and
retention, the blocklists (`blocked_extensions`, `blocked_mime`), the
maintenance-mode switch, and the Cloudflare credentials.

---

## 6. Reverse proxy and TLS

TEX Share serves plain HTTP and expects TLS to be terminated upstream. With
Nginx:

```nginx
server {
    listen 443 ssl http2;
    server_name share.example.com;

    ssl_certificate     /etc/letsencrypt/live/share.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/share.example.com/privkey.pem;

    client_max_body_size 5g;   # match your max_share_size_bytes

    location / {
        proxy_pass http://127.0.0.1:3535;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_request_buffering off;      # stream large uploads
    }
}
```

When running behind a proxy:

- Set `TEXSHARE_PUBLIC_URL` (and `TEXSHARE_ADMIN_URL` if you expose the admin UI
  the same way) to the public URLs.
- List the proxy addresses/CIDRs in `TEXSHARE_TRUSTED_PROXIES` (or the
  **Trusted proxies** field under **Settings → Domains & Netzwerk**). Only those
  peers may set `X-Forwarded-For`, `X-Forwarded-Proto`, `X-Forwarded-Host` and
  `Forwarded`; headers from anyone else are ignored. The legacy
  `TEXSHARE_TRUST_PROXY=true` trusts forwarded headers from loopback only.
- Set `TEXSHARE_FORCE_SECURE_COOKIES=true` so cookies are always `Secure`.
- Forward the `Host` header unchanged and register every public hostname either
  in **Admin → Domains** or via `TEXSHARE_ALLOWED_HOSTS`.

Expose the admin listener on a **separate** hostname that is additionally
protected (VPN, mTLS, IP allowlist, or an authenticating proxy).

The admin wizard **Settings → Domains & Netzwerk** generates ready-to-use
configurations for Nginx, Caddy, Traefik, Nginx Proxy Manager and HAProxy, and
covers manual DNS, DynDNS, home servers and CGNAT/DS-Lite. The complete guide is
in [`NETWORK.md`](NETWORK.md).

---

## 7. Optional features

### Antivirus (ClamAV)

The bundled image includes `clamd`, `freshclam` and `clamdscan`. Enable scanning
in **Admin → Settings → Antivirus** or with `TEXSHARE_CLAMAV_ENABLED=true`.

- **managed** (default): TEX Share runs `clamd`, refreshes signatures with
  `freshclam`, and scans with `clamdscan`.
- **external**: point it at an existing `clamdscan`.

Uploads are held as *pending* while scanning is enabled but the scanner is
unavailable, then scanned automatically. Infected files are quarantined and
never served.

### Cloudflare Tunnel

Admins can publish shares on a public hostname without opening inbound ports.
In **Admin → Settings → Cloudflare**, click *Connect with Cloudflare*. TEX Share
runs `cloudflared tunnel login` and shows the Cloudflare dashboard link, where
you authorise the domain in the browser — no API token is needed. Enter the
public hostname and provision the tunnel; TEX Share creates the tunnel, DNS
record and ingress rules and supervises `cloudflared`. The account certificate
and credentials are kept under `<data>/cloudflare/`. This is optional and
requires `cloudflared` (bundled in the image).

---

## 8. Verify the install

```bash
curl -fsS http://127.0.0.1:3535/healthz   # {"ok":true,...}
curl -fsS http://127.0.0.1:3535/readyz    # database reachable
```

Then sign in to the admin UI, create a user, and have that user create a share
from the public UI. Follow [BACKUP.md](BACKUP.md) to take a first backup before
you rely on the instance.

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| `configuration error: public and admin addresses must differ` | Both listeners set to the same address/port |
| `403 origin_rejected` / `csrf_failed` | `TEXSHARE_PUBLIC_URL`/`ADMIN_URL` don't match the browser origin |
| `404`/`403` on every hostname | The hostname is not a registered domain and not in `TEXSHARE_ALLOWED_HOSTS` |
| Uploads rejected as too large | `max_file_size_bytes` / `max_share_size_bytes` or the proxy `client_max_body_size` |
| Shares stuck pending AV | ClamAV enabled but the scanner has not finished downloading signatures yet |
| Client IPs all show the proxy | Set `TEXSHARE_TRUST_PROXY=true` behind a trusted proxy |

Logs are JSON on stdout; the log level follows `TEXSHARE_LOG_LEVEL`.
