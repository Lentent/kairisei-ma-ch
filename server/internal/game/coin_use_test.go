package game

import (
	"testing"

	"kairisei.local/server/internal/gamestate"
)

func TestCardExpansionChargesOnlyAcceptedCapacity(t *testing.T) {
	for _, container := range []bool{false, true} {
		s := &Account{cardMax: 5920, cardContainerMax: 5920, coinFree: 640, highestDeckRank: 1}
		extend, capacity := s.ExtendCardCapacity, s.CardCapacity
		if container {
			extend, capacity = s.ExtendCardContainerCapacity, s.CardContainerCapacity
		}
		for option, want := range []int{5925, 5950, 6000} {
			if err := extend(option); err != nil || capacity() != want {
				t.Fatalf("container=%v expansion=%d capacity=%d err=%v", container, option, capacity(), err)
			}
		}
		snapshot := s.Snapshot(gamestate.State{})
		stored := snapshot.User.CardMax
		if container {
			stored = snapshot.User.CardContainerMax
		}
		if s.coinFree != 0 || stored != 6000 {
			t.Fatal("capacity or charge missing from snapshot")
		}
		s.coinFree = 400
		for _, option := range []int{-1, 0, 1, 2, 3} {
			if extend(option) == nil || s.coinFree != 400 || capacity() != 6000 {
				t.Fatal("rejected expansion changed capacity or crystals")
			}
		}
		s.cardMax, s.cardContainerMax, s.coinFree = 5900, 5900, 39
		if extend(0) == nil || capacity() != 5900 || s.coinFree != 39 {
			t.Fatal("unaffordable expansion changed state")
		}
	}
}
