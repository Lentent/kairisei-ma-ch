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
	"time"
)

type AuditCleanupReport struct {
	StartUTC     string `json:"start_utc"`
	EndUTC       string `json:"end_utc"`
	Records      int    `json:"records"`
	PayloadBytes int64  `json:"payload_bytes"`
	Digest       string `json:"digest"`
	Backup       string `json:"backup,omitempty"`
	Applied      bool   `json:"applied"`
}

func AuditCleanupRange(start, end string) (string, string, error) {
	until, err := time.Parse(time.RFC3339Nano, end)
	if err != nil || until.After(time.Now()) {
		return "", "", errors.New("请选择不晚于当前时间的清理结束时间")
	}
	if start != "" {
		from, err := time.Parse(time.RFC3339Nano, start)
		if err != nil || !from.Before(until) {
			return "", "", errors.New("清理开始时间须早于结束时间")
		}
		start = from.UTC().Format(time.RFC3339Nano)
	}
	return start, until.UTC().Format(time.RFC3339Nano), nil
}

// Audit history has no gameplay authority. Full configuration and account
// receipts live elsewhere; this function can only delete cn_admin_audit rows.
func (d *Database) CleanupAuditHistory(ctx context.Context, start, end, expected string) (report AuditCleanupReport, err error) {
	defer func() {
		if err != nil {
			report.Applied = false
		}
	}()
	report.StartUTC, report.EndUTC, err = AuditCleanupRange(start, end)
	if err != nil {
		return report, err
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
	visit := func(fn func(AuditRecord) error) error {
		last := int64(0)
		for {
			rows, err := tx.QueryContext(ctx, `SELECT audit_id FROM cn_admin_audit WHERE audit_id>? AND (?='' OR julianday(created_utc)>=julianday(?)) AND julianday(created_utc)<julianday(?) ORDER BY audit_id LIMIT 64`, last, report.StartUTC, report.StartUTC, report.EndUTC)
			if err != nil {
				return err
			}
			ids := []int64{}
			for rows.Next() {
				var id int64
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
				var row AuditRecord
				if err := tx.QueryRowContext(ctx, `SELECT audit_id,created_utc,operation,target,payload_json,payload_sha256 FROM cn_admin_audit WHERE audit_id=?`, id).Scan(&row.AuditID, &row.CreatedUTC, &row.Operation, &row.Target, &row.Payload, &row.SHA256); err != nil {
					return err
				}
				sum := sha256.Sum256(row.Payload)
				if hex.EncodeToString(sum[:]) != row.SHA256 {
					return errors.New("审计内容哈希不一致，已拒绝清理")
				}
				if err := fn(row); err != nil {
					return err
				}
				last = id
			}
		}
	}
	hash := sha256.New()
	fmt.Fprintf(hash, "%s\n%s", report.StartUTC, report.EndUTC)
	err = visit(func(row AuditRecord) error {
		report.Records++
		report.PayloadBytes += int64(len(row.Payload))
		identity := row
		identity.Payload = nil
		raw, err := json.Marshal(identity)
		if err != nil {
			return err
		}
		_, err = hash.Write(raw)
		return err
	})
	if err != nil {
		return report, err
	}
	report.Digest = hex.EncodeToString(hash.Sum(nil))
	if expected == "" {
		return report, nil
	}
	if expected != report.Digest {
		return report, errors.New("审计记录或时间范围已变化，请重新预览")
	}
	if report.Records == 0 {
		report.Applied = true
		return report, nil
	}
	dir := filepath.Join(filepath.Dir(d.databasePath), "maintenance-backups")
	if err = os.MkdirAll(dir, 0700); err != nil {
		return report, err
	}
	file, err := os.CreateTemp(dir, "audit-*.jsonl.gz")
	if err != nil {
		return report, err
	}
	path := file.Name()
	keep := false
	defer func() {
		file.Close()
		if !keep {
			_ = os.Remove(path)
			report.Backup = ""
		}
	}()
	zipped := gzip.NewWriter(file)
	defer zipped.Close()
	encoder := json.NewEncoder(zipped)
	if err = encoder.Encode(map[string]any{"format": "audit-cleanup-v1", "created_utc": time.Now().UTC(), "report": report}); err != nil {
		return report, err
	}
	err = visit(func(row AuditRecord) error {
		if err := encoder.Encode(row); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM cn_admin_audit WHERE audit_id=?`, row.AuditID)
		return err
	})
	if err != nil {
		return report, err
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
	report.Backup = path
	report.Applied = true
	audit := AdminAudit{Operation: "audit-history-cleanup", Target: "cn_admin_audit", Payload: report}
	if err = audit.appendTo(tx, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return report, err
	}
	keep = true
	err = tx.Commit()
	return report, err
}
