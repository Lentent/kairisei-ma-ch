package admin

import (
	"github.com/go-chi/chi/v5"
	"kairisei.local/server/internal/accountstore"
	"net/http"
	"strconv"
)

func (admin *API) accountBan(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "userID"))
	if err != nil || id < accountstore.PrimaryUserID || id >= accountstore.SystemPartnerUserIDBase {
		WriteAdminError(w, 400, "无效的玩家 UID")
		return
	}
	if r.Method == http.MethodPost {
		if err := requireAdminMutation(r); err != nil {
			WriteAdminError(w, 403, err.Error())
			return
		}
		var body struct {
			Banned    *bool  `json:"banned"`
			Reason    string `json:"reason"`
			UntilUnix int64  `json:"until_unix"`
		}
		if err := DecodeAdminJSONLimit(r, &body, 4096); err != nil || body.Banned == nil {
			WriteAdminError(w, 400, "请指定封禁或解封状态")
			return
		}
		lock := admin.business.AccountLock(id)
		lock.Lock()
		defer lock.Unlock()
		if err := admin.accounts.SetAccountBan(id, accountstore.AccountBan{Banned: *body.Banned, Reason: body.Reason, UntilUnix: body.UntilUnix}); err != nil {
			WriteAdminError(w, 400, err.Error())
			return
		}
		admin.business.Invalidate(id)
	}
	b, err := admin.accounts.AccountBan(id)
	if err != nil {
		WriteAdminError(w, 500, err.Error())
		return
	}
	WriteAdminJSON(w, 200, b)
}
