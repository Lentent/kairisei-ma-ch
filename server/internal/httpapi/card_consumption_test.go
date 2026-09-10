package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"kairisei.local/server/internal/release"
)

func consumptionTestStore(t *testing.T) *store {
	t.Helper()
	content, err := os.ReadFile("../../config/cn602-card-progression-runtime-profile.json")
	if err != nil {
		t.Fatal(err)
	}
	var policy release.CardProgressionPolicy
	if err := json.Unmarshal(content, &policy); err != nil {
		t.Fatal(err)
	}
	return &store{
		gold: 100000, cardProgression: policy,
		cards: []cardInfo{{UniqueID: 1, CardID: 10, Level: 2, LevelMax: 2, Experience: 1000000, BaseAddPrice: 200},
			{UniqueID: 2, CardID: 20, Level: 1, AddExperience: 10}},
		cardDefinitions: map[int]release.Card{
			10: {CardID: 10, LevelMax: 2, ExperienceTableID: 1},
			11: {CardID: 11, LevelMax: 3, ExperienceTableID: 2},
			20: {CardID: 20, SameCardID: 20, RarityRank: 6, SellGold: 100},
		},
		cardActions: release.CardActionState{EvolutionTransitions: []release.EvolutionTransition{{
			FromCardID: 10, ToCardID: 11, Gold: 100, Materials: []release.EvolutionMaterial{{CardID: 20, Num: 1}},
		}}},
		cardTemplates:     map[int]cardInfo{11: {CardID: 11, Level: 1, LevelMax: 3}},
		cardExperience:    map[int][]int{1: {1000000}, 2: {1000000, 1000000}},
		cardCollectionIDs: map[int]struct{}{},
	}
}

func TestCardConsumptionReturnsBusinessErrors(t *testing.T) {
	for _, operation := range []string{"fusion", "evolution", "sell", "container sell"} {
		for _, state := range []string{"main deck", "support deck", "locked", "missing"} {
			if operation == "container sell" && strings.Contains(state, "deck") {
				continue
			}
			t.Run(operation+"/"+state, func(t *testing.T) {
				s := consumptionTestStore(t)
				want := errCardInDeck
				switch state {
				case "main deck":
					s.decks = []deckInfo{{CardUniqueIDs: []int64{2}}}
				case "support deck":
					s.decks = []deckInfo{{SupportCardUniqueIDs: []int64{2}}}
				case "locked":
					s.cards[1].IsLock = 1
					want = errCardLocked
				case "missing":
					s.cards = s.cards[:1]
					want = errCardUnavailable
				}
				if operation == "container sell" && len(s.cards) > 1 {
					s.containerCards, s.cards = s.cards[1:], s.cards[:1]
				}
				beforeCards, beforeContainer, beforeDecks := cloneCards(s.cards), cloneCards(s.containerCards), cloneDecks(s.decks)
				api := &API{store: s, release: &release.Release{}}
				var handler http.HandlerFunc
				body := `{"base_uniqid":1,"add_uniqids":[2],"add_container_uniqids":[],"add_cardids":[],"add_stackcards":[]}`
				switch operation {
				case "fusion":
					handler = api.cardFusion
				case "evolution":
					handler = api.cardEvolution
					body = `{"base_uniqid":1,"to_cardid":11,"add_uniqids":[2],"add_container_uniqids":[],"add_cardids":[],"add_itemids":[]}`
				case "sell":
					handler, body = api.cardSell, `{"uniqids":[2],"cardids":[]}`
				case "container sell":
					handler, body = api.cardContainerSell, `{"uniqids":[2]}`
				}
				response := httptest.NewRecorder()
				handler(response, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))
				var common commonResponse
				if response.Code != 200 || json.Unmarshal([]byte(strings.Split(response.Body.String(), "\n")[0]), &common) != nil ||
					common.ResultCode != want.code || common.ResultString != want.message || common.ResultErrorAction != 2 || common.ResultDeleteSaveData != 0 {
					t.Fatalf("expected recoverable business error: %s", response.Body.String())
				}
				if s.gold != 100000 || !reflect.DeepEqual(beforeCards, cloneCards(s.cards)) || !reflect.DeepEqual(beforeContainer, cloneCards(s.containerCards)) || !reflect.DeepEqual(beforeDecks, cloneDecks(s.decks)) {
					t.Fatal("rejected consumption changed cards, decks or gold")
				}
			})
		}
	}
}

