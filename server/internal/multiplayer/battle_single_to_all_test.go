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
	if err != nil || other.singleToAllMembers != 0 {
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
		{"self heal inherited", "SELF", "HEAL_FIXED", "SELECT", [10]string{"100", "0", "0", "0", "MND"}},
		{"self support inherited", "SELF", "ATK_UP_FIXED", "SELECT", [10]string{"2", "ATK", "1000", "50"}},
		{"self support explicit", "ENEMY_ONE", "ATK_UP_FIXED", "SELF", [10]string{"2", "ATK", "1000", "50"}},
		{"support", "USER_ONE", "ATK_UP_FIXED", "SELECT", [10]string{"2", "ATK", "1000", "50"}},
		{"independent ally effect", "ENEMY_ONE", "ATK_UP_FIXED", "USER_ONE", [10]string{"2", "ATK", "1000", "50"}},
		{"independent enemy effect", "USER_ONE", "ATK_BREAK_FIXED", "ENEMY_ONE", [10]string{"2", "ATK", "1000", "50"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := singleToAllFixture(t)
			engine.singleToAllMembers = 15
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

func TestSingleToAllExpandsSelfAndPreservesTargetFilters(t *testing.T) {
	engine := singleToAllFixture(t)
	engine.singleToAllMembers = 15
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
		wantAttack, wantHP := 100, 450
		if i == 1 {
			wantAttack = 150
		}
		if i == 3 {
			wantHP = 0
		}
		if player.Attack != wantAttack || player.HP != wantHP {
			t.Fatalf("player %d lost target restrictions: HP=%d attack=%d", i+1, player.HP, player.Attack)
		}
	}
	for _, target := range []string{"USER_ALL", "ENEMY_ALL", "RANDOM", "DEAD_ENEMY_ONE"} {
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
	if _, err := engine.executePlayerAction(action, nil); err != nil || engine.singleToAllMembers != 0 {
		t.Fatalf("dead caster activated the effect: %v", err)
	}
	engine.singleToAllMembers, engine.phase = 15, battlePhaseChaliceEnemy
	if _, err := engine.TurnPhase(); err != nil || engine.singleToAllMembers != 0 {
		t.Fatalf("effect survived the next turn: %v", err)
	}
	engine.singleToAllMembers, engine.phase, engine.endType = 15, battlePhaseEnded, 1
	next, err := engine.NextBattle(1, nil)
	if err != nil || next.singleToAllMembers != 0 {
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
	if err != nil || engine.singleToAllMembers == 0 {
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
	if _, err := engine.executePlayerAction(action, nil); err != nil || engine.singleToAllMembers == 0 {
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

func TestSingleToAllRecipientScopes(t *testing.T) {
	for _, tc := range []struct {
		scope string
		want  uint8
	}{{"SELF", 2}, {"USER_ONE", 4}, {"USER_ALL", 15}} {
		t.Run(tc.scope, func(t *testing.T) {
			engine := singleToAllFixture(t)
			activator := singleToAllAction(engine, 98000001, 2, 3, "USER_ONE",
				CombatSkillRole{Function: CombatFunctionTurnSingleToAll, Target: tc.scope, Parameters: [10]string{"1"}},
				CombatSkillRole{Function: "ATK_UP_FIXED", Target: "SELECT", Parameters: [10]string{"1", "ATK", "1000", "10"}},
			)
			if _, err := engine.executePlayerAction(activator, nil); err != nil {
				t.Fatal(err)
			}
			if engine.singleToAllMembers != tc.want {
				t.Fatalf("recipients = %04b, want %04b", engine.singleToAllMembers, tc.want)
			}
			for i, player := range engine.players {
				want := 100
				if i == 2 {
					want = 110
				}
				if player.Attack != want {
					t.Fatal("activator expanded its own combined effect")
				}
			}
			// Each of the four actors plays a card with two independent single-target
			// effects. Only recipient actors may expand either of them.
			for actor := 1; actor <= 4; actor++ {
				for i := range engine.players {
					engine.players[i].HP = 500
					engine.players[i].Attack = 100
					engine.players[i].Effects = nil
				}
				action := singleToAllAction(engine, 100+actor, actor, 4, "USER_ONE",
					CombatSkillRole{Function: "HEAL_FIXED", Target: "SELECT", Parameters: [10]string{"100", "0", "0", "0", "MND"}},
					CombatSkillRole{Function: "ATK_UP_FIXED", Target: "USER_ONE", Parameters: [10]string{"1", "ATK", "1000", "50"}},
				)
				if _, err := engine.executePlayerAction(action, nil); err != nil {
					t.Fatal(err)
				}
				for i, player := range engine.players {
					affected := i == 3 || tc.want&(1<<uint(actor-1)) != 0
					hp, attack := 500, 100
					if affected {
						hp, attack = 600, 150
					}
					if player.HP != hp || player.Attack != attack {
						t.Fatalf("actor %d target %d: HP=%d attack=%d, want %d/%d", actor, i+1, player.HP, player.Attack, hp, attack)
					}
				}
			}
		})
	}
}

func TestSingleToAllRecipientUnionAndExistingExpansion(t *testing.T) {
	engine := singleToAllFixture(t)
	for _, tc := range []struct {
		actor, target int
		scope         string
		want          uint8
	}{
		{1, 2, "SELF", 1},
		// Actor 1 already expands ordinary effects. Its selected recipient must
		// nevertheless stay actor 3 even when the outer USER_ONE becomes USER_ALL.
		{1, 3, "USER_ONE", 5},
		{2, 4, "SELF", 7},
	} {
		action := singleToAllAction(engine, 98000001, tc.actor, tc.target, "USER_ONE", CombatSkillRole{Function: CombatFunctionTurnSingleToAll, Target: tc.scope, Parameters: [10]string{"1"}})
		if _, err := engine.executePlayerAction(action, nil); err != nil {
			t.Fatal(err)
		}
		if engine.singleToAllMembers != tc.want {
			t.Fatalf("recipients = %04b, want %04b", engine.singleToAllMembers, tc.want)
		}
	}
	engine.phase = battlePhaseChaliceEnemy
	if _, err := engine.TurnPhase(); err != nil || engine.singleToAllMembers != 0 {
		t.Fatalf("scoped recipients survived next turn: %v", err)
	}
}

func TestSingleToAllExpandsCriticalAndSelfRecoveryForRecipients(t *testing.T) {
	for _, scope := range []string{"SELF", "USER_ONE", "USER_ALL"} {
		t.Run(scope, func(t *testing.T) {
			engine := singleToAllFixture(t)
			activator := singleToAllAction(engine, 98000001, 2, 3, "USER_ONE", CombatSkillRole{
				Function: CombatFunctionTurnSingleToAll, Target: scope, Parameters: [10]string{"1"},
			})
			if _, err := engine.executePlayerAction(activator, nil); err != nil {
				t.Fatal(err)
			}
			for actor := 1; actor <= 4; actor++ {
				for i := range engine.players {
					engine.players[i].Effects = nil
					engine.players[i].Recovery = 0
				}
				// Same target combination as the reported card: selected ally's
				// critical chance, followed by the caster's recovery parameter.
				action := singleToAllAction(engine, 100+actor, actor, 4, "USER_ONE",
					CombatSkillRole{Function: "CRITICAL_UP", Target: "SELECT", Parameters: [10]string{"2", "500", "0"}},
					CombatSkillRole{Function: "ATK_UP_FIXED", Target: "SELF", Parameters: [10]string{"2", "MND", "1000", "50"}},
				)
				if _, err := engine.executePlayerAction(action, nil); err != nil {
					t.Fatal(err)
				}
				expanded := scope == "USER_ALL" || scope == "SELF" && actor == 2 || scope == "USER_ONE" && actor == 3
				for i, player := range engine.players {
					wantRecovery, wantCritical := 0, 0
					if expanded || i+1 == actor {
						wantRecovery = 50
					}
					if expanded || i == 3 {
						wantCritical = 500
					}
					critical := 0
					for _, effect := range player.Effects {
						if effect.Function == "CRITICAL_UP" {
							critical += effect.Rate
						}
					}
					if player.Recovery != wantRecovery || critical != wantCritical {
						t.Fatalf("actor %d target %d: recovery=%d critical=%d, want %d/%d", actor, i+1, player.Recovery, critical, wantRecovery, wantCritical)
					}
				}
				if engine.catalog.PlayerSkillRoles[action.cardID][1].Target != "SELF" {
					t.Fatal("modified shared self-target master role")
				}
			}
		})
	}
}

func TestSingleToAllSelfDrainHealsEachLivingTarget(t *testing.T) {
	for _, active := range []bool{false, true} {
		engine := singleToAllFixture(t)
		engine.enemyCount = 1
		engine.players[0].HP = engine.players[0].MaxHP
		engine.players[2].HP = engine.players[2].MaxHP - 25
		engine.players[3].HP = 0
		if active {
			engine.singleToAllMembers = 1
		}
		action := singleToAllAction(engine, 101, 1, 5, "ENEMY_ONE",
			CombatSkillRole{Function: "ATTACK_AA", Target: "SELECT", Parameters: [10]string{"200", "0", "0", "0", "1", "ATK", "0", "FIRE", "PHYSICS"}},
			CombatSkillRole{Function: "ATK_OP_DRAIN", Target: "SELF", Parameters: [10]string{"50", "0"}},
		)
		if _, err := engine.executePlayerAction(action, nil); err != nil {
			t.Fatal(err)
		}
		wantSecond, wantThird := 500, engine.players[2].MaxHP-25
		if active {
			wantSecond, wantThird = 600, engine.players[2].MaxHP
		}
		if engine.players[0].HP != engine.players[0].MaxHP || engine.players[1].HP != wantSecond || engine.players[2].HP != wantThird || engine.players[3].HP != 0 {
			t.Fatalf("active=%v: full/dead caster or per-target cap changed drain: HP=%d/%d/%d/%d", active, engine.players[0].HP, engine.players[1].HP, engine.players[2].HP, engine.players[3].HP)
		}
	}
}
