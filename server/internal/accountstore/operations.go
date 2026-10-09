package accountstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

type AdminAudit struct {
	Operation string
	Target    string
	Payload   any
	Receipt   *AdminActionReceipt
	CDKClaim  *CDKClaim
}

// Account changes and their audit record share the snapshot transaction.
func (audit *AdminAudit) appendTo(transaction *sql.Tx, updatedUTC string) error {
	if audit == nil {
		return nil
	}
	if audit.CDKClaim != nil {
		if audit.Receipt == nil || audit.Receipt.OperationKey != audit.CDKClaim.Key {
			return errors.New("CDK claim requires a matching receipt")
		}
		var used int
		if err := transaction.QueryRow(`SELECT count(*) FROM cn_admin_action_receipt WHERE operation_key=? AND user_id=?`, audit.Receipt.OperationKey, audit.Receipt.UserID).Scan(&used); err != nil {
			return err
		}
		if used > 0 {
			return ErrCDKUsed
		}
		if err := audit.CDKClaim.appendTo(transaction); err != nil {
			return err
		}
	}
	if err := audit.Receipt.appendTo(transaction, updatedUTC); err != nil {
		return err
	}
	content, err := json.Marshal(audit.Payload)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(content)
	_, err = transaction.ExecContext(
		context.Background(),
		`INSERT INTO cn_admin_audit
		 (created_utc, operation, target, payload_json, payload_sha256)
		 VALUES (?, ?, ?, ?, ?)`,
		updatedUTC, audit.Operation, audit.Target, content, hex.EncodeToString(digest[:]),
	)
	if err != nil {
		return fmt.Errorf("append CN admin audit: %w", err)
	}
	return nil
}
