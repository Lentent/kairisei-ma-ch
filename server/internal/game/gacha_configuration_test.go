package game

import (
	"testing"
	"time"

	"kairisei.local/server/internal/gamestate"
)

func TestGachaConfigurationPreservesPlayerStateAndScheduledPaymentFallback(t *testing.T) {
	profiles := []gamestate.GachaProfile{
		{GachaID: 11, GroupID: 1, PayType: 4, PayTypeID: 99, Price: 1, PlayCount: 7, CardNum: 1, CardNumMax: 1, CardIDs: []int{100}, CardWeights: []int{1}},
		{GachaID: 12, GroupID: 1, PayType: 3, Price: 5, CardNum: 1, CardNumMax: 1, CardIDs: []int{100}, CardWeights: []int{1}},
	}
	s := &Account{gachas: CloneGachaProfiles(profiles), items: map[int]gamestate.Item{99: {Num: 5}}, gachaDailyClaims: map[int]string{11: "2026-09-06"}, gachaSelections: map[int][]gamestate.Reward{}}
	configured := profiles[0]
	configured.Price = 2
	configured.CardIDs = []int{200}
	configured.PlayCount = 0
	s.ApplyGachaConfiguration(1, []GachaConfiguration{{Profile: configured, StartUnix: time.Now().Add(time.Hour).Unix()}})
	if s.gachas[0].Price != 2 || s.gachas[0].PlayCount != 7 || s.gachaDailyClaims[11] != "2026-09-06" || s.gachas[0].CardIDs[0] != 200 {
		t.Fatal("operator config overwrote player state or did not update the pool")
	}
	visible := s.VisibleGachasLocked()
	if len(visible) != 1 || visible[0].GachaID != 12 {
		t.Fatalf("scheduled ticket hid the usable crystal alternative: %+v", visible)
	}
	if _, err := s.PlayGacha(11, 4, nil); err == nil || s.items[99].Num != 5 || s.gachas[0].PlayCount != 7 {
		t.Fatal("future pool allowed payment")
	}
	s.ApplyGachaConfiguration(2, []GachaConfiguration{{Profile: configured, EndUnix: time.Now().Add(-time.Hour).Unix()}})
	if s.GachaAvailableForPlayLocked(11) {
		t.Fatal("expired pool allowed payment")
	}
	s.ApplyGachaConfiguration(3, []GachaConfiguration{{Profile: configured}})
	visible = s.VisibleGachasLocked()
	if len(visible) != 1 || visible[0].GachaID != 11 {
		t.Fatalf("active ticket did not regain priority: %+v", visible)
	}
	s.ApplyGachaConfiguration(4, []GachaConfiguration{{Profile: configured, Disabled: true}})
	visible = s.VisibleGachasLocked()
	if len(visible) != 1 || visible[0].GachaID != 12 || s.GachaAvailableForPlayLocked(11) || s.gachas[0].PlayCount != 7 {
		t.Fatal("disabled single draw did not fall back without changing player count")
	}
}

