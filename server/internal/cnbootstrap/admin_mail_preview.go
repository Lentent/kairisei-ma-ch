package cnbootstrap

import (
	"net/http"
	"sort"
	"strings"
)

func (admin *cnAdmin) mailPreview(writer http.ResponseWriter, request *http.Request) {
	if err := requireCNAdminMutation(request); err != nil {
		writeCNAdminError(writer, 403, err.Error())
		return
	}
	var body struct {
		Mail    cnAdminMailRequest `json:"mail"`
		UserIDs []int              `json:"user_ids"`
	}
	if err := decodeCNAdminJSON(request, &body); err != nil {
		writeCNAdminError(writer, 400, err.Error())
		return
	}
	body.Mail.Title = strings.TrimSpace(body.Mail.Title)
	body.Mail.Message = strings.TrimSpace(body.Mail.Message)
	if len([]rune(body.Mail.Title)) < 1 || len([]rune(body.Mail.Title)) > 40 || len([]rune(body.Mail.Message)) < 1 || len([]rune(body.Mail.Message)) > 200 || len(body.UserIDs) < 1 || len(body.UserIDs) > 10000 {
		writeCNAdminError(writer, 400, "标题、正文或收件名单无效（最多 10000 个账号）")
		return
	}
	reward, entry, err := admin.mailReward(body.Mail)
	if err != nil {
		writeCNAdminError(writer, 400, err.Error())
		return
	}
	accounts, err := admin.loadAccounts()
	if err != nil {
		writeCNAdminError(writer, 500, err.Error())
		return
	}
	known := make(map[int]bool, len(accounts))
	for _, account := range accounts {
		known[account.UserID] = true
	}
	seen := make(map[int]bool)
	users := []int{}
	for _, id := range body.UserIDs {
		if !known[id] {
			writeCNAdminError(writer, 400, "收件名单包含不存在的账号")
			return
		}
		if !seen[id] {
			seen[id] = true
			users = append(users, id)
		}
	}
	sort.Ints(users)
	writeCNAdminJSON(writer, 200, map[string]any{"state": "PASS", "user_ids": users, "mail": body.Mail, "reward": reward, "catalog": entry, "total_quantity": int64(len(users)) * int64(body.Mail.Quantity)})
}
