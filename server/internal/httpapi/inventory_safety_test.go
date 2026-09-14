package httpapi

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	"kairisei.local/server/internal/release"
)

func TestAdminWithdrawnMailIsNotClaimHistory(t *testing.T) {
	s := &store{presentHistories: []release.Present{
		{PresentID: 1, State: release.PresentStateAdminDeleted},
		{PresentID: 2, State: 1},
	}}
	_, history := s.presentState()
	if len(history) != 1 || history[0].PresentID != 2 || len(s.presentHistories) != 2 {
		t.Fatal("withdrawal appeared as a received reward or lost its tombstone")
	}
}

func TestCardFusionMaterialCount(t *testing.T) {
	// Load the shipped policy: a fixture-only limit would miss the original bug.
	content, err := os.ReadFile("../../config/cn602-card-progression-runtime-profile.json")
	if err != nil {
		t.Fatal(err)
	}
	var policy release.CardProgressionPolicy
	if err := json.Unmarshal(content, &policy); err != nil {
		t.Fatal(err)
	}
	for _, input := range []struct {
		name                        string
		stack, inventory, container int
		gold                        int
		wantStatus                  int
	}{
		{"eight", 8, 0, 0, 20000, http.StatusOK},
		{"nine", 9, 0, 0, 20000, http.StatusOK},
		{"hundred", 100, 0, 0, 20000, http.StatusOK},
		{"over limit", 101, 0, 0, 20000, http.StatusBadRequest},
		{"mixed hundred", 98, 1, 1, 20000, http.StatusOK},
		{"mixed over limit", 99, 1, 1, 20000, http.StatusBadRequest},
		{"insufficient gold", 9, 0, 0, 899, http.StatusOK},
	} {
		t.Run(input.name, func(t *testing.T) {
			base := cardInfo{UniqueID: 1, CardID: 10, Level: 1, LevelMax: 2, BaseAddPrice: 100}
			s := &store{
				gold: input.gold, cards: []cardInfo{base}, cardProgression: policy,
				cardDefinitions: map[int]release.Card{
					10: {CardID: 10, LevelMax: 2, ExperienceTableID: 1},
					11: {CardID: 11, SameCardID: 11},
				},
				cardExperience: map[int][]int{1: {100000}},
				stackCards:     []release.CardStack{{CardID: 20, Num: 200, AddExperience: 10}},
			}
			uniqueIDs, containerIDs, stackIDs := []int64{}, []int64{}, []int{}
			if input.inventory != 0 {
				s.cards = append(s.cards, cardInfo{UniqueID: 2, CardID: 11, AddExperience: 10})
				uniqueIDs = append(uniqueIDs, 2)
			}
			if input.container != 0 {
				s.containerCards = []cardInfo{{UniqueID: 3, CardID: 11, AddExperience: 10}}
				containerIDs = append(containerIDs, 3)
			}
			for range input.stack {
				stackIDs = append(stackIDs, 20) // Original CardFusion2 sends repeated IDs.
			}
			body, err := json.Marshal(map[string]any{
				"base_uniqid": 1, "add_uniqids": uniqueIDs, "add_container_uniqids": containerIDs,
				"add_cardids": stackIDs, "add_stackcards": []int{},
			})
			if err != nil {
				t.Fatal(err)
			}
			api := &API{store: s, release: &release.Release{}}
			response := httptest.NewRecorder()
			api.cardFusion(response, httptest.NewRequest(http.MethodPost, "/CardFusion2", strings.NewReader(string(body))))
			if response.Code != input.wantStatus {
				t.Fatalf("status=%d want=%d: %s", response.Code, input.wantStatus, response.Body.String())
			}
			if input.wantStatus != http.StatusOK || input.name == "insufficient gold" {
				if input.name == "insufficient gold" {
					var common commonResponse
					if err := json.Unmarshal([]byte(strings.Split(response.Body.String(), "\n")[0]), &common); err != nil || common.ResultCode != -1030 || common.ResultErrorAction != 2 {
						t.Fatalf("expected native gold error: %s", response.Body.String())
					}
				}
				if s.gold != input.gold || s.stackCards[0].Num != 200 || !equalCardInfo(s.cards[0], base) || len(s.cards) != 1+input.inventory || len(s.containerCards) != input.container {
					t.Fatal("rejected fusion changed inventory, experience or gold")
				}
				return
			}
			var result struct {
				ResultCard struct {
					SuccessType int `json:"success_type"`
				} `json:"result_card"`
			}
			if err := json.Unmarshal([]byte(strings.Split(response.Body.String(), "\n")[1]), &result); err != nil {
				t.Fatal(err)
			}
			count := input.stack + input.inventory + input.container
			multiplier := []int{1000, 1500, 2500}[result.ResultCard.SuccessType]
			if s.gold != input.gold-100*count || s.stackCards[0].Num != 200-input.stack || len(s.cards) != 1 || len(s.containerCards) != 0 || s.cards[0].Experience != count*10*multiplier/1000 {
				t.Fatalf("fusion consumption/result differs: gold=%d stack=%d cards=%+v", s.gold, s.stackCards[0].Num, s.cards)
			}
		})
	}
}

