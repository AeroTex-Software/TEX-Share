# Security model and hardening

This document describes how TEX Share protects an instance and how to operate it
safely. It is written for administrators who deploy the software.

---

## 1. Threat model

TEX Share assumes:

- The **host and data directory are trusted**. Anyone with filesystem access to
  `secret.key`, the SQLite database or the running process can read all
  server-side secrets. Full-disk encryption and OS access control are your
  responsibility.
- The **admin port is not exposed to the public internet**. It is the control
  plane; treat it like SSH.
- **TLS is terminated upstream** (reverse proxy, load balancer or Cloudflare).
  TEX Share speaks plain HTTP.
- **E2EE protects content from the server operator too.** For end-to-end
  encrypted shares, the server only ever sees ciphertext and cannot recover the
  plaintext, even with full database access.

Explicitly out of scope: a compromised host, a malicious administrator (admins
can see all non-E2EE data), and side channels outside the application.

---

## 2. Authentication

- **Password hashing** uses **Argon2id** (PHC format, per-password random salt,
  3 iterations, 32 MiB, 2 lanes, 32-byte output). Verification is constant-time.
- **No default credentials and no public registration.** The first admin is
  created by the setup wizard, which disables itself permanently afterwards.
- **Temporary passwords** issued by admins force a password change on first
  login; most endpoints reject the session until the change is done.
- **Password policy** enforces a configurable minimum length (≥8) plus at least
  three character classes.
- **Two-factor authentication** (TOTP) is available per user, with QR enrolment
  and one-time recovery codes. Recovery codes are stored only as HMACs derived
  from `secret.key`; codes are single-use.
- **Suspension-aware login**: suspended accounts are refused and their sessions
  invalidated, while their shares are preserved.

### Sessions

- Separate cookies per realm: `texshare_pub` (public) and `texshare_adm`
  (admin). A session is bound to its realm and cannot be used on the other
  listener.
- Cookies are `HttpOnly`, `SameSite=Lax`, `Path=/`, and `Secure` whenever TLS is
  detected or `TEXSHARE_FORCE_SECURE_COOKIES=true`.
- Session records live in the database, so they can be listed and revoked
  individually. Idle and absolute lifetimes are configurable
  (`max_session_hours`, `remember_me_days`).
- A separate per-share access cookie (`texshare_share_<id>`) records that a
  visitor unlocked a password-protected share, scoped to that share.

### CSRF and origin checks

- Every state-changing authenticated request must carry the session's CSRF token
  in the `X-CSRF-Token` header; comparison is constant-time.
- Unauthenticated POSTs (login, setup, unlock, view) are checked against the
  request `Origin`/`Host` so cross-site requests are refused.
- Responses set a strict `Content-Security-Policy` (see below), which blocks
  script injection even if a template ever escaped user input.

---

## 3. Realm separation

The public and admin listeners are independent HTTP servers with independent
routers, sessions and static bundles.

- Admin endpoints require an admin-role session; non-admin sessions receive
  `403`.
- An admin session presented to the public API (or vice versa) is rejected with
  `401 unauthorized` because cookies are realm-bound.
- The public bundle never contains admin routes, and the admin bundle is served
  only on the admin port.

---

## 4. Request handling

Applied to every request by the common middleware:

- **Host validation.** The `Host` header must match a registered domain, a
  configured host (`TEXSHARE_ALLOWED_HOSTS` / `TEXSHARE_ADMIN_ALLOWED_HOSTS`),
  or localhost/an IP. A share's host must match the domain it was created on.
  Mismatches return `421 Misdirected Request`.
- **Security headers**: `X-Content-Type-Options: nosniff`,
  `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer`, a restrictive
  `Permissions-Policy`, `X-Robots-Tag: noindex`, `Cross-Origin-Opener-Policy:
  same-origin`, and HSTS when TLS is detected.
- **Content-Security-Policy**: `default-src 'self'`, scripts limited to self
  (plus `wasm-unsafe-eval` for the E2EE WASM), images/media limited to self,
  data and blob URLs, `object-src 'none'`, `frame-ancestors 'none'`.
- **Rate limiting** on login, setup and the API, plus **account lockout** after a
  configurable number of failed logins (`login_rate_per_minute`,
  `login_failures_before_lock`, `lockout_minutes`).
- **Panic recovery** so a handler fault cannot crash the server, and a bounded
  request body (1 MiB JSON limit) for API payloads.
- **Restore lock**: while a backup restore is in progress every other request
  gets `503 restore_in_progress`, preventing reads against a half-swapped data
  directory.

---

## 5. Data protection

- **SSRF-safe domain handling.** When TEX Share resolves or verifies a hostname
  it rejects private, loopback and link-local addresses.
