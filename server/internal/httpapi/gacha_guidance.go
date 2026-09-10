package httpapi

import "net/http"

type urCardIDFromGacha struct {
	CardID  int `json:"CardID"`
	GachaID int `json:"GaChaID"`
	LineID  int `json:"LineID"`
}

// getURCardNoGetFromCurrentGaCha restores the battle-failure recommendation
// query from the current local gacha publication and the account's durable
// discovery history. The CN 6.0.2 client consumes only CardID; GaChaID is kept
// as the first visible source pool and LineID remains zero because the local
// gacha profile has no independently sourced historical lineup identifier.
func (a *API) getURCardNoGetFromCurrentGaCha(writer http.ResponseWriter, request *http.Request) {
	a.writeProtocol(writer, map[string]any{
		"URCardList": a.store.uncollectedCurrentGachaCards(func(id int) bool { return gachaPublished(request, id) }),
	})
}

func (s *store) uncollectedCurrentGachaCards(published func(int) bool) []urCardIDFromGacha {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]urCardIDFromGacha, 0)
	seen := make(map[int]struct{})
	for _, gacha := range s.visibleGachasLocked() {
		if !published(gacha.GachaID) {
			continue
		}
		for _, cardID := range gacha.CardIDs {
			if _, collected := s.cardCollectionIDs[cardID]; collected {
				continue
			}
			if _, duplicate := seen[cardID]; duplicate {
				continue
			}
			seen[cardID] = struct{}{}
			result = append(result, urCardIDFromGacha{
				CardID:  cardID,
				GachaID: gacha.GachaID,
				LineID:  0,
			})
		}
	}
	return result
}
