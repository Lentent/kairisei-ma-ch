package accountstore

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const CDKPrefix = "cdk:"

var (
	ErrCDKUnavailable = errors.New("兑换码无效、未开始或已停用")
	ErrCDKExpired     = errors.New("兑换码已过期")
	ErrCDKUsed        = errors.New("该账号已经兑换过此礼包码")
	ErrCDKExhausted   = errors.New("兑换码使用次数已用完")
)

// Rewards are immutable; only Enabled may change after creation.
type CDKPolicy struct {
	Enabled     bool  `json:"enabled"`
	StartUnix   int64 `json:"start_unix"`
	ExpiresUnix int64 `json:"expires_unix"`
	MaxUses     int   `json:"max_uses"` // 0 means unlimited accounts; 1 is a single-use code.
}

type CDKClaim struct{ Key, SHA256 string }

// Called inside the account snapshot transaction, before its durable receipt.
// The single SQLite writer makes global usage limits atomic across accounts.
func (claim *CDKClaim) appendTo(tx *sql.Tx) error {
	if claim == nil {
		return nil
	}
	var body []byte
	var sha string
	if err := tx.QueryRow(`SELECT payload_json,payload_sha256 FROM cn_global_operation WHERE operation_key=?`, claim.Key).Scan(&body, &sha); err != nil {
		return err
	}
	digest := sha256.Sum256(body)
	if hex.EncodeToString(digest[:]) != sha || sha != claim.SHA256 {
		return ErrDocumentConflict
	}
	var policy CDKPolicy
	if err := json.Unmarshal(body, &policy); err != nil {
		return err
	}
	now := time.Now().Unix()
	if !policy.Enabled || now < policy.StartUnix {
		return ErrCDKUnavailable
	}
	if policy.ExpiresUnix != 0 && now >= policy.ExpiresUnix {
		return ErrCDKExpired
	}
	var used int
	if err := tx.QueryRow(`SELECT count(*) FROM cn_admin_action_receipt WHERE operation_key=?`, claim.Key).Scan(&used); err != nil {
		return err
	}
	if policy.MaxUses > 0 && used >= policy.MaxUses {
		return ErrCDKExhausted
	}
	return nil
}

// Batch creation is all-or-nothing, including its audit record.
func (storage *Database) CreateCDKDocuments(values map[string]any) error {
	if len(values) < 1 || len(values) > 100 {
		return errors.New("每批生成1–100个兑换码")
	}
	db, err := storage.Open()
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for code, value := range values {
		body, err := json.Marshal(value)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(body)
		result, err := tx.Exec(`INSERT INTO cn_global_operation(operation_key,revision,updated_utc,payload_json,payload_sha256) VALUES (?,1,?,?,?) ON CONFLICT(operation_key) DO NOTHING`, CDKPrefix+code, now, body, hex.EncodeToString(digest[:]))
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrDocumentConflict
		}
		if err := (&AdminAudit{Operation: "cdk-create", Target: code, Payload: value}).appendTo(tx, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (storage *Database) CDKUsedBy(code string, userID int) (bool, error) {
	db, err := storage.OpenRead()
	if err != nil {
		return false, err
	}
	var n int
	err = db.QueryRow(`SELECT count(*) FROM cn_admin_action_receipt WHERE operation_key=? AND user_id=?`, CDKPrefix+code, userID).Scan(&n)
	return n > 0, err
}

type CDKRecord struct {
	UserID     int             `json:"user_id"`
	CreatedUTC string          `json:"created_utc"`
	Result     json.RawMessage `json:"result"`
}

func (storage *Database) CDKRecords(code string, limit, offset int) ([]CDKRecord, int, error) {
	db, err := storage.OpenRead()
	if err != nil {
		return nil, 0, err
	}
	key := CDKPrefix + code
	var total int
	if err := db.QueryRow(`SELECT count(*) FROM cn_admin_action_receipt WHERE operation_key=?`, key).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := db.Query(`SELECT user_id,created_utc,result_json,result_sha256 FROM cn_admin_action_receipt WHERE operation_key=? ORDER BY created_utc DESC,user_id LIMIT ? OFFSET ?`, key, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	result := []CDKRecord{}
	for rows.Next() {
		var record CDKRecord
		var sha string
		if err := rows.Scan(&record.UserID, &record.CreatedUTC, &record.Result, &sha); err != nil {
			return nil, 0, err
		}
		digest := sha256.Sum256(record.Result)
		if hex.EncodeToString(digest[:]) != sha {
			return nil, 0, fmt.Errorf("兑换记录校验失败")
		}
		result = append(result, record)
	}
	return result, total, rows.Err()
}
