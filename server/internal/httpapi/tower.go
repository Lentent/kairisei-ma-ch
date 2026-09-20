package httpapi

import (
	"net/http"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

func (a *API) towerQuestShow(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		TowerID int `json:"towerid"`
	}
	if err := decodeExact(request, []string{"towerid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	profile, progress, consumed, err := a.account.TowerQuestShowState(payload.TowerID)
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
			"0": game.BoolInt(progress.LastRankUp),
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
	profile, progress, err := a.account.TowerQuestRankingState(payload.TowerID)
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
	for _, card := range a.account.ShowCards() {
		if card.UniqueID == a.initialState.User.LeaderCardUniqueID {
			leaderLevel = card.Level
			leaderLove = card.Love
			leaderFame = int16(card.Fame)
			break
		}
	}
	a.writeProtocol(writer, map[string]any{
		"0": []any{map[string]any{
			"0":  1,
			"1":  a.initialState.User.UserID,
			"2":  a.account.UserLevel(),
			"3":  a.account.UserName(),
			"4":  rank,
			"5":  floor,
			"6":  int64(len(progress.ClearedFloors)) * 1000,
			"7":  rankName,
			"8":  a.initialState.User.LeaderCardID,
			"9":  leaderLevel,
			"10": leaderLove,
			"11": leaderFame,
		}},
	})
}

func towerRewardWire(reward gamestate.Reward) map[string]any {
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
