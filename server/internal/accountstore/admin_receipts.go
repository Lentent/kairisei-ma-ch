package accountstore

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
)

// A receipt is committed in the same transaction as the account and audit.
// It survives gift collection/deletion and browser or process restarts.
type AdminActionReceipt struct {
	OperationKey  string
	UserID        int
	RequestSHA256 string
	Result        any
}

func (r *AdminActionReceipt) appendTo(tx *sql.Tx, now string) error {
	if r == nil {
		return nil
	}
	content, err := json.Marshal(r.Result)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(content)
	_, err = tx.Exec(`INSERT INTO cn_admin_action_receipt
		(operation_key,user_id,request_sha256,result_json,result_sha256,created_utc) VALUES (?,?,?,?,?,?)`,
		r.OperationKey, r.UserID, r.RequestSHA256, content, hex.EncodeToString(digest[:]), now)
	return err
}
