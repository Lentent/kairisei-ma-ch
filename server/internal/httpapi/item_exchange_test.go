package httpapi

import (
	"testing"

	"kairisei.local/server/internal/release"
)

func TestItemExchangePreservesEmptyRewardSkills(t *testing.T) {
	s := &store{
		items: map[int]release.Item{10: {ItemID: 10, Num: 3}},
		itemExchangeProfiles: map[int]release.ItemExchangeProfile{10: {
			ItemID: 10, NeedNum: 1,
			Reward: release.Reward{Type: 4, Num: 5, CardSkillLevels: []int16{}},
		}},
	}
	result, err := s.exchangeItem(10, 2)
	if err != nil {
		t.Fatalf("valid currency exchange rejected: %v", err)
	}
	if s.items[10].Num != 1 || s.gold != 10 || len(result.Reward.Rewards) != 1 ||
		result.Reward.Rewards[0].Reward.CardSkillLevels == nil {
		t.Fatalf("exchange inventory, balance or reward shape is incorrect: %+v", result)
	}
	if _, err := s.exchangeItem(10, 2); err == nil || s.items[10].Num != 1 || s.gold != 10 {
		t.Fatal("insufficient inventory changed exchange balances")
	}
}
