package game

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"kairisei.local/server/internal/gamestate"
)

func TestLevelFriendCapacitySurvivesSnapshotAndFollowCeiling(t *testing.T) {
	raw, err := os.ReadFile("../../config/cn602-player-progression-runtime.json")
	if err != nil {
		t.Fatal(err)
	}
	var policy gamestate.PlayerProgressionPolicy
	if err := json.Unmarshal(raw, &policy); err != nil {
		t.Fatal(err)
	}
	s := &Account{playerProgression: policy, currentLevel: 47, currentFriendMax: 50, followMax: 50,
		bp: 10, bpMax: policy.BattlePointMaximum(47), bpRecoveryInterval: 3 * time.Minute, apRecoveryInterval: 2 * time.Hour}
	s.applyPlayerExperienceLocked(policy.ExperienceRequired(47))
	view := s.PlayerProgressionState()
	if view.Level != 48 || view.FriendMax != 51 || s.FriendMaximum() != 51 || s.FollowMaximum() != 65 {
		t.Fatalf("level-up is blocked by follow ceiling: %+v, follow=%d", view, s.FollowMaximum())
	}
	if saved := s.Snapshot(gamestate.State{}); saved.User.FriendMax != 51 || saved.User.Level != 48 {
		t.Fatal("level friend capacity not persisted")
	}
	s.followMax = 80
	if s.FollowMaximum() != 80 {
		t.Fatal("larger configured follow capacity was reduced")
	}
}