- **Path-traversal prevention.** Storage paths are server-generated; the storage
  layer refuses absolute paths and any `..` component and re-checks that the
  resolved path stays under the storage root. User input never becomes a path.
- **Safe download headers.** Filenames are sanitised and emitted with both a
  quoted `filename` and an RFC 5987 `filename*=UTF-8''…` form, preventing header
  injection.
- **Inline allowlist.** Only safe types (images except SVG, video, audio, PDF,
  plain text) can be rendered inline; everything else is forced to
  `Content-Disposition: attachment`. SVG and HTML are never inlined.
- **Upload blocklists.** Extensions, declared MIME types and magic bytes are
  checked; executables, scripts and archives that look dangerous are rejected.
  `nosniff` is always set.
- **Atomic quotas.** Storage is reserved transactionally at upload start so
  concurrent uploads cannot overshoot a user's or the instance's limit, and the
  reservation is released if an upload is cancelled or fails.
- **Atomic download limits.** A download slot is reserved at the start of a
  transfer and released if the transfer aborts, so parallel downloads cannot
  exceed the configured limit. Range requests are not counted.
- **Antivirus quarantine.** When ClamAV is enabled, files are held pending until
  scanned; infected files are moved to quarantine and never served. E2EE files
  are marked encrypted/unscanned because the server cannot read them.

### End-to-end encryption

- The **master key is generated in the browser** and carried in the URL fragment
  (`#key=…`), which browsers never send to the server.
- Content is encrypted with **AES-256-GCM** using per-file keys derived via HKDF
  and per-chunk random nonces; the manifest (including titles and filenames) is
  encrypted too.
- In password mode the master key is wrapped with an Argon2id-derived key, plus a
  separate **recovery key** shown once and never stored. The server stores only
  opaque wrapped keys and ciphertext.
- Because the server cannot decrypt, E2EE content cannot be previewed, indexed or
  virus-scanned. This is a deliberate trade-off.

---

## 6. Secrets and keys

| Secret | Location | Purpose |
| --- | --- | --- |
| `secret.key` | `<data>/secret.key` | Root for HKDF-derived subkeys (audit HMAC, recovery-code hashes, sealing integration secrets) |
| `backup.key` | `<data>/backup.key` (or `TEXSHARE_BACKUP_KEY_FILE`) | Encrypts automatic backups |
| Cloudflare certificate | `<data>/cloudflare/home/.cloudflared/cert.pem` | Cloudflare account authorisation for the tunnel |

- `secret.key` and `backup.key` are **never written to the database or logs**.
- Integration secrets (e.g. TOTP seeds) are sealed with AES-GCM under a key
  derived from `secret.key`, and are stripped from API responses.
- The Cloudflare tunnel is authorised with a browser login
  (`cloudflared tunnel login`) instead of an API token; the resulting account
  certificate and tunnel credentials never leave the data directory and are not
  exposed through the API.
- Losing `secret.key` invalidates stored integration secrets and existing TOTP
  enrolments; losing `backup.key` makes encrypted backups unrecoverable.

### Audit log

TEX Share records administrative and security-relevant actions in an append-only
audit log where each entry is chained with an HMAC derived from `secret.key`.
**Admin → Audit → Verify** recomputes the chain and reports any tampering; the
log can be exported as CSV.

> The chain is **tamper-evident, not immutable**. An attacker with both the
> database and `secret.key` could recompute it. Off-site copies of the audit
> export are the strongest protection.

---

## 7. Hardening checklist

- [ ] Keep the **admin port off the public internet** (localhost, VPN, or an
      authenticating proxy).
- [ ] Terminate **TLS** upstream; set `TEXSHARE_FORCE_SECURE_COOKIES=true`.
- [ ] Set `TEXSHARE_PUBLIC_URL` / `TEXSHARE_ADMIN_URL` to the real origins.
- [ ] Enable `TEXSHARE_TRUST_PROXY` **only** behind a trusted proxy.
- [ ] Restrict the data directory to the service user (`0700`/`0750`).
- [ ] Keep `secret.key` and `backup.key` backed up, separate from the data disk.
- [ ] Turn on **antivirus** if you host untrusted uploads.
- [ ] Enable **2FA** for administrator accounts.
- [ ] Review **rate limits**, lockout and session lifetimes for your traffic.
- [ ] Keep **off-site backups** and test a restore periodically.
- [ ] Watch **Admin → Monitoring** for error spikes and disk pressure.
- [ ] Use **maintenance mode** during risky operations.
- [ ] Keep the host, Docker and this software up to date
      (see [UPDATE.md](UPDATE.md)).

---

## 8. Reporting a vulnerability

Please report security issues privately through the project's GitHub page
(<https://github.com/AeroTex-Software/TEX-Share>) rather than a public issue, and
include steps to reproduce. We aim to acknowledge reports promptly and will
credit responsible disclosure unless you prefer otherwise.
