package cnbootstrap

import (
	"database/sql"
	"fmt"
	"slices"
)

var cnFixedStateTableColumns = map[string][]string{
	"cn_account_balance":        {"user_id", "gold", "coin", "coin_free", "friend_point", "pvp_point"},
	"cn_account_card":           {"user_id", "unique_id", "slot", "card_id", "level", "experience", "now_exp", "next_exp", "love", "fame", "is_lock", "hp", "attack", "magic", "mind", "base_add_price", "skill_levels", "sort_order", "row_sha256"},
	"cn_account_item":           {"user_id", "item_id", "num", "limit_time", "sort_order", "row_sha256"},
	"cn_account_stack_card":     {"user_id", "card_id", "num", "hp", "attack", "magic", "mind", "add_exp", "base_add_price", "material_type", "sort_order", "row_sha256"},
	"cn_account_present":        {"user_id", "present_id", "history", "issued_at_unix", "add_elapsed_sec", "limit_time", "state", "reason", "title", "comment", "url", "auto_fusion_used", "auto_loveup_used", "admin_key", "rewards_json", "sort_order", "row_sha256"},
	"cn_save_snapshot":          {"singleton", "schema_version", "revision", "updated_utc", "payload_json", "payload_sha256"},
	"cn_local_account":          {"user_id", "login_uuid", "session_key", "created_utc", "last_login_utc"},
	"cn_account_snapshot":       {"user_id", "schema_version", "revision", "updated_utc", "payload_json", "payload_sha256"},
	"cn_account_projection":     {"user_id", "schema_version", "snapshot_revision", "updated_utc", "payload_json", "payload_sha256"},
	"cn_multiplayer_runtime":    {"singleton", "next_room_id"},
	"cn_multiplayer_completion": {"room_id", "expires_unix", "payload_json", "payload_sha256"},
	"cn_global_operation":       {"operation_key", "revision", "updated_utc", "payload_json", "payload_sha256"},
	"cn_admin_audit":            {"audit_id", "created_utc", "operation", "target", "payload_json", "payload_sha256"},
	"cn_friend_point_rental":    {"event_id", "event_key", "owner_user_id", "renter_user_id", "boss_id", "friend_point", "created_utc"},
	"cn_local_account_follow":   {"follower_user_id", "followed_user_id", "created_utc"},
}

func validateFixedCNStateSchema(transaction *sql.Tx) error {
	for table, expectedColumns := range cnFixedStateTableColumns {
		rows, err := transaction.Query(`PRAGMA table_info(` + table + `)`)
		if err != nil {
			return fmt.Errorf("inspect CN state table %s: %w", table, err)
		}
		actualColumns := make([]string, 0, len(expectedColumns))
		for rows.Next() {
			var columnID int
			var name string
			var dataType string
			var notNull int
			var defaultValue any
			var primaryKey int
			if err := rows.Scan(
				&columnID,
				&name,
				&dataType,
				&notNull,
				&defaultValue,
				&primaryKey,
			); err != nil {
				_ = rows.Close()
				return fmt.Errorf("scan CN state table %s: %w", table, err)
			}
			actualColumns = append(actualColumns, name)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return fmt.Errorf("iterate CN state table %s: %w", table, err)
		}
		if err := rows.Close(); err != nil {
			return fmt.Errorf("close CN state table %s inspection: %w", table, err)
		}
		if !slices.Equal(actualColumns, expectedColumns) {
			return fmt.Errorf(
				"CN state table %s columns are %v, want %v",
				table,
				actualColumns,
				expectedColumns,
			)
		}
	}
	for _, index := range []string{
		"cn_friend_point_rental_owner_event",
		"cn_local_account_follow_inbound",
	} {
		var count int
		if err := transaction.QueryRow(
			`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?`,
			index,
		).Scan(&count); err != nil {
			return fmt.Errorf("inspect CN state index %s: %w", index, err)
		}
		if count != 1 {
			return fmt.Errorf("CN state index %s is missing", index)
		}
	}
	return nil
}
