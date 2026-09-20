package admin

import (
	"net/http"
)

func (admin *API) resolveCatalog(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Rewards []AdminMailRequest `json:"rewards"`
	}
	if err := decodeAdminJSON(r, &body); err != nil || len(body.Rewards) < 1 || len(body.Rewards) > 120 {
		WriteAdminError(w, 400, "每次查询1–120种奖励")
		return
	}
	entries := make([]AdminCatalogEntry, 0, len(body.Rewards))
	for _, request := range body.Rewards {
		_, entry, err := admin.mailReward(request)
		if err != nil {
			WriteAdminError(w, 400, err.Error())
			return
		}
		entries = append(entries, entry)
	}
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "entries": entries})
}
