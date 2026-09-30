package game

import (
	"kairisei.local/server/internal/gamestate"
	"testing"
)

func TestItemGachaCostumeDelivery(t *testing.T) {
	r := gamestate.Reward{Type: 14, RewardTypeID: 16, Num: 1, CardSkillLevels: []int16{}}
	s := &Account{
		items:               map[int]gamestate.Item{9999: {ItemID: 9999, Num: 2}},
		itemDefinitions:     map[int]gamestate.ItemDefinition{9999: {ItemID: 9999, ItemType: "GACHA", Function: "GACHA_EXEC", FunctionValue: 69999999, MaxOwned: 99999}},
		itemGachaProfiles:   map[int]gamestate.ItemGachaProfile{9999: {ItemID: 9999, FunctionValue: 69999999, RewardPool: []gamestate.WeightedReward{{Reward: r, Weight: 1}}}},
		costumeIDs:          map[int]struct{}{},
		collectionRewardIDs: map[[2]int]struct{}{{14, 16}: {}},
	}
	for remaining := 1; remaining >= 0; remaining-- {
		result, err := s.PlayItemGacha(9999, 1)
		if err != nil {
			t.Fatal(err)
		}
		if result.Item.Num != remaining || len(s.costumeIDs) != 1 {
			t.Fatal("wrong box/skin inventory")
		}
		if _, ok := s.costumeIDs[16]; !ok {
			t.Fatal("skin not delivered")
		}
		if len(result.Reward.Rewards) != 1 || result.Reward.Rewards[0].Reward.Type != 14 {
			t.Fatal("missing skin reward response")
		}
	}
	if _, err := s.PlayItemGacha(9999, 1); err == nil {
		t.Fatal("opened empty inventory")
	}
	s.items[9999] = gamestate.Item{ItemID: 9999, Num: 1}
	delete(s.collectionRewardIDs, [2]int{14, 16})
	if _, err := s.PlayItemGacha(9999, 1); err == nil || s.items[9999].Num != 1 {
		t.Fatal("invalid skin consumed a box")
	}
}
