package multiplayer

import (
	"strconv"
	"testing"
)

// Reproduces 98000001's exported normal/Arthur skills, including its large
// independent IDs, USER_ALL/SELECT mix, priority and all four parameters.
func TestCustomCardFourParameterBuffAppliesBeforeSameTurnAttack(t *testing.T) {
	for _, skillID := range []int{1900000000, 1900000001} {
		t.Run(strconv.Itoa(skillID), func(t *testing.T) {
			engine, _ := nextBattleFixture(t)
			engine.enemies[0].HP, engine.enemies[0].MaxHP = 10000, 10000
			attack := engine.catalog.PlayerSkills[1][0]
			attack.PriorityPVE, attack.Kind = 430, "ATTACK"
			engine.catalog.PlayerSkills[1] = []CombatSkillDefinition{attack}
			engine.catalog.PlayerSkillRoles[1][0].Parameters = [10]string{"0", "0", "1000", "0", "1", "ATK", "0", "FIRE", "PHYSICS"}
			base, priority, want := "240000", 120, 340
			if skillID == 1900000001 {
				base, priority, want = "400000", 110, 500
			}
			support := CombatSkillDefinition{ID: skillID, FunctionID: skillID, Target: "USER_ALL", Attribute: "DARK", Kind: "SUPPORT", PriorityPVE: priority, Cost: 1}
			roles := []CombatSkillRole{}
			for i, parameter := range []string{"ATK", "INT", "DEF", "MDEF"} {
				function, target := "ATK_UP_FIXED", "SELECT"
				if i >= 2 {
					function = "DEF_UP_FIXED"
				}
				if skillID == 1900000000 && i < 3 {
					target = "USER_ALL"
				}
				roles = append(roles, CombatSkillRole{SkillID: skillID, RoleIndex: i, Function: function, Target: target,
					Parameters: [10]string{"3", parameter, "1", base, "2000", "0"}, ChainRate: 20})
			}
			engine.catalog.Cards[98000001] = CombatCardDefinition{ID: 98000001, NormalSkillID: skillID}
			engine.catalog.PlayerSkills[skillID], engine.catalog.PlayerSkillRoles[skillID] = []CombatSkillDefinition{support}, roles
			before := [4][4]int{}
			for i, p := range engine.players {
				before[i] = [4]int{p.Attack, p.Magic, p.Defense, p.MDefense}
			}
			// Submit attack first: the support's inherited priority must still
			// execute it before the attack, independently of selection order.
			rows, err := engine.executeUserActions([]battleAction{
				{memberType: 1, cardID: 1, cardType: 1, cardLevel: 1, target: 5, skill: attack},
				{memberType: 2, cardID: 98000001, cardType: 1, cardLevel: 50, skill: support},
			})
			if err != nil {
				t.Fatal(err)
			}
			for i, p := range engine.players {
				got := [4]int{p.Attack - before[i][0], p.Magic - before[i][1], p.Defense - before[i][2], p.MDefense - before[i][3]}
				if got != [4]int{want, want, want, want} {
					t.Fatalf("member %d same-turn buffs = %v, want %d each", i+1, got, want)
				}
				for _, effect := range p.Effects {
					if effect.AppliedTurn != engine.turn || effect.Remaining != 3 {
						t.Fatalf("buff was delayed or lost its duration: %+v", effect)
					}
				}
			}
			if got := 10000 - engine.enemies[0].HP; got != before[0][0]+want {
				t.Fatalf("same-turn attack damage = %d, buffed attack = %d", got, before[0][0]+want)
			}
			buffs, params, attackSeen := 0, 0, false
			for _, row := range rows {
				if row.Command == resultCardSkill && row.Args[2] == 1 {
					attackSeen = true
				}
				if row.Command == resultBuff || row.Command == resultBattleParam {
					if attackSeen {
						t.Fatal("client buff updates arrived after the attack")
					}
					if row.Command == resultBuff {
						buffs++
					} else {
						params++
					}
				}
			}
			if buffs != 16 || params != 16 {
				t.Fatalf("client updates: buffs=%d params=%d, want 16 each", buffs, params)
			}
		})
	}
}
