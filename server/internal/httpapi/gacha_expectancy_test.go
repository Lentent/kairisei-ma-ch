package httpapi

import (
	"testing"

	"kairisei.local/server/internal/release"
)

func TestGachaExpectancyUsesHighestActualCardRarity(t *testing.T) {
	definitions := map[int]release.Card{
		3: {CardID: 3, RarityRank: 3},
		4: {CardID: 4, RarityRank: 4},
		5: {CardID: 5, RarityRank: 5},
		6: {CardID: 6, RarityRank: 6},
		8: {CardID: 8, RarityRank: 8},
	}
	for _, test := range []struct {
		name  string
		cards []int
		want  int
	}{
		{"three star silver", []int{3}, 2},
		{"tutorial four star gold", []int{4}, 3},
		{"five star rainbow", []int{5}, 4},
		{"multi without five star", []int{3, 4, 3}, 3},
		{"multi with five star", []int{3, 5, 4}, 4},
		{"native higher rarity", []int{6, 8, 3}, 7},
	} {
		t.Run(test.name, func(t *testing.T) {
			rewards := make([]release.Reward, len(test.cards))
			for i, id := range test.cards {
				rewards[i] = gachaCardReward(id)
			}
			if got := gachaResultExpectancy(rewards, definitions); got != test.want {
				t.Fatalf("expectancy=%d, want %d", got, test.want)
			}
		})
	}
}
