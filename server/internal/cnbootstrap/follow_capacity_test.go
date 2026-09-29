package cnbootstrap

import (
	"testing"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/testfixture"
)

func TestLegacyFollowCapacityUpgradePersists(t *testing.T) {
	state := testfixture.RuntimeState(t)
	state.Friends.FollowMax = 50
	account, err := game.New(state)
	if err != nil {
		t.Fatal(err)
	}
	if got := account.FollowMaximum(); got != 500 {
		t.Fatalf("follow maximum = %d, want 500", got)
	}
	if got := account.FriendMaximum(); got != state.User.FriendMax {
		t.Fatalf("mutual friend maximum changed: %d, want %d", got, state.User.FriendMax)
	}
	saved := account.Snapshot(state)
	if got := saved.Friends.FollowMax; got != 500 {
		t.Fatalf("saved follow maximum = %d, want 500", got)
	}
	reloaded, err := game.New(saved)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.FollowMaximum(); got != 500 {
		t.Fatalf("persisted follow maximum = %d, want 500", got)
	}
}
