package httpapi

import (
	"testing"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

func TestAwakeBodyRewardIsInheritedOnce(t *testing.T) {
	profile := gamestate.TeamBattleRewardProfile{ResultRewards: []gamestate.Reward{{Type: 0, Num: 900}}, EnemyDrops: []gamestate.TeamBattleEnemyDrop{
		{BattleIndex: 0, Reward: gamestate.Reward{Type: 4, Num: 900}},
		{BattleIndex: 0, Reward: gamestate.Reward{Type: 8, RewardTypeID: 4000, Num: 600}},
		{BattleIndex: 0, EnemyIndex: 1, Reward: gamestate.Reward{Type: 13, RewardTypeID: 20000003, Num: 1}},
		{BattleIndex: 0, EnemyIndex: 2, Reward: gamestate.Reward{Type: 13, RewardTypeID: 20000003, Num: 1}},
	}}
	types := []int8{1, 4}
	plan, err := game.PlanTeamBattleDrops(profile, types, "awake")
	if err != nil {
		t.Fatal(err)
	}
	context := game.TeamBattleContext{DropPlanSet: true, DropPlan: plan, BattleEnemyTypes: types}
	for _, bits := range [][]int{{6, 1}, {7}, {7, 1}} {
		rewards := game.SettledTeamBattleRewards(profile, context, game.TeamBattleDropReport{EnemyDeadBits: bits})
		if len(rewards) != 5 || rewards[1].Num != 900 || rewards[2].Num != 600 {
			t.Fatalf("death bits %v: rewards=%+v", bits, rewards)
		}
	}
	if got := game.SettledTeamBattleRewards(profile, context, game.TeamBattleDropReport{EnemyDeadBits: []int{6, 0}}); len(got) != 3 {
		t.Fatalf("a living body must not release drops: %+v", got)
	}
	if got := teamBattleDropPlanWire(plan, types, 1); len(got) != 1 {
		t.Fatalf("awake wire should carry only body drops: %+v", got)
	}
}
