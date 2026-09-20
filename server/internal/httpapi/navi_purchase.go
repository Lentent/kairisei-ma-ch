package httpapi

import (
	"net/http"
)

func (a *API) buyNavi(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		NaviID int8 `json:"navi_id"`
	}
	if err := decodeExact(request, []string{"navi_id"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if err := a.account.PurchaseNavi(payload.NaviID); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, map[string]any{"res": payload.NaviID})
}
