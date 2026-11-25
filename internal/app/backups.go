package app

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AeroTex-Software/TEX-Share/internal/db"
	"github.com/AeroTex-Software/TEX-Share/internal/secrets"
	"github.com/AeroTex-Software/TEX-Share/internal/settings"
	"github.com/AeroTex-Software/TEX-Share/internal/util"
)

const (
	backupMagic   = "TEXSHARE-BACKUP\n"
	backupVersion = 1
	backupChunk   = 1 << 20
)

type backupRecord struct {
	ID        string `json:"id"`
	Filename  string `json:"filename"`
	SizeBytes int64  `json:"sizeBytes"`
	Kind      string `json:"kind"`
	Encrypted bool   `json:"encrypted"`
	CreatedAt int64  `json:"createdAt"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
}

func (a *App) backupDir() string {
	dir := strings.TrimSpace(a.settings.Get("backup_path"))
	if dir == "" {
		return filepath.Join(a.cfg.DataDir, "backups")
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(a.cfg.DataDir, dir)
	}
	return dir
}

func (a *App) backupKey() ([]byte, error) {
	return a.secrets.BackupKey(a.cfg.BackupKeyFile)
}

// createBackup writes a consistent database snapshot plus stored files. The
// archive always contains secret.key so restored installations keep TOTP and
// integration secrets working; the archive itself is encrypted with the
// separate backup key.
func (a *App) createBackup(kind string, encrypt bool) (*backupRecord, error) {
	a.backupMu.Lock()
	defer a.backupMu.Unlock()

	now := time.Now().UTC()
	id := util.RandomHex(8)
	dir := a.backupDir()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	ext := ".tar.gz"
	if encrypt {
		ext += ".enc"
	}
	name := fmt.Sprintf("texshare-%s-%s%s", kind, now.Format("20060102-150405"), ext)
	final := filepath.Join(dir, name)

	tmpDir := filepath.Join(a.cfg.DataDir, "tmp", "backup-"+id)
	if err := os.MkdirAll(tmpDir, 0o750); err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	dbSnapshot := filepath.Join(tmpDir, "texshare.db")
	if _, err := a.db.Exec(`VACUUM INTO ?`, dbSnapshot); err != nil {
		a.recordBackup(id, name, 0, kind, encrypt, "failed", err.Error())
		return nil, fmt.Errorf("database snapshot: %w", err)
	}
	manifest := map[string]any{
		"format":    backupVersion,
		"app":       "TEX Share",
		"version":   a.cfg.Version,
		"commit":    a.cfg.Commit,
		"createdAt": now.Unix(),
		"encrypted": encrypt,
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "manifest.json"), []byte(util.JSON(manifest)), 0o640); err != nil {
		return nil, err
	}
	entries := []backupEntry{
		{path: dbSnapshot, name: "db/texshare.db"},
		{path: filepath.Join(a.cfg.DataDir, "secret.key"), name: "secret.key"},
		{path: filepath.Join(tmpDir, "manifest.json"), name: "manifest.json"},
	}
	entries = append(entries, walkBackupEntries(filepath.Join(a.cfg.DataDir, "files"), "files")...)
	entries = append(entries, walkBackupEntries(filepath.Join(a.cfg.DataDir, "branding"), "branding")...)

	tarPath := filepath.Join(tmpDir, "backup.tar.gz")
	if err := writeTarGz(tarPath, entries); err != nil {
		a.recordBackup(id, name, 0, kind, encrypt, "failed", err.Error())
		return nil, err
	}
	if err := a.writeBackupFile(tarPath, final, encrypt); err != nil {
		os.Remove(final)
		a.recordBackup(id, name, 0, kind, encrypt, "failed", err.Error())
		return nil, err
	}
	fi, err := os.Stat(final)
	if err != nil {
		return nil, err
	}
	if err := a.recordBackup(id, name, fi.Size(), kind, encrypt, "complete", ""); err != nil {
		return nil, err
	}
	a.audit(0, "system", "backup.create", "backup", id, "success", map[string]any{"kind": kind, "encrypted": encrypt, "bytes": fi.Size()})
	return &backupRecord{ID: id, Filename: name, SizeBytes: fi.Size(), Kind: kind, Encrypted: encrypt, CreatedAt: util.Now(), Status: "complete"}, nil
}

type backupEntry struct {
	path string
	name string
}

func walkBackupEntries(root, prefix string) []backupEntry {
	out := []backupEntry{}
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		out = append(out, backupEntry{path: path, name: prefix + "/" + filepath.ToSlash(rel)})
		return nil
	})
	return out
}

func writeTarGz(dest string, entries []backupEntry) error {
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		fi, err := os.Stat(e.path)
		if err != nil {
			continue
		}
		hdr := &tar.Header{Name: e.name, Mode: 0o640, Size: fi.Size(), ModTime: fi.ModTime()}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		src, err := os.Open(e.path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tw, src)
		src.Close()
		if copyErr != nil {
			return copyErr
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	return f.Sync()
}

func (a *App) writeBackupFile(tarPath, dest string, encrypt bool) error {
	src, err := os.Open(tarPath)
	if err != nil {
		return err
	}
	defer src.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	defer out.Close()
	bw := bufio.NewWriterSize(out, 256<<10)
	if _, err := bw.WriteString(backupMagic); err != nil {
		return err
	}
	if err := bw.WriteByte(backupVersion); err != nil {
		return err
	}
	flag := byte(0)
	if encrypt {
		flag = 1
	}
	if err := bw.WriteByte(flag); err != nil {
		return err
	}
	if !encrypt {
		if _, err := io.Copy(bw, src); err != nil {
			return err
		}
		if err := bw.Flush(); err != nil {
			return err
		}
		return out.Sync()
	}
	key, err := a.backupKey()
	if err != nil {
		return err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	if _, err := bw.Write(salt); err != nil {
		return err
	}
	derived, err := hkdf.Key(sha256.New, key, salt, "texshare/backup/v1", 32)
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(derived)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	buf := make([]byte, backupChunk)
	var lenBuf [4]byte
	for {
		n, readErr := io.ReadFull(src, buf)
		if readErr != nil && readErr != io.ErrUnexpectedEOF && readErr != io.EOF {
			return readErr
		}
		if n > 0 {
			nonce := make([]byte, gcm.NonceSize())
			if _, err := rand.Read(nonce); err != nil {
				return err
			}
			sealed := gcm.Seal(nil, nonce, buf[:n], []byte("texshare-backup-v1"))
			binary.BigEndian.PutUint32(lenBuf[:], uint32(n))
			if _, err := bw.Write(lenBuf[:]); err != nil {
				return err
			}
			if _, err := bw.Write(nonce); err != nil {
				return err
			}
			if _, err := bw.Write(sealed); err != nil {
				return err
			}
		}
		if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
			break
		}
	}
	binary.BigEndian.PutUint32(lenBuf[:], 0)
	if _, err := bw.Write(lenBuf[:]); err != nil {
		return err
	}
	if err := bw.Flush(); err != nil {
		return err
	}
	return out.Sync()
}

func (a *App) recordBackup(id, filename string, size int64, kind string, encrypted bool, status, errMsg string) error {
	_, err := a.db.Exec(`INSERT INTO backups(id, filename, size_bytes, kind, encrypted, created_at, status, error)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?)`, id, filename, size, kind, boolInt(encrypted), util.Now(), status, errMsg)
	return err
}

func (a *App) listBackups() ([]backupRecord, error) {
	rows, err := a.db.Query(`SELECT id, filename, size_bytes, kind, encrypted, created_at, status, COALESCE(error,'')
		FROM backups ORDER BY created_at DESC LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []backupRecord{}
	for rows.Next() {
		var b backupRecord
		var enc int
		if rows.Scan(&b.ID, &b.Filename, &b.SizeBytes, &b.Kind, &enc, &b.CreatedAt, &b.Status, &b.Error) == nil {
			b.Encrypted = enc == 1
			out = append(out, b)
		}
	}
	return out, rows.Err()
}

