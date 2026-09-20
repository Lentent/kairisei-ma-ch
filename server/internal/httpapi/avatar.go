package httpapi

import (
	"net/http"

	"kairisei.local/server/internal/game"
)

type wireAvatarPartInfo struct {
	PartID int `json:"0"`
}

type wireAvatarPartsDeckInfo struct {
	ArthurType    int8  `json:"0"`
	Index         int8  `json:"1"`
	AvatarPartIDs []int `json:"2"`
}

type wireAvatarPartsShow struct {
	Parts   []wireAvatarPartInfo      `json:"0"`
	Decks   []wireAvatarPartsDeckInfo `json:"1"`
	Avatars []wireAvatarInfo          `json:"2"`
}

func (a *API) avatarPartsShow(writer http.ResponseWriter, _ *http.Request) {
	partIDs := a.account.AvatarPartsState()
	parts := make([]wireAvatarPartInfo, len(partIDs))
	for index, partID := range partIDs {
		parts[index] = wireAvatarPartInfo{PartID: partID}
	}
	decks := a.account.AvatarPartsDeckState()
	wireDecks := make([]wireAvatarPartsDeckInfo, len(decks))
	for index, deck := range decks {
		wireDecks[index] = wireAvatarPartsDeckInfo{
			ArthurType: deck.ArthurType, Index: deck.Index,
			AvatarPartIDs: append([]int(nil), deck.AvatarPartIDs...),
		}
	}
	avatars := a.account.AvatarsState()
	wireAvatars := make([]wireAvatarInfo, len(avatars))
	for index, avatar := range avatars {
		wireAvatars[index] = wireAvatarInfo{
			CostumeID: avatar.CostumeID, AvatarPartIDs: append([]int(nil), avatar.AvatarPartIDs...),
		}
	}
	a.writeProtocol(writer, wireAvatarPartsShow{Parts: parts, Decks: wireDecks, Avatars: wireAvatars})
}

func (a *API) avatarPartsDeckSet(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		Decks []game.AvatarPartsDeckInput `json:"avatar_parts_decks"`
	}
	if err := decodeExact(request, []string{"avatar_parts_decks"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if err := a.account.SetAvatarPartsDecks(payload.Decks); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, map[string]any{})
}

func (a *API) avatarShopShow(writer http.ResponseWriter, _ *http.Request) {
	a.writeProtocol(writer, map[string]any{
		"lineup": a.account.AvatarShopState(),
		"user":   a.userPayload(),
	})
}

func (a *API) avatarShopBuy(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		LineupID  int `json:"avatar_shop_lineupid"`
		SaleIndex int `json:"sales_index"`
	}
	if err := decodeExact(request, []string{"avatar_shop_lineupid", "sales_index"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	result, err := a.account.BuyAvatarPart(payload.LineupID, payload.SaleIndex)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	var rewards any
	if len(result.Rewards) != 0 {
		rewards = battleResultRewardsWire(result.Rewards)
	}
	a.writeProtocol(writer, map[string]any{
		"user":           a.userPayload(),
		"items":          a.itemInfosWire(result.UpdatedItems),
		"result_rewards": rewards,
	})
}
