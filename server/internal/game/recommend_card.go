package game

import (
	"errors"
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

func (s *Account) RecommendCardInfo() ([]localRecommendCardInfo, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cardsByUniqueID := make(map[int64]CardInfo, len(s.cards)+len(s.containerCards))
	for _, group := range [][]CardInfo{s.cards, s.containerCards} {
		for _, card := range group {
			cardsByUniqueID[card.UniqueID] = card
		}
	}

	result := make([]localRecommendCardInfo, 0, 4*localRecommendCardsPerArthur)
	haveGet := 0
	for arthurType := int8(1); arthurType <= 4; arthurType++ {
		var primaryDeck *DeckInfo
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
