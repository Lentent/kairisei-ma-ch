package httpapi

import (
	"reflect"
	"testing"

	"kairisei.local/server/internal/release"
)

func TestDropPlanSettlementUsesDestroyedEnemiesWithoutReroll(t *testing.T) {
	zero, guaranteed := 0, 1000000
	profile := release.TeamBattleRewardProfile{
		ResultRewards: []release.Reward{{Type: 0, Num: 20}, {Type: 4, Num: 999}},
		EnemyDrops: []release.TeamBattleEnemyDrop{
			{BattleIndex: 0, EnemyIndex: 0, Reward: release.Reward{Type: 4, Num: 10}},
			{BattleIndex: 1, EnemyIndex: 0, Reward: release.Reward{Type: 4, Num: 20}, ChancePerMillion: &guaranteed},
			{BattleIndex: 1, EnemyIndex: 1, Reward: release.Reward{Type: 4, Num: 30}},
			{BattleIndex: 1, EnemyIndex: 2, Reward: release.Reward{Type: 4, Num: 40}, ChancePerMillion: &zero},
		},
	}
	plan, err := planTeamBattleDrops(profile, []int8{0, 0}, "same-start")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 3 {
		t.Fatalf("chance filtering produced %v", plan)
	}
	context := teamBattleContext{DropPlanSet: true, DropPlan: plan}
	// First wave body, then final body+part. The 999 aggregate must not also be granted.
	got := settledTeamBattleRewards(profile, context, teamBattleDropReport{EnemyDeadBits: []int{1, 3}})
	want := []release.Reward{{Type: 0, Num: 20}, {Type: 4, Num: 10}, {Type: 4, Num: 20}, {Type: 4, Num: 30}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("destroyed-enemy settlement=%v, want %v", got, want)
	}
	profile.EnemyDrops[2].Reward.Num = 9999
	got = settledTeamBattleRewards(profile, context, teamBattleDropReport{EnemyDeadBits: []int{1, 1}})
	if !reflect.DeepEqual(got, want[:3]) {
		t.Fatal("surviving part or edited runtime table changed frozen rewards")
	}
	// Native DESTRUCT may mark dead without releasing drops. Multiplayer trusts
	// the engine's immutable released ledger, not merely the dead bitmask.
	got = settledTeamBattleRewards(profile, context, teamBattleDropReport{Authoritative: true, ReleasedDrops: plan[:1]})
	if !reflect.DeepEqual(got, want[:2]) {
		t.Fatal("authoritative release ledger was not respected")
	}
}

func TestAwakeBodyRewardIsInheritedOnce(t *testing.T) {
	profile := release.TeamBattleRewardProfile{ResultRewards: []release.Reward{{Type: 0, Num: 900}}, EnemyDrops: []release.TeamBattleEnemyDrop{
		{BattleIndex: 0, Reward: release.Reward{Type: 4, Num: 900}},
		{BattleIndex: 0, Reward: release.Reward{Type: 8, RewardTypeID: 4000, Num: 600}},
		{BattleIndex: 0, EnemyIndex: 1, Reward: release.Reward{Type: 13, RewardTypeID: 20000003, Num: 1}},
		{BattleIndex: 0, EnemyIndex: 2, Reward: release.Reward{Type: 13, RewardTypeID: 20000003, Num: 1}},
	}}
	types := []int8{1, 4}
	plan, err := planTeamBattleDrops(profile, types, "awake")
	if err != nil {
		t.Fatal(err)
	}
	context := teamBattleContext{DropPlanSet: true, DropPlan: plan, BattleEnemyTypes: types}
	for _, bits := range [][]int{{6, 1}, {7}, {7, 1}} {
		rewards := settledTeamBattleRewards(profile, context, teamBattleDropReport{EnemyDeadBits: bits})
		if len(rewards) != 5 || rewards[1].Num != 900 || rewards[2].Num != 600 {
			t.Fatalf("death bits %v: rewards=%+v", bits, rewards)
		}
	}
	if got := settledTeamBattleRewards(profile, context, teamBattleDropReport{EnemyDeadBits: []int{6, 0}}); len(got) != 3 {
		t.Fatalf("a living body must not release drops: %+v", got)
	}
	if got := teamBattleDropPlanWire(plan, types, 1); len(got) != 1 {
		t.Fatalf("awake wire should carry only body drops: %+v", got)
	}
}
