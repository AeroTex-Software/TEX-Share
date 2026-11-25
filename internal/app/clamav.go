package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/AeroTex-Software/TEX-Share/internal/util"
)

type clamAV struct {
	app     *App
	mu      sync.Mutex
	status  string
	message string
	version string
	lastUpdate int64
	lastUpdateAttempt int64
	lastScan int64
	proc    *os.Process
	cmd     *exec.Cmd
	stopCh  chan struct{}
	stopOnce sync.Once
	scanSem chan struct{}
	scanLocks sync.Map
}

func newClamAV(a *App) *clamAV {
	return &clamAV{app: a, status: "disabled", stopCh: make(chan struct{}), scanSem: make(chan struct{}, 2)}
}

func (c *clamAV) enabled() bool {
	if c.app.cfg.ClamAVEnabled {
		return true
	}
	return c.app.settings.Bool("antivirus_enabled")
}

func (c *clamAV) mode() string {
	if c.app.cfg.ClamavMode == "external" {
		return "external"
	}
	return "managed"
}

func (c *clamAV) Start() error {
	go c.run()
	return nil
}

func (c *clamAV) Stop() {
	c.stopOnce.Do(func() { close(c.stopCh) })
	c.mu.Lock()
	defer c.mu.Unlock()
	c.killLocked()
}

func (c *clamAV) setStatus(status, msg string) {
	c.mu.Lock()
	c.status = status
	c.message = msg
	c.mu.Unlock()
}

func (c *clamAV) Status() map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	return map[string]any{
		"enabled": c.enabled(), "status": c.status, "message": c.message, "version": c.version,
		"lastUpdate": c.lastUpdate, "lastScan": c.lastScan,
	}
}

func (c *clamAV) Available() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status == "available"
}

func (c *clamAV) run() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.stopCh:
			c.mu.Lock()
			c.killLocked()
			c.mu.Unlock()
			return
		default:
		}
		if !c.enabled() {
			c.mu.Lock()
			c.killLocked()
			c.mu.Unlock()
			c.setStatus("disabled", "")
			select {
			case <-c.stopCh:
				return
			case <-ticker.C:
			}
			continue
		}
		c.ensureManaged()
		c.updateSignaturesIfDue()
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
		}
	}
}

func (c *clamAV) ensureManaged() {
	if c.mode() == "external" {
		if _, err := exec.LookPath(c.app.cfg.ClamdscanPath); err != nil {
			c.setStatus("unavailable", "clamdscan not found")
			return
		}
		c.setStatus("available", "external scanner")
		c.refreshVersion()
		return
	}
	c.mu.Lock()
	running := c.proc != nil
	c.mu.Unlock()
	if running {
		if c.socketReady() {
			c.setStatus("available", "")
			c.refreshVersion()
		}
		return
	}
	c.setStatus("starting", "")
	confPath := filepath.Join(c.app.cfg.DataDir, "clamav", "clamd.conf")
	if err := c.writeConfigs(confPath); err != nil {
		c.setStatus("error", err.Error())
		return
	}
	// Update signatures before the first start when no database exists.
	if !c.hasDatabase() {
		c.setStatus("updating", "")
		if err := c.runFreshclam(confPath); err != nil {
			c.setStatus("error", "signature update failed: "+err.Error())
			return
		}
	}
	cmd := exec.Command(c.app.cfg.ClamavPath, "-F", "-c", confPath)
	setProcAttrs(cmd)
	logFile, err := os.OpenFile(filepath.Join(c.app.cfg.DataDir, "clamav", "clamd.out.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		c.setStatus("error", err.Error())
		return
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		logFile.Close()
		c.setStatus("unavailable", "clamd not available: "+err.Error())
		return
	}
	c.mu.Lock()
	c.proc = cmd.Process
	c.cmd = cmd
	c.mu.Unlock()
	go func() {
		err := cmd.Wait()
		logFile.Close()
		c.mu.Lock()
		if c.cmd == cmd {
			c.proc = nil
			c.cmd = nil
		}
		c.mu.Unlock()
		if err != nil && !strings.Contains(err.Error(), "signal") {
			c.setStatus("unavailable", "clamd exited: "+err.Error())
		}
	}()
	// Give clamd time to load signatures, then verify.
	for i := 0; i < 12; i++ {
		select {
		case <-c.stopCh:
			return
		case <-time.After(2 * time.Second):
		}
		if c.socketReady() {
			c.setStatus("available", "")
			c.refreshVersion()
			return
		}
	}
	c.setStatus("unavailable", "clamd did not become ready")
}

func (c *clamAV) killLocked() {
	if c.cmd != nil && c.cmd.Process != nil {
		terminateProcess(c.cmd.Process)
	}
	c.proc = nil
	c.cmd = nil
}

func (c *clamAV) socketReady() bool {
	sock := filepath.Join(c.app.cfg.DataDir, "clamav", "clamd.sock")
	_, err := os.Stat(sock)
	return err == nil
}

func (c *clamAV) hasDatabase() bool {
	dir := filepath.Join(c.app.cfg.DataDir, "clamav", "db")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		name := strings.ToLower(e.Name())
		if strings.HasSuffix(name, ".cvd") || strings.HasSuffix(name, ".cld") {
			return true
		}
	}
	return false
}