func TestCardFusionFame(t *testing.T) {
	for _, name := range []string{"dual attribute caps fame", "wrong attribute", "manual leveled same family"} {
		t.Run(name, func(t *testing.T) {
			base := cardInfo{UniqueID: 1, CardID: 10, Level: 2, LevelMax: 2, Experience: 10, Fame: 90, BaseAddPrice: 100}
			s := &store{gold: 1000, cards: []cardInfo{base},
				cardDefinitions: map[int]release.Card{
					10: {CardID: 10, SameCardID: 10, RarityRank: 5, LevelMax: 2, ExperienceTableID: 1, FameMax: 100, FusionAttributes: 3},
					11: {CardID: 11, SameCardID: 10, RarityRank: 6},
				},
				cardExperience: map[int][]int{1: {10}},
				stackCards:     []release.CardStack{{CardID: 20, Num: 3, AddExperience: 10}},
				cardProgression: release.CardProgressionPolicy{ConfigVersion: 5, MaximumCardMaterialCount: 100,
					FusionGoldPerMaterialPerBaseLevel: 100,
					FusionSuccessTypes:                []release.CardFusionSuccessPolicy{{SuccessType: 0, Weight: 1, ExperiencePermille: 1000}},
					FameMaterials:                     map[int][5]int{20: {1, 5}},
				},
			}
			var uniqueIDs []int64
			uses := []release.CardStackUse{{CardID: 20, Num: 2}}
			wantFame, wantGold, wantStack := 100, 800, 1
			if name == "wrong attribute" {
				definition := s.cardDefinitions[10]
				definition.FusionAttributes = 4
				s.cardDefinitions[10] = definition
			}
			if name == "manual leveled same family" {
				s.cards = append(s.cards, cardInfo{UniqueID: 2, CardID: 11, Level: 50, Fame: 7, AddExperience: 10})
				uniqueIDs, uses = []int64{2}, nil
				wantFame, wantGold, wantStack = 97, 900, 3
			}
			_, result, _, _, err := s.fuseCard(1, uniqueIDs, nil, uses)
			if name == "wrong attribute" {
				if err == nil || s.gold != 1000 || s.stackCards[0].Num != 3 || !equalCardInfo(s.cards[0], base) {
					t.Fatal("ineffective fame material changed inventory")
				}
				return
			}
			if err != nil || result.Fame != wantFame || result.Experience != base.Experience || s.gold != wantGold || s.stackCards[0].Num != wantStack || len(s.cards) != 1 {
				t.Fatalf("fusion fame or consumption differs: result=%+v gold=%d stack=%v err=%v", result, s.gold, s.stackCards, err)
			}
		})
	}
}

