package game

import (
	"testing"

	"kairisei.local/server/internal/gamestate"
)

func TestTowerQuestRankingStateRejectsUnpublishedContent(t *testing.T) {
	const towerID = 7
	state := &Account{
		towerQuestProfiles: map[int]gamestate.TowerQuestProfile{
			towerID: {TowerID: towerID, ClientEntryPublished: false},
		},
		towerQuestProgress: map[int]gamestate.TowerQuestProgress{
			towerID: {TowerID: towerID},
		},
	}

	if _, _, err := state.TowerQuestRankingState(towerID); err == nil {
		t.Fatal("unpublished tower ranking unexpectedly returned a local rank row")
	}

	profile := state.towerQuestProfiles[towerID]
	profile.ClientEntryPublished = true
	state.towerQuestProfiles[towerID] = profile
	if _, _, err := state.TowerQuestRankingState(towerID); err != nil {
		t.Fatalf("published tower ranking was rejected: %v", err)
	}
}
