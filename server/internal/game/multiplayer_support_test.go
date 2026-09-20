package game

import (
	"testing"

	"kairisei.local/server/internal/gamestate"
)

func TestMultiplayerMemberFreezesSupportSlotsAndLove(t *testing.T) {
	deck := DeckInfo{ArthurType: 1, JobType: 1, LeaderCardIndex: 0, SupportCardUniqueIDs: []int64{0, 11}}
	s := &Account{deckHonorIDs: []int{0, 0, 0, 0}, avatars: []gamestate.Avatar{{}}}
	for i := int64(1); i <= 10; i++ {
		deck.CardUniqueIDs = append(deck.CardUniqueIDs, i)
		s.cards = append(s.cards, CardInfo{UniqueID: i, CardID: int(i), Level: 1, HP: 100, Fame: 1})
	}
	s.cards = append(s.cards, CardInfo{UniqueID: 11, CardID: 123, Level: 60, Love: 5000, LoveMax: 10000})
	s.decks = []DeckInfo{deck}
	member, err := s.MultiplayerMember(0, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(member.SupportCards) != 1 || member.SupportCards[0].CardType != 12 || member.SupportCards[0].Love != 5000 || member.SupportCards[0].CardID != 123 || member.SupportCards[0].Level != 60 {
		t.Fatalf("support identity/love/empty slot lost: %+v", member.SupportCards)
	}
	s.cards[10].Love = 10000
	s.decks[0].SupportCardUniqueIDs[1] = 0
	if member.SupportCards[0].Love != 5000 || member.SupportCards[0].CardID != 123 {
		t.Fatal("editing an account mutated the frozen battle deck")
	}
}

func TestOwnerFallbackUsesSelectedDeckWithoutReplacingEmptySelection(t *testing.T) {
	s := &Account{deckHonorIDs: make([]int, 4), avatars: make([]gamestate.Avatar, 4)}
	for id := int64(1); id <= 10; id++ {
		s.cards = append(s.cards, CardInfo{UniqueID: id, CardID: int(id), Level: 1, HP: 100, Fame: 1})
	}
	for arthur := int8(2); arthur <= 4; arthur++ {
		for index := int8(0); index <= 1; index++ {
			deck := DeckInfo{ArthurType: arthur, JobType: arthur, Index: index, IsActive: index,
				Name: "first", CardUniqueIDs: make([]int64, 10)}
			if index == 1 {
				deck.Name = "selected second"
			}
			if arthur == 2 || (arthur == 3 && index == 0) {
				for i := range deck.CardUniqueIDs {
					deck.CardUniqueIDs[i] = int64(i + 1)
				}
			}
			s.decks = append(s.decks, deck)
		}
	}
	party := s.MultiplayerOwnerFallbackParty(1000001, 1)
	if len(party) != 1 || party[0].ArthurType != 2 || party[0].DeckName != "selected second" || party[0].UserID != 10000012 {
		t.Fatalf("fallback must use the selected playable deck, never another deck for an empty selection: %+v", party)
	}
	s.decks[1].CardUniqueIDs[0] = 0
	if party[0].DeckCards[0].CardID != 1 || len(s.MultiplayerOwnerFallbackParty(1000001, 1)) != 0 {
		t.Fatal("room deck snapshot changed or empty selected deck still became an AI")
	}
}
