package accountstore

import (
	"kairisei.local/server/internal/gamestate"
)

// Cross-account consumers only read these card values. Growth tables, text,
// acquisition history and unequipped inventory belong to the private snapshot.
type projectionCard struct {
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

func projectCard(card gamestate.Card) projectionCard {
	return projectionCard{UniqueID: card.UniqueID, CardID: card.CardID, Level: card.Level, Love: card.Love, LoveMax: card.LoveMax, SkillLevels: append([]int16(nil), card.SkillLevels...), HP: card.HP, Attack: card.Attack, Magic: card.Magic, Mind: card.Mind, Fame: card.Fame}
}

func restoreProjectionCards(cards []projectionCard) []gamestate.Card {
	result := make([]gamestate.Card, len(cards))
	for i, card := range cards {
		result[i] = gamestate.Card{UniqueID: card.UniqueID, CardID: card.CardID, Level: card.Level, Love: card.Love, LoveMax: card.LoveMax, SkillLevels: append([]int16(nil), card.SkillLevels...), HP: card.HP, Attack: card.Attack, Magic: card.Magic, Mind: card.Mind, Fame: card.Fame}
	}
	return result
}