func TestCardEvolutionRecipes(t *testing.T) {
	for _, input := range []struct {
		name                      string
		kind, count, fame         int
		keep, container, rejected bool
	}{
		{"normal resets level", 0, 3, 0, false, false, false},
		{"limit keeps level with 25+25+3 materials", 3, 53, 0, true, false, false},
		{"limit consumes last mixed materials", 3, 53, 0, true, false, false},
		{"limit insufficient materials", 3, 53, 0, true, false, true},
		{"limit insufficient gold", 3, 53, 0, true, false, true},
		{"knights accepts 800 materials", 2, 800, 0, false, false, false},
		{"god keeps level", 1, 1, 3, true, false, false},
		{"god low fame", 1, 1, 2, true, false, true},
		{"god warehouse material", 1, 1, 3, true, true, false},
		{"god warehouse low fame", 1, 1, 2, true, true, true},
	} {
		t.Run(input.name, func(t *testing.T) {
			base := cardInfo{UniqueID: 1, CardID: 10, Level: 2, LevelMax: 2, Experience: 10, Fame: 3, Love: 5, IsLock: 1}
			transition := release.EvolutionTransition{FromCardID: 10, ToCardID: 11, Type: input.kind, Gold: 100, KeepLevel: input.keep,
				Materials: []release.EvolutionMaterial{{CardID: 20, Num: input.count, Fame: 1}}}
			if input.kind == 3 {
				transition.Materials = []release.EvolutionMaterial{{CardID: 20, Num: 25, Fame: 1}, {CardID: 21, Num: 25, Fame: 1}, {CardID: 22, Num: 3, Fame: 1}}
			}
			s := &store{gold: 500, cards: []cardInfo{base}, cardCollectionIDs: map[int]struct{}{},
				cardActions:     release.CardActionState{EvolutionTransitions: []release.EvolutionTransition{transition}},
				cardDefinitions: map[int]release.Card{11: {CardID: 11, LevelMax: 3, ExperienceTableID: 2, LoveMax: 10000, FameMax: 100}},
				cardTemplates:   map[int]cardInfo{11: {CardID: 11, Level: 1, LevelMax: 3}},
				cardExperience:  map[int][]int{2: {20, 30}},
				cardProgression: release.CardProgressionPolicy{ConfigVersion: 5, FusionGoldPerMaterialPerBaseLevel: 100},
			}
			var uniqueIDs, containerIDs []int64
			var materialIDs []int
			if input.kind == 1 {
				s.cardActions.EvolutionTransitions[0].Materials[0].Fame = 3
				material := cardInfo{UniqueID: 2, CardID: 20, Fame: input.fame}
				if input.container {
					s.containerCards = []cardInfo{material}
					containerIDs = []int64{2}
				} else {
					s.cards = append(s.cards, material)
					uniqueIDs = []int64{2}
				}
			} else {
				for _, material := range transition.Materials {
					s.stackCards = append(s.stackCards, release.CardStack{CardID: material.CardID, Num: material.Num + 1})
					materialIDs = append(materialIDs, material.CardID)
				}
				if input.name == "limit insufficient materials" {
					s.stackCards[1].Num = 24
				}
				if input.name == "limit consumes last mixed materials" {
					for i := range s.stackCards {
						s.stackCards[i].Num--
					}
				}
			}
			if input.name == "limit insufficient gold" {
				s.gold = transition.Gold - 1
			}
			beforeGold := s.gold
			beforeStacks := slices.Clone(s.stackCards)
			beforeCards := len(s.cards) + len(s.containerCards)
			body, err := json.Marshal(map[string]any{
				"base_uniqid": 1, "to_cardid": 11, "add_uniqids": uniqueIDs,
				"add_container_uniqids": containerIDs, "add_cardids": materialIDs, "add_itemids": []int{},
			})
			if err != nil {
				t.Fatal(err)
			}
			api := &API{store: s, release: &release.Release{}}
			response := httptest.NewRecorder()
			api.cardEvolution(response, httptest.NewRequest(http.MethodPost, "/CardEvolution", strings.NewReader(string(body))))
			if input.rejected {
				wantCode := -1
				if input.name == "limit insufficient materials" {
					wantCode = -1200
				}
				if input.name == "limit insufficient gold" {
					wantCode = -1030
				}
				var common commonResponse
				if response.Code != http.StatusOK || json.Unmarshal([]byte(strings.Split(response.Body.String(), "\n")[0]), &common) != nil || common.ResultCode != wantCode || common.ResultErrorAction != 2 || common.ResultDeleteSaveData != 0 {
					t.Fatalf("expected evolution business rejection: %s", response.Body.String())
				}
				if s.gold != beforeGold || !equalCardInfo(s.cards[0], base) || len(s.cards)+len(s.containerCards) != beforeCards || !slices.Equal(s.stackCards, beforeStacks) || len(s.cardCollectionIDs) != 0 {
					t.Fatalf("invalid evolution changed state: %s", response.Body.String())
				}
				return
			}
			if response.Code != http.StatusOK {
				t.Fatalf("evolution failed: %s", response.Body.String())
			}
			result := s.cards[0]
			wantLevel, wantExp := 1, 0
			if input.keep {
				wantLevel, wantExp = 2, 20
			}
			if result.CardID != 11 || result.UniqueID != base.UniqueID || result.Level != wantLevel || result.Experience != wantExp || result.Love != base.Love || result.Fame != base.Fame || result.IsLock != base.IsLock || s.gold != 400 || len(s.cards) != 1 || len(s.containerCards) != 0 {
				t.Fatalf("evolution result or consumption differs: %+v", result)
			}
			if input.kind != 1 {
				remaining := 1
				if input.name == "limit consumes last mixed materials" {
					remaining = 0
					if len(toWireStackCards(s.stackState())) != 0 {
						t.Fatal("depleted evolution materials reappeared after inventory refresh")
					}
				}
				for _, material := range s.stackCards {
					if material.Num != remaining {
						t.Fatal("evolution consumed wrong stack quantity")
					}
				}
			}
			if _, ok := s.cardCollectionIDs[11]; !ok {
				t.Fatal("evolution target was not collected")
			}
		})
	}
}

