package httpapi

import (
	"net/http"
)

// getURCardNoGetFromCurrentGaCha restores the battle-failure recommendation
// query from the current local gacha publication and the account's durable
// discovery history. The CN 6.0.2 client consumes only CardID; GaChaID is kept
// as the first visible source pool and LineID remains zero because the local
// gacha profile has no independently sourced historical lineup identifier.
func (a *API) getURCardNoGetFromCurrentGaCha(writer http.ResponseWriter, request *http.Request) {
	a.writeProtocol(writer, map[string]any{
		"URCardList": a.account.UncollectedCurrentGachaCards(func(id int) bool { return gachaPublished(request, id) }),
	})
}
