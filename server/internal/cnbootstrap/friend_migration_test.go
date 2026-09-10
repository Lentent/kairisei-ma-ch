package cnbootstrap

import (
	"testing"

	"kairisei.local/server/internal/release"
)

func TestNormalizeCNLegacyStaticFriends(t *testing.T) {
	state := release.State{
		Friends: release.FriendCollectionState{
			FollowMax: 50,
			Users:     []release.Friend{{UserID: 1000002, Name: "legacy fake"}},
		},
	}
	if !normalizeCNLegacyStaticFriends(&state) {
		t.Fatal("legacy friend rows were not removed")
	}
	if state.Friends.Users == nil || len(state.Friends.Users) != 0 {
		t.Fatalf("legacy friend rows = %#v, want non-nil empty list", state.Friends.Users)
	}
	if state.Friends.FollowMax != 50 {
		t.Fatalf("follow maximum = %d, want 50", state.Friends.FollowMax)
	}
	if normalizeCNLegacyStaticFriends(&state) {
		t.Fatal("empty friend state must be idempotent")
	}
}
