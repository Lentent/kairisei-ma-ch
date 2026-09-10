package cnbootstrap

import "kairisei.local/server/internal/release"

// Cross-account consumers only read these card values. Growth tables, text,
// acquisition history and unequipped inventory belong to the private snapshot.
type cnProjectionCard struct {
	UniqueID    int64   `json:"uniqid"`
	CardID      int     `json:"cardid"`
	Level       int     `json:"lv"`
	Love        int     `json:"love"`
	LoveMax     int     `json:"love_max"`
	SkillLevels []int16 `json:"skill_lv"`
	HP          int     `json:"hp"`
	Attack      int     `json:"atkp"`
	Magic       int     `json:"intp"`
	Mind        int     `json:"mndp"`
	Fame        int     `json:"fame"`
}

func projectCNCard(card release.Card) cnProjectionCard {
	return cnProjectionCard{UniqueID: card.UniqueID, CardID: card.CardID, Level: card.Level, Love: card.Love, LoveMax: card.LoveMax, SkillLevels: append([]int16(nil), card.SkillLevels...), HP: card.HP, Attack: card.Attack, Magic: card.Magic, Mind: card.Mind, Fame: card.Fame}
}

func restoreCNProjectionCards(cards []cnProjectionCard) []release.Card {
	result := make([]release.Card, len(cards))
	for i, card := range cards {
		result[i] = release.Card{UniqueID: card.UniqueID, CardID: card.CardID, Level: card.Level, Love: card.Love, LoveMax: card.LoveMax, SkillLevels: append([]int16(nil), card.SkillLevels...), HP: card.HP, Attack: card.Attack, Magic: card.Magic, Mind: card.Mind, Fame: card.Fame}
	}
	return result
}
