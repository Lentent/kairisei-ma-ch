package game

import (
	"kairisei.local/server/internal/gamestate"
	"testing"
)

func TestItemGachaHonorDelivery(t *testing.T) {
	r := gamestate.Reward{Type: 18, RewardTypeID: 26093002, Num: 1, CardSkillLevels: []int16{}}
	s := &Account{
		items:               map[int]gamestate.Item{9999: {ItemID: 9999, Num: 2}},
		itemDefinitions:     map[int]gamestate.ItemDefinition{9999: {ItemID: 9999, ItemType: "GACHA", Function: "GACHA_EXEC", FunctionValue: 69999999, MaxOwned: 99999}},
		itemGachaProfiles:   map[int]gamestate.ItemGachaProfile{9999: {ItemID: 9999, FunctionValue: 69999999, RewardPool: []gamestate.WeightedReward{{Reward: r, Weight: 1}}}},
		honorIDs:            map[int]struct{}{},
		collectionRewardIDs: map[[2]int]struct{}{{18, 26093002}: {}},
	}
	for remaining := 1; remaining >= 0; remaining-- {
		result, err := s.PlayItemGacha(9999, 1)
		if err != nil {
			t.Fatal(err)
		}
		if result.Item.Num != remaining || len(s.honorIDs) != 1 {
			t.Fatal("wrong box/honor inventory")
		}
		if _, ok := s.honorIDs[26093002]; !ok {
			t.Fatal("honor not delivered")
		}
		if len(result.Reward.Rewards) != 1 || result.Reward.Rewards[0].Reward.Type != 18 {
			t.Fatal("missing honor reward response")
		}
	}
	if _, err := s.PlayItemGacha(9999, 1); err == nil {
		t.Fatal("opened empty inventory")
	}
	s.items[9999] = gamestate.Item{ItemID: 9999, Num: 1}
	delete(s.collectionRewardIDs, [2]int{18, 26093002})
	if _, err := s.PlayItemGacha(9999, 1); err == nil || s.items[9999].Num != 1 {
		t.Fatal("invalid honor consumed a box")
	}
}
