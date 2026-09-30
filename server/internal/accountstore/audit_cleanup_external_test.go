package accountstore_test

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/testfixture"
)

func TestAuditCleanupTimeRangePreservesCurrentConfigAndReceipts(t *testing.T) {
	a := testfixture.NewFriendCapacityTestAccounts(t)
	uid := testfixture.CreateNamedFriendCapacityAccount(t, a, 1)
	d := a.Database()
	db, err := d.Open()
	if err != nil {
		t.Fatal(err)
	}
	end := time.Now().UTC().AddDate(0, 0, -10).Truncate(time.Second)
	start := end.AddDate(0, 0, -20)
	dates := []time.Time{start.AddDate(0, 0, -1), start, end.AddDate(0, 0, -1), end}
	ids := []int{}
	var latest accountstore.Document
	for i, date := range dates {
		latest, err = d.WriteDocument("gacha-live:60300001", i, map[string]any{"name": "保留完整审计", "price": i + 1, "card_ids": []int{1001, 1002}}, "gacha-live")
		if err != nil {
			t.Fatal(err)
		}
		var id int
		if err = db.QueryRow(`SELECT MAX(audit_id) FROM cn_admin_audit`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
		if _, err = db.Exec(`UPDATE cn_admin_audit SET created_utc=? WHERE audit_id=?`, date.Format(time.RFC3339Nano), id); err != nil {
			t.Fatal(err)
		}
	}
	state, err := a.LoadState(uid)
	if err != nil {
		t.Fatal(err)
	}
	receipt := &accountstore.AdminActionReceipt{OperationKey: "retained-delivery", UserID: uid, RequestSHA256: strings.Repeat("a", 64), Result: map[string]any{"sent": true}}
	if err = a.PersistStateWithAudit(uid, state, &accountstore.AdminAudit{Operation: "mail-delivery", Target: "player", Payload: map[string]any{"complete": true}, Receipt: receipt}); err != nil {
		t.Fatal(err)
	}
	var before string
	_ = db.QueryRow(`SELECT payload_sha256 FROM cn_save_snapshot WHERE singleton=1`).Scan(&before)
	from, to := start.Format(time.RFC3339Nano), end.Format(time.RFC3339Nano)
	preview, err := d.CleanupAuditHistory(context.Background(), from, to, "")
	if err != nil || preview.Records != 2 {
		t.Fatalf("preview %+v %v", preview, err)
	}
	if _, err = d.CleanupAuditHistory(context.Background(), "", to, preview.Digest); err == nil {
		t.Fatal("changed range accepted")
	}
	if _, err = db.Exec(`CREATE TRIGGER fail_audit_cleanup BEFORE DELETE ON cn_admin_audit WHEN OLD.audit_id=` + strconv.Itoa(ids[2]) + ` BEGIN SELECT RAISE(ABORT,'injected audit cleanup failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = d.CleanupAuditHistory(context.Background(), from, to, preview.Digest); err == nil {
		t.Fatal("injected failure ignored")
	}
	var remaining int
	_ = db.QueryRow(`SELECT COUNT(*) FROM cn_admin_audit WHERE audit_id=?`, ids[1]).Scan(&remaining)
	if remaining != 1 {
		t.Fatal("partial audit deletion committed")
	}
	if _, err = db.Exec(`DROP TRIGGER fail_audit_cleanup`); err != nil {
		t.Fatal(err)
	}
	result, err := d.CleanupAuditHistory(context.Background(), from, to, preview.Digest)
	if err != nil || !result.Applied {
		t.Fatalf("apply %+v %v", result, err)
	}
	for i, id := range ids {
		_ = db.QueryRow(`SELECT COUNT(*) FROM cn_admin_audit WHERE audit_id=?`, id).Scan(&remaining)
		want := 0
		if i == 0 || i == 3 {
			want = 1
		}
		if remaining != want {
			t.Fatal("time range boundary changed", i, remaining)
		}
	}
	live, err := d.ReadDocument("gacha-live:60300001")
	if err != nil || live.SHA256 != latest.SHA256 || live.Revision != 4 {
		t.Fatal("current config changed", err)
	}
	_ = db.QueryRow(`SELECT COUNT(*) FROM cn_global_operation WHERE operation_key='gacha-live:60300001'`).Scan(&remaining)
	if remaining != 1 {
		t.Fatal("configuration keeps hidden revisions")
	}
	var after string
	_ = db.QueryRow(`SELECT payload_sha256 FROM cn_save_snapshot WHERE singleton=1`).Scan(&after)
	if before != after {
		t.Fatal("player state changed")
	}
	if saved, err := d.ActionReceipt(receipt.OperationKey, uid, receipt.RequestSHA256); err != nil || len(saved) == 0 {
		t.Fatal("reward receipt deleted", err)
	}
	file, err := os.Open(result.Backup)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	decoder := json.NewDecoder(gz)
	var header map[string]any
	if err = decoder.Decode(&header); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids[1:3] {
		var row accountstore.AuditRecord
		if err = decoder.Decode(&row); err != nil || row.AuditID != id {
			t.Fatal("backup missing original audit", err)
		}
		var full map[string]any
		if json.Unmarshal(row.Payload, &full) != nil || full["card_ids"] == nil {
			t.Fatal("audit was replaced by a summary")
		}
	}
	if _, err = d.CleanupAuditHistory(context.Background(), from, to, preview.Digest); err == nil {
		t.Fatal("old preview reused after deletion")
	}
}
