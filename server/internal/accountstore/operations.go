package accountstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

type AdminAudit struct {
	Operation string
	Target    string
	Payload   any
	Receipt   *AdminActionReceipt
}

// Account changes and their audit record share the snapshot transaction.
func (audit *AdminAudit) appendTo(transaction *sql.Tx, updatedUTC string) error {
	if audit == nil {
		return nil
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
