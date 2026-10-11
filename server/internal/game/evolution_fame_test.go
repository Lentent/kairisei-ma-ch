package game

import (
	"reflect"
	"slices"
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
	// A second evolution consumes the warehouse card when its remaining fame matches.
	s.cards = append(s.cards, CardInfo{UniqueID: 4, CardID: 10, Level: 2, LevelMax: 2})
	_, _, _, err = s.EvolveCard(4, 11, []int64{2}, []int64{3}, nil)
	if err != nil || len(s.cards) != 3 || len(s.containerCards) != 0 || s.cards[1].Fame != 2 || s.gold != 99800 {
		t.Fatalf("second evolution did not consume the depleted warehouse material: %v, %+v, %+v", err, s.cards, s.containerCards)
	}
	// A third base cannot reuse the consumed warehouse card.
	s.cards = append(s.cards, CardInfo{UniqueID: 5, CardID: 10, Level: 2, LevelMax: 2})
	beforeCards := append([]CardInfo(nil), s.cards...)
	beforeContainer := slices.Clone(s.containerCards)
	_, _, _, err = s.EvolveCard(5, 11, []int64{2}, []int64{3}, nil)
	if err == nil || s.gold != 99800 || !reflect.DeepEqual(s.cards, beforeCards) || !reflect.DeepEqual(s.containerCards, beforeContainer) {
		t.Fatal("repeated evolution spent unavailable fame or changed state on rejection")
	}
}

func TestDivergentEvolutionRejectsWithoutSpendingFame(t *testing.T) {
	for _, name := range []string{"gold", "target", "material definition", "warehouse fame", "locked", "deck", "duplicate", "stack"} {
		t.Run(name, func(t *testing.T) {
			s := divergentEvolutionTestAccount(t)
			s.cards[1].Fame = 3
			s.containerCards[0].Fame = 2
			uniqueIDs, containerIDs := []int64{2}, []int64{3}
			var stackIDs []int
			switch name {
			case "gold":
				s.gold = 99
			case "target":
				delete(s.cardTemplates, 11)
			case "material definition":
				s.containerCards[0].Fame = 4
				delete(s.cardDefinitions, 21)
			case "warehouse fame":
				s.containerCards[0].Fame = 1
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

func TestDivergentEvolutionConsumesOnlyDepletedMaterials(t *testing.T) {
	for _, input := range []struct {
		name                         string
		inventoryFame, warehouseFame int
	}{
		{"inventory exact", 3, 4},
		{"warehouse exact", 8, 2},
		{"both exact", 3, 2},
	} {
		t.Run(input.name, func(t *testing.T) {
			s := divergentEvolutionTestAccount(t)
			s.cards[1].Fame = input.inventoryFame
			s.containerCards[0].Fame = input.warehouseFame
			// Put the base after a potentially consumed card to exercise index changes.
			s.cards[0], s.cards[1] = s.cards[1], s.cards[0]
			unselected := CardInfo{UniqueID: 4, CardID: 20, Level: 1, Fame: 3}
			unselectedContainer := CardInfo{UniqueID: 5, CardID: 21, Level: 1, Fame: 2}
			var err error
			unselected, err = s.normalizeCardLocked(unselected)
			if err != nil {
				t.Fatal(err)
			}
			unselectedContainer, err = s.normalizeCardLocked(unselectedContainer)
			if err != nil {
				t.Fatal(err)
			}
			s.cards = append(s.cards, unselected)
			s.containerCards = append(s.containerCards, unselectedContainer)
			oldInventory, oldWarehouse := s.cards[0], s.containerCards[0]
			base := s.cards[1]
			result, gold, _, err := s.EvolveCard(1, 11, []int64{2}, []int64{3}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if result.UniqueID != base.UniqueID || result.CardID != 11 || result.Fame != base.Fame || result.Love != base.Love || gold != 99900 {
				t.Fatalf("evolved base or gold differs: %+v, %d", result, gold)
			}
			snapshot := s.Snapshot(gamestate.State{})
			inventory := CardsFromState(snapshot.Cards, 0)
			warehouse := CardsFromState(snapshot.ContainerCards, 0)
			for _, material := range []struct {
				cards []CardInfo
				old   CardInfo
				cost  int
			}{
				{inventory, oldInventory, 3},
				{warehouse, oldWarehouse, 2},
			} {
				index := cardIndexByUniqueID(material.cards, material.old.UniqueID)
				if material.old.Fame == material.cost {
					if index >= 0 {
						t.Fatalf("depleted card %d remains in save snapshot", material.old.UniqueID)
					}
					continue
				}
				want := material.old
				want.Fame -= material.cost
				want.HP, want.Attack, want.Magic, want.Mind = want.Fame, want.Fame, want.Fame, want.Fame
				if index < 0 || !EqualCardInfo(material.cards[index], want) {
					t.Fatalf("material %d was consumed or incorrectly updated: %+v", want.UniqueID, material.cards)
				}
			}
			inventoryCount, warehouseCount := 3, 2
			if input.inventoryFame == 3 {
				inventoryCount--
			}
			if input.warehouseFame == 2 {
				warehouseCount--
			}
			if len(inventory) != inventoryCount || len(warehouse) != warehouseCount ||
				!EqualCardInfo(inventory[len(inventory)-1], unselected) ||
				!EqualCardInfo(warehouse[len(warehouse)-1], unselectedContainer) {
				t.Fatalf("evolution changed unselected cards or inventory counts: %+v, %+v", snapshot.Cards, snapshot.ContainerCards)
			}
		})
	}
}
