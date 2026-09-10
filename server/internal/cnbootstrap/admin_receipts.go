package cnbootstrap

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
)

// A receipt is committed in the same transaction as the account and audit.
// It survives gift collection/deletion and browser or process restarts.
type cnAdminActionReceipt struct {
	OperationKey  string
	UserID        int
	RequestSHA256 string
	Result        any
}

func (r *cnAdminActionReceipt) appendTo(tx *sql.Tx, now string) error {
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

func (operations *cnOperationStore) actionReceipt(key string, userID int, requestSHA string) (json.RawMessage, error) {
	db, err := operations.storage.open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	var content []byte
	var recordedRequest, recordedResult string
	err = db.QueryRow(`SELECT request_sha256,result_json,result_sha256 FROM cn_admin_action_receipt WHERE operation_key=? AND user_id=?`, key, userID).Scan(&recordedRequest, &content, &recordedResult)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(content)
	if recordedRequest != requestSHA || hex.EncodeToString(digest[:]) != recordedResult {
		return nil, errors.New("操作编号已对应其他内容，或发放收据不完整")
	}
	return json.RawMessage(content), nil
}
