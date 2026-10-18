package multiplayer

import (
	"reflect"
	"testing"
)

func singleToAllFixture(t *testing.T) *BattleEngine {
	t.Helper()
	engine, _ := nextBattleFixture(t)
	engine.turn, engine.enemyCount = 1, 3
	for i := 0; i < engine.enemyCount; i++ {
		engine.enemies[i] = engine.enemies[0]
		engine.enemies[i].MemberType = i + 5
		engine.enemies[i].HP, engine.enemies[i].MaxHP = 100000, 100000
		engine.enemies[i].Attack, engine.enemies[i].BaseAttack = 1000, 1000
	}
	for i := range engine.players {
		engine.players[i].HP = 500
	}
	return engine
}

func singleToAllAction(engine *BattleEngine, id, actor, target int, kind string, roles ...CombatSkillRole) battleAction {
	skill := CombatSkillDefinition{ID: id, FunctionID: id, Target: kind, Attribute: "FIRE", Cost: 1}
	engine.catalog.Cards[id] = CombatCardDefinition{ID: id, NormalSkillID: id}
	engine.catalog.PlayerSkills[id] = []CombatSkillDefinition{skill}
	for i := range roles {
		roles[i].SkillID, roles[i].RoleIndex = id, i
	}
	engine.catalog.PlayerSkillRoles[id] = roles
	return battleAction{memberType: actor, cardID: id, cardType: 1, cardLevel: 1, target: target, skill: skill, roles: roles}
}

func singleToAllActivator(engine *BattleEngine) battleAction {
	role := CombatSkillRole{Function: "ATK_UP_FIXED", Target: "SELECT", Parameters: [10]string{"1", "ATK", "1000", "10"}}
	action := singleToAllAction(engine, 98000001, 2, 2, "USER_ONE", role)
	card := engine.catalog.Cards[action.cardID]
	card.TurnSingleToAll = true
	engine.catalog.Cards[action.cardID] = card
	return action
}

