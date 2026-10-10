package multiplayer

import (
	"kairisei.local/server/internal/gamestate"
	"os"
	"testing"
)

func customActionInt(n int) *int { return &n }

func TestCustomEnemyIceDragon3DFallbackWithResources(t *testing.T) {
	root := os.Getenv("CUSTOM_BOSS_RESOURCE_ROOT")
	if root == "" {
		t.Skip("optional resource set check")
	}
	catalog, err := LoadOperationsEnemySkills(root)
	if err != nil {
		t.Fatal(err)
	}
	levels, err := LoadOperationsEnemyLevels(root)
	if err != nil {
		t.Fatal(err)
	}
	e := newChargedEnemyTestEngine()
	e.catalog = catalog
	e.enemyCount = 3
	e.turn = 1
	for i, id := range []int{30010151, 30010152, 30010153} {
		e.enemies[i] = battleEnemy{EnemyID: id, MemberType: i + 5, HP: 1000, MaxHP: 1000, Level: levels[id], CustomAnimationModel: "3d"}
	}
	for i := range e.players {
		e.players[i].Attribute = "FIRE"
	}
	a := gamestate.TeamBattleEnemyAction{Turn: 1, SkillID: 44320106, FunctionID: 44320106, Target: "THIEF", Buffs: []gamestate.TeamBattleEnemyBuffOverride{{RoleIndex: 0, Value: customActionInt(5000), Duration: customActionInt(4)}}}
	rows, err := e.executeEnemyActionCandidate(&e.enemies[0], enemyActionCandidate{action: CombatEnemyAction{SkillID: a.SkillID}, custom: &a})
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].Args[5] == 44320106 || catalog.EnemySkillRoles[int(rows[0].Args[5])][0].Effect3D == "" || e.players[2].Attack != 5000 || e.players[2].Effects[0].Remaining != 4 {
		t.Fatalf("ice dragon buff/3D reference incorrect: rows=%v player=%+v", rows, e.players[2])
	}
	t.Logf("ice dragon 3D fallback role set: %d", rows[0].Args[5])
}

