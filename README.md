# TEX Share

**Share securely. Self-hosted.**

TEX Share is a single-binary, self-hosted file sharing platform. It runs one
container, serves a public UI and a separate admin UI on two independent ports,
and stores everything locally in SQLite plus the filesystem — no external
database or object store required.

It supports password-protected links, expiry dates, download limits, a full
trash/restore workflow, per-user storage quotas, antivirus scanning, encrypted
backups, optional Cloudflare Tunnel provisioning, and **true end-to-end
encryption** where the server never sees your keys or your data.

> Every page carries the mandatory **Powered by TEX Share** footer linking to
> <https://github.com/AeroTex-Software/TEX-Share>.

---

## Table of contents

- [Highlights](#highlights)
- [Architecture](#architecture)
- [Quick start (Docker)](#quick-start-docker)
- [Manual build](#manual-build)
- [First-run setup](#first-run-setup)
- [Configuration](#configuration)
- [End-to-end encryption](#end-to-end-encryption)
- [Sharing model](#sharing-model)
- [Backups](#backups)
- [Antivirus](#antivirus)
- [Cloudflare Tunnel](#cloudflare-tunnel)
- [Reverse proxy and TLS](#reverse-proxy-and-tls)
- [Security](#security)
- [Development](#development)
- [Documentation](#documentation)

---

## Highlights

- **One container.** Go backend with the Svelte/TypeScript frontend embedded as
  static assets. Nothing else to deploy.
- **Two ports, two realms.** Public UI on `3535`, admin UI on `3636`. Separate
  session cookies (`texshare_pub` / `texshare_adm`) and independent auth; normal
  users can never reach admin endpoints.
- **Setup wizard.** Runs only on the admin port and permanently disables itself
  after the first administrator is created. No default passwords, no public
  registration.
- **Users and roles.** Admins create accounts with temporary passwords, force a
  password change on first login, suspend/reactivate accounts (shares are
  preserved), and delete accounts through a wizard that transfers or trashes
  their shares.
- **Strong authentication.** Argon2id password hashing, CSRF protection, rate
  limiting and lockout, TOTP two-factor authentication with QR codes and
  one-time recovery codes, and per-session management/revocation.
- **Rich share types.** Files, multi-file folders (server-side ZIP or
  client-side E2EE ZIP), links, text snippets and media galleries. Optional
  password, E2EE, expiry, download limit, pause, lock and trash.
- **Quotas and integrity.** Atomic quota reservation on upload, resumable
  chunked uploads, blocklists by extension/MIME/magic bytes, safe download
  headers, and streaming transfers for low RAM usage.
- **Real end-to-end encryption.** AES-256-GCM with Argon2id key derivation, the
  key carried in the URL fragment, an optional password mode and a recovery
  key. The server only ever stores ciphertext.
- **Operations built in.** Tamper-evident audit log, encrypted manual and
  scheduled backups with one-click restore, live monitoring, in-app
  notifications, audit export, maintenance mode, DE/EN i18n and light/dark
  themes.

## Architecture

```
                          ┌───────────────────────────────────────┐
   public :3535  ───────▶  │  public server                        │
   (share links)          │   • SPA (public bundle)               │
                          │   • /api/public/*                     │
                          │   • share download / ZIP / E2EE blobs │
                          └───────────────┬───────────────────────┘
                                          │  shared core
   admin  :3636  ───────▶  ┌──────────────▼───────────────────────┐
   (private!)             │  admin server                          │
                          │   • SPA (admin bundle)                 │
                          │   • /api/admin/*                       │
                          └───────────────┬───────────────────────┘
                                          │
                ┌─────────────────────────▼──────────────────────────┐
                │  SQLite (WAL)  ·  local file storage  ·  secret.key │
                │  ClamAV  ·  Cloudflare Tunnel  ·  backup engine     │
                └─────────────────────────────────────────────────────┘
```

Everything lives in one data directory (`/data` in the container):

```
data/
├── db/texshare.db        SQLite database (WAL mode)
├── files/                share blobs
├── uploads/              in-progress uploads
├── quarantine/           antivirus quarantine
├── backups/              generated backups
├── branding/             logo and favicon
├── clamav/               managed ClamAV config and signature database
├── tmp/                  scratch space
├── secret.key            master secret (HKDF root for server-side secrets)
└── backup.key            automatic-backup encryption key
```

> `secret.key` and `backup.key` are the crown jewels. Back them up securely and
> never commit them. Without `secret.key` you cannot decrypt stored server-side
> secrets; without `backup.key` you cannot restore encrypted automatic backups.

## Quick start (Docker)

Requirements: Docker with the Compose plugin.

```bash
git clone https://github.com/AeroTex-Software/TEX-Share.git
cd TEX-Share
cp .env.example .env      # edit as needed
docker compose up -d --build
```

Then:

1. Open the **admin UI** at `http://<host>:3636` and complete the setup wizard.
   It creates the first administrator and the initial domain.
2. Create users and additional domains from the admin UI.
3. Users sign in at the **public UI** on `http://<host>:3535`.

The public UI listens on `3535` and the admin UI on `3636`. **Keep the admin
port private** — bind it to localhost or a private network, or put it behind an
authenticating proxy/VPN.

## Manual build

Requirements: Go 1.24+ and Node 20+.

```bash
# 1. Build the frontend (produces web/dist, embedded by the Go binary).
cd web
npm ci
npm run build
cd ..

# 2. Build the server.
go build -trimpath -ldflags "-s -w" -o texshare .

# 3. Run it.
TEXSHARE_DATA_DIR=./data ./texshare
```

On Windows the same commands work with `texshare.exe`. The binary embeds
`web/dist`, so rebuild the frontend and the Go binary whenever the UI changes.

## First-run setup

The setup wizard is reachable **only on the admin port** and only until the
first administrator exists. It asks for:

- **Username / display name** for the first administrator.
- **Password** (must satisfy the password policy — at least three character
  classes and the configured minimum length).
- **Application name** shown across the UI.
- **Primary domain** (defaults to `localhost`; use your real hostname in
  production).

After completion the wizard is permanently disabled. Create additional users
from **Admin → Users**; each receives a temporary password and is forced to
change it on first login.

## Configuration

Bootstrap and secret values come from environment variables (see
[`.env.example`](.env.example)); everything else is managed from the admin UI
and stored in the database.

Frequently used variables:

| Variable | Default | Purpose |
| --- | --- | --- |
| `TEXSHARE_DATA_DIR` | `/data` (container) | State directory |
| `TEXSHARE_PUBLIC_ADDR` | `0.0.0.0:3535` | Public bind address |
| `TEXSHARE_ADMIN_ADDR` | `0.0.0.0:3636` | Admin bind address |
| `TEXSHARE_PUBLIC_URL` | – | Canonical public URL (CSRF/link generation) |
| `TEXSHARE_ADMIN_URL` | – | Canonical admin URL |
| `TEXSHARE_ALLOWED_HOSTS` | – | Extra accepted public hostnames |
| `TEXSHARE_TRUST_PROXY` | `false` | Trust forwarded client-IP headers |
| `TEXSHARE_FORCE_SECURE_COOKIES` | `false` | Force `Secure` cookies behind TLS |
| `TEXSHARE_CLAMAV_ENABLED` | `false` | Turn antivirus on |
| `TEXSHARE_CLOUDFLARE_ENABLED` | `false` | Enable tunnel integration |
| `TEXSHARE_DEV` | `false` | Development mode — never in production |

Runtime settings (storage limits, session lifetimes, rate limits, blocklists,
backup schedule, maintenance mode, branding, Cloudflare credentials, and more)
are edited in the admin UI under **Settings**.

## End-to-end encryption

When you enable E2EE for a share, encryption happens **entirely in the browser**:

- A random 256-bit **master key** is generated in the browser and never sent to
  the server. It travels in the URL fragment (`#key=…`), which browsers do not
  transmit.
- Each file is encrypted with **AES-256-GCM** using a per-file key derived from
  the master key via **HKDF** and a random salt. Every 1 MiB chunk uses a fresh
  nonce, and a final zero-length record authenticates the end of the stream.
- The **manifest** (title, description, text, link URL and file names) is also
  encrypted, so the server stores only opaque metadata.
- In **password mode**, the master key is wrapped with a key derived from your
  password using **Argon2id** (64 MiB, 3 iterations). A separate **recovery
  key** wraps the same master key so access can be regained if the password is
  lost. The recovery key is shown once and is never stored anywhere.
- Encrypted ZIP archives are assembled client-side; the server only ever serves
  ciphertext blobs.

Because the server cannot read encrypted content, E2EE shares cannot be indexed,
previewed, or scanned by antivirus, and their metadata cannot be edited
server-side. Rotate the key (or token) from the share detail page if needed.

## Sharing model

A share link is:

```
https://<domain>/<namespace?>/<slug>/<token>
```

- **Domain** — one of the hostnames configured in the admin UI.
- **Namespace** (optional) — an admin-assigned per-user prefix.
- **Slug** — a human-readable name.
- **Token** — 43 characters of URL-safe base64. Only the **hash** of the token
  is stored, so a database leak does not reveal working links. Tokens can be
  rotated at any time.

Shares support an optional password (stored as an Argon2id hash), an expiry
date, a download limit (reserved atomically so concurrent downloads cannot
overshoot it), pause, admin lock, and a trash with configurable retention before
permanent purge.

## Backups

TEX Share creates portable backups as `tar.gz` archives containing a consistent
SQLite snapshot (`VACUUM INTO`), the `secret.key`, share files, branding and a
manifest.

- **Manual** backups: **Admin → Backups → Create**.
- **Scheduled** backups: choose daily/weekly and an hour in **Settings**.
- **Encryption**: automatic backups are encrypted by default with a key derived
  from `backup.key` (AES-256-GCM, 1 MiB chunks). Store `backup.key` separately
  from the backups themselves.
- **Restore**: upload an archive or pick an existing one. TEX Share validates it
  (including that it contains at least one administrator), creates a
  pre-restore safety backup, then swaps the data in under an exclusive lock and
  reloads. Other requests receive `503` while a restore is in progress.

See [`docs/BACKUP.md`](docs/BACKUP.md) for the full procedure.

## Antivirus

Enable ClamAV in **Settings → Antivirus** (or via `TEXSHARE_CLAMAV_ENABLED`).
The bundled image includes `clamd`, `freshclam` and `clamdscan`.

- **Managed mode** (default): TEX Share runs `clamd` itself, keeps signatures
  fresh with `freshclam`, and scans uploads with `clamdscan`.
- **External mode** (`TEXSHARE_CLAMAV_MODE=external`): point it at an existing
  `clamdscan`.
- Uploads are held as **pending** while the scanner is enabled but unavailable,
  and scanned automatically once it comes online. Infected files are moved to
  quarantine and never served.

## Cloudflare Tunnel

Admins can expose shares on a public hostname without opening inbound ports.
Configure an API token, account/zone IDs and a tunnel name in
**Settings → Cloudflare**, then provision a hostname. TEX Share creates the
tunnel, the DNS record and the ingress rules, and supervises the `cloudflared`
process. This integration is entirely optional.

## Reverse proxy and TLS

TEX Share speaks plain HTTP and expects TLS to be terminated upstream. When
running behind a proxy:

- Set `TEXSHARE_PUBLIC_URL` and `TEXSHARE_ADMIN_URL` to the public URLs.
- Set `TEXSHARE_TRUST_PROXY=true` **only** if the proxy is trusted; otherwise
  clients can spoof their IP and bypass rate limiting.
- Set `TEXSHARE_FORCE_SECURE_COOKIES=true` so cookies are always `Secure`.
- Forward the `Host` header unchanged and add every public hostname to
  `TEXSHARE_ALLOWED_HOSTS` (or register it as a domain in the admin UI).

## Security

TEX Share is built security-first. See [`docs/SECURITY.md`](docs/SECURITY.md)
for the threat model and hardening checklist. Highlights:

- Argon2id password hashing; constant-time comparisons for tokens and CSRF.
- Per-realm sessions with CSRF tokens, origin checks, rate limiting, lockout,
  and suspension-aware login.
- Strict separation of the public and admin servers; admin endpoints reject
  non-admin sessions.
- Host validation against configured domains, SSRF-safe domain checks, path
  traversal prevention, safe `Content-Disposition` headers, and an inline
  allowlist that excludes SVG/HTML.
- A tamper-evident audit log (HMAC hash chain) that can be verified and exported
  from the admin UI.
- Server-side secrets sealed with keys derived from `secret.key`; automatic
  backup keys kept out of the database.
- Antivirus quarantine, upload blocklists (extension/MIME/magic bytes) and
  atomic quota enforcement.

## Development

```bash
# Backend tests (integration + unit).
go test ./...

# Frontend unit tests (E2EE crypto, containers, key wrapping).
cd web && npm test

# Type-check the Svelte apps.
cd web && npm run check

# Live frontend dev server (proxying the API is not required for the API tests).
cd web && npm run dev
```

The Go integration tests in `internal/app/integration_test.go` exercise the
setup wizard, realm separation, CSRF enforcement, the share lifecycle, password
unlock, uploads/downloads with limits, trash/restore and audit-chain
verification against the real HTTP handlers.

## Documentation

- [`docs/INSTALL.md`](docs/INSTALL.md) — install and first-run
- [`docs/UPDATE.md`](docs/UPDATE.md) — updating safely
- [`docs/BACKUP.md`](docs/BACKUP.md) — backup and restore
- [`docs/SECURITY.md`](docs/SECURITY.md) — security model and hardening

---

TEX Share is developed by **AeroTex Software** —
<https://github.com/AeroTex-Software/TEX-Share>.