func (a *App) backupPathFor(rec backupRecord) string {
	return filepath.Join(a.backupDir(), filepath.Base(rec.Filename))
}

func (a *App) deleteBackup(id string) error {
	var rec backupRecord
	var enc int
	err := a.db.QueryRow(`SELECT id, filename, size_bytes, kind, encrypted, created_at, status, COALESCE(error,'') FROM backups WHERE id = ?`, id).
		Scan(&rec.ID, &rec.Filename, &rec.SizeBytes, &rec.Kind, &enc, &rec.CreatedAt, &rec.Status, &rec.Error)
	if err != nil {
		return err
	}
	rec.Encrypted = enc == 1
	if err := os.Remove(a.backupPathFor(rec)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_, err = a.db.Exec(`DELETE FROM backups WHERE id = ?`, id)
	return err
}

// backupTick runs automatic backups according to the schedule. It never blocks
// the job loop: archive creation runs in its own goroutine.
func (a *App) backupTick() {
	schedule := strings.ToLower(a.settings.Get("backup_schedule"))
	if schedule != "daily" && schedule != "weekly" {
		return
	}
	now := time.Now().UTC()
	hour := int(a.settings.Int("backup_hour"))
	if now.Hour() != hour {
		return
	}
	window := now.Add(-23 * time.Hour)
	if schedule == "weekly" {
		window = now.Add(-6 * 24 * time.Hour)
	}
	var n int
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM backups WHERE kind = 'auto' AND created_at >= ?`, window.Unix()).Scan(&n)
	if n > 0 {
		return
	}
	if !a.backupMu.TryLock() {
		return
	}
	a.backupMu.Unlock()
	go func() {
		if _, err := a.createBackup("auto", a.settings.Bool("backup_encrypt_auto")); err != nil {
			a.log.Error("automatic backup failed", "error", err)
			return
		}
		a.pruneBackups()
	}()
}

func (a *App) pruneBackups() {
	keep := int(a.settings.Int("backup_retention_count"))
	if keep <= 0 {
		return
	}
	rows, err := a.db.Query(`SELECT id, filename FROM backups WHERE status = 'complete' ORDER BY created_at DESC`)
	if err != nil {
		return
	}
	type item struct{ id, filename string }
	items := []item{}
	for rows.Next() {
		var it item
		if rows.Scan(&it.id, &it.filename) == nil {
			items = append(items, it)
		}
	}
	rows.Close()
	for i, it := range items {
		if i < keep {
			continue
		}
		_ = os.Remove(filepath.Join(a.backupDir(), filepath.Base(it.filename)))
		_, _ = a.db.Exec(`DELETE FROM backups WHERE id = ?`, it.id)
	}
}

// decryptBackupTo extracts the archive body into a temporary tar.gz file.
func (a *App) decryptBackupTo(srcPath, destPath string) error {
	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()
	br := bufio.NewReaderSize(f, 256<<10)
	magic := make([]byte, len(backupMagic))
	if _, err := io.ReadFull(br, magic); err != nil || string(magic) != backupMagic {
		return errors.New("keine gültige TEX-Share-Sicherung")
	}
	version, err := br.ReadByte()
	if err != nil || version != backupVersion {
		return errors.New("nicht unterstützte Sicherungsversion")
	}
	flag, err := br.ReadByte()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(destPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	defer out.Close()
	bw := bufio.NewWriterSize(out, 256<<10)
	if flag == 0 {
		if _, err := io.Copy(bw, br); err != nil {
			return err
		}
		return bw.Flush()
	}
	if flag != 1 {
		return errors.New("unbekanntes Sicherungsformat")
	}
	salt := make([]byte, 16)
	if _, err := io.ReadFull(br, salt); err != nil {
		return err
	}
	key, err := a.backupKey()
	if err != nil {
		return err
	}
	derived, err := hkdf.Key(sha256.New, key, salt, "texshare/backup/v1", 32)
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(derived)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	var lenBuf [4]byte
	for {
		if _, err := io.ReadFull(br, lenBuf[:]); err != nil {
			return errors.New("Sicherung ist unvollständig")
		}
		n := binary.BigEndian.Uint32(lenBuf[:])
		if n == 0 {
			break
		}
		if n > backupChunk {
			return errors.New("ungültige Blockgröße")
		}
		nonce := make([]byte, gcm.NonceSize())
		if _, err := io.ReadFull(br, nonce); err != nil {
			return err
		}
		sealed := make([]byte, int(n)+gcm.Overhead())
		if _, err := io.ReadFull(br, sealed); err != nil {
			return err
		}
		plain, err := gcm.Open(nil, nonce, sealed, []byte("texshare-backup-v1"))
		if err != nil {
			return errors.New("Sicherung konnte nicht entschlüsselt werden (falscher Schlüssel?)")
		}
		if _, err := bw.Write(plain); err != nil {
			return err
		}
	}
	return bw.Flush()
}

// extractBackup safely unpacks a backup into an empty staging directory.
func extractBackup(tarPath, dest string) error {
	f, err := os.Open(tarPath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var total int64
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := filepath.ToSlash(filepath.Clean(hdr.Name))
		if name == "." || strings.HasPrefix(name, "../") || strings.HasPrefix(name, "/") || strings.Contains(name, "..") {
			return errors.New("unsicherer Pfad in Sicherung")
		}
		allowed := name == "secret.key" || name == "manifest.json" ||
			strings.HasPrefix(name, "db/") || strings.HasPrefix(name, "files/") || strings.HasPrefix(name, "branding/")
		if !allowed {
			continue
		}
		if hdr.Typeflag == tar.TypeDir {
			continue
		}
		if hdr.Typeflag != tar.TypeReg {
			return errors.New("nicht unterstützter Eintrag in Sicherung")
		}
		total += hdr.Size
		if total > 1<<40 {
			return errors.New("Sicherung ist zu groß")
		}
		target := filepath.Join(dest, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, io.LimitReader(tr, hdr.Size))
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	if _, err := os.Stat(filepath.Join(dest, "db", "texshare.db")); err != nil {
		return errors.New("Sicherung enthält keine Datenbank")
	}
	return nil
}

// restoreFromFile validates and applies a backup. A safety backup is created
// before anything is replaced, and the previous state is kept on disk so a
// failed swap can be rolled back.
func (a *App) restoreFromFile(path string) error {
	work := filepath.Join(a.cfg.DataDir, "tmp", "restore-"+util.RandomHex(8))
	if err := os.MkdirAll(work, 0o750); err != nil {
		return err
	}
	defer os.RemoveAll(work)

	// Safety backup first (while the current database is still healthy).
	if _, err := a.createBackup("pre-restore", true); err != nil {
		return fmt.Errorf("Sicherheitskopie fehlgeschlagen: %w", err)
	}
	tarPath := filepath.Join(work, "restore.tar.gz")
	if err := a.decryptBackupTo(path, tarPath); err != nil {
		return err
	}
	stage := filepath.Join(work, "stage")
	if err := os.MkdirAll(stage, 0o750); err != nil {
		return err
	}
	if err := extractBackup(tarPath, stage); err != nil {
		return err
	}
	// Verify the staged database opens and contains an administrator.
	stagedDB, err := db.Open(filepath.Join(stage, "db", "texshare.db"))
	if err != nil {
		return fmt.Errorf("Sicherungsdatenbank ist ungültig: %w", err)
	}
	var admins int
	if err := stagedDB.QueryRow(`SELECT COUNT(*) FROM users WHERE role = 'admin'`).Scan(&admins); err != nil {
		stagedDB.Close()
		return fmt.Errorf("Sicherungsdatenbank ist unvollständig: %w", err)
	}
	if admins == 0 {
		stagedDB.Close()
		return errors.New("Sicherung enthält keinen Administrator")
	}
	stagedDB.Close()

	a.restoreMu.Lock()
	defer a.restoreMu.Unlock()
	a.restoring = true
	defer func() { a.restoring = false }()

	old := filepath.Join(work, "old")
	if err := os.MkdirAll(old, 0o750); err != nil {
		return err
	}
	_ = a.db.Close()
	swap := []string{"db", "files", "branding", "secret.key"}
	for _, item := range swap {
		current := filepath.Join(a.cfg.DataDir, item)
		if _, err := os.Stat(current); errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err := os.Rename(current, filepath.Join(old, item)); err != nil {
			return a.rollbackRestore(old, swap, err)
		}
	}
	for _, item := range swap {
		staged := filepath.Join(stage, item)
		if _, err := os.Stat(staged); errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(filepath.Join(a.cfg.DataDir, item)), 0o750); err != nil {
			return a.rollbackRestore(old, swap, err)
		}
		if err := os.Rename(staged, filepath.Join(a.cfg.DataDir, item)); err != nil {
			return a.rollbackRestore(old, swap, err)
		}
	}
	if err := a.reopenAfterRestore(); err != nil {
		return a.rollbackRestore(old, swap, err)
	}
	a.audit(0, "system", "backup.restore", "backup", filepath.Base(path), "success", nil)
	return nil
}

func (a *App) rollbackRestore(old string, items []string, cause error) error {
	for _, item := range items {
		_ = os.RemoveAll(filepath.Join(a.cfg.DataDir, item))
	}
	for _, item := range items {
		if _, err := os.Stat(filepath.Join(old, item)); err == nil {
			_ = os.Rename(filepath.Join(old, item), filepath.Join(a.cfg.DataDir, item))
		}
	}
	if err := a.reopenAfterRestore(); err != nil {
		a.log.Error("restore rollback failed", "error", err)
	}
	return fmt.Errorf("Wiederherstellung fehlgeschlagen: %w", cause)
}

func (a *App) reopenAfterRestore() error {
	database, err := db.Open(a.cfg.DataDir + "/db/texshare.db")
	if err != nil {
		return err
	}
	sec, err := secrets.LoadOrCreate(a.cfg.DataDir)
	if err != nil {
		database.Close()
		return err
	}
	set, err := settings.New(database)
	if err != nil {
		database.Close()
		return err
	}
	a.db = database
	a.secrets = sec
	a.settings = set
	a.refreshHostCache()
	_ = a.initAudit()
	return nil
}

func (a *App) listBackupsHandler(w http.ResponseWriter, r *http.Request) {
	items, err := a.listBackups()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"backups": items, "directory": a.backupDir()})
}

func (a *App) createBackupHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Encrypt *bool `json:"encrypt"`
	}
	_ = readJSON(r, &req)
	encrypt := true
	if req.Encrypt != nil {
		encrypt = *req.Encrypt
	}
	rec, err := a.createBackup("manual", encrypt)
	if err != nil {
		a.log.Error("manual backup failed", "error", err)
		writeError(w, http.StatusInternalServerError, "backup_failed", err.Error())
		return
	}
	a.auditRequest(r, "backup.create", "backup", rec.ID, "success", map[string]any{"encrypted": encrypt})
	writeJSON(w, http.StatusCreated, map[string]any{"backup": rec})
}

func (a *App) downloadBackupHandler(w http.ResponseWriter, r *http.Request) {
	var rec backupRecord
	var enc int
	err := a.db.QueryRow(`SELECT id, filename, size_bytes, kind, encrypted, created_at, status, COALESCE(error,'') FROM backups WHERE id = ?`, r.PathValue("id")).
		Scan(&rec.ID, &rec.Filename, &rec.SizeBytes, &rec.Kind, &enc, &rec.CreatedAt, &rec.Status, &rec.Error)
	if err != nil {
		writeError(w, http.StatusNotFound, "backup_not_found", "")
		return
	}
	rec.Encrypted = enc == 1
	f, err := os.Open(a.backupPathFor(rec))
	if err != nil {
		writeError(w, http.StatusNotFound, "backup_file_missing", "")
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", util.ContentDisposition("attachment", rec.Filename))
	w.Header().Set("Cache-Control", "no-store")
	a.auditRequest(r, "backup.download", "backup", rec.ID, "success", nil)
	http.ServeContent(w, r, rec.Filename, time.Unix(rec.CreatedAt, 0), f)
}

func (a *App) deleteBackupHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := a.deleteBackup(id); err != nil {
		writeError(w, http.StatusNotFound, "backup_not_found", "")
		return
	}
	a.auditRequest(r, "backup.delete", "backup", id, "success", nil)
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) restoreBackupHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "")
		return
	}
	var path string
	if file, _, err := r.FormFile("file"); err == nil {
		defer file.Close()
		tmp, err := os.CreateTemp(filepath.Join(a.cfg.DataDir, "tmp"), "restore-upload-*")
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "")
			return
		}
		path = tmp.Name()
		defer os.Remove(path)
		if _, err := io.Copy(tmp, io.LimitReader(file, 1<<40)); err != nil {
			tmp.Close()
			writeError(w, http.StatusBadRequest, "upload_failed", "")
			return
		}
		if err := tmp.Close(); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "")
			return
		}
	} else {
		id := r.FormValue("id")
		var rec backupRecord
		var enc int
		if err := a.db.QueryRow(`SELECT id, filename, size_bytes, kind, encrypted, created_at, status, COALESCE(error,'') FROM backups WHERE id = ?`, id).
			Scan(&rec.ID, &rec.Filename, &rec.SizeBytes, &rec.Kind, &enc, &rec.CreatedAt, &rec.Status, &rec.Error); err != nil {
			writeError(w, http.StatusNotFound, "backup_not_found", "")
			return
		}
		path = a.backupPathFor(rec)
	}
	if err := a.restoreFromFile(path); err != nil {
		a.log.Error("restore failed", "error", err)
		writeError(w, http.StatusBadRequest, "restore_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "restarted": true})
}

func (a *App) pruneBackupsHandler(w http.ResponseWriter, r *http.Request) {
	a.pruneBackups()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) backupSettings() map[string]any {
	return map[string]any{
		"schedule":  a.settings.Get("backup_schedule"),
		"hour":      a.settings.Int("backup_hour"),
		"retention": a.settings.Int("backup_retention_count"),
		"directory": a.backupDir(),
		"encrypt":   a.settings.Bool("backup_encrypt_auto"),
	}
}