func TestEquippedBaseAcceptsManyMRMaterials(t *testing.T) {
	for _, count := range []int{9, 100} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			s := consumptionTestStore(t)
			s.cards[0].Level, s.cards[0].Experience, s.cards[0].IsLock = 1, 0, 1
			s.cards = s.cards[:1]
			s.decks = []deckInfo{{CardUniqueIDs: []int64{1}}}
			selected := make([]int64, count)
			for i := range count {
				id := int64(i + 2)
				selected[i] = id
				s.cards = append(s.cards, cardInfo{UniqueID: id, CardID: 20, Level: 1, AddExperience: 10})
			}
			_, result, _, _, err := s.fuseCard(1, selected, nil, nil)
			if err != nil || result.Experience <= 0 || result.IsLock != 1 || len(s.cards) != 1 || s.gold != 100000-200*count || s.decks[0].CardUniqueIDs[0] != 1 {
				t.Fatalf("equipped base with %d MR cards: result=%+v err=%v", count, result, err)
			}
		})
	}
}

func TestBuddyConsumptionReturnsBusinessErrors(t *testing.T) {
	for _, operation := range []string{"fusion", "evolution", "sell"} {
		for _, missing := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/missing=%v", operation, missing), func(t *testing.T) {
				s := consumptionTestStore(t)
				s.buddies = []release.Buddy{{UniqueID: 1, BuddyID: 100, Level: 1, BaseAddPrice: 500}, {UniqueID: 2, BuddyID: 100, Level: 1, IsLock: 1}}
				s.buddyDefinitions = map[int]release.BuddyDefinition{
					100: {BuddyID: 100, SameBuddyID: 100, MaxLevel: 2, ExperienceTableID: 1, EvolutionID: 101, Rarity: "MILLIONRARE"},
					101: {BuddyID: 101, SameBuddyID: 100, MaxLevel: 3, ExperienceTableID: 1, EvolutionCount: 1},
				}
				s.buddyExperience = map[int][]int{1: {10000, 10000}}
				s.buddyEvoPrices = map[string][]int{"MILLIONRARE": {100}}
				s.buddyProgression.MaximumMaterialCount = 100
				want := errBuddyLocked
				if missing {
					s.buddies = s.buddies[:1]
					want = errBuddyUnavailable
				}
				before := append([]release.Buddy(nil), s.buddies...)
				api := &API{store: s, release: &release.Release{}}
				handler, body := api.buddyFusion, `{"base_uniqid":1,"add_inputs":[{"input_type":2,"id":2,"num":1}]}`
				switch operation {
				case "evolution":
					handler, body = api.buddyEvolution, `{"base_uniqid":1,"add_buddy_uniqid":2,"add_cardid":0}`
				case "sell":
					handler, body = api.buddySell, `{"uniqids":[2]}`
				}
				response := httptest.NewRecorder()
				handler(response, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))
				var common commonResponse
				if response.Code != 200 || json.Unmarshal([]byte(strings.Split(response.Body.String(), "\n")[0]), &common) != nil || common.ResultCode != want.code || common.ResultString != want.message || common.ResultErrorAction != 2 || common.ResultDeleteSaveData != 0 {
					t.Fatalf("expected recoverable buddy error: %s", response.Body.String())
				}
				if s.gold != 100000 || !reflect.DeepEqual(s.buddies, before) {
					t.Fatal("rejected buddy consumption changed state")
				}
			})
		}
	}
}
