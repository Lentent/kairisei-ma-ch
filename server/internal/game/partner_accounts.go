package game

import (
	"kairisei.local/server/internal/gamestate"
)

func DecksFromState(source []gamestate.Deck) []DeckInfo {
	decks := make([]DeckInfo, len(source))
	for index, deck := range source {
		decks[index] = DeckInfo{
			ArthurType:           deck.ArthurType,
			Index:                deck.Index,
			JobType:              deck.JobType,
			LeaderCardIndex:      deck.LeaderCardIndex,
			CardUniqueIDs:        append([]int64(nil), deck.CardUniqueIDs...),
			SupportCardUniqueIDs: append([]int64(nil), deck.SupportCardUniqueIDs...),
			SphereUniqueIDs:      fixedInt64Slots(deck.SphereUniqueIDs, DeckSphereSlots),
			BuddyUniqueIDs:       fixedInt64Slots(deck.BuddyUniqueIDs, deckBuddySlots),
			Name:                 deck.Name,
			IsActive:             deck.IsActive,
			IsRental:             deck.IsRental,
			DeckRank:             deck.DeckRank,
		}
	}
	return decks
}