func TestPresentMultiReceiveUsesClientFilterLimitAndPartialResults(t *testing.T) {
	gold := func(id int64) release.Present {
		return release.Present{PresentID: id, Reward: release.Reward{Type: 4, Num: 5}}
	}
	item := release.Present{PresentID: 1, Reward: release.Reward{Type: 8, RewardTypeID: 10, Num: 1}}
	for _, name := range []string{"twenty per batch", "full item keeps other rewards", "daily item filter", "nonclaimable keeps other rewards"} {
		t.Run(name, func(t *testing.T) {
			s := &store{items: map[int]release.Item{}, itemDefinitions: map[int]release.ItemDefinition{
				10: {ItemID: 10, MaxOwned: 1},
			}}
			receiveTypes := []int{2}
			var wantIDs, wantFailed []int64
			wantGold := 0
			switch name {
			case "nonclaimable keeps other rewards":
				unclaimable := gold(1)
				unclaimable.State = 1
				s.presents = []release.Present{unclaimable, gold(2)}
				wantIDs, wantFailed, wantGold = []int64{2}, []int64{1}, 5
			case "twenty per batch":
				for id := int64(1); id <= 21; id++ {
					s.presents = append(s.presents, gold(id))
				}
				for id := int64(21); id >= 2; id-- {
					wantIDs = append(wantIDs, id)
				}
				wantGold = 100
			case "full item keeps other rewards":
				s.presents = []release.Present{item, gold(2)}
				s.items[10] = release.Item{ItemID: 10, Num: 1}
				wantIDs, wantFailed, wantGold = []int64{2}, []int64{1}, 5
			case "daily item filter":
				definition := s.itemDefinitions[10]
				definition.DailyLimited = 1
				s.itemDefinitions[10] = definition
				expiringGold := gold(2)
				expiringGold.LimitTime = 2147483647
				s.presents = []release.Present{item, expiringGold}
				receiveTypes, wantIDs = []int{1}, []int64{1}
			}
			beforeCount := len(s.presents)
			result, err := s.receivePresents(receiveTypes, true)
			if err != nil || !slices.Equal(result.PresentID, wantIDs) || !slices.Equal(result.FailedID, wantFailed) || s.gold != wantGold || len(s.presents) != beforeCount || len(s.presentHistories) != 0 {
				t.Fatalf("gift selection/grant differs: ids=%v failed=%v gold=%d remaining=%d err=%v", result.PresentID, result.FailedID, s.gold, len(s.presents), err)
			}
			if name == "full item keeps other rewards" {
				full, err := s.receivePresent(1)
				if err != nil || !slices.Equal(full.FailedID, []int64{1}) || len(s.presents) != 2 {
					t.Fatalf("full gift must remain claimable: %+v %v", full, err)
				}
				s.items[10] = release.Item{ItemID: 10}
				claimed, err := s.receivePresent(1)
				if err != nil || !slices.Equal(claimed.PresentID, []int64{1}) || s.items[10].Num != 1 {
					t.Fatalf("gift failed after freeing space: %+v %v", claimed, err)
				}
				retry, err := s.receivePresent(1)
				if err != nil || !slices.Equal(retry.FailedID, []int64{1}) || s.items[10].Num != 1 || len(s.presentHistories) != 0 {
					t.Fatalf("retry duplicated reward/history: %+v %v", retry, err)
				}
			}
			if name == "nonclaimable keeps other rewards" {
				result, err := s.receivePresent(1)
				if err != nil || !slices.Equal(result.FailedID, []int64{1}) || s.gold != 5 || len(s.presentHistories) != 0 {
					t.Fatalf("nonclaimable gift was granted: %+v %v", result, err)
				}
				deleted, err := s.deletePresents(1)
				if err != nil || !slices.Equal(deleted, []int64{1}) || s.gold != 5 || len(s.presents) != 1 || len(s.presentHistories) != 1 {
					t.Fatalf("discarding gift changed reward or failed: %v %v", deleted, err)
				}
			}
		})
	}
}

