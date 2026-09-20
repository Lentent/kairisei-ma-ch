package httpapi

import (
	"net/http"

	"kairisei.local/server/internal/game"
)

func (a *API) sphereFusion(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		BaseUniqueID int64                    `json:"base_uniqid"`
		Inputs       []game.SphereFusionInput `json:"add_inputs"`
	}
	if err := decodeExact(request, []string{"base_uniqid", "add_inputs"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	base, successType, gold, count, decks, err := a.account.FuseSphere(payload.BaseUniqueID, payload.Inputs)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, wireSphereFusion{
		SuccessType: successType, BaseSphere: toWireSphere(base), Gold: gold, SphereNum: count,
		Decks: toWireDecks(decks), BackUniqueIDs: []int64{}, BackStackCards: []wireCardStackInfo{},
	})
}

func (a *API) sphereEvolution(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		BaseUniqueID     int64 `json:"base_uniqid"`
		MaterialUniqueID int64 `json:"add_uniqid"`
		MaterialCardID   int   `json:"add_cardid"`
	}
	if err := decodeExact(request, []string{"base_uniqid", "add_uniqid", "add_cardid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	base, gold, count, decks, err := a.account.EvolveSphere(
		payload.BaseUniqueID, payload.MaterialUniqueID, payload.MaterialCardID,
	)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, wireSphereEvolution{
		BaseSphere: toWireSphere(base), Gold: gold, SphereNum: count,
		Decks: toWireDecks(decks), Reward: nil,
	})
}

func (a *API) sphereSell(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		UniqueIDs []int64 `json:"uniqids"`
	}
	if err := decodeExact(request, []string{"uniqids"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	getGold, gold, count, decks, err := a.account.SellSpheres(payload.UniqueIDs)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, wireSphereSell{
		SphereNum: count, GetGold: getGold, Gold: gold, Decks: toWireDecks(decks),
	})
}

func (a *API) setSphereLock(writer http.ResponseWriter, request *http.Request, locked bool) {
	var payload struct {
		UniqueID int64 `json:"uniqid"`
	}
	if err := decodeExact(request, []string{"uniqid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if err := a.account.SetSphereLock(payload.UniqueID, locked); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, struct{}{})
}

func (a *API) sphereLock(writer http.ResponseWriter, request *http.Request) {
	a.setSphereLock(writer, request, true)
}

func (a *API) sphereUnlock(writer http.ResponseWriter, request *http.Request) {
	a.setSphereLock(writer, request, false)
}
