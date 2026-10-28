package multiplayer

import (
	"kairisei.local/server/internal/gamestate"
	"reflect"
	"testing"
)

func TestOriginalEnemySkillPreservesChargeScheduleAndSource(t *testing.T) {
	baseline, edited := newChargedEnemyTestEngine(), newChargedEnemyTestEngine()
	enemy := &edited.enemies[0]
	enemy.OriginalSkills = []gamestate.TeamBattleEnemyOriginalSkill{{Slot: 15, SkillID: 1, FunctionID: 1, Power: customActionInt(300), PowerRate: customActionInt(50), Hits: customActionInt(2)}}
	if err := edited.catalog.ValidateOriginalEnemySkills(enemy.Level, enemy.OriginalSkills); err != nil {
		t.Fatal(err)
	}
	for _, e := range []*BattleEngine{baseline, edited} {
		if _, err := e.TurnPhase(); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(baseline.enemies[0].ChargedActions, enemy.ChargedActions) || baseline.rng != edited.rng || !reflect.DeepEqual(baseline.enemyUses, edited.enemyUses) {
		t.Fatal("native scheduling changed")
	}
	for _, e := range []*BattleEngine{baseline, edited} {
		e.phase = battlePhaseUserAttack
		if _, err := e.EnemyPhase(); err != nil {
			t.Fatal(err)
		}
	}
	if baseline.players[0].HP != 900 || edited.players[0].HP != 700 || edited.players[1].HP != 1000 {
		t.Fatalf("damage/target incorrect: %d %d", baseline.players[0].HP, edited.players[0].HP)
	}
	if enemy.ActionConsumed != 1 || edited.enemyUses[enemyActionUseKey(enemy, enemy.Level.Actions[0])] != 1 {
		t.Fatal("charge schedule counted twice")
	}
	if edited.catalog.EnemySkillRoles[1][0].CustomAttack != nil || edited.catalog.EnemySkillRoles[1][0].Parameters[0] != "100" {
		t.Fatal("shared catalog mutated")
	}
}

func TestOriginalEnemySkillStillSelectsBranchAndSlot(t *testing.T) {
	for _, tc := range []struct{ turn, slot, want int }{{1, 15, 900}, {2, 15, 600}, {2, 14, 900}} {
		e := newChargedEnemyTestEngine()
		e.turn = tc.turn
		branch := e.catalog.EnemySkills[1][0]
		branch.FunctionID, branch.BranchPriority, branch.BranchCondition, branch.BranchParameters = 2, 1, "TURN", [5]string{"2", "2"}
		e.catalog.EnemySkills[1] = append(e.catalog.EnemySkills[1], branch)
		e.catalog.EnemySkillRoles[2] = append([]CombatSkillRole(nil), e.catalog.EnemySkillRoles[1]...)
		e.enemies[0].OriginalSkills = []gamestate.TeamBattleEnemyOriginalSkill{{Slot: 15, SkillID: 1, FunctionID: 2, Power: customActionInt(400)}}
		a := e.enemies[0].Level.Actions[0]
		a.Slot = tc.slot
		if _, err := e.executeEnemyActionCandidate(&e.enemies[0], enemyActionCandidate{action: a}); err != nil {
			t.Fatal(err)
		}
		if e.players[0].HP != tc.want {
			t.Fatalf("turn %d slot %d: hp %d, want %d", tc.turn, tc.slot, e.players[0].HP, tc.want)
		}
		// An explicitly scheduled custom action using the same skill is independent.
		custom := gamestate.TeamBattleEnemyAction{SkillID: 1, FunctionID: 2, Target: "MERCENARY"}
		if _, err := e.executeEnemyActionCandidate(&e.enemies[0], enemyActionCandidate{action: a, custom: &custom}); err != nil {
			t.Fatal(err)
		}
		if e.players[0].HP != tc.want-100 {
			t.Fatal("original override leaked into custom action")
		}
	}
}

func TestOriginalEnemySkillValidationAndRoomSnapshot(t *testing.T) {
	e := newChargedEnemyTestEngine()
	valid := gamestate.TeamBattleEnemyOriginalSkill{Slot: 15, SkillID: 1, FunctionID: 1, Power: customActionInt(300)}
	for _, change := range []func(*gamestate.TeamBattleEnemyOriginalSkill){
		func(s *gamestate.TeamBattleEnemyOriginalSkill) { s.Slot = 0 },
		func(s *gamestate.TeamBattleEnemyOriginalSkill) { s.SkillID = 2 },
		func(s *gamestate.TeamBattleEnemyOriginalSkill) { s.FunctionID = 2 },
		func(s *gamestate.TeamBattleEnemyOriginalSkill) { s.Power = customActionInt(-1) },
		func(s *gamestate.TeamBattleEnemyOriginalSkill) { s.Hits = customActionInt(21) },
	} {
		bad := valid
		change(&bad)
		if e.catalog.ValidateOriginalEnemySkills(e.enemies[0].Level, []gamestate.TeamBattleEnemyOriginalSkill{bad}) == nil {
			t.Fatalf("accepted invalid override: %+v", bad)
		}
	}
	if e.catalog.ValidateOriginalEnemySkills(e.enemies[0].Level, []gamestate.TeamBattleEnemyOriginalSkill{valid, valid}) == nil {
		t.Fatal("accepted duplicate override")
	}
	input := []gamestate.TeamBattleEnemyOverride{{OriginalSkills: []gamestate.TeamBattleEnemyOriginalSkill{valid}}}
	snapshot := gamestate.CloneTeamBattleEnemyOverrides(input)
	*input[0].OriginalSkills[0].Power = 99
	if *snapshot[0].OriginalSkills[0].Power != 300 {
		t.Fatal("room snapshot shares editable value")
	}
}
