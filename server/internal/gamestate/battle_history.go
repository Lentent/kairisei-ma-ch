package gamestate

import "encoding/json"

// BattleClearDeck is a frozen, public loadout. It contains no account save,
// credentials, inventory unique IDs or mutable card ownership.
type BattleClearDeck struct {
	UserID     int             `json:"user_id"`
	Name       string          `json:"name"`
	ArthurType int             `json:"arthur_type"`
	HonorIDs   []int           `json:"honor_ids"`
	Deck       json.RawMessage `json:"deck"`
}

type BattleClearRecord struct {
	BossID      int
	UserID      int
	Mode        int // 0 solo, 1 multiplayer.
	ArthurType  int // Solo aggregates all professions under zero.
	Count       int
	CompletedAt int64
	EventKey    string
	Decks       []BattleClearDeck // Solo's controlled profession comes first.
}

func CloneBattleClearDecks(source []BattleClearDeck) []BattleClearDeck {
	if source == nil {
		return nil
	}
	result := append([]BattleClearDeck{}, source...)
	for i := range result {
		result[i].HonorIDs = append([]int{}, source[i].HonorIDs...)
		result[i].Deck = append(json.RawMessage(nil), source[i].Deck...)
	}
	return result
}

func BattleClearDay(unix int64) int64 { return (unix + 8*3600) / 86400 }
