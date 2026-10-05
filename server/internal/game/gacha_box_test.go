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

func TestGachaBoxBossCurrencyPaysForStackMaterials(t *testing.T) {
	s := boxTestAccount()
	p := &s.gachas[0]
	p.PayType, p.PayTypeID, p.Price = 4, 4000, 2
	s.items[4000] = gamestate.Item{ItemID: 4000, Num: 2}
	s.itemDefinitions[4000] = gamestate.ItemDefinition{ItemID: 4000, Name: "Boss币", ItemType: "TRADE", MaxOwned: 10000}
	s.stackCardTemplates = map[int]gamestate.CardStack{123: {CardID: 123}}
	for i := range p.BoxRounds {
		p.BoxRounds[i].Rewards = []gamestate.GachaBoxReward{{Stock: 50, Reward: gamestate.Reward{Type: 13, RewardTypeID: 123, Num: 3, CardSkillLevels: []int16{}}}}
	}
	if err := s.validateGachaRulesLocked(*p); err != nil {
		t.Fatal(err)
	}
	coins := s.coinFree
	result, err := s.PlayGacha(p.GachaID, 4, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.items[4000].Num != 0 || s.coinFree != coins || result.Item.ItemID != 4000 || len(result.Reward.StackCards) != 1 || result.Reward.StackCards[0].Num != 3 || len(s.stackCards) != 1 || s.stackCards[0].Num != 3 || s.gachaBoxes[p.GroupID].Remaining[0].Stock != 49 {
		t.Fatal("Boss currency payment or material reward failed", result)
	}
	if !strings.Contains(s.GachaBoxOddsMessage(s.GachaState()[0]), "素材卡 123 × 3：剩余 49 份") {
		t.Fatal("material reward mislabeled")
	}
	if _, err := s.PlayGacha(p.GachaID, 4, nil); err == nil || s.stackCards[0].Num != 3 || s.gachaBoxes[p.GroupID].Remaining[0].Stock != 49 {
		t.Fatal("insufficient Boss currency changed stock or materials")
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

func TestGachaBoxVariableRoundSizesExhaustThenRepeat(t *testing.T) {
	s := boxTestAccount()
	sizes := []int{10, 20, 30, 40, 50, 60, 70, 80, 90, 100, 7}
	for i, size := range sizes {
		s.gachas[0].BoxRounds[i].Rewards[0].Stock = size
	}
	for round := 1; round <= 13; round++ {
		size := sizes[min(round, gamestate.GachaBoxTemplates)-1]
		for draw := 1; draw <= size; draw++ {
			before := s.items[10].Num
			result, err := s.PlayGacha(70000001, 3, nil)
			if err != nil {
				t.Fatalf("round %d draw %d: %v", round, draw, err)
			}
			if s.items[10].Num-before != min(round, gamestate.GachaBoxTemplates) {
				t.Fatalf("round %d used the wrong reward template", round)
			}
			wantRound, wantStock := uint64(round), size-draw
			if draw == size {
				wantRound++
				wantStock = sizes[min(round+1, gamestate.GachaBoxTemplates)-1]
			}
			progress := s.gachaBoxes[70000001]
			if progress.Round != wantRound || result.Gachas[0].BoxRound != wantRound || gamestate.GachaBoxStock(progress.Remaining) != wantStock {
				t.Fatalf("round %d draw %d: progress %+v, want round %d stock %d", round, draw, progress, wantRound, wantStock)
			}
		}
	}
}

func TestGachaBoxStockSizeRepublishPreservesCurrentBox(t *testing.T) {
	s := boxTestAccount()
	if _, err := s.PlayGacha(70000001, 3, nil); err != nil {
		t.Fatal(err)
	}
	before := gamestate.CloneGachaBoxes(s.gachaBoxes)
	p := gamestate.CloneGachas(s.gachas)[0]
	for i := range p.BoxRounds {
		p.BoxRounds[i].Rewards[0].Stock = (i + 1) * 10
		p.BoxRounds[i].Rewards[0].Reward.Num = 20
	}
	s.ApplyGachaConfiguration(1, []GachaConfiguration{{Profile: p}})
	if !reflect.DeepEqual(before, s.gachaBoxes) {
		t.Fatal("changing template sizes changed the active box")
	}
	shown := s.GachaState()[0]
	if !strings.Contains(shown.Name, "剩余 49 份") || strings.Contains(shown.BuyMessage, "/50") || strings.Contains(s.GachaBoxOddsMessage(shown), "每轮 50") {
		t.Fatal("active box display uses a stale fixed total", shown)
	}
	for i := 0; i < 49; i++ {
		if _, err := s.PlayGacha(70000001, 3, nil); err != nil {
			t.Fatal(err)
		}
	}
	progress := s.gachaBoxes[70000001]
	if s.items[10].Num != 50 || progress.Round != 2 || gamestate.GachaBoxStock(progress.Remaining) != 20 || progress.Remaining[0].Reward.Num != 20 {
		t.Fatal("old inventory did not finish before applying the new round size", progress)
	}
	if !strings.Contains(s.GachaBoxOddsMessage(s.GachaState()[0]), "第 2 轮，剩余 20 份") {
		t.Fatal("odds message does not show the actual new inventory")
	}
}

func TestGachaBoxMaximumStockOddsRemainInClientRange(t *testing.T) {
	s := boxTestAccount()
	p := &s.gachas[0]
	p.BoxRounds[0].Rewards = []gamestate.GachaBoxReward{
		{Stock: 1, Reward: gamestate.Reward{Type: 8, RewardTypeID: 10, Num: 1, CardSkillLevels: []int16{}}},
		{Stock: gamestate.GachaBoxMaxStock - 1, Reward: gamestate.Reward{Type: 8, RewardTypeID: 10, Num: 2, CardSkillLevels: []int16{}}},
	}
	stages, err := PreviewGachaStages(s.GachaState()[0], nil)
	if err != nil || len(stages) != 1 || !reflect.DeepEqual(stages[0].Odds, []int{10, 9999990}) {
		t.Fatal("maximum box stock overflows client odds", stages, err)
	}
	if _, err := s.PlayGacha(p.GachaID, 3, nil); err != nil || gamestate.GachaBoxStock(s.gachaBoxes[p.GroupID].Remaining) != gamestate.GachaBoxMaxStock-1 {
		t.Fatal("maximum stock draw failed", err)
	}
}

func addBoxDrawTestVariant(s *Account, count, price int) int {
	p := gamestate.CloneGachas(s.gachas[:1])[0]
	p.GachaID++
	p.CardNum, p.CardNumMax, p.Price = count, count, price
	s.gachas = append(s.gachas, p)
	return p.GachaID
}

func TestGachaBoxSingleAndTenDrawShareStockAcrossRounds(t *testing.T) {
	s := boxTestAccount()
	s.gachas[0].BoxRounds[0].Rewards[0].Stock = 10
	s.gachas[0].BoxRounds[1].Rewards[0].Stock = 20
	multiID := addBoxDrawTestVariant(s, 10, 5)
	if _, err := s.PlayGacha(70000001, 3, nil); err != nil {
		t.Fatal(err)
	}
	result, err := s.PlayGacha(multiID, 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Reward.Rewards) != 10 {
		t.Fatal("ten-draw did not return ten separate wins", result)
	}
	for i, received := range result.Reward.Rewards {
		want := 1
		if i == 9 {
			want = 2
		}
		if received.Reward.Num != want {
			t.Fatalf("draw %d did not use the sequential round template", i+1)
		}
	}
	progress := s.gachaBoxes[70000001]
	if len(s.gachaBoxes) != 1 || progress.Round != 2 || gamestate.GachaBoxStock(progress.Remaining) != 19 || s.items[10].Num != 12 || s.coinFree != 9994 || s.gachas[0].PlayCount != 1 || s.gachas[1].PlayCount != 1 {
		t.Fatal("variants did not share inventory or charged once per request", progress, s.items, s.coinFree)
	}
	for _, shown := range result.Gachas {
		if shown.BoxRound != 2 || !strings.Contains(shown.Name, "剩余 19 份") {
			t.Fatal("variants show different current boxes", result.Gachas)
		}
		if shown.GachaID == multiID && !strings.Contains(shown.BuyMessage, "抽取 10 份奖励") {
			t.Fatal("batch purchase message still says single draw", shown.BuyMessage)
		}
	}
	if _, err := s.PlayGacha(70000001, 3, nil); err != nil || gamestate.GachaBoxStock(s.gachaBoxes[70000001].Remaining) != 18 {
		t.Fatal("single draw did not continue the same batch inventory", err)
	}
}

func TestGachaBoxMultiDrawCrossesLoopBoundaries(t *testing.T) {
	s := boxTestAccount()
	s.gachas[0].BoxRounds[10].Rewards[0].Stock = 2
	multiID := addBoxDrawTestVariant(s, 11, 7)
	s.gachaBoxes[70000001] = gamestate.GachaBoxProgress{Round: 10, Remaining: []gamestate.GachaBoxReward{{Stock: 1, Reward: gamestate.Reward{Type: 8, RewardTypeID: 10, Num: 10, CardSkillLevels: []int16{}}}}}
	result, err := s.PlayGacha(multiID, 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	progress := s.gachaBoxes[70000001]
	if progress.Round != 16 || gamestate.GachaBoxStock(progress.Remaining) != 2 || s.items[10].Num != 120 || len(result.Reward.Rewards) != 11 || result.Reward.Rewards[0].Reward.Num != 10 || s.coinFree != 9993 {
		t.Fatal("batch failed to advance through repeating boxes", progress, result)
	}
}

func TestGachaBoxMultiDrawFailuresAreAtomic(t *testing.T) {
	for _, failure := range []string{"balance", "reward after boundary", "batch capacity", "invalid next stock"} {
		t.Run(failure, func(t *testing.T) {
			s := boxTestAccount()
			multiID := addBoxDrawTestVariant(s, 10, 5)
			s.gachaBoxes[70000001] = gamestate.GachaBoxProgress{Round: 1, Remaining: []gamestate.GachaBoxReward{{Stock: 1, Reward: gamestate.Reward{Type: 8, RewardTypeID: 10, Num: 1, CardSkillLevels: []int16{}}}}}
			switch failure {
			case "balance":
				s.coinFree = 4
			case "reward after boundary":
				s.gachas[1].BoxRounds[1].Rewards[0].Reward.RewardTypeID = 999999
			case "batch capacity":
				s.gold = math.MaxInt - 8
				s.gachas[1].BoxRounds[1].Rewards[0].Reward = gamestate.Reward{Type: 4, Num: 1, CardSkillLevels: []int16{}}
			case "invalid next stock":
				s.gachas[1].BoxRounds[1].Rewards[0].Stock = 0
			}
			boxes := gamestate.CloneGachaBoxes(s.gachaBoxes)
			coins, gold, count := s.coinFree, s.gold, s.gachas[1].PlayCount
			if _, err := s.PlayGacha(multiID, 3, nil); err == nil {
				t.Fatal("invalid batch accepted")
			}
			if !reflect.DeepEqual(boxes, s.gachaBoxes) || s.coinFree != coins || s.gold != gold || s.items[10].Num != 0 || len(s.presents) != 0 || s.gachas[1].PlayCount != count {
				t.Fatal("failed batch changed payment, stock or rewards", s.gachaBoxes, s.items)
			}
		})
	}
}

func TestGachaBoxMultiDrawFullInventoryUsesPresents(t *testing.T) {
	s := boxTestAccount()
	definition := s.itemDefinitions[10]
	definition.MaxOwned = 3
	s.itemDefinitions[10] = definition
	multiID := addBoxDrawTestVariant(s, 10, 5)
	result, err := s.PlayGacha(multiID, 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.items[10].Num != 3 || len(s.presents) != 7 || len(result.Reward.Rewards) != 10 || s.coinFree != 9995 || gamestate.GachaBoxStock(s.gachaBoxes[70000001].Remaining) != 40 {
		t.Fatal("full inventory dropped batch wins or consumed incorrect stock", result)
	}
	for i, received := range result.Reward.Rewards {
		if received.InPresentBox != (i >= 3) {
			t.Fatal("batch delivery markers differ from actual inventory/presents", result.Reward.Rewards)
		}
	}
}

func TestGachaBoxTenDrawPaysBossCurrencyOnce(t *testing.T) {
	s := boxTestAccount()
	for i := range s.gachas[0].BoxRounds {
		s.gachas[0].BoxRounds[i].Rewards = []gamestate.GachaBoxReward{{Stock: 50, Reward: gamestate.Reward{Type: 13, RewardTypeID: 123, Num: 3, CardSkillLevels: []int16{}}}}
	}
	s.stackCardTemplates = map[int]gamestate.CardStack{123: {CardID: 123}}
	s.items[4000] = gamestate.Item{ItemID: 4000, Num: 5}
	s.itemDefinitions[4000] = gamestate.ItemDefinition{ItemID: 4000, Name: "Boss币", MaxOwned: 10000}
	multiID := addBoxDrawTestVariant(s, 10, 3)
	s.gachas[1].PayType, s.gachas[1].PayTypeID = 4, 4000
	result, err := s.PlayGacha(multiID, 4, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.items[4000].Num != 2 || result.Item.Num != 2 || s.coinFree != 10000 || len(s.stackCards) != 1 || s.stackCards[0].Num != 30 || len(result.Reward.Rewards) != 10 || gamestate.GachaBoxStock(s.gachaBoxes[70000001].Remaining) != 40 {
		t.Fatal("Boss currency batch price or material quantity was charged per slot", result)
	}
	before := gamestate.CloneGachaBoxes(s.gachaBoxes)
	if _, err := s.PlayGacha(multiID, 4, nil); err == nil || s.items[4000].Num != 2 || s.stackCards[0].Num != 30 || !reflect.DeepEqual(before, s.gachaBoxes) {
		t.Fatal("insufficient Boss currency changed stock or materials", err)
	}
}
