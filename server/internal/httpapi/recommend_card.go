package httpapi

import (
	"net/http"
)

// getRecommendCardInfo restores the original client's 4 x 7 recommendation
// grid from the account's four persisted primary decks. The historical service
// promotion lineup is not present in the official local client data, so the
// local profile deliberately projects only official cards already selected by
// this account and never invents an external acquisition destination.
func (a *API) getRecommendCardInfo(writer http.ResponseWriter, _ *http.Request) {
	cards, haveGet, err := a.account.RecommendCardInfo()
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	a.writeProtocol(writer, map[string]any{
		"cardinfo": cards,
		"haveget":  haveGet,
	})
}
