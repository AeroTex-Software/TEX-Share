package app

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/AeroTex-Software/TEX-Share/internal/util"
)

// startJobs launches the single background scheduler. Individual jobs are
// idempotent so a missed tick only delays cleanup, never corrupts data.
func (a *App) startJobs() {
	a.jobWG.Add(1)
	go func() {
		defer a.jobWG.Done()
		// Run once shortly after boot, then every minute.
		timer := time.NewTimer(20 * time.Second)
		defer timer.Stop()
		select {
		case <-a.stopCh:
			return
		case <-timer.C:
			a.runJobs()
		}
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-a.stopCh:
				return
			case <-ticker.C:
				a.runJobs()
			}
		}
	}()
}

func (a *App) runJobs() {
	defer func() {
		if v := recover(); v != nil {
			a.log.Error("background job panic", "panic", fmt.Sprint(v))
		}
	}()
	a.expireShares()
	a.purgeDeletedShares()
	a.cleanupExpiredUploads()
	a.cleanupEphemeral()
	a.retentionJobs()
	if a.clamav != nil {
		a.clamav.scanPending()
		a.clamav.scanPendingShareFiles()
	}
	a.notifyExpiringShares()
	a.notifyStorageUsage()
	a.backupTick()
	a.ddnsTick()
	a.flushTraffic()
}

// flushTraffic folds the deltas of the monotonic network counters into today's
// traffic_daily row. The counters are process-local and reset on restart, so a
// decrease is treated as a restart and the whole current value is attributed to
// the current day. Failures are non-fatal: the counters keep accumulating and
// the next tick retries.
func (a *App) flushTraffic() {
	tx := atomic.LoadInt64(&a.trafficTx)
	rx := atomic.LoadInt64(&a.trafficRx)
	dtx := tx - a.lastTrafficTx
	drx := rx - a.lastTrafficRx
	if dtx < 0 {
		dtx = tx
	}
	if drx < 0 {
		drx = rx
	}
	a.lastTrafficTx = tx
	a.lastTrafficRx = rx
	if dtx == 0 && drx == 0 {
		return
	}
	day := util.Day(util.Now())
	_, _ = a.db.Exec(`INSERT INTO traffic_daily(day, tx_bytes, rx_bytes) VALUES(?, ?, ?)
		ON CONFLICT(day) DO UPDATE SET tx_bytes = tx_bytes + excluded.tx_bytes, rx_bytes = rx_bytes + excluded.rx_bytes`,
		day, dtx, drx)
}

