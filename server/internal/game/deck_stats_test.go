package game

import (
	"testing"

	"kairisei.local/server/internal/gamestate"
)

// CN native leader-card parameter contract.
func TestPartnerDeckStatsIncludesLeaderBonus(t *testing.T) {
	cards := map[int64]CardInfo{}
	deck := DeckInfo{LeaderCardIndex: 0}
	for i := int64(1); i <= 10; i++ {
		cards[i] = CardInfo{HP: 100, Attack: 100, Magic: 100, Mind: 100}
		deck.CardUniqueIDs = append(deck.CardUniqueIDs, i)
	}
	hp, atk, mag, mind := PartnerDeckStats(deck, cards, gamestate.JobParameter{})
	if hp != 1050 || atk != 1050 || mag != 1050 || mind != 1050 {
		t.Fatalf("10 cards each stat 100, leader multiplier 1.5: got HP/ATK/MAG/MIND=%d/%d/%d/%d; want 1050 each", hp, atk, mag, mind)
	}
	deck.SupportCardUniqueIDs = []int64{11}
	for _, sample := range []struct {
		love int
		want [4]int
	}{
		{0, [4]int{6, 20, 20, 10}},
		{50, [4]int{33, 110, 110, 55}},
		{100, [4]int{60, 200, 200, 100}},
	} {
		cards[11] = CardInfo{HP: 100, Attack: 100, Magic: 100, Mind: 100, Love: sample.love, LoveMax: 100}
		hp, atk, mag, mind = PartnerDeckStats(deck, cards, gamestate.JobParameter{})
		if got := [4]int{hp - 1050, atk - 1050, mag - 1050, mind - 1050}; got != sample.want {
			t.Fatalf("support love=%d: got %v, want %v", sample.love, got, sample.want)
		}
	}
}
