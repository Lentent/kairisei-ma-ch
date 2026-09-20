package game

import (
	"testing"
)

func TestLiveBattleSeedsAreNotFixedReplaySeeds(t *testing.T) {
	seeds := make(map[int]bool)
	for range 8 {
		seed := NewBattleSeed()
		if seed <= 0 || seed > 2147483647 {
			t.Fatalf("seed outside native Int32 range: %d", seed)
		}
		seeds[seed] = true
	}
	if len(seeds) == 1 {
		t.Fatal("every new battle used the same seed")
	}
}
