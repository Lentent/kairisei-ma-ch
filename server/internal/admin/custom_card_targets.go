package admin

import "errors"

func customCardSkillTargetAllowed(kind, target string) bool {
	if kind == "ATTACK" || kind == "SORCERY" {
		return target == "ENEMY_ONE" || target == "ENEMY_ALL"
	}
	switch target {
	case "SELF", "USER_ONE", "USER_ALL", "ENEMY_ONE", "ENEMY_ALL":
		return true
	}
	return false
}

func customCardEffectTargetAllowed(function, target string) bool {
	if target == "SELECT" {
		return true
	}
	if function == "ATTACK_AA" {
		return target == "ENEMY_ONE" || target == "ENEMY_ALL"
	}
	return target == "FRIEND_ALL" || customCardSkillTargetAllowed("", target)
}

// The native role enum can express a single chosen ally/enemy only via SELECT.
func validateCustomSingleRoleTargets(c customCard) error {
	for _, role := range c.Roles {
		if len(role) < 10 || isCustomSpecialRole(role) || (role[9] != "USER_ONE" && role[9] != "ENEMY_ONE") {
			continue
		}
		for _, skill := range c.Skills {
			fn := customRowInt(skill, 49)
			if fn == 0 {
				fn = customRowInt(skill, 0)
			}
			if fn == customRowInt(role, 0) && (len(skill) <= 19 || skill[19] != role[9]) {
				return errors.New("单体效果须与技能选择的单体目标一致，请先设置技能目标，再选择沿用技能选择目标")
			}
		}
	}
	return nil
}

// Native SkillData selects one target before evaluating its condition branches.
// Preserve historical rows unchanged, but apply edited targets to the whole root.
func validateCustomCardSharedTargets(c, base customCard) error {
	edited := map[string]bool{}
	for i, skill := range c.Skills {
		if skill[19] != base.Skills[i][19] {
			edited[skill[0]] = true
		}
	}
	targets := map[string]string{}
	for _, skill := range c.Skills {
		if !edited[skill[0]] {
			continue
		}
		if target, ok := targets[skill[0]]; ok && target != skill[19] {
			return errors.New("同一技能的条件分支须使用相同目标；普通技能与觉醒技能可分别设置")
		}
		targets[skill[0]] = skill[19]
	}
	return nil
}
