package cnbootstrap

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

func (admin *cnAdmin) setAccountCredentials(w http.ResponseWriter, r *http.Request) {
	if err := requireCNAdminMutation(r); err != nil {
		writeCNAdminError(w, 403, err.Error())
		return
	}
	userID, err := strconv.Atoi(chi.URLParam(r, "userID"))
	if err != nil || userID < cnPrimaryUserID || userID >= cnSystemPartnerUserIDBase {
		writeCNAdminError(w, 400, "请选择玩家账号")
		return
	}
	var payload struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeCNAdminJSONLimit(r, &payload, 2048); err != nil {
		writeCNAdminError(w, 400, "请求格式无效")
		return
	}
	name, err := normalizeCNUsername(payload.Username)
	if err != nil {
		writeCNAdminError(w, 400, err.Error())
		return
	}
	salt, hash, err := cnPasswordHash(payload.Password)
	if err != nil {
		writeCNAdminError(w, 400, err.Error())
		return
	}
	if err := admin.accounts.writeCredentials(userID, name, salt, hash, true); err != nil {
		writeCNAdminError(w, 400, err.Error())
		return
	}
	writeCNAdminJSON(w, 200, map[string]any{"state": "PASS", "user_id": userID, "username": name})
}

func (admin *cnAdmin) unbindAccount(w http.ResponseWriter, r *http.Request) {
	if err := requireCNAdminMutation(r); err != nil {
		writeCNAdminError(w, 403, err.Error())
		return
	}
	userID, err := strconv.Atoi(chi.URLParam(r, "userID"))
	if err != nil || userID < cnPrimaryUserID || userID >= cnSystemPartnerUserIDBase {
		writeCNAdminError(w, 400, "请选择玩家账号")
		return
	}
	var payload struct {
		Username string `json:"username"`
	}
	if err := decodeCNAdminJSONLimit(r, &payload, 2048); err != nil {
		writeCNAdminError(w, 400, "请求格式无效")
		return
	}
	name, err := normalizeCNUsername(payload.Username)
	if err != nil {
		writeCNAdminError(w, 400, err.Error())
		return
	}
	if err := admin.accounts.unbindAccount(userID, name); err != nil {
		writeCNAdminError(w, 400, err.Error())
		return
	}
	writeCNAdminJSON(w, 200, map[string]any{"state": "PASS", "user_id": userID, "username": ""})
}
