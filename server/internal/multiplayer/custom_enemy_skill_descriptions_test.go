package multiplayer

import (
	"strings"
	"testing"
)

func TestCustomEnemySkillDescriptionUsesRuntimeBuffValueAndSide(t *testing.T) {
	role := CombatSkillRole{Function: "ATK_UP_FIXED", Target: "SELECT", Parameters: [10]string{"3", "ATK", "1000", "1000", "0", "0"}}
	skill := CombatSkillDefinition{Target: "USER_ALL"}
	effects, playerBuff := DescribeCustomEnemySkill(skill, []CombatSkillRole{role})
	if !playerBuff || len(effects) != 1 || effects[0] != "全体玩家：物理攻击增加 1000 点，持续 3 回合" {
		t.Fatalf("wrong player buff explanation: %v, %v", effects, playerBuff)
	}
	role.Target = "SELF"
	effects, playerBuff = DescribeCustomEnemySkill(skill, []CombatSkillRole{role})
	if playerBuff || !strings.Contains(effects[0], "Boss 部位自身") {
		t.Fatalf("boss buff mislabeled as player buff: %v", effects)
	}
	role.Target, role.Parameters[1], role.Parameters[0] = "SELECT", "INT", "4"
	attack := CombatSkillRole{Function: "ATTACK_AA", Target: "SELECT", Parameters: [10]string{"2000", "0", "1000", "0", "2", "INT"}}
	effects, playerBuff = DescribeCustomEnemySkill(skill, []CombatSkillRole{attack, role})
	if !playerBuff || len(effects) != 2 || !strings.Contains(effects[0], "魔法攻击，原攻击次数 2") || !strings.Contains(effects[1], "魔法攻击增加 1000 点，持续 4 回合") {
		t.Fatalf("mixed skill concealed attack or buff: %v", effects)
	}
}
