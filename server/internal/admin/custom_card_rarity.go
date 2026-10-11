package admin

import "errors"

// CardCsvData.RARITY is zero based in the client enum, while the runtime
// master exposes the eight native rarities as ranks 1..8.
var customCardRarityCodes = [...]string{"", "NORMAL", "HIGHNORMAL", "RARE", "SUPERRARE", "ULTRARARE", "MILLIONRARE", "EXRARE", "LEGEND"}

func validateCustomCardRarity(c customCard) error {
	if c.RarityRank != nil && (*c.RarityRank < 1 || *c.RarityRank >= len(customCardRarityCodes)) {
		return errors.New("稀有度须为1–8星，或选择沿用模板")
	}
	return nil
}

func customCardTemplateRarities(s customCardSources, cards []customCard) map[int]int {
	ranks := map[int]int{}
	for _, c := range cards {
		row := s.Cards[c.TemplateID]
		if len(row) <= 7 {
			continue
		}
		for rank := 1; rank < len(customCardRarityCodes); rank++ {
			if row[7] == customCardRarityCodes[rank] {
				ranks[c.TemplateID] = rank
				break
			}
		}
	}
	return ranks
}
