package httpapi

import (
	"reflect"
	"testing"

	"kairisei.local/server/internal/release"
)

func TestPartnerDeckAttributes(t *testing.T) {
	view := friendPointPartnerView{Cards: map[int64]cardInfo{1: {CardID: 100}, 2: {CardID: 200}, 3: {CardID: 300}}, CardAttributes: map[int]uint8{100: 1, 200: 1 | 16, 300: 2}}
	deck := deckInfo{CardUniqueIDs: []int64{1, 2, 0}, SupportCardUniqueIDs: []int64{3}}
	if got := view.deckAttributeCounts(deck); !reflect.DeepEqual(got, []int{0, 2, 0, 0, 0, 1}) {
		t.Fatalf("main deck attributes %v", got)
	}
}

func TestRentalProfessionAndKindCounts(t *testing.T) {
	state := release.State{User: release.User{UserID: 1000001, Name: "player", ActiveArthurType: 1}, Avatars: make([]release.Avatar, 4), SupportDeck: release.SupportDeckState{UnlockSlotNums: make([]int8, 4)}}
	ids := make([]int64, 10)
	for i := range ids {
		ids[i] = int64(i + 1)
		state.Cards = append(state.Cards, release.Card{UniqueID: ids[i], CardID: 100 + i})
	}
	state.Decks = []release.Deck{{ArthurType: 1, IsActive: 1, CardUniqueIDs: ids}, {ArthurType: 3, IsRental: 1, CardUniqueIDs: ids}}
	view, ok := friendPointPartnerViewFromState(state)
	if !ok || view.ArthurType != 3 {
		t.Fatalf("rental profession: %+v, %v", view, ok)
	}
	view.CardKinds = map[int]int{100: 1, 101: 2, 102: 3, 103: 4, 104: 5, 105: 6, 106: 7, 107: 1, 108: 2, 109: 3}
	deck, ok := view.activeDeck()
	if !ok || deck.ArthurType != 3 {
		t.Fatal("wrong rental deck")
	}
	if got := view.deckKindCounts(deck); !reflect.DeepEqual(got, []int{0, 2, 2, 2, 1, 1, 1, 1}) {
		t.Fatalf("kinds %v", got)
	}
	// A public thief rental must not replace the mercenary deck selected by
	// this same player as a solo partner, including its appearance and slots.
	state.Avatars[0].CostumeID = 24
	state.Avatars[2].CostumeID = 26
	state.SupportDeck.UnlockSlotNums[0] = 2
	state.SupportDeck.UnlockSlotNums[2] = 3
	own, ok := partnerViewFromState(state, 1)
	if !ok || own.ArthurType != 1 || own.Avatar.CostumeID != 24 || own.SupportUnlocked != 2 {
		t.Fatalf("own selected profession replaced by public rental: %+v, %v", own, ok)
	}
	if _, found := exactDeck(own.Decks, own.ArthurType, 0); !found {
		t.Fatal("own selected deck disappeared")
	}
	public, ok := friendPointPartnerViewFromState(state)
	if !ok || public.ArthurType != 3 || public.Avatar.CostumeID != 26 {
		t.Fatal("own selection changed public rental")
	}
}
