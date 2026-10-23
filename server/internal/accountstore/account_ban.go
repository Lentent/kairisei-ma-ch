package accountstore

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrAccountBanned = errors.New("账号已封禁，请联系管理员")

const accountBanSchema = `CREATE TABLE IF NOT EXISTS cn_account_ban (
 user_id INTEGER PRIMARY KEY REFERENCES cn_local_account(user_id),
 reason TEXT NOT NULL, until_unix INTEGER NOT NULL, updated_utc TEXT NOT NULL)`

type AccountBan struct {
	Banned    bool   `json:"banned"`
	Reason    string `json:"reason"`
	UntilUnix int64  `json:"until_unix"`
}

func (accounts *Accounts) AccountBan(userID int) (AccountBan, error) {
	db, err := accounts.storage.OpenRead()
	if err != nil {
		return AccountBan{}, err
	}
	var b AccountBan
	err = db.QueryRow(`SELECT reason, until_unix FROM cn_account_ban WHERE user_id = ?`, userID).Scan(&b.Reason, &b.UntilUnix)
	if errors.Is(err, sql.ErrNoRows) {
		return b, nil
	}
	if err != nil {
		return b, err
	}
	b.Banned = b.UntilUnix == 0 || b.UntilUnix > time.Now().Unix()
	return b, nil
}
func (accounts *Accounts) CheckAccountAccess(userID int) error {
	b, err := accounts.AccountBan(userID)
	if err != nil {
		return err
	}
	if b.Banned {
		return ErrAccountBanned
	}
	return nil
}

// Ban and session revocation commit atomically; unbanning never restores old keys.
func (accounts *Accounts) SetAccountBan(userID int, b AccountBan) error {
	b.Reason = strings.TrimSpace(b.Reason)
	if userID < PrimaryUserID || userID >= SystemPartnerUserIDBase {
		return errors.New("无效的玩家 UID")
	}
	if b.Banned && (b.Reason == "" || utf8.RuneCountInString(b.Reason) > 500 || b.UntilUnix < 0 || (b.UntilUnix > 0 && b.UntilUnix <= time.Now().Unix())) {
		return errors.New("请填写 1–500 字封禁原因及有效的到期时间")
	}
	db, err := accounts.storage.Open()
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id int
	if err := tx.QueryRow(`SELECT user_id FROM cn_local_account WHERE user_id=?`, userID).Scan(&id); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	op := "account-unban"
	if b.Banned {
		op = "account-ban"
		if _, err = tx.Exec(`INSERT INTO cn_account_ban VALUES (?,?,?,?) ON CONFLICT(user_id) DO UPDATE SET reason=excluded.reason,until_unix=excluded.until_unix,updated_utc=excluded.updated_utc`, userID, b.Reason, b.UntilUnix, now); err != nil {
			return err
		}
		if _, err = tx.Exec(`UPDATE cn_local_account SET session_key=? WHERE user_id=?`, fmt.Sprintf("banned:%d", userID), userID); err != nil {
			return err
		}
	} else {
		if _, err = tx.Exec(`DELETE FROM cn_account_ban WHERE user_id=?`, userID); err != nil {
			return err
		}
	}
	if err = (&AdminAudit{Operation: op, Target: fmt.Sprint(userID), Payload: b}).appendTo(tx, now); err != nil {
		return err
	}
	return tx.Commit()
}
