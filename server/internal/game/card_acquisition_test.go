package game

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"kairisei.local/server/internal/gamestate"
)

func TestCardSourcesFollowPublishedRewardsAndRemainingEligibility(t *testing.T) {
	card := func(id int) gamestate.Reward { return gamestate.Reward{Type: 6, RewardTypeID: id, Num: 1} }
	pool := func(id int) []gamestate.WeightedReward {
		return []gamestate.WeightedReward{{Reward: card(id), Weight: 1}}
	}
	zero := 0
	s := &Account{
		cardDefinitions:    map[int]gamestate.Card{},
		stackCardTemplates: map[int]gamestate.CardStack{},
		teamBattleSolo:     json.RawMessage(`{"9":[],"10":[{"0":100,"4":"活动","10":[{"0":101,"4":"超级","10":0,"26":0}]}],"11":[],"12":[]}`),
		gachas: []gamestate.GachaProfile{{GachaID: 201, GroupID: 200, Name: "阶段池", PlayCount: 1, PlayCountMax: 3,
			Steps: []gamestate.GachaStep{{RewardPool: pool(1)}, {RewardPool: pool(2)}, {RewardPool: pool(3)}, {RewardPool: pool(4)}},
			Gifts: []gamestate.GachaGiftRule{{FromPlay: 1, ToPlay: 1, Rewards: []gamestate.Reward{card(1)}}, {FromPlay: 3, Rewards: []gamestate.Reward{card(5)}}}}},
		tradeShopProfiles: map[int]gamestate.TradeShopProfile{301: {TradeShopID: 301, Name: "兑换所", Lineups: []gamestate.TradeShopLineupProfile{
			{LineupID: 311, StockNum: 1, Rewards: []gamestate.Reward{card(6)}}, {LineupID: 312, Disabled: true, Rewards: []gamestate.Reward{card(1)}},
		}}},
		tradeShopPurchases: map[int]int{},
		missions:           []gamestate.Mission{{Info: gamestate.MissionInfo{MissionID: 401, Title: "任务"}, RewardPresent: gamestate.Present{Reward: card(7)}}},
		cardActions:        gamestate.CardActionState{EvolutionTransitions: []gamestate.EvolutionTransition{{FromCardID: 1, ToCardID: 8}}},
		towerQuestProfiles: map[int]gamestate.TowerQuestProfile{501: {TowerID: 501, Name: "塔", ClientEntryPublished: true,
			Floors: []gamestate.TowerQuestFloorProfile{{Floor: 1, Boss: json.RawMessage(`{"0":511}`)}}}},
		towerQuestProgress: map[int]gamestate.TowerQuestProgress{501: {TowerID: 501}},
	}
	ids := []int{}
	for id := 1; id <= 13; id++ {
		s.cardDefinitions[id] = gamestate.Card{CardID: id}
		ids = append(ids, id)
	}
	delete(s.cardDefinitions, 10)
	s.stackCardTemplates[10] = gamestate.CardStack{CardID: 10}
	material := gamestate.Reward{Type: 13, RewardTypeID: 10, Num: 1}
	profiles := []gamestate.TeamBattleRewardProfile{
		{BossID: 101, EnemyDrops: []gamestate.TeamBattleEnemyDrop{{Reward: material}, {Reward: card(1), ChancePerMillion: &zero}},
			FirstClearRewards: []gamestate.Reward{card(9)}, ScorePolicy: &gamestate.TeamBattleScorePolicy{Grades: []gamestate.TeamBattleScoreGrade{{Rewards: []gamestate.Reward{card(11)}}}}},
		{BossID: 511, TowerID: 501, TowerFloor: 1, FirstClearRewards: []gamestate.Reward{card(12)}},
	}
	read := func(groups map[int]struct{}) map[int][]howToGetCardEntry {
		t.Helper()
		lists, err := s.HowToGetCards(ids, profiles, groups)
		if err != nil {
			t.Fatal(err)
		}
		rows := map[int][]howToGetCardEntry{}
		for _, list := range lists {
			rows[list.CardID] = list.GetCards
		}
		return rows
	}
	check := func(rows map[int][]howToGetCardEntry, id, kind, target, subtype int) {
		t.Helper()
		if len(rows[id]) != 1 || rows[id][0].Type != kind || rows[id][0].ContentID != target || rows[id][0].Subtype != subtype {
			t.Fatalf("card %d sources: %+v; want type=%d target=%d subtype=%d", id, rows[id], kind, target, subtype)
		}
	}
	rows := read(nil)
	for _, id := range []int{1, 4, 13} {
		check(rows, id, 0, 0, 0)
	}
	for _, id := range []int{2, 3, 5} {
		check(rows, id, 7, 201, 0)
	}
	check(rows, 6, 8, 301, 0)
	check(rows, 7, 9, 401, 0)
	check(rows, 8, 4, 1, 0)
	for _, id := range []int{9, 10, 11} {
		check(rows, id, 10, 101, 0)
	}
	check(rows, 12, 10, 501, 1)
	// Permanent ownership and successful play counters gate only their own sources.
	s.tradeShopPurchases[311] = 1
	s.missions[0].Info.State = 2
	s.ApplyEvolutionRestrictions(NewEvolutionRestrictions([]EvolutionPath{{1, 8}}))
	s.teamBattleScores = map[int]gamestate.TeamBattleScoreProgress{101: {Claimed: 1}}
	s.towerQuestProgress[501] = gamestate.TowerQuestProgress{TowerID: 501, ClearedFloors: []int{1}}
	s.teamBattleSolo = json.RawMessage(strings.Replace(string(s.teamBattleSolo), `"10":0`, `"10":2`, 1))
	s.gachas[0].PlayCountMax = 2
	rows = read(nil)
	for _, id := range []int{3, 5, 6, 7, 8, 9, 11, 12} {
		check(rows, id, 0, 0, 0)
	}
	check(rows, 10, 10, 101, 0)
	check(read(map[int]struct{}{}), 10, 0, 0, 0)
	s.disabledTeamBattleBossIDs = map[int]bool{101: true}
	check(read(nil), 10, 0, 0, 0)
	s.tradeShopPurchases[311] = 0
	shop := s.tradeShopProfiles[301]
	shop.StartTime = int(time.Now().Add(time.Hour).Unix())
	s.tradeShopProfiles[301] = shop
	check(read(nil), 6, 0, 0, 0)
}
