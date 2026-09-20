package game

import (
	"reflect"
	"testing"

	"kairisei.local/server/internal/gamestate"
)

func TestBuddyRemovalPreservesValidFormation(t *testing.T) {
	for _, removed := range []int64{1, 2} {
		s := &Account{
			buddies: []gamestate.Buddy{{UniqueID: 1}, {UniqueID: 2}},
			decks:   []DeckInfo{{BuddyUniqueIDs: []int64{1, 2, 0}}},
		}
		before := append([]gamestate.Buddy(nil), s.buddies...)
		s.clearBuddyIDsFromDecksLocked(map[int64]struct{}{removed: {}})
		want := []int64{0, 0, 0}
		if removed == 2 {
			want[0] = 1
		}
		if !reflect.DeepEqual(s.decks[0].BuddyUniqueIDs, want) || !reflect.DeepEqual(s.buddies, before) {
			t.Fatalf("removing %d: decks=%v inventory=%v", removed, s.decks, s.buddies)
		}
	}
}
