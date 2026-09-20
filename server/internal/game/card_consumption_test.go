package game

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"kairisei.local/server/internal/gamestate"
)

func TestCardConsumptionReturnsBusinessErrors(t *testing.T) {
	for _, operation := range []string{"fusion", "evolution", "sell", "container sell"} {
		for _, state := range []string{"main deck", "support deck", "locked", "missing"} {
			if operation == "container sell" && strings.Contains(state, "deck") {
				continue
			}
			t.Run(operation+"/"+state, func(t *testing.T) {
				s := ConsumptionTestStore(t)
				want := ErrCardInDeck
				switch state {
				case "main deck":
					s.decks = []DeckInfo{{CardUniqueIDs: []int64{2}}}
				case "support deck":
					s.decks = []DeckInfo{{SupportCardUniqueIDs: []int64{2}}}
				case "locked":
					s.cards[1].IsLock = 1
					want = ErrCardLocked
				case "missing":
					s.cards = s.cards[:1]
					want = ErrCardUnavailable
				}
				if operation == "container sell" && len(s.cards) > 1 {
					s.containerCards, s.cards = s.cards[1:], s.cards[:1]
				}
				beforeCards, beforeContainer, beforeDecks := CloneCards(s.cards), CloneCards(s.containerCards), CloneDecks(s.decks)
				var err error
				switch operation {
				case "fusion":
					_, _, _, _, err = s.FuseCard(1, []int64{2}, nil, nil)
				case "evolution":
					_, _, _, err = s.EvolveCard(1, 11, []int64{2}, nil, nil)
				case "sell":
					_, _, _, err = s.SellCards([]int64{2}, nil)
				case "container sell":
					_, _, err = s.SellContainerCards([]int64{2})
				}
				if err != want {
					t.Fatalf("consumption error = %v, want %v", err, want)
				}
				if s.gold != 100000 || !reflect.DeepEqual(beforeCards, CloneCards(s.cards)) || !reflect.DeepEqual(beforeContainer, CloneCards(s.containerCards)) || !reflect.DeepEqual(beforeDecks, CloneDecks(s.decks)) {
					t.Fatal("rejected consumption changed cards, decks or gold")
				}
			})
		}
	}
}

func TestBuddyConsumptionReturnsBusinessErrors(t *testing.T) {
	for _, operation := range []string{"fusion", "evolution", "sell"} {
		for _, missing := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/missing=%v", operation, missing), func(t *testing.T) {
				s := ConsumptionTestStore(t)
				s.buddies = []gamestate.Buddy{{UniqueID: 1, BuddyID: 100, Level: 1, BaseAddPrice: 500}, {UniqueID: 2, BuddyID: 100, Level: 1, IsLock: 1}}
				s.buddyDefinitions = map[int]gamestate.BuddyDefinition{
					100: {BuddyID: 100, SameBuddyID: 100, MaxLevel: 2, ExperienceTableID: 1, EvolutionID: 101, Rarity: "MILLIONRARE"},
					101: {BuddyID: 101, SameBuddyID: 100, MaxLevel: 3, ExperienceTableID: 1, EvolutionCount: 1},
				}
				s.buddyExperience = map[int][]int{1: {10000, 10000}}
				s.buddyEvoPrices = map[string][]int{"MILLIONRARE": {100}}
				s.buddyProgression.MaximumMaterialCount = 100
				want := ErrBuddyLocked
				if missing {
					s.buddies = s.buddies[:1]
					want = ErrBuddyUnavailable
				}
				before := append([]gamestate.Buddy(nil), s.buddies...)
				var err error
				switch operation {
				case "fusion":
					_, err = s.FuseBuddy(1, []BuddyFusionInput{{InputType: 2, ID: 2, Num: 1}})
				case "evolution":
					_, err = s.EvolveBuddy(1, 2, 0)
				case "sell":
					_, _, _, _, err = s.SellBuddies([]int64{2})
				}
				if err != want {
					t.Fatalf("buddy error = %v, want %v", err, want)
				}
				if s.gold != 100000 || !reflect.DeepEqual(s.buddies, before) {
					t.Fatal("rejected buddy consumption changed state")
				}
			})
		}
	}
}

func ConsumptionTestStore(t *testing.T) *Account {
	t.Helper()
	return &Account{
		gold: 100000, cardProgression: cardFusionTestPolicy(),
		cards: []CardInfo{{UniqueID: 1, CardID: 10, Level: 2, LevelMax: 2, Experience: 1000000, BaseAddPrice: 200},
			{UniqueID: 2, CardID: 20, Level: 1, AddExperience: 10}},
		cardDefinitions: map[int]gamestate.Card{
			10: {CardID: 10, LevelMax: 2, ExperienceTableID: 1},
			11: {CardID: 11, LevelMax: 3, ExperienceTableID: 2},
			20: {CardID: 20, SameCardID: 20, RarityRank: 6, SellGold: 100},
		},
		cardActions: gamestate.CardActionState{EvolutionTransitions: []gamestate.EvolutionTransition{{
			FromCardID: 10, ToCardID: 11, Gold: 100, Materials: []gamestate.EvolutionMaterial{{CardID: 20, Num: 1}},
		}}},
		cardTemplates:     map[int]CardInfo{11: {CardID: 11, Level: 1, LevelMax: 3}},
		cardExperience:    map[int][]int{1: {1000000}, 2: {1000000, 1000000}},
		cardCollectionIDs: map[int]struct{}{},
	}
}

func cardFusionTestPolicy() gamestate.CardProgressionPolicy {
	return gamestate.CardProgressionPolicy{
		ConfigVersion: 5, MaximumCardMaterialCount: 100,
		FusionGoldPerMaterialPerBaseLevel: 100,
		FusionSuccessTypes: []gamestate.CardFusionSuccessPolicy{
			{SuccessType: 0, Weight: 90, ExperiencePermille: 1000},
			{SuccessType: 1, Weight: 9, ExperiencePermille: 1500},
			{SuccessType: 2, Weight: 1, ExperiencePermille: 2500},
		},
	}
}

func TestEquippedBaseAcceptsManyMRMaterials(t *testing.T) {
	for _, count := range []int{9, 100} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			s := ConsumptionTestStore(t)
			s.cards[0].Level, s.cards[0].Experience, s.cards[0].IsLock = 1, 0, 1
			s.cards = s.cards[:1]
			s.decks = []DeckInfo{{CardUniqueIDs: []int64{1}}}
			selected := make([]int64, count)
			for i := range count {
				id := int64(i + 2)
				selected[i] = id
				s.cards = append(s.cards, CardInfo{UniqueID: id, CardID: 20, Level: 1, AddExperience: 10})
			}
			_, result, _, _, err := s.FuseCard(1, selected, nil, nil)
			if err != nil || result.Experience <= 0 || result.IsLock != 1 || len(s.cards) != 1 || s.gold != 100000-200*count || s.decks[0].CardUniqueIDs[0] != 1 {
				t.Fatalf("equipped base with %d MR cards: result=%+v err=%v", count, result, err)
			}
		})
	}
}