func TestSingleToAllUsesExecutionOrderAndKeepsActivatorScope(t *testing.T) {
	engine := singleToAllFixture(t)
	attack := singleToAllAction(engine, 101, 1, 6, "ENEMY_ONE", CombatSkillRole{
		Function: "ATTACK_AA", Target: "SELECT", Parameters: [10]string{"200", "0", "0", "0", "1", "ATK", "0", "FIRE", "PHYSICS"},
	})
	if _, err := engine.executePlayerAction(attack, nil); err != nil {
		t.Fatal(err)
	}
	if engine.enemies[0].HP != 100000 || engine.enemies[1].HP == 100000 || engine.enemies[2].HP != 100000 {
		t.Fatal("an attack before the activator was expanded")
	}
	if _, err := engine.executePlayerAction(singleToAllActivator(engine), nil); err != nil {
		t.Fatal(err)
	}
	for i, player := range engine.players {
		want := 100
		if i == 1 {
			want += 10
		}
		if player.Attack != want {
			t.Fatalf("activator changed its own target: player %d attack=%d", i+1, player.Attack)
		}
	}
	before := [3]int{engine.enemies[0].HP, engine.enemies[1].HP, engine.enemies[2].HP}
	rows, err := engine.executePlayerAction(attack, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i, hp := range before {
		if hp-engine.enemies[i].HP != 200 {
			t.Fatalf("enemy %d damage=%d; want the full 200 per target", i, hp-engine.enemies[i].HP)
		}
	}
	code, _ := combatSkillTargetCode("ENEMY_ALL")
	if rows[0].Command != resultCardSkill || rows[0].Args[3] != 0 || rows[0].Args[5] != int64(code) {
		t.Fatalf("client direction does not announce all targets: %+v", rows[0])
	}
	if engine.catalog.PlayerSkills[101][0].Target != "ENEMY_ONE" || engine.catalog.PlayerSkillRoles[101][0].Target != "SELECT" {
		t.Fatal("expanded a shared master definition")
	}
	other, err := newBattleEngine(engine.catalog, RoomSpec{EnemyPartyID: 1}, mustSingleToAllMembers(t))
	if err != nil || other.singleToAllActive {
		t.Fatalf("effect leaked to a new room: %v", err)
	}
}

func mustSingleToAllMembers(t *testing.T) []Member {
	t.Helper()
	_, members := nextBattleFixture(t)
	return members
}

func TestSingleToAllExpandsAttackDebuffHealAndSupport(t *testing.T) {
	for _, tc := range []struct {
		name, kind, function, target string
		parameters                   [10]string
	}{
		{"attack inherited", "ENEMY_ONE", "ATTACK_AA", "SELECT", [10]string{"200", "0", "0", "0", "1", "ATK", "0", "FIRE", "PHYSICS"}},
		{"attack explicit", "ENEMY_ONE", "ATTACK_AA", "ENEMY_ONE", [10]string{"200", "0", "0", "0", "1", "ATK", "0", "FIRE", "PHYSICS"}},
		{"debuff", "ENEMY_ONE", "ATK_BREAK_FIXED", "SELECT", [10]string{"2", "ATK", "1000", "50"}},
		{"heal", "USER_ONE", "HEAL_FIXED", "SELECT", [10]string{"100", "0", "0", "0", "MND"}},
		{"support", "USER_ONE", "ATK_UP_FIXED", "SELECT", [10]string{"2", "ATK", "1000", "50"}},
		{"independent ally effect", "ENEMY_ONE", "ATK_UP_FIXED", "USER_ONE", [10]string{"2", "ATK", "1000", "50"}},
		{"independent enemy effect", "USER_ONE", "ATK_BREAK_FIXED", "ENEMY_ONE", [10]string{"2", "ATK", "1000", "50"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := singleToAllFixture(t)
			engine.singleToAllActive = true
			target := 2
			if tc.kind == "ENEMY_ONE" {
				target = 6
			}
			action := singleToAllAction(engine, 101, 1, target, tc.kind, CombatSkillRole{Function: tc.function, Target: tc.target, Parameters: tc.parameters})
			if _, err := engine.executePlayerAction(action, nil); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < engine.enemyCount; i++ {
				enemy := engine.enemies[i]
				if tc.function == "ATTACK_AA" && enemy.HP != 99800 || tc.function == "ATK_BREAK_FIXED" && enemy.Attack != 950 {
					t.Fatalf("enemy %d did not receive the full effect: HP=%d attack=%d", i, enemy.HP, enemy.Attack)
				}
			}
			for i, player := range engine.players {
				if tc.function == "HEAL_FIXED" && player.HP != 600 || tc.function == "ATK_UP_FIXED" && player.Attack != 150 {
					t.Fatalf("player %d did not receive the full effect: HP=%d attack=%d", i, player.HP, player.Attack)
				}
			}
		})
	}
}

func TestSingleToAllPreservesSelfAndTargetFilters(t *testing.T) {
	engine := singleToAllFixture(t)
	engine.singleToAllActive = true
	roles := []CombatSkillRole{
		{Function: "ATK_UP_FIXED", Target: "USER_ONE", ExcludeSelf: true, HasTargetAttributes: true, Attributes: [9]bool{true}, Parameters: [10]string{"1", "ATK", "1000", "50"}},
		{Function: "HP_CUT", Target: "SELF", Parameters: [10]string{"10"}},
	}
	for i := range engine.players {
		engine.players[i].Attribute = "FIRE"
	}
	engine.players[2].Attribute = "ICE"
	engine.players[3].HP = 0
	action := singleToAllAction(engine, 101, 1, 2, "USER_ONE", roles...)
	if _, err := engine.executePlayerAction(action, nil); err != nil {
		t.Fatal(err)
	}
	for i, player := range engine.players {
		wantAttack, wantHP := 100, 500
		if i == 1 {
			wantAttack = 150
		}
		if i == 0 {
			wantHP = 450
		} else if i == 3 {
			wantHP = 0
		}
		if player.Attack != wantAttack || player.HP != wantHP {
			t.Fatalf("player %d lost target/self restrictions: HP=%d attack=%d", i+1, player.HP, player.Attack)
		}
	}
	for _, target := range []string{"SELF", "USER_ALL", "ENEMY_ALL", "RANDOM", "DEAD_ENEMY_ONE"} {
		if singleToAllTarget(target) != target {
			t.Fatalf("changed special target %s", target)
		}
	}
	for _, cardType := range []int{0, 21, 22} {
		action.cardType = cardType
		if got := engine.applySingleToAll(action); !reflect.DeepEqual(got, action) {
			t.Fatalf("changed a non-hand action %d", cardType)
		}
	}
	action.cardType, action.sphereSlot = 1, 1
	if got := engine.applySingleToAll(action); !reflect.DeepEqual(got, action) {
		t.Fatal("changed a sphere")
	}
}

func TestSingleToAllExpiresAndDeadCasterCannotActivate(t *testing.T) {
	engine := singleToAllFixture(t)
	action := singleToAllActivator(engine)
	engine.players[action.memberType-1].HP = 0
	if _, err := engine.executePlayerAction(action, nil); err != nil || engine.singleToAllActive {
		t.Fatalf("dead caster activated the effect: %v", err)
	}
	engine.singleToAllActive, engine.phase = true, battlePhaseChaliceEnemy
	if _, err := engine.TurnPhase(); err != nil || engine.singleToAllActive {
		t.Fatalf("effect survived the next turn: %v", err)
	}
	engine.singleToAllActive, engine.phase, engine.endType = true, battlePhaseEnded, 1
	next, err := engine.NextBattle(1, nil)
	if err != nil || next.singleToAllActive {
		t.Fatalf("effect survived the next wave: %v", err)
	}
}

func TestSingleToAllFollowsSortedActionPriority(t *testing.T) {
	for _, priority := range []int{100, 900} {
		engine := singleToAllFixture(t)
		activator := singleToAllActivator(engine)
		activator.skill.PriorityPVE = priority
		attack := singleToAllAction(engine, 101, 1, 6, "ENEMY_ONE", CombatSkillRole{Function: "ATTACK_AA", Target: "ENEMY_ONE", Parameters: [10]string{"200", "0", "0", "0", "1", "ATK", "0", "FIRE", "PHYSICS"}})
		attack.skill.PriorityPVE = 400
		if _, err := engine.executeUserActions([]battleAction{attack, activator}); err != nil {
			t.Fatal(err)
		}
		want := 100000
		if priority < 400 {
			want -= 200
		}
		if engine.enemies[0].HP != want || engine.enemies[2].HP != want || engine.enemies[1].HP != 99800 {
			t.Fatalf("priority %d did not control activation order", priority)
		}
	}
}

func TestSingleToAllStandaloneSkillHasNoInheritedEffects(t *testing.T) {
	engine := singleToAllFixture(t)
	activator := singleToAllAction(engine, 98000001, 2, 2, "USER_ONE", CombatSkillRole{Function: CombatFunctionTurnSingleToAll, Target: "SELF"})
	beforePlayers, beforeEnemies := engine.players, engine.enemies
	rows, err := engine.executePlayerAction(activator, nil)
	if err != nil || !engine.singleToAllActive {
		t.Fatalf("standalone effect did not activate: %v", err)
	}
	if engine.catalog.Cards[activator.cardID].TurnSingleToAll {
		t.Fatal("standalone skill unexpectedly requires the legacy card flag")
	}
	for i, player := range engine.players {
		if player.HP != beforePlayers[i].HP || player.Attack != beforePlayers[i].Attack || len(player.Effects) != 0 {
			t.Fatalf("standalone skill retained player effects: member %d", i+1)
		}
	}
	for i := 0; i < engine.enemyCount; i++ {
		if engine.enemies[i].HP != beforeEnemies[i].HP || engine.enemies[i].Attack != beforeEnemies[i].Attack || len(engine.enemies[i].Effects) != 0 {
			t.Fatalf("standalone skill retained enemy effects: member %d", i+5)
		}
	}
	if len(rows) == 0 || rows[0].Command != resultCardSkill {
		t.Fatal("standalone skill lost its card animation header")
	}
	attack := singleToAllAction(engine, 101, 1, 6, "ENEMY_ONE", CombatSkillRole{Function: "ATTACK_AA", Target: "SELECT", Parameters: [10]string{"200", "0", "0", "0", "1", "ATK", "0", "FIRE", "PHYSICS"}})
	if _, err := engine.executePlayerAction(attack, nil); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < engine.enemyCount; i++ {
		if engine.enemies[i].HP != 99800 {
			t.Fatalf("standalone skill did not expand the following card: enemy %d", i)
		}
	}
}

func TestSingleToAllCombinedRoleActivatesAfterCompleteCard(t *testing.T) {
	engine := singleToAllFixture(t)
	action := singleToAllAction(engine, 98000001, 1, 2, "USER_ONE",
		CombatSkillRole{Function: CombatFunctionTurnSingleToAll, Target: "SELF"},
		CombatSkillRole{Function: "ATK_UP_FIXED", Target: "SELECT", Parameters: [10]string{"1", "ATK", "1000", "10"}},
	)
	if _, err := engine.executePlayerAction(action, nil); err != nil || !engine.singleToAllActive {
		t.Fatalf("combined role did not activate: %v", err)
	}
	for i, player := range engine.players {
		want := 100
		if i == 1 {
			want = 110
		}
		if player.Attack != want {
			t.Fatalf("marker expanded a later role in its own card: member %d", i+1)
		}
	}
}
