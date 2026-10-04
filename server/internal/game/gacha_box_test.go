package game

import (
	"math"
	"reflect"
	"strings"
	"sync"
	"testing"

	"kairisei.local/server/internal/gamestate"
)

func boxTestProfile() gamestate.GachaProfile {
	p := gamestate.GachaProfile{GachaID: 70000001, GroupID: 70000001, Name: "箱池", PublicationKey: "custom", PayType: 3, Price: 1, CardNum: 1, CardNumMax: 1}
	for round := 1; round <= 11; round++ {
		p.BoxRounds = append(p.BoxRounds, gamestate.GachaBoxRound{Rewards: []gamestate.GachaBoxReward{{Reward: gamestate.Reward{Type: 8, RewardTypeID: 10, Num: round, CardSkillLevels: []int16{}}, Stock: 50}}})
	}
	return p
}

func boxTestAccount() *Account {
	return &Account{gachas: []gamestate.GachaProfile{boxTestProfile()}, coinFree: 10000, items: map[int]gamestate.Item{}, itemDefinitions: map[int]gamestate.ItemDefinition{10: {ItemID: 10, MaxOwned: 1000000}}, gachaBoxes: map[int]gamestate.GachaBoxProgress{}}
}

func TestGachaBoxTenRoundsThenInfiniteRepeat(t *testing.T) {
	s := boxTestAccount()
	for round := 1; round <= 12; round++ {
		for draw := 1; draw <= 50; draw++ {
			before := s.items[10].Num
			result, err := s.PlayGacha(70000001, 3, nil)
			if err != nil {
				t.Fatal(err)
			}
			if s.items[10].Num-before != min(round, 11) {
				t.Fatalf("round %d used wrong template", round)
			}
			wantRound, wantStock := uint64(round), 50-draw
			if draw == 50 {
				wantRound++
				wantStock = 50
			}
			if result.Gachas[0].BoxRound != wantRound || s.gachaBoxes[70000001].Round != wantRound || gamestate.GachaBoxStock(s.gachaBoxes[70000001].Remaining) != wantStock {
				t.Fatalf("round %d draw %d advanced before depletion or did not refill", round, draw)
			}
		}
	}
	// Cross the formerly mentioned 999 boundary; it is not a limit.
	p := s.gachaBoxes[70000001]
	p.Round, p.Remaining[0].Stock = 999, 1
	s.gachaBoxes[70000001] = p
	result, err := s.PlayGacha(70000001, 3, nil)
	if err != nil || result.Gachas[0].BoxRound != 1000 {
		t.Fatal("999 unexpectedly capped box", err)
	}
	// Machine counter exhaustion also must not close the infinite template.
	p = s.gachaBoxes[70000001]
	p.Round, p.Remaining[0].Stock = math.MaxUint64, 1
	s.gachaBoxes[70000001], s.gachas[0].PlayCount = p, math.MaxInt
	if _, err := s.PlayGacha(70000001, 3, nil); err != nil || gamestate.GachaBoxStock(s.gachaBoxes[70000001].Remaining) != 50 {
		t.Fatal("counter saturation stopped replenishment", err)
	}
}

func TestGachaBoxWithoutReplacementAndConcurrentBoundary(t *testing.T) {
	s := boxTestAccount()
	s.gachas[0].BoxRounds[0].Rewards[0].Stock = 49
	s.gachas[0].BoxRounds[0].Rewards = append(s.gachas[0].BoxRounds[0].Rewards, gamestate.GachaBoxReward{Stock: 1, Reward: gamestate.Reward{Type: 8, RewardTypeID: 10, Num: 100, CardSkillLevels: []int16{}}})
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.PlayGacha(70000001, 3, nil); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if s.items[10].Num != 149 || s.coinFree != 9950 || s.gachaBoxes[70000001].Round != 2 || gamestate.GachaBoxStock(s.gachaBoxes[70000001].Remaining) != 50 {
		t.Fatal("parallel draws duplicated stock or charged incorrectly")
	}
}

func TestGachaBoxSnapshotPreservesInventoryWithoutSharing(t *testing.T) {
	s := boxTestAccount()
	if _, err := s.PlayGacha(70000001, 3, nil); err != nil {
		t.Fatal(err)
	}
	snapshot := s.Snapshot(gamestate.State{})
	if snapshot.GachaBoxes[70000001].Round != 1 || snapshot.GachaBoxes[70000001].Remaining[0].Stock != 49 {
		t.Fatal("account snapshot omitted committed stock")
	}
	snapshot.GachaBoxes[70000001].Remaining[0].Stock = 9
	snapshot.Gachas[0].BoxRounds[0].Rewards[0].Stock = 8
	if s.gachaBoxes[70000001].Remaining[0].Stock != 49 || s.gachas[0].BoxRounds[0].Rewards[0].Stock != 50 {
		t.Fatal("snapshot shares mutable inventory/templates")
	}
}

func TestGachaBoxFailedDrawAndRepublishPreserveStock(t *testing.T) {
	s := boxTestAccount()
	if _, err := s.PlayGacha(70000001, 3, nil); err != nil {
		t.Fatal(err)
	}
	before := gamestate.CloneGachaBoxes(s.gachaBoxes)
	coins := s.coinFree
	if _, err := s.PlayGacha(70000001, 4, nil); err == nil || !reflect.DeepEqual(before, s.gachaBoxes) || s.coinFree != coins {
		t.Fatal("failed payment changed stock")
	}
	s.coinFree = 0
	if _, err := s.PlayGacha(70000001, 3, nil); err == nil || !reflect.DeepEqual(before, s.gachaBoxes) {
		t.Fatal("insufficient funds changed stock")
	}
	s.coinFree = coins
	delete(s.itemDefinitions, 10)
	if _, err := s.PlayGacha(70000001, 3, nil); err == nil || !reflect.DeepEqual(before, s.gachaBoxes) || s.coinFree != coins {
		t.Fatal("failed reward validation changed stock or payment")
	}
	s.itemDefinitions[10] = gamestate.ItemDefinition{ItemID: 10, MaxOwned: 1000000}
	p := gamestate.CloneGachas(s.gachas)[0]
	for i := range p.BoxRounds {
		p.BoxRounds[i].Rewards[0].Reward.Num = 20
	}
	s.ApplyGachaConfiguration(1, []GachaConfiguration{{Profile: p}})
	if !reflect.DeepEqual(before, s.gachaBoxes) {
		t.Fatal("republish refilled an active box")
	}
	for i := 0; i < 49; i++ {
		if _, err := s.PlayGacha(70000001, 3, nil); err != nil {
			t.Fatal(err)
		}
	}
	if s.items[10].Num != 50 || s.gachaBoxes[70000001].Round != 2 || s.gachaBoxes[70000001].Remaining[0].Reward.Num != 20 {
		t.Fatal("republish changed existing wins or failed to update next box")
	}
	if !strings.Contains(s.GachaBoxOddsMessage(s.GachaState()[0]), "剩余 50 份") {
		t.Fatal("box odds missing remaining stock")
	}
	// Removal and restoration keep independent persisted box progress.
	s.ApplyGachaConfiguration(2, nil)
	s.ApplyGachaConfiguration(3, []GachaConfiguration{{Profile: p}})
	if s.GachaState()[0].BoxRound != 2 {
		t.Fatal("reopening reset box progress")
	}
	other := boxTestAccount()
	if other.GachaState()[0].BoxRound != 1 {
		t.Fatal("accounts share boxes")
	}
}