// ddnsTick runs the DDNS engine if enabled. It is isolated from the other jobs
// so a network problem can never abort cleanup or notifications.
func (a *App) ddnsTick() {
	if a.network == nil || a.network.ddns == nil {
		return
	}
	defer func() {
		if v := recover(); v != nil {
			a.log.Error("ddns job panic", "panic", fmt.Sprint(v))
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	a.network.ddns.Tick(ctx, a.applyDDNS, a.ddnsConfig())
}

func (a *App) expireShares() {
	now := util.Now()
	rows, err := a.db.Query(`SELECT id, COALESCE(owner_id,0), slug, COALESCE(title,'') FROM shares
		WHERE status = 'active' AND expires_at IS NOT NULL AND expires_at > 0 AND expires_at <= ? LIMIT 200`, now)
	if err != nil {
		return
	}
	type item struct {
		id      string
		ownerID int64
		slug    string
		title   string
	}
	items := []item{}
	for rows.Next() {
		var it item
		if rows.Scan(&it.id, &it.ownerID, &it.slug, &it.title) == nil {
			items = append(items, it)
		}
	}
	rows.Close()
	for _, it := range items {
		if _, err := a.db.Exec(`UPDATE shares SET status = 'expired', updated_at = ? WHERE id = ? AND status = 'active'`, now, it.id); err != nil {
			continue
		}
		a.activity(it.ownerID, it.id, "share_expired", nil)
		a.notify(it.ownerID, "share_expired", map[string]any{"shareId": it.id, "slug": it.slug, "title": it.title})
		a.audit(0, "system", "share.expired", "share", it.id, "success", nil)
	}
}

func (a *App) purgeDeletedShares() {
	now := util.Now()
	rows, err := a.db.Query(`SELECT id FROM shares WHERE status = 'deleted' AND purge_after IS NOT NULL AND purge_after > 0 AND purge_after <= ? LIMIT 50`, now)
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
		s, err := a.shareByID(id)
		if err != nil {
			continue
		}
		owner := s.OwnerID
		if err := a.purgeShare(s); err == nil {
			a.audit(0, "system", "share.purged", "share", id, "success", nil)
			a.notify(owner, "share_purged", map[string]any{"shareId": id, "slug": s.Slug})
		}
	}
}

func (a *App) cleanupEphemeral() {
	now := util.Now()
	_, _ = a.db.Exec(`DELETE FROM sessions WHERE expires_at <= ?`, now)
	_, _ = a.db.Exec(`DELETE FROM login_challenges WHERE expires_at <= ?`, now)
	_, _ = a.db.Exec(`DELETE FROM share_access WHERE expires_at <= ?`, now)
	_, _ = a.db.Exec(`DELETE FROM zip_tickets WHERE expires_at <= ?`, now)
	_, _ = a.db.Exec(`DELETE FROM password_resets WHERE created_at < ?`, now-30*86400)
}

func (a *App) retentionJobs() {
	days := a.settings.Int("stats_retention_days")
	if days <= 0 {
		days = 180
	}
	cutoff := time.Now().AddDate(0, 0, -int(days)).UTC().Format("2006-01-02")
	_, _ = a.db.Exec(`DELETE FROM stats_daily WHERE day < ?`, cutoff)
	a.purgeOldActivity(util.Now() - days*86400)
}

// notifyExpiringShares warns owners once per day for shares expiring within
// three days. Duplicate warnings are suppressed by checking recent notifications.
func (a *App) notifyExpiringShares() {
	now := util.Now()
	rows, err := a.db.Query(`SELECT s.id, s.owner_id, s.slug, COALESCE(s.title,''), s.expires_at FROM shares s
		WHERE s.status = 'active' AND s.owner_id IS NOT NULL AND s.expires_at > ? AND s.expires_at <= ?`, now, now+3*86400)
	if err != nil {
		return
	}
	type item struct {
		id      string
		ownerID int64
		slug    string
		title   string
		expires int64
	}
	items := []item{}
	for rows.Next() {
		var it item
		if rows.Scan(&it.id, &it.ownerID, &it.slug, &it.title, &it.expires) == nil {
			items = append(items, it)
		}
	}
	rows.Close()
	for _, it := range items {
		var n int
		_ = a.db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE user_id = ? AND type = 'share_expiring'
			AND data LIKE ? AND created_at > ?`, it.ownerID, "%"+it.id+"%", now-86400).Scan(&n)
		if n > 0 {
			continue
		}
		a.notify(it.ownerID, "share_expiring", map[string]any{"shareId": it.id, "slug": it.slug, "title": it.title, "expiresAt": it.expires})
	}
}

func (a *App) notifyStorageUsage() {
	rows, err := a.db.Query(`SELECT id, username, storage_limit_bytes, storage_used_bytes FROM users
		WHERE storage_limit_bytes > 0 AND is_suspended = 0`)
	if err != nil {
		return
	}
	type item struct {
		id    int64
		name  string
		limit int64
		used  int64
	}
	items := []item{}
	for rows.Next() {
		var it item
		if rows.Scan(&it.id, &it.name, &it.limit, &it.used) == nil {
			items = append(items, it)
		}
	}
	rows.Close()
	now := util.Now()
	for _, it := range items {
		if it.limit <= 0 || it.used*100 < it.limit*90 {
			continue
		}
		var n int
		_ = a.db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE user_id = ? AND type = 'storage_warning' AND created_at > ?`, it.id, now-7*86400).Scan(&n)
		if n > 0 {
			continue
		}
		a.notify(it.id, "storage_warning", map[string]any{"used": it.used, "limit": it.limit})
	}
}
