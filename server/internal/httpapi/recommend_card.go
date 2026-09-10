package httpapi

import (
	"errors"
	"net/http"
)

const localRecommendCardsPerArthur = 7

type localRecommendCardInfo struct {
	Type     int    `json:"type"`
	Job      int8   `json:"job"`
	CardID   int    `json:"cardID"`
	Name     string `json:"name"`
	Method   string `json:"method"`
	GoToType string `json:"gototype"`
}

// getRecommendCardInfo restores the original client's 4 x 7 recommendation
// grid from the account's four persisted primary decks. The historical service
// promotion lineup is not present in the official local client data, so the
// local profile deliberately projects only official cards already selected by
// this account and never invents an external acquisition destination.
func (a *API) getRecommendCardInfo(writer http.ResponseWriter, _ *http.Request) {
	cards, haveGet, err := a.store.recommendCardInfo()
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	a.writeProtocol(writer, map[string]any{
		"cardinfo": cards,
		"haveget":  haveGet,
	})
}

func (s *store) recommendCardInfo() ([]localRecommendCardInfo, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cardsByUniqueID := make(map[int64]cardInfo, len(s.cards)+len(s.containerCards))
	for _, group := range [][]cardInfo{s.cards, s.containerCards} {
		for _, card := range group {
			cardsByUniqueID[card.UniqueID] = card
		}
	}

	result := make([]localRecommendCardInfo, 0, 4*localRecommendCardsPerArthur)
	haveGet := 0
	for arthurType := int8(1); arthurType <= 4; arthurType++ {
		var primaryDeck *deckInfo
		for index := range s.decks {
			if s.decks[index].ArthurType == arthurType && s.decks[index].Index == 0 {
				primaryDeck = &s.decks[index]
				break
			}
		}
		if primaryDeck == nil || len(primaryDeck.CardUniqueIDs) < localRecommendCardsPerArthur {
			return nil, 0, errors.New("local primary deck cannot populate the recommendation grid")
		}
		for slot := 0; slot < localRecommendCardsPerArthur; slot++ {
			card, exists := cardsByUniqueID[primaryDeck.CardUniqueIDs[slot]]
			if !exists || card.CardID <= 0 {
				return nil, 0, errors.New("local recommendation deck references an unavailable card")
			}
			bit := len(result)
			if _, discovered := s.cardCollectionIDs[card.CardID]; discovered {
				haveGet |= 1 << bit
			}
			result = append(result, localRecommendCardInfo{
				Type:   0,
				Job:    arthurType,
				CardID: card.CardID,
				// DeckRecListItem resolves the visible name from the official
				// client CardCsvData by cardID; the service field is parsed but
				// not consumed by this client build.
				Name:     "",
				Method:   "当前本地账号卡组",
				GoToType: "none",
			})
		}
	}
	return result, haveGet, nil
}
