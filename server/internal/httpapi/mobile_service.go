package httpapi

import (
	"kairisei.local/server/internal/release"
	"net/http"
)

// The CN archive's information button still sends this legacy GM request.
// A successful URL would invoke an uninitialized official Android GM bridge.
// Use the existing recoverable dialog/return flow without issuing a token,
// accessing the supplied query, or changing the account.
func (a *API) mobileServiceCompatibility(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Sprite string `json:"sprite"`
	}
	if err := decodeExact(r, []string{"sprite"}, &payload); err != nil {
		a.writeStoreError(w, err)
		return
	}
	a.writeProtocolResponse(w, map[string]any{"url": ""}, []release.PopupProfile{}, -1,
		"暂无额外乖离信息，可在卡牌详情中查看进化条件。", 2)
}
