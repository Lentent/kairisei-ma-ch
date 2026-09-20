package httpapi

import (
	"testing"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

func TestTeamBattlePartnerDeckWireKeepsLockedSupportSlotsNonEmpty(t *testing.T) {
	deck := game.DeckInfo{
		ArthurType:           1,
		Index:                0,
		JobType:              1,
		CardUniqueIDs:        []int64{1},
		SupportCardUniqueIDs: []int64{0, 0, 0},
		SphereUniqueIDs:      []int64{0, 0, 0},
		BuddyUniqueIDs:       []int64{0, 0, 0, 0, 0},
		Name:                 "test",
	}
	cards := map[int64]game.CardInfo{
		1: {
			UniqueID:    1,
			CardID:      10000001,
			Level:       1,
			SkillLevels: []int16{1},
			Fame:        1,
		},
	}

	wire, err := teamBattlePartnerDeckWire(
		1000001,
		1,
		deck,
		cards,
		gamestate.JobParameter{},
		gamestate.Avatar{AvatarPartIDs: []int{0, 0, 0, 0, 0, 0, 0}},
		map[int64]gamestate.Sphere{},
		map[int64]gamestate.Buddy{},
		0,
		0,
	)
	if err != nil {
		t.Fatalf("project partner deck: %v", err)
	}

	support, ok := wire["support_deck"].([]any)
	if !ok {
		t.Fatalf("support_deck type = %T, want []any", wire["support_deck"])
	}
	if len(support) != len(deck.SupportCardUniqueIDs) {
		t.Fatalf("support_deck slots = %d, want %d", len(support), len(deck.SupportCardUniqueIDs))
	}
	if got := wire["support_card_unlock_slot_num"]; got != int8(0) {
		t.Fatalf("support unlock count = %v, want 0", got)
	}
	for index, value := range support {
		card, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("support_deck[%d] type = %T, want map[string]any", index, value)
		}
		if got := card["cardid"]; got != 0 {
			t.Fatalf("support_deck[%d].cardid = %v, want 0", index, got)
		}
	}
}
