package game

import (
	"errors"
	"sort"

	"kairisei.local/server/internal/gamestate"
)

func (s *Account) TowerQuestShowState(
	towerID int,
) (gamestate.TowerQuestProfile, gamestate.TowerQuestProgress, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	profile, exists := s.towerQuestProfiles[towerID]
	if !exists || !profile.ClientEntryPublished {
		return gamestate.TowerQuestProfile{}, gamestate.TowerQuestProgress{}, false,
			errors.New("tower quest is not published")
	}
	progress, exists := s.towerQuestProgress[towerID]
	if !exists || !profile.ClientEntryPublished {
		return gamestate.TowerQuestProfile{}, gamestate.TowerQuestProgress{}, false,
			errors.New("tower quest progress is unavailable")
	}
	responseProgress := cloneTowerQuestProgress(progress)
	consumed := progress.LastResult != ""
	if consumed {
		progress.LastResult = ""
		progress.LastRankUp = false
		progress.LastResultLoseCount = 0
		s.towerQuestProgress[towerID] = progress
	}
	return cloneTowerQuestProfile(profile), responseProgress, consumed, nil
}

func (s *Account) TowerQuestRankingState(
	towerID int,
) (gamestate.TowerQuestProfile, gamestate.TowerQuestProgress, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	profile, exists := s.towerQuestProfiles[towerID]
	if !exists || !profile.ClientEntryPublished {
		return gamestate.TowerQuestProfile{}, gamestate.TowerQuestProgress{},
			errors.New("tower quest is not published")
	}
	progress, exists := s.towerQuestProgress[towerID]
	if !exists {
		return gamestate.TowerQuestProfile{}, gamestate.TowerQuestProgress{},
			errors.New("tower quest progress is unavailable")
	}
	return cloneTowerQuestProfile(profile), cloneTowerQuestProgress(progress), nil
}

func (s *Account) TowerQuestIDs() []int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]int, 0, len(s.towerQuestProfiles))
	for towerID, profile := range s.towerQuestProfiles {
		if !profile.ClientEntryPublished {
			continue
		}
		result = append(result, towerID)
	}
	sort.Ints(result)
	return result
}

func (s *Account) ShowCards() []CardInfo {
	cards, _ := s.Show()
	return cards
}
