package admin

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/gamestate"
)

func (admin *API) listAccountMail(writer http.ResponseWriter, request *http.Request) {
	userID, err := strconv.Atoi(chi.URLParam(request, "userID"))
	if err != nil || userID < accountstore.PrimaryUserID || userID >= accountstore.SystemPartnerUserIDBase {
		WriteAdminError(writer, http.StatusBadRequest, "invalid player ID")
		return
	}
	offset, _ := strconv.Atoi(request.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}
	query := strings.ToLower(strings.TrimSpace(request.URL.Query().Get("q")))
	status := request.URL.Query().Get("status")
	lock := admin.business.AccountLock(userID)
	lock.Lock()
	defer lock.Unlock()
	state, err := admin.loadAccountState(userID)
	if err != nil {
		WriteAdminError(writer, http.StatusNotFound, "account not found")
		return
	}
	items := []map[string]any{}
	total := 0
	for i := len(state.Engagement.Presents) - 1; i >= 0; i-- {
		p := state.Engagement.Presents[i]
		if status == "pending" && p.State != 0 || status == "received" && p.State != 1 {
			continue
		}
		entry := admin.catalogByKey[adminCatalogKey(p.Reward.Type, p.Reward.RewardTypeID)]
		if query != "" && !strings.Contains(strings.ToLower(p.Title+" "+p.Comment+" "+entry.Name), query) {
			continue
		}
		total++
		if total <= offset || len(items) >= 50 {
			continue
		}
		// Native present IDs are int64 and can exceed JavaScript's safe integer.
		items = append(items, map[string]any{
			"present_id": strconv.FormatInt(p.PresentID, 10), "title": p.Title,
			"message": p.Comment, "state": p.State, "issued_at_unix": p.IssuedAtUnix,
			"reward_name": entry.Name, "reward_type": p.Reward.Type,
			"reward_id": p.Reward.RewardTypeID, "quantity": p.Reward.Num,
		})
	}
	WriteAdminJSON(writer, http.StatusOK, map[string]any{"items": items, "total": total, "user_id": userID})
}

func (admin *API) deleteAccountMail(writer http.ResponseWriter, request *http.Request) {
	if err := requireAdminMutation(request); err != nil {
		WriteAdminError(writer, http.StatusForbidden, err.Error())
		return
	}
	userID, err := strconv.Atoi(chi.URLParam(request, "userID"))
	if err != nil || userID < accountstore.PrimaryUserID || userID >= accountstore.SystemPartnerUserIDBase {
		WriteAdminError(writer, http.StatusBadRequest, "invalid player ID")
		return
	}
	var body struct {
		PresentIDs []string `json:"present_ids"`
	}
	if err := decodeAdminJSON(request, &body); err != nil || len(body.PresentIDs) == 0 || len(body.PresentIDs) > 100 {
		WriteAdminError(writer, http.StatusBadRequest, "请选择 1–100 封邮件")
		return
	}
	selected := map[int64]bool{}
	for _, value := range body.PresentIDs {
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil || id <= 0 || strconv.FormatInt(id, 10) != value || selected[id] {
			WriteAdminError(writer, http.StatusBadRequest, "邮件 ID 无效或重复")
			return
		}
		selected[id] = true
	}
	// Serialize against gameplay receipt and other admin mutations. Persist the
	// account, deletion tombstones and audit in the existing SQLite transaction.
	lock := admin.business.AccountLock(userID)
	lock.Lock()
	defer lock.Unlock()
	state, err := admin.loadAccountState(userID)
	if err != nil {
		WriteAdminError(writer, http.StatusNotFound, "account not found")
		return
	}
	remaining := make([]gamestate.Present, 0, len(state.Engagement.Presents))
	deleted := []string{}
	pending, received := 0, 0
	for _, p := range state.Engagement.Presents {
		if !selected[p.PresentID] {
			remaining = append(remaining, p)
			continue
		}
		if p.State == 0 {
			pending++
		} else {
			received++
		}
		p.State = gamestate.PresentStateAdminDeleted
		state.Engagement.Histories = append(state.Engagement.Histories, p)
		deleted = append(deleted, strconv.FormatInt(p.PresentID, 10))
	}
	if len(deleted) != 0 {
		state.Engagement.Presents = remaining
		if err := admin.accounts.PersistStateWithAudit(userID, state, &accountstore.AdminAudit{
			Operation: "account-mail-delete", Target: strconv.Itoa(userID),
			Payload: map[string]any{"present_ids": deleted, "unclaimed": pending, "received": received, "rewards_reclaimed": false},
		}); err != nil {
			WriteAdminError(writer, http.StatusInternalServerError, err.Error())
			return
		}
		admin.business.Invalidate(userID)
	}
	// IDs no longer in this player's inbox are harmless retries. They must not
	// resolve to another account or trigger a second reward/audit.
	WriteAdminJSON(writer, http.StatusOK, map[string]any{
		"state": "PASS", "deleted": deleted, "unclaimed": pending, "received": received,
		"already_absent": len(selected) - len(deleted),
	})
}
