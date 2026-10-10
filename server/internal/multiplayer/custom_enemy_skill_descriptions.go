package multiplayer

import (
	"fmt"
	"strings"
)

// The editor reports the same fixed buff values as the battle engine. SELECT
// follows the skill scope; SELF/ENEMY_* belong to the boss side here.
func DescribeCustomEnemySkill(skill CombatSkillDefinition, roles []CombatSkillRole) (effects []string, playerBuff bool) {
	for _, role := range roles {
		target := role.Target
		if target == "SELECT" {
			target = skill.Target
		}
		who := customEnemyEffectTarget(target)
		parameter := map[string]string{"ATK": "物理攻击", "INT": "魔法攻击", "MND": "回复量", "DEF": "物理防御", "MDEF": "魔法防御", "HP": "HP"}[role.Parameters[1]]
		if parameter == "" {
			parameter = role.Parameters[1]
		}
		up := strings.HasPrefix(role.Function, "ATK_UP") || strings.HasPrefix(role.Function, "DEF_UP")
		if customPlayerSupportName(role) != "" && target != "SELF" && combatTargetIsPlayer(target) {
			playerBuff = true
		}
		switch role.Function {
		case "NONE", "OUTPUT_TEXT":
			continue
		case "ATK_UP_FIXED", "DEF_UP_FIXED", "ATK_BREAK_FIXED", "GUARD_BREAK_FIXED":
			verb := "增加"
			if !up {
				verb = "降低"
			}
			effects = append(effects, fmt.Sprintf("%s：%s%s %d 点，持续 %d 回合", who, parameter, verb, fixedBuffRoleValue(role, calibratedEnemySkillLevel(role.Function), 1), maxInt(1, combatParameterInt(role.Parameters[0]))))
		case "ATK_UP_BY_SELF_PARAM", "DEF_UP_BY_SELF_PARAM", "ATK_UP_BY_TARGET_PARAM", "ATK_UP_BY_NOW_TURN_DAMAGE", "ATK_BREAK_BY_SELF_PARAM", "ATK_BREAK_BY_TARGET_PARAM", "ATK_BREAK_BY_NOW_TURN_DAMAGE", "GUARD_BREAK_BY_SELF_PARAM", "GUARD_BREAK_BY_TARGET_PARAM", "GUARD_BREAK_BY_NOW_TURN_DAMAGE":
			verb := "提升"
			if !up {
				verb = "降低"
			}
			effects = append(effects, fmt.Sprintf("%s：%s%s，数值按招式的属性／伤害公式计算，持续 %d 回合", who, parameter, verb, maxInt(1, combatParameterInt(role.Parameters[0]))))
		case "ATTACK_AA":
			kind := "物理"
			if role.Parameters[5] == "INT" {
				kind = "魔法"
			}
			effects = append(effects, fmt.Sprintf("%s：%s攻击，原攻击次数 %d；本次威力可在下方伤害设置中修改", who, kind, maxInt(1, combatParameterInt(role.Parameters[4]))))
		case "HEAL_FIXED", "HEAL_BY_SELF_PARAM", "HEAL_BY_TARGET_MAXHP":
			effects = append(effects, who+"：回复 HP，回复数值沿用招式公式")
		default:
			if name := customPlayerSupportName(role); name != "" {
				detail := name
				for _, editor := range CustomEnemyBuffEditors(skill, []CombatSkillRole{role}) {
					for _, f := range editor.Fields {
						if f.Value != nil {
							detail += fmt.Sprintf("，%s %g", f.Label, *f.Value)
						} else {
							detail += "，" + f.Label + "按原公式计算"
						}
					}
				}
				effects = append(effects, who+"："+detail)
				continue
			}
			// Do not conceal extra effects on mixed attack/buff skills.
			effects = append(effects, who+"：附带效果 "+role.Function+"，沿用招式原参数")
		}
	}
	return effects, playerBuff
}

func customEnemyEffectTarget(target string) string {
	switch target {
	case "USER_ALL", "FRIEND_ALL":
		return "全体玩家"
	case "USER_ONE", "SELECT", "FRIEND_ONE":
		return "选中的单个玩家"
	case "MERCENARY":
		return "佣兵"
	case "MILLIONAIRE":
		return "富豪"
	case "THIEF":
		return "盗贼"
	case "SINGER":
		return "歌姬"
	case "SELF":
		return "施放招式的 Boss 部位自身"
	case "ENEMY_ALL":
		return "全部 Boss 部位"
	case "ENEMY_ONE":
		return "选中的 Boss 部位"
	default:
		return "招式目标（" + target + "）"
	}
}
