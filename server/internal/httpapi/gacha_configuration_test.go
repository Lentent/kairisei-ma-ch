package httpapi

import (
	"testing"
	"time"

	"kairisei.local/server/internal/release"
)

func TestGachaConfigurationPreservesPlayerStateAndScheduledPaymentFallback(t *testing.T) {
	profiles := []release.GachaProfile{
		{GachaID: 11, GroupID: 1, PayType: 4, PayTypeID: 99, Price: 1, PlayCount: 7, CardNum: 1, CardNumMax: 1, CardIDs: []int{100}, CardWeights: []int{1}},
		{GachaID: 12, GroupID: 1, PayType: 3, Price: 5, CardNum: 1, CardNumMax: 1, CardIDs: []int{100}, CardWeights: []int{1}},
	}
	s := &store{gachas: cloneGachaProfiles(profiles), items: map[int]release.Item{99: {Num: 5}}, gachaDailyClaims: map[int]string{11: "2026-09-06"}, gachaSelections: map[int][]release.Reward{}}
	h := &accountBusinessHandler{api: &API{store: s}}
	configured := profiles[0]
	configured.Price = 2
	configured.CardIDs = []int{200}
	configured.PlayCount = 0
	h.ApplyGachaConfiguration(1, []GachaConfiguration{{Profile: configured, StartUnix: time.Now().Add(time.Hour).Unix()}})
	if s.gachas[0].Price != 2 || s.gachas[0].PlayCount != 7 || s.gachaDailyClaims[11] != "2026-09-06" || s.gachas[0].CardIDs[0] != 200 {
		t.Fatal("operator config overwrote player state or did not update the pool")
	}
	visible := s.visibleGachasLocked()
	if len(visible) != 1 || visible[0].GachaID != 12 {
		t.Fatalf("scheduled ticket hid the usable crystal alternative: %+v", visible)
	}
	if _, err := s.playGacha(11, 4, nil); err == nil || s.items[99].Num != 5 || s.gachas[0].PlayCount != 7 {
		t.Fatal("future pool allowed payment")
	}
	h.ApplyGachaConfiguration(2, []GachaConfiguration{{Profile: configured, EndUnix: time.Now().Add(-time.Hour).Unix()}})
	if s.gachaAvailableForPlayLocked(11) {
		t.Fatal("expired pool allowed payment")
	}
	h.ApplyGachaConfiguration(3, []GachaConfiguration{{Profile: configured}})
	visible = s.visibleGachasLocked()
	if len(visible) != 1 || visible[0].GachaID != 11 {
		t.Fatalf("active ticket did not regain priority: %+v", visible)
	}
	h.ApplyGachaConfiguration(4, []GachaConfiguration{{Profile: configured, Disabled: true}})
	visible = s.visibleGachasLocked()
	if len(visible) != 1 || visible[0].GachaID != 12 || s.gachaAvailableForPlayLocked(11) || s.gachas[0].PlayCount != 7 {
		t.Fatal("disabled single draw did not fall back without changing player count")
	}
}

func TestGachaStepsCommitWithLimitedGifts(t *testing.T) {
	pool := []release.WeightedReward{{Weight: 1, Reward: release.Reward{Type: 8, RewardTypeID: 10, Num: 3, CardSkillLevels: []int16{}}}}
	profile := release.GachaProfile{GachaID: 7, GroupID: 7, PayType: 3, Price: 1, CardNum: 1, CardNumMax: 1,
		RewardPool: pool, Steps: []release.GachaStep{{Price: 1, RewardPool: pool}, {Price: 2, RewardPool: pool}},
		Gifts: []release.GachaGiftRule{{FromPlay: 1, ToPlay: 5, Rewards: []release.Reward{{Type: 8, RewardTypeID: 9, Num: 1, CardSkillLevels: []int16{}}}}}}
	s := &store{gachas: release.CloneGachas([]release.GachaProfile{profile}), items: map[int]release.Item{10: {ItemID: 10, Num: 20}},
		itemDefinitions: map[int]release.ItemDefinition{9: {ItemID: 9, MaxOwned: 5}, 10: {ItemID: 10, MaxOwned: 100}}}
	if _, err := s.playGacha(7, 3, nil); err == nil || s.gachas[0].PlayCount != 0 || s.gold != 0 {
		t.Fatal("failed payment advanced the step")
	}
	s.coinFree = 100
	for play := 1; play <= 6; play++ {
		result, err := s.playGacha(7, 3, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Reward.Rewards) != 1 || len(result.Gifts) != min(1, 6-play) {
			t.Fatal("gift was lost or animated as an extra draw")
		}
		deltas := gachaItemDeltas(result.Reward)
		if len(deltas) != 1 || deltas[0].ItemID != 10 || deltas[0].Num != 3 {
			t.Fatal("client received owned totals or duplicated gift quantities")
		}
		// Simulate a serialization boundary: counters persist, publication rules do not change.
		s.gachas = release.CloneGachas(s.gachas)
	}
	if s.coinFree != 89 || s.items[10].Num != 38 || s.items[9].Num != 5 || s.gachas[0].PlayCount != 6 || profile.PlayCount != 0 {
		t.Fatal("step price, gift limit, or account isolation failed")
	}
	// A full gift inventory must not prevent the paid draw or claim direct delivery.
	s.gachas[0].PlayCount = 0
	result, err := s.playGacha(7, 3, nil)
	if err != nil || s.coinFree != 88 || len(result.Gifts) != 0 || len(result.PresentGifts) != 1 ||
		len(s.presents) != 1 || s.presents[0].Reward.RewardTypeID != 9 || len(result.Reward.Rewards) != 1 || result.Reward.Rewards[0].InPresentBox {
		t.Fatalf("full gift inventory blocked or duplicated draw: %v", err)
	}
}

func TestMixedGachaExpectancyComparesPresentationGrades(t *testing.T) {
	s := &store{
		cardDefinitions:  map[int]release.Card{1: {RarityRank: 5}},
		buddyDefinitions: map[int]release.BuddyDefinition{2: {Rarity: "LEGEND"}},
		sphereDefinitions: map[int]release.SphereDefinition{
			3: {Type: "CHALICE", Rarity: "NORMAL"},
			4: {Type: "CHALICE", Rarity: "MILLIONRARE"},
		},
	}
	card := release.Reward{Type: 6, RewardTypeID: 1, Num: 1}
	buddy := release.Reward{Type: 19, RewardTypeID: 2, Num: 1}
	normal := release.Reward{Type: 15, RewardTypeID: 3, Num: 1}
	rare := release.Reward{Type: 15, RewardTypeID: 4, Num: 1}
	for _, tc := range []struct {
		rewards []release.Reward
		want    int
	}{
		{[]release.Reward{normal}, 8},
		{[]release.Reward{normal, card}, 4},
		{[]release.Reward{card, rare}, 10},
		{[]release.Reward{normal, buddy, rare}, 7},
		{[]release.Reward{rare, buddy, normal}, 7},
	} {
		if got := s.gachaMixedResultExpectancy(tc.rewards); got != tc.want {
			t.Fatalf("mixed expectancy = %d, want %d", got, tc.want)
		}
	}
}
