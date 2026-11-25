package app

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/AeroTex-Software/TEX-Share/internal/util"
)

// audit appends a tamper-evident entry to the audit chain. Details must never
// contain passwords, tokens or key material.
func (a *App) audit(actorID int64, actorName, action, objectType, objectID, result string, details any) {
	detailJSON := "{}"
	if details != nil {
		if b, err := json.Marshal(details); err == nil {
			detailJSON = string(b)
		}
	}
	a.auditMu.Lock()
	defer a.auditMu.Unlock()
	ts := util.Now()
	prev := a.lastAuditHash
	if prev == "" {
		prev = "genesis"
	}
	payload := fmt.Sprintf("%s|%d|%d|%s|%s|%s|%s|%s|%s", prev, ts, actorID, actorName, action, objectType, objectID, result, detailJSON)
	mac := hmac.New(sha256.New, a.secrets.HMACKey("audit"))
	mac.Write([]byte(payload))
	entryHash := hex.EncodeToString(mac.Sum(nil))
	_, err := a.db.Exec(`INSERT INTO audit_log(ts, actor_id, actor_username, action, object_type, object_id, result, details, prev_hash, entry_hash)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, ts, actorID, actorName, action, objectType, objectID, result, detailJSON, prev, entryHash)
	if err != nil {
		a.log.Error("audit write failed", "action", action, "error", err)
		return
	}
	a.lastAuditHash = entryHash
}

// auditRequest records the authenticated actor (or system).
func (a *App) auditRequest(r *http.Request, action, objectType, objectID, result string, details any) {
	sess, user := sessionFromContext(r)
	if user != nil {
		a.audit(user.ID, user.Username, action, objectType, objectID, result, details)
		return
	}
	if sess != nil {
		a.audit(sess.UserID, "session", action, objectType, objectID, result, details)
		return
	}
	a.audit(0, "anonymous", action, objectType, objectID, result, details)
}

type auditVerifyResult struct {
	OK        bool   `json:"ok"`
	Checked   int64  `json:"checked"`
	BrokenAt  int64  `json:"brokenAt,omitempty"`
	Message   string `json:"message,omitempty"`
}

func (a *App) verifyAuditChain() (auditVerifyResult, error) {
	rows, err := a.db.Query(`SELECT id, ts, actor_id, actor_username, action, object_type, object_id, result, details, prev_hash, entry_hash FROM audit_log ORDER BY id ASC`)
	if err != nil {
		return auditVerifyResult{}, err
	}
	defer rows.Close()
	prev := "genesis"
	var checked int64
	for rows.Next() {
		var id, ts, actorID int64
		var actorName, action, objType, objID, result, details, prevHash, entryHash string
		if err := rows.Scan(&id, &ts, &actorID, &actorName, &action, &objType, &objID, &result, &details, &prevHash, &entryHash); err != nil {
			return auditVerifyResult{}, err
		}
		if prevHash != prev {
			return auditVerifyResult{OK: false, Checked: checked, BrokenAt: id, Message: "previous hash mismatch"}, nil
		}
		payload := fmt.Sprintf("%s|%d|%d|%s|%s|%s|%s|%s|%s", prev, ts, actorID, actorName, action, objType, objID, result, details)
		mac := hmac.New(sha256.New, a.secrets.HMACKey("audit"))
		mac.Write([]byte(payload))
		want := hex.EncodeToString(mac.Sum(nil))
		if !hmac.Equal([]byte(want), []byte(entryHash)) {
			return auditVerifyResult{OK: false, Checked: checked, BrokenAt: id, Message: "entry hash mismatch"}, nil
		}
		prev = entryHash
		checked++
	}
	return auditVerifyResult{OK: true, Checked: checked}, rows.Err()
}

// notify creates an in-app notification if the user has not disabled the type.
func (a *App) notify(userID int64, typ string, data map[string]any) {
	if userID <= 0 {
		return
	}
	var prefsRaw string
	if err := a.db.QueryRow(`SELECT notification_prefs FROM users WHERE id = ?`, userID).Scan(&prefsRaw); err != nil {
		return
	}
	prefs := map[string]bool{}
	_ = json.Unmarshal([]byte(prefsRaw), &prefs)
	if enabled, ok := prefs[typ]; ok && !enabled {
		return
	}
	raw, _ := json.Marshal(data)
	if _, err := a.db.Exec(`INSERT INTO notifications(user_id, type, data, created_at) VALUES(?, ?, ?, ?)`, userID, typ, string(raw), util.Now()); err != nil {
		a.log.Warn("notification insert failed", "error", err)
	}
}

func (a *App) activity(userID int64, shareID, typ string, meta map[string]any) {
	raw := "{}"
	if meta != nil {
		if b, err := json.Marshal(meta); err == nil {
			raw = string(b)
		}
	}
	var share any = nil
	if shareID != "" {
		share = shareID
	}
	_, _ = a.db.Exec(`INSERT INTO activity(user_id, share_id, type, meta, created_at) VALUES(?, ?, ?, ?, ?)`, userID, share, typ, raw, util.Now())
}

// purgeOldActivity is called by the retention job.
func (a *App) purgeOldActivity(cutoff int64) {
	_, _ = a.db.Exec(`DELETE FROM activity WHERE created_at < ?`, cutoff)
	_, _ = a.db.Exec(`DELETE FROM notifications WHERE created_at < ? AND read_at IS NOT NULL`, cutoff)
}

var _ = time.Now
