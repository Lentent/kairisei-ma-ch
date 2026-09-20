package game

type UrCardIDFromGacha struct {
	CardID  int `json:"CardID"`
	GachaID int `json:"GaChaID"`
	LineID  int `json:"LineID"`
}

func (s *Account) UncollectedCurrentGachaCards(published func(int) bool) []UrCardIDFromGacha {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]UrCardIDFromGacha, 0)
	seen := make(map[int]struct{})
	for _, gacha := range s.VisibleGachasLocked() {
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
			result = append(result, UrCardIDFromGacha{
				CardID:  cardID,
				GachaID: gacha.GachaID,
				LineID:  0,
			})
		}
	}
	return result
}
