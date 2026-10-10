package multiplayer

import "testing"

func TestCustomCardEditableAttackTargetScopes(t *testing.T) {
	for _, tc := range []struct {
		name, skillTarget, effectTarget string
		count                           int
	}{
		{"single inherited", "ENEMY_ONE", "SELECT", 1},
		{"all inherited", "ENEMY_ALL", "SELECT", 3},
		{"all explicit", "ENEMY_ONE", "ENEMY_ALL", 3},
		{"single explicit", "ENEMY_ALL", "ENEMY_ONE", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine, _ := nextBattleFixture(t)
			engine.enemyCount = 3
			for i := 0; i < 3; i++ {
				engine.enemies[i] = engine.enemies[0]
				engine.enemies[i].MemberType, engine.enemies[i].HP, engine.enemies[i].MaxHP = i+5, 10000, 10000
			}
			role := engine.catalog.PlayerSkillRoles[1][0]
			role.Target = tc.effectTarget
			skill := engine.catalog.PlayerSkills[1][0]
			skill.Target = tc.skillTarget
			if _, err := engine.executePlayerRole(battleAction{memberType: 1, cardLevel: 1, target: 6, skill: skill, roles: []CombatSkillRole{role}}, role, 1); err != nil {
				t.Fatal(err)
			}
			count := 0
			for i := 0; i < 3; i++ {
				if engine.enemies[i].HP < 10000 {
					count++
					if tc.count == 1 && i != 1 {
						t.Fatal("single attack ignored the chosen enemy")
					}
				}
			}
			if count != tc.count {
				t.Fatalf("damaged enemies = %d, want %d", count, tc.count)
			}
		})
	}
}

func TestCustomCardEditableBuffTargetScopes(t *testing.T) {
	for _, tc := range []struct {
		name, skillTarget, effectTarget string
		count                           int
	}{
		{"self inherited", "SELF", "SELECT", 1},
		{"single inherited", "USER_ONE", "SELECT", 1},
		{"all inherited", "USER_ALL", "SELECT", 4},
		{"all explicit", "SELF", "USER_ALL", 4},
		{"self explicit", "USER_ALL", "SELF", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine, _ := nextBattleFixture(t)
			before := [4]int{}
			for i := range engine.players {
				before[i] = engine.players[i].Attack
			}
			role := CombatSkillRole{Function: "ATK_UP_FIXED", Target: tc.effectTarget, Parameters: [10]string{"3", "ATK", "1000", "500", "0", "0"}}
			action := battleAction{memberType: 1, target: 2, cardLevel: 50, skill: CombatSkillDefinition{Target: tc.skillTarget}}
			if _, err := engine.executePlayerRole(action, role, 1); err != nil {
				t.Fatal(err)
			}
			count := 0
			for i := range engine.players {
				if delta := engine.players[i].Attack - before[i]; delta != 0 {
					if delta != 500 {
						t.Fatalf("buff delta = %d, want 500", delta)
					}
					count++
				}
			}
			if count != tc.count {
				t.Fatalf("buffed players = %d, want %d", count, tc.count)
			}
		})
	}
}
