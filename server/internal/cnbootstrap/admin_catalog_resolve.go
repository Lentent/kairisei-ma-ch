package cnbootstrap

import "net/http"

func (admin *cnAdmin) resolveCatalog(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Rewards []cnAdminMailRequest `json:"rewards"`
	}
	if err := decodeCNAdminJSON(r, &body); err != nil || len(body.Rewards) < 1 || len(body.Rewards) > 120 {
		writeCNAdminError(w, 400, "每次查询1–120种奖励")
		return
	}
	entries := make([]cnAdminCatalogEntry, 0, len(body.Rewards))
	for _, request := range body.Rewards {
		_, entry, err := admin.mailReward(request)
		if err != nil {
			writeCNAdminError(w, 400, err.Error())
			return
		}
		entries = append(entries, entry)
	}
	writeCNAdminJSON(w, 200, map[string]any{"state": "PASS", "entries": entries})
}
