package httpapi

import (
	"net/http"
	"time"

	"kairisei.local/server/internal/game"
)

func (a *API) localShopQueryCard(w http.ResponseWriter, _ *http.Request) {
	a.writeProtocol(w, a.account.LocalCardPayload(time.Now()))
}

func (a *API) localShopQueryOrder(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Order string `json:"order"`
	}
	if err := decodeExact(r, []string{"order"}, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := game.LocalOrderProduct(input.Order); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := a.account.LocalPurchase(input.Order, time.Now(), a.initialState, a.persistState)
	if err != nil {
		if !a.writeBusinessError(w, err) {
			writeError(w, http.StatusInternalServerError, "local purchase could not be saved")
		}
		return
	}
	a.writeProtocol(w, result)
}

func (a *API) localShopGetCard(w http.ResponseWriter, r *http.Request) {
	var input struct {
		CardType int `json:"cardtype"`
	}
	if err := decodeExact(r, []string{"cardtype"}, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if input.CardType < 0 || input.CardType > 1 {
		writeError(w, http.StatusBadRequest, "unknown card type")
		return
	}
	result, err := a.account.LocalCardClaim(input.CardType, time.Now(), a.initialState, a.persistState)
	if err != nil {
		if !a.writeBusinessError(w, err) {
			writeError(w, http.StatusInternalServerError, "card reward could not be saved")
		}
		return
	}
	a.writeProtocol(w, result)
}
