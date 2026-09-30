package accountstore

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Only these bounded, configuration-owned counters are eligible. Inventory,
// progress, claims/transaction receipts and the audit trail are not garbage.
type OperationalReferences struct {
	Gachas map[int]bool `json:"gachas"`
	Items  map[int]bool `json:"items"`
	Events map[int]bool `json:"events"`
	Trades map[int]bool `json:"trades"`
}

type OperationalCleanupReport struct {
	RegistryRevision   int            `json:"registry_revision,omitempty"`
	GachaIDs           []int          `json:"gacha_ids,omitempty"`
	RemovedDocuments   int            `json:"removed_documents,omitempty"`
	ConfigurationBytes int64          `json:"configuration_bytes,omitempty"`
	Scanned            int            `json:"scanned"`
	Accounts           int            `json:"accounts"`
	Entries            map[string]int `json:"entries"`
	BytesBefore        int64          `json:"bytes_before"`
	BytesAfter         int64          `json:"bytes_after"`
	Digest             string         `json:"digest"`
	Backup             string         `json:"backup,omitempty"`
	Applied            bool           `json:"applied"`
}

// Conservatively retain identities in all draft/live documents, even if a
// document is not currently published. Called while configuration writes pause.
func (d *Database) RetainedGachaDocumentIDs(ctx context.Context) ([]int, error) {
	db, err := d.OpenRead()
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT operation_key FROM cn_global_operation WHERE (operation_key GLOB 'gacha-draft:*' OR operation_key GLOB 'gacha-live:*') AND CAST(payload_json AS TEXT)<>'null'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		_, value, _ := strings.Cut(key, ":")
		id, err := strconv.Atoi(value)
		if err != nil || id <= 0 {
			return nil, errors.New("invalid retained gacha document identity")
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func pruneOperationalMetadata(raw []byte, refs OperationalReferences, retired map[int]bool) ([]byte, map[string]int, error) {
	if _, err := DecodeAccountSnapshot(raw); err != nil {
		return nil, nil, err
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, nil, err
	}
	counts := map[string]int{}
	keepID := func(key string, id int, keep map[int]bool) bool {
		if retired != nil {
			if strings.HasPrefix(key, "gacha_") {
				return !retired[id]
			}
			return true
		}
		return keep[id]
	}
	pruneList := func(key, idKey string, keep map[int]bool) error {
		if len(root[key]) == 0 {
			return nil
		}
		var values []json.RawMessage
		if err := json.Unmarshal(root[key], &values); err != nil {
			return err
		}
		retained := make([]json.RawMessage, 0, len(values))
		for _, value := range values {
			var identity map[string]json.RawMessage
			if err := json.Unmarshal(value, &identity); err != nil {
				return err
			}
			var id int
			if err := json.Unmarshal(identity[idKey], &id); err != nil || id <= 0 {
				return errors.New("invalid cleanup list identity")
			}
			if keepID(key, id, keep) {
				retained = append(retained, value)
			} else {
				counts[key]++
			}
		}
		if counts[key] > 0 {
			encoded, err := json.Marshal(retained)
			if err != nil {
				return err
			}
			root[key] = encoded
		}
		return nil
	}
	for _, list := range []struct {
		key, id string
		keep    map[int]bool
	}{
		{"gacha_selections", "gachaid", refs.Gachas}, {"gacha_daily_claims", "gachaid", refs.Gachas},
		{"event_shop_purchases", "event_shop_lineupid", refs.Events}, {"trade_shop_purchases", "lineupid", refs.Trades},
	} {
		if err := pruneList(list.key, list.id, list.keep); err != nil {
			return nil, nil, err
		}
	}
	var progress map[string]json.RawMessage
	if err := json.Unmarshal(root["progress"], &progress); err != nil {
		return nil, nil, err
	}
	changedProgress := false
	for _, entry := range []struct {
		key  string
		keep map[int]bool
	}{{"gacha_plays", refs.Gachas}, {"shop_purchases", refs.Items}, {"shop_periods", refs.Items}} {
		if len(progress[entry.key]) == 0 {
			continue
		}
		var values map[string]json.RawMessage
		if err := json.Unmarshal(progress[entry.key], &values); err != nil {
			return nil, nil, err
		}
		for text := range values {
			id, err := strconv.Atoi(text)
			if err != nil || id <= 0 {
				return nil, nil, errors.New("invalid cleanup counter identity")
			}
			if !keepID(entry.key, id, entry.keep) {
				delete(values, text)
				counts[entry.key]++
			}
		}
		if counts[entry.key] > 0 {
			encoded, err := json.Marshal(values)
			if err != nil {
				return nil, nil, err
			}
			progress[entry.key] = encoded
			changedProgress = true
		}
	}
	if changedProgress {
		encoded, err := json.Marshal(progress)
		if err != nil {
			return nil, nil, err
		}
		root["progress"] = encoded
	}
	if len(counts) == 0 {
		return raw, counts, nil
	}
	encoded, err := json.Marshal(root)
	return encoded, counts, err
}

// One SQLite transaction, bounded keyset batches, one account payload in memory.
// Preview and apply both verify hashes; apply recomputes the reviewed digest.
// The caller has closed game/admin write admission and will invalidate caches.
func (d *Database) CleanupOperationalState(ctx context.Context, refs OperationalReferences, expected string) (report OperationalCleanupReport, err error) {
	return d.cleanupOperationalState(ctx, refs, expected, nil)
}

func (d *Database) cleanupOperationalState(ctx context.Context, refs OperationalReferences, expected string, purge *GachaPurgePlan) (report OperationalCleanupReport, err error) {
	defer func() {
		if err != nil {
			report.Applied = false
		}
	}()
	if purge == nil && (refs.Gachas == nil || refs.Items == nil || refs.Events == nil || refs.Trades == nil) {
		return report, errors.New("cleanup reference catalog is incomplete")
	}
	db, err := d.Open()
	if err != nil {
		return report, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return report, err
	}
	defer tx.Rollback()
	apply := expected != ""
	refJSON, err := json.Marshal(refs)
	if err != nil {
		return report, err
	}
	hash := sha256.New()
	_, _ = hash.Write(refJSON)
	var retired map[int]bool
	var configuration []purgeDocument
	if purge != nil {
		configuration, err = gachaPurgeDocuments(ctx, tx, purge)
		if err != nil {
			return report, err
		}
		retired = map[int]bool{}
		for _, id := range purge.GachaIDs {
			retired[id] = true
		}
		report.GachaIDs = append([]int{}, purge.GachaIDs...)
		for _, doc := range configuration {
			report.ConfigurationBytes += int64(len(doc.Payload))
			if doc.Remove && doc.Revision > 0 {
				report.RemovedDocuments++
			}
		}
		for _, write := range purge.Writes {
			if write.Key == "gacha-custom" {
				report.RegistryRevision = write.Expected + 1
			}
			body, err := json.Marshal(write.Value)
			if err != nil {
				return report, err
			}
			report.ConfigurationBytes -= int64(len(body))
		}
		body, err := json.Marshal(struct {
			Plan      *GachaPurgePlan
			Documents []purgeDocument
		}{purge, configuration})
		if err != nil {
			return report, err
		}
		_, _ = hash.Write(body)
	}
	report.Entries = map[string]int{}
	visit := func(fn func(int, string, int, []byte, string) error) error {
		// Primary account has its own canonical table; skip any legacy mirror.
		var raw []byte
		var sha string
		var revision int
		if err := tx.QueryRowContext(ctx, `SELECT revision,payload_json,payload_sha256 FROM cn_save_snapshot WHERE singleton=1`).Scan(&revision, &raw, &sha); err != nil {
			return err
		}
		if err := fn(PrimaryUserID, "cn_save_snapshot", revision, raw, sha); err != nil {
			return err
		}
		last := PrimaryUserID
		for {
			rows, err := tx.QueryContext(ctx, `SELECT user_id FROM cn_account_snapshot WHERE user_id>? ORDER BY user_id LIMIT 64`, last)
			if err != nil {
				return err
			}
			ids := []int{}
			for rows.Next() {
				var id int
				if err := rows.Scan(&id); err != nil {
					rows.Close()
					return err
				}
				ids = append(ids, id)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return err
			}
			if len(ids) == 0 {
				return nil
			}
			for _, id := range ids {
				if err := tx.QueryRowContext(ctx, `SELECT revision,payload_json,payload_sha256 FROM cn_account_snapshot WHERE user_id=?`, id).Scan(&revision, &raw, &sha); err != nil {
					return err
				}
				if err := fn(id, "cn_account_snapshot", revision, raw, sha); err != nil {
					return err
				}
				last = id
			}
		}
	}
	transform := func(raw []byte, sha string) ([]byte, map[string]int, error) {
		sum := sha256.Sum256(raw)
		if hex.EncodeToString(sum[:]) != sha {
			return nil, nil, errors.New("account hash mismatch; cleanup refused")
		}
		return pruneOperationalMetadata(raw, refs, retired)
	}
	err = visit(func(id int, table string, revision int, raw []byte, sha string) error {
		next, counts, err := transform(raw, sha)
		if err != nil {
			return fmt.Errorf("account %d: %w", id, err)
		}
		report.Scanned++
		report.BytesBefore += int64(len(raw))
		report.BytesAfter += int64(len(next))
		// Include all snapshots so writes after preview invalidate the plan.
		fmt.Fprintf(hash, "\n%d:%d:%s", id, revision, sha)
		if len(counts) > 0 {
			report.Accounts++
			for key, n := range counts {
				report.Entries[key] += n
			}
		}
		return nil
	})
	if err != nil {
		return report, err
	}
	report.Digest = hex.EncodeToString(hash.Sum(nil))
	if !apply {
		return report, nil
	}
	if expected != report.Digest {
		return report, errors.New("数据或配置已变化，请重新预览后清理")
	}
	if report.Accounts == 0 && purge == nil {
		report.Applied = true
		return report, nil
	}
	backupDir := filepath.Join(filepath.Dir(d.databasePath), "maintenance-backups")
	if err = os.MkdirAll(backupDir, 0700); err != nil {
		return report, err
	}
	pattern := "operational-*.jsonl.gz"
	if purge != nil {
		pattern = "gacha-purge-*.jsonl.gz"
	}
	file, err := os.CreateTemp(backupDir, pattern)
	if err != nil {
		return report, err
	}
	backupPath := file.Name()
	keepBackup := false
	defer func() {
		file.Close()
		if !keepBackup {
			_ = os.Remove(backupPath)
			report.Backup = ""
		}
	}()
	zipped := gzip.NewWriter(file)
	defer zipped.Close()
	encoder := json.NewEncoder(zipped)
	if err = encoder.Encode(map[string]any{"format": "operational-cleanup-v1", "created_utc": time.Now().UTC(), "report": report}); err != nil {
		return report, err
	}
	for _, doc := range configuration {
		if doc.Revision > 0 {
			if err = encoder.Encode(doc); err != nil {
				return report, err
			}
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	err = visit(func(id int, table string, revision int, raw []byte, sha string) error {
		next, counts, err := transform(raw, sha)
		if err != nil {
			return err
		}
		if len(counts) == 0 {
			return nil
		}
		if err = encoder.Encode(map[string]any{"user_id": id, "table": table, "revision": revision, "sha256": sha, "payload": json.RawMessage(raw)}); err != nil {
			return err
		}
		sum := sha256.Sum256(next)
		key, value := "user_id", id
		if table == "cn_save_snapshot" {
			key, value = "singleton", 1
		}
		_, err = tx.ExecContext(ctx, "UPDATE "+table+" SET revision=revision+1,updated_utc=?,payload_json=?,payload_sha256=? WHERE "+key+"=?", now, next, hex.EncodeToString(sum[:]), value)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE cn_account_projection SET snapshot_revision=?,updated_utc=? WHERE user_id=?`, revision+1, now, id)
		return err
	})
	if err != nil {
		return report, err
	}
	if purge != nil {
		if err = applyGachaPurge(ctx, tx, purge, configuration, now); err != nil {
			return report, err
		}
	}
	if err = zipped.Close(); err != nil {
		return report, err
	}
	if err = file.Sync(); err != nil {
		return report, err
	}
	if err = file.Close(); err != nil {
		return report, err
	}
	report.Backup = backupPath
	report.Applied = true
	audit := AdminAudit{Operation: "operational-cleanup", Target: "unreferenced-counters", Payload: report}
	if purge != nil {
		audit.Operation = "gacha-purge"
		audit.Target = strconv.Itoa(purge.GroupID)
	}
	if err = audit.appendTo(tx, now); err != nil {
		return report, err
	}
	// Keep a synced recovery copy even when commit outcome is uncertain.
	keepBackup = true
	if err = tx.Commit(); err != nil {
		report.Applied = false
		return report, err
	}
	return report, nil
}
