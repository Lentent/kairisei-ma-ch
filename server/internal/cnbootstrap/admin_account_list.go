package cnbootstrap

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

func cnAdminPage(r *http.Request, defaultLimit, maximum int) (int, int, error) {
	limit, offset := defaultLimit, 0
	for key, target := range map[string]*int{"limit": &limit, "offset": &offset} {
		if raw := r.URL.Query().Get(key); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil {
				return 0, 0, fmt.Errorf("%s 无效", key)
			}
			*target = value
		}
	}
	if limit < 1 || limit > maximum || offset < 0 {
		return 0, 0, fmt.Errorf("分页须为1–%d项，偏移量不能为负", maximum)
	}
	return limit, offset, nil
}

func (admin *cnAdmin) loadAccounts() ([]cnAdminAccountListItem, error) {
	rows, _, err := admin.queryAccounts("", true, 0, 0)
	return rows, err
}

func (admin *cnAdmin) queryAccounts(search string, includeSystem bool, limit, offset int, ids ...int) ([]cnAdminAccountListItem, int, error) {
	db, err := admin.accounts.storage.open()
	if err != nil {
		return nil, 0, err
	}
	defer db.Close()
	// The compact public projection has the display name; never read inventory
	// snapshots to render a list. SQLite parameters keep searches literal.
	from := ` FROM cn_local_account a
		LEFT JOIN cn_account_credentials c ON c.user_id=a.user_id
		LEFT JOIN cn_account_projection p ON p.user_id=a.user_id
		LEFT JOIN cn_save_snapshot s ON s.singleton=1 AND a.user_id=?
		LEFT JOIN cn_account_snapshot x ON x.user_id=a.user_id`
	name := `COALESCE(json_extract(CAST(p.payload_json AS TEXT),'$.user.name'),'')`
	where := ` WHERE (? OR a.user_id<?) AND (?='' OR instr(lower(CAST(a.user_id AS TEXT)||' '||a.login_uuid||' '||COALESCE(c.username,'')||' '||` + name + `),?)>0)`
	q := strings.ToLower(strings.TrimSpace(search))
	args := []any{cnPrimaryUserID, includeSystem, cnSystemPartnerUserIDBase, q, q}
	if len(ids) > 0 {
		where += ` AND a.user_id IN (` + strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + `)`
		for _, id := range ids {
			args = append(args, id)
		}
	}
	var total int
	if err := db.QueryRow(`SELECT count(*)`+from+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	query := `SELECT a.user_id,a.login_uuid,COALESCE(c.username,''),` + name + `,a.created_utc,a.last_login_utc,COALESCE(s.revision,x.revision,0),COALESCE(s.updated_utc,x.updated_utc,'')` + from + where + ` ORDER BY a.user_id`
	if limit > 0 {
		query += ` LIMIT ? OFFSET ?`
		args = append(args, limit, offset)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	result := []cnAdminAccountListItem{}
	for rows.Next() {
		var a cnAdminAccountListItem
		if err := rows.Scan(&a.UserID, &a.LoginUUID, &a.Username, &a.Name, &a.CreatedUTC, &a.LastLoginUTC, &a.Revision, &a.UpdatedUTC); err != nil {
			return nil, 0, err
		}
		result = append(result, a)
	}
	return result, total, rows.Err()
}

func (admin *cnAdmin) resolveAccounts(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UserIDs []int `json:"user_ids"`
	}
	if err := decodeCNAdminJSON(r, &body); err != nil || len(body.UserIDs) < 1 || len(body.UserIDs) > 500 {
		writeCNAdminError(w, 400, "每次查询1–500名玩家")
		return
	}
	rows, _, err := admin.queryAccounts("", false, 500, 0, body.UserIDs...)
	if err != nil {
		writeCNAdminError(w, 500, err.Error())
		return
	}
	writeCNAdminJSON(w, 200, map[string]any{"state": "PASS", "accounts": rows})
}

func (admin *cnAdmin) accountList(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := cnAdminPage(r, 50, 200)
	if err != nil {
		writeCNAdminError(w, 400, err.Error())
		return
	}
	rows, total, err := admin.queryAccounts(r.URL.Query().Get("q"), r.URL.Query().Get("include_system") == "1", limit, offset)
	if err != nil {
		writeCNAdminError(w, 500, err.Error())
		return
	}
	writeCNAdminJSON(w, 200, map[string]any{"state": "PASS", "accounts": rows, "total": total, "limit": limit, "offset": offset})
}
