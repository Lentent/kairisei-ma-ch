package httpapi

import (
	"net/http"

	"kairisei.local/server/internal/game"
)

const cardCapacityBase = 100

func (a *API) coinUse(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		Type  int `json:"type"`
		Param int `json:"param"`
	}
	if err := decodeExact(request, []string{"type", "param"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if payload.Type != game.CoinUseCardExtend && payload.Param != 0 {
		writeError(writer, http.StatusBadRequest, "unsupported coin-use parameter")
		return
	}
	var err error
	if payload.Type == game.CoinUseCardExtend {
		err = a.account.ExtendCardCapacity(payload.Param)
	} else {
		err = a.account.FullHealWithCrystals(payload.Type)
	}
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, map[string]any{
		"type": payload.Type,
		"user": a.userPayload(),
	})
}
