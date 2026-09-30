package accountstore_test

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"reflect"
	"testing"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/testfixture"
)

func TestOperationalCleanupPreservesAccountsAndRollsBack(t *testing.T) {
	a := testfixture.NewFriendCapacityTestAccounts(t)
	_ = testfixture.CreateNamedFriendCapacityAccount(t, a, 1)
	id := testfixture.CreateNamedFriendCapacityAccount(t, a, 2)
	d := a.Database()
	db, err := d.Open()
	if err != nil {
		t.Fatal(err)
	}
	refs := accountstore.OperationalReferences{Gachas: map[int]bool{11: true}, Items: map[int]bool{12: true}, Events: map[int]bool{13: true}, Trades: map[int]bool{14: true}}
	original := map[int][]byte{}
	for _, uid := range []int{accountstore.PrimaryUserID, id} {
		table, key, value := "cn_account_snapshot", "user_id", uid
		if uid == accountstore.PrimaryUserID {
			table, key, value = "cn_save_snapshot", "singleton", 1
		}
		var raw []byte
		if err := db.QueryRow("SELECT payload_json FROM "+table+" WHERE "+key+"=?", value).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var root map[string]json.RawMessage
		_ = json.Unmarshal(raw, &root)
		var progress map[string]json.RawMessage
		_ = json.Unmarshal(root["progress"], &progress)
		// Even an ID absent from today's catalog is stable progress, not garbage.
		for _, key := range []string{"boss_states", "main_story", "cn_main_story", "sub_story", "event_story"} {
			progress[key] = json.RawMessage(`{"99999999":3}`)
		}
		progress["gacha_plays"] = json.RawMessage(`{"11":7,"99999999":2}`)
		progress["shop_purchases"] = json.RawMessage(`{"12":3,"99999999":1}`)
		progress["shop_periods"] = json.RawMessage(`{"12":{"day":"2026-09-30","day_count":1},"99999999":{"day":"2026-09-30","day_count":1}}`)
		root["progress"], _ = json.Marshal(progress)
		root["gacha_daily_claims"] = json.RawMessage(`[{"gachaid":11,"day":"2026-09-30"},{"gachaid":99999999,"day":"2026-09-30"}]`)
		root["gacha_selections"] = json.RawMessage(`[{"gachaid":11,"rewards":[]},{"gachaid":99999999,"rewards":[]}]`)
		root["trade_shop_purchases"] = json.RawMessage(`[{"lineupid":14,"count":8},{"lineupid":99999999,"count":4}]`)
		root["event_shop_purchases"] = json.RawMessage(`[{"event_shop_lineupid":13,"count":8},{"event_shop_lineupid":99999999,"count":4}]`)
		raw, _ = json.Marshal(root)
		sum := sha256.Sum256(raw)
		original[uid] = raw
		if _, err := db.Exec("UPDATE "+table+" SET payload_json=?,payload_sha256=? WHERE "+key+"=?", raw, hex.EncodeToString(sum[:]), value); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	preview, err := d.CleanupOperationalState(ctx, refs, "")
	if err != nil || preview.Accounts != 2 || len(preview.Entries) != 7 || preview.Applied {
		t.Fatalf("preview: %+v %v", preview, err)
	}
	refs.Gachas[99999999] = true
	if _, err = d.CleanupOperationalState(ctx, refs, preview.Digest); err == nil {
		t.Fatal("changed references accepted")
	}
	delete(refs.Gachas, 99999999)
	if _, err = db.Exec(`CREATE TRIGGER fail_cleanup BEFORE UPDATE ON cn_account_snapshot BEGIN SELECT RAISE(ABORT,'injected failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = d.CleanupOperationalState(ctx, refs, preview.Digest); err == nil {
		t.Fatal("injected cleanup failure ignored")
	}
	var after []byte
	_ = db.QueryRow(`SELECT payload_json FROM cn_save_snapshot WHERE singleton=1`).Scan(&after)
	if string(after) != string(original[accountstore.PrimaryUserID]) {
		t.Fatal("partial cleanup committed")
	}
	if _, err = db.Exec(`DROP TRIGGER fail_cleanup`); err != nil {
		t.Fatal(err)
	}
	result, err := d.CleanupOperationalState(ctx, refs, preview.Digest)
	if err != nil || !result.Applied || result.Backup == "" {
		t.Fatalf("apply: %+v %v", result, err)
	}
	for _, uid := range []int{accountstore.PrimaryUserID, id} {
		table, key, value := "cn_account_snapshot", "user_id", uid
		if uid == accountstore.PrimaryUserID {
			table, key, value = "cn_save_snapshot", "singleton", 1
		}
		var raw []byte
		var sha string
		_ = db.QueryRow("SELECT payload_json,payload_sha256 FROM "+table+" WHERE "+key+"=?", value).Scan(&raw, &sha)
		sum := sha256.Sum256(raw)
		if hex.EncodeToString(sum[:]) != sha {
			t.Fatal("snapshot checksum not updated")
		}
		var beforeMap, afterMap map[string]any
		_ = json.Unmarshal(original[uid], &beforeMap)
		_ = json.Unmarshal(raw, &afterMap)
		for _, key := range []string{"gacha_daily_claims", "gacha_selections", "trade_shop_purchases", "event_shop_purchases"} {
			if len(afterMap[key].([]any)) != 1 {
				t.Fatal("referenced record removed or orphan retained", key)
			}
			delete(beforeMap, key)
			delete(afterMap, key)
		}
		for _, key := range []string{"gacha_plays", "shop_purchases", "shop_periods"} {
			left, right := beforeMap["progress"].(map[string]any), afterMap["progress"].(map[string]any)
			delete(left[key].(map[string]any), "99999999")
			if !reflect.DeepEqual(left[key], right[key]) {
				t.Fatal("referenced limit changed", key)
			}
		}
		if !reflect.DeepEqual(beforeMap, afterMap) {
			t.Fatal("unrelated account fields changed")
		}
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
	n := 0
	for {
		var row map[string]json.RawMessage
		err := decoder.Decode(&row)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if n > 0 {
			var uid int
			_ = json.Unmarshal(row["user_id"], &uid)
			if string(row["payload"]) != string(original[uid]) {
				t.Fatal("backup is not the original payload")
			}
		}
		n++
	}
	if n != 3 {
		t.Fatal("backup missing account", n)
	}
	preview, err = d.CleanupOperationalState(ctx, refs, "")
	if err != nil || preview.Accounts != 0 {
		t.Fatalf("second preview: %+v %v", preview, err)
	}
}