func (c *clamAV) writeConfigs(confPath string) error {
	dir := filepath.Join(c.app.cfg.DataDir, "clamav")
	db := filepath.Join(dir, "db")
	tmp := filepath.Join(c.app.cfg.DataDir, "tmp")
	if err := os.MkdirAll(db, 0o750); err != nil {
		return err
	}
	sock := filepath.Join(dir, "clamd.sock")
	conf := fmt.Sprintf(`LocalSocket %s
Foreground true
DatabaseDirectory %s
TemporaryDirectory %s
LogFile %s
LogFileMaxSize 10M
LogTime yes
PidFile %s
MaxScanSize 200M
MaxFileSize 100M
MaxRecursion 10
MaxFiles 1000
MaxEmbeddedPE 50M
ScanArchive yes
ScanPE yes
ScanELF yes
ScanHTML yes
ScanOLE2 yes
ScanPDF yes
ScanSWF yes
`, sock, db, tmp, filepath.Join(dir, "clamd.log"), filepath.Join(dir, "clamd.pid"))
	if err := os.WriteFile(confPath, []byte(conf), 0o640); err != nil {
		return err
	}
	freshConf := fmt.Sprintf(`DatabaseDirectory %s
UpdateLogFile %s
DatabaseMirror database.clamav.net
Checks 12
`, db, filepath.Join(dir, "freshclam.log"))
	return os.WriteFile(filepath.Join(dir, "freshclam.conf"), []byte(freshConf), 0o640)
}

func (c *clamAV) runFreshclam(confPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.app.cfg.FreshclamPath, "--config-file="+filepath.Join(c.app.cfg.DataDir, "clamav", "freshclam.conf"))
	out, err := cmd.CombinedOutput()
	if len(out) > 0 {
		_ = os.WriteFile(filepath.Join(c.app.cfg.DataDir, "clamav", "freshclam.out.log"), out, 0o640)
	}
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.lastUpdate = util.Now()
	c.mu.Unlock()
	return nil
}

func (c *clamAV) refreshVersion() {
	if c.version != "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, c.app.cfg.ClamdscanPath, "--version").CombinedOutput()
	if err == nil {
		c.mu.Lock()
		c.version = strings.TrimSpace(string(out))
		c.mu.Unlock()
	}
}

func (c *clamAV) scanArgs(path string) []string {
	if c.mode() == "external" {
		return []string{"--no-summary", "--stream", path}
	}
	return []string{"--config-file=" + filepath.Join(c.app.cfg.DataDir, "clamav", "clamd.conf"), "--no-summary", "--stream", path}
}

// ScanFile scans one file. It returns clean=true only for an explicit clean
// verdict. Infrastructure errors are returned separately so callers never
// silently mark a file as clean.
func (c *clamAV) ScanFile(path string) (clean bool, message string, err error) {
	if !c.enabled() {
		return false, "antivirus disabled", errors.New("antivirus disabled")
	}
	if !c.Available() {
		return false, "scanner unavailable", errors.New("antivirus unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.app.cfg.ClamdscanPath, c.scanArgs(path)...)
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if len(text) > 1000 {
		text = text[:1000]
	}
	c.mu.Lock()
	c.lastScan = util.Now()
	c.mu.Unlock()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return false, text, nil
		}
		return false, text, err
	}
	return true, text, nil
}

func (c *clamAV) queueScan(uploadID string) {
	go func() {
		c.scanSem <- struct{}{}
		defer func() { <-c.scanSem }()
		lockAny, _ := c.scanLocks.LoadOrStore(uploadID, &sync.Mutex{})
		lock := lockAny.(*sync.Mutex)
		lock.Lock()
		defer lock.Unlock()
		c.scanUpload(uploadID)
	}()
}