func TestCardSellChecksEachQuantityBeforeMerging(t *testing.T) {
	for _, input := range []struct {
		name, cards string
		sold        int
	}{
		{"negative cancellation", `[{"cardid":9,"num":-1},{"0":9,"1":2}]`, 0},
		{"overflow wraps positive", fmt.Sprintf(`[{"cardid":9,"num":%d},{"0":9,"1":%d},9,9,9]`, math.MaxInt, math.MaxInt), 0},
		{"ordinary mixed entries", `[9,{"0":9,"1":1},{"cardid":9,"num":1}]`, 3},
	} {
		t.Run(input.name, func(t *testing.T) {
			s := &store{gold: 100, stackCards: []release.CardStack{{CardID: 9, Num: 3, BaseAddPrice: 10}}}
			api := &API{store: s, release: &release.Release{}}
			response := httptest.NewRecorder()
			api.cardSell(response, httptest.NewRequest(http.MethodPost, "/CardSell",
				strings.NewReader(`{"uniqids":[],"cardids":`+input.cards+`}`)))
			wantStatus := http.StatusBadRequest
			if input.sold > 0 {
				wantStatus = http.StatusOK
			}
			if response.Code != wantStatus || s.gold != 100+10*input.sold || s.stackCards[0].Num != 3-input.sold {
				t.Fatalf("status=%d gold=%d quantity=%d; want status=%d sold=%d", response.Code, s.gold, s.stackCards[0].Num, wantStatus, input.sold)
			}
		})
	}
}

func TestCardSalesRejectOverflowBeforeConsumingInventory(t *testing.T) {
	for _, container := range []bool{false, true} {
		s := &store{gold: 1, cardDefinitions: map[int]release.Card{9: {CardID: 9, SellGold: math.MaxInt}}}
		cards := []cardInfo{{UniqueID: 1, CardID: 9}}
		var err error
		if container {
			s.containerCards = cards
			_, _, err = s.sellContainerCards([]int64{1})
			if len(s.containerCards) != 1 {
				t.Fatal("failed sale removed a warehouse card")
			}
		} else {
			s.cards = cards
			_, _, _, err = s.sellCards([]int64{1}, nil)
			if len(s.cards) != 1 {
				t.Fatal("failed sale removed an inventory card")
			}
		}
		if err == nil || s.gold != 1 {
			t.Fatal("overflowing sale changed balance")
		}
	}
	if _, err := checkedCardSaleGold(0, 0, math.MaxInt, 2); err == nil {
		t.Fatal("stack quantity multiplication overflow accepted")
	}
	if total, err := checkedCardSaleGold(100, 10, 20, 3); err != nil || total != 70 {
		t.Fatal("ordinary sale was rejected")
	}
}

func TestFameTrainingCannotUnlockOrReplaceItsCard(t *testing.T) {
	s := &store{cards: []cardInfo{{UniqueID: 1, CardID: 9, IsLock: 1}}, cardFameTraining: &release.CardFameTraining{UniqueID: 1}}
	if err := s.setCardLock(1, 0, false); err == nil || s.cards[0].IsLock != 1 {
		t.Fatal("active training lock removed")
	}
	if _, _, _, _, err := s.fuseCard(1, []int64{2}, nil, nil); err == nil || !strings.Contains(err.Error(), "active fame training") {
		t.Fatal("training card accepted as fusion base")
	}
	if _, _, _, err := s.evolveCard(1, 10, nil, nil, nil); err == nil || !strings.Contains(err.Error(), "active fame training") {
		t.Fatal("training card accepted as evolution base")
	}
	s.cardFameTraining = nil
	if err := s.setCardLock(1, 0, false); err != nil {
		t.Fatal("ordinary manual unlock rejected")
	}
}