func TestCustomEnemyBuffValuesAreIndependentAndAnimationFallsBack(t *testing.T) {
	e := newChargedEnemyTestEngine()
	e.turn = 1
	e.catalog.EnemySkills[99] = []CombatSkillDefinition{{ID: 99, FunctionID: 99, Target: "USER_ALL", Kind: "SUPPORT"}}
	e.catalog.EnemySkillRoles[99] = []CombatSkillRole{
		{RoleIndex: 0, Function: "ATK_UP_FIXED", Target: "SELECT", Effect2D: "player_buff", Parameters: [10]string{"3", "ATK", "1000", "1000", "0", "0"}},
		{RoleIndex: 1, Function: "ATK_UP_FIXED", Target: "SELECT", Parameters: [10]string{"3", "INT", "1000", "1000", "0", "0"}},
	}
	a := gamestate.TeamBattleEnemyAction{Turn: 1, SkillID: 99, FunctionID: 99, Target: "AUTO", Buffs: []gamestate.TeamBattleEnemyBuffOverride{{RoleIndex: 0, Value: customActionInt(5000), Duration: customActionInt(7)}, {RoleIndex: 1, Value: customActionInt(2500)}}}
	skill, roles, err := e.catalog.CustomEnemySkill(a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.executeEnemyActionCandidate(&e.enemies[0], enemyActionCandidate{action: CombatEnemyAction{SkillID: 99}, custom: &a}); err != nil {
		t.Fatal(err)
	}
	for _, p := range e.players {
		if p.Attack != 5000 || p.Magic != 2500 || p.Effects[0].Remaining != 7 || p.Effects[1].Remaining != 3 {
			t.Fatalf("per-effect overrides lost: %+v", p)
		}
	}
	if e.catalog.EnemySkillRoles[99][0].CustomBuffValue != nil || e.catalog.EnemySkillRoles[99][0].Parameters[0] != "3" {
		t.Fatal("shared source mutated")
	}
	// A native 3D buff with the same role layout can supply only the visuals.
	e.catalog.EnemySkills[100] = []CombatSkillDefinition{{ID: 100, FunctionID: 100, Target: "ENEMY_ALL", Kind: "SUPPORT"}}
	e.catalog.EnemySkillRoles[100] = []CombatSkillRole{{Function: "ATK_UP_BY_SELF_PARAM", Effect3D: "enemy_buff"}, {Function: "ATK_UP_FIXED"}}
	enemy := &e.enemies[0]
	enemy.CustomAnimationModel = "3d"
	enemy.Level.Actions = []CombatEnemyAction{{Category: "skill", SkillID: 100}}
	visual := e.customEnemyPresentation(enemy, a, skill, roles)
	if visual.ID != 99 || visual.FunctionID != 100 || visual.Target != "USER_ALL" {
		t.Fatal("missing 3D animation was not replaced without changing identity/targets")
	}
	rows, err := e.executeEnemyActionCandidate(enemy, enemyActionCandidate{action: CombatEnemyAction{SkillID: 99}, custom: &a})
	if err != nil || rows[0].Command != resultSkill || rows[0].Args[1] != 99 || rows[0].Args[5] != 100 {
		t.Fatal("animation reference not sent to client", err, rows)
	}
	a.AnimationSource = true
	if e.customEnemyPresentation(enemy, a, skill, roles).FunctionID != 99 {
		t.Fatal("source animation preference ignored")
	}
	a.AnimationSource = false
	a.AnimationFunctionID = 1
	if _, _, err = e.catalog.CustomEnemySkill(a); err == nil {
		t.Fatal("incompatible attack animation accepted for buff")
	}
	a.AnimationFunctionID = 0
	a.Buffs[0].Count = customActionInt(2)
	if _, _, err = e.catalog.CustomEnemySkill(a); err == nil {
		t.Fatal("unsupported buff field accepted")
	}
	a.Buffs[0].Count = nil
	a.Buffs[0].RoleIndex = 4
	if _, _, err = e.catalog.CustomEnemySkill(a); err == nil {
		t.Fatal("out-of-layout effect accepted")
	}
}

func TestCustomEnemyCriticalBarrierEnchantAndDrawOverrides(t *testing.T) {
	cases := []struct {
		function string
		params   [10]string
		edit     gamestate.TeamBattleEnemyBuffOverride
		check    func(*testing.T, *BattleEngine)
	}{
		{"CRITICAL_UP", [10]string{"3", "100", "0"}, gamestate.TeamBattleEnemyBuffOverride{Value: customActionInt(75), Duration: customActionInt(6)}, func(t *testing.T, e *BattleEngine) {
			p := e.players[2]
			if len(p.Effects) != 1 || p.Effects[0].Rate != 750 || p.Effects[0].Remaining != 6 {
				t.Fatalf("critical override wrong: %+v", p.Effects)
			}
		}},
		{"ATTACK_BARRIER", [10]string{"2", "1000", "0", "1", "ALL"}, gamestate.TeamBattleEnemyBuffOverride{Value: customActionInt(5000), Count: customActionInt(3), Duration: customActionInt(4)}, func(t *testing.T, e *BattleEngine) {
			p := e.players[2]
			if len(p.Effects) != 1 || p.Effects[0].Value != 5000 || p.Effects[0].Uses != 3 || p.Effects[0].Remaining != 4 {
				t.Fatalf("barrier override wrong: %+v", p.Effects)
			}
		}},
		{"ENCHANT", [10]string{"3", "1000", "1000", "0", "0", "FIRE"}, gamestate.TeamBattleEnemyBuffOverride{Value: customActionInt(1234)}, func(t *testing.T, e *BattleEngine) {
			p := e.players[2]
			if len(p.Effects) != 1 || p.Effects[0].Value != 1234 || p.Effects[0].Attribute != "FIRE" {
				t.Fatalf("enchant override wrong: %+v", p.Effects)
			}
		}},
		{"DEAL_BONUS", [10]string{"1"}, gamestate.TeamBattleEnemyBuffOverride{Value: customActionInt(2)}, func(t *testing.T, e *BattleEngine) {
			p := e.players[2]
			if len(p.Effects) != 1 || p.Effects[0].Value != 2 {
				t.Fatalf("draw override wrong: %+v", p.Effects)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.function, func(t *testing.T) {
			e := newChargedEnemyTestEngine()
			e.turn = 1
			e.catalog.EnemySkills[99] = []CombatSkillDefinition{{ID: 99, FunctionID: 99, Target: "USER_ONE", Kind: "SUPPORT"}}
			e.catalog.EnemySkillRoles[99] = []CombatSkillRole{{Function: c.function, Target: "SELECT", Parameters: c.params}}
			a := gamestate.TeamBattleEnemyAction{Turn: 1, SkillID: 99, FunctionID: 99, Target: "THIEF", Buffs: []gamestate.TeamBattleEnemyBuffOverride{c.edit}}
			if _, err := e.executeEnemyActionCandidate(&e.enemies[0], enemyActionCandidate{action: CombatEnemyAction{SkillID: 99}, custom: &a}); err != nil {
				t.Fatal(err)
			}
			c.check(t, e)
			for _, i := range []int{0, 1, 3} {
				if len(e.players[i].Effects) != 0 {
					t.Fatal("buff went to wrong profession")
				}
			}
		})
	}
}

func TestCustomEnemyCombinedLogicChargeOrderAndUnscheduledTurns(t *testing.T) {
	e := newChargedEnemyTestEngine()
	e.catalog.EnemySkills[2] = []CombatSkillDefinition{{ID: 2, FunctionID: 2, Target: "USER_ONE", Kind: "SORCERY", DamageKind: "MAGIC"}}
	e.catalog.EnemySkillRoles[2] = []CombatSkillRole{{Function: "ATTACK_AA", Target: "SELECT", Parameters: [10]string{"50", "0", "0", "0", "1", "INT", "0", "FIRE", "MAGIC"}}}
	enemy := &e.enemies[0]
	enemy.Level.Actions = append(enemy.Level.Actions, CombatEnemyAction{Slot: 0, Category: "normal", SkillID: 1, Target: "MERCENARY", ActionCost: 1, MaxUses: 100, Rate: 100})
	enemy.IncludeOriginalActions = true
	enemy.CustomActions = []gamestate.TeamBattleEnemyAction{{Turn: 1, RepeatEvery: 2, SkillID: 2, FunctionID: 2, Target: "THIEF"}}
	if _, err := e.TurnPhase(); err != nil {
		t.Fatal(err)
	}
	if enemy.ChargedActionCount != 1 || enemy.ActionConsumed != 1 || e.enemyAttackSign(enemy) != 3 {
		t.Fatal("combined mode lost native charge/budget or either attack sign")
	}
	e.phase = battlePhaseUserAttack
	rows, err := e.EnemyPhase()
	if err != nil {
		t.Fatal(err)
	}
	var functions []int64
	for _, r := range rows {
		if r.Command == resultSkill {
			functions = append(functions, r.Args[5])
		}
	}
	if len(functions) != 2 || functions[0] != 1 || functions[1] != 2 || e.players[0].HP != 900 || e.players[2].HP != 950 {
		t.Fatalf("native action must precede custom action: functions=%v players=%+v", functions, e.players)
	}
	if enemy.ActionConsumed != 1 || e.enemyUses[enemyActionUseKey(enemy, enemy.Level.Actions[0])] != 1 {
		t.Fatal("custom action consumed native budget or recharged the native skill")
	}
	for _, turn := range []int{2, 3} {
		e.turn, e.phase = turn, battlePhaseUserAttack
		enemy.ActionConsumed, enemy.ChargedActionCount = 0, 0
		if _, err = e.EnemyPhase(); err != nil {
			t.Fatal(err)
		}
	}
	if e.players[0].HP != 700 || e.players[2].HP != 900 {
		t.Fatal("native normal action must remain on unscheduled turns; custom repeats only on turn 3")
	}
	enemy.Effects = []battleEffect{{Function: "STAN", Remaining: 2}}
	e.turn, e.phase = 5, battlePhaseUserAttack
	rows, err = e.EnemyPhase()
	if err != nil || battleResultsContainCommand(rows, resultSkill) || e.players[0].HP != 700 || e.players[2].HP != 900 {
		t.Fatal("combined mode ignored stun", err)
	}
}

func TestCustomEnemyCombinedOpeningAndEmptySchedule(t *testing.T) {
	e := newChargedEnemyTestEngine()
	e.catalog.EnemyAIOrders = map[int]CombatEnemyAIOrder{1: {ID: 1, Fields: enemyAIConditionFields("NULL")}}
	enemy := &e.enemies[0]
	enemy.Level.Actions = []CombatEnemyAction{{Slot: 1, Category: "skill", SkillID: 1, Target: "MERCENARY", AIConditionID: 1, ActionCost: 1, MaxUses: 100, Rate: 100}}
	enemy.IncludeOriginalActions = true
	enemy.CustomActions = []gamestate.TeamBattleEnemyAction{{Turn: 0, SkillID: 1, FunctionID: 1, Target: "THIEF"}}
	if _, err := e.executeEnemyFirstAttack(); err != nil || e.players[0].HP != 900 || e.players[2].HP != 900 {
		t.Fatal("combined opening lost native or custom first attack", err)
	}
	enemy.CustomActions = []gamestate.TeamBattleEnemyAction{}
	e.turn = 1
	if len(e.buildEnemyActionPlan(enemy)) != 1 {
		t.Fatal("combined empty list disabled original logic")
	}
	enemy.IncludeOriginalActions = false
	if len(e.buildEnemyActionPlan(enemy)) != 0 {
		t.Fatal("replacement empty list no longer disables original logic")
	}
	enemy.IncludeOriginalActions = true
	enemy.HP = 0
	if len(e.buildEnemyActionPlan(enemy)) != 0 {
		t.Fatal("dead enemy still acts in combined mode")
	}
}

func TestCustomEnemyPlayerBuffSingleAndAllTargets(t *testing.T) {
	for _, scope := range []string{"USER_ONE", "USER_ALL"} {
		t.Run(scope, func(t *testing.T) {
			e := newChargedEnemyTestEngine()
			e.turn = 1
			e.catalog.EnemySkills[99] = []CombatSkillDefinition{{ID: 99, FunctionID: 99, Target: scope, Kind: "SUPPORT"}}
			e.catalog.EnemySkillRoles[99] = []CombatSkillRole{{Function: "ATK_UP_FIXED", Target: "SELECT", Parameters: [10]string{"3", "ATK", "1000", "1000", "0", "0"}}}
			for i := range e.players {
				e.players[i].Attack = 200
			}
			e.enemies[0].CustomActions = []gamestate.TeamBattleEnemyAction{{Turn: 1, SkillID: 99, FunctionID: 99, Target: "THIEF"}}
			plan := e.buildEnemyActionPlan(&e.enemies[0])
			if len(plan) != 1 {
				t.Fatal("buff action not scheduled")
			}
			if _, err := e.executeEnemyActionCandidate(&e.enemies[0], plan[0]); err != nil {
				t.Fatal(err)
			}
			for i, p := range e.players {
				want := 200
				if scope == "USER_ALL" || i == 2 {
					want = 1200
					if len(p.Effects) != 1 || p.Effects[0].Remaining != 3 || p.Effects[0].Delta != 1000 {
						t.Fatalf("player %d missing 3-turn +1000 buff: %+v", i, p.Effects)
					}
				}
				if p.Attack != want || p.HP != 1000 {
					t.Fatalf("wrong buff recipient or unexpected damage: player %d attack=%d HP=%d", i, p.Attack, p.HP)
				}
			}
			if len(e.enemies[0].Effects) != 0 {
				t.Fatal("player buff applied to boss")
			}
		})
	}
}

func TestCustomEnemyTurnsBorrowedBranchOrderAndDamage(t *testing.T) {
	e := newChargedEnemyTestEngine()
	// A donor not referenced by this enemy's original action list, including a
	// conditional variant which normal source AI would not select here.
	e.catalog.EnemySkills[2] = []CombatSkillDefinition{
		{ID: 2, FunctionID: 20, Name: "借来的单体", Target: "USER_ONE", Kind: "ATTACK", DamageKind: "PHYSICS"},
		{ID: 2, FunctionID: 21, Name: "条件分支", Target: "USER_ONE", Kind: "ATTACK", DamageKind: "PHYSICS", BranchCondition: "IMPOSSIBLE"},
	}
	for _, fn := range []int{20, 21} {
		e.catalog.EnemySkillRoles[fn] = []CombatSkillRole{{Function: "ATTACK_AA", Target: "SELECT", Parameters: [10]string{"10000", "0", "0", "0", "1", "ATK", "0", "FIRE", "PHYSICS"}}}
	}
	for i := range e.players {
		e.players[i].HP, e.players[i].MaxHP, e.players[i].Defense = 30000, 30000, 1000
	}
	e.enemies[0].CustomActions = []gamestate.TeamBattleEnemyAction{
		{Turn: 1, SkillID: 2, FunctionID: 21, Target: "MERCENARY", Power: customActionInt(5000)},
		{Turn: 1, RepeatEvery: 2, SkillID: 2, FunctionID: 20, Target: "MILLIONAIRE", PowerRate: customActionInt(50), Hits: customActionInt(2)},
	}
	if _, err := e.TurnPhase(); err != nil {
		t.Fatal(err)
	}
	if e.enemies[0].ChargedActionCount != 0 || e.enemyAttackSign(&e.enemies[0]) != 1 {
		t.Fatal("template charge leaked or custom attack sign missing")
	}
	e.phase = battlePhaseUserAttack
	rows, err := e.EnemyPhase()
	if err != nil {
		t.Fatal(err)
	}
	var functions []int64
	for _, r := range rows {
		if r.Command == resultSkill {
			functions = append(functions, r.Args[5])
		}
	}
	if len(functions) != 2 || functions[0] != 21 || functions[1] != 20 || e.players[0].HP != 26000 || e.players[1].HP != 22000 {
		t.Fatalf("custom branch/order/damage failed: functions=%v HP=%d,%d", functions, e.players[0].HP, e.players[1].HP)
	}
	if e.catalog.EnemySkillRoles[20][0].Parameters[0] != "10000" || e.catalog.EnemySkillRoles[20][0].Parameters[4] != "1" {
		t.Fatal("shared donor skill was mutated")
	}
	e.turn, e.phase = 2, battlePhaseUserAttack
	rows, err = e.EnemyPhase()
	if err != nil || battleResultsContainCommand(rows, resultSkill) || !battleResultsContainCommand(rows, resultWaitAndSee) {
		t.Fatal("unconfigured turn inherited template action", err)
	}
	e.turn, e.phase = 3, battlePhaseUserAttack
	if _, err = e.EnemyPhase(); err != nil || e.players[1].HP != 14000 || e.players[0].HP != 26000 {
		t.Fatal("loop repeated wrong action", err)
	}
	e.enemies[0].Effects = []battleEffect{{Function: "STAN", Remaining: 2}}
	e.turn, e.phase = 5, battlePhaseUserAttack
	rows, err = e.EnemyPhase()
	if err != nil || battleResultsContainCommand(rows, resultSkill) || e.players[1].HP != 14000 {
		t.Fatal("custom action ignored stun", err)
	}
}

func TestCustomEnemyActionSnapshotAndWaveIsolation(t *testing.T) {
	base, members := nextBattleFixture(t)
	base.catalog.EnemySkills = map[int][]CombatSkillDefinition{1: {{ID: 1, FunctionID: 1, Target: "USER_ONE"}}}
	base.catalog.EnemySkillRoles = map[int][]CombatSkillRole{1: {{Function: "ATTACK_AA"}}}
	power := 5000
	rows := []gamestate.TeamBattleEnemyOverride{
		{BattleIndex: 0, EnemyIndex: 0, Stats: gamestate.TeamBattleEnemyStats{EnemyID: 1, HP: 100, Attribute: "FIRE"}, IncludeOriginalActions: true, Actions: []gamestate.TeamBattleEnemyAction{{Turn: 1, SkillID: 1, FunctionID: 1, Target: "AUTO", Power: &power}}},
		{BattleIndex: 1, EnemyIndex: 0, Stats: gamestate.TeamBattleEnemyStats{EnemyID: 1, HP: 100, Attribute: "FIRE"}, Actions: []gamestate.TeamBattleEnemyAction{}},
	}
	e, err := newBattleEngine(base.catalog, RoomSpec{EnemyPartyID: 1, CostInitial: 3, HoldMax: 5, EnemyOverrides: rows}, members)
	if err != nil {
		t.Fatal(err)
	}
	power = 1
	rows[0].Actions[0].Turn = 9
	rows[0].IncludeOriginalActions = false
	if !e.enemies[0].IncludeOriginalActions || *e.enemies[0].CustomActions[0].Power != 5000 || e.enemies[0].CustomActions[0].Turn != 1 {
		t.Fatal("room did not freeze action configuration")
	}
	e.phase, e.endType = battlePhaseEnded, 1
	next, err := e.NextBattle(1, nil)
	if err != nil || next.enemies[0].IncludeOriginalActions || next.enemies[0].CustomActions == nil || len(next.enemies[0].CustomActions) != 0 || len(e.enemies[0].CustomActions) != 1 {
		t.Fatal("wave action config was lost", err)
	}
	bad := rows
	bad[0].Actions[0].FunctionID = 999
	if _, err = newBattleEngine(base.catalog, RoomSpec{EnemyPartyID: 1, CostInitial: 3, HoldMax: 5, EnemyOverrides: bad}, members); err == nil {
		t.Fatal("unknown skill function accepted")
	}
}

func TestCustomEnemyOpeningAndEmptySchedule(t *testing.T) {
	e := newChargedEnemyTestEngine()
	e.enemies[0].CustomActions = []gamestate.TeamBattleEnemyAction{{Turn: 0, SkillID: 1, FunctionID: 1, Target: "MERCENARY"}}
	rows, err := e.executeEnemyFirstAttack()
	if err != nil || !battleResultsContainCommand(rows, resultEnemyFirstAttack) || e.players[0].HP != 900 {
		t.Fatal("opening action not executed", err)
	}
	e.enemies[0].CustomActions = []gamestate.TeamBattleEnemyAction{}
	e.turn = 1
	if len(e.buildEnemyActionPlan(&e.enemies[0])) != 0 || len(e.buildEnemyChargeStartPlan(&e.enemies[0])) != 0 || e.enemyAttackSign(&e.enemies[0]) != 0 {
		t.Fatal("empty schedule reverted to template")
	}
}