func (c *clamAV) scanUpload(uploadID string) {
	var status, path, filename string
	var userID, size int64
	err := c.app.db.QueryRow(`SELECT status, COALESCE(storage_path,''), filename, user_id, size_bytes FROM uploads WHERE id = ?`, uploadID).
		Scan(&status, &path, &filename, &userID, &size)
	if err != nil || path == "" {
		return
	}
	if status == "cancelled" {
		return
	}
	var attached int
	_ = c.app.db.QueryRow(`SELECT COUNT(*) FROM share_files WHERE id = ?`, uploadID).Scan(&attached)
	if attached == 0 && status == "complete" {
		// The upload may still be staged and waiting for share creation.
	} else if attached == 0 {
		return
	}
	_, _ = c.app.db.Exec(`UPDATE uploads SET scan_status = 'scanning' WHERE id = ?`, uploadID)
	clean, msg, scanErr := c.ScanFile(filepath.Join(c.app.cfg.DataDir, filepath.FromSlash(path)))
	if scanErr != nil {
		_, _ = c.app.db.Exec(`UPDATE uploads SET scan_status = 'pending' WHERE id = ?`, uploadID)
		return
	}
	if clean {
		_, _ = c.app.db.Exec(`UPDATE uploads SET scan_status = 'clean' WHERE id = ?`, uploadID)
		_, _ = c.app.db.Exec(`UPDATE share_files SET scan_status = 'clean', scan_message = NULL WHERE id = ?`, uploadID)
		c.app.notify(userID, "scan_complete", map[string]any{"filename": filename, "result": "clean"})
		return
	}
	_, _ = c.app.db.Exec(`UPDATE uploads SET scan_status = 'infected' WHERE id = ?`, uploadID)
	_, _ = c.app.db.Exec(`UPDATE share_files SET scan_status = 'infected', scan_message = ? WHERE id = ?`, msg, uploadID)
	qpath, qerr := c.app.store.MoveToQuarantine(path, "quarantine", uploadID)
	if qerr == nil {
		_, _ = c.app.db.Exec(`UPDATE share_files SET storage_path = ? WHERE id = ?`, qpath, uploadID)
		_, _ = c.app.db.Exec(`UPDATE uploads SET storage_path = ? WHERE id = ?`, qpath, uploadID)
	}
	if attached == 0 {
		_, _ = c.app.db.Exec(`UPDATE uploads SET status = 'cancelled' WHERE id = ?`, uploadID)
		c.app.refundQuota(userID, size)
	}
	c.app.notify(userID, "scan_complete", map[string]any{"filename": filename, "result": "infected"})
}

func (c *clamAV) scanPending() {
	if !c.enabled() || !c.Available() {
		return
	}
	rows, err := c.app.db.Query(`SELECT id FROM uploads WHERE scan_status IN ('pending','scanning') AND status IN ('complete','uploading') LIMIT 50`)
	if err != nil {
		return
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		c.queueScan(id)
	}
}

func (c *clamAV) updateSignaturesIfDue() {
	if !c.enabled() || c.mode() != "managed" || !c.Available() {
		return
	}
	hours := c.app.settings.Int("freshclam_interval_hours")
	if hours <= 0 {
		hours = 6
	}
	now := util.Now()
	c.mu.Lock()
	due := (c.lastUpdate == 0 || now-c.lastUpdate > hours*3600) && (c.lastUpdateAttempt == 0 || now-c.lastUpdateAttempt > 900)
	if due {
		c.lastUpdateAttempt = now
	}
	c.mu.Unlock()
	if !due {
		return
	}
	conf := filepath.Join(c.app.cfg.DataDir, "clamav", "freshclam.conf")
	if err := c.runFreshclam(conf); err != nil {
		c.app.log.Warn("freshclam update failed", "error", err)
	}
}

func (c *clamAV) scanPendingShareFiles() {
	if !c.enabled() || !c.Available() {
		return
	}
	rows, err := c.app.db.Query(`SELECT f.share_id, f.id FROM share_files f WHERE f.scan_status = 'pending' LIMIT 50`)
	if err != nil {
		return
	}
	type pair struct{ shareID, fileID string }
	list := []pair{}
	for rows.Next() {
		var p pair
		if rows.Scan(&p.shareID, &p.fileID) == nil {
			list = append(list, p)
		}
	}
	rows.Close()
	for _, p := range list {
		c.scanShareFile(p.shareID, p.fileID)
	}
}

func (c *clamAV) scanShareFile(shareID, fileID string) {
	var path, name string
	var ownerID int64
	err := c.app.db.QueryRow(`SELECT f.storage_path, f.name, COALESCE(s.owner_id,0) FROM share_files f
		JOIN shares s ON s.id = f.share_id WHERE f.id = ? AND f.share_id = ?`, fileID, shareID).Scan(&path, &name, &ownerID)
	if err != nil || path == "" {
		return
	}
	_, _ = c.app.db.Exec(`UPDATE share_files SET scan_status = 'scanning' WHERE id = ?`, fileID)
	clean, msg, scanErr := c.ScanFile(filepath.Join(c.app.cfg.DataDir, filepath.FromSlash(path)))
	if scanErr != nil {
		_, _ = c.app.db.Exec(`UPDATE share_files SET scan_status = 'pending' WHERE id = ?`, fileID)
		return
	}
	if clean {
		_, _ = c.app.db.Exec(`UPDATE share_files SET scan_status = 'clean', scan_message = NULL WHERE id = ?`, fileID)
	} else {
		_, _ = c.app.db.Exec(`UPDATE share_files SET scan_status = 'infected', scan_message = ? WHERE id = ?`, msg, fileID)
		if qpath, qerr := c.app.store.MoveToQuarantine(path, shareID, fileID); qerr == nil {
			_, _ = c.app.db.Exec(`UPDATE share_files SET storage_path = ? WHERE id = ?`, qpath, fileID)
		}
	}
	c.app.notify(ownerID, "scan_complete", map[string]any{"filename": name, "result": map[bool]string{true: "clean", false: "infected"}[clean]})
}

var _ = os.ErrNotExist
