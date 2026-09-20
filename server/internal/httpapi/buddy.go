package httpapi

import (
	"net/http"

	"kairisei.local/server/internal/game"
)

func (a *API) buddyFusion(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		BaseUniqueID int64                   `json:"base_uniqid"`
		Inputs       []game.BuddyFusionInput `json:"add_inputs"`
	}
	if err := decodeExact(request, []string{"base_uniqid", "add_inputs"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	result, err := a.account.FuseBuddy(payload.BaseUniqueID, payload.Inputs)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	backStackCards := toWireStackCards(result.BackStackCards)
	a.writeProtocol(writer, wireBuddyFusion{
		SuccessType: result.SuccessType, BaseBuddy: toWireBuddy(result.Buddy), Gold: result.Gold, BuddyNum: result.BuddyNum,
		Decks: toWireDecks(result.Decks), BackUniqueIDs: result.BackUniqueIDs, BackStackCards: backStackCards,
	})
}

func (a *API) buddyEvolution(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		BaseUniqueID     int64 `json:"base_uniqid"`
		MaterialUniqueID int64 `json:"add_buddy_uniqid"`
		MaterialCardID   int   `json:"add_cardid"`
	}
	if err := decodeExact(request, []string{"base_uniqid", "add_buddy_uniqid", "add_cardid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	result, err := a.account.EvolveBuddy(payload.BaseUniqueID, payload.MaterialUniqueID, payload.MaterialCardID)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, wireBuddyEvolution{
		BaseBuddy: toWireBuddy(result.Buddy), Gold: result.Gold, BuddyNum: result.BuddyNum,
		Decks: toWireDecks(result.Decks), Reward: result.Reward,
	})
}

func (a *API) buddySell(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		UniqueIDs []int64 `json:"uniqids"`
	}
	if err := decodeExact(request, []string{"uniqids"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	getGold, gold, count, decks, err := a.account.SellBuddies(payload.UniqueIDs)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, wireBuddySell{BuddyNum: count, GetGold: getGold, Gold: gold, Decks: toWireDecks(decks)})
}

func (a *API) setBuddyLock(writer http.ResponseWriter, request *http.Request, locked bool) {
	var payload struct {
		UniqueID int64 `json:"uniqid"`
	}
	if err := decodeExact(request, []string{"uniqid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if err := a.account.SetBuddyLock(payload.UniqueID, locked); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, struct{}{})
}

func (a *API) buddyLock(writer http.ResponseWriter, request *http.Request) {
	a.setBuddyLock(writer, request, true)
}

func (a *API) buddyUnlock(writer http.ResponseWriter, request *http.Request) {
	a.setBuddyLock(writer, request, false)
}
