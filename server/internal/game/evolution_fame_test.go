package game

import (
	"reflect"
	"testing"

	"kairisei.local/server/internal/gamestate"
)

func divergentEvolutionTestAccount(t *testing.T) *Account {
	t.Helper()
	s := ConsumptionTestStore(t)
	s.cards[1].Fame = 8
	s.cards[1].Love = 5
	s.cardDefinitions[20] = gamestate.Card{CardID: 20, LevelMax: 1, LoveMax: 100, FameMax: 100}
	s.cardDefinitions[21] = gamestate.Card{CardID: 21, LevelMax: 1, LoveMax: 100, FameMax: 100}
	s.cardProgression.FameNormal = gamestate.CardParameter{HP: 100, Attack: 100, Magic: 100, Mind: 100}
	s.containerCards = []CardInfo{{UniqueID: 3, CardID: 21, Level: 1, Fame: 4, Love: 5}}
	s.cardActions.EvolutionTransitions[0].Type = 1
	s.cardActions.EvolutionTransitions[0].Materials = []gamestate.EvolutionMaterial{
		{CardID: 20, Num: 1, Fame: 3},
		{CardID: 21, Num: 1, Fame: 2},
	}
	for i, card := range s.cards[1:] {
		normalized, err := s.normalizeCardLocked(card)
		if err != nil {
			t.Fatal(err)
		}
		s.cards[i+1] = normalized
	}
	card, err := s.normalizeCardLocked(s.containerCards[0])
	if err != nil {
		t.Fatal(err)
	}
	s.containerCards[0] = card
	return s
}

func TestDivergentEvolutionSpendsFameAcrossInventoryAndWarehouse(t *testing.T) {
	s := divergentEvolutionTestAccount(t)
	oldMaterial, oldContainer := s.cards[1], s.containerCards[0]
	_, _, _, err := s.EvolveCard(1, 11, []int64{2}, []int64{3}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.cards) != 2 || len(s.containerCards) != 1 || s.gold != 99900 {
		t.Fatalf("inventory/gold after evolution: %+v, %+v, %d", s.cards, s.containerCards, s.gold)
	}
	oldMaterial.Fame = 5
	oldMaterial.HP, oldMaterial.Attack, oldMaterial.Magic, oldMaterial.Mind = 5, 5, 5, 5
	oldContainer.Fame = 2
	oldContainer.HP, oldContainer.Attack, oldContainer.Magic, oldContainer.Mind = 2, 2, 2, 2
	if !EqualCardInfo(s.cards[1], oldMaterial) || !EqualCardInfo(s.containerCards[0], oldContainer) {
		t.Fatalf("material identity, development or fame stats changed incorrectly: %+v, %+v", s.cards[1], s.containerCards[0])
	}
	snapshot := s.Snapshot(gamestate.State{})
	if len(snapshot.Cards) != 2 || len(snapshot.ContainerCards) != 1 || snapshot.Cards[1].UniqueID != 2 || snapshot.Cards[1].Fame != 5 || snapshot.ContainerCards[0].UniqueID != 3 || snapshot.ContainerCards[0].Fame != 2 {
		t.Fatal("save snapshot lost material cards or fame deductions")
	}
	// A second base cannot reuse the warehouse material after its fame is spent.
	s.cards = append(s.cards, CardInfo{UniqueID: 4, CardID: 10, Level: 2, LevelMax: 2})
	beforeCards := append([]CardInfo(nil), s.cards...)
	beforeContainer := append([]CardInfo(nil), s.containerCards...)
	_, _, _, err = s.EvolveCard(4, 11, []int64{2}, []int64{3}, nil)
	if err == nil || s.gold != 99900 || !reflect.DeepEqual(s.cards, beforeCards) || !reflect.DeepEqual(s.containerCards, beforeContainer) {
		t.Fatal("repeated evolution spent unavailable fame or changed state on rejection")
	}
}

func TestDivergentEvolutionRejectsWithoutSpendingFame(t *testing.T) {
	for _, name := range []string{"gold", "target", "material definition", "warehouse fame", "locked", "deck", "duplicate", "stack"} {
		t.Run(name, func(t *testing.T) {
			s := divergentEvolutionTestAccount(t)
			uniqueIDs, containerIDs := []int64{2}, []int64{3}
			var stackIDs []int
			switch name {
			case "gold":
				s.gold = 99
			case "target":
				delete(s.cardTemplates, 11)
			case "material definition":
				delete(s.cardDefinitions, 21)
			case "warehouse fame":
				s.containerCards[0].Fame = 2
			case "locked":
				s.cards[1].IsLock = 1
			case "deck":
				s.decks = []DeckInfo{{CardUniqueIDs: []int64{2}}}
			case "duplicate":
				uniqueIDs = []int64{2, 2}
			case "stack":
				uniqueIDs = nil
				stackIDs = []int{20}
				s.stackCards = []gamestate.CardStack{{CardID: 20, Num: 1}}
			}
			beforeCards := append([]CardInfo(nil), s.cards...)
			beforeContainer := append([]CardInfo(nil), s.containerCards...)
			beforeStacks := append([]gamestate.CardStack(nil), s.stackCards...)
			gold := s.gold
			_, _, _, err := s.EvolveCard(1, 11, uniqueIDs, containerIDs, stackIDs)
			if err == nil || s.gold != gold || !reflect.DeepEqual(s.cards, beforeCards) || !reflect.DeepEqual(s.containerCards, beforeContainer) || !reflect.DeepEqual(s.stackCards, beforeStacks) || len(s.cardCollectionIDs) != 0 {
				t.Fatalf("rejected evolution changed inventory, fame, gold or collection: %v", err)
			}
		})
	}
}
