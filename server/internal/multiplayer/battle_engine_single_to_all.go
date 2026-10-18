package multiplayer

const CombatFunctionTurnSingleToAll = "TURN_SINGLE_TO_ALL"

func hasTurnSingleToAllRole(roles []CombatSkillRole) bool {
	for _, role := range roles {
		if role.Function == CombatFunctionTurnSingleToAll {
			return true
		}
	}
	return false
}

func singleToAllTarget(target string) string {
	switch target {
	case "USER_ONE", "FRIEND_ONE":
		return "USER_ALL"
	case "ENEMY_ONE":
		return "ENEMY_ALL"
	default:
		return target // SELF, all-target and special targeting retain their rules.
	}
}

func (engine *BattleEngine) applySingleToAll(action battleAction) battleAction {
	if !engine.singleToAllActive || action.sphereSlot != 0 || action.cardType < 1 || action.cardType > 10 {
		return action
	}
	target := singleToAllTarget(action.skill.Target)
	if target != action.skill.Target {
		action.skill.Target, action.target = target, 0
	}
	// Actions can share master roles and held-card slices. Rewrite only a copy.
	action.roles = append([]CombatSkillRole(nil), action.roles...)
	for i := range action.roles {
		action.roles[i].Target = singleToAllTarget(action.roles[i].Target)
	}
	return action
}
