package httpapi

import (
	"errors"
	"net/http"
	"sort"

	"kairisei.local/server/internal/release"
)

func (a *API) towerQuestShow(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		TowerID int `json:"towerid"`
	}
	if err := decodeExact(request, []string{"towerid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	profile, progress, consumed, err := a.store.towerQuestShowState(payload.TowerID)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if consumed && !a.persistOrError(writer) {
		return
	}

	wins := []any{}
	loses := []any{}
	if progress.LastResult == "win" {
		wins = append(wins, map[string]any{
			"0": boolInt(progress.LastRankUp),
			"1": progress.LastResultLoseCount,
		})
	} else if progress.LastResult == "lose" {
		loses = append(loses, map[string]any{"0": progress.LastResultLoseCount})
	}
	ranks := make([]any, 0, len(profile.Ranks))
	for _, rank := range profile.Ranks {
		ranks = append(ranks, map[string]any{"0": rank.Rank, "1": rank.RankName})
	}
	floors := make([]any, 0, len(profile.Floors))
	for _, floor := range profile.Floors {
		clearRewards := make([]any, 0, len(floor.ClearRewards))
		for _, reward := range floor.ClearRewards {
			clearRewards = append(clearRewards, towerRewardWire(reward))
		}
		floors = append(floors, map[string]any{
			"0": floor.Rank,
			"1": floor.Floor,
			"2": floor.Boss,
			"3": clearRewards,
			"4": profile.ItemID,
			"5": profile.ItemUse,
		})
	}
	a.writeProtocol(writer, map[string]any{
		"0": wins,
		"1": loses,
		"2": progress.Floor,
		"3": progress.LastBattleFloor,
		"4": progress.LoseCount,
		"5": profile.LoseCountMax,
		"6": ranks,
		"7": floors,
		"8": profile.AllClearText,
	})
}

func (a *API) towerRankingShow(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		TowerID int `json:"towerid"`
	}
	if err := decodeExact(request, []string{"towerid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	profile, progress, err := a.store.towerQuestRankingState(payload.TowerID)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	rank := 1
	floor := progress.Floor
	if floor == 0 {
		floor = len(profile.Floors)
	}
	if floor > 0 && floor <= len(profile.Floors) {
		rank = profile.Floors[floor-1].Rank
	}
	rankName := profile.Ranks[rank-1].RankName
	leaderLevel := 1
	leaderLove := 0
	leaderFame := int16(1)
	for _, card := range a.store.showCards() {
		if card.UniqueID == a.release.State.User.LeaderCardUniqueID {
			leaderLevel = card.Level
			leaderLove = card.Love
			leaderFame = int16(card.Fame)
			break
		}
	}
	a.writeProtocol(writer, map[string]any{
		"0": []any{map[string]any{
			"0":  1,
			"1":  a.release.State.User.UserID,
			"2":  a.store.userLevel(),
			"3":  a.store.userName(),
			"4":  rank,
			"5":  floor,
			"6":  int64(len(progress.ClearedFloors)) * 1000,
			"7":  rankName,
			"8":  a.release.State.User.LeaderCardID,
			"9":  leaderLevel,
			"10": leaderLove,
			"11": leaderFame,
		}},
	})
}

func (s *store) towerQuestShowState(
	towerID int,
) (release.TowerQuestProfile, release.TowerQuestProgress, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	profile, exists := s.towerQuestProfiles[towerID]
	if !exists || !profile.ClientEntryPublished {
		return release.TowerQuestProfile{}, release.TowerQuestProgress{}, false,
			errors.New("tower quest is not published")
	}
	progress, exists := s.towerQuestProgress[towerID]
	if !exists || !profile.ClientEntryPublished {
		return release.TowerQuestProfile{}, release.TowerQuestProgress{}, false,
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

func (s *store) towerQuestRankingState(
	towerID int,
) (release.TowerQuestProfile, release.TowerQuestProgress, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	profile, exists := s.towerQuestProfiles[towerID]
	if !exists || !profile.ClientEntryPublished {
		return release.TowerQuestProfile{}, release.TowerQuestProgress{},
			errors.New("tower quest is not published")
	}
	progress, exists := s.towerQuestProgress[towerID]
	if !exists {
		return release.TowerQuestProfile{}, release.TowerQuestProgress{},
			errors.New("tower quest progress is unavailable")
	}
	return cloneTowerQuestProfile(profile), cloneTowerQuestProgress(progress), nil
}

func (s *store) towerQuestIDs() []int {
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

func (s *store) showCards() []cardInfo {
	cards, _ := s.show()
	return cards
}

func towerRewardWire(reward release.Reward) map[string]any {
	return map[string]any{
		"0": reward.Type,
		"1": reward.Num,
		"2": reward.RewardTypeID,
		"3": reward.CardLevel,
		"4": reward.CardFame,
		"5": reward.CardLove,
		"6": append([]int16(nil), reward.CardSkillLevels...),
	}
}
