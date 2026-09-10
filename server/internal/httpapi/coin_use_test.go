package httpapi

import (
	"testing"

	"kairisei.local/server/internal/release"
)

func TestCardExpansionChargesOnlyAcceptedCapacity(t *testing.T) {
	s := &store{cardMax: 5920, coinFree: 640, highestDeckRank: 1}
	for option, want := range []int{5925, 5950, 6000} {
		if err := s.extendCardCapacity(option); err != nil || s.cardMax != want {
			t.Fatalf("expansion %d: capacity=%d err=%v", option, s.cardMax, err)
		}
	}
	if s.coinFree != 0 || s.snapshot(release.State{}).User.CardMax != 6000 {
		t.Fatal("capacity or crystal charge was not persisted")
	}
	s.coinFree = 400
	for _, option := range []int{-1, 0, 1, 2, 3} {
		if s.extendCardCapacity(option) == nil || s.coinFree != 400 || s.cardMax != 6000 {
			t.Fatal("rejected expansion changed capacity or charged crystals")
		}
	}
	s.cardMax, s.coinFree = 5900, 39
	if s.extendCardCapacity(0) == nil || s.cardMax != 5900 || s.coinFree != 39 {
		t.Fatal("unaffordable expansion changed state")
	}
}