func TestGachaStepsCommitWithLimitedGifts(t *testing.T) {
	pool := []gamestate.WeightedReward{{Weight: 1, Reward: gamestate.Reward{Type: 8, RewardTypeID: 10, Num: 3, CardSkillLevels: []int16{}}}}
	profile := gamestate.GachaProfile{GachaID: 7, GroupID: 7, PayType: 3, Price: 1, CardNum: 1, CardNumMax: 1,
		RewardPool: pool, Steps: []gamestate.GachaStep{{Price: 1, RewardPool: pool}, {Price: 2, RewardPool: pool}},
		Gifts: []gamestate.GachaGiftRule{{FromPlay: 1, ToPlay: 5, Rewards: []gamestate.Reward{{Type: 8, RewardTypeID: 9, Num: 1, CardSkillLevels: []int16{}}}}}}
	s := &Account{gachas: gamestate.CloneGachas([]gamestate.GachaProfile{profile}), items: map[int]gamestate.Item{10: {ItemID: 10, Num: 20}},
		itemDefinitions: map[int]gamestate.ItemDefinition{9: {ItemID: 9, MaxOwned: 5}, 10: {ItemID: 10, MaxOwned: 100}}}
	if _, err := s.PlayGacha(7, 3, nil); err == nil || s.gachas[0].PlayCount != 0 || s.gold != 0 {
		t.Fatal("failed payment advanced the step")
	}
	s.coinFree = 100
	for play := 1; play <= 6; play++ {
		result, err := s.PlayGacha(7, 3, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Reward.Rewards) != 1 || len(result.Gifts) != min(1, 6-play) {
			t.Fatal("gift was lost or animated as an extra draw")
		}
		// Simulate a serialization boundary: counters persist, publication rules do not change.
		s.gachas = gamestate.CloneGachas(s.gachas)
	}
	if s.coinFree != 89 || s.items[10].Num != 38 || s.items[9].Num != 5 || s.gachas[0].PlayCount != 6 || profile.PlayCount != 0 {
		t.Fatal("step price, gift limit, or account isolation failed")
	}
	// A full gift inventory must not prevent the paid draw or claim direct delivery.
	s.gachas[0].PlayCount = 0
	result, err := s.PlayGacha(7, 3, nil)
	if err != nil || s.coinFree != 88 || len(result.Gifts) != 0 || len(result.PresentGifts) != 1 ||
		len(s.presents) != 1 || s.presents[0].Reward.RewardTypeID != 9 || len(result.Reward.Rewards) != 1 || result.Reward.Rewards[0].InPresentBox {
		t.Fatalf("full gift inventory blocked or duplicated draw: %v", err)
	}
	// Operator switches this pool to a named item. Insufficient items cannot
	// charge crystals or advance the stage, and a valid draw spends that item only.
	configured := profile
	configured.PayType, configured.PayTypeID = 4, 4000
	s.ApplyGachaConfiguration(1, []GachaConfiguration{{Profile: configured}})
	if _, err := s.PlayGacha(7, 3, nil); err == nil || s.coinFree != 88 {
		t.Fatal("old payment type charged after configuration changed")
	}
	if _, err := s.PlayGacha(7, 4, nil); err == nil || s.coinFree != 88 || s.gachas[0].PlayCount != 1 {
		t.Fatal("insufficient configured item spent money or advanced the step")
	}
	s.items[4000] = gamestate.Item{ItemID: 4000, Num: 10}
	if _, err := s.PlayGacha(7, 4, nil); err != nil || s.items[4000].Num != 8 || s.coinFree != 88 || s.gachas[0].PlayCount != 2 {
		t.Fatal("configured item or step price was not charged correctly", err)
	}
}

func TestMixedGachaExpectancyComparesPresentationGrades(t *testing.T) {
	s := &Account{
		cardDefinitions:  map[int]gamestate.Card{1: {RarityRank: 5}},
		buddyDefinitions: map[int]gamestate.BuddyDefinition{2: {Rarity: "LEGEND"}},
		sphereDefinitions: map[int]gamestate.SphereDefinition{
			3: {Type: "CHALICE", Rarity: "NORMAL"},
			4: {Type: "CHALICE", Rarity: "MILLIONRARE"},
		},
	}
	card := gamestate.Reward{Type: 6, RewardTypeID: 1, Num: 1}
	buddy := gamestate.Reward{Type: 19, RewardTypeID: 2, Num: 1}
	normal := gamestate.Reward{Type: 15, RewardTypeID: 3, Num: 1}
	rare := gamestate.Reward{Type: 15, RewardTypeID: 4, Num: 1}
	for _, tc := range []struct {
		rewards []gamestate.Reward
		want    int
	}{
		{[]gamestate.Reward{normal}, 8},
		{[]gamestate.Reward{normal, card}, 4},
		{[]gamestate.Reward{card, rare}, 10},
		{[]gamestate.Reward{normal, buddy, rare}, 7},
		{[]gamestate.Reward{rare, buddy, normal}, 7},
	} {
		if got := s.gachaMixedResultExpectancy(tc.rewards); got != tc.want {
			t.Fatalf("mixed expectancy = %d, want %d", got, tc.want)
		}
	}
}
