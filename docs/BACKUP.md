# Backup and restore

TEX Share keeps everything in one data directory: a SQLite database, the stored
share files, branding assets and the `secret.key` / `backup.key` material.
Backups capture all of it so an instance can be rebuilt exactly.

There are two backup mechanisms:

- **In-app backups** (recommended) — created from the admin UI or on a schedule,
  portable, optionally encrypted, and restorable with one click.
- **Filesystem snapshots** — stop the service and copy the whole data directory.
  Simple but requires downtime.

---

## 1. What an in-app backup contains

A backup is a `tar.gz` archive with these entries:

```
db/texshare.db     consistent SQLite snapshot (VACUUM INTO)
secret.key         master secret (keeps TOTP + integration secrets working)
manifest.json      format/version/createdAt/encrypted metadata
files/…            all stored share blobs
branding/…         logo and favicon
```

The archive file itself is named like
`texshare-manual-20260101-030000.tar.gz` (or `.tar.gz.enc` when encrypted).

### Encryption format

When encryption is enabled the archive is wrapped in a small container:

```
"TEXSHARE-BACKUP\n"   12-byte magic
<version>             1 byte, currently 1
<encrypted flag>      1 byte (1 = encrypted)
<salt>                16 bytes
<ciphertext>          1 MiB AES-256-GCM chunks, each with its own nonce
```

The 32-byte encryption key is derived with **HKDF-SHA256** (info
`texshare/backup/v1`) from the backup key and the random salt. The backup key is
`<data>/backup.key` unless `TEXSHARE_BACKUP_KEY_FILE` points elsewhere, and it is
created automatically on first use.

> **The backup key is not stored inside the backup.** Without `backup.key` (or
> the file named by `TEXSHARE_BACKUP_KEY_FILE`) an encrypted backup cannot be
> restored. Store it separately from the backups — ideally in a password
> manager or a different machine.

---

## 2. Creating backups

### Manual (admin UI)

**Admin → Backups → Create**. Encryption is on by default. The new backup
appears in the list with its size, kind, and status.

### Scheduled

In **Admin → Settings → Backups** set:

| Setting | Meaning |
| --- | --- |
| `backup_schedule` | `off`, `daily`, or `weekly` |
| `backup_hour` | Hour of day (0–23) to run |
| `backup_retention_count` | How many scheduled backups to keep |
| `backup_encrypt_auto` | Encrypt automatic backups |
| `backup_path` | Where backups are written (default `<data>/backups`) |

Scheduled backups are pruned to the retention count automatically. Pruning can
also be triggered manually from the Backups page.

### Filesystem snapshot

```bash
sudo systemctl stop texshare
sudo tar -C /var/lib -czf texshare-$(date +%F).tar.gz texshare
sudo systemctl start texshare
```

Or with Docker:

```bash
docker compose stop texshare
tar -czf texshare-$(date +%F).tar.gz -C . data
docker compose start texshare
```

> Copying the SQLite file while the server is running can capture an
> inconsistent state. Prefer the in-app backup (it uses `VACUUM INTO`) or stop
> the service first.

---

## 3. Restoring

### From the admin UI (recommended)

1. Go to **Admin → Backups**.
2. Either pick an existing backup and choose **Restore**, or use **Upload** to
   provide an archive from another instance.
3. Confirm. TEX Share:
   - validates the archive (magic, version, database integrity, and that it
     contains at least one administrator),
   - creates a **pre-restore** safety backup of the current state,
   - swaps the data in under an exclusive lock and reloads the database, secrets
     and settings.
4. During the swap, other requests receive `503 restore_in_progress`. You may
   need to reload the admin UI when it finishes.

The response is `{"ok":true,"restarted":true}`.

### Restore expectations

- The restored database must contain **at least one admin account**, otherwise
  the restore is refused.
- The archive's `secret.key` replaces the current one, so TOTP secrets and
  stored integration credentials keep working.
- Encrypted archives require the matching `backup.key` to be present.
- Uploads are limited to 1 TiB per archive and staged under `<data>/tmp`.

### Manual restore (filesystem)

```bash
sudo systemctl stop texshare
sudo mv /var/lib/texshare /var/lib/texshare.broken
sudo tar -C /var/lib -xzf texshare-YYYY-MM-DD.tar.gz
sudo chown -R texshare:texshare /var/lib/texshare
sudo systemctl start texshare
```

---

## 4. Off-site and verification

- **Copy backups off the host.** A backup on the same disk does not survive a
  disk failure. Sync `<data>/backups` (or your `backup_path`) to remote storage.
- **Keep the keys separate.** `secret.key` and `backup.key` should not live only
  alongside the backups they protect.
- **Test restores.** Periodically restore a backup into a throwaway instance to
  confirm it is usable.
- **Verify the audit chain.** After a restore, run
  **Admin → Audit → Verify** to confirm the tamper-evident log is intact.

---

## 5. Disaster-recovery checklist

| Loss | Recovery |
| --- | --- |
| Database corrupted | Restore the latest in-app backup |
| Share files deleted | Restore a backup that includes `files/` |
| `secret.key` lost | Stored server-side secrets (e.g. Cloudflare token) and existing TOTP enrolments become unusable; users must re-enrol 2FA |
| `backup.key` lost | Encrypted backups cannot be decrypted — unencrypted backups are unaffected |
| Whole host lost | Restore `data/` (or a backup archive) onto a fresh host, then start TEX Share |
| Admin password lost | With another admin, reset it in **Admin → Users**; with no admin, restore a backup that contains one |
