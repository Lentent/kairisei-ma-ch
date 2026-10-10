package gamestate

import (
	"encoding/json"
	"testing"
)

func TestCustomActionsValidateQueuesAndPreserveInheritance(t *testing.T) {
	a := TeamBattleEnemyAction{Turn: 1, RepeatEvery: 2, SkillID: 1, FunctionID: 1, Target: "AUTO"}
	if err := ValidateTeamBattleEnemyActions([]TeamBattleEnemyAction{a}); err != nil {
		t.Fatal(err)
	}
	rows := make([]TeamBattleEnemyAction, 21)
	for i := range rows {
		rows[i] = a
	}
	if ValidateTeamBattleEnemyActions(rows) == nil {
		t.Fatal("oversized simultaneous queue accepted")
	}
	a.Turn = -1
	if ValidateTeamBattleEnemyActions([]TeamBattleEnemyAction{a}) == nil {
		t.Fatal("negative turn accepted")
	}
	for _, raw := range []string{`{"actions":null}`, `{"actions":[]}`, `{}`} {
		var row TeamBattleEnemyOverride
		if err := json.Unmarshal([]byte(raw), &row); err != nil {
			t.Fatal(err)
		}
		cloned := CloneTeamBattleEnemyOverrides([]TeamBattleEnemyOverride{row})[0]
		if (cloned.Actions == nil) != (row.Actions == nil) {
			t.Fatal("empty schedule/inheritance changed during clone")
		}
	}
}

func TestCustomBuffOverridesCloneAndValidate(t *testing.T) {
	value, duration := 5000, 7
	a := TeamBattleEnemyAction{Turn: 1, SkillID: 1, FunctionID: 1, Target: "AUTO", Buffs: []TeamBattleEnemyBuffOverride{{RoleIndex: 0, Value: &value, Duration: &duration}}}
	clone := CloneTeamBattleEnemyActions([]TeamBattleEnemyAction{a})
	value, duration = 1, 2
	a.Buffs[0].RoleIndex = 3
	if clone[0].Buffs[0].RoleIndex != 0 || *clone[0].Buffs[0].Value != 5000 || *clone[0].Buffs[0].Duration != 7 {
		t.Fatal("buff snapshot was not deep copied")
	}
	clone[0].Buffs = append(clone[0].Buffs, clone[0].Buffs[0])
	if ValidateTeamBattleEnemyActions(clone) == nil {
		t.Fatal("duplicate effect override accepted")
	}
	clone[0].Buffs = clone[0].Buffs[:1]
	bad := -1
	clone[0].Buffs[0].Value = &bad
	if ValidateTeamBattleEnemyActions(clone) == nil {
		t.Fatal("negative buff accepted")
	}
}
