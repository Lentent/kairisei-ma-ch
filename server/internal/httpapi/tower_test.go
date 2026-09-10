package httpapi

import (
	"testing"

	"kairisei.local/server/internal/release"
)

func TestTowerQuestRankingStateRejectsUnpublishedContent(t *testing.T) {
	const towerID = 7
	state := &store{
		towerQuestProfiles: map[int]release.TowerQuestProfile{
			towerID: {TowerID: towerID, ClientEntryPublished: false},
		},
		towerQuestProgress: map[int]release.TowerQuestProgress{
			towerID: {TowerID: towerID},
		},
	}

	if _, _, err := state.towerQuestRankingState(towerID); err == nil {
		t.Fatal("unpublished tower ranking unexpectedly returned a local rank row")
	}

	profile := state.towerQuestProfiles[towerID]
	profile.ClientEntryPublished = true
	state.towerQuestProfiles[towerID] = profile
	if _, _, err := state.towerQuestRankingState(towerID); err != nil {
		t.Fatalf("published tower ranking was rejected: %v", err)
	}
}
