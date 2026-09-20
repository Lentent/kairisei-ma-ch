package admin

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"kairisei.local/server/internal/accountstore"
)

func (admin *API) setAccountCredentials(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	userID, err := strconv.Atoi(chi.URLParam(r, "userID"))
	if err != nil || userID < accountstore.PrimaryUserID || userID >= accountstore.SystemPartnerUserIDBase {
		WriteAdminError(w, 400, "请选择玩家账号")
		return
	}
	var payload struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := DecodeAdminJSONLimit(r, &payload, 2048); err != nil {
		WriteAdminError(w, 400, "请求格式无效")
		return
	}
	name, err := accountstore.NormalizeUsername(payload.Username)
	if err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	salt, hash, err := accountstore.PasswordHash(payload.Password)
	if err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	if err := admin.accounts.WriteCredentials(userID, name, salt, hash, true); err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "user_id": userID, "username": name})
}

func (admin *API) unbindAccount(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	userID, err := strconv.Atoi(chi.URLParam(r, "userID"))
	if err != nil || userID < accountstore.PrimaryUserID || userID >= accountstore.SystemPartnerUserIDBase {
		WriteAdminError(w, 400, "请选择玩家账号")
		return
	}
	var payload struct {
		Username string `json:"username"`
	}
	if err := DecodeAdminJSONLimit(r, &payload, 2048); err != nil {
		WriteAdminError(w, 400, "请求格式无效")
		return
	}
	name, err := accountstore.NormalizeUsername(payload.Username)
	if err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	if err := admin.accounts.UnbindAccount(userID, name); err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "user_id": userID, "username": ""})
}
