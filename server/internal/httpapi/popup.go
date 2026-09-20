package httpapi

import (
	"net/http"
)

func (a *API) popupExec(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		PopupID  int  `json:"popupid"`
		IsSystem int8 `json:"is_system"`
	}
	if err := decodeExact(request, []string{"popupid", "is_system"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if err := a.account.AcknowledgePopup(a.initialState.PopupProfile, payload.PopupID, payload.IsSystem); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, struct{}{})
}
